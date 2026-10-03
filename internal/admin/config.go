package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// Invalidator 让缓存立即失效（params.Store、flags.Service 实现）：后台改了配置，App 下一次请求即生效。
type Invalidator interface{ Invalidate() }

// ---------- 7.8 规则参数（免费额度、会员价格、规则参数） ----------

// Param 是一条规则参数。
type Param struct {
	Key         string
	Value       json.RawMessage
	Description string
	Version     int
	UpdatedAt   time.Time
}

func (s *Service) Params(ctx context.Context) ([]Param, error) {
	rows, err := s.q.ListRuleParamsFull(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Param, len(rows))
	for i, r := range rows {
		out[i] = Param{Key: r.ParamKey, Value: r.Value, Description: r.Description, Version: int(r.Version), UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}

// sameShape 要求新值与旧值结构一致：同样的键、同样的类型，数字不能为负；旧值为 null 或数字的位置可以填 null 或数字
// （额度的 null 表示不限）。这样后台只能改数值，不会改出程序读不懂的结构。
func sameShape(path string, old, cur any) error {
	switch o := old.(type) {
	case map[string]any:
		c, ok := cur.(map[string]any)
		if !ok {
			return fmt.Errorf("%s 应为对象", path)
		}
		for k := range o {
			if _, ok := c[k]; !ok {
				return fmt.Errorf("缺少 %s.%s", path, k)
			}
		}
		for k, v := range c {
			ov, ok := o[k]
			if !ok {
				return fmt.Errorf("不认识的字段 %s.%s", path, k)
			}
			if err := sameShape(path+"."+k, ov, v); err != nil {
				return err
			}
		}
	case []any:
		c, ok := cur.([]any)
		if !ok {
			return fmt.Errorf("%s 应为数组", path)
		}
		if len(o) > 0 {
			for i, v := range c {
				if err := sameShape(fmt.Sprintf("%s[%d]", path, i), o[0], v); err != nil {
					return err
				}
			}
		}
	case float64, nil:
		switch c := cur.(type) {
		case float64:
			if c < 0 {
				return fmt.Errorf("%s 不能为负数", path)
			}
		case nil:
			if _, wasNum := old.(float64); wasNum && !nullable(path) {
				return fmt.Errorf("%s 不能为空", path)
			}
		default:
			return fmt.Errorf("%s 应为数字", path)
		}
	case bool:
		if _, ok := cur.(bool); !ok {
			return fmt.Errorf("%s 应为是 / 否", path)
		}
	case string:
		if _, ok := cur.(string); !ok {
			return fmt.Errorf("%s 应为文字", path)
		}
	}
	return nil
}

// nullable 是可以填「不限」（null）的字段：额度上限。
func nullable(path string) bool { return strings.HasPrefix(path, "quota.") }

// UpdateParam 修改一条规则参数（7.8）：结构必须与原来一致，规则参数要能通过 rules.ParseParams；version 防止覆盖别人刚改的值。
// 改完立即让缓存失效：App 下一次请求即按新值（T29 验收）。改价格只影响之后的新订单，改额度只影响之后的使用。
func (s *Service) UpdateParam(ctx context.Context, adminID uint64, key string, value json.RawMessage, version int) (Param, error) {
	cur, err := s.q.GetRuleParam(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Param{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Param{}, err
	}
	var oldV, newV any
	_ = json.Unmarshal(cur.Value, &oldV)
	if err := json.Unmarshal(value, &newV); err != nil {
		return Param{}, apperr.New(apperr.BadRequest, "不是合法的 JSON")
	}
	if err := sameShape(key, oldV, newV); err != nil {
		return Param{}, apperr.New(apperr.BadRequest, "参数格式不对："+err.Error())
	}
	all, err := s.q.ListRuleParamsFull(ctx)
	if err != nil {
		return Param{}, err
	}
	raw := map[string]json.RawMessage{}
	for _, r := range all {
		raw[r.ParamKey] = r.Value
	}
	raw[key] = value
	if _, err := rules.ParseParams(raw); err != nil {
		return Param{}, apperr.New(apperr.BadRequest, "规则参数校验没通过："+err.Error())
	}
	n, err := s.q.UpdateRuleParam(ctx, dbq.UpdateRuleParamParams{Value: value, UpdatedBy: sql.NullInt64{Int64: int64(adminID), Valid: true}, ParamKey: key, Version: uint32(version)})
	if err != nil {
		return Param{}, err
	}
	if n == 0 {
		return Param{}, apperr.New(apperr.Conflict, "这条参数刚被别人改过，请刷新后再改")
	}
	s.invalidate()
	r, err := s.q.GetRuleParam(ctx, key)
	if err != nil {
		return Param{}, err
	}
	return Param{Key: r.ParamKey, Value: r.Value, Description: r.Description, Version: int(r.Version), UpdatedAt: r.UpdatedAt}, nil
}

func (s *Service) invalidate() {
	for _, inv := range s.d.Invalidate {
		inv.Invalidate()
	}
}

// ---------- 7.8 协议正文与版本 ----------

// Agreement 是一个协议版本。
type Agreement struct {
	ID            uint64
	Kind          string
	Version       string
	Title         string
	Body          string
	ChangeSummary string
	EffectiveAt   time.Time
	PublishedAt   *time.Time
}

func (s *Service) Agreements(ctx context.Context) ([]Agreement, error) {
	rows, err := s.q.ListAllAgreements(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Agreement, len(rows))
	for i, r := range rows {
		out[i] = Agreement{ID: r.ID, Kind: string(r.Kind), Version: r.Version, Title: r.Title, ChangeSummary: r.ChangeSummary.String, EffectiveAt: r.EffectiveAt,
			PublishedAt: nullTimePtr(r.PublishedAt)}
	}
	return out, nil
}

func (s *Service) Agreement(ctx context.Context, id uint64) (Agreement, error) {
	r, err := s.q.GetAgreement(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Agreement{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Agreement{}, err
	}
	return Agreement{ID: r.ID, Kind: string(r.Kind), Version: r.Version, Title: r.Title, Body: r.Body, ChangeSummary: r.ChangeSummary.String, EffectiveAt: r.EffectiveAt,
		PublishedAt: nullTimePtr(r.PublishedAt)}, nil
}

// SaveAgreementDraft 新建（ID 为 0）或修改协议草稿；已发布的版本不能改，要改就发新版本。
func (s *Service) SaveAgreementDraft(ctx context.Context, a Agreement) (Agreement, error) {
	if strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Body) == "" {
		return Agreement{}, apperr.New(apperr.BadRequest, "标题和正文不能为空")
	}
	summary := sql.NullString{String: a.ChangeSummary, Valid: a.ChangeSummary != ""}
	if a.ID == 0 {
		if !regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`).MatchString(a.Version) {
			return Agreement{}, apperr.New(apperr.BadRequest, "版本号格式如 1.1")
		}
		id, err := s.q.InsertAgreement(ctx, dbq.InsertAgreementParams{Kind: dbq.AgreementsKind(a.Kind), Version: a.Version, Title: a.Title, Body: a.Body,
			ChangeSummary: summary, EffectiveAt: a.EffectiveAt.UTC()})
		if isDuplicate(err) {
			return Agreement{}, apperr.New(apperr.Conflict, "这个版本号已经存在")
		}
		if err != nil {
			return Agreement{}, err
		}
		return s.Agreement(ctx, uint64(id))
	}
	n, err := s.q.UpdateAgreementDraft(ctx, dbq.UpdateAgreementDraftParams{Title: a.Title, Body: a.Body, ChangeSummary: summary, EffectiveAt: a.EffectiveAt.UTC(), ID: a.ID})
	if err != nil {
		return Agreement{}, err
	}
	if n == 0 {
		return Agreement{}, apperr.New(apperr.Conflict, "已发布的版本不能修改，请新建版本")
	}
	return s.Agreement(ctx, a.ID)
}

// PublishAgreement 发布协议版本：老用户下次启动时看到 0.4b 确认（/bootstrap 返回未同意的最新版本），
// 消息中心的「协议更新」由定时任务发（T27）。
func (s *Service) PublishAgreement(ctx context.Context, id uint64) (Agreement, error) {
	n, err := s.q.PublishAgreement(ctx, dbq.PublishAgreementParams{PublishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id})
	if err != nil {
		return Agreement{}, err
	}
	if n == 0 {
		if _, err := s.Agreement(ctx, id); err != nil {
			return Agreement{}, err
		}
		return Agreement{}, apperr.New(apperr.Conflict, "这个版本已经发布过了")
	}
	return s.Agreement(ctx, id)
}

// ---------- 7.8 初试日期 ----------

// ExamDate 是一个研考年份的初试日期（北京时间日期）。
type ExamDate struct {
	Year            int
	Label           string
	FirstExamStart  string
	FirstExamEnd    string
	SubjectExamDate string
}

func (s *Service) ExamDates(ctx context.Context) ([]ExamDate, error) {
	rows, err := s.q.ListExamDates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ExamDate, len(rows))
	for i, r := range rows {
		out[i] = ExamDate{Year: int(r.ExamYear), Label: r.Label, FirstExamStart: r.FirstExamStart.Format("2006-01-02"), FirstExamEnd: r.FirstExamEnd.Format("2006-01-02"),
			SubjectExamDate: r.SubjectExamDate.Format("2006-01-02")}
	}
	return out, nil
}

// SaveExamDate 新增或修改某年的初试日期：结束不早于开始，专业课考试日在初试期间。
func (s *Service) SaveExamDate(ctx context.Context, e ExamDate) error {
	parse := func(v string) (time.Time, error) { return time.Parse("2006-01-02", v) }
	st, err1 := parse(e.FirstExamStart)
	en, err2 := parse(e.FirstExamEnd)
	sub, err3 := parse(e.SubjectExamDate)
	switch {
	case err1 != nil || err2 != nil || err3 != nil:
		return apperr.New(apperr.BadRequest, "日期格式应为 2026-12-20")
	case en.Before(st):
		return apperr.New(apperr.BadRequest, "初试结束不能早于开始")
	case sub.Before(st) || sub.After(en):
		return apperr.New(apperr.BadRequest, "专业课考试日应在初试期间")
	case e.Year < 2000 || e.Year > 2100:
		return apperr.New(apperr.BadRequest, "年份不对")
	}
	label := e.Label
	if label == "" {
		label = strconv.Itoa(e.Year) + " 研考"
	}
	if err := s.q.UpsertExamDate(ctx, dbq.UpsertExamDateParams{ExamYear: uint16(e.Year), Label: label, FirstExamStart: st, FirstExamEnd: en, SubjectExamDate: sub}); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// ---------- 7.8 功能开关 ----------

// Flag 是一个功能开关：全部打开，或只对指定用户打开（ADR 0009）。
type Flag struct {
	Key         string
	Description string
	EnabledAll  bool
	UserIDs     []uint64
	UpdatedAt   time.Time
}

func (s *Service) Flags(ctx context.Context) ([]Flag, error) {
	rows, err := s.q.ListFeatureFlagsFull(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Flag, len(rows))
	for i, r := range rows {
		f := Flag{Key: r.FlagKey, Description: r.Description, EnabledAll: r.EnabledForAll, UpdatedAt: r.UpdatedAt, UserIDs: []uint64{}}
		for _, p := range strings.Split(str(r.UserIds), ",") {
			if id, err := strconv.ParseUint(p, 10, 64); err == nil {
				f.UserIDs = append(f.UserIDs, id)
			}
		}
		out[i] = f
	}
	return out, nil
}

// SetFlag 设置开关：全部打开，或只对指定用户打开（最多 200 个）。不存在的用户 ID 忽略。
func (s *Service) SetFlag(ctx context.Context, adminID uint64, key string, all bool, userIDs []uint64) error {
	if len(userIDs) > 200 {
		return apperr.New(apperr.BadRequest, "指定用户最多 200 个")
	}
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		n, err := q.UpdateFeatureFlag(ctx, dbq.UpdateFeatureFlagParams{EnabledForAll: all, UpdatedBy: sql.NullInt64{Int64: int64(adminID), Valid: true}, FlagKey: key})
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := s.flagExists(ctx, q, key); err != nil {
				return err
			}
		}
		if err := q.DeleteFlagUsers(ctx, key); err != nil {
			return err
		}
		for _, id := range userIDs {
			if err := q.InsertFlagUser(ctx, dbq.InsertFlagUserParams{FlagKey: key, ID: id}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.invalidate()
	return nil
}

func (s *Service) flagExists(ctx context.Context, q *dbq.Queries, key string) (bool, error) {
	rows, err := q.ListFeatureFlagsFull(ctx)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(rows, func(r dbq.ListFeatureFlagsFullRow) bool { return r.FlagKey == key }) {
		return true, nil
	}
	return false, apperr.NotFoundErr()
}

// ---------- 7.8 App 版本 ----------

// AppVersion 是一个平台的最新与最低版本（低于最低版本强制更新，0.6b）。
type AppVersion struct {
	Platform     string
	Latest       string
	Min          string
	DownloadURL  string
	ReleaseNotes string
	UpdatedAt    time.Time
}

func (s *Service) AppVersions(ctx context.Context) ([]AppVersion, error) {
	rows, err := s.q.ListAppVersions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AppVersion, len(rows))
	for i, r := range rows {
		out[i] = AppVersion{Platform: string(r.Platform), Latest: r.LatestVersion, Min: r.MinVersion, DownloadURL: r.DownloadUrl, ReleaseNotes: r.ReleaseNotes.String, UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}

var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range 3 {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	return false
}

func (s *Service) SaveAppVersion(ctx context.Context, v AppVersion) error {
	switch {
	case v.Platform != "ios" && v.Platform != "android":
		return apperr.New(apperr.BadRequest, "平台只能是 ios 或 android")
	case !semver.MatchString(v.Latest) || !semver.MatchString(v.Min):
		return apperr.New(apperr.BadRequest, "版本号格式如 1.2.0")
	case versionLess(v.Latest, v.Min):
		return apperr.New(apperr.BadRequest, "最低版本不能高于最新版本")
	case !strings.HasPrefix(v.DownloadURL, "https://"):
		return apperr.New(apperr.BadRequest, "下载地址必须是 https")
	}
	return s.q.UpsertAppVersion(ctx, dbq.UpsertAppVersionParams{Platform: dbq.AppVersionsPlatform(v.Platform), LatestVersion: v.Latest, MinVersion: v.Min,
		DownloadUrl: v.DownloadURL, ReleaseNotes: sql.NullString{String: v.ReleaseNotes, Valid: v.ReleaseNotes != ""}})
}

// ---------- 7.8 AI 任务与灰度 ----------

// VersionStat 是一个「模型 + 提示词版本」近 7 天的表现。
type VersionStat struct {
	Model       string
	Prompt      string
	Calls       int
	SuccessRate float64
	AvgCostYuan float64
	AvgLatency  int
}

// AITask 是 7.8 AI 任务列表的一行。
type AITask struct {
	Capability      string
	StableModel     string
	StablePrompt    string
	CandidateModel  string
	CandidatePrompt string
	CandidatePct    int
	Prompts         []string // 可选的提示词版本
	Calls           int
	CostYuan        float64
	CostShare       float64
	Versions        []VersionStat
}

// AIOverview 是 7.8 顶部：每活跃用户每天 AI 成本与预算、本月 AI 成本及占收入比例。
type AIOverview struct {
	CostPerActiveUserDay float64
	BudgetPerUserDay     float64
	MonthCostYuan        float64
	MonthRevenueYuan     float64
	RevenueRatio         float64
	RatioMax             float64
	Tasks                []AITask
}

const microYuan = 1_000_000.0

func (s *Service) AIOverview(ctx context.Context) (AIOverview, error) {
	now := s.now()
	week := now.UTC().Add(-statWindow)
	stats, err := s.q.AIStatsByVersion(ctx, week)
	if err != nil {
		return AIOverview{}, err
	}
	rolls, err := s.q.ListAIRollouts(ctx)
	if err != nil {
		return AIOverview{}, err
	}
	var budget struct {
		Daily float64 `json:"daily_cost_per_active_user_yuan"`
		Ratio float64 `json:"revenue_ratio_max"`
	}
	_ = s.d.Params.Get(ctx, "ai_budget", &budget)
	out := AIOverview{BudgetPerUserDay: budget.Daily, RatioMax: budget.Ratio}

	byCap := map[string]*AITask{}
	task := func(c string) *AITask {
		if byCap[c] == nil {
			byCap[c] = &AITask{Capability: c}
		}
		return byCap[c]
	}
	catalog := ai.PromptCatalog()
	for c, vs := range catalog {
		t := task(c)
		t.Prompts = append([]string{}, vs...)
		sort.Strings(t.Prompts)
	}
	var weekCost float64
	for _, r := range stats {
		t := task(r.Capability)
		cost := float64(r.Cost) / microYuan
		t.Calls += int(r.Calls)
		t.CostYuan += cost
		weekCost += cost
		vs := VersionStat{Model: r.Model, Prompt: r.PromptVersion, Calls: int(r.Calls), AvgLatency: int(r.Latency)}
		if r.Calls > 0 {
			vs.SuccessRate = float64(r.Ok) / float64(r.Calls)
			vs.AvgCostYuan = cost / float64(r.Calls)
		}
		t.Versions = append(t.Versions, vs)
	}
	for _, r := range rolls {
		t := task(r.Capability)
		t.StableModel, t.StablePrompt = r.StableModel, r.StablePrompt
		t.CandidateModel, t.CandidatePrompt, t.CandidatePct = r.CandidateModel.String, r.CandidatePrompt.String, int(r.CandidatePercent)
	}
	for _, t := range byCap {
		if weekCost > 0 {
			t.CostShare = t.CostYuan / weekCost
		}
		if t.StablePrompt == "" && len(t.Prompts) > 0 {
			t.StablePrompt = t.Prompts[len(t.Prompts)-1] // 没配灰度时用代码里的默认版本（目前都是最新版本）
		}
		out.Tasks = append(out.Tasks, *t)
	}
	sort.Slice(out.Tasks, func(i, j int) bool {
		return out.Tasks[i].CostYuan > out.Tasks[j].CostYuan || (out.Tasks[i].CostYuan == out.Tasks[j].CostYuan && out.Tasks[i].Capability < out.Tasks[j].Capability)
	})

	// 每活跃用户每天：近 7 天成本 / 近 7 天日活之和。
	today := dayStart(now)
	active, err := s.q.SumStat(ctx, dbq.SumStatParams{Metric: mActiveUsers, Bucket: today.AddDate(0, 0, -6), Bucket_2: today})
	if err != nil {
		return AIOverview{}, err
	}
	if a, _ := strconv.ParseFloat(active, 64); a > 0 {
		out.CostPerActiveUserDay = weekCost / a
	}
	ms := monthStart(now)
	mc, err := s.q.AICostSince(ctx, ms)
	if err != nil {
		return AIOverview{}, err
	}
	out.MonthCostYuan = float64(mc) / microYuan
	rev, err := s.q.SumStat(ctx, dbq.SumStatParams{Metric: mRevenue, Bucket: ms, Bucket_2: today})
	if err != nil {
		return AIOverview{}, err
	}
	if r, _ := strconv.ParseFloat(rev, 64); r > 0 {
		out.MonthRevenueYuan = r / 100
		out.RevenueRatio = out.MonthCostYuan / out.MonthRevenueYuan
	}
	return out, nil
}

// Rollout 是灰度设置：稳定版与候选版的模型、提示词版本，候选版按用户比例放量。
type Rollout struct {
	StableModel     string
	StablePrompt    string
	CandidateModel  string
	CandidatePrompt string
	CandidatePct    int
}

// SetRollout 设置某能力的灰度（7.8）：提示词版本必须在 prompts 里存在；比例 0–100。
// 「扩大比例」改 CandidatePct；「全量」= 候选版写成稳定版、清空候选；「回滚」= 清空候选。
func (s *Service) SetRollout(ctx context.Context, adminID uint64, capability string, r Rollout) error {
	catalog := ai.PromptCatalog()
	versions, ok := catalog[capability]
	if !ok {
		return apperr.NotFoundErr()
	}
	if r.StableModel == "" || !slices.Contains(versions, r.StablePrompt) {
		return apperr.New(apperr.BadRequest, "稳定版的模型或提示词版本不对")
	}
	if r.CandidatePct < 0 || r.CandidatePct > 100 {
		return apperr.New(apperr.BadRequest, "放量比例需在 0–100 之间")
	}
	hasCand := r.CandidateModel != "" || r.CandidatePrompt != ""
	if hasCand && (r.CandidateModel == "" || !slices.Contains(versions, r.CandidatePrompt)) {
		return apperr.New(apperr.BadRequest, "候选版的模型或提示词版本不对")
	}
	if !hasCand {
		r.CandidatePct = 0
	}
	return s.q.UpsertAIRollout(ctx, dbq.UpsertAIRolloutParams{Capability: capability, StableModel: r.StableModel, StablePrompt: r.StablePrompt,
		CandidateModel: sql.NullString{String: r.CandidateModel, Valid: hasCand}, CandidatePrompt: sql.NullString{String: r.CandidatePrompt, Valid: hasCand},
		CandidatePercent: uint8(r.CandidatePct), UpdatedBy: sql.NullInt64{Int64: int64(adminID), Valid: true}})
}

// ---------- 7.9 公告 ----------

// Announcement 是一条手动公告。
type Announcement struct {
	ID          uint64
	Title       string
	Body        string
	All         bool
	UserIDs     []uint64
	WithPopup   bool
	ScheduledAt *time.Time
	SentAt      *time.Time
	SentCount   int
	CreatedAt   time.Time
}

func (s *Service) Announcements(ctx context.Context) ([]Announcement, error) {
	rows, err := s.q.ListAnnouncements(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Announcement, len(rows))
	for i, r := range rows {
		var au struct {
			All     bool     `json:"all"`
			UserIDs []uint64 `json:"user_ids"`
		}
		_ = json.Unmarshal(r.Audience, &au)
		out[i] = Announcement{ID: r.ID, Title: r.Title, Body: r.Body, All: au.All, UserIDs: au.UserIDs, WithPopup: r.WithPopup, ScheduledAt: nullTimePtr(r.ScheduledAt),
			SentAt: nullTimePtr(r.SentAt), SentCount: int(r.SentCount), CreatedAt: r.CreatedAt}
		if out[i].UserIDs == nil {
			out[i].UserIDs = []uint64{}
		}
	}
	return out, nil
}

// CreateAnnouncement 新建公告：发送对象为全部用户或指定用户；不填定时则 5 分钟内由定时任务发出（渠道：消息中心）。
func (s *Service) CreateAnnouncement(ctx context.Context, adminID uint64, a Announcement) (uint64, error) {
	switch {
	case len([]rune(strings.TrimSpace(a.Title))) < 2 || len([]rune(strings.TrimSpace(a.Body))) < 2:
		return 0, apperr.New(apperr.BadRequest, "标题和内容不能为空")
	case !a.All && len(a.UserIDs) == 0:
		return 0, apperr.New(apperr.BadRequest, "请选择发送对象")
	case a.ScheduledAt != nil && a.ScheduledAt.Before(s.now().Add(-time.Minute)):
		return 0, apperr.New(apperr.BadRequest, "定时发送的时间已经过去了")
	}
	au := map[string]any{"all": a.All}
	if !a.All {
		au["user_ids"] = a.UserIDs
	}
	b, _ := json.Marshal(au)
	sched := sql.NullTime{}
	if a.ScheduledAt != nil {
		sched = sql.NullTime{Time: a.ScheduledAt.UTC(), Valid: true}
	}
	id, err := s.q.InsertAnnouncement(ctx, dbq.InsertAnnouncementParams{Title: a.Title, Body: a.Body, Audience: b, WithPopup: a.WithPopup, ScheduledAt: sched,
		CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: true}})
	return uint64(id), err
}

// CancelAnnouncement 取消还没发出的公告。
func (s *Service) CancelAnnouncement(ctx context.Context, id uint64) error {
	n, err := s.q.DeleteUnsentAnnouncement(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.New(apperr.Conflict, "公告已发出或不存在，不能取消")
	}
	return nil
}

// ---------- 7.15 后台账号与权限 ----------

// Account 是后台账号（手机号脱敏）。
type Account struct {
	ID                 uint64
	Username           string
	DisplayName        string
	PhoneMasked        string
	Roles              []Role
	Status             string
	MustChangePassword bool
	LastLoginAt        *time.Time
	CreatedAt          time.Time
}

func (s *Service) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.q.ListAdmins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Account, len(rows))
	for i, r := range rows {
		var roles []Role
		_ = json.Unmarshal(r.Roles, &roles)
		out[i] = Account{ID: r.ID, Username: r.Username, DisplayName: r.DisplayName, PhoneMasked: MaskPhone(r.Phone), Roles: roles, Status: string(r.Status),
			MustChangePassword: r.MustChangePassword, LastLoginAt: nullTimePtr(r.LastLoginAt), CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// UpdateAccount 修改成员的显示名、手机号、角色、状态。不能停用自己或去掉自己的管理员角色（避免把自己锁在门外）；
// 停用或改角色后清掉该账号的会话，立即按新权限重新登录。
func (s *Service) UpdateAccount(ctx context.Context, adminID, id uint64, displayName, phone string, roles []Role, active bool) error {
	if len(roles) == 0 {
		return apperr.New(apperr.BadRequest, "至少选一个角色")
	}
	for _, r := range roles {
		if !validRole(r) {
			return apperr.New(apperr.BadRequest, "未知角色："+string(r))
		}
	}
	if id == adminID && (!active || !slices.Contains(roles, RoleAdmin)) {
		return apperr.New(apperr.BadRequest, "不能停用自己或去掉自己的管理员角色")
	}
	cur, err := s.q.GetAdminByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	if phone == "" || strings.Contains(phone, "*") {
		phone = cur.Phone // 前端拿到的是脱敏号，没改就沿用
	}
	st := dbq.AdminUsersStatusActive
	if !active {
		st = dbq.AdminUsersStatusDisabled
	}
	rb, _ := json.Marshal(roles)
	if _, err := s.q.UpdateAdmin(ctx, dbq.UpdateAdminParams{DisplayName: displayName, Phone: phone, Roles: rb, Status: st, ID: id}); err != nil {
		return err
	}
	return s.q.DeleteAdminSessionsFor(ctx, id)
}

// ResetPassword 重置成员密码（新的初始密码），对方下次登录必须修改；清掉对方的会话。
func (s *Service) ResetPassword(ctx context.Context, id uint64, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	if _, err := s.q.GetAdminByID(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.q.ResetAdminPassword(ctx, dbq.ResetAdminPasswordParams{PasswordHash: string(h), ID: id}); err != nil {
		return err
	}
	return s.q.DeleteAdminSessionsFor(ctx, id)
}
