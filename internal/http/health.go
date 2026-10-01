package http

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/logx"
)

// healthTimeout 防止数据库卡住时健康检查一起卡住（Nginx 与流水线会用它判断发布是否成功）。
const healthTimeout = 2 * time.Second

// GetHealth 返回版本号与 MySQL、Redis 连通性；只返回 ok / error，不暴露连接细节。
func (h *Handlers) GetHealth(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), healthTimeout)
	defer cancel()

	var wg sync.WaitGroup
	var mysqlStatus, redisStatus gen.HealthStatus
	wg.Go(func() { mysqlStatus = h.check(ctx, "mysql", h.deps.MySQL) })
	wg.Go(func() { redisStatus = h.check(ctx, "redis", h.deps.Redis) })
	wg.Wait()

	resp := gen.Health{
		Status:  gen.HealthStatusOk,
		Version: h.deps.Version,
		Mysql:   mysqlStatus,
		Redis:   redisStatus,
	}
	code := http.StatusOK
	if mysqlStatus != gen.HealthStatusOk || redisStatus != gen.HealthStatusOk {
		resp.Status = gen.HealthStatusError
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, resp)
}

func (h *Handlers) check(ctx context.Context, name string, p Pinger) gen.HealthStatus {
	if p == nil {
		return gen.HealthStatusError
	}
	if err := p.Ping(ctx); err != nil {
		logx.From(ctx).Warn("health check failed", "dependency", name, "err", err)
		return gen.HealthStatusError
	}
	return gen.HealthStatusOk
}
