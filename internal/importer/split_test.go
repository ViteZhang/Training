package importer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

func TestSplitExamPaper(t *testing.T) {
	pages := []Page{
		{No: 1, Text: `2024 年某某大学 中国语言文学基础 考研真题
一、名词解释（每题 5 分，共 20 分）
1. 意境
2、典型
二、简答题（每小题 15 分）
3. 简述唐传奇的艺术成就。
(1) 情节
(2) 人物`},
		{No: 2, Text: `三、论述题
4. 论述鲁迅小说的
启蒙意义。
参考答案
1. 意境是情景交融的艺术境界。
3. 情节曲折，人物鲜明。`},
		{No: 3, Text: "这一页接着上一页的答案继续写。"},
		{No: 4, Text: "短"},
		{No: 5, Text: `2023年某某大学考研试题
一、单项选择题（每题 2 分）
1. 下列属于唐传奇的是
A. 莺莺传
B. 搜神记`},
	}
	got := Split(pages)
	want := []ai.Block{
		{ID: 1, Page: 1, QType: "term", Score: 5, Year: 2024, No: "1", Text: "1. 意境"},
		{ID: 2, Page: 1, QType: "term", Score: 5, Year: 2024, No: "2", Text: "2、典型"},
		{ID: 3, Page: 1, QType: "short_answer", Score: 15, Year: 2024, No: "3", Text: "3. 简述唐传奇的艺术成就。\n(1) 情节\n(2) 人物"},
		{ID: 4, Page: 2, QType: "discussion", Year: 2024, No: "4", Text: "4. 论述鲁迅小说的\n启蒙意义。"},
		{ID: 5, Page: 2, QType: "discussion", Year: 2024, No: "1", Answer: true, Text: "1. 意境是情景交融的艺术境界。"},
		// 题目跨页时续到下一页
		{ID: 6, Page: 2, QType: "discussion", Year: 2024, No: "3", Answer: true, Text: "3. 情节曲折，人物鲜明。\n这一页接着上一页的答案继续写。\n短"},
		{ID: 7, Page: 5, QType: "single_choice", Score: 2, Year: 2023, No: "1", Text: "1. 下列属于唐传奇的是\nA. 莺莺传\nB. 搜神记"},
	}
	if len(got) != len(want) {
		t.Fatalf("块数 %d：%+v", len(got), got)
	}
	for i := range want {
		g := got[i]
		g.Section = ""
		if g != want[i] {
			t.Errorf("第 %d 块：\n got %+v\nwant %+v", i+1, g, want[i])
		}
	}
	if !strings.HasPrefix(got[0].Section, "一、名词解释") {
		t.Errorf("section：%q", got[0].Section)
	}
}

