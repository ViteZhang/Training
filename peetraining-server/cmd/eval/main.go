// eval 跑 AI 能力评测：读样本与人工标注，输出指标与是否达标（门槛见 PRD v3 12.3、dev-spec 第七节）。
//
// 样本放 evals/private/<能力>.jsonl（不提交）；没有时用 evals/samples/ 下的公开示例，只验证评测本身能跑。
// 每行一个样本：{"name": "...", "pages": ["第 1 页文字", ...] 或 "file": "相对 evals/private 的 .docx/.xlsx/.txt", "expected": [...]}
//   - import：expected 为 [{"stem": "题干", "answer": "答案，可为空"}]，指标是题干识别正确率与答案识别正确率，门槛 95%
//   - kp：expected 为 [{"name": "知识点名", "original_text": "原文表述", "rubric": ["采分点", ...]}]，指标是知识点召回率（门槛 85%）、
//     原文一致率（门槛 90%）与采分点召回率（标了 rubric 时，门槛 85%）
//   - rubric：每行一道题 {"name","qtype","stem","reference","score","points":[{"content":"采分点"}]}，指标是从参考答案提采分点的召回率（门槛 85%）
//   - grading：每行一道题 {"name","subject","qtype","stem","reference","points":[{"seq","content","score"}],
//     "answers":[{"text":"考生答案","human":人工评分}]}；指标是与人工偏差 ≤ 1 分的比例（门槛 80%）
//     与重批一致性（同一答案批 3 次，最高最低分差 ≤ 1 分的比例，PRD 11.14 要求全部做到）
//
// 模型按环境变量选择：AI_PROVIDER=bailian（BAILIAN_BASE_URL、BAILIAN_API_KEY、AI_MODEL_STRONG、AI_MODEL_CHEAP），默认 mock。
//
//   - essay：每行一个题目 {"name","subject","topic","required_words","dimensions":[{"name","score","description"}],
//     "essays":[{"text":"作文全文，空行分段","human":人工总分}]}；指标是与人工总分偏差 ≤ 10 分的比例（门槛 75%）
//     与重批一致性（同一篇批 3 次，最高最低分差 ≤ 6 分，PRD 11.13 要求全部做到）
//
//   - ocr：每行一张图 {"name","file":"相对 evals/private 的图片","handwriting":true,"text":"人工逐字转写"}；指标是字准确率
//     （1 − 编辑距离 / 字数，门槛 95%）。默认 OCR_PROVIDER=mock 把文件内容当识别结果，只验证评测能跑
//
// 每次评测另外输出每千次调用成本（需配 AI_PRICES）与时延 P50 / P95。
// -model、-prompt 指定这次用的模型与提示词版本，对比候选版；-record 把结果追加到 evals/README.md 的记录表。
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
	"sync"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/cloud"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/config"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/importer"
)

var capabilities = []string{"import", "kp", "rubric", "grading", "essay", "ocr"}

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
	Topic         string        `json:"topic"`
	RequiredWords int           `json:"required_words"`
	Dimensions    []ai.EssayDim `json:"dimensions"`
	Essays        []struct {
		Text  string  `json:"text"`
		Human float64 `json:"human"`
	} `json:"essays"`
	Pages       []string          `json:"pages"`
	File        string            `json:"file"`
	Expected    []json.RawMessage `json:"expected"`
	Score       float64           `json:"score"`
	Handwriting bool              `json:"handwriting"`
	Text        string            `json:"text"`
}

// capOf 是每项评测主要用到的模型能力（-prompt 指定的版本作用在它上面）。
func capOf(capability string) string {
	switch capability {
	case "import":
		return ai.Structure.Name
	case "kp":
		return ai.ExtractKPs.Name
	case "rubric":
		return ai.ExtractRubric.Name
	case "grading":
		return ai.Grade.Name
	case "essay":
		return ai.EssayGrade.Name
	}
	return ""
}

// stats 汇总模型调用的成本与时延。
type stats struct {
	mu        sync.Mutex
	calls     int
	failed    int
	costMicro int64
	latencies []time.Duration
	versions  map[string]bool
}

