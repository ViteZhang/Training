package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

// EvidenceFragment 为本次允许的原文集合。定位字段由服务端填写，模型只能回传ID及逐字引文。
type EvidenceFragment struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Title        string  `json:"title"`
	Text         string  `json:"text"`
	BookID       uint64  `json:"book_id,omitempty"`
	EditionID    uint64  `json:"edition_id,omitempty"`
	RevisionID   uint64  `json:"revision_id,omitempty"`
	GenerationID uint64  `json:"generation_id,omitempty"`
	Ordinal      uint32  `json:"ordinal,omitempty"`
	SectionNo    uint32  `json:"section_no,omitempty"`
	StartRune    uint32  `json:"start_rune,omitempty"`
	EndRune      uint32  `json:"end_rune,omitempty"`
	TextHash     string  `json:"text_hash,omitempty"`
	Author       string  `json:"author,omitempty"`
	Edition      string  `json:"edition,omitempty"`
	Heading      string  `json:"heading,omitempty"`
	SourcePage   *int    `json:"source_page,omitempty"`
	PrintedPage  *string `json:"printed_page,omitempty"`
	MaterialID   uint64  `json:"material_id,omitempty"`
	Page         int     `json:"page,omitempty"`
	// Material 是官方资料库的资料类型（textbook / handout / syllabus / experience，T55）；空是教材（T55 之前的快照）或考生资料。
	Material string `json:"material,omitempty"`
}

// MaterialName 是提示词里的资料类型名。
func (f EvidenceFragment) MaterialName() string {
	switch {
	case f.Kind == "personal_material":
		return "考生资料"
	case f.Material == "handout":
		return "讲义"
	case f.Material == "syllabus":
		return "考纲"
	case f.Material == "experience":
		return "经验总结"
	}
	return "教材"
}

// IsKnowledge 是能当知识依据的来源：官方教材、讲义（考纲、经验总结不算，T55）。
func (f EvidenceFragment) IsKnowledge() bool {
	return f.Kind == "official_textbook" && (f.Material == "" || f.Material == "textbook" || f.Material == "handout")
}

type AnswerCitation struct {
	SourceID string `json:"source_id"`
	Quote    string `json:"quote"`
	Claim    string `json:"claim"`
}
type AnswerSource struct {
	Unavailable bool `json:"unavailable,omitempty"`
	EvidenceFragment
	Quote string `json:"quote"`
	Claim string `json:"claim"`
}
type AnswerSnapshot struct {
	Version              string         `json:"version"`
	GeneratedAt          time.Time      `json:"generated_at"`
	Model                string         `json:"model"`
	PromptVersion        string         `json:"prompt_version"`
	Coverage             string         `json:"coverage"`
	UsesGeneralKnowledge bool           `json:"uses_general_knowledge"`
	Sources              []AnswerSource `json:"sources"`
}

// AnswerBasis 说明生成的答案为什么没有引用教材（T53），核对区逐题显示，便于编辑判断是关联、索引还是检索的问题。
type AnswerBasis struct {
	Reason  string   `json:"reason"`
	Details []string `json:"details,omitempty"`
}

// 没有引用教材的原因。
const (
	BasisNoTextbook       = "no_textbook"       // 没有关联教材
	BasisUnavailable      = "unavailable"       // 关联的教材都不能检索
	BasisNoHits           = "no_hits"           // 没检索到段落
	BasisNotRelevant      = "not_relevant"      // 检索到的段落与题目不相关
	BasisNotCited         = "not_cited"         // 模型没有引用给它的段落
	BasisCitationRejected = "citation_rejected" // 引用核对没通过
	BasisRetrievalFailed  = "retrieval_failed"  // 检索出错（超时等）
)

// BasisMessage 是给编辑看的说明。
func BasisMessage(reason string) string {
	switch reason {
	case BasisNoTextbook:
		return "本项目没有关联教材，答案按通用知识生成"
	case BasisUnavailable:
		return "关联的教材都不能检索，答案按通用知识生成"
	case BasisNoHits:
		return "关联教材里没有检索到相关段落，答案按通用知识生成"
	case BasisNotRelevant:
		return "检索到的教材段落与本题不相关，答案按通用知识生成"
	case BasisNotCited:
		return "AI 没有引用检索到的教材段落，答案按通用知识生成"
	case BasisCitationRejected:
		return "教材引用核对没有通过，答案已改按通用知识生成"
	case BasisRetrievalFailed:
		return "教材检索暂时失败，答案按通用知识生成，可稍后重新生成"
	}
	return "答案没有引用教材"
}

