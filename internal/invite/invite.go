// Package invite 是邀请研友（6.8，PRD 13.4）：好友用邀请码注册、并导入第一份资料后，双方各得 7 天会员；
// 邀请人累计上限 70 天。受 invite 开关控制，默认关闭（ADR 0009）。
//
// 注册时绑定邀请关系（auth 在创建账号的事务里调 Bind），被邀请人第一次确认导入资料时激活（importer 在确认入库的
// 事务里调 Activate）。奖励只在激活时发一次，并发确认导入由行锁保证不重复发。
package invite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/params"
)

// FlagChecker 判断开关对某用户是否打开。
type FlagChecker interface {
	Enabled(ctx context.Context, key string, userID uint64) (bool, error)
}

// growth 是 rule_params.growth（后台 7.8 可改）。
type growth struct {
	InviteDays    int `json:"invite_days"`
	InviteMaxDays int `json:"invite_max_days"`
}

// Service 是邀请研友。
type Service struct {
	db     *sql.DB
	q      *dbq.Queries
	params *params.Store
	flags  FlagChecker
	now    func() time.Time
}

func New(db *sql.DB, ps *params.Store, fl FlagChecker, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, q: dbq.New(db), params: ps, flags: fl, now: now}
}

func (s *Service) rules(ctx context.Context) (growth, error) {
	g := growth{InviteDays: 7, InviteMaxDays: 70}
	if err := s.params.Get(ctx, "growth", &g); err != nil {
		return growth{}, err
	}
	return g, nil
}

// Bind 在注册事务里记录邀请关系。邀请码无效、填了自己的码、邀请开关对邀请人关闭时忽略，不影响注册（D34）。
func (s *Service) Bind(ctx context.Context, q *dbq.Queries, inviteeID uint64, code string, now time.Time) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	inviter, err := q.GetUserByInviteCode(ctx, code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if inviter.ID == inviteeID || inviter.Status != dbq.UsersStatusActive {
		return nil
	}
	on, err := s.flags.Enabled(ctx, flags.Invite, inviter.ID)
	if err != nil || !on {
		return err
	}
	return q.InsertInvite(ctx, dbq.InsertInviteParams{InviterID: inviter.ID, InviteeID: inviteeID, RegisteredAt: now.UTC()})
}

// Activate 在被邀请人确认导入资料的事务里调用：第一次时给被邀请人 7 天、邀请人 7 天（累计不超过 70 天），并发消息。
// 没有待激活的邀请时什么都不做，所以每次确认导入都可以调。
func (s *Service) Activate(ctx context.Context, q *dbq.Queries, inviteeID uint64) error {
	inv, err := q.GetPendingInviteForUpdate(ctx, inviteeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	g, err := s.rules(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if _, err := q.LockUserRow(ctx, inv.InviterID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	earned, err := q.SumInviterDays(ctx, inv.InviterID)
	if err != nil {
		return err
	}
	inviterDays := max(min(g.InviteDays, g.InviteMaxDays-int(earned)), 0)
	n, err := q.ActivateInvite(ctx, dbq.ActivateInviteParams{ActivatedAt: sql.NullTime{Time: now, Valid: true}, InviterDays: uint16(inviterDays), ID: inv.ID})
	if err != nil || n == 0 {
		return err
	}
	if _, err := membership.Apply(ctx, q, membership.Grant{UserID: inviteeID, Tier: "gift", Days: g.InviteDays, Source: "invite", SourceRef: inv.ID, Now: now}); err != nil {
		return err
	}
	if err := q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: inviteeID, Mtype: dbq.MessagesMtypeMembership, Title: "邀请奖励已到账",
		Body: "你导入了第一份资料，研友邀请奖励的会员天数已叠加到你的会员里", DedupeKey: sql.NullString{String: "invitee_reward", Valid: true}}); err != nil {
		return err
	}
	if inviterDays == 0 {
		return nil
	}
	if _, err := membership.Apply(ctx, q, membership.Grant{UserID: inv.InviterID, Tier: "gift", Days: inviterDays, Source: "invite", SourceRef: inv.ID, Now: now}); err != nil {
		return err
	}
	return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: inv.InviterID, Mtype: dbq.MessagesMtypeMembership, Title: "邀请奖励已到账",
		Body: "你邀请的研友导入了第一份资料，会员天数已叠加到你的会员里", DedupeKey: sql.NullString{String: "inviter_reward:" + strconv.FormatUint(inv.ID, 10), Valid: true}})
}

// Record 是一条邀请记录。好友只显示为「研友」，不透露对方的手机号与昵称。
type Record struct {
	RegisteredAt time.Time
	Activated    bool
	Days         int
}

// Overview 是 6.8 邀请研友。
type Overview struct {
	Code       string
	RewardDays int
	MaxDays    int
	EarnedDays int
	Invited    int
	Records    []Record
}

// Overview 返回我的邀请码、规则与邀请记录。开关关闭时由路由层返回 404。
func (s *Service) Overview(ctx context.Context, userID uint64) (Overview, error) {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	g, err := s.rules(ctx)
	if err != nil {
		return Overview{}, err
	}
	rows, err := s.q.ListMyInvites(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	total, err := s.q.CountMyInvites(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	earned, err := s.q.SumInviterDays(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{Code: u.InviteCode, RewardDays: g.InviteDays, MaxDays: g.InviteMaxDays, EarnedDays: int(earned), Invited: int(total), Records: make([]Record, len(rows))}
	for i, r := range rows {
		out.Records[i] = Record{RegisteredAt: r.RegisteredAt, Activated: r.ActivatedAt.Valid, Days: int(r.InviterDays)}
	}
	return out, nil
}
