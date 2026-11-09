package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
)

func findCourse(cs []official.CatalogCourse, code string) (official.CatalogCourse, bool) {
	for _, c := range cs {
		if c.SubjectCode == code {
			return c, true
		}
	}
	return official.CatalogCourse{}, false
}

// TestExamSubjects 是 T50 的验收：考试科目按「院校 + 学科 + 代码 + 名称」维护，专业只是适用专业；
// 全国统考科目全国一条、只立一个项目，任何院校（包括库里没有的）都能选到，题库上线时通知所有选了它的用户。
func TestExamSubjects(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	uid, _ := f.user(t)

	// 旧版 App 不带 include_national：只有本校自命题的两门。
	cs, err := s.Courses(ctx, uid, "四川大学", 2027, true, false)
	if err != nil || len(cs) != 2 {
		t.Fatalf("旧版 App 的列表：%+v %v", cs, err)
	}
	// 新版 App：再加上 4 门全国统考；库里没有的院校只有统考。
	cs, _ = s.Courses(ctx, uid, "四川大学", 2027, true, true)
	if len(cs) != 6 {
		t.Fatalf("带统考的列表：%d", len(cs))
	}
	if c, ok := findCourse(cs, "333"); !ok || c.Scope != "national" || c.School != "" || c.Discipline != "教育学" || c.Registered {
		t.Errorf("统考 333：%+v", c)
	}
	if cs, _ := s.Courses(ctx, uid, "某冷门师范学院", 2027, true, true); len(cs) != 4 {
		t.Errorf("库里没有的院校只看到统考：%d", len(cs))
	}

	// 学科只能从固定选项选；统考必须有代码；同一院校同一代码只能有一条（专业不同也不行）。
	if _, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "北京师范大学", SubjectName: "中国语言文学", SubjectCode: "808", Discipline: "中国语言文学"}); kind(err) != apperr.BadRequest {
		t.Errorf("学科不在固定选项里：%v", err)
	}
	if _, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{Scope: "national", SubjectName: "某统考", Discipline: "教育学"}); kind(err) != apperr.BadRequest {
		t.Errorf("统考没有代码：%v", err)
	}
	bnu, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "北京师范大学", SubjectName: "中国语言文学", SubjectCode: "808", Discipline: "文学",
		Majors: []string{"文艺学", "古代文学", "文艺学", " "}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "北京师范大学", Major: "现当代文学", SubjectName: "中国语言文学综合", SubjectCode: "808", Discipline: "文学"}); kind(err) != apperr.Conflict {
		t.Errorf("同一院校同一代码：%v", err)
	}
	if _, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "北京师范大学", SubjectName: "中国语言文学", Discipline: "文学"}); kind(err) != apperr.Conflict {
		t.Errorf("同名且有一方没填代码：%v", err)
	}
	if _, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{Scope: "national", School: "随便填", SubjectName: "教育综合", SubjectCode: "333", Discipline: "教育学"}); kind(err) != apperr.Conflict {
		t.Errorf("统考同代码：%v", err)
	}
	// 旧版后台改科目只传 major：适用专业的其他项保留。
	if err := s.UpdateCatalogCourse(ctx, bnu, official.CatalogInput{School: "北京师范大学", Major: "文艺学", SubjectName: "中国语言文学", SubjectCode: "808", Discipline: "文学"}); err != nil {
		t.Fatal(err)
	}
	all, err := s.AdminCatalog(ctx, "北京师范", "", "")
	if err != nil || len(all) != 1 || len(all[0].Majors) != 2 || all[0].Majors[1] != "古代文学" {
		t.Fatalf("适用专业去重并保留：%+v %v", all, err)
	}

	// 本校有自命题 333 时，不再列统考 333（免得选错）。
	if _, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "某某师范大学", SubjectName: "教育综合", SubjectCode: "333", Discipline: "教育学"}); err != nil {
		t.Fatal(err)
	}
	cs, _ = s.Courses(ctx, uid, "某某师范大学", 2027, true, true)
	if c, _ := findCourse(cs, "333"); len(cs) != 4 || c.Scope != "school" {
		t.Errorf("本校自命题 333 优先：%+v", cs)
	}

	// 登记院校：只有统考能登记；登记后院校搜索能搜到、列表里标 registered。
	nat, err := s.AdminCatalog(ctx, "", "national", "教育学")
	if err != nil || len(nat) != 3 {
		t.Fatalf("按命题方式与学科筛选：%+v %v", nat, err)
	}
	var edu uint64
	for _, c := range nat {
		if c.SubjectCode == "333" {
			edu = c.ID
		}
	}
	if err := s.SetCourseSchools(ctx, 0, bnu, []string{"北京师范大学"}); kind(err) != apperr.BadRequest {
		t.Errorf("自命题科目不能登记院校：%v", err)
	}
	if err := s.SetCourseSchools(ctx, 0, edu, []string{"华东师范大学", " 华东师范大学 ", "陕西师范大学"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.CourseSchools(ctx, edu); len(got) != 2 {
		t.Errorf("登记的院校去重：%v", got)
	}
	if sc, _ := s.SearchSchools(ctx, "华东师范"); len(sc) != 1 || sc[0].CourseCount != 1 {
		t.Errorf("只登记了统考的院校也能搜到：%+v", sc)
	}
	cs, _ = s.Courses(ctx, uid, "华东师范大学", 2027, true, true)
	if c, _ := findCourse(cs, "333"); !c.Registered {
		t.Errorf("登记过的院校标 registered：%+v", c)
	}
	if got, _ := s.AdminCatalog(ctx, "陕西师范", "", ""); len(got) != 1 || got[0].SchoolCount != 2 {
		t.Errorf("后台按登记的院校搜到统考科目：%+v", got)
	}

	// 两所院校的考生都选了统考 333：专业课记的是各自的目标院校与选填的专业。
	u1, s1 := f.user(t)
	u2, s2 := f.user(t)
	if err := s.AttachSubject(ctx, u1, s1, official.CourseChoice{CatalogCourseID: edu, School: "华东师范大学", Major: "教育学原理"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSubject(ctx, u2, s2, official.CourseChoice{CatalogCourseID: edu, School: "某冷门师范学院"}); err != nil {
		t.Fatal(err)
	}
	var school, major string
	if err := f.db.QueryRow("SELECT school, COALESCE(major, '') FROM subjects WHERE id = ?", s1).Scan(&school, &major); err != nil || school != "华东师范大学" || major != "教育学原理" {
		t.Errorf("专业课记下目标院校与专业：%q %q %v", school, major, err)
	}

	// 需求洞察：统考按代码统计，两所院校的考生算在同一行，院校为空串。
	if _, err := f.db.Exec("UPDATE subjects SET code = '333' WHERE id IN (?, ?)", s1, s2); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkUnplanned(ctx, 0, "", "333", true); err != nil {
		t.Fatal(err)
	}
	ds, err := s.Demands(ctx, f.clock.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	var row *official.Demand
	for i := range ds {
		if ds[i].Code == "333" {
			if row != nil {
				t.Fatalf("统考 333 只有一行：%+v", ds)
			}
			row = &ds[i]
		}
	}
	if row == nil || row.School != "" || row.Users != 2 || !row.Unplanned {
		t.Fatalf("统考需求：%+v", row)
	}

	// 从考试科目立项：院校记为「全国统考」，一门考试科目只能立一个项目；立项清掉未规划标记。
	editor, lead := adminUser(t, f, "editor-333"), adminUser(t, f, "lead-333")
	pid, err := s.CreateProjectFromCourse(ctx, lead, edu, []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Project(ctx, pid)
	if err != nil || p.School != official.NationalSchool || p.CatalogCourseID != edu || p.SubjectCode != "333" {
		t.Fatalf("统考立项：%+v %v", p, err)
	}
	if _, err := s.CreateProjectFromCourse(ctx, lead, edu, nil); kind(err) != apperr.Conflict {
		t.Errorf("同一考试科目重复立项：%v", err)
	}
	if ds, _ := s.Demands(ctx, f.clock.AddDate(0, 0, -7)); len(ds) > 0 {
		for _, d := range ds {
			if d.Code == "333" && (d.Unplanned || d.ProjectID != pid) {
				t.Errorf("立项后需求洞察：%+v", d)
			}
		}
	}

	// 发布后任何院校看到的统考 333 都是「有官方题库」；两所院校的考生都收到上线通知。
	bankID := fillAndPublish(t, s, pid, editor, lead)
	for _, sc := range []string{"华东师范大学", "某冷门师范学院"} {
		cs, _ := s.Courses(ctx, uid, sc, 2027, true, true)
		if c, _ := findCourse(cs, "333"); c.OfficialStatus != official.OfficialAvailable || c.Official == nil || c.Official.BankID != bankID {
			t.Errorf("%s 的统考 333：%+v", sc, c)
		}
	}
	for _, u := range []uint64{u1, u2} {
		if n := countMsgs(t, f, u, "official_bank"); n != 1 {
			t.Errorf("选了统考 333 的用户 %d 收到上线通知：%d", u, n)
		}
	}

	// 旧版后台按「全国统考 + 代码」立项：挂到统考科目上（312 还没立项）。
	old, err := s.CreateProject(ctx, lead, official.NationalSchool, "", "312", "心理学专业基础", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Project(ctx, old); p.CatalogCourseID == 0 {
		t.Errorf("旧版后台立项挂到考试科目：%+v", p)
	}

	// 用户手动添加的 333 合并到统考：这所院校登记为使用该统考。
	u3, s3 := f.user(t)
	if err := s.AttachSubject(ctx, u3, s3, official.CourseChoice{School: "江西师范大学", Name: "教育综合", Code: "333"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE subjects SET code = '333' WHERE id = ?", s3); err != nil {
		t.Fatal(err)
	}
	pend, err := s.PendingCourses(ctx)
	if err != nil || len(pend) != 1 {
		t.Fatalf("待入库：%+v %v", pend, err)
	}
	if _, err := s.ResolvePending(ctx, 0, pend[0].ID, "merge", edu, official.CatalogInput{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.CourseSchools(ctx, edu); len(got) != 3 {
		t.Errorf("合并到统考后登记院校：%v", got)
	}
	var linked int64
	_ = f.db.QueryRow("SELECT COALESCE(catalog_course_id, 0) FROM subjects WHERE id = ?", s3).Scan(&linked)
	if uint64(linked) != edu {
		t.Errorf("合并后专业课关联到统考：%d", linked)
	}
}
