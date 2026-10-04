// Package ai 是 AI 能力层（dev-spec 第七节，ADR 0008）：业务代码只经这里调用模型。
//
//   - 按能力配置模型、提示词版本、温度；提示词放在 prompts/<能力>@<版本>.tmpl，改提示词就加新版本
//   - 灰度：ai_rollouts 里每个能力有稳定版与候选版，候选版按用户比例放量（同一用户稳定落在同一边）
//   - 每次调用都按 JSON Schema 校验输出，再跑能力自己的校验（如原文必须能在资料里逐字找到）；
//     不合格自动重试一次，再失败返回 AIFailed（「生成失败，未扣除次数」）
//   - 每次调用写一条 ai_calls：能力、模型、版本、token、费用、耗时、成败；不存输入输出原文
//   - AI_PROVIDER=mock 时不发请求，由能力自带的 Mock 函数按规则产出，开发与测试不需要模型
package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/dbq"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

// Def 是一项能力的静态定义。
type Def struct {
	// Name 是能力名（PRD v3 12.1），也是 ai_calls.capability 与 ai_rollouts 的键。
	Name string
	// Version 是默认提示词版本；ai_rollouts 没有配置时用它。
	Version string
	// Tier 是模型档位：strong / cheap（dev-spec 第七节「成本控制」）。
	Tier        string
	Temperature float64
	// Schema 是输出的 JSON Schema（OpenAPI 3 子集）。
	Schema string
}

// Cap 是带类型的能力：输入 In、输出 Out。
type Cap[In, Out any] struct {
	Def
	// Check 是 Schema 之外的校验，返回错误时重试一次。
	Check func(in In, out *Out) error
	// Mock 在 AI_PROVIDER=mock 时代替模型。
	Mock func(in In) (Out, error)

	once   sync.Once
	schema *openapi3.Schema
	err    error
}

// Models 是各档位的默认模型（Q02 未定前的开发默认值，后台 7.8 可按能力覆盖）。
type Models struct {
	Strong, Cheap string
	// PreferStrong 为 true 时便宜档能力也用 Strong（优先高阶模型）。
	PreferStrong bool
}

func (m Models) pick(tier string) string {
	if tier == "cheap" && !m.PreferStrong {
		return m.Cheap
	}
	return m.Strong
}

// Fallback 是主用平台出错时改用的平台与它的模型。
type Fallback struct {
	Client cloudai.Client
	Models Models
}

// Price 是每千 token 的价格，单位百万分之一元。
type Price struct{ InputPer1K, OutputPer1K int64 }

// Engine 执行能力调用。
type Engine struct {
	client   cloudai.Client
	fallback *Fallback
	q        dbq.Querier
	useMock  bool
	models   Models
	prices   map[string]Price
	salt     string
	now      func() time.Time
	override map[string]Meta
	onCall   func(Call)
	moderate TextChecker
}

// TextChecker 是内容安全的文本审核（cloud/moderation.Checker 满足它）。
type TextChecker interface {
	CheckText(ctx context.Context, text string) (moderation.Verdict, error)
}

// moderated 是要过内容安全的能力：生成新文字给用户看的（答案、讲解、变式题、命题、批改评语、考情分析）。
// 结构化、拆知识点、提采分点这类主要搬运用户自己资料原文的能力，资料上传时已经审过，不重复审（dev-spec 第九节）。
var moderated = map[string]bool{
	"answer_generate": true, "kp_explain": true, "question_variant": true, "essay_topic": true,
	"essay_grade": true, "grade_subjective": true, "grade_norm": true, "exam_style": true,
}

// ErrModeration 表示模型输出没过内容安全，按不合格输出处理（重试一次，再不过返回 AIFailed，不扣次数）。
var ErrModeration = errors.New("ai: 输出未通过内容安全")

// Call 是一次模型调用的记录（评测命令用它统计每千次成本与时延）。
type Call struct {
	Capability, Model, Version string
	InputTokens, OutputTokens  int
	CostMicroYuan              int64
	Latency                    time.Duration
	ErrorKind                  string
}

// Config 是 Engine 的配置。
type Config struct {
	Client cloudai.Client
	// Fallback 不为空时，主用平台调用出错（网络、5xx、超时）改用它重试一次。
	Fallback *Fallback
	Queries  dbq.Querier
	// UseMock 为 true 时不调用模型，用各能力的 Mock 函数。
	UseMock bool
	Models  Models
	Prices  map[string]Price
	// Salt 用于计算 ai_calls.user_hash，让账本里的用户无法被直接对应。
	Salt string
	// Override 按能力指定模型与提示词版本（评测命令对比候选版用）；空字段沿用默认。线上走 7.8 的灰度设置，不用它。
	Override map[string]Meta
	// OnCall 在每次调用模型后执行（评测命令统计成本与时延）。
	OnCall func(Call)
	// Moderation 不为空时，生成类能力的输出过内容安全后才返回（dev-spec 第九节：AI 输出过审后才入库展示）。
	Moderation TextChecker
}

