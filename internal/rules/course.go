package rules

import (
	"strings"
	"unicode"
)

// CourseKey 是一门专业课的归并键（PRD v3 增补「申请收录」）：按「院校 + 专业课名称」归并，有代码时优先按代码。
// 院校专业课库的待入库列表与申请收录共用这个键。
func CourseKey(school, code, name string) string {
	school = normalize(school)
	if c := normalize(code); c != "" {
		return school + "|code:" + c
	}
	return school + "|name:" + normalize(name)
}

// normalize 去掉空白与常见标点、统一大小写，避免「西方哲学史」「西方 哲学史」被算成两门课。
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || r == '·' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
