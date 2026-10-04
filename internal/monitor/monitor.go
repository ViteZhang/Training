// Package monitor 是监控告警（T32，dev-spec 第十二节 G4）：接口错误率、队列积压、AI 失败率、AI 成本超预算。
//
// API 每个请求在 Redis 里按分钟计数（总数与 5xx）；Worker 每 5 分钟检查一次，超过阈值时写一条 ERROR 级日志（log_type = alert，
// SLS 按它配告警规则），配了 ALERT_WEBHOOK_URL 时再推送到钉钉或企业微信群机器人。同一项告警 30 分钟内只发一次。
package monitor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/dbq"
)

// 阈值。调整时同步改 docs/runbook.md「监控告警」。
const (
	window        = 15 * time.Minute
	minRequests   = 50   // 请求太少时错误率没有意义
	maxErrorRate  = 0.02 // 5xx 占比
	minAICalls    = 20
	maxAIFailRate = 0.10
	maxPending    = 200              // 单个队列等待中的任务数
	maxLatency    = 10 * time.Minute // 队列里最老的任务等了多久
	silence       = 30 * time.Minute
	counterTTL    = 2 * time.Hour
)

func minuteKey(t time.Time) string { return "metrics:http:" + t.UTC().Format("200601021504") }

// HTTPCounter 在 Redis 里按分钟数请求数与 5xx 数。计数失败不影响请求。
type HTTPCounter struct{ rdb *redis.Client }

func NewHTTPCounter(rdb *redis.Client) *HTTPCounter { return &HTTPCounter{rdb: rdb} }

// Observe 记一次请求。用独立的短超时 context：请求已经结束，不能因为客户端断开而丢计数，也不能拖慢响应太久。
func (h *HTTPCounter) Observe(status int) {
	if h == nil || h.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	key := minuteKey(time.Now())
	p := h.rdb.Pipeline()
	p.HIncrBy(ctx, key, "total", 1)
	if status >= 500 {
		p.HIncrBy(ctx, key, "err", 1)
	}
	p.Expire(ctx, key, counterTTL)
	_, _ = p.Exec(ctx)
}

// QueueInspector 读队列状态（asynq.Inspector 满足它）。
type QueueInspector interface {
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
}

// CostReader 读 7.8 的 AI 成本概览（admin.Service 满足它）。
type CostReader interface {
	AIOverview(ctx context.Context) (admin.AIOverview, error)
}

// Deps 是检查的依赖。
type Deps struct {
	DB      *sql.DB
	Redis   *redis.Client
	Queues  QueueInspector
	Names   []string
	Cost    CostReader
	Log     *slog.Logger
	Webhook string
	Client  *http.Client
	Now     func() time.Time
}

// Checker 做一轮检查并发告警。
type Checker struct {
	d Deps
	q *dbq.Queries
}

