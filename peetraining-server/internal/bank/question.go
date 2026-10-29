package bank

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// QuestionFilter 是题目列表的筛选（3.1b）。
type QuestionFilter struct {
	QType    string
	Source   string
	ExamYear *int
	KPID     *uint64
	Status   string // mastered / wrong / needs_review / unanswered
	// AnswerStatus 按答案档位筛选（3.1b）：material / confirmed / ai / missing（PRD v3 11.16）
	AnswerStatus string
	Sort         string // chapter（按章节）/ recent（最近做过）
	After        int    // 分页游标：已返回的条数
	Limit        int
}

// QuestionSummary 是列表里的一题。
type QuestionSummary struct {
	ID              uint64
	QType           string
	Stem            string
	Score           *float64
	Source          string
	ExamYear        *int
	StatusTag       string
	LastScore       *float64
	AttemptCount    int
	Path            []string
	GeneratedFromKP string
	AnswerTier      string
	// 专业分组与共用材料归组键（列表里不带材料原文）
	GroupLabel, GroupMajors, PassageKey string
}

// Facets 是各题型、各来源的题数与真题年份（筛选项上显示）。
type Facets struct {
	ByQType  map[string]int
	BySource map[string]int
	// ByAnswer 是各答案档位的题数（AI 出题自带的答案算在 material 里）
	ByAnswer map[string]int
	Years    []int
}

// statusTag：待核对 > AI 答案待确认 > 错题 > 已掌握（主知识点已掌握）> 还没做过 > 已做过。
func statusTag(q dbq.ListBankQuestionsFullRow, kpState string) string {
	switch {
	case q.NeedsReview:
		return "needs_review"
	case listTier(q) == rules.AnswerAI:
		return "ai_answer"
	case q.InWrongBook:
		return "wrong"
	case q.AttemptCount > 0 && kpState == string(dbq.KpMasteryStateMastered):
		return "mastered"
	case q.AttemptCount == 0:
		return "unanswered"
	}
	return "answered"
}

// listTier 是列表里一题的答案档位。
func listTier(q dbq.ListBankQuestionsFullRow) rules.AnswerTier {
	answer, origin := "", ""
	if q.HasAnswer == 1 {
		answer = "x"
	}
	if q.AnswerOrigin.Valid {
		origin = string(q.AnswerOrigin.QuestionsAnswerOrigin)
	}
	return rules.TierOf(answer, origin, string(q.Source))
}

// answerFacet 把 AI 出题自带的答案归到「来自你的资料」一档里统计与筛选（它们按确认过的采分点出题，与原题同样计分）。
func answerFacet(t rules.AnswerTier) string {
	if t == rules.AnswerAIQuestion {
		return string(rules.AnswerMaterial)
	}
	return string(t)
}

func parseScore(s sql.NullString) *float64 {
	if !s.Valid {
		return nil
	}
	v, err := strconv.ParseFloat(s.String, 64)
	if err != nil {
		return nil
	}
	return &v
}

func lastTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case []byte:
		x, _ := time.Parse("2006-01-02 15:04:05.000", string(t))
		return x
	}
	return time.Time{}
}

