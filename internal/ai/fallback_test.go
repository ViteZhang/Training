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
