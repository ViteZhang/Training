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
	TypeImportFile    = "import:material"
	TypeImportFinish  = "import:finalize"
	TypeDailyPlan     = "plan:daily"
	TypePaperGrade    = "paper:grade"
	TypePaperDeadline = "paper:deadline"
	TypeEssayGrade    = "essay:grade"
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

// Importer 是导入流水线（importer.Service 实现）：逐个文件处理，全部文件结束后整理（配答案、采分点、标签、去重）。
type Importer interface {
	ProcessMaterial(ctx context.Context, userID, jobID, materialID uint64) error
	// GiveUp 在重试用完时把文件标为失败、退回额度，避免进度永远停在「解析中」。
	GiveUp(ctx context.Context, userID, jobID, materialID uint64) error
	Finalize(ctx context.Context, userID, jobID uint64) error
}

// ImportPayload 指定导入任务里的一个文件（MaterialID 为 0 表示整理整个任务）。
type ImportPayload struct {
	UserID     uint64 `json:"user_id"`
	JobID      uint64 `json:"job_id"`
	MaterialID uint64 `json:"material_id,omitempty"`
}

// NewImportFileTask 创建处理一个文件的任务。attempt 是用户手动重试的次数，让重试生成新任务 ID。
func NewImportFileTask(userID, jobID, materialID uint64, attempt int) (*asynq.Task, error) {
	b, err := json.Marshal(ImportPayload{UserID: userID, JobID: jobID, MaterialID: materialID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeImportFile, b, asynq.Queue(QueueDefault), asynq.MaxRetry(3), asynq.Timeout(30*time.Minute),
		asynq.TaskID(fmt.Sprintf("%s:%d:%d:%d", TypeImportFile, jobID, materialID, attempt))), nil
}

// NewImportFinishTask 创建整理任务。多个文件同时结束会各自入队，Unique 去掉排队中的重复，任务本身也可重复执行。
func NewImportFinishTask(userID, jobID uint64) (*asynq.Task, error) {
	b, err := json.Marshal(ImportPayload{UserID: userID, JobID: jobID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeImportFinish, b, asynq.Queue(QueueDefault), asynq.MaxRetry(3), asynq.Timeout(30*time.Minute),
		asynq.Unique(time.Minute)), nil
}

// Handlers 是全部任务处理器的依赖。后续卡片在这里加业务服务。
type Handlers struct {
	Logger    *slog.Logger
	Auth      AccountPurger
	Material  MaterialExtractor
	Permanent IsPermanent
	Import    Importer
	Plan      PlanGenerator
	Paper     PaperGrader
	Essay     EssayGrader
}

// EssayGrader 是作文的后台批改（essay.Service 实现）：提交后约 30 秒批完，完成后发消息。
type EssayGrader interface {
	GradeEssay(ctx context.Context, userID, essayID uint64) error
}

// EssayPayload 指定一次作文批改；Round 是第几次批改（复核重批时加 1，生成新的任务 ID）。
type EssayPayload struct {
	UserID  uint64 `json:"user_id"`
	EssayID uint64 `json:"essay_id"`
	Round   int    `json:"round"`
}

// NewEssayGradeTask 创建作文批改任务（用户在等，放 critical 队列）。同一篇同一轮只排一个。
func NewEssayGradeTask(userID, essayID uint64, round int) (*asynq.Task, error) {
	b, err := json.Marshal(EssayPayload{UserID: userID, EssayID: essayID, Round: round})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeEssayGrade, b, asynq.Queue(QueueCritical), asynq.MaxRetry(2), asynq.Timeout(5*time.Minute),
		asynq.TaskID(fmt.Sprintf("%s:%d:%d", TypeEssayGrade, essayID, round))), nil
}

// PaperGrader 是整卷的后台任务（practice.Service 实现）：交卷后按采分点逐题批改主观题；模拟考试到截止时间自动交卷。
type PaperGrader interface {
	GradePaper(ctx context.Context, userID, sessionID uint64) error
	AutoSubmitPaper(ctx context.Context, userID, sessionID uint64) error
}

// PaperPayload 指定一次整卷作答。
type PaperPayload struct {
	UserID    uint64 `json:"user_id"`
	SessionID uint64 `json:"session_id"`
}

// NewPaperGradeTask 创建整卷批改任务（约 2 分钟，用户在等，放 critical 队列）。同一次交卷只排一个。
func NewPaperGradeTask(userID, sessionID uint64) (*asynq.Task, error) {
	b, err := json.Marshal(PaperPayload{UserID: userID, SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypePaperGrade, b, asynq.Queue(QueueCritical), asynq.MaxRetry(3), asynq.Timeout(20*time.Minute),
		asynq.TaskID(fmt.Sprintf("%s:%d", TypePaperGrade, sessionID))), nil
}

// NewPaperDeadlineTask 创建模拟考试到点自动交卷的任务，在截止时间执行（恢复中断后截止时间变了，用新的时间再排一个）。
func NewPaperDeadlineTask(userID, sessionID uint64, deadline time.Time) (*asynq.Task, error) {
	b, err := json.Marshal(PaperPayload{UserID: userID, SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypePaperDeadline, b, asynq.Queue(QueueCritical), asynq.MaxRetry(5), asynq.Timeout(time.Minute),
		asynq.ProcessAt(deadline), asynq.TaskID(fmt.Sprintf("%s:%d:%d", TypePaperDeadline, sessionID, deadline.Unix()))), nil
}

// PlanGenerator 给所有用户生成今天的计划（plan.Service 实现）。
type PlanGenerator interface {
	GenerateAll(ctx context.Context, batch int) (int, error)
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
	mux.HandleFunc(TypeImportFile, h.handleImportFile)
	mux.HandleFunc(TypeImportFinish, h.handleImportFinish)
	mux.HandleFunc(TypeDailyPlan, h.handleDailyPlan)
	mux.HandleFunc(TypePaperGrade, h.handlePaperGrade)
	mux.HandleFunc(TypePaperDeadline, h.handlePaperDeadline)
	mux.HandleFunc(TypeEssayGrade, h.handleEssayGrade)
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

func (h *Handlers) handleImportFile(ctx context.Context, t *asynq.Task) error {
	var p ImportPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("解析导入载荷：%w", errors.Join(err, asynq.SkipRetry))
	}
	err := h.Import.ProcessMaterial(ctx, p.UserID, p.JobID, p.MaterialID)
	if err != nil && lastAttempt(ctx) {
		if gerr := h.Import.GiveUp(ctx, p.UserID, p.JobID, p.MaterialID); gerr != nil {
			return errors.Join(err, gerr)
		}
	}
	return err
}

func (h *Handlers) handleImportFinish(ctx context.Context, t *asynq.Task) error {
	var p ImportPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("解析导入载荷：%w", errors.Join(err, asynq.SkipRetry))
	}
	return h.Import.Finalize(ctx, p.UserID, p.JobID)
}

// handleDailyPlan 每天 0 点给所有用户生成今日计划（PRD 11.5）。已生成的跳过，重跑不重复生成。
func (h *Handlers) handleDailyPlan(ctx context.Context, _ *asynq.Task) error {
	n, err := h.Plan.GenerateAll(ctx, 200)
	logx.From(ctx).Info("daily plans generated", "users", n)
	return err
}

func (h *Handlers) handlePaperGrade(ctx context.Context, t *asynq.Task) error {
	var p PaperPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("解析整卷载荷：%w", errors.Join(err, asynq.SkipRetry))
	}
	return h.Paper.GradePaper(ctx, p.UserID, p.SessionID)
}

func (h *Handlers) handlePaperDeadline(ctx context.Context, t *asynq.Task) error {
	var p PaperPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("解析整卷载荷：%w", errors.Join(err, asynq.SkipRetry))
	}
	return h.Paper.AutoSubmitPaper(ctx, p.UserID, p.SessionID)
}

func (h *Handlers) handleEssayGrade(ctx context.Context, t *asynq.Task) error {
	var p EssayPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("解析作文载荷：%w", errors.Join(err, asynq.SkipRetry))
	}
	return h.Essay.GradeEssay(ctx, p.UserID, p.EssayID)
}

// lastAttempt 报告这是不是最后一次重试（重试用完后 Asynq 不再执行，需要把状态收尾）。
func lastAttempt(ctx context.Context) bool {
	retry, ok1 := asynq.GetRetryCount(ctx)
	maxRetry, ok2 := asynq.GetMaxRetry(ctx)
	return ok1 && ok2 && retry >= maxRetry
}

// Schedule 是一个定时任务：cron 表达式按北京时间。
type Schedule struct {
	Cron string
	Type string
}

// Schedules 是全部定时任务。业务日期按北京时间（CLAUDE.md 必须遵守第 13 条）；
// 每日计划每天 0 点生成；用户当天首次打开首页时也会补生成，定时任务只是提前准备。
var Schedules = []Schedule{
	{Cron: "17 * * * *", Type: TypePurgeAccounts},
	{Cron: "0 0 * * *", Type: TypeDailyPlan},
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