func TestSplitWholePages(t *testing.T) {
	got := Split([]Page{
		{No: 1, Text: "这一页是一大段没有题号的讲义文字，规则切不开，需要交给模型整体处理。"},
		{No: 2, Text: "短"},
		{No: 3, Text: "第三页同样没有题号，但是文字足够长，也要整页交给模型。"},
	})
	if len(got) != 2 || !got[0].Whole || got[0].Page != 1 || got[1].Page != 3 || got[1].ID != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestSplitParenStyle(t *testing.T) {
	got := Split([]Page{{No: 1, Text: "（1）什么是意境\n答案：情景交融\n（2）什么是典型"}})
	if len(got) != 2 || got[0].No != "1" || got[1].Text != "（2）什么是典型" {
		t.Fatalf("%+v", got)
	}
}

func TestChunk(t *testing.T) {
	bs := []ai.Block{{Text: strings.Repeat("a", 30)}, {Text: strings.Repeat("b", 30)}, {Text: strings.Repeat("c", 50)}, {Text: strings.Repeat("d", 200)}}
	got := Chunk(bs, 100)
	if len(got) != 3 || len(got[0]) != 2 || len(got[1]) != 1 || len(got[2]) != 1 {
		t.Fatalf("%d", len(got))
	}
	if Chunk(nil, 100) != nil {
		t.Fatal("空")
	}
}

func TestGiveUpReason(t *testing.T) {
	cases := map[dbq.ImportJobMaterialsStep]string{
		dbq.ImportJobMaterialsStepExtract:   "文件读取或文字识别服务",
		dbq.ImportJobMaterialsStepModerate:  "内容安全审核服务",
		dbq.ImportJobMaterialsStepStructure: "AI 解析服务",
	}
	for step, want := range cases {
		if got := giveUpReason(step); !strings.HasPrefix(got, want) || !strings.Contains(got, "没扣额度") {
			t.Errorf("%s：%q", step, got)
		}
	}
}

func TestRunBatches(t *testing.T) {
	var running, peak atomic.Int32
	out, errs, err := runBatches(context.Background(), 10, func(_ context.Context, i int) (int, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		running.Add(-1)
		if i == 3 {
			return 0, apperr.New(apperr.AIFailed, "这一批识别失败")
		}
		return i * i, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range out {
		if i == 3 {
			if errs[i] == nil {
				t.Error("第 4 批应记为识别失败")
			}
			continue
		}
		if v != i*i || errs[i] != nil {
			t.Errorf("结果应按批的顺序：%d → %d %v", i, v, errs[i])
		}
	}
	if p := peak.Load(); p > aiParallel || p < 2 {
		t.Errorf("同时调用的批数应在 2 到 %d 之间：%d", aiParallel, p)
	}

	platform := errors.New("平台不可用")
	var calls atomic.Int32
	_, _, err = runBatches(context.Background(), 20, func(ctx context.Context, i int) (int, error) {
		calls.Add(1)
		if i == 0 {
			return 0, platform
		}
		<-ctx.Done()
		return 0, ctx.Err()
	})
	if !errors.Is(err, platform) {
		t.Fatalf("平台错误应返回给任务重试：%v", err)
	}
	if calls.Load() > aiParallel+1 {
		t.Errorf("出错后应停止发起新的批：调用了 %d 次", calls.Load())
	}
}

// 仿照北师大 808 真题结构自拟：Markdown 标记、题型标题折行、按专业分组、组内共用材料、题内引文。
const groupedPaper = `## 2024 某某大学 808 中国语言文学真题
### 一、名词解释（共 12 分，每题 6 分）
1. 意境
2. 典型
### 三、论述题（本题共分三组，请按专业分组答题，每组共 70 分，组内 2 小
题，每小题 35 分，回答非报考专业不得分）
**第一组：请选择文艺学、比较文学与世界文学专业的考生作答**
1. 什么是比较文学形象学？
2. 谈谈异国形象中「他者」与「自我」的关系。
**第二组：请选择中国民间文学专业的考生作答**
1. 请阅读以下材料，评述其中的观点。
 > "表演存在于表演者对观众承担展示自己交流能力的责任。"
 >
 > ——【美】理查德·鲍曼
2. 如何认识民间文学「传承人」的角色？
**第三组：请选择学科教学（语文）专业的考生作答**
夫君子之行，静以修身，俭以养德。非淡泊无以明志，非宁静无以致远。
1. 请翻译上面这段话。
2. 请谈谈你对文章主题内涵的理解。`

func TestSplitGroupsAndPassages(t *testing.T) {
	got := Split([]Page{{No: 1, Text: groupedPaper}})
	if len(got) != 8 {
		t.Fatalf("块数 %d：%+v", len(got), got)
	}
	// 题型标题去掉 ### 后识别出来，折行的说明接回标题，不当成材料
	if got[0].QType != "term" || got[0].Score != 6 || got[0].Year != 2024 || got[0].Group != "" || got[0].Passage != "" {
		t.Errorf("名词解释：%+v", got[0])
	}
	if !strings.Contains(got[2].Section, "回答非报考专业不得分") || got[2].Score != 35 || got[2].QType != "discussion" {
		t.Errorf("折行的题型标题应接回：%+v", got[2])
	}
	type want struct{ group, majors, text, passage string }
	wants := []want{
		{"第一组", "文艺学、比较文学与世界文学", "1. 什么是比较文学形象学？", ""},
		{"第一组", "文艺学、比较文学与世界文学", "2. 谈谈异国形象中「他者」与「自我」的关系。", ""},
		// 题内引文留在题干里（去掉 > 标记），分组标题不能并进上一题
		{"第二组", "中国民间文学", "1. 请阅读以下材料，评述其中的观点。\n\"表演存在于表演者对观众承担展示自己交流能力的责任。\"\n——【美】理查德·鲍曼", ""},
		{"第二组", "中国民间文学", "2. 如何认识民间文学「传承人」的角色？", ""},
		{"第三组", "学科教学（语文）", "1. 请翻译上面这段话。", "夫君子之行，静以修身，俭以养德。非淡泊无以明志，非宁静无以致远。"},
		{"第三组", "学科教学（语文）", "2. 请谈谈你对文章主题内涵的理解。", "夫君子之行，静以修身，俭以养德。非淡泊无以明志，非宁静无以致远。"},
	}
	for i, w := range wants {
		b := got[i+2]
		if b.Group != w.group || b.GroupMajors != w.majors || b.Text != w.text || b.Passage != w.passage {
			t.Errorf("第 %d 块：\n got %q %q %q %q\nwant %q %q %q %q", i+3, b.Group, b.GroupMajors, b.Text, b.Passage, w.group, w.majors, w.text, w.passage)
		}
	}
}

func TestSplitSectionPassage(t *testing.T) {
	got := Split([]Page{{No: 1, Text: `三、材料分析题（每题 20 分）
阅读下面的材料，回答问题：「文变染乎世情，兴废系乎时序。」这句话出自《文心雕龙·时序》，讨论文学与时代的关系。
1. 解释这句话的含义。
2. 结合文学史举例说明。
四、论述题
（本大题共 1 小题）
3. 论述文学的社会功能。`}})
	if len(got) != 3 {
		t.Fatalf("块数 %d：%+v", len(got), got)
	}
	if got[0].Passage == "" || got[1].Passage != got[0].Passage || !strings.HasPrefix(got[0].Passage, "阅读下面的材料") {
		t.Errorf("题型下的长材料应挂到这一题型的题上：%q %q", got[0].Passage, got[1].Passage)
	}
	if got[2].Passage != "" {
		t.Errorf("新题型开始后材料失效，短说明不算材料：%q", got[2].Passage)
	}
}

func TestIsGroupHead(t *testing.T) {
	for line, want := range map[string]bool{
		"第一组": true,
		"第二组：请选择古代文学专业的考生作答":      true,
		"第三组（供学科教学方向考生选答）":        true,
		"第二组数据表明，城市化进程中出现了大量新词新语": false,
	} {
		if got := isGroupHead(line); got != want {
			t.Errorf("%s：%v", line, got)
		}
	}
	if got := groupMajors("请选择中国现当代文学和儿童文学的考生作答"); got != "中国现当代文学和儿童文学" {
		t.Errorf("%q", got)
	}
}

func TestSplitLongSectionHead(t *testing.T) {
	got := Split([]Page{{No: 1, Text: `二、简答题（共 20 分，每题 10 分）
1. 简述得意忘言说
三、论述题（本题共分七组，请按专业分组答题，每组共 70 分，组内 2 小题，每小题 35 分，回答非报考专业不得分）
**第一组：请选择文艺学专业的考生作答**
1. 如何理解梁启超的小说观？`}})
	if len(got) != 2 || got[1].QType != "discussion" || got[1].Score != 35 || got[1].Group != "第一组" {
		t.Fatalf("同一行的长题型标题也要识别：%+v", got)
	}
}

func TestMatchMajors(t *testing.T) {
	for _, c := range []struct {
		mine, group string
		want        bool
	}{
		{"古代文学", "中国古代文学、中国古典文献学", true},
		{"学科教学（语文）", "学科教学（语文）", true},
		{"中国现当代文学专业", "中国现当代文学和儿童文学", true},
		{"语言学及应用语言学", "汉语言文字学", false},
		{"", "文艺学", false},
	} {
		if got := MatchMajors(c.mine, c.group); got != c.want {
			t.Errorf("%s / %s：%v", c.mine, c.group, got)
		}
	}
}

func TestPickGroup(t *testing.T) {
	row := func(id uint64, label, majors string) dbq.ListExamQuestionsByYearRow {
		return dbq.ListExamQuestionsByYearRow{ID: id, GroupLabel: sql.NullString{String: label, Valid: label != ""}, GroupMajors: sql.NullString{String: majors, Valid: majors != ""}}
	}
	qs := []dbq.ListExamQuestionsByYearRow{row(1, "", ""), row(2, "第一组", "文艺学"), row(3, "第二组", "中国古代文学"), row(4, "第二组", "中国古代文学")}
	got, note := pickGroup(qs, "古代文学")
	if len(got) != 3 || got[1].ID != 3 || note != "" {
		t.Errorf("按报考方向选组：%+v %q", got, note)
	}
	got, note = pickGroup(qs, "")
	if len(got) != 2 || got[1].ID != 2 || !strings.Contains(note, "第一组") {
		t.Errorf("没设方向时取第一组并说明：%+v %q", got, note)
	}
	if got, _ := pickGroup(qs[:1], ""); len(got) != 1 {
		t.Error("没有分组时原样返回")
	}
}
