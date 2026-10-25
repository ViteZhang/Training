package bank

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/rules"
)

// kpIndex 是题库全部节点：按 ID 查节点、找所在板块。
type kpIndex struct {
	byID map[uint64]dbq.ListBankKPsFullRow
	rows []dbq.ListBankKPsFullRow
}

func (s *Service) kpIndex(ctx context.Context, userID, bankID uint64) (kpIndex, error) {
	rows, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: bankID, Owner: owner(userID)})
	if err != nil {
		return kpIndex{}, err
	}
	ix := kpIndex{byID: map[uint64]dbq.ListBankKPsFullRow{}, rows: rows}
	for _, r := range rows {
		ix.byID[r.ID] = r
	}
	return ix, nil
}

// section 返回节点所在的板块（找不到返回 0）。
func (ix kpIndex) section(id uint64) uint64 {
	for i := 0; i < 5; i++ {
		k, ok := ix.byID[id]
		if !ok {
			return 0
		}
		if k.Level == dbq.KnowledgePointsLevelSection {
			return k.ID
		}
		if !k.ParentID.Valid {
			return 0
		}
		id = uint64(k.ParentID.Int64)
	}
	return 0
}

func (ix kpIndex) path(id uint64) []string {
	var p []string
	k, ok := ix.byID[id]
	for i := 0; ok && k.ParentID.Valid && i < 5; i++ {
		k, ok = ix.byID[uint64(k.ParentID.Int64)]
		if ok {
			p = append([]string{k.Name}, p...)
		}
	}
	return p
}

func m(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// ExamProfile 是考情分析（3.8）。
type ExamProfile struct {
	rules.ExamProfile
	MinPapers      int
	StructureTimes []int
	TotalMinutes   int
	CheckMinutes   int
	Sections       []SectionShare
	HighFreq       []HighFreqKP
	HighFreqTotal  int
	Missing        []SectionShare
	StyleTags      []string
	Basis          []Source
	// GroupNote：论述题按专业分组时只统计一组，写明是哪一组。
	GroupNote string
	// PendingQuestions 是还没确认入库的题数，PendingJobID 是其中最近的导入任务（跳到核对页）。
	PendingQuestions int
	PendingJobID     uint64
}

// SectionShare 是板块的真题分值占比与掌握度。
type SectionShare struct {
	ID      uint64
	Name    string
	Share   float64
	Mastery float64
	KPCount int
}

// HighFreqKP 是高频考点。
type HighFreqKP struct {
	KPID      uint64
	Name      string
	Path      []string
	ExamCount int
	State     string
}

// ExamProfile 统计考情（PRD 11.11）：数字全部来自 rules.BuildExamProfile 与 rules.SuggestedTimes，出题风格标签由 AI 生成并缓存。
func (s *Service) ExamProfile(ctx context.Context, userID, subjectID uint64) (ExamProfile, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return ExamProfile{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return ExamProfile{}, err
	}
	st, err := s.examStats(ctx, userID, b.BankID, p)
	if err != nil {
		return ExamProfile{}, err
	}
	prof, ix, qs, materials := st.Profile, st.index, st.questions, st.materials
	out := ExamProfile{ExamProfile: prof, MinPapers: p.ExamProfile.MinPapers, TotalMinutes: p.PaperTime.DefaultTotalMinutes, CheckMinutes: p.PaperTime.CheckMinutes,
		GroupNote: st.GroupNote}
	// 真题不够时说清为什么：有题还没确认入库（能补全年份的题由 importer.Repairable 另算）。
	pend, err := s.q.CountPendingImportQuestions(ctx, dbq.CountPendingImportQuestionsParams{BankID: b.BankID, OwnerUserID: userID})
	if err != nil {
		return ExamProfile{}, err
	}
	out.PendingQuestions, out.PendingJobID = int(pend.N), uint64(max(pend.JobID, 0))
	for id := range materials {
		if mt, err := s.q.GetMaterial(ctx, dbq.GetMaterialParams{ID: id, OwnerUserID: userID}); err == nil {
			out.Basis = append(out.Basis, Source{MaterialID: id, FileName: mt.FileName})
		}
	}
	sort.Slice(out.Basis, func(i, j int) bool { return out.Basis[i].MaterialID < out.Basis[j].MaterialID })
	if !prof.Ready {
		return out, nil
	}

	// 题型建议用时（PRD 11.9，与选择模式、时间报告同一套数字）。
	counts := make([]rules.SectionCount, len(prof.Structure))
	for i, st := range prof.Structure {
		counts[i] = rules.SectionCount{QType: st.QType, Count: st.Count}
	}
	for _, t := range rules.SuggestedTimes(counts, p.PaperTime.DefaultTotalMinutes, p.PaperTime) {
		out.StructureTimes = append(out.StructureTimes, t.Minutes)
	}

	// 板块占比、掌握度与缺资料提醒。
	out.Sections = st.sectionShares(p)
	var coverage []rules.SectionCoverage
	for _, sh := range out.Sections {
		coverage = append(coverage, rules.SectionCoverage{SectionID: int64(sh.ID), Share: sh.Share, KPCount: sh.KPCount, Mastery: sh.Mastery})
	}
	missing := map[int64]bool{}
	for _, id := range rules.MissingMaterialSections(coverage, p.ExamProfile) {
		missing[id] = true
	}
	for _, sh := range out.Sections {
		if missing[int64(sh.ID)] {
			out.Missing = append(out.Missing, sh)
		}
	}

	// 高频考点（真题出现 ≥ 2 次），列表只给前 20 个，另给真题考过的知识点总数。
	for _, hf := range prof.HighFreq {
		k, ok := ix.byID[uint64(hf.KPID)]
		if !ok || len(out.HighFreq) >= 20 {
			continue
		}
		out.HighFreq = append(out.HighFreq, HighFreqKP{KPID: k.ID, Name: k.Name, Path: ix.path(k.ID), ExamCount: hf.Count, State: string(k.State)})
	}
	for _, k := range ix.rows {
		if k.Level == dbq.KnowledgePointsLevelPoint && k.ExamCount > 0 {
			out.HighFreqTotal++
		}
	}
	out.StyleTags, err = s.examStyle(ctx, userID, b, qs)
	return out, err
}

// sectionShares 是各板块的真题分值占比与掌握度（3.8 与提分看板共用），按占比从高到低。
func (e ExamStats) sectionShares(p rules.Params) []SectionShare {
	sectionKPs := map[uint64][]float64{}
	for _, k := range e.index.rows {
		if k.Level == dbq.KnowledgePointsLevelPoint {
			sec := e.index.section(k.ID)
			sectionKPs[sec] = append(sectionKPs[sec], m(k.M))
		}
	}
	var out []SectionShare
	for _, k := range e.index.rows {
		if k.Level != dbq.KnowledgePointsLevelSection {
			continue
		}
		avg, _ := rules.SectionMastery(sectionKPs[k.ID], p.MasteryState)
		out = append(out, SectionShare{ID: k.ID, Name: k.Name, Share: e.Profile.SectionShares[int64(k.ID)], Mastery: avg, KPCount: len(sectionKPs[k.ID])})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Share > out[j].Share })
	return out
}

