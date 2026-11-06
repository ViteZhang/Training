package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"peetraining-server/internal/apperr"
)

// TextbookMetadata 是共享教材身份。官方教材库需求 5.2，不能用别名作为唯一键。
type TextbookMetadata struct {
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Kind       string   `json:"kind"`
	Discipline string   `json:"discipline"`
	Aliases    []string `json:"aliases"`
	// Series 是套书名（如上下卷共用「西方哲学史」）；omitempty 让单册教材的请求哈希与旧版一致。
	Series string `json:"series,omitempty"`
}
type TextbookEditionInput struct {
	Edition         string `json:"edition"`
	Publisher       string `json:"publisher"`
	PublicationYear *int   `json:"publication_year,omitempty"`
	ISBN            string `json:"isbn"`
	LicenseNote     string `json:"license_note"`
	Authorized      bool   `json:"authorized"`
	ScopeNote       string `json:"scope_note"`
}
type TextbookSection struct {
	SourceImportID *int64  `json:"source_import_id,omitempty"`
	NeedsReview    bool    `json:"needs_review,omitempty"`
	Heading        string  `json:"heading"`
	Text           string  `json:"text"`
	SourcePage     *int    `json:"source_page,omitempty"`
	PrintedPage    *string `json:"printed_page,omitempty"`
}

func boundedText(v string, min, max int) bool {
	n := utf8.RuneCountInString(v)
	return n >= min && n <= max
}

// 官方资料库的资料类型（Q20，T55）：教材、讲义是答案的知识依据；考纲只定考点范围；经验总结只参考答题结构与侧重点。
const (
	MaterialTextbook   = "textbook"
	MaterialHandout    = "handout"
	MaterialSyllabus   = "syllabus"
	MaterialExperience = "experience"
)

// ExperienceLicenseNote 是经验总结默认的授权说明：上传即视为已获授权，不另外校验（Q20）。
const ExperienceLicenseNote = "经验总结：上传即视为已获授权"

// IsMaterialKind 是不是资料库认得的类型。
func IsMaterialKind(kind string) bool {
	switch kind {
	case MaterialTextbook, MaterialHandout, MaterialSyllabus, MaterialExperience:
		return true
	}
	return false
}

// IsKnowledgeMaterial 是能当答案知识依据的类型（教材、讲义）；考生在 App 里填书名也只匹配这两类。
func IsKnowledgeMaterial(kind string) bool {
	return kind == MaterialTextbook || kind == MaterialHandout
}

// ExperienceEdition 给经验总结补上授权：上传即视为已获授权，没写说明时用默认说明（Q20）。其他类型原样返回。
func ExperienceEdition(kind string, e TextbookEditionInput) TextbookEditionInput {
	if kind != MaterialExperience {
		return e
	}
	e.Authorized = true
	if strings.TrimSpace(e.LicenseNote) == "" {
		e.LicenseNote = ExperienceLicenseNote
	}
	return e
}

