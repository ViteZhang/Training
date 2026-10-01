package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/config"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	env := testenv.New(t)
	t.Setenv("APP_ENV", config.EnvTest)
	t.Setenv("MYSQL_DSN", env.MySQLDSN)
	t.Setenv("REDIS_ADDR", env.RedisAddr)
	t.Setenv("REDIS_DB", strconv.Itoa(env.RedisDB))
	t.Setenv("SHUTDOWN_TIMEOUT", "5s")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// syncBuffer 让并发写日志的 Worker 与读日志的测试不互相干扰。
type syncBuffer struct {
	ch chan string
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.ch <- string(p)
	return len(p), nil
}

// T01 验收：连真实 MySQL 与 Redis，健康检查返回 200 且两者正常；迁移可重复执行。
func TestIntegrationHealthAndMigrations(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	log := logx.New(io.Discard, slog.LevelDebug)

	for i := range 2 {
		if err := Migrate(ctx, cfg, log, "up"); err != nil {
			t.Fatalf("第 %d 次 migrate up：%v", i+1, err)
		}
	}
	if err := Migrate(ctx, cfg, log, "status"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, cfg, log, "down"); err == nil {
		t.Error("未知操作应报错")
	}

	b, err := Open(ctx, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	var tz string
	if err := b.DB.QueryRowContext(ctx, "SELECT @@session.time_zone").Scan(&tz); err != nil || tz != "+00:00" {
		t.Errorf("会话时区应为 UTC，got %q %v", tz, err)
	}

	handler, err := b.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var h gen.Health
	_ = json.NewDecoder(resp.Body).Decode(&h)
	if resp.StatusCode != http.StatusOK || h.Mysql != gen.HealthStatusOk || h.Redis != gen.HealthStatusOk {
		t.Fatalf("health = %d %+v", resp.StatusCode, h)
	}

	// 数据库断开后健康检查返回 503。
	_ = b.DB.Close()
	resp2, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("数据库断开后应返回 503，got %d", resp2.StatusCode)
	}
	b.DB, _ = store.OpenMySQL(ctx, cfg.MySQLDSN) // 让 defer Close 正常
}

// 「API 入队 → Worker 执行」整条链路，以及 Worker 优雅停机。
func TestIntegrationWorkerProcessesPing(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	logs := &syncBuffer{ch: make(chan string, 1024)}
	log := logx.New(logs, slog.LevelDebug)

	b, err := Open(ctx, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	w, err := NewWorker(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	task, _ := jobs.NewPingTask("integration-ping")
	if _, err := b.Queue.EnqueueContext(ctx, task); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(20 * time.Second)
	for {
		select {
		case line := <-logs.ch:
			if strings.Contains(line, "task done") && strings.Contains(line, jobs.TypePing) {
				return
			}
		case <-deadline:
			t.Fatal("20 秒内 Worker 没有处理 ping 任务")
		}
	}
}

// RunAPI 在 ctx 取消后优雅退出。
func TestIntegrationRunAPIGracefulShutdown(t *testing.T) {
	cfg := testConfig(t)
	cfg.HTTPAddr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunAPI(ctx, cfg, logx.New(io.Discard, slog.LevelDebug)) }()
	time.Sleep(500 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunAPI 没有在 10 秒内停止")
	}
}

// T06：经 HTTP 走一遍登录 → 带令牌访问 → 退出。
func TestIntegrationLoginOverHTTP(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	log := logx.New(io.Discard, slog.LevelDebug)
	if err := Migrate(ctx, cfg, log, "up"); err != nil {
		t.Fatal(err)
	}
	b, err := Open(ctx, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Redis.FlushDB(ctx)
	handler, err := b.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	post := func(path, token, body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	get := func(path, token string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post("/api/v1/auth/sms-codes", "", `{"phone":"13812345678","purpose":"login","agree":true}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("发码：%d", resp.StatusCode)
	}
	code, _ := b.Cloud.SMS.(interface{ LastCode(string) (string, bool) }).LastCode("13812345678")
	resp = post("/api/v1/auth/login", "", `{"phone":"13812345678","code":"`+code+`","device":{"device_id":"device-0001","platform":"ios","device_name":"iPhone"}}`)
	var login gen.LoginResponse
	_ = json.NewDecoder(resp.Body).Decode(&login)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !login.IsNewUser || login.User.PhoneMasked != "138****5678" {
		t.Fatalf("登录：%d %+v", resp.StatusCode, login)
	}

	if r := get("/api/v1/me", ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("没带令牌应 401：%d", r.StatusCode)
	}
	r := get("/api/v1/me", login.AccessToken)
	var me gen.Me
	_ = json.NewDecoder(r.Body).Decode(&me)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || me.Id != login.User.Id {
		t.Fatalf("/me：%d %+v", r.StatusCode, me)
	}
	r = get("/api/v1/bootstrap?platform=ios&app_version=1.0.0", login.AccessToken)
	var boot gen.Bootstrap
	_ = json.NewDecoder(r.Body).Decode(&boot)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || boot.LoggedIn == nil || !*boot.LoggedIn || boot.AppName == "" {
		t.Fatalf("/bootstrap：%d %+v", r.StatusCode, boot)
	}
	if r := get("/api/v1/bootstrap?platform=ios&app_version=1.0.0", ""); r.StatusCode != http.StatusOK {
		t.Fatalf("/bootstrap 未登录也可调用：%d", r.StatusCode)
	}
	if r := post("/api/v1/auth/logout", login.AccessToken, ""); r.StatusCode != http.StatusNoContent {
		t.Fatalf("退出：%d", r.StatusCode)
	}
	r = post("/api/v1/auth/refresh", "", `{"refresh_token":"`+login.RefreshToken+`","device_id":"device-0001"}`)
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("退出后刷新应 401：%d", r.StatusCode)
	}
}
