package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// Item 是待确认条目。
type Item = dbq.GetImportItemRow

// Items 分页返回待确认条目（1.7）。filter：all / needs_review / subjective / objective。
func (s *Service) Items(ctx context.Context, userID, jobID uint64, filter string, afterID uint64, limit int) ([]dbq.ImportItem, dbq.CountImportItemsRow, error) {
	if _, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: jobID, OwnerUserID: userID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, dbq.CountImportItemsRow{}, apperr.NotFoundErr()
		}
		return nil, dbq.CountImportItemsRow{}, err
	}
	if filter == "" {
		filter = "all"
	}
	items, err := s.q.ListImportItemsPage(ctx, dbq.ListImportItemsPageParams{JobID: jobID, OwnerUserID: userID, AfterID: afterID, Filter: filter, Limit: int32(limit)})
	if err != nil {
		return nil, dbq.CountImportItemsRow{}, err
	}
	c, err := s.q.CountImportItems(ctx, dbq.CountImportItemsParams{JobID: jobID, OwnerUserID: userID})
	return items, c, err
}

// GetItem 返回单个待确认条目；不是自己的返回 404。
func (s *Service) GetItem(ctx context.Context, userID, itemID uint64) (Item, error) {
	it, err := s.q.GetImportItem(ctx, dbq.GetImportItemParams{ID: itemID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, apperr.NotFoundErr()
	}
	return it, err
}

// Patch 是用户对条目的修改（1.7、1.7b）。
type Patch struct {
	Status   string // confirmed / deleted / pending，空表示不改
	Question *QuestionDraft
	KP       *KPDraft
}

// UpdateItem 修改或删除待确认条目。主观题采分点有分值时合计必须等于题目分值（rubric_sum_mismatch）。
func (s *Service) UpdateItem(ctx context.Context, userID, itemID uint64, p Patch) (Item, error) {
	it, err := s.GetItem(ctx, userID, itemID)
	if err != nil {
		return Item{}, err
	}
	if it.CreatedEntityID.Valid {
		return Item{}, apperr.New(apperr.Conflict, "这一条已经入库，请到题库里修改")
	}
	payload, status, reasons := it.Payload, it.Status, parseReasons(it.ReviewReasons)
	switch {
	case p.Question != nil:
		if it.ItemType != dbq.ImportItemsItemTypeQuestion {
			return Item{}, apperr.New(apperr.BadRequest, "这一条不是题目")
		}
		q := *p.Question
		var old QuestionDraft
		_ = json.Unmarshal(it.Payload, &old)
		if err := validateQuestion(&q, old); err != nil {
			return Item{}, err
		}
		// 用户改过：识别置信度不再是问题；采分点改动后视为用户确认。
		keep := slices.DeleteFunc(slices.Clone(reasons), func(r string) bool { return r == ReasonLowConfidence })
		reasons = reviewReasons(q, keep)
		if payload, err = json.Marshal(q); err != nil {
			return Item{}, err
		}
		status = dbq.ImportItemsStatusEdited
	case p.KP != nil:
		if it.ItemType != dbq.ImportItemsItemTypeKnowledgePoint {
			return Item{}, apperr.New(apperr.BadRequest, "这一条不是知识点")
		}
		k := *p.KP
		if strings.TrimSpace(k.Name) == "" || len(k.KPPath) == 0 {
			return Item{}, apperr.New(apperr.BadRequest, "知识点名称与归属不能为空")
		}
		var old KPDraft
		_ = json.Unmarshal(it.Payload, &old)
		k.SourcePage = old.SourcePage
		reasons = nil
		if payload, err = json.Marshal(k); err != nil {
			return Item{}, err
		}
		status = dbq.ImportItemsStatusEdited
	}
	if p.Status != "" {
		st := dbq.ImportItemsStatus(p.Status)
		if !st.Valid() || st == dbq.ImportItemsStatusEdited {
			return Item{}, apperr.New(apperr.BadRequest, "状态不正确")
		}
		if st != dbq.ImportItemsStatusPending || status == dbq.ImportItemsStatusDeleted {
			status = st
		}
	}
	if err := s.q.UpdateImportItem(ctx, dbq.UpdateImportItemParams{Payload: payload, Status: status, NeedsReview: needsReview(reasons),
		ReviewReasons: reasonsJSON(reasons), DuplicateOfQuestionID: it.DuplicateOfQuestionID, ID: itemID, OwnerUserID: userID}); err != nil {
		return Item{}, err
	}
	return s.GetItem(ctx, userID, itemID)
}

func validateQuestion(q *QuestionDraft, old QuestionDraft) error {
	q.Stem = strings.TrimSpace(q.Stem)
	if q.Stem == "" || !slices.Contains(ai.QTypes, q.QType) {
		return apperr.New(apperr.BadRequest, "题型与题干不能为空")
	}
	q.SourcePage = old.SourcePage
	if q.Source == "" {
		q.Source = old.Source
	}
	if q.Answer != old.Answer && q.Answer != "" {
		q.AnswerOrigin = "user_confirmed"
	}
	if ai.Subjective(q.QType) && q.Score != nil && len(q.RubricPoints) > 0 {
		withScore := 0
		for _, r := range q.RubricPoints {
			if r.Score > 0 {
				withScore++
			}
		}
		if withScore > 0 && !rubricSumOK(q.RubricPoints, *q.Score) {
			return apperr.New(apperr.BadRequest, fmt.Sprintf("采分点分值合计要等于题目分值 %s 分", fmtScore(*q.Score))).With("reason", ReasonRubricSumMismatch)
		}
	}
	if !rubricEqual(q.RubricPoints, old.RubricPoints) && len(q.RubricPoints) > 0 {
		q.RubricOrigin = "user_confirmed"
	}
	return nil
}

func rubricEqual(a, b []ai.RubricPoint) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func fmtScore(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// GenerateAnswer 缺答案时让 AI 生成参考答案与采分点（1.7b，标「AI 生成」），计入每日 AI 出题额度；生成失败不扣。
func (s *Service) GenerateAnswer(ctx context.Context, userID, itemID uint64) (Item, error) {
	it, err := s.GetItem(ctx, userID, itemID)
	if err != nil {
		return Item{}, err
	}
	if it.ItemType != dbq.ImportItemsItemTypeQuestion || it.CreatedEntityID.Valid {
		return Item{}, apperr.New(apperr.BadRequest, "这一条不能生成参考答案")
	}
	var q QuestionDraft
	if err := json.Unmarshal(it.Payload, &q); err != nil {
		return Item{}, err
	}
	if q.Answer != "" {
		return it, nil
	}
	if left, err := s.quota.Remaining(ctx, userID, quota.AIQuestions); err != nil {
		return Item{}, err
	} else if left != nil && *left <= 0 {
		return Item{}, apperr.New(apperr.QuotaExceeded, "今天的 AI 出题次数用完了").With("quota_type", string(quota.AIQuestions)).With("remaining", 0)
	}
	job, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: it.JobID, OwnerUserID: userID})
	if err != nil {
		return Item{}, err
	}
	score := 0.0
	if q.Score != nil {
		score = *q.Score
	}
	out, _, err := ai.GenerateAnswer.Run(ctx, s.ai, userID, ai.GenerateAnswerIn{Subject: job.SubjectName, QType: q.QType, Stem: q.Stem, Score: score})
	if err != nil {
		return Item{}, err
	}
	q.Answer, q.AnswerOrigin = out.Answer, "ai_generated"
	if ai.Subjective(q.QType) {
		q.RubricPoints, q.RubricOrigin = out.Points, "ai_generated"
	}
	reasons := reviewReasons(q, parseReasons(it.ReviewReasons))
	b, err := json.Marshal(q)
	if err != nil {
		return Item{}, err
	}
	err = store.WithTx(ctx, s.db, func(tx *dbq.Queries) error {
		if err := s.quota.Consume(ctx, tx, quota.Charge{UserID: userID, Type: quota.AIQuestions, Amount: 1,
			Ref: quota.Ref{Type: "import_item", ID: itemID}, Key: quota.KeyFor("import_answer", itemID)}); err != nil {
			return err
		}
		return tx.UpdateImportItem(ctx, dbq.UpdateImportItemParams{Payload: b, Status: it.Status, NeedsReview: needsReview(reasons),
			ReviewReasons: reasonsJSON(reasons), DuplicateOfQuestionID: it.DuplicateOfQuestionID, ID: itemID, OwnerUserID: userID})
	})
	if err != nil {
		return Item{}, err
	}
	return s.GetItem(ctx, userID, itemID)
}

