package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/params"
	"peetraining-server/internal/quota"
)

// TestSupplementPrompt 是 T48 的验收：板块标题显示官方 / 我的题数；某板块错得多且官方题少时提示补充资料，
// 「以后再说」后 7 天内同一板块不再提示；没添加官方题库的课不提示；别人的课 404。
func TestSupplementPrompt(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	q := dbq.New(f.db)
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: q, UseMock: true}), Now: func() time.Time { return f.clock }})
	ps := params.New(q)
	bk := bank.New(bank.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: q, UseMock: true}), Quota: quota.New(q, ps, func() time.Time { return f.clock }), Params: ps,
		Now: func() time.Time { return f.clock }})

	uid, sid := f.user(t)
	other, _ := f.user(t)
	_, bankID := publishOfficial(t, f, s, "四川大学", "812", "西方哲学史")
	if p, err := bk.SupplementPrompt(ctx, uid, sid); err != nil || p != nil {
		t.Fatalf("没添加官方题库不提示：%+v %v", p, err)
	}
	if err := s.Subscribe(ctx, uid, bankID, sid); err != nil {
		t.Fatal(err)
	}
	// 板块标题：官方 3 · 我的 0。
	roots, _, err := bk.Tree(ctx, uid, sid, "all")
	if err != nil {
		t.Fatal(err)
	}
	var sec *bank.Node
	for _, r := range roots {
		if r.Name == "近代哲学" {
			sec = r
		}
	}
	if sec == nil || sec.OfficialQuestions != 3 || sec.MyQuestions != 0 {
		t.Fatalf("板块题数：%+v", sec)
	}

	// 近 14 天在这个板块作答 5 次、得分率 20%：提示。
	rows, err := f.db.Query("SELECT id FROM questions WHERE owner_user_id = ? AND official_question_id IS NOT NULL", uid)
	if err != nil {
		t.Fatal(err)
	}
	var qids []uint64
	for rows.Next() {
		var id uint64
		_ = rows.Scan(&id)
		qids = append(qids, id)
	}
	_ = rows.Close()
	for i := range 5 {
		if _, err := f.db.Exec("INSERT INTO attempts (owner_user_id, question_id, answer_mode, answer_text, score, full_score, answered_at) VALUES (?, ?, 'typed', 'x', 1, 5, ?)",
			uid, qids[i%len(qids)], f.clock.AddDate(0, 0, -1)); err != nil {
			t.Fatal(err)
		}
	}
	p, err := bk.SupplementPrompt(ctx, uid, sid)
	if err != nil || p == nil || p.Reason != "section_gap" || p.SectionID != sec.ID || p.Official != 3 || p.Attempts != 5 {
		t.Fatalf("板块缺口提示：%+v %v", p, err)
	}
	if _, err := bk.SupplementPrompt(ctx, other, sid); kind(err) != apperr.NotFound {
		t.Errorf("别人的课 404：%v", err)
	}
	if err := bk.DismissSupplementPrompt(ctx, other, sid, "section_gap", sec.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的课以后再说 404：%v", err)
	}
	// 以后再说：7 天内不提示，第 8 天恢复（作答仍在 14 天窗口内）。
	if err := bk.DismissSupplementPrompt(ctx, uid, sid, "section_gap", sec.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := bk.SupplementPrompt(ctx, uid, sid); p != nil {
		t.Errorf("以后再说后不提示：%+v", p)
	}
	f.clock = f.clock.AddDate(0, 0, 8)
	if p, _ := bk.SupplementPrompt(ctx, uid, sid); p == nil || p.Reason != "section_gap" {
		t.Errorf("7 天后恢复：%+v", p)
	}
}
