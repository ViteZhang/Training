package importer

import (
	"context"
	"errors"
	"testing"

	"peetraining-server/internal/apperr"
)

func TestSyncAIError(t *testing.T) {
	// 模型平台超时：转成 AIFailed，保留原始原因供日志与 errors.Is 判断。
	err := syncAIError(context.DeadlineExceeded)
	if !apperr.IsKind(err, apperr.AIFailed) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("平台错误应转成 AIFailed 并保留原因：%v", err)
	}
	// 业务错误保持原样（额度用完要弹额度提示，不能被改成「生成失败」）。
	quota := apperr.New(apperr.QuotaExceeded, "今天的 AI 出题次数用完了")
	if got := syncAIError(quota); !errors.Is(got, quota) {
		t.Fatalf("业务错误不应改写：%v", got)
	}
	failed := apperr.New(apperr.AIFailed, "生成失败，未扣除次数，请重试")
	if got := syncAIError(failed); !errors.Is(got, failed) {
		t.Fatalf("已是 AIFailed 的错误不应改写：%v", got)
	}
}
