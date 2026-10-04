package ai

import (
	"context"
	"testing"
)

func TestOverrideAndOnCall(t *testing.T) {
	var calls []Call
	e := NewEngine(Config{UseMock: true, OnCall: func(c Call) { calls = append(calls, c) },
		Override: map[string]Meta{ExtractRubric.Name: {Version: "v1"}, Grade.Name: {Version: "v404"}}})
	in := RubricIn{QType: "term", Stem: "意境", Answer: "意境是情景交融、虚实相生的艺术境界。", Score: 10}
	if _, m, err := ExtractRubric.Run(context.Background(), e, 0, in); err != nil || m.Version != "v1" {
		t.Fatalf("指定已有版本：%+v %v", m, err)
	}
	if len(calls) != 1 || calls[0].Capability != ExtractRubric.Name || calls[0].Version != "v1" {
		t.Errorf("每次调用回调一次：%+v", calls)
	}
	if _, _, err := Grade.Run(context.Background(), e, 0, GradeIn{}); err == nil {
		t.Error("指定不存在的提示词版本应报错，不能悄悄用默认版本跑评测")
	}
}