// Questions 返回题目列表与筛选项统计。
func (s *Service) Questions(ctx context.Context, userID, subjectID uint64, f QuestionFilter) ([]QuestionSummary, Facets, int, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return nil, Facets{}, 0, err
	}
	rows, err := s.q.ListBankQuestionsFull(ctx, dbq.ListBankQuestionsFullParams{UserID: userID, BankID: b.BankID, Owner: owner(userID)})
	if err != nil {
		return nil, Facets{}, 0, err
	}
	kps, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: b.BankID, Owner: owner(userID)})
	if err != nil {
		return nil, Facets{}, 0, err
	}
	byID := map[uint64]dbq.ListBankKPsFullRow{}
	order := map[uint64]int{}
	for i, k := range kps {
		byID[k.ID] = k
		order[k.ID] = i
	}
	pathOf := func(id uint64) []string {
		var p []string
		for k, ok := byID[id]; ok && len(p) < 3; k, ok = byID[uint64(k.ParentID.Int64)] {
			if k.Level != dbq.KnowledgePointsLevelPoint {
				p = append([]string{k.Name}, p...)
			}
			if !k.ParentID.Valid {
				break
			}
		}
		return p
	}
	var under map[uint64]bool
	if f.KPID != nil {
		if _, ok := byID[*f.KPID]; !ok {
			return nil, Facets{}, 0, apperr.NotFoundErr()
		}
		under = map[uint64]bool{*f.KPID: true}
		for changed := true; changed; {
			changed = false
			for _, k := range kps {
				if k.ParentID.Valid && under[uint64(k.ParentID.Int64)] && !under[k.ID] {
					under[k.ID], changed = true, true
				}
			}
		}
	}
	facets := Facets{ByQType: map[string]int{}, BySource: map[string]int{}, ByAnswer: map[string]int{}}
	years := map[int]bool{}
	var out []QuestionSummary
	var times []time.Time
	var orders []int
	for _, r := range rows {
		facets.ByQType[string(r.Qtype)]++
		facets.BySource[string(r.Source)]++
		if r.ExamYear.Valid {
			years[int(r.ExamYear.Int16)] = true
		}
		kp := uint64(r.PrimaryKpID)
		tag := statusTag(r, string(byID[kp].State))
		tier := listTier(r)
		facets.ByAnswer[answerFacet(tier)]++
		switch {
		case f.AnswerStatus != "" && answerFacet(tier) != f.AnswerStatus:
			continue
		case f.QType != "" && string(r.Qtype) != f.QType,
			f.Source != "" && string(r.Source) != f.Source,
			f.ExamYear != nil && (!r.ExamYear.Valid || int(r.ExamYear.Int16) != *f.ExamYear),
			under != nil && !under[kp],
			f.Status != "" && tag != f.Status:
			continue
		}
		q := QuestionSummary{ID: r.ID, QType: string(r.Qtype), Stem: r.Stem, Score: parseScore(r.Score), Source: string(r.Source),
			ExamYear: year(r.ExamYear), StatusTag: tag, LastScore: parseScore(r.LastScore), AttemptCount: int(r.AttemptCount), Path: pathOf(kp),
			GroupLabel: r.GroupLabel.String, GroupMajors: r.GroupMajors.String, PassageKey: r.PassageKey.String, AnswerTier: string(tier)}
		if r.GeneratedFromKpID.Valid {
			q.GeneratedFromKP = byID[uint64(r.GeneratedFromKpID.Int64)].Name
		}
		out = append(out, q)
		times = append(times, lastTime(r.LastAnsweredAt))
		o, ok := order[kp]
		if !ok {
			o = math.MaxInt32
		}
		orders = append(orders, o)
	}
	for y := range years {
		facets.Years = append(facets.Years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(facets.Years)))
	idx := make([]int, len(out))
	for i := range idx {
		idx[i] = i
	}
	if f.Sort == "recent" {
		sort.SliceStable(idx, func(a, b int) bool { return times[idx[a]].After(times[idx[b]]) })
	} else {
		sort.SliceStable(idx, func(a, b int) bool { return orders[idx[a]] < orders[idx[b]] })
	}
	sorted := make([]QuestionSummary, len(out))
	for i, j := range idx {
		sorted[i] = out[j]
	}
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	start := min(max(f.After, 0), len(sorted))
	end := min(start+limit, len(sorted))
	next := 0
	if end < len(sorted) {
		next = end
	}
	return sorted[start:end], facets, next, nil
}

// Attempt 是一次作答（3.3「我的每次作答」）。
type Attempt struct {
	ID         uint64
	AnsweredAt time.Time
	Score      *float64
	FullScore  *float64
	IsCorrect  *bool
	Missed     []string
	LossTypes  []string
}

