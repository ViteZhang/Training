package practice_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/quota"
)

type heldPhotoQueue struct{}

func (*heldPhotoQueue) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{}, nil
}
func photoService(f *fx, onCall func(ai.Call)) *practice.Service {
	q := dbq.New(f.db)
	ps := params.New(q)
	now := func() time.Time { return f.clock }
	return practice.New(practice.Deps{DB: f.db, Params: ps, Plan: f.plan, AI: ai.NewEngine(ai.Config{Queries: q, UseMock: true, OnCall: onCall}), Quota: quota.New(q, ps, now), OSS: f.oss, Moderation: moderation.NewMock(), Queue: &heldPhotoQueue{}, Now: now})
}
func photoPaper(t *testing.T, f *fx, pr *practice.Service) (uint64, practice.PaperSessionView, int) {
	t.Helper()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	pb := realPaper(t, f, uid, sid)
	v, err := pr.StartPaper(context.Background(), uid, pb.ID, "mock", nextKey())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range v.Items {
		if it.Stem == "意境" {
			return uid, v, it.Seq
		}
	}
	t.Fatal("missing subjective fixture")
	return 0, v, 0
}
func hwKey(uid uint64, name string) string { return fmt.Sprintf("u/%d/hw/%s.jpg", uid, name) }
func findPhotoItem(t *testing.T, v practice.PaperSessionView, seq int) practice.PaperItem {
	t.Helper()
	for _, it := range v.Items {
		if it.Seq == seq {
			return it
		}
	}
	t.Fatal("missing item")
	return practice.PaperItem{}
}

func TestPaperHandwritingDeadlineAndDefaultConfirmation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pr := photoService(f, nil)
	uid, v, seq := photoPaper(t, f, pr)
	original := "已有答案"
	if _, err := pr.UpdatePaperItem(ctx, uid, v.ID, seq, practice.ItemUpdate{Draft: &original}); err != nil {
		t.Fatal(err)
	}
	key := hwKey(uid, "before-deadline")
	f.oss.Seed(key, []byte("意境是情景交融的境界。"))
	job, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{key}, 0)
	if err != nil || job.Status != "pending" {
		t.Fatalf("register: %+v %v", job, err)
	}
	// 请求已经返回、页面退出，新的 Service/Worker 实例仍然只依赖持久化的照片任务。
	pr = photoService(f, nil)
	f.clock = v.Deadline.Add(time.Second)
	if err := pr.AutoSubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	state, err := pr.PaperSession(ctx, uid, v.ID)
	if err != nil || state.Status != "grading" || !state.SubmittedAt.Equal(*v.Deadline) {
		t.Fatalf("must lock exactly at deadline: %+v %v", state, err)
	}
	if _, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{hwKey(uid, "late")}, 0); kind(err) != apperr.Conflict {
		t.Fatalf("late photos rejected: %v", err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	state, err = pr.PaperSession(ctx, uid, v.ID)
	if err != nil || state.Status != "graded" {
		t.Fatalf("grade: %+v %v", state, err)
	}
	it := findPhotoItem(t, state, seq)
	if it.Draft != "已有答案\n意境是情景交融的境界。" || len(it.Handwriting) != 1 || it.Handwriting[0].Status != "confirmed" || it.GradingID == 0 {
		t.Fatalf("must not grade pending photo as blank: %+v", it)
	}
	var answer string
	if err := f.db.QueryRow("SELECT answer_text FROM attempts WHERE paper_session_id=? AND question_id=?", v.ID, it.QuestionID).Scan(&answer); err != nil || answer != it.Draft {
		t.Fatalf("grading must use photo text: %q %v", answer, err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if used(f, uid, "paper_grading") != 1 {
		t.Fatal("must charge once")
	}
}

func TestPaperHandwritingConcurrentRecognitionAndConfirmIsIdempotent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var calls atomic.Int32
	pr := photoService(f, func(c ai.Call) {
		if c.Capability == ai.RecognizeHandwriting.Name {
			calls.Add(1)
		}
	})
	uid, v, seq := photoPaper(t, f, pr)
	key := hwKey(uid, "concurrent")
	f.oss.Seed(key, []byte("未核对的手写内容"))
	job, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{key}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- pr.RecognizePaperHandwriting(ctx, uid, job.ID) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent workers must call model once, got %d", calls.Load())
	}
	original := "手动补充内容"
	if _, err := pr.UpdatePaperItem(ctx, uid, v.ID, seq, practice.ItemUpdate{Draft: &original}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		it, err := pr.ConfirmPaperHandwriting(ctx, uid, v.ID, job.ID, seq, "核对后的文字")
		if err != nil || it.Draft != "手动补充内容\n核对后的文字" {
			t.Fatalf("confirm retry must not duplicate: %+v %v", it, err)
		}
	}
	if _, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, job.Key, []string{hwKey(uid, "other")}, 0); kind(err) != apperr.Conflict {
		t.Fatalf("key must not be reused for another photo: %v", err)
	}
}

