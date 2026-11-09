package official

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
)

func TestFillFailureFatal(t *testing.T) {
	wrappedBalance := apperr.New(apperr.AIFailed, "生成失败").Wrap(cloudai.ErrInsufficientBalance)
	if !fillFailureFatal(wrappedBalance, 0) {
		t.Fatal("余额不足应立即停止整份资料")
	}
	wrappedRate := apperr.New(apperr.AIFailed, "生成失败").Wrap(cloudai.ErrRateLimited)
	if fillFailureFatal(wrappedRate, maxFillRateLimits-1) {
		t.Fatal("未达到连续限流阈值不应停止")
	}
	if !fillFailureFatal(wrappedRate, maxFillRateLimits) {
		t.Fatal("连续限流达到阈值应停止")
	}
	if fillFailureFatal(errors.New("单题输出不合格"), maxFillRateLimits) {
		t.Fatal("普通单题错误不应停止整份资料")
	}
}

type fakeImportQueue struct {
	err   error
	calls int
}

func (q *fakeImportQueue) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	q.calls++
	return nil, q.err
}

func newTestImporter(q ImportQueue) *Service {
	s := New(Deps{})
	s.ConfigureImports(ImportDeps{Queue: q, NewTask: func(fileID uint64) (*asynq.Task, error) {
		return asynq.NewTask("official:import", nil), nil
	}})
	return s
}

func TestEnqueueImportIgnoresDuplicate(t *testing.T) {
	for _, err := range []error{asynq.ErrDuplicateTask, asynq.ErrTaskIDConflict} {
		q := &fakeImportQueue{err: err}
		if err := newTestImporter(q).enqueueImport(context.Background(), 1); err != nil {
			t.Fatalf("重复入队应忽略：%v", err)
		}
		if q.calls != 1 {
			t.Fatalf("应尝试入队一次，实际 %d 次", q.calls)
		}
	}
}

func TestEnqueueImportPropagatesOtherErrors(t *testing.T) {
	want := errors.New("redis down")
	if err := newTestImporter(&fakeImportQueue{err: want}).enqueueImport(context.Background(), 1); !errors.Is(err, want) {
		t.Fatalf("其他错误应返回：%v", err)
	}
}
