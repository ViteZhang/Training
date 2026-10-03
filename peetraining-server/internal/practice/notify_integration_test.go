package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/feedback"
	"peetraining-server/internal/notify"
)

func countMsgs(t *testing.T, f *fx, uid uint64, mtype string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND mtype = ?", uid, mtype).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMessageCenter(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	ns := notify.New(f.db, func() time.Time { return f.clock })
	uid, sid := f.user(t)
	other, _ := f.user(t)

	// 导入完成发「题库整理完成」，带跳转目标。
	f.imported(t, uid, sid)
	if n := countMsgs(t, f, uid, "import_done"); n != 2 {
		t.Fatalf("两次导入各一条题库整理完成：%d", n)
	}
	p, err := ns.List(ctx, uid, "")
	if err != nil || p.Unread < 2 || p.Items[0].Page != "import_review" || p.Items[0].Params["job_id"] == nil {
		t.Fatalf("消息列表：%+v %v", p, err)
	}
	if err := ns.Read(ctx, other, p.Items[0].ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的消息 404：%v", err)
	}
	if err := ns.Read(ctx, uid, p.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := ns.Unread(ctx, uid); n != p.Unread-1 {
		t.Errorf("标已读后未读减一：%d", n)
	}
	if err := ns.ReadAll(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if n, _ := ns.Unread(ctx, uid); n != 0 {
		t.Errorf("全部已读：%d", n)
	}

	// 保留 30 天：更早的不显示，清理任务删除。
	if _, err := f.db.Exec("INSERT INTO messages (owner_user_id, mtype, title, body, created_at) VALUES (?, 'announcement', '旧消息', '旧', ?)", uid, f.clock.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, _ = ns.List(ctx, uid, "")
	for _, m := range p.Items {
		if m.Title == "旧消息" {
			t.Error("30 天前的消息不显示")
		}
	}
	if n, err := ns.Cleanup(ctx); err != nil || n != 1 {
		t.Errorf("清理：%d %v", n, err)
	}

	// 公告：全部用户 / 指定用户；重复执行不重复发。
	if _, err := f.db.Exec(`INSERT INTO announcements (title, body, audience) VALUES ('国庆活动', '内容', '{"all": true}'),
		('只给一个人', '内容', JSON_OBJECT('user_ids', JSON_ARRAY(?))), ('还没到点', '内容', '{"all": true}')`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE announcements SET scheduled_at = ? WHERE title = '还没到点'", f.clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := ns.SendAnnouncements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if countMsgs(t, f, uid, "announcement") != 1 || countMsgs(t, f, other, "announcement") != 2 {
		t.Errorf("公告：%d %d", countMsgs(t, f, uid, "announcement"), countMsgs(t, f, other, "announcement"))
	}

	// 协议更新：发布新版本后，没同意的老用户收到一条。
	if _, err := f.db.Exec("INSERT INTO agreements (kind, version, title, body, change_summary, effective_at, published_at) VALUES ('privacy', '9.0', '隐私政策', '正文', '新增导出说明', ?, ?)",
		f.clock, f.clock.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := ns.SendAgreementUpdates(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if countMsgs(t, f, uid, "agreement_update") != 1 {
		t.Errorf("协议更新：%d", countMsgs(t, f, uid, "agreement_update"))
	}

	// 复习到期：打开提醒、有到期内容的用户每天一条。
	if _, err := f.db.Exec("UPDATE kp_mastery SET next_review_on = '2026-10-01' WHERE owner_user_id = ? LIMIT 1", uid); err != nil {
		t.Fatal(err)
	}
	var due int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM kp_mastery WHERE owner_user_id = ? AND next_review_on IS NOT NULL", uid).Scan(&due)
	if due == 0 {
		if _, err := f.db.Exec("INSERT INTO kp_mastery (owner_user_id, kp_id, next_review_on) SELECT owner_user_id, id, '2026-10-01' FROM knowledge_points WHERE owner_user_id = ? LIMIT 1", uid); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := ns.SendReviewDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if countMsgs(t, f, uid, "review_due") != 1 || countMsgs(t, f, other, "review_due") != 0 {
		t.Errorf("复习到期：%d %d", countMsgs(t, f, uid, "review_due"), countMsgs(t, f, other, "review_due"))
	}
	if _, err := f.db.Exec("UPDATE study_profiles SET notify_review_due = 0 WHERE user_id = ?", uid); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(24 * time.Hour)
	if _, _ = ns.SendReviewDue(ctx); countMsgs(t, f, uid, "review_due") != 1 {
		t.Error("关闭复习到期提醒后不再发")
	}
}

// 验收：后台查看授权内容后，用户在消息中心收到通知。
func TestContentAccessNotice(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	ns := notify.New(f.db, func() time.Time { return f.clock })
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	var mid uint64
	_ = f.db.QueryRow("SELECT id FROM materials WHERE owner_user_id = ? LIMIT 1", uid).Scan(&mid)
	fb, err := feedback.New(f.db, func() time.Time { return f.clock }).Submit(ctx, uid, feedback.Input{Type: "recognition", Content: "第 3 页的题目识别错了", AllowAccess: true, MaterialID: mid})
	if err != nil {
		t.Fatal(err)
	}
	var grant uint64
	_ = f.db.QueryRow("SELECT id FROM content_access_grants WHERE user_id = ?", uid).Scan(&grant)

	owner, err := ns.RecordAccess(ctx, 9, grant, notify.Target{Type: "material", ID: mid})
	if err != nil || owner != uid {
		t.Fatalf("授权内查看：%d %v", owner, err)
	}
	if countMsgs(t, f, uid, "content_accessed") != 1 {
		t.Fatal("查看后用户收到通知")
	}
	var logs int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM content_access_logs WHERE grant_id = ? AND admin_id = 9", grant).Scan(&logs)
	if logs != 1 {
		t.Errorf("每次查看都记日志：%d", logs)
	}
	f.clock = f.clock.Add(2 * time.Minute)
	if _, err := ns.RecordAccess(ctx, 9, grant, notify.Target{Type: "feedback", ID: fb.ID}); err != nil || countMsgs(t, f, uid, "content_accessed") != 2 {
		t.Errorf("再次查看再通知：%v", err)
	}
	if _, err := ns.RecordAccess(ctx, 9, grant, notify.Target{Type: "material", ID: mid + 999}); kind(err) != apperr.Forbidden {
		t.Errorf("授权范围外：%v", err)
	}
	f.clock = f.clock.Add(73 * time.Hour)
	if _, err := ns.RecordAccess(ctx, 9, grant, notify.Target{Type: "material", ID: mid}); kind(err) != apperr.Forbidden {
		t.Errorf("72 小时后授权失效：%v", err)
	}

	// 客服回复进消息中心。
	if err := ns.Reply(ctx, 9, fb.ID, "已修复第 3 页的识别"); err != nil {
		t.Fatal(err)
	}
	p, _ := ns.List(ctx, uid, "")
	if p.Items[0].Type != "support_reply" || p.Items[0].Body != "已修复第 3 页的识别" || p.Items[0].Page != "feedback" {
		t.Errorf("客服回复：%+v", p.Items[0])
	}
	if err := ns.Reply(ctx, 9, fb.ID+999, "x"); kind(err) != apperr.NotFound {
		t.Errorf("不存在的反馈：%v", err)
	}
}
