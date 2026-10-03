package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
)

// 整卷与模拟考试：4.18 整卷列表、4.19 选择作答模式、4.20 练习模式、4.21 模拟考试、4.22 答题卡、4.23 交卷（T21）。
// 时间按 PRD 11.9，AI 组卷按 11.10。

// Enqueuer 是 Asynq 客户端（测试里换成同步执行）。
type Enqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

const (
	paperModePractice = "practice"
	paperModeMock     = "mock"
)

// PaperSection 是试卷的一种题型。
type PaperSection struct {
	QType            string  `json:"qtype"`
	Count            int     `json:"count"`
	ScoreEach        float64 `json:"score_each"`
	Total            float64 `json:"total"`
	SuggestedMinutes int     `json:"-"`
}

// PaperBrief 是整卷列表里的一套卷。
type PaperBrief struct {
	ID              uint64
	Kind            string
	Title           string
	ExamYear        *int
	QuestionCount   int
	FullScore       float64
	ActualScore     float64
	MissingNote     string
	DurationMinutes int
	AIFilled        int
	Status          string // not_started / in_progress / done
	Active          *PaperRun
	Last            *PaperRun
}

// PaperRun 是一次作答的概况。
type PaperRun struct {
	SessionID  uint64
	Mode       string
	Status     string
	Answered   int
	Total      int
	FinishedAt time.Time
	Score      *float64
	FullScore  float64
}

// PaperDetail 是 4.19 选择作答模式的数据。
type PaperDetail struct {
	PaperBrief
	Sections          []PaperSection
	CheckMinutes      int
	RecommendedMode   string
	CountsForEstimate bool
}

// PaperList 是 4.18 整卷列表。
type PaperList struct {
	Real, AI        []PaperBrief
	WeeklyRemaining *int
	InProgress      *struct {
		SessionID, PaperID, SubjectID uint64
		Title                         string
	}
}

func dec(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func structureOf(raw json.RawMessage) []PaperSection {
	var out []PaperSection
	_ = json.Unmarshal(raw, &out)
	return out
}

func brief(p dbq.Paper, count, aiFilled int64) PaperBrief {
	b := PaperBrief{ID: p.ID, Kind: string(p.Kind), Title: p.Title, QuestionCount: int(count), FullScore: dec(p.FullScore), ActualScore: dec(p.ActualScore),
		MissingNote: p.MissingNote.String, DurationMinutes: int(p.DurationMinutes), AIFilled: int(aiFilled), Status: "not_started"}
	if p.ExamYear.Valid {
		y := int(p.ExamYear.Int16)
		b.ExamYear = &y
	}
	return b
}

// Papers 列出一门课的整卷（4.18）：真题卷按年份，AI 组卷按新到旧；每套卷的进行中 / 已完成 / 未开始。
func (s *Service) Papers(ctx context.Context, userID, subjectID uint64) (PaperList, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return PaperList{}, err
	}
	rows, err := s.q.ListBankPapers(ctx, dbq.ListBankPapersParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return PaperList{}, err
	}
	runs, err := s.q.ListPaperSessionsOfUser(ctx, dbq.ListPaperSessionsOfUserParams{OwnerUserID: userID, SubjectID: b.SubjectID})
	if err != nil {
		return PaperList{}, err
	}
	out := PaperList{Real: []PaperBrief{}, AI: []PaperBrief{}}
	for _, r := range rows {
		p := dbq.Paper{ID: r.ID, OwnerUserID: r.OwnerUserID, BankID: r.BankID, Kind: r.Kind, Title: r.Title, ExamYear: r.ExamYear, FullScore: r.FullScore,
			ActualScore: r.ActualScore, DurationMinutes: r.DurationMinutes, Structure: r.Structure, MissingNote: r.MissingNote}
		pb := brief(p, r.QuestionCount, r.AiFilled)
		for _, run := range runs {
			if !run.PaperID.Valid || uint64(run.PaperID.Int64) != r.ID {
				continue
			}
			pr := &PaperRun{SessionID: run.ID, Mode: string(run.Mode), Status: string(run.Status), FullScore: dec(run.FullScore)}
			if v, ok := parseScore(run.Score); ok {
				pr.Score = &v
			}
			if run.SubmittedAt.Valid {
				pr.FinishedAt = run.SubmittedAt.Time
			}
			switch run.Status {
			case dbq.PaperSessionsStatusInProgress, dbq.PaperSessionsStatusPaused:
				if pb.Active == nil {
					items, err := s.q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: run.ID, OwnerUserID: userID})
					if err != nil {
						return PaperList{}, err
					}
					for _, it := range items {
						pr.Total++
						if strings.TrimSpace(it.DraftText.String) != "" {
							pr.Answered++
						}
					}
					pb.Active, pb.Status = pr, "in_progress"
				}
			default:
				if pb.Last == nil {
					pb.Last = pr
					if pb.Status != "in_progress" {
						pb.Status = "done"
					}
				}
			}
		}
		if r.Kind == dbq.PapersKindRealExam {
			out.Real = append(out.Real, pb)
		} else {
			out.AI = append(out.AI, pb)
		}
	}
	if out.WeeklyRemaining, err = s.quota.Remaining(ctx, userID, quota.PaperGrading); err != nil {
		return PaperList{}, err
	}
	if a, err := s.q.ActivePaperSession(ctx, userID); err == nil && a.PaperID.Valid {
		out.InProgress = &struct {
			SessionID, PaperID, SubjectID uint64
			Title                         string
		}{a.ID, uint64(a.PaperID.Int64), a.SubjectID, a.PaperTitle}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PaperList{}, err
	}
	return out, nil
}

