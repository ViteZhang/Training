package oss

import (
	"context"
	"errors"
	"io"
	"testing"
)

// API 与 Worker 是两个进程：一个写入的对象，另一个用同一目录新建的 Mock 必须能读到。
func TestDirMockSharedAcrossInstances(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	api, err := NewDirMock(dir)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewDirMock(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := MaterialKey(1, 2, 3, "pdf")
	if err := api.Put(ctx, key, []byte("data"), "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if info, err := worker.Head(ctx, key); err != nil || !info.Exists || info.Size != 4 {
		t.Fatalf("Head：%+v %v", info, err)
	}
	r, err := worker.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "data" {
		t.Fatalf("内容：%q", b)
	}
	if n, err := worker.DeletePrefix(ctx, BankPrefix(1, 2)); err != nil || n != 1 {
		t.Fatalf("DeletePrefix：%d %v", n, err)
	}
	if _, err := api.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应不存在：%v", err)
	}
}

func TestDirMockRejectsEscapingKey(t *testing.T) {
	m, err := NewDirMock(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Put(context.Background(), "../x", []byte("x"), ""); err == nil {
		t.Fatal("跳出目录的键应被拒绝")
	}
}
