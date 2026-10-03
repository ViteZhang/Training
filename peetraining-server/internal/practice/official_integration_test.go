package practice_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/practice"
)

func adminUser(t *testing.T, f *fx, name string) uint64 {
	t.Helper()
	res, err := f.db.Exec("INSERT INTO admin_users (username, display_name, password_hash, phone, roles) VALUES (?, ?, 'x', '13700000000', '[\"content_editor\"]')", name, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return uint64(id)
}

func payload(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// approveAll 提交草稿并审核通过（抽检未抽中的直接通过）。
func approveAll(t *testing.T, s *official.Service, editor, lead, projectID uint64, drafts ...uint64) {
	t.Helper()
	ctx := context.Background()
	for _, d := range drafts {
		if _, err := s.Submit(ctx, editor, projectID, d); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := s.Reviews(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if err := s.Decide(ctx, lead, r.ID, "approved", "", nil); err != nil {
			t.Fatal(err)
		}
	}
}

func userKP(t *testing.T, f *fx, uid uint64, name string) (id uint64, text, source string, officialID sql.NullInt64) {
	t.Helper()
	err := f.db.QueryRow("SELECT id, IFNULL(original_text, ''), origin, official_kp_id FROM knowledge_points WHERE owner_user_id = ? AND name = ? AND level = 'point' ORDER BY IFNULL(original_text, '') = '', id LIMIT 1", uid, name).
		Scan(&id, &text, &source, &officialID)
	if err != nil {
		t.Fatalf("知识点 %s：%v", name, err)
	}
	return
}

// TestOfficialBank 是 T30 的验收：立项 → 生产 → 审核 → 发布 → 用户添加 → 两边题都能练 → 发布新版本后掌握度保留。
func TestOfficialBank(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	editor, lead := adminUser(t, f, "editor1"), adminUser(t, f, "lead1")

	// 用户自建了同一门专业课（代码 654），导入过自己的资料，其中有「意境」。
	uid, sid := f.user(t)
	if _, err := f.db.Exec("UPDATE subjects SET code = '654' WHERE id = ?", sid); err != nil {
		t.Fatal(err)
	}
	f.imported(t, uid, sid)
	myYijing, myText, _, _ := userKP(t, f, uid, "意境")
	// 官方题库开关只对这个用户打开（首次上线提醒只发给能看到的用户）。
	if _, err := f.db.Exec("INSERT INTO feature_flag_users (flag_key, user_id) VALUES ('official_bank', ?)", uid); err != nil {
		t.Fatal(err)
	}
	other, otherSid := f.user(t)

	// 7.10 需求洞察：只有计数。
	ds, err := s.Demands(ctx, f.clock.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range ds {
		if d.Code == "654" {
			found = d.Users >= 1 && d.AvgQuestions > 0
		}
	}
	if !found {
		t.Fatalf("需求洞察应统计到 654：%+v", ds)
	}

	// 7.11 立项。
	pid, err := s.CreateProject(ctx, lead, "某某大学", "中国古代文学", "654", "中国语言文学基础", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsEditorOf(ctx, editor, pid); !ok {
		t.Fatal("编辑应被分配到项目")
	}
	if err := s.UpdateProject(ctx, pid, []uint64{editor}, []official.Material{{Name: "2015–2024 真题", Basis: "public_exam"}}, "producing"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateProject(ctx, pid, []uint64{editor}, []official.Material{{Name: "某教材", Basis: "maybe"}}, "producing"); err == nil {
		t.Error("授权资料须注明授权依据")
	}

	// 7.12 生产：AI 拆知识点候选，与现有知识点树对齐；录题。
	cands, err := s.KPCandidates(ctx, pid, notes)
	if err != nil || len(cands) == 0 {
		t.Fatalf("知识点候选：%+v %v", cands, err)
	}
	for _, c := range cands {
		if c.Alignment != "new" {
			t.Errorf("官方题库还是空的，候选都应是新增：%+v", c)
		}
	}
	rub := []official.Point{{Content: "情景交融", Score: 5}, {Content: "虚实相生", Score: 5}}
	kp1, err := s.SaveDraft(ctx, editor, pid, 0, "knowledge_point", "add", 0, payload(official.KPPayload{Section: "文学理论", Chapter: "审美范畴", Name: "意境",
		OriginalText: "意境：情景交融、虚实相生的艺术境界（官方表述）。", Rubric: rub}))
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := s.SaveDraft(ctx, editor, pid, 0, "knowledge_point", "add", 0, payload(official.KPPayload{Section: "文学理论", Chapter: "审美范畴", Name: "形象思维",
		OriginalText: "形象思维：借助具体形象进行的思维。", Rubric: []official.Point{{Content: "借助具体形象", Score: 5}, {Content: "伴随情感", Score: 5}}}))
	if err != nil {
		t.Fatal(err)
	}
	q1, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(official.QuestionPayload{QType: "single_choice", Stem: "形象思维的特点是",
		Options: []ai.Option{{Key: "A", Text: "借助具体形象"}, {Key: "B", Text: "纯粹抽象推理"}}, Answer: "A", KPNames: []string{"形象思维"}}))
	if err != nil {
		t.Fatal(err)
	}
	sc := 10.0
	q2, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(official.QuestionPayload{QType: "term", Stem: "名词解释：形象思维", Answer: "借助具体形象进行的思维。",
		Score: &sc, KPNames: []string{"形象思维"}, Rubric: []official.Point{{Content: "借助具体形象", Score: 5}, {Content: "伴随情感", Score: 5}}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(official.QuestionPayload{QType: "term", Stem: "名词解释：没有采分点"})); kind(err) != apperr.BadRequest {
		t.Errorf("主观题要有采分点：%v", err)
	}

	// 7.13 审核：前 3 次全量审核；退回必须填原因；不能审核自己的。
	for _, d := range []uint64{kp1.ID, kp2.ID, q1.ID} {
		if mode, err := s.Submit(ctx, editor, pid, d); err != nil || mode != "full" {
			t.Fatalf("前 3 次全量审核：%s %v", mode, err)
		}
	}
	rs, _ := s.Reviews(ctx, pid)
	if len(rs) != 3 {
		t.Fatalf("审核队列：%d", len(rs))
	}
	if err := s.Decide(ctx, editor, rs[0].ID, "approved", "", nil); kind(err) != apperr.Forbidden {
		t.Errorf("不能审核自己提交的：%v", err)
	}
	if err := s.Decide(ctx, lead, rs[0].ID, "rejected", "", nil); kind(err) != apperr.BadRequest {
		t.Errorf("退回必须填原因：%v", err)
	}
	var rejected uint64
	for _, r := range rs {
		if r.DraftID == q1.ID {
			rejected = r.DraftID
			if err := s.Decide(ctx, lead, r.ID, "rejected", "选项太少", nil); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := s.Decide(ctx, lead, r.ID, "approved", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	// 被退回的改完重新提交：之后按 30% 抽检，没抽中的直接通过。
	if _, err := s.SaveDraft(ctx, editor, pid, rejected, "question", "add", 0, payload(official.QuestionPayload{QType: "single_choice", Stem: "形象思维的特点是",
		Options: []ai.Option{{Key: "A", Text: "借助具体形象"}, {Key: "B", Text: "纯粹抽象推理"}, {Key: "C", Text: "与情感无关"}}, Answer: "A", KPNames: []string{"形象思维"}})); err != nil {
		t.Fatal(err)
	}
	approveAll(t, s, editor, lead, pid, rejected, q2.ID)

	// 7.14 发布：变更清单、自动检查。
	pv, err := s.ReleasePreview(ctx, pid)
	if err != nil || len(pv.Changes) != 4 || pv.NextVersion != "1.0" {
		t.Fatalf("发布预览：%+v %v", pv, err)
	}
	v1, err := s.Publish(ctx, lead, pid, "")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != "1.0" || len(v1.Changes) != 4 {
		t.Fatalf("发布：%+v", v1)
	}
	if _, err := s.Publish(ctx, lead, pid, ""); err == nil {
		t.Error("没有新变更不能发布")
	}
	// 首次上线：提醒自建了同一专业课的用户。
	if n := countMsgs(t, f, uid, "official_bank"); n != 1 {
		t.Errorf("首次上线提醒：%d", n)
	}
	if n := countMsgs(t, f, other, "official_bank"); n != 0 {
		t.Errorf("没自建 654 的用户不提醒：%d", n)
	}

	// 用户添加：建议添加到代码相同的专业课。
	banks, err := s.Banks(ctx, uid)
	if err != nil || len(banks) != 1 || banks[0].Suggested != sid || banks[0].AddedTo != 0 {
		t.Fatalf("可添加的官方题库：%+v %v", banks, err)
	}
	bankID := banks[0].BankID
	if err := s.Subscribe(ctx, uid, bankID, otherSid); kind(err) != apperr.NotFound {
		t.Errorf("添加到别人的专业课 404：%v", err)
	}
	if err := s.Subscribe(ctx, uid, bankID, sid); err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(ctx, uid, bankID, sid); kind(err) != apperr.Conflict {
		t.Errorf("重复添加：%v", err)
	}
	// 同名知识点合并：保留用户自己的表述，只建立对应关系。
	id, text, _, off := userKP(t, f, uid, "意境")
	if id != myYijing || text != myText || !off.Valid {
		t.Errorf("同名知识点应合并且保留用户表述：%d %q %v", id, text, off)
	}
	xs, text, origin, off := userKP(t, f, uid, "形象思维")
	if origin != "official" || !off.Valid || text != "形象思维：借助具体形象进行的思维。" {
		t.Errorf("官方知识点复制到用户题库：%s %q %v", origin, text, off)
	}

	// 两边的题都能练：题型专项里同时有自建的和官方的单选题。
	sess, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindTypeDrill, QType: "single_choice"})
	if err != nil {
		t.Fatal(err)
	}
	mine, off1 := choice(sess, "下列属于唐传奇"), choice(sess, "形象思维的特点是")
	if mine.ID == 0 || off1.ID == 0 {
		t.Fatalf("两边的题都要能练：%+v", sess.Questions)
	}
	r, err := f.pr.Submit(ctx, uid, sess.ID, practice.AttemptInput{QuestionID: off1.ID, Key: nextKey(), Selected: []string{"A"}, Duration: 20})
	if err != nil || r.IsCorrect == nil || !*r.IsCorrect {
		t.Fatalf("练官方题：%+v %v", r, err)
	}
	var m float64
	if err := f.db.QueryRow("SELECT m FROM kp_mastery WHERE owner_user_id = ? AND kp_id = ?", uid, xs).Scan(&m); err != nil || m <= 0 {
		t.Fatalf("官方知识点有掌握度：%v %v", m, err)
	}
	// 其他用户看不到这个用户的副本。
	if _, err := f.pr.Submit(ctx, other, sess.ID, practice.AttemptInput{QuestionID: off1.ID, Key: nextKey(), Selected: []string{"A"}}); kind(err) != apperr.NotFound {
		t.Errorf("别人的会话 404：%v", err)
	}

	// 用户改了名词解释题的题干（自己的版本）。
	var myTerm uint64
	_ = f.db.QueryRow("SELECT id FROM questions WHERE owner_user_id = ? AND stem = '名词解释：形象思维'", uid).Scan(&myTerm)
	if _, err := f.db.Exec("UPDATE questions SET stem = '名词解释：形象思维（我的笔记版）' WHERE id = ?", myTerm); err != nil {
		t.Fatal(err)
	}

	// 发布 1.1：修订「形象思维」的表述与采分点、修订名词解释题、下线单选题。
	items, err := s.Items(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var offKP, offTerm, offChoice uint64
	for _, it := range items {
		switch it.Title {
		case "形象思维":
			offKP = it.ID
		case "名词解释：形象思维":
			offTerm = it.ID
		case "形象思维的特点是":
			offChoice = it.ID
		}
	}
	r1, _ := s.SaveDraft(ctx, editor, pid, 0, "knowledge_point", "revise", offKP, payload(official.KPPayload{Section: "文学理论", Chapter: "审美范畴", Name: "形象思维",
		OriginalText: "形象思维：借助具体形象、伴随情感活动的思维（修订）。", Rubric: []official.Point{{Content: "借助具体形象", Score: 4}, {Content: "伴随情感活动", Score: 6}}}))
	r2, _ := s.SaveDraft(ctx, editor, pid, 0, "question", "revise", offTerm, payload(official.QuestionPayload{QType: "term", Stem: "名词解释：形象思维（修订）",
		Answer: "借助具体形象、伴随情感活动的思维。", Score: &sc, KPNames: []string{"形象思维"}, Rubric: []official.Point{{Content: "借助具体形象", Score: 5}, {Content: "伴随情感", Score: 5}}}))
	r3, err := s.SaveDraft(ctx, editor, pid, 0, "question", "offline", offChoice, nil)
	if err != nil {
		t.Fatal(err)
	}
	approveAll(t, s, editor, lead, pid, r1.ID, r2.ID, r3.ID)
	pv, _ = s.ReleasePreview(ctx, pid)
	rubricChanged := false
	for _, c := range pv.Changes {
		rubricChanged = rubricChanged || (c.EntityID == offKP && c.RubricChanged)
	}
	if !rubricChanged || pv.NextVersion != "1.1" {
		t.Errorf("变更清单标出采分点有变化：%+v", pv)
	}
	v2, err := s.Publish(ctx, lead, pid, "")
	if err != nil {
		t.Fatal(err)
	}

	// 没改过的跟着更新，id 不变，掌握度保留；改过的不覆盖，提示官方内容已更新；下线的不再出题。
	id, text, _, _ = userKP(t, f, uid, "形象思维")
	if id != xs || text != "形象思维：借助具体形象、伴随情感活动的思维（修订）。" {
		t.Errorf("没改过的知识点跟着更新、id 不变：%d %q", id, text)
	}
	var m2 float64
	if err := f.db.QueryRow("SELECT m FROM kp_mastery WHERE owner_user_id = ? AND kp_id = ?", uid, xs).Scan(&m2); err != nil || m2 != m {
		t.Errorf("新版本后掌握度保留：%v → %v %v", m, m2, err)
	}
	var stem string
	_ = f.db.QueryRow("SELECT stem FROM questions WHERE id = ?", myTerm).Scan(&stem)
	if stem != "名词解释：形象思维（我的笔记版）" {
		t.Errorf("用户改过的题不覆盖：%q", stem)
	}
	var updated int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND mtype = 'official_bank' AND title LIKE '%官方内容已更新%'", uid).Scan(&updated)
	if updated != 1 {
		t.Errorf("改过的内容有官方更新时提示：%d", updated)
	}
	var status string
	_ = f.db.QueryRow("SELECT status FROM questions WHERE id = ?", off1.ID).Scan(&status)
	if status != "offline" {
		t.Errorf("下线的题目归档：%s", status)
	}
	if n := countMsgs(t, f, uid, "official_bank"); n < 3 {
		t.Errorf("版本更新通知订阅用户：%d", n)
	}

	// 回滚 1.1：没改过的副本恢复，掌握度仍在。
	if err := s.Rollback(ctx, pid, v1.ID); kind(err) != apperr.Conflict {
		t.Errorf("只能回滚最新版本：%v", err)
	}
	if err := s.Rollback(ctx, pid, v2.ID); err != nil {
		t.Fatal(err)
	}
	_, text, _, _ = userKP(t, f, uid, "形象思维")
	_ = f.db.QueryRow("SELECT status FROM questions WHERE id = ?", off1.ID).Scan(&status)
	if text != "形象思维：借助具体形象进行的思维。" || status != "active" {
		t.Errorf("回滚后恢复：%q %s", text, status)
	}
	if err := f.db.QueryRow("SELECT m FROM kp_mastery WHERE owner_user_id = ? AND kp_id = ?", uid, xs).Scan(&m2); err != nil || m2 != m {
		t.Errorf("回滚后掌握度保留：%v %v", m2, err)
	}
	if vs, _ := s.Versions(ctx, pid); len(vs) != 2 || vs[0].RolledBackAt == nil {
		t.Errorf("版本历史标出已回滚：%+v", vs)
	}

	// 移除：没改过的副本删除；改过的保留为用户自己的内容；同名合并的保留用户的。
	if err := s.Unsubscribe(ctx, uid, bankID); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM knowledge_points WHERE owner_user_id = ? AND name = '形象思维'", uid).Scan(&n)
	if n != 0 {
		t.Errorf("没改过的官方知识点随移除删除：%d", n)
	}
	_ = f.db.QueryRow("SELECT COUNT(*) FROM questions WHERE id = ? AND official_question_id IS NULL", myTerm).Scan(&n)
	if n != 1 {
		t.Errorf("改过的题保留为自己的：%d", n)
	}
	id, text, _, off = userKP(t, f, uid, "意境")
	if id != myYijing || text != myText || off.Valid {
		t.Errorf("合并的知识点保留且解除对应：%d %q %v", id, text, off)
	}
}
