package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/cloud"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/config"
)

// CheckAI 用当前配置向大模型发一条很短的请求，确认地址、密钥和模型名都可用（本地配置用，见 make ai-check）。
// 不连数据库，不记 ai_calls。
func CheckAI(ctx context.Context, cfg *config.Config, out io.Writer) error {
	if cfg.AI.Provider == config.ProviderMock {
		return fmt.Errorf("AI_PROVIDER 还是 mock：没有读到大模型配置，检查 .env.local 是否放在 peetraining-server 目录下、变量名是否写对")
	}
	clients, err := cloud.New(cfg)
	if err != nil {
		return err
	}
	models, _ := ai.Routing(cfg.AI, clients.AIFallback)
	_, _ = fmt.Fprintf(out, "AI_PROVIDER=%s，模型 %s，正在发送测试请求…\n", cfg.AI.Provider, models.Strong)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	start := time.Now()
	resp, err := clients.AI.Complete(ctx, cloudai.Request{
		Capability: "ai_check",
		Model:      models.Strong,
		Messages:   []cloudai.Message{{Role: "user", Content: "只回复两个字：成功"}},
	})
	if err != nil {
		return fmt.Errorf("调用失败：%w", err)
	}
	_, _ = fmt.Fprintf(out, "✅ 配置成功：模型 %s 回复「%s」，用时 %s，输入 %d / 输出 %d token\n",
		resp.Model, strings.TrimSpace(resp.Content), time.Since(start).Round(time.Millisecond), resp.InputTokens, resp.OutputTokens)
	return nil
}