// QuestionDetail 是题目详情（3.3）。
type QuestionDetail struct {
	ID              uint64
	SubjectID       uint64
	QType           string
	Stem            string
	Options         []ai.Option
	Answer          string
	AnswerOrigin    string
	Analysis        string
	Score           *float64
	Source          string
	ExamYear        *int
	SourceRef       *Source
	OriginTags      []string
	GeneratedFromKP *uint64
	Rubric          []RubricPoint
	RubricVersion   int
	KPs             []QuestionKP
	Attempts        []Attempt
	InWrongBook     bool
	NeedsReview     bool
	ReviewReasons   []string
	// 答案档位与出处（PRD v3 11.16）；ReferenceGradings 是按这份 AI 参考答案批改过的次数与最近一次得分（3.3b）。
	AnswerTier        string
	AnswerMatch       string
	AnswerSource      *Source
	ReferenceGradings *ReferenceGradings
	// 专业分组与共用材料（迁移 00020）：组名、适用专业、材料原文与归组键
	GroupLabel, GroupMajors, Passage, PassageKey string
}

// ReferenceGradings 是 3.3b 显示的按 AI 参考答案批改的情况（不计入预估分）。
type ReferenceGradings struct {
	Count               int
	LastScore, LastFull *float64
}

// QuestionKP 是题目关联的知识点。
type QuestionKP struct {
	ID        uint64
	Name      string
	IsPrimary bool
}

func (s *Service) question(ctx context.Context, userID, id uint64) (dbq.GetQuestionFullRow, error) {
	q, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: id, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && q.Status != dbq.QuestionsStatusActive) {
		return q, apperr.NotFoundErr()
	}
	return q, err
}

// Question 返回题目详情：参考答案与出处、采分点与来源、知识点、我的每次作答。
func (s *Service) Question(ctx context.Context, userID, id uint64) (QuestionDetail, error) {
	q, err := s.question(ctx, userID, id)
	if err != nil {
		return QuestionDetail{}, err
	}
	d := QuestionDetail{ID: q.ID, SubjectID: uint64(q.SubjectID.Int64), QType: string(q.Qtype), Stem: q.Stem, Answer: q.Answer.String, Analysis: q.Analysis.String,
		Score: parseScore(q.Score), Source: string(q.Source), ExamYear: year(q.ExamYear), RubricVersion: int(q.RubricVersion),
		NeedsReview: q.NeedsReview, OriginTags: []string{},
		GroupLabel: q.GroupLabel.String, GroupMajors: q.GroupMajors.String, Passage: q.Passage.String, PassageKey: q.PassageKey.String}
	if q.AnswerOrigin.Valid {
		d.AnswerOrigin = string(q.AnswerOrigin.QuestionsAnswerOrigin)
	}
	tier := rules.TierOf(q.Answer.String, d.AnswerOrigin, string(q.Source))
	d.AnswerTier = string(tier)
	if q.AnswerMatch.Valid {
		d.AnswerMatch = string(q.AnswerMatch.QuestionsAnswerMatch)
	}
	if tier == rules.AnswerMaterial {
		mid, page := q.AnswerMaterialID, q.AnswerPage
		if !mid.Valid {
			mid, page = q.SourceMaterialID, q.SourcePage
		}
		if mid.Valid {
			if m, err := s.q.GetMaterial(ctx, dbq.GetMaterialParams{ID: uint64(mid.Int64), OwnerUserID: userID}); err == nil {
				d.AnswerSource = &Source{MaterialID: m.ID, FileName: m.FileName, Page: int(page.Int32)}
			}
		}
	}
	if tier == rules.AnswerAI {
		n, err := s.q.CountReferenceOnlyAttempts(ctx, dbq.CountReferenceOnlyAttemptsParams{QuestionID: id, OwnerUserID: userID})
		if err != nil {
			return d, err
		}
		rg := &ReferenceGradings{Count: int(n)}
		if n > 0 {
			if last, err := s.q.LatestReferenceOnlyAttempt(ctx, dbq.LatestReferenceOnlyAttemptParams{QuestionID: id, OwnerUserID: userID}); err == nil {
				rg.LastScore, rg.LastFull = parseScore(last.Score), parseScore(last.FullScore)
			}
		}
		d.ReferenceGradings = rg
	}
	_ = json.Unmarshal(q.Options, &d.Options)
	_ = json.Unmarshal(q.ReviewReasons, &d.ReviewReasons)
	switch q.Source {
	case dbq.QuestionsSourceAiGenerated:
		d.OriginTags = append(d.OriginTags, "ai_generated")
	case dbq.QuestionsSourceOfficial:
		d.OriginTags = append(d.OriginTags, "official")
	}
	if q.GeneratedFromKpID.Valid {
		v := uint64(q.GeneratedFromKpID.Int64)
		d.GeneratedFromKP = &v
	}
	if q.SourceMaterialID.Valid {
		if m, err := s.q.GetMaterial(ctx, dbq.GetMaterialParams{ID: uint64(q.SourceMaterialID.Int64), OwnerUserID: userID}); err == nil {
			d.SourceRef = &Source{MaterialID: m.ID, FileName: m.FileName, Page: int(q.SourcePage.Int32)}
		}
	}
	rps, err := s.q.ListQuestionRubric(ctx, dbq.ListQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: owner(userID)})
	if err != nil {
		return d, err
	}
	d.Rubric = toRubric(rps)
	kps, err := s.q.ListQuestionKPs(ctx, dbq.ListQuestionKPsParams{QuestionID: id, OwnerUserID: owner(userID)})
	if err != nil {
		return d, err
	}
	for _, k := range kps {
		d.KPs = append(d.KPs, QuestionKP{ID: k.ID, Name: k.Name, IsPrimary: k.IsPrimary})
	}
	as, err := s.q.ListQuestionAttempts(ctx, dbq.ListQuestionAttemptsParams{QuestionID: id, OwnerUserID: userID})
	if err != nil {
		return d, err
	}
	for _, a := range as {
		at := Attempt{ID: a.ID, AnsweredAt: a.AnsweredAt, Score: parseScore(a.Score), FullScore: parseScore(a.FullScore)}
		if a.IsCorrect.Valid {
			at.IsCorrect = &a.IsCorrect.Bool
		}
		at.Missed, at.LossTypes = gradingSummary(a.PointResults, a.Loss)
		d.Attempts = append(d.Attempts, at)
	}
	ok, err := s.q.InWrongBook(ctx, dbq.InWrongBookParams{OwnerUserID: userID, QuestionID: id})
	d.InWrongBook = ok
	return d, err
}

