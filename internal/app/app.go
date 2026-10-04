// Package app 组装依赖并运行 API 与 Worker。
//
// API 与 Worker 是同一套代码的两种启动方式（docs/tech-plan.md「架构」），共用配置、存储与云服务。
package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/auth"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/cloud"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/cloud/pay"
	"peetraining-server/internal/config"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/essay"
	"peetraining-server/internal/events"
	"peetraining-server/internal/export"
	"peetraining-server/internal/feedback"
	"peetraining-server/internal/flags"
	apihttp "peetraining-server/internal/http"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/invite"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/material"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/monitor"
	"peetraining-server/internal/notify"
	"peetraining-server/internal/official"
	"peetraining-server/internal/params"
	"peetraining-server/internal/payment"
	"peetraining-server/internal/plan"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/score"
	"peetraining-server/internal/store"
)

// Version 在构建时用 -ldflags "-X peetraining-server/internal/app.Version=v0.1.0" 注入。
var Version = "dev"

// NewLogger 创建写到标准输出的 JSON 日志器（容器日志由 SLS 采集）。
func NewLogger(cfg *config.Config) *slog.Logger {
	level := slog.LevelInfo
	if !cfg.IsProduction() {
		level = slog.LevelDebug
	}
	return logx.New(os.Stdout, level).With("version", Version, "env", cfg.AppEnv)
}

// Base 是 API 与 Worker 共用的依赖。
type Base struct {
	Config   *config.Config
	Logger   *slog.Logger
	DB       *sql.DB
	Redis    *redis.Client
	Cloud    *cloud.Clients
	Queue    *asynq.Client
	Params   *params.Store
	Flags    *flags.Service
	Auth     *auth.Service
	Profile  *profile.Service
	Quota    *quota.Service
	Material *material.Service
	AI       *ai.Engine
	Importer *importer.Service
	Bank     *bank.Service
	Plan     *plan.Service
	Practice *practice.Service
	Score    *score.Service
	Essay    *essay.Service
	// T24 我的。
	Membership *membership.Service
	Feedback   *feedback.Service
	Export     *export.Service
	// T25 会员中心与支付。
	Payment *payment.Service
	// T26 邀请研友。
	Invite *invite.Service
	// T27 消息中心。
	Notify *notify.Service
	// T28 管理后台。
	Admin    *admin.Service
	Official *official.Service
}

