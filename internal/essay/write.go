package essay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/notify"
	"peetraining-server/internal/quota"
)

// 写作与批改的限制。
const (
	minWords       = 50 // 少于 50 字不批改
	maxWords       = 6000
	defaultWords   = 800 // 没有字数要求时按 800 字
	maxPhotoKeys   = 6
	aiTopicHistory = 10 // AI 命题避开最近 10 道
)

// AITopic 是一道 AI 命题（标「AI 出题」）。
type AITopic struct {
	ID            uint64
	Topic         string
	RequiredWords int
	Note          string
	CreatedAt     time.Time
}

// GenerateTopic 按用户作文真题的命题方式出一道新题（5.1「AI 命题」「再出一道」），计 1 道 AI 出题额度（D25）。
func (s *Service) GenerateTopic(ctx context.Context, userID, subjectID uint64) (AITopic, error) {
	sub, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return AITopic{}, err
	}
	past, err := s.q.ListEssayTopicsKB(ctx, dbq.ListEssayTopicsKBParams{BankID: sub.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return AITopic{}, err
	}
	recent, err := s.q.ListEssayAITopics(ctx, dbq.ListEssayAITopicsParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return AITopic{}, err
	}
	in := ai.EssayPromptIn{Subject: sub.Name, PastTopics: []string{}, Avoid: []string{}}
	for _, p := range past {
		in.PastTopics = append(in.PastTopics, p.Stem)
	}
	for i, r := range recent {
		if i < aiTopicHistory {
			in.Avoid = append(in.Avoid, r.Topic)
		}
	}
	// 先看额度够不够，避免白调模型；扣减和保存放在同一个事务里。
	if left, err := s.quota.Remaining(ctx, userID, quota.AIQuestions); err != nil {
		return AITopic{}, err
	} else if left != nil && *left <= 0 {
		return AITopic{}, s.quota.Consume(ctx, s.q, quota.Charge{UserID: userID, Type: quota.AIQuestions, Amount: 1, Key: "essay_topic:probe"})
	}
	out, meta, err := ai.EssayPrompt.Run(ctx, s.ai, userID, in)
	if err != nil {
		return AITopic{}, err
	}
	words := out.RequiredWords
	if words <= 0 {
		words = defaultWords
	}
	now := s.now().UTC()
	var id int64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		id, err = q.InsertEssayAITopic(ctx, dbq.InsertEssayAITopicParams{OwnerUserID: userID, SubjectID: subjectID, Topic: out.Topic,
			RequiredWords: sql.NullInt16{Int16: int16(words), Valid: true}, Note: sql.NullString{String: out.Note, Valid: out.Note != ""},
			Model: sql.NullString{String: meta.Model, Valid: true}, PromptVersion: sql.NullString{String: meta.Version, Valid: true}, CreatedAt: now})
		if err != nil {
			return err
		}
		return s.quota.Consume(ctx, q, quota.Charge{UserID: userID, Type: quota.AIQuestions, Amount: 1, Ref: quota.Ref{Type: "essay_topic", ID: uint64(id)},
			Key: quota.KeyFor("essay_topic", uint64(id))})
	})
	if err != nil {
		return AITopic{}, err
	}
	return AITopic{ID: uint64(id), Topic: out.Topic, RequiredWords: words, Note: out.Note, CreatedAt: now}, nil
}

// CreateInput 是开始写一篇作文：真题（QuestionID）、AI 命题（AITopicID）、自拟（TopicText）三选一；
// ParentID 不为空时是「按建议重写」，沿用原题，记为下一稿。
type CreateInput struct {
	SubjectID     uint64
	Source        string // exam / ai / custom
	QuestionID    uint64
	AITopicID     uint64
	TopicText     string
	RequiredWords int
	ParentID      uint64
	Key           string
}