func New(d Deps) *Checker {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Client == nil {
		d.Client = &http.Client{Timeout: 5 * time.Second}
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Checker{d: d, q: dbq.New(d.DB)}
}

// Alert 是一项告警。
type Alert struct {
	Key     string
	Message string
}

// Check 跑全部检查，返回触发的告警（已按 30 分钟去重发出）。某项检查自身出错时记日志、继续查其他项。
func (c *Checker) Check(ctx context.Context) []Alert {
	var alerts []Alert
	for _, f := range []func(context.Context) ([]Alert, error){c.httpErrors, c.queues, c.aiFailures, c.aiCost} {
		as, err := f(ctx)
		if err != nil {
			c.d.Log.WarnContext(ctx, "monitor check failed", "err", err)
			continue
		}
		alerts = append(alerts, as...)
	}
	var sent []Alert
	for _, a := range alerts {
		if c.firstIn(ctx, a.Key) {
			c.send(ctx, a)
			sent = append(sent, a)
		}
	}
	return sent
}

func (c *Checker) httpErrors(ctx context.Context) ([]Alert, error) {
	if c.d.Redis == nil {
		return nil, nil
	}
	now := c.d.Now()
	var total, errs int64
	for i := range int(window / time.Minute) {
		m, err := c.d.Redis.HGetAll(ctx, minuteKey(now.Add(-time.Duration(i)*time.Minute))).Result()
		if err != nil {
			return nil, err
		}
		t, _ := strconv.ParseInt(m["total"], 10, 64)
		e, _ := strconv.ParseInt(m["err"], 10, 64)
		total += t
		errs += e
	}
	if total >= minRequests && float64(errs)/float64(total) > maxErrorRate {
		return []Alert{{Key: "http_errors", Message: fmt.Sprintf("近 15 分钟接口错误率 %.1f%%（%d / %d 次请求返回 5xx）", float64(errs)*100/float64(total), errs, total)}}, nil
	}
	return nil, nil
}

func (c *Checker) queues(_ context.Context) ([]Alert, error) {
	if c.d.Queues == nil {
		return nil, nil
	}
	var out []Alert
	for _, name := range c.d.Names {
		info, err := c.d.Queues.GetQueueInfo(name)
		if err != nil {
			// 队列还没出现过任务时 asynq 报不存在，不算异常。
			continue
		}
		if info.Pending > maxPending || info.Latency > maxLatency {
			out = append(out, Alert{Key: "queue_backlog:" + name, Message: fmt.Sprintf("队列 %s 积压：等待 %d 个，最老的已等 %s；检查 worker 是否在跑", name, info.Pending, info.Latency.Round(time.Second))})
		}
	}
	return out, nil
}

func (c *Checker) aiFailures(ctx context.Context) ([]Alert, error) {
	h, err := c.q.AICallHealth(ctx, c.d.Now().UTC().Add(-window))
	if err != nil {
		return nil, err
	}
	if h.Calls >= minAICalls && float64(h.Failed)/float64(h.Calls) > maxAIFailRate {
		return []Alert{{Key: "ai_failures", Message: fmt.Sprintf("近 15 分钟 AI 调用失败率 %.1f%%（%d / %d）；看 7.8 哪个能力在失败，必要时回滚灰度", float64(h.Failed)*100/float64(h.Calls), h.Failed, h.Calls)}}, nil
	}
	return nil, nil
}

func (c *Checker) aiCost(ctx context.Context) ([]Alert, error) {
	if c.d.Cost == nil {
		return nil, nil
	}
	o, err := c.d.Cost.AIOverview(ctx)
	if err != nil {
		return nil, err
	}
	var out []Alert
	if o.BudgetPerUserDay > 0 && o.CostPerActiveUserDay > o.BudgetPerUserDay {
		out = append(out, Alert{Key: "ai_cost_user", Message: fmt.Sprintf("近 7 天每活跃用户每天 AI 成本 ¥%.2f，超过预算 ¥%.2f", o.CostPerActiveUserDay, o.BudgetPerUserDay)})
	}
	if o.RatioMax > 0 && o.MonthRevenueYuan > 0 && o.RevenueRatio > o.RatioMax {
		out = append(out, Alert{Key: "ai_cost_revenue", Message: fmt.Sprintf("本月 AI 成本占收入 %.0f%%，超过上限 %.0f%%", o.RevenueRatio*100, o.RatioMax*100)})
	}
	return out, nil
}

// firstIn 同一项告警 30 分钟内只发一次。没有 Redis 时每次都发。
func (c *Checker) firstIn(ctx context.Context, key string) bool {
	if c.d.Redis == nil {
		return true
	}
	ok, err := c.d.Redis.SetNX(ctx, "alert:"+key, "1", silence).Result()
	return err != nil || ok
}

func (c *Checker) send(ctx context.Context, a Alert) {
	c.d.Log.ErrorContext(ctx, "alert", "log_type", "alert", "alert", a.Key, "detail", a.Message)
	if c.d.Webhook == "" {
		return
	}
	// 钉钉与企业微信群机器人都接受这个格式。
	body, _ := json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": "【考研Training 告警】" + a.Message}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.d.Webhook, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.d.Client.Do(req)
	if err != nil {
		c.d.Log.WarnContext(ctx, "alert webhook failed", "err", err)
		return
	}
	_ = resp.Body.Close()
}

// Run 是定时任务入口。
func (c *Checker) Run(ctx context.Context) error {
	c.Check(ctx)
	return nil
}
