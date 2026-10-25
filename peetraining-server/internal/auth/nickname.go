package auth

import (
	"crypto/rand"
	"math/big"
)

// 默认昵称：注册时随机给一个上岸、鼓励类的昵称，由「前半句 + 后半句」拼成，如「一战上岸的锦鲤」。
// 用户没设过昵称（老账号为空）时，读取「我的」信息时补一个。昵称最长 16 个字。
var (
	nicknameHeads = []string{
		"一战上岸的", "稳稳上岸的", "逢考必过的", "必上岸的", "梦校在望的",
		"高分预定的", "笔下生花的", "背书不累的", "锦鲤附体的", "早起背书的",
		"越学越顺的", "专业课稳了的", "采分点全中的", "论述题满分的", "复试见的",
		"今天也自律的", "不熬夜也高分的", "考神附体的", "一路开挂的", "答题如有神的",
	}
	nicknameTails = []string{
		"研友", "锦鲤", "学霸", "追光者", "小太阳",
		"准研究生", "备考人", "上岸人", "读书人", "小机灵",
		"夜行者", "答题王", "笔杆子", "背书侠", "种子选手",
	}
)

// RandomNickname 随机生成一个默认昵称。
func RandomNickname() string {
	return pick(nicknameHeads) + pick(nicknameTails)
}

func pick(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}
