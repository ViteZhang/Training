package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hibiken/asynq"
	"peetraining-server/internal/apperr"
	"time"
)

const TypeTextbookImport = "textbook:import"
const TypeTextbookRecover = "textbook:recover"

type TextbookImporter interface {
	ProcessImport(context.Context, uint64) error
	RecoverImports(context.Context) error
	ProcessIndex(context.Context, uint64) error
	RecoverIndexes(context.Context) error
}

func NewTextbookImportTask(id uint64, attempt uint32) (*asynq.Task, error) {
	data, err := json.Marshal(struct {
		ID      uint64 `json:"id"`
		Attempt uint32 `json:"attempt"`
	}{id, attempt})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeTextbookImport, data, asynq.Queue(QueueLow), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour), asynq.Unique(10*time.Minute)), nil
}
func (h *Handlers) handleTextbookImport(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil || payload.ID == 0 {
		return asynq.SkipRetry
	}
	err := h.Textbooks.ProcessImport(ctx, payload.ID)
	if apperr.IsKind(err, apperr.BadRequest) || apperr.IsKind(err, apperr.NotFound) {
		return fmt.Errorf("教材导入不能自动重试: %w", asynq.SkipRetry)
	}
	return err
}

const TypeTextbookIndex = "textbook:index"

func NewTextbookIndexTask(id uint64, attempt uint32) (*asynq.Task, error) {
	data, err := json.Marshal(struct {
		ID      uint64 `json:"id"`
		Attempt uint32 `json:"attempt"`
	}{id, attempt})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeTextbookIndex, data, asynq.Queue(QueueLow), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour), asynq.Unique(10*time.Minute)), nil
}
func (h *Handlers) handleTextbookIndex(ctx context.Context, t *asynq.Task) error {
	var p struct {
		ID uint64 `json:"id"`
	}
	if json.Unmarshal(t.Payload(), &p) != nil || p.ID == 0 {
		return asynq.SkipRetry
	}
	return h.Textbooks.ProcessIndex(ctx, p.ID)
}
