// Package practice 是练习会话、客观题判分、错题本与学习状态更新（PRD 模块 4、11.1–11.3、11.8；T17）。
package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/asr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/plan"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// Kind 是练习类型。
type Kind string

const (
	KindDaily     Kind = "daily"
	KindTypeDrill Kind = "type_drill"
	KindCustom    Kind = "custom"
	KindWrongRedo Kind = "wrong_redo"
	KindPlacement Kind = "placement"
)

// 题量默认值。
const (
	defaultCount   = 10
	maxCount       = 50
	placementCount = 20 // 1.8 摸底测抽 20 题
	maxAIFill      = 5  // 一组最多让 AI 补 5 道
	reportOffline  = 3  // AI 题被报错 3 次自动下线
	weeklyDrills   = 3  // 强化期每周 3 次题型专项
)

type Service struct {
	db         *sql.DB
	q          *dbq.Queries
	params     *params.Store
	plan       *plan.Service
	ai         *ai.Engine
	quota      *quota.Service
	oss        oss.Store
	ocr        ocr.Recognizer
	moderation moderation.Checker
	asr        asr.Transcriber
	flags      FlagChecker
	queue      Enqueuer
	now        func() time.Time
}

// FlagChecker 判断功能开关（口述背诵）。
type FlagChecker interface {
	Enabled(ctx context.Context, key string, userID uint64) (bool, error)
}

type Deps struct {
	DB     *sql.DB
	Params *params.Store
	Plan   *plan.Service
	AI     *ai.Engine
	Quota  *quota.Service
	// 拍手写稿（T19）：照片直传、内容安全检查、手写识别。
	OSS        oss.Store
	OCR        ocr.Recognizer
	Moderation moderation.Checker
	// 口述背诵（T20，功能开关 oral_recite）：语音识别。
	ASR   asr.Transcriber
	Flags FlagChecker
	// Queue 排整卷批改与模拟考试自动交卷任务（T21）；为空时交卷后同步批改。
	Queue Enqueuer
	Now   func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: d.DB, q: dbq.New(d.DB), params: d.Params, plan: d.Plan, ai: d.AI, quota: d.Quota, oss: d.OSS, ocr: d.OCR, moderation: d.Moderation, asr: d.ASR, flags: d.Flags, queue: d.Queue, now: now}
}

func (s *Service) today() rules.Day { return rules.DayOf(s.now()) }

func owner(userID uint64) sql.NullInt64 { return sql.NullInt64{Int64: int64(userID), Valid: true} }

