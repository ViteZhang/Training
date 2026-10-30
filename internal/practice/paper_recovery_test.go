package practice

import (
	"context"
	"errors"
	"github.com/hibiken/asynq"
	"testing"
)

type gradeTaskStub struct {
	state         asynq.TaskState
	err           error
	runs, deleted int
}

func (f *gradeTaskStub) GetTaskInfo(_, id string) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{ID: id, State: f.state}, f.err
}
func (f *gradeTaskStub) RunTask(_, _ string) error    { f.runs++; return nil }
func (f *gradeTaskStub) DeleteTask(_, _ string) error { f.deleted++; return nil }

type gradeQueueStub struct{ calls int }

func (f *gradeQueueStub) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	f.calls++
	return &asynq.TaskInfo{}, nil
}
func TestPaperGradeRecoveryStates(t *testing.T) {
	for _, tc := range []struct {
		name                string
		state               asynq.TaskState
		err                 error
		queue, run, deleted int
		problem             bool
	}{
		{name: "active", state: asynq.TaskStateActive},
		{name: "pending", state: asynq.TaskStatePending},
		{name: "retry", state: asynq.TaskStateRetry, run: 1, problem: true},
		{name: "archived", state: asynq.TaskStateArchived, run: 1, problem: true},
		{name: "missing", err: asynq.ErrTaskNotFound, queue: 1, problem: true},
		{name: "missing queue", err: asynq.ErrQueueNotFound, queue: 1, problem: true},
		{name: "completed with unfinished session", state: asynq.TaskStateCompleted, deleted: 1, queue: 1, problem: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := &gradeTaskStub{state: tc.state, err: tc.err}
			queue := &gradeQueueStub{}
			s := &Service{queue: queue, gradeTasks: tasks}
			if (s.paperGradingProblem("paper:grade:90") != "") != tc.problem {
				t.Fatal("incorrect recovery message")
			}
			if err := s.enqueueGrade(context.Background(), 7, 90); err != nil {
				t.Fatal(err)
			}
			if queue.calls != tc.queue || tasks.runs != tc.run || tasks.deleted != tc.deleted {
				t.Fatalf("queue=%d run=%d deleted=%d", queue.calls, tasks.runs, tasks.deleted)
			}
		})
	}
	tasks := &gradeTaskStub{err: errors.New("redis unavailable")}
	s := &Service{queue: &gradeQueueStub{}, gradeTasks: tasks}
	if s.enqueueGrade(context.Background(), 7, 90) == nil || s.paperGradingProblem("paper:grade:90") == "" {
		t.Fatal("redis failure should be recoverable")
	}
}
