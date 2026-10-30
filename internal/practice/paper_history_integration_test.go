package practice_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/hibiken/asynq"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/quota"
	"reflect"
	"testing"
	"time"
)

type recoverPaperQueue struct {
	fail  bool
	calls int
	run   func() error
}

func (q *recoverPaperQueue) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	q.calls++
	if q.fail {
		return nil, errors.New("redis unavailable")
	}
	return &asynq.TaskInfo{}, q.run()
}
func TestPaperEnqueueFailureCanRecoverWithoutDoubleCharge(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	pb := realPaper(t, f, uid, sid)
	now := func() time.Time { return f.clock }
	q := dbq.New(f.db)
	ps := params.New(q)
	queue := &recoverPaperQueue{fail: true}
	pr := practice.New(practice.Deps{DB: f.db, Params: ps, AI: ai.NewEngine(ai.Config{Queries: q, UseMock: true}), Quota: quota.New(q, ps, now), Queue: queue, Now: now})
	v, err := pr.StartPaper(ctx, uid, pb.ID, "practice", nextKey())
	if err != nil {
		t.Fatal(err)
	}
	// 同一累计时间重试不能翻倍，兼容旧客户端 delta；标记更新不能覆盖草稿。
	total := 17
	for i := 0; i < 2; i++ {
		it, err := pr.UpdatePaperItem(ctx, uid, v.ID, v.Items[0].Seq, practice.ItemUpdate{TimeTotal: &total})
		if err != nil || it.TimeSpent != 17 {
			t.Fatalf("累计计时幂等: %+v %v", it, err)
		}
	}
	answer := "意境是情景交融的艺术境界。"
	for _, it := range v.Items {
		if it.Stem == "意境" {
			if _, err := pr.UpdatePaperItem(ctx, uid, v.ID, it.Seq, practice.ItemUpdate{Draft: &answer}); err != nil {
				t.Fatal(err)
			}
		}
	}
	v, err = pr.SubmitPaper(ctx, uid, v.ID)
	if err != nil || v.Status != "grading" || v.GradingError == "" {
		t.Fatalf("保存答卷后排队失败应可恢复: %+v %v", v, err)
	}
	if used(f, uid, "paper_grading") != 1 {
		t.Fatal("first submit must charge once")
	}
	queue.fail = false
	queue.run = func() error { return pr.GradePaper(ctx, uid, v.ID) }
	v, err = pr.SubmitPaper(ctx, uid, v.ID)
	if err != nil || v.Status != "graded" {
		t.Fatalf("恢复原答卷: %+v %v", v, err)
	}
	var beforeReport practice.PaperReport
	var raw []byte
	if err := f.db.QueryRow("SELECT report FROM paper_sessions WHERE id=?", v.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &beforeReport); err != nil {
		t.Fatal(err)
	}
	// 模拟全部题已批完、汇总前任务中断：恢复要重建相同的失分归因，并保留逐题结果。
	if _, err := f.db.Exec("UPDATE paper_sessions SET status='grading' WHERE id=?", v.ID); err != nil {
		t.Fatal(err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	var afterReport practice.PaperReport
	if err := f.db.QueryRow("SELECT report FROM paper_sessions WHERE id=?", v.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &afterReport); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeReport.Loss, afterReport.Loss) || beforeReport.Score != afterReport.Score {
		t.Fatal("恢复后报告应一致")
	}
	calls := queue.calls
	if _, err = pr.SubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if used(f, uid, "paper_grading") != 1 || queue.calls != calls {
		t.Fatal("completed paper must not requeue or charge twice")
	}
}
func TestPaperHistoryPaginationAndOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	other, _ := f.user(t)
	var ids []uint64
	for _, st := range []string{"graded", "grading", "abandoned"} {
		res, err := f.db.Exec(`INSERT INTO paper_sessions(owner_user_id,subject_id,paper_kind,paper_title,mode,status,full_score) VALUES(?,?,'real_exam','2024 年真题','mock',?,150)`, uid, sid, st)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, uint64(id))
	}
	page, more, err := f.pr.PaperHistory(ctx, uid, sid, 0, 2)
	if err != nil || !more || len(page) != 2 || page[0].ID != ids[2] {
		t.Fatalf("page1=%+v more=%v err=%v", page, more, err)
	}
	page, more, err = f.pr.PaperHistory(ctx, uid, sid, page[1].ID, 2)
	if err != nil || more || len(page) != 1 || page[0].ID != ids[0] {
		t.Fatalf("page2=%+v more=%v err=%v", page, more, err)
	}
	if _, _, err = f.pr.PaperHistory(ctx, other, sid, 0, 2); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("ownership: %v", err)
	}
}