// Create 开始写一篇作文（5.2），同一个幂等键返回同一篇。真题默认按考试时长限时。
func (s *Service) Create(ctx context.Context, userID uint64, in CreateInput) (View, error) {
	if in.Key != "" {
		if e, err := s.q.GetEssayByKey(ctx, dbq.GetEssayByKeyParams{OwnerUserID: userID, IdempotencyKey: sql.NullString{String: in.Key, Valid: true}}); err == nil {
			return s.view(ctx, userID, e)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return View{}, err
		}
	}
	p := dbq.InsertEssayParams{OwnerUserID: userID, DraftNo: 1, IdempotencyKey: sql.NullString{String: in.Key, Valid: in.Key != ""}}
	if in.ParentID != 0 {
		parent, err := s.essay(ctx, userID, in.ParentID)
		if err != nil {
			return View{}, err
		}
		if parent.Status != dbq.EssaysStatusGraded {
			return View{}, apperr.New(apperr.Conflict, "这篇还没批改完，批改后再按建议重写")
		}
		p.SubjectID, p.TopicSource, p.TopicQuestionID, p.AiTopicID = parent.SubjectID, parent.TopicSource, parent.TopicQuestionID, parent.AiTopicID
		p.TopicText, p.RequiredWords, p.Timed = parent.TopicText, parent.RequiredWords, parent.Timed
		p.DraftNo, p.ParentEssayID = parent.DraftNo+1, sql.NullInt64{Int64: int64(parent.ID), Valid: true}
	} else {
		if _, err := s.subject(ctx, userID, in.SubjectID); err != nil {
			return View{}, err
		}
		p.SubjectID = in.SubjectID
		switch in.Source {
		case "exam":
			q, err := s.q.GetEssayTopicQuestion(ctx, dbq.GetEssayTopicQuestionParams{ID: in.QuestionID, OwnerUserID: owner(userID)})
			if errors.Is(err, sql.ErrNoRows) || (err == nil && uint64(q.SubjectID.Int64) != in.SubjectID) {
				return View{}, apperr.NotFoundErr()
			}
			if err != nil {
				return View{}, err
			}
			p.TopicSource, p.TopicQuestionID, p.TopicText, p.RequiredWords, p.Timed = dbq.EssaysTopicSourceExam, sql.NullInt64{Int64: int64(q.ID), Valid: true}, q.Stem, q.RequiredWords, true
			// 同一道真题写过的话，记为下一稿。
			if n, err := s.draftsOf(ctx, userID, in.SubjectID, q.ID); err == nil {
				p.DraftNo = uint8(min(n+1, 255))
			}
		case "ai":
			t, err := s.q.GetEssayAITopic(ctx, dbq.GetEssayAITopicParams{ID: in.AITopicID, OwnerUserID: userID})
			if errors.Is(err, sql.ErrNoRows) || (err == nil && t.SubjectID != in.SubjectID) {
				return View{}, apperr.NotFoundErr()
			}
			if err != nil {
				return View{}, err
			}
			p.TopicSource, p.AiTopicID, p.TopicText, p.RequiredWords = dbq.EssaysTopicSourceAi, sql.NullInt64{Int64: int64(t.ID), Valid: true}, t.Topic, t.RequiredWords
		case "custom":
			text := strings.TrimSpace(in.TopicText)
			if len([]rune(text)) < 4 || len([]rune(text)) > 2000 {
				return View{}, apperr.New(apperr.BadRequest, "题目写完整一些（4–2000 字）")
			}
			p.TopicSource, p.TopicText = dbq.EssaysTopicSourceCustom, text
			if in.RequiredWords > 0 {
				p.RequiredWords = sql.NullInt16{Int16: int16(min(in.RequiredWords, 5000)), Valid: true}
			}
		default:
			return View{}, apperr.New(apperr.BadRequest, "请选择题目来源")
		}
	}
	id, err := s.q.InsertEssay(ctx, p)
	if err != nil {
		return View{}, err
	}
	e, err := s.essay(ctx, userID, uint64(id))
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, userID, e)
}

func (s *Service) draftsOf(ctx context.Context, userID, subjectID, questionID uint64) (int, error) {
	rows, err := s.q.ListSubjectEssays(ctx, dbq.ListSubjectEssaysParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if r.TopicQuestionID.Valid && uint64(r.TopicQuestionID.Int64) == questionID {
			n++
		}
	}
	return n, nil
}

// DraftInput 是草稿内容；为空的字段不改。
type DraftInput struct {
	Content         *string
	DurationSeconds *int
	Timed           *bool
	PhotoKeys       *[]string
}

