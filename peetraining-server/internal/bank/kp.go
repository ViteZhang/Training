package bank

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// RubricPoint 是采分点（来源用于界面标注）。
type RubricPoint struct {
	ID       uint64
	Seq      int
	Content  string
	Score    *float64
	Keywords []string
	Origin   string
}

// RubricInput 是用户编辑的采分点。
type RubricInput struct {
	Content  string
	Score    *float64
	Keywords []string
}

// Mastery 是掌握度（PRD 11.1、11.2）。
type Mastery struct {
	M              float64
	State          string
	NextReviewOn   *rules.Day
	LastSelfAssess string
}

// Source 是出处（文件 + 页码）。
type Source struct {
	MaterialID uint64
	FileName   string
	Page       int
}

// QuestionBrief 是相关题目。
type QuestionBrief struct {
	ID       uint64
	QType    string
	Stem     string
	Source   string
	ExamYear *int
}

// KPDetail 是知识点卡片（3.4）。
type KPDetail struct {
	ID            uint64
	Level         string
	Name          string
	Path          []string
	ParentID      *uint64
	OriginalText  string
	Source        *Source
	Origin        string
	ExamCount     int
	Mastery       Mastery
	Rubric        []RubricPoint
	AIExplanation string
	Related       []QuestionBrief
	NeedsReview   bool
	Official      bool
	SubjectID     uint64
}

