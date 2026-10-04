package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/notify"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// UserRow 是 7.2 用户列表的一行：只有计数与状态，手机号已脱敏。
type UserRow struct {
	ID           uint64
	PhoneMasked  string
	InviteCode   string
	Status       string
	Subjects     string
	Materials    int
	Questions    int
	IsMember     bool
	CreatedAt    time.Time
	LastActiveAt *time.Time
}

// UserFilters 是 7.2 的筛选：全部、付费、额度用完、解析失败、未导入资料。
var UserFilters = []string{"", "paid", "quota_out", "parse_failed", "no_material"}

// freeParsePages 读免费版累计解析页数，用于「额度用完」筛选。
func (s *Service) freeParsePages(ctx context.Context) int {
	var l struct {
		Free struct {
			ParsePagesTotal *int `json:"parse_pages_total"`
		} `json:"free"`
	}
	if err := s.d.Params.Get(ctx, "quota", &l); err != nil || l.Free.ParsePagesTotal == nil {
		return 100
	}
	return *l.Free.ParsePagesTotal
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// SearchUsers 搜索手机号、用户 ID、邀请码，按筛选条件列出用户（50 条一页，cursor 为上一页最后一个 ID）。
func (s *Service) SearchUsers(ctx context.Context, q, filter string, before uint64) ([]UserRow, error) {
	valid := false
	for _, f := range UserFilters {
		valid = valid || f == filter
	}
	if !valid {
		return nil, apperr.New(apperr.BadRequest, "未知筛选条件")
	}
	id, _ := strconv.ParseUint(q, 10, 64)
	rows, err := s.q.AdminSearchUsers(ctx, dbq.AdminSearchUsersParams{Now: s.now().UTC(), Q: q, UserID: int64(id), Filter: filter, FreePages: int64(s.freeParsePages(ctx)), BeforeID: int64(before)})
	if err != nil {
		return nil, err
	}
	out := make([]UserRow, len(rows))
	for i, r := range rows {
		out[i] = UserRow{ID: r.ID, PhoneMasked: MaskPhone(r.Phone), InviteCode: r.InviteCode, Status: string(r.Status), Subjects: r.Subjects.String,
			Materials: int(r.MaterialCount), Questions: int(r.QuestionCount), IsMember: r.IsMember, CreatedAt: r.CreatedAt, LastActiveAt: nullTimePtr(r.LastActiveAt)}
	}
	return out, nil
}

// SubjectStat 是用户一门专业课的统计（数字）。
type SubjectStat struct {
	Name        string
	Code        string
	FullScore   int
	TargetScore *int
	EstLow      *int
	EstHigh     *int
}

// UserDetail 是 7.2 用户详情：注册信息、资料与解析额度、题库统计（只有数量）、整卷与预估分、本周批改、会员、邀请。
type UserDetail struct {
	UserRow
	Nickname        string
	FailedMaterials int
	Pages           int
	KnowledgePoints int
	Papers          int
	WeekGradings    int
	Invited         int
	InviteDays      int
	MemberTier      string
	MemberUntil     *time.Time
	Subjects        []SubjectStat
	Quota           []quota.Item
}

// User 返回用户详情；不存在时 404。
func (s *Service) User(ctx context.Context, id uint64) (UserDetail, error) {
	u, err := s.q.AdminGetUser(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return UserDetail{}, apperr.NotFoundErr()
	}
	if err != nil {
		return UserDetail{}, err
	}
	now := s.now()
	c, err := s.q.AdminUserCounts(ctx, dbq.AdminUserCountsParams{UserID: int64(id), WeekStart: weekStart(now)})
	if err != nil {
		return UserDetail{}, err
	}
	subs, err := s.q.AdminUserSubjects(ctx, id)
	if err != nil {
		return UserDetail{}, err
	}
	items, member, err := s.d.Quota.Summary(ctx, id)
	if err != nil {
		return UserDetail{}, err
	}
	out := UserDetail{UserRow: UserRow{ID: u.ID, PhoneMasked: MaskPhone(u.Phone), InviteCode: u.InviteCode, Status: string(u.Status), Materials: int(c.Materials),
		Questions: int(c.Questions), IsMember: member, CreatedAt: u.CreatedAt, LastActiveAt: nullTimePtr(u.LastActiveAt)},
		Nickname: u.Nickname, FailedMaterials: int(c.FailedMaterials), Pages: int(c.Pages), KnowledgePoints: int(c.KnowledgePoints), Papers: int(c.Papers),
		WeekGradings: int(c.WeekGradings), Invited: int(c.Invited), InviteDays: int(c.InviteDays), Quota: items}
	if cur, err := s.q.GetCurrentMembership(ctx, dbq.GetCurrentMembershipParams{OwnerUserID: id, StartsAt: now.UTC(), EndsAt: now.UTC()}); err == nil {
		out.MemberTier = string(cur.Tier)
		if last, err := s.q.GetLatestMembershipEnd(ctx, dbq.GetLatestMembershipEndParams{OwnerUserID: id, EndsAt: now.UTC()}); err == nil {
			out.MemberUntil = &last.EndsAt
		}
	}
	for _, sb := range subs {
		st := SubjectStat{Name: sb.Name, Code: sb.Code.String, FullScore: int(sb.FullScore)}
		if sb.TargetScore.Valid {
			v := int(sb.TargetScore.Int16)
			st.TargetScore = &v
		}
		if sb.EstLow >= 0 && sb.EstHigh >= 0 {
			lo, hi := int(sb.EstLow), int(sb.EstHigh)
			st.EstLow, st.EstHigh = &lo, &hi
		}
		out.Subjects = append(out.Subjects, st)
		out.UserRow.Subjects += sb.Code.String + " "
	}
	return out, nil
}

func (s *Service) mustUser(ctx context.Context, id uint64) error {
	if _, err := s.q.AdminGetUser(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	} else if err != nil {
		return err
	}
	return nil
}

// GrantParsePages 给用户加解析额度（7.2、7.5 补偿）。key 让同一次操作重试不重复加。
func (s *Service) GrantParsePages(ctx context.Context, adminID, userID uint64, pages int, key string) error {
	if err := s.mustUser(ctx, userID); err != nil {
		return err
	}
	if pages <= 0 || pages > 2000 {
		return apperr.New(apperr.BadRequest, "页数需在 1–2000 之间")
	}
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		if err := s.d.Quota.Grant(ctx, q, userID, quota.ParsePages, pages, quota.Ref{Type: "admin", ID: adminID}, "admin_grant:"+key); err != nil {
			return err
		}
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeAnnouncement, Title: "资料解析额度已增加",
			Body: fmt.Sprintf("客服为你增加了 %d 页资料解析额度", pages), Link: notify.Link("library", nil),
			DedupeKey: sql.NullString{String: "admin_grant:" + key, Valid: true}})
	})
}