// paper 读出一套卷与它的专业课（不是自己的当作不存在）。
func (s *Service) paper(ctx context.Context, userID, paperID uint64) (dbq.GetPaperRow, error) {
	p, err := s.q.GetPaper(ctx, dbq.GetPaperParams{ID: paperID, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return p, apperr.NotFoundErr()
	}
	return p, err
}

// sectionsWithTimes 给试卷结构算各题型建议用时（PRD 11.9，与考情分析、时间报告同一套数字）。
func sectionsWithTimes(secs []PaperSection, total int, p rules.Params) []PaperSection {
	in := make([]rules.SectionCount, len(secs))
	for i, sc := range secs {
		in[i] = rules.SectionCount{QType: rules.QType(sc.QType), Count: sc.Count}
	}
	times := rules.SuggestedTimes(in, total, p.PaperTime)
	out := make([]PaperSection, len(secs))
	for i, sc := range secs {
		sc.SuggestedMinutes = times[i].Minutes
		out[i] = sc
	}
	return out
}

// Paper 是 4.19：题数、满分、时长、各题型建议用时、按阶段推荐的模式（强化期练习，冲刺期、考前期模拟考试）。
func (s *Service) Paper(ctx context.Context, userID, paperID uint64) (PaperDetail, error) {
	r, err := s.paper(ctx, userID, paperID)
	if err != nil {
		return PaperDetail{}, err
	}
	qs, err := s.q.ListPaperQuestions(ctx, dbq.ListPaperQuestionsParams{PaperID: r.ID, OwnerUserID: owner(userID)})
	if err != nil {
		return PaperDetail{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return PaperDetail{}, err
	}
	ai := 0
	for _, q := range qs {
		if q.Source == dbq.QuestionsSourceAiGenerated {
			ai++
		}
	}
	paper := dbq.Paper{ID: r.ID, Kind: r.Kind, Title: r.Title, ExamYear: r.ExamYear, FullScore: r.FullScore, ActualScore: r.ActualScore,
		DurationMinutes: r.DurationMinutes, MissingNote: r.MissingNote}
	d := PaperDetail{PaperBrief: brief(paper, int64(len(qs)), int64(ai)), CheckMinutes: p.PaperTime.CheckMinutes, RecommendedMode: paperModePractice,
		CountsForEstimate: r.Kind == dbq.PapersKindRealExam}
	d.Sections = sectionsWithTimes(structureOf(r.Structure), int(r.DurationMinutes), p)
	if pl, err := s.plan.Today(ctx, userID); err == nil {
		if st := rules.Stage(pl.Stage); st == rules.Sprint || st == rules.Final {
			d.RecommendedMode = paperModeMock
		}
	}
	return d, nil
}

var qtypeSection = map[string]string{"term": "名词解释", "short_answer": "简答", "discussion": "论述", "essay": "作文", "single_choice": "单选", "multi_choice": "多选",
	"true_false": "判断", "fill_blank": "填空", "calculation": "计算", "other": "其他"}

// ComposePaper 是 AI 组卷（PRD 11.10）：结构等于最近一套真题卷；按板块真题分值占比分配知识点；标准卷真题考过的知识点占 50–70%、
// 不出现在整卷里做过的真题；针对卷薄弱知识点（M < 60）占 50%、避开近 30 天做过的题；题库不够时用确认过的知识点出 AI 变式题补足、逐题标出。
// 组卷要本周还有整卷批改次数（做这套卷时扣）；补足的变式题不扣 AI 出题额度。
func (s *Service) ComposePaper(ctx context.Context, userID, subjectID uint64, kind string) (PaperDetail, error) {
	if kind != string(rules.PaperStandard) && kind != string(rules.PaperTargeted) {
		return PaperDetail{}, apperr.New(apperr.BadRequest, "组卷类型不正确")
	}
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return PaperDetail{}, err
	}
	real, err := s.q.LatestRealExamPaper(ctx, dbq.LatestRealExamPaperParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return PaperDetail{}, apperr.New(apperr.BadRequest, "先导入一套真题卷，AI 才能按你的真题结构组卷")
	}
	if err != nil {
		return PaperDetail{}, err
	}
	if left, err := s.quota.Remaining(ctx, userID, quota.PaperGrading); err != nil {
		return PaperDetail{}, err
	} else if left != nil && *left <= 0 {
		return PaperDetail{}, apperr.New(apperr.QuotaExceeded, "本周的整卷批改次数用完了")
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return PaperDetail{}, err
	}
	items, kps, err := s.pool(ctx, userID, b)
	if err != nil {
		return PaperDetail{}, err
	}
	done, err := s.q.DonePaperQuestionIDs(ctx, userID)
	if err != nil {
		return PaperDetail{}, err
	}
	doneSet := map[uint64]bool{}
	for _, id := range done {
		if id.Valid {
			doneSet[uint64(id.Int64)] = true
		}
	}
	lasts, err := s.q.LastAttemptDays(ctx, userID)
	if err != nil {
		return PaperDetail{}, err
	}
	today := s.today()
	lastDay := map[uint64]int{}
	for _, l := range lasts {
		if t, ok := asTime(l.LastAt); ok {
			lastDay[l.QuestionID] = int(today - rules.DayOf(t))
		}
	}
	// 知识点所在板块（最上层祖先）。
	sectionOf := func(kpID uint64) uint64 {
		cur, ok := kps[kpID]
		for depth := 0; ok && cur.ParentID.Valid && depth < 8; depth++ {
			parent, has := kps[uint64(cur.ParentID.Int64)]
			if !has {
				break
			}
			cur = parent
		}
		if !ok {
			return 0
		}
		return cur.ID
	}
	// 各板块的真题分值占比。
	exam, err := s.q.ListBankExamQuestions(ctx, dbq.ListBankExamQuestionsParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return PaperDetail{}, err
	}
	primary := map[uint64]int64{}
	for _, it := range items {
		primary[it.ID] = it.PrimaryKp
	}
	shares := map[int64]float64{}
	total := 0.0
	for _, q := range exam {
		sc, _ := parseScore(q.Score)
		if sec := sectionOf(uint64(primary[q.ID])); sec != 0 && sc > 0 {
			shares[int64(sec)] += sc
			total += sc
		}
	}
	for k := range shares {
		shares[k] /= total
	}
	var cands []rules.PaperCandidate
	for _, it := range items {
		days := -1
		if d, ok := lastDay[it.ID]; ok {
			days = d
		}
		k := kps[uint64(it.PrimaryKp)]
		cands = append(cands, rules.PaperCandidate{QuestionID: int64(it.ID), KPID: it.PrimaryKp, SectionID: int64(sectionOf(uint64(it.PrimaryKp))),
			QType: rules.QType(it.Qtype), ExamKP: k.ExamCount > 0, M: it.m, DoneInPaper: doneSet[it.ID] && it.Source == dbq.QuestionsSourceExam, DaysSinceDone: days})
	}
	secs := structureOf(real.Structure)
	slots := make([]rules.PaperSlot, len(secs))
	for i, sc := range secs {
		slots[i] = rules.PaperSlot{QType: rules.QType(sc.QType), Count: sc.Count, ScoreEach: sc.ScoreEach}
	}
	res := rules.ComposePaper(rules.ComposeInput{Kind: rules.PaperKind(kind), Slots: slots, Candidates: cands, SectionShares: shares}, p.AIPaper)
	// 缺的题用 AI 变式题补足（逐题标 AI 出题，不扣 AI 出题额度）。
	r := rand.New(rand.NewPCG(uint64(s.now().UnixNano()), userID)) //nolint:gosec // 只用来打乱出题顺序，不涉及安全
	type pick struct {
		qid   uint64
		qtype string
		score float64
	}
	var picks []pick
	for _, sl := range slots {
		for _, it := range res.Items {
			if it.QType == sl.QType {
				picks = append(picks, pick{uint64(it.QuestionID), string(it.QType), it.Score})
			}
		}
		if short := res.Shortfall[sl.QType]; short > 0 {
			gen, err := s.fill(ctx, userID, b, Config{QTypes: []string{string(sl.QType)}, OnlyUnmastered: kind == string(rules.PaperTargeted)}, short, kps, r, false)
			if err != nil {
				return PaperDetail{}, err
			}
			for _, id := range gen {
				picks = append(picks, pick{id, string(sl.QType), sl.ScoreEach})
			}
		}
	}
	if len(picks) == 0 {
		return PaperDetail{}, apperr.New(apperr.BadRequest, "题库里的题不够组卷，先多导入一些题目或讲义")
	}
	full := 0.0
	for _, pk := range picks {
		full += pk.score
	}
	title := map[string]string{string(rules.PaperStandard): "AI 标准卷", string(rules.PaperTargeted): "AI 针对卷"}[kind] + " · " + s.now().In(cst).Format("1月2日 15:04")
	var id int64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		var err error
		id, err = q.InsertComposedPaper(ctx, dbq.InsertComposedPaperParams{OwnerUserID: owner(userID), BankID: b.BankID, Kind: dbq.PapersKind(kind), Title: title,
			FullScore: fmtScore(full).String, ActualScore: fmtScore(full).String, DurationMinutes: real.DurationMinutes, Structure: real.Structure})
		if err != nil {
			return err
		}
		for i, pk := range picks {
			if err := q.InsertPaperQuestion(ctx, dbq.InsertPaperQuestionParams{PaperID: uint64(id), Seq: uint16(i + 1), QuestionID: pk.qid,
				Section: qtypeSection[pk.qtype], Score: fmtScore(pk.score).String}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return PaperDetail{}, err
	}
	return s.Paper(ctx, userID, uint64(id))
}

// asTime 读出 sqlc 推断为 interface{} 的时间列。
func asTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case []byte:
		t, err := time.Parse("2006-01-02 15:04:05.999", string(x))
		return t, err == nil
	case string:
		t, err := time.Parse("2006-01-02 15:04:05.999", x)
		return t, err == nil
	}
	return time.Time{}, false
}

