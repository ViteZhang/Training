package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// 体验一道题、学科通用体验题与 AI 现场出题（PRD v3 增补，T45）。
//
// 体验不消耗任何免费额度（解析、批改、AI 出题）；体验一道题和通用体验题共用一个次数（rule_params.official_first.trial_limit，
// 默认 1），以批改成功为准，批改失败不计次。体验的批改结果标「仅供参考」，不计入预估分。
// 用户选择加入自建题库后题目、采分点和这次作答都保留；选择不保存时全部删除（次数照算）。

// 体验只支持这三种题型（1.9）。
var trialQTypes = []string{"term", "short_answer", "discussion"}

// trialDefaultScore 是识别不出分值时的默认分值。
var trialDefaultScore = map[string]float64{"term": 5, "short_answer": 15, "discussion": 30}

const (
	maxTrialStem   = 2000
	maxTrialAnswer = 6000
)

// TrialStatus 是体验次数。
type TrialStatus struct {
	Limit, Used, Remaining int
}

func (s *Service) trialLimit(ctx context.Context) (int, error) {
	p, err := s.params.Rules(ctx)
	if err != nil {
		return 0, err
	}
	return p.OfficialFirst.TrialLimit, nil
}

// TrialStatus 返回体验次数（1.1c、1.9 右上角）。
func (s *Service) TrialStatus(ctx context.Context, userID uint64) (TrialStatus, error) {
	limit, err := s.trialLimit(ctx)
	if err != nil {
		return TrialStatus{}, err
	}
	used, err := s.q.GetTrialUsage(ctx, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TrialStatus{}, err
	}
	return TrialStatus{Limit: limit, Used: int(used), Remaining: max(limit-int(used), 0)}, nil
}

func trialUsedUp() error {
	return apperr.New(apperr.Conflict, "体验次数已用完，导入你自己的资料就能继续练").With("reason", "trial_used_up")
}

// TrialRecognized 是 1.9 识别出的题目。
type TrialRecognized struct {
	QType     string
	Score     float64
	Stem      string
	Answer    string
	Supported bool
}

// RecognizeTrial 从粘贴的文字（或拍照识别出的文字）认出一道题：题型、分值、题干，原文带了答案时一并给出。
// 不是名词解释、简答、论述时 Supported 为假，App 提示更换。
func (s *Service) RecognizeTrial(ctx context.Context, userID uint64, text string) (TrialRecognized, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return TrialRecognized{}, apperr.New(apperr.BadRequest, "先粘贴一道题")
	}
	if len([]rune(text)) > maxTrialStem+maxTrialAnswer {
		return TrialRecognized{}, apperr.New(apperr.BadRequest, "内容太长了，一次只放一道题")
	}
	// 先用导入引擎的规则切块：题型标题「二、名词解释（每题 5 分）」里的题型与分值由规则认出，比模型可靠。
	blocks := importer.Split([]importer.Page{{No: 1, Text: text}})
	if len(blocks) == 0 {
		blocks = []ai.Block{{ID: 1, Page: 1, Text: text}}
	}
	blocks = blocks[:min(len(blocks), 4)]
	byID := map[int]ai.Block{}
	for _, b := range blocks {
		byID[b.ID] = b
	}
	out, _, err := ai.Structure.Run(ctx, s.ai, userID, ai.StructureIn{Blocks: blocks})
	if err != nil {
		return TrialRecognized{}, err
	}
	var r TrialRecognized
	for _, it := range out.Items {
		if it.Kind != "question" || strings.TrimSpace(it.Stem) == "" {
			continue
		}
		b := byID[it.Block]
		r.Stem, r.QType, r.Answer = strings.TrimSpace(it.Stem), it.QType, strings.TrimSpace(it.Answer)
		if b.QType != "" {
			r.QType = b.QType
		}
		if it.Score != nil && *it.Score > 0 {
			r.Score = *it.Score
		}
		if b.Score > 0 {
			r.Score = b.Score
		}
		break
	}
	if r.Stem == "" {
		return TrialRecognized{}, apperr.New(apperr.BadRequest, "没认出题目，换一道试试").With("reason", "not_recognized")
	}
	r.Supported = slices.Contains(trialQTypes, r.QType)
	if r.Score == 0 {
		r.Score = trialDefaultScore[r.QType]
	}
	return r, nil
}

