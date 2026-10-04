package admin

import (
	"context"
	"strconv"
	"time"

	"peetraining-server/internal/dbq"
)

// 统计指标（stats_hourly.metric）。日指标的 bucket 是北京时间那天 0 点；快照指标记在当天的 bucket 上，取最新一天。
const (
	mNewUsers    = "new_users"
	mActiveUsers = "active_users"
	mWeekActive  = "week_active_users" // bucket 为周一
	mAnswers     = "answers"
	mParsePages  = "parse_pages"
	mParseOK     = "parse_ok"
	mParseTotal  = "parse_total"
	mPayingUsers = "paying_users"
	mRevenue     = "revenue_cents"
	mDisputes    = "disputes"
	mGradings    = "gradings"
	mImportMode  = "import_mode" // dimension = 导入方式
	mFunnel      = "funnel"      // dimension = registered / imported / trained / retained / paid
	mMembers     = "members"
	mUsersTotal  = "users_total"
	mHotSubject  = "hot_subject" // dimension = 专业课代码
	parseTarget  = 0.95          // 解析成功率目标（PRD 10.2 7.5）
	disputeAlert = 0.15          // 异议率超过 15% 提醒「异议集中」
	overviewDays = 7
)

func (s *Service) put(ctx context.Context, metric, dim string, bucket time.Time, v float64) error {
	return s.q.UpsertStat(ctx, dbq.UpsertStatParams{Metric: metric, Dimension: dim, Bucket: bucket, Value: strconv.FormatFloat(v, 'f', 4, 64)})
}

// Aggregate 汇总统计到 stats_hourly（Worker 每小时一次；dev-spec 第十节：7.1 只读聚合表，不实时扫用户内容表）。
// 重算今天与昨天两个日桶（跨零点时补齐昨天），以及本周活跃与当前的漏斗、热门专业课快照。重复执行结果相同。
func (s *Service) Aggregate(ctx context.Context) error {
	now := s.now()
	today := dayStart(now)
	for _, day := range []time.Time{today.AddDate(0, 0, -1), today} {
		r, err := s.q.StatDay(ctx, dbq.StatDayParams{FromT: day, ToT: day.AddDate(0, 0, 1)})
		if err != nil {
			return err
		}
		for m, v := range map[string]int64{mNewUsers: r.NewUsers, mActiveUsers: r.ActiveUsers, mAnswers: r.Answers, mParsePages: r.ParsePages, mParseOK: r.ParseOk,
			mParseTotal: r.ParseTotal, mPayingUsers: r.PayingUsers, mRevenue: r.RevenueCents, mDisputes: r.Disputes, mGradings: r.Gradings} {
			if err := s.put(ctx, m, "", day, float64(v)); err != nil {
				return err
			}
		}
		modes, err := s.q.StatImportModes(ctx, dbq.StatImportModesParams{CreatedAt: day, CreatedAt_2: day.AddDate(0, 0, 1)})
		if err != nil {
			return err
		}
		for _, m := range modes {
			if err := s.put(ctx, mImportMode, string(m.Mode), day, float64(m.N)); err != nil {
				return err
			}
		}
	}
	ws := weekStart(now)
	wk, err := s.q.StatDay(ctx, dbq.StatDayParams{FromT: ws, ToT: ws.AddDate(0, 0, 7)})
	if err != nil {
		return err
	}
	if err := s.put(ctx, mWeekActive, "", ws, float64(wk.ActiveUsers)); err != nil {
		return err
	}
	f, err := s.q.StatFunnel(ctx, dbq.StatFunnelParams{Now: now.UTC()})
	if err != nil {
		return err
	}
	for dim, v := range map[string]int64{"registered": f.Registered, "imported": f.Imported, "trained": f.Trained, "retained": f.Retained, "paid": f.Paid} {
		if err := s.put(ctx, mFunnel, dim, today, float64(v)); err != nil {
			return err
		}
	}
	if err := s.put(ctx, mMembers, "", today, float64(f.Members)); err != nil {
		return err
	}
	if err := s.put(ctx, mUsersTotal, "", today, float64(f.Registered)); err != nil {
		return err
	}
	hot, err := s.q.StatHotSubjects(ctx)
	if err != nil {
		return err
	}
	for _, h := range hot {
		if err := s.put(ctx, mHotSubject, h.Code.String, today, float64(h.Users)); err != nil {
			return err
		}
	}
	return nil
}

// DayPoint 是一天的指标。
type DayPoint struct {
	Day          string // 2026-10-03
	NewUsers     int
	ActiveUsers  int
	Answers      int
	ParsePages   int
	RevenueCents int64
}

// Named 是带名字的计数（导入方式、漏斗、热门专业课）。
type Named struct {
	Name  string
	Value int
}

// Overview 是 7.1 概览。
type Overview struct {
	UpdatedAt       *time.Time
	UsersTotal      int
	WeekNewUsers    int
	WeekActiveUsers int
	WeekParsePages  int
	WeekParseRate   float64
	PaidUsers       int
	Members         int
	Conversion      float64 // 付费 / 导入过资料
	MonthRevenue    int64
	Funnel          []Named
	ImportModes     []Named
	HotSubjects     []Named
	Days            []DayPoint
	Alerts          []string
	WeekDisputeRate float64
}

