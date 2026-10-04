package ai

import (
	"context"
	"errors"
	"testing"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/moderation"
)

type fakeChecker struct {
	calls int
	pass  []bool
	err   error
}

func (f *fakeChecker) CheckText(_ context.Context, _ string) (moderation.Verdict, error) {
	f.calls++
	if f.err != nil {
		return moderation.Verdict{}, f.err
	}
	ok := f.pass[min(f.calls-1, len(f.pass)-1)]
	return moderation.Verdict{Pass: ok}, nil
}

func TestOutputModeration(t *testing.T) {
	ctx := context.Background()
	gen := GenerateAnswerIn{Subject: "中国语言文学基础", QType: "term", Stem: "意境", Score: 10}

	// 第一次没过、重试后通过：返回第二次的结果。
	c := &fakeChecker{pass: []bool{false, true}}
	var kinds []string
	e := NewEngine(Config{UseMock: true, Moderation: c, OnCall: func(x Call) { kinds = append(kinds, x.ErrorKind) }})
	if _, _, err := GenerateAnswer.Run(ctx, e, 0, gen); err != nil || c.calls != 2 {
		t.Fatalf("重试后通过：%v，审核 %d 次", err, c.calls)
	}
	if len(kinds) != 2 || kinds[0] != "moderation" || kinds[1] != "" {
		t.Errorf("账本记下没过审的那次：%v", kinds)
	}

	// 两次都没过：AIFailed，不把内容给用户。
	c = &fakeChecker{pass: []bool{false}}
	e = NewEngine(Config{UseMock: true, Moderation: c})
	if _, _, err := GenerateAnswer.Run(ctx, e, 0, gen); !apperr.IsKind(err, apperr.AIFailed) {
		t.Errorf("两次没过应生成失败：%v", err)
	}

	// 审核服务不可用：不放行。
	down := errors.New("moderation down")
	e = NewEngine(Config{UseMock: true, Moderation: &fakeChecker{err: down}})
	if _, _, err := GenerateAnswer.Run(ctx, e, 0, gen); !errors.Is(err, down) {
		t.Errorf("审核不可用时应返回错误：%v", err)
	}

	// 搬运用户原文的能力不重复审。
	c = &fakeChecker{pass: []bool{false}}
	e = NewEngine(Config{UseMock: true, Moderation: c})
	if _, _, err := ExtractRubric.Run(ctx, e, 0, RubricIn{QType: "term", Stem: "意境", Answer: "意境是情景交融的艺术境界。", Score: 10}); err != nil || c.calls != 0 {
		t.Errorf("提采分点不过内容安全：%v %d", err, c.calls)
	}
}
