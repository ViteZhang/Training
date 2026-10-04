// api 启动 HTTP 服务，也负责数据库迁移。
//
//	api                      启动 HTTP 服务
//	api migrate up|status    执行迁移 / 查看迁移状态（流水线发布时执行 up）
//	api migrate new <name>   在 db/migrations 下新建顺序编号的迁移文件（本地开发用）
//	api import-template <path>  生成 Excel 导入模板（App 1.5 提供下载，放到 peetraining-web 的静态资源里）
//	api create-admin <账号> <显示名> <手机号> <角色,角色>  新建后台账号；初始密码读环境变量 ADMIN_INITIAL_PASSWORD，首次登录须修改
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/pressly/goose/v3"

	"peetraining-server/internal/app"
	"peetraining-server/internal/config"
	"peetraining-server/internal/extract"
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

	if len(args) >= 1 && args[0] == "import-template" {
		if len(args) != 2 {
			return fmt.Errorf("用法：api import-template <path>")
		}
		b, err := extract.Template()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Clean(args[1]), b, 0o600) //nolint:gosec // 路径来自开发者本机的命令行参数
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
	case args[0] == "create-admin" && len(args) == 5:
		// 初始密码不从命令行参数读，避免留在 shell 历史里。
		return app.CreateAdmin(ctx, cfg, log, args[1], args[2], args[3], strings.Split(args[4], ","), os.Getenv("ADMIN_INITIAL_PASSWORD"))
	default:
		return fmt.Errorf("未知命令 %q（可用：serve、migrate up|status|new、import-template、create-admin）", args)
	}
}
