package official

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/notify"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// 申请收录（PRD v3 增补「申请收录」，T46）：没有官方题库的用户申请收录自己的专业课；后台 7.16 按课归并处理。
//   - 按「院校 + 专业课名称」归并，有代码时优先按代码（rules.CourseKey）；同一用户同一门课只能申请一次
//   - 同一门课申请人数达到阈值（默认 3）标「建议立项」，新申请与建议立项推送到运营企业微信群（不含手机号）
//   - 超过 24 小时未处理标红；处理结果（已立项、已联系、暂不收录）与官方题库上线都通知所有申请人
//   - 后台只看申请表单信息，不看用户任何资料内容

// ApplyInput 是 1.11 的申请表单。
type ApplyInput struct {
	School, Major, SubjectName, SubjectCode string
	ExamYear                                int
	Entry                                   string // onboarding / bank / settings
}

// ApplyResult 是申请结果（1.11a 显示目前申请人数）。
type ApplyResult struct {
	GroupID        uint64
	ApplicantCount int
}

func (s *Service) officialParams(ctx context.Context) rules.OfficialFirstParams {
	if s.d.Params != nil {
		if p, err := s.d.Params.Rules(ctx); err == nil {
			return p.OfficialFirst
		}
	}
	return rules.DefaultParams().OfficialFirst
}

// Apply 提交申请收录。重复申请同一门课返回 409「你已申请过」（detail 带 applicant_count）。
func (s *Service) Apply(ctx context.Context, userID uint64, in ApplyInput) (ApplyResult, error) {
	in.School, in.Major = strings.TrimSpace(in.School), strings.TrimSpace(in.Major)
	in.SubjectName, in.SubjectCode = strings.TrimSpace(in.SubjectName), strings.TrimSpace(in.SubjectCode)
	if in.School == "" || in.Major == "" || in.SubjectName == "" || in.ExamYear < 2000 {
		return ApplyResult{}, apperr.New(apperr.BadRequest, "院校、专业、专业课名称、考试年份都要填")
	}
	entry := dbq.CourseApplicationsEntry(in.Entry)
	if !entry.Valid() {
		entry = dbq.CourseApplicationsEntryOnboarding
	}
	key := rules.CourseKey(in.School, in.SubjectCode, in.SubjectName)
	discipline, err := s.q.CatalogDiscipline(ctx, dbq.CatalogDisciplineParams{School: in.School, Code: nullStr(in.SubjectCode), Name: in.SubjectName})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ApplyResult{}, err
	}
	now := s.now().UTC()
	threshold := s.officialParams(ctx).ApplySuggestThreshold
	var g dbq.CourseApplicationGroup
	var suggested, dup bool
	err = store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		if err := q.UpsertApplicationGroup(ctx, dbq.UpsertApplicationGroupParams{GroupKey: key, School: in.School, Major: in.Major, SubjectName: in.SubjectName,
			SubjectCode: nullStr(in.SubjectCode), Discipline: discipline, PendingSince: sql.NullTime{Time: now, Valid: true}, Now: now}); err != nil {
			return err
		}
		var err error
		if g, err = q.LockApplicationGroupByKey(ctx, key); err != nil {
			return err
		}
		n, err := q.InsertCourseApplication(ctx, dbq.InsertCourseApplicationParams{OwnerUserID: userID, GroupID: g.ID, School: in.School, Major: in.Major,
			SubjectName: in.SubjectName, SubjectCode: nullStr(in.SubjectCode), ExamYear: uint16(in.ExamYear), Entry: entry, CreatedAt: now})
		if err != nil {
			return err
		}
		if n == 0 {
			dup = true
			return nil
		}
		if err := q.BumpApplicationGroup(ctx, dbq.BumpApplicationGroupParams{Now: now, ID: g.ID}); err != nil {
			return err
		}
		if g, err = q.GetApplicationGroup(ctx, g.ID); err != nil {
			return err
		}
		if threshold > 0 && int(g.ApplicantCount) >= threshold {
			n, err := q.SetApplicationGroupSuggested(ctx, g.ID)
			if err != nil {
				return err
			}
			if n > 0 {
				suggested, g.Status = true, dbq.CourseApplicationGroupsStatusSuggested
			}
		}
		return nil
	})
	if err != nil {
		return ApplyResult{}, err
	}
	if dup {
		return ApplyResult{}, apperr.New(apperr.Conflict, "你已申请过这门课，收录上线后会第一时间通知你").With("reason", "already_applied").
			With("applicant_count", g.ApplicantCount)
	}
	s.pushOps(ctx, g, suggested)
	// 已立项的课又有人申请：直接告诉他进度。
	if g.Status == dbq.CourseApplicationGroupsStatusInitiated {
		s.message(ctx, userID, g, "已立项", "「"+courseLabel(g)+"」已经立项制作官方题库，上线后会通知你")
	}
	return ApplyResult{GroupID: g.ID, ApplicantCount: int(g.ApplicantCount)}, nil
}

