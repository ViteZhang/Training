package ai

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/dbq"
)

type fakeQ struct {
	dbq.Querier
	mu      sync.Mutex
	calls   []dbq.InsertAICallParams
	rollout *dbq.AiRollout
}

func (f *fakeQ) InsertAICall(_ context.Context, p dbq.InsertAICallParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	return nil
}

func (f *fakeQ) GetAIRollout(context.Context, string) (dbq.AiRollout, error) {
	if f.rollout == nil {
		return dbq.AiRollout{}, sql.ErrNoRows
	}
	return *f.rollout, nil
}

type failingClient struct{}

func (failingClient) Complete(context.Context, cloudai.Request) (cloudai.Response, error) {
	return cloudai.Response{}, cloudai.ErrRateLimited
}

func sampleBlocks() StructureIn {
	return StructureIn{Subject: "中国语言文学基础", Blocks: []Block{
		{ID: 1, Page: 1, Section: "一、名词解释（每题 5 分）", QType: "term", Score: 5, Year: 2024, No: "1", Text: "1. 意境 答案：意境是情景交融的艺术境界。"},
		{ID: 2, Page: 1, Section: "二、单项选择", QType: "", Year: 2024, No: "2", Text: "2. 下列属于唐传奇的是\nA. 莺莺传\nB. 搜神记"},
		{ID: 3, Page: 2, Answer: true, No: "2", Year: 2024, Text: "2. A"},
		{ID: 4, Page: 3, Whole: true, Text: "没有题号的一整页"},
	}}
}

func TestAllCapabilitiesCompileAndRender(t *testing.T) {
	inputs := map[string]any{
		Structure.Name:      sampleBlocks(),
		ExtractRubric.Name:  RubricIn{QType: "term", Stem: "意境", Answer: "情景交融。", Score: 5},
		GenerateAnswer.Name: GenerateAnswerIn{Subject: "x", QType: "term", Stem: "意境", Score: 5},
		ExtractKPs.Name:     KPIn{Subject: "x", Pages: []KPPage{{No: 1, Text: "意境：情景交融"}}},
		TagQuestions.Name:   TagIn{Subject: "x", Existing: [][]string{{"a", "b", "c"}}, Questions: []TagQ{{ID: 1, QType: "term", Stem: "意境"}}},
	}
	schemas := map[string]func() error{
		Structure.Name:      func() error { _, err := Structure.compiled(); return err },
		ExtractRubric.Name:  func() error { _, err := ExtractRubric.compiled(); return err },
		GenerateAnswer.Name: func() error { _, err := GenerateAnswer.compiled(); return err },
		ExtractKPs.Name:     func() error { _, err := ExtractKPs.compiled(); return err },
		TagQuestions.Name:   func() error { _, err := TagQuestions.compiled(); return err },
	}
	versions := map[string]string{Structure.Name: Structure.Version, ExtractRubric.Name: ExtractRubric.Version,
		GenerateAnswer.Name: GenerateAnswer.Version, ExtractKPs.Name: ExtractKPs.Version, TagQuestions.Name: TagQuestions.Version}
	for name, in := range inputs {
		if err := schemas[name](); err != nil {
			t.Errorf("%s 的 Schema 无效：%v", name, err)
		}
		sys, usr, err := render(name, versions[name], in)
		if err != nil || sys == "" || usr == "" {
			t.Errorf("%s 提示词渲染失败：%v", name, err)
		}
	}
	if _, _, err := render("nope", "v1", nil); err == nil {
		t.Error("不存在的提示词应报错")
	}
}

func TestMockStructure(t *testing.T) {
	q := &fakeQ{}
	e := NewEngine(Config{Queries: q, UseMock: true})
	out, meta, err := Structure.Run(context.Background(), e, 1, sampleBlocks())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "mock" || len(out.Items) != 3 {
		t.Fatalf("%+v %+v", meta, out)
	}
	term, choice, ans := out.Items[0], out.Items[1], out.Items[2]
	if term.QType != "term" || term.Stem != "意境" || term.Answer != "意境是情景交融的艺术境界。" || *term.Score != 5 || *term.ExamYear != 2024 {
		t.Errorf("名词解释：%+v", term)
	}
	if choice.QType != "single_choice" || choice.Stem != "下列属于唐传奇的是" || len(choice.Options) != 2 || choice.Options[1].Text != "搜神记" {
		t.Errorf("选择题：%+v", choice)
	}
	if ans.Kind != "answer" || ans.QuestionNo != "2" || ans.Answer != "A" {
		t.Errorf("答案条目：%+v", ans)
	}
	if len(q.calls) != 1 || !q.calls[0].Success || q.calls[0].Model != "mock" || !q.calls[0].UserHash.Valid {
		t.Errorf("账本：%+v", q.calls)
	}
}

