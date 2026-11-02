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