// TrialInput 是开始一次体验：自己的题（题干、题型、分值、参考答案选填），或一道通用体验题 / AI 出题。
type TrialInput struct {
	SampleQuestionID uint64
	InputMode        string // paste / photo
	QType            string
	Score            float64
	Stem             string
	Answer           string
}

// Trial 是一次体验。
type Trial struct {
	ID           uint64
	Kind         string
	QType        string
	Score        float64
	Stem         string
	AnswerSource string // user / ai
	PointCount   int
	Status       string
	// 以下批改后才有（提交前不给用户看采分点，1.9a）。
	Reference  string
	Points     []PointResult
	Total      *float64
	Extra      gradingExtra
	AnswerText string
	QuestionID uint64
	Remaining  int
}

type trialResult struct {
	Out ai.GradeOut `json:"out"`
}

func pointsJSONOf(ps []ai.RubricPoint) json.RawMessage {
	b, _ := json.Marshal(ps)
	return b
}

// CreateTrial 开始一次体验。没有参考答案时由 AI 生成（标「AI 参考答案」），再提采分点；不扣任何额度。
func (s *Service) CreateTrial(ctx context.Context, userID uint64, in TrialInput) (Trial, error) {
	st, err := s.TrialStatus(ctx, userID)
	if err != nil {
		return Trial{}, err
	}
	if st.Remaining <= 0 {
		return Trial{}, trialUsedUp()
	}
	p := dbq.InsertTrialParams{OwnerUserID: userID}
	if in.SampleQuestionID != 0 {
		sq, err := s.q.GetSampleQuestion(ctx, in.SampleQuestionID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && sq.Status != dbq.SampleQuestionsStatusOnline) {
			return Trial{}, apperr.NotFoundErr()
		}
		if err != nil {
			return Trial{}, err
		}
		p.Kind = dbq.TrialsKindSample
		flag := "sample_trial"
		if sq.Origin == dbq.SampleQuestionsOriginAiLive {
			p.Kind, flag = dbq.TrialsKindAiLive, "ai_live_question"
		}
		if err := s.requireFlag(ctx, flag, userID); err != nil {
			return Trial{}, err
		}
		p.SampleQuestionID = sql.NullInt64{Int64: int64(sq.ID), Valid: true}
		p.InputMode, p.Qtype, p.Score, p.Stem = dbq.TrialsInputModeSample, dbq.TrialsQtype(sq.Qtype), sq.Score, sq.Stem
		p.ReferenceAnswer, p.AnswerSource, p.Rubric = sq.Answer, dbq.TrialsAnswerSourceAi, sq.Rubric
	} else {
		if err := s.requireFlag(ctx, "trial", userID); err != nil {
			return Trial{}, err
		}
		stem, answer := strings.TrimSpace(in.Stem), strings.TrimSpace(in.Answer)
		if stem == "" || len([]rune(stem)) > maxTrialStem || len([]rune(answer)) > maxTrialAnswer {
			return Trial{}, apperr.New(apperr.BadRequest, "题干不能为空，内容也不能太长")
		}
		if !slices.Contains(trialQTypes, in.QType) {
			return Trial{}, apperr.New(apperr.BadRequest, "体验只支持名词解释、简答、论述，换一道试试").With("reason", "unsupported_qtype")
		}
		if in.Score <= 0 || in.Score > 300 {
			return Trial{}, apperr.New(apperr.BadRequest, "分值不正确")
		}
		mode := dbq.TrialsInputModePaste
		if in.InputMode == "photo" {
			mode = dbq.TrialsInputModePhoto
		}
		var points []ai.RubricPoint
		source := dbq.TrialsAnswerSourceUser
		if answer != "" {
			out, _, err := ai.ExtractRubric.Run(ctx, s.ai, userID, ai.RubricIn{QType: in.QType, Stem: stem, Answer: answer, Score: in.Score})
			if err != nil {
				return Trial{}, err
			}
			points = out.Points
		} else {
			out, _, err := ai.GenerateAnswer.Run(ctx, s.ai, userID, ai.GenerateAnswerIn{QType: in.QType, Stem: stem, Score: in.Score})
			if err != nil {
				return Trial{}, err
			}
			answer, points, source = out.Answer, out.Points, dbq.TrialsAnswerSourceAi
		}
		if len(points) == 0 {
			return Trial{}, apperr.New(apperr.AIFailed, "没能整理出采分点，换一道试试")
		}
		ai.ScalePoints(points, in.Score)
		p.Kind, p.InputMode, p.Qtype, p.Score, p.Stem = dbq.TrialsKindOwn, mode, dbq.TrialsQtype(in.QType), fmtScore(in.Score).String, stem
		p.ReferenceAnswer, p.AnswerSource, p.Rubric = answer, source, pointsJSONOf(points)
	}
	id, err := s.q.InsertTrial(ctx, p)
	if err != nil {
		return Trial{}, err
	}
	return s.Trial(ctx, userID, uint64(id))
}

