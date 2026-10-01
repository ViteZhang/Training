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
	TypePing          = "system:ping"
	TypePurgeAccounts = "account:purge_deleted"
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

// AccountPurger 物理删除冷静期已到的账号（auth.Service 实现）。
type AccountPurger interface {
	PurgeDue(ctx context.Context, batch int) (int, error)
}

// Handlers 是全部任务处理器的依赖。后续卡片在这里加业务服务。
type Handlers struct {
	Logger *slog.Logger
	Auth   AccountPurger
}

// Mux 注册全部任务处理器。
func (h *Handlers) Mux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(h.withLogger)
	mux.HandleFunc(TypePing, h.handlePing)
	mux.HandleFunc(TypePurgeAccounts, h.handlePurgeAccounts)
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

// handlePurgeAccounts 删除注销冷静期已到的账号（PRD 6.12）。每批 100 个，删不完下个小时继续。
func (h *Handlers) handlePurgeAccounts(ctx context.Context, _ *asynq.Task) error {
	_, err := h.Auth.PurgeDue(ctx, 100)
	return err
}

// Schedule 是一个定时任务：cron 表达式按北京时间。
type Schedule struct {
	Cron string
	Type string
}

// Schedules 是全部定时任务。业务日期按北京时间（CLAUDE.md 必须遵守第 13 条）；
// 每日计划在 T16 加入（每天 0 点）。
var Schedules = []Schedule{
	{Cron: "17 * * * *", Type: TypePurgeAccounts},
}

// RegisterSchedules 注册定时任务。调度器全局只启一个，跑在 Worker 里。
func RegisterSchedules(s *asynq.Scheduler) error {
	for _, sc := range Schedules {
		// 同一时间只允许一个实例在跑（Unique），任务本身也可重复执行。
		if _, err := s.Register(sc.Cron, asynq.NewTask(sc.Type, nil), asynq.Queue(QueueLow), asynq.Unique(time.Hour)); err != nil {
			return fmt.Errorf("注册定时任务 %s：%w", sc.Type, err)
		}
	}
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