func TestPaperHandwritingFailureRecoveryDoesNotScoreBlank(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pr := photoService(f, nil)
	uid, v, seq := photoPaper(t, f, pr)
	key := hwKey(uid, "not-uploaded")
	job, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{key}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := pr.RecognizePaperHandwriting(ctx, uid, job.ID); err == nil {
		t.Fatal("missing photo must fail")
	}
	state, err := pr.PaperSession(ctx, uid, v.ID)
	if err != nil || findPhotoItem(t, state, seq).Handwriting[0].Status != "failed" {
		t.Fatalf("persist failure: %+v %v", state, err)
	}
	if _, err := pr.SubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err == nil {
		t.Fatal("must not silently score missing photo as blank")
	}
	state, _ = pr.PaperSession(ctx, uid, v.ID)
	if state.Status != "grading" {
		t.Fatal("failed recognition must preserve recoverable answer")
	}
	f.oss.Seed(key, []byte("识别服务恢复后的原题答案"))
	if _, err := pr.SubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	state, _ = pr.PaperSession(ctx, uid, v.ID)
	it := findPhotoItem(t, state, seq)
	if state.Status != "graded" || it.Draft != "识别服务恢复后的原题答案" || it.Handwriting[0].Keys[0] != key || used(f, uid, "paper_grading") != 1 {
		t.Fatalf("recovery: %+v %+v", state, it)
	}
}

func TestPaperHandwritingOwnershipLimitAndDiscard(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pr := photoService(f, nil)
	uid, v, seq := photoPaper(t, f, pr)
	other, _ := f.user(t)
	var keys []string
	for i := 0; i < 6; i++ {
		keys = append(keys, hwKey(uid, fmt.Sprint(i)))
	}
	job, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), keys, 0)
	if err != nil {
		t.Fatal(err)
	}
	again, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, job.Key, keys, 0)
	if err != nil || again.ID != job.ID {
		t.Fatalf("register response retry: %+v %v", again, err)
	}
	if _, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{hwKey(uid, "seventh")}, 0); kind(err) != apperr.BadRequest {
		t.Fatalf("limit per item, not per task: %v", err)
	}
	if _, err := pr.StartPaperHandwriting(ctx, other, v.ID, seq, nextKey(), []string{hwKey(other, "own")}, 0); kind(err) != apperr.NotFound {
		t.Fatalf("session ownership: %v", err)
	}
	if _, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{hwKey(other, "own")}, 0); kind(err) != apperr.NotFound {
		t.Fatalf("photo ownership: %v", err)
	}
	if _, err := pr.ConfirmPaperHandwriting(ctx, other, v.ID, job.ID, seq, "bad"); kind(err) != apperr.NotFound {
		t.Fatalf("confirm ownership: %v", err)
	}
	if err := pr.DiscardPaperHandwriting(ctx, other, v.ID, job.ID, seq); kind(err) != apperr.NotFound {
		t.Fatalf("discard ownership: %v", err)
	}
	if _, err := pr.ConfirmPaperHandwriting(ctx, uid, v.ID, job.ID, seq+1, "bad"); kind(err) != apperr.NotFound {
		t.Fatalf("cross-question result: %v", err)
	}
	if err := pr.DiscardPaperHandwriting(ctx, uid, v.ID, job.ID, seq); err != nil {
		t.Fatal(err)
	}
	if err := pr.RecognizePaperHandwriting(ctx, uid, job.ID); err != nil {
		t.Fatalf("discarded queued task must do nothing: %v", err)
	}
	state, _ := pr.PaperSession(ctx, uid, v.ID)
	it := findPhotoItem(t, state, seq)
	if it.Handwriting[0].Status != "discarded" || strings.TrimSpace(it.Draft) != "" {
		t.Fatalf("discard: %+v", it)
	}

}

