package material_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/material"
	"peetraining-server/internal/params"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type fx struct {
	db    *sql.DB
	svc   *material.Service
	quota *quota.Service
	prof  *profile.Service
	oss   *oss.Mock
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
	ps := params.New(dbq.New(db))
	f := &fx{db: db, oss: oss.NewMock()}
	f.quota = quota.New(dbq.New(db), ps, now)
	f.prof = profile.New(db, ps, f.oss, now)
	f.svc = material.New(material.Deps{DB: db, OSS: f.oss, Moderation: moderation.NewMock(), Quota: f.quota, Params: ps, Now: now,
		OCR: ocr.NewMock(), PDF: ocr.NewMock(), Flags: flags.New(dbq.New(db)),
	})
	return f
}

var phones = 0

func (f *fx) user(t *testing.T) (uid, subjectID, bankID uint64) {
	t.Helper()
	phones++
	phone := "1380000" + strings.Repeat("0", 4-len(itoa(phones))) + itoa(phones)
	res, err := f.db.Exec("INSERT INTO users (phone, invite_code) VALUES (?, ?)", phone, phone[3:])
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	s, err := f.prof.CreateSubject(context.Background(), uint64(id), profile.SubjectInput{Name: "中国语言文学基础", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id), s.ID, s.BankID
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func kind(err error) apperr.Kind {
	if e, ok := apperr.As(err); ok {
		return e.Kind
	}
	return 0
}

func pdf(name, sha string, pages int, size int64) material.File {
	return material.File{Name: name, Format: "pdf", Size: size, SHA256: strings.Repeat(sha, 64)[:64], PageCount: pages}
}

func TestUploadFlowAndDuplicate(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, bankID := f.user(t)

	if _, err := f.svc.RequestUploads(ctx, uid, sid, "question", []material.File{pdf("a.pdf", "a", 10, 1000)}, false); kind(err) != apperr.BadRequest {
		t.Fatalf("未确认使用权应拒绝：%v", err)
	}

	ts, err := f.svc.RequestUploads(ctx, uid, sid, "question", []material.File{pdf("真题.pdf", "a", 10, 1000), pdf("真题副本.pdf", "a", 10, 1000)}, true)
	if err != nil {
		t.Fatal(err)
	}
	if ts[0].Upload == nil || ts[0].Duplicate || !ts[1].Duplicate || ts[1].MaterialID != ts[0].MaterialID {
		t.Fatalf("同一请求里的相同文件应只上传一次：%+v", ts)
	}
	m, err := f.svc.Get(ctx, uid, ts[0].MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if want := oss.MaterialKey(uid, bankID, m.ID, "pdf"); m.ObjectKey.String != want || m.Status != dbq.MaterialsStatusUploading {
		t.Fatalf("对象键与状态：%+v", m)
	}

	// 还没传完就回调 → 400；传了大小不对 → 400；传完 → uploaded
	if _, err := f.svc.ConfirmUploaded(ctx, uid, m.ID); kind(err) != apperr.BadRequest {
		t.Fatalf("未上传：%v", err)
	}
	f.oss.Seed(m.ObjectKey.String, []byte("short"))
	if _, err := f.svc.ConfirmUploaded(ctx, uid, m.ID); kind(err) != apperr.BadRequest {
		t.Fatalf("大小不一致：%v", err)
	}
	f.oss.Seed(m.ObjectKey.String, make([]byte, 1000))
	if m, err = f.svc.ConfirmUploaded(ctx, uid, m.ID); err != nil || m.Status != dbq.MaterialsStatusUploaded {
		t.Fatalf("确认上传：%v %+v", err, m)
	}

	// 以后再传相同内容 → 直接返回已有资料
	ts2, err := f.svc.RequestUploads(ctx, uid, sid, "", []material.File{pdf("again.pdf", "a", 10, 1000)}, true)
	if err != nil || !ts2[0].Duplicate || ts2[0].MaterialID != m.ID || ts2[0].Upload != nil {
		t.Fatalf("重复上传：%v %+v", err, ts2)
	}
}

func TestUploadLimits(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, _ := f.user(t)

	many := make([]material.File, 11)
	for i := range many {
		many[i] = pdf("x.pdf", string(rune('a'+i)), 1, 10)
	}
	images := make([]material.File, 31)
	for i := range images {
		images[i] = material.File{Name: "p.jpg", Format: "image", Size: 10, SHA256: strings.Repeat(string(rune('a'+i%26)), 64)}
	}
	cases := map[string]struct {
		files  []material.File
		reason string
	}{
		"超过 10 个文件": {many, "too_many_files"},
		"超过 30 张图片": {images, "too_many_images"},
		"超过 50 MB":  {[]material.File{pdf("big.pdf", "b", 1, 51<<20)}, "file_too_large"},
		"超过 200 页":  {[]material.File{pdf("long.pdf", "c", 201, 10)}, "too_many_pages"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.RequestUploads(ctx, uid, sid, "", tc.files, true)
			e, ok := apperr.As(err)
			if !ok || e.Kind != apperr.BadRequest || e.Detail["reason"] != tc.reason {
				t.Fatalf("got %v", err)
			}
		})
	}

	// 免费版累计 100 页：已用 95，再传 10 页 → 402，detail 带所需与剩余
	err := store.WithTx(ctx, f.db, func(q *dbq.Queries) error {
		return f.quota.Consume(ctx, q, quota.Charge{UserID: uid, Type: quota.ParsePages, Amount: 95, Key: "seed"})
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.RequestUploads(ctx, uid, sid, "", []material.File{pdf("ok.pdf", "d", 10, 10)}, true)
	e, ok := apperr.As(err)
	if !ok || e.Kind != apperr.QuotaExceeded || e.Detail["remaining"] != 5 || e.Detail["need"] != 10 {
		t.Fatalf("额度不足应在开始前提示：%v", err)
	}
}

func TestPasteAndModeration(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, _ := f.user(t)

	text := strings.Repeat("文学是语言的艺术。\n", 400) // 4000 字
	m, err := f.svc.CreatePasted(ctx, uid, sid, "reference", "", text, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != dbq.MaterialsFormatText || m.Status != dbq.MaterialsStatusUploaded || m.BilledPages != 3 || m.PageCount != 3 {
		t.Fatalf("4000 字应计 3 页：%+v", m)
	}
	if !strings.HasPrefix(m.FileName, "粘贴的文字 10-02 10:00") {
		t.Errorf("默认标题：%q", m.FileName)
	}
	if ok, err := f.svc.Moderate(ctx, uid, m.ID); err != nil || !ok {
		t.Fatalf("正常文字应通过审核：%v", err)
	}
	if _, err := f.svc.CreatePasted(ctx, uid, sid, "", "", strings.Repeat("长", 20001), true); kind(err) != apperr.BadRequest {
		t.Fatalf("超过 2 万字：%v", err)
	}

	bad, err := f.svc.CreatePasted(ctx, uid, sid, "", "违规", "第一页正常。"+strings.Repeat("字", 1600)+moderation.MockBlockWord, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := f.svc.Moderate(ctx, uid, bad.ID); err != nil || ok {
		t.Fatalf("应判为不通过：%v", err)
	}
	bad, _ = f.svc.Get(ctx, uid, bad.ID)
	if bad.Status != dbq.MaterialsStatusRejected || !strings.Contains(bad.FailReason.String, "第 2 页") {
		t.Fatalf("拒绝状态与原因：%+v", bad)
	}
}

func TestSplitPages(t *testing.T) {
	pages := material.SplitPages(strings.Repeat("a", 1400)+"\n"+strings.Repeat("b", 200), 1500)
	if len(pages) != 2 || len([]rune(pages[0])) != 1401 {
		t.Fatalf("应在换行处断页：%d %d", len(pages), len([]rune(pages[0])))
	}
	if got := material.SplitPages(strings.Repeat("字", 3000), 1500); len(got) != 2 {
		t.Fatalf("3000 字两页：%d", len(got))
	}
}

func TestOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, _ := f.user(t)
	other, _, _ := f.user(t)
	m, err := f.svc.CreatePasted(ctx, uid, sid, "", "", "我的笔记", true)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]error{}
	_, checks["get"] = f.svc.Get(ctx, other, m.ID)
	_, checks["confirm"] = f.svc.ConfirmUploaded(ctx, other, m.ID)
	_, checks["impact"] = f.svc.DeletionImpact(ctx, other, m.ID)
	checks["delete"] = f.svc.Delete(ctx, other, m.ID)
	_, checks["moderate"] = f.svc.Moderate(ctx, other, m.ID)
	_, checks["list"] = f.svc.List(ctx, other, sid)
	_, checks["upload"] = f.svc.RequestUploads(ctx, other, sid, "", []material.File{pdf("a.pdf", "z", 1, 1)}, true)
	_, checks["paste"] = f.svc.CreatePasted(ctx, other, sid, "", "", "x", true)
	for name, err := range checks {
		if kind(err) != apperr.NotFound {
			t.Errorf("%s：拿别人的 ID 应返回 404，got %v", name, err)
		}
	}
	if _, err := f.svc.Get(ctx, uid, m.ID); err != nil {
		t.Fatal("资料不应被别人删掉")
	}
}

func TestDeleteCascade(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, bankID := f.user(t)

	ts, err := f.svc.RequestUploads(ctx, uid, sid, "question", []material.File{pdf("真题.pdf", "a", 2, 4), pdf("讲义.pdf", "b", 2, 4)}, true)
	if err != nil {
		t.Fatal(err)
	}
	a, b := ts[0].MaterialID, ts[1].MaterialID
	ma, _ := f.svc.Get(ctx, uid, a)
	f.oss.Seed(ma.ObjectKey.String, []byte("data"))

	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := f.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	kpOnlyA := exec("INSERT INTO knowledge_points (owner_user_id, bank_id, level, name, origin, source_material_id, source_page) VALUES (?, ?, 'point', '意象', 'ai_extracted', ?, 1)", uid, bankID, a)
	kpBoth := exec("INSERT INTO knowledge_points (owner_user_id, bank_id, level, name, origin, source_material_id, source_page) VALUES (?, ?, 'point', '典型', 'ai_extracted', ?, 1)", uid, bankID, a)
	exec("INSERT INTO kp_sources (kp_id, material_id, page_no, owner_user_id) VALUES (?, ?, 1, ?), (?, ?, 1, ?), (?, ?, 2, ?)", kpOnlyA, a, uid, kpBoth, a, uid, kpBoth, b, uid)
	qA := exec("INSERT INTO questions (owner_user_id, bank_id, qtype, stem, source, source_material_id, content_hash) VALUES (?, ?, 'term', '意象', 'exam', ?, SHA2('意象', 256))", uid, bankID, a)
	qB := exec("INSERT INTO questions (owner_user_id, bank_id, qtype, stem, source, source_material_id, content_hash) VALUES (?, ?, 'term', '典型', 'exercise', ?, SHA2('典型', 256))", uid, bankID, b)
	exec("INSERT INTO attempts (owner_user_id, question_id, answer_mode, answer_text) VALUES (?, ?, 'typed', '答'), (?, ?, 'typed', '答')", uid, qA, uid, qB)
	exec("INSERT INTO wrong_book (owner_user_id, question_id, added_reason) VALUES (?, ?, 'wrong')", uid, qA)
	paper := exec("INSERT INTO papers (owner_user_id, bank_id, kind, title, full_score, actual_score, structure, source_material_id) VALUES (?, ?, 'real_exam', '2025 真题', 150, 150, '[]', ?)", uid, bankID, a)
	session := exec("INSERT INTO paper_sessions (owner_user_id, paper_id, subject_id, paper_kind, paper_title, mode, status, full_score) VALUES (?, ?, ?, 'real_exam', '2025 真题', 'mock', 'graded', 150)", uid, paper, sid)

	im, err := f.svc.DeletionImpact(ctx, uid, a)
	if err != nil {
		t.Fatal(err)
	}
	if want := (material.Impact{Questions: 1, Attempts: 1, Wrong: 1, KPDelete: 1, KPKeep: 1, PaperSessions: 1}); im != want {
		t.Fatalf("连带影响：got %+v want %+v", im, want)
	}

	var recomputed uint64
	f.svc.OnDeleted = func(_ context.Context, _ uint64, subjectID uint64) { recomputed = subjectID }
	if err := f.svc.Delete(ctx, uid, a); err != nil {
		t.Fatal(err)
	}
	count := func(q string, args ...any) int {
		var n int
		if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("SELECT COUNT(*) FROM questions WHERE id = ?", qA) != 0 || count("SELECT COUNT(*) FROM attempts WHERE question_id = ?", qA) != 0 ||
		count("SELECT COUNT(*) FROM wrong_book WHERE question_id = ?", qA) != 0 {
		t.Error("从它识别出的题连同作答与错题应删除")
	}
	if count("SELECT COUNT(*) FROM questions WHERE id = ?", qB) != 1 || count("SELECT COUNT(*) FROM attempts WHERE question_id = ?", qB) != 1 {
		t.Error("其他资料的题不受影响")
	}
	if count("SELECT COUNT(*) FROM knowledge_points WHERE id = ?", kpOnlyA) != 0 {
		t.Error("只来自它的知识点应删除")
	}
	if count("SELECT COUNT(*) FROM knowledge_points WHERE id = ? AND source_material_id = ? AND source_page = 2", kpBoth, b) != 1 {
		t.Error("其他资料也有的知识点应保留，出处改指到另一份资料")
	}
	if count("SELECT COUNT(*) FROM paper_sessions WHERE id = ? AND paper_id IS NULL", session) != 1 {
		t.Error("整卷成绩应保留")
	}
	if len(f.oss.Keys()) != 0 {
		t.Errorf("原件应删除：%v", f.oss.Keys())
	}
	if recomputed != sid {
		t.Error("删除后应触发预估分重算")
	}
	if _, err := f.svc.Get(ctx, uid, a); kind(err) != apperr.NotFound {
		t.Fatal(err)
	}
	list, err := f.svc.List(ctx, uid, sid)
	if err != nil || len(list) != 1 || list[0].ID != b {
		t.Fatalf("列表：%v %+v", err, list)
	}
}

func TestQuotaLedger(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, _, _ := f.user(t)
	tx := func(fn func(q *dbq.Queries) error) error { return store.WithTx(ctx, f.db, fn) }

	// 预占 30 页，解析成功 28 页 → 已用 28，预占清零
	var ticket quota.Ticket
	if err := tx(func(q *dbq.Queries) (err error) {
		ticket, err = f.quota.Reserve(ctx, q, quota.Charge{UserID: uid, Type: quota.ParsePages, Amount: 30, Key: "parse:1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if left, _ := f.quota.Remaining(ctx, uid, quota.ParsePages); *left != 70 {
		t.Fatalf("预占后剩余：%d", *left)
	}
	for range 2 { // 结算重复执行只生效一次
		if err := tx(func(q *dbq.Queries) error {
			return f.quota.Settle(ctx, q, ticket, 28, quota.Ref{Type: "material", ID: 1})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if left, _ := f.quota.Remaining(ctx, uid, quota.ParsePages); *left != 72 {
		t.Fatalf("结算后剩余：%d", *left)
	}
	// 同一幂等键再预占不重复扣
	if err := tx(func(q *dbq.Queries) error {
		_, err := f.quota.Reserve(ctx, q, quota.Charge{UserID: uid, Type: quota.ParsePages, Amount: 30, Key: "parse:1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if left, _ := f.quota.Remaining(ctx, uid, quota.ParsePages); *left != 72 {
		t.Fatalf("幂等：%d", *left)
	}

	// 免费版每天 3 次批改：10 个并发请求只成功 3 个
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, exceeded := 0, 0
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := tx(func(q *dbq.Queries) error {
				return f.quota.Consume(ctx, q, quota.Charge{UserID: uid, Type: quota.Grading, Amount: 1, Key: quota.KeyFor("grade", uint64(i))})
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case kind(err) == apperr.QuotaExceeded:
				exceeded++
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ok != 3 || exceeded != 7 {
		t.Fatalf("并发扣减不应超额：成功 %d，超额 %d", ok, exceeded)
	}

	// 退回后可以再用
	if err := tx(func(q *dbq.Queries) error {
		return f.quota.Refund(ctx, q, uid, quota.Grading, "2026-10-02", 1, quota.Ref{}, "refund:1")
	}); err != nil {
		t.Fatal(err)
	}
	items, member, err := f.quota.Summary(ctx, uid)
	if err != nil || member {
		t.Fatal(err)
	}
	for _, it := range items {
		switch it.Type {
		case quota.Grading:
			if it.Used != 2 || *it.Limit != 3 || it.Period != quota.Daily || !it.ResetsAt.Equal(time.Date(2026, 10, 2, 16, 0, 0, 0, time.UTC)) {
				t.Errorf("批改额度：%+v", it)
			}
		case quota.ParsePages:
			if it.Used != 28 || it.Period != quota.Total || !it.ResetsAt.IsZero() {
				t.Errorf("解析额度：%+v", it)
			}
		}
	}

	// 事务回滚时扣减也回滚
	_ = tx(func(q *dbq.Queries) error {
		if err := f.quota.Consume(ctx, q, quota.Charge{UserID: uid, Type: quota.Grading, Amount: 1, Key: "grade:rollback"}); err != nil {
			return err
		}
		return errors.New("写批改结果失败")
	})
	if left, _ := f.quota.Remaining(ctx, uid, quota.Grading); *left != 1 {
		t.Fatalf("回滚后剩余：%d", *left)
	}
}

func (f *fx) uploaded(t *testing.T, uid, sid uint64, file material.File, data []byte) uint64 {
	t.Helper()
	ctx := context.Background()
	file.Size = int64(len(data))
	ts, err := f.svc.RequestUploads(ctx, uid, sid, "", []material.File{file}, true)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := f.svc.Get(ctx, uid, ts[0].MaterialID)
	f.oss.Seed(m.ObjectKey.String, data)
	if _, err := f.svc.ConfirmUploaded(ctx, uid, m.ID); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func (f *fx) pages(t *testing.T, uid, id uint64) []dbq.MaterialPage {
	t.Helper()
	ps, err := dbq.New(f.db).ListMaterialPages(context.Background(), dbq.ListMaterialPagesParams{MaterialID: id, OwnerUserID: uid})
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestExtract(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, _ := f.user(t)

	// Excel：模板 → 2 页（题目表、说明表）
	tpl, _ := extract.Template()
	x := f.uploaded(t, uid, sid, material.File{Name: "题目.xlsx", Format: "xlsx", SHA256: strings.Repeat("1", 64)}, tpl)
	if err := f.svc.Extract(ctx, uid, x); err != nil {
		t.Fatal(err)
	}
	m, _ := f.svc.Get(ctx, uid, x)
	ps := f.pages(t, uid, x)
	if m.Status != dbq.MaterialsStatusParsing || m.PageCount != 2 || m.BilledPages != 2 || len(ps) != 2 || ps[0].Tables == nil || ps[0].LowConfidence != nil {
		t.Fatalf("xlsx：%+v %d", m, len(ps))
	}

	// 文字版 PDF：3 页，第 2 页有低置信度；重跑后页数变少，多余的旧页删除
	pdfData := []byte("第一页\f第二页【模糊】\f第三页")
	p := f.uploaded(t, uid, sid, material.File{Name: "真题.pdf", Format: "pdf", SHA256: strings.Repeat("2", 64), PageCount: 3}, pdfData)
	if err := f.svc.Extract(ctx, uid, p); err != nil {
		t.Fatal(err)
	}
	ps = f.pages(t, uid, p)
	var low []extract.Span
	_ = json.Unmarshal(ps[1].LowConfidence, &low)
	if len(ps) != 3 || ps[1].Text != "第二页模糊" || len(low) != 1 || low[0] != (extract.Span{Start: 3, End: 5}) {
		t.Fatalf("pdf：%+v", ps)
	}
	pm, _ := f.svc.Get(ctx, uid, p)
	f.oss.Seed(pm.ObjectKey.String, []byte("只有一页"))
	if err := f.svc.Extract(ctx, uid, p); err != nil {
		t.Fatal(err)
	}
	if ps = f.pages(t, uid, p); len(ps) != 1 || ps[0].Text != "只有一页" {
		t.Fatalf("重跑：%+v", ps)
	}
	if pm, _ = f.svc.Get(ctx, uid, p); pm.PageCount != 1 || pm.BilledPages != 1 {
		t.Fatalf("重跑页数：%+v", pm)
	}

	// 拍照：每张 1 页
	img := f.uploaded(t, uid, sid, material.File{Name: "p.jpg", Format: "image", SHA256: strings.Repeat("3", 64)}, []byte("一、名词解释 1.意境"))
	if err := f.svc.Extract(ctx, uid, img); err != nil {
		t.Fatal(err)
	}
	if ps = f.pages(t, uid, img); len(ps) != 1 || ps[0].Text != "一、名词解释 1.意境" {
		t.Fatalf("图片：%+v", ps)
	}

	// 粘贴的文字不用再取
	txt, _ := f.svc.CreatePasted(ctx, uid, sid, "", "", "粘贴", true)
	if err := f.svc.Extract(ctx, uid, txt.ID); err != nil {
		t.Fatal(err)
	}
}

func TestExtractFailures(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, _ := f.user(t)

	fail := func(name string, file material.File, data []byte, wantReason string) {
		t.Helper()
		id := f.uploaded(t, uid, sid, file, data)
		err := f.svc.Extract(ctx, uid, id)
		if !material.IsPermanent(err) {
			t.Fatalf("%s：应是重试也不会好的错误：%v", name, err)
		}
		m, _ := f.svc.Get(ctx, uid, id)
		if m.Status != dbq.MaterialsStatusFailed || !strings.Contains(m.FailReason.String, wantReason) {
			t.Fatalf("%s：%s %q", name, m.Status, m.FailReason.String)
		}
	}
	fail("旧版 doc", material.File{Name: "a.docx", Format: "docx", SHA256: strings.Repeat("a", 64)}, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0}, ".docx")
	fail("模糊照片", material.File{Name: "b.jpg", Format: "image", SHA256: strings.Repeat("b", 64)}, []byte("   "), "重新拍照")
	scanned := []byte(ocr.MockScannedMark + "扫描的第一页\f第二页")
	fail("扫描版 PDF 未开放", material.File{Name: "c.pdf", Format: "pdf", SHA256: strings.Repeat("c", 64), PageCount: 2}, scanned, "扫描版")

	// 只对这个用户打开扫描版开关后可以导入
	if _, err := f.db.Exec("INSERT INTO feature_flag_users (flag_key, user_id) VALUES ('scanned_pdf', ?)", uid); err != nil {
		t.Fatal(err)
	}
	id := f.uploaded(t, uid, sid, material.File{Name: "d.pdf", Format: "pdf", SHA256: strings.Repeat("d", 64), PageCount: 2}, append([]byte("x"), scanned...))
	if err := f.svc.Extract(ctx, uid, id); err != nil {
		t.Fatalf("开关打开后：%v", err)
	}

	// 还没上传完成 → 不能取文本；别人的资料 → 404
	ts, _ := f.svc.RequestUploads(ctx, uid, sid, "", []material.File{pdf("e.pdf", "e", 1, 1)}, true)
	if err := f.svc.Extract(ctx, uid, ts[0].MaterialID); !material.IsPermanent(err) {
		t.Fatalf("未上传：%v", err)
	}
	other, _, _ := f.user(t)
	if err := f.svc.Extract(ctx, other, id); kind(err) != apperr.NotFound {
		t.Fatalf("别人的资料：%v", err)
	}
	if material.IsPermanent(errors.New("timeout")) {
		t.Fatal("临时错误应重试")
	}
}