func (s *stats) add(c ai.Call) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if c.ErrorKind != "" {
		s.failed++
	}
	s.costMicro += c.CostMicroYuan
	s.latencies = append(s.latencies, c.Latency)
	if s.versions == nil {
		s.versions = map[string]bool{}
	}
	s.versions[c.Capability+"@"+c.Version+" · "+c.Model] = true
}

func (s *stats) percentile(p float64) time.Duration {
	if len(s.latencies) == 0 {
		return 0
	}
	ls := slices.Clone(s.latencies)
	slices.Sort(ls)
	return ls[min(len(ls)-1, int(float64(len(ls))*p))]
}

// summary 是成本与时延：每千次调用多少元、P50 / P95。
func (s *stats) summary() string {
	if s.calls == 0 {
		return "没有调用模型"
	}
	per1k := float64(s.costMicro) / float64(s.calls) * 1000 / 1e6
	return fmt.Sprintf("调用 %d 次（失败 %d）· 每千次 ¥%.2f · 时延 P50 %s P95 %s", s.calls, s.failed, per1k,
		s.percentile(0.5).Round(10*time.Millisecond), s.percentile(0.95).Round(10*time.Millisecond))
}

func (s *stats) versionList() string {
	vs := make([]string, 0, len(s.versions))
	for v := range s.versions {
		vs = append(vs, v)
	}
	slices.Sort(vs)
	return strings.Join(vs, "；")
}

// result 是一项评测的结论。
type result struct {
	pass    bool
	metrics string
}

func main() {
	capability := flag.String("cap", "", "评测能力：import、kp、grading、essay、ocr")
	dir := flag.String("dir", "evals", "评测目录")
	model := flag.String("model", "", "这次评测用的模型（默认按 AI_PROVIDER 与 AI_MODEL_* 选）")
	prompt := flag.String("prompt", "", "这次评测用的提示词版本，如 v2（对应 internal/ai/prompts/<能力>@<版本>.tmpl）")
	record := flag.Bool("record", false, "把结果追加到 <dir>/README.md 的记录表")
	note := flag.String("note", "", "记录表的备注（改了什么）")
	flag.Parse()
	if !slices.Contains(capabilities, *capability) {
		fmt.Fprintf(os.Stderr, "eval: 请用 -cap 指定能力，可选 %v\n", capabilities)
		os.Exit(2)
	}
	pass, err := run(context.Background(), opts{capability: *capability, dir: *dir, model: *model, prompt: *prompt, record: *record, note: *note})
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(2)
	}
	if !pass {
		os.Exit(1)
	}
}

type opts struct {
	capability, dir, model, prompt, note string
	record                               bool
}

func run(ctx context.Context, o opts) (bool, error) {
	capability, dir := o.capability, o.dir
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	clients, err := cloud.New(cfg)
	if err != nil {
		return false, err
	}
	models, fallback := ai.Routing(cfg.AI, clients.AIFallback)
	st := &stats{}
	override := map[string]ai.Meta{}
	if o.model != "" || o.prompt != "" {
		if o.model != "" {
			models.Strong, models.Cheap = o.model, o.model
		}
		if c := capOf(capability); c != "" && o.prompt != "" {
			override[c] = ai.Meta{Version: o.prompt}
		}
	}
	engine := ai.NewEngine(ai.Config{Client: clients.AI, Fallback: fallback, UseMock: cfg.AI.Provider == config.ProviderMock, Models: models,
		Prices: ai.PricesFrom(cfg.AI), Override: override, OnCall: st.add})
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
	fb := "无"
	if fallback != nil {
		fb = fallback.Models.Strong
	}
	fmt.Printf("能力 %s · 样本 %d 份 · 模型 %s / %s · 备用 %s · AI_PROVIDER=%s\n", capability, len(samples), models.Strong, models.Cheap, fb, cfg.AI.Provider)
	start := time.Now()
	var r result
	switch capability {
	case "import":
		r, err = evalImport(ctx, engine, base, samples)
	case "rubric":
		r, err = evalRubric(ctx, engine, samples)
	case "grading":
		r, err = evalGrading(ctx, engine, samples)
	case "essay":
		r, err = evalEssay(ctx, engine, samples)
	case "ocr":
		r, err = evalOCR(ctx, clients.OCR, base, samples, st)
	default:
		r, err = evalKP(ctx, engine, base, samples)
	}
	if err != nil {
		return false, err
	}
	fmt.Printf("%s\n耗时 %s\n", st.summary(), time.Since(start).Round(time.Second))
	if o.record {
		model := st.versionList()
		if capability == "ocr" {
			model = "OCR_PROVIDER=" + cfg.OCR.Provider
		}
		if err := appendRecord(filepath.Join(dir, "README.md"), capability, len(samples), r, model, st.summary(), o.note, base); err != nil {
			return r.pass, err
		}
		fmt.Println("已追加到", filepath.Join(dir, "README.md"))
	}
	return r.pass, nil
}

