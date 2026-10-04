package auth

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.10", "1.10.0", -1}, {"1.0", "1.0.0", 0}, {"v2.0.0", "1.9.9", 1}, {"1.0.0-beta", "1.0.0", 0}, {"", "0.0.1", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d", c.a, c.b, got)
		}
	}
}

func TestMaskAndInvite(t *testing.T) {
	if MaskPhone("13812345678") != "138****5678" || MaskPhone("123") != "123" {
		t.Error("脱敏")
	}
	code, err := newInviteCode()
	if err != nil || len(code) != 8 {
		t.Fatal(code, err)
	}
	for _, c := range code {
		switch c {
		case '0', 'O', '1', 'I':
			t.Errorf("邀请码不能有易混字符：%s", code)
		}
	}
}

func TestTokenRoundTrip(t *testing.T) {
	s := &Service{jwtSecret: []byte("k"), now: timeNow}
	tok, _, err := s.signAccess(42, "dev", timeNow(), 3600e9)
	if err != nil {
		t.Fatal(err)
	}
	if id, did, err := s.ParseAccess(tok); err != nil || id != 42 || did != "dev" {
		t.Fatalf("%d %s %v", id, did, err)
	}
	other := &Service{jwtSecret: []byte("other"), now: timeNow}
	if _, _, err := other.ParseAccess(tok); err == nil {
		t.Error("密钥不同应失败")
	}
	if _, _, err := s.ParseAccess("garbage"); err == nil {
		t.Error("乱码应失败")
	}
}
