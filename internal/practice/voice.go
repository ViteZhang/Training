package practice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/asr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
)

func (s *Service) voiceEnabled(ctx context.Context, userID uint64) error {
	if s.flags == nil {
		return apperr.NotFoundErr()
	}
	on, err := s.flags.Enabled(ctx, "voice_answer", userID)
	if err != nil {
		return err
	}
	if !on {
		return apperr.NotFoundErr()
	}
	return nil
}
func voicePrefix(userID uint64) string { return fmt.Sprintf("u/%d/voice/", userID) }
func ownVoice(userID uint64, key string) error {
	if !strings.HasPrefix(key, voicePrefix(userID)) || strings.Contains(key, "..") || len(key) > 512 {
		return apperr.NotFoundErr()
	}
	if _, ok := audioExt["audio/"+key[strings.LastIndex(key, ".")+1:]]; !ok && !strings.HasSuffix(key, ".mp3") {
		return apperr.New(apperr.BadRequest, "录音格式不支持")
	}
	return nil
}
func (s *Service) RequestVoiceUpload(ctx context.Context, userID uint64, contentType string, size int64) (UploadTarget, error) {
	if err := s.voiceEnabled(ctx, userID); err != nil {
		return UploadTarget{}, err
	}
	ext, ok := audioExt[contentType]
	if !ok || size <= 0 || size > 10<<20 {
		return UploadTarget{}, apperr.New(apperr.BadRequest, "请录制 5 分钟以内、10MB 以内的语音")
	}
	key := voicePrefix(userID) + strconv.FormatInt(s.now().UnixNano(), 36) + "." + ext
	p, err := s.oss.PresignPut(ctx, key, contentType, size, 15*time.Minute)
	return UploadTarget{ObjectKey: key, URL: p.URL, Headers: p.Headers, ExpiresAt: p.ExpiresAt}, err
}
func (s *Service) transcribeVoice(ctx context.Context, userID uint64, key string) (string, error) {
	if err := ownVoice(userID, key); err != nil {
		return "", err
	}
	info, err := s.oss.Head(ctx, key)
	if err != nil || !info.Exists {
		return "", apperr.NotFoundErr()
	}
	if info.Size <= 0 || info.Size > 10<<20 {
		return "", apperr.New(apperr.BadRequest, "录音为空或超过 10MB")
	}
	if s.asr == nil || s.ai == nil {
		return "", apperr.New(apperr.AIFailed, "语音识别暂不可用，录音已保存")
	}
	return s.ai.TranscribeVoice(ctx, userID, s.asr, asr.Audio{ObjectKey: key, Format: key[strings.LastIndex(key, ".")+1:]})
}

type VoiceTranscription struct {
	ID                                  uint64
	CaptureOrder                        int64
	Key, ObjectKey, Status, Text, Error string
}

