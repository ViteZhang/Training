// Package events 是埋点（PRD v3 第 15 节，dev-spec 第二节）：App 批量上报到 POST /events，服务端校验后按行写 JSON 日志
// （log_type = event），由 SLS 采集应用容器的标准输出入库；不接第三方统计 SDK。
//
// 事件里不能带作答原文和资料内容：事件名只收 PRD 15 节列出的；属性只收短的标量值，键名像正文的一律丢掉。
package events

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Names 是允许上报的事件（PRD v3 第 15 节「关键埋点事件」）。
var Names = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`
		login_success onboarding_step subject_add target_set stage_set
		import_start import_done import_fail confirm_edit confirm_submit
		plan_generate plan_complete score_estimate_view stage_change dashboard_view
		session_start session_finish answer_submit reveal_answer loss_attribution recite_finish
		paper_start time_reminder_shown paper_submit time_report_view essay_submit essay_rewrite
		grade_result grade_dispute grade_recheck rubric_edit question_report
		quota_block member_page_view pay_success redeem invite_success export
		survey_submit app_open official_bank_add`) {
		Names[n] = true
	}
}

// 限制：一批最多 100 条；每条最多 12 个属性；字符串属性最多 64 个字。
const (
	MaxBatch    = 100
	maxProps    = 12
	maxValueLen = 64
)

var (
	reKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// 像正文的键：即使值很短也不收，防止 App 误把作答、资料、手机号塞进来。
	contentKeys = []string{"text", "answer", "content", "stem", "note", "phone", "mobile", "name", "title", "body", "essay", "material"}
)

// Event 是一条埋点。
type Event struct {
	Name  string
	At    time.Time
	Props map[string]any
}

// Common 是每条事件的公共字段（PRD 15 节）：用户 ID 由服务端从登录态取，其余由 App 带上。
type Common struct {
	UserID      uint64
	SubjectCode string
	Stage       string
	AppVersion  string
	Platform    string
}

// Recorder 写埋点日志。
type Recorder struct {
	log *slog.Logger
	now func() time.Time
}

func New(log *slog.Logger, now func() time.Time) *Recorder {
	if now == nil {
		now = time.Now
	}
	return &Recorder{log: log, now: now}
}

// Record 写一批事件，返回收下的条数。不认识的事件跳过（App 新版本可能先于服务端加事件）；时间超出前后 7 天的按服务端时间记。
func (r *Recorder) Record(ctx context.Context, c Common, evs []Event) int {
	now := r.now().UTC()
	n := 0
	for i, e := range evs {
		if i >= MaxBatch || !Names[e.Name] {
			continue
		}
		at := e.At.UTC()
		if at.IsZero() || at.Before(now.AddDate(0, 0, -7)) || at.After(now.Add(24*time.Hour)) {
			at = now
		}
		attrs := []any{
			slog.String("log_type", "event"), slog.String("event", e.Name), slog.Time("event_at", at),
			slog.Uint64("user_id", c.UserID), slog.String("subject_code", short(c.SubjectCode)), slog.String("stage", short(c.Stage)),
			slog.String("app_version", short(c.AppVersion)), slog.String("platform", short(c.Platform)),
		}
		if p := Clean(e.Props); len(p) > 0 {
			attrs = append(attrs, slog.Any("props", p))
		}
		r.log.InfoContext(ctx, "event", attrs...)
		n++
	}
	return n
}

// Clean 只留下合规的属性：键名小写下划线、不像正文；值是布尔、数字，或不超过 64 个字的字符串。
func Clean(props map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range props {
		if len(out) >= maxProps || !reKey.MatchString(k) || looksLikeContent(k) {
			continue
		}
		switch x := v.(type) {
		case bool, float64, float32, int, int64:
			out[k] = x
		case string:
			if utf8.RuneCountInString(x) <= maxValueLen {
				out[k] = x
			}
		}
	}
	return out
}

func looksLikeContent(k string) bool {
	for _, c := range contentKeys {
		if k == c || strings.HasSuffix(k, "_"+c) || strings.HasPrefix(k, c+"_") {
			return true
		}
	}
	return false
}

func short(s string) string {
	if utf8.RuneCountInString(s) > 32 {
		return string([]rune(s)[:32])
	}
	return s
}