func courseLabel(g dbq.CourseApplicationGroup) string {
	label := g.School + " · " + g.SubjectName
	if g.SubjectCode.Valid {
		label += "（" + g.SubjectCode.String + "）"
	}
	return label
}

// pushOps 推送到运营企业微信群：新申请，或达到阈值的「建议立项」。只发院校专业课、人数与后台链接，不含手机号。
func (s *Service) pushOps(ctx context.Context, g dbq.CourseApplicationGroup, suggested bool) {
	head := "新的收录申请"
	if suggested {
		head = "建议立项"
	}
	text := fmt.Sprintf("【考研Training %s】%s · %s，当前 %d 人申请。处理：%s/official/applications?group=%d",
		head, courseLabel(g), g.Major, g.ApplicantCount, strings.TrimRight(s.d.AdminBaseURL, "/"), g.ID)
	s.d.Log.InfoContext(ctx, "course application", "group_id", g.ID, "applicants", g.ApplicantCount, "suggested", suggested)
	if s.d.OpsWebhook == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": text}})
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, s.d.OpsWebhook, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.d.HTTP.Do(req)
	if err != nil {
		s.d.Log.WarnContext(ctx, "ops webhook failed", "err", err)
		return
	}
	_ = resp.Body.Close()
}

func (s *Service) message(ctx context.Context, userID uint64, g dbq.CourseApplicationGroup, state, body string) {
	_ = s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeOfficialBank, Title: "收录申请" + state + "：" + g.SubjectName,
		Body: body, DedupeKey: sql.NullString{String: "course_apply:" + strconv.FormatUint(g.ID, 10) + ":" + state, Valid: true}})
}

// notifyApplicants 处理状态变化时通知所有申请人。
func (s *Service) notifyApplicants(ctx context.Context, g dbq.CourseApplicationGroup, state, body string) {
	ids, err := s.q.ApplicantIDs(ctx, g.ID)
	if err != nil {
		s.d.Log.WarnContext(ctx, "list applicants failed", "err", err)
		return
	}
	for _, u := range ids {
		s.message(ctx, u, g, state, body)
	}
}

// MyApplication 是用户自己的一条申请。
type MyApplication struct {
	ID                                      uint64
	School, Major, SubjectName, SubjectCode string
	ExamYear                                int
	Status                                  string
	ApplicantCount                          int
	CreatedAt                               time.Time
}