// gradingSummary 从批改结果里取遗漏的采分点与失分类型（批改结构在 T18 定，这里只读需要的字段）。
func gradingSummary(points, loss []byte) ([]string, []string) {
	var ps []struct {
		Content string `json:"content"`
		Result  string `json:"result"`
	}
	_ = json.Unmarshal(points, &ps)
	var missed []string
	for _, p := range ps {
		if p.Result == "miss" {
			missed = append(missed, p.Content)
		}
	}
	var l map[string]json.RawMessage
	_ = json.Unmarshal(loss, &l)
	var types []string
	for _, t := range []string{"knowledge", "norm", "time"} {
		if _, ok := l[t]; ok {
			types = append(types, t)
		}
	}
	return missed, types
}

// QuestionInput 是手动添加或编辑题目的内容；编辑时为空的字段不改。
type QuestionInput struct {
	QType       *string
	Stem        *string
	Options     *[]ai.Option
	Answer      *string
	Analysis    *string
	Score       *float64
	ScoreSet    bool
	Source      *string
	ExamYear    *int
	ExamYearSet bool
	KPIDs       *[]uint64
	Rubric      *[]RubricInput
	NeedsReview *bool
}

func checkRubricSum(qtype string, score *float64, rubric []RubricInput) error {
	if !ai.Subjective(qtype) || score == nil || len(rubric) == 0 {
		return nil
	}
	sum, any := 0.0, false
	for _, r := range rubric {
		if r.Score != nil {
			sum += *r.Score
			any = true
		}
	}
	if any && math.Abs(sum-*score) > 0.01 {
		return apperr.New(apperr.BadRequest, fmt.Sprintf("采分点分值合计要等于题目分值 %s 分", strconv.FormatFloat(*score, 'f', -1, 64))).With("reason", "rubric_sum_mismatch")
	}
	return nil
}

// validKPs 检查知识点都在这个题库里。
func (s *Service) validKPs(ctx context.Context, userID, bankID uint64, ids []uint64) error {
	for _, id := range ids {
		k, err := s.kp(ctx, userID, id)
		if err != nil || k.BankID != bankID {
			return apperr.New(apperr.BadRequest, "知识点不存在").With("reason", "kp_not_found")
		}
	}
	return nil
}