// appendRecord 在 README 的记录表末尾加一行。公开示例跑出来的结果标明「公开示例」，不当真实效果。
func appendRecord(readme, capability string, n int, r result, model, cost, note, base string) error {
	b, err := os.ReadFile(filepath.Clean(readme)) //nolint:gosec // 评测目录来自开发者本机的命令行参数
	if err != nil {
		return err
	}
	if filepath.Base(base) == "samples" {
		note = strings.TrimSpace("公开示例 " + note)
	}
	ok := "否"
	if r.pass {
		ok = "是"
	}
	cell := func(v string) string { return strings.ReplaceAll(v, "|", "/") }
	row := fmt.Sprintf("| %s | %s | %d | %s | %s | %s | %s | %s |\n", time.Now().Format("2006-01-02"), capability, n, cell(r.metrics), ok, cell(model), cell(cost), cell(note))
	s := strings.TrimRight(string(b), "\n") + "\n" + row
	return os.WriteFile(filepath.Clean(readme), []byte(s), 0o600) //nolint:gosec // 评测目录来自开发者本机的命令行参数
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

func evalImport(ctx context.Context, e *ai.Engine, base string, samples []sample) (result, error) {
	totalQ, stemOK, totalA, ansOK, failed := 0, 0, 0, 0, 0
	for _, s := range samples {
		ps, err := pages(base, s)
		if err != nil {
			return result{}, fmt.Errorf("%s：%w", s.Name, err)
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
				return result{}, err
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
	m := fmt.Sprintf("题干识别正确率 %.1f%%（%d/%d）%s；答案识别正确率 %.1f%%（%d/%d）%s；模型输出不合格的批次 %d",
		stem*100, stemOK, totalQ, verdict(stem, 0.95), ans*100, ansOK, totalA, verdict(ans, 0.95), failed)
	fmt.Println(m)
	return result{pass: stem >= 0.95 && ans >= 0.95, metrics: m}, nil
}

func yearNo(q ai.QItem) string {
	y := ""
	if q.ExamYear != nil {
		y = fmt.Sprint(*q.ExamYear)
	}
	return y + "#" + q.QuestionNo
}

func evalKP(ctx context.Context, e *ai.Engine, base string, samples []sample) (result, error) {
	total, recalled, predicted, verbatim, failed := 0, 0, 0, 0, 0
	rubricTotal, rubricHit := 0, 0
	for _, s := range samples {
		ps, err := pages(base, s)
		if err != nil {
			return result{}, fmt.Errorf("%s：%w", s.Name, err)
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
			var want struct {
				Name   string
				Rubric []string
			}
			if err := json.Unmarshal(raw, &want); err != nil {
				return result{}, err
			}
			total++
			idx := slices.IndexFunc(out.Points, func(k ai.KPItem) bool {
				return importer.Normalize(k.Name) == importer.Normalize(want.Name) || similar(k.Name, want.Name)
			})
			if idx >= 0 {
				recalled++
				r++
			}
			// 采分点召回：标注的每个采分点能在拆出的同名知识点的采分点里找到意思相同的（没拆出这个知识点算全部没召回）。
			for _, w := range want.Rubric {
				rubricTotal++
				if idx >= 0 && rubricHit1(w, out.Points[idx].RubricPoints) {
					rubricHit++
				}
			}
		}
		fmt.Printf("  %-20s 召回 %d/%d  拆出 %d 个\n", s.Name, r, len(s.Expected), len(out.Points))
	}
	rec := pct(recalled, total)
	cons := pct(verbatim, predicted+failed)
	rub := pct(rubricHit, rubricTotal)
	m := fmt.Sprintf("知识点召回率 %.1f%%（%d/%d）%s；原文一致率 %.1f%% %s；不合格批次 %d",
		rec*100, recalled, total, verdict(rec, 0.85), cons*100, verdict(cons, 0.9), failed)
	if rubricTotal > 0 {
		m += fmt.Sprintf("；采分点召回率 %.1f%%（%d/%d）%s", rub*100, rubricHit, rubricTotal, verdict(rub, 0.85))
	}
	fmt.Println(m)
	return result{pass: rec >= 0.85 && cons >= 0.9 && rub >= 0.85, metrics: m}, nil
}

// rubricHit1 判断标注的采分点能不能在模型给的采分点里找到：意思相同按字面重合度 ≥ 0.6 算（采分点常有同义改写）。
func rubricHit1(want string, got []ai.RubricPoint) bool {
	return slices.ContainsFunc(got, func(p ai.RubricPoint) bool {
		return ai.Overlap(want, p.Content) >= 0.6 || ai.Overlap(p.Content, want) >= 0.6
	})
}

// evalRubric 评测从参考答案提采分点（录题、导入时没有采分点的题都靠它）：标注的采分点召回率 ≥ 85%。
func evalRubric(ctx context.Context, e *ai.Engine, samples []sample) (result, error) {
	total, hit, failed := 0, 0, 0
	for _, s := range samples {
		score := s.Score
		if score == 0 {
			score = 10
		}
		out, _, err := ai.ExtractRubric.Run(ctx, e, 0, ai.RubricIn{QType: s.QType, Stem: s.Stem, Answer: s.Reference, Score: score})
		if err != nil {
			failed++
		}
		h := 0
		for _, p := range s.Points {
			total++
			if err == nil && rubricHit1(p.Content, out.Points) {
				hit++
				h++
			}
		}
		fmt.Printf("  %-20s 召回 %d/%d  提出 %d 个\n", s.Name, h, len(s.Points), len(out.Points))
	}
	rec := pct(hit, total)
	m := fmt.Sprintf("采分点召回率 %.1f%%（%d/%d）%s；失败 %d 题", rec*100, hit, total, verdict(rec, 0.85), failed)
	fmt.Println(m)
	return result{pass: rec >= 0.85, metrics: m}, nil
}

// evalOCR 评测手写与印刷识别的字准确率（PRD 12.3：手写识别字准确率 ≥ 95%）：1 − 总编辑距离 / 人工转写总字数。
// 比较前去掉空白，标点统一成全角，避免换行和中英文标点差异算成错字。
func evalOCR(ctx context.Context, rec ocr.Recognizer, base string, samples []sample, st *stats) (result, error) {
	totalChars, totalEdits, failed := 0, 0, 0
	for _, s := range samples {
		data, err := os.ReadFile(filepath.Join(base, filepath.Clean("/"+s.File))) //nolint:gosec // 样本路径限定在评测目录内
		if err != nil {
			return result{}, fmt.Errorf("%s：%w", s.Name, err)
		}
		start := time.Now()
		got, err := rec.Recognize(ctx, ocr.Image{Data: data, Handwriting: s.Handwriting})
		st.add(ai.Call{Capability: "ocr", Model: "ocr", Latency: time.Since(start), ErrorKind: errKind(err)})
		want := []rune(ocrNormalize(s.Text))
		totalChars += len(want)
		if err != nil {
			failed++
			totalEdits += len(want)
			continue
		}
		d := editDistance(want, []rune(ocrNormalize(got.Text)))
		totalEdits += d
		fmt.Printf("  %-20s 字准确率 %.1f%%（%d 字，错 %d）\n", s.Name, (1-pct(d, len(want)))*100, len(want), d)
	}
	acc := 1 - pct(totalEdits, totalChars)
	if totalChars == 0 {
		acc = 0
	}
	m := fmt.Sprintf("字准确率 %.1f%%（%d 字）%s；识别失败 %d 张", acc*100, totalChars, verdict(acc, 0.95), failed)
	fmt.Println(m)
	return result{pass: acc >= 0.95, metrics: m}, nil
}

func errKind(err error) string {
	if err != nil {
		return "error"
	}
	return ""
}

var punct = strings.NewReplacer(",", "，", ".", "。", ";", "；", ":", "：", "?", "？", "!", "！", "(", "（", ")", "）")

func ocrNormalize(s string) string {
	s = strings.Join(strings.Fields(s), "")
	return punct.Replace(s)
}

// editDistance 是按字计算的编辑距离（插入、删除、替换各算 1）。
func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := prev[j-1]
			if a[i-1] != b[j-1] {
				c++
			}
			cur[j] = min(c, prev[j]+1, cur[j-1]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// gradingRepeats 是一致性评测里同一答案的批改次数。
const gradingRepeats = 3

func evalGrading(ctx context.Context, e *ai.Engine, samples []sample) (result, error) {
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
	m := fmt.Sprintf("与人工偏差 ≤ 1 分 %.1f%%（%d/%d）%s；重批一致性 %.1f%%（%d/%d）%s；批改失败 %d 次",
		dev*100, close1, total, verdict(dev, 0.8), con*100, consistent, total, verdict(con, 1), failed)
	fmt.Println(m)
	return result{pass: dev >= 0.8 && con >= 1, metrics: m}, nil
}

// 作文批改评测（PRD 12.3、11.13）：与人工总分偏差 ≤ 10 分的比例 ≥ 75%；同一篇批 3 次总分差 ≤ 6 分。
const (
	essayRepeats   = 3
	essayDeviation = 10.0
	essayRepeatGap = 6.0
)

func essayParagraphs(text string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func evalEssay(ctx context.Context, e *ai.Engine, samples []sample) (result, error) {
	total, close10, consistent, failed := 0, 0, 0, 0
	for _, s := range samples {
		in := ai.EssayGradeIn{Subject: s.Subject, Topic: s.Topic, RequiredWords: s.RequiredWords, Dimensions: s.Dimensions}
		ok, cons := 0, 0
		for _, es := range s.Essays {
			in.Paragraphs = essayParagraphs(es.Text)
			var scores []float64
			for range essayRepeats {
				out, _, err := ai.EssayGrade.Run(ctx, e, 0, in)
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
			if d := scores[0] - es.Human; d <= essayDeviation && d >= -essayDeviation {
				close10++
				ok++
			}
			if len(scores) == essayRepeats && slices.Max(scores)-slices.Min(scores) <= essayRepeatGap {
				consistent++
				cons++
			}
			fmt.Printf("    人工 %.1f · AI %v\n", es.Human, scores)
		}
		fmt.Printf("  %-20s 偏差 ≤ 10 分 %d/%d  三次分差 ≤ 6 分 %d/%d\n", s.Name, ok, len(s.Essays), cons, len(s.Essays))
	}
	dev, con := pct(close10, total), pct(consistent, total)
	m := fmt.Sprintf("与人工偏差 ≤ 10 分 %.1f%%（%d/%d）%s；重批一致性 %.1f%%（%d/%d）%s；批改失败 %d 次",
		dev*100, close10, total, verdict(dev, 0.75), con*100, consistent, total, verdict(con, 1), failed)
	fmt.Println(m)
	return result{pass: dev >= 0.75 && con >= 1, metrics: m}, nil
}
