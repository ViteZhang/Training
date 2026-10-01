package auth

import (
	"crypto/rand"
	"math/big"
)

// inviteAlphabet 去掉了 0 / O、1 / I 等易混字符（与兑换码一致，PRD 13.4）。
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newInviteCode 生成 8 位邀请码。
func newInviteCode() (string, error) {
	b := make([]byte, 8)
	max := big.NewInt(int64(len(inviteAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = inviteAlphabet[n.Int64()]
	}
	return string(b), nil
}

// MaskPhone 返回脱敏手机号：138****5678。
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}