// Open 建立数据库、Redis、队列与云服务客户端。任一失败都关闭已打开的资源并返回错误。
func Open(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Base, error) {
	clients, err := cloud.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("初始化云服务：%w", err)
	}
	db, err := store.OpenMySQL(ctx, cfg.MySQLDSN)
	if err != nil {
		return nil, err
	}
	redisOpts := store.RedisOptions(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	rdb, err := store.OpenRedis(ctx, redisOpts)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	q := dbq.New(db)
	ps := params.New(q)
	qs := quota.New(q, ps, nil)
	fl := flags.New(q)
	mat := material.New(material.Deps{
		DB: db, OSS: clients.OSS, Moderation: clients.Moderation, Quota: qs, Params: ps,
		OCR: clients.OCR, PDF: clients.PDF, Flags: fl,
	})
	salt := sha256.Sum256([]byte("ai-calls:" + cfg.JWTSecret))
	models, fallback := ai.Routing(cfg.AI, clients.AIFallback)
	engine := ai.NewEngine(ai.Config{
		Client: clients.AI, Fallback: fallback, Queries: q, UseMock: cfg.AI.Provider == config.ProviderMock,
		Models: models, Salt: hex.EncodeToString(salt[:]), Prices: ai.PricesFrom(cfg.AI), Moderation: clients.Moderation,
	})
	queue := asynq.NewClientFromRedisClient(rdb)
	prof := profile.New(db, ps, clients.OSS, nil)
	bk := bank.New(bank.Deps{DB: db, AI: engine, Quota: qs, Params: ps})
	pl := plan.New(plan.Deps{DB: db, Params: ps, Profile: prof, Bank: bk})
	imp := importer.New(importer.Deps{DB: db, Material: mat, Quota: qs, AI: engine, Queue: queue})
	// 确认入库后立即重排今日计划（PRD 1.8）。
	imp.AfterConfirm = func(ctx context.Context, userID, _ uint64) bool { return pl.Regenerate(ctx, userID) }
	// 被邀请人第一次确认导入资料后，双方各得会员天数（T26）。
	inv := invite.New(db, ps, fl, nil)
	imp.OnConfirmedTx = func(ctx context.Context, q *dbq.Queries, userID uint64) error { return inv.Activate(ctx, q, userID) }
	sc := score.New(score.Deps{DB: db, Params: ps, Profile: prof, Bank: bk, Plan: pl})
	// 删除资料不删除整卷成绩，但会触发预估分重算（PRD 11.6）；失败只记日志。
	mat.OnDeleted = func(ctx context.Context, userID, subjectID uint64) {
		if err := sc.Recompute(ctx, userID, subjectID, "material_deleted"); err != nil {
			log.Warn("recompute estimate", "err", err)
		}
	}
	base := &Base{
		Config: cfg,
		Logger: log,
		DB:     db,
		Redis:  rdb,
		Cloud:  clients,
		Queue:  queue,
		Params: ps,
		Flags:  fl,
		Auth: auth.New(auth.Deps{
			DB: db, Redis: rdb, SMS: clients.SMS, OSS: clients.OSS, Params: ps,
			JWTSecret: cfg.JWTSecret, Logger: log,
			LogCodes:   !cfg.IsProduction() && cfg.SMS.Provider == config.ProviderMock,
			OnRegister: inv.Bind,
		}),
		Profile:    prof,
		Quota:      qs,
		Material:   mat,
		AI:         engine,
		Importer:   imp,
		Bank:       bk,
		Plan:       pl,
		Practice:   practice.New(practice.Deps{DB: db, Params: ps, Plan: pl, AI: engine, Quota: qs, OSS: clients.OSS, OCR: clients.OCR, Moderation: clients.Moderation, ASR: clients.ASR, Flags: fl, Queue: queue, Score: sc}),
		Score:      sc,
		Essay:      essay.New(essay.Deps{DB: db, Params: ps, AI: engine, Quota: qs, Queue: queue, Score: sc}),
		Membership: membership.New(membership.Deps{DB: db, Redis: rdb}),
		Feedback:   feedback.New(db, nil),
		Export:     export.New(export.Deps{DB: db, OSS: clients.OSS, Queue: queue, Fonts: export.Fonts{CJK: cfg.ExportFontPath, Latin: cfg.ExportLatinFontPath}}),
		Invite:     inv,
		Notify:     notify.New(db, nil),
		Payment: payment.New(payment.Deps{DB: db, Params: ps, Quota: qs, Flags: fl, Gateway: clients.Pay, NotifyBaseURL: cfg.Pay.NotifyBaseURL,
			AppleBundleID: cfg.Pay.AppleBundleID, Log: log}),
	}
	base.Admin = admin.New(admin.Deps{DB: db, Redis: rdb, SMS: clients.SMS, OSS: clients.OSS, Params: ps, Quota: qs, Notify: base.Notify, Payment: base.Payment,
		Import: imp, Log: log, Invalidate: []admin.Invalidator{ps, fl}, LogCodes: !cfg.IsProduction() && cfg.SMS.Provider == config.ProviderMock})
	base.Official = official.New(official.Deps{DB: db, AI: engine, Log: log})
	return base, nil
}

// CreateAdmin 新建后台账号（命令行；第一个管理员用它建，之后在 7.15 管理）。
func CreateAdmin(ctx context.Context, cfg *config.Config, log *slog.Logger, username, displayName, phone string, roles []string, password string) error {
	b, err := Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer b.Close()
	rs := make([]admin.Role, len(roles))
	for i, r := range roles {
		rs[i] = admin.Role(strings.TrimSpace(r))
	}
	id, err := b.Admin.CreateAdmin(ctx, username, displayName, phone, password, rs, true)
	if err != nil {
		return err
	}
	log.Info("admin created", "id", id, "username", username)
	return nil
}

// Close 释放资源。Queue 与 Redis 共用连接，只关一次。
func (b *Base) Close() error {
	return errors.Join(b.Redis.Close(), b.DB.Close())
}

// asynqRedisOpt 返回 Asynq 服务端用的 Redis 连接参数。
func asynqRedisOpt(cfg *config.Config) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB}
}

// AppName 是 App 显示名，由环境变量 APP_NAME 配置（不写死）。
func appName() string {
	if v := os.Getenv("APP_NAME"); v != "" {
		return v
	}
	return "考研Training"
}

// Handler 组装 API 的 HTTP 处理器。
func (b *Base) Handler() (http.Handler, error) {
	return apihttp.NewRouter(apihttp.Deps{
		Logger:     b.Logger,
		Version:    Version,
		AppName:    appName(),
		MySQL:      store.SQLPinger{DB: b.DB},
		Redis:      store.RedisPinger{Client: b.Redis},
		Auth:       b.Auth,
		Flags:      b.Flags,
		Profile:    b.Profile,
		Material:   b.Material,
		Quota:      b.Quota,
		Importer:   b.Importer,
		Bank:       b.Bank,
		Plan:       b.Plan,
		Practice:   b.Practice,
		Score:      b.Score,
		Essay:      b.Essay,
		Membership: b.Membership,
		Feedback:   b.Feedback,
		Export:     b.Export,
		DevOSS:     b.devOSS(),
		Payment:    b.Payment,
		Invite:     b.Invite,
		Notify:     b.Notify,
		Admin:      b.Admin,
		Official:   b.Official,
		Events:     events.New(b.Logger, nil),
		Metrics:    monitor.NewHTTPCounter(b.Redis),
		DevMockPay: b.devMockPay(),
	})
}