func TestPaperHandwritingCaptureOrderAndUnconfirmedEdits(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pr := photoService(f, nil)
	uid, v, seq := photoPaper(t, f, pr)
	first, second := hwKey(uid, "first"), hwKey(uid, "second")
	f.oss.Seed(first, []byte("第一页原文"))
	f.oss.Seed(second, []byte("第二页原文"))
	// 第二张先上传完成、先登记，但拼接必须按拍摄顺序。
	later, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{second}, 200)
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, nextKey(), []string{first}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := pr.RecognizePaperHandwriting(ctx, uid, later.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.ConfirmPaperHandwriting(ctx, uid, v.ID, later.ID, seq, "第二页核对后"); kind(err) != apperr.Conflict {
		t.Fatalf("cannot append second page ahead of first: %v", err)
	}
	if _, err := pr.SavePaperHandwritingText(ctx, uid, v.ID, later.ID, seq, "第二页核对后"); err != nil {
		t.Fatal(err)
	}
	// 第一张尚在识别时就交卷；Worker 先完成原图识别，再按拍照顺序采用核对文字。
	if _, err := pr.SubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	state, err := pr.PaperSession(ctx, uid, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	it := findPhotoItem(t, state, seq)
	if it.Draft != "第一页原文\n第二页核对后" || it.Handwriting[0].ID != earlier.ID || it.Handwriting[1].ID != later.ID {
		t.Fatalf("capture order and review edit: %+v", it)
	}
}

func TestPaperHandwritingTextObjectiveWaitsForRecognition(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pr := photoService(f, nil)
	uid, v, _ := photoPaper(t, f, pr)
	it := v.Items[0]
	if _, err := f.db.Exec("UPDATE questions SET qtype='fill_blank', options=NULL, answer='甲乙' WHERE id=? AND owner_user_id=?", it.QuestionID, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE paper_session_items i JOIN paper_sessions s ON s.id=i.paper_session_id SET i.qtype='fill_blank' WHERE i.paper_session_id=? AND i.seq=? AND s.owner_user_id=?", v.ID, it.Seq, uid); err != nil {
		t.Fatal(err)
	}
	draft := "甲"
	if _, err := pr.UpdatePaperItem(ctx, uid, v.ID, it.Seq, practice.ItemUpdate{Draft: &draft}); err != nil {
		t.Fatal(err)
	}
	key := hwKey(uid, "fill-blank")
	f.oss.Seed(key, []byte("乙"))
	if _, err := pr.StartPaperHandwriting(ctx, uid, v.ID, it.Seq, nextKey(), []string{key}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.SubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM attempts WHERE paper_session_id=? AND question_id=?", v.ID, it.QuestionID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("must not judge incomplete typed/photo answer: %d %v", n, err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	var answer string
	if err := f.db.QueryRow("SELECT answer_text FROM attempts WHERE paper_session_id=? AND question_id=?", v.ID, it.QuestionID).Scan(&answer); err != nil || answer != "甲\n乙" {
		t.Fatalf("must judge complete recognized answer: %q %v", answer, err)
	}
}
