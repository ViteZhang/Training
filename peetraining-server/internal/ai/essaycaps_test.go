package ai

import (
	"context"
	"errors"
	"testing"

	cloudai "peetraining-server/internal/cloud/ai"
)

func TestClassifyMock(t *testing.T) {
	cases := []struct {
		name, text, cat, sub string
	}{
		{"真题.pdf", "2024 年真题\n1. 意境\n2. 典型", "question", "真题汇编"},
		{"练习.docx", "1. 意境\n2. 典型\n3. 象征", "question", "习题集"},
		{"讲义.docx", "第一章 先秦文学\n神话是……", "reference", "讲义"},
		{"908.docx", "作文评分细则\n立意（30 分）", "essay", "评分细则"},
		{"范文.docx", "作文范文《守正》\n正文", "essay", "范文"},
		{"真题作文.docx", "2024 年考研作文题：写作", "essay", "作文真题"},
		{"笔记.docx", "写作素材积累", "essay", "写作笔记"},
	}
	for _, c := range cases {
		out, err := mockClassify(ClassifyIn{FileName: c.name, Pages: []KPPage{{No: 1, Text: c.text}}})
		if err != nil || out.Category != c.cat || out.SubType != c.sub {
			t.Errorf("%s：%+v", c.name, out)
		}
		if err := Classify.Check(ClassifyIn{}, &out); err != nil {
			t.Errorf("%s：mock 输出应能通过校验：%v", c.name, err)
		}
	}
	if Classify.Check(ClassifyIn{}, &ClassifyOut{Category: "essay", SubType: "讲义"}) == nil {
		t.Error("细分类型不匹配应不合格")
	}
}

func TestCheckEssay(t *testing.T) {
	in := EssayIn{Pages: []KPPage{{No: 1, Text: "作文题：守正与创新\n范文正文第一段。\n素材：屠呦呦提取青蒿素。"}}}
	ok := EssayOut{
		Topics:    []EssayTopic{{Title: "守正与创新", Page: 1}},
		Models:    []ModelEssay{{Title: "x", Content: "范文正文第一段。", Page: 1}},
		Materials: []EssayMaterial{{Theme: "创新", Content: "屠呦呦提取青蒿素", Page: 1}, {Theme: "补充", Content: "编的", AISupplement: true}},
		Rubric:    &EssayRubric{FullScore: 60, Dimensions: []RubricDimension{{Score: 30}, {Score: 30}}},
	}
	if err := checkEssay(in, &ok); err != nil {
		t.Fatal(err)
	}
	bad := []func(o *EssayOut){
		func(o *EssayOut) { o.Topics[0].Title = "不存在的题" },
		func(o *EssayOut) { o.Models[0].Content = "改写过的范文" },
		func(o *EssayOut) { o.Materials[1].AISupplement = false },
		func(o *EssayOut) { o.Rubric.FullScore = 100 },
	}
	for i, mut := range bad {
		o := ok
		o.Topics = append([]EssayTopic(nil), ok.Topics...)
		o.Models = append([]ModelEssay(nil), ok.Models...)
		o.Materials = append([]EssayMaterial(nil), ok.Materials...)
		r := *ok.Rubric
		o.Rubric = &r
		mut(&o)
		if checkEssay(in, &o) == nil {
			t.Errorf("第 %d 种错误应不合格", i+1)
		}
	}
	// 经过引擎：编造范文 → 重试后 AIFailed
	m := cloudai.NewMock()
	m.Set(OrganizeEssay.Name, `{"topics":[],"methods":[],"materials":[],"model_essays":[{"title":"x","content":"编造","page":1,"structure":{"opening":"","points":[],"elevation":"","ending":""}}]}`)
	if _, _, err := OrganizeEssay.Run(context.Background(), NewEngine(Config{Client: m, Queries: &fakeQ{}}), 1, in); !errors.Is(err, ErrInvalidOutput) {
		t.Fatal(err)
	}
	for _, c := range []string{Classify.Name, OrganizeEssay.Name} {
		if _, _, err := render(c, "v1", EssayIn{}); c == OrganizeEssay.Name && err != nil {
			t.Error(err)
		}
	}
	if _, err := OrganizeEssay.compiled(); err != nil {
		t.Fatal(err)
	}
	if _, err := Classify.compiled(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := render(Classify.Name, "v1", ClassifyIn{}); err != nil {
		t.Fatal(err)
	}
}
