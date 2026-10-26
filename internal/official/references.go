package official

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/store"
)

// ---------- 7.11 官方参考素材（PRD v3 7.11，10 月 26 日更新） ----------
// 内容团队录入有授权的教材、讲义原文，切分成段；7.12 录题时 AI 生成官方题库的参考答案与采分点，
// 优先引用相关段落（answer_generate 的 Context）并返回出处，编辑核对后再写进草稿。

// Reference 是一份官方参考素材。
type Reference struct {
	ID          uint64
	Title       string
	Kind        string // textbook 教材 / handout 讲义
	LicenseNote string
	Segments    int
	Chars       int
	CreatedAt   time.Time
}

// Segment 是切分后的一段。
type Segment struct {
	Heading string
	Text    string
}

const (
	maxReferenceRunes = 2000000 // 一次录入的字数上限，与接口契约一致
	segmentTarget     = 600     // 一段攒到这么多字就切
	segmentMax        = 1200    // 单个自然段超过这么长时硬切
	maxSegments       = 5000    // 一份素材最多切这么多段（约 300 万字）
	citeOverlap       = 0.3     // 题目二字片段在段落里出现的比例，达到才算相关
	maxCitations      = 3       // 交给模型的段落数
	citeContextRune   = 800     // 每段交给模型的字数上限
)

// reHeading 认章节标题：「第一章 文学活动」「第三节」「一、」「1.2 」「# 标题」。
var reHeading = regexp.MustCompile(`^(#{1,6}\s*\S.*|第[一二三四五六七八九十百零〇\d]+[编章节讲部分].{0,40}|[一二三四五六七八九十]+、.{1,40}|\d+(\.\d+)+\s+.{1,40})$`)

// SplitReference 把素材原文按章节标题与段落切分：同一章节下的自然段攒到约 600 字一段，章节标题作为出处。
func SplitReference(text string) []Segment {
	var (
		out     []Segment
		heading string
		buf     []string
		size    int
	)
	flush := func() {
		if size > 0 {
			out = append(out, Segment{Heading: heading, Text: strings.Join(buf, "\n")})
		}
		buf, size = nil, 0
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if n := len([]rune(line)); n <= 48 && reHeading.MatchString(line) {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(line, "# "))
			continue
		}
		for r := []rune(line); len(r) > 0; {
			n := min(len(r), segmentMax)
			buf = append(buf, string(r[:n]))
			size += n
			r = r[n:]
			if size >= segmentTarget {
				flush()
			}
		}
	}
	flush()
	if len(out) > maxSegments {
		out = out[:maxSegments]
	}
	return out
}

func (s *Service) projectExists(ctx context.Context, projectID uint64) (dbq.OfficialProject, error) {
	p, err := s.q.GetOfficialProject(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, apperr.NotFoundErr()
	}
	return p, err
}

func toReference(r dbq.OfficialReference) Reference {
	return Reference{ID: r.ID, Title: r.Title, Kind: string(r.Kind), LicenseNote: r.LicenseNote, Segments: int(r.Segments), Chars: int(r.Chars), CreatedAt: r.CreatedAt}
}

// References 列出项目的官方参考素材。
func (s *Service) References(ctx context.Context, projectID uint64) ([]Reference, error) {
	if _, err := s.projectExists(ctx, projectID); err != nil {
		return nil, err
	}
	rs, err := s.q.ListOfficialReferences(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Reference, len(rs))
	for i, r := range rs {
		out[i] = toReference(r)
	}
	return out, nil
}

