package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisOptions 返回 go-redis 与 Asynq 共用的连接参数。
func RedisOptions(addr, password string, db int) *redis.Options {
	return &redis.Options{Addr: addr, Password: password, DB: db}
}

// OpenRedis 创建客户端并确认可连通。Redis 用于验证码、限频与 Asynq 队列。
func OpenRedis(ctx context.Context, opts *redis.Options) (*redis.Client, error) {
	c := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.Ping(pingCtx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("连接 Redis：%w", err)
	}
	return c, nil
}

// RedisPinger 让 *redis.Client 满足健康检查的 Ping(ctx) 接口。
type RedisPinger struct{ Client *redis.Client }

func (p RedisPinger) Ping(ctx context.Context) error { return p.Client.Ping(ctx).Err() }
