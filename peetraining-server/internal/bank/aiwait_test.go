package bank

import (
	"context"
	"testing"
	"time"
)

func TestWaitOrBackground(t *testing.T) {
	old := aiWait
	aiWait = 50 * time.Millisecond
	t.Cleanup(func() { aiWait = old })
	s := &Service{}
	ctx := context.Background()

	// 很快跑完：等到结果
	var v int
	if !s.waitOrBackground(ctx, "k", func(context.Context) { v = 1 }) || v != 1 {
		t.Fatal("应等到结果")
	}

	// 跑得慢：先返回 false，后台照样跑完，而且不随请求取消
	reqCtx, cancel := context.WithCancel(ctx)
	release, done := make(chan struct{}), make(chan error, 1)
	if s.waitOrBackground(reqCtx, "slow", func(bg context.Context) {
		<-release
		done <- bg.Err()
	}) {
		t.Fatal("慢任务应先返回 false")
	}
	cancel()
	// 同一个 key 还在跑：不再起第二份
	if s.waitOrBackground(ctx, "slow", func(context.Context) { t.Error("不应重复执行") }) {
		t.Fatal("同 key 在跑时应返回 false")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal("请求取消后后台任务不应被取消", err)
	}
	// 跑完后 key 释放
	deadline := time.Now().Add(time.Second)
	for {
		if _, busy := s.inflight.Load("slow"); !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("key 没有释放")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
