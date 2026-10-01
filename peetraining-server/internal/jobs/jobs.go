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
	TypeExtract       = "material:extract"
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

// MaterialExtractor 是导入流水线第 3 步「取文本」（material.Service 实现）。
type MaterialExtractor interface {
	Extract(ctx context.Context, userID, materialID uint64) error
}

// IsPermanent 判断错误是否重试也不会好（文件本身的问题，原因已写进资料记录）。由 app 包注入。
type IsPermanent func(error) bool

// Handlers 是全部任务处理器的依赖。后续卡片在这里加业务服务。
type Handlers struct {
	Logger    *slog.Logger
	Auth      AccountPurger
	Material  MaterialExtractor
	Permanent IsPermanent
}

// MaterialPayload 指定一份资料。带上 user_id，处理器按归属查询（CLAUDE.md 必须遵守第 4 条）。
type MaterialPayload struct {
	UserID     uint64 `json:"user_id"`
	MaterialID uint64 `json:"material_id"`
}

// NewExtractTask 创建取文本任务。同一份资料同一时间只排一个（TaskID 去重）；网络或识别服务出错最多重试 3 次。
func NewExtractTask(userID, materialID uint64) (*asynq.Task, error) {
	b, err := json.Marshal(MaterialPayload{UserID: userID, MaterialID: materialID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeExtract, b, asynq.Queue(QueueDefault), asynq.MaxRetry(3), asynq.Timeout(10*time.Minute),
		asynq.TaskID(fmt.Sprintf("%s:%d", TypeExtract, materialID))), nil
}

// Mux 注册全部任务处理器。
func (h *Handlers) Mux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(h.withLogger)
	mux.HandleFunc(TypePing, h.handlePing)
	mux.HandleFunc(TypePurgeAccounts, h.handlePurgeAccounts)
	mux.HandleFunc(TypeExtract, h.handleExtract)
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

func (h *Handlers) handleExtract(ctx context.Context, t *asynq.Task) error {
	var p MaterialPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("解析取文本载荷：%w", errors.Join(err, asynq.SkipRetry))
	}
	err := h.Material.Extract(ctx, p.UserID, p.MaterialID)
	if err != nil && h.Permanent != nil && h.Permanent(err) {
		// 原因已写进资料记录（1.6b 显示），任务本身算处理完。
		logx.From(ctx).Info("material extract rejected", "material_id", p.MaterialID)
		return nil
	}
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
