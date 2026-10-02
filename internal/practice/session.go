package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

// KPRef 是题目关联的知识点。
type KPRef struct {
	ID        uint64
	Name      string
	IsPrimary bool
}

// Answered 是本组里这道题最近一次作答。
type Answered struct {
	IsCorrect  *bool
	Revealed   bool
	SelfAssess string
	Selected   []string
}

// Question 是练习里的一道题（含客观题答案，供离线判分）。
type Question struct {
	ID         uint64
	QType      string
	Stem       string
	Options    []Option
	Answer     string
	Analysis   string
	Score      *float64
	Source     string
	ExamYear   *int
	MaterialID uint64
	FileName   string
	Page       int
	PlanGroup  string
	KPs        []KPRef
	RubricN    int
	Answered   *Answered
}

// Session 是练习会话。
type Session struct {
	ID          uint64
	Kind        Kind
	Title       string
	SubjectID   uint64
	Status      string
	CursorIndex int
	StartedAt   time.Time
	Questions   []Question
	DoneCount   int
	AIFilled    int
	Shortfall   int
}

func (s *Service) session(ctx context.Context, q *dbq.Queries, userID, id uint64, lock bool) (dbq.PracticeSession, []uint64, sessionMeta, error) {
	var row dbq.PracticeSession
	var err error
	if lock {
		row, err = q.GetPracticeSessionForUpdate(ctx, dbq.GetPracticeSessionForUpdateParams{ID: id, OwnerUserID: userID})
	} else {
		row, err = q.GetPracticeSession(ctx, dbq.GetPracticeSessionParams{ID: id, OwnerUserID: userID})
	}
	if errors.Is(err, sql.ErrNoRows) {
		return row, nil, sessionMeta{}, apperr.NotFoundErr()
	}
	if err != nil {
		return row, nil, sessionMeta{}, err
	}
	var ids []uint64
	if err := json.Unmarshal(row.QuestionIds, &ids); err != nil {
		return row, nil, sessionMeta{}, err
	}
	var meta sessionMeta
	if row.Config != nil {
		_ = json.Unmarshal(row.Config, &meta)
	}
	return row, ids, meta, nil
}

// Get 返回会话与整组题目。被删除或下线（AI 题被报错 3 次）的题跳过。
func (s *Service) Get(ctx context.Context, userID, id uint64) (Session, error) {
	row, ids, meta, err := s.session(ctx, s.q, userID, id, false)
	if err != nil {
		return Session{}, err
	}
	out := Session{ID: row.ID, Kind: Kind(row.Kind), Title: row.Title, SubjectID: uint64(row.SubjectID.Int64), Status: string(row.Status),
		CursorIndex: int(row.CursorIndex), StartedAt: row.StartedAt, AIFilled: meta.AIFilled, Shortfall: meta.Shortfall}
	atts, err := s.q.ListSessionAttempts(ctx, dbq.ListSessionAttemptsParams{PracticeSessionID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: userID})
	if err != nil {
		return Session{}, err
	}
	last := map[uint64]dbq.ListSessionAttemptsRow{}
	for _, a := range atts {
		last[a.QuestionID] = a
	}
	files := map[uint64]string{}
	for _, qid := range ids {
		qr, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: qid, OwnerUserID: owner(userID)})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return Session{}, err
		}
		if qr.Status != dbq.QuestionsStatusActive {
			continue
		}
		qu, err := s.question(ctx, userID, qr, files)
		if err != nil {
			return Session{}, err
		}
		qu.PlanGroup = meta.Groups[strconv.FormatUint(qid, 10)]
		if a, ok := last[qid]; ok {
			ans := &Answered{Revealed: a.RevealedAnswer}
			if a.IsCorrect.Valid {
				v := a.IsCorrect.Bool
				ans.IsCorrect = &v
			}
			if a.SelfAssess.Valid {
				ans.SelfAssess = string(a.SelfAssess.AttemptsSelfAssess)
			}
			if a.Selected != nil {
				_ = json.Unmarshal(a.Selected, &ans.Selected)
			}
			qu.Answered = ans
			out.DoneCount++
		}
		out.Questions = append(out.Questions, qu)
	}
	return out, nil
}

