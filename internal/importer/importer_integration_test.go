package importer_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/material"
	"peetraining-server/internal/params"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

// inlineQueue 同步执行入队的任务，测试里不需要 Worker。
type inlineQueue struct {
	h     *jobs.Handlers
	tasks []string
}

func (q *inlineQueue) EnqueueContext(ctx context.Context, t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, t.Type())
	return &asynq.TaskInfo{}, q.h.Mux().ProcessTask(ctx, t)
}

type fx struct {
	db    *sql.DB
	svc   *importer.Service
	mat   *material.Service
	prof  *profile.Service
	quota *quota.Service
	oss   *oss.Mock
	queue *inlineQueue
}

func setup(t *testing.T) *fx {
	t.Helper()
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC) }
	q := dbq.New(db)
	ps := params.New(q)
	f := &fx{db: db, oss: oss.NewMock()}
	f.quota = quota.New(q, ps, now)
	f.prof = profile.New(db, ps, f.oss, now)
	f.mat = material.New(material.Deps{DB: db, OSS: f.oss, Moderation: moderation.NewMock(), Quota: f.quota, Params: ps,
		OCR: ocr.NewMock(), PDF: ocr.NewMock(), Flags: flags.New(q), Now: now})
	f.queue = &inlineQueue{}
	f.svc = importer.New(importer.Deps{DB: db, Material: f.mat, Quota: f.quota, AI: ai.NewEngine(ai.Config{Queries: q, UseMock: true}), Queue: f.queue, Now: now})
	f.queue.h = &jobs.Handlers{Logger: logx.New(io.Discard, slog.LevelInfo), Import: f.svc, Material: f.mat, Permanent: material.IsPermanent}
	return f
}

var phone = 0

