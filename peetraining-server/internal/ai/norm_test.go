package ai

import "testing"

func TestGradeNormCheck(t *testing.T) {
	in := NormIn{QType: "term", Answer: "典型是共性与个性的统一。其特征一是鲜明的个性。",
		Elements: []NormElement{{Name: "定义"}, {Name: "特征要点"}, {Name: "出处或例子"}}}
	out, err := mockNorm(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := GradeNorm.Check(in, &out); err != nil {
		t.Fatalf("mock 应能通过校验：%v", err)
	}
	if !out.Elements[0].Present || out.Elements[2].Present || len(out.Suggestions) == 0 {
		t.Errorf("mock 判定：%+v", out)
	}
	bad := NormOut{Elements: []NormResult{{Name: "定义", Present: true, Quote: "没写过的话"}, {Name: "特征要点"}, {Name: "出处或例子"}}}
	if GradeNorm.Check(in, &bad) == nil {
		t.Error("引用不是考生原话应校验失败")
	}
	wrong := NormOut{Elements: []NormResult{{Name: "观点"}, {Name: "特征要点"}, {Name: "出处或例子"}}}
	if GradeNorm.Check(in, &wrong) == nil {
		t.Error("要素名不对应校验失败")
	}
}