func TestRetryThenFail(t *testing.T) {
	q := &fakeQ{}
	m := cloudai.NewMock()
	// 题干不在原文里（编造）→ 校验失败，重试一次仍失败 → AIFailed
	m.Set(Structure.Name, "```json\n{\"items\":[{\"block\":1,\"kind\":\"question\",\"qtype\":\"term\",\"stem\":\"完全编造的题目内容\",\"page\":1,\"confidence\":0.9}]}\n```")
	e := NewEngine(Config{Client: m, Queries: q})
	_, _, err := Structure.Run(context.Background(), e, 1, sampleBlocks())
	if e, ok := apperr.As(err); !ok || e.Kind != apperr.AIFailed || !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("应返回 AIFailed：%v", err)
	}
	if len(m.Calls()) != 2 || len(q.calls) != 2 || q.calls[0].Retried || !q.calls[1].Retried || q.calls[1].ErrorKind.String != "invalid_output" {
		t.Fatalf("应重试一次并各记一条：%d %+v", len(m.Calls()), q.calls)
	}
	if m.Calls()[0].Model != "qwen-plus" || !m.Calls()[0].JSONMode || !strings.Contains(m.Calls()[0].Messages[1].Content, "莺莺传") {
		t.Errorf("请求：%+v", m.Calls()[0])
	}

	// 合格的输出（带代码块包裹）通过
	m.Set(Structure.Name, "```json\n{\"items\":[{\"block\":1,\"kind\":\"question\",\"qtype\":\"term\",\"stem\":\"意境\",\"page\":1,\"confidence\":0.9}]}\n```")
	out, _, err := Structure.Run(context.Background(), e, 1, sampleBlocks())
	if err != nil || out.Items[0].Stem != "意境" {
		t.Fatalf("%v %+v", err, out)
	}

	// Schema 不合格（缺 confidence、题型不在枚举里）
	for _, bad := range []string{`{"items":[{"block":1,"kind":"question","page":1}]}`, `{"items":[{"block":1,"kind":"question","qtype":"poem","stem":"意境","page":1,"confidence":1}]}`, `not json`} {
		m.Set(Structure.Name, bad)
		if _, _, err := Structure.Run(context.Background(), e, 1, sampleBlocks()); !errors.Is(err, ErrInvalidOutput) {
			t.Errorf("%s：应判不合格，got %v", bad, err)
		}
	}

	// 平台错误不在这里重试，原样返回给任务重试
	e2 := NewEngine(Config{Client: failingClient{}, Queries: q})
	if _, _, err := ExtractRubric.Run(context.Background(), e2, 1, RubricIn{Answer: "a"}); !errors.Is(err, cloudai.ErrRateLimited) {
		t.Fatalf("平台错误：%v", err)
	}
}

func TestRollout(t *testing.T) {
	q := &fakeQ{rollout: &dbq.AiRollout{Capability: "kp_tag", StableModel: "qwen-max", StablePrompt: "v1",
		CandidateModel: sql.NullString{String: "deepseek-v3", Valid: true}, CandidatePrompt: sql.NullString{String: "v9", Valid: true}, CandidatePercent: 100}}
	e := NewEngine(Config{Queries: q, Models: Models{Cheap: "cheap-x"}})
	m, err := e.route(context.Background(), TagQuestions.Def, 1)
	if err != nil || m.Model != "deepseek-v3" || m.Version != "v1" {
		t.Fatalf("候选版 100%%，不存在的提示词版本回到默认：%+v %v", m, err)
	}
	q.rollout.CandidatePercent = 0
	if m, _ = e.route(context.Background(), TagQuestions.Def, 1); m.Model != "qwen-max" {
		t.Fatalf("稳定版：%+v", m)
	}
	q.rollout = nil
	if m, _ = e.route(context.Background(), TagQuestions.Def, 1); m.Model != "cheap-x" || m.Version != "v1" {
		t.Fatalf("没有配置时按档位：%+v", m)
	}
	// 按用户分桶大致均匀且稳定
	in := 0
	for u := uint64(1); u <= 2000; u++ {
		b := bucket("kp_tag", u)
		if b < 30 {
			in++
		}
		if again := bucket("kp_tag", u); again != b {
			t.Fatal("分桶应稳定")
		}
	}
	if in < 500 || in > 700 {
		t.Errorf("30%% 放量实际 %d / 2000", in)
	}
}

