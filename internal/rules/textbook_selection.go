package rules

import (
	"peetraining-server/internal/apperr"
	"strings"
)

// TextbookSelection 只在用户主动选择明确版次时保存；自由书名保持可重新匹配。
type TextbookSelection struct {
	Input     string `json:"input"`
	EditionID uint64 `json:"edition_id"`
}

func CleanTextbookSelections(books []string, selections []TextbookSelection) ([]TextbookSelection, error) {
	allowed := map[string]bool{}
	for _, b := range books {
		allowed[b] = true
	}
	out := []TextbookSelection{}
	seen := map[string]bool{}
	for _, v := range selections {
		v.Input = strings.TrimSpace(v.Input)
		if !allowed[v.Input] {
			continue
		} // 删掉书名时同时删掉显式绑定；旧客户端仍可只提交书名。
		if v.EditionID == 0 || seen[v.Input] {
			return nil, apperr.New(apperr.BadRequest, "教材版次选择不正确")
		}
		seen[v.Input] = true
		out = append(out, v)
	}
	return out, nil
}
