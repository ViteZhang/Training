package jobs

import (
	"context"
	"encoding/json"
	"github.com/hibiken/asynq"
	"time"
)

const TypeAnswerCandidate = "answer:candidate"
const TypeAnswerCandidateRecover = "answer:recover"

type AnswerCandidates interface {
	ProcessAnswerCandidate(context.Context, uint64) error
	RecoverAnswerCandidates(context.Context) error
}

func NewAnswerCandidateTask(id uint64) (*asynq.Task, error) {
	data, err := json.Marshal(struct {
		ID uint64 `json:"id"`
	}{id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeAnswerCandidate, data, asynq.Queue(QueueDefault), asynq.MaxRetry(0), asynq.Timeout(25*time.Minute), asynq.Unique(10*time.Minute)), nil
}
func (h *Handlers) handleAnswerCandidate(ctx context.Context, t *asynq.Task) error {
	var p struct {
		ID uint64 `json:"id"`
	}
	if json.Unmarshal(t.Payload(), &p) != nil || p.ID == 0 {
		return asynq.SkipRetry
	}
	return h.AnswerCandidates.ProcessAnswerCandidate(ctx, p.ID)
}
