package practice

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
)

// PaperGradeTasks 用持久队列区分处理中、等待重试和重试耗尽，避免永久显示批改中。
type PaperGradeTasks interface {
	GetTaskInfo(queue, id string) (*asynq.TaskInfo, error)
	RunTask(queue, id string) error
	DeleteTask(queue, id string) error
}

func sessionGradeTaskID(id uint64) string { return fmt.Sprintf("%s:%d", jobs.TypePaperGrade, id) }
func (s *Service) paperGradingProblem(id string) string {
	if s.gradeTasks == nil {
		return ""
	}
	info, err := s.gradeTasks.GetTaskInfo(jobs.QueueCritical, id)
	if errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound) {
		return "答卷已保存，但批改任务未启动。请点恢复批改，不会重复扣次数。"
	}
	if err != nil {
		return "暂时无法获取批改任务状态，答卷已保存，请稍后刷新。"
	}
	if info.State == asynq.TaskStateArchived || info.State == asynq.TaskStateCompleted {
		return "批改任务未能完成，答卷及已批改结果已保存。请点恢复批改，不会重复扣次数。"
	}
	if info.State == asynq.TaskStateRetry {
		return "批改遇到暂时故障，系统正在等待重试。也可以点恢复批改立即重试，不会重复扣次数。"
	}
	return ""
}

// PaperHistory 按逐次 session 读取，保留原试卷删除后的标题快照；游标不依赖可变状态。
func (s *Service) PaperHistory(ctx context.Context, userID, subjectID, before uint64, limit int) ([]dbq.ListPaperHistoryRow, bool, error) {
	if _, err := s.subject(ctx, userID, subjectID); err != nil {
		return nil, false, err
	}
	if before == 0 {
		before = math.MaxUint64
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	rows, err := s.q.ListPaperHistory(ctx, dbq.ListPaperHistoryParams{OwnerUserID: userID, SubjectID: subjectID, BeforeID: before, Limit: int32(limit + 1)})
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	return rows, more, err
}
