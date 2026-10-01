// Package jobs 定义 Asynq 任务、处理器与定时任务。
//
// 规矩（CLAUDE.md 必须遵守第 12 条）：任务必须可重复执行；入队在数据库事务提交之后；
// 任务状态同时记在 MySQL（具体业务任务表在各卡里加）。
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/logx"
)

// 队列与权重：用户在等的任务（批改）优先于后台批量任务（解析、导出、统计）。
const (
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

// Queues 是 Worker 消费的队列与权重。
var Queues = map[string]int{QueueCritical: 6, QueueDefault: 3, QueueLow: 1}

// 任务类型。命名为「领域:动作」。
const (
	TypePing = "system:ping"
)

// PingPayload 用于检查「API 入队 → Worker 执行」整条链路。
type PingPayload struct {
	Message string `json:"message"`
}

// NewPingTask 创建 ping 任务。
func NewPingTask(message string) (*asynq.Task, error) {
	b, err := json.Marshal(PingPayload{Message: message})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypePing, b, asynq.Queue(QueueLow), asynq.MaxRetry(1), asynq.Timeout(10*time.Second)), nil
}

// Handlers 是全部任务处理器的依赖。后续卡片在这里加业务服务。
type Handlers struct {
	Logger *slog.Logger
}

// Mux 注册全部任务处理器。
func (h *Handlers) Mux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(h.withLogger)
	mux.HandleFunc(TypePing, h.handlePing)
	return mux
}

// withLogger 给每个任务带上类型与 ID 的日志器，并记录耗时与结果。
func (h *Handlers) withLogger(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		id, _ := asynq.GetTaskID(ctx)
		retry, _ := asynq.GetRetryCount(ctx)
		l := h.Logger.With("task_type", t.Type(), "task_id", id, "retry", retry)
		ctx = logx.WithLogger(ctx, l)
		start := time.Now()
		err := next.ProcessTask(ctx, t)
		if err != nil {
			l.Error("task failed", "err", err, "duration_ms", time.Since(start).Milliseconds())
		} else {
			l.Info("task done", "duration_ms", time.Since(start).Milliseconds())
		}
		return err
	})
}

func (h *Handlers) handlePing(ctx context.Context, t *asynq.Task) error {
	var p PingPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		// 载荷坏了重试也没用，直接放弃。
		return fmt.Errorf("解析 ping 载荷：%w", errors.Join(err, asynq.SkipRetry))
	}
	logx.From(ctx).Info("ping", "message", p.Message)
	return nil
}

// RegisterSchedules 注册定时任务。业务日期按北京时间（CLAUDE.md 必须遵守第 13 条），
// 例如每日计划在 T16 注册为每天 0 点（Asia/Shanghai）。调度器全局只启一个，跑在 Worker 里。
func RegisterSchedules(_ *asynq.Scheduler) error {
	return nil
}

// Shanghai 是业务日期使用的时区。
var Shanghai = mustLoadLocation("Asia/Shanghai")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// 容器镜像里没有时区数据时回退为固定 +8，中国不实行夏令时，结果一致。
		return time.FixedZone(name, 8*3600)
	}
	return loc
}

// AsynqLogger 把 Asynq 的日志转到 slog。
type AsynqLogger struct{ L *slog.Logger }

func (a AsynqLogger) Debug(args ...any) { a.L.Debug(fmt.Sprint(args...)) }
func (a AsynqLogger) Info(args ...any)  { a.L.Info(fmt.Sprint(args...)) }
func (a AsynqLogger) Warn(args ...any)  { a.L.Warn(fmt.Sprint(args...)) }
func (a AsynqLogger) Error(args ...any) { a.L.Error(fmt.Sprint(args...)) }
func (a AsynqLogger) Fatal(args ...any) { a.L.Error(fmt.Sprint(args...)) }