func (s *Service) requireFlag(ctx context.Context, key string, userID uint64) error {
	if s.flags == nil {
		return nil
	}
	on, err := s.flags.Enabled(ctx, key, userID)
	if err != nil {
		return err
	}
	if !on {
		return apperr.NotFoundErr()
	}
	return nil
}

func rubricOf(raw json.RawMessage) []ai.RubricPoint {
	var ps []ai.RubricPoint
	_ = json.Unmarshal(raw, &ps)
	return ps
}

func gradePoints(ps []ai.RubricPoint) []ai.GradePoint {
	out := make([]ai.GradePoint, len(ps))
	for i, p := range ps {
		out[i] = ai.GradePoint{Seq: i + 1, Content: p.Content, Score: p.Score, Keywords: p.Keywords}
	}
	return out
}

// Trial 读出一次体验；批改前不返回参考答案与采分点内容，只返回采分点个数。
func (s *Service) Trial(ctx context.Context, userID, id uint64) (Trial, error) {
	r, err := s.q.GetTrial(ctx, dbq.GetTrialParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Trial{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Trial{}, err
	}
	st, err := s.TrialStatus(ctx, userID)
	if err != nil {
		return Trial{}, err
	}
	score, _ := strconv.ParseFloat(r.Score, 64)
	ps := rubricOf(r.Rubric)
	t := Trial{ID: r.ID, Kind: string(r.Kind), QType: string(r.Qtype), Score: score, Stem: r.Stem, AnswerSource: string(r.AnswerSource),
		PointCount: len(ps), Status: string(r.Status), QuestionID: uint64(r.SavedQuestionID.Int64), Remaining: st.Remaining}
	if r.Status == dbq.TrialsStatusReady {
		return t, nil
	}
	var res trialResult
	_ = json.Unmarshal(r.Result, &res)
	snap := rubricSnap{Points: gradePoints(ps)}
	if len(res.Out.Points) == len(ps) {
		t.Points = points(snap, res.Out)
	}
	if v, ok := parseScore(r.Total); ok {
		t.Total = &v
	}
	t.Reference, t.AnswerText = r.ReferenceAnswer, r.AnswerText.String
	t.Extra = gradingExtra{Items: res.Out.Suggestions, StructureOK: res.Out.StructureOK, StructureNote: res.Out.StructureNote}
	return t, nil
}

// TrialAnswer 是一次体验作答。
type TrialAnswer struct {
	Key       string
	Answer    string
	Mode      string // typed / photo
	PhotoKeys []string
}

// SubmitTrial 提交体验作答并批改（不扣额度）。批改成功才计一次体验；次数用完时不调模型。
func (s *Service) SubmitTrial(ctx context.Context, userID, id uint64, in TrialAnswer) (Trial, error) {
	if len(in.Key) < 8 {
		return Trial{}, apperr.New(apperr.BadRequest, "缺少幂等键")
	}
	if prev, err := s.q.GetTrialByKey(ctx, dbq.GetTrialByKeyParams{OwnerUserID: userID, IdempotencyKey: sql.NullString{String: in.Key, Valid: true}}); err == nil {
		return s.Trial(ctx, userID, prev.ID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Trial{}, err
	}
	answer := strings.TrimSpace(in.Answer)
	if answer == "" {
		return Trial{}, apperr.New(apperr.BadRequest, "先写点内容再提交")
	}
	r, err := s.q.GetTrial(ctx, dbq.GetTrialParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Trial{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Trial{}, err
	}
	if r.Status != dbq.TrialsStatusReady {
		return s.Trial(ctx, userID, id)
	}
	if len(in.PhotoKeys) > 0 {
		if err := ownPhotos(userID, in.PhotoKeys); err != nil {
			return Trial{}, err
		}
	}
	if st, err := s.TrialStatus(ctx, userID); err != nil {
		return Trial{}, err
	} else if st.Remaining <= 0 {
		return Trial{}, trialUsedUp()
	}
	ps := gradePoints(rubricOf(r.Rubric))
	out, _, err := ai.Grade.Run(ctx, s.ai, userID, ai.GradeIn{QType: string(r.Qtype), Stem: r.Stem, Reference: r.ReferenceAnswer, Points: ps, Answer: answer})
	if err != nil {
		return Trial{}, err // 批改失败不计次
	}
	limit, err := s.trialLimit(ctx)
	if err != nil {
		return Trial{}, err
	}
	mode := dbq.TrialsAnswerModeTyped
	if in.Mode == "photo" {
		mode = dbq.TrialsAnswerModePhoto
	}
	resJSON, _ := json.Marshal(trialResult{Out: out})
	var photos dbtypes.NullJSON
	if len(in.PhotoKeys) > 0 {
		photos, _ = json.Marshal(in.PhotoKeys)
	}
	now := s.now().UTC()
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		if err := q.EnsureTrialUsage(ctx, userID); err != nil {
			return err
		}
		used, err := q.LockTrialUsage(ctx, userID)
		if err != nil {
			return err
		}
		if int(used) >= limit {
			return trialUsedUp()
		}
		n, err := q.SetTrialGraded(ctx, dbq.SetTrialGradedParams{AnswerMode: dbq.NullTrialsAnswerMode{TrialsAnswerMode: mode, Valid: true},
			AnswerText: sql.NullString{String: answer, Valid: true}, PhotoKeys: photos, Result: resJSON, Total: fmtScore(out.Total()),
			IdempotencyKey: sql.NullString{String: in.Key, Valid: true}, GradedAt: sql.NullTime{Time: now, Valid: true}, ID: id, OwnerUserID: userID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.New(apperr.Conflict, "这道题已经批改过了")
		}
		if err := q.IncTrialUsage(ctx, dbq.IncTrialUsageParams{Now: sql.NullTime{Time: now, Valid: true}, OwnerUserID: userID}); err != nil {
			return err
		}
		if r.SampleQuestionID.Valid {
			return q.IncSampleTryCount(ctx, uint64(r.SampleQuestionID.Int64))
		}
		return nil
	})
	if err != nil {
		return Trial{}, err
	}
	return s.Trial(ctx, userID, id)
}

// 1.9b 弹层的三个选择。
const (
	TrialSaveImport  = "import_all" // 加入，并导入整套真题 → 1.5
	TrialSaveOnly    = "only"       // 只加入这道题 → 2.1
	TrialSaveDiscard = "discard"    // 不保存 → 2.1c
)

// SaveTrial 处理 1.9b 的选择：加入时把题目、采分点和这次作答存进这门专业课的自建题库（作为第一题），
// 答案来源为「来自你的资料」（用户填了参考答案）或「AI 参考答案 · 待确认」；不保存时整条删除。返回存入的题目 id。
func (s *Service) SaveTrial(ctx context.Context, userID, id uint64, action string, subjectID uint64) (uint64, error) {
	r, err := s.q.GetTrial(ctx, dbq.GetTrialParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperr.NotFoundErr()
	}
	if err != nil {
		return 0, err
	}
	switch action {
	case TrialSaveDiscard:
		if r.Status == dbq.TrialsStatusSaved {
			return 0, apperr.New(apperr.Conflict, "这道题已经加入题库了")
		}
		return 0, s.q.DeleteTrial(ctx, dbq.DeleteTrialParams{ID: id, OwnerUserID: userID})
	case TrialSaveImport, TrialSaveOnly:
	default:
		return 0, apperr.New(apperr.BadRequest, "选择不正确")
	}
	if r.Status == dbq.TrialsStatusSaved {
		return uint64(r.SavedQuestionID.Int64), nil
	}
	if r.Status != dbq.TrialsStatusGraded {
		return 0, apperr.New(apperr.BadRequest, "先提交批改")
	}
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return 0, err
	}
	ps := rubricOf(r.Rubric)
	var res trialResult
	_ = json.Unmarshal(r.Result, &res)
	score, _ := strconv.ParseFloat(r.Score, 64)
	origin, tier := dbq.QuestionsAnswerOriginImported, rules.AnswerMaterial
	rubricOrigin := dbq.RubricPointsOriginAiExtracted
	if r.AnswerSource == dbq.TrialsAnswerSourceAi {
		origin, tier, rubricOrigin = dbq.QuestionsAnswerOriginAiGenerated, rules.AnswerAI, dbq.RubricPointsOriginAiGenerated
	}
	snap := rubricSnap{Version: 1, Source: string(rubricOrigin), Reference: r.ReferenceAnswer, Points: gradePoints(ps), FullScore: score, AnswerTier: string(tier)}
	snapJSON, _ := json.Marshal(snap)
	var qid uint64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		res2, err := q.InsertQuestion(ctx, dbq.InsertQuestionParams{OwnerUserID: owner(userID), BankID: b.BankID, Qtype: dbq.QuestionsQtype(r.Qtype), Stem: r.Stem,
			Answer: sql.NullString{String: r.ReferenceAnswer, Valid: true}, AnswerOrigin: dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: origin, Valid: true},
			Score: fmtScore(score), Source: dbq.QuestionsSourceExercise, ContentHash: importer.ContentHash(r.Stem),
			Difficulty: dbq.NullQuestionsDifficulty{QuestionsDifficulty: dbq.QuestionsDifficultyMedium, Valid: true}})
		if err != nil {
			return err
		}
		qid = uint64(res2)
		qnull := sql.NullInt64{Int64: int64(qid), Valid: true}
		for i, p := range ps {
			var kw dbtypes.NullJSON
			if len(p.Keywords) > 0 {
				kw, _ = json.Marshal(p.Keywords)
			}
			if err := q.InsertRubricPoint(ctx, dbq.InsertRubricPointParams{OwnerUserID: owner(userID), QuestionID: qnull, Seq: uint16(i + 1), Content: p.Content,
				Keywords: kw, Score: fmtScore(p.Score), Origin: rubricOrigin}); err != nil {
				return err
			}
		}
		// 这次作答一起保留（4.7 能看到），不计入掌握度与预估分（体验结果仅供参考）。
		mode := dbq.AttemptsAnswerModeTyped
		if r.AnswerMode.Valid && r.AnswerMode.TrialsAnswerMode == dbq.TrialsAnswerModePhoto {
			mode = dbq.AttemptsAnswerModePhoto
		}
		aid, err := q.InsertAttempt(ctx, dbq.InsertAttemptParams{OwnerUserID: userID, QuestionID: qid, AnswerMode: mode, AnswerText: r.AnswerText,
			Score: r.Total, FullScore: fmtScore(score), AnsweredAt: r.GradedAt.Time})
		if err != nil {
			return err
		}
		if err := markReference(ctx, q, userID, uint64(aid), snap.AnswerTier); err != nil {
			return err
		}
		extra := gradingExtra{Items: res.Out.Suggestions, StructureOK: res.Out.StructureOK, StructureNote: res.Out.StructureNote}
		if len(res.Out.Points) != len(ps) {
			return apperr.New(apperr.Conflict, "批改结果不完整，不能加入题库")
		}
		if _, err := s.insertDone(ctx, q, userID, uint64(aid), dbq.GradingsTriggerReasonSubmit, 0, snap, snapJSON, res.Out, nil, extra, ai.Meta{}, false, ""); err != nil {
			return err
		}
		if err := q.SetTrialSaved(ctx, dbq.SetTrialSavedParams{SavedQuestionID: qnull, ID: id, OwnerUserID: userID}); err != nil {
			return err
		}
		return q.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	})
	return qid, err
}

