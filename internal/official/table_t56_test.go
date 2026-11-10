package official

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"peetraining-server/internal/dbq"
)

func TestSplitKPNames(t *testing.T) {
	got := SplitKPNames(" 六书；单义词、多义词与反义词;六书\n同音词 ")
	want := []string{"六书", "单义词、多义词与反义词", "同音词"}
	if !slices.Equal(got, want) {
		t.Fatalf("拆知识点：%v，想要 %v", got, want)
	}
	if SplitKPNames("") != nil {
		t.Fatal("空格子不应有知识点")
	}
}

func parseRows(t *testing.T, csv string) []dbq.InsertOfficialImportItemParams {
	t.Helper()
	out, err := New(Deps{}).parseTable(context.Background(), 1, csv, false)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// T56：题目表的解析、出处、答案来源、多个知识点。
func TestParseTableQuestionColumns(t *testing.T) {
	csv := string(rune(0xFEFF)) + "年份,题号,题型,分值,题干,参考答案,解析,采分点,知识点,出处,答案来源,来源类型\n" +
		"2026,一-1,简答,10,题一,\"1. 要点甲\n2. 要点乙\",解析一,\"要点甲（6分）\n要点乙（4分）\",六书；古注的阅读,王宁《古代汉语》六书,AI生成,真题\n" +
		"2026,一-2,简答,10,题二,答案二,,要点（10分）,善本,,,真题\n"
	rows := parseRows(t, csv)
	if len(rows) != 2 {
		t.Fatalf("应解析出 2 题，实际 %d", len(rows))
	}
	var p1, p2 QuestionPayload
	_ = json.Unmarshal(rows[0].Payload, &p1)
	_ = json.Unmarshal(rows[1].Payload, &p2)
	if !p1.AnswerAI || p1.Analysis != "解析一" || p1.Reference != "王宁《古代汉语》六书" || !slices.Equal(p1.KPNames, []string{"六书", "古注的阅读"}) {
		t.Fatalf("第一题：%+v", p1)
	}
	if p2.AnswerAI || p2.Reference != "" {
		t.Fatalf("第二题不填答案来源应是团队答案、没有出处：%+v", p2)
	}
	if !strings.Contains(rows[0].SourceText.String, "出处：王宁") {
		t.Fatal("核对区原文里仍带出处")
	}
}

func TestParseTableBadAnswerOrigin(t *testing.T) {
	csv := "题型,分值,题干,参考答案,答案来源\n简答,10,题,答,机器\n"
	if _, err := New(Deps{}).parseTable(context.Background(), 1, csv, false); err == nil || !strings.Contains(err.Error(), "答案来源") {
		t.Fatalf("答案来源写错应报错：%v", err)
	}
}

// T56：表头有板块、章节、知识点而没有题干的是知识点表。
func TestParseKPTable(t *testing.T) {
	csv := "板块,章节,知识点,原文表述,采分点,出处,关联题目\n" +
		"古代汉语,汉字,六书,六书是……,\"名称（5分）\n评价（5分）\",王宁《古代汉语》,一-1\n" +
		"古典文献学,版本,善本,,,,\n"
	rows := parseRows(t, csv)
	if len(rows) != 2 || rows[0].ItemType != dbq.OfficialImportItemsItemTypeKnowledgePoint {
		t.Fatalf("应解析出 2 个知识点：%+v", rows)
	}
	var k KPPayload
	_ = json.Unmarshal(rows[0].Payload, &k)
	if k.Section != "古代汉语" || k.Chapter != "汉字" || k.Name != "六书" || k.Reference != "王宁《古代汉语》" || len(k.Rubric) != 2 {
		t.Fatalf("知识点：%+v", k)
	}
	if rows[0].Confidence != dbq.OfficialImportItemsConfidenceHigh || rows[1].Confidence != dbq.OfficialImportItemsConfidenceLow {
		t.Fatal("没有采分点、原文表述的知识点置信度应为 low")
	}
	dup := "板块,章节,知识点\n甲,乙,六书\n甲,乙,六书\n"
	if _, err := New(Deps{}).parseTable(context.Background(), 1, dup, false); err == nil || !strings.Contains(err.Error(), "重名") {
		t.Fatalf("同一张表重名应报错：%v", err)
	}
	noChapter := "板块,章节,知识点\n甲,,六书\n"
	if _, err := New(Deps{}).parseTable(context.Background(), 1, noChapter, false); err == nil {
		t.Fatal("缺章节应报错")
	}
}

func TestQuestionKPCheck(t *testing.T) {
	q := func(stem string, kps ...string) dbq.OfficialDraft {
		b, _ := json.Marshal(QuestionPayload{QType: "short_answer", Stem: stem, KPNames: kps})
		return dbq.OfficialDraft{EntityType: dbq.OfficialDraftsEntityTypeQuestion, ChangeType: dbq.OfficialDraftsChangeTypeAdd, Payload: b}
	}
	kp := func(name string) dbq.OfficialDraft {
		b, _ := json.Marshal(KPPayload{Section: "甲", Chapter: "乙", Name: name, Rubric: []Point{{Content: "要点", Score: 10}}})
		return dbq.OfficialDraft{EntityType: dbq.OfficialDraftsEntityTypeKnowledgePoint, ChangeType: dbq.OfficialDraftsChangeTypeAdd, Payload: b}
	}
	drafts := []dbq.OfficialDraft{q("题一", "六书", "善本"), q("题二", "同音词"), kp("善本")}
	got := questionKPCheck(drafts, map[string]bool{"六书": true})
	if len(got) != 1 || !strings.Contains(got[0], "同音词") {
		t.Fatalf("应只报「同音词」不存在：%v", got)
	}
	checks := checksFor(drafts, map[string]bool{"六书": true})
	if checks[len(checks)-1].Passed {
		t.Fatal("发布前检查应不通过")
	}
}

func TestKeepFields(t *testing.T) {
	orig := json.RawMessage(`{"stem":"旧","reference":"出处","analysis":"解析"}`)
	got := keepFields(json.RawMessage(`{"stem":"新"}`), orig, "reference", "analysis")
	var m map[string]string
	_ = json.Unmarshal(got, &m)
	if m["stem"] != "新" || m["reference"] != "出处" || m["analysis"] != "解析" {
		t.Fatalf("修改后应沿用出处、解析：%s", got)
	}
	got = keepFields(json.RawMessage(`{"stem":"新","reference":""}`), orig, "reference")
	_ = json.Unmarshal(got, &m)
	if m["reference"] != "" {
		t.Fatal("编辑明确清空出处时不应补回")
	}
}

// 解析、出处为空时哈希与 T56 以前一致，已同步的副本不会被判为有变化。
func TestHashCompatible(t *testing.T) {
	origin := dbq.NullQuestionsAnswerOrigin{}
	old := hashOf("short_answer", "题", "答", "要点")
	if questionHashOf("short_answer", "题", "答", "要点", origin, "", "", "") != old {
		t.Fatal("没有解析、出处时题目哈希应不变")
	}
	if questionHashOf("short_answer", "题", "答", "要点", origin, "", "", "出处") == old {
		t.Fatal("有出处时哈希应变化")
	}
	if kpHashOf("六书", "原文", "要点", "") != hashOf("六书", "原文", "要点") {
		t.Fatal("没有出处时知识点哈希应不变")
	}
}
