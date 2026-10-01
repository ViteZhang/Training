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
