package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// 7.17 后台 AI 解析导入（PRD v3 增补，T47）：团队资料的解析任务，与用户资料的导入任务分开。
const (
	TypeOfficialImport        = "official:import"
	TypeOfficialImportRecover = "official:import_recover"
	// TypeOfficialImportFill 是解析后「一键补全」答案与采分点（T49）。
	TypeOfficialImportFill = "official:import_fill"
)

// OfficialImporter 解析团队上传的资料（official.Service 实现）。
type OfficialImporter interface {
	ProcessImport(ctx context.Context, fileID uint64) error
	ProcessImportFill(ctx context.Context, fileID uint64) error
	RecoverImports(ctx context.Context) error
}

// NewOfficialImportTask 创建解析任务；同一份资料同一时间只排一个。
func NewOfficialImportTask(fileID uint64) (*asynq.Task, error) {
	data, err := json.Marshal(struct {
		ID uint64 `json:"id"`
	}{fileID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeOfficialImport, data, asynq.Queue(QueueLow), asynq.MaxRetry(2), asynq.Timeout(time.Hour),
		asynq.TaskID(fmt.Sprintf("%s:%d:%d", TypeOfficialImport, fileID, time.Now().Unix()/60))), nil
}

func (h *Handlers) handleOfficialImport(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil || payload.ID == 0 {
		return asynq.SkipRetry
	}
	if h.Official == nil {
		return nil
	}
	return h.Official.ProcessImport(ctx, payload.ID)
}

// NewOfficialImportFillTask 创建一键补全任务；同一份资料同一时间只排一个。
func NewOfficialImportFillTask(fileID uint64) (*asynq.Task, error) {
	data, err := json.Marshal(struct {
		ID uint64 `json:"id"`
	}{fileID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeOfficialImportFill, data, asynq.Queue(QueueLow), asynq.MaxRetry(3), asynq.Timeout(time.Hour),
		asynq.TaskID(fmt.Sprintf("%s:%d:%d", TypeOfficialImportFill, fileID, time.Now().Unix()/60))), nil
}

func (h *Handlers) handleOfficialImportFill(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil || payload.ID == 0 {
		return asynq.SkipRetry
	}
	if h.Official == nil {
		return nil
	}
	return h.Official.ProcessImportFill(ctx, payload.ID)
}