// kp 取知识点；不是自己的返回 404。
func (s *Service) kp(ctx context.Context, userID, id uint64) (dbq.GetKPRow, error) {
	k, err := s.q.GetKP(ctx, dbq.GetKPParams{ID: id, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return k, apperr.NotFoundErr()
	}
	return k, err
}

// path 返回知识点的上级名称（板块 / 章节）。
func (s *Service) path(ctx context.Context, userID uint64, parent sql.NullInt64) ([]string, error) {
	var out []string
	for i := 0; parent.Valid && i < 5; i++ {
		p, err := s.kp(ctx, userID, uint64(parent.Int64))
		if err != nil {
			return nil, err
		}
		out = append([]string{p.Name}, out...)
		parent = p.ParentID
	}
	return out, nil
}

// KnowledgePoint 返回知识点卡片。第一次打开时同步生成并缓存 AI 解读；生成失败时解读为空，不影响其他内容。
func (s *Service) KnowledgePoint(ctx context.Context, userID, id uint64) (KPDetail, error) {
	d, err := s.detail(ctx, userID, id)
	if err != nil {
		return d, err
	}
	if d.AIExplanation == "" && d.Level == string(dbq.KnowledgePointsLevelPoint) {
		if text, err := s.explain(ctx, userID, d); err == nil {
			d.AIExplanation = text
		} else if !apperr.IsKind(err, apperr.AIFailed) {
			return d, err
		}
	}
	return d, nil
}

func (s *Service) detail(ctx context.Context, userID, id uint64) (KPDetail, error) {
	k, err := s.kp(ctx, userID, id)
	if err != nil {
		return KPDetail{}, err
	}
	d := KPDetail{ID: k.ID, Level: string(k.Level), Name: k.Name, OriginalText: k.OriginalText.String, Origin: string(k.Origin),
		ExamCount: int(k.ExamCount), NeedsReview: k.NeedsReview, Official: k.OfficialKpID.Valid, AIExplanation: k.AiExplanation.String,
		SubjectID: uint64(k.SubjectID.Int64), Mastery: Mastery{State: string(dbq.KpMasteryStateUnlearned)}}
	if k.ParentID.Valid {
		p := uint64(k.ParentID.Int64)
		d.ParentID = &p
	}
	if d.Path, err = s.path(ctx, userID, k.ParentID); err != nil {
		return d, err
	}
	srcs, err := s.q.ListKPSourcesOfKP(ctx, dbq.ListKPSourcesOfKPParams{KpID: id, OwnerUserID: owner(userID)})
	if err != nil {
		return d, err
	}
	for _, src := range srcs {
		// 出处优先用知识点记录的那一处，没有时用第一处来源。
		if d.Source == nil || (k.SourceMaterialID.Valid && uint64(k.SourceMaterialID.Int64) == src.MaterialID && int(k.SourcePage.Int32) == int(src.PageNo)) {
			d.Source = &Source{MaterialID: src.MaterialID, FileName: src.FileName, Page: int(src.PageNo)}
		}
	}
	m, err := s.q.GetKPMastery(ctx, dbq.GetKPMasteryParams{OwnerUserID: userID, KpID: id})
	switch {
	case err == nil:
		d.Mastery.M, _ = strconv.ParseFloat(m.M, 64)
		d.Mastery.State = string(m.State)
		if m.NextReviewOn.Valid {
			day := rules.DayFromDateColumn(m.NextReviewOn.Time)
			d.Mastery.NextReviewOn = &day
		}
		if m.LastSelfAssess.Valid {
			d.Mastery.LastSelfAssess = string(m.LastSelfAssess.KpMasteryLastSelfAssess)
		}
	case !errors.Is(err, sql.ErrNoRows):
		return d, err
	}
	rps, err := s.q.ListKPRubric(ctx, dbq.ListKPRubricParams{KpID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: owner(userID)})
	if err != nil {
		return d, err
	}
	d.Rubric = toRubric(rps)
	rel, err := s.q.ListRelatedQuestions(ctx, dbq.ListRelatedQuestionsParams{KpID: id, OwnerUserID: owner(userID)})
	if err != nil {
		return d, err
	}
	for _, r := range rel {
		d.Related = append(d.Related, QuestionBrief{ID: r.ID, QType: string(r.Qtype), Stem: r.Stem, Source: string(r.Source), ExamYear: year(r.ExamYear)})
	}
	return d, nil
}

func year(y sql.NullInt16) *int {
	if !y.Valid {
		return nil
	}
	v := int(y.Int16)
	return &v
}

func toRubric(rps []dbq.RubricPoint) []RubricPoint {
	out := make([]RubricPoint, len(rps))
	for i, r := range rps {
		out[i] = RubricPoint{ID: r.ID, Seq: int(r.Seq), Content: r.Content, Origin: string(r.Origin)}
		if v, err := strconv.ParseFloat(r.Score.String, 64); err == nil && r.Score.Valid {
			out[i].Score = &v
		}
		_ = json.Unmarshal(r.Keywords, &out[i].Keywords)
	}
	return out
}

// explain 生成并缓存 AI 解读（3.4）。
func (s *Service) explain(ctx context.Context, userID uint64, d KPDetail) (string, error) {
	in := ai.ExplainIn{Name: d.Name, Path: d.Path, OriginalText: d.OriginalText}
	if b, err := s.subject(ctx, userID, d.SubjectID); err == nil {
		in.Subject = b.Name
	}
	for _, r := range d.Rubric {
		in.Points = append(in.Points, r.Content)
	}
	out, _, err := ai.Explain.Run(ctx, s.ai, userID, in)
	if err != nil {
		return "", err
	}
	err = s.q.SetKPExplanation(ctx, dbq.SetKPExplanationParams{AiExplanation: sql.NullString{String: out.Explanation, Valid: true},
		AiExplanationAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: d.ID, OwnerUserID: owner(userID)})
	return out.Explanation, err
}

// RegenerateExplanation 是「AI 解读不准，重新生成」（3.5）；失败返回 AIFailed，原解读保留。
func (s *Service) RegenerateExplanation(ctx context.Context, userID, id uint64) (KPDetail, error) {
	d, err := s.detail(ctx, userID, id)
	if err != nil {
		return d, err
	}
	text, err := s.explain(ctx, userID, d)
	if err != nil {
		return d, err
	}
	d.AIExplanation = text
	return d, nil
}

// validParent 检查层级：板块没有上级；章节的上级是板块；知识点的上级是板块或章节；不能挂到自己或自己的下级下面。
func (s *Service) validParent(ctx context.Context, userID, bankID uint64, level string, parentID *uint64, self uint64) (sql.NullInt64, error) {
	if level == string(dbq.KnowledgePointsLevelSection) {
		if parentID != nil {
			return sql.NullInt64{}, apperr.New(apperr.BadRequest, "板块不能放在其他节点下面")
		}
		return sql.NullInt64{}, nil
	}
	if parentID == nil {
		return sql.NullInt64{}, apperr.New(apperr.BadRequest, "请选择所属的板块或章节")
	}
	p, err := s.kp(ctx, userID, *parentID)
	if err != nil || p.BankID != bankID {
		return sql.NullInt64{}, apperr.New(apperr.BadRequest, "所属节点不存在").With("reason", "parent_not_found")
	}
	switch {
	case level == string(dbq.KnowledgePointsLevelChapter) && p.Level != dbq.KnowledgePointsLevelSection:
		return sql.NullInt64{}, apperr.New(apperr.BadRequest, "章节只能放在板块下面")
	case level == string(dbq.KnowledgePointsLevelPoint) && p.Level == dbq.KnowledgePointsLevelPoint:
		return sql.NullInt64{}, apperr.New(apperr.BadRequest, "知识点只能放在板块或章节下面")
	}
	// 沿上级一路找，遇到自己说明要移到自己的下级下面。
	for cur := p; self != 0; {
		if cur.ID == self {
			return sql.NullInt64{}, apperr.New(apperr.BadRequest, "不能移到自己下面")
		}
		if !cur.ParentID.Valid {
			break
		}
		if cur, err = s.kp(ctx, userID, uint64(cur.ParentID.Int64)); err != nil {
			return sql.NullInt64{}, err
		}
	}
	return sql.NullInt64{Int64: int64(*parentID), Valid: true}, nil
}

// NodeInput 是新建节点（用户手动调整树结构）。
type NodeInput struct {
	Level        string
	Name         string
	ParentID     *uint64
	OriginalText string
}

// CreateNode 新建板块、章节或知识点。
func (s *Service) CreateNode(ctx context.Context, userID, subjectID uint64, in NodeInput) (KPDetail, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return KPDetail{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || !dbq.KnowledgePointsLevel(in.Level).Valid() {
		return KPDetail{}, apperr.New(apperr.BadRequest, "名称不能为空")
	}
	parent, err := s.validParent(ctx, userID, b.BankID, in.Level, in.ParentID, 0)
	if err != nil {
		return KPDetail{}, err
	}
	sort, err := s.q.MaxKPSort(ctx, dbq.MaxKPSortParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return KPDetail{}, err
	}
	id, err := s.q.InsertKnowledgePoint(ctx, dbq.InsertKnowledgePointParams{OwnerUserID: owner(userID), BankID: b.BankID, ParentID: parent,
		Level: dbq.KnowledgePointsLevel(in.Level), Name: in.Name, OriginalText: sql.NullString{String: in.OriginalText, Valid: in.OriginalText != ""},
		Origin: dbq.KnowledgePointsOriginUserConfirmed, SortOrder: uint32(sort) + 1})
	if err != nil {
		return KPDetail{}, err
	}
	return s.detail(ctx, userID, uint64(id))
}

// KPPatch 是编辑知识点（3.6）或调整归属；为空的字段不改。
type KPPatch struct {
	Name         *string
	OriginalText *string
	ParentID     *uint64
	Rubric       []RubricInput
	RubricSet    bool
	NeedsReview  *bool
}

// UpdateKnowledgePoint 编辑知识点。改了采分点后 AI 解读重新生成（下次打开时）；只影响自己的题库。
func (s *Service) UpdateKnowledgePoint(ctx context.Context, userID, id uint64, p KPPatch) (KPDetail, error) {
	k, err := s.kp(ctx, userID, id)
	if err != nil {
		return KPDetail{}, err
	}
	name, orig, parent, review := k.Name, k.OriginalText, k.ParentID, k.NeedsReview
	if p.Name != nil {
		if name = strings.TrimSpace(*p.Name); name == "" {
			return KPDetail{}, apperr.New(apperr.BadRequest, "名称不能为空")
		}
	}
	if p.OriginalText != nil {
		orig = sql.NullString{String: *p.OriginalText, Valid: *p.OriginalText != ""}
	}
	if p.ParentID != nil {
		if parent, err = s.validParent(ctx, userID, k.BankID, string(k.Level), p.ParentID, id); err != nil {
			return KPDetail{}, err
		}
	}
	if p.NeedsReview != nil {
		review = *p.NeedsReview
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := q.UpdateKP(ctx, dbq.UpdateKPParams{Name: name, OriginalText: orig, ParentID: parent, NeedsReview: review,
			Origin: dbq.KnowledgePointsOriginUserConfirmed, ID: id, OwnerUserID: owner(userID)}); err != nil {
			return err
		}
		changed := p.Name != nil || p.OriginalText != nil || p.RubricSet
		if p.RubricSet {
			if err := q.DeleteKPRubric(ctx, dbq.DeleteKPRubricParams{KpID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: owner(userID)}); err != nil {
				return err
			}
			if err := insertRubric(ctx, q, userID, sql.NullInt64{}, sql.NullInt64{Int64: int64(id), Valid: true}, p.Rubric); err != nil {
				return err
			}
		}
		if changed {
			// 内容改了，AI 解读作废，下次打开时重新生成。
			return q.SetKPExplanation(ctx, dbq.SetKPExplanationParams{ID: id, OwnerUserID: owner(userID)})
		}
		return nil
	})
	if err != nil {
		return KPDetail{}, err
	}
	return s.detail(ctx, userID, id)
}

func insertRubric(ctx context.Context, q *dbq.Queries, userID uint64, questionID, kpID sql.NullInt64, points []RubricInput) error {
	for i, p := range points {
		content := strings.TrimSpace(p.Content)
		if content == "" {
			continue
		}
		var kw []byte
		if len(p.Keywords) > 0 {
			kw, _ = json.Marshal(p.Keywords)
		}
		score := sql.NullString{}
		if p.Score != nil {
			score = sql.NullString{String: strconv.FormatFloat(*p.Score, 'f', 2, 64), Valid: true}
		}
		if err := q.InsertRubricPoint(ctx, dbq.InsertRubricPointParams{OwnerUserID: owner(userID), QuestionID: questionID, KpID: kpID,
			Seq: uint16(i + 1), Content: content, Keywords: kw, Score: score, Origin: dbq.RubricPointsOriginUserConfirmed}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteKnowledgePoint 删除知识点（3.5）：下级节点、题目关联、掌握度随外键一起删除，题目本身保留。
func (s *Service) DeleteKnowledgePoint(ctx context.Context, userID, id uint64) error {
	if _, err := s.kp(ctx, userID, id); err != nil {
		return err
	}
	_, err := s.q.DeleteKP(ctx, dbq.DeleteKPParams{ID: id, OwnerUserID: owner(userID)})
	return err
}

// Merge 合并到其他知识点（3.5）：题目关联、出处、下级节点、采分点、掌握度迁到目标，然后删除来源。
func (s *Service) Merge(ctx context.Context, userID, id, targetID uint64) (KPDetail, error) {
	if id == targetID {
		return KPDetail{}, apperr.New(apperr.BadRequest, "不能合并到自己")
	}
	src, err := s.kp(ctx, userID, id)
	if err != nil {
		return KPDetail{}, err
	}
	dst, err := s.kp(ctx, userID, targetID)
	if err != nil {
		return KPDetail{}, err
	}
	if src.BankID != dst.BankID || src.Level != dst.Level {
		return KPDetail{}, apperr.New(apperr.BadRequest, "只能合并到同一门课里同一层级的节点")
	}
	o := owner(userID)
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := q.MoveQuestionKPs(ctx, dbq.MoveQuestionKPsParams{TargetID: targetID, SourceID: id, Owner: o}); err != nil {
			return err
		}
		if err := q.CopyKPSources(ctx, dbq.CopyKPSourcesParams{TargetID: targetID, SourceID: id, Owner: o}); err != nil {
			return err
		}
		if err := q.MoveKPChildren(ctx, dbq.MoveKPChildrenParams{TargetID: sql.NullInt64{Int64: int64(targetID), Valid: true}, SourceID: sql.NullInt64{Int64: int64(id), Valid: true}, Owner: o}); err != nil {
			return err
		}
		existing, err := q.ListKPRubric(ctx, dbq.ListKPRubricParams{KpID: sql.NullInt64{Int64: int64(targetID), Valid: true}, OwnerUserID: o})
		if err != nil {
			return err
		}
		if err := q.MoveKPRubric(ctx, dbq.MoveKPRubricParams{TargetID: sql.NullInt64{Int64: int64(targetID), Valid: true}, Offset: uint16(len(existing)),
			SourceID: sql.NullInt64{Int64: int64(id), Valid: true}, Owner: o}); err != nil {
			return err
		}
		if err := q.InsertKPMasteryCopy(ctx, dbq.InsertKPMasteryCopyParams{TargetID: targetID, UserID: userID, SourceID: id}); err != nil {
			return err
		}
		if _, err := q.DeleteKP(ctx, dbq.DeleteKPParams{ID: id, OwnerUserID: o}); err != nil {
			return err
		}
		return q.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: src.BankID, OwnerUserID: o})
	})
	if err != nil {
		return KPDetail{}, err
	}
	return s.detail(ctx, userID, targetID)
}

