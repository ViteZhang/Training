package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"sort"
	"strconv"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
)

// fill 在题库不够时让 AI 按资料里的知识点出变式题（PRD 模块 4），一律标 AI 出题并记录依据的知识点与资料页。
// 每题扣 1 次 AI 出题额度，额度用完或出题失败就停，少出的题算缺题，不报错。
func (s *Service) fill(ctx context.Context, userID uint64, b dbq.GetSubjectBankRow, c Config, n int, kps map[uint64]dbq.ListBankKPsFullRow, r *rand.Rand) ([]uint64, error) {
	var cands []dbq.ListBankKPsFullRow
	for _, k := range kps {
		if k.Level != dbq.KnowledgePointsLevelPoint {
			continue
		}
		if c.OnlyUnmastered && k.State == dbq.KpMasteryStateMastered {
			continue
		}
		if len(c.SectionIDs) > 0 && !under(k, c.SectionIDs, kps) {
			continue
		}
		cands = append(cands, k)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].ID < cands[j].ID })
	r.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	sort.SliceStable(cands, func(i, j int) bool {
		mi, _ := strconv.ParseFloat(cands[i].M, 64)
		mj, _ := strconv.ParseFloat(cands[j].M, 64)
		return mi < mj
	})
	qtypes := c.QTypes
	if len(qtypes) == 0 {
		qtypes = []string{"single_choice", "term"}
	}
	var out []uint64
	for i := 0; i < len(cands) && len(out) < n; i++ {
		k := cands[i]
		qt := qtypes[len(out)%len(qtypes)]
		if qt == "essay" || qt == "calculation" || qt == "other" || qt == "fill_blank" {
			qt = "term"
		}
		id, err := s.generate(ctx, userID, b, k.ID, qt)
		if errors.Is(err, errNoSource) {
			continue
		}
		if err != nil {
			var ae *apperr.Error
			if errors.As(err, &ae) && (ae.Kind == apperr.QuotaExceeded || ae.Kind == apperr.AIFailed) {
				logx.From(ctx).Info("ai fill stopped", "kind", int(ae.Kind))
				break
			}
			return out, err
		}
		out = append(out, id)
	}
	return out, nil
}

// errNoSource：知识点没有资料原文与出处，不能据此出题（AI 题必须写明依据哪份资料哪一页）。
var errNoSource = errors.New("知识点没有资料出处")

func under(k dbq.ListBankKPsFullRow, sections []uint64, kps map[uint64]dbq.ListBankKPsFullRow) bool {
	want := map[uint64]bool{}
	for _, id := range sections {
		want[id] = true
	}
	for cur, depth := k, 0; depth < 8; depth++ {
		if want[cur.ID] {
			return true
		}
		if !cur.ParentID.Valid {
			return false
		}
		p, ok := kps[uint64(cur.ParentID.Int64)]
		if !ok {
			return false
		}
		cur = p
	}
	return false
}

func (s *Service) generate(ctx context.Context, userID uint64, b dbq.GetSubjectBankRow, kpID uint64, qtype string) (uint64, error) {
	if left, err := s.quota.Remaining(ctx, userID, quota.AIQuestions); err != nil {
		return 0, err
	} else if left != nil && *left <= 0 {
		return 0, apperr.New(apperr.QuotaExceeded, "AI 出题次数用完了")
	}
	kp, err := s.q.GetKP(ctx, dbq.GetKPParams{ID: kpID, OwnerUserID: owner(userID)})
	if err != nil {
		return 0, err
	}
	if !kp.OriginalText.Valid || kp.OriginalText.String == "" || !kp.SourceMaterialID.Valid {
		return 0, errNoSource
	}
	rp, err := s.q.ListKPRubric(ctx, dbq.ListKPRubricParams{KpID: sql.NullInt64{Int64: int64(kpID), Valid: true}, OwnerUserID: owner(userID)})
	if err != nil {
		return 0, err
	}
	in := ai.VariantIn{Subject: b.Name, KPName: kp.Name, OriginalText: kp.OriginalText.String, QType: qtype}
	for _, p := range rp {
		in.Points = append(in.Points, p.Content)
	}
	out, _, err := ai.Variant.Run(ctx, s.ai, userID, in)
	if err != nil {
		return 0, err
	}
	var id uint64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		p := dbq.InsertQuestionParams{OwnerUserID: owner(userID), BankID: b.BankID, Qtype: dbq.QuestionsQtype(qtype), Stem: out.Stem,
			Answer: sql.NullString{String: out.Answer, Valid: true}, Analysis: sql.NullString{String: out.Analysis, Valid: out.Analysis != ""},
			Source: dbq.QuestionsSourceAiGenerated, ContentHash: importer.ContentHash(out.Stem), SourceMaterialID: kp.SourceMaterialID, SourcePage: kp.SourcePage,
			Difficulty: dbq.NullQuestionsDifficulty{QuestionsDifficulty: dbq.QuestionsDifficultyMedium, Valid: true}}
		if len(out.Options) > 0 {
			p.Options, _ = json.Marshal(out.Options)
		}
		res, err := q.InsertQuestion(ctx, p)
		if err != nil {
			return err
		}
		id = uint64(res)
		if err := q.SetQuestionGeneratedFrom(ctx, dbq.SetQuestionGeneratedFromParams{GeneratedFromKpID: sql.NullInt64{Int64: int64(kpID), Valid: true}, ID: id, OwnerUserID: owner(userID)}); err != nil {
			return err
		}
		if err := q.InsertQuestionKP(ctx, dbq.InsertQuestionKPParams{QuestionID: id, KpID: kpID, IsPrimary: true, OwnerUserID: owner(userID)}); err != nil {
			return err
		}
		return s.quota.Consume(ctx, q, quota.Charge{UserID: userID, Type: quota.AIQuestions, Amount: 1, Ref: quota.Ref{Type: "question", ID: id}, Key: quota.KeyFor("ai_question", id)})
	})
	return id, err
}

