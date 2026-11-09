package rules

import "testing"

func TestCourseKey(t *testing.T) {
	cases := []struct {
		a, b [3]string
		same bool
	}{
		// 有代码时按代码归并，课程名写法不同也算一门。
		{[3]string{"四川大学", "812", "西方哲学史"}, [3]string{"四川大学 ", "812", "西方哲学"}, true},
		// 没有代码时按名称，忽略空格与标点。
		{[3]string{"四川大学", "", "西方哲学史"}, [3]string{"四川大学", "", "西方 哲学史。"}, true},
		{[3]string{"四川大学", "812", ""}, [3]string{"四川大学", "662", ""}, false},
		{[3]string{"四川大学", "", "西方哲学史"}, [3]string{"南京大学", "", "西方哲学史"}, false},
	}
	for _, c := range cases {
		got := CourseKey(c.a[0], c.a[1], c.a[2]) == CourseKey(c.b[0], c.b[1], c.b[2])
		if got != c.same {
			t.Errorf("%v vs %v：期望相同=%v", c.a, c.b, c.same)
		}
	}
}

func TestExamSubjectKey(t *testing.T) {
	cases := []struct {
		scope, school, code, name string
		year                      int
		want                      string
	}{
		{ScopeSchool, "四川大学", "812", "西方哲学史", 0, "school|四川大学|812|0"},
		{ScopeSchool, "四川大学", "", "西方哲学史", 2027, "school|四川大学|n:西方哲学史|2027"},
		// 统考不属于任何院校：传了院校也忽略。
		{ScopeNational, "北京师范大学", "333", "教育综合", 0, "national||333|0"},
	}
	for _, c := range cases {
		if got := ExamSubjectKey(c.scope, c.school, c.code, c.name, c.year); got != c.want {
			t.Errorf("ExamSubjectKey(%q,%q,%q,%q,%d) = %q, want %q", c.scope, c.school, c.code, c.name, c.year, got, c.want)
		}
	}
	if !ValidDiscipline("教育学") || ValidDiscipline("中国语言文学") || ValidDiscipline("") {
		t.Error("学科只能是固定的学科门类")
	}
}
