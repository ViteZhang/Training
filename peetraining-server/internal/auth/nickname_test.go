package auth

import (
	"testing"
	"unicode/utf8"
)

func TestRandomNicknameFits(t *testing.T) {
	// 每种组合都不超过昵称上限 16 个字。
	for _, h := range nicknameHeads {
		for _, tail := range nicknameTails {
			if n := utf8.RuneCountInString(h + tail); n > 16 {
				t.Fatalf("%s%s 有 %d 个字，超过 16", h, tail, n)
			}
		}
	}
	if RandomNickname() == "" {
		t.Fatal("昵称为空")
	}
}