// --- 学科通用体验题（1.10）与 AI 现场出题 ---

// SampleItem 是一道体验题的摘要。
type SampleItem struct {
	ID       uint64
	QType    string
	Score    float64
	Stem     string
	Textbook string
	AI       bool // AI 现场出题，标「AI 出题」
}

// SampleDirection 是一个方向下已上线的体验题。
type SampleDirection struct {
	ID        uint64
	Name      string
	Questions []SampleItem
}

// SampleDiscipline 是一个学科。
type SampleDiscipline struct {
	Name       string
	Directions []SampleDirection
}

// SampleCatalog 是 1.10：已覆盖学科、方向与已上线的体验题。
func (s *Service) SampleCatalog(ctx context.Context) ([]SampleDiscipline, error) {
	rows, err := s.q.ListSampleCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var out []SampleDiscipline
	for _, r := range rows {
		if len(out) == 0 || out[len(out)-1].Name != r.Discipline {
			out = append(out, SampleDiscipline{Name: r.Discipline})
		}
		d := &out[len(out)-1]
		if len(d.Directions) == 0 || d.Directions[len(d.Directions)-1].ID != r.DirectionID {
			d.Directions = append(d.Directions, SampleDirection{ID: r.DirectionID, Name: r.Direction})
		}
		dir := &d.Directions[len(d.Directions)-1]
		sc, _ := strconv.ParseFloat(r.Score, 64)
		dir.Questions = append(dir.Questions, SampleItem{ID: r.ID, QType: string(r.Qtype), Score: sc, Stem: r.Stem, Textbook: r.Textbook})
	}
	if out == nil {
		out = []SampleDiscipline{}
	}
	return out, nil
}

