// worker 消费 Asynq 队列并运行定时任务。调度器全局只能有一个，生产上只部署一个 worker 容器。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"peetraining-server/internal/app"
	"peetraining-server/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
	log := app.NewLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.RunWorker(ctx, cfg, log); err != nil {
		log.Error("worker exited", "err", err)
		stop()
		os.Exit(1)
	}
}