// PaperItem 是整卷里的一道题（作答时不下发答案）。
type PaperItem struct {
	Seq           int
	Section       string
	QType         string
	Score         float64
	QuestionID    uint64
	Stem          string
	Options       []Option
	AIGenerated   bool
	Draft         string
	Marked        bool
	Answered      bool
	TimeSpent     int
	RequiredWords *int
	Got           *float64
	GradingID     uint64
}

// PaperReminder 是模拟考试的题型用时提醒（PRD 11.9：每种题型的累计建议用时到达时提醒一次）。
type PaperReminder struct {
	QType            string
	AtMinutes        int
	SuggestedMinutes int
	Text             string
}

// PaperSessionView 是一次整卷作答。
type PaperSessionView struct {
	ID, PaperID, SubjectID uint64
	Title, Kind, Mode      string
	Status                 string
	StartedAt              time.Time
	Deadline               *time.Time
	ServerNow              time.Time
	ElapsedSeconds         int
	TotalMinutes           int
	CheckMinutes           int
	RemindLeftMinutes      int
	Reminders              []PaperReminder
	Items                  []PaperItem
	ResumeAvailable        bool
	Score                  *float64
	FullScore              float64
	CountsForEstimate      bool
}

// StartPaper 开始做一套卷：同一时间只能有一套进行中；预占 1 次本周整卷批改次数（交卷结算、放弃退回）；
// 模拟考试按服务端截止时间计时，并在截止时间排一个自动交卷任务。
func (s *Service) StartPaper(ctx context.Context, userID, paperID uint64, mode, key string) (PaperSessionView, error) {
	if mode != paperModePractice && mode != paperModeMock {
		return PaperSessionView{}, apperr.New(apperr.BadRequest, "作答模式不正确")
	}
	if len(key) < 8 {
		return PaperSessionView{}, apperr.New(apperr.BadRequest, "缺少幂等键")
	}
	if prev, err := s.q.GetPaperSessionByKey(ctx, dbq.GetPaperSessionByKeyParams{OwnerUserID: userID, IdempotencyKey: sql.NullString{String: key, Valid: true}}); err == nil {
		return s.PaperSession(ctx, userID, prev.ID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return PaperSessionView{}, err
	}
	pr, err := s.paper(ctx, userID, paperID)
	if err != nil {
		return PaperSessionView{}, err
	}
	if a, err := s.q.ActivePaperSession(ctx, userID); err == nil {
		e := apperr.New(apperr.Conflict, "「"+a.PaperTitle+"」还没做完，先交卷或放弃再开始新的一套")
		e.Detail = map[string]any{"session_id": a.ID}
		return PaperSessionView{}, e
	} else if !errors.Is(err, sql.ErrNoRows) {
		return PaperSessionView{}, err
	}
	qs, err := s.q.ListPaperQuestions(ctx, dbq.ListPaperQuestionsParams{PaperID: pr.ID, OwnerUserID: owner(userID)})
	if err != nil {
		return PaperSessionView{}, err
	}
	if len(qs) == 0 {
		return PaperSessionView{}, apperr.New(apperr.BadRequest, "这套卷没有题目")
	}
	now := s.now().UTC()
	var deadline sql.NullTime
	if mode == paperModeMock {
		deadline = sql.NullTime{Time: rules.MockDeadline(now, int(pr.DurationMinutes)), Valid: true}
	}
	var id int64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		t, err := s.quota.Reserve(ctx, q, quota.Charge{UserID: userID, Type: quota.PaperGrading, Amount: 1, Ref: quota.Ref{Type: "paper", ID: pr.ID}, Key: "paper_start:" + key})
		if err != nil {
			return err
		}
		id, err = q.InsertPaperSession(ctx, dbq.InsertPaperSessionParams{OwnerUserID: userID, PaperID: sql.NullInt64{Int64: int64(pr.ID), Valid: true},
			SubjectID: uint64(pr.SubjectID.Int64), PaperKind: dbq.PaperSessionsPaperKind(pr.Kind), PaperTitle: pr.Title, Mode: dbq.PaperSessionsMode(mode),
			StartedAt: now, DeadlineAt: deadline, FullScore: pr.FullScore, CountsForEstimate: pr.Kind == dbq.PapersKindRealExam,
			QuotaPeriod: sql.NullString{String: t.Period, Valid: true}, IdempotencyKey: sql.NullString{String: key, Valid: true}})
		if err != nil {
			return err
		}
		for _, x := range qs {
			if err := q.InsertPaperSessionItem(ctx, dbq.InsertPaperSessionItemParams{PaperSessionID: uint64(id), Seq: x.Seq, QuestionID: sql.NullInt64{Int64: int64(x.QuestionID), Valid: true},
				Qtype: string(x.Qtype), Section: x.Section, ScoreMax: x.Score}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return PaperSessionView{}, err
	}
	if deadline.Valid {
		s.scheduleDeadline(ctx, userID, uint64(id), deadline.Time)
	}
	return s.PaperSession(ctx, userID, uint64(id))
}

// scheduleDeadline 在截止时间排自动交卷；排失败不影响作答（打开作答页时过了截止时间也会自动交卷）。
func (s *Service) scheduleDeadline(ctx context.Context, userID, sessionID uint64, deadline time.Time) {
	if s.queue == nil {
		return
	}
	t, err := jobs.NewPaperDeadlineTask(userID, sessionID, deadline)
	if err == nil {
		_, err = s.queue.EnqueueContext(ctx, t)
	}
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		logx.From(ctx).Warn("schedule paper deadline failed", "session_id", sessionID, "err", err)
	}
}

func (s *Service) paperSessionRow(ctx context.Context, q *dbq.Queries, userID, id uint64, lock bool) (dbq.PaperSession, error) {
	var row dbq.PaperSession
	var err error
	if lock {
		row, err = q.GetPaperSessionForUpdate(ctx, dbq.GetPaperSessionForUpdateParams{ID: id, OwnerUserID: userID})
	} else {
		row, err = q.GetPaperSession(ctx, dbq.GetPaperSessionParams{ID: id, OwnerUserID: userID})
	}
	if errors.Is(err, sql.ErrNoRows) {
		return row, apperr.NotFoundErr()
	}
	return row, err
}

func open(st dbq.PaperSessionsStatus) bool {
	return st == dbq.PaperSessionsStatusInProgress || st == dbq.PaperSessionsStatusPaused
}

// expired 报告模拟考试是否过了截止时间（以服务端时间为准）。
func (s *Service) expired(row dbq.PaperSession) bool {
	return row.Mode == dbq.PaperSessionsModeMock && row.DeadlineAt.Valid && !s.now().Before(row.DeadlineAt.Time) && open(row.Status)
}

// PaperSession 读出一次整卷作答；模拟考试过了截止时间先自动交卷。
func (s *Service) PaperSession(ctx context.Context, userID, id uint64) (PaperSessionView, error) {
	row, err := s.paperSessionRow(ctx, s.q, userID, id, false)
	if err != nil {
		return PaperSessionView{}, err
	}
	if s.expired(row) {
		if _, err := s.SubmitPaper(ctx, userID, id); err != nil {
			return PaperSessionView{}, err
		}
		if row, err = s.paperSessionRow(ctx, s.q, userID, id, false); err != nil {
			return PaperSessionView{}, err
		}
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return PaperSessionView{}, err
	}
	now := s.now()
	v := PaperSessionView{ID: row.ID, SubjectID: row.SubjectID, Title: row.PaperTitle, Kind: string(row.PaperKind), Mode: string(row.Mode), Status: string(row.Status),
		StartedAt: row.StartedAt, ServerNow: now.UTC(), CheckMinutes: p.PaperTime.CheckMinutes, RemindLeftMinutes: p.PaperTime.RemindLeftMinutes,
		FullScore: dec(row.FullScore), CountsForEstimate: row.CountsForEstimate, Reminders: []PaperReminder{}}
	if row.PaperID.Valid {
		v.PaperID = uint64(row.PaperID.Int64)
	}
	if row.DeadlineAt.Valid {
		d := row.DeadlineAt.Time
		v.Deadline = &d
	}
	if sc, ok := parseScore(row.Score); ok {
		v.Score = &sc
	}
	end := now
	if row.SubmittedAt.Valid {
		end = row.SubmittedAt.Time
	}
	paused := int(row.PausedSeconds)
	if row.PausedAt.Valid && open(row.Status) {
		paused += int(now.Sub(row.PausedAt.Time).Seconds())
	}
	v.ElapsedSeconds = max(int(end.Sub(row.StartedAt).Seconds())-paused, 0)
	v.ResumeAvailable = row.Mode == dbq.PaperSessionsModeMock && !row.ResumeUsed && open(row.Status)
	items, err := s.q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: id, OwnerUserID: userID})
	if err != nil {
		return PaperSessionView{}, err
	}
	var secs []rules.SectionCount
	for _, it := range items {
		pi := PaperItem{Seq: int(it.Seq), Section: it.Section, QType: it.Qtype, Score: dec(it.ScoreMax), Stem: it.Stem.String, Draft: it.DraftText.String,
			Marked: it.Marked, TimeSpent: int(it.TimeSpentSeconds), AIGenerated: it.Source.QuestionsSource == dbq.QuestionsSourceAiGenerated, GradingID: uint64(it.GradingID)}
		if it.QuestionID.Valid {
			pi.QuestionID = uint64(it.QuestionID.Int64)
		}
		if it.Options != nil {
			_ = json.Unmarshal(it.Options, &pi.Options)
		}
		if it.RequiredWords.Valid {
			w := int(it.RequiredWords.Int16)
			pi.RequiredWords = &w
		}
		if g, ok := parseScore(it.Got); ok && !open(row.Status) {
			pi.Got = &g
		}
		pi.Answered = strings.TrimSpace(pi.Draft) != ""
		v.Items = append(v.Items, pi)
		if n := len(secs); n > 0 && string(secs[n-1].QType) == it.Qtype {
			secs[n-1].Count++
		} else {
			secs = append(secs, rules.SectionCount{QType: rules.QType(it.Qtype), Count: 1})
		}
	}
	total := 0
	if row.PaperID.Valid {
		if pr, err := s.paper(ctx, userID, uint64(row.PaperID.Int64)); err == nil {
			total = int(pr.DurationMinutes)
		}
	}
	if total == 0 {
		total = p.PaperTime.DefaultTotalMinutes
	}
	v.TotalMinutes = total
	times := rules.SuggestedTimes(secs, total, p.PaperTime)
	for i, t := range times {
		r := PaperReminder{QType: string(t.QType), AtMinutes: t.CumulativeMinutes, SuggestedMinutes: t.Minutes}
		name := qtypeSection[string(t.QType)]
		if i+1 < len(times) {
			r.Text = fmt.Sprintf("%s的建议用时（%d 分钟）到了，建议进入%s", name, t.Minutes, qtypeSection[string(times[i+1].QType)])
		} else {
			r.Text = fmt.Sprintf("%s的建议用时到了，留 %d 分钟检查", name, p.PaperTime.CheckMinutes)
		}
		v.Reminders = append(v.Reminders, r)
	}
	return v, nil
}

