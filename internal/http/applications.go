package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/official"
)

// T46 申请收录（PRD v3 增补）：App 1.11；后台 7.16、7.2 种子用户标记。

func init() {
	flagGuards["GET "+APIPrefix+"/course-applications"] = "course_apply"
	flagGuards["POST "+APIPrefix+"/course-applications"] = "course_apply"
}

func (h *Handlers) ApplyCourse(c *gin.Context) {
	var body gen.ApplyCourseJSONBody
	if !bind(c, &body) {
		return
	}
	in := official.ApplyInput{School: body.School, Major: body.Major, SubjectName: body.SubjectName, ExamYear: body.ExamYear}
	if body.SubjectCode != nil {
		in.SubjectCode = *body.SubjectCode
	}
	if body.Entry != nil {
		in.Entry = string(*body.Entry)
	}
	r, err := h.deps.Official.Apply(c.Request.Context(), currentUser(c), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"applicant_count": r.ApplicantCount})
}

func (h *Handlers) ListMyCourseApplications(c *gin.Context) {
	rows, err := h.deps.Official.MyApplications(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = gin.H{"id": r.ID, "school": r.School, "major": r.Major, "subject_name": r.SubjectName, "subject_code": optStr(r.SubjectCode),
			"exam_year": r.ExamYear, "status": r.Status, "applicant_count": r.ApplicantCount, "created_at": r.CreatedAt.UTC()}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) AdminCourseApplicationSummary(c *gin.Context) {
	st, err := h.deps.Official.ApplicationSummary(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pending": st.Pending, "suggested": st.Suggested, "overdue": st.Overdue, "week_new": st.WeekNew, "school_majors": st.SchoolMajors})
}

func (h *Handlers) AdminListCourseApplications(c *gin.Context, p gen.AdminListCourseApplicationsParams) {
	disc := ""
	if p.Discipline != nil {
		disc = *p.Discipline
	}
	rows, err := h.deps.Official.ApplicationGroups(c.Request.Context(), disc, p.OnlyPending != nil && *p.OnlyPending)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminCourseApplicationGroup, len(rows))
	for i, r := range rows {
		out[i] = gen.AdminCourseApplicationGroup{Id: int64(r.ID), School: r.School, Major: r.Major, SubjectName: r.SubjectName, SubjectCode: optStr(r.SubjectCode),
			Discipline: r.Discipline, Status: gen.CourseApplicationStatus(r.Status), Reason: optStr(r.Reason), ApplicantCount: r.ApplicantCount,
			PaidCount: r.PaidCount, SeedCount: r.SeedCount, PendingSince: r.PendingSince, Overdue: r.Overdue, ProjectId: optID(r.ProjectID), LastAppliedAt: r.LastAppliedAt}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) AdminListApplicants(c *gin.Context, groupID int64) {
	rows, err := h.deps.Official.Applicants(c.Request.Context(), uint64(groupID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = gin.H{"nickname": r.Nickname, "exam_year": r.ExamYear, "entry": r.Entry, "applied_at": r.AppliedAt.UTC(), "paid": r.Paid, "seed": r.Seed}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) AdminInitiateCourseApplication(c *gin.Context, groupID int64) {
	var body gen.AdminInitiateCourseApplicationJSONBody
	if !bind(c, &body) {
		return
	}
	code, major := "", ""
	if body.SubjectCode != nil {
		code = *body.SubjectCode
	}
	if body.Major != nil {
		major = *body.Major
	}
	pid, err := h.deps.Official.InitiateApplication(c.Request.Context(), currentAdmin(c).ID, uint64(groupID), code, major)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"project_id": pid})
}

func (h *Handlers) AdminContactApplicants(c *gin.Context, groupID int64) {
	var body gen.AdminContactApplicantsJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Official.ContactApplicants(c.Request.Context(), currentAdmin(c).ID, uint64(groupID), body.Message); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) AdminRejectCourseApplication(c *gin.Context, groupID int64) {
	var body gen.AdminRejectCourseApplicationJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Official.RejectApplication(c.Request.Context(), currentAdmin(c).ID, uint64(groupID), body.Reason); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) SetAdminUserSeed(c *gin.Context, userID int64) {
	var body gen.SetAdminUserSeedJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Official.SetSeed(c.Request.Context(), uint64(userID), body.Seed); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