// ConfirmResult 是确认入库的结果（1.8）。
type ConfirmResult struct {
	Questions, KPs, NeedsReview, Papers int
	PlanReady                           bool
	SubjectsWithoutImport               []uint64
}

// Confirm 确认入库（1.7「确认入库 N 题」）：除已删除外的条目入库，需核对的带标记入库；可多次调用。
// 导入题数计入额度（与写题目在同一事务，超额整批不入库，返回 402）；入库后按年份重组真题卷、重算资料与考点统计。
func (s *Service) Confirm(ctx context.Context, userID, jobID uint64, itemIDs []uint64) (ConfirmResult, error) {
	job, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: jobID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return ConfirmResult{}, apperr.NotFoundErr()
	}
	if err != nil {
		return ConfirmResult{}, err
	}
	items, err := s.q.ListJobItems(ctx, dbq.ListJobItemsParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return ConfirmResult{}, err
	}
	var todo []dbq.ImportItem
	for _, it := range items {
		if it.CreatedEntityID.Valid || (len(itemIDs) > 0 && !slices.Contains(itemIDs, it.ID)) {
			continue
		}
		todo = append(todo, it)
	}
	var res ConfirmResult
	years := map[int]bool{}
	mats := map[uint64]bool{}
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		t := &tree{q: q, bank: job.BankID, owner: owner, cache: map[string]uint64{}}
		for _, it := range todo {
			var id uint64
			switch it.ItemType {
			case dbq.ImportItemsItemTypeQuestion:
				var d QuestionDraft
				if err := json.Unmarshal(it.Payload, &d); err != nil {
					return err
				}
				if err := s.quota.Consume(ctx, q, quota.Charge{UserID: userID, Type: quota.ImportQuestions, Amount: 1,
					Ref: quota.Ref{Type: "import_item", ID: it.ID}, Key: quota.KeyFor("import_confirm", it.ID)}); err != nil {
					return err
				}
				if id, err = s.insertQuestion(ctx, q, t, job, it, d); err != nil {
					return err
				}
				res.Questions++
				if it.NeedsReview {
					res.NeedsReview++
				}
				if d.Source == "exam" && d.ExamYear != nil && !d.IsRecollection {
					years[*d.ExamYear] = true
				}
			case dbq.ImportItemsItemTypeKnowledgePoint:
				var d KPDraft
				if err := json.Unmarshal(it.Payload, &d); err != nil {
					return err
				}
				if id, err = s.insertKP(ctx, q, t, it, d); err != nil {
					return err
				}
				res.KPs++
			default:
				continue // 作文资料条目在 T11 入库
			}
			if it.MaterialID.Valid {
				mats[uint64(it.MaterialID.Int64)] = true
			}
			if err := q.SetImportItemCreated(ctx, dbq.SetImportItemCreatedParams{CreatedEntityID: sql.NullInt64{Int64: int64(id), Valid: true}, ID: it.ID, OwnerUserID: userID}); err != nil {
				return err
			}
		}
		for y := range years {
			ok, err := s.buildPaper(ctx, q, job, y)
			if err != nil {
				return err
			}
			if ok {
				res.Papers++
			}
		}
		for m := range mats {
			if err := q.RecountMaterial(ctx, dbq.RecountMaterialParams{SourceMaterialID: sql.NullInt64{Int64: int64(m), Valid: true}, MaterialID: m, ID: m, OwnerUserID: userID}); err != nil {
				return err
			}
		}
		if err := q.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: job.BankID, OwnerUserID: owner}); err != nil {
			return err
		}
		c, err := q.CountImportItems(ctx, dbq.CountImportItemsParams{JobID: jobID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		if c.Confirmed >= c.Questions+c.KnowledgePoints && job.Status == dbq.ImportJobsStatusReviewing {
			return q.UpdateImportJobStatus(ctx, dbq.UpdateImportJobStatusParams{Status: dbq.ImportJobsStatusConfirmed,
				ConfirmedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: jobID, OwnerUserID: userID})
		}
		return nil
	})
	if err != nil {
		return ConfirmResult{}, err
	}
	if s.AfterConfirm != nil && job.SubjectID.Valid {
		res.PlanReady = s.AfterConfirm(ctx, userID, uint64(job.SubjectID.Int64))
	}
	res.SubjectsWithoutImport, err = s.q.ListSubjectsWithoutContent(ctx, userID)
	return res, err
}