// SaveDraft 保存草稿（客户端每 5 秒一次）。提交后不能再改（409）。
func (s *Service) SaveDraft(ctx context.Context, userID, id uint64, in DraftInput) (View, error) {
	e, err := s.essay(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	if e.Status != dbq.EssaysStatusDraft && e.Status != dbq.EssaysStatusFailed {
		return View{}, apperr.New(apperr.Conflict, "这篇已经提交批改了")
	}
	content := e.Content.String
	if in.Content != nil {
		content = *in.Content
	}
	if countWords(content) > maxWords {
		return View{}, apperr.New(apperr.BadRequest, "作文太长了（最多 6000 字）")
	}
	dur, timed, photos := int(e.DurationSeconds), e.Timed, e.PhotoKeys
	if in.DurationSeconds != nil {
		dur = max(*in.DurationSeconds, 0)
	}
	if in.Timed != nil {
		timed = *in.Timed
	}
	if in.PhotoKeys != nil {
		keys := *in.PhotoKeys
		prefix := "u/" + strconv.FormatUint(userID, 10) + "/hw/"
		if len(keys) > maxPhotoKeys {
			return View{}, apperr.New(apperr.BadRequest, "一篇最多拍 6 页")
		}
		for _, k := range keys {
			if !strings.HasPrefix(k, prefix) {
				return View{}, apperr.NotFoundErr()
			}
		}
		raw, _ := json.Marshal(keys)
		photos = dbtypes.NullJSON(raw)
	}
	if _, err := s.q.SaveEssayDraft(ctx, dbq.SaveEssayDraftParams{Content: sql.NullString{String: content, Valid: true}, WordCount: uint32(countWords(content)),
		DurationSeconds: uint32(dur), Timed: timed, PhotoKeys: photos, ID: id, OwnerUserID: userID}); err != nil {
		return View{}, err
	}
	e, err = s.essay(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, userID, e)
}

func (s *Service) ticket(e dbq.Essay) quota.Ticket {
	return quota.Ticket{UserID: e.OwnerUserID, Type: quota.EssayGrading, Period: e.QuotaPeriod.String, Amount: 1, Key: "essay_submit:" + strconv.FormatUint(e.ID, 10)}
}

// rubricSnapshot 是批改时用的评分标准快照：改了标准后新写的作文按新标准，已批改的分数不变（5.9）。
type rubricSnapshot struct {
	ID         uint64        `json:"id"`
	Name       string        `json:"name"`
	Source     string        `json:"source"`
	FullScore  float64       `json:"full_score"`
	Dimensions []ai.EssayDim `json:"dimensions"`
	MaterialID uint64        `json:"material_id,omitempty"`
	FileName   string        `json:"file_name,omitempty"`
	Page       int           `json:"page,omitempty"`
}

// Submit 提交批改（5.5）：按当前评分标准记快照，预占 1 次作文批改（免费版每周 1 篇），后台约 30 秒批完，可以先离开。
// 重复提交返回同一结果；批改失败后可以再提交。
func (s *Service) Submit(ctx context.Context, userID, id uint64) (View, error) {
	e, err := s.essay(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	if e.Status == dbq.EssaysStatusGrading || e.Status == dbq.EssaysStatusGraded {
		return s.view(ctx, userID, e)
	}
	if countWords(e.Content.String) < minWords {
		return View{}, apperr.New(apperr.BadRequest, "至少写 50 字再提交批改")
	}
	r, err := s.activeRubric(ctx, userID, e.SubjectID)
	if err != nil {
		return View{}, err
	}
	snap, _ := json.Marshal(rubricSnapshot{ID: r.ID, Name: r.Name, Source: r.Source, FullScore: r.FullScore, Dimensions: r.Dimensions,
		MaterialID: r.MaterialID, FileName: r.FileName, Page: r.Page})
	round := int(e.GradingRound) + 1
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		cur, err := q.GetEssayForUpdate(ctx, dbq.GetEssayForUpdateParams{ID: id, OwnerUserID: userID})
		if err != nil {
			return err
		}
		if cur.Status != dbq.EssaysStatusDraft && cur.Status != dbq.EssaysStatusFailed {
			return nil
		}
		// 每次提交一个预占键：失败退回后再提交要重新预占。
		t, err := s.quota.Reserve(ctx, q, quota.Charge{UserID: userID, Type: quota.EssayGrading, Amount: 1, Ref: quota.Ref{Type: "essay", ID: id},
			Key: "essay_submit:" + strconv.FormatUint(id, 10) + ":" + strconv.Itoa(round)})
		if err != nil {
			return err
		}
		return q.SubmitEssay(ctx, dbq.SubmitEssayParams{RubricID: sql.NullInt64{Int64: int64(r.ID), Valid: true}, RubricSnapshot: dbtypes.NullJSON(snap),
			QuotaPeriod: sql.NullString{String: t.Period, Valid: true}, SubmittedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id, OwnerUserID: userID})
	})
	if err != nil {
		return View{}, err
	}
	if err := s.enqueue(ctx, userID, id, round); err != nil {
		return View{}, err
	}
	return s.Get(ctx, userID, id)
}

