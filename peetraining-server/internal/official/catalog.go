package official

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// 考试科目库（PRD v3 增补「院校专业课库」T44、「考试科目」T50）：1.1 的院校搜索和科目列表都来自这里，由运营在后台手工维护。
// 一套卷子是一条记录（院校 + 学科 + 考试代码 + 科目名称），专业只是选填的「适用专业」。命题方式分院校自命题和全国统考：
// 统考科目不属于任何院校，全国只有一条、只立一个官方题库项目。表名沿用 catalog_courses。
// 每门课的官方题库状态不存，按 official_projects 实时算：立项挂在考试科目上；T50 之前的旧立项按院校 + 代码匹配。

// 官方题库状态：有 / 生产中 / 无。
const (
	OfficialAvailable = "available"
	OfficialProducing = "producing"
	OfficialNone      = "none"
)

// NationalSchool 是全国统考科目立项时记在 official_projects.school 里的院校名，题库标题和上线通知都显示它。
const NationalSchool = "全国统考"

// School 是院校搜索结果。
type School struct {
	Name        string
	CourseCount int
}

// OfficialSummary 是 1.1a 显示的官方题库摘要。
type OfficialSummary struct {
	BankID         uint64
	Version        string
	ExamYearCount  int
	KPCount        int
	PaperCount     int
	QuestionCount  int
	AddedSubjectID uint64
}

// CatalogCourse 是考试科目库里的一条考试科目。
type CatalogCourse struct {
	ID uint64
	// Scope 是命题方式：rules.ScopeSchool 院校自命题 / rules.ScopeNational 全国统考（School 为空串）。
	Scope       string
	School      string
	Major       string // 旧字段：适用专业的第一个
	Majors      []string
	Discipline  string
	SubjectName string
	SubjectCode string
	Textbooks   []string
	ExamYear    int
	// Registered：全国统考科目，查询的院校在后台登记了使用它。
	Registered bool
	// OfficialStatus 是 available / producing / none；官方题库开关对用户关闭时一律按 none（增补「功能开关」）。
	OfficialStatus string
	Official       *OfficialSummary
}

// SearchSchools 是 1.1 的院校搜索。
func (s *Service) SearchSchools(ctx context.Context, q string) ([]School, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []School{}, nil
	}
	rows, err := s.q.SearchCatalogSchools(ctx, dbq.SearchCatalogSchoolsParams{Q: q})
	if err != nil {
		return nil, err
	}
	out := make([]School, len(rows))
	for i, r := range rows {
		out[i] = School{Name: r.School, CourseCount: int(r.CourseCount)}
	}
	return out, nil
}

// majorsOf 是适用专业；旧数据没有 majors 时用 major。
func majorsOf(raw dbtypes.NullJSON, major string) []string {
	out := textbooksOf(raw)
	if len(out) == 0 && major != "" {
		out = []string{major}
	}
	return out
}

