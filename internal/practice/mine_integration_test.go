package practice_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/export"
	"peetraining-server/internal/feedback"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/score"
)

func seedCode(t *testing.T, f *fx, code, tier string, days int, expires time.Time, batchStatus, status string) {
	t.Helper()
	res, err := f.db.Exec(`INSERT INTO redeem_batches (name, tier, days, quantity, code_expires_at, status) VALUES ('测试', ?, NULLIF(?, 0), 1, ?, ?)`, tier, days, expires, batchStatus)
	if err != nil {
		t.Fatal(err)
	}
	bid, _ := res.LastInsertId()
	n := membership.NormalizeCode(code)
	if _, err := f.db.Exec(`INSERT INTO redeem_codes (batch_id, code_hash, code_tail, status) VALUES (?, ?, ?, ?)`, bid, membership.HashCode(n), n[len(n)-3:], status); err != nil {
		t.Fatal(err)
	}
}

func reason(err error) string {
	var e *apperr.Error
	if errors.As(err, &e) {
		if r, ok := e.Detail["reason"].(string); ok {
			return r
		}
	}
	return ""
}

func TestRedeemCodes(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	ms := membership.New(membership.Deps{DB: f.db, Now: func() time.Time { return f.clock }})
	uid, _ := f.user(t)
	future := f.clock.Add(30 * 24 * time.Hour)
	seedCode(t, f, "ABCD2345", "gift", 7, future, "active", "unused")
	seedCode(t, f, "WXYZ6789", "monthly", 0, future, "active", "unused")
	seedCode(t, f, "VQDK2345", "gift", 7, future, "active", "void")
	seedCode(t, f, "STPM2345", "gift", 7, future, "disabled", "unused")
	seedCode(t, f, "QLDC2345", "gift", 7, f.clock.Add(-time.Hour), "active", "unused")
	seedCode(t, f, "SPRT2345", "sprint", 0, future, "active", "unused")
	seedCode(t, f, "SEAS2345", "season", 0, future, "active", "unused")

	// 不区分大小写、忽略空格与连字符；第二个码叠加到第一个之后。
	r, err := ms.Redeem(ctx, uid, " abcd-2345 ")
	if err != nil || r.Days != 7 || !r.StartsAt.Equal(f.clock) {
		t.Fatalf("兑换：%+v %v", r, err)
	}
	r2, err := ms.Redeem(ctx, uid, "wxyz6789")
	if err != nil || !r2.StartsAt.Equal(r.EndsAt) || r2.Days != 30 {
		t.Fatalf("时长叠加到当前会员之后：%+v %v", r2, err)
	}
	cases := map[string]string{"ABCD2345": "used", "VQDK2345": "void", "STPM2345": "disabled", "QLDC2345": "expired", "ZZZZ2345": "invalid", "ABC0O1I!": "invalid"}
	for code, want := range cases {
		if _, err := ms.Redeem(ctx, uid, code); kind(err) != apperr.BadRequest || reason(err) != want {
			t.Errorf("%s 应提示 %s：%v", code, want, err)
		}
	}
	// 冲刺卡至当年初试结束（2027 研考，初试 2026-12-20 结束）；没配置次年日期时考季卡提示稍后再试。
	r3, err := ms.Redeem(ctx, uid, "SPRT2345")
	if err != nil || !r3.EndsAt.Equal(time.Date(2026, 12, 20, 16, 0, 0, 0, time.UTC)) {
		t.Errorf("冲刺卡：%+v %v", r3, err)
	}
	if _, err := ms.Redeem(ctx, uid, "SEAS2345"); kind(err) != apperr.Conflict {
		t.Errorf("考季卡缺初试日期：%v", err)
	}
	if _, err := ms.Redeem(ctx, uid, "SEAS2345"); kind(err) != apperr.Conflict {
		t.Errorf("没用掉的码还能再试：%v", err)
	}
}