// Overview 读聚合表生成 7.1 概览。
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	now := s.now()
	today := dayStart(now)
	from := today.AddDate(0, 0, -(overviewDays - 1))
	ms := monthStart(now)
	if ms.Before(from) {
		from = ms
	}
	if ws := weekStart(now); ws.Before(from) {
		from = ws
	}
	rows, err := s.q.ListStats(ctx, dbq.ListStatsParams{Bucket: from, Bucket_2: today})
	if err != nil {
		return Overview{}, err
	}
	var out Overview
	days := map[time.Time]*DayPoint{}
	modes := map[string]int{}
	latest := time.Time{}
	var weekParseOK, weekParseTotal, weekDisputes, weekGradings int
	for _, r := range rows {
		v, _ := strconv.ParseFloat(r.Value, 64)
		b := r.Bucket.UTC()
		if b.After(latest) {
			latest = b
		}
		inWeek := !b.Before(weekStart(now))
		if !b.Before(today.AddDate(0, 0, -(overviewDays-1))) && r.Metric != mWeekActive {
			if days[b] == nil {
				days[b] = &DayPoint{Day: b.In(shanghai).Format("2006-01-02")}
			}
		}
		d := days[b]
		switch r.Metric {
		case mNewUsers:
			if d != nil {
				d.NewUsers = int(v)
			}
			if inWeek {
				out.WeekNewUsers += int(v)
			}
		case mActiveUsers:
			if d != nil {
				d.ActiveUsers = int(v)
			}
		case mAnswers:
			if d != nil {
				d.Answers = int(v)
			}
		case mParsePages:
			if d != nil {
				d.ParsePages = int(v)
			}
			if inWeek {
				out.WeekParsePages += int(v)
			}
		case mParseOK:
			if inWeek {
				weekParseOK += int(v)
			}
		case mParseTotal:
			if inWeek {
				weekParseTotal += int(v)
			}
		case mRevenue:
			if d != nil {
				d.RevenueCents = int64(v)
			}
			if !b.Before(ms) {
				out.MonthRevenue += int64(v)
			}
		case mDisputes:
			if inWeek {
				weekDisputes += int(v)
			}
		case mGradings:
			if inWeek {
				weekGradings += int(v)
			}
		case mWeekActive:
			if b.Equal(weekStart(now)) {
				out.WeekActiveUsers = int(v)
			}
		case mImportMode:
			if inWeek {
				modes[r.Dimension] += int(v)
			}
		}
	}
	// 快照指标取最新一天。
	var funnel = map[string]int{}
	for _, r := range rows {
		if !r.Bucket.UTC().Equal(latest) {
			continue
		}
		v, _ := strconv.ParseFloat(r.Value, 64)
		switch r.Metric {
		case mFunnel:
			funnel[r.Dimension] = int(v)
		case mMembers:
			out.Members = int(v)
		case mUsersTotal:
			out.UsersTotal = int(v)
		case mHotSubject:
			out.HotSubjects = append(out.HotSubjects, Named{Name: r.Dimension, Value: int(v)})
		}
	}
	for _, k := range []string{"registered", "imported", "trained", "retained", "paid"} {
		out.Funnel = append(out.Funnel, Named{Name: k, Value: funnel[k]})
	}
	out.PaidUsers = funnel["paid"]
	if funnel["imported"] > 0 {
		out.Conversion = float64(funnel["paid"]) / float64(funnel["imported"])
	}
	for _, k := range []string{"question", "reference", "essay"} {
		out.ImportModes = append(out.ImportModes, Named{Name: k, Value: modes[k]})
	}
	sortNamed(out.HotSubjects)
	for i := overviewDays - 1; i >= 0; i-- {
		b := today.AddDate(0, 0, -i)
		if d := days[b]; d != nil {
			out.Days = append(out.Days, *d)
		} else {
			out.Days = append(out.Days, DayPoint{Day: b.In(shanghai).Format("2006-01-02")})
		}
	}
	if weekParseTotal > 0 {
		out.WeekParseRate = float64(weekParseOK) / float64(weekParseTotal)
		if out.WeekParseRate < parseTarget {
			out.Alerts = append(out.Alerts, "本周资料解析成功率低于 95%，去「资料解析监控」看失败任务")
		}
	}
	if weekGradings > 0 {
		out.WeekDisputeRate = float64(weekDisputes) / float64(weekGradings)
		if out.WeekDisputeRate > disputeAlert {
			out.Alerts = append(out.Alerts, "本周批改异议率偏高，去「批改异议」看原因分布")
		}
	}
	if !latest.IsZero() {
		if rows := rows; len(rows) > 0 {
			t := latest
			out.UpdatedAt = &t
		}
	}
	return out, nil
}

func sortNamed(xs []Named) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].Value > xs[j-1].Value; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
