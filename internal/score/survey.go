package score

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// surveyRewardDays 是考后回访赠送的会员天数（PRD 13.4），每人一次。
const surveyRewardDays = 30

// SurveySubject 是回访页的一门课：考前预估（没有时为空，显示「考前没有预估分」）与已填的实际成绩。
type SurveySubject struct {
	SubjectID uint64
	Name      string
	FullScore int
	Low, High *int
	Actual    *float64
}

// Survey 是考后回访（6.14）。
type Survey struct {
	Open         bool // 初试开始后开放
	ExamYear     int
	Submitted    bool
	RewardDays   int
	Subjects     []SurveySubject
	Retest       string
	Admission    string
	ShareConsent bool
}

// surveyScore 是 survey_responses.scores 里的一门课：实际成绩与提交时的考前预估（用于校准预估分参数）。
type surveyScore struct {
	SubjectID uint64   `json:"subject_id"`
	Name      string   `json:"name"`
	FullScore int      `json:"full_score"`
	Actual    float64  `json:"actual"`
	EstLow    *int     `json:"est_low,omitempty"`
	EstHigh   *int     `json:"est_high,omitempty"`
	EstMid    *float64 `json:"est_mid,omitempty"`
}

// examStart 是用户备考年份的初试第一天 0 点（北京时间）；没有配置时返回 false。
func (s *Service) examStart(ctx context.Context, userID uint64) (time.Time, int, bool, error) {
	prof, err := s.q.GetStudyProfile(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, 0, false, nil
	}
	if err != nil {
		return time.Time{}, 0, false, err
	}
	d, err := s.q.GetExamDate(ctx, prof.ExamYear)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, int(prof.ExamYear), false, nil
	}
	if err != nil {
		return time.Time{}, 0, false, err
	}
	day := rules.DayFromDate(d.FirstExamStart.Year(), d.FirstExamStart.Month(), d.FirstExamStart.Day())
	return day.Time(), int(prof.ExamYear), true, nil
}

// Survey 返回考后回访页：每门课的考前预估与已填内容。
func (s *Service) Survey(ctx context.Context, userID uint64) (Survey, error) {
	start, year, ok, err := s.examStart(ctx, userID)
	if err != nil {
		return Survey{}, err
	}
	out := Survey{Open: ok && !s.now().Before(start), ExamYear: year, RewardDays: surveyRewardDays, Subjects: []SurveySubject{}}
	cards, err := s.Cards(ctx, userID)
	if err != nil {
		return Survey{}, err
	}
	actual := map[uint64]float64{}
	if r, err := s.q.GetSurveyResponse(ctx, userID); err == nil {
		out.Submitted, out.Retest, out.Admission, out.ShareConsent = true, string(r.RetestResult), string(r.Admission.SurveyResponsesAdmission), r.ShareConsent
		var scores []surveyScore
		_ = json.Unmarshal(r.Scores, &scores)
		for _, sc := range scores {
			actual[sc.SubjectID] = sc.Actual
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Survey{}, err
	}
	for _, c := range cards {
		ss := SurveySubject{SubjectID: c.SubjectID, Name: c.Name, FullScore: c.FullScore}
		if c.Ready {
			lo, hi := c.Low, c.High
			ss.Low, ss.High = &lo, &hi
		}
		if v, ok := actual[c.SubjectID]; ok {
			ss.Actual = &v
		}
		out.Subjects = append(out.Subjects, ss)
	}
	return out, nil
}

// SurveyInput 是提交的回访。
type SurveyInput struct {
	Scores       map[uint64]float64
	Retest       string // in / out / unknown
	Admission    string // admitted / adjusted / rejected / pending，可空
	ShareConsent bool
}

// SubmitSurvey 提交考后回访：第一次提交送 30 天会员（每人一次）；之后再提交只更新复试、录取结果（可稍后补）。
// 每门课的实际成绩与当时的考前预估一起保存，用于校准预估分参数（PRD 11.6）。
func (s *Service) SubmitSurvey(ctx context.Context, userID uint64, in SurveyInput) (Survey, error) {
	switch in.Retest {
	case "in", "out", "unknown":
	default:
		return Survey{}, apperr.New(apperr.BadRequest, "请选择复试结果")
	}
	switch in.Admission {
	case "", "admitted", "adjusted", "rejected", "pending":
	default:
		return Survey{}, apperr.New(apperr.BadRequest, "录取结果不对")
	}
	cur, err := s.Survey(ctx, userID)
	if err != nil {
		return Survey{}, err
	}
	if !cur.Open {
		return Survey{}, apperr.New(apperr.Conflict, "初试开始后才能填写考后回访")
	}
	admission := dbq.NullSurveyResponsesAdmission{SurveyResponsesAdmission: dbq.SurveyResponsesAdmission(in.Admission), Valid: in.Admission != ""}
	if cur.Submitted {
		if err := s.q.UpdateSurveyResponse(ctx, dbq.UpdateSurveyResponseParams{RetestResult: dbq.SurveyResponsesRetestResult(in.Retest), Admission: admission,
			ShareConsent: in.ShareConsent, OwnerUserID: userID}); err != nil {
			return Survey{}, err
		}
		return s.Survey(ctx, userID)
	}
	if len(in.Scores) == 0 {
		return Survey{}, apperr.New(apperr.BadRequest, "至少填一门专业课的实际成绩")
	}
	cards, err := s.Cards(ctx, userID)
	if err != nil {
		return Survey{}, err
	}
	byID := map[uint64]Card{}
	for _, c := range cards {
		byID[c.SubjectID] = c
	}
	var scores []surveyScore
	for id, v := range in.Scores {
		c, ok := byID[id]
		if !ok {
			return Survey{}, apperr.NotFoundErr()
		}
		if v < 0 || v > float64(c.FullScore) {
			return Survey{}, apperr.New(apperr.BadRequest, "「"+c.Name+"」的成绩要在 0 到满分之间")
		}
		sc := surveyScore{SubjectID: id, Name: c.Name, FullScore: c.FullScore, Actual: v}
		if c.Ready {
			lo, hi := c.Low, c.High
			mid := float64(lo+hi) / 2
			sc.EstLow, sc.EstHigh, sc.EstMid = &lo, &hi, &mid
		}
		scores = append(scores, sc)
	}
	raw, _ := json.Marshal(scores)
	now := s.now()
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.GetSurveyResponse(ctx, userID); err == nil {
			return apperr.New(apperr.Conflict, "已经提交过考后回访了")
		}
		g, err := membership.Apply(ctx, q, membership.Grant{UserID: userID, Tier: "gift", Days: surveyRewardDays, Source: "survey", Now: now})
		if err != nil {
			return err
		}
		if err := q.InsertSurveyResponse(ctx, dbq.InsertSurveyResponseParams{OwnerUserID: userID, ExamYear: uint16(cur.ExamYear), Scores: raw,
			RetestResult: dbq.SurveyResponsesRetestResult(in.Retest), Admission: admission, ShareConsent: in.ShareConsent,
			MembershipID: sql.NullInt64{Int64: int64(g.ID), Valid: true}}); err != nil {
			return err
		}
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeMembership, Title: "考后回访奖励已到账",
			Body: "感谢填写考后回访，30 天会员已叠加到你的会员时长里", DedupeKey: sql.NullString{String: "survey_reward", Valid: true}})
	})
	if err != nil {
		return Survey{}, err
	}
	return s.Survey(ctx, userID)
}