func (f *fx) user(t *testing.T) (uint64, uint64) {
	t.Helper()
	phone++
	p := "1390000" + strings.Repeat("0", 4-len(itoa(phone))) + itoa(phone)
	res, err := f.db.Exec("INSERT INTO users (phone, invite_code) VALUES (?, ?)", p, p[3:])
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	s, err := f.prof.CreateSubject(context.Background(), uint64(id), profile.SubjectInput{Name: "中国语言文学基础", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id), s.ID
}

func itoa(n int) string {
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	if s == "" {
		return "0"
	}
	return s
}

var shaN = 0

// upload 上传一份 mock PDF（\f 分页）或图片，返回资料 ID。
func (f *fx) upload(t *testing.T, uid, sid uint64, format, text string) uint64 {
	t.Helper()
	ctx := context.Background()
	shaN++
	sha := strings.Repeat(itoa(shaN%10), 64)[:60] + strings.Repeat("0", 4-len(itoa(shaN))) + itoa(shaN)
	file := material.File{Name: "资料" + itoa(shaN) + "." + format, Format: format, Size: int64(len(text)), SHA256: sha, PageCount: strings.Count(text, "\f") + 1}
	if format == "image" {
		file.Name = "照片.jpg"
	}
	ts, err := f.mat.RequestUploads(ctx, uid, sid, "", []material.File{file}, true)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := f.mat.Get(ctx, uid, ts[0].MaterialID)
	f.oss.Seed(m.ObjectKey.String, []byte(text))
	if _, err := f.mat.ConfirmUploaded(ctx, uid, m.ID); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

const examPaper = "2024 年某某大学中国语言文学基础考研真题\n一、名词解释（每题 10 分）\n1. 意境\n2. 典型\n" +
	"\f二、简答题（每题 20 分）\n3. 简述唐传奇的艺术成就。\n三、单项选择题（每题 2 分）\n4. 下列属于唐传奇的是\nA. 莺莺传\nB. 搜神记"

const answerFile = "2024 年某某大学中国语言文学基础考研真题答案\n1. 意境是情景交融、虚实相生的艺术境界。\n" +
	"3. （1）情节曲折；（2）人物鲜明；（3）语言华美。\n4. A"

func used(t *testing.T, f *fx, uid uint64, typ quota.Type) int {
	t.Helper()
	items, _, err := f.quota.Summary(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Type == typ {
			return it.Used
		}
	}
	return -1
}

func TestQuestionImportPipeline(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	paper := f.upload(t, uid, sid, "pdf", examPaper)
	ans := f.upload(t, uid, sid, "pdf", answerFile)

	job, err := f.svc.CreateJob(ctx, uid, sid, "question", []uint64{paper, ans})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != dbq.ImportJobsStatusReviewing || job.ReservedPages != 3 || job.BilledPages != 3 {
		t.Fatalf("任务：%s reserved=%d billed=%d", job.Status, job.ReservedPages, job.BilledPages)
	}
	if used(t, f, uid, quota.ParsePages) != 3 {
		t.Fatalf("解析额度应按计费页数扣：%d", used(t, f, uid, quota.ParsePages))
	}
	for _, m := range job.Materials {
		if m.Status != dbq.ImportJobMaterialsStatusDone && m.MaterialID == paper {
			t.Fatalf("文件状态：%+v", m)
		}
	}

	items, counts, err := f.svc.Items(ctx, uid, job.ID, "all", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Questions != 4 || counts.Subjective != 3 || counts.Objective != 1 {
		t.Fatalf("统计：%+v", counts)
	}
	byNo := map[string]importer.QuestionDraft{}
	itemByNo := map[string]dbq.ImportItem{}
	for _, it := range items {
		var q importer.QuestionDraft
		_ = json.Unmarshal(it.Payload, &q)
		byNo[q.QuestionNo] = q
		itemByNo[q.QuestionNo] = it
	}
	q1, q2, q3, q4 := byNo["1"], byNo["2"], byNo["3"], byNo["4"]
	if q1.QType != "term" || q1.Answer != "意境是情景交融、虚实相生的艺术境界。" || q1.AnswerOrigin != "imported" || *q1.Score != 10 || q1.Source != "exam" || *q1.ExamYear != 2024 {
		t.Errorf("1 跨文件配答案：%+v", q1)
	}
	if len(q3.RubricPoints) != 3 || q3.RubricOrigin != "ai_extracted" || len(q3.KPPath) != 3 {
		t.Errorf("3 采分点与知识点：%+v", q3)
	}
	if q2.Answer != "" || !strings.Contains(string(itemByNo["2"].ReviewReasons), "missing_answer") {
		t.Errorf("2 缺答案：%+v %s", q2, itemByNo["2"].ReviewReasons)
	}
	if q4.QType != "single_choice" || q4.Answer != "A" || len(q4.Options) != 2 || q4.SourcePage != 2 {
		t.Errorf("4 选择题：%+v", q4)
	}

	// 重跑任一步：不重复扣额度、不产生重复条目
	for _, m := range []uint64{paper, ans} {
		if err := f.svc.ProcessMaterial(ctx, uid, job.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.Finalize(ctx, uid, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, c, _ := f.svc.Items(ctx, uid, job.ID, "all", 0, 100); c.Questions != 4 || used(t, f, uid, quota.ParsePages) != 3 {
		t.Fatalf("重跑后：%+v，额度 %d", c, used(t, f, uid, quota.ParsePages))
	}

	// 1.7b：改采分点，合计不等于分值 → 400
	bad := q3
	bad.RubricPoints = []ai.RubricPoint{{Content: "情节", Score: 5}}
	if _, err := f.svc.UpdateItem(ctx, uid, itemByNo["3"].ID, importer.Patch{Question: &bad}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("采分点合计：%v", err)
	}
	good := q3
	good.RubricPoints = []ai.RubricPoint{{Content: "情节曲折", Score: 10}, {Content: "人物鲜明", Score: 10}}
	it3, err := f.svc.UpdateItem(ctx, uid, itemByNo["3"].ID, importer.Patch{Question: &good})
	if err != nil || it3.Status != dbq.ImportItemsStatusEdited || strings.Contains(string(it3.ReviewReasons), "rubric_unconfirmed") {
		t.Fatalf("改采分点：%v %+v", err, it3)
	}
	// 缺答案让 AI 生成，计入 AI 出题额度
	it2, err := f.svc.GenerateAnswer(ctx, uid, itemByNo["2"].ID)
	if err != nil {
		t.Fatal(err)
	}
	var g importer.QuestionDraft
	_ = json.Unmarshal(it2.Payload, &g)
	if g.AnswerOrigin != "ai_generated" || g.RubricOrigin != "ai_generated" || used(t, f, uid, quota.AIQuestions) != 1 {
		t.Fatalf("生成参考答案：%+v", g)
	}

	// 先确认 2 题，再确认其余；重复确认不重复入库、不重复扣导入题数
	r, err := f.svc.Confirm(ctx, uid, job.ID, []uint64{itemByNo["1"].ID, itemByNo["4"].ID})
	if err != nil || r.Questions != 2 {
		t.Fatalf("部分确认：%v %+v", err, r)
	}
	r, err = f.svc.Confirm(ctx, uid, job.ID, nil)
	if err != nil || r.Questions != 2 || r.Papers != 1 {
		t.Fatalf("确认其余：%v %+v", err, r)
	}
	if r2, _ := f.svc.Confirm(ctx, uid, job.ID, nil); r2.Questions != 0 || used(t, f, uid, quota.ImportQuestions) != 4 {
		t.Fatalf("重复确认：%+v %d", r2, used(t, f, uid, quota.ImportQuestions))
	}
	count := func(q string, args ...any) int {
		var n int
		if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("SELECT COUNT(*) FROM questions WHERE owner_user_id = ?", uid); n != 4 {
		t.Fatalf("题目 %d", n)
	}
	if n := count("SELECT COUNT(*) FROM rubric_points WHERE owner_user_id = ? AND origin = 'user_confirmed'", uid); n != 2 {
		t.Errorf("用户确认的采分点 %d", n)
	}
	var full, actual, missing sql.NullString
	if err := f.db.QueryRow("SELECT full_score, actual_score, missing_note FROM papers WHERE owner_user_id = ? AND exam_year = 2024", uid).Scan(&full, &actual, &missing); err != nil {
		t.Fatal(err)
	}
	if full.String != "150.00" || actual.String != "42.00" || !strings.Contains(missing.String, "按比例换算") {
		t.Errorf("真题卷：%s %s %s", full.String, actual.String, missing.String)
	}
	if n := count("SELECT COUNT(*) FROM paper_questions pq JOIN papers p ON p.id = pq.paper_id WHERE p.owner_user_id = ?", uid); n != 4 {
		t.Errorf("卷内题目 %d", n)
	}
	if n := count("SELECT COUNT(*) FROM question_kps WHERE owner_user_id = ?", uid); n != 4 {
		t.Errorf("题目知识点 %d", n)
	}
	if n := count("SELECT question_count FROM materials WHERE id = ?", paper); n != 4 {
		t.Errorf("资料题数 %d", n)
	}
	if n := count("SELECT COUNT(*) FROM knowledge_points WHERE owner_user_id = ? AND level = 'point' AND exam_count = 1", uid); n < 1 {
		t.Errorf("真题出现次数没有统计")
	}
	if j, _ := f.svc.Get(ctx, uid, job.ID); j.Status != dbq.ImportJobsStatusConfirmed {
		t.Errorf("任务状态：%s", j.Status)
	}

	// 再导入同一份卷子 → 疑似重复
	again := f.upload(t, uid, sid, "pdf", strings.Replace(examPaper, "某某大学", "某某大学（重印）", 1))
	job2, err := f.svc.CreateJob(ctx, uid, sid, "question", []uint64{again})
	if err != nil {
		t.Fatal(err)
	}
	items2, c2, _ := f.svc.Items(ctx, uid, job2.ID, "needs_review", 0, 100)
	if c2.NeedsReview != 4 || !items2[0].DuplicateOfQuestionID.Valid {
		t.Fatalf("疑似重复：%+v", c2)
	}
}

func TestFailuresAndRefund(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	blank := f.upload(t, uid, sid, "image", "   ")
	blocked := f.upload(t, uid, sid, "pdf", "1. 意境 "+moderation.MockBlockWord)
	ok := f.upload(t, uid, sid, "pdf", "1. 意境\n2. 典型")
	job, err := f.svc.CreateJob(ctx, uid, sid, "question", []uint64{blank, blocked, ok})
	if err != nil {
		t.Fatal(err)
	}
	st := map[uint64]dbq.ListImportJobMaterialsRow{}
	for _, m := range job.Materials {
		st[m.MaterialID] = m
	}
	if st[blank].Status != dbq.ImportJobMaterialsStatusFailed || !strings.Contains(st[blank].FailReason.String, "重新拍照") {
		t.Errorf("照片没有文字：%+v", st[blank])
	}
	if st[blocked].Status != dbq.ImportJobMaterialsStatusFailed || !strings.Contains(st[blocked].FailReason.String, "内容安全") {
		t.Errorf("内容安全：%+v", st[blocked])
	}
	if job.Status != dbq.ImportJobsStatusReviewing || used(t, f, uid, quota.ParsePages) != 1 || job.BilledPages != 1 {
		t.Fatalf("失败文件应退回额度：%s 已用 %d", job.Status, used(t, f, uid, quota.ParsePages))
	}
	// 重新拍照后重试
	m, _ := f.mat.Get(ctx, uid, blank)
	f.oss.Seed(m.ObjectKey.String, []byte("5. 什么是意象"))
	job, err = f.svc.Retry(ctx, uid, job.ID, blank)
	if err != nil {
		t.Fatal(err)
	}
	if _, c, _ := f.svc.Items(ctx, uid, job.ID, "all", 0, 100); c.Questions != 3 || used(t, f, uid, quota.ParsePages) != 2 {
		t.Fatalf("重试后：%+v 额度 %d", c, used(t, f, uid, quota.ParsePages))
	}
	if _, err := f.svc.Retry(ctx, uid, job.ID, blocked); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatalf("审核不通过不能重试：%v", err)
	}
	// 移除文件：它的条目一起删掉
	job, err = f.svc.Remove(ctx, uid, job.ID, ok)
	if err != nil || len(job.Materials) != 2 || job.Counts.Questions != 1 {
		t.Fatalf("移除：%v %+v", err, job.Counts)
	}
	// 解析额度不足在开始前提示
	big := f.upload(t, uid, sid, "pdf", "1. a\f2. b\f3. c")
	if err := store.WithTx(ctx, f.db, func(q *dbq.Queries) error {
		return f.quota.Consume(ctx, q, quota.Charge{UserID: uid, Type: quota.ParsePages, Amount: 98, Key: "fill"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateJob(ctx, uid, sid, "question", []uint64{big}); !apperr.IsKind(err, apperr.QuotaExceeded) {
		t.Fatalf("额度不足：%v", err)
	}
}

func TestReferenceImport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	text := "第一章 先秦文学\n一、神话\n神话：远古人民表现对自然及社会现象的认识。\n二、诗经\n诗经：我国第一部诗歌总集，收录诗歌 305 篇。"
	m, err := f.mat.CreatePasted(ctx, uid, sid, "reference", "讲义", text, true)
	if err != nil {
		t.Fatal(err)
	}
	job, err := f.svc.CreateJob(ctx, uid, sid, "reference", []uint64{m.ID})
	if err != nil || job.Counts.KnowledgePoints != 2 {
		t.Fatalf("%v %+v", err, job.Counts)
	}
	r, err := f.svc.Confirm(ctx, uid, job.ID, nil)
	if err != nil || r.KPs != 2 {
		t.Fatalf("%v %+v", err, r)
	}
	var name, orig, parent string
	if err := f.db.QueryRow(`SELECT k.name, k.original_text, p.name FROM knowledge_points k JOIN knowledge_points p ON p.id = k.parent_id
		WHERE k.owner_user_id = ? AND k.level = 'point' ORDER BY k.id LIMIT 1`, uid).Scan(&name, &orig, &parent); err != nil {
		t.Fatal(err)
	}
	if name != "神话" || !strings.HasPrefix(orig, "神话：") || parent != "一、神话" {
		t.Fatalf("%s %s %s", name, orig, parent)
	}
	var n int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM kp_sources WHERE owner_user_id = ?", uid).Scan(&n)
	if n != 2 {
		t.Fatalf("知识点出处 %d", n)
	}
	_ = f.db.QueryRow("SELECT kp_count FROM materials WHERE id = ?", m.ID).Scan(&n)
	if n != 2 {
		t.Fatalf("资料知识点数 %d", n)
	}
}

func TestImportOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	other, otherSubject := f.user(t)
	m := f.upload(t, uid, sid, "pdf", "1. 意境")
	job, err := f.svc.CreateJob(ctx, uid, sid, "question", []uint64{m})
	if err != nil {
		t.Fatal(err)
	}
	items, _, _ := f.svc.Items(ctx, uid, job.ID, "all", 0, 10)
	checks := map[string]error{}
	_, checks["get"] = f.svc.Get(ctx, other, job.ID)
	_, _, checks["items"] = f.svc.Items(ctx, other, job.ID, "all", 0, 10)
	_, checks["item"] = f.svc.GetItem(ctx, other, items[0].ID)
	_, checks["update"] = f.svc.UpdateItem(ctx, other, items[0].ID, importer.Patch{Status: "deleted"})
	_, checks["generate"] = f.svc.GenerateAnswer(ctx, other, items[0].ID)
	_, checks["confirm"] = f.svc.Confirm(ctx, other, job.ID, nil)
	_, checks["retry"] = f.svc.Retry(ctx, other, job.ID, m)
	_, checks["remove"] = f.svc.Remove(ctx, other, job.ID, m)
	_, checks["create-with-others-material"] = f.svc.CreateJob(ctx, other, otherSubject, "question", []uint64{m})
	for name, err := range checks {
		if !apperr.IsKind(err, apperr.NotFound) {
			t.Errorf("%s：拿别人的 ID 应返回 404，got %v", name, err)
		}
	}
	if list, _ := f.svc.List(ctx, other, false); len(list) != 0 {
		t.Error("列表不应看到别人的任务")
	}
}

const essayNotes = `2024 年 908 写作考研真题
2024 年作文题：以「守正与创新」为题写一篇议论文（不少于 800 字）
评分细则
立意（40 分）：切题、观点明确
- 35-40 分：立意深刻
- 25-34 分：立意明确
结构（30 分）：层次清晰
内容（50 分）：论据充实
语言（30 分）：通顺准确
方法：开头点题，第一段直接亮出中心论点。
素材【创新】屠呦呦从古籍中获得灵感，提取青蒿素。
范文《守正方能出新》
守正是根基，创新是动力。
传统文化的生命力在于不断创造性转化。
唯有守正，创新才不会迷失方向。`

func TestEssayImport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	// 用户在 1.4 选的是「题目」，资料被判断为作文类，按作文资料整理
	m, err := f.mat.CreatePasted(ctx, uid, sid, "", "908 写作笔记", essayNotes, true)
	if err != nil {
		t.Fatal(err)
	}
	job, err := f.svc.CreateJob(ctx, uid, sid, "question", []uint64{m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !job.DetectedEssay || job.Counts.EssayItems != 5 || job.Status != dbq.ImportJobsStatusReviewing {
		t.Fatalf("作文资料：essay=%v %+v %s %+v", job.DetectedEssay, job.Counts, job.Status, job.Materials)
	}
	items, _, _ := f.svc.Items(ctx, uid, job.ID, "all", 0, 100)
	byType := map[dbq.ImportItemsItemType]json.RawMessage{}
	for _, it := range items {
		byType[it.ItemType] = it.Payload
		if !it.MaterialID.Valid {
			t.Error("作文条目应带出处资料")
		}
	}
	var rubric ai.EssayRubric
	_ = json.Unmarshal(byType[dbq.ImportItemsItemTypeEssayRubric], &rubric)
	if rubric.FullScore != 150 || len(rubric.Dimensions) != 4 || len(rubric.Dimensions[0].Bands) != 2 || rubric.Page != 1 {
		t.Fatalf("评分细则：%+v", rubric)
	}
	var topic ai.EssayTopic
	_ = json.Unmarshal(byType[dbq.ImportItemsItemTypeEssayTopic], &topic)
	if topic.Year == nil || *topic.Year != 2024 || topic.RequiredWords == nil || *topic.RequiredWords != 800 {
		t.Fatalf("作文题：%+v", topic)
	}
	var cat, sub string
	var isEssay bool
	_ = f.db.QueryRow("SELECT category, sub_type FROM materials WHERE id = ?", m.ID).Scan(&cat, &sub)
	_ = f.db.QueryRow("SELECT is_essay FROM subjects WHERE id = ?", sid).Scan(&isEssay)
	if cat != "essay" || sub != "评分细则" || !isEssay {
		t.Fatalf("资料类型 %s/%s，作文课 %v", cat, sub, isEssay)
	}

	r, err := f.svc.Confirm(ctx, uid, job.ID, nil)
	if err != nil || r.EssayItems != 5 {
		t.Fatalf("%v %+v", err, r)
	}
	count := func(q string) int {
		var n int
		if err := f.db.QueryRow(q, uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	checks := map[string]int{
		"SELECT COUNT(*) FROM questions WHERE owner_user_id = ? AND qtype = 'essay' AND exam_year = 2024 AND required_words = 800":           1,
		"SELECT COUNT(*) FROM essay_rubrics WHERE owner_user_id = ? AND is_active = 1 AND source = 'user_material' AND source_page = 1":      1,
		"SELECT COUNT(*) FROM writing_methods WHERE owner_user_id = ? AND source_material_id IS NOT NULL AND source_page = 1":                1,
		"SELECT COUNT(*) FROM essay_materials WHERE owner_user_id = ? AND theme = '创新' AND origin = 'ai_extracted'":                          1,
		"SELECT COUNT(*) FROM model_essays WHERE owner_user_id = ? AND topic_question_id IS NULL AND JSON_LENGTH(structure, '$.points') = 1": 1,
	}
	for q, want := range checks {
		if n := count(q); n != want {
			t.Errorf("%s = %d，want %d", q, n, want)
		}
	}
	if j, _ := f.svc.Get(ctx, uid, job.ID); j.Status != dbq.ImportJobsStatusConfirmed {
		t.Errorf("任务状态：%s", j.Status)
	}

	// 用户改过作文课判断后不再自动改
	if _, err := f.db.Exec("UPDATE subjects SET is_essay = 0, essay_set_by = 'user' WHERE id = ?", sid); err != nil {
		t.Fatal(err)
	}
	m2, _ := f.mat.CreatePasted(ctx, uid, sid, "", "范文集", strings.ReplaceAll(essayNotes, "守正", "坚守"), true)
	if _, err := f.svc.CreateJob(ctx, uid, sid, "essay", []uint64{m2.ID}); err != nil {
		t.Fatal(err)
	}
	_ = f.db.QueryRow("SELECT is_essay FROM subjects WHERE id = ?", sid).Scan(&isEssay)
	if isEssay {
		t.Fatal("用户改过的判断不应被自动覆盖")
	}
}
