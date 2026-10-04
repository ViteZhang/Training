package ai

import (
	"strings"
	"testing"
)

func essayIn() EssayGradeIn {
	return EssayGradeIn{Subject: "写作", Topic: "以「守正与创新」为题", RequiredWords: 100,
		Dimensions: []EssayDim{{Name: "立意", Score: 30}, {Name: "结构", Score: 30}},
		Paragraphs: []string{"守正是创新的根基。没有根基，创新无从谈起。", "创新让守正有了活力。", "所以要守正创新。"}}
}

func TestEssayGradeCheck(t *testing.T) {
	in := essayIn()
	good := func() EssayGradeOut {
		return EssayGradeOut{Dimensions: []EssayDimScore{{Name: "结构", Score: 20.3}, {Name: "立意", Score: 25}}, Suggestions: []string{"补论据"},
			Annotations: []EssayAnnotation{{Paragraph: 2, Quote: "创新让守正 有了活力。", Issue: "展开不够", Suggestion: "补例子"}}}
	}
	out := good()
	if err := EssayGrade.Check(in, &out); err != nil {
		t.Fatal(err)
	}
	if out.Dimensions[0].Name != "立意" || out.Dimensions[1].Score != 20.5 || out.Total() != 45.5 {
		t.Errorf("按评分标准的顺序排列、取到 0.5 分：%+v", out.Dimensions)
	}
	bad := []func(o *EssayGradeOut){
		func(o *EssayGradeOut) { o.Dimensions[0].Score = 31 },
		func(o *EssayGradeOut) { o.Dimensions = o.Dimensions[:1] },
		func(o *EssayGradeOut) { o.Dimensions[1].Name = "文采" },
		func(o *EssayGradeOut) { o.Annotations[0].Paragraph = 4 },
		func(o *EssayGradeOut) { o.Annotations[0].Quote = "不是原文的话" },
		func(o *EssayGradeOut) { o.Suggestions = []string{"这样写保证得分"} },
	}
	for i, f := range bad {
		o := good()
		f(&o)
		if err := EssayGrade.Check(in, &o); err == nil {
			t.Errorf("第 %d 种不合格输出应被拒绝", i)
		}
	}
}

func TestEssayMocksAndPrompts(t *testing.T) {
	in := essayIn()
	a, _ := mockEssayGrade(in)
	b, _ := mockEssayGrade(in)
	if a.Total() != b.Total() || len(a.Dimensions) != 2 || a.Thesis != "守正是创新的根基。" {
		t.Errorf("mock 批改应确定：%+v", a)
	}
	if err := EssayGrade.Check(in, &a); err != nil {
		t.Errorf("mock 输出应通过校验：%v", err)
	}
	p, err := mockEssayPrompt(EssayPromptIn{PastTopics: []string{mockTopics[0]}})
	if err != nil || p.Topic == mockTopics[0] || EssayPrompt.Check(EssayPromptIn{PastTopics: []string{mockTopics[0]}}, &p) != nil {
		t.Errorf("AI 命题不重复真题：%+v %v", p, err)
	}
	dup := EssayPromptOut{Topic: mockTopics[0]}
	if EssayPrompt.Check(EssayPromptIn{Avoid: []string{mockTopics[0]}}, &dup) == nil {
		t.Error("和最近出过的题重复应被拒绝")
	}
	for _, c := range []struct {
		name, version string
		in            any
		schema        func() error
	}{
		{EssayGrade.Name, EssayGrade.Version, in, func() error { _, err := EssayGrade.compiled(); return err }},
		{EssayPrompt.Name, EssayPrompt.Version, EssayPromptIn{Subject: "写作", PastTopics: []string{"x"}}, func() error { _, err := EssayPrompt.compiled(); return err }},
	} {
		if err := c.schema(); err != nil {
			t.Errorf("%s 的 Schema 无效：%v", c.name, err)
		}
		_, usr, err := render(c.name, c.version, c.in)
		if err != nil || usr == "" {
			t.Errorf("%s 提示词渲染失败：%v", c.name, err)
		}
		if c.name == EssayGrade.Name && !strings.Contains(usr, "[3] 所以要守正创新。") {
			t.Errorf("正文按段编号：%s", usr)
		}
	}
}
