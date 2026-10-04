package practice_test

import (
	"context"
	"strings"
	"testing"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/practice"
)

func TestHandwritingRecognizeAndSubmit(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	other, _ := f.user(t)
	f.imported(t, uid, sid)

	if _, err := f.pr.RequestUploads(ctx, uid, []practice.PhotoFile{{ContentType: "application/pdf", Size: 10}}); kind(err) != apperr.BadRequest {
		t.Errorf("只收照片：%v", err)
	}
	ts, err := f.pr.RequestUploads(ctx, uid, []practice.PhotoFile{{ContentType: "image/jpeg", Size: 1000}, {ContentType: "image/png", Size: 1000}})
	if err != nil || len(ts) != 2 || ts[0].URL == "" || ts[0].ObjectKey == ts[1].ObjectKey {
		t.Fatalf("直传地址：%+v %v", ts, err)
	}
	// mock OCR 把照片内容当识别结果，【】内为不确定的字词；两张续拍按顺序拼接。
	f.oss.Seed(ts[0].ObjectKey, []byte("意境是情景【交融】、虚实相生的艺术境界。"))
	f.oss.Seed(ts[1].ObjectKey, []byte("它韵味【无穷】。"))
	keys := []string{ts[0].ObjectKey, ts[1].ObjectKey}
	r, err := f.pr.Recognize(ctx, uid, keys)
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "意境是情景交融、虚实相生的艺术境界。\n它韵味无穷。" || len(r.LowConfidence) != 2 {
		t.Fatalf("识别：%q %v", r.Text, r.LowConfidence)
	}
	runes := []rune(r.Text)
	if string(runes[r.LowConfidence[0][0]:r.LowConfidence[0][1]]) != "交融" || string(runes[r.LowConfidence[1][0]:r.LowConfidence[1][1]]) != "无穷" {
		t.Errorf("不确定位置换算到全文：%v", r.LowConfidence)
	}
	// 别人的照片当作不存在。
	if _, err := f.pr.Recognize(ctx, other, keys); kind(err) != apperr.NotFound {
		t.Errorf("越权识别：%v", err)
	}
	f.oss.Seed(strings.Replace(ts[0].ObjectKey, ".jpg", "-blank.jpg", 1), []byte("   "))
	if _, err := f.pr.Recognize(ctx, uid, []string{strings.Replace(ts[0].ObjectKey, ".jpg", "-blank.jpg", 1)}); kind(err) != apperr.BadRequest {
		t.Errorf("没识别到文字：%v", err)
	}

	// 核对后提交批改：记下照片。
	s := termSession(t, f, uid, sid)
	q := question(s, "意境")
	g, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: r.Text, Mode: "photo", PhotoKeys: keys})
	if err != nil || g.Status != "done" {
		t.Fatalf("拍照作答批改：%+v %v", g, err)
	}
	var mode, photos string
	_ = f.db.QueryRow("SELECT answer_mode, CAST(photo_keys AS CHAR) FROM attempts WHERE id = ?", g.AttemptID).Scan(&mode, &photos)
	if mode != "photo" || !strings.Contains(photos, ts[1].ObjectKey) {
		t.Errorf("作答记录：%s %s", mode, photos)
	}
	if _, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "x", Mode: "photo", PhotoKeys: []string{"u/999999/hw/x.jpg"}}); kind(err) != apperr.NotFound {
		t.Errorf("别人的照片不能提交：%v", err)
	}
}