// tree 按名称找或建「板块 → 章节 → 知识点」，同名节点复用。
type tree struct {
	q     *dbq.Queries
	bank  uint64
	owner sql.NullInt64
	cache map[string]uint64
	sort  uint32
}

var levels = []dbq.KnowledgePointsLevel{dbq.KnowledgePointsLevelSection, dbq.KnowledgePointsLevelChapter, dbq.KnowledgePointsLevelPoint}

// ensure 返回路径上每一级的 ID。路径不足三级时：一级当知识点放在「未分类」板块下，两级是「板块 / 知识点」。
func (t *tree) ensure(ctx context.Context, path []string, leaf func(parent sql.NullInt64) (uint64, error)) ([]uint64, error) {
	clean := make([]string, 0, len(path))
	for _, p := range path {
		if p = strings.TrimSpace(p); p != "" {
			clean = append(clean, truncate(p, 128))
		}
	}
	if len(clean) == 0 {
		return nil, apperr.New(apperr.BadRequest, "知识点名称不能为空")
	}
	if len(clean) == 1 {
		clean = []string{"未分类", clean[0]}
	}
	if len(clean) > 3 {
		clean = append(clean[:2], clean[len(clean)-1])
	}
	lv := levels[:len(clean)-1]
	lv = append(slices.Clone(lv), dbq.KnowledgePointsLevelPoint)
	var ids []uint64
	parent := sql.NullInt64{}
	for i, name := range clean {
		key := fmt.Sprintf("%d/%s/%s", parent.Int64, lv[i], name)
		id, ok := t.cache[key]
		if !ok {
			found, err := t.q.FindKPByName(ctx, dbq.FindKPByNameParams{BankID: t.bank, OwnerUserID: t.owner, ParentID: parent, Level: lv[i], Name: name})
			switch {
			case err == nil:
				id = found
			case errors.Is(err, sql.ErrNoRows):
				if i == len(clean)-1 && leaf != nil {
					id, err = leaf(parent)
				} else {
					id, err = t.insert(ctx, parent, lv[i], name, dbq.InsertKnowledgePointParams{})
				}
				if err != nil {
					return nil, err
				}
			default:
				return nil, err
			}
			t.cache[key] = id
		}
		ids = append(ids, id)
		parent = sql.NullInt64{Int64: int64(id), Valid: true}
	}
	return ids, nil
}

