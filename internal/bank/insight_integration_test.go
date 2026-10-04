package bank_test

import (
	"context"
	"testing"

	"peetraining-server/internal/apperr"
)

// 两年的真题：名词解释 2 题 × 10 分 + 简答 1 题 × 20 分，「意境」两年都考。
const twoYears = `2024 年某某大学中国语言文学基础考研真题
一、名词解释（每题 10 分）
1. 意境
2. 典型
二、简答题（每题 20 分）
3. 简述唐传奇的艺术成就。
2023 年某某大学中国语言文学基础考研真题
一、名词解释（每题 10 分）
1. 意境
2. 陌生化
二、简答题（每题 20 分）
3. 简述建安风骨的内涵。`

func (f *fx) importText(t *testing.T, uid, sid uint64, mode, title, text string) {
	t.Helper()
	ctx := context.Background()
	m, err := f.mat.CreatePasted(ctx, uid, sid, mode, title, text, true)
	if err != nil {
		t.Fatal(err)
	}
	job, err := f.imp.CreateJob(ctx, uid, sid, mode, []uint64{m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.imp.Confirm(ctx, uid, job.ID, nil); err != nil {
		t.Fatal(err)
	}
}

func TestExamProfile(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)

	// 只有 1 套真题：提示再导入
	f.importText(t, uid, sid, "question", "2024 真题", exam)
	p, err := f.bank.ExamProfile(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Ready || p.PaperCount != 1 || p.MinPapers != 2 || len(p.Basis) != 1 {
		t.Fatalf("1 套：%+v", p)
	}

	// 再导入两年的真题（2024 年同题去重后是同一年）→ 2 套，显示统计
	uid2, sid2 := f.user(t)
	f.importText(t, uid2, sid2, "question", "真题汇编", twoYears)
	p, err = f.bank.ExamProfile(ctx, uid2, sid2)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Ready || p.PaperCount != 2 || p.Years[0] != 2024 || p.StableYears != 2 || len(p.ChangedYears) != 0 {
		t.Fatalf("2 套：%+v", p.ExamProfile)
	}
	// 结构：名词解释 2 × 10 = 20，简答 1 × 20 = 20；建议用时：(2×4 + 1×15) 按 165 分钟等比例放大并取整到 5 分钟
	if len(p.Structure) != 2 || p.Structure[0].Count != 2 || p.Structure[0].Total != 20 || p.Structure[1].Total != 20 {
		t.Fatalf("结构：%+v", p.Structure)
	}
	if len(p.StructureTimes) != 2 || p.StructureTimes[0]+p.StructureTimes[1] != 165 || p.StructureTimes[0] != 55 {
		t.Fatalf("建议用时：%v", p.StructureTimes)
	}
	// 高频考点：意境考了 2 次
	if len(p.HighFreq) != 1 || p.HighFreq[0].Name != "意境" || p.HighFreq[0].ExamCount != 2 || p.HighFreqTotal != 5 {
		t.Fatalf("高频考点：%+v total=%d", p.HighFreq, p.HighFreqTotal)
	}
	// 板块分值占比合计 1
	sum := 0.0
	for _, s := range p.Sections {
		sum += s.Share
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("板块占比合计 %.3f：%+v", sum, p.Sections)
	}
	if len(p.StyleTags) < 2 {
		t.Fatalf("出题风格：%v", p.StyleTags)
	}
	// 风格标签缓存：真题没变不重新生成
	var key1, key2 string
	_ = f.db.QueryRow("SELECT exam_style_key FROM banks WHERE subject_id = ?", sid2).Scan(&key1)
	if _, err := f.bank.ExamProfile(ctx, uid2, sid2); err != nil {
		t.Fatal(err)
	}
	_ = f.db.QueryRow("SELECT exam_style_key FROM banks WHERE subject_id = ?", sid2).Scan(&key2)
	if key1 == "" || key1 != key2 {
		t.Fatal("风格标签应缓存")
	}
	if _, err := f.bank.ExamProfile(ctx, uid, sid2); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("别人的专业课应 404")
	}
}

func TestGraphAndRelations(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	g, err := f.bank.Graph(ctx, uid, sid, "all", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) < 4 || len(g.Edges) == 0 || len(g.Sections) < 2 {
		t.Fatalf("图谱：%d 节点 %d 关联", len(g.Nodes), len(g.Edges))
	}
	types := map[string]int{}
	for _, e := range g.Edges {
		types[e.Type]++
		if e.Source >= e.Target {
			t.Fatal("关联两端按 ID 从小到大存")
		}
	}
	if types["sibling"] == 0 || types["contrast"] == 0 {
		t.Fatalf("应有同章并列与易混对比：%v", types)
	}
	// 只生成一次：删光后不再自动生成
	for _, e := range g.Edges {
		if err := f.bank.DeleteRelation(ctx, uid, e.ID); err != nil {
			t.Fatal(err)
		}
	}
	if g, _ = f.bank.Graph(ctx, uid, sid, "all", nil); len(g.Edges) != 0 {
		t.Fatal("用户删光后不应再生成")
	}
	// 筛选：真题考过、按板块
	exam, _ := f.bank.Graph(ctx, uid, sid, "exam", nil)
	for _, n := range exam.Nodes {
		if n.ExamCount == 0 {
			t.Fatal("真题考过筛选")
		}
	}
	roots, _, _ := f.bank.Tree(ctx, uid, sid, "all")
	sec := find(roots, "第一章 文学理论").ID
	only, _ := f.bank.Graph(ctx, uid, sid, "all", &sec)
	if len(only.Nodes) != 2 {
		t.Fatalf("按板块：%d", len(only.Nodes))
	}
	// 在卡片里手动关联
	a, b := notesKP(roots, "意象").ID, notesKP(roots, "意境").ID
	r, err := f.bank.CreateRelation(ctx, uid, b, a, "contrast")
	if err != nil || r.Source != a || r.Target != b || r.Origin != "user_confirmed" {
		t.Fatalf("手动关联：%v %+v", err, r)
	}
	if _, err := f.bank.CreateRelation(ctx, uid, a, a, "related"); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("不能和自己关联")
	}
	other, otherSubject := f.user(t)
	f.imported(t, other, otherSubject)
	otherRoots, _, _ := f.bank.Tree(ctx, other, otherSubject, "all")
	if _, err := f.bank.CreateRelation(ctx, uid, a, notesKP(otherRoots, "意境").ID, "related"); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("不能关联别人的知识点")
	}
	if _, err := f.bank.Graph(ctx, other, sid, "all", nil); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("别人的图谱 404")
	}
	if err := f.bank.DeleteRelation(ctx, other, r.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("别人的关联 404")
	}
	if _, err := f.bank.CreateRelation(ctx, other, a, b, "related"); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("别人的知识点 404")
	}
}