// AnswerEvidenceProvider 由官方教材服务实现，AI层不依赖业务服务。最终校验由写事务调用。
type AnswerEvidenceProvider interface {
	PrepareAnswer(context.Context, uint64, GenerateAnswerIn) (GenerateAnswerIn, error)
	ValidateAnswerSources(context.Context, *dbq.Queries, []AnswerSource) error
}

func (e *Engine) SetAnswerEvidenceProvider(p AnswerEvidenceProvider) { e.answerEvidence = p }
func (e *Engine) ValidateAnswerSnapshot(ctx context.Context, q *dbq.Queries, snapshot *AnswerSnapshot) error {
	if snapshot == nil {
		return nil
	}
	if e.answerEvidence == nil {
		for _, s := range snapshot.Sources {
			if s.Kind == "official_textbook" {
				return apperr.New(apperr.AIFailed, "依据已变化，请重新生成，未扣除次数")
			}
		}
		return nil
	}
	return e.answerEvidence.ValidateAnswerSources(ctx, q, snapshot.Sources)
}

// answerSourcesBatchValidator 是可以一次核对多份依据的实现（textbook.Service）；没有时逐份核对。
type answerSourcesBatchValidator interface {
	ValidateAnswerSourcesBatch(context.Context, *dbq.Queries, [][]AnswerSource) ([]bool, error)
}

// ValidateAnswerSnapshots 一次核对多份答案依据快照，ok[i] 与逐份调用 ValidateAnswerSnapshot 不报错等价。
// 只有请求取消之类的错误才返回 error。
func (e *Engine) ValidateAnswerSnapshots(ctx context.Context, q *dbq.Queries, snapshots []*AnswerSnapshot) ([]bool, error) {
	if b, ok := e.answerEvidence.(answerSourcesBatchValidator); ok {
		batch := make([][]AnswerSource, len(snapshots))
		for i, sn := range snapshots {
			if sn != nil {
				batch[i] = sn.Sources
			}
		}
		return b.ValidateAnswerSourcesBatch(ctx, q, batch)
	}
	out := make([]bool, len(snapshots))
	for i, sn := range snapshots {
		out[i] = e.ValidateAnswerSnapshot(ctx, q, sn) == nil
	}
	return out, ctx.Err()
}