func textbooksOf(raw dbtypes.NullJSON) []string {
	out := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// Courses 按院校列出考试科目，标出官方题库状态。officialOn 是官方题库开关对这个用户是否打开；
// includeNational 时另外列出全国统考科目（新版 App），院校不在库里时只有统考科目。
func (s *Service) Courses(ctx context.Context, userID uint64, school string, examYear int, officialOn, includeNational bool) ([]CatalogCourse, error) {
	rows, err := s.q.ListCatalogCoursesBySchool(ctx, dbq.ListCatalogCoursesBySchoolParams{School: strings.TrimSpace(school),
		ExamYear: sql.NullInt16{Int16: int16(examYear), Valid: true}, IncludeNational: includeNational})
	if err != nil {
		return nil, err
	}
	var subs map[uint64]uint64
	out := make([]CatalogCourse, 0, len(rows))
	for _, r := range rows {
		c := CatalogCourse{ID: r.ID, Scope: string(r.Scope), School: r.School, Major: r.Major, Majors: majorsOf(r.Majors, r.Major), Discipline: r.Discipline,
			SubjectName: r.SubjectName, SubjectCode: r.SubjectCode.String, Textbooks: textbooksOf(r.Textbooks), ExamYear: int(r.ExamYear.Int16),
			Registered: r.Registered != 0, OfficialStatus: OfficialNone}
		if officialOn && r.ProjectID > 0 {
			sum, status, err := s.projectSummary(ctx, uint64(r.ProjectID))
			if err != nil {
				return nil, err
			}
			c.OfficialStatus = status
			if sum != nil {
				if subs == nil {
					if subs, err = s.subscriptions(ctx, userID); err != nil {
						return nil, err
					}
				}
				sum.AddedSubjectID = subs[sum.BankID]
				c.Official = sum
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) subscriptions(ctx context.Context, userID uint64) (map[uint64]uint64, error) {
	rows, err := s.q.ListMySubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	m := map[uint64]uint64{}
	for _, r := range rows {
		m[r.BankID] = r.SubjectID
	}
	return m, nil
}

// projectSummary 返回立项的官方题库状态；发布上线了的带摘要。
func (s *Service) projectSummary(ctx context.Context, projectID uint64) (*OfficialSummary, string, error) {
	p, err := s.q.GetOfficialProjectStage(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, OfficialNone, nil
	}
	if err != nil {
		return nil, "", err
	}
	if p.Stage != dbq.OfficialProjectsStagePublished || !p.BankID.Valid {
		return nil, OfficialProducing, nil
	}
	r, err := s.q.OfficialBankSummary(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	return &OfficialSummary{BankID: uint64(p.BankID.Int64), Version: str(r.Version), ExamYearCount: int(r.ExamYearCount), KPCount: int(r.KpCount),
		PaperCount: int(r.PaperCount), QuestionCount: int(r.QuestionCount)}, OfficialAvailable, nil
}

// CourseChoice 是用户在 1.1 选的课：从库里选（CatalogCourseID），或手动添加（院校 + 名称 + 代码）。
// School 是用户的目标院校（选全国统考科目时也带上），Major 是选填的专业，只用来展示和统计。
type CourseChoice struct {
	CatalogCourseID uint64
	School, Major   string
	Name, Code      string
}

// AttachSubject 在用户建好专业课之后记下它来自哪门课（1.1）：从库里选的关联上，官方题库上线时能通知到；
// 手动添加的、库里没有的课进「待入库」列表，只记归并后的课程与人数。
func (s *Service) AttachSubject(ctx context.Context, userID, subjectID uint64, c CourseChoice) error {
	if c.CatalogCourseID != 0 {
		cc, err := s.q.GetCatalogCourse(ctx, c.CatalogCourseID)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		// 旧版 App 不传院校和专业：用考试科目上的（统考科目没有院校）。
		school, major := strings.TrimSpace(c.School), strings.TrimSpace(c.Major)
		if school == "" {
			school = cc.School
		}
		if major == "" && c.School == "" {
			major = cc.Major
		}
		return s.q.SetSubjectCatalog(ctx, dbq.SetSubjectCatalogParams{CatalogCourseID: sql.NullInt64{Int64: int64(cc.ID), Valid: true},
			School: nullStr(school), Major: nullStr(major), ID: subjectID, OwnerUserID: userID})
	}
	school := strings.TrimSpace(c.School)
	if school == "" {
		return nil
	}
	if err := s.q.SetSubjectCatalog(ctx, dbq.SetSubjectCatalogParams{School: nullStr(school), Major: nullStr(strings.TrimSpace(c.Major)), ID: subjectID, OwnerUserID: userID}); err != nil {
		return err
	}
	key := rules.CourseKey(school, c.Code, c.Name)
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		if err := q.UpsertCatalogPending(ctx, dbq.UpsertCatalogPendingParams{GroupKey: key, School: school, Major: strings.TrimSpace(c.Major),
			SubjectName: strings.TrimSpace(c.Name), SubjectCode: nullStr(strings.TrimSpace(c.Code)), Now: s.now().UTC()}); err != nil {
			return err
		}
		p, err := q.GetCatalogPendingByKey(ctx, key)
		if err != nil {
			return err
		}
		if p.Status != dbq.CatalogPendingCoursesStatusPending {
			return nil
		}
		n, err := q.AddCatalogPendingUser(ctx, dbq.AddCatalogPendingUserParams{PendingID: p.ID, UserID: userID})
		if err != nil || n == 0 {
			return err
		}
		return q.IncCatalogPendingCount(ctx, p.ID)
	})
}

// --- 后台：考试科目库（配置组）---

// AdminCatalogCourse 是后台列表里的一条考试科目，带官方题库状态、在 1.1 选了它的人数与登记使用它的院校数。
type AdminCatalogCourse struct {
	CatalogCourse
	Status      string
	ChosenCount int
	SchoolCount int
	ProjectID   uint64
}

// CatalogInput 是新增 / 修改一条考试科目。Majors 为 nil 表示没传（旧版后台只传 Major）。
type CatalogInput struct {
	Scope                                               string
	School, Major, Discipline, SubjectName, SubjectCode string
	Majors                                              []string
	Textbooks                                           []string
	ExamYear                                            int
	Hidden                                              bool
}

func stageStatus(stage string) string {
	switch stage {
	case "":
		return OfficialNone
	case string(dbq.OfficialProjectsStagePublished):
		return OfficialAvailable
	default:
		return OfficialProducing
	}
}

// Disciplines 是考试科目可选的学科。
func (s *Service) Disciplines() []string { return slices.Clone(rules.Disciplines) }

// AdminCatalog 列出考试科目库：按命题方式、学科筛选，按院校、科目名或代码搜索。
func (s *Service) AdminCatalog(ctx context.Context, q, scope, discipline string) ([]AdminCatalogCourse, error) {
	q = strings.TrimSpace(q)
	rows, err := s.q.AdminListCatalogCourses(ctx, dbq.AdminListCatalogCoursesParams{Scope: dbq.CatalogCoursesScope(scope), Discipline: strings.TrimSpace(discipline),
		Q: sql.NullString{String: q, Valid: true}, SchoolQ: q})
	if err != nil {
		return nil, err
	}
	out := make([]AdminCatalogCourse, len(rows))
	for i, r := range rows {
		out[i] = AdminCatalogCourse{CatalogCourse: CatalogCourse{ID: r.ID, Scope: string(r.Scope), School: r.School, Major: r.Major, Majors: majorsOf(r.Majors, r.Major),
			Discipline: r.Discipline, SubjectName: r.SubjectName, SubjectCode: r.SubjectCode.String, Textbooks: textbooksOf(r.Textbooks), ExamYear: int(r.ExamYear.Int16),
			OfficialStatus: stageStatus(str(r.ProjectStage))},
			Status: string(r.Status), ChosenCount: int(r.ChosenCount), SchoolCount: int(r.SchoolCount), ProjectID: uint64(r.ProjectID)}
	}
	return out, nil
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, b := range in {
		if b = strings.TrimSpace(b); b != "" && !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	return out
}

func (in CatalogInput) validate() (CatalogInput, error) {
	in.School, in.Major, in.SubjectName = strings.TrimSpace(in.School), strings.TrimSpace(in.Major), strings.TrimSpace(in.SubjectName)
	in.SubjectCode, in.Discipline = strings.TrimSpace(in.SubjectCode), strings.TrimSpace(in.Discipline)
	switch in.Scope {
	case "", rules.ScopeSchool:
		in.Scope = rules.ScopeSchool
		if in.School == "" {
			return in, apperr.New(apperr.BadRequest, "院校自命题的科目要填院校")
		}
	case rules.ScopeNational:
		in.School = ""
		if in.SubjectCode == "" {
			return in, apperr.New(apperr.BadRequest, "全国统考科目要填考试代码")
		}
	default:
		return in, apperr.New(apperr.BadRequest, "命题方式不正确")
	}
	if in.SubjectName == "" {
		return in, apperr.New(apperr.BadRequest, "科目名称要填")
	}
	if !rules.ValidDiscipline(in.Discipline) {
		return in, apperr.New(apperr.BadRequest, "学科要从固定选项里选："+strings.Join(rules.Disciplines, "、"))
	}
	if in.Majors != nil {
		in.Majors = cleanList(in.Majors)
	} else if in.Major != "" {
		in.Majors = []string{in.Major}
	}
	in.Major = ""
	if len(in.Majors) > 0 {
		in.Major = in.Majors[0]
	}
	in.Textbooks = cleanList(in.Textbooks)
	return in, nil
}

func (in CatalogInput) key() string {
	return rules.ExamSubjectKey(in.Scope, in.School, in.SubjectCode, in.SubjectName, in.ExamYear)
}

func booksJSON(b []string) dbtypes.NullJSON {
	if len(b) == 0 {
		return nil
	}
	raw, _ := json.Marshal(b)
	return raw
}

func yearOf(y int) sql.NullInt16 { return sql.NullInt16{Int16: int16(y), Valid: y > 0} }

var errDupCourse = apperr.New(apperr.Conflict, "已经有这门考试科目（同一院校、考试代码与年份；全国统考看代码与年份）")

func dupErr(err error) error {
	if store.IsDuplicate(err) {
		return errDupCourse
	}
	return err
}

// checkSameName：同一院校、同一年份下同名的科目，有一方没填代码时算同一门（去重键只看代码，这里补上按名称的判断）。
func (s *Service) checkSameName(ctx context.Context, in CatalogInput, exclude uint64) error {
	rows, err := s.q.CatalogCoursesSameName(ctx, dbq.CatalogCoursesSameNameParams{Scope: dbq.CatalogCoursesScope(in.Scope), School: in.School, SubjectName: in.SubjectName,
		Year: sql.NullInt16{Int16: int16(in.ExamYear), Valid: true}, ExcludeID: exclude})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if in.SubjectCode == "" || r.SubjectCode.String == "" {
			return errDupCourse
		}
	}
	return nil
}

// CreateCatalogCourse 新增一条考试科目。
func (s *Service) CreateCatalogCourse(ctx context.Context, adminID uint64, in CatalogInput) (uint64, error) {
	in, err := in.validate()
	if err != nil {
		return 0, err
	}
	if err := s.checkSameName(ctx, in, 0); err != nil {
		return 0, err
	}
	id, err := s.q.InsertCatalogCourse(ctx, dbq.InsertCatalogCourseParams{Scope: dbq.CatalogCoursesScope(in.Scope), School: in.School, Major: in.Major,
		Majors: booksJSON(in.Majors), Discipline: in.Discipline, SubjectName: in.SubjectName, SubjectCode: nullStr(in.SubjectCode), Textbooks: booksJSON(in.Textbooks),
		ExamYear: yearOf(in.ExamYear), CourseKey: in.key(), CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}})
	return uint64(id), dupErr(err)
}

// UpdateCatalogCourse 修改一条考试科目；Hidden 为真时 1.1 不再显示（已选了它的用户不受影响）。
// 旧版后台只传 major：major 还是原来的第一个适用专业时保留其他适用专业。改成自命题时清掉登记的院校。
func (s *Service) UpdateCatalogCourse(ctx context.Context, id uint64, in CatalogInput) error {
	cur, err := s.q.AdminGetCatalogCourse(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	} else if err != nil {
		return err
	}
	if in.Scope == "" {
		in.Scope = string(cur.Scope)
	}
	if in.Majors == nil && strings.TrimSpace(in.Major) == cur.Major {
		in.Majors = majorsOf(cur.Majors, cur.Major)
	}
	if in, err = in.validate(); err != nil {
		return err
	}
	if err := s.checkSameName(ctx, in, id); err != nil {
		return err
	}
	status := dbq.CatalogCoursesStatusActive
	if in.Hidden {
		status = dbq.CatalogCoursesStatusHidden
	}
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		if err := dupErr(q.UpdateCatalogCourse(ctx, dbq.UpdateCatalogCourseParams{Scope: dbq.CatalogCoursesScope(in.Scope), School: in.School, Major: in.Major,
			Majors: booksJSON(in.Majors), Discipline: in.Discipline, SubjectName: in.SubjectName, SubjectCode: nullStr(in.SubjectCode), Textbooks: booksJSON(in.Textbooks),
			ExamYear: yearOf(in.ExamYear), CourseKey: in.key(), Status: status, ID: id})); err != nil {
			return err
		}
		if in.Scope != rules.ScopeNational {
			return q.DeleteCatalogSchoolExams(ctx, id)
		}
		return nil
	})
}