// submitTicket 是某一轮提交的预占（复核重批没有预占）。
func (s *Service) submitTicket(e dbq.Essay, round int) quota.Ticket {
	t := s.ticket(e)
	t.Key += ":" + strconv.Itoa(round)
	return t
}

// dimScore 是存进 essays.dimension_scores 的一个维度。
type dimScore struct {
	Name    string  `json:"name"`
	Score   float64 `json:"score"`
	Max     float64 `json:"max"`
	Comment string  `json:"comment"`
}

// review 是存进 essays.review 的总评与逐段批注。
type review struct {
	Thesis      string               `json:"thesis"`
	Highlights  []string             `json:"highlights"`
	Problems    []string             `json:"problems"`
	Suggestions []string             `json:"suggestions"`
	Annotations []ai.EssayAnnotation `json:"annotations"`
	Paragraphs  []string             `json:"paragraphs"`
}

// GradeEssay 是后台批改：按提交时的评分标准快照给分、写总评与逐段批注；完成后结算次数、发消息、重算作文课预估分。
// AI 两次都不合格时标为批改失败并退回次数（「批改失败，未扣次数」），用户可以重新提交。可重复执行。
func (s *Service) GradeEssay(ctx context.Context, userID, id uint64) error {
	e, err := s.essay(ctx, userID, id)
	if err != nil {
		return err
	}
	if e.Status != dbq.EssaysStatusGrading {
		return nil
	}
	var snap rubricSnapshot
	if err := json.Unmarshal(e.RubricSnapshot, &snap); err != nil || len(snap.Dimensions) == 0 {
		return s.fail(ctx, e, "评分标准缺失，请重新提交")
	}
	sub, err := s.subject(ctx, userID, e.SubjectID)
	if err != nil {
		return err
	}
	paras := paragraphs(e.Content.String)
	words := int(e.RequiredWords.Int16)
	if words <= 0 {
		words = defaultWords
	}
	out, meta, err := ai.EssayGrade.Run(ctx, s.ai, userID, ai.EssayGradeIn{Subject: sub.Name, Topic: e.TopicText, RequiredWords: words, Dimensions: snap.Dimensions, Paragraphs: paras})
	if err != nil {
		if apperr.IsKind(err, apperr.AIFailed) {
			return s.fail(ctx, e, "批改失败，未扣次数，请重新提交")
		}
		return err // 网络等临时问题交给任务重试
	}
	dims := make([]dimScore, len(out.Dimensions))
	for i, d := range out.Dimensions {
		dims[i] = dimScore{Name: d.Name, Score: d.Score, Max: snap.Dimensions[i].Score, Comment: d.Comment}
	}
	rv := review{Thesis: out.Thesis, Highlights: out.Highlights, Problems: out.Problems, Suggestions: out.Suggestions, Annotations: out.Annotations, Paragraphs: paras}
	dimsRaw, _ := json.Marshal(dims)
	rvRaw, _ := json.Marshal(rv)
	total := out.Total()
	// 按用户评分细则批改、且以真题限时完成的才计入作文课预估分（PRD 11.13）。
	counts := snap.Source == "user_material" && e.TopicSource == dbq.EssaysTopicSourceExam && e.Timed
	round := int(e.GradingRound)
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		cur, err := q.GetEssayForUpdate(ctx, dbq.GetEssayForUpdateParams{ID: id, OwnerUserID: userID})
		if err != nil || cur.Status != dbq.EssaysStatusGrading {
			return err
		}
		if err := q.FinishEssayGrading(ctx, dbq.FinishEssayGradingParams{Score: sql.NullString{String: fmtScore(total), Valid: true},
			FullScore: sql.NullString{String: fmtScore(snap.FullScore), Valid: true}, DimensionScores: dbtypes.NullJSON(dimsRaw), Review: dbtypes.NullJSON(rvRaw),
			CountsForEstimate: counts, Model: sql.NullString{String: meta.Model, Valid: true}, PromptVersion: sql.NullString{String: meta.Version, Valid: true},
			GradedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id, OwnerUserID: userID}); err != nil {
			return err
		}
		if !cur.Disputed {
			// 复核重批不计次；首次批改结算提交时的预占。
			if err := s.quota.Settle(ctx, q, s.submitTicket(cur, round), 1, quota.Ref{Type: "essay", ID: id}); err != nil {
				return err
			}
		}
		if !notify.TaskDoneEnabled(ctx, q) {
			return nil
		}
		link, _ := json.Marshal(map[string]any{"page": "essay_result", "params": map[string]any{"essay_id": id}})
		body := "AI 批改得分 " + fmtShort(total) + " / " + fmtShort(snap.FullScore)
		if snap.Source == "generic" {
			body += "（按通用标准，只作参考）"
		} else {
			body += "（仅供参考）"
		}
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeEssayGraded, Title: "作文批改完成", Body: body + "，点开看逐段批注",
			Link: link, DedupeKey: sql.NullString{String: "essay_graded:" + strconv.FormatUint(id, 10) + ":" + strconv.Itoa(round), Valid: true}})
	})
	if err != nil {
		return err
	}
	if counts && s.score != nil {
		if err := s.score.Recompute(ctx, userID, e.SubjectID, "essay_graded"); err != nil {
			logWarn(ctx, "recompute estimate", "err", err, "essay_id", id)
		}
	}
	return nil
}