// ItemUpdate 是一道题的草稿、标记与停留用时。
type ItemUpdate struct {
	Draft     *string
	Marked    *bool
	TimeDelta int
}

// UpdatePaperItem 保存草稿（客户端每 5 秒同步）。交卷后或模拟考试过了截止时间返回 409（并自动交卷）。
func (s *Service) UpdatePaperItem(ctx context.Context, userID, sessionID uint64, seq int, in ItemUpdate) (PaperItem, error) {
	row, err := s.paperSessionRow(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return PaperItem{}, err
	}
	if s.expired(row) {
		if _, err := s.SubmitPaper(ctx, userID, sessionID); err != nil {
			return PaperItem{}, err
		}
		return PaperItem{}, apperr.New(apperr.Conflict, "时间到了，已自动交卷")
	}
	if !open(row.Status) {
		return PaperItem{}, apperr.New(apperr.Conflict, "这套卷已经交卷了")
	}
	items, err := s.q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: sessionID, OwnerUserID: userID})
	if err != nil {
		return PaperItem{}, err
	}
	var cur *dbq.ListPaperSessionItemsRow
	for i := range items {
		if int(items[i].Seq) == seq {
			cur = &items[i]
		}
	}
	if cur == nil {
		return PaperItem{}, apperr.NotFoundErr()
	}
	draft, marked := cur.DraftText, cur.Marked
	if in.Draft != nil {
		draft = sql.NullString{String: *in.Draft, Valid: true}
	}
	if in.Marked != nil {
		marked = *in.Marked
	}
	delta := uint32(max(0, min(in.TimeDelta, 600)))
	if row.Status == dbq.PaperSessionsStatusPaused {
		delta = 0 // 暂停时不计用时
	}
	if _, err := s.q.UpdatePaperItem(ctx, dbq.UpdatePaperItemParams{DraftText: draft, Marked: marked, TimeSpentSeconds: delta, PaperSessionID: sessionID,
		Seq: uint16(seq), OwnerUserID: userID}); err != nil {
		return PaperItem{}, err
	}
	return PaperItem{Seq: seq, Section: cur.Section, QType: cur.Qtype, Score: dec(cur.ScoreMax), QuestionID: uint64(cur.QuestionID.Int64), Stem: cur.Stem.String,
		Draft: draft.String, Marked: marked, Answered: strings.TrimSpace(draft.String) != "", TimeSpent: int(cur.TimeSpentSeconds + delta)}, nil
}