// liveKey 是 AI 现场出题的缓存键：同一门专业课（按院校 + 代码或名称归并；没有院校时只按名称）复用同一道题。
func liveKey(school, code, name string) string {
	return "live:" + rules.CourseKey(school, code, name)
}

// LiveSample 为未覆盖学科的用户按其专业课和参考教材现场生成一道名词解释（标「AI 出题」），同一门专业课的结果缓存复用。
// 不扣 AI 出题额度（增补「体验与额度」）。
func (s *Service) LiveSample(ctx context.Context, userID, subjectID uint64) (SampleItem, error) {
	sj, err := s.q.GetSubject(ctx, dbq.GetSubjectParams{ID: subjectID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return SampleItem{}, apperr.NotFoundErr()
	}
	if err != nil {
		return SampleItem{}, err
	}
	key := liveKey(sj.School.String, sj.Code.String, sj.Name)
	toItem := func(r dbq.SampleQuestion) SampleItem {
		sc, _ := strconv.ParseFloat(r.Score, 64)
		return SampleItem{ID: r.ID, QType: string(r.Qtype), Score: sc, Stem: r.Stem, Textbook: r.Textbook, AI: true}
	}
	if r, err := s.q.GetLiveSampleByKey(ctx, sql.NullString{String: key, Valid: true}); err == nil {
		return toItem(r), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SampleItem{}, err
	}
	var books []string
	if len(sj.Textbooks) > 0 {
		_ = json.Unmarshal(sj.Textbooks, &books)
	}
	avoid, err := s.q.ListStemsByCacheKey(ctx, sql.NullString{String: key, Valid: true})
	if err != nil {
		return SampleItem{}, err
	}
	const qtype, score = "term", 5.0
	out, _, err := ai.SampleQuestion.Run(ctx, s.ai, userID, ai.SampleQuestionIn{Subject: sj.Name, Textbooks: books, QType: qtype, Score: score, Avoid: avoid})
	if err != nil {
		return SampleItem{}, err
	}
	book := ""
	if len(books) > 0 {
		book = books[0]
	}
	id, err := s.q.InsertSampleQuestion(ctx, dbq.InsertSampleQuestionParams{Origin: dbq.SampleQuestionsOriginAiLive, CacheKey: sql.NullString{String: key, Valid: true},
		SubjectLabel: truncate(sj.Name, 128), Textbook: truncate(book, 128), Qtype: dbq.SampleQuestionsQtype(qtype), Score: fmtScore(score).String,
		Stem: out.Stem, Answer: out.Answer, Rubric: pointsJSONOf(out.Points), Status: dbq.SampleQuestionsStatusOnline})
	if err != nil {
		return SampleItem{}, err
	}
	r, err := s.q.GetSampleQuestion(ctx, uint64(id))
	if err != nil {
		return SampleItem{}, err
	}
	return toItem(r), nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// --- 后台 7.18 通用体验题 ---

// SampleStats 是 7.18 的统计卡。
type SampleStats struct {
	Disciplines, Directions, Questions, Pending, WeekTries int
}

// AdminSampleDirection 是学科与方向树上的一个方向。
type AdminSampleDirection struct {
	ID            uint64
	Discipline    string
	Name          string
	QuestionCount int
}

// AdminSample 是后台的一道体验题（含参考答案与采分点）。
type AdminSample struct {
	SampleItem
	DirectionID  uint64
	Origin       string
	SubjectLabel string
	Answer       string
	Rubric       []ai.RubricPoint
	Status       string
	TryCount     int
	CreatedAt    time.Time
}

func toAdminSample(r dbq.SampleQuestion) AdminSample {
	sc, _ := strconv.ParseFloat(r.Score, 64)
	return AdminSample{SampleItem: SampleItem{ID: r.ID, QType: string(r.Qtype), Score: sc, Stem: r.Stem, Textbook: r.Textbook, AI: r.Origin == dbq.SampleQuestionsOriginAiLive},
		DirectionID: uint64(r.DirectionID.Int64), Origin: string(r.Origin), SubjectLabel: r.SubjectLabel, Answer: r.Answer, Rubric: rubricOf(r.Rubric),
		Status: string(r.Status), TryCount: int(r.TryCount), CreatedAt: r.CreatedAt}
}

// SampleOverview 是 7.18 的统计卡与学科方向树。
func (s *Service) SampleOverview(ctx context.Context) (SampleStats, []AdminSampleDirection, error) {
	st, err := s.q.AdminSampleStats(ctx, sql.NullTime{Time: s.now().UTC().AddDate(0, 0, -7), Valid: true})
	if err != nil {
		return SampleStats{}, nil, err
	}
	rows, err := s.q.AdminListSampleDirections(ctx)
	if err != nil {
		return SampleStats{}, nil, err
	}
	dirs := make([]AdminSampleDirection, len(rows))
	for i, r := range rows {
		dirs[i] = AdminSampleDirection{ID: r.ID, Discipline: r.Discipline, Name: r.Name, QuestionCount: int(r.QuestionCount)}
	}
	return SampleStats{Disciplines: int(st.Disciplines), Directions: int(st.Directions), Questions: int(st.Questions), Pending: int(st.Pending), WeekTries: int(st.WeekTries)}, dirs, nil
}

// CreateSampleDirection 新增学科或方向。
func (s *Service) CreateSampleDirection(ctx context.Context, discipline, name string) (uint64, error) {
	discipline, name = strings.TrimSpace(discipline), strings.TrimSpace(name)
	if discipline == "" || name == "" {
		return 0, apperr.New(apperr.BadRequest, "学科和方向都要填")
	}
	id, err := s.q.InsertSampleDirection(ctx, dbq.InsertSampleDirectionParams{Discipline: discipline, Name: name, SortOrder: 99})
	if store.IsDuplicate(err) {
		return 0, apperr.New(apperr.Conflict, "这个方向已经有了")
	}
	return uint64(id), err
}

// SampleQuestions 是一个方向下的题目（含待审核、已下线）；directionID 为 0 时是「AI 出题缓存」列表。
func (s *Service) SampleQuestions(ctx context.Context, directionID uint64) ([]AdminSample, error) {
	var rows []dbq.SampleQuestion
	var err error
	if directionID == 0 {
		rows, err = s.q.AdminListLiveSamples(ctx)
	} else {
		rows, err = s.q.AdminListSampleQuestions(ctx, sql.NullInt64{Int64: int64(directionID), Valid: true})
	}
	if err != nil {
		return nil, err
	}
	out := make([]AdminSample, len(rows))
	for i, r := range rows {
		out[i] = toAdminSample(r)
	}
	return out, nil
}

// SampleInput 是手工录入或修改一道体验题。
type SampleInput struct {
	Textbook string
	QType    string
	Score    float64
	Stem     string
	Answer   string
	Rubric   []ai.RubricPoint
}

func (in SampleInput) validate() (SampleInput, error) {
	in.Stem, in.Answer, in.Textbook = strings.TrimSpace(in.Stem), strings.TrimSpace(in.Answer), strings.TrimSpace(in.Textbook)
	if in.Stem == "" || in.Answer == "" || !slices.Contains(trialQTypes, in.QType) || in.Score <= 0 {
		return in, apperr.New(apperr.BadRequest, "题干、参考答案、题型、分值都要填")
	}
	if len(in.Rubric) == 0 {
		return in, apperr.New(apperr.BadRequest, "至少一个采分点")
	}
	sum := 0.0
	for _, p := range in.Rubric {
		if strings.TrimSpace(p.Content) == "" || p.Score <= 0 {
			return in, apperr.New(apperr.BadRequest, "每个采分点都要有内容和分值")
		}
		sum += p.Score
	}
	// 增补「导入数据结构」：采分点合计等于题目分值。
	if math.Abs(sum-in.Score) > 0.01 {
		return in, apperr.New(apperr.BadRequest, "采分点合计 "+strconv.FormatFloat(sum, 'f', -1, 64)+" 分，和分值不一致").With("reason", "rubric_sum")
	}
	return in, nil
}

// SaveSample 新建（id 为 0，进待审核）或修改一道体验题。
func (s *Service) SaveSample(ctx context.Context, adminID, directionID, id uint64, in SampleInput) (uint64, error) {
	in, err := in.validate()
	if err != nil {
		return 0, err
	}
	if id == 0 {
		if _, err := s.q.GetSampleDirection(ctx, directionID); errors.Is(err, sql.ErrNoRows) {
			return 0, apperr.NotFoundErr()
		} else if err != nil {
			return 0, err
		}
		nid, err := s.q.InsertSampleQuestion(ctx, dbq.InsertSampleQuestionParams{DirectionID: sql.NullInt64{Int64: int64(directionID), Valid: true}, Origin: dbq.SampleQuestionsOriginManual,
			Textbook: truncate(in.Textbook, 128), Qtype: dbq.SampleQuestionsQtype(in.QType), Score: fmtScore(in.Score).String, Stem: in.Stem, Answer: in.Answer,
			Rubric: pointsJSONOf(in.Rubric), Status: dbq.SampleQuestionsStatusPending, CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}})
		return uint64(nid), err
	}
	if _, err := s.q.GetSampleQuestion(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return 0, apperr.NotFoundErr()
	} else if err != nil {
		return 0, err
	}
	return id, s.q.UpdateSampleQuestion(ctx, dbq.UpdateSampleQuestionParams{Textbook: truncate(in.Textbook, 128), Qtype: dbq.SampleQuestionsQtype(in.QType),
		Score: fmtScore(in.Score).String, Stem: in.Stem, Answer: in.Answer, Rubric: pointsJSONOf(in.Rubric), ID: id})
}

// SetSampleStatus 审核上线（online）或下线（offline）一道体验题；AI 出题缓存也可以在这里下线，下线后同一门课下次重新生成。
func (s *Service) SetSampleStatus(ctx context.Context, adminID, id uint64, status string) error {
	st := dbq.SampleQuestionsStatus(status)
	if st != dbq.SampleQuestionsStatusOnline && st != dbq.SampleQuestionsStatusOffline {
		return apperr.New(apperr.BadRequest, "状态不正确")
	}
	if _, err := s.q.GetSampleQuestion(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	} else if err != nil {
		return err
	}
	return s.q.SetSampleQuestionStatus(ctx, dbq.SetSampleQuestionStatusParams{Status: st, ReviewedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0},
		ReviewedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id})
}