// Home 是训练首页（4.1）。
type Home struct {
	SubjectID    uint64
	Stage        string
	TodayTotal   int
	TodayDone    int
	TodayMinutes float64
	TodaySession uint64
	DrillQType   string
	DrillsWeek   int
	QTypeCounts  []QTypeCount
	Total        int
	WrongTotal   int
	WrongDue     int
	ReciteDue    int
	PaperFirst   bool
	InProgress   *InProgress
}

type QTypeCount struct {
	QType string
	Count int
}

type InProgress struct {
	SessionID   uint64
	Title       string
	Kind        Kind
	Done, Total int
}

// qtypeOrder 是题型显示顺序：主观题在前（本产品以主观题为主），客观题在后。
var qtypeOrder = map[string]int{"term": 0, "short_answer": 1, "discussion": 2, "essay": 3, "single_choice": 4, "multi_choice": 5, "true_false": 6, "fill_blank": 7, "calculation": 8, "other": 9}

func (s *Service) Home(ctx context.Context, userID, subjectID uint64) (Home, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Home{}, err
	}
	pl, err := s.plan.Today(ctx, userID)
	if err != nil {
		return Home{}, err
	}
	h := Home{SubjectID: b.SubjectID, Stage: pl.Stage}
	for i, it := range pl.Items {
		if it.SubjectID != b.SubjectID {
			continue
		}
		h.TodayTotal++
		h.TodayMinutes += it.Minutes
		if pl.Done[i] {
			h.TodayDone++
		}
	}
	stage := rules.Stage(pl.Stage)
	h.PaperFirst = stage == rules.Sprint || stage == rules.Final
	counts, err := s.q.QTypeCounts(ctx, dbq.QTypeCountsParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return Home{}, err
	}
	for _, c := range counts {
		h.QTypeCounts = append(h.QTypeCounts, QTypeCount{QType: string(c.Qtype), Count: int(c.N)})
		h.Total += int(c.N)
	}
	sort.Slice(h.QTypeCounts, func(i, j int) bool { return qtypeOrder[h.QTypeCounts[i].QType] < qtypeOrder[h.QTypeCounts[j].QType] })
	// 本周题型专项：真题里分值最高的主观题型；没有真题统计时取题最多的主观题型。
	home, err := s.plan.Home(ctx, userID)
	if err == nil && home.Push != nil && home.Push.QType != "" {
		h.DrillQType = home.Push.QType
	}
	if h.DrillQType == "" {
		best := 0
		for _, c := range h.QTypeCounts {
			if !rules.IsObjective(rules.QType(c.QType)) && c.QType != "essay" && c.Count > best {
				h.DrillQType, best = c.QType, c.Count
			}
		}
	}
	today := s.today()
	weekStart := today.AddDays(-int((today.Time().In(cst).Weekday() + 6) % 7))
	n, err := s.q.CountSessionsSince(ctx, dbq.CountSessionsSinceParams{OwnerUserID: userID, SubjectID: sql.NullInt64{Int64: int64(b.SubjectID), Valid: true},
		Kind: dbq.PracticeSessionsKindTypeDrill, StartedAt: weekStart.Time().UTC()})
	if err != nil {
		return Home{}, err
	}
	h.DrillsWeek = int(n)
	ws, err := s.q.ListWrongBook(ctx, dbq.ListWrongBookParams{OwnerUserID: userID, BankID: b.BankID})
	if err != nil {
		return Home{}, err
	}
	for _, w := range ws {
		if w.Status != dbq.WrongBookStatusActive {
			continue
		}
		h.WrongTotal++
		if !w.NextReviewOn.Valid || rules.DayFromDateColumn(w.NextReviewOn.Time) <= today {
			h.WrongDue++
		}
	}
	rd, err := s.q.CountReciteDue(ctx, dbq.CountReciteDueParams{BankID: b.BankID, OwnerUserID: owner(userID), ReciteNextReviewOn: sql.NullTime{Time: today.Date(), Valid: true}})
	if err != nil {
		return Home{}, err
	}
	h.ReciteDue = int(rd)
	cur, err := s.q.LatestInProgressSession(ctx, dbq.LatestInProgressSessionParams{OwnerUserID: userID, SubjectID: sql.NullInt64{Int64: int64(b.SubjectID), Valid: true}})
	if err == nil {
		var ids []uint64
		_ = json.Unmarshal(cur.QuestionIds, &ids)
		if Kind(cur.Kind) == KindDaily && rules.DayOf(cur.StartedAt) == today {
			h.TodaySession = cur.ID
		}
		if Kind(cur.Kind) != KindDaily || rules.DayOf(cur.StartedAt) == today {
			h.InProgress = &InProgress{SessionID: cur.ID, Title: cur.Title, Kind: Kind(cur.Kind), Done: int(cur.CursorIndex), Total: len(ids)}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Home{}, err
	}
	return h, nil
}