// PausePaper 暂停（只有练习模式可以）。
func (s *Service) PausePaper(ctx context.Context, userID, sessionID uint64) (PaperSessionView, error) {
	row, err := s.paperSessionRow(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return PaperSessionView{}, err
	}
	if row.Mode != dbq.PaperSessionsModePractice {
		return PaperSessionView{}, apperr.New(apperr.Conflict, "模拟考试不能暂停")
	}
	if row.Status != dbq.PaperSessionsStatusInProgress {
		return PaperSessionView{}, apperr.New(apperr.Conflict, "这套卷不在作答中")
	}
	if err := s.q.PausePaperSession(ctx, dbq.PausePaperSessionParams{PausedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: sessionID, OwnerUserID: userID}); err != nil {
		return PaperSessionView{}, err
	}
	return s.PaperSession(ctx, userID, sessionID)
}

// ResumePaper 继续作答：练习模式取消暂停；模拟考试因系统原因中断时，10 分钟内可恢复一次并补回中断时长（PRD 11.9）。
func (s *Service) ResumePaper(ctx context.Context, userID, sessionID uint64, interruptedAt *time.Time) (PaperSessionView, error) {
	row, err := s.paperSessionRow(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return PaperSessionView{}, err
	}
	now := s.now()
	switch row.Mode {
	case dbq.PaperSessionsModePractice:
		if row.Status != dbq.PaperSessionsStatusPaused {
			return s.PaperSession(ctx, userID, sessionID)
		}
		paused := 0
		if row.PausedAt.Valid {
			paused = int(now.Sub(row.PausedAt.Time).Seconds())
		}
		if err := s.q.ResumePaperSession(ctx, dbq.ResumePaperSessionParams{PausedSeconds: uint32(max(paused, 0)), ID: sessionID, OwnerUserID: userID}); err != nil {
			return PaperSessionView{}, err
		}
	default:
		p, err := s.params.Rules(ctx)
		if err != nil {
			return PaperSessionView{}, err
		}
		if interruptedAt == nil || !open(row.Status) || interruptedAt.Before(row.StartedAt) || !rules.CanResume(*interruptedAt, now, row.ResumeUsed, p.PaperTime) {
			return PaperSessionView{}, apperr.New(apperr.Conflict, "只能在中断后 10 分钟内恢复一次")
		}
		deadline := row.DeadlineAt.Time.Add(now.Sub(*interruptedAt))
		if err := s.q.RecoverPaperSession(ctx, dbq.RecoverPaperSessionParams{DeadlineAt: sql.NullTime{Time: deadline.UTC(), Valid: true},
			InterruptedAt: sql.NullTime{Time: interruptedAt.UTC(), Valid: true}, ID: sessionID, OwnerUserID: userID}); err != nil {
			return PaperSessionView{}, err
		}
		s.scheduleDeadline(ctx, userID, sessionID, deadline)
	}
	return s.PaperSession(ctx, userID, sessionID)
}

