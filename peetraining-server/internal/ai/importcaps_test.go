package ai

import (
	"errors"
	"strings"
	"testing"
)

// 块上没有年份、分值时，模型会把可选字段输出成 null（qwen-plus 实测），应按缺省处理而不是整批判为不合格。
func TestStructureAcceptsNullOptionals(t *testing.T) {
	in := StructureIn{Blocks: []Block{{ID: 1, Page: 1, Text: "1. 现代汉民族共同语"}}}
	out, err := Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":"term","stem":"现代汉民族共同语",
		"options":null,"answer":null,"score":null,"exam_year":null,"question_no":"1","page":1,"confidence":0.9,"is_recollection":null}]}`)
	if err != nil {
		t.Fatalf("可选字段为 null 应能通过校验：%v", err)
	}
	it := out.Items[0]
	if it.ExamYear != nil || it.Score != nil || it.Answer != "" || it.Options != nil {
		t.Fatalf("null 应解析为缺省：%+v", it)
	}
	// 必填字段仍然要有：缺题型照样不合格（唯一一条不合格，整批判不合格）。
	_, err = Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":null,"stem":"现代汉民族共同语","page":1,"confidence":0.9}]}`)
	if !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("题目缺题型应不合格：%v", err)
	}
	// 范围校验不受影响。
	_, err = Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":"term","stem":"现代汉民族共同语","exam_year":1900,"page":1,"confidence":0.9}]}`)
	if !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("年份超出范围应不合格：%v", err)
	}
}

// 不开思考的 qwen3 模型会把路径照抄成 [["板块", "章节"]]（实测 qwen3.8-flash），整理成一层后应能通过校验。
func TestKPPathFlattened(t *testing.T) {
	in := KPIn{Pages: []KPPage{{No: 1, Text: "一、神话\n神话：远古人民表现对自然及社会现象的认识。"}}}
	out, err := ExtractKPs.validate(in, `{"points":[{"name":"神话","original_text":"神话：远古人民表现对自然及社会现象的认识。","page":1,
		"path":[["第一章 先秦文学","一、神话"]],"rubric_points":[{"content":"远古人民","keywords":["远古人民"]}]}]}`)
	if err != nil {
		t.Fatalf("嵌套路径应整理后通过：%v", err)
	}
	if p := out.Points[0].Path; len(p) != 2 || p[0] != "第一章 先秦文学" || p[1] != "一、神话" {
		t.Fatalf("路径：%v", p)
	}
}

// 一批里个别条目不合格时只剔除这几条，其余照常保留（川大真题合集曾因一条整批 108 道作废）；
// 题型标题里的要求并进题干算来自原文。
func TestStructureDropsBadItemsOnly(t *testing.T) {
	sec := "二、简答（每題 10 分，共 40 分）说出以下各语的出处，并简要分析其含义："
	in := StructureIn{Blocks: []Block{
		{ID: 1, Page: 1, Section: sec, Text: "1.興於詩，立於禮，成於樂."},
		{ID: 2, Page: 1, Section: sec, Text: "2.窮則變，變則通，通則久。"},
		{ID: 3, Page: 1, Text: "3.坐忘"},
	}}
	out, err := Structure.validate(in, `{"items":[
		{"block":1,"kind":"question","qtype":"short_answer","stem":"说出以下各语的出处，并简要分析其含义：興於詩，立於禮，成於樂.","page":1,"confidence":0.9},
		{"block":2,"kind":"question","qtype":"short_answer","stem":"论述宋明理学的发展脉络","page":1,"confidence":0.9},
		{"block":3,"kind":"question","qtype":"term","stem":"坐忘","page":1,"confidence":0.9},
		{"block":9,"kind":"question","qtype":"term","stem":"心斋","page":1,"confidence":0.9}]}`)
	if err != nil {
		t.Fatalf("个别条目不合格不应整批失败：%v", err)
	}
	if len(out.Items) != 2 || out.Items[0].Block != 1 || out.Items[1].Block != 3 {
		t.Fatalf("保留的条目：%+v", out.Items)
	}
	if len(out.Dropped) != 1 || out.Dropped[0] != 2 {
		t.Fatalf("剔除的块：%v", out.Dropped)
	}
}

// 模型遇到古文翻译、标点、病句修改题时会自造题型（北师大 708 真题合集实测），应归为 other，不能让整批 Schema 不通过、整份解析失败。
func TestStructureUnknownQTypeBecomesOther(t *testing.T) {
	in := StructureIn{Blocks: []Block{{ID: 1, Page: 1, Text: "1. 请用现代汉语翻译下面文中大字正文部分"}, {ID: 2, Page: 1, Text: "2. 名词解释：六书"}}}
	out, err := Structure.validate(in, `{"items":[
		{"block":1,"kind":"question","qtype":"translation","stem":"请用现代汉语翻译下面文中大字正文部分","page":1,"confidence":0.9},
		{"block":2,"kind":"question","qtype":" Term ","stem":"名词解释：六书","page":1,"confidence":0.9}]}`)
	if err != nil {
		t.Fatalf("自造题型应归为 other：%v", err)
	}
	if out.Items[0].QType != "other" || out.Items[1].QType != "term" {
		t.Fatalf("题型应为 other、term：%+v", out.Items)
	}
}