// Sections 返回一门课各板块的掌握度与真题分值占比（6.2 提分看板，与 3.8 一致）；ready 为 false 时真题不足，占比不展示。
func (s *Service) Sections(ctx context.Context, userID, subjectID uint64) ([]SectionShare, bool, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return nil, false, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return nil, false, err
	}
	st, err := s.examStats(ctx, userID, b.BankID, p)
	if err != nil {
		return nil, false, err
	}
	return st.sectionShares(p), st.Profile.Ready, nil
}

// ExamStats 是一门课的真题统计（PRD 11.11），供考情分析与今日计划共用。
type ExamStats struct {
	Profile rules.ExamProfile
	// GroupNote 不为空时，论述题按专业分组、只统计了一组，界面写明是哪一组。
	GroupNote string
	index     kpIndex
	questions []dbq.ListBankExamQuestionsRow
	materials map[uint64]bool
}

// Section 返回知识点所在的板块（找不到返回 0）。
func (e ExamStats) Section(kpID uint64) uint64 { return e.index.section(kpID) }

// SectionCount 是题库里板块的个数。
func (e ExamStats) SectionCount() int {
	n := 0
	for _, k := range e.index.rows {
		if k.Level == dbq.KnowledgePointsLevelSection {
			n++
		}
	}
	return n
}

// Stats 返回题库的真题统计（不调用 AI）。
func (s *Service) Stats(ctx context.Context, userID, bankID uint64) (ExamStats, error) {
	p, err := s.params.Rules(ctx)
	if err != nil {
		return ExamStats{}, err
	}
	return s.examStats(ctx, userID, bankID, p)
}