// AbandonPaper 放弃这套卷，退回预占的整卷批改次数。
func (s *Service) AbandonPaper(ctx context.Context, userID, sessionID uint64) error {
	return s.withTx(ctx, func(q *dbq.Queries) error {
		row, err := s.paperSessionRow(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		if !open(row.Status) {
			return apperr.New(apperr.Conflict, "这套卷已经交卷了")
		}
		if err := q.AbandonPaperSession(ctx, dbq.AbandonPaperSessionParams{ID: sessionID, OwnerUserID: userID}); err != nil {
			return err
		}
		return s.quota.Settle(ctx, q, s.paperTicket(row), 0, quota.Ref{Type: "paper_session", ID: sessionID})
	})
}

func (s *Service) paperTicket(row dbq.PaperSession) quota.Ticket {
	return quota.Ticket{UserID: row.OwnerUserID, Type: quota.PaperGrading, Period: row.QuotaPeriod.String, Amount: 1, Key: "paper_start:" + row.IdempotencyKey.String}
}

// SubmitPaper 交卷：客观题当场判分并更新学习状态，结算整卷批改次数；主观题交给后台逐题批改（约 2 分钟），完成后发消息。
// 重复交卷返回同一结果。
func (s *Service) SubmitPaper(ctx context.Context, userID, sessionID uint64) (PaperSessionView, error) {
	p, err := s.params.Rules(ctx)
	if err != nil {
		return PaperSessionView{}, err
	}
	submitted := false
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		row, err := s.paperSessionRow(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		if !open(row.Status) {
			return nil
		}
		now := s.now().UTC()
		at := now
		if row.Mode == dbq.PaperSessionsModeMock && row.DeadlineAt.Valid && row.DeadlineAt.Time.Before(now) {
			at = row.DeadlineAt.Time // 时间到自动交卷：按截止时间记
		}
		paused := 0
		if row.PausedAt.Valid {
			paused = int(at.Sub(row.PausedAt.Time).Seconds())
		}
		if err := q.SubmitPaperSession(ctx, dbq.SubmitPaperSessionParams{SubmittedAt: sql.NullTime{Time: at, Valid: true}, PausedSeconds: uint32(max(paused, 0)),
			ID: sessionID, OwnerUserID: userID}); err != nil {
			return err
		}
		items, err := q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: sessionID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		for _, it := range items {
			if !it.QuestionID.Valid || !rules.IsObjective(rules.QType(it.Qtype)) || strings.TrimSpace(it.DraftText.String) == "" {
				continue
			}
			if err := s.judgePaperObjective(ctx, q, p, userID, sessionID, row, it, at); err != nil {
				return err
			}
		}
		submitted = true
		return s.quota.Settle(ctx, q, s.paperTicket(row), 1, quota.Ref{Type: "paper_session", ID: sessionID})
	})
	if err != nil {
		return PaperSessionView{}, err
	}
	if submitted {
		if err := s.enqueueGrade(ctx, userID, sessionID); err != nil {
			return PaperSessionView{}, err
		}
	}
	return s.PaperSession(ctx, userID, sessionID)
}

func (s *Service) enqueueGrade(ctx context.Context, userID, sessionID uint64) error {
	if s.queue == nil {
		return s.GradePaper(ctx, userID, sessionID)
	}
	t, err := jobs.NewPaperGradeTask(userID, sessionID)
	if err != nil {
		return err
	}
	if _, err := s.queue.EnqueueContext(ctx, t); err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		return err
	}
	return nil
}

func (s *Service) judgePaperObjective(ctx context.Context, q *dbq.Queries, p rules.Params, userID, sessionID uint64, row dbq.PaperSession, it dbq.ListPaperSessionItemsRow, at time.Time) error {
	qr, err := q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: uint64(it.QuestionID.Int64), OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var opts []Option
	if qr.Options != nil {
		_ = json.Unmarshal(qr.Options, &opts)
	}
	draft := it.DraftText.String
	var selected []string
	if qr.Qtype != dbq.QuestionsQtypeFillBlank {
		selected = keysOf(draft)
	}
	correct, ok := Judge(string(qr.Qtype), opts, qr.Answer.String, selected, draft)
	if !ok {
		return nil
	}
	got := 0.0
	if correct {
		got = dec(it.ScoreMax)
	}
	sel, _ := json.Marshal(selected)
	aid, err := q.InsertPaperAttempt(ctx, dbq.InsertPaperAttemptParams{OwnerUserID: userID, QuestionID: qr.ID, PaperSessionID: sql.NullInt64{Int64: int64(sessionID), Valid: true},
		AnswerMode: dbq.AttemptsAnswerModeChoice, Selected: sel, AnswerText: sql.NullString{String: draft, Valid: true}, IsCorrect: sql.NullBool{Bool: correct, Valid: true},
		Score: fmtScore(got), FullScore: sql.NullString{String: it.ScoreMax, Valid: true}, DurationSeconds: it.TimeSpentSeconds,
		Timed: row.Mode == dbq.PaperSessionsModeMock, AnsweredAt: at})
	if err != nil {
		return err
	}
	if err := q.SetPaperItemAttempt(ctx, dbq.SetPaperItemAttemptParams{AttemptID: sql.NullInt64{Int64: aid, Valid: true}, PaperSessionID: sessionID, Seq: it.Seq, OwnerUserID: userID}); err != nil {
		return err
	}
	_, err = Apply(ctx, q, p, userID, qr.ID, Outcome{Objective: true, Correct: correct}, rules.DayOf(at))
	return err
}

