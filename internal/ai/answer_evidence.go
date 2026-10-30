package ai

import (
	"context"
	"encoding/json"
	"fmt"
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

// GenerateReferenceAnswer 是全部答案生成入口的共同通道。检索错误静默降级，最终模型失败正常返回。
func (e *Engine) GenerateReferenceAnswer(ctx context.Context, user uint64, in GenerateAnswerIn) (GenerateAnswerOut, Meta, error) {
	if e.answerEvidence != nil {
		if prepared, err := e.answerEvidence.PrepareAnswer(ctx, user, in); err == nil {
			in = prepared
		}
	}
	// 个人原文同样使用服务端分配的ID；不接受模型生成的来源元数据。
	for i, c := range in.Context {
		in.Evidence = append(in.Evidence, EvidenceFragment{ID: fmt.Sprintf("personal:%d", i+1), Kind: "personal_material", Title: c.Source, Text: c.Text, MaterialID: c.MaterialID, Page: c.Page})
	}
	out, meta, err := GenerateAnswer.Run(ctx, e, user, in)
	if err != nil {
		return out, meta, err
	}
	if len(out.Citations) == 0 && len(in.Evidence) > 0 {
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
		if checkErr != nil || !checked.AllSupported {
			// 引文无法证实支撑时整份答案重新生成，不能只删标签保留来源内容。
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
	out.Snapshot = &AnswerSnapshot{Version: "v1", GeneratedAt: e.now().UTC(), Model: meta.Model, PromptVersion: meta.Version, Coverage: in.Coverage, UsesGeneralKnowledge: out.UsesGeneralKnowledge || len(sources) == 0, Sources: sources}
	if out.Snapshot.Coverage == "" {
		out.Snapshot.Coverage = "none"
	}
	return out, meta, nil
}
func validateAnswerCitations(in GenerateAnswerIn, out *GenerateAnswerOut) error {
	if len(out.Citations) > 16 {
		return fmt.Errorf("引用过多")
	}
	for _, c := range out.Citations {
		if strings.TrimSpace(c.Quote) == "" || strings.TrimSpace(c.Claim) == "" || len([]rune(c.Quote)) > 1500 {
			return fmt.Errorf("引文或支持观点为空/超限")
		}
		var source *EvidenceFragment
		for i := range in.Evidence {
			if in.Evidence[i].ID == c.SourceID {
				source = &in.Evidence[i]
				break
			}
		}
		if source == nil || !strings.Contains(source.Text, c.Quote) {
			return fmt.Errorf("引用不属于本次原文集合或不是逐字子串")
		}
		found := strings.Contains(out.Answer, c.Claim)
		for _, p := range out.Points {
			found = found || strings.Contains(p.Content, c.Claim)
		}
		if !found {
			return fmt.Errorf("引用观点不在答案或采分点中")
		}
	}
	return nil
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
	Def: Def{Name: "answer_evidence_select", Version: "v1", Tier: "strong", Temperature: 0, Schema: `{"type":"object","required":["coverage","source_ids"],"properties":{"coverage":{"type":"string","enum":["none","partial","sufficient"]},"source_ids":{"type":"array","maxItems":8,"items":{"type":"string"}}}}`},
	Check: func(in SelectEvidenceIn, out *SelectEvidenceOut) error {
		if (out.Coverage == "none") != (len(out.SourceIDs) == 0) {
			return fmt.Errorf("依据覆盖与片段不一致")
		}
		seen := map[string]bool{}
		for _, id := range out.SourceIDs {
			found := false
			for _, f := range in.Evidence {
				found = found || f.ID == id
			}
			if !found || seen[id] {
				return fmt.Errorf("片段ID无效")
			}
			seen[id] = true
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
type CitationCheckOut struct {
	AllSupported bool `json:"all_supported"`
}

var CheckAnswerCitations = &Cap[CitationCheckIn, CitationCheckOut]{Def: Def{Name: "answer_citation_check", Version: "v1", Tier: "strong", Temperature: 0, Schema: `{"type":"object","required":["all_supported"],"properties":{"all_supported":{"type":"boolean"}}}`}, Mock: func(CitationCheckIn) (CitationCheckOut, error) { return CitationCheckOut{false}, nil }}

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
