package ai

import "testing"

func TestGradeCheckAndMock(t *testing.T) {
	in := GradeIn{QType: "term", Answer: "意境是情景交融的艺术境界。它虚实相生。",
		Points: []GradePoint{{Seq: 1, Content: "情景交融", Score: 4}, {Seq: 2, Content: "虚实相生", Score: 3}, {Seq: 3, Content: "引出王国维境界说", Score: 3}}}
	out, err := mockGrade(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := Grade.Check(in, &out); err != nil {
		t.Fatalf("mock 结果应能通过校验：%v", err)
	}
	if out.Points[0].Verdict != "hit" || out.Points[2].Verdict != "miss" || out.Total() != 7 {
		t.Errorf("mock 判定：%+v total=%v", out.Points, out.Total())
	}
	bad := GradeOut{Points: []GradeResult{{Seq: 1, Verdict: "hit", Quote: "考生没写过的话"}, {Seq: 2, Verdict: "miss"}, {Seq: 3, Verdict: "miss"}}}
	if Grade.Check(in, &bad) == nil {
		t.Error("引用不是考生原话应校验失败")
	}
	over := GradeOut{Points: []GradeResult{{Seq: 1, Verdict: "partial", Score: 9, Quote: "情景交融"}, {Seq: 2, Verdict: "miss"}, {Seq: 3, Verdict: "miss"}}}
	if Grade.Check(in, &over) == nil {
		t.Error("得分超过采分点分值应校验失败")
	}
	missing := GradeOut{Points: []GradeResult{{Seq: 1, Verdict: "miss"}}}
	if Grade.Check(in, &missing) == nil {
		t.Error("漏判采分点应校验失败")
	}
	spaced := GradeOut{Points: []GradeResult{{Seq: 1, Verdict: "hit", Score: 1, Quote: "情景 交融"}, {Seq: 2, Verdict: "miss", Score: 3}, {Seq: 3, Verdict: "miss"}}}
	if err := Grade.Check(in, &spaced); err != nil || spaced.Points[0].Score != 4 || spaced.Points[1].Score != 0 {
		t.Errorf("命中给满分、遗漏 0 分、引用忽略空白：%v %+v", err, spaced.Points)
	}
}