// WrongItem 是错题本里的一道题。
type WrongItem struct {
	dbq.ListWrongBookRow
}

// WrongBook 是错题本（4.12）。
type WrongBook struct {
	Total, ToRedo, WeekNew, Eliminated int
	Items                              []dbq.ListWrongBookRow
}

func (s *Service) WrongBook(ctx context.Context, userID, subjectID uint64) (WrongBook, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return WrongBook{}, err
	}
	ws, err := s.q.ListWrongBook(ctx, dbq.ListWrongBookParams{OwnerUserID: userID, BankID: b.BankID})
	if err != nil {
		return WrongBook{}, err
	}
	today := s.today()
	weekAgo := today.AddDays(-6).Time()
	out := WrongBook{Items: []dbq.ListWrongBookRow{}}
	for _, w := range ws {
		switch w.Status {
		case dbq.WrongBookStatusEliminated:
			out.Eliminated++
		case dbq.WrongBookStatusActive:
			out.Total++
			out.Items = append(out.Items, w)
			if !w.NextReviewOn.Valid || rules.DayFromDateColumn(w.NextReviewOn.Time) <= today {
				out.ToRedo++
			}
			if !w.AddedAt.Before(weekAgo) {
				out.WeekNew++
			}
		}
	}
	return out, nil
}

// RemoveWrong 手动移出错题本。
func (s *Service) RemoveWrong(ctx context.Context, userID, questionID uint64) error {
	n, err := s.q.RemoveWrongBook(ctx, dbq.RemoveWrongBookParams{RemovedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, OwnerUserID: userID, QuestionID: questionID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFoundErr()
	}
	return nil
}

// Report 是「题目有问题」报错（4.3）：AI 出的题累计 3 次自动下线。返回题目是否已下线。
func (s *Service) Report(ctx context.Context, userID, questionID uint64, reason string) (bool, error) {
	if _, err := s.q.GetQuestionStatus(ctx, dbq.GetQuestionStatusParams{ID: questionID, OwnerUserID: owner(userID)}); errors.Is(err, sql.ErrNoRows) {
		return false, apperr.NotFoundErr()
	} else if err != nil {
		return false, err
	}
	err := s.withTx(ctx, func(q *dbq.Queries) error {
		if err := q.UpsertQuestionReport(ctx, dbq.UpsertQuestionReportParams{QuestionID: questionID, OwnerUserID: userID, Reason: sql.NullString{String: reason, Valid: reason != ""}}); err != nil {
			return err
		}
		return q.BumpQuestionReport(ctx, dbq.BumpQuestionReportParams{ReportCount: reportOffline, ID: questionID, OwnerUserID: owner(userID)})
	})
	if err != nil {
		return false, err
	}
	st, err := s.q.GetQuestionStatus(ctx, dbq.GetQuestionStatusParams{ID: questionID, OwnerUserID: owner(userID)})
	return st.Status == dbq.QuestionsStatusOffline, err
}
