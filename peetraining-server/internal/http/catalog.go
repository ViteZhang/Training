package http

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/official"
)

// T44 院校专业课库、引导分流与官方首题（PRD v3 增补）。

func init() {
	// 新 1.1 关闭时 App 回到旧 1.1（增补「功能开关」）。
	flagGuards["GET "+APIPrefix+"/catalog/schools"] = "onboarding_route"
	flagGuards["GET "+APIPrefix+"/catalog/courses"] = "onboarding_route"
	flagGuards["POST "+APIPrefix+"/onboarding/official-first"] = "official_bank"
}

func (h *Handlers) SearchCatalogSchools(c *gin.Context, p gen.SearchCatalogSchoolsParams) {
	rows, err := h.deps.Official.SearchSchools(c.Request.Context(), p.Q)
	if err != nil {
		_ = c.Error(err)
		return
	}
	type school struct {
		Name        string `json:"name"`
		CourseCount int    `json:"course_count"`
	}
	out := make([]school, len(rows))
	for i, r := range rows {
		out[i] = school{Name: r.Name, CourseCount: r.CourseCount}
	}
	c.JSON(http.StatusOK, items(out))
}

func toCatalogCourse(c official.CatalogCourse) gen.CatalogCourse {
	out := gen.CatalogCourse{Id: int64(c.ID), School: c.School, Major: c.Major, Discipline: c.Discipline, SubjectName: c.SubjectName,
		SubjectCode: optStr(c.SubjectCode), Textbooks: c.Textbooks, OfficialStatus: gen.CatalogCourseOfficialStatus(c.OfficialStatus)}
	if c.ExamYear > 0 {
		out.ExamYear = ptr(c.ExamYear)
	}
	if o := c.Official; o != nil {
		out.Official = &gen.OfficialSummary{BankId: int64(o.BankID), Version: o.Version, ExamYearCount: o.ExamYearCount, KpCount: o.KPCount,
			PaperCount: o.PaperCount, QuestionCount: o.QuestionCount, AddedSubjectId: optID(o.AddedSubjectID)}
	}
	return out
}

func (h *Handlers) ListCatalogCourses(c *gin.Context, p gen.ListCatalogCoursesParams) {
	ctx := c.Request.Context()
	uid := currentUser(c)
	on, err := h.deps.Flags.Enabled(ctx, "official_bank", uid)
	if err != nil {
		_ = c.Error(err)
		return
	}
	year := 0
	if p.ExamYear != nil {
		year = *p.ExamYear
	}
	rows, err := h.deps.Official.Courses(ctx, uid, p.School, year, on)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.CatalogCourse, len(rows))
	for i, r := range rows {
		out[i] = toCatalogCourse(r)
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) StartOfficialFirst(c *gin.Context) {
	var body gen.StartOfficialFirstJSONBody
	if !bind(c, &body) {
		return
	}
	s, err := h.deps.Practice.CreateOfficialFirst(c.Request.Context(), currentUser(c), uint64(body.BankId))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenSession(s))
}

// --- 后台：院校专业课库 ---

func catalogInput(in gen.CatalogCourseInput) official.CatalogInput {
	out := official.CatalogInput{School: in.School, Major: in.Major, SubjectName: in.SubjectName}
	if in.Discipline != nil {
		out.Discipline = *in.Discipline
	}
	if in.SubjectCode != nil {
		out.SubjectCode = *in.SubjectCode
	}
	if in.Textbooks != nil {
		out.Textbooks = *in.Textbooks
	}
	if in.ExamYear != nil {
		out.ExamYear = *in.ExamYear
	}
	if in.Hidden != nil {
		out.Hidden = *in.Hidden
	}
	return out
}

func (h *Handlers) AdminListCatalogCourses(c *gin.Context, p gen.AdminListCatalogCoursesParams) {
	q := ""
	if p.Q != nil {
		q = *p.Q
	}
	rows, err := h.deps.Official.AdminCatalog(c.Request.Context(), q)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminCatalogCourse, len(rows))
	for i, r := range rows {
		cc := toCatalogCourse(r.CatalogCourse)
		out[i] = gen.AdminCatalogCourse{Id: cc.Id, School: cc.School, Major: cc.Major, Discipline: cc.Discipline, SubjectName: cc.SubjectName, SubjectCode: cc.SubjectCode,
			Textbooks: cc.Textbooks, ExamYear: cc.ExamYear, OfficialStatus: gen.AdminCatalogCourseOfficialStatus(cc.OfficialStatus),
			Status: gen.AdminCatalogCourseStatus(r.Status), ChosenCount: r.ChosenCount}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) AdminCreateCatalogCourse(c *gin.Context) {
	var body gen.CatalogCourseInput
	if !bind(c, &body) {
		return
	}
	id, err := h.deps.Official.CreateCatalogCourse(c.Request.Context(), currentAdmin(c).ID, catalogInput(body))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *Handlers) AdminUpdateCatalogCourse(c *gin.Context, courseID int64) {
	var body gen.CatalogCourseInput
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Official.UpdateCatalogCourse(c.Request.Context(), uint64(courseID), catalogInput(body)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) AdminListCatalogPending(c *gin.Context) {
	rows, err := h.deps.Official.PendingCourses(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	type pending struct {
		ID           uint64    `json:"id"`
		School       string    `json:"school"`
		Major        string    `json:"major"`
		SubjectName  string    `json:"subject_name"`
		SubjectCode  *string   `json:"subject_code,omitempty"`
		RequestCount int       `json:"request_count"`
		FirstAt      time.Time `json:"first_at"`
		LastAt       time.Time `json:"last_at"`
	}
	out := make([]pending, len(rows))
	for i, r := range rows {
		out[i] = pending{ID: r.ID, School: r.School, Major: r.Major, SubjectName: r.SubjectName, SubjectCode: optStr(r.SubjectCode), RequestCount: r.RequestCount,
			FirstAt: r.FirstAt.UTC(), LastAt: r.LastAt.UTC()}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) AdminResolveCatalogPending(c *gin.Context, pendingID int64) {
	var body gen.AdminResolveCatalogPendingJSONBody
	if !bind(c, &body) {
		return
	}
	var in official.CatalogInput
	if body.Course != nil {
		in = catalogInput(*body.Course)
	}
	var target uint64
	if body.TargetCourseId != nil {
		target = uint64(*body.TargetCourseId)
	}
	if body.Action == "merge" && target == 0 {
		_ = c.Error(apperr.New(apperr.BadRequest, "请选择要合并到的课程"))
		return
	}
	id, err := h.deps.Official.ResolvePending(c.Request.Context(), currentAdmin(c).ID, uint64(pendingID), string(body.Action), target, in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"course_id": optID(id)})
}
