package answerupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/params"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

func setup(t *testing.T) (*Service, *sql.DB, uint64, uint64) {
	t.Helper()
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO users (id,phone,invite_code) VALUES (1,'18600010001','USER1'),(2,'18600010002','USER2')`,
		`INSERT INTO subjects(id,owner_user_id,name)VALUES(1,1,'文学')`,
		`INSERT INTO banks(id,source,owner_user_id,subject_id)VALUES(1,'user',1,1)`,
		`INSERT INTO questions(id,owner_user_id,bank_id,qtype,stem,answer,answer_origin,score,source,content_hash)VALUES(1,1,1,'term','意境','原有已确认的答案','user_confirmed',5,'exercise',REPEAT('a',64))`,
	} {
		if _, err = db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	q := dbq.New(db)
	qs := quota.New(q, params.New(q), nil)
	s := New(db, ai.NewEngine(ai.Config{UseMock: true}), qs, nil)
	return s, db, 1, 1
}
func chargeCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM quota_ledger WHERE ref_type='answer_candidate'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestCandidateIdempotencyQuotaAdoptionAndIsolation(t *testing.T) {
	s, db, user, question := setup(t)
	ctx := context.Background()
	c, err := s.Create(ctx, user, question, "candidate-key-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Create(ctx, user, question, "candidate-key-1")
	if err != nil || again.ID != c.ID {
		t.Fatal(again, err)
	}
	if _, err = s.Get(ctx, 2, question, c.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("越权读取", err)
	}
	if _, err = s.Decide(ctx, 2, question, c.ID, "adopt-wrong-user", true); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("越权采用", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.ProcessAnswerCandidate(ctx, c.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if chargeCount(t, db) != 1 {
		t.Fatal("重复扣次")
	}
	ready, err := s.Get(ctx, user, question, c.ID)
	if err != nil || ready.Status != "ready" {
		t.Fatal(ready, err)
	}
	// 模拟已提交成功但提交响应丢失后的失败收尾，不能隐藏已扣次候选。
	if err = dbq.New(db).FailAnswerCandidate(ctx, dbq.FailAnswerCandidateParams{ID: c.ID, QuestionID: question, OwnerUserID: user, FailureMessage: "失败收尾", UpdatedAt: s.now().UTC()}); err != nil {
		t.Fatal(err)
	}
	ready, err = s.Get(ctx, user, question, c.ID)
	if err != nil || ready.Status != "ready" || chargeCount(t, db) != 1 {
		t.Fatal("失败收尾覆盖了已成功候选", ready, err)
	}
	var old string
	_ = db.QueryRow("SELECT answer FROM questions WHERE id=1").Scan(&old)
	if old != "原有已确认的答案" {
		t.Fatal("候选生成覆盖当前答案")
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Decide(ctx, user, question, c.ID, "adopt-candidate-key", true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if chargeCount(t, db) != 1 {
		t.Fatal("采用重复扣次")
	}
	var current, origin string
	var snapshot uint64
	_ = db.QueryRow("SELECT answer,answer_origin,answer_snapshot_id FROM questions WHERE id=1").Scan(&current, &origin, &snapshot)
	if current == old || origin != "ai_generated" || snapshot == 0 {
		t.Fatal(current, origin, snapshot)
	}
	// 新候选放弃不退已成功生成的额度。
	next, err := s.Create(ctx, user, question, "candidate-key-2")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessAnswerCandidate(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(ctx, user, question, next.ID, "discard-candidate-key", false); err != nil {
		t.Fatal(err)
	}
	if chargeCount(t, db) != 2 {
		t.Fatal("放弃退次")
	}
}

type revokedProvider struct{}

func (revokedProvider) PrepareAnswer(_ context.Context, _ uint64, in ai.GenerateAnswerIn) (ai.GenerateAnswerIn, error) {
	return in, nil
}
func (revokedProvider) ValidateAnswerSources(context.Context, *dbq.Queries, []ai.AnswerSource) error {
	return errors.New("source revoked")
}
func TestCandidateConcurrentEditFailureAndSourceRevalidation(t *testing.T) {
	s, db, user, question := setup(t)
	ctx := context.Background()
	c, err := s.Create(ctx, user, question, "candidate-changed-key")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec("UPDATE questions SET answer='编辑中的答案' WHERE id=1")
	if s.ProcessAnswerCandidate(ctx, c.ID) == nil {
		t.Fatal("变化的题目应失败")
	}
	if chargeCount(t, db) != 0 {
		t.Fatal("失败扣次")
	}
	c, err = s.Create(ctx, user, question, "candidate-for-source")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessAnswerCandidate(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	ready, _ := s.Get(ctx, user, question, c.ID)
	// 模拟生成后授权撤销；采用必须再次校验，不能仅隐藏引用。
	s.engine.SetAnswerEvidenceProvider(revokedProvider{})
	if _, err = s.Decide(ctx, user, question, c.ID, "adopt-revoked-key", true); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal(err)
	}
	var answer string
	_ = db.QueryRow("SELECT answer FROM questions WHERE id=1").Scan(&answer)
	if answer != "编辑中的答案" {
		t.Fatal("撤销授权候选覆盖旧答案")
	}
	var payload json.RawMessage
	_ = db.QueryRow("SELECT payload FROM answer_evidence_snapshots WHERE id=?", ready.SnapshotID.Int64).Scan(&payload)
	if len(payload) == 0 {
		t.Fatal("快照丢失")
	}
	if chargeCount(t, db) != 1 {
		t.Fatal("采用失败重复扣次")
	}
}

func TestCandidateConfirmationConflictAndAccountCleanup(t *testing.T) {
	s, db, user, question := setup(t)
	ctx := context.Background()
	if _, err := db.Exec("UPDATE questions SET answer_origin='ai_generated' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	c, err := s.Create(ctx, user, question, "confirmation-candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessAnswerCandidate(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	// 确认不修改正文，但旧候选也不能覆盖刚确认的答案。
	if _, err = db.Exec("UPDATE questions SET answer_origin='user_confirmed' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(ctx, user, question, c.ID, "adopt-after-confirm", true); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatalf("want confirmation conflict: %v", err)
	}
	if _, err = s.QuestionSnapshot(ctx, 2, question); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("snapshot ownership", err)
	}
	if _, err = s.GradingSnapshot(ctx, 2, 999); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("grading ownership", err)
	}
	if _, err = s.ImportSnapshot(ctx, 2, 999); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("import ownership", err)
	}
	// 共享快照和其他用户快照不会随这个用户注销删除。
	if _, err = db.Exec("INSERT INTO answer_evidence_snapshots(owner_user_id,answer_hash,payload,created_at)VALUES(NULL,REPEAT('b',64),'{}',UTC_TIMESTAMP(3)),(2,REPEAT('c',64),'{}',UTC_TIMESTAMP(3))"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM questions WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM answer_update_candidates").Scan(&n); err != nil || n != 0 {
		t.Fatal("question candidate cleanup", n, err)
	}
	if _, err = db.Exec("DELETE FROM users WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM answer_evidence_snapshots WHERE owner_user_id=1").Scan(&n); err != nil || n != 0 {
		t.Fatal("private snapshot cleanup", n, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM answer_evidence_snapshots").Scan(&n); err != nil || n != 2 {
		t.Fatal("other snapshots deleted", n, err)
	}
}