func (t *tree) insert(ctx context.Context, parent sql.NullInt64, level dbq.KnowledgePointsLevel, name string, extra dbq.InsertKnowledgePointParams) (uint64, error) {
	if t.sort == 0 {
		n, err := t.q.MaxKPSort(ctx, dbq.MaxKPSortParams{BankID: t.bank, OwnerUserID: t.owner})
		if err != nil {
			return 0, err
		}
		t.sort = uint32(n)
	}
	t.sort++
	p := extra
	p.OwnerUserID, p.BankID, p.ParentID, p.Level, p.Name, p.SortOrder = t.owner, t.bank, parent, level, name, t.sort
	if p.Origin == "" {
		p.Origin = dbq.KnowledgePointsOriginAiExtracted
	}
	id, err := t.q.InsertKnowledgePoint(ctx, p)
	return uint64(id), err
}

func (s *Service) insertKP(ctx context.Context, q *dbq.Queries, t *tree, it dbq.ImportItem, d KPDraft) (uint64, error) {
	path := append(slices.Clone(d.KPPath), d.Name)
	origin := dbq.KnowledgePointsOriginAiExtracted
	if it.Status == dbq.ImportItemsStatusEdited {
		origin = dbq.KnowledgePointsOriginUserConfirmed
	}
	created := false
	ids, err := t.ensure(ctx, path, func(parent sql.NullInt64) (uint64, error) {
		created = true
		return t.insert(ctx, parent, dbq.KnowledgePointsLevelPoint, truncate(strings.TrimSpace(d.Name), 128), dbq.InsertKnowledgePointParams{
			OriginalText: sql.NullString{String: d.OriginalText, Valid: d.OriginalText != ""}, SourceMaterialID: it.MaterialID,
			SourcePage: sql.NullInt32{Int32: int32(d.SourcePage), Valid: d.SourcePage > 0}, Origin: origin,
		})
	})
	if err != nil {
		return 0, err
	}
	kp := ids[len(ids)-1]
	if it.MaterialID.Valid {
		if err := q.InsertKPSource(ctx, dbq.InsertKPSourceParams{KpID: kp, MaterialID: uint64(it.MaterialID.Int64), PageNo: uint32(max(d.SourcePage, 0)), OwnerUserID: t.owner}); err != nil {
			return 0, err
		}
	}
	if created {
		rorigin := dbq.RubricPointsOriginAiExtracted
		if origin == dbq.KnowledgePointsOriginUserConfirmed {
			rorigin = dbq.RubricPointsOriginUserConfirmed
		}
		if err := insertRubric(ctx, q, t.owner, sql.NullInt64{}, sql.NullInt64{Int64: int64(kp), Valid: true}, d.RubricPoints, rorigin); err != nil {
			return 0, err
		}
	}
	return kp, nil
}

