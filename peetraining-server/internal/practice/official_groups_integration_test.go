package practice_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/profile"
)

// 北师大 808 中国语言文学 2025 的结构：名词解释必答；论述题按专业分组（限选），回答非报考专业不得分。
const exam808 = `一、名词解释（共 30 分，每题 6 分）
1. 复句
2. 儒家十三经
三、论述题（本题共分七组，请按专业分组答题，每组共 70 分，组内 2 小题，每小题 35 分，回答非报考专业不得分）
第一组：请选择文艺学、比较文学与世界文学专业的考生作答
1. 谈一谈异国形象中"他者"与"自我"的关系（35 分）
第二组：请选择古代文学、中国古典文献学专业的考生作答
1. 类书是什么？试列举三种类书，并说明类书对古典文献和古典文学研究的意义（35 分）
`

// copies 是用户题库里官方题副本：题干开头 → 状态。
func copies(t *testing.T, f *fx, uid uint64) map[string]string {
	t.Helper()
	rows, err := f.db.Query("SELECT stem, status FROM questions WHERE owner_user_id = ? AND official_question_id IS NOT NULL", uid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var stem, st string
		if err := rows.Scan(&stem, &st); err != nil {
			t.Fatal(err)
		}
		out[string([]rune(stem)[:2])] = st
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestOfficialGroups 是 T51 的验收：官方题分必答 / 限选；用户添加官方题库时限选题只复制报考方向那一组，
// 1.1 选填的专业就是报考方向，改方向后重新对；真题卷解析与表格导入都能标出限选题。
func TestOfficialGroups(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	course, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "北京师范大学", SubjectName: "中国语言文学", SubjectCode: "808", Discipline: "文学",
		Majors: []string{"文艺学", "古代文学"}})
	if err != nil {
		t.Fatal(err)
	}
	editor, lead := adminUser(t, f, "editor-808"), adminUser(t, f, "lead-808")
	pid, err := s.CreateProjectFromCourse(ctx, lead, course, []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}

	// 真题卷解析：分组标题下的题是限选题，名词解释是必答题。
	file, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "text", FileName: "2025 真题", Text: exam808, ExamYear: 2025})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.ImportItems(ctx, pid, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	var required, limited int
	for _, it := range d.Items {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		switch {
		case p.Group == nil:
			required++
		case strings.HasPrefix(p.Stem, "类书") && p.Group.Label == "第二组" && strings.Contains(p.Group.Majors, "古代文学"):
			limited++
		case strings.Contains(p.Stem, "他者") && p.Group.Label == "第一组" && strings.Contains(p.Group.Majors, "文艺学"):
			limited++
		}
	}
	if required != 2 || limited != 2 {
		t.Fatalf("解析出必答 %d、限选 %d：%+v", required, limited, d.Items)
	}

	// 表格导入：「限选分组」「适用专业」两列选填；只填了适用专业报错。
	csv := "来源类型,年份,题型,分值,题干,限选分组,适用专业\n" +
		"真题,2025,论述,35,论述语法的基本理论,第五组,语言学及应用语言学\n" +
		"真题,2025,简答,10,简述得意忘言说,,\n"
	tf, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: csv})
	if err != nil {
		t.Fatal(err)
	}
	td, _ := s.ImportItems(ctx, pid, tf.ID)
	var g0, g1 official.QuestionPayload
	_ = json.Unmarshal(td.Items[0].Payload, &g0)
	_ = json.Unmarshal(td.Items[1].Payload, &g1)
	if g0.Group == nil || g0.Group.Label != "第五组" || g0.Group.Majors != "语言学及应用语言学" || g1.Group != nil {
		t.Errorf("表格导入的限选分组：%+v %+v", g0.Group, g1.Group)
	}
	bf, _ := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: "题型,分值,题干,适用专业\n论述,35,论述省略现象,汉语言文字学\n"})
	if bd, _ := s.ImportItems(ctx, pid, bf.ID); bd.File.Status != "failed" {
		t.Errorf("只填适用专业应失败：%+v", bd.File)
	}

	// 7.12 草稿：限选题要有组名。
	rub := []official.Point{{Content: "要点一", Score: 20}, {Content: "要点二", Score: 15}}
	if _, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(official.QuestionPayload{QType: "discussion", Stem: "论述题", Rubric: rub,
		Group: &official.QuestionGroup{Majors: "文艺学"}})); kind(err) != apperr.BadRequest {
		t.Errorf("限选题没有组名：%v", err)
	}
	y := 2025
	sc5, sc35 := 6.0, 35.0
	qs := []official.QuestionPayload{
		{QType: "term", Stem: "复句", Answer: "由两个以上分句构成的句子。", Score: &sc5, ExamYear: &y, Rubric: []official.Point{{Content: "分句", Score: 6}}},
		{QType: "discussion", Stem: "他者与自我", Answer: "略", Score: &sc35, ExamYear: &y, Rubric: rub, Group: &official.QuestionGroup{Label: "第一组", Majors: "文艺学、比较文学与世界文学"}},
		{QType: "discussion", Stem: "类书的意义", Answer: "略", Score: &sc35, ExamYear: &y, Rubric: rub, Group: &official.QuestionGroup{Label: "第二组", Majors: "古代文学、中国古典文献学"}},
	}
	var drafts []uint64
	for _, q := range qs {
		dr, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(q))
		if err != nil {
			t.Fatal(err)
		}
		drafts = append(drafts, dr.ID)
	}
	approveAll(t, s, editor, lead, pid, drafts...)
	if _, err := s.Publish(ctx, lead, pid, ""); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Project(ctx, pid)
	var label string
	_ = f.db.QueryRow("SELECT COALESCE(group_label, '') FROM questions WHERE bank_id = ? AND owner_user_id IS NULL AND stem = '类书的意义'", p.BankID).Scan(&label)
	if label != "第二组" {
		t.Fatalf("发布后官方题记下限选分组：%q", label)
	}

	// 1.1 选了 808、报考专业填「古代文学」：限选题只复制第二组；没填专业的用户各组都有。
	ua, sa := f.user(t)
	if err := s.AttachSubject(ctx, ua, sa, official.CourseChoice{CatalogCourseID: course, School: "北京师范大学", Major: "古代文学"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(ctx, ua, p.BankID, sa); err != nil {
		t.Fatal(err)
	}
	if got := copies(t, f, ua); len(got) != 2 || got["复句"] != "active" || got["类书"] != "active" {
		t.Errorf("古代文学方向的副本：%v", got)
	}
	ub, sb := f.user(t)
	if err := s.Subscribe(ctx, ub, p.BankID, sb); err != nil {
		t.Fatal(err)
	}
	if got := copies(t, f, ub); len(got) != 3 {
		t.Errorf("没填方向时各组都保留：%v", got)
	}
	var gm string
	_ = f.db.QueryRow("SELECT COALESCE(group_majors, '') FROM questions WHERE owner_user_id = ? AND stem = '他者与自我'", ub).Scan(&gm)
	if gm != "文艺学、比较文学与世界文学" {
		t.Errorf("副本带上限选分组：%q", gm)
	}

	// 改报考方向为文艺学：第二组下线（作答记录保留），第一组补上；改回来恢复。
	wy := "文艺学"
	if _, err := f.prof.UpdateSubject(ctx, ua, sa, profile.SubjectPatch{MyMajors: &wy}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResyncSubject(ctx, ua, sa); err != nil {
		t.Fatal(err)
	}
	if got := copies(t, f, ua); got["他者"] != "active" || got["类书"] != "offline" || got["复句"] != "active" {
		t.Errorf("改方向后：%v", got)
	}
	gd := "古代文学"
	if _, err := f.prof.UpdateSubject(ctx, ua, sa, profile.SubjectPatch{MyMajors: &gd}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResyncUser(ctx, ua); err != nil {
		t.Fatal(err)
	}
	if got := copies(t, f, ua); got["他者"] != "offline" || got["类书"] != "active" {
		t.Errorf("改回来后：%v", got)
	}

	// 方向和哪一组都对不上时全部保留（免得整年的论述题都没了）。
	uc, sc := f.user(t)
	if err := s.AttachSubject(ctx, uc, sc, official.CourseChoice{CatalogCourseID: course, School: "北京师范大学", Major: "新闻学"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(ctx, uc, p.BankID, sc); err != nil {
		t.Fatal(err)
	}
	if got := copies(t, f, uc); len(got) != 3 {
		t.Errorf("方向对不上任何一组时都保留：%v", got)
	}

	// 已经有报考方向的不被 1.1 的专业覆盖。
	if _, err := f.db.Exec("UPDATE subjects SET my_majors = '现当代文学' WHERE id = ?", sb); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSubject(ctx, ub, sb, official.CourseChoice{CatalogCourseID: course, School: "北京师范大学", Major: "古代文学"}); err != nil {
		t.Fatal(err)
	}
	var mine string
	_ = f.db.QueryRow("SELECT my_majors FROM subjects WHERE id = ?", sb).Scan(&mine)
	if mine != "现当代文学" {
		t.Errorf("已有的报考方向不覆盖：%q", mine)
	}
}