// AutoSubmitPaper 是模拟考试截止时间的后台任务：还没交卷就自动交卷（提前执行时不做事，作答页过了截止时间也会自动交卷）。
func (s *Service) AutoSubmitPaper(ctx context.Context, userID, sessionID uint64) error {
	row, err := s.paperSessionRow(ctx, s.q, userID, sessionID, false)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Kind == apperr.NotFound {
			return nil
		}
		return err
	}
	if !s.expired(row) {
		return nil
	}
	_, err = s.SubmitPaper(ctx, userID, sessionID)
	return err
}

// PaperReport 是整卷批改完成时保存的统计（4.24、4.25 在 T22 用它出报告）。
type PaperReport struct {
	RawScore    float64 `json:"raw_score"`
	ActualFull  float64 `json:"actual_full"`
	PaperFull   float64 `json:"paper_full"`
	Score       float64 `json:"score"`
	Unanswered  int     `json:"unanswered"`
	TimeLoss    float64 `json:"time_loss"`
	UsedSeconds int     `json:"used_seconds"`
	// ElapsedSeconds 是开考到交卷的时间（扣除暂停）；TotalMinutes 是试卷时长（T22 时间分析用）。
	ElapsedSeconds int                `json:"elapsed_seconds"`
	TotalMinutes   int                `json:"total_minutes"`
	Loss           map[string]float64 `json:"loss"`
	ByQType        []PaperQTypeStat   `json:"by_qtype"`
}

// PaperQTypeStat 是一种题型的得分与用时。
type PaperQTypeStat struct {
	QType            string  `json:"qtype"`
	Count            int     `json:"count"`
	Answered         int     `json:"answered"`
	Got              float64 `json:"got"`
	Full             float64 `json:"full"`
	TimeSeconds      int     `json:"time_seconds"`
	SuggestedMinutes int     `json:"suggested_minutes"`
	Overtime         bool    `json:"overtime"`
}

// GradePaper 是交卷后的后台批改：主观题逐题按采分点批改（不另扣批改次数，整卷已算 1 次），没作答的计 0 分；
// 汇总得分（缺题卷按比例换算到整卷满分）、失分归因与各题型用时，完成后发消息。可重复执行：已批改的题跳过。
func (s *Service) GradePaper(ctx context.Context, userID, sessionID uint64) error {
	row, err := s.paperSessionRow(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return err
	}
	if row.Status != dbq.PaperSessionsStatusGrading {
		return nil
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return err
	}
	items, err := s.q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: sessionID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	mock := row.Mode == dbq.PaperSessionsModeMock
	at := row.SubmittedAt.Time
	// 各题型每题的建议秒数（判定「时间不够」用）。
	var secs []rules.SectionCount
	for _, it := range items {
		if n := len(secs); n > 0 && string(secs[n-1].QType) == it.Qtype {
			secs[n-1].Count++
		} else {
			secs = append(secs, rules.SectionCount{QType: rules.QType(it.Qtype), Count: 1})
		}
	}
	total := p.PaperTime.DefaultTotalMinutes
	if row.PaperID.Valid {
		if pr, err := s.paper(ctx, userID, uint64(row.PaperID.Int64)); err == nil {
			total = int(pr.DurationMinutes)
		}
	}
	times := rules.SuggestedTimes(secs, total, p.PaperTime)
	perQuestion := map[string]float64{}
	suggested := map[string]int{}
	for i, t := range times {
		suggested[string(t.QType)] = t.Minutes
		if secs[i].Count > 0 {
			perQuestion[string(t.QType)] = float64(t.Minutes) * 60 / float64(secs[i].Count)
		}
	}
	subject := ""
	if b, err := s.subject(ctx, userID, row.SubjectID); err == nil {
		subject = b.Name
	}
	lossAll := map[string]float64{"knowledge": 0, "norm": 0, "time": 0}
	for i, it := range items {
		if !it.QuestionID.Valid || it.AttemptID.Valid || rules.IsObjective(rules.QType(it.Qtype)) {
			continue
		}
		answer := strings.TrimSpace(it.DraftText.String)
		if answer == "" {
			continue // 没作答：计 0 分，时间失分在汇总里估计
		}
		qr, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: uint64(it.QuestionID.Int64), OwnerUserID: owner(userID)})
		if err != nil {
			continue
		}
		snap, err := s.snapshot(ctx, s.q, userID, qr)
		if err != nil {
			continue // 没有采分点也没有参考答案的题不批改
		}
		// 整卷里每题按卷面分值批改。
		if full := dec(it.ScoreMax); full > 0 && snap.FullScore > 0 && full != snap.FullScore {
			ratio := full / snap.FullScore
			for j := range snap.Points {
				snap.Points[j].Score = round1(snap.Points[j].Score * ratio)
			}
			snap.FullScore = full
		}
		kpM, kpName, err := s.primaryKP(ctx, s.q, userID, qr.ID)
		if err != nil {
			return err
		}
		out, meta, err := ai.Grade.Run(ctx, s.ai, userID, ai.GradeIn{Subject: subject, QType: string(qr.Qtype), Stem: qr.Stem, Reference: snap.Reference, Points: snap.Points, Answer: answer})
		if err != nil {
			return err // 交给任务重试；已批改的题下次跳过
		}
		loss, mainLoss := diagnose(snap, out, kpM, kpName, lossTiming{Timed: mock, Position: float64(i) / float64(max(len(items)-1, 1)),
			SpentSeconds: float64(it.TimeSpentSeconds), SuggestedSeconds: perQuestion[it.Qtype]}, p)
		snapJSON, _ := json.Marshal(snap)
		err = s.withTx(ctx, func(q *dbq.Queries) error {
			aid, err := q.InsertPaperAttempt(ctx, dbq.InsertPaperAttemptParams{OwnerUserID: userID, QuestionID: qr.ID, PaperSessionID: sql.NullInt64{Int64: int64(sessionID), Valid: true},
				AnswerMode: dbq.AttemptsAnswerModeTyped, AnswerText: sql.NullString{String: answer, Valid: true}, Score: fmtScore(out.Total()), FullScore: fmtScore(snap.FullScore),
				DurationSeconds: it.TimeSpentSeconds, Timed: mock, AnsweredAt: at})
			if err != nil {
				return err
			}
			eff, err := Apply(ctx, q, p, userID, qr.ID, Outcome{Graded: true, ScoreRate: out.Total() / snap.FullScore, LossType: mainLoss}, rules.DayOf(at))
			if err != nil {
				return err
			}
			extra := gradingExtra{Items: out.Suggestions, StructureOK: out.StructureOK, StructureNote: out.StructureNote, KPs: eff.KPs, WrongBook: eff.WrongBook}
			if _, err := s.insertDone(ctx, q, userID, uint64(aid), dbq.GradingsTriggerReasonSubmit, 0, snap, snapJSON, out, loss, extra, meta, false, ""); err != nil {
				return err
			}
			return q.SetPaperItemAttempt(ctx, dbq.SetPaperItemAttemptParams{AttemptID: sql.NullInt64{Int64: aid, Valid: true}, PaperSessionID: sessionID, Seq: it.Seq, OwnerUserID: userID})
		})
		if err != nil {
			return err
		}
		for _, l := range loss {
			lossAll[l.Type] += l.Points
		}
	}
	return s.finishPaper(ctx, userID, row, total, suggested, lossAll, p)
}