const essayNotes = `2024 年 908 写作考研真题
2024 年作文题：以「守正与创新」为题写一篇议论文（不少于 800 字）
评分细则
立意（40 分）：切题、观点明确
结构（30 分）：层次清晰
内容（50 分）：论据充实
语言（30 分）：通顺准确
方法：开头点题，第一段直接亮出中心论点。
素材【创新】屠呦呦从古籍中获得灵感，提取青蒿素。
范文《守正方能出新》
守正是根基，创新是动力。
传统文化的生命力在于不断创造性转化。
唯有守正，创新才不会迷失方向。`

func TestEssayKnowledgeBase(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)

	// 还没导入：用通用五维度标准
	kb, err := f.bank.EssayKnowledgeBase(ctx, uid, sid)
	if err != nil || kb.Rubric == nil || kb.Rubric.Source != "generic" || kb.Rubric.FullScore != 150 || len(kb.Methods) != 0 {
		t.Fatalf("空：%v %+v", err, kb.Rubric)
	}
	f.importText(t, uid, sid, "essay", "908 笔记", essayNotes)
	kb, err = f.bank.EssayKnowledgeBase(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if kb.Rubric.Source != "user_material" || kb.Rubric.SourceRef == nil || len(kb.Methods) != 1 || kb.Methods[0].FileName.String != "908 笔记" ||
		len(kb.Materials) != 1 || len(kb.Models) != 1 || len(kb.Topics) != 1 || kb.Topics[0].RequiredWords.Int16 != 800 {
		t.Fatalf("知识库：%+v", kb)
	}
	if err := f.bank.SetEssayMaterialFavorite(ctx, uid, kb.Materials[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.bank.SetEssayMaterialFavorite(ctx, uid, kb.Materials[0].ID, true); err != nil {
		t.Fatal("重复收藏不应报错")
	}
	if kb, _ = f.bank.EssayKnowledgeBase(ctx, uid, sid); !kb.Materials[0].Favorite {
		t.Fatal("收藏")
	}
	other, _ := f.user(t)
	if _, err := f.bank.EssayKnowledgeBase(ctx, other, sid); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("别人的作文知识库 404")
	}
	if err := f.bank.SetEssayMaterialFavorite(ctx, other, kb.Materials[0].ID, false); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("别人的素材 404")
	}
}