func (s *Service) examStats(ctx context.Context, userID, bankID uint64, p rules.Params) (ExamStats, error) {
	all, err := s.q.ListBankExamQuestions(ctx, dbq.ListBankExamQuestionsParams{BankID: bankID, OwnerUserID: owner(userID)})
	if err != nil {
		return ExamStats{}, err
	}
	mine, err := s.q.GetBankMyMajors(ctx, dbq.GetBankMyMajorsParams{ID: bankID, OwnerUserID: owner(userID), OwnerUserID_2: userID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ExamStats{}, err
	}
	qs, groupNote := pickExamGroups(all, mine.String)
	links, err := s.q.ListBankQuestionKPLinks(ctx, dbq.ListBankQuestionKPLinksParams{BankID: bankID, OwnerUserID: owner(userID)})
	if err != nil {
		return ExamStats{}, err
	}
	ix, err := s.kpIndex(ctx, userID, bankID)
	if err != nil {
		return ExamStats{}, err
	}
	kpsOf := map[uint64][]int64{}
	for _, l := range links {
		kpsOf[l.QuestionID] = append(kpsOf[l.QuestionID], int64(l.KpID))
	}
	in := make([]rules.ExamQuestion, 0, len(qs))
	materials := map[uint64]bool{}
	for _, q := range qs {
		eq := rules.ExamQuestion{QType: rules.QType(q.Qtype), Recollection: q.IsRecollection, KPIDs: kpsOf[q.ID]}
		if q.ExamYear.Valid {
			eq.Year = int(q.ExamYear.Int16)
		}
		if sc := parseScore(q.Score); sc != nil {
			eq.Score = *sc
		}
		if len(eq.KPIDs) > 0 {
			eq.SectionID = int64(ix.section(uint64(eq.KPIDs[0])))
		}
		in = append(in, eq)
		if q.SourceMaterialID.Valid && eq.Year > 0 && eq.Score > 0 && !eq.Recollection {
			materials[uint64(q.SourceMaterialID.Int64)] = true
		}
	}
	return ExamStats{Profile: rules.BuildExamProfile(in, p.ExamProfile), GroupNote: groupNote, index: ix, questions: qs, materials: materials}, nil
}

// pickExamGroups 去掉按专业分组的论述题里不计分的组（回答非报考专业不得分）：每年只留一组，
// 选法与真题卷相同（importer.ChooseGroup），否则七组论述题全算进去，题型结构和总分都会虚高。
// 返回留下的题和界面上要写明的说明。
func pickExamGroups(qs []dbq.ListBankExamQuestionsRow, mine string) ([]dbq.ListBankExamQuestionsRow, string) {
	labels := map[int16][]string{}
	majors := map[int16]map[string]string{}
	for _, q := range qs {
		l := q.GroupLabel.String
		if l == "" || !q.ExamYear.Valid {
			continue
		}
		y := q.ExamYear.Int16
		if majors[y] == nil {
			majors[y] = map[string]string{}
		}
		if !slices.Contains(labels[y], l) {
			labels[y] = append(labels[y], l)
			majors[y][l] = q.GroupMajors.String
		}
	}
	chosen := map[int16]string{}
	var picked []string
	auto := false
	for y, ls := range labels {
		c, a := importer.ChooseGroup(ls, majors[y], mine)
		if c == "" {
			continue
		}
		chosen[y] = c
		auto = auto || a
		if !slices.Contains(picked, c) {
			picked = append(picked, c)
		}
	}
	if len(chosen) == 0 {
		return qs, ""
	}
	out := make([]dbq.ListBankExamQuestionsRow, 0, len(qs))
	for _, q := range qs {
		if c, ok := chosen[q.ExamYear.Int16]; ok && q.ExamYear.Valid && q.GroupLabel.String != "" && q.GroupLabel.String != c {
			continue
		}
		out = append(out, q)
	}
	sort.Strings(picked)
	groups := "「" + strings.Join(picked, "」「") + "」"
	if auto {
		return out, "论述题按专业分组，还没选报考方向，暂按" + groups + "统计；导入真题时在核对页选报考方向后按你的方向统计"
	}
	return out, "论述题按专业分组，只统计你报考方向的" + groups
}

// examStyle 取出题风格标签：真题没变用缓存，变了重新生成；生成失败返回空，不影响其他统计。
func (s *Service) examStyle(ctx context.Context, userID uint64, b dbq.GetSubjectBankRow, qs []dbq.ListBankExamQuestionsRow) ([]string, error) {
	h := sha256.New()
	var items []ai.StyleItem
	for _, q := range qs {
		if !q.ExamYear.Valid || q.IsRecollection {
			continue
		}
		h.Write([]byte(strconv.FormatUint(q.ID, 10) + ":" + q.Stem + "\n"))
		if len(items) < 120 {
			items = append(items, ai.StyleItem{Year: int(q.ExamYear.Int16), QType: string(q.Qtype), Stem: q.Stem})
		}
	}
	key := hex.EncodeToString(h.Sum(nil))
	cur, err := s.q.GetBankInsight(ctx, dbq.GetBankInsightParams{ID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return nil, err
	}
	var tags []string
	if cur.ExamStyleKey.String == key && json.Unmarshal(cur.ExamStyle, &tags) == nil && len(tags) > 0 {
		return tags, nil
	}
	out, _, err := ai.ExamStyle.Run(ctx, s.ai, userID, ai.StyleIn{Subject: b.Name, Questions: items})
	if apperr.IsKind(err, apperr.AIFailed) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(out.Tags)
	err = s.q.SetBankExamStyle(ctx, dbq.SetBankExamStyleParams{ExamStyle: raw, ExamStyleKey: sql.NullString{String: key, Valid: true}, ID: b.BankID, OwnerUserID: owner(userID)})
	return out.Tags, err
}

// GraphNode 是图谱节点：颜色 = 掌握状态，大小 = 真题次数。
type GraphNode struct {
	ID        uint64
	Name      string
	SectionID uint64
	State     string
	M         float64
	ExamCount int
}

// Relation 是知识关联。
type Relation struct {
	ID     uint64
	Source uint64
	Target uint64
	Type   string
	Origin string
}

// Graph 是知识图谱（3.9）。
type Graph struct {
	Sections []SectionShare
	Nodes    []GraphNode
	Edges    []Relation
}

// maxGraphNodes 控制一次返回的节点数，保证手机上缩放流畅（T14 验收：200 个节点内流畅）。
const maxGraphNodes = 200

// Graph 返回知识图谱。filter：all / weak（掌握分 < 40）/ exam（真题考过）；可只看一个板块。
// 第一次打开时生成关联：同章并列、同题出现由规则生成，易混对比由 AI 找；之后由用户在卡片里调整。
func (s *Service) Graph(ctx context.Context, userID, subjectID uint64, filter string, sectionID *uint64) (Graph, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Graph{}, err
	}
	ix, err := s.kpIndex(ctx, userID, b.BankID)
	if err != nil {
		return Graph{}, err
	}
	if err := s.ensureRelations(ctx, userID, b, ix); err != nil {
		return Graph{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Graph{}, err
	}
	var g Graph
	for _, k := range ix.rows {
		if k.Level == dbq.KnowledgePointsLevelSection {
			g.Sections = append(g.Sections, SectionShare{ID: k.ID, Name: k.Name})
		}
	}
	for _, k := range ix.rows {
		if k.Level != dbq.KnowledgePointsLevelPoint {
			continue
		}
		sec := ix.section(k.ID)
		switch {
		case sectionID != nil && sec != *sectionID,
			filter == "weak" && m(k.M) >= p.MasteryState.WeakSectionAvg,
			filter == "exam" && k.ExamCount == 0:
			continue
		}
		g.Nodes = append(g.Nodes, GraphNode{ID: k.ID, Name: k.Name, SectionID: sec, State: string(k.State), M: m(k.M), ExamCount: int(k.ExamCount)})
	}
	if len(g.Nodes) > maxGraphNodes {
		// 节点太多时优先保留真题考过多的、掌握差的。
		sort.SliceStable(g.Nodes, func(i, j int) bool {
			if g.Nodes[i].ExamCount != g.Nodes[j].ExamCount {
				return g.Nodes[i].ExamCount > g.Nodes[j].ExamCount
			}
			return g.Nodes[i].M < g.Nodes[j].M
		})
		g.Nodes = g.Nodes[:maxGraphNodes]
	}
	in := map[uint64]bool{}
	for _, n := range g.Nodes {
		in[n.ID] = true
	}
	rels, err := s.q.ListKPRelations(ctx, dbq.ListKPRelationsParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return Graph{}, err
	}
	for _, r := range rels {
		if in[r.KpAID] && in[r.KpBID] {
			g.Edges = append(g.Edges, toRelation(r))
		}
	}
	return g, nil
}

func toRelation(r dbq.KpRelation) Relation {
	return Relation{ID: r.ID, Source: r.KpAID, Target: r.KpBID, Type: string(r.RelationType), Origin: string(r.Origin)}
}

// ensureRelations 第一次打开图谱时生成关联（只生成一次，用户删光后不再自动生成）。
func (s *Service) ensureRelations(ctx context.Context, userID uint64, b dbq.GetSubjectBankRow, ix kpIndex) error {
	cur, err := s.q.GetBankInsight(ctx, dbq.GetBankInsightParams{ID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil || cur.RelationsGeneratedAt.Valid {
		return err
	}
	type pair struct{ a, b uint64 }
	kinds := map[pair]dbq.KpRelationsRelationType{}
	add := func(x, y uint64, t dbq.KpRelationsRelationType) {
		if x == y {
			return
		}
		if x > y {
			x, y = y, x
		}
		if _, ok := kinds[pair{x, y}]; !ok || t == dbq.KpRelationsRelationTypeContrast {
			kinds[pair{x, y}] = t
		}
	}
	// 同章并列：同一章节下相邻的知识点（按顺序串起来，避免一章内两两相连太密）。
	byParent := map[int64][]uint64{}
	var points []ai.RelatePKP
	idOf := map[int]uint64{}
	for _, k := range ix.rows {
		if k.Level != dbq.KnowledgePointsLevelPoint {
			continue
		}
		byParent[k.ParentID.Int64] = append(byParent[k.ParentID.Int64], k.ID)
		if len(points) < 300 {
			idOf[len(points)+1] = k.ID
			points = append(points, ai.RelatePKP{ID: len(points) + 1, Name: k.Name, Path: ix.path(k.ID)})
		}
	}
	for _, ids := range byParent {
		for i := 0; i+1 < len(ids); i++ {
			add(ids[i], ids[i+1], dbq.KpRelationsRelationTypeSibling)
		}
	}
	// 同题出现：一道题关联的多个知识点。
	links, err := s.q.ListBankQuestionKPLinks(ctx, dbq.ListBankQuestionKPLinksParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return err
	}
	byQ := map[uint64][]uint64{}
	for _, l := range links {
		byQ[l.QuestionID] = append(byQ[l.QuestionID], l.KpID)
	}
	for _, ks := range byQ {
		for i := 0; i < len(ks); i++ {
			for j := i + 1; j < len(ks); j++ {
				add(ks[i], ks[j], dbq.KpRelationsRelationTypeRelated)
			}
		}
	}
	// 易混对比：AI。
	if len(points) >= 2 {
		out, _, err := ai.Relate.Run(ctx, s.ai, userID, ai.RelateIn{Subject: b.Name, Points: points})
		switch {
		case err == nil:
			for _, c := range out.Contrasts {
				add(idOf[c.A], idOf[c.B], dbq.KpRelationsRelationTypeContrast)
			}
		case !apperr.IsKind(err, apperr.AIFailed):
			return err
		}
	}
	for pr, t := range kinds {
		if _, err := s.q.InsertKPRelation(ctx, dbq.InsertKPRelationParams{OwnerUserID: owner(userID), BankID: b.BankID, KpAID: pr.a, KpBID: pr.b,
			RelationType: t, Origin: dbq.KpRelationsOriginAiGenerated}); err != nil {
			return err
		}
	}
	return s.q.MarkRelationsGenerated(ctx, dbq.MarkRelationsGeneratedParams{RelationsGeneratedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: b.BankID, OwnerUserID: owner(userID)})
}

// CreateRelation 在知识点卡片里添加关联；两个知识点必须在同一个题库里。
func (s *Service) CreateRelation(ctx context.Context, userID, kpID, targetID uint64, kind string) (Relation, error) {
	if kpID == targetID {
		return Relation{}, apperr.New(apperr.BadRequest, "不能和自己关联")
	}
	t := dbq.KpRelationsRelationType(kind)
	if !t.Valid() {
		return Relation{}, apperr.New(apperr.BadRequest, "关联类型不正确")
	}
	a, err := s.kp(ctx, userID, kpID)
	if err != nil {
		return Relation{}, err
	}
	bk, err := s.kp(ctx, userID, targetID)
	if err != nil || bk.BankID != a.BankID {
		return Relation{}, apperr.New(apperr.BadRequest, "要关联的知识点不存在").With("reason", "kp_not_found")
	}
	x, y := kpID, targetID
	if x > y {
		x, y = y, x
	}
	id, err := s.q.InsertKPRelation(ctx, dbq.InsertKPRelationParams{OwnerUserID: owner(userID), BankID: a.BankID, KpAID: x, KpBID: y, RelationType: t, Origin: dbq.KpRelationsOriginUserConfirmed})
	if err != nil {
		return Relation{}, err
	}
	r, err := s.q.GetKPRelation(ctx, dbq.GetKPRelationParams{ID: uint64(id), OwnerUserID: owner(userID)})
	return toRelation(r), err
}

// DeleteRelation 删除关联；不是自己的返回 404。
func (s *Service) DeleteRelation(ctx context.Context, userID, id uint64) error {
	n, err := s.q.DeleteKPRelation(ctx, dbq.DeleteKPRelationParams{ID: id, OwnerUserID: owner(userID)})
	if err == nil && n == 0 {
		return apperr.NotFoundErr()
	}
	return err
}

// EssayKB 是作文知识库（3.10）。
type EssayKB struct {
	Rubric    *EssayRubric
	Methods   []dbq.ListWritingMethodsRow
	Materials []dbq.ListEssayMaterialsKBRow
	Models    []dbq.ListModelEssaysKBRow
	Topics    []dbq.ListEssayTopicsKBRow
}

// EssayRubric 是当前使用的评分标准。
type EssayRubric struct {
	ID         uint64
	Name       string
	FullScore  float64
	Source     string
	SourceRef  *Source
	Dimensions json.RawMessage
}

// EssayKnowledgeBase 返回作文知识库：写作方法（带掌握状态）、素材（收藏的在前）、范文（按作文题归类、结构拆解）、作文题与当前评分标准。
func (s *Service) EssayKnowledgeBase(ctx context.Context, userID, subjectID uint64) (EssayKB, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return EssayKB{}, err
	}
	o := owner(userID)
	var kb EssayKB
	if kb.Methods, err = s.q.ListWritingMethods(ctx, dbq.ListWritingMethodsParams{BankID: b.BankID, OwnerUserID: o}); err != nil {
		return kb, err
	}
	if kb.Materials, err = s.q.ListEssayMaterialsKB(ctx, dbq.ListEssayMaterialsKBParams{BankID: b.BankID, OwnerUserID: o}); err != nil {
		return kb, err
	}
	if kb.Models, err = s.q.ListModelEssaysKB(ctx, dbq.ListModelEssaysKBParams{BankID: b.BankID, OwnerUserID: o}); err != nil {
		return kb, err
	}
	if kb.Topics, err = s.q.ListEssayTopicsKB(ctx, dbq.ListEssayTopicsKBParams{BankID: b.BankID, OwnerUserID: o}); err != nil {
		return kb, err
	}
	r, err := s.q.GetActiveEssayRubric(ctx, dbq.GetActiveEssayRubricParams{UserID: o, SubjectID: sql.NullInt64{Int64: int64(subjectID), Valid: true}})
	switch {
	case err == nil:
		kb.Rubric = &EssayRubric{ID: r.ID, Name: r.Name, FullScore: m(r.FullScore), Source: string(r.Source), Dimensions: r.Dimensions}
		if r.SourceMaterialID.Valid {
			kb.Rubric.SourceRef = &Source{MaterialID: uint64(r.SourceMaterialID.Int64), FileName: r.FileName.String, Page: int(r.SourcePage.Int32)}
		}
	case !errors.Is(err, sql.ErrNoRows):
		return kb, err
	}
	return kb, nil
}

// SetEssayMaterialFavorite 收藏或取消收藏素材；不是自己的返回 404。
func (s *Service) SetEssayMaterialFavorite(ctx context.Context, userID, id uint64, fav bool) error {
	exists, err := s.q.EssayMaterialExists(ctx, dbq.EssayMaterialExistsParams{ID: id, OwnerUserID: owner(userID)})
	if err != nil {
		return err
	}
	if !exists {
		return apperr.NotFoundErr()
	}
	_, err = s.q.SetEssayMaterialFavorite(ctx, dbq.SetEssayMaterialFavoriteParams{Favorite: fav, ID: id, OwnerUserID: owner(userID)})
	return err
}