// 生成参考答案：模型给的采分点合计差一点、关键词改写了，按比例调分值、去掉找不到的关键词，不再整题失败。
func TestGenerateAnswerRepairsPoints(t *testing.T) {
	answer := "六书指象形、指事、会意、形声、转注、假借。前四者是造字之法，后二者是用字之法。许慎的划分影响深远。"
	out := &GenerateAnswerOut{Answer: answer, Citations: []AnswerCitation{}, UsesGeneralKnowledge: true, Points: []RubricPoint{
		{Content: "六书指象形、指事、会意、形声、转注、假借。", Score: 4, Keywords: []string{"象形", "六种造字法"}},
		{Content: "前四者是造字之法，后二者是用字之法。", Score: 3, Keywords: []string{"四体二用"}},
		{Content: "许慎的划分影响深远。", Score: 2, Keywords: []string{"许慎"}},
	}}
	if err := GenerateAnswer.Check(GenerateAnswerIn{QType: "short_answer", Stem: "请列出六书", Score: 10}, out); err != nil {
		t.Fatal(err)
	}
	sum := 0.0
	for _, p := range out.Points {
		sum += p.Score
		if len(p.Keywords) == 0 {
			t.Fatalf("关键词不能为空：%+v", p)
		}
		for _, k := range p.Keywords {
			if !strings.Contains(answer, k) {
				t.Fatalf("关键词不在答案里：%q", k)
			}
		}
	}
	if sum != 10 || out.Points[0].Score < out.Points[2].Score {
		t.Fatalf("分值：%+v", out.Points)
	}
	if got := out.Points[0].Keywords; len(got) != 1 || got[0] != "象形" {
		t.Fatalf("只去掉找不到的：%v", got)
	}
	if got := out.Points[1].Keywords; got[0] != "前四者是造字" {
		t.Fatalf("一个都不剩时取原句开头：%v", got)
	}

	// 采分点太多、每条分不到 0.5 分时调不出来，照旧报错重试。
	many := &GenerateAnswerOut{Answer: answer, Citations: []AnswerCitation{}, UsesGeneralKnowledge: true}
	for range 5 {
		many.Points = append(many.Points, RubricPoint{Content: "许慎的划分影响深远。", Score: 1, Keywords: []string{"许慎"}})
	}
	if err := GenerateAnswer.Check(GenerateAnswerIn{QType: "term", Stem: "六书", Score: 2}, many); err == nil {
		t.Fatal("调不出来应当报错")
	}
	// 答案里的 Markdown 加粗去掉，采分点原句就能对上。
	md := &GenerateAnswerOut{Answer: "1. **诗可以兴**：指诗歌能感发志意。", Citations: []AnswerCitation{}, Points: []RubricPoint{{Content: "诗可以兴：指诗歌能感发志意。", Score: 10, Keywords: []string{"**感发志意**"}}}}
	if err := GenerateAnswer.Check(GenerateAnswerIn{QType: "short_answer", Stem: "兴观群怨", Score: 10}, md); err != nil || strings.Contains(md.Answer, "*") || md.Points[0].Keywords[0] != "感发志意" {
		t.Fatalf("加粗：%v %+v", err, md)
	}
	// 采分点是答案里一段话的略写（结尾标点不同、省掉引诗）：算出自答案。
	brief := &GenerateAnswerOut{Answer: "《小雅·鹿鸣》描写宴饮场景，“呦呦鹿鸣，食野之苹”，表现了主人对宾客的热情招待，体现了人际和谐的功能，影响深远。", Citations: []AnswerCitation{},
		Points: []RubricPoint{{Content: "《小雅·鹿鸣》描写宴饮场景，表现了主人对宾客的热情招待，体现了人际和谐的功能。", Score: 10, Keywords: []string{"鹿鸣"}}}}
	if err := GenerateAnswer.Check(GenerateAnswerIn{QType: "short_answer", Stem: "诗可以群", Score: 10}, brief); err != nil {
		t.Fatalf("略写：%v", err)
	}
	// 采分点不是答案原句：照旧报错。
	bad := &GenerateAnswerOut{Answer: answer, Citations: []AnswerCitation{}, Points: []RubricPoint{{Content: "答案里没有这句", Score: 10, Keywords: []string{"许慎"}}}}
	if err := GenerateAnswer.Check(GenerateAnswerIn{QType: "short_answer", Stem: "六书", Score: 10}, bad); err == nil {
		t.Fatal("采分点不是原句应当报错")
	}
}