func (s *Service) subject(ctx context.Context, userID, subjectID uint64) (dbq.GetSubjectBankRow, error) {
	b, err := s.q.GetSubjectBank(ctx, dbq.GetSubjectBankParams{ID: subjectID, OwnerUserID: userID, OwnerUserID_2: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return b, apperr.NotFoundErr()
	}
	return b, err
}

// Config 是自定义练习条件（4.2）。
type Config struct {
	SectionIDs     []uint64 `json:"section_ids,omitempty"`
	QTypes         []string `json:"qtypes,omitempty"`
	Count          int      `json:"count,omitempty"`
	OnlyUnmastered bool     `json:"only_unmastered,omitempty"`
	AIFill         bool     `json:"ai_fill,omitempty"`
}

// WrongGroup 是错题重做只做某一组。
type WrongGroup struct {
	By  string `json:"by"` // kp / qtype / loss
	Key string `json:"key"`
}

// CreateInput 是开始一组练习的请求。
type CreateInput struct {
	SubjectID  uint64
	Kind       Kind
	QType      string
	Config     Config
	WrongGroup *WrongGroup
	// AIFill 只用于今日训练：题量不够时 AI 补变式题。
	AIFill bool
}

// sessionMeta 存在 practice_sessions.config：自定义条件、今日训练的分组、AI 补题与缺题数。
type sessionMeta struct {
	Config    *Config           `json:"config,omitempty"`
	Groups    map[string]string `json:"groups,omitempty"` // 题目 ID → 计划分组
	AIFilled  int               `json:"ai_filled,omitempty"`
	Shortfall int               `json:"shortfall,omitempty"`
}

// poolItem 是选题池里的一道题与它的学习状态。
type poolItem struct {
	dbq.ListPracticePoolRow
	m        float64
	state    rules.MasteryState
	sections map[uint64]bool // 主知识点的全部祖先
}

// pool 读出题库里在用的题与主知识点的掌握情况。
func (s *Service) pool(ctx context.Context, userID uint64, b dbq.GetSubjectBankRow) ([]poolItem, map[uint64]dbq.ListBankKPsFullRow, error) {
	rows, err := s.q.ListPracticePool(ctx, dbq.ListPracticePoolParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return nil, nil, err
	}
	kps, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: b.BankID, Owner: owner(userID)})
	if err != nil {
		return nil, nil, err
	}
	byID := map[uint64]dbq.ListBankKPsFullRow{}
	for _, k := range kps {
		byID[k.ID] = k
	}
	out := make([]poolItem, 0, len(rows))
	for _, r := range rows {
		it := poolItem{ListPracticePoolRow: r, state: rules.StateUnlearned, sections: map[uint64]bool{}}
		if k, ok := byID[uint64(r.PrimaryKp)]; ok {
			it.m, _ = strconv.ParseFloat(k.M, 64)
			it.state = rules.MasteryState(k.State)
			for cur, depth := k, 0; depth < 8; depth++ {
				it.sections[cur.ID] = true
				if !cur.ParentID.Valid {
					break
				}
				p, ok := byID[uint64(cur.ParentID.Int64)]
				if !ok {
					break
				}
				cur = p
			}
		}
		out = append(out, it)
	}
	return out, byID, nil
}

