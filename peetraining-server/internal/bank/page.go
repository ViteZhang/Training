package bank

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

// Range 是一段文字的位置（按 Unicode 码点计，契约 TextRange）。
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Page 是原文查看的一页（3.7）。
type Page struct {
	MaterialID    uint64
	FileName      string
	PageNo        int
	PageCount     int
	Text          string
	Highlights    []Range
	LowConfidence []Range
	KPs           []QuestionKP
}

// MaterialPage 返回资料的一页原文：定位高亮 highlight（如知识点原文表述，忽略空白差异），低置信度位置，本页识别出的知识点。
func (s *Service) MaterialPage(ctx context.Context, userID, materialID uint64, pageNo int, highlight string) (Page, error) {
	if pageNo < 1 {
		return Page{}, apperr.NotFoundErr()
	}
	p, err := s.q.GetMaterialPage(ctx, dbq.GetMaterialPageParams{MaterialID: materialID, PageNo: uint32(pageNo), OwnerUserID: userID, OwnerUserID_2: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Page{}, err
	}
	out := Page{MaterialID: materialID, FileName: p.FileName, PageNo: pageNo, PageCount: int(p.PageCount), Text: p.Text,
		Highlights: Find(p.Text, highlight), LowConfidence: []Range{}}
	_ = json.Unmarshal(p.LowConfidence, &out.LowConfidence)
	kps, err := s.q.ListKPsOnPage(ctx, dbq.ListKPsOnPageParams{MaterialID: materialID, PageNo: uint32(pageNo), OwnerUserID: owner(userID)})
	if err != nil {
		return out, err
	}
	for _, k := range kps {
		out.KPs = append(out.KPs, QuestionKP{ID: k.ID, Name: k.Name})
	}
	return out, nil
}

// Find 返回 needle 在 text 里每次出现的位置（码点）。比较时忽略空白：资料取文本时的换行、空格不影响定位。
func Find(text, needle string) []Range {
	out := []Range{}
	var want []rune
	for _, r := range needle {
		if !unicode.IsSpace(r) {
			want = append(want, unicode.ToLower(r))
		}
	}
	if len(want) == 0 {
		return out
	}
	// 去掉空白后的字符及其原始位置。
	var flat []rune
	var pos []int
	for i, r := range []rune(text) {
		if !unicode.IsSpace(r) {
			flat = append(flat, unicode.ToLower(r))
			pos = append(pos, i)
		}
	}
	for i := 0; i+len(want) <= len(flat); i++ {
		match := true
		for j, r := range want {
			if flat[i+j] != r {
				match = false
				break
			}
		}
		if match {
			out = append(out, Range{Start: pos[i], End: pos[i+len(want)-1] + 1})
			i += len(want) - 1
		}
	}
	return out
}

// Hit 是搜索命中的片段与高亮位置（相对片段）。
type Hit struct {
	Text       string
	Highlights []Range
}

// snippet 截取命中位置前后各 30 字的片段。
func snippet(text, q string) Hit {
	rs := []rune(text)
	ranges := Find(text, q)
	if len(ranges) == 0 {
		end := min(len(rs), 60)
		return Hit{Text: string(rs[:end]), Highlights: []Range{}}
	}
	start := max(ranges[0].Start-30, 0)
	end := min(ranges[0].End+30, len(rs))
	h := Hit{Text: string(rs[start:end]), Highlights: []Range{}}
	for _, r := range ranges {
		if r.Start >= start && r.End <= end {
			h.Highlights = append(h.Highlights, Range{Start: r.Start - start, End: r.End - start})
		}
	}
	if start > 0 {
		h.Text = "…" + h.Text
		for i := range h.Highlights {
			h.Highlights[i].Start++
			h.Highlights[i].End++
		}
	}
	if end < len(rs) {
		h.Text += "…"
	}
	return h
}

// SearchResult 是搜索结果（3.2），分知识点、题目、资料原文三组。
type SearchResult struct {
	KPs []struct {
		ID   uint64
		Name string
		Path []string
		Hit  Hit
	}
	Questions []struct {
		ID    uint64
		QType string
		Hit   Hit
	}
	Pages []struct {
		MaterialID uint64
		FileName   string
		PageNo     int
		Hit        Hit
	}
}

func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// Search 在当前专业课内搜索知识点、题目与资料原文。
func (s *Service) Search(ctx context.Context, userID, subjectID uint64, query string) (SearchResult, error) {
	var res SearchResult
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 64 {
		return res, apperr.New(apperr.BadRequest, "请输入 1–64 个字")
	}
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return res, err
	}
	pat := likePattern(query)
	kps, err := s.q.SearchKPs(ctx, dbq.SearchKPsParams{BankID: b.BankID, Owner: owner(userID), NamePattern: pat, TextPattern: sql.NullString{String: pat, Valid: true}})
	if err != nil {
		return res, err
	}
	for _, k := range kps {
		text := k.Name
		if len(Find(k.Name, query)) == 0 && k.OriginalText.Valid {
			text = k.OriginalText.String
		}
		path, err := s.path(ctx, userID, k.ParentID)
		if err != nil {
			return res, err
		}
		res.KPs = append(res.KPs, struct {
			ID   uint64
			Name string
			Path []string
			Hit  Hit
		}{k.ID, k.Name, path, snippet(text, query)})
	}
	qs, err := s.q.SearchQuestions(ctx, dbq.SearchQuestionsParams{BankID: b.BankID, Owner: owner(userID), Pattern: pat})
	if err != nil {
		return res, err
	}
	for _, q := range qs {
		res.Questions = append(res.Questions, struct {
			ID    uint64
			QType string
			Hit   Hit
		}{q.ID, string(q.Qtype), snippet(q.Stem, query)})
	}
	ps, err := s.q.SearchMaterialPages(ctx, dbq.SearchMaterialPagesParams{BankID: b.BankID, UserID: userID, Pattern: pat})
	if err != nil {
		return res, err
	}
	for _, p := range ps {
		res.Pages = append(res.Pages, struct {
			MaterialID uint64
			FileName   string
			PageNo     int
			Hit        Hit
		}{p.MaterialID, p.FileName, int(p.PageNo), snippet(p.Text, query)})
	}
	return res, nil
}
