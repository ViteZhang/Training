package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
)

// 主观题批改（PRD 4.4–4.9、11.7、11.14、12.2；T18）。

// defaultScore 是题目没有分值、采分点也没有分值时的满分（按题型）。
var defaultScore = map[string]float64{"term": 10, "short_answer": 20, "discussion": 30, "essay": 50, "calculation": 10, "other": 10}

// grantHours 是异议授权后台查看的时长（PRD 10.1）。
const grantHours = 72

// rubricSnap 是批改时的采分点快照（gradings.rubric_snapshot），历史批改不随采分点修改变化（PRD 11.14）。
type rubricSnap struct {
	Version    int             `json:"version"`
	Source     string          `json:"source"`
	Reference  string          `json:"reference"`
	Points     []ai.GradePoint `json:"points"`
	FullScore  float64         `json:"full_score"`
	MaterialID uint64          `json:"material_id,omitempty"`
	FileName   string          `json:"file_name,omitempty"`
	Page       int             `json:"page,omitempty"`
}

// PointResult 是一个采分点的批改结果。
type PointResult struct {
	Seq     int     `json:"seq"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	Got     float64 `json:"got"`
	Verdict string  `json:"verdict"`
	Quote   string  `json:"quote,omitempty"`
	Reason  string  `json:"reason,omitempty"`
}

// LossItem 是一类失分（PRD 11.7）。
type LossItem struct {
	Type   string  `json:"type"`
	Points float64 `json:"points"`
	Reason string  `json:"reason"`
}

// gradingExtra 存在 gradings.suggestions：建议、结构判断，以及这次批改对学习状态的影响（4.7 重新打开时显示）。
type gradingExtra struct {
	Items         []string   `json:"items"`
	StructureOK   bool       `json:"structure_ok"`
	StructureNote string     `json:"structure_note,omitempty"`
	KPs           []KPChange `json:"kp_changes,omitempty"`
	WrongBook     string     `json:"wrong_book,omitempty"`
}

// Grading 是一次批改。
type Grading struct {
	ID, AttemptID, QuestionID uint64
	Status                    string
	Score                     *float64
	FullScore                 float64
	Points                    []PointResult
	Rubric                    rubricSnap
	Extra                     gradingExtra
	Loss                      []LossItem
	AnswerText                string
	Trigger                   string
	ParentID                  uint64
	Disputed                  bool
	QuotaCharged              bool
	RubricChanged             bool
}

// SubjectiveInput 是一次主观题作答。
type SubjectiveInput struct {
	QuestionID uint64
	Key        string
	Answer     string
	Mode       string
	Duration   int
	Timed      bool
	// PhotoKeys 是拍手写稿的照片（Mode=photo），Answer 是识别后用户核对过的文字。
	PhotoKeys []string
}

func parseScore(s sql.NullString) (float64, bool) {
	if !s.Valid {
		return 0, false
	}
	v, err := strconv.ParseFloat(s.String, 64)
	return v, err == nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// snapshot 读出题目当前的采分点作为批改依据。没有采分点时按参考答案整体批改（标「按参考答案」）。
func (s *Service) snapshot(ctx context.Context, q *dbq.Queries, userID uint64, qr dbq.GetQuestionFullRow) (rubricSnap, error) {
	rp, err := q.ListQuestionRubric(ctx, dbq.ListQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(qr.ID), Valid: true}, OwnerUserID: owner(userID)})
	if err != nil {
		return rubricSnap{}, err
	}
	snap := rubricSnap{Version: int(qr.RubricVersion), Reference: qr.Answer.String}
	full, hasFull := parseScore(qr.Score)
	sum, allScored := 0.0, len(rp) > 0
	for _, p := range rp {
		v, ok := parseScore(p.Score)
		if !ok {
			allScored = false
		}
		sum += v
		gp := ai.GradePoint{Seq: int(p.Seq), Content: p.Content, Score: v}
		if p.Keywords != nil {
			_ = json.Unmarshal(p.Keywords, &gp.Keywords)
		}
		snap.Points = append(snap.Points, gp)
		snap.Source = string(p.Origin)
	}
	if !hasFull {
		full = defaultScore[string(qr.Qtype)]
		if allScored && sum > 0 {
			full = sum
		}
	}
	snap.FullScore = full
	switch {
	case len(snap.Points) == 0:
		if strings.TrimSpace(snap.Reference) == "" {
			return snap, apperr.New(apperr.BadRequest, "这道题还没有参考答案和采分点，先在题库里补上再批改")
		}
		snap.Source = "reference_answer"
		snap.Points = []ai.GradePoint{{Seq: 1, Content: snap.Reference, Score: full}}
	case !allScored || math.Abs(sum-full) > 0.01:
		// 采分点没有分值或合计与满分不符时按满分平均分配（导入时缺分值的题）。
		each := round1(full / float64(len(snap.Points)))
		for i := range snap.Points {
			snap.Points[i].Score = each
		}
		snap.Points[len(snap.Points)-1].Score = round1(full - each*float64(len(snap.Points)-1))
	}
	if qr.SourceMaterialID.Valid {
		if m, err := q.GetMaterial(ctx, dbq.GetMaterialParams{ID: uint64(qr.SourceMaterialID.Int64), OwnerUserID: userID}); err == nil {
			snap.MaterialID, snap.FileName, snap.Page = m.ID, m.FileName, int(qr.SourcePage.Int32)
		}
	}
	return snap, nil
}

// grade 调模型批改（不在事务里，目标 8 秒内）。
func (s *Service) grade(ctx context.Context, userID uint64, subject string, qr dbq.GetQuestionFullRow, snap rubricSnap, answer string) (ai.GradeOut, ai.Meta, error) {
	return ai.Grade.Run(ctx, s.ai, userID, ai.GradeIn{Subject: subject, QType: string(qr.Qtype), Stem: qr.Stem, Reference: snap.Reference, Points: snap.Points, Answer: answer})
}

// lossTiming 是判定「时间不够」用的作答情况：只在限时作答与模拟考试中判定（PRD 11.7）。
type lossTiming struct {
	Timed, Unanswered bool
	// Position 是题目在试卷中的相对位置 0–1；单题限时作答为 0。
	Position         float64
	SpentSeconds     float64
	SuggestedSeconds float64
}

// diagnose 按失分分值归入三类（PRD 11.7）。kpM 是批改前主知识点的掌握分。返回各类失分与主要失分类型。
func diagnose(snap rubricSnap, out ai.GradeOut, kpM float64, kpName string, tm lossTiming, p rules.Params) ([]LossItem, string) {
	var inputs []rules.LossInput
	missed := 0
	for i, r := range out.Points {
		lost := snap.Points[i].Score - r.Score
		if r.Verdict != "hit" {
			missed++
		}
		inputs = append(inputs, rules.LossInput{LostScore: lost, MissedOrPartial: r.Verdict != "hit", StructureLacking: !out.StructureOK,
			KPM: kpM, Timed: tm.Timed, Unanswered: tm.Unanswered, Position: tm.Position, TimeSpentSeconds: tm.SpentSeconds, SuggestedSeconds: tm.SuggestedSeconds})
	}
	agg := rules.AggregateLoss(inputs, p.LossDiagnosis)
	reasons := map[rules.LossType]string{
		rules.LossKnowledge: strconv.Itoa(missed) + " 个采分点没写到或没写全，知识点还没掌握",
		rules.LossNorm:      "知道但没写到位",
		rules.LossTime:      "限时作答没写完",
	}
	if kpName != "" {
		reasons[rules.LossKnowledge] = strconv.Itoa(missed) + " 个采分点没写到或没写全，「" + kpName + "」还没掌握"
	}
	if out.StructureNote != "" {
		reasons[rules.LossNorm] = "知道但没写到位：" + out.StructureNote
	}
	var items []LossItem
	main, best := "", 0.0
	for _, t := range []rules.LossType{rules.LossKnowledge, rules.LossNorm, rules.LossTime} {
		if v := agg[t]; v > 0 {
			items = append(items, LossItem{Type: string(t), Points: round1(v), Reason: reasons[t]})
			if v > best {
				main, best = string(t), v
			}
		}
	}
	return items, main
}

func lossJSON(items []LossItem) dbtypes.NullJSON {
	m := map[string]LossItem{}
	for _, it := range items {
		m[it.Type] = it
	}
	b, _ := json.Marshal(m)
	return b
}

func points(snap rubricSnap, out ai.GradeOut) []PointResult {
	res := make([]PointResult, len(out.Points))
	for i, r := range out.Points {
		res[i] = PointResult{Seq: r.Seq, Content: snap.Points[i].Content, Score: snap.Points[i].Score, Got: r.Score, Verdict: r.Verdict, Quote: r.Quote, Reason: r.Reason}
	}
	return res
}

// primaryKP 返回题目主知识点批改前的掌握分与名称。
func (s *Service) primaryKP(ctx context.Context, q *dbq.Queries, userID, questionID uint64) (float64, string, error) {
	kps, err := q.ListQuestionKPs(ctx, dbq.ListQuestionKPsParams{QuestionID: questionID, OwnerUserID: owner(userID)})
	if err != nil || len(kps) == 0 {
		return 0, "", err
	}
	row, err := q.GetKPMastery(ctx, dbq.GetKPMasteryParams{OwnerUserID: userID, KpID: kps[0].ID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, kps[0].Name, nil
	}
	m, _ := strconv.ParseFloat(row.M, 64)
	return m, kps[0].Name, err
}

func (s *Service) subjectName(ctx context.Context, userID uint64, qr dbq.GetQuestionFullRow) string {
	if !qr.SubjectID.Valid {
		return ""
	}
	b, err := s.subject(ctx, userID, uint64(qr.SubjectID.Int64))
	if err != nil {
		return ""
	}
	return b.Name
}

func fmtScore(v float64) sql.NullString {
	return sql.NullString{String: strconv.FormatFloat(v, 'f', 2, 64), Valid: true}
}

// SubmitSubjective 提交主观题并批改。次数用完时答案存为待批改；批改失败返回 AI_FAILED，不扣次数、不保存。
func (s *Service) SubmitSubjective(ctx context.Context, userID, sessionID uint64, in SubjectiveInput) (Grading, error) {
	if len(in.Key) < 8 {
		return Grading{}, apperr.New(apperr.BadRequest, "缺少幂等键")
	}
	if prev, err := s.q.GetGradingByKey(ctx, dbq.GetGradingByKeyParams{OwnerUserID: userID, IdempotencyKey: sql.NullString{String: in.Key, Valid: true}}); err == nil {
		return s.Grading(ctx, userID, prev.ID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Grading{}, err
	}
	answer := strings.TrimSpace(in.Answer)
	if answer == "" {
		return Grading{}, apperr.New(apperr.BadRequest, "先写点内容再提交")
	}
	row, ids, _, err := s.session(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return Grading{}, err
	}
	idx := indexOf(ids, in.QuestionID)
	if idx < 0 {
		return Grading{}, apperr.NotFoundErr()
	}
	qr, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: in.QuestionID, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return Grading{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Grading{}, err
	}
	if rules.IsObjective(rules.QType(qr.Qtype)) {
		return Grading{}, apperr.New(apperr.BadRequest, "客观题直接提交选项")
	}
	snap, err := s.snapshot(ctx, s.q, userID, qr)
	if err != nil {
		return Grading{}, err
	}
	mode := dbq.AttemptsAnswerModeTyped
	if in.Mode == "voice" || in.Mode == "photo" {
		mode = dbq.AttemptsAnswerMode(in.Mode)
	}
	if len(in.PhotoKeys) > 0 {
		if err := ownPhotos(userID, in.PhotoKeys); err != nil {
			return Grading{}, err
		}
	}
	now := s.now().UTC()
	attempt := dbq.InsertAttemptParams{OwnerUserID: userID, QuestionID: qr.ID, PracticeSessionID: sql.NullInt64{Int64: int64(sessionID), Valid: true},
		AnswerMode: mode, AnswerText: sql.NullString{String: answer, Valid: true}, FullScore: fmtScore(snap.FullScore),
		DurationSeconds: uint32(max(in.Duration, 0)), AnsweredAt: now}
	snapJSON, _ := json.Marshal(snap)
	advance := func(q *dbq.Queries) error {
		if next := uint32(idx + 1); next > row.CursorIndex {
			return q.SetSessionCursor(ctx, dbq.SetSessionCursorParams{CursorIndex: next, ID: sessionID, OwnerUserID: userID})
		}
		return nil
	}
	queue := func() (Grading, error) {
		var gid int64
		err := s.withTx(ctx, func(q *dbq.Queries) error {
			aid, err := q.InsertAttempt(ctx, attempt)
			if err != nil {
				return err
			}
			if err := s.annotate(ctx, q, userID, uint64(aid), in); err != nil {
				return err
			}
			gid, err = q.InsertGrading(ctx, dbq.InsertGradingParams{OwnerUserID: userID, AttemptID: uint64(aid), TriggerReason: dbq.GradingsTriggerReasonSubmit,
				Status: dbq.GradingsStatusQueuedQuota, RubricVersion: sql.NullInt32{Int32: int32(snap.Version), Valid: true}, RubricSnapshot: snapJSON,
				FullScore: fmtScore(snap.FullScore), IdempotencyKey: sql.NullString{String: in.Key, Valid: true}})
			if err != nil {
				return err
			}
			return advance(q)
		})
		if err != nil {
			return Grading{}, err
		}
		return s.Grading(ctx, userID, uint64(gid))
	}
	// 今日次数用完：答案保存为待批改（4.9），不调模型。
	if left, err := s.quota.Remaining(ctx, userID, quota.Grading); err != nil {
		return Grading{}, err
	} else if left != nil && *left <= 0 {
		return queue()
	}
	kpM, kpName, err := s.primaryKP(ctx, s.q, userID, qr.ID)
	if err != nil {
		return Grading{}, err
	}
	out, meta, err := s.grade(ctx, userID, s.subjectName(ctx, userID, qr), qr, snap, answer)
	if err != nil {
		return Grading{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Grading{}, err
	}
	loss, mainLoss := diagnose(snap, out, kpM, kpName, lossTiming{Timed: in.Timed}, p)
	total := out.Total()
	var gid int64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		attempt.Score = fmtScore(total)
		aid, err := q.InsertAttempt(ctx, attempt)
		if err != nil {
			return err
		}
		if err := s.annotate(ctx, q, userID, uint64(aid), in); err != nil {
			return err
		}
		eff, err := Apply(ctx, q, p, userID, qr.ID, Outcome{Graded: true, ScoreRate: total / snap.FullScore, LossType: mainLoss}, rules.DayOf(now))
		if err != nil {
			return err
		}
		extra := gradingExtra{Items: out.Suggestions, StructureOK: out.StructureOK, StructureNote: out.StructureNote, KPs: eff.KPs, WrongBook: eff.WrongBook}
		gid, err = s.insertDone(ctx, q, userID, uint64(aid), dbq.GradingsTriggerReasonSubmit, 0, snap, snapJSON, out, loss, extra, meta, true, in.Key)
		if err != nil {
			return err
		}
		if err := s.quota.Consume(ctx, q, quota.Charge{UserID: userID, Type: quota.Grading, Amount: 1, Ref: quota.Ref{Type: "grading", ID: uint64(gid)},
			Key: quota.KeyFor("grading", uint64(gid))}); err != nil {
			return err
		}
		return advance(q)
	})
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Kind == apperr.QuotaExceeded {
		// 并发提交把次数用完了：答案存为待批改。
		return queue()
	}
	if err != nil {
		return Grading{}, err
	}
	return s.Grading(ctx, userID, uint64(gid))
}

// annotate 记下限时作答与手写稿照片。
func (s *Service) annotate(ctx context.Context, q *dbq.Queries, userID, attemptID uint64, in SubjectiveInput) error {
	if in.Timed {
		if err := q.SetAttemptTimed(ctx, dbq.SetAttemptTimedParams{ID: attemptID, OwnerUserID: userID}); err != nil {
			return err
		}
	}
	if len(in.PhotoKeys) == 0 {
		return nil
	}
	keys, _ := json.Marshal(in.PhotoKeys)
	return q.SetAttemptPhotos(ctx, dbq.SetAttemptPhotosParams{PhotoKeys: keys, ID: attemptID, OwnerUserID: userID})
}

func (s *Service) insertDone(ctx context.Context, q *dbq.Queries, userID, attemptID uint64, trigger dbq.GradingsTriggerReason, parent uint64, snap rubricSnap,
	snapJSON []byte, out ai.GradeOut, loss []LossItem, extra gradingExtra, meta ai.Meta, charged bool, key string) (int64, error) {
	pts, _ := json.Marshal(points(snap, out))
	ex, _ := json.Marshal(extra)
	p := dbq.InsertGradingParams{OwnerUserID: userID, AttemptID: attemptID, TriggerReason: trigger, Status: dbq.GradingsStatusDone,
		RubricVersion: sql.NullInt32{Int32: int32(snap.Version), Valid: true}, RubricSnapshot: snapJSON, Score: fmtScore(out.Total()), FullScore: fmtScore(snap.FullScore),
		PointResults: pts, Loss: lossJSON(loss), Suggestions: ex, Model: sql.NullString{String: meta.Model, Valid: meta.Model != ""},
		PromptVersion: sql.NullString{String: ai.Grade.Name + "@" + meta.Version, Valid: true}, QuotaCharged: charged,
		FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}}
	if parent != 0 {
		p.ParentGradingID = sql.NullInt64{Int64: int64(parent), Valid: true}
	}
	if key != "" {
		p.IdempotencyKey = sql.NullString{String: key, Valid: true}
	}
	return q.InsertGrading(ctx, p)
}

func indexOf(ids []uint64, id uint64) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

// Grading 读出一次批改（4.7）。
func (s *Service) Grading(ctx context.Context, userID, id uint64) (Grading, error) {
	g, err := s.q.GetGrading(ctx, dbq.GetGradingParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Grading{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Grading{}, err
	}
	out := Grading{ID: g.ID, AttemptID: g.AttemptID, QuestionID: g.QuestionID, Status: string(g.Status), AnswerText: g.AnswerText.String,
		Trigger: string(g.TriggerReason), QuotaCharged: g.QuotaCharged, Points: []PointResult{}, Loss: []LossItem{}}
	if g.ParentGradingID.Valid {
		out.ParentID = uint64(g.ParentGradingID.Int64)
	}
	if g.RubricSnapshot != nil {
		_ = json.Unmarshal(g.RubricSnapshot, &out.Rubric)
	}
	out.FullScore = out.Rubric.FullScore
	if v, ok := parseScore(g.Score); ok {
		out.Score = &v
	}
	if g.PointResults != nil {
		_ = json.Unmarshal(g.PointResults, &out.Points)
	}
	if g.Loss != nil {
		var m map[string]LossItem
		_ = json.Unmarshal(g.Loss, &m)
		for _, t := range []string{"knowledge", "norm", "time"} {
			if it, ok := m[t]; ok {
				out.Loss = append(out.Loss, it)
			}
		}
	}
	if g.Suggestions != nil {
		_ = json.Unmarshal(g.Suggestions, &out.Extra)
	}
	if out.Extra.Items == nil {
		out.Extra.Items = []string{}
	}
	ds, err := s.q.DisputedGradings(ctx, dbq.DisputedGradingsParams{OwnerUserID: userID, GradingID: id, RegradeID: sql.NullInt64{Int64: int64(id), Valid: true}})
	if err != nil {
		return Grading{}, err
	}
	out.Disputed = len(ds) > 0
	if qr, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: g.QuestionID, OwnerUserID: owner(userID)}); err == nil {
		out.RubricChanged = int(qr.RubricVersion) != out.Rubric.Version
	}
	return out, nil
}

// regrade 按题目当前的采分点重批一次（异议复核、改采分点后重批），不扣次数；以重批结果为准回算掌握度与错题（PRD 11.14）。
func (s *Service) regrade(ctx context.Context, userID uint64, old Grading, trigger dbq.GradingsTriggerReason, after func(q *dbq.Queries, newID int64) error) (Grading, error) {
	if old.Status != "done" || old.Score == nil {
		return Grading{}, apperr.New(apperr.Conflict, "这次作答还没有批改")
	}
	qr, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: old.QuestionID, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return Grading{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Grading{}, err
	}
	snap, err := s.snapshot(ctx, s.q, userID, qr)
	if err != nil {
		return Grading{}, err
	}
	kpM, kpName, err := s.primaryKP(ctx, s.q, userID, qr.ID)
	if err != nil {
		return Grading{}, err
	}
	out, meta, err := s.grade(ctx, userID, s.subjectName(ctx, userID, qr), qr, snap, old.AnswerText)
	if err != nil {
		return Grading{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Grading{}, err
	}
	g, err := s.q.GetGrading(ctx, dbq.GetGradingParams{ID: old.ID, OwnerUserID: userID})
	if err != nil {
		return Grading{}, err
	}
	loss, _ := diagnose(snap, out, kpM, kpName, lossTiming{Timed: g.Timed}, p)
	snapJSON, _ := json.Marshal(snap)
	oldRate := *old.Score / old.FullScore
	newRate := out.Total() / snap.FullScore
	var gid int64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		kps, err := Correct(ctx, q, p, userID, qr.ID, oldRate, newRate, s.today())
		if err != nil {
			return err
		}
		wb := "none"
		if newRate >= 1 {
			if err := q.DropPartialWrongBook(ctx, dbq.DropPartialWrongBookParams{RemovedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, OwnerUserID: userID, QuestionID: qr.ID}); err != nil {
				return err
			}
		} else if err := q.CorrectWrongBookRate(ctx, dbq.CorrectWrongBookRateParams{LastScoreRate: sql.NullString{String: strconv.FormatFloat(newRate, 'f', 4, 64), Valid: true},
			OwnerUserID: userID, QuestionID: qr.ID}); err != nil {
			return err
		}
		extra := gradingExtra{Items: out.Suggestions, StructureOK: out.StructureOK, StructureNote: out.StructureNote, KPs: kps, WrongBook: wb}
		gid, err = s.insertDone(ctx, q, userID, old.AttemptID, trigger, old.ID, snap, snapJSON, out, loss, extra, meta, false, "")
		if err != nil {
			return err
		}
		if err := q.SetAttemptScore(ctx, dbq.SetAttemptScoreParams{Score: fmtScore(out.Total()), FullScore: fmtScore(snap.FullScore), ID: old.AttemptID, OwnerUserID: userID}); err != nil {
			return err
		}
		if after != nil {
			return after(q, gid)
		}
		return nil
	})
	if err != nil {
		return Grading{}, err
	}
	return s.Grading(ctx, userID, uint64(gid))
}

// RegradeAfterRubricChange 是 4.8「采分点本身不对」改完采分点后的重批。采分点没改过返回 409。
func (s *Service) RegradeAfterRubricChange(ctx context.Context, userID, gradingID uint64) (Grading, error) {
	old, err := s.Grading(ctx, userID, gradingID)
	if err != nil {
		return Grading{}, err
	}
	if !old.RubricChanged {
		return Grading{}, apperr.New(apperr.Conflict, "采分点没有改过，先去改采分点")
	}
	g, err := s.regrade(ctx, userID, old, dbq.GradingsTriggerReasonRubricChanged, nil)
	if err == nil && s.score != nil {
		// 改采分点重批后重算预估分（PRD 11.6）；失败只记日志。
		if err := s.score.RecomputeForQuestion(ctx, userID, old.QuestionID, "rubric_changed"); err != nil {
			logx.From(ctx).Warn("recompute estimate", "err", err, "grading_id", gradingID)
		}
	}
	return g, err
}

// DisputeInput 是批改异议（4.8）。
type DisputeInput struct {
	Reason      string
	Note        string
	AllowAccess bool
}

// Dispute 提交复核：重批一次，不扣次数；每次批改只能复核一次。勾选授权时写 content_access_grants（72 小时）。
func (s *Service) Dispute(ctx context.Context, userID, gradingID uint64, in DisputeInput) (Grading, error) {
	switch in.Reason {
	case "hit_missed", "rubric_wrong", "score_unfair", "other":
	default:
		return Grading{}, apperr.New(apperr.BadRequest, "请选择异议原因")
	}
	old, err := s.Grading(ctx, userID, gradingID)
	if err != nil {
		return Grading{}, err
	}
	if old.Disputed {
		return Grading{}, apperr.New(apperr.Conflict, "这次批改已经复核过了")
	}
	return s.regrade(ctx, userID, old, dbq.GradingsTriggerReasonDisputeRecheck, func(q *dbq.Queries, newID int64) error {
		g, err := q.GetGrading(ctx, dbq.GetGradingParams{ID: uint64(newID), OwnerUserID: userID})
		if err != nil {
			return err
		}
		did, err := q.InsertDispute(ctx, dbq.InsertDisputeParams{OwnerUserID: userID, GradingID: gradingID, Reason: dbq.DisputesReason(in.Reason),
			Note: sql.NullString{String: in.Note, Valid: in.Note != ""}, AllowAccess: in.AllowAccess, RegradeID: sql.NullInt64{Int64: newID, Valid: true},
			ScoreBefore: fmtScore(*old.Score), ScoreAfter: g.Score})
		if err != nil {
			return err
		}
		if !in.AllowAccess {
			return nil
		}
		scope, _ := json.Marshal([]map[string]any{{"type": "question", "id": old.QuestionID}, {"type": "attempt", "id": old.AttemptID},
			{"type": "grading", "id": gradingID}, {"type": "grading", "id": newID}})
		return q.InsertContentAccessGrant(ctx, dbq.InsertContentAccessGrantParams{UserID: userID, SourceID: uint64(did), Scope: scope,
			ExpiresAt: s.now().UTC().Add(grantHours * time.Hour)})
	})
}

// PendingItem 是一道待批改的题。
type PendingItem struct {
	GradingID, QuestionID uint64
	QType, Stem           string
	SavedAt               time.Time
}

// Pending 列出待批改（4.9「明天再批改」），附今天剩余次数（nil 为不限）。
func (s *Service) Pending(ctx context.Context, userID uint64) ([]PendingItem, *int, error) {
	rows, err := s.q.ListQueuedGradings(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]PendingItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, PendingItem{GradingID: r.ID, QuestionID: r.QuestionID, QType: string(r.Qtype), Stem: r.Stem, SavedAt: r.CreatedAt})
	}
	left, err := s.quota.Remaining(ctx, userID, quota.Grading)
	return out, left, err
}

// SubmitPending 一键提交待批改：按存入顺序批改，次数用完为止。单道批改失败跳过，留在待批改里。
func (s *Service) SubmitPending(ctx context.Context, userID uint64) ([]Grading, int, error) {
	rows, err := s.q.ListQueuedGradings(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return nil, 0, err
	}
	var done []Grading
	for _, r := range rows {
		if left, err := s.quota.Remaining(ctx, userID, quota.Grading); err != nil {
			return done, 0, err
		} else if left != nil && *left <= 0 {
			break
		}
		g, err := s.q.GetGrading(ctx, dbq.GetGradingParams{ID: r.ID, OwnerUserID: userID})
		if err != nil {
			return done, 0, err
		}
		qr, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: g.QuestionID, OwnerUserID: owner(userID)})
		if err != nil {
			continue
		}
		snap, err := s.snapshot(ctx, s.q, userID, qr)
		if err != nil {
			continue
		}
		kpM, kpName, err := s.primaryKP(ctx, s.q, userID, qr.ID)
		if err != nil {
			return done, 0, err
		}
		out, meta, err := s.grade(ctx, userID, s.subjectName(ctx, userID, qr), qr, snap, g.AnswerText.String)
		if err != nil {
			continue
		}
		loss, mainLoss := diagnose(snap, out, kpM, kpName, lossTiming{Timed: g.Timed}, p)
		snapJSON, _ := json.Marshal(snap)
		err = s.withTx(ctx, func(q *dbq.Queries) error {
			eff, err := Apply(ctx, q, p, userID, qr.ID, Outcome{Graded: true, ScoreRate: out.Total() / snap.FullScore, LossType: mainLoss}, s.today())
			if err != nil {
				return err
			}
			ex, _ := json.Marshal(gradingExtra{Items: out.Suggestions, StructureOK: out.StructureOK, StructureNote: out.StructureNote, KPs: eff.KPs, WrongBook: eff.WrongBook})
			pts, _ := json.Marshal(points(snap, out))
			if err := q.CompleteGrading(ctx, dbq.CompleteGradingParams{RubricVersion: sql.NullInt32{Int32: int32(snap.Version), Valid: true}, RubricSnapshot: snapJSON,
				Score: fmtScore(out.Total()), FullScore: fmtScore(snap.FullScore), PointResults: pts, Loss: lossJSON(loss), Suggestions: ex,
				Model: sql.NullString{String: meta.Model, Valid: meta.Model != ""}, PromptVersion: sql.NullString{String: ai.Grade.Name + "@" + meta.Version, Valid: true},
				FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: r.ID, OwnerUserID: userID}); err != nil {
				return err
			}
			if err := q.SetAttemptScore(ctx, dbq.SetAttemptScoreParams{Score: fmtScore(out.Total()), FullScore: fmtScore(snap.FullScore), ID: g.AttemptID, OwnerUserID: userID}); err != nil {
				return err
			}
			return s.quota.Consume(ctx, q, quota.Charge{UserID: userID, Type: quota.Grading, Amount: 1, Ref: quota.Ref{Type: "grading", ID: r.ID}, Key: quota.KeyFor("grading", r.ID)})
		})
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Kind == apperr.QuotaExceeded {
			break
		}
		if err != nil {
			return done, 0, err
		}
		gr, err := s.Grading(ctx, userID, r.ID)
		if err != nil {
			return done, 0, err
		}
		done = append(done, gr)
	}
	left, err := s.q.ListQueuedGradings(ctx, userID)
	return done, len(left), err
}
