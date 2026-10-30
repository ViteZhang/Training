package practice_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/quota"
)

func voiceService(f *fx) *practice.Service {
	q := dbq.New(f.db)
	ps := params.New(q)
	now := func() time.Time { return f.clock }
	return practice.New(practice.Deps{DB: f.db, Params: ps, Plan: f.plan, AI: ai.NewEngine(ai.Config{Queries: q, UseMock: true}), Quota: quota.New(q, ps, now), OSS: f.oss, Moderation: moderation.NewMock(), ASR: f.asr, Flags: f.flags, Queue: &heldPhotoQueue{}, Now: now})
}
func enableVoice(t *testing.T, f *fx, uid uint64) {
	t.Helper()
	if _, err := f.db.Exec("INSERT INTO feature_flag_users (flag_key,user_id) VALUES ('voice_answer',?)", uid); err != nil {
		t.Fatal(err)
	}
	f.flags.Invalidate()
}
func TestPracticeVoicePersistenceOrderRetryAndOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	other, _ := f.user(t)
	f.imported(t, uid, sid)
	pr := voiceService(f)
	if _, err := pr.RequestVoiceUpload(ctx, uid, "audio/m4a", 100); kind(err) != apperr.NotFound {
		t.Fatalf("disabled: %v", err)
	}
	enableVoice(t, f, uid)
	enableVoice(t, f, other)
	session, err := pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindTypeDrill, QType: "term"})
	if err != nil || len(session.Questions) == 0 {
		t.Fatalf("fixture: %+v %v", session, err)
	}
	qid := session.Questions[0].ID
	upload, err := pr.RequestVoiceUpload(ctx, uid, "audio/m4a", 100)
	if err != nil || !strings.HasPrefix(upload.ObjectKey, "u/") {
		t.Fatalf("upload: %+v %v", upload, err)
	}
	f.oss.Seed(upload.ObjectKey, []byte("audio"))
	late, err := pr.StartPracticeVoice(ctx, uid, session.ID, qid, nextKey(), upload.ObjectKey, 200)
	if err != nil || late.Status != "pending" {
		t.Fatalf("register: %+v %v", late, err)
	}
	early, err := pr.StartPracticeVoice(ctx, uid, session.ID, qid, nextKey(), upload.ObjectKey, 100)
	if err != nil {
		t.Fatal(err)
	}
	// 新服务实例从数据库恢复；上传完成顺序不影响录音顺序。
	pr = voiceService(f)
	list, err := pr.ListPracticeVoice(ctx, uid, session.ID, qid)
	if err != nil || len(list) != 2 || list[0].ID != early.ID {
		t.Fatalf("restore order: %+v %v", list, err)
	}
	replay, err := pr.StartPracticeVoice(ctx, uid, session.ID, qid, late.Key, upload.ObjectKey, 200)
	if err != nil || replay.ID != late.ID {
		t.Fatalf("idempotency: %+v %v", replay, err)
	}
	if _, err := pr.ListPracticeVoice(ctx, other, session.ID, qid); kind(err) != apperr.NotFound {
		t.Fatalf("other session: %v", err)
	}
	if _, err := pr.StartPracticeVoice(ctx, other, session.ID, qid, nextKey(), upload.ObjectKey, 0); kind(err) != apperr.NotFound {
		t.Fatalf("other audio: %v", err)
	}
	if err := pr.DiscardPracticeVoice(ctx, other, session.ID, qid, late.ID); kind(err) != apperr.NotFound {
		t.Fatalf("other discard: %v", err)
	}
	if _, err := pr.ListPracticeVoice(ctx, uid, session.ID, qid+99999); kind(err) != apperr.NotFound {
		t.Fatalf("wrong question: %v", err)
	}
	f.asr.Text = ""
	if err := pr.RecognizePracticeVoice(ctx, uid, early.ID); err == nil {
		t.Fatal("empty transcription must fail")
	}
	list, _ = pr.ListPracticeVoice(ctx, uid, session.ID, qid)
	if list[0].Status != "failed" {
		t.Fatalf("failure persisted: %+v", list)
	}
	f.asr.Text = "意境是情景交融的境界。"
	if err := pr.RecognizePracticeVoice(ctx, uid, early.ID); err != nil {
		t.Fatal(err)
	}
	if err := pr.DiscardPracticeVoice(ctx, uid, session.ID, qid, late.ID); err != nil {
		t.Fatal(err)
	}
	if err := pr.RecognizePracticeVoice(ctx, uid, late.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = pr.ListPracticeVoice(ctx, uid, session.ID, qid)
	if list[0].Status != "ready" || list[0].Text != f.asr.Text || list[1].Status != "discarded" {
		t.Fatalf("retry/discard: %+v", list)
	}
}
func TestPaperVoiceSavedBeforeDeadlineAndDefaultConfirmed(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pr := voiceService(f)
	uid, v, seq := photoPaper(t, f, pr)
	enableVoice(t, f, uid)
	up, err := pr.RequestVoiceUpload(ctx, uid, "audio/m4a", 100)
	if err != nil {
		t.Fatal(err)
	}
	f.oss.Seed(up.ObjectKey, []byte("audio"))
	f.asr.Text = "意境是情景交融的境界。"
	job, err := pr.StartPaperVoice(ctx, uid, v.ID, seq, nextKey(), []string{up.ObjectKey}, 10)
	if err != nil || job.InputKind != "voice" || job.Status != "pending" {
		t.Fatalf("voice: %+v %v", job, err)
	}
	if _, err := pr.StartPaperHandwriting(ctx, uid, v.ID, seq, job.Key, job.Keys, 10); kind(err) != apperr.NotFound {
		t.Fatalf("cannot treat audio as photo: %v", err)
	}
	// 交卷后后台完成识别，默认采用原话再批改，不能按空白答案计分。
	f.clock = v.Deadline.Add(time.Second)
	if err := pr.AutoSubmitPaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.StartPaperVoice(ctx, uid, v.ID, seq, nextKey(), job.Keys, 11); kind(err) != apperr.Conflict {
		t.Fatalf("late audio: %v", err)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	state, err := pr.PaperSession(ctx, uid, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	item := findPhotoItem(t, state, seq)
	if state.Status != "graded" || item.Draft != f.asr.Text || len(item.Handwriting) != 1 || item.Handwriting[0].Status != "confirmed" || item.GradingID == 0 {
		t.Fatalf("default confirmation: %+v %+v", state.Status, item)
	}
	if err := pr.GradePaper(ctx, uid, v.ID); err != nil {
		t.Fatal(err)
	}
	if used(f, uid, "paper_grading") != 1 {
		t.Fatal("must only charge once")
	}
}