func voiceView(v dbq.VoiceTranscription) VoiceTranscription {
	return VoiceTranscription{ID: v.ID, CaptureOrder: v.CaptureOrder, Key: v.IdempotencyKey, ObjectKey: v.ObjectKey, Status: v.Status, Text: v.RecognizedText, Error: v.ErrorMessage}
}
func (s *Service) practiceVoiceItems(ctx context.Context, userID, sessionID, questionID uint64) ([]dbq.VoiceTranscription, error) {
	if err := s.voiceEnabled(ctx, userID); err != nil {
		return nil, err
	}
	_, ids, _, err := s.session(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(ids, questionID) {
		return nil, apperr.NotFoundErr()
	}
	return s.q.ListVoiceTranscriptions(ctx, dbq.ListVoiceTranscriptionsParams{OwnerUserID: userID, PracticeSessionID: sessionID, QuestionID: questionID})
}
func (s *Service) ListPracticeVoice(ctx context.Context, userID, sessionID, questionID uint64) ([]VoiceTranscription, error) {
	rows, err := s.practiceVoiceItems(ctx, userID, sessionID, questionID)
	if err != nil {
		return nil, err
	}
	result := make([]VoiceTranscription, len(rows))
	for i, v := range rows {
		result[i] = voiceView(v)
	}
	return result, nil
}
func (s *Service) StartPracticeVoice(ctx context.Context, userID, sessionID, questionID uint64, key, objectKey string, order int64) (VoiceTranscription, error) {
	if err := s.voiceEnabled(ctx, userID); err != nil {
		return VoiceTranscription{}, err
	}
	if len(key) < 8 || len(key) > 128 || order < 0 || order > 9007199254740991 {
		return VoiceTranscription{}, apperr.New(apperr.BadRequest, "缺少录音保存标识")
	}
	if err := ownVoice(userID, objectKey); err != nil {
		return VoiceTranscription{}, err
	}
	info, err := s.oss.Head(ctx, objectKey)
	if err != nil || !info.Exists {
		return VoiceTranscription{}, apperr.NotFoundErr()
	}
	if info.Size <= 0 || info.Size > 10<<20 {
		return VoiceTranscription{}, apperr.New(apperr.BadRequest, "录音为空或超过 10MB")
	}
	var id uint64
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		row, ids, _, err := s.session(ctx, q, userID, sessionID, true)
		if err != nil {
			return err
		}
		if !slices.Contains(ids, questionID) {
			return apperr.NotFoundErr()
		}
		list, err := q.ListVoiceTranscriptions(ctx, dbq.ListVoiceTranscriptionsParams{OwnerUserID: userID, PracticeSessionID: sessionID, QuestionID: questionID})
		if err != nil {
			return err
		}
		for _, v := range list {
			if v.IdempotencyKey == key {
				if v.ObjectKey != objectKey {
					return apperr.New(apperr.Conflict, "录音标识已使用")
				}
				id = v.ID
				return nil
			}
		}
		if string(row.Status) != "in_progress" {
			return apperr.New(apperr.Conflict, "本组练习已结束")
		}
		if len(list) >= 30 {
			return apperr.New(apperr.BadRequest, "每道题最多保存 30 段录音")
		}
		n, err := q.InsertVoiceTranscription(ctx, dbq.InsertVoiceTranscriptionParams{OwnerUserID: userID, PracticeSessionID: sessionID, QuestionID: questionID, IdempotencyKey: key, ObjectKey: objectKey, CaptureOrder: order})
		id = uint64(n)
		return err
	})
	if err != nil {
		return VoiceTranscription{}, err
	}
	r, err := s.q.GetVoiceTranscription(ctx, dbq.GetVoiceTranscriptionParams{ID: id, OwnerUserID: userID})
	if err != nil {
		return VoiceTranscription{}, err
	}
	if r.Status == "pending" || r.Status == "failed" {
		if err = s.q.PendingVoiceTranscription(ctx, dbq.PendingVoiceTranscriptionParams{ID: id, OwnerUserID: userID}); err != nil {
			return VoiceTranscription{}, err
		}
		if err = s.enqueuePracticeVoice(ctx, userID, id); err != nil {
			_ = s.q.FailVoiceTranscription(ctx, dbq.FailVoiceTranscriptionParams{ID: id, OwnerUserID: userID})
		}
	}
	r, err = s.q.GetVoiceTranscription(ctx, dbq.GetVoiceTranscriptionParams{ID: id, OwnerUserID: userID})
	return voiceView(r), err
}
func (s *Service) enqueuePracticeVoice(ctx context.Context, userID, id uint64) error {
	if s.queue == nil {
		return s.RecognizePracticeVoice(ctx, userID, id)
	}
	taskID := fmt.Sprintf("%s:%d", jobs.TypeVoiceTranscribe, id)
	if s.gradeTasks != nil {
		info, err := s.gradeTasks.GetTaskInfo(jobs.QueueCritical, taskID)
		if err == nil {
			switch info.State {
			case asynq.TaskStateArchived, asynq.TaskStateRetry, asynq.TaskStateScheduled:
				return s.gradeTasks.RunTask(jobs.QueueCritical, taskID)
			case asynq.TaskStateCompleted:
				if err = s.gradeTasks.DeleteTask(jobs.QueueCritical, taskID); err != nil {
					return err
				}
			default:
				return nil
			}
		} else if !errors.Is(err, asynq.ErrTaskNotFound) && !errors.Is(err, asynq.ErrQueueNotFound) {
			return err
		}
	}
	t, err := jobs.NewVoiceTask(userID, id)
	if err != nil {
		return err
	}
	_, err = s.queue.EnqueueContext(ctx, t)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}
func (s *Service) RecognizePracticeVoice(ctx context.Context, userID, id uint64) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	name := fmt.Sprintf("training:voice:%d", id)
	var locked int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 175)", name).Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return errors.New("语音仍在识别")
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(release, "SELECT RELEASE_LOCK(?)", name)
	}()
	r, err := s.q.GetVoiceTranscription(ctx, dbq.GetVoiceTranscriptionParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Status != "pending" && r.Status != "failed" {
		return nil
	}
	text, err := s.transcribeVoice(ctx, userID, r.ObjectKey)
	if err != nil {
		_ = s.q.FailVoiceTranscription(ctx, dbq.FailVoiceTranscriptionParams{ID: id, OwnerUserID: userID})
		return errors.New("语音识别失败，录音已保存")
	}
	return s.q.ReadyVoiceTranscription(ctx, dbq.ReadyVoiceTranscriptionParams{ID: id, OwnerUserID: userID, RecognizedText: text})
}
func (s *Service) DiscardPracticeVoice(ctx context.Context, userID, sessionID, questionID, id uint64) error {
	if _, err := s.practiceVoiceItems(ctx, userID, sessionID, questionID); err != nil {
		return err
	}
	r, err := s.q.GetVoiceTranscription(ctx, dbq.GetVoiceTranscriptionParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	if r.PracticeSessionID != sessionID || r.QuestionID != questionID {
		return apperr.NotFoundErr()
	}
	return s.q.DiscardVoiceTranscription(ctx, dbq.DiscardVoiceTranscriptionParams{ID: id, OwnerUserID: userID})
}