func TestPostExamSurvey(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	mockPaper(t, f, uid, sid)
	sv, err := f.sc.Survey(ctx, uid)
	if err != nil || sv.Open || sv.ExamYear != 2027 || len(sv.Subjects) != 1 || sv.Subjects[0].Low == nil {
		t.Fatalf("初试前未开放、带考前预估：%+v %v", sv, err)
	}
	in := score.SurveyInput{Scores: map[uint64]float64{sid: 108}, Retest: "unknown"}
	if _, err := f.sc.SubmitSurvey(ctx, uid, in); kind(err) != apperr.Conflict {
		t.Errorf("初试前不能提交：%v", err)
	}
	f.clock = time.Date(2026, 12, 21, 2, 0, 0, 0, time.UTC)
	if _, err := f.sc.SubmitSurvey(ctx, uid, score.SurveyInput{Scores: map[uint64]float64{sid: 151}, Retest: "unknown"}); kind(err) != apperr.BadRequest {
		t.Errorf("成绩不能超过满分：%v", err)
	}
	other, osid := f.user(t)
	if _, err := f.sc.SubmitSurvey(ctx, uid, score.SurveyInput{Scores: map[uint64]float64{osid: 100}, Retest: "unknown"}); kind(err) != apperr.NotFound {
		t.Errorf("别人的课：%v", err)
	}
	sv, err = f.sc.SubmitSurvey(ctx, uid, in)
	if err != nil || !sv.Submitted || sv.Subjects[0].Actual == nil || *sv.Subjects[0].Actual != 108 {
		t.Fatalf("提交：%+v %v", sv, err)
	}
	var days int
	_ = f.db.QueryRow("SELECT TIMESTAMPDIFF(DAY, starts_at, ends_at) FROM memberships WHERE owner_user_id = ? AND source = 'survey'", uid).Scan(&days)
	if days != 30 {
		t.Errorf("送 30 天会员：%d", days)
	}
	var est string
	_ = f.db.QueryRow("SELECT CAST(scores AS CHAR) FROM survey_responses WHERE owner_user_id = ?", uid).Scan(&est)
	if !strings.Contains(est, "est_low") {
		t.Errorf("保存当时的考前预估，用于校准：%s", est)
	}
	// 稍后补录取结果：不再送会员。
	in.Retest, in.Admission = "in", "admitted"
	if sv, err = f.sc.SubmitSurvey(ctx, uid, in); err != nil || sv.Admission != "admitted" || sv.Retest != "in" {
		t.Errorf("补录取结果：%+v %v", sv, err)
	}
	var n int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM memberships WHERE owner_user_id = ? AND source = 'survey'", uid).Scan(&n)
	if n != 1 {
		t.Errorf("每人只送一次：%d", n)
	}
	if sv, _ := f.sc.Survey(ctx, other); sv.Submitted {
		t.Error("别人的回访互不影响")
	}
}

func TestFeedback(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	fb := feedback.New(f.db, func() time.Time { return f.clock })
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	var mid uint64
	_ = f.db.QueryRow("SELECT id FROM materials WHERE owner_user_id = ? LIMIT 1", uid).Scan(&mid)
	if _, err := fb.Submit(ctx, uid, feedback.Input{Type: "nope", Content: "识别不准的问题"}); kind(err) != apperr.BadRequest {
		t.Errorf("类型：%v", err)
	}
	if _, err := fb.Submit(ctx, uid, feedback.Input{Type: "bug", Content: "短"}); kind(err) != apperr.BadRequest {
		t.Errorf("描述太短：%v", err)
	}
	if _, err := fb.Submit(ctx, uid, feedback.Input{Type: "bug", Content: "截图不是我的", Screenshots: []string{"u/999/hw/a.jpg"}}); kind(err) != apperr.NotFound {
		t.Errorf("别人的截图：%v", err)
	}
	other, _ := f.user(t)
	if _, err := fb.Submit(ctx, other, feedback.Input{Type: "recognition", Content: "别人的资料识别不准", MaterialID: mid}); kind(err) != apperr.NotFound {
		t.Errorf("别人的资料：%v", err)
	}
	got, err := fb.Submit(ctx, uid, feedback.Input{Type: "recognition", Content: "第 3 页的题目识别错了", Screenshots: []string{"u/" + uitoa(uid) + "/hw/1.jpg"},
		AllowAccess: true, MaterialID: mid})
	if err != nil || got.Status != "open" {
		t.Fatalf("提交：%+v %v", got, err)
	}
	var hours int
	_ = f.db.QueryRow("SELECT TIMESTAMPDIFF(HOUR, granted_at, expires_at) FROM content_access_grants WHERE user_id = ? AND source = 'feedback'", uid).Scan(&hours)
	if hours != 72 {
		t.Errorf("授权 72 小时：%d", hours)
	}
	list, err := fb.List(ctx, uid)
	if err != nil || len(list) != 1 || len(list[0].Screenshots) != 1 {
		t.Errorf("历史：%+v %v", list, err)
	}
	if l, _ := fb.List(ctx, other); len(l) != 0 {
		t.Error("看不到别人的反馈")
	}
}