// filter 按自定义条件筛题（4.2）。
func filter(items []poolItem, c Config) []poolItem {
	qt := map[string]bool{}
	for _, t := range c.QTypes {
		qt[t] = true
	}
	var out []poolItem
	for _, it := range items {
		if len(qt) > 0 && !qt[string(it.Qtype)] {
			continue
		}
		if len(c.SectionIDs) > 0 {
			hit := false
			for _, id := range c.SectionIDs {
				if it.sections[id] {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		if c.OnlyUnmastered && (it.PrimaryKp == 0 || it.state == rules.StateMastered) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// rank 让没做过、掌握分低的题排在前面；同档内随机，避免每次都是同一组。
func rank(items []poolItem, r *rand.Rand) {
	r.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	sort.SliceStable(items, func(i, j int) bool {
		if (items[i].AttemptCount == 0) != (items[j].AttemptCount == 0) {
			return items[i].AttemptCount == 0
		}
		return items[i].m < items[j].m
	})
}

func clampCount(n, def int) int {
	if n <= 0 {
		return def
	}
	return min(n, maxCount)
}

// Preview 是 4.2 实时显示的题数与预计用时。
type Preview struct {
	Available, Count, AIFill int
	Minutes                  float64
}

func (s *Service) Preview(ctx context.Context, userID, subjectID uint64, c Config) (Preview, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Preview{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Preview{}, err
	}
	items, _, err := s.pool(ctx, userID, b)
	if err != nil {
		return Preview{}, err
	}
	hit := filter(items, c)
	want := clampCount(c.Count, defaultCount)
	out := Preview{Available: len(hit), Count: min(len(hit), want)}
	if c.AIFill && out.Count < want {
		out.AIFill = min(want-out.Count, maxAIFill)
	}
	for _, it := range hit[:out.Count] {
		out.Minutes += rules.ItemMinutes(rules.QType(it.Qtype), false, p.Plan)
	}
	if out.AIFill > 0 {
		qt := "term"
		if len(c.QTypes) > 0 {
			qt = c.QTypes[0]
		}
		out.Minutes += float64(out.AIFill) * rules.ItemMinutes(rules.QType(qt), false, p.Plan)
	}
	return out, nil
}

var kindTitle = map[Kind]string{KindDaily: "今日训练", KindTypeDrill: "题型专项", KindCustom: "自定义练习", KindWrongRedo: "错题重做", KindPlacement: "摸底测"}

// Create 开始一组练习。今日训练当天只有一个进行中的会话，重复开始返回它（从断点继续）。
func (s *Service) Create(ctx context.Context, userID uint64, in CreateInput) (Session, error) {
	b, err := s.subject(ctx, userID, in.SubjectID)
	if err != nil {
		return Session{}, err
	}
	if in.Kind == KindDaily {
		cur, err := s.q.LatestInProgressSession(ctx, dbq.LatestInProgressSessionParams{OwnerUserID: userID, SubjectID: sql.NullInt64{Int64: int64(b.SubjectID), Valid: true}})
		if err == nil && Kind(cur.Kind) == KindDaily && rules.DayOf(cur.StartedAt) == s.today() {
			return s.Get(ctx, userID, cur.ID)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Session{}, err
		}
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Session{}, err
	}
	items, kps, err := s.pool(ctx, userID, b)
	if err != nil {
		return Session{}, err
	}
	r := rand.New(rand.NewPCG(uint64(s.now().UnixNano()), userID)) //nolint:gosec // 只用来打乱出题顺序，不涉及安全
	meta := sessionMeta{}
	title := kindTitle[in.Kind]
	var ids []uint64
	switch in.Kind {
	case KindDaily:
		pl, err := s.plan.Today(ctx, userID)
		if err != nil {
			return Session{}, err
		}
		meta.Groups = map[string]string{}
		for _, it := range pl.Items {
			if it.SubjectID == b.SubjectID && it.QuestionID != 0 {
				ids = append(ids, it.QuestionID)
				meta.Groups[strconv.FormatUint(it.QuestionID, 10)] = it.Group
			}
		}
		// 题量不够排满每日时长时，打开 AI 补题才按缺的分钟数补变式题（4.2 / 今日训练的 AI 补题开关，关闭时不生成）。
		if in.AIFill && pl.Shortfall > 0 {
			n := int(math.Ceil(pl.Shortfall / rules.ItemMinutes(rules.QTermExplain, false, p.Plan)))
			gen, err := s.fill(ctx, userID, b, Config{OnlyUnmastered: true}, min(n, maxAIFill), kps, r, true)
			if err != nil {
				return Session{}, err
			}
			for _, id := range gen {
				ids = append(ids, id)
				meta.Groups[strconv.FormatUint(id, 10)] = string(rules.GroupWeak)
			}
			meta.AIFilled = len(gen)
		}
	case KindTypeDrill:
		qt := in.QType
		if qt == "" {
			return Session{}, apperr.New(apperr.BadRequest, "请选择题型")
		}
		title = "题型专项 · " + qtypeName(qt)
		hit := filter(items, Config{QTypes: []string{qt}})
		rank(hit, r)
		for _, it := range hit[:min(len(hit), clampCount(in.Config.Count, defaultCount))] {
			ids = append(ids, it.ID)
		}
	case KindCustom:
		c := in.Config
		c.Count = clampCount(c.Count, defaultCount)
		meta.Config = &c
		hit := filter(items, c)
		rank(hit, r)
		for _, it := range hit[:min(len(hit), c.Count)] {
			ids = append(ids, it.ID)
		}
		if short := c.Count - len(ids); short > 0 {
			if c.AIFill {
				gen, err := s.fill(ctx, userID, b, c, min(short, maxAIFill), kps, r, true)
				if err != nil {
					return Session{}, err
				}
				ids = append(ids, gen...)
				meta.AIFilled = len(gen)
			}
			meta.Shortfall = c.Count - len(ids)
		}
	case KindWrongRedo:
		ws, err := s.q.ListWrongBook(ctx, dbq.ListWrongBookParams{OwnerUserID: userID, BankID: b.BankID})
		if err != nil {
			return Session{}, err
		}
		for _, w := range ws {
			if w.Status != dbq.WrongBookStatusActive || !inGroup(w, in.WrongGroup) {
				continue
			}
			ids = append(ids, w.QuestionID)
			if len(ids) >= maxCount {
				break
			}
		}
	case KindPlacement:
		ids = placement(items, r)
	default:
		return Session{}, apperr.New(apperr.BadRequest, "练习类型不正确")
	}
	if len(ids) == 0 {
		return Session{}, apperr.New(apperr.BadRequest, "没有符合条件的题目")
	}
	idsJSON, _ := json.Marshal(ids)
	metaJSON, _ := json.Marshal(meta)
	id, err := s.q.InsertPracticeSession(ctx, dbq.InsertPracticeSessionParams{OwnerUserID: userID, SubjectID: sql.NullInt64{Int64: int64(b.SubjectID), Valid: true},
		Kind: dbq.PracticeSessionsKind(in.Kind), Title: title, Config: metaJSON, QuestionIds: idsJSON, StartedAt: s.now().UTC()})
	if err != nil {
		return Session{}, err
	}
	if in.Kind == KindDaily {
		if err := s.q.SetDailyPlanSession(ctx, dbq.SetDailyPlanSessionParams{PracticeSessionID: sql.NullInt64{Int64: id, Valid: true}, OwnerUserID: userID, PlanDate: s.today().Date()}); err != nil {
			return Session{}, err
		}
	}
	return s.Get(ctx, userID, uint64(id))
}

func inGroup(w dbq.ListWrongBookRow, g *WrongGroup) bool {
	if g == nil {
		return true
	}
	switch g.By {
	case "kp":
		return strconv.FormatInt(w.KpID, 10) == g.Key
	case "qtype":
		return string(w.Qtype) == g.Key
	case "loss":
		return w.LastLossType.Valid && string(w.LastLossType.WrongBookLastLossType) == g.Key
	}
	return true
}

// placement 抽摸底测（1.8）：按题型轮流取，尽量覆盖不同知识点，最多 20 题。
func placement(items []poolItem, r *rand.Rand) []uint64 {
	r.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	byType := map[string][]poolItem{}
	var types []string
	for _, it := range items {
		t := string(it.Qtype)
		if _, ok := byType[t]; !ok {
			types = append(types, t)
		}
		byType[t] = append(byType[t], it)
	}
	sort.Strings(types)
	usedKP := map[int64]bool{}
	var out []uint64
	// 第一轮每个知识点只取一题，第二轮补满。
	for round := 0; round < 2 && len(out) < placementCount; round++ {
		progress := true
		for progress && len(out) < placementCount {
			progress = false
			for _, t := range types {
				for i, it := range byType[t] {
					if round == 0 && it.PrimaryKp != 0 && usedKP[it.PrimaryKp] {
						continue
					}
					out = append(out, it.ID)
					usedKP[it.PrimaryKp] = true
					byType[t] = append(byType[t][:i:i], byType[t][i+1:]...)
					progress = true
					break
				}
				if len(out) >= placementCount {
					break
				}
			}
		}
	}
	return out
}

var qtypeNames = map[string]string{"single_choice": "单选", "multi_choice": "多选", "true_false": "判断", "fill_blank": "填空", "term": "名词解释",
	"short_answer": "简答", "discussion": "论述", "essay": "作文", "calculation": "计算", "other": "其他"}

func qtypeName(t string) string {
	if n, ok := qtypeNames[t]; ok {
		return n
	}
	return t
}

// withTx 是事务的简写。
func (s *Service) withTx(ctx context.Context, fn func(q *dbq.Queries) error) error {
	return store.WithTx(ctx, s.db, fn)
}

var cst = time.FixedZone("Asia/Shanghai", 8*3600)