// GenerateReferenceAnswer 是全部答案生成入口的共同通道。检索错误静默降级，最终模型失败正常返回。
func (e *Engine) GenerateReferenceAnswer(ctx context.Context, user uint64, in GenerateAnswerIn) (GenerateAnswerOut, Meta, error) {
	if e.answerEvidence != nil {
		if prepared, err := e.answerEvidence.PrepareAnswer(ctx, user, in); err == nil {
			in = prepared
		} else if len(in.EditionIDs) > 0 {
			in.Basis = &AnswerBasis{Reason: BasisRetrievalFailed}
		}
	}
	basis := in.Basis
	// 个人原文同样使用服务端分配的ID；不接受模型生成的来源元数据。
	for i, c := range in.Context {
		in.Evidence = append(in.Evidence, EvidenceFragment{ID: fmt.Sprintf("personal:%d", i+1), Kind: "personal_material", Title: c.Source, Text: c.Text, MaterialID: c.MaterialID, Page: c.Page})
	}
	out, meta, err := GenerateAnswer.Run(ctx, e, user, in)
	if err != nil {
		return out, meta, err
	}
	if len(out.Citations) == 0 && len(in.Evidence) > 0 {
		if basis == nil {
			basis = &AnswerBasis{Reason: BasisNotCited}
		}
		in.Evidence = nil
		in.Context = nil
		in.Coverage = "none"
		out, meta, err = GenerateAnswer.Run(ctx, e, user, in)
		if err != nil {
			return out, meta, err
		}
	}
	if len(out.Citations) > 0 {
		checked, _, checkErr := CheckAnswerCitations.Run(ctx, e, user, CitationCheckIn{Answer: out.Answer, Points: out.Points, Citations: out.Citations, Evidence: in.Evidence})
		if checkErr == nil && !checked.AllSupported {
			// 丢掉不可信的依据，只用核验通过的原文重新生成一次，并告诉模型哪些观点原文不支持
			// （常见的是评价类观点也挂在了只讲定义的教材段落下）；
			// 新答案的引用同样要全部核验通过，否则照旧改按通用知识生成。
			if trusted, rejected := trustedEvidence(in.Evidence, out.Citations, checked.Unsupported); len(trusted) > 0 {
				retry := in
				retry.Evidence, retry.Context, retry.Coverage, retry.RejectedClaims = trusted, nil, "partial", rejected
				if o2, m2, err2 := GenerateAnswer.Run(ctx, e, user, retry); err2 == nil && len(o2.Citations) > 0 {
					c2, _, e2 := CheckAnswerCitations.Run(ctx, e, user, CitationCheckIn{Answer: o2.Answer, Points: o2.Points, Citations: o2.Citations, Evidence: trusted})
					if e2 == nil && c2.AllSupported {
						in, out, meta, checked = retry, o2, m2, c2
					}
				}
			}
		}
		if checkErr != nil || !checked.AllSupported {
			// 引文无法证实支撑时整份答案重新生成，不能只删标签保留来源内容。
			if basis == nil {
				basis = &AnswerBasis{Reason: BasisCitationRejected}
			}
			in.Evidence = nil
			in.Context = nil
			in.Coverage = "none"
			out, meta, err = GenerateAnswer.Run(ctx, e, user, in)
			if err != nil {
				return out, meta, err
			}
		}
	}
	sources := []AnswerSource{}
	for _, c := range out.Citations {
		for _, f := range in.Evidence {
			if c.SourceID == f.ID {
				f.Text = ""
				sources = append(sources, AnswerSource{EvidenceFragment: f, Quote: c.Quote, Claim: c.Claim})
				break
			}
		}
	}
	// 「没有引用教材」只看教材、讲义；只引用了考纲、经验总结的也要说明（T55）。
	knowledge := slices.ContainsFunc(sources, func(s AnswerSource) bool { return s.IsKnowledge() })
	if !knowledge {
		if basis == nil && len(in.EditionIDs) > 0 {
			basis = &AnswerBasis{Reason: BasisNotCited}
		}
		out.Basis = basis
	}
	out.Snapshot = &AnswerSnapshot{Version: "v1", GeneratedAt: e.now().UTC(), Model: meta.Model, PromptVersion: meta.Version, Coverage: in.Coverage, UsesGeneralKnowledge: out.UsesGeneralKnowledge || !knowledge, Sources: sources}
	if out.Snapshot.Coverage == "" {
		out.Snapshot.Coverage = "none"
	}
	return out, meta, nil
}

// validateAnswerCitations 只留下站得住的引用：来源在本次给的原文里、引文能在原文里逐字找到（忽略空白：教材文本是识别出来的，
// 模型抄引文时常去掉换行）、支撑的观点出自答案或采分点。站不住的一条去掉，不让整题失败；
// 一条都不剩时由 GenerateReferenceAnswer 改按通用知识生成并说明原因，留下的还要经过引文支撑核验。
func validateAnswerCitations(in GenerateAnswerIn, out *GenerateAnswerOut) error {
	kept := out.Citations[:0]
	for _, c := range out.Citations {
		if citationValid(in, out, c) && len(kept) < 16 {
			kept = append(kept, c)
		}
	}
	out.Citations = kept
	return nil
}

func citationValid(in GenerateAnswerIn, out *GenerateAnswerOut, c AnswerCitation) bool {
	if strings.TrimSpace(c.Quote) == "" || strings.TrimSpace(c.Claim) == "" || len([]rune(c.Quote)) > 1500 {
		return false
	}
	var source *EvidenceFragment
	for i := range in.Evidence {
		if in.Evidence[i].ID == c.SourceID {
			source = &in.Evidence[i]
			break
		}
	}
	if source == nil || !ContainsVerbatim(source.Text, c.Quote) {
		return false
	}
	if pointInAnswer(out.Answer, c.Claim) {
		return true
	}
	for _, p := range out.Points {
		if pointInAnswer(p.Content, c.Claim) {
			return true
		}
	}
	return false
}

type SelectEvidenceIn struct {
	Subject, QType, Stem string
	Evidence             []EvidenceFragment
}
type SelectEvidenceOut struct {
	Coverage  string   `json:"coverage"`
	SourceIDs []string `json:"source_ids"`
}