func TestAnswerNorm(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	other, _ := f.user(t)
	f.imported(t, uid, sid)

	n, err := f.pr.AnswerNorm(ctx, uid, sid, "term")
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Elements) != 3 || n.Elements[0].Name != "定义" || n.Example == nil || len(n.Example.Points) == 0 || n.Last != nil || n.PracticeID == 0 {
		t.Fatalf("答题规范：%+v", n)
	}
	if _, err := f.pr.AnswerNorm(ctx, uid, sid, "essay"); kind(err) != apperr.NotFound {
		t.Errorf("作文没有这一页：%v", err)
	}
	if _, err := f.pr.AnswerNorm(ctx, other, sid, "term"); kind(err) != apperr.NotFound {
		t.Errorf("越权：%v", err)
	}

	// 批改过一道名词解释后，「你上次的写法」标出缺了什么。
	s := termSession(t, f, uid, sid)
	q := question(s, "典型")
	if _, err := f.pr.SubmitSubjective(ctx, uid, s.ID, practice.SubjectiveInput{QuestionID: q.ID, Key: nextKey(), Answer: "典型是文学里的人物。"}); err != nil {
		t.Fatal(err)
	}
	n, _ = f.pr.AnswerNorm(ctx, uid, sid, "term")
	if n.Last == nil || n.Last.Answer != "典型是文学里的人物。" || len(n.Last.Missing) == 0 {
		t.Fatalf("你上次的写法：%+v", n.Last)
	}

	// 按结构写一道：缺「出处或例子」，扣 1 次批改次数；同一个幂等键不重复扣。
	key := nextKey()
	c, err := f.pr.CheckNorm(ctx, uid, n.PracticeID, "典型是共性与个性的统一。其特征一是鲜明的个性，二是深刻的共性。", key)
	if err != nil {
		t.Fatal(err)
	}
	if c.Complete || len(c.Elements) != 3 || !c.Elements[0].Present || c.Elements[2].Present || len(c.Suggestions) == 0 {
		t.Errorf("结构批改：%+v", c)
	}
	again, err := f.pr.CheckNorm(ctx, uid, n.PracticeID, "不同的答案", key)
	if err != nil || again.GradingID != c.GradingID {
		t.Errorf("幂等：%+v %v", again, err)
	}
	var used int
	_ = f.db.QueryRow("SELECT used FROM quota_counters WHERE owner_user_id = ? AND quota_type = 'grading'", uid).Scan(&used)
	if used != 2 {
		t.Errorf("批改 1 次 + 结构批改 1 次：%d", used)
	}
	if _, err := f.pr.CheckNorm(ctx, other, n.PracticeID, "x", nextKey()); kind(err) != apperr.NotFound {
		t.Errorf("越权结构批改：%v", err)
	}
}

func TestDailyAIFillSwitch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	countAI := func(uid uint64) int {
		var n int
		_ = f.db.QueryRow("SELECT COUNT(*) FROM questions WHERE owner_user_id = ? AND source = 'ai_generated'", uid).Scan(&n)
		return n
	}
	// 关闭 AI 补题：题量不够也不生成变式题。
	a, sa := f.user(t)
	f.imported(t, a, sa)
	off, err := f.pr.Create(ctx, a, practice.CreateInput{SubjectID: sa, Kind: practice.KindDaily})
	if err != nil {
		t.Fatal(err)
	}
	if off.AIFilled != 0 || countAI(a) != 0 {
		t.Errorf("关闭时不应生成：%d %d", off.AIFilled, countAI(a))
	}
	// 打开：按缺的分钟数补，只用确认过的知识点，记 AI 出题额度。
	b, sb := f.user(t)
	f.imported(t, b, sb)
	on, err := f.pr.Create(ctx, b, practice.CreateInput{SubjectID: sb, Kind: practice.KindDaily, AIFill: true})
	if err != nil {
		t.Fatal(err)
	}
	if on.AIFilled == 0 || countAI(b) != on.AIFilled || len(on.Questions) != len(off.Questions)+on.AIFilled {
		t.Fatalf("打开时补题：补 %d，生成 %d，共 %d 题（关闭时 %d 题）", on.AIFilled, countAI(b), len(on.Questions), len(off.Questions))
	}
	var unconfirmed int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM questions q JOIN knowledge_points k ON k.id = q.generated_from_kp_id
		WHERE q.owner_user_id = ? AND q.source = 'ai_generated' AND k.needs_review = 1`, b).Scan(&unconfirmed)
	if unconfirmed != 0 {
		t.Error("变式题只能基于确认过的知识点")
	}
	for _, q := range on.Questions {
		if q.Source == "ai_generated" && q.PlanGroup == "" {
			t.Error("补的题要有分组")
		}
	}
}