func (s *Service) question(ctx context.Context, userID uint64, qr dbq.GetQuestionFullRow, files map[uint64]string) (Question, error) {
	qu := Question{ID: qr.ID, QType: string(qr.Qtype), Stem: qr.Stem, Answer: qr.Answer.String, Analysis: qr.Analysis.String, Source: string(qr.Source)}
	if qr.Options != nil {
		_ = json.Unmarshal(qr.Options, &qu.Options)
	}
	if qr.Score.Valid {
		v, _ := strconv.ParseFloat(qr.Score.String, 64)
		qu.Score = &v
	}
	if qr.ExamYear.Valid {
		y := int(qr.ExamYear.Int16)
		qu.ExamYear = &y
	}
	if qr.SourceMaterialID.Valid {
		mid := uint64(qr.SourceMaterialID.Int64)
		name, ok := files[mid]
		if !ok {
			m, err := s.q.GetMaterial(ctx, dbq.GetMaterialParams{ID: mid, OwnerUserID: userID})
			if err == nil {
				name = m.FileName
			}
			files[mid] = name
		}
		if name != "" {
			qu.MaterialID, qu.FileName, qu.Page = mid, name, int(qr.SourcePage.Int32)
		}
	}
	kps, err := s.q.ListQuestionKPs(ctx, dbq.ListQuestionKPsParams{QuestionID: qr.ID, OwnerUserID: owner(userID)})
	if err != nil {
		return qu, err
	}
	for _, k := range kps {
		qu.KPs = append(qu.KPs, KPRef{ID: k.ID, Name: k.Name, IsPrimary: k.IsPrimary})
	}
	rp, err := s.q.ListQuestionRubric(ctx, dbq.ListQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(qr.ID), Valid: true}, OwnerUserID: owner(userID)})
	qu.RubricN = len(rp)
	return qu, err
}

// SaveProgress 保存断点（4.10 退出训练）。
func (s *Service) SaveProgress(ctx context.Context, userID, id uint64, cursor int) error {
	_, ids, _, err := s.session(ctx, s.q, userID, id, false)
	if err != nil {
		return err
	}
	cursor = max(0, min(cursor, len(ids)))
	return s.q.SetSessionCursor(ctx, dbq.SetSessionCursorParams{CursorIndex: uint32(cursor), ID: id, OwnerUserID: userID})
}

// AttemptInput 是一次作答。
type AttemptInput struct {
	QuestionID uint64
	Key        string
	Selected   []string
	AnswerText string
	Revealed   bool
	SelfAssess string
	Duration   int
	Offline    bool
	AnsweredAt *time.Time
}

// AttemptResult 是作答结果。
type AttemptResult struct {
	AttemptID uint64
	IsCorrect *bool
	Answer    string
	Analysis  string
	Effect
}

// offlineWindow 是离线作答最多补交多早的记录：当天题目预下载，补交超过 7 天的按收到时间算。
const offlineWindow = 7 * 24 * time.Hour