func insertRubric(ctx context.Context, q *dbq.Queries, owner, questionID, kpID sql.NullInt64, points []ai.RubricPoint, origin dbq.RubricPointsOrigin) error {
	for i, p := range points {
		var kw []byte
		if len(p.Keywords) > 0 {
			kw, _ = json.Marshal(p.Keywords)
		}
		if err := q.InsertRubricPoint(ctx, dbq.InsertRubricPointParams{OwnerUserID: owner, QuestionID: questionID, KpID: kpID, Seq: uint16(i + 1),
			Content: p.Content, Keywords: kw, Score: sql.NullString{String: decimal(p.Score), Valid: p.Score > 0}, Origin: origin}); err != nil {
			return err
		}
	}
	return nil
}

func decimal(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

func (s *Service) insertQuestion(ctx context.Context, q *dbq.Queries, t *tree, job dbq.GetImportJobRow, it dbq.ImportItem, d QuestionDraft) (uint64, error) {
	p := dbq.InsertQuestionParams{
		OwnerUserID: t.owner, BankID: job.BankID, Qtype: dbq.QuestionsQtype(d.QType), Stem: d.Stem,
		Answer: sql.NullString{String: d.Answer, Valid: d.Answer != ""}, Analysis: sql.NullString{String: d.Analysis, Valid: d.Analysis != ""},
		Source: dbq.QuestionsSource(d.Source), QuestionNo: sql.NullString{String: truncate(d.QuestionNo, 16), Valid: d.QuestionNo != ""},
		IsRecollection: d.IsRecollection, SourceMaterialID: it.MaterialID, SourcePage: sql.NullInt32{Int32: int32(d.SourcePage), Valid: d.SourcePage > 0},
		ContentHash: ContentHash(d.Stem), NeedsReview: it.NeedsReview, ReviewReasons: it.ReviewReasons,
		// 首个版本不识别难度，按中（D16）。
		Difficulty: dbq.NullQuestionsDifficulty{QuestionsDifficulty: dbq.QuestionsDifficultyMedium, Valid: true},
	}
	if !p.Source.Valid() {
		p.Source = dbq.QuestionsSourceExercise
	}
	if len(d.Options) > 0 {
		p.Options, _ = json.Marshal(d.Options)
	}
	if d.AnswerOrigin != "" {
		p.AnswerOrigin = dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOrigin(d.AnswerOrigin), Valid: true}
	}
	if d.Score != nil {
		p.Score = sql.NullString{String: decimal(*d.Score), Valid: true}
	}
	if d.ExamYear != nil {
		p.ExamYear = sql.NullInt16{Int16: int16(*d.ExamYear), Valid: true}
	}
	res, err := q.InsertQuestion(ctx, p)
	if err != nil {
		return 0, err
	}
	qid := uint64(res)
	origin := dbq.RubricPointsOrigin(d.RubricOrigin)
	if !origin.Valid() || origin == dbq.RubricPointsOriginOfficial {
		origin = dbq.RubricPointsOriginAiExtracted
	}
	if err := insertRubric(ctx, q, t.owner, sql.NullInt64{Int64: int64(qid), Valid: true}, sql.NullInt64{}, d.RubricPoints, origin); err != nil {
		return 0, err
	}
	var kp uint64
	if d.KPID != nil {
		if _, err := q.GetBankKP(ctx, dbq.GetBankKPParams{ID: uint64(*d.KPID), BankID: job.BankID, OwnerUserID: t.owner}); err == nil {
			kp = uint64(*d.KPID)
		}
	}
	if kp == 0 && len(d.KPPath) > 0 {
		ids, err := t.ensure(ctx, d.KPPath, nil)
		if err != nil {
			return 0, err
		}
		kp = ids[len(ids)-1]
	}
	if kp != 0 {
		if err := q.InsertQuestionKP(ctx, dbq.InsertQuestionKPParams{QuestionID: qid, KpID: kp, IsPrimary: true, OwnerUserID: t.owner}); err != nil {
			return 0, err
		}
	}
	return qid, nil
}