func uitoa(v uint64) string { return strconv.FormatUint(v, 10) }

func firstFile(paths ...string) string {
	for _, p := range paths {
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func fonts(t *testing.T) export.Fonts {
	f := export.Fonts{
		CJK:   firstFile(os.Getenv("EXPORT_FONT_PATH"), "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf", "/app/fonts/DroidSansFallbackFull.ttf"),
		Latin: firstFile(os.Getenv("EXPORT_LATIN_FONT_PATH"), "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", "/app/fonts/DejaVuSans.ttf"),
	}
	if f.CJK == "" || f.Latin == "" {
		t.Skip("没有 TrueType 字体（安装 fonts-droid-fallback、fonts-dejavu-core 或设置 EXPORT_FONT_PATH、EXPORT_LATIN_FONT_PATH）")
	}
	return f
}

func download(t *testing.T, f *fx, j export.Job) []byte {
	t.Helper()
	key := "u/" + uitoa(f.ownerOf(t, j.ID)) + "/exports/" + uitoa(j.ID) + "." + j.Format
	r, err := f.oss.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("导出文件应在 OSS：%v", err)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	return b
}

func (f *fx) ownerOf(t *testing.T, jobID uint64) uint64 {
	var o uint64
	if err := f.db.QueryRow("SELECT owner_user_id FROM export_jobs WHERE id = ?", jobID).Scan(&o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestExportOnlyOwnData(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	ex := export.New(export.Deps{DB: f.db, OSS: f.oss, Fonts: fonts(t), Now: func() time.Time { return f.clock }})
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	mockPaper(t, f, uid, sid) // 产生错题和作答

	p, err := ex.Preview(ctx, uid, sid)
	if err != nil || p.Questions.Count != 4 || p.Questions.Pages < 1 || p.KPs.Count == 0 || p.Wrong.Count == 0 || p.AIVariants.Count != 0 {
		t.Fatalf("预览：%+v %v", p, err)
	}
	if _, err := ex.Create(ctx, uid, sid, export.Options{}, "pdf"); kind(err) != apperr.BadRequest {
		t.Errorf("至少选一项：%v", err)
	}
	all := export.Options{Questions: true, KPs: true, Wrong: true}
	j, err := ex.Create(ctx, uid, sid, all, "pdf")
	if err != nil || j.Status != "done" || j.DownloadURL == "" || j.ExpiresAt == nil || !strings.HasSuffix(j.FileName, ".pdf") {
		t.Fatalf("PDF：%+v %v", j, err)
	}
	if pdf := download(t, f, j); !bytes.HasPrefix(pdf, []byte("%PDF")) || len(pdf) < 1000 {
		t.Errorf("应是 PDF：%d 字节", len(pdf))
	}

	// 另一个用户导入不同的内容：各自的导出只包含自己的数据。
	other, osid := f.user(t)
	if _, err := f.mat.CreatePasted(ctx, other, osid, "question", "别人的资料", "2023 年真题\n一、名词解释（每题 10 分）\n1. 别人独有的题目甲\n答案：别人的答案。", true); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Get(ctx, other, j.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的导出：%v", err)
	}
	if _, err := ex.Preview(ctx, other, sid); kind(err) != apperr.NotFound {
		t.Errorf("别人的课：%v", err)
	}
	w, err := ex.Create(ctx, uid, sid, all, "docx")
	if err != nil || w.Status != "done" {
		t.Fatalf("Word：%+v %v", w, err)
	}
	docx := download(t, f, w)
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatalf("Word 应是 zip：%v", err)
	}
	var body string
	for _, zf := range zr.File {
		if zf.Name == "word/document.xml" {
			rc, _ := zf.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			body = string(b)
		}
	}
	for _, want := range []string{"题目和参考答案", "意境", "知识点卡片", "错题和我的作答", "我的作答"} {
		if !strings.Contains(body, want) {
			t.Errorf("Word 里应有「%s」", want)
		}
	}
	if strings.Contains(body, "别人独有的题目甲") {
		t.Error("不应包含别人的数据")
	}

	// 24 小时后删除文件，链接失效。
	f.clock = f.clock.Add(25 * time.Hour)
	if n, err := ex.Cleanup(ctx); err != nil || n != 2 {
		t.Errorf("清理：%d %v", n, err)
	}
	if g, _ := ex.Get(ctx, uid, j.ID); g.Status != "expired" || g.DownloadURL != "" {
		t.Errorf("过期：%+v", g)
	}
}