// GrantMembershipDays 赠送会员天数（7.2），叠加到当前会员之后。
func (s *Service) GrantMembershipDays(ctx context.Context, adminID, userID uint64, days int) (time.Time, error) {
	if err := s.mustUser(ctx, userID); err != nil {
		return time.Time{}, err
	}
	if days <= 0 || days > 400 {
		return time.Time{}, apperr.New(apperr.BadRequest, "天数需在 1–400 之间")
	}
	var end time.Time
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		g, err := membership.Apply(ctx, q, membership.Grant{UserID: userID, Tier: "gift", Days: days, Source: "admin", SourceRef: adminID, Now: s.now()})
		if err != nil {
			return err
		}
		end = g.EndsAt
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeMembership, Title: "获得会员赠送",
			Body: fmt.Sprintf("客服赠送了你 %d 天会员，已叠加到你的会员时长里", days), Link: notify.Link("member", nil),
			DedupeKey: sql.NullString{String: "admin_gift:" + strconv.FormatUint(g.ID, 10), Valid: true}})
	})
	return end, err
}

// ReparseFailed 重新解析用户最近解析失败的文件（7.2），返回重新排队的文件数。
func (s *Service) ReparseFailed(ctx context.Context, userID uint64) (int, error) {
	if err := s.mustUser(ctx, userID); err != nil {
		return 0, err
	}
	ids, err := s.q.AdminFailedMaterials(ctx, userID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, mid := range ids {
		job, err := s.q.AdminMaterialJob(ctx, mid)
		if err != nil {
			continue
		}
		if err := s.d.Import.RetryAsSystem(ctx, userID, job, mid); err != nil {
			s.d.Log.WarnContext(ctx, "admin reparse", "material_id", mid, "err", err)
			continue
		}
		n++
	}
	return n, nil
}

// SendMessage 给用户发一条站内消息（7.2；短信只用于验证码）。
func (s *Service) SendMessage(ctx context.Context, userID uint64, title, body string) error {
	if err := s.mustUser(ctx, userID); err != nil {
		return err
	}
	return s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeAnnouncement, Title: title, Body: body,
		DedupeKey: sql.NullString{String: "admin_msg:" + strconv.FormatInt(s.now().UnixNano(), 36), Valid: true}})
}

// SetBanned 封禁或解封账号（7.2）。封禁时作废全部登录令牌，下次请求即退出。
func (s *Service) SetBanned(ctx context.Context, userID uint64, banned bool) error {
	st := dbq.UsersStatusActive
	if banned {
		st = dbq.UsersStatusBanned
	}
	n, err := s.q.AdminSetUserStatus(ctx, dbq.AdminSetUserStatusParams{Status: st, ID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		if err := s.mustUser(ctx, userID); err != nil {
			return err
		}
	}
	if banned {
		return s.q.AdminRevokeUserTokens(ctx, dbq.AdminRevokeUserTokensParams{RevokedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, UserID: userID})
	}
	return nil
}

// Phone 返回完整手机号（只有管理员能调，调用由审计中间件记日志，PRD 10.1）。
func (s *Service) Phone(ctx context.Context, userID uint64) (string, error) {
	p, err := s.q.AdminGetUserPhone(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperr.NotFoundErr()
	}
	return p, err
}

// AuditRow 是一条操作记录。
type AuditRow struct {
	ID         uint64
	AdminName  string
	Action     string
	TargetType string
	TargetID   string
	Detail     map[string]any
	CreatedAt  time.Time
}

// AuditLogs 返回操作记录（7.2 用户详情的「操作记录」传 targetType=user；7.15 全部在 T29）。
func (s *Service) AuditLogs(ctx context.Context, targetType, targetID string, before uint64) ([]AuditRow, error) {
	rows, err := s.q.ListAdminAudit(ctx, dbq.ListAdminAuditParams{TargetType: targetType, TargetID: targetID, BeforeID: int64(before)})
	if err != nil {
		return nil, err
	}
	out := make([]AuditRow, len(rows))
	for i, r := range rows {
		out[i] = AuditRow{ID: r.ID, AdminName: r.DisplayName.String, Action: r.Action, TargetType: r.TargetType.String, TargetID: r.TargetID.String, CreatedAt: r.CreatedAt}
		if len(r.Detail) > 0 {
			_ = jsonUnmarshal(r.Detail, &out[i].Detail)
		}
	}
	return out, nil
}
