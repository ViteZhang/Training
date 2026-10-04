package logx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestMaskPhones(t *testing.T) {
	cases := map[string]string{
		"13812345678":                 "138****5678",
		"手机号13812345678登录":            "手机号138****5678登录",
		"a 13812345678,15900001111 b": "a 138****5678,159****1111 b",
		"订单号 213812345678":            "订单号 213812345678", // 前面紧挨数字，不是手机号
		"12345678901":                 "12345678901",      // 不是 13–19 开头
		"":                            "",
	}
	for in, want := range cases {
		if got := MaskPhones(in); got != want {
			t.Errorf("MaskPhones(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestLoggerRedactsPhones(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo)
	l.Info("发送验证码 13812345678", "phone", "13912345678", "err", errors.New("号码 15012345678 被限频"))
	out := buf.String()
	for _, raw := range []string{"13812345678", "13912345678", "15012345678"} {
		if strings.Contains(out, raw) {
			t.Errorf("日志里出现了手机号明文 %s：%s", raw, out)
		}
	}
	if !strings.Contains(out, "139****5678") {
		t.Errorf("应有脱敏后的号码：%s", out)
	}
}

func TestContextLogger(t *testing.T) {
	if From(context.Background()) != slog.Default() {
		t.Error("没有日志器时应返回默认日志器")
	}
	l := New(&bytes.Buffer{}, slog.LevelInfo)
	if From(WithLogger(context.Background(), l)) != l {
		t.Error("应取回放进去的日志器")
	}
}