// MyApplications 列出用户自己的申请（备考设置里查看进度）。
func (s *Service) MyApplications(ctx context.Context, userID uint64) ([]MyApplication, error) {
	rows, err := s.q.MyCourseApplications(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]MyApplication, len(rows))
	for i, r := range rows {
		out[i] = MyApplication{ID: r.ID, School: r.School, Major: r.Major, SubjectName: r.SubjectName, SubjectCode: r.SubjectCode.String, ExamYear: int(r.ExamYear),
			Status: string(r.Status), ApplicantCount: int(r.ApplicantCount), CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// --- 后台 7.16 ---

// ApplicationStats 是 7.16 顶部的提醒条与统计卡，也用于 7.1「新的收录申请」与侧栏红点。
type ApplicationStats struct {
	Pending, Suggested, Overdue, WeekNew, SchoolMajors int
}

// ApplicationGroup 是按院校 · 专业 · 专业课汇总的一行。
type ApplicationGroup struct {
	ID                                                  uint64
	School, Major, SubjectName, SubjectCode, Discipline string
	Status, Reason                                      string
	ApplicantCount, PaidCount, SeedCount                int
	PendingSince                                        *time.Time
	Overdue                                             bool
	ProjectID                                           uint64
	LastAppliedAt                                       time.Time
}

func (s *Service) overdueBefore(ctx context.Context) time.Time {
	return s.now().UTC().Add(-time.Duration(s.officialParams(ctx).ApplyOverdueHours) * time.Hour)
}

// ApplicationSummary 是待处理、建议立项、超时与本周新增的计数。
func (s *Service) ApplicationSummary(ctx context.Context) (ApplicationStats, error) {
	now := s.now().UTC()
	st, err := s.q.ApplicationStats(ctx, dbq.ApplicationStatsParams{OverdueBefore: sql.NullTime{Time: s.overdueBefore(ctx), Valid: true}, WeekStart: now.AddDate(0, 0, -7)})
	if err != nil {
		return ApplicationStats{}, err
	}
	return ApplicationStats{Pending: int(st.Pending), Suggested: int(st.Suggested), Overdue: int(st.Overdue), WeekNew: int(st.WeekNew), SchoolMajors: int(st.SchoolMajors)}, nil
}

// ApplicationGroups 是 7.16 的列表：待处理置顶，可按学科筛选、只看待处理。
func (s *Service) ApplicationGroups(ctx context.Context, discipline string, onlyPending bool) ([]ApplicationGroup, error) {
	rows, err := s.q.AdminListApplicationGroups(ctx, dbq.AdminListApplicationGroupsParams{Discipline: strings.TrimSpace(discipline), OnlyPending: onlyPending})
	if err != nil {
		return nil, err
	}
	before := s.overdueBefore(ctx)
	out := make([]ApplicationGroup, len(rows))
	for i, r := range rows {
		g := ApplicationGroup{ID: r.ID, School: r.School, Major: r.Major, SubjectName: r.SubjectName, SubjectCode: r.SubjectCode.String, Discipline: r.Discipline,
			Status: string(r.Status), Reason: r.Reason.String, ApplicantCount: int(r.ApplicantCount), PaidCount: int(r.PaidCount), SeedCount: int(r.SeedCount),
			ProjectID: uint64(r.ProjectID.Int64), LastAppliedAt: r.LastAppliedAt}
		if r.PendingSince.Valid {
			t := r.PendingSince.Time
			g.PendingSince = &t
			g.Overdue = t.Before(before)
		}
		out[i] = g
	}
	return out, nil
}

// Applicant 是申请人：只有申请表单信息与标记。
type Applicant struct {
	Nickname   string
	ExamYear   int
	Entry      string
	AppliedAt  time.Time
	Paid, Seed bool
}

// Applicants 是 7.16 右侧详情的申请人列表。
func (s *Service) Applicants(ctx context.Context, groupID uint64) ([]Applicant, error) {
	if _, err := s.group(ctx, groupID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListApplicants(ctx, groupID)
	if err != nil {
		return nil, err
	}
	out := make([]Applicant, len(rows))
	for i, r := range rows {
		out[i] = Applicant{Nickname: r.Nickname, ExamYear: int(r.ExamYear), Entry: string(r.Entry), AppliedAt: r.CreatedAt, Paid: r.Paid, Seed: r.IsSeed}
	}
	return out, nil
}

func (s *Service) group(ctx context.Context, id uint64) (dbq.CourseApplicationGroup, error) {
	g, err := s.q.GetApplicationGroup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return g, apperr.NotFoundErr()
	}
	return g, err
}

func (s *Service) handle(ctx context.Context, adminID uint64, g dbq.CourseApplicationGroup, status dbq.CourseApplicationGroupsStatus, reason string, project uint64) error {
	return s.q.HandleApplicationGroup(ctx, dbq.HandleApplicationGroupParams{Status: status, Reason: nullStr(reason),
		ProjectID: sql.NullInt64{Int64: int64(project), Valid: project > 0}, HandledBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0},
		HandledAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: g.ID})
}

// InitiateApplication 立项：生成 7.11 官方题库项目（官方题库要有专业课代码，申请里没有时由运营补上），通知所有申请人。
func (s *Service) InitiateApplication(ctx context.Context, adminID, groupID uint64, code, major string) (uint64, error) {
	g, err := s.group(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if g.Status == dbq.CourseApplicationGroupsStatusInitiated {
		return 0, apperr.New(apperr.Conflict, "这门课已经立项了")
	}
	if code = strings.TrimSpace(code); code == "" {
		code = g.SubjectCode.String
	}
	if code == "" {
		return 0, apperr.New(apperr.BadRequest, "立项要填专业课代码").With("reason", "code_required")
	}
	if major = strings.TrimSpace(major); major == "" {
		major = g.Major
	}
	pid, err := s.CreateProject(ctx, adminID, g.School, major, code, g.SubjectName, nil)
	if err != nil {
		return 0, err
	}
	if err := s.handle(ctx, adminID, g, dbq.CourseApplicationGroupsStatusInitiated, "", pid); err != nil {
		return 0, err
	}
	s.notifyApplicants(ctx, g, "已立项", "你申请的「"+courseLabel(g)+"」已经立项制作官方题库，上线后会第一时间通知你")
	return pid, nil
}

// ContactApplicants 联系用户：给所有申请人发一条站内信（如征集真题、约访谈），状态记为「已联系」。
func (s *Service) ContactApplicants(ctx context.Context, adminID, groupID uint64, text string) error {
	g, err := s.group(ctx, groupID)
	if err != nil {
		return err
	}
	if text = strings.TrimSpace(text); text == "" || len([]rune(text)) > 500 {
		return apperr.New(apperr.BadRequest, "消息内容要填，最多 500 字")
	}
	if g.Status != dbq.CourseApplicationGroupsStatusInitiated {
		if err := s.handle(ctx, adminID, g, dbq.CourseApplicationGroupsStatusContacted, "", 0); err != nil {
			return err
		}
	}
	ids, err := s.q.ApplicantIDs(ctx, g.ID)
	if err != nil {
		return err
	}
	stamp := strconv.FormatInt(s.now().UnixMilli(), 10)
	for _, u := range ids {
		_ = s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: u, Mtype: dbq.MessagesMtypeOfficialBank, Title: "关于你申请收录的「" + g.SubjectName + "」",
			Body: text, DedupeKey: sql.NullString{String: "course_apply:" + strconv.FormatUint(g.ID, 10) + ":contact:" + stamp, Valid: true}})
	}
	return nil
}