func fmtShort(v float64) string { return strconv.FormatFloat(round1(v), 'f', -1, 64) }

// fail 把这一轮批改标为失败：首次批改退回预占；复核重批失败时回到原来的分数。
func (s *Service) fail(ctx context.Context, e dbq.Essay, reason string) error {
	return s.withTx(ctx, func(q *dbq.Queries) error {
		cur, err := q.GetEssayForUpdate(ctx, dbq.GetEssayForUpdateParams{ID: e.ID, OwnerUserID: e.OwnerUserID})
		if err != nil || cur.Status != dbq.EssaysStatusGrading {
			return err
		}
		status := dbq.EssaysStatusFailed
		if cur.Disputed && cur.Score.Valid {
			status = dbq.EssaysStatusGraded
		} else if err := s.quota.Settle(ctx, q, s.submitTicket(cur, int(cur.GradingRound)), 0, quota.Ref{Type: "essay", ID: e.ID}); err != nil {
			return err
		}
		return q.FailEssayGrading(ctx, dbq.FailEssayGradingParams{Status: status, FailReason: sql.NullString{String: reason, Valid: true}, ID: e.ID, OwnerUserID: e.OwnerUserID})
	})
}

// Dispute 对批改有异议（与主观题相同，4.8）：每篇复核一次，重批不计次，以重批结果为准。
func (s *Service) Dispute(ctx context.Context, userID, id uint64, reason, note string) (View, error) {
	switch reason {
	case "hit_missed", "rubric_wrong", "score_unfair", "other":
	default:
		return View{}, apperr.New(apperr.BadRequest, "请选择异议原因")
	}
	e, err := s.essay(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	if e.Status != dbq.EssaysStatusGraded {
		return View{}, apperr.New(apperr.Conflict, "这篇还没批改完")
	}
	if e.Disputed {
		return View{}, apperr.New(apperr.Conflict, "这篇已经复核过了")
	}
	if len([]rune(note)) > 500 {
		return View{}, apperr.New(apperr.BadRequest, "说明不超过 500 字")
	}
	if err := s.q.DisputeEssay(ctx, dbq.DisputeEssayParams{DisputeReason: sql.NullString{String: reason, Valid: true},
		DisputeNote: sql.NullString{String: note, Valid: note != ""}, ID: id, OwnerUserID: userID}); err != nil {
		return View{}, err
	}
	if err := s.enqueue(ctx, userID, id, int(e.GradingRound)+1); err != nil {
		return View{}, err
	}
	return s.Get(ctx, userID, id)
}
