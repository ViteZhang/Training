package ai

import (
	"context"
	"errors"
	"testing"

	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/config"
)

// scripted 按顺序返回预设结果，并记下每次请求的模型。
type scripted struct {
	results []func() (cloudai.Response, error)
	models  []string
}

func (s *scripted) Complete(_ context.Context, req cloudai.Request) (cloudai.Response, error) {
	s.models = append(s.models, req.Model)
	if len(s.results) == 0 {
		return cloudai.Response{}, errors.New("没有预设结果")
	}
	r := s.results[0]
	s.results = s.results[1:]
	return r()
}

func ok(content string) func() (cloudai.Response, error) {
	return func() (cloudai.Response, error) { return cloudai.Response{Content: content}, nil }
}

func fail() func() (cloudai.Response, error) {
	return func() (cloudai.Response, error) { return cloudai.Response{}, errors.New("502 upstream unavailable") }
}

const explainJSON = `{"explanation":"意境是情景交融、虚实相生的艺术境界，答题时写全三个特征。"}`

// 中转站的单个模型被下架或暂时不可用时，先在同一平台换模型，不要直接跳到备用平台。
func TestModelFallbackWithinRelay(t *testing.T) {
	cfg := config.AIConfig{Provider: "relay", PreferStrong: true,
		ModelStrong: "deepseek-flash", ModelCheap: "deepseek-flash",
		RelayModelStrong: "deepseek-v4-flash-0731", RelayModelCheap: "deepseek-v4-flash-0731",
		RelayModelFallbacks: []string{"deepseek-v4.1-flash", "gemini-3.8-flash"}}
	relay, official := &scripted{}, &scripted{}
	models, fb := Routing(cfg, official)
	e := NewEngine(Config{Client: relay, Fallback: fb, Models: models, ModelFallbacks: ModelFallbacks(cfg)})

	// 主模型 503 → 第一个备用模型成功，不碰备用平台。
	relay.results = []func() (cloudai.Response, error){fail(), ok(explainJSON)}
	_, meta, err := Explain.Run(context.Background(), e, 0, ExplainIn{Name: "意境"})
	if err != nil || meta.Model != "deepseek-v4.1-flash" {
		t.Fatalf("同平台换模型：%+v %v", meta, err)
	}
	if len(relay.models) != 2 || relay.models[1] != "deepseek-v4.1-flash" || len(official.models) != 0 {
		t.Errorf("请求的模型：中转 %v 备用平台 %v", relay.models, official.models)
	}

	// 换模型后输出不合格重试时仍用该模型，不要退回已失败的主模型。
	relay.models, official.models = nil, nil
	relay.results = []func() (cloudai.Response, error){fail(), ok(`{"bad":1}`), ok(explainJSON)}
	if _, meta, err := Explain.Run(context.Background(), e, 0, ExplainIn{Name: "意境"}); err != nil || meta.Model != "deepseek-v4.1-flash" {
		t.Fatalf("换模型后重试：%+v %v", meta, err)
	}
	if len(relay.models) != 3 || relay.models[2] != "deepseek-v4.1-flash" || len(official.models) != 0 {
		t.Errorf("重试时的模型：中转 %v 备用平台 %v", relay.models, official.models)
	}

	// 所有中转模型都失败 → 才切备用平台。
	relay.models, official.models = nil, nil
	relay.results = []func() (cloudai.Response, error){fail(), fail(), fail()}
	official.results = []func() (cloudai.Response, error){ok(explainJSON)}
	if _, meta, err := Explain.Run(context.Background(), e, 0, ExplainIn{Name: "意境"}); err != nil || meta.Model != "deepseek-flash" {
		t.Fatalf("切备用平台：%+v %v", meta, err)
	}
	if len(relay.models) != 3 || len(official.models) != 1 {
		t.Errorf("全部失败后的顺序：中转 %v 备用平台 %v", relay.models, official.models)
	}
}

func TestRoutingPreferStrongAndFallback(t *testing.T) {
	cfg := config.AIConfig{Provider: "relay", ModelStrong: "qwen-max", ModelCheap: "qwen-plus", PreferStrong: true,
		RelayModelStrong: "gpt-5.5", RelayModelCheap: "gpt-5.4-mini"}
	relay, bailian := &scripted{}, &scripted{}
	models, fb := Routing(cfg, bailian)
	if models.pick("cheap") != "gpt-5.5" || fb == nil || fb.Models.pick("cheap") != "qwen-max" {
		t.Fatalf("优先高阶模型：%+v %+v", models, fb)
	}
	e := NewEngine(Config{Client: relay, Fallback: fb, Models: models})

	// 中转出错 → 改用百炼高阶模型。
	relay.results = []func() (cloudai.Response, error){fail()}
	bailian.results = []func() (cloudai.Response, error){ok(explainJSON)}
	_, meta, err := Explain.Run(context.Background(), e, 0, ExplainIn{Name: "意境"})
	if err != nil || meta.Model != "qwen-max" {
		t.Fatalf("备用：%+v %v", meta, err)
	}
	if relay.models[0] != "gpt-5.5" || bailian.models[0] != "qwen-max" {
		t.Errorf("请求的模型：中转 %v 百炼 %v", relay.models, bailian.models)
	}

	// 中转输出不合格重试时仍先找中转，模型名不会串成百炼的。
	relay.models, bailian.models = nil, nil
	relay.results = []func() (cloudai.Response, error){ok(`{"bad":1}`), ok(explainJSON)}
	if _, meta, err := Explain.Run(context.Background(), e, 0, ExplainIn{Name: "意境"}); err != nil || meta.Model != "gpt-5.5" {
		t.Fatalf("重试：%+v %v", meta, err)
	}
	if len(relay.models) != 2 || relay.models[1] != "gpt-5.5" || len(bailian.models) != 0 {
		t.Errorf("重试时的模型：中转 %v 百炼 %v", relay.models, bailian.models)
	}

	// 两边都失败：返回平台错误，交给调用方稍后重试。
	relay.results = []func() (cloudai.Response, error){fail()}
	bailian.results = []func() (cloudai.Response, error){fail()}
	if _, _, err := Explain.Run(context.Background(), e, 0, ExplainIn{Name: "意境"}); err == nil {
		t.Error("两边都失败应返回错误")
	}

	// 只配百炼、关掉优先高阶：便宜档用便宜模型，没有备用。
	cfg.Provider, cfg.PreferStrong = "bailian", false
	m, fb2 := Routing(cfg, bailian)
	if m.pick("cheap") != "qwen-plus" || m.pick("strong") != "qwen-max" || fb2 != nil {
		t.Errorf("百炼：%+v %+v", m, fb2)
	}
}