// RejectApplication 暂不收录（原因必填），通知所有申请人。之后再有人申请时重新进入待处理。
func (s *Service) RejectApplication(ctx context.Context, adminID, groupID uint64, reason string) error {
	g, err := s.group(ctx, groupID)
	if err != nil {
		return err
	}
	if reason = strings.TrimSpace(reason); reason == "" {
		return apperr.New(apperr.BadRequest, "暂不收录要填原因")
	}
	if len([]rune(reason)) > 200 {
		return apperr.New(apperr.BadRequest, "原因最多 200 字")
	}
	if g.Status == dbq.CourseApplicationGroupsStatusInitiated {
		return apperr.New(apperr.Conflict, "已立项的课不能改为暂不收录")
	}
	if err := s.handle(ctx, adminID, g, dbq.CourseApplicationGroupsStatusRejected, reason, 0); err != nil {
		return err
	}
	s.notifyApplicants(ctx, g, "暂不收录", "你申请的「"+courseLabel(g)+"」暂不收录："+reason+"。先导入你自己的资料，同样能刷题、按采分点批改。")
	return nil
}

// notifyLaunchApplicants 官方题库首次上线时通知这门课的申请人。
func (s *Service) notifyLaunchApplicants(ctx context.Context, p Project) {
	ids, err := s.q.ApplicationGroupsOfProject(ctx, dbq.ApplicationGroupsOfProjectParams{ProjectID: sql.NullInt64{Int64: int64(p.ID), Valid: true},
		GroupKey: rules.CourseKey(p.School, p.SubjectCode, p.SubjectName)})
	if err != nil {
		return
	}
	for _, id := range ids {
		g, err := s.q.GetApplicationGroup(ctx, id)
		if err != nil {
			continue
		}
		users, err := s.q.ApplicantIDs(ctx, id)
		if err != nil {
			continue
		}
		for _, u := range users {
			_ = s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: u, Mtype: dbq.MessagesMtypeOfficialBank, Title: "官方题库上线：" + p.School + " " + p.SubjectName,
				Body: "你申请收录的「" + courseLabel(g) + "」官方题库已上线，所有用户免费使用，不需要上传任何资料", Link: notify.Link("official_banks", nil),
				DedupeKey: sql.NullString{String: "official_launch:" + strconv.FormatUint(p.BankID, 10), Valid: true}})
		}
	}
}

// SetSeed 在 7.2 标记或取消种子用户。
func (s *Service) SetSeed(ctx context.Context, userID uint64, seed bool) error {
	n, err := s.q.SetUserSeed(ctx, dbq.SetUserSeedParams{IsSeed: seed, ID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.q.GetUserSeed(ctx, userID); errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
	}
	return nil
}

// IsSeed 是 7.2 用户详情里的种子用户标记。
func (s *Service) IsSeed(ctx context.Context, userID uint64) (bool, error) {
	v, err := s.q.GetUserSeed(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, apperr.NotFoundErr()
	}
	return v, err
}
