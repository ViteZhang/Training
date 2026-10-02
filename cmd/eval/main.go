// eval 跑 AI 能力评测：读样本与人工标注，输出指标与是否达标（门槛见 PRD v3 12.3、dev-spec 第七节）。
//
// 样本放 evals/private/<能力>.jsonl（不提交）；没有时用 evals/samples/ 下的公开示例，只验证评测本身能跑。
// 每行一个样本：{"name": "...", "pages": ["第 1 页文字", ...] 或 "file": "相对 evals/private 的 .docx/.xlsx/.txt", "expected": [...]}
//   - import：expected 为 [{"stem": "题干", "answer": "答案，可为空"}]，指标是题干识别正确率与答案识别正确率，门槛 95%
//   - kp：expected 为 [{"name": "知识点名", "original_text": "原文表述"}]，指标是知识点召回率（门槛 85%）与原文一致率（门槛 90%）
//   - grading：每行一道题 {"name","subject","qtype","stem","reference","points":[{"seq","content","score"}],
//     "answers":[{"text":"考生答案","human":人工评分}]}；指标是与人工偏差 ≤ 1 分的比例（门槛 80%）
//     与重批一致性（同一答案批 3 次，最高最低分差 ≤ 1 分的比例，PRD 11.14 要求全部做到）
//
// 模型按环境变量选择：AI_PROVIDER=bailian（BAILIAN_BASE_URL、BAILIAN_API_KEY、AI_MODEL_STRONG、AI_MODEL_CHEAP），默认 mock。
// 其余能力（essay、ocr）在 T23、T19 实现。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/cloud"
	"peetraining-server/internal/config"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/importer"
)

var capabilities = []string{"import", "kp", "grading", "essay", "ocr"}

type sample struct {
	Name      string          `json:"name"`
	Subject   string          `json:"subject"`
	QType     string          `json:"qtype"`
	Stem      string          `json:"stem"`
	Reference string          `json:"reference"`
	Points    []ai.GradePoint `json:"points"`
	Answers   []struct {
		Text  string  `json:"text"`
		Human float64 `json:"human"`
	} `json:"answers"`
	Pages    []string          `json:"pages"`
	File     string            `json:"file"`
	Expected []json.RawMessage `json:"expected"`
}

func main() {
	capability := flag.String("cap", "", "评测能力：import、kp、grading、essay、ocr")
	dir := flag.String("dir", "evals", "评测目录")
	flag.Parse()
	if !slices.Contains(capabilities, *capability) {
		fmt.Fprintf(os.Stderr, "eval: 请用 -cap 指定能力，可选 %v\n", capabilities)
		os.Exit(2)
	}
	pass, err := run(context.Background(), *capability, *dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(2)
	}
	if !pass {
		os.Exit(1)
	}
}

func run(ctx context.Context, capability, dir string) (bool, error) {
	if capability != "import" && capability != "kp" && capability != "grading" {
		return false, fmt.Errorf("%s 评测尚未实现（见 docs/tasks 对应卡片）", capability)
	}
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	clients, err := cloud.New(cfg)
	if err != nil {
		return false, err
	}
	engine := ai.NewEngine(ai.Config{Client: clients.AI, UseMock: cfg.AI.Provider == config.ProviderMock,
		Models: ai.Models{Strong: cfg.AI.ModelStrong, Cheap: cfg.AI.ModelCheap}})
	file := filepath.Join(dir, "private", capability+".jsonl")
	base := filepath.Join(dir, "private")
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		file, base = filepath.Join(dir, "samples", capability+".jsonl"), filepath.Join(dir, "samples")
		fmt.Printf("没有 %s，改用公开示例（只验证评测能跑，不代表真实效果）\n", filepath.Join(dir, "private", capability+".jsonl"))
	}
	samples, err := load(file)
	if err != nil {
		return false, err
	}
	fmt.Printf("能力 %s · 样本 %d 份 · 模型 %s / %s · AI_PROVIDER=%s\n", capability, len(samples), cfg.AI.ModelStrong, cfg.AI.ModelCheap, cfg.AI.Provider)
	start := time.Now()
	var pass bool
	switch capability {
	case "import":
		pass, err = evalImport(ctx, engine, base, samples)
	case "grading":
		pass, err = evalGrading(ctx, engine, samples)
	default:
		pass, err = evalKP(ctx, engine, base, samples)
	}
	fmt.Printf("耗时 %s\n", time.Since(start).Round(time.Second))
	return pass, err
}