// nationalCourse 取一条全国统考科目；不存在返回 404，不是统考返回 400。
func (s *Service) nationalCourse(ctx context.Context, id uint64) error {
	cc, err := s.q.AdminGetCatalogCourse(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	if cc.Scope != dbq.CatalogCoursesScopeNational {
		return apperr.New(apperr.BadRequest, "只有全国统考科目能登记院校")
	}
	return nil
}

// CourseSchools 是登记使用这门全国统考的院校。
func (s *Service) CourseSchools(ctx context.Context, id uint64) ([]string, error) {
	if _, err := s.q.AdminGetCatalogCourse(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundErr()
	} else if err != nil {
		return nil, err
	}
	return s.q.ListCatalogSchoolExams(ctx, id)
}

// SetCourseSchools 整体替换登记使用这门全国统考的院校。登记是选填的：没登记的院校在 1.1 选到这个学科时也能看到统考科目，
// 登记后这所院校的科目不多时，统考科目直接出现在列表里（不用先选学科）。
func (s *Service) SetCourseSchools(ctx context.Context, adminID, id uint64, schools []string) error {
	if err := s.nationalCourse(ctx, id); err != nil {
		return err
	}
	schools = cleanList(schools)
	for _, sc := range schools {
		if utf8.RuneCountInString(sc) > 64 {
			return apperr.New(apperr.BadRequest, "院校名称太长")
		}
	}
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		if err := q.DeleteCatalogSchoolExams(ctx, id); err != nil {
			return err
		}
		for _, sc := range schools {
			if err := q.InsertCatalogSchoolExam(ctx, dbq.InsertCatalogSchoolExamParams{School: sc, CourseID: id, CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}}); err != nil {
				return err
			}
		}
		return nil
	})
}