// CreateReference 录入一份素材：只收有授权的教材、讲义（PRD v3 7.11），切分后整份写入。
func (s *Service) CreateReference(ctx context.Context, adminID, projectID uint64, title, kind string, authorized bool, licenseNote, text string) (Reference, error) {
	if _, err := s.projectExists(ctx, projectID); err != nil {
		return Reference{}, err
	}
	title, licenseNote = strings.TrimSpace(title), strings.TrimSpace(licenseNote)
	if !authorized || licenseNote == "" {
		return Reference{}, apperr.New(apperr.BadRequest, "只能录入已获授权的教材或讲义，请写明授权说明")
	}
	if title == "" {
		return Reference{}, apperr.New(apperr.BadRequest, "请填写素材名称")
	}
	if kind != string(dbq.OfficialReferencesKindTextbook) && kind != string(dbq.OfficialReferencesKindHandout) {
		return Reference{}, apperr.New(apperr.BadRequest, "素材类型只能是教材或讲义")
	}
	if len([]rune(text)) > maxReferenceRunes {
		return Reference{}, apperr.New(apperr.BadRequest, "素材太长，请按册或按章分开录入")
	}
	segs := SplitReference(text)
	if len(segs) == 0 {
		return Reference{}, apperr.New(apperr.BadRequest, "素材原文是空的")
	}
	chars := 0
	for _, g := range segs {
		chars += len([]rune(g.Text))
	}
	var id uint64
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		rid, err := q.CreateOfficialReference(ctx, dbq.CreateOfficialReferenceParams{
			ProjectID: projectID, Title: title, Kind: dbq.OfficialReferencesKind(kind), LicenseNote: licenseNote,
			Segments: uint32(len(segs)), Chars: uint32(chars), CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0},
		})
		if err != nil {
			return err
		}
		id = uint64(rid)
		for i, g := range segs {
			if err := q.InsertOfficialReferenceSegment(ctx, dbq.InsertOfficialReferenceSegmentParams{
				ReferenceID: id, Seq: uint32(i + 1), Heading: cutRunes(g.Heading, 128), Text: g.Text,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Reference{}, err
	}
	return Reference{ID: id, Title: title, Kind: kind, LicenseNote: licenseNote, Segments: len(segs), Chars: chars, CreatedAt: s.now().UTC()}, nil
}

// DeleteReference 删除素材（段落级联删除）。已经写进草稿的答案不受影响。
func (s *Service) DeleteReference(ctx context.Context, projectID, referenceID uint64) error {
	n, err := s.q.DeleteOfficialReference(ctx, dbq.DeleteOfficialReferenceParams{ID: referenceID, ProjectID: projectID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFoundErr()
	}
	return nil
}

// Citation 是生成答案时引用的段落。
type Citation struct {
	ReferenceID uint64
	Title       string
	Heading     string
}

// AnswerCandidate 是 AI 给官方题生成的参考答案、采分点与出处。
type AnswerCandidate struct {
	Answer    string
	Points    []Point
	Citations []Citation
}

// relatedSegments 按题目在段落里的字面重合度挑出最相关的几段。
func relatedSegments(stem string, segs []dbq.ListOfficialReferenceSegmentsRow, n int) []dbq.ListOfficialReferenceSegmentsRow {
	core := importer.Normalize(stem)
	for _, p := range []string{"名词解释", "简答题", "简述", "论述题", "试论", "论述"} {
		core = strings.TrimPrefix(core, p)
	}
	if core == "" {
		return nil
	}
	type scored struct {
		seg dbq.ListOfficialReferenceSegmentsRow
		v   float64
	}
	var all []scored
	for _, g := range segs {
		if v := ai.Overlap(core, g.Heading+g.Text); v >= citeOverlap {
			all = append(all, scored{g, v})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].v > all[j].v })
	out := make([]dbq.ListOfficialReferenceSegmentsRow, 0, n)
	for _, x := range all {
		if len(out) == n {
			break
		}
		out = append(out, x.seg)
	}
	return out
}

// AnswerCandidates 给官方题生成参考答案与采分点：相关的官方参考素材段落 → 教材书名 → 通用知识（PRD v3 7.11、11.16 第 5 条）。
// 结果只返回给编辑，编辑核对后写进草稿走审核，不直接入库。
func (s *Service) AnswerCandidates(ctx context.Context, projectID uint64, qtype, stem string, score float64) (AnswerCandidate, error) {
	p, err := s.projectExists(ctx, projectID)
	if err != nil {
		return AnswerCandidate{}, err
	}
	refs, err := s.q.ListOfficialReferences(ctx, projectID)
	if err != nil {
		return AnswerCandidate{}, err
	}
	segs, err := s.q.ListOfficialReferenceSegments(ctx, projectID)
	if err != nil {
		return AnswerCandidate{}, err
	}
	in := ai.GenerateAnswerIn{Subject: p.SubjectName, QType: qtype, Stem: stem, Score: score}
	for _, r := range refs {
		if r.Kind == dbq.OfficialReferencesKindTextbook {
			in.Textbooks = append(in.Textbooks, r.Title)
		}
	}
	var cites []Citation
	for _, g := range relatedSegments(stem, segs, maxCitations) {
		src := g.Title
		if g.Heading != "" {
			src += " · " + g.Heading
		}
		in.Context = append(in.Context, ai.AnswerContext{Source: src, Text: cutRunes(g.Text, citeContextRune)})
		cites = append(cites, Citation{ReferenceID: g.ReferenceID, Title: g.Title, Heading: g.Heading})
	}
	out, _, err := ai.GenerateAnswer.Run(ctx, s.d.AI, 0, in)
	if err != nil {
		return AnswerCandidate{}, apperr.New(apperr.AIFailed, "AI 生成参考答案失败，请稍后再试").Wrap(err)
	}
	pts := make([]Point, len(out.Points))
	for i, x := range out.Points {
		pts[i] = Point{Content: x.Content, Score: x.Score, Keywords: x.Keywords}
	}
	if cites == nil {
		cites = []Citation{}
	}
	return AnswerCandidate{Answer: out.Answer, Points: pts, Citations: cites}, nil
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