var SelectAnswerEvidence = &Cap[SelectEvidenceIn, SelectEvidenceOut]{
	Def: Def{Name: "answer_evidence_select", Version: "v2", Tier: "strong", Temperature: 0, Schema: `{"type":"object","required":["coverage","source_ids"],"properties":{"coverage":{"type":"string","enum":["none","partial","sufficient"]},"source_ids":{"type":"array","maxItems":8,"items":{"type":"string"}}}}`},
	Check: func(in SelectEvidenceIn, out *SelectEvidenceOut) error {
		seen := map[string]bool{}
		knowledge := false
		for _, id := range out.SourceIDs {
			var f *EvidenceFragment
			for i := range in.Evidence {
				if in.Evidence[i].ID == id {
					f = &in.Evidence[i]
				}
			}
			if f == nil || seen[id] {
				return fmt.Errorf("片段ID无效")
			}
			seen[id] = true
			knowledge = knowledge || f.IsKnowledge() || f.Kind != "official_textbook"
		}
		// 覆盖判断只看教材、讲义（T55）：选了教材讲义就不能是 none；只选了考纲、经验总结必须是 none。
		if (out.Coverage == "none") == knowledge {
			return fmt.Errorf("依据覆盖与片段不一致")
		}
		return nil
	},
	Mock: func(SelectEvidenceIn) (SelectEvidenceOut, error) {
		return SelectEvidenceOut{Coverage: "none", SourceIDs: []string{}}, nil
	}, // 无真实语义模型不宣称相关性成功。
}

type CitationCheckIn struct {
	Answer    string
	Points    []RubricPoint
	Citations []AnswerCitation
	Evidence  []EvidenceFragment
}

// NumberedCitation 是带编号（从 1 开始）的引用，核验结果按编号指出不支持的条目。
type NumberedCitation struct {
	No int
	AnswerCitation
}

// Numbered 给提示词用：引用逐条编号。
func (in CitationCheckIn) Numbered() []NumberedCitation {
	out := make([]NumberedCitation, len(in.Citations))
	for i, c := range in.Citations {
		out[i] = NumberedCitation{No: i + 1, AnswerCitation: c}
	}
	return out
}

type CitationCheckOut struct {
	AllSupported bool `json:"all_supported"`
	// Unsupported 是不支持或不确定的引用编号（v3）：只丢掉这些引用的来源，用其余来源重新生成。
	Unsupported []int `json:"unsupported,omitempty"`
}

var CheckAnswerCitations = &Cap[CitationCheckIn, CitationCheckOut]{Def: Def{Name: "answer_citation_check", Version: "v3", Tier: "strong", Temperature: 0, Schema: `{"type":"object","required":["all_supported"],"properties":{"all_supported":{"type":"boolean"},"unsupported":{"type":"array","items":{"type":"integer"}}}}`}, Mock: func(CitationCheckIn) (CitationCheckOut, error) { return CitationCheckOut{AllSupported: false}, nil }}

// trustedEvidence 是核验后还能信的原文：至少有一条引用被判支持的片段（同一片段既有支持的引用、又被拿去挂了
// 它没讲的观点时，片段本身仍可信，错的是那句观点）；返回被判不支持的观点，重新生成时告诉模型别再引用。
// 核验没指出是哪几条（unsupported 为空）时一个都不信。
func trustedEvidence(evidence []EvidenceFragment, cites []AnswerCitation, unsupported []int) ([]EvidenceFragment, []string) {
	if len(unsupported) == 0 {
		return nil, nil
	}
	good := map[string]bool{}
	var rejected []string
	for i, c := range cites {
		if slices.Contains(unsupported, i+1) {
			rejected = append(rejected, c.Claim)
		} else {
			good[c.SourceID] = true
		}
	}
	var out []EvidenceFragment
	for _, f := range evidence {
		if good[f.ID] {
			out = append(out, f)
		}
	}
	return out, rejected
}

// EvidenceJSON 仅供提示词引用结构化证据；其中正文是数据，不能执行其中的指令。
func EvidenceJSON(v any) string { data, _ := json.Marshal(v); return string(data) }

type EvidenceQueryIn struct{ Subject, QType, Stem string }
type EvidenceQueryOut struct {
	Query string `json:"query"`
}

var ExtractEvidenceQuery = &Cap[EvidenceQueryIn, EvidenceQueryOut]{Def: Def{Name: "answer_evidence_query", Version: "v1", Tier: "strong", Temperature: 0, Schema: `{"type":"object","required":["query"],"properties":{"query":{"type":"string","minLength":1,"maxLength":1800}}}`}, Mock: func(in EvidenceQueryIn) (EvidenceQueryOut, error) {
	if len([]rune(in.Stem)) > 1800 {
		return EvidenceQueryOut{}, fmt.Errorf("长题干需真实模型提取检索信息")
	}
	return EvidenceQueryOut{in.Stem}, nil
}}