// qtypeNames 是真题卷里题型分组的名称。
var qtypeNames = map[string]string{
	"single_choice": "单项选择题", "multi_choice": "多项选择题", "true_false": "判断题", "fill_blank": "填空题",
	"term": "名词解释", "short_answer": "简答题", "discussion": "论述题", "essay": "作文", "calculation": "计算题", "other": "其他",
}

var qtypeOrder = []string{"single_choice", "multi_choice", "true_false", "fill_blank", "term", "short_answer", "calculation", "discussion", "essay", "other"}

// buildPaper 按年份把真题组成真题卷（PRD 11.6）：满分取专业课满分；收录题目的分值合计小于满分时记缺题说明，成绩按比例换算。
// 题目没有分值的年份不组卷（无法计分）。返回是否组了卷。
func (s *Service) buildPaper(ctx context.Context, q *dbq.Queries, job dbq.GetImportJobRow, year int) (bool, error) {
	owner := sql.NullInt64{Int64: int64(job.OwnerUserID), Valid: true}
	y := sql.NullInt16{Int16: int16(year), Valid: true}
	qs, err := q.ListExamQuestionsByYear(ctx, dbq.ListExamQuestionsByYearParams{BankID: job.BankID, OwnerUserID: owner, ExamYear: y})
	if err != nil {
		return false, err
	}
	type entry struct {
		id    uint64
		qtype string
		score float64
	}
	var es []entry
	actual := 0.0
	for _, x := range qs {
		sc, _ := strconv.ParseFloat(x.Score.String, 64)
		if sc <= 0 {
			continue
		}
		es = append(es, entry{x.ID, string(x.Qtype), sc})
		actual += sc
	}
	if len(es) == 0 {
		return false, nil
	}
	sort.SliceStable(es, func(i, j int) bool {
		return slices.Index(qtypeOrder, es[i].qtype) < slices.Index(qtypeOrder, es[j].qtype)
	})
	type part struct {
		QType     string  `json:"qtype"`
		Count     int     `json:"count"`
		ScoreEach float64 `json:"score_each"`
		Total     float64 `json:"total"`
	}
	var structure []part
	for _, e := range es {
		if n := len(structure); n > 0 && structure[n-1].QType == e.qtype {
			structure[n-1].Count++
			structure[n-1].Total += e.score
			structure[n-1].ScoreEach = structure[n-1].Total / float64(structure[n-1].Count)
			continue
		}
		structure = append(structure, part{QType: e.qtype, Count: 1, ScoreEach: e.score, Total: e.score})
	}
	sb, _ := json.Marshal(structure)
	full := float64(job.SubjectFullScore)
	if actual > full {
		full = actual
	}
	missing := sql.NullString{}
	if actual < full {
		missing = sql.NullString{String: fmt.Sprintf("收录题目共 %s 分，少于整卷 %s 分，成绩按比例换算", fmtScore(actual), fmtScore(full)), Valid: true}
	}
	var paperID uint64
	p, err := q.GetRealExamPaper(ctx, dbq.GetRealExamPaperParams{BankID: job.BankID, OwnerUserID: owner, ExamYear: y})
	switch {
	case err == nil:
		paperID = p.ID
		if err := q.UpdatePaperScores(ctx, dbq.UpdatePaperScoresParams{FullScore: decimal(full), ActualScore: decimal(actual), Structure: sb, MissingNote: missing, ID: p.ID, OwnerUserID: owner}); err != nil {
			return false, err
		}
		if err := q.DeletePaperQuestions(ctx, dbq.DeletePaperQuestionsParams{PaperID: p.ID, OwnerUserID: owner}); err != nil {
			return false, err
		}
	case errors.Is(err, sql.ErrNoRows):
		src := sql.NullInt64{}
		if len(qs) > 0 {
			src = qs[0].SourceMaterialID
		}
		id, err := q.InsertPaper(ctx, dbq.InsertPaperParams{OwnerUserID: owner, BankID: job.BankID, Title: fmt.Sprintf("%d 年真题", year),
			ExamYear: y, FullScore: decimal(full), ActualScore: decimal(actual), Structure: sb, MissingNote: missing, SourceMaterialID: src})
		if err != nil {
			return false, err
		}
		paperID = uint64(id)
	default:
		return false, err
	}
	for i, e := range es {
		if err := q.InsertPaperQuestion(ctx, dbq.InsertPaperQuestionParams{PaperID: paperID, Seq: uint16(i + 1), QuestionID: e.id,
			Section: qtypeNames[e.qtype], Score: decimal(e.score)}); err != nil {
			return false, err
		}
	}
	return true, nil
}