func load(path string) ([]sample, error) {
	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // 评测目录来自开发者本机的命令行参数
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []sample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var s sample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("%s 第 %d 行：%w", path, n, err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

// pages 取样本的分页文字：直接给的 pages，或本地解析 .docx / .xlsx / .txt（PDF 与图片请先用识别服务转成 pages）。
func pages(base string, s sample) ([]importer.Page, error) {
	var texts []string
	switch {
	case len(s.Pages) > 0:
		texts = s.Pages
	case s.File != "":
		data, err := os.ReadFile(filepath.Join(base, filepath.Clean("/"+s.File))) //nolint:gosec // 样本路径限定在评测目录内
		if err != nil {
			return nil, err
		}
		var res extract.Result
		switch strings.ToLower(filepath.Ext(s.File)) {
		case ".docx":
			res, err = extract.Docx(data, 1500)
		case ".xlsx":
			res, err = extract.Xlsx(data, 50)
		case ".txt":
			res = extract.Text(string(data), 1500)
		default:
			return nil, fmt.Errorf("%s：评测只直接读 .docx / .xlsx / .txt", s.File)
		}
		if err != nil {
			return nil, err
		}
		for _, p := range res.Pages {
			texts = append(texts, p.Text)
		}
	}
	out := make([]importer.Page, len(texts))
	for i, t := range texts {
		out[i] = importer.Page{No: i + 1, Text: t}
	}
	return out, nil
}

func similar(a, b string) bool {
	return ai.Overlap(a, b) >= 0.9 && ai.Overlap(b, a) >= 0.9
}

func pct(n, d int) float64 {
	if d == 0 {
		return 1
	}
	return float64(n) / float64(d)
}

func verdict(v, threshold float64) string {
	if v >= threshold {
		return "达标"
	}
	return "未达标"
}

func evalImport(ctx context.Context, e *ai.Engine, base string, samples []sample) (bool, error) {
	totalQ, stemOK, totalA, ansOK, failed := 0, 0, 0, 0, 0
	for _, s := range samples {
		ps, err := pages(base, s)
		if err != nil {
			return false, fmt.Errorf("%s：%w", s.Name, err)
		}
		var qs []ai.QItem
		answers := map[string]string{}
		for _, chunk := range importer.Chunk(importer.Split(ps), 6000) {
			out, _, err := ai.Structure.Run(ctx, e, 0, ai.StructureIn{Blocks: chunk})
			if err != nil {
				failed++
				continue
			}
			for _, it := range out.Items {
				if it.Kind == "answer" {
					answers[yearNo(it)] = it.Answer
				} else {
					qs = append(qs, it)
				}
			}
		}
		for i := range qs {
			if qs[i].Answer == "" {
				qs[i].Answer = answers[yearNo(qs[i])]
			}
		}
		sq := 0
		for _, raw := range s.Expected {
			var want struct{ Stem, Answer string }
			if err := json.Unmarshal(raw, &want); err != nil {
				return false, err
			}
			totalQ++
			idx := slices.IndexFunc(qs, func(q ai.QItem) bool { return similar(want.Stem, q.Stem) })
			if idx < 0 {
				continue
			}
			stemOK++
			sq++
			if want.Answer != "" {
				totalA++
				if similar(want.Answer, qs[idx].Answer) {
					ansOK++
				}
			}
		}
		fmt.Printf("  %-20s 题干 %d/%d  识别出 %d 题\n", s.Name, sq, len(s.Expected), len(qs))
	}
	stem, ans := pct(stemOK, totalQ), pct(ansOK, totalA)
	fmt.Printf("题干识别正确率 %.1f%%（%d/%d）%s；答案识别正确率 %.1f%%（%d/%d）%s；模型输出不合格的批次 %d\n",
		stem*100, stemOK, totalQ, verdict(stem, 0.95), ans*100, ansOK, totalA, verdict(ans, 0.95), failed)
	return stem >= 0.95 && ans >= 0.95, nil
}

func yearNo(q ai.QItem) string {
	y := ""
	if q.ExamYear != nil {
		y = fmt.Sprint(*q.ExamYear)
	}
	return y + "#" + q.QuestionNo
}

func evalKP(ctx context.Context, e *ai.Engine, base string, samples []sample) (bool, error) {
	total, recalled, predicted, verbatim, failed := 0, 0, 0, 0, 0
	for _, s := range samples {
		ps, err := pages(base, s)
		if err != nil {
			return false, fmt.Errorf("%s：%w", s.Name, err)
		}
		in := ai.KPIn{}
		for _, p := range ps {
			in.Pages = append(in.Pages, ai.KPPage{No: p.No, Text: p.Text})
		}
		out, _, err := ai.ExtractKPs.Run(ctx, e, 0, in)
		if err != nil {
			// 原文对不上时整批不合格，拆出的知识点都算不一致。
			failed++
		}
		predicted += len(out.Points)
		verbatim += len(out.Points)
		r := 0
		for _, raw := range s.Expected {
			var want struct{ Name string }
			if err := json.Unmarshal(raw, &want); err != nil {
				return false, err
			}
			total++
			if slices.ContainsFunc(out.Points, func(k ai.KPItem) bool {
				return importer.Normalize(k.Name) == importer.Normalize(want.Name) || similar(k.Name, want.Name)
			}) {
				recalled++
				r++
			}
		}
		fmt.Printf("  %-20s 召回 %d/%d  拆出 %d 个\n", s.Name, r, len(s.Expected), len(out.Points))
	}
	rec := pct(recalled, total)
	cons := pct(verbatim, predicted+failed)
	fmt.Printf("知识点召回率 %.1f%%（%d/%d）%s；原文一致率 %.1f%% %s；不合格批次 %d\n",
		rec*100, recalled, total, verdict(rec, 0.85), cons*100, verdict(cons, 0.9), failed)
	return rec >= 0.85 && cons >= 0.9, nil
}

// gradingRepeats 是一致性评测里同一答案的批改次数。
const gradingRepeats = 3

func evalGrading(ctx context.Context, e *ai.Engine, samples []sample) (bool, error) {
	total, close1, consistent, failed := 0, 0, 0, 0
	for _, s := range samples {
		in := ai.GradeIn{Subject: s.Subject, QType: s.QType, Stem: s.Stem, Reference: s.Reference, Points: s.Points}
		ok, cons := 0, 0
		for _, a := range s.Answers {
			in.Answer = a.Text
			var scores []float64
			for range gradingRepeats {
				out, _, err := ai.Grade.Run(ctx, e, 0, in)
				if err != nil {
					failed++
					continue
				}
				scores = append(scores, out.Total())
			}
			total++
			if len(scores) == 0 {
				continue
			}
			if d := scores[0] - a.Human; d <= 1 && d >= -1 {
				close1++
				ok++
			}
			if len(scores) == gradingRepeats && slices.Max(scores)-slices.Min(scores) <= 1 {
				consistent++
				cons++
			}
		}
		fmt.Printf("  %-20s 偏差 ≤ 1 分 %d/%d  三次一致 %d/%d\n", s.Name, ok, len(s.Answers), cons, len(s.Answers))
	}
	dev, con := pct(close1, total), pct(consistent, total)
	fmt.Printf("与人工偏差 ≤ 1 分 %.1f%%（%d/%d）%s；重批一致性 %.1f%%（%d/%d）%s；批改失败 %d 次\n",
		dev*100, close1, total, verdict(dev, 0.8), con*100, consistent, total, verdict(con, 1), failed)
	return dev >= 0.8 && con >= 1, nil
}