func NewEngine(c Config) *Engine {
	if c.Models.Strong == "" {
		c.Models.Strong = "qwen-plus"
	}
	if c.Models.Cheap == "" {
		c.Models.Cheap = "qwen-turbo"
	}
	if c.Fallback != nil && c.Fallback.Client == nil {
		c.Fallback = nil
	}
	return &Engine{client: c.Client, fallback: c.Fallback, q: c.Queries, useMock: c.UseMock, models: c.Models, prices: c.Prices, salt: c.Salt, now: time.Now,
		override: c.Override, onCall: c.OnCall, moderate: c.Moderation}
}

// ErrInvalidOutput 表示模型输出不合格（Schema 或能力校验没过）。
var ErrInvalidOutput = errors.New("ai: 输出不合格")

// Meta 是一次调用实际用到的模型与版本（写进 import_jobs.prompt_versions 等，便于追溯）。
type Meta struct {
	Model, Version string
}

// route 决定这次调用用稳定版还是候选版。
func (e *Engine) route(ctx context.Context, d Def, userID uint64) (Meta, error) {
	m := Meta{Model: e.models.pick(d.Tier), Version: d.Version}
	if o, ok := e.override[d.Name]; ok {
		if o.Model != "" {
			m.Model = o.Model
		}
		if o.Version != "" {
			if !hasPrompt(d.Name, o.Version) {
				return Meta{}, fmt.Errorf("ai: 能力 %s 没有提示词版本 %s", d.Name, o.Version)
			}
			m.Version = o.Version
		}
		return m, nil
	}
	if e.q == nil {
		return m, nil // 评测命令不连数据库
	}
	r, err := e.q.GetAIRollout(ctx, d.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return Meta{}, err
	}
	m = Meta{Model: r.StableModel, Version: r.StablePrompt}
	if r.CandidateModel.Valid && r.CandidatePrompt.Valid && bucket(d.Name, userID) < int(r.CandidatePercent) {
		m = Meta{Model: r.CandidateModel.String, Version: r.CandidatePrompt.String}
	}
	if !hasPrompt(d.Name, m.Version) {
		// 配了不存在的版本时回到代码里的默认版本，不让线上调用失败。
		m.Version = d.Version
	}
	return m, nil
}

// bucket 把用户稳定地分到 0–99 的桶里，同一能力下同一用户总在同一边。
func bucket(capability string, userID uint64) int {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", capability, userID)))
	return int(binary.BigEndian.Uint32(h[:4]) % 100)
}

func promptName(capability, version string) string {
	return "prompts/" + capability + "@" + version + ".tmpl"
}

func hasPrompt(capability, version string) bool {
	_, err := promptFS.Open(promptName(capability, version))
	return err == nil
}

// userSeparator 分开提示词文件里的系统提示与用户提示。
const userSeparator = "\n---USER---\n"

var funcs = template.FuncMap{
	"json": func(v any) (string, error) {
		b, err := json.MarshalIndent(v, "", "  ")
		return string(b), err
	},
	// inc 把从 0 开始的下标变成从 1 开始的序号（作文按段编号）。
	"inc": func(i int) int { return i + 1 },
}

func render(capability, version string, in any) (system, user string, err error) {
	raw, err := promptFS.ReadFile(promptName(capability, version))
	if err != nil {
		return "", "", fmt.Errorf("ai: 没有提示词 %s@%s", capability, version)
	}
	sys, usr, ok := strings.Cut(string(raw), userSeparator)
	if !ok {
		return "", "", fmt.Errorf("ai: 提示词 %s@%s 缺少 ---USER--- 分隔", capability, version)
	}
	t, err := template.New(capability).Funcs(funcs).Option("missingkey=error").Parse(usr)
	if err != nil {
		return "", "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, in); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(sys), b.String(), nil
}

func (c *Cap[In, Out]) compiled() (*openapi3.Schema, error) {
	c.once.Do(func() {
		var s openapi3.Schema
		if c.err = json.Unmarshal([]byte(c.Schema), &s); c.err == nil {
			c.schema = &s
		}
	})
	return c.schema, c.err
}

// validate 先按 Schema 校验，再解码并跑能力校验。
func (c *Cap[In, Out]) validate(in In, content string) (Out, error) {
	var zero Out
	schema, err := c.compiled()
	if err != nil {
		return zero, err
	}
	var generic any
	if err := json.Unmarshal([]byte(stripFence(content)), &generic); err != nil {
		return zero, fmt.Errorf("%w：不是 JSON", ErrInvalidOutput)
	}
	if err := schema.VisitJSON(generic, openapi3.MultiErrors()); err != nil {
		return zero, fmt.Errorf("%w：%s", ErrInvalidOutput, firstLine(err.Error()))
	}
	var out Out
	if err := json.Unmarshal([]byte(stripFence(content)), &out); err != nil {
		return zero, fmt.Errorf("%w：%s", ErrInvalidOutput, err.Error())
	}
	if c.Check != nil {
		if err := c.Check(in, &out); err != nil {
			return zero, fmt.Errorf("%w：%s", ErrInvalidOutput, err.Error())
		}
	}
	return out, nil
}

// stripFence 去掉模型偶尔包在外面的 ```json 代码块。
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len([]rune(line)) > 120 {
		line = string([]rune(line)[:120])
	}
	return line
}