func setKPs(ctx context.Context, q *dbq.Queries, userID, questionID uint64, ids []uint64) error {
	if err := q.DeleteQuestionKPs(ctx, dbq.DeleteQuestionKPsParams{QuestionID: questionID, OwnerUserID: owner(userID)}); err != nil {
		return err
	}
	for i, id := range slices.Compact(ids) {
		if err := q.InsertQuestionKP(ctx, dbq.InsertQuestionKPParams{QuestionID: questionID, KpID: id, IsPrimary: i == 0, OwnerUserID: owner(userID)}); err != nil {
			return err
		}
	}
	return nil
}

func nullStr(p *string) sql.NullString {
	if p == nil || *p == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func scoreStr(p *float64) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: strconv.FormatFloat(*p, 'f', 2, 64), Valid: true}
}

// CreateQuestion 手动添加一道题，计入导入题数额度。
func (s *Service) CreateQuestion(ctx context.Context, userID, subjectID uint64, in QuestionInput) (QuestionDetail, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return QuestionDetail{}, err
	}
	if in.QType == nil || !slices.Contains(ai.QTypes, *in.QType) || in.Stem == nil || strings.TrimSpace(*in.Stem) == "" {
		return QuestionDetail{}, apperr.New(apperr.BadRequest, "题型与题干不能为空")
	}
	var rubric []RubricInput
	if in.Rubric != nil {
		rubric = *in.Rubric
	}
	if err := checkRubricSum(*in.QType, in.Score, rubric); err != nil {
		return QuestionDetail{}, err
	}
	var kps []uint64
	if in.KPIDs != nil {
		kps = *in.KPIDs
	}
	if err := s.validKPs(ctx, userID, b.BankID, kps); err != nil {
		return QuestionDetail{}, err
	}
	source := dbq.QuestionsSourceExercise
	if in.Source != nil && *in.Source == "exam" {
		source = dbq.QuestionsSourceExam
	}
	stem := strings.TrimSpace(*in.Stem)
	var id uint64
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		p := dbq.InsertQuestionParams{OwnerUserID: owner(userID), BankID: b.BankID, Qtype: dbq.QuestionsQtype(*in.QType), Stem: stem,
			Answer: nullStr(in.Answer), Analysis: nullStr(in.Analysis), Score: scoreStr(in.Score), Source: source, ContentHash: importer.ContentHash(stem),
			Difficulty: dbq.NullQuestionsDifficulty{QuestionsDifficulty: dbq.QuestionsDifficultyMedium, Valid: true}}
		if in.Answer != nil && *in.Answer != "" {
			p.AnswerOrigin = dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginUserConfirmed, Valid: true}
		}
		if in.Options != nil && len(*in.Options) > 0 {
			p.Options, _ = json.Marshal(*in.Options)
		}
		if in.ExamYear != nil {
			p.ExamYear = sql.NullInt16{Int16: int16(*in.ExamYear), Valid: true}
		}
		res, err := q.InsertQuestion(ctx, p)
		if err != nil {
			return err
		}
		id = uint64(res)
		if err := s.quota.Consume(ctx, q, quota.Charge{UserID: userID, Type: quota.ImportQuestions, Amount: 1,
			Ref: quota.Ref{Type: "question", ID: id}, Key: quota.KeyFor("question_create", id)}); err != nil {
			return err
		}
		if err := insertRubric(ctx, q, userID, sql.NullInt64{Int64: int64(id), Valid: true}, sql.NullInt64{}, rubric); err != nil {
			return err
		}
		if err := setKPs(ctx, q, userID, id, kps); err != nil {
			return err
		}
		return q.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	})
	if err != nil {
		return QuestionDetail{}, err
	}
	return s.Question(ctx, userID, id)
}