// Submit 记录一次作答并更新学习状态（PRD 11.1–11.3、11.8）。客观题由服务端判分；离线作答联网后补交，服务端复核并以服务端结果为准。
// 同一个幂等键重复提交返回第一次的结果，不重复更新。
func (s *Service) Submit(ctx context.Context, userID, sessionID uint64, in AttemptInput) (AttemptResult, error) {
	if len(in.Key) < 8 {
		return AttemptResult{}, apperr.New(apperr.BadRequest, "缺少幂等键")
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return AttemptResult{}, err
	}
	now := s.now()
	at := now
	if in.Offline && in.AnsweredAt != nil && !in.AnsweredAt.After(now) && now.Sub(*in.AnsweredAt) <= offlineWindow {
		at = *in.AnsweredAt
	}
	var res AttemptResult
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		if prev, err := q.GetAttemptByKey(ctx, dbq.GetAttemptByKeyParams{OwnerUserID: userID, IdempotencyKey: sql.NullString{String: in.Key, Valid: true}}); err == nil {
			res = AttemptResult{AttemptID: prev.ID, Effect: Effect{WrongBook: "none"}}
			if prev.IsCorrect.Valid {
				v := prev.IsCorrect.Bool
				res.IsCorrect = &v
			}
			qr, err := q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: prev.QuestionID, OwnerUserID: owner(userID)})
			if err == nil {
				res.Answer, res.Analysis = qr.Answer.String, qr.Analysis.String
			}
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, ids, _, err := s.session(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		idx := -1
		for i, id := range ids {
			if id == in.QuestionID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return apperr.NotFoundErr()
		}
		qr, err := q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: in.QuestionID, OwnerUserID: owner(userID)})
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		var opts []Option
		if qr.Options != nil {
			_ = json.Unmarshal(qr.Options, &opts)
		}
		objective := rules.IsObjective(rules.QType(qr.Qtype))
		o := Outcome{Revealed: in.Revealed}
		ins := dbq.InsertAttemptParams{OwnerUserID: userID, QuestionID: qr.ID, PracticeSessionID: sql.NullInt64{Int64: int64(sessionID), Valid: true},
			RevealedAnswer: in.Revealed, DurationSeconds: uint32(max(in.Duration, 0)), Offline: in.Offline, IdempotencyKey: sql.NullString{String: in.Key, Valid: true},
			AnsweredAt: at.UTC(), AnswerMode: dbq.AttemptsAnswerModeSelfAssess}
		if in.Offline {
			ins.VerifiedAt = sql.NullTime{Time: now.UTC(), Valid: true}
		}
		if in.SelfAssess != "" {
			ins.SelfAssess = dbq.NullAttemptsSelfAssess{AttemptsSelfAssess: dbq.AttemptsSelfAssess(in.SelfAssess), Valid: true}
		}
		if objective && !in.Revealed {
			correct, ok := Judge(string(qr.Qtype), opts, qr.Answer.String, in.Selected, in.AnswerText)
			if !ok {
				return apperr.New(apperr.BadRequest, "这道题还没有答案，先在题库里补上答案")
			}
			o.Objective, o.Correct = true, correct
			ins.IsCorrect = sql.NullBool{Bool: correct, Valid: true}
			res.IsCorrect = &correct
			ins.AnswerMode = dbq.AttemptsAnswerModeChoice
			if qr.Qtype == dbq.QuestionsQtypeFillBlank {
				ins.AnswerMode = dbq.AttemptsAnswerModeTyped
			}
		} else if objective {
			o.Objective = true
		}
		if len(in.Selected) > 0 {
			ins.Selected, _ = json.Marshal(in.Selected)
		}
		if in.AnswerText != "" {
			ins.AnswerText = sql.NullString{String: in.AnswerText, Valid: true}
		}
		aid, err := q.InsertAttempt(ctx, ins)
		if err != nil {
			return err
		}
		res.AttemptID, res.Answer, res.Analysis = uint64(aid), qr.Answer.String, qr.Analysis.String
		if res.Effect, err = Apply(ctx, q, p, userID, qr.ID, o, rules.DayOf(at)); err != nil {
			return err
		}
		if next := uint32(idx + 1); next > row.CursorIndex {
			return q.SetSessionCursor(ctx, dbq.SetSessionCursorParams{CursorIndex: next, ID: sessionID, OwnerUserID: userID})
		}
		return nil
	})
	return res, err
}

// Summary 是本组训练总结（4.11）。
type Summary struct {
	SessionID     uint64      `json:"session_id"`
	Title         string      `json:"title"`
	QuestionCount int         `json:"question_count"`
	Answered      int         `json:"answered"`
	CorrectRate   float64     `json:"correct_rate"`
	Minutes       float64     `json:"minutes"`
	ByQType       []QTypeStat `json:"by_qtype"`
	FromBank      int         `json:"from_bank"`
	FromAI        int         `json:"from_ai"`
	ReviewKPs     []ReviewKP  `json:"review_kps"`
	Comment       string      `json:"comment"`
}

type QTypeStat struct {
	QType   string `json:"qtype"`
	Total   int    `json:"total"`
	Correct int    `json:"correct"`
}

