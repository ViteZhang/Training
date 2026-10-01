// api 启动 HTTP 服务，也负责数据库迁移。
//
//	api                      启动 HTTP 服务
//	api migrate up|status    执行迁移 / 查看迁移状态（流水线发布时执行 up）
//	api migrate new <name>   在 db/migrations 下新建顺序编号的迁移文件（本地开发用）
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pressly/goose/v3"

	"peetraining-server/internal/app"
	"peetraining-server/internal/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) >= 2 && args[0] == "migrate" && args[1] == "new" {
		if len(args) != 3 {
			return fmt.Errorf("用法：api migrate new <name>")
		}
		goose.SetSequential(true)
		return goose.Create(nil, "db/migrations", args[2], "sql")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := app.NewLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch {
	case len(args) == 0 || args[0] == "serve":
		return app.RunAPI(ctx, cfg, log)
	case args[0] == "migrate" && len(args) == 2:
		return app.Migrate(ctx, cfg, log, args[1])
	default:
		return fmt.Errorf("未知命令 %q（可用：serve、migrate up|status|new）", args)
	}
}