// PendingCourse 是待入库的课。
type PendingCourse struct {
	ID                                      uint64
	School, Major, SubjectName, SubjectCode string
	RequestCount                            int
	FirstAt, LastAt                         time.Time
}

// PendingCourses 是「待入库」列表：用户在 1.1 手动添加、库里没有的课，按人数排序。
func (s *Service) PendingCourses(ctx context.Context) ([]PendingCourse, error) {
	rows, err := s.q.ListCatalogPending(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PendingCourse, len(rows))
	for i, r := range rows {
		out[i] = PendingCourse{ID: r.ID, School: r.School, Major: r.Major, SubjectName: r.SubjectName, SubjectCode: r.SubjectCode.String,
			RequestCount: int(r.RequestCount), FirstAt: r.FirstAt, LastAt: r.LastAt}
	}
	return out, nil
}

// ResolvePending 处理一条待入库：add 一键入库（可补学科与参考书）、merge 合并到已有课程（合并到全国统考时登记这所院校）、ignore 忽略。
// 入库或合并后，当初手动添加这门课的用户的专业课关联到库里的课。
func (s *Service) ResolvePending(ctx context.Context, adminID, pendingID uint64, action string, targetID uint64, in CatalogInput) (uint64, error) {
	p, err := s.q.GetCatalogPending(ctx, pendingID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperr.NotFoundErr()
	}
	if err != nil {
		return 0, err
	}
	if p.Status != dbq.CatalogPendingCoursesStatusPending {
		return 0, apperr.New(apperr.Conflict, "这条已经处理过了")
	}
	var status dbq.CatalogPendingCoursesStatus
	var course uint64
	var national bool
	switch action {
	case "add":
		status = dbq.CatalogPendingCoursesStatusAdded
		if in.School == "" {
			in.School = p.School
		}
		if in.Major == "" {
			in.Major = p.Major
		}
		if in.SubjectName == "" {
			in.SubjectName = p.SubjectName
		}
		if in.SubjectCode == "" {
			in.SubjectCode = p.SubjectCode.String
		}
		if course, err = s.CreateCatalogCourse(ctx, adminID, in); err != nil {
			return 0, err
		}
	case "merge":
		status = dbq.CatalogPendingCoursesStatusMerged
		target, err := s.q.AdminGetCatalogCourse(ctx, targetID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, apperr.New(apperr.BadRequest, "要合并到的课程不存在")
		} else if err != nil {
			return 0, err
		}
		course = targetID
		national = target.Scope == dbq.CatalogCoursesScopeNational
	case "ignore":
		status = dbq.CatalogPendingCoursesStatusIgnored
	default:
		return 0, apperr.New(apperr.BadRequest, "处理方式不正确")
	}
	err = store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		cid := sql.NullInt64{Int64: int64(course), Valid: course > 0}
		if err := q.ResolveCatalogPending(ctx, dbq.ResolveCatalogPendingParams{Status: status, CatalogCourseID: cid,
			HandledBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}, HandledAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: p.ID}); err != nil {
			return err
		}
		if course == 0 {
			return nil
		}
		// 合并到全国统考科目：这所院校登记为使用该统考（T50）。
		if national && p.School != "" {
			if err := q.InsertCatalogSchoolExam(ctx, dbq.InsertCatalogSchoolExamParams{School: p.School, CourseID: course,
				CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}}); err != nil {
				return err
			}
		}
		return q.LinkPendingSubjects(ctx, dbq.LinkPendingSubjectsParams{CourseID: cid, PendingID: p.ID, School: nullStr(p.School), Code: p.SubjectCode, Name: p.SubjectName})
	})
	return course, err
}
