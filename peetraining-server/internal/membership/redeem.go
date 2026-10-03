package membership

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/store"
)

// Service 处理兑换码（6.7，PRD 13.4）。
type Service struct {
	db  *sql.DB
	q   *dbq.Queries
	rdb *redis.Client
	now func() time.Time
}

// Deps 是创建服务的依赖。Redis 为空时不限制兑换失败次数（测试与本地）。
type Deps struct {
	DB    *sql.DB
	Redis *redis.Client
	Now   func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: d.DB, q: dbq.New(d.DB), rdb: d.Redis, now: now}
}

// codeAlphabet 是兑换码字符集：8 位大写字母和数字，去掉 0 / O、1 / I 等易混字符（PRD 13.4）。
const (
	codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	codeLen      = 8
	// failLimit 是每小时最多兑换失败几次，防止穷举兑换码。
	failLimit = 10
)

// NormalizeCode 去掉空格与连字符并转成大写（兑换码不区分大小写）；不是合法格式时返回空串。
func NormalizeCode(code string) string {
	c := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code)))
	if len(c) != codeLen {
		return ""
	}
	for _, r := range c {
		if !strings.ContainsRune(codeAlphabet, r) {
			return ""
		}
	}
	return c
}

// HashCode 是兑换码在库里存的哈希（只存哈希，后台生成批次时用同一函数）。
func HashCode(normalized string) string {
	h := sha256.Sum256([]byte("redeem:" + normalized))
	return hex.EncodeToString(h[:])
}

// Redeemed 是兑换结果。
type Redeemed struct {
	Tier       string
	Days       int
	StartsAt   time.Time
	EndsAt     time.Time
	TotalUntil time.Time
}

func redeemErr(reason, msg string) error {
	return apperr.New(apperr.BadRequest, msg).With("reason", reason)
}

func failKey(userID uint64, now time.Time) string {
	return "redeem:fail:" + strconv.FormatUint(userID, 10) + ":" + now.UTC().Format("2006010215")
}

// Redeem 兑换（6.7）：一码一次，时长叠加到当前会员之后；无效、已使用、已作废、批次停用、过期分别提示。
func (s *Service) Redeem(ctx context.Context, userID uint64, code string) (Redeemed, error) {
	now := s.now()
	if s.rdb != nil {
		if n, _ := s.rdb.Get(ctx, failKey(userID, now)).Int(); n >= failLimit {
			return Redeemed{}, apperr.New(apperr.TooManyRequests, "尝试次数太多，请一小时后再试").With("reason", "too_many_attempts")
		}
	}
	out, err := s.redeem(ctx, userID, code, now)
	if err != nil && apperr.IsKind(err, apperr.BadRequest) && s.rdb != nil {
		k := failKey(userID, now)
		if n, _ := s.rdb.Incr(ctx, k).Result(); n == 1 {
			s.rdb.Expire(ctx, k, time.Hour)
		}
	}
	return out, err
}

func (s *Service) redeem(ctx context.Context, userID uint64, code string, now time.Time) (Redeemed, error) {
	c := NormalizeCode(code)
	if c == "" {
		return Redeemed{}, redeemErr("invalid", "兑换码无效，请检查后重试")
	}
	var out Redeemed
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		row, err := q.GetRedeemCodeForUpdate(ctx, HashCode(c))
		if errors.Is(err, sql.ErrNoRows) {
			return redeemErr("invalid", "兑换码无效，请检查后重试")
		}
		if err != nil {
			return err
		}
		switch {
		case row.Status == dbq.RedeemCodesStatusUsed:
			return redeemErr("used", "这个兑换码已经被使用过了")
		case row.Status == dbq.RedeemCodesStatusVoid:
			return redeemErr("void", "这个兑换码已作废")
		case row.BatchStatus == dbq.RedeemBatchesStatusDisabled:
			return redeemErr("disabled", "这个兑换码已停用")
		case !row.CodeExpiresAt.After(now):
			return redeemErr("expired", "这个兑换码已过期")
		}
		g, err := Apply(ctx, q, Grant{UserID: userID, Tier: string(row.Tier), Days: int(row.Days.Int16), Source: "redeem", SourceRef: row.ID, Now: now})
		if errors.Is(err, ErrNoExamDate) {
			return apperr.New(apperr.Conflict, "今年的初试日期还没公布，这类兑换码暂时不能使用，请稍后再试")
		}
		if err != nil {
			return err
		}
		n, err := q.UseRedeemCode(ctx, dbq.UseRedeemCodeParams{UsedBy: sql.NullInt64{Int64: int64(userID), Valid: true}, UsedAt: sql.NullTime{Time: now.UTC(), Valid: true},
			MembershipID: sql.NullInt64{Int64: int64(g.ID), Valid: true}, ID: row.ID})
		if err != nil {
			return err
		}
		if n == 0 {
			return redeemErr("used", "这个兑换码已经被使用过了")
		}
		out = Redeemed{Tier: string(row.Tier), Days: int(g.EndsAt.Sub(g.StartsAt).Hours() / 24), StartsAt: g.StartsAt, EndsAt: g.EndsAt, TotalUntil: g.TotalUntil}
		return nil
	})
	return out, err
}
