// Package testenv 用 testcontainers 起真实的 MySQL 与 Redis，供集成测试使用。
//
// 同一个测试二进制里只起一次（sync.Once），各测试用独立的数据库名隔离。
// 没有 Docker 时跳过集成测试并给出提示；设置 REQUIRE_INTEGRATION=1 时改为失败（流水线用）。
package testenv

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"peetraining-server/internal/store"
)

// Env 是一套测试用的连接参数。
type Env struct {
	MySQLDSN  string // 指向本测试独占的数据库
	RedisAddr string
	RedisDB   int
}

var (
	once      sync.Once
	rootDSN   string
	redisAddr string
	startErr  error
	dbSeq     atomic.Int64
)

func start() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mc, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("training"),
		tcmysql.WithUsername("root"),
		tcmysql.WithPassword("test"),
	)
	if err != nil {
		startErr = fmt.Errorf("启动 MySQL 容器：%w", err)
		return
	}
	rootDSN, err = mc.ConnectionString(ctx)
	if err != nil {
		startErr = err
		return
	}

	rc, err := tcredis.Run(ctx, "redis:7")
	if err != nil {
		startErr = fmt.Errorf("启动 Redis 容器：%w", err)
		return
	}
	uri, err := rc.ConnectionString(ctx)
	if err != nil {
		startErr = err
		return
	}
	redisAddr = strings.TrimPrefix(uri, "redis://")
	// 容器由 testcontainers 的 Ryuk 在测试进程结束后回收。
}

// New 返回一套全新的测试环境：独立的 MySQL 数据库与 Redis DB。
func New(t *testing.T) Env {
	t.Helper()
	if testing.Short() {
		t.Skip("-short 模式跳过集成测试")
	}
	if err := dockerAvailable(); err != nil {
		if os.Getenv("REQUIRE_INTEGRATION") == "1" {
			t.Fatalf("集成测试需要 Docker：%v", err)
		}
		t.Skipf("没有可用的 Docker，跳过集成测试：%v", err)
	}
	once.Do(start)
	if startErr != nil {
		t.Fatal(startErr)
	}

	n := dbSeq.Add(1)
	name := fmt.Sprintf("t%d_%d", os.Getpid(), n)
	ctx := context.Background()
	root, err := store.OpenMySQL(ctx, rootDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := root.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db, err := store.OpenMySQL(context.Background(), rootDSN); err == nil {
			_, _ = db.Exec("DROP DATABASE `" + name + "`")
			_ = db.Close()
		}
	})

	cfg, err := mysql.ParseDSN(rootDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	// Redis 只有 16 个 DB；各测试轮流用，测试内自己清理用到的键。
	return Env{MySQLDSN: cfg.FormatDSN(), RedisAddr: redisAddr, RedisDB: int(n % 16)}
}

func dockerAvailable() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	p, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.Health(ctx)
}