func TestRubricAndAnswer(t *testing.T) {
	e := NewEngine(Config{Queries: &fakeQ{}, UseMock: true})
	out, _, err := ExtractRubric.Run(context.Background(), e, 1, RubricIn{QType: "short_answer", Answer: "（1）情节曲折；（2）人物鲜明；（3）语言华美。", Score: 10})
	if err != nil {
		t.Fatal(err)
	}
	sum := 0.0
	for _, p := range out.Points {
		sum += p.Score
	}
	if len(out.Points) != 3 || sum != 10 || out.Points[0].Content != "情节曲折；" {
		t.Fatalf("%+v", out.Points)
	}
	g, _, err := GenerateAnswer.Run(context.Background(), e, 1, GenerateAnswerIn{QType: "term", Stem: "意境", Score: 5})
	if err != nil || !strings.HasPrefix(g.Answer, "（AI 生成）") {
		t.Fatalf("%v %+v", err, g)
	}

	// 关键词不在参考答案里 → 不合格
	m := cloudai.NewMock()
	m.Set(ExtractRubric.Name, `{"points":[{"content":"情节","score":5,"keywords":["编造"]}]}`)
	if _, _, err := ExtractRubric.Run(context.Background(), NewEngine(Config{Client: m, Queries: &fakeQ{}}), 1, RubricIn{Answer: "情节曲折"}); !errors.Is(err, ErrInvalidOutput) {
		t.Fatal(err)
	}
	m.Set(GenerateAnswer.Name, `{"answer":"a","points":[{"content":"a","score":1,"keywords":[]}]}`)
	if _, _, err := GenerateAnswer.Run(context.Background(), NewEngine(Config{Client: m, Queries: &fakeQ{}}), 1, GenerateAnswerIn{Score: 5}); !errors.Is(err, ErrInvalidOutput) {
		t.Fatal("采分点合计不等于分值应不合格")
	}
}

func TestKPsAndTags(t *testing.T) {
	e := NewEngine(Config{Queries: &fakeQ{}, UseMock: true})
	in := KPIn{Pages: []KPPage{{No: 1, Text: "第一章 先秦文学\n一、神话\n神话：远古人民表现对自然及社会现象的认识。\n普通的段落"}, {No: 2, Text: "（1）诗经：我国第一部诗歌总集。"}}}
	out, _, err := ExtractKPs.Run(context.Background(), e, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Points) != 2 || out.Points[0].Name != "神话" || strings.Join(out.Points[0].Path, "/") != "第一章 先秦文学/一、神话" || out.Points[1].Name != "诗经" || out.Points[1].Page != 2 {
		t.Fatalf("%+v", out.Points)
	}
	m := cloudai.NewMock()
	m.Set(ExtractKPs.Name, `{"points":[{"name":"神话","original_text":"神话是编的","page":1,"path":["a"],"rubric_points":[]}]}`)
	if _, _, err := ExtractKPs.Run(context.Background(), NewEngine(Config{Client: m, Queries: &fakeQ{}}), 1, in); !errors.Is(err, ErrInvalidOutput) {
		t.Fatal("原文表述找不到应不合格")
	}

	tags, _, err := TagQuestions.Run(context.Background(), e, 1, TagIn{Questions: []TagQ{{ID: 7, Stem: "意境"}}})
	if err != nil || tags.Tags[0].ID != 7 || len(tags.Tags[0].Path) != 3 {
		t.Fatalf("%v %+v", err, tags)
	}
	m.Set(TagQuestions.Name, `{"tags":[{"id":99,"path":["a","b","c"]}]}`)
	if _, _, err := TagQuestions.Run(context.Background(), NewEngine(Config{Client: m, Queries: &fakeQ{}}), 1, TagIn{Questions: []TagQ{{ID: 7}}}); !errors.Is(err, ErrInvalidOutput) {
		t.Fatal("引用不存在的题应不合格")
	}
}

func TestText(t *testing.T) {
	if !ContainsVerbatim("情景\n交融的 境界", "情景交融") || ContainsVerbatim("abc", "") || ContainsVerbatim("abc", "abd") {
		t.Error("ContainsVerbatim")
	}
	if Overlap("意境", "意境是") != 1 || Overlap("x", "abc") != 0 || Overlap("y", "y") != 1 || math.Abs(Overlap("abcd", "abxcd")-2.0/3) > 1e-9 {
		t.Error("Overlap")
	}
	if !Subjective("discussion") || Subjective("single_choice") {
		t.Error("Subjective")
	}
}