// SplitPart 是拆分出的一部分。
type SplitPart struct {
	Name, OriginalText string
}

// Split 拆分为多个知识点（3.5）：新知识点放在原位置，都关联原来的题目与出处；原知识点删除。
func (s *Service) Split(ctx context.Context, userID, id uint64, parts []SplitPart) ([]KPDetail, error) {
	if len(parts) < 2 {
		return nil, apperr.New(apperr.BadRequest, "至少拆成两个")
	}
	k, err := s.kp(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if k.Level != dbq.KnowledgePointsLevelPoint {
		return nil, apperr.New(apperr.BadRequest, "只能拆分知识点")
	}
	o := owner(userID)
	var ids []uint64
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		for i, p := range parts {
			name := strings.TrimSpace(p.Name)
			if name == "" {
				return apperr.New(apperr.BadRequest, fmt.Sprintf("第 %d 个知识点的名称不能为空", i+1))
			}
			nid, err := q.InsertKnowledgePoint(ctx, dbq.InsertKnowledgePointParams{OwnerUserID: o, BankID: k.BankID, ParentID: k.ParentID,
				Level: dbq.KnowledgePointsLevelPoint, Name: name, OriginalText: sql.NullString{String: p.OriginalText, Valid: p.OriginalText != ""},
				SourceMaterialID: k.SourceMaterialID, SourcePage: k.SourcePage, Origin: dbq.KnowledgePointsOriginUserConfirmed, SortOrder: k.SortOrder})
			if err != nil {
				return err
			}
			if err := q.MoveQuestionKPs(ctx, dbq.MoveQuestionKPsParams{TargetID: uint64(nid), SourceID: id, Owner: o}); err != nil {
				return err
			}
			if err := q.CopyKPSources(ctx, dbq.CopyKPSourcesParams{TargetID: uint64(nid), SourceID: id, Owner: o}); err != nil {
				return err
			}
			ids = append(ids, uint64(nid))
		}
		if _, err := q.DeleteKP(ctx, dbq.DeleteKPParams{ID: id, OwnerUserID: o}); err != nil {
			return err
		}
		return q.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: k.BankID, OwnerUserID: o})
	})
	if err != nil {
		return nil, err
	}
	out := make([]KPDetail, 0, len(ids))
	for _, nid := range ids {
		d, err := s.detail(ctx, userID, nid)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// SelfAssess 三档自评（3.4，只作参考）：还没有作答记录时按自评设掌握分（不会 0、模糊 30、掌握 50），有作答记录时只记录（D16）。
func (s *Service) SelfAssess(ctx context.Context, userID, id uint64, level string) (Mastery, error) {
	if _, err := s.kp(ctx, userID, id); err != nil {
		return Mastery{}, err
	}
	lv := rules.SelfAssess(level)
	if lv != rules.SelfUnknown && lv != rules.SelfVague && lv != rules.SelfMastered {
		return Mastery{}, apperr.New(apperr.BadRequest, "自评档位不正确")
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Mastery{}, err
	}
	m := rules.ApplyMastery(0, rules.MasteryEvent{Kind: rules.EventSelfAssess, SelfAssess: lv}, false, p.Mastery)
	state := rules.StateOf(rules.MasteryFacts{M: m, Assessed: true}, rules.DayOf(s.now()), p.MasteryState)
	if err := s.q.UpsertKPSelfAssess(ctx, dbq.UpsertKPSelfAssessParams{OwnerUserID: userID, KpID: id, M: strconv.FormatFloat(m, 'f', 2, 64),
		State: dbq.KpMasteryState(state), LastSelfAssess: dbq.NullKpMasteryLastSelfAssess{KpMasteryLastSelfAssess: dbq.KpMasteryLastSelfAssess(lv), Valid: true},
		LastSelfAssessAt: sql.NullTime{Time: s.now().UTC(), Valid: true}}); err != nil {
		return Mastery{}, err
	}
	d, err := s.detail(ctx, userID, id)
	return d.Mastery, err
}
