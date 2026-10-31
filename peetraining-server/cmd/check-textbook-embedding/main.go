// 只验证教材嵌入，不初始化应用、数据库、Redis、队列或OSS。不得打印配置或响应正文。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/config"
	"time"
)

func main() {
	configOnly := flag.Bool("config-only", false, "report only non-sensitive readiness; no network or database access")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		report("invalid_configuration", nil)
		os.Exit(1)
	}
	if *configOnly {
		report("configuration", map[string]any{"chat_provider": cfg.AI.Provider, "chat_credentials_present": cfg.AI.BailianAPIKey != "" || cfg.AI.RelayAPIKey != "", "embedding_credentials_present": cfg.Textbook.EmbeddingAPIKey != "", "embedding_dimensions": cfg.Textbook.EmbeddingDimensions, "vector_endpoint_configured": cfg.Textbook.VectorURL != "", "database_connected": false})
		return
	}
	// 单独探测允许仍保持 provider=disabled；只在本进程显式启用，不改文件/生产开关。
	c := cfg.Textbook
	c.EmbeddingProvider = "openai"
	if c.Validate(config.EnvLocal) != nil {
		report("missing_or_invalid_embedding_configuration", nil)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	start := time.Now()
	out, err := ai.NewEmbedder(c).Embed(ctx, []string{"文学活动包括作品、作者、世界与读者。", "文学活动的四个要素是什么？"})
	if err != nil {
		report("embedding_probe_failed", map[string]any{"stage": "provider_or_response_validation", "reason": err.Error()})
		os.Exit(1)
	}
	report("ok", map[string]any{"model": c.EmbeddingModel, "dimensions": len(out[0]), "vectors": len(out), "elapsed_ms": time.Since(start).Milliseconds(), "database_connected": false})
}
func report(status string, detail any) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status": status, "detail": detail})
}