// RunAPI 启动 HTTP 服务，ctx 取消后优雅停机：不再接新请求，等进行中的请求在 ShutdownTimeout 内完成。
func RunAPI(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	b, err := Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer b.Close()

	h, err := b.Handler()
	if err != nil {
		return err
	}
	srv := apihttp.NewServer(cfg.HTTPAddr, h)
	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("HTTP 服务异常退出：%w", err)
	case <-ctx.Done():
	}

	log.Info("api shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅停机：%w", err)
	}
	log.Info("api stopped")
	return nil
}

// Worker 是 Asynq 服务端与调度器。
type Worker struct {
	server    *asynq.Server
	scheduler *asynq.Scheduler
	mux       *asynq.ServeMux
}

// NewWorker 创建 Worker。调度器全局只能有一个实例，生产上只部署一个 worker 容器。
func NewWorker(b *Base) (*Worker, error) {
	opt := asynqRedisOpt(b.Config)
	alog := jobs.AsynqLogger{L: b.Logger}
	server := asynq.NewServer(opt, asynq.Config{
		Concurrency:     10,
		Queues:          jobs.Queues,
		ShutdownTimeout: b.Config.ShutdownTimeout,
		Logger:          alog,
		BaseContext:     func() context.Context { return logx.WithLogger(context.Background(), b.Logger) },
	})
	scheduler := asynq.NewScheduler(opt, &asynq.SchedulerOpts{Location: jobs.Shanghai, Logger: alog})
	if err := jobs.RegisterSchedules(scheduler); err != nil {
		return nil, fmt.Errorf("注册定时任务：%w", err)
	}
	h := &jobs.Handlers{Logger: b.Logger, Auth: b.Auth, Material: b.Material, Permanent: material.IsPermanent, Import: b.Importer, Plan: b.Plan, Paper: b.Practice, Essay: b.Essay, Export: b.Export, Notify: b.Notify, Stats: b.Admin,
		Monitor: monitor.New(monitor.Deps{DB: b.DB, Redis: b.Redis, Queues: asynq.NewInspector(opt), Names: []string{jobs.QueueCritical, jobs.QueueDefault, jobs.QueueLow},
			Cost: b.Admin, Log: b.Logger, Webhook: b.Config.AlertWebhookURL})}
	return &Worker{server: server, scheduler: scheduler, mux: h.Mux()}, nil
}

// Start 启动任务消费与调度。
func (w *Worker) Start() error {
	if err := w.server.Start(w.mux); err != nil {
		return fmt.Errorf("启动 Worker：%w", err)
	}
	if err := w.scheduler.Start(); err != nil {
		w.server.Shutdown()
		return fmt.Errorf("启动调度器：%w", err)
	}
	return nil
}

// Stop 先停调度器，再等进行中的任务在 ShutdownTimeout 内完成；没完成的会回到队列重试。
func (w *Worker) Stop() {
	w.scheduler.Shutdown()
	w.server.Shutdown()
}

// RunWorker 启动 Worker，ctx 取消后优雅停机。
func RunWorker(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	b, err := Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer b.Close()

	w, err := NewWorker(b)
	if err != nil {
		return err
	}
	if err := w.Start(); err != nil {
		return err
	}
	log.Info("worker started")
	<-ctx.Done()
	log.Info("worker shutting down", "timeout", cfg.ShutdownTimeout.String())
	w.Stop()
	log.Info("worker stopped")
	return nil
}

// Migrate 执行 up 或 status。
func Migrate(ctx context.Context, cfg *config.Config, log *slog.Logger, action string) error {
	db, err := store.OpenMySQL(ctx, cfg.MySQLDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	switch action {
	case "up":
		return store.MigrateUp(ctx, db, log)
	case "status":
		return store.MigrateStatus(ctx, db, log)
	default:
		return fmt.Errorf("未知的迁移操作 %q（可用：up、status）", action)
	}
}

// devOSS 返回本地 mock OSS，用来注册直传入口；生产环境永远返回 nil。
// devMockPay 只在非生产环境、mock 支付渠道时注册「模拟支付成功」入口。
func (b *Base) devMockPay() bool {
	_, ok := b.Cloud.Pay.(*pay.Mock)
	return ok && !b.Config.IsProduction()
}

func (b *Base) devOSS() *oss.Mock {
	if b.Config.IsProduction() {
		return nil
	}
	m, _ := b.Cloud.OSS.(*oss.Mock)
	return m
}
