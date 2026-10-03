package importer

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
	"peetraining-server/internal/notify"
)

// Finalize 在任务的全部文件都结束后整理（第 7–10 步）：按「年份 + 题号」跨文件配答案、提取采分点、打知识点标签、
// 与题库已有题目去重，然后任务进入待确认。可重复执行：只改用户还没动过（pending）的条目，已提取的不重复调模型。
func (s *Service) Finalize(ctx context.Context, userID, jobID uint64) error {
	job, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: jobID, OwnerUserID: userID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if job.Status == dbq.ImportJobsStatusCanceled {
		return nil
	}
	ms, err := s.q.ListImportJobMaterials(ctx, dbq.ListImportJobMaterialsParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	allFailed := len(ms) > 0
	for _, m := range ms {
		if m.Status == dbq.ImportJobMaterialsStatusPending || m.Status == dbq.ImportJobMaterialsStatusRunning {
			return nil // 还有文件没处理完，最后一个结束时会再触发
		}
		if m.Status != dbq.ImportJobMaterialsStatusFailed {
			allFailed = false
		}
	}
	items, err := s.q.ListJobItems(ctx, dbq.ListJobItemsParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	answers, err := s.q.ListImportAnswers(ctx, dbq.ListImportAnswersParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	bank, err := s.q.ListBankQuestionsForDedupe(ctx, dbq.ListBankQuestionsForDedupeParams{BankID: job.BankID, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if err != nil {
		return err
	}

	type work struct {
		item    dbq.ImportItem
		q       QuestionDraft
		reasons []string
		dupOf   uint64
		changed bool
	}
	var ws []*work
	for _, it := range items {
		if it.ItemType != dbq.ImportItemsItemTypeQuestion || it.Status != dbq.ImportItemsStatusPending {
			continue
		}
		w := &work{item: it, reasons: parseReasons(it.ReviewReasons)}
		if err := json.Unmarshal(it.Payload, &w.q); err != nil {
			continue
		}
		ws = append(ws, w)
	}

	// 第 7 步：配答案。
	for _, w := range ws {
		if w.q.Answer != "" || w.q.QuestionNo == "" {
			continue
		}
		for _, a := range answers {
			if a.QuestionNo == w.q.QuestionNo && sameYear(a.ExamYear, w.q.ExamYear) {
				w.q.Answer, w.q.AnswerOrigin, w.changed = a.Answer, "imported", true
				break
			}
		}
	}
	// 第 8 步：有参考答案的主观题提取采分点（标「AI 提取 · 待确认」）。
	for _, w := range ws {
		if !ai.Subjective(w.q.QType) || w.q.Answer == "" || len(w.q.RubricPoints) > 0 {
			continue
		}
		score := 0.0
		if w.q.Score != nil {
			score = *w.q.Score
		}
		out, _, err := ai.ExtractRubric.Run(ctx, s.ai, userID, ai.RubricIn{QType: w.q.QType, Stem: w.q.Stem, Answer: w.q.Answer, Score: score})
		if apperr.IsKind(err, apperr.AIFailed) {
			continue // 没提取出来的，用户可在 1.7b 手动加
		}
		if err != nil {
			return err
		}
		w.q.RubricPoints, w.q.RubricOrigin, w.changed = out.Points, "ai_extracted", true
	}
	// 第 9 步：知识点标签。已有的知识点路径优先复用。
	var need []*work
	for _, w := range ws {
		if len(w.q.KPPath) == 0 && w.q.KPID == nil {
			need = append(need, w)
		}
	}
	if len(need) > 0 {
		existing, err := s.existingPaths(ctx, job.BankID, userID)
		if err != nil {
			return err
		}
		for start := 0; start < len(need); start += tagBatch {
			batch := need[start:min(start+tagBatch, len(need))]
			in := ai.TagIn{Subject: job.SubjectName, Existing: existing}
			for i, w := range batch {
				in.Questions = append(in.Questions, ai.TagQ{ID: i + 1, QType: w.q.QType, Stem: w.q.Stem, Answer: truncate(w.q.Answer, 300)})
			}
			out, _, err := ai.TagQuestions.Run(ctx, s.ai, userID, in)
			if apperr.IsKind(err, apperr.AIFailed) {
				continue
			}
			if err != nil {
				return err
			}
			for _, t := range out.Tags {
				if t.ID >= 1 && t.ID <= len(batch) {
					w := batch[t.ID-1]
					w.q.KPPath, w.changed = t.Path, true
					existing = append(existing, t.Path)
				}
			}
		}
	}
	// 第 10 步：与题库已有题目去重（内容哈希相同，或同题型且题干高度相似）。
	for _, w := range ws {
		h := ContentHash(w.q.Stem)
		for _, b := range bank {
			// 不同年份的真题考了同一题不算重复。
			if b.ExamYear.Valid && w.q.ExamYear != nil && int(b.ExamYear.Int16) != *w.q.ExamYear {
				continue
			}
			if b.ContentHash == h || (string(b.Qtype) == w.q.QType && ai.Overlap(w.q.Stem, b.Stem) >= 0.9 && ai.Overlap(b.Stem, w.q.Stem) >= 0.9) {
				w.dupOf, w.changed = b.ID, true
				w.reasons = append(w.reasons, ReasonDuplicate)
				break
			}
		}
	}
	for _, w := range ws {
		reasons := reviewReasons(w.q, w.reasons)
		if !w.changed && strings.Join(reasons, ",") == strings.Join(parseReasons(w.item.ReviewReasons), ",") {
			continue
		}
		b, err := json.Marshal(w.q)
		if err != nil {
			return err
		}
		if err := s.q.UpdateImportItem(ctx, dbq.UpdateImportItemParams{Payload: b, Status: dbq.ImportItemsStatusPending,
			NeedsReview: needsReview(reasons), ReviewReasons: reasonsJSON(reasons),
			DuplicateOfQuestionID: sql.NullInt64{Int64: int64(w.dupOf), Valid: w.dupOf != 0}, ID: w.item.ID, OwnerUserID: userID}); err != nil {
			return err
		}
	}

	if err := s.detectEssaySubject(ctx, userID, job.SubjectID); err != nil {
		return err
	}
	st := dbq.ImportJobsStatusReviewing
	reason := sql.NullString{}
	if allFailed {
		st, reason = dbq.ImportJobsStatusFailed, sql.NullString{String: "全部文件都没能解析", Valid: true}
	}
	if job.Status == dbq.ImportJobsStatusConfirmed && !allFailed {
		st = dbq.ImportJobsStatusConfirmed
		if c, err := s.q.CountImportItems(ctx, dbq.CountImportItemsParams{JobID: jobID, OwnerUserID: userID}); err == nil && c.Confirmed < c.Questions+c.KnowledgePoints+c.EssayItems {
			st = dbq.ImportJobsStatusReviewing
		}
	}
	if err := s.q.UpdateImportJobStatus(ctx, dbq.UpdateImportJobStatusParams{Status: st, FailReason: reason,
		FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: jobID, OwnerUserID: userID}); err != nil {
		return err
	}
	return s.notifyFinished(ctx, userID, jobID, st)
}

// notifyFinished 发「题库整理完成」，有待核对的再发「需核对」（2.3）。dedupe_key 按任务去重，任务重试不重复发。
func (s *Service) notifyFinished(ctx context.Context, userID, jobID uint64, st dbq.ImportJobsStatus) error {
	if !notify.TaskDoneEnabled(ctx, s.q) {
		return nil
	}
	id := strconv.FormatUint(jobID, 10)
	link := notify.Link("import_review", map[string]any{"job_id": jobID})
	if st == dbq.ImportJobsStatusFailed {
		return s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeImportDone, Title: "资料解析失败",
			Body: "这次导入的文件都没能解析，可以换清晰的文件或粘贴文字重新导入", Link: notify.Link("import", nil), DedupeKey: sql.NullString{String: "import_done:" + id, Valid: true}})
	}
	c, err := s.q.CountImportItems(ctx, dbq.CountImportItemsParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	body := fmt.Sprintf("识别出 %d 道题、%d 个知识点，确认后就能开始练", c.Questions, c.KnowledgePoints)
	if c.EssayItems > 0 {
		body = fmt.Sprintf("识别出 %d 条作文资料，确认后就能开始练", c.EssayItems)
	}
	if err := s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeImportDone, Title: "题库整理完成", Body: body,
		Link: link, DedupeKey: sql.NullString{String: "import_done:" + id, Valid: true}}); err != nil {
		return err
	}
	if c.NeedsReview == 0 {
		return nil
	}
	return s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeReviewNeeded, Title: "有内容需要核对",
		Body: fmt.Sprintf("%d 处需要你核对（采分点、答案或识别不清的地方），核对后批改更准", c.NeedsReview), Link: link,
		DedupeKey: sql.NullString{String: "review_needed:" + id, Valid: true}})
}

func sameYear(a sql.NullInt16, b *int) bool {
	if !a.Valid || b == nil {
		return !a.Valid && b == nil
	}
	return int(a.Int16) == *b
}

// existingPaths 返回题库里已有的「板块 / 章节 / 知识点」路径，供打标签时复用。
func (s *Service) existingPaths(ctx context.Context, bankID, userID uint64) ([][]string, error) {
	kps, err := s.q.ListBankKPs(ctx, dbq.ListBankKPsParams{BankID: bankID, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if err != nil {
		return nil, err
	}
	byID := map[uint64]dbq.ListBankKPsRow{}
	for _, k := range kps {
		byID[k.ID] = k
	}
	var out [][]string
	for _, k := range kps {
		if k.Level != dbq.KnowledgePointsLevelPoint {
			continue
		}
		path := []string{k.Name}
		for p := k.ParentID; p.Valid; {
			parent, ok := byID[uint64(p.Int64)]
			if !ok {
				break
			}
			path = append([]string{parent.Name}, path...)
			p = parent.ParentID
		}
		if len(path) == 3 {
			out = append(out, path)
		}
		if len(out) >= 300 {
			break
		}
	}
	return out, nil
}
