package official

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// 院校专业课库（PRD v3 增补「院校专业课库」，T44）：1.1 的院校搜索和专业课列表都来自这里，由运营在后台手工维护。
// 每门课的官方题库状态不存，按 official_projects 实时算（院校相同，代码相同，没有代码时名称相同）。

// 官方题库状态：有 / 生产中 / 无。
const (
	OfficialAvailable = "available"
	OfficialProducing = "producing"
	OfficialNone      = "none"
)

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

// CatalogCourse 是院校专业课库里的一门课。
type CatalogCourse struct {
	ID          uint64
	School      string
	Major       string
	Discipline  string
	SubjectName string
	SubjectCode string
	Textbooks   []string
	ExamYear    int
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
	rows, err := s.q.SearchCatalogSchools(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]School, len(rows))
	for i, r := range rows {
		out[i] = School{Name: r.School, CourseCount: int(r.CourseCount)}
	}
	return out, nil
}

func textbooksOf(raw dbtypes.NullJSON) []string {
	out := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// Courses 按院校列出专业与专业课，标出官方题库状态。officialOn 是官方题库开关对这个用户是否打开。
func (s *Service) Courses(ctx context.Context, userID uint64, school string, examYear int, officialOn bool) ([]CatalogCourse, error) {
	rows, err := s.q.ListCatalogCoursesBySchool(ctx, dbq.ListCatalogCoursesBySchoolParams{School: strings.TrimSpace(school), ExamYear: sql.NullInt16{Int16: int16(examYear), Valid: true}})
	if err != nil {
		return nil, err
	}
	var subs map[uint64]uint64
	out := make([]CatalogCourse, 0, len(rows))
	for _, r := range rows {
		c := CatalogCourse{ID: r.ID, School: r.School, Major: r.Major, Discipline: r.Discipline, SubjectName: r.SubjectName, SubjectCode: r.SubjectCode.String,
			Textbooks: textbooksOf(r.Textbooks), ExamYear: int(r.ExamYear.Int16), OfficialStatus: OfficialNone}
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
		return s.q.SetSubjectCatalog(ctx, dbq.SetSubjectCatalogParams{CatalogCourseID: sql.NullInt64{Int64: int64(cc.ID), Valid: true},
			School: nullStr(cc.School), Major: nullStr(cc.Major), ID: subjectID, OwnerUserID: userID})
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

// --- 后台：院校专业课库（配置组）---

// AdminCatalogCourse 是后台列表里的一门课，带官方题库状态与在 1.1 选了它的人数。
type AdminCatalogCourse struct {
	CatalogCourse
	Status      string
	ChosenCount int
}

// CatalogInput 是新增 / 修改一门课。
type CatalogInput struct {
	School, Major, Discipline, SubjectName, SubjectCode string
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

// AdminCatalog 列出院校专业课库（按院校、课程名或代码搜索）。
func (s *Service) AdminCatalog(ctx context.Context, q string) ([]AdminCatalogCourse, error) {
	rows, err := s.q.AdminListCatalogCourses(ctx, dbq.AdminListCatalogCoursesParams{Q: sql.NullString{String: strings.TrimSpace(q), Valid: true}})
	if err != nil {
		return nil, err
	}
	out := make([]AdminCatalogCourse, len(rows))
	for i, r := range rows {
		out[i] = AdminCatalogCourse{CatalogCourse: CatalogCourse{ID: r.ID, School: r.School, Major: r.Major, Discipline: r.Discipline, SubjectName: r.SubjectName,
			SubjectCode: r.SubjectCode.String, Textbooks: textbooksOf(r.Textbooks), ExamYear: int(r.ExamYear.Int16), OfficialStatus: stageStatus(str(r.ProjectStage))},
			Status: string(r.Status), ChosenCount: int(r.ChosenCount)}
	}
	return out, nil
}

func (in CatalogInput) validate() (CatalogInput, error) {
	in.School, in.Major, in.SubjectName = strings.TrimSpace(in.School), strings.TrimSpace(in.Major), strings.TrimSpace(in.SubjectName)
	in.SubjectCode, in.Discipline = strings.TrimSpace(in.SubjectCode), strings.TrimSpace(in.Discipline)
	if in.School == "" || in.Major == "" || in.SubjectName == "" {
		return in, apperr.New(apperr.BadRequest, "院校、专业、专业课名称都要填")
	}
	books := make([]string, 0, len(in.Textbooks))
	for _, b := range in.Textbooks {
		if b = strings.TrimSpace(b); b != "" {
			books = append(books, b)
		}
	}
	in.Textbooks = books
	return in, nil
}

func booksJSON(b []string) dbtypes.NullJSON {
	if len(b) == 0 {
		return nil
	}
	raw, _ := json.Marshal(b)
	return raw
}

func yearOf(y int) sql.NullInt16 { return sql.NullInt16{Int16: int16(y), Valid: y > 0} }

func dupErr(err error) error {
	if store.IsDuplicate(err) {
		return apperr.New(apperr.Conflict, "这所院校的这个专业下已经有同名专业课")
	}
	return err
}

// CreateCatalogCourse 新增一门课。
func (s *Service) CreateCatalogCourse(ctx context.Context, adminID uint64, in CatalogInput) (uint64, error) {
	in, err := in.validate()
	if err != nil {
		return 0, err
	}
	id, err := s.q.InsertCatalogCourse(ctx, dbq.InsertCatalogCourseParams{School: in.School, Major: in.Major, Discipline: in.Discipline, SubjectName: in.SubjectName,
		SubjectCode: nullStr(in.SubjectCode), Textbooks: booksJSON(in.Textbooks), ExamYear: yearOf(in.ExamYear), CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}})
	return uint64(id), dupErr(err)
}

// UpdateCatalogCourse 修改一门课；Hidden 为真时 1.1 不再显示（已选了它的用户不受影响）。
func (s *Service) UpdateCatalogCourse(ctx context.Context, id uint64, in CatalogInput) error {
	in, err := in.validate()
	if err != nil {
		return err
	}
	if _, err := s.q.AdminGetCatalogCourse(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	} else if err != nil {
		return err
	}
	status := dbq.CatalogCoursesStatusActive
	if in.Hidden {
		status = dbq.CatalogCoursesStatusHidden
	}
	return dupErr(s.q.UpdateCatalogCourse(ctx, dbq.UpdateCatalogCourseParams{School: in.School, Major: in.Major, Discipline: in.Discipline, SubjectName: in.SubjectName,
		SubjectCode: nullStr(in.SubjectCode), Textbooks: booksJSON(in.Textbooks), ExamYear: yearOf(in.ExamYear), Status: status, ID: id}))
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

// ResolvePending 处理一条待入库：add 一键入库（可补学科与参考书）、merge 合并到已有课程、ignore 忽略。
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
		if _, err := s.q.AdminGetCatalogCourse(ctx, targetID); errors.Is(err, sql.ErrNoRows) {
			return 0, apperr.New(apperr.BadRequest, "要合并到的课程不存在")
		} else if err != nil {
			return 0, err
		}
		course = targetID
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
		return q.LinkPendingSubjects(ctx, dbq.LinkPendingSubjectsParams{CourseID: cid, PendingID: p.ID, School: nullStr(p.School), Code: p.SubjectCode, Name: p.SubjectName})
	})
	return course, err
}
