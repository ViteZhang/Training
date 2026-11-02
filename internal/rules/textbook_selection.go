package rules

import (
	"peetraining-server/internal/apperr"
	"strings"
)

// TextbookSelection 只在用户主动选择明确版次时保存；自由书名保持可重新匹配。
// 同一书名可锁定多个版次（套书的上下卷各一条）。
type TextbookSelection struct {
	Input     string `json:"input"`
	EditionID uint64 `json:"edition_id"`
}

// MaxEditionsPerTextbook 限制一个书名锁定的册数，防止异常请求写入过大的 JSON。
const MaxEditionsPerTextbook = 10

func CleanTextbookSelections(books []string, selections []TextbookSelection) ([]TextbookSelection, error) {
	allowed := map[string]bool{}
	for _, b := range books {
		allowed[b] = true
	}
	out := []TextbookSelection{}
	seen := map[TextbookSelection]bool{}
	perInput := map[string]int{}
	for _, v := range selections {
		v.Input = strings.TrimSpace(v.Input)
		if !allowed[v.Input] {
			continue
		} // 删掉书名时同时删掉显式绑定；旧客户端仍可只提交书名。
		if v.EditionID == 0 {
			return nil, apperr.New(apperr.BadRequest, "教材版次选择不正确")
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		perInput[v.Input]++
		if perInput[v.Input] > MaxEditionsPerTextbook {
			return nil, apperr.New(apperr.BadRequest, "教材版次选择不正确")
		}
		out = append(out, v)
	}
	return out, nil
}
