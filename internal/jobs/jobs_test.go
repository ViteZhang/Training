package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/logx"
)

func TestPingTask(t *testing.T) {
	task, err := NewPingTask("hello")
	if err != nil {
		t.Fatal(err)
	}
	if task.Type() != TypePing {
		t.Fatalf("type = %s", task.Type())
	}
	var buf bytes.Buffer
	h := &Handlers{Logger: logx.New(&buf, slog.LevelDebug)}
	if err := h.Mux().ProcessTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"message":"hello"`) || !strings.Contains(buf.String(), "task done") {
		t.Errorf("日志不对：%s", buf.String())
	}
}

func TestPingBadPayloadSkipsRetry(t *testing.T) {
	h := &Handlers{Logger: logx.New(&bytes.Buffer{}, slog.LevelDebug)}
	err := h.Mux().ProcessTask(context.Background(), asynq.NewTask(TypePing, []byte("not json")))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("坏载荷应 SkipRetry，实际 %v", err)
	}
}

func TestShanghai(t *testing.T) {
	utc := time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC)
	if got := utc.In(Shanghai).Format("2006-01-02 15:04"); got != "2026-10-02 00:00" {
		t.Errorf("UTC 16:00 应是北京时间次日 0 点，got %s", got)
	}
}

func TestQueuesAndSchedules(t *testing.T) {
	if Queues[QueueCritical] <= Queues[QueueDefault] || Queues[QueueDefault] <= Queues[QueueLow] {
		t.Error("队列权重应 critical > default > low")
	}
	for _, sc := range Schedules {
		if sc.Cron == "" || sc.Type == "" {
			t.Errorf("定时任务配置不完整：%+v", sc)
		}
	}
	var buf bytes.Buffer
	l := AsynqLogger{L: logx.New(&buf, slog.LevelDebug)}
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	l.Fatal("f")
	if strings.Count(buf.String(), "\n") != 5 {
		t.Errorf("AsynqLogger 应写 5 行：%s", buf.String())
	}
}

type fakeExtractor struct {
	got []uint64
	err error
}

func (f *fakeExtractor) Extract(_ context.Context, userID, materialID uint64) error {
	f.got = []uint64{userID, materialID}
	return f.err
}

func TestExtractTask(t *testing.T) {
	task, err := NewExtractTask(7, 42)
	if err != nil {
		t.Fatal(err)
	}
	permanent := errors.New("文件损坏")
	transient := errors.New("识别服务超时")
	for name, tc := range map[string]struct {
		err     error
		wantErr bool
	}{
		"成功":     {nil, false},
		"文件本身问题": {permanent, false},
		"临时错误重试": {transient, true},
	} {
		t.Run(name, func(t *testing.T) {
			ex := &fakeExtractor{err: tc.err}
			h := &Handlers{Logger: logx.New(&bytes.Buffer{}, slog.LevelDebug), Material: ex, Permanent: func(e error) bool { return errors.Is(e, permanent) }}
			err := h.Mux().ProcessTask(context.Background(), task)
			if (err != nil) != tc.wantErr || ex.got[0] != 7 || ex.got[1] != 42 {
				t.Fatalf("err=%v got=%v", err, ex.got)
			}
		})
	}
	h := &Handlers{Logger: logx.New(&bytes.Buffer{}, slog.LevelDebug)}
	if err := h.Mux().ProcessTask(context.Background(), asynq.NewTask(TypeExtract, []byte("x"))); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("坏载荷：%v", err)
	}
}
