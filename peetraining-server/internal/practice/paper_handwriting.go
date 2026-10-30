package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
)

// PaperHandwriting 与题号永久关联，页面切题不取消任务；截止时间只锁定照片，不截断识别。
type PaperHandwriting struct {
	ID                  uint64
	Order               uint64
	Seq                 int
	Key                 string
	Keys                []string
	Status, Text, Error string
	InputKind           string
	LowConfidence       [][2]int
}

func paperHandwritingView(r dbq.PaperHandwriting) PaperHandwriting {
	v := PaperHandwriting{ID: r.ID, Order: r.CaptureOrder, Seq: int(r.Seq), Key: r.IdempotencyKey, InputKind: r.InputKind, Status: r.Status, Text: r.RecognizedText, Error: r.ErrorMessage}
	_ = json.Unmarshal(r.ObjectKeys, &v.Keys)
	_ = json.Unmarshal(r.LowConfidence, &v.LowConfidence)
	return v
}

func (s *Service) StartPaperHandwriting(ctx context.Context, userID, sessionID uint64, seq int, key string, keys []string, captureOrder int64) (PaperHandwriting, error) {
	return s.startPaperInput(ctx, userID, sessionID, seq, key, keys, captureOrder, "photo")
}
func (s *Service) StartPaperVoice(ctx context.Context, userID, sessionID uint64, seq int, key string, keys []string, order int64) (PaperHandwriting, error) {
	if err := s.voiceEnabled(ctx, userID); err != nil {
		return PaperHandwriting{}, err
	}
	return s.startPaperInput(ctx, userID, sessionID, seq, key, keys, order, "voice")
}
func (s *Service) startPaperInput(ctx context.Context, userID, sessionID uint64, seq int, key string, keys []string, captureOrder int64, inputKind string) (PaperHandwriting, error) {
	if key == "" || len(key) > 128 || captureOrder < 0 || captureOrder > 9007199254740991 {
		return PaperHandwriting{}, apperr.New(apperr.BadRequest, "文件保存标识无效")
	}
	if inputKind == "voice" {
		if len(keys) != 1 {
			return PaperHandwriting{}, apperr.New(apperr.BadRequest, "每次只能登记一段录音")
		}
		if err := ownVoice(userID, keys[0]); err != nil {
			return PaperHandwriting{}, err
		}
		info, err := s.oss.Head(ctx, keys[0])
		if err != nil || !info.Exists {
			return PaperHandwriting{}, apperr.NotFoundErr()
		}
		if info.Size <= 0 || info.Size > 10<<20 {
			return PaperHandwriting{}, apperr.New(apperr.BadRequest, "录音为空或超过 10MB")
		}
	} else if err := ownPhotos(userID, keys); err != nil {
		return PaperHandwriting{}, err
	}
	var id uint64
	err := s.withTx(ctx, func(q *dbq.Queries) error {
		row, err := s.paperSessionRow(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		all, err := q.ListPaperHandwriting(ctx, dbq.ListPaperHandwritingParams{PaperSessionID: sessionID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		// 请求响应丢失后仍能找回原任务，即使已经交卷也不重新登记照片。
		for _, v := range all {
			if int(v.Seq) == seq && v.IdempotencyKey == key {
				var old []string
				_ = json.Unmarshal(v.ObjectKeys, &old)
				if v.InputKind != inputKind || !slices.Equal(old, keys) {
					return apperr.New(apperr.Conflict, "文件保存标识已用于其他文件")
				}
				id = v.ID
				return nil
			}
		}
		if !open(row.Status) || s.expired(row) {
			return apperr.New(apperr.Conflict, "考试已结束，不能再增加照片或录音")
		}
		items, err := q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: sessionID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		found := false
		for _, it := range items {
			if int(it.Seq) == seq {
				found = true
			}
		}
		if !found {
			return apperr.NotFoundErr()
		}
		n := len(keys)
		used := map[string]bool{}
		for _, v := range all {
			if int(v.Seq) == seq && v.Status != "discarded" && v.InputKind == inputKind {
				pv := paperHandwritingView(v)
				n += len(pv.Keys)
				for _, k := range pv.Keys {
					used[k] = true
				}
			}
		}
		if inputKind == "voice" && n > 30 {
			return apperr.New(apperr.BadRequest, "一道题最多保存 30 段录音")
		}
		if inputKind == "photo" && n > handwritingMax {
			return apperr.New(apperr.BadRequest, "一道题最多拍 6 张")
		}
		for _, k := range keys {
			if used[k] {
				return apperr.New(apperr.BadRequest, "文件已经保存，请勿重复上传")
			}
			used[k] = true
		}
		b, _ := json.Marshal(keys)
		v, err := q.InsertPaperHandwriting(ctx, dbq.InsertPaperHandwritingParams{OwnerUserID: userID, PaperSessionID: sessionID, Seq: uint16(seq), IdempotencyKey: key, CaptureOrder: uint64(captureOrder), ObjectKeys: b, InputKind: inputKind})
		id = uint64(v)
		return err
	})
	if err != nil {
		return PaperHandwriting{}, err
	}
	r, err := s.q.GetPaperHandwriting(ctx, dbq.GetPaperHandwritingParams{ID: id, OwnerUserID: userID})
	if err != nil {
		return PaperHandwriting{}, err
	}
	if r.Status == "pending" || r.Status == "failed" {
		if err := s.enqueuePaperHandwriting(ctx, userID, id); err != nil {
			_ = s.q.FailPaperHandwriting(ctx, dbq.FailPaperHandwritingParams{ID: id, OwnerUserID: userID, ErrorMessage: "原文件已保存，后台识别暂未启动，请重试"})
		}
	}
	r, err = s.q.GetPaperHandwriting(ctx, dbq.GetPaperHandwritingParams{ID: id, OwnerUserID: userID})
	return paperHandwritingView(r), err
}
func (s *Service) enqueuePaperHandwriting(ctx context.Context, userID, id uint64) error {
	if s.queue == nil {
		return s.RecognizePaperHandwriting(ctx, userID, id)
	}
	taskID := fmt.Sprintf("%s:%d", jobs.TypePaperHandwriting, id)
	if s.gradeTasks != nil {
		info, err := s.gradeTasks.GetTaskInfo(jobs.QueueCritical, taskID)
		if err == nil {
			switch info.State {
			case asynq.TaskStateArchived, asynq.TaskStateRetry, asynq.TaskStateScheduled:
				return s.gradeTasks.RunTask(jobs.QueueCritical, taskID)
			case asynq.TaskStateCompleted:
				if err := s.gradeTasks.DeleteTask(jobs.QueueCritical, taskID); err != nil {
					return err
				}
			default:
				return nil
			}
		} else if !errors.Is(err, asynq.ErrTaskNotFound) && !errors.Is(err, asynq.ErrQueueNotFound) {
			return err
		}
	}
	t, err := jobs.NewPaperHandwritingTask(userID, id)
	if err != nil {
		return err
	}
	_, err = s.queue.EnqueueContext(ctx, t)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// 独占一张稿件的识别，避免 Worker 和交卷补偿同时调用大模型。连接关闭/进程退出时 MySQL 自动释放锁。
func (s *Service) RecognizePaperHandwriting(ctx context.Context, userID, id uint64) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	name := fmt.Sprintf("training:paper-hw:%d", id)
	var locked int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 175)", name).Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return errors.New("文件仍在识别，请稍后重试")
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(release, "SELECT RELEASE_LOCK(?)", name)
	}()
	r, err := s.q.GetPaperHandwriting(ctx, dbq.GetPaperHandwritingParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Status != "pending" && r.Status != "failed" {
		return nil
	}
	row, err := s.paperSessionRow(ctx, s.q, userID, r.PaperSessionID, false)
	if err != nil {
		return err
	}
	if row.Status == dbq.PaperSessionsStatusAbandoned {
		return nil
	}
	v := paperHandwritingView(r)
	var result Handwriting
	if r.InputKind == "voice" {
		result.Text, err = s.transcribeVoice(ctx, userID, v.Keys[0])
	} else {
		result, err = s.Recognize(ctx, userID, v.Keys)
	}
	if err != nil {
		// 不持久化 SDK 错误，避免错误信息带出图片、原文或服务凭证。
		_ = s.q.FailPaperHandwriting(ctx, dbq.FailPaperHandwritingParams{ID: id, OwnerUserID: userID, ErrorMessage: "识别失败，原文件已保存，请重试"})
		return errors.New("识别失败，原文件已保存")
	}
	b, _ := json.Marshal(result.LowConfidence)
	if string(b) == "null" {
		b = []byte("[]")
	}
	return s.q.ReadyPaperHandwriting(ctx, dbq.ReadyPaperHandwritingParams{ID: id, OwnerUserID: userID, RecognizedText: result.Text, LowConfidence: b})
}

func (s *Service) confirmPaperHandwriting(ctx context.Context, userID, sessionID, id uint64, seq int, text *string, automatic bool) error {
	return s.withTx(ctx, func(q *dbq.Queries) error {
		row, err := s.paperSessionRow(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		r, err := q.GetPaperHandwriting(ctx, dbq.GetPaperHandwritingParams{ID: id, OwnerUserID: userID})
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		if r.PaperSessionID != sessionID || int(r.Seq) != seq {
			return apperr.NotFoundErr()
		}
		if r.Status == "confirmed" || r.Status == "discarded" {
			return nil
		}
		if automatic {
			if row.Status != dbq.PaperSessionsStatusGrading {
				return apperr.New(apperr.Conflict, "答卷尚未提交")
			}
		} else if !open(row.Status) || s.expired(row) {
			return apperr.New(apperr.Conflict, "考试已结束，不能再修改识别结果")
		}
		if r.Status != "ready" {
			return apperr.New(apperr.Conflict, "请等待识别完成")
		}
		previous, err := q.ListPaperHandwriting(ctx, dbq.ListPaperHandwritingParams{PaperSessionID: sessionID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		for _, photo := range previous {
			if photo.ID == id {
				break
			}
			if photo.Seq == r.Seq && photo.Status != "confirmed" && photo.Status != "discarded" {
				return apperr.New(apperr.Conflict, "请先处理前面的照片或录音，答案将按保存顺序拼接")
			}
		}
		value := r.RecognizedText
		if text != nil {
			value = *text
		}
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 20000 {
			return apperr.New(apperr.BadRequest, "识别文字不能为空且不能超过 20000 字")
		}
		items, err := q.ListPaperSessionItems(ctx, dbq.ListPaperSessionItemsParams{PaperSessionID: sessionID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		for _, it := range items {
			if int(it.Seq) == seq {
				combined := value
				if strings.TrimSpace(it.DraftText.String) != "" {
					combined = it.DraftText.String + "\n" + value
				}
				if utf8.RuneCountInString(combined) > 20000 {
					return apperr.New(apperr.BadRequest, "本题答案超过 20000 字，请精简后再确认")
				}
				if err := q.AppendPaperHandwriting(ctx, dbq.AppendPaperHandwritingParams{DraftText: sql.NullString{String: combined, Valid: true}, PaperSessionID: sessionID, Seq: uint16(seq), OwnerUserID: userID}); err != nil {
					return err
				}
				return q.ConfirmPaperHandwriting(ctx, dbq.ConfirmPaperHandwritingParams{ID: id, OwnerUserID: userID, RecognizedText: value})
			}
		}
		return apperr.NotFoundErr()
	})
}

// SavePaperHandwritingText 保存核对编辑，但延后追加，避免另一张较早的照片尚在识别时打乱拼接顺序。
func (s *Service) SavePaperHandwritingText(ctx context.Context, userID, sessionID, id uint64, seq int, text string) (PaperItem, error) {
	err := s.withTx(ctx, func(q *dbq.Queries) error {
		row, err := s.paperSessionRow(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		r, err := q.GetPaperHandwriting(ctx, dbq.GetPaperHandwritingParams{ID: id, OwnerUserID: userID})
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		if r.PaperSessionID != sessionID || int(r.Seq) != seq {
			return apperr.NotFoundErr()
		}
		if !open(row.Status) || s.expired(row) || r.Status != "ready" {
			return apperr.New(apperr.Conflict, "考试已结束或识别状态已变化，请刷新")
		}
		if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 20000 {
			return apperr.New(apperr.BadRequest, "识别文字不能为空且不能超过 20000 字")
		}
		return q.EditPaperHandwriting(ctx, dbq.EditPaperHandwritingParams{ID: id, OwnerUserID: userID, RecognizedText: text})
	})
	if err != nil {
		return PaperItem{}, err
	}
	v, err := s.PaperSession(ctx, userID, sessionID)
	if err != nil {
		return PaperItem{}, err
	}
	for _, it := range v.Items {
		if it.Seq == seq {
			return it, nil
		}
	}
	return PaperItem{}, apperr.NotFoundErr()
}

func (s *Service) ConfirmPaperHandwriting(ctx context.Context, userID, sessionID, id uint64, seq int, text string) (PaperItem, error) {
	if err := s.confirmPaperHandwriting(ctx, userID, sessionID, id, seq, &text, false); err != nil {
		return PaperItem{}, err
	}
	v, err := s.PaperSession(ctx, userID, sessionID)
	if err != nil {
		return PaperItem{}, err
	}
	for _, it := range v.Items {
		if it.Seq == seq {
			return it, nil
		}
	}
	return PaperItem{}, apperr.NotFoundErr()
}
func (s *Service) DiscardPaperHandwriting(ctx context.Context, userID, sessionID, id uint64, seq int) error {
	return s.withTx(ctx, func(q *dbq.Queries) error {
		row, err := s.paperSessionRow(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		r, err := q.GetPaperHandwriting(ctx, dbq.GetPaperHandwritingParams{ID: id, OwnerUserID: userID})
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		if r.PaperSessionID != sessionID || int(r.Seq) != seq {
			return apperr.NotFoundErr()
		}
		if !open(row.Status) || s.expired(row) || r.Status == "confirmed" {
			return apperr.New(apperr.Conflict, "不能弃用已经提交的文件")
		}
		return q.DiscardPaperHandwriting(ctx, dbq.DiscardPaperHandwritingParams{ID: id, OwnerUserID: userID})
	})
}

// 交卷不等于放弃未识别的照片：补跑、默认确认后才允许计分，失败交给原有批改恢复流程。
func (s *Service) finishPaperHandwriting(ctx context.Context, userID, sessionID uint64) error {
	rows, err := s.q.ListPaperHandwriting(ctx, dbq.ListPaperHandwritingParams{PaperSessionID: sessionID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Status == "confirmed" || r.Status == "discarded" {
			continue
		}
		if err := s.RecognizePaperHandwriting(ctx, userID, r.ID); err != nil {
			return err
		}
		if err := s.confirmPaperHandwriting(ctx, userID, sessionID, r.ID, int(r.Seq), nil, true); err != nil {
			return err
		}
	}
	return nil
}
