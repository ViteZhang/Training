package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"peetraining-server/internal/dbq"
)

func AnswerHash(answer string, points any) string {
	data, _ := json.Marshal(struct {
		Answer string
		Points any
	}{answer, points})
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func (e *Engine) SaveAnswerSnapshot(ctx context.Context, q *dbq.Queries, owner sql.NullInt64, answer string, points []RubricPoint, snapshot *AnswerSnapshot) (uint64, error) {
	if snapshot == nil {
		return 0, nil
	}
	if err := e.ValidateAnswerSnapshot(ctx, q, snapshot); err != nil {
		return 0, err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return 0, err
	}
	id, err := q.CreateAnswerSnapshot(ctx, dbq.CreateAnswerSnapshotParams{OwnerUserID: owner, AnswerHash: AnswerHash(answer, points), Payload: data, CreatedAt: e.now().UTC()})
	return uint64(id), err
}
