package importer

import "testing"

// 旧规则切不开 groupedPaper（认不出「## 2024 …真题」与 Markdown 标题），模型整理出的题干是干净的，但没有年份、
// 名词解释也没有分值（实测 qwen-plus）。补全时按现在的规则重新切，按题干对上块。
func TestPlanRepair(t *testing.T) {
	blocks := Split([]Page{{No: 1, Text: groupedPaper}})
	qs := []repairQ{
		{ID: 1, QType: "term", Stem: "意境", ContentHash: "h1"},
		{ID: 2, QType: "term", Stem: "典型", ContentHash: "h2"},
		// 模型给了分值的：只补年份与分组
		{ID: 3, QType: "discussion", Stem: "什么是比较文学形象学？", ContentHash: "h3", HasScore: true},
		// 旧规则把下一组的标题并进了题干
		{ID: 4, QType: "discussion", Stem: "谈谈异国形象中「他者」与「自我」的关系。\n**第二组：请选择中国民间文学专业的考生作答**", ContentHash: "h4"},
		// 共用材料补到题上；题型被识别错的不改题型
		{ID: 5, QType: "essay", Stem: "请翻译上面这段话。", ContentHash: "h5"},
		// 后来又按新规则导入过一遍、题库里已有同年同题的真题：不补，免得算两次
		{ID: 6, QType: "discussion", Stem: "请谈谈你对文章主题内涵的理解。", ContentHash: "dup"},
		// 资料里没有的题
		{ID: 7, QType: "short_answer", Stem: "简述唐传奇的艺术成就。", ContentHash: "h7"},
		// 同一份资料里重复的两道（旧导入留下的重复题）只补第一道
		{ID: 8, QType: "term", Stem: "意境", ContentHash: "h1"},
	}
	taken := map[string]bool{examKey("dup", 2024): true}
	got := map[uint64]repairFix{}
	for _, f := range planRepair(qs, blocks, taken) {
		got[f.ID] = f
	}
	if len(got) != 5 {
		t.Fatalf("应补全 5 道：%+v", got)
	}
	if f := got[1]; f.Year != 2024 || f.Score != 6 || f.Group != "" || f.Passage != "" {
		t.Errorf("名词解释补年份与分值：%+v", f)
	}
	if f := got[2]; f.Year != 2024 || f.Score != 6 {
		t.Errorf("典型：%+v", f)
	}
	if f := got[3]; f.Year != 2024 || f.Score != 0 || f.Group != "第一组" || f.GroupMajors != "文艺学、比较文学与世界文学" {
		t.Errorf("已有分值的只补年份与分组：%+v", f)
	}
	if f := got[4]; f.Year != 2024 || f.Score != 35 || f.Group != "第一组" {
		t.Errorf("题干带着旧标题也要对上：%+v", f)
	}
	if f := got[5]; f.Year != 2024 || f.Group != "第三组" || f.Passage == "" {
		t.Errorf("共用材料补到题上：%+v", f)
	}
	for _, id := range []uint64{6, 7, 8} {
		if f, ok := got[id]; ok {
			t.Errorf("第 %d 道不应补：%+v", id, f)
		}
	}
	if !taken[examKey("h1", 2024)] {
		t.Error("补上的题要记进已有真题")
	}
}

// 已有年份、只缺分值的真题也补分值；资料里认不出年份的题不当真题。
func TestPlanRepairScoreOnlyAndNoYear(t *testing.T) {
	blocks := Split([]Page{{No: 1, Text: groupedPaper}})
	fixes := planRepair([]repairQ{{ID: 1, QType: "term", Stem: "意境", ContentHash: "h1", HasYear: true}}, blocks, map[string]bool{})
	if len(fixes) != 1 || fixes[0].Year != 0 || fixes[0].Score != 6 {
		t.Fatalf("只补分值：%+v", fixes)
	}
	noYear := Split([]Page{{No: 1, Text: "一、名词解释（每题 6 分）\n1. 意境\n2. 典型"}})
	if fixes := planRepair([]repairQ{{ID: 1, QType: "term", Stem: "意境", ContentHash: "h1"}}, noYear, map[string]bool{}); len(fixes) != 0 {
		t.Fatalf("认不出年份的不补：%+v", fixes)
	}
}
