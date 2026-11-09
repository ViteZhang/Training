package ai

import (
	"context"
	"encoding/json"
	"errors"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/dbq"
	"strings"
	"testing"
)

type evidenceClient struct {
	calls     int
	supported bool
	prompts   []string
}

// DeepSeek 的 json_object 模式要求提示词明确包含 JSON；仅有对象示例会返回400。
type jsonEvidenceClient struct{ t *testing.T }

func (c jsonEvidenceClient) Complete(_ context.Context, r cloudai.Request) (cloudai.Response, error) {
	all := ""
	for _, m := range r.Messages {
		all += m.Content
	}
	if !r.JSONMode || !strings.Contains(strings.ToLower(all), "json") {
		c.t.Fatal("evidence capability must explicitly request JSON for compatible providers")
	}
	content := map[string]string{
		"answer_evidence_query":  `{"query":"意境"}`,
		"answer_evidence_select": `{"coverage":"none","source_ids":[]}`,
		"answer_citation_check":  `{"all_supported":false}`,
	}[r.Capability]
	return cloudai.Response{Content: content}, nil
}

func TestEvidenceCapabilitiesRequestJSON(t *testing.T) {
	e := NewEngine(Config{Client: jsonEvidenceClient{t}})
	ctx := context.Background()
	if _, _, err := ExtractEvidenceQuery.Run(ctx, e, 1, EvidenceQueryIn{Stem: "意境"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SelectAnswerEvidence.Run(ctx, e, 1, SelectEvidenceIn{Stem: "意境"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CheckAnswerCitations.Run(ctx, e, 1, CitationCheckIn{Answer: "意境"}); err != nil {
		t.Fatal(err)
	}
}

func (c *evidenceClient) Complete(_ context.Context, r cloudai.Request) (cloudai.Response, error) {
	c.calls++
	prompt := r.Messages[0].Content
	c.prompts = append(c.prompts, r.Messages[1].Content)
	var out any
	if strings.Contains(prompt, "独立的引文支撑核验器") {
		out = CitationCheckOut{AllSupported: c.supported}
	} else if strings.Contains(r.Messages[1].Content, "textbook:1:1") {
		out = GenerateAnswerOut{Answer: "文学具有审美性质。", Points: []RubricPoint{{Content: "文学具有审美性质。", Score: 5, Keywords: []string{"审美"}}}, Citations: []AnswerCitation{{"textbook:1:1", "文学具有审美性质。", "文学具有审美性质。"}}}
	} else {
		out = GenerateAnswerOut{Answer: "通用知识生成的参考答案。", Points: []RubricPoint{{Content: "通用知识生成的参考答案。", Score: 5, Keywords: []string{}}}, UsesGeneralKnowledge: true, Citations: []AnswerCitation{}}
	}
	data, _ := json.Marshal(out)
	return cloudai.Response{Content: string(data)}, nil
}
func TestCitationValidationRejectsInventedAndUnsupportedFields(t *testing.T) {
	in := GenerateAnswerIn{Evidence: []EvidenceFragment{{ID: "allowed", Text: "文学具有审美性质。"}}}
	for _, c := range []AnswerCitation{{"invented", "文学", "文学"}, {"allowed", "假的引文", "文学"}, {"allowed", "文学", "答案里没有的观点"}} {
		out := &GenerateAnswerOut{Answer: "文学", Citations: []AnswerCitation{c}}
		if validateAnswerCitations(in, out); len(out.Citations) != 0 {
			t.Fatal(c)
		}
	}
}

// 站不住的引用只去掉那一条；引文跨了识别文本的换行也算逐字。
func TestCitationValidationDropsOnlyBadOnes(t *testing.T) {
	in := GenerateAnswerIn{Evidence: []EvidenceFragment{{ID: "t1", Text: "六书是汉字的\n造字条例。"}}}
	out := &GenerateAnswerOut{Answer: "六书是汉字的造字条例。", Citations: []AnswerCitation{
		{"t1", "六书是汉字的造字条例。", "六书是汉字的造字条例。"},
		{"t1", "编造的引文", "六书是汉字的造字条例。"},
		{"t2", "六书", "六书是汉字的造字条例。"},
	}}
	if err := validateAnswerCitations(in, out); err != nil || len(out.Citations) != 1 || out.Citations[0].Quote != "六书是汉字的造字条例。" {
		t.Fatal(err, out.Citations)
	}
}
func TestAnswerCitationSupportFailureRegeneratesEntireAnswer(t *testing.T) {
	for _, supported := range []bool{true, false} {
		t.Run(map[bool]string{true: "supported", false: "unsupported"}[supported], func(t *testing.T) {
			client := &evidenceClient{supported: supported}
			e := NewEngine(Config{Client: client})
			out, _, err := e.GenerateReferenceAnswer(context.Background(), 1, GenerateAnswerIn{Stem: "文学", Score: 5, Coverage: "sufficient", Evidence: []EvidenceFragment{{ID: "textbook:1:1", Kind: "official_textbook", Text: "文学具有审美性质。"}}})
			if err != nil {
				t.Fatal(err)
			}
			if supported {
				if len(out.Snapshot.Sources) != 1 || client.calls != 2 {
					t.Fatal(out, client.calls)
				}
			} else {
				if len(out.Snapshot.Sources) != 0 || client.calls != 3 || out.Answer != "通用知识生成的参考答案。" {
					t.Fatal(out, client.calls)
				}
				if strings.Contains(client.prompts[2], "文学具有审美性质") {
					t.Fatal("回退仍携带被拒原文")
				}
			}
		})
	}
}

type failingEvidence struct{}

func (failingEvidence) PrepareAnswer(context.Context, uint64, GenerateAnswerIn) (GenerateAnswerIn, error) {
	return GenerateAnswerIn{}, errors.New("retrieval offline")
}
func (failingEvidence) ValidateAnswerSources(context.Context, *dbq.Queries, []AnswerSource) error {
	return nil
}
func TestRetrievalFailureDoesNotBlockAnswer(t *testing.T) {
	e := NewEngine(Config{UseMock: true})
	e.SetAnswerEvidenceProvider(failingEvidence{})
	out, _, err := e.GenerateReferenceAnswer(context.Background(), 1, GenerateAnswerIn{Stem: "意境", Score: 5})
	if err != nil || out.Answer == "" || len(out.Snapshot.Sources) != 0 {
		t.Fatal(out, err)
	}
}

func TestPartialEvidenceDoesNotInventGeneralKnowledgeSupplement(t *testing.T) {
	client := &evidenceClient{supported: true}
	e := NewEngine(Config{Client: client})
	out, _, err := e.GenerateReferenceAnswer(context.Background(), 1, GenerateAnswerIn{Stem: "文学", Score: 5, Coverage: "partial", Evidence: []EvidenceFragment{{ID: "textbook:1:1", Kind: "official_textbook", Text: "文学具有审美性质。"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Snapshot.UsesGeneralKnowledge || len(out.Snapshot.Sources) != 1 {
		t.Fatal("partial coverage must not imply unused general knowledge", out.Snapshot)
	}
}

// T53 没有引用教材时说明原因。
type basisEvidence struct{ basis *AnswerBasis }

func (b basisEvidence) PrepareAnswer(_ context.Context, _ uint64, in GenerateAnswerIn) (GenerateAnswerIn, error) {
	in.Basis = b.basis
	return in, nil
}
func (basisEvidence) ValidateAnswerSources(context.Context, *dbq.Queries, []AnswerSource) error {
	return nil
}

func TestAnswerBasisExplainsMissingCitations(t *testing.T) {
	// 检索失败：关联了教材时记 retrieval_failed；没关联（用户端）不记。
	e := NewEngine(Config{UseMock: true})
	e.SetAnswerEvidenceProvider(failingEvidence{})
	out, _, err := e.GenerateReferenceAnswer(context.Background(), 0, GenerateAnswerIn{Stem: "意境", Score: 5, EditionIDs: []uint64{1}})
	if err != nil || out.Basis == nil || out.Basis.Reason != BasisRetrievalFailed {
		t.Fatalf("检索失败：%+v %v", out.Basis, err)
	}
	out, _, _ = e.GenerateReferenceAnswer(context.Background(), 1, GenerateAnswerIn{Stem: "意境", Score: 5})
	if out.Basis != nil {
		t.Fatalf("没关联教材的用户端不记原因：%+v", out.Basis)
	}
	// 检索那一步给的原因原样带出来。
	e.SetAnswerEvidenceProvider(basisEvidence{&AnswerBasis{Reason: BasisUnavailable, Details: []string{"《甲》：还没有可检索的索引"}}})
	out, _, _ = e.GenerateReferenceAnswer(context.Background(), 0, GenerateAnswerIn{Stem: "意境", Score: 5, EditionIDs: []uint64{1}})
	if out.Basis == nil || out.Basis.Reason != BasisUnavailable || len(out.Basis.Details) != 1 {
		t.Fatalf("不能检索：%+v", out.Basis)
	}
	// 引用核对没通过 → citation_rejected；通过了没有原因。
	for _, supported := range []bool{true, false} {
		e := NewEngine(Config{Client: &evidenceClient{supported: supported}})
		out, _, err := e.GenerateReferenceAnswer(context.Background(), 1, GenerateAnswerIn{Stem: "文学", Score: 5, Coverage: "sufficient", Evidence: []EvidenceFragment{{ID: "textbook:1:1", Kind: "official_textbook", Text: "文学具有审美性质。"}}})
		if err != nil {
			t.Fatal(err)
		}
		if supported && out.Basis != nil || !supported && (out.Basis == nil || out.Basis.Reason != BasisCitationRejected) {
			t.Fatalf("引用核对 %v：%+v", supported, out.Basis)
		}
	}
	// 给了段落、模型没引用 → not_cited。
	e = NewEngine(Config{UseMock: true})
	out, _, _ = e.GenerateReferenceAnswer(context.Background(), 1, GenerateAnswerIn{Stem: "文学", Score: 5, Coverage: "sufficient", Evidence: []EvidenceFragment{{ID: "textbook:1:1", Kind: "official_textbook", Text: "文学具有审美性质。"}}})
	if out.Basis == nil || out.Basis.Reason != BasisNotCited {
		t.Fatalf("没引用：%+v", out.Basis)
	}
	for _, r := range []string{BasisNoTextbook, BasisUnavailable, BasisNoHits, BasisNotRelevant, BasisNotCited, BasisCitationRejected, BasisRetrievalFailed} {
		if BasisMessage(r) == BasisMessage("") {
			t.Errorf("%s 没有说明", r)
		}
	}
}

// T55：只引用了考纲、经验总结时，仍说明没有引用教材；类型名交给提示词。
type syllabusClient struct{}

func (syllabusClient) Complete(_ context.Context, r cloudai.Request) (cloudai.Response, error) {
	var out any
	switch r.Capability {
	case "answer_citation_check":
		out = CitationCheckOut{AllSupported: true}
	default:
		out = GenerateAnswerOut{Answer: "意境是情景交融的艺术境界。", Points: []RubricPoint{{Content: "意境是情景交融的艺术境界。", Score: 5, Keywords: []string{"情景交融"}}},
			Citations: []AnswerCitation{{"textbook:9:1", "考查意境的内涵", "意境是情景交融的艺术境界。"}}}
	}
	data, _ := json.Marshal(out)
	return cloudai.Response{Content: string(data)}, nil
}

func TestSyllabusCitationIsNotTextbook(t *testing.T) {
	e := NewEngine(Config{Client: syllabusClient{}})
	ev := []EvidenceFragment{{ID: "textbook:9:1", Kind: "official_textbook", Material: "syllabus", Text: "考查意境的内涵与特征。"}}
	out, _, err := e.GenerateReferenceAnswer(context.Background(), 0, GenerateAnswerIn{Stem: "意境", Score: 5, Evidence: ev, EditionIDs: []uint64{9},
		Basis: &AnswerBasis{Reason: BasisNoTextbook}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Snapshot.Sources) != 1 || out.Snapshot.Sources[0].Material != "syllabus" {
		t.Fatalf("考纲引用保留：%+v", out.Snapshot.Sources)
	}
	if out.Basis == nil || out.Basis.Reason != BasisNoTextbook || !out.Snapshot.UsesGeneralKnowledge {
		t.Fatalf("只引用考纲仍说明没有教材：%+v %v", out.Basis, out.Snapshot.UsesGeneralKnowledge)
	}
	names := map[string]string{"": "教材", "handout": "讲义", "syllabus": "考纲", "experience": "经验总结"}
	for m, want := range names {
		if got := (EvidenceFragment{Kind: "official_textbook", Material: m}).MaterialName(); got != want {
			t.Errorf("%q → %s", m, got)
		}
	}
	if (EvidenceFragment{Kind: "official_textbook", Material: "experience"}).IsKnowledge() || !(EvidenceFragment{Kind: "official_textbook", Material: "handout"}).IsKnowledge() {
		t.Error("只有教材讲义算知识依据")
	}
}

// 核验指出个别引用不支持：丢掉没有一条引用站得住的来源，告诉模型哪些观点原文不支持，用其余原文重新生成，不整份退回通用知识。
type partialCheckClient struct {
	checks  int
	prompts []string
}

func (c *partialCheckClient) Complete(_ context.Context, r cloudai.Request) (cloudai.Response, error) {
	var out any
	user := r.Messages[1].Content
	switch r.Capability {
	case "answer_citation_check":
		c.checks++
		if c.checks == 1 {
			out = CitationCheckOut{AllSupported: false, Unsupported: []int{2}}
		} else {
			out = CitationCheckOut{AllSupported: true}
		}
	default:
		c.prompts = append(c.prompts, user)
		cites := []AnswerCitation{{"t:1", "指事者，视而可识", "指事是视而可识。"}}
		if strings.Contains(user, "ID=t:2") {
			cites = append(cites, AnswerCitation{"t:2", "隶变是分水岭", "六书影响深远。"})
		}
		out = GenerateAnswerOut{Answer: "指事是视而可识。六书影响深远。", Points: []RubricPoint{{Content: "指事是视而可识。", Score: 5, Keywords: []string{"指事"}}, {Content: "六书影响深远。", Score: 5, Keywords: []string{"六书"}}}, Citations: cites}
	}
	data, _ := json.Marshal(out)
	return cloudai.Response{Content: string(data)}, nil
}

func TestAnswerCitationPartialRejectionKeepsTrustedSources(t *testing.T) {
	client := &partialCheckClient{}
	e := NewEngine(Config{Client: client})
	out, _, err := e.GenerateReferenceAnswer(context.Background(), 1, GenerateAnswerIn{Stem: "六书", Score: 10, Coverage: "sufficient", Evidence: []EvidenceFragment{
		{ID: "t:1", Kind: "official_textbook", Text: "指事者，视而可识，察而见意。"},
		{ID: "t:2", Kind: "official_textbook", Text: "隶变是分水岭。"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Basis != nil || out.Snapshot == nil || len(out.Snapshot.Sources) != 1 || out.Snapshot.Sources[0].ID != "t:1" {
		t.Fatalf("保留核验通过的教材：%+v %+v", out.Basis, out.Snapshot)
	}
	if len(client.prompts) != 2 || strings.Contains(client.prompts[1], "ID=t:2") || !strings.Contains(client.prompts[1], "- 六书影响深远。") || client.checks != 2 {
		t.Fatalf("第二次只给可信原文：%d 次生成 %d 次核验", len(client.prompts), client.checks)
	}
}
