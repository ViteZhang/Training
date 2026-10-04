package monitor_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/monitor"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type queues map[string]*asynq.QueueInfo

func (q queues) GetQueueInfo(name string) (*asynq.QueueInfo, error) {
	if i, ok := q[name]; ok {
		return i, nil
	}
	return nil, errors.New("queue not found")
}

type cost struct{ o admin.AIOverview }

func (c cost) AIOverview(context.Context) (admin.AIOverview, error) { return c.o, nil }

func TestMonitorAlerts(t *testing.T) {
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: env.RedisAddr, DB: env.RedisDB})
	defer rdb.Close()
	rdb.FlushDB(ctx)

	var mu sync.Mutex
	var pushed []string
	hook := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		pushed = append(pushed, string(b))
		mu.Unlock()
	}))
	defer hook.Close()

	c := monitor.New(monitor.Deps{DB: db, Redis: rdb, Log: logx.New(io.Discard, slog.LevelInfo), Webhook: hook.URL, Names: []string{"default", "low", "critical"},
		Queues: queues{"default": {Pending: 500, Latency: time.Minute}, "low": {Pending: 3, Latency: time.Second}},
		Cost:   cost{admin.AIOverview{CostPerActiveUserDay: 1.2, BudgetPerUserDay: 0.8}}})

	// 一切正常时（只有队列与成本有问题之前）先确认请求太少不报错误率。
	counter := monitor.NewHTTPCounter(rdb)
	for range 10 {
		counter.Observe(500)
	}
	// 再来 90 个正常请求：100 个里 10 个 5xx，超过 2%。
	for range 90 {
		counter.Observe(200)
	}
	if _, err := db.Exec(`INSERT INTO ai_calls (capability, model, prompt_version, success) VALUES ` + strings.TrimSuffix(strings.Repeat("('grade_subjective', 'm', 'v1', 0),", 5)+strings.Repeat("('grade_subjective', 'm', 'v1', 1),", 20), ",")); err != nil {
		t.Fatal(err)
	}

	keys := map[string]bool{}
	for _, a := range c.Check(ctx) {
		keys[a.Key] = true
	}
	for _, want := range []string{"http_errors", "queue_backlog:default", "ai_failures", "ai_cost_user"} {
		if !keys[want] {
			t.Errorf("应告警 %s：%v", want, keys)
		}
	}
	if keys["queue_backlog:low"] || len(keys) != 4 {
		t.Errorf("没超阈值的不报：%v", keys)
	}
	mu.Lock()
	if len(pushed) != 4 || !strings.Contains(pushed[0], "考研Training 告警") {
		t.Errorf("推送到群机器人：%v", pushed)
	}
	mu.Unlock()

	// 30 分钟内同一项不重复发。
	if again := c.Check(ctx); len(again) != 0 {
		t.Errorf("30 分钟内不重复：%v", again)
	}
}
