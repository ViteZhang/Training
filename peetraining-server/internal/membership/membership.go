// Package membership 判断会员身份与对应的额度上限（PRD 13）。开通、兑换、叠加在 T24、T25 实现。
package membership

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peetraining-server/internal/dbq"
)

// IsMember 判断用户此刻是否会员（有覆盖当前时间、未收回的会员时段）。
func IsMember(ctx context.Context, q dbq.Querier, userID uint64, now time.Time) (bool, error) {
	_, err := q.GetCurrentMembership(ctx, dbq.GetCurrentMembershipParams{OwnerUserID: userID, StartsAt: now, EndsAt: now})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Grant 是一次发放会员时长（兑换码、考后回访、邀请、订单）。Days > 0 时按天数；否则按档位：
// 月卡 30 天，冲刺卡至当年初试结束，考季卡至次年初试结束（PRD 13.2）。
type Grant struct {
	UserID    uint64
	Tier      string // sprint / season / monthly / gift
	Days      int
	Source    string // redeem / order / invite / survey / admin
	SourceRef uint64
	Now       time.Time
}

// Granted 是发放结果：本段时长与叠加后的最晚结束时间。
type Granted struct {
	ID         uint64
	StartsAt   time.Time
	EndsAt     time.Time
	TotalUntil time.Time
}

// ErrNoExamDate 表示后台还没配置初试日期，冲刺卡与考季卡算不出结束时间。
var ErrNoExamDate = errors.New("membership: 没有配置初试日期")

// monthlyDays 是月卡天数。
const monthlyDays = 30

// shanghai 是初试日期所在时区：初试最后一天 24 点（北京时间）结束。
var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// Span 计算一次发放的起止时间：起点叠加到当前会员之后（PRD 13.3、13.4）。会员中心预览「有效期至」也用它。
func Span(ctx context.Context, q dbq.Querier, g Grant) (time.Time, time.Time, error) {
	now := g.Now.UTC()
	start := now
	if last, err := q.GetLatestMembershipEnd(ctx, dbq.GetLatestMembershipEndParams{OwnerUserID: g.UserID, EndsAt: now}); err == nil {
		if last.EndsAt.After(start) {
			start = last.EndsAt.UTC()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, time.Time{}, err
	}
	switch {
	case g.Days > 0:
		return start, start.AddDate(0, 0, g.Days), nil
	case g.Tier == "monthly":
		return start, start.AddDate(0, 0, monthlyDays), nil
	case g.Tier == "sprint" || g.Tier == "season":
		dates, err := q.ListExamEndsFrom(ctx, start)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		idx := 0
		if g.Tier == "season" {
			idx = 1
		}
		if len(dates) <= idx {
			return time.Time{}, time.Time{}, ErrNoExamDate
		}
		d := dates[idx].FirstExamEnd
		return start, time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, shanghai).AddDate(0, 0, 1).UTC(), nil
	default:
		return time.Time{}, time.Time{}, errors.New("membership: 赠送天数必须大于 0")
	}
}

// Apply 在调用方的事务里发放会员：时长叠加到当前会员之后（PRD 13.3、13.4）。
func Apply(ctx context.Context, q *dbq.Queries, g Grant) (Granted, error) {
	start, end, err := Span(ctx, q, g)
	if err != nil {
		return Granted{}, err
	}
	ref := sql.NullInt64{Int64: int64(g.SourceRef), Valid: g.SourceRef != 0}
	id, err := q.InsertMembership(ctx, dbq.InsertMembershipParams{OwnerUserID: g.UserID, Tier: dbq.MembershipsTier(g.Tier), Source: dbq.MembershipsSource(g.Source),
		SourceRef: ref, StartsAt: start, EndsAt: end})
	if err != nil {
		return Granted{}, err
	}
	return Granted{ID: uint64(id), StartsAt: start, EndsAt: end, TotalUntil: end}, nil
}
