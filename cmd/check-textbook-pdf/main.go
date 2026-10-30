// 本地真实样本探测：不初始化应用，不连接 MySQL/Redis/OSS，不发布教材。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/config"
	"peetraining-server/internal/rules"
)

type report struct {
	Pages             int    `json:"pages"`
	Rendered          int    `json:"rendered"`
	Recognized        int    `json:"recognized"`
	Chunks            int    `json:"chunks,omitempty"`
	VectorDimensions  int    `json:"vector_dimensions,omitempty"`
	CitationCount     int    `json:"citation_count,omitempty"`
	Coverage          string `json:"coverage,omitempty"`
	Calls             []call `json:"calls,omitempty"`
	DurationMS        int64  `json:"duration_ms"`
	DatabaseConnected bool   `json:"database_connected"`
}
type call struct {
	Capability   string `json:"capability"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	DurationMS   int64  `json:"duration_ms"`
	ErrorKind    string `json:"error_kind,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Sample check failed:", err)
		os.Exit(1)
	}
}
func run() (runErr error) {
	pdf := flag.String("pdf", "", "local authorized PDF")
	output := flag.String("out", "", "private output directory outside repository")
	pagesFlag := flag.String("pages", "4,5,9", "comma-separated physical PDF page numbers to recognize")
	recognize := flag.Bool("recognize", false, "use configured real vision model; otherwise only render all pages")
	query := flag.String("query", "文学理论有哪些基本形态？", "test question; empty skips embedding and answer test")
	flag.Parse()
	if *pdf == "" || *output == "" {
		return errors.New("-pdf and -out are required")
	}
	pages := map[int]bool{}
	for _, n := range strings.Split(*pagesFlag, ",") {
		v, e := strconv.Atoi(n)
		if e != nil || v < 1 {
			return errors.New("invalid -pages")
		}
		pages[v] = true
	}
	data, err := os.ReadFile(*pdf)
	if err != nil {
		return errors.New("cannot read sample PDF")
	}
	if len(data) > 50<<20 {
		return errors.New("sample exceeds 50 MiB")
	}
	if err = os.MkdirAll(*output, 0o700); err != nil {
		return err
	}
	start := time.Now()
	r := report{}
	// 即使中途失败也保存调用计数和安全错误类别，不记录供应商响应或教材正文。
	defer func() {
		r.DurationMS = time.Since(start).Milliseconds()
		raw, e := json.MarshalIndent(r, "", "  ")
		if e == nil {
			e = os.WriteFile(filepath.Join(*output, "report.json"), raw, 0o600)
		}
		if runErr == nil && e != nil {
			runErr = e
		}
		_ = json.NewEncoder(os.Stdout).Encode(r)
	}()
	var engine *ai.Engine
	var cfg *config.Config
	if *recognize {
		cfg, err = config.Load()
		if err != nil {
			return errors.New("invalid configuration")
		}
		var client *cloudai.OpenAI
		var model string
		switch cfg.AI.Provider {
		case "relay":
			if cfg.AI.RelayBaseURL == "" || cfg.AI.RelayAPIKey == "" {
				return errors.New("configure AI_RELAY_BASE_URL and AI_RELAY_API_KEY")
			}
			client = cloudai.NewOpenAI(cfg.AI.RelayBaseURL, cfg.AI.RelayAPIKey)
			model = cfg.AI.RelayModelStrong
		case "bailian":
			if cfg.AI.BailianBaseURL == "" || cfg.AI.BailianAPIKey == "" {
				return errors.New("configure BAILIAN_BASE_URL and BAILIAN_API_KEY")
			}
			client = cloudai.NewOpenAI(cfg.AI.BailianBaseURL, cfg.AI.BailianAPIKey)
			client.Thinking = &cfg.AI.BailianThinking
			model = cfg.AI.ModelStrong
		default:
			return errors.New("real vision model required; mock cannot accept this sample")
		}
		engine = ai.NewEngine(ai.Config{Client: client, Models: ai.Models{Strong: model, Cheap: model}, OnCall: func(c ai.Call) {
			r.Calls = append(r.Calls, call{c.Capability, c.InputTokens, c.OutputTokens, c.Latency.Milliseconds(), c.ErrorKind})
		}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	sections := []rules.TextbookSection{}
	err = ocr.NewPDFium(nil).RenderAllPages(ctx, data, 1000, func(total, page int, jpeg []byte) error {
		r.Pages = total
		r.Rendered++
		if !pages[page] {
			return nil
		}
		if err := os.WriteFile(filepath.Join(*output, fmt.Sprintf("page-%03d.jpg", page)), jpeg, 0o600); err != nil {
			return err
		}
		if !*recognize {
			return nil
		}
		recognized, _, e := ai.RecognizeTextbook.Run(ctx, engine, 0, ai.TextbookPageIn{PageNo: page, Image: cloudai.Image{Data: jpeg}})
		if e != nil {
			return fmt.Errorf("vision recognition failed for page %d", page)
		}
		r.Recognized++
		raw, e := json.MarshalIndent(recognized, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(*output, fmt.Sprintf("page-%03d.json", page)), raw, 0o600); e != nil {
			return e
		}
		for _, b := range recognized.Blocks {
			printed := recognized.PrintedPage
			pageNo := page
			sec := rules.TextbookSection{Heading: b.Heading, Text: b.Text, SourcePage: &pageNo, NeedsReview: b.Uncertain}
			if printed != "" {
				sec.PrintedPage = &printed
			}
			sections = append(sections, sec)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("PDF rendering/recognition failed: %w", err)
	}
	for page := range pages {
		if page > r.Pages {
			return errors.New("requested page is outside sample")
		}
	}
	if engine != nil && *query != "" {
		chunks, e := rules.TextbookChunks(sections, rules.TextbookRetrievalParams{Version: "sample-v1", ChunkRunes: 700, OverlapRunes: 100, RecallK: 20, ResultK: 8, ContextRunes: 6000})
		if e != nil {
			return e
		}
		r.Chunks = len(chunks)
		c := cfg.Textbook
		c.EmbeddingProvider = "openai"
		if c.Validate(config.EnvLocal) != nil {
			return errors.New("invalid embedding configuration")
		}
		embedder := ai.NewEmbedder(c).WithAudit(engine)
		vectors := [][]float64{}
		for i := 0; i < len(chunks); i += 16 {
			inputs := []string{}
			for _, ch := range chunks[i:min(i+16, len(chunks))] {
				inputs = append(inputs, ch.Text)
			}
			v, e := embedder.Embed(ctx, inputs)
			if e != nil {
				return errors.New("sample document embedding failed")
			}
			vectors = append(vectors, v...)
		}
		qv, e := embedder.Embed(ctx, []string{*query})
		if e != nil {
			return errors.New("sample query embedding failed")
		}
		r.VectorDimensions = len(qv[0])
		type ranked struct {
			index int
			score float64
		}
		ranks := []ranked{}
		for i, v := range vectors {
			ranks = append(ranks, ranked{i, cosine(v, qv[0])})
		}
		sort.Slice(ranks, func(i, j int) bool { return ranks[i].score > ranks[j].score })
		fragments := []ai.EvidenceFragment{}
		for _, rank := range ranks[:min(8, len(ranks))] {
			ch := chunks[rank.index]
			sec := sections[ch.Section-1]
			fragments = append(fragments, ai.EvidenceFragment{ID: fmt.Sprintf("sample:%d", ch.Ordinal), Kind: "official_textbook", Title: "文学理论教程", Author: "童庆炳", Edition: "第五版（测试样本）", Heading: ch.Heading, Text: ch.Text, SourcePage: sec.SourcePage, PrintedPage: sec.PrintedPage})
		}
		selection, _, e := ai.SelectAnswerEvidence.Run(ctx, engine, 0, ai.SelectEvidenceIn{Subject: "文学理论", QType: "short_answer", Stem: *query, Evidence: fragments})
		if e != nil {
			return errors.New("sample semantic selection failed")
		}
		selected := []ai.EvidenceFragment{}
		for _, id := range selection.SourceIDs {
			for _, f := range fragments {
				if f.ID == id {
					selected = append(selected, f)
				}
			}
		}
		answer, _, e := engine.GenerateReferenceAnswer(ctx, 0, ai.GenerateAnswerIn{Subject: "文学理论", QType: "short_answer", Stem: *query, Score: 10, Coverage: selection.Coverage, Evidence: selected})
		if e != nil {
			return errors.New("sample answer/citation validation failed")
		}
		r.CitationCount = len(answer.Snapshot.Sources)
		r.Coverage = selection.Coverage
		raw, e := json.MarshalIndent(answer.Snapshot, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(*output, "answer-evidence.json"), raw, 0o600); e != nil {
			return e
		}
		raw, e = json.MarshalIndent(answer, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(*output, "answer.json"), raw, 0o600); e != nil {
			return e
		}
	}
	return nil
}
func cosine(a, b []float64) float64 {
	dot, na, nb := 0., 0., 0.
	for i, v := range a {
		dot += v * b[i]
		na += v * v
		nb += b[i] * b[i]
	}
	return dot / math.Sqrt(na*nb)
}
