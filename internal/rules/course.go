package rules

import (
	"strconv"
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

// 考试科目（PRD v3 增补「考试科目」，T50）。

// 命题方式：院校自命题 / 全国统考。
const (
	ScopeSchool   = "school"
	ScopeNational = "national"
)

// Disciplines 是考试科目可选的学科（学科门类，不再细分）：后台录入只能选这些，1.1 按它分组。
var Disciplines = []string{"哲学", "经济学", "法学", "教育学", "文学", "历史学", "理学", "工学", "农学", "医学", "军事学", "管理学", "艺术学", "交叉学科"}

// ValidDiscipline 判断学科是否在固定选项里。
func ValidDiscipline(d string) bool {
	for _, x := range Disciplines {
		if x == d {
			return true
		}
	}
	return false
}

// ExamSubjectKey 是考试科目的去重键：自命题按「院校 + 代码 + 年份」，统考按「代码 + 年份」，没有代码时用名称。
// 格式与迁移 00043 回填的一致（各字段已去掉首尾空白），年份 0 表示各年通用。
func ExamSubjectKey(scope, school, code, name string, year int) string {
	if scope == ScopeNational {
		school = ""
	}
	id := code
	if id == "" {
		id = "n:" + name
	}
	return scope + "|" + school + "|" + id + "|" + strconv.Itoa(year)
}
