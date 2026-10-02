package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/rules"
)

// Outcome 是一次作答的结果，供掌握分、复习排期与错题本更新（PRD 11.1–11.3、11.8）。
// 练习（T17）、批改（T18）、整卷（T21）都经这里更新学习状态。
type Outcome struct {
	Objective bool
	Correct   bool    // 客观题对错
	Graded    bool    // 主观题有批改得分
	ScoreRate float64 // 主观题得分率 0–1
	Revealed  bool    // 点了「看答案 / 看参考答案」
	LossType  string  // 主观题主要失分类型（T18）
}

// KPChange 是一个知识点掌握状态的变化。
type KPChange struct {
	KPID     uint64
	Name     string
	From, To rules.MasteryState
	M        float64
}

// Effect 是一次作答对学习状态的影响。
type Effect struct {
	KPs       []KPChange
	WrongBook string // added / still / removed / none
}

// verified 报告这次作答是否算作答验证：判了对错、批了分，或看了答案。主观题只自评不算。
func (o Outcome) verified() bool { return o.Objective || o.Graded || o.Revealed }

func (o Outcome) correct(p rules.Params) bool {
	if o.Revealed {
		return false
	}
	return rules.IsCorrect(o.Objective, o.Correct, o.ScoreRate, p.MasteryState.SubjectiveCorrectRate)
}

func (o Outcome) event() rules.MasteryEvent {
	switch {
	case o.Revealed:
		return rules.MasteryEvent{Kind: rules.EventRevealAnswer}
	case o.Objective && o.Correct:
		return rules.MasteryEvent{Kind: rules.EventObjectiveCorrect}
	case o.Objective:
		return rules.MasteryEvent{Kind: rules.EventObjectiveWrong}
	default:
		return rules.MasteryEvent{Kind: rules.EventSubjective, ScoreRate: o.ScoreRate}
	}
}

func (o Outcome) review(p rules.Params) rules.ReviewOutcome {
	switch {
	case o.Revealed:
		return rules.ReviewFail
	case o.Objective && o.Correct:
		return rules.ReviewPass
	case o.Objective:
		return rules.ReviewFail
	default:
		return rules.SubjectiveOutcome(o.ScoreRate, p.Review)
	}
}

func encodeDays(days []rules.Day) dbtypes.NullJSON {
	s := make([]string, len(days))
	for i, d := range days {
		s[i] = d.String()
	}
	b, _ := json.Marshal(s)
	return dbtypes.NullJSON(b)
}

func decodeDays(j dbtypes.NullJSON) []rules.Day {
	if j == nil {
		return nil
	}
	var s []string
	if json.Unmarshal(j, &s) != nil {
		return nil
	}
	var out []rules.Day
	for _, v := range s {
		if len(v) < 10 {
			continue
		}
		y, _ := strconv.Atoi(v[0:4])
		m, _ := strconv.Atoi(v[5:7])
		d, _ := strconv.Atoi(v[8:10])
		out = append(out, rules.DayFromDate(y, timeMonth(m), d))
	}
	return out
}

func dayOrZero(t sql.NullTime) rules.Day {
	if !t.Valid {
		return 0
	}
	return rules.DayFromDateColumn(t.Time)
}

func nullDay(d rules.Day) sql.NullTime { return sql.NullTime{Time: d.Date(), Valid: true} }

func fmtM(m float64) string { return strconv.FormatFloat(m, 'f', 2, 64) }