func NormalizeTextbook(m TextbookMetadata) (TextbookMetadata, error) {
	m.Title = strings.TrimSpace(m.Title)
	m.Author = strings.TrimSpace(m.Author)
	m.Discipline = strings.TrimSpace(m.Discipline)
	m.Series = strings.TrimSpace(m.Series)
	if !boundedText(m.Series, 0, 128) {
		return m, apperr.New(apperr.BadRequest, "套书名最多 128 字")
	}
	if !boundedText(m.Title, 1, 128) || !boundedText(m.Author, 1, 128) || !boundedText(m.Discipline, 1, 128) || !IsMaterialKind(m.Kind) || len(m.Aliases) > 20 {
		return m, apperr.New(apperr.BadRequest, "请完整填写书名、作者、类型和学科")
	}
	aliases := make([]string, 0, len(m.Aliases))
	seen := map[string]bool{}
	for _, a := range m.Aliases {
		a = strings.TrimSpace(a)
		if !boundedText(a, 1, 128) {
			return m, apperr.New(apperr.BadRequest, "别名需为 1–128 字")
		}
		if !seen[a] {
			aliases = append(aliases, a)
			seen[a] = true
		}
	}
	m.Aliases = aliases
	return m, nil
}
func NormalizeTextbookEdition(e TextbookEditionInput) (TextbookEditionInput, error) {
	e.Edition = strings.TrimSpace(e.Edition)
	e.Publisher = strings.TrimSpace(e.Publisher)
	e.ISBN = strings.TrimSpace(e.ISBN)
	e.LicenseNote = strings.TrimSpace(e.LicenseNote)
	e.ScopeNote = strings.TrimSpace(e.ScopeNote)
	if !boundedText(e.Edition, 1, 64) || !boundedText(e.Publisher, 0, 128) || !boundedText(e.ISBN, 0, 32) || !boundedText(e.LicenseNote, 1, 500) || !boundedText(e.ScopeNote, 0, 255) || (e.PublicationYear != nil && (*e.PublicationYear < 1800 || *e.PublicationYear > 2200)) {
		return e, apperr.New(apperr.BadRequest, "版本信息或授权说明不完整")
	}
	// FR-ADM-04：导入前必须有明确授权；不把“保存草稿”当成越过授权的入口。
	if !e.Authorized {
		return e, apperr.New(apperr.BadRequest, "请确认素材可用于应用内检索和相关摘录展示")
	}
	return e, nil
}
func ValidateTextbookRequestKey(key string) error {
	if key != strings.TrimSpace(key) || !boundedText(key, 8, 128) {
		return apperr.New(apperr.BadRequest, "请求标识需为 8–128 字")
	}
	return nil
}

// NormalizeTextbookSections 不润色正文。首版每次保存上限 8 MiB，避免巨型 JSON 事务。
func NormalizeTextbookSections(sections []TextbookSection) ([]TextbookSection, error) {
	if len(sections) < 1 || len(sections) > 5000 {
		return nil, apperr.New(apperr.BadRequest, "原文需包含 1–5000 个单元")
	}
	out := make([]TextbookSection, len(sections))
	total := 0
	for i, s := range sections {
		if !boundedText(s.Heading, 0, 512) || !boundedText(s.Text, 1, 100000) || strings.TrimSpace(s.Text) == "" || (s.SourcePage != nil && *s.SourcePage < 1) || (s.PrintedPage != nil && !boundedText(*s.PrintedPage, 0, 32)) {
			return nil, apperr.New(apperr.BadRequest, "请检查章节、正文和页码")
		}
		out[i] = s
		total += len(s.Text) + len(s.Heading)
	}
	if total > 8*1024*1024 {
		return nil, apperr.New(apperr.BadRequest, "单次原文超过 8 MiB，请分章导入")
	}
	return out, nil
}
func TextbookHash(value any) (string, json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), data, nil
}

// TextbookEditionScopeUpdate 保持使用范围归属于原出版版本。
type TextbookEditionScopeUpdate struct {
	EditionID       uint64
	ExpectedVersion uint64
	ScopeNote       string
}

func NormalizeTextbookEditionScopes(scopes []TextbookEditionScopeUpdate) ([]TextbookEditionScopeUpdate, error) {
	if len(scopes) > 1000 {
		return nil, apperr.New(apperr.BadRequest, "单次最多修改 1000 个出版版本")
	}
	out := make([]TextbookEditionScopeUpdate, len(scopes))
	seen := map[uint64]bool{}
	for i, scope := range scopes {
		scope.ScopeNote = strings.TrimSpace(scope.ScopeNote)
		if scope.EditionID == 0 || scope.ExpectedVersion == 0 || seen[scope.EditionID] || !boundedText(scope.ScopeNote, 0, 255) {
			return nil, apperr.New(apperr.BadRequest, "请检查出版版本及使用范围，范围最多 255 字且版本不可重复")
		}
		seen[scope.EditionID] = true
		out[i] = scope
	}
	return out, nil
}
