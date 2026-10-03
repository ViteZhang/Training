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
	"peetraining-server/internal/store"
)

// QuestionFilter 是题目列表的筛选（3.1b）。
type QuestionFilter struct {
	QType    string
	Source   string
	ExamYear *int
	KPID     *uint64
	Status   string // mastered / wrong / needs_review / unanswered
	Sort     string // chapter（按章节）/ recent（最近做过）
	After    int    // 分页游标：已返回的条数
	Limit    int
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
}

// Facets 是各题型、各来源的题数与真题年份（筛选项上显示）。
type Facets struct {
	ByQType  map[string]int
	BySource map[string]int
	Years    []int
}

// statusTag：待核对 > 错题 > 已掌握（主知识点已掌握）> 还没做过 > 已做过。
func statusTag(q dbq.ListBankQuestionsFullRow, kpState string) string {
	switch {
	case q.NeedsReview:
		return "needs_review"
	case q.InWrongBook:
		return "wrong"
	case q.AttemptCount > 0 && kpState == string(dbq.KpMasteryStateMastered):
		return "mastered"
	case q.AttemptCount == 0:
		return "unanswered"
	}
	return "answered"
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
	facets := Facets{ByQType: map[string]int{}, BySource: map[string]int{}}
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
		switch {
		case f.QType != "" && string(r.Qtype) != f.QType,
			f.Source != "" && string(r.Source) != f.Source,
			f.ExamYear != nil && (!r.ExamYear.Valid || int(r.ExamYear.Int16) != *f.ExamYear),
			under != nil && !under[kp],
			f.Status != "" && tag != f.Status:
			continue
		}
		q := QuestionSummary{ID: r.ID, QType: string(r.Qtype), Stem: r.Stem, Score: parseScore(r.Score), Source: string(r.Source),
			ExamYear: year(r.ExamYear), StatusTag: tag, LastScore: parseScore(r.LastScore), AttemptCount: int(r.AttemptCount), Path: pathOf(kp)}
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
	d := QuestionDetail{ID: q.ID, QType: string(q.Qtype), Stem: q.Stem, Answer: q.Answer.String, Analysis: q.Analysis.String,
		Score: parseScore(q.Score), Source: string(q.Source), ExamYear: year(q.ExamYear), RubricVersion: int(q.RubricVersion),
		NeedsReview: q.NeedsReview, OriginTags: []string{}}
	if q.AnswerOrigin.Valid {
		d.AnswerOrigin = string(q.AnswerOrigin.QuestionsAnswerOrigin)
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
	if in.Options != nil {
		p.Options = nil
		if len(*in.Options) > 0 {
			p.Options, _ = json.Marshal(*in.Options)
		}
	}
	if in.Answer != nil && *in.Answer != q.Answer.String {
		p.Answer = nullStr(in.Answer)
		p.AnswerOrigin = dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginUserConfirmed, Valid: p.Answer.Valid}
	}
	if in.Analysis != nil {
		p.Analysis = nullStr(in.Analysis)
	}
	if in.ExamYearSet {
		p.ExamYear = sql.NullInt16{}
		if in.ExamYear != nil {
			p.ExamYear = sql.NullInt16{Int16: int16(*in.ExamYear), Valid: true}
		}
		if q.Source == dbq.QuestionsSourceExam || q.Source == dbq.QuestionsSourceExercise {
			p.Source = dbq.QuestionsSourceExercise
			if p.ExamYear.Valid {
				p.Source = dbq.QuestionsSourceExam
			}
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
	q, err := s.question(ctx, userID, id)
	if err != nil {
		return err
	}
	return store.WithTx(ctx, s.db, func(tx *dbq.Queries) error {
		if _, err := tx.DeleteQuestion(ctx, dbq.DeleteQuestionParams{ID: id, OwnerUserID: owner(userID)}); err != nil {
			return err
		}
		if q.SourceMaterialID.Valid {
			m := uint64(q.SourceMaterialID.Int64)
			if err := tx.RecountMaterial(ctx, dbq.RecountMaterialParams{SourceMaterialID: q.SourceMaterialID, MaterialID: m, ID: m, OwnerUserID: userID}); err != nil {
				return err
			}
		}
		return tx.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: q.BankID, OwnerUserID: owner(userID)})
	})
}