// UpdateQuestion 编辑题目（3.3）。改了采分点时 rubric_version 加 1：之后的批改按新采分点，
// 历史批改的分数与当时的采分点快照不变（PRD 11.14）。
func (s *Service) UpdateQuestion(ctx context.Context, userID, id uint64, in QuestionInput) (QuestionDetail, error) {
	q, err := s.question(ctx, userID, id)
	if err != nil {
		return QuestionDetail{}, err
	}
	qtype, stem := string(q.Qtype), q.Stem
	if in.QType != nil {
		if !slices.Contains(ai.QTypes, *in.QType) {
			return QuestionDetail{}, apperr.New(apperr.BadRequest, "题型不正确")
		}
		qtype = *in.QType
	}
	if in.Stem != nil {
		if stem = strings.TrimSpace(*in.Stem); stem == "" {
			return QuestionDetail{}, apperr.New(apperr.BadRequest, "题干不能为空")
		}
	}
	score := parseScore(q.Score)
	if in.ScoreSet {
		score = in.Score
	}
	var rubric []RubricInput
	if in.Rubric != nil {
		rubric = *in.Rubric
	} else {
		cur, err := s.q.ListQuestionRubric(ctx, dbq.ListQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: owner(userID)})
		if err != nil {
			return QuestionDetail{}, err
		}
		for _, r := range toRubric(cur) {
			rubric = append(rubric, RubricInput{Content: r.Content, Score: r.Score, Keywords: r.Keywords})
		}
	}
	if err := checkRubricSum(qtype, score, rubric); err != nil {
		return QuestionDetail{}, err
	}
	if in.KPIDs != nil {
		if err := s.validKPs(ctx, userID, q.BankID, *in.KPIDs); err != nil {
			return QuestionDetail{}, err
		}
	}
	p := dbq.UpdateQuestionParams{Qtype: dbq.QuestionsQtype(qtype), Stem: stem, Options: q.Options, Answer: q.Answer, AnswerOrigin: q.AnswerOrigin,
		Analysis: q.Analysis, Score: scoreStr(score), ExamYear: q.ExamYear, Source: q.Source, ContentHash: importer.ContentHash(stem),
		RubricVersion: q.RubricVersion, NeedsReview: q.NeedsReview, ReviewReasons: q.ReviewReasons, ID: id, OwnerUserID: owner(userID)}
	if in.Source != nil {
		if q.Source != dbq.QuestionsSourceExam && q.Source != dbq.QuestionsSourceExercise && q.Source != dbq.QuestionsSourceUnconfirmed {
			return QuestionDetail{}, apperr.New(apperr.BadRequest, "这类题目的来源不能修改")
		}
		switch *in.Source {
		case "exam", "exercise", "unconfirmed":
			p.Source = dbq.QuestionsSource(*in.Source)
		default:
			return QuestionDetail{}, apperr.New(apperr.BadRequest, "题目来源不正确")
		}
	}
	if in.Options != nil {
		p.Options = nil
		if len(*in.Options) > 0 {
			p.Options, _ = json.Marshal(*in.Options)
		}
	}
	editedAI := false
	if in.Answer != nil && *in.Answer != q.Answer.String {
		p.Answer = nullStr(in.Answer)
		p.AnswerOrigin = dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginUserConfirmed, Valid: p.Answer.Valid}
		editedAI = q.AnswerOrigin.Valid && q.AnswerOrigin.QuestionsAnswerOrigin == dbq.QuestionsAnswerOriginAiGenerated
	}
	// 「我来改」改了 AI 参考答案的采分点，也算确认过答案（PRD v3 11.16 第 6 条：用户修改采分点同理）。
	if in.Rubric != nil && q.AnswerOrigin.Valid && q.AnswerOrigin.QuestionsAnswerOrigin == dbq.QuestionsAnswerOriginAiGenerated && p.AnswerOrigin == q.AnswerOrigin {
		p.AnswerOrigin = dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginUserConfirmed, Valid: true}
		editedAI = true
	}
	if in.Analysis != nil {
		p.Analysis = nullStr(in.Analysis)
	}
	if in.ExamYearSet {
		p.ExamYear = sql.NullInt16{}
		if in.ExamYear != nil {
			p.ExamYear = sql.NullInt16{Int16: int16(*in.ExamYear), Valid: true}
		}
	}
	if in.Rubric != nil {
		p.RubricVersion++
	}
	if in.NeedsReview != nil && !*in.NeedsReview {
		p.NeedsReview, p.ReviewReasons = false, nil
	}
	err = store.WithTx(ctx, s.db, func(tx *dbq.Queries) error {
		if err := tx.UpdateQuestion(ctx, p); err != nil {
			return err
		}
		if editedAI {
			if err := tx.MarkAIAnswerEdited(ctx, dbq.MarkAIAnswerEditedParams{ID: id, OwnerUserID: owner(userID)}); err != nil {
				return err
			}
		}
		if in.Rubric != nil {
			if err := tx.DeleteQuestionRubric(ctx, dbq.DeleteQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: owner(userID)}); err != nil {
				return err
			}
			if err := insertRubric(ctx, tx, userID, sql.NullInt64{Int64: int64(id), Valid: true}, sql.NullInt64{}, rubric); err != nil {
				return err
			}
		}
		if in.KPIDs != nil {
			if err := setKPs(ctx, tx, userID, id, *in.KPIDs); err != nil {
				return err
			}
		}
		return tx.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: q.BankID, OwnerUserID: owner(userID)})
	})
	if err != nil {
		return QuestionDetail{}, err
	}
	return s.Question(ctx, userID, id)
}