type ReviewKP struct {
	KPID   uint64 `json:"kp_id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Finish 结束本组并生成总结；已结束的返回保存的总结。总结只描述数据，不调模型。
func (s *Service) Finish(ctx context.Context, userID, id uint64) (Summary, error) {
	row, ids, _, err := s.session(ctx, s.q, userID, id, false)
	if err != nil {
		return Summary{}, err
	}
	if row.Status == dbq.PracticeSessionsStatusFinished && row.Summary != nil {
		var sum Summary
		if json.Unmarshal(row.Summary, &sum) == nil {
			return sum, nil
		}
	}
	atts, err := s.q.ListSessionAttempts(ctx, dbq.ListSessionAttemptsParams{PracticeSessionID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: userID})
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{SessionID: id, Title: row.Title, QuestionCount: len(ids), ByQType: []QTypeStat{}, ReviewKPs: []ReviewKP{}}
	last := map[uint64]dbq.ListSessionAttemptsRow{}
	var order []uint64
	for _, a := range atts {
		if _, ok := last[a.QuestionID]; !ok {
			order = append(order, a.QuestionID)
		}
		last[a.QuestionID] = a
		sum.Minutes += float64(a.DurationSeconds) / 60
	}
	stats := map[string]*QTypeStat{}
	var qtOrder []string
	judged, correct := 0, 0
	type miss struct {
		wrong, revealed int
	}
	misses := map[uint64]*miss{}
	names := map[uint64]string{}
	var missOrder []uint64
	for _, qid := range order {
		a := last[qid]
		sum.Answered++
		if a.Source == dbq.QuestionsSourceAiGenerated {
			sum.FromAI++
		} else {
			sum.FromBank++
		}
		st, ok := stats[string(a.Qtype)]
		if !ok {
			st = &QTypeStat{QType: string(a.Qtype)}
			stats[string(a.Qtype)] = st
			qtOrder = append(qtOrder, string(a.Qtype))
		}
		st.Total++
		ok = a.IsCorrect.Valid && a.IsCorrect.Bool && !a.RevealedAnswer
		if a.IsCorrect.Valid || a.RevealedAnswer {
			judged++
			if ok {
				correct++
				st.Correct++
			}
		}
		if ok || (!a.IsCorrect.Valid && !a.RevealedAnswer) {
			continue
		}
		kps, err := s.q.ListQuestionKPs(ctx, dbq.ListQuestionKPsParams{QuestionID: qid, OwnerUserID: owner(userID)})
		if err != nil {
			return Summary{}, err
		}
		if len(kps) == 0 {
			continue
		}
		k := kps[0]
		m, ok := misses[k.ID]
		if !ok {
			m = &miss{}
			misses[k.ID] = m
			names[k.ID] = k.Name
			missOrder = append(missOrder, k.ID)
		}
		if a.RevealedAnswer {
			m.revealed++
		} else {
			m.wrong++
		}
	}
	for _, t := range qtOrder {
		sum.ByQType = append(sum.ByQType, *stats[t])
	}
	if judged > 0 {
		sum.CorrectRate = float64(correct) / float64(judged)
	}
	for _, kp := range missOrder {
		if len(sum.ReviewKPs) >= 3 {
			break
		}
		m := misses[kp]
		var parts []string
		if m.wrong > 0 {
			parts = append(parts, "答错 "+strconv.Itoa(m.wrong)+" 题")
		}
		if m.revealed > 0 {
			parts = append(parts, "看了 "+strconv.Itoa(m.revealed)+" 次答案")
		}
		sum.ReviewKPs = append(sum.ReviewKPs, ReviewKP{KPID: kp, Name: names[kp], Reason: strings.Join(parts, "，")})
	}
	sum.Comment = comment(sum, judged)
	raw, _ := json.Marshal(sum)
	if err := s.q.FinishPracticeSession(ctx, dbq.FinishPracticeSessionParams{Summary: raw, FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id, OwnerUserID: userID}); err != nil {
		return Summary{}, err
	}
	return sum, nil
}

// comment 是一句只描述数据的总结（PRD 4.11），不做评价、不承诺分数。
func comment(s Summary, judged int) string {
	b := "本组完成 " + strconv.Itoa(s.Answered) + " / " + strconv.Itoa(s.QuestionCount) + " 题，用时约 " + strconv.Itoa(int(s.Minutes+0.5)) + " 分钟"
	if judged > 0 {
		b += "，判分的题正确率 " + strconv.Itoa(int(s.CorrectRate*100+0.5)) + "%"
	}
	if len(s.ReviewKPs) > 0 {
		b += "；" + s.ReviewKPs[0].Name + " 等 " + strconv.Itoa(len(s.ReviewKPs)) + " 个知识点需要再看"
	}
	return b + "。"
}