func (s *Service) finishPaper(ctx context.Context, userID uint64, row dbq.PaperSession, total int, suggested map[string]int, lossAll map[string]float64, p rules.Params) error {
	items, err := s.q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: row.ID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	rates, err := s.q.QTypeScoreRates(ctx, userID)
	if err != nil {
		return err
	}
	avg := map[rules.QType]float64{}
	for _, r := range rates {
		if f := dec(r.Full); f > 0 {
			avg[rules.QType(r.Qtype)] = dec(r.Got) / f
		}
	}
	rep := PaperReport{Loss: lossAll, PaperFull: dec(row.FullScore), TotalMinutes: total}
	if row.SubmittedAt.Valid {
		rep.ElapsedSeconds = max(int(row.SubmittedAt.Time.Sub(row.StartedAt).Seconds())-int(row.PausedSeconds), 0)
	}
	stats := map[string]*PaperQTypeStat{}
	var order []string
	var unanswered []rules.UnansweredQuestion
	for _, it := range items {
		full := dec(it.ScoreMax)
		rep.ActualFull += full
		rep.UsedSeconds += int(it.TimeSpentSeconds)
		st, ok := stats[it.Qtype]
		if !ok {
			st = &PaperQTypeStat{QType: it.Qtype, SuggestedMinutes: suggested[it.Qtype]}
			stats[it.Qtype] = st
			order = append(order, it.Qtype)
		}
		st.Count++
		st.Full += full
		st.TimeSeconds += int(it.TimeSpentSeconds)
		if strings.TrimSpace(it.DraftText.String) == "" {
			rep.Unanswered++
			unanswered = append(unanswered, rules.UnansweredQuestion{QType: rules.QType(it.Qtype), FullScore: full})
			continue
		}
		st.Answered++
		if g, ok := parseScore(it.Got); ok {
			st.Got += g
			rep.RawScore += g
		}
	}
	for _, q := range order {
		st := stats[q]
		st.Overtime = st.SuggestedMinutes > 0 && rules.Overtime(float64(st.TimeSeconds)/60, float64(st.SuggestedMinutes), p.PaperTime)
		rep.ByQType = append(rep.ByQType, *st)
	}
	if row.Mode == dbq.PaperSessionsModeMock {
		rep.TimeLoss = round1(rules.TimeLoss(unanswered, avg))
		rep.Loss["time"] += rep.TimeLoss
	} else {
		for _, u := range unanswered {
			rep.Loss["knowledge"] += u.FullScore
		}
	}
	if rep.PaperFull <= 0 {
		rep.PaperFull = rep.ActualFull
	}
	rep.Score = round1(rules.ScalePaperScore(rep.RawScore, rep.ActualFull, rep.PaperFull))
	raw, _ := json.Marshal(rep)
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		if err := q.FinishPaperGrading(ctx, dbq.FinishPaperGradingParams{GradedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, Score: fmtScore(rep.Score),
			Report: dbtypes.NullJSON(raw), ID: row.ID, OwnerUserID: userID}); err != nil {
			return err
		}
		link, _ := json.Marshal(map[string]any{"page": "paper_report", "params": map[string]any{"session_id": row.ID}})
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypePaperGraded, Title: "整卷批改完成",
			Body: fmt.Sprintf("「%s」批改完成，AI 批改得分 %s / %s（仅供参考），点开看整卷报告", row.PaperTitle, fmtScore(rep.Score).String, fmtScore(rep.PaperFull).String),
			Link: link, DedupeKey: sql.NullString{String: "paper_graded:" + strconv.FormatUint(row.ID, 10), Valid: true}})
	})
	if err != nil {
		return err
	}
	// 交卷批改完成后重算预估分（PRD 11.6）；只有导入真题卷计入，重算失败不影响整卷结果。
	if row.CountsForEstimate && s.score != nil {
		if err := s.score.Recompute(ctx, userID, row.SubjectID, "paper_graded"); err != nil {
			logx.From(ctx).Warn("recompute estimate", "err", err, "session_id", row.ID)
		}
	}
	return nil
}