// Run 执行一次能力调用：不合格重试一次，再失败返回 AIFailed。
func (c *Cap[In, Out]) Run(ctx context.Context, e *Engine, userID uint64, in In) (Out, Meta, error) {
	var zero Out
	meta, err := e.route(ctx, c.Def, userID)
	if err != nil {
		return zero, Meta{}, err
	}
	var lastErr error
	primary := meta.Model
	for attempt := 0; attempt < 2; attempt++ {
		start := e.now()
		var content string
		var resp cloudai.Response
		if e.useMock {
			meta.Model = "mock"
			if c.Mock == nil {
				return zero, meta, fmt.Errorf("ai: 能力 %s 没有 mock", c.Name)
			}
			o, err := c.Mock(in)
			if err != nil {
				return zero, meta, err
			}
			b, err := json.Marshal(o)
			if err != nil {
				return zero, meta, err
			}
			content = string(b)
		} else {
			system, user, err := render(c.Name, meta.Version, in)
			if err != nil {
				return zero, meta, err
			}
			meta.Model = primary
			req := cloudai.Request{
				Capability: c.Name, Model: meta.Model, JSONMode: true, Temperature: c.Temperature,
				Messages: []cloudai.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
			}
			resp, err = e.client.Complete(ctx, req)
			if err != nil && e.fallback != nil && ctx.Err() == nil {
				// 主用平台不可用：记一笔失败，改用备用平台（优先高阶模型）。
				e.record(ctx, c.Def, meta, userID, resp, start, "provider_error", attempt > 0)
				meta.Model = e.fallback.Models.pick(c.Tier)
				req.Model = meta.Model
				start = e.now()
				resp, err = e.fallback.Client.Complete(ctx, req)
			}
			if err != nil {
				e.record(ctx, c.Def, meta, userID, resp, start, "provider_error", attempt > 0)
				// 平台错误交给调用方（Asynq 任务）稍后重试，不在这里连打。
				return zero, meta, err
			}
			content = resp.Content
		}
		out, err := c.validate(in, content)
		if err == nil && e.moderate != nil && moderated[c.Name] {
			v, merr := e.moderate.CheckText(ctx, content)
			if merr != nil {
				// 审核服务不可用时不放行，交给调用方稍后重试。
				e.record(ctx, c.Def, meta, userID, resp, start, "moderation_unavailable", attempt > 0)
				return zero, meta, merr
			}
			if !v.Pass {
				err = ErrModeration
			}
		}
		if err == nil {
			e.record(ctx, c.Def, meta, userID, resp, start, "", attempt > 0)
			return out, meta, nil
		}
		e.record(ctx, c.Def, meta, userID, resp, start, errorKind(err), attempt > 0)
		lastErr = err
	}
	return zero, meta, apperr.New(apperr.AIFailed, "生成失败，未扣除次数，请重试").Wrap(lastErr)
}

func errorKind(err error) string {
	if errors.Is(err, ErrModeration) {
		return "moderation"
	}
	if errors.Is(err, ErrInvalidOutput) {
		return "invalid_output"
	}
	return "error"
}

// record 写 ai_calls。账本写失败不影响业务调用。
func (e *Engine) record(ctx context.Context, d Def, m Meta, userID uint64, resp cloudai.Response, start time.Time, errKind string, retried bool) {
	p := e.prices[m.Model]
	cost := (int64(resp.InputTokens)*p.InputPer1K + int64(resp.OutputTokens)*p.OutputPer1K) / 1000
	if e.onCall != nil {
		e.onCall(Call{Capability: d.Name, Model: m.Model, Version: m.Version, InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens,
			CostMicroYuan: cost, Latency: e.now().Sub(start), ErrorKind: errKind})
	}
	if e.q == nil {
		return
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", e.salt, userID)))
	_ = e.q.InsertAICall(context.WithoutCancel(ctx), dbq.InsertAICallParams{
		Capability: d.Name, Model: m.Model, PromptVersion: m.Version,
		UserHash:    sql.NullString{String: hex.EncodeToString(h[:]), Valid: userID != 0},
		InputTokens: uint32(max(resp.InputTokens, 0)), OutputTokens: uint32(max(resp.OutputTokens, 0)),
		CostMicroYuan: uint64(max(cost, 0)), LatencyMs: uint32(max(e.now().Sub(start).Milliseconds(), 0)),
		Success: errKind == "", ErrorKind: sql.NullString{String: errKind, Valid: errKind != ""}, Retried: retried,
	})
}

// PromptCatalog 列出每个能力已有的提示词版本（prompts/<能力>@<版本>.tmpl），7.8 灰度只能选这里有的版本。
func PromptCatalog() map[string][]string {
	out := map[string][]string{}
	entries, _ := promptFS.ReadDir("prompts")
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		capName, ver, ok := strings.Cut(name, "@")
		if ok {
			out[capName] = append(out[capName], ver)
		}
	}
	return out
}
