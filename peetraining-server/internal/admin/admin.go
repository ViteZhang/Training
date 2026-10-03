// Package admin 是管理后台的服务（T28，PRD 10、dev-spec 第十节）：后台账号与两步验证、角色、操作审计、
// 7.1–7.7 运营与质量页面需要的统计与操作。
//
// 守住的规矩（CLAUDE.md 必须遵守第 5 条）：这个包只读计数、状态与元数据，不读用户资料、题目、作答的原文。
// 原文只在 content.go 里经 content_access_grants 授权读取，每次读取都由 notify.RecordAccess 记日志并通知用户。
package admin

import (
	"errors"

	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"log/slog"
	"slices"
	"time"

	"github.com/redis/go-redis/v9"

	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/cloud/sms"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/notify"
	"peetraining-server/internal/params"
	"peetraining-server/internal/payment"
	"peetraining-server/internal/quota"
)

// Role 是后台角色（PRD 10.1）。
type Role string

const (
	RoleAdmin         Role = "admin"          // 管理员：全部
	RoleSupport       Role = "support"        // 客服：用户、订单（只读）、反馈、兑换码查询；经授权查看资料
	RoleContentLead   Role = "content_lead"   // 内容负责人：官方题库（T30）
	RoleContentEditor Role = "content_editor" // 内容编辑：被分配的生产任务（T30）
	RoleAnalyst       Role = "analyst"        // 数据分析：概览、需求洞察，只看统计
)

// AllRoles 是全部角色。
var AllRoles = []Role{RoleAdmin, RoleSupport, RoleContentLead, RoleContentEditor, RoleAnalyst}

// Admin 是登录的后台账号。
type Admin struct {
	ID                 uint64
	Username           string
	DisplayName        string
	Roles              []Role
	MustChangePassword bool
	ExpiresAt          time.Time
}

// Has 判断是否有任一角色；管理员拥有全部权限。
func (a Admin) Has(roles ...Role) bool {
	if slices.Contains(a.Roles, RoleAdmin) {
		return true
	}
	for _, r := range roles {
		if slices.Contains(a.Roles, r) {
			return true
		}
	}
	return false
}

// Retrier 重新解析一个文件（importer.Service 实现）。
type Retrier interface {
	RetryAsSystem(ctx context.Context, userID, jobID, materialID uint64) error
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB      *sql.DB
	Redis   *redis.Client
	SMS     sms.Sender
	OSS     oss.Store
	Params  *params.Store
	Quota   *quota.Service
	Notify  *notify.Service
	Payment *payment.Service
	Import  Retrier
	Log     *slog.Logger
	Now     func() time.Time
	// Invalidate 是改配置后要立即失效的缓存（规则参数、功能开关）。
	Invalidate []Invalidator
	// LogCodes 为 true 时把两步验证码写进日志（只在本地与测试环境用 mock 短信时打开）。
	LogCodes bool
}

// Service 是管理后台。
type Service struct {
	d   Deps
	q   *dbq.Queries
	now func() time.Time
}

func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{d: d, q: dbq.New(d.DB), now: d.Now}
}

// shanghai 是业务日期所在时区（统计按北京时间的日与周）。
var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// dayStart 返回 t 所在北京时间那天 0 点（UTC 表示）。
func dayStart(t time.Time) time.Time {
	l := t.In(shanghai)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, shanghai).UTC()
}

// weekStart 返回 t 所在周的周一 0 点（北京时间）。
func weekStart(t time.Time) time.Time {
	d := dayStart(t)
	wd := int(d.In(shanghai).Weekday())
	if wd == 0 {
		wd = 7
	}
	return d.AddDate(0, 0, 1-wd)
}

// monthStart 返回 t 所在月 1 日 0 点（北京时间）。
func monthStart(t time.Time) time.Time {
	l := t.In(shanghai)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, shanghai).UTC()
}

// MaskPhone 把手机号脱敏为 138****5678（PRD 10.1：默认脱敏显示）。
func MaskPhone(p string) string {
	if len(p) < 7 {
		return "****"
	}
	return p[:3] + "****" + p[len(p)-4:]
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// str 把 sqlc 推断为 interface{} 的列（IFNULL 后的字符串）转成 string。
func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// isDuplicate 判断唯一键冲突（MySQL 1062）。
func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