// maxSampleBatch 是一次 AI 批量生成的题数上限。
const maxSampleBatch = 10

// GenerateSamples 按方向用 AI 批量生成体验题初稿，全部进待审核。生成失败的跳过，返回成功的条数。
func (s *Service) GenerateSamples(ctx context.Context, adminID, directionID uint64, qtype string, score float64, count int, textbook string) (int, error) {
	d, err := s.q.GetSampleDirection(ctx, directionID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperr.NotFoundErr()
	}
	if err != nil {
		return 0, err
	}
	if !slices.Contains(trialQTypes, qtype) || score <= 0 {
		return 0, apperr.New(apperr.BadRequest, "题型或分值不正确")
	}
	count = min(max(count, 1), maxSampleBatch)
	avoid, err := s.q.ListDirectionStems(ctx, sql.NullInt64{Int64: int64(directionID), Valid: true})
	if err != nil {
		return 0, err
	}
	var books []string
	if textbook = strings.TrimSpace(textbook); textbook != "" {
		books = []string{textbook}
	}
	made := 0
	var lastErr error
	for range count {
		out, _, err := ai.SampleQuestion.Run(ctx, s.ai, 0, ai.SampleQuestionIn{Subject: d.Discipline + " · " + d.Name, Textbooks: books, QType: qtype, Score: score, Avoid: avoid})
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := s.q.InsertSampleQuestion(ctx, dbq.InsertSampleQuestionParams{DirectionID: sql.NullInt64{Int64: int64(directionID), Valid: true}, Origin: dbq.SampleQuestionsOriginAiBatch,
			Textbook: truncate(textbook, 128), Qtype: dbq.SampleQuestionsQtype(qtype), Score: fmtScore(score).String, Stem: out.Stem, Answer: out.Answer,
			Rubric: pointsJSONOf(out.Points), Status: dbq.SampleQuestionsStatusPending, CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}}); err != nil {
			return made, err
		}
		avoid = append(avoid, out.Stem)
		made++
	}
	if made == 0 && lastErr != nil {
		return 0, lastErr
	}
	return made, nil
}
