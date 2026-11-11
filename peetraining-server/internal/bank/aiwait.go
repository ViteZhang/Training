package bank

import (
	"context"
	"time"
)

// 页面接口里顺带调 AI（知识点 AI 解读、图谱易混对比）时最多等这么久：App 等 20 秒就超时，
// 模型慢的时候整页都打不开。等不到就先返回页面，AI 在后台跑完写库，下次打开就有了。
var (
	aiWait       = 6 * time.Second
	aiBackground = 2 * time.Minute
)

// waitOrBackground 在不随请求取消的上下文里跑 fn，最多等 aiWait。跑完返回 true；
// 没跑完返回 false，fn 继续在后台跑（最多 aiBackground），这时调用方不能再读 fn 写的变量。
// key 不为空时同一个 key 同时只跑一份：页面反复打开不会重复调模型（返回 false）。
func (s *Service) waitOrBackground(ctx context.Context, key string, fn func(ctx context.Context)) bool {
	if key != "" {
		if _, busy := s.inflight.LoadOrStore(key, struct{}{}); busy {
			return false
		}
	}
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), aiBackground)
	done := make(chan struct{})
	go func() {
		defer func() {
			cancel()
			if key != "" {
				s.inflight.Delete(key)
			}
			close(done)
		}()
		fn(bg)
	}()
	t := time.NewTimer(aiWait)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}