// DeleteQuestion 删除题目，连同作答记录与错题（外键级联）。
func (s *Service) DeleteQuestion(ctx context.Context, userID, id uint64) error {
	_, err := s.DeleteQuestions(ctx, userID, []uint64{id})
	return err
}

// DeleteQuestions 批量删除题目（3.1b 多选删除）：先核对每一道都是自己的，有一道不是就整体返回 404、全部不删；
// 在一个事务里删除，再按涉及的资料与题库重算统计。返回删除的题数。
func (s *Service) DeleteQuestions(ctx context.Context, userID uint64, ids []uint64) (int, error) {
	type ref struct {
		id       uint64
		material sql.NullInt64
		bank     uint64
	}
	refs := make([]ref, 0, len(ids))
	seen := map[uint64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		q, err := s.question(ctx, userID, id)
		if err != nil {
			return 0, err
		}
		refs = append(refs, ref{id: id, material: q.SourceMaterialID, bank: q.BankID})
	}
	err := store.WithTx(ctx, s.db, func(tx *dbq.Queries) error {
		materials, banks := map[int64]sql.NullInt64{}, map[uint64]bool{}
		for _, r := range refs {
			if _, err := tx.DeleteQuestion(ctx, dbq.DeleteQuestionParams{ID: r.id, OwnerUserID: owner(userID)}); err != nil {
				return err
			}
			if r.material.Valid {
				materials[r.material.Int64] = r.material
			}
			banks[r.bank] = true
		}
		for id, mid := range materials {
			m := uint64(id)
			if err := tx.RecountMaterial(ctx, dbq.RecountMaterialParams{SourceMaterialID: mid, MaterialID: m, ID: m, OwnerUserID: userID}); err != nil {
				return err
			}
		}
		for b := range banks {
			if err := tx.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: b, OwnerUserID: owner(userID)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(refs), nil
}

// ConfirmAIAnswer 「答案没问题，确认」（3.3b、4.7b）：AI 参考答案与它的采分点变为「你已确认」，之后的批改计入预估分；
// 确认前的批改分数不追溯（PRD v3 11.16 第 6 条）。答案不是 AI 参考答案时返回 409。
func (s *Service) ConfirmAIAnswer(ctx context.Context, userID, id uint64) (QuestionDetail, error) {
	if _, err := s.question(ctx, userID, id); err != nil {
		return QuestionDetail{}, err
	}
	err := store.WithTx(ctx, s.db, func(tx *dbq.Queries) error {
		n, err := tx.ConfirmAIAnswer(ctx, dbq.ConfirmAIAnswerParams{ID: id, OwnerUserID: owner(userID)})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.New(apperr.Conflict, "这道题的答案不是待确认的 AI 参考答案")
		}
		return tx.ConfirmQuestionRubric(ctx, dbq.ConfirmQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: owner(userID)})
	})
	if err != nil {
		return QuestionDetail{}, err
	}
	return s.Question(ctx, userID, id)
}