// Apply 在事务里按一次作答更新学习状态：题目关联的全部知识点（主知识点全额、其余减半）的掌握分、
// 状态、答对日期、复习日，以及错题本。day 是作答当天（离线作答取客户端时间）。
func Apply(ctx context.Context, q *dbq.Queries, p rules.Params, userID, questionID uint64, o Outcome, day rules.Day) (Effect, error) {
	var eff Effect
	if !o.verified() {
		eff.WrongBook = "none"
		return eff, nil
	}
	kps, err := q.ListQuestionKPs(ctx, dbq.ListQuestionKPsParams{QuestionID: questionID, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if err != nil {
		return eff, err
	}
	correct := o.correct(p)
	for _, k := range kps {
		row, err := q.GetKPMasteryForUpdate(ctx, dbq.GetKPMasteryForUpdateParams{OwnerUserID: userID, KpID: k.ID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return eff, err
		}
		exists := err == nil
		m, from := 0.0, rules.StateUnlearned
		if exists {
			m, _ = strconv.ParseFloat(row.M, 64)
			from = rules.MasteryState(row.State)
		}
		// 先补上逾期衰减（每天 −3，只算一次），再计本次事件。
		decayed := dayOrZero(row.DecayAppliedOn)
		m, decayed = rules.ApplyOverdue(m, dayOrZero(row.NextReviewOn), decayed, day, p.Mastery)
		ev := o.event()
		ev.Secondary = !k.IsPrimary
		m = rules.ApplyMastery(m, ev, true, p.Mastery)
		days := decodeDays(row.CorrectDates)
		if correct {
			days = append(days, day)
		}
		days = rules.TrimCorrectDays(days, day, p.MasteryState.MasteredWindowDays)
		next, step := rules.NextReview(o.review(p), int(row.IntervalStep), day, p.Review)
		to := rules.StateOf(rules.MasteryFacts{M: m, Answered: true, Viewed: row.Viewed, CorrectDays: days}, day, p.MasteryState)
		dec := sql.NullTime{}
		if decayed != 0 {
			dec = nullDay(decayed)
		}
		if err := q.UpsertKPMasteryAnswer(ctx, dbq.UpsertKPMasteryAnswerParams{OwnerUserID: userID, KpID: k.ID, M: fmtM(m), State: dbq.KpMasteryState(to),
			CorrectDates: encodeDays(days), NextReviewOn: nullDay(next), IntervalStep: uint8(step), DecayAppliedOn: dec}); err != nil {
			return eff, err
		}
		eff.KPs = append(eff.KPs, KPChange{KPID: k.ID, Name: k.Name, From: from, To: to, M: m})
	}
	eff.WrongBook, err = applyWrongBook(ctx, q, p, userID, questionID, o, correct, day)
	return eff, err
}

// applyWrongBook 收录或推进错题本（PRD 11.8）：答错、主观题得分率 < 100%、看答案收录；
// 收录后在 2 个不同日期连续答对自动移出（已消灭），答错一次清零。
func applyWrongBook(ctx context.Context, q *dbq.Queries, p rules.Params, userID, questionID uint64, o Outcome, correct bool, day rules.Day) (string, error) {
	row, err := q.GetWrongBookForUpdate(ctx, dbq.GetWrongBookForUpdateParams{OwnerUserID: userID, QuestionID: questionID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	active := err == nil && row.Status == dbq.WrongBookStatusActive
	now := day.Time()
	rate := sql.NullString{}
	if !o.Objective && o.Graded {
		rate = sql.NullString{String: strconv.FormatFloat(o.ScoreRate, 'f', 4, 64), Valid: true}
	}
	loss := dbq.NullWrongBookLastLossType{}
	if o.LossType != "" {
		loss = dbq.NullWrongBookLastLossType{WrongBookLastLossType: dbq.WrongBookLastLossType(o.LossType), Valid: true}
	}
	reason, add := rules.WrongBookAdd(o.Objective, o.Correct, o.ScoreRate, o.Revealed)
	if add {
		up := dbq.UpsertWrongBookParams{OwnerUserID: userID, QuestionID: questionID, Status: dbq.WrongBookStatusActive, AddedReason: dbq.WrongBookAddedReason(reason),
			LastLossType: loss, WrongCount: 1, LastScoreRate: rate, CorrectDates: encodeDays(nil), NextReviewOn: nullDay(day.AddDays(p.Review.ResetDays)), AddedAt: now}
		status := "added"
		if active {
			up.WrongCount = row.WrongCount + 1
			up.AddedAt = row.AddedAt
			if !loss.Valid {
				up.LastLossType = row.LastLossType
			}
			status = "still"
		}
		// 主观题得分率 ≥ 80% 但 < 100% 时也收录，同时算一次答对（PRD 11.8）。
		if active && correct {
			days, removed := rules.WrongBookProgress(decodeDays(row.CorrectDates), true, day, p.WrongBook)
			up.CorrectDates = encodeDays(days)
			if removed {
				up.Status, up.RemovedAt, status = dbq.WrongBookStatusEliminated, sql.NullTime{Time: now, Valid: true}, "removed"
			}
		}
		return status, q.UpsertWrongBook(ctx, up)
	}
	if !active {
		return "none", nil
	}
	days, removed := rules.WrongBookProgress(decodeDays(row.CorrectDates), correct, day, p.WrongBook)
	up := dbq.UpsertWrongBookParams{OwnerUserID: userID, QuestionID: questionID, Status: row.Status, AddedReason: row.AddedReason, LastLossType: row.LastLossType,
		WrongCount: row.WrongCount, LastScoreRate: rate, CorrectDates: encodeDays(days), NextReviewOn: nullDay(day.AddDays(p.Review.Steps[0])), AddedAt: row.AddedAt}
	if !rate.Valid {
		up.LastScoreRate = row.LastScoreRate
	}
	status := "still"
	if removed {
		up.Status, up.RemovedAt, status = dbq.WrongBookStatusEliminated, sql.NullTime{Time: now, Valid: true}, "removed"
	}
	return status, q.UpsertWrongBook(ctx, up)
}

func timeMonth(m int) time.Month { return time.Month(m) }

// Correct 按重批结果回算掌握分（PRD 11.14：以重批结果为准）：撤回原批改的主观题事件、改记新的得分率，
// 即 M 加上 25 ×（新得分率 − 旧得分率），非主知识点减半；新得分率达到答对线时补记答对日期。复习排期不变。
func Correct(ctx context.Context, q *dbq.Queries, p rules.Params, userID, questionID uint64, oldRate, newRate float64, day rules.Day) ([]KPChange, error) {
	kps, err := q.ListQuestionKPs(ctx, dbq.ListQuestionKPsParams{QuestionID: questionID, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if err != nil {
		return nil, err
	}
	var out []KPChange
	for _, k := range kps {
		row, err := q.GetKPMasteryForUpdate(ctx, dbq.GetKPMasteryForUpdateParams{OwnerUserID: userID, KpID: k.ID})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		m, _ := strconv.ParseFloat(row.M, 64)
		delta := p.Mastery.SubjectiveSlope * (newRate - oldRate)
		if !k.IsPrimary {
			delta *= p.Mastery.SecondaryKPFactor
		}
		m = math.Max(0, math.Min(100, m+delta))
		days := decodeDays(row.CorrectDates)
		th := p.MasteryState.SubjectiveCorrectRate
		if newRate >= th && oldRate < th {
			days = append(days, day)
		}
		days = rules.TrimCorrectDays(days, day, p.MasteryState.MasteredWindowDays)
		to := rules.StateOf(rules.MasteryFacts{M: m, Answered: true, Viewed: row.Viewed, CorrectDays: days}, day, p.MasteryState)
		if err := q.UpsertKPMasteryAnswer(ctx, dbq.UpsertKPMasteryAnswerParams{OwnerUserID: userID, KpID: k.ID, M: fmtM(m), State: dbq.KpMasteryState(to),
			CorrectDates: encodeDays(days), NextReviewOn: row.NextReviewOn, IntervalStep: row.IntervalStep, DecayAppliedOn: row.DecayAppliedOn}); err != nil {
			return nil, err
		}
		out = append(out, KPChange{KPID: k.ID, Name: k.Name, From: rules.MasteryState(row.State), To: to, M: m})
	}
	return out, nil
}
