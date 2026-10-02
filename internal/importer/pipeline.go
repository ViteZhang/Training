package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/material"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// 每批交给模型的字数：结构化按块分批，知识点按页分批。
const (
	structureChunkChars = 6000
	kpChunkChars        = 8000
	tagBatch            = 30
)

var steps = []dbq.ImportJobMaterialsStep{
	dbq.ImportJobMaterialsStepQueued, dbq.ImportJobMaterialsStepExtract, dbq.ImportJobMaterialsStepModerate,
	dbq.ImportJobMaterialsStepStructure, dbq.ImportJobMaterialsStepDone,
}

func stepIndex(st dbq.ImportJobMaterialsStep) int {
	for i, x := range steps {
		if x == st {
			return i
		}
	}
	return 0
}

// ProcessMaterial 处理任务里的一个文件：取文本 → 补预占 → 内容安全 → 切题与结构化（或拆知识点）→ 结算额度。
// 可重复执行：每步完成后记下步骤，重试从没完成的那一步开始；结构化重跑会先清掉这份资料用户还没动过的条目。
func (s *Service) ProcessMaterial(ctx context.Context, userID, jobID, materialID uint64) error {
	jm, err := s.jobMaterial(ctx, userID, jobID, materialID)
	if apperr.IsKind(err, apperr.NotFound) {
		return nil // 已从任务里移除
	}
	if err != nil {
		return err
	}
	if jm.Status == dbq.ImportJobMaterialsStatusDone || jm.Status == dbq.ImportJobMaterialsStatusFailed || jm.Status == dbq.ImportJobMaterialsStatusPartial {
		return nil
	}
	job, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: jobID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	if job.Status == dbq.ImportJobsStatusQueued {
		if err := s.q.UpdateImportJobStatus(ctx, dbq.UpdateImportJobStatusParams{Status: dbq.ImportJobsStatusRunning,
			StartedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: jobID, OwnerUserID: userID}); err != nil {
			return err
		}
	}
	mark := func(st dbq.ImportJobMaterialsStep) error {
		jm.Step, jm.Status = st, dbq.ImportJobMaterialsStatusRunning
		return s.q.UpdateImportJobMaterial(ctx, dbq.UpdateImportJobMaterialParams{Step: st, Status: jm.Status, Attempts: jm.Attempts,
			JobID: jobID, MaterialID: materialID, OwnerUserID: userID})
	}
	partial := ""

	if stepIndex(jm.Step) <= stepIndex(dbq.ImportJobMaterialsStepExtract) {
		if err := mark(dbq.ImportJobMaterialsStepExtract); err != nil {
			return err
		}
		if err := s.material.Extract(ctx, userID, materialID); err != nil {
			if material.IsPermanent(err) {
				return s.fail(ctx, jm, userMessage(err, "文件无法读取，请检查后重新上传"))
			}
			return err
		}
		if ok, err := s.topUp(ctx, &jm); err != nil || !ok {
			return err
		}
	}
	if stepIndex(jm.Step) <= stepIndex(dbq.ImportJobMaterialsStepModerate) {
		if err := mark(dbq.ImportJobMaterialsStepModerate); err != nil {
			return err
		}
		ok, err := s.material.Moderate(ctx, userID, materialID)
		if err != nil {
			return err
		}
		if !ok {
			m, _ := s.material.Get(ctx, userID, materialID)
			return s.fail(ctx, jm, m.FailReason.String)
		}
	}
	if stepIndex(jm.Step) <= stepIndex(dbq.ImportJobMaterialsStepStructure) {
		if err := mark(dbq.ImportJobMaterialsStepStructure); err != nil {
			return err
		}
		n, failed, err := s.structure(ctx, job, materialID)
		if err != nil {
			return err
		}
		switch {
		case n == 0 && failed > 0:
			return s.fail(ctx, jm, "AI 没能识别这份资料，请稍后重试")
		case n == 0:
			return s.fail(ctx, jm, emptyReason(job.Mode))
		case failed > 0:
			partial = fmt.Sprintf("有 %d 段没能识别，已识别出 %d 条", failed, n)
		}
	}
	return s.finishFile(ctx, jm, partial)
}

func emptyReason(mode dbq.ImportJobsMode) string {
	if mode == dbq.ImportJobsModeEssay {
		return "没有从这份资料里整理出作文题、评分细则、写作方法、素材或范文"
	}
	if mode == dbq.ImportJobsModeReference {
		return "没有从这份资料里拆出知识点。如果它是真题或习题，请按「题目」导入"
	}
	return "没有识别出题目。如果这是讲义或笔记，请按「资料」导入；照片请对准题目重新拍"
}

func userMessage(err error, def string) string {
	if ue, ok := extract.AsUserError(err); ok {
		return ue.Msg
	}
	return def
}

// topUp 取完文本后按实际计费页数补预占（Word、Excel 建任务时页数未知）；额度不足时这个文件标失败。
func (s *Service) topUp(ctx context.Context, jm *dbq.ImportJobMaterial) (bool, error) {
	m, err := s.material.Get(ctx, jm.OwnerUserID, jm.MaterialID)
	if err != nil {
		return false, err
	}
	extra := int(m.BilledPages) - int(jm.ReservedPages)
	if extra <= 0 {
		return true, nil
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		t, err := s.quota.Reserve(ctx, q, quota.Charge{UserID: jm.OwnerUserID, Type: quota.ParsePages, Amount: extra,
			Ref: quota.Ref{Type: "material", ID: jm.MaterialID}, Key: fmt.Sprintf("%s:%d:extra", reserveKey(jm.JobID, jm.MaterialID), jm.Attempts)})
		if err != nil {
			return err
		}
		if t.Period != jm.QuotaPeriod.String {
			// 跨了额度周期（如会员跨月）：补的部分在新周期里直接记为已用；原预占结算时最多按原预占扣。
			return s.quota.Settle(ctx, q, t, extra, quota.Ref{Type: "material", ID: jm.MaterialID})
		}
		jm.ReservedPages += uint32(extra)
		if err := q.AddImportJobPages(ctx, dbq.AddImportJobPagesParams{Reserved: uint32(extra), ID: jm.JobID, OwnerUserID: jm.OwnerUserID}); err != nil {
			return err
		}
		return q.SetImportJobMaterialQuota(ctx, dbq.SetImportJobMaterialQuotaParams{ReservedPages: jm.ReservedPages, QuotaPeriod: jm.QuotaPeriod,
			JobID: jm.JobID, MaterialID: jm.MaterialID, OwnerUserID: jm.OwnerUserID})
	})
	if apperr.IsKind(err, apperr.QuotaExceeded) {
		return false, s.fail(ctx, *jm, fmt.Sprintf("这份资料有 %d 页，解析额度不足。开通会员或删减页数后再试", m.BilledPages))
	}
	return err == nil, err
}

// structure 第 5–6 步：按导入方式结构化，写入待确认条目。返回写入条数与没能识别的批数。
func (s *Service) structure(ctx context.Context, job dbq.GetImportJobRow, materialID uint64) (int, int, error) {
	userID := job.OwnerUserID
	pages, err := s.q.ListMaterialPages(ctx, dbq.ListMaterialPagesParams{MaterialID: materialID, OwnerUserID: userID})
	if err != nil {
		return 0, 0, err
	}
	mid := sql.NullInt64{Int64: int64(materialID), Valid: true}
	if err := s.q.DeletePendingItemsOfMaterial(ctx, dbq.DeletePendingItemsOfMaterialParams{JobID: job.ID, MaterialID: mid, OwnerUserID: userID}); err != nil {
		return 0, 0, err
	}
	if err := s.q.DeleteImportAnswersOfMaterial(ctx, dbq.DeleteImportAnswersOfMaterialParams{JobID: job.ID, MaterialID: materialID, OwnerUserID: userID}); err != nil {
		return 0, 0, err
	}
	m, err := s.material.Get(ctx, userID, materialID)
	if err != nil {
		return 0, 0, err
	}
	category, err := s.classify(ctx, job, m, pages)
	if err != nil {
		return 0, 0, err
	}
	// 作文类资料不管 1.4 选的是什么都按作文资料整理（作文课不需要用户事先声明，PRD 1.4）。
	if category == "essay" || job.Mode == dbq.ImportJobsModeEssay {
		return s.organizeEssay(ctx, job, materialID, pages)
	}
	switch job.Mode {
	case dbq.ImportJobsModeQuestion:
		return s.structureQuestions(ctx, job, materialID, pages)
	case dbq.ImportJobsModeReference:
		return s.extractKPs(ctx, job, materialID, pages)
	}
	return 0, 0, nil
}

func (s *Service) structureQuestions(ctx context.Context, job dbq.GetImportJobRow, materialID uint64, pages []dbq.MaterialPage) (int, int, error) {
	in := make([]Page, len(pages))
	for i, p := range pages {
		in[i] = Page{No: int(p.PageNo), Text: p.Text}
	}
	n, failed := 0, 0
	for _, chunk := range Chunk(Split(in), structureChunkChars) {
		out, meta, err := ai.Structure.Run(ctx, s.ai, job.OwnerUserID, ai.StructureIn{Subject: job.SubjectName, Blocks: chunk})
		if apperr.IsKind(err, apperr.AIFailed) {
			failed++
			continue
		}
		if err != nil {
			return n, failed, err
		}
		s.notePrompt(ctx, job, ai.Structure.Name, meta)
		for _, it := range out.Items {
			if err := s.saveStructured(ctx, job, materialID, it); err != nil {
				return n, failed, err
			}
			n++ // 题目与「只有答案」的条目都算识别出的内容（答案文件里只有答案）
		}
	}
	return n, failed, nil
}

func (s *Service) saveStructured(ctx context.Context, job dbq.GetImportJobRow, materialID uint64, it ai.QItem) error {
	userID := job.OwnerUserID
	if it.Kind == "answer" {
		year := sql.NullInt16{}
		if it.ExamYear != nil {
			year = sql.NullInt16{Int16: int16(*it.ExamYear), Valid: true}
		}
		return s.q.UpsertImportAnswer(ctx, dbq.UpsertImportAnswerParams{OwnerUserID: userID, JobID: job.ID, MaterialID: materialID,
			ExamYear: year, QuestionNo: it.QuestionNo, Answer: it.Answer, Page: uint32(it.Page),
			DedupeKey: hashOf(strconv.FormatUint(materialID, 10), yearKey(it.ExamYear), it.QuestionNo)})
	}
	q := QuestionDraft{QType: it.QType, Stem: strings.TrimSpace(it.Stem), Options: it.Options, Answer: strings.TrimSpace(it.Answer),
		Score: it.Score, ExamYear: it.ExamYear, QuestionNo: it.QuestionNo, IsRecollection: it.IsRecollection, SourcePage: it.Page,
		Source: "exercise"}
	if q.ExamYear != nil {
		q.Source = "exam"
	}
	if q.Answer != "" {
		q.AnswerOrigin = "imported"
	}
	var keep []string
	if it.Confidence < LowConfidence {
		keep = append(keep, ReasonLowConfidence)
	}
	return s.putItem(ctx, job, materialID, dbq.ImportItemsItemTypeQuestion, q, it.Confidence, reviewReasons(q, keep), ContentHash(q.Stem)+":"+q.QType)
}

func yearKey(y *int) string {
	if y == nil {
		return ""
	}
	return strconv.Itoa(*y)
}

// putItem 写一条待确认条目。dedupe_key 是内容哈希：同一任务里同一道题只留一条（重跑、两份资料里重复的题）。
func (s *Service) putItem(ctx context.Context, job dbq.GetImportJobRow, materialID uint64, typ dbq.ImportItemsItemType, payload any, conf float64, reasons []string, key string) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	seq := uint32(0)
	if p, ok := payload.(QuestionDraft); ok {
		if n, err := strconv.Atoi(p.QuestionNo); err == nil {
			seq = uint32(max(n, 0))
		}
		seq += uint32(p.SourcePage) * 1000
	}
	if p, ok := payload.(KPDraft); ok {
		seq = uint32(p.SourcePage) * 1000
	}
	return s.q.UpsertImportItem(ctx, dbq.UpsertImportItemParams{
		OwnerUserID: job.OwnerUserID, JobID: job.ID, MaterialID: sql.NullInt64{Int64: int64(materialID), Valid: true},
		ItemType: typ, Seq: seq, Payload: b, Confidence: sql.NullString{String: strconv.FormatFloat(conf, 'f', 3, 64), Valid: true},
		NeedsReview: needsReview(reasons), ReviewReasons: reasonsJSON(reasons), DedupeKey: sql.NullString{String: hashOf(key), Valid: true},
	})
}

// extractKPs 参考资料拆知识点（原文表述逐字可查，由 ai.ExtractKPs 校验）。
func (s *Service) extractKPs(ctx context.Context, job dbq.GetImportJobRow, materialID uint64, pages []dbq.MaterialPage) (int, int, error) {
	var batches [][]ai.KPPage
	var cur []ai.KPPage
	size := 0
	for _, p := range pages {
		l := len([]rune(p.Text))
		if len(cur) > 0 && size+l > kpChunkChars {
			batches, cur, size = append(batches, cur), nil, 0
		}
		cur = append(cur, ai.KPPage{No: int(p.PageNo), Text: p.Text})
		size += l
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	n, failed := 0, 0
	for _, b := range batches {
		out, meta, err := ai.ExtractKPs.Run(ctx, s.ai, job.OwnerUserID, ai.KPIn{Subject: job.SubjectName, Pages: b})
		if apperr.IsKind(err, apperr.AIFailed) {
			failed++
			continue
		}
		if err != nil {
			return n, failed, err
		}
		s.notePrompt(ctx, job, ai.ExtractKPs.Name, meta)
		for _, k := range out.Points {
			d := KPDraft{Name: k.Name, OriginalText: k.OriginalText, KPPath: k.Path, RubricPoints: k.RubricPoints, SourcePage: k.Page}
			reasons := []string{}
			if len(d.RubricPoints) > 0 {
				reasons = append(reasons, ReasonRubricUnconfirmed)
			}
			if err := s.putItem(ctx, job, materialID, dbq.ImportItemsItemTypeKnowledgePoint, d, 0.9, reasons, "kp:"+strings.Join(k.Path, "/")+"/"+Normalize(k.Name)); err != nil {
				return n, failed, err
			}
			n++
		}
	}
	return n, failed, nil
}

// notePrompt 把用到的模型与提示词版本记在任务上，便于追溯（import_jobs.prompt_versions）。
func (s *Service) notePrompt(ctx context.Context, job dbq.GetImportJobRow, capability string, m ai.Meta) {
	cur, _ := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: job.ID, OwnerUserID: job.OwnerUserID})
	v := map[string]string{}
	_ = json.Unmarshal(cur.PromptVersions, &v)
	want := m.Model + "@" + m.Version
	if v[capability] == want {
		return
	}
	v[capability] = want
	b, _ := json.Marshal(v)
	_ = s.q.SetImportJobPromptVersions(ctx, dbq.SetImportJobPromptVersionsParams{PromptVersions: b, ID: job.ID, OwnerUserID: job.OwnerUserID})
}

// fail 把文件标为失败、退回预占的额度，然后看整个任务能不能进入整理。
func (s *Service) fail(ctx context.Context, jm dbq.ImportJobMaterial, reason string) error {
	if reason == "" {
		reason = "这份资料没能解析"
	}
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := s.settle(ctx, q, jm, 0); err != nil {
			return err
		}
		return q.UpdateImportJobMaterial(ctx, dbq.UpdateImportJobMaterialParams{Step: jm.Step, Status: dbq.ImportJobMaterialsStatusFailed,
			FailReason: sql.NullString{String: truncate(reason, 255), Valid: true}, Attempts: jm.Attempts,
			JobID: jm.JobID, MaterialID: jm.MaterialID, OwnerUserID: jm.OwnerUserID})
	})
	if err != nil {
		return err
	}
	if m, err := s.material.Get(ctx, jm.OwnerUserID, jm.MaterialID); err == nil && m.Status != dbq.MaterialsStatusRejected {
		if err := s.material.SetStatus(ctx, jm.OwnerUserID, jm.MaterialID, dbq.MaterialsStatusFailed, truncate(reason, 255)); err != nil {
			return err
		}
	}
	return s.enqueueFinish(ctx, jm.OwnerUserID, jm.JobID)
}

// GiveUp 在任务重试用完时调用。
func (s *Service) GiveUp(ctx context.Context, userID, jobID, materialID uint64) error {
	jm, err := s.jobMaterial(ctx, userID, jobID, materialID)
	if apperr.IsKind(err, apperr.NotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.fail(ctx, jm, "识别服务暂时不可用，没扣额度，请稍后点「重试」")
}

// finishFile 文件处理完：按计费页数结算额度，资料标为已解析（部分失败标 partial 并写原因）。
func (s *Service) finishFile(ctx context.Context, jm dbq.ImportJobMaterial, partial string) error {
	m, err := s.material.Get(ctx, jm.OwnerUserID, jm.MaterialID)
	if err != nil {
		return err
	}
	jstatus, mstatus := dbq.ImportJobMaterialsStatusDone, dbq.MaterialsStatusParsed
	if partial != "" {
		jstatus, mstatus = dbq.ImportJobMaterialsStatusPartial, dbq.MaterialsStatusPartial
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := s.settle(ctx, q, jm, min(int(m.BilledPages), int(jm.ReservedPages))); err != nil {
			return err
		}
		return q.UpdateImportJobMaterial(ctx, dbq.UpdateImportJobMaterialParams{Step: dbq.ImportJobMaterialsStepDone, Status: jstatus,
			FailReason: sql.NullString{String: partial, Valid: partial != ""}, Attempts: jm.Attempts,
			JobID: jm.JobID, MaterialID: jm.MaterialID, OwnerUserID: jm.OwnerUserID})
	})
	if err != nil {
		return err
	}
	if err := s.material.SetStatus(ctx, jm.OwnerUserID, jm.MaterialID, mstatus, partial); err != nil {
		return err
	}
	return s.enqueueFinish(ctx, jm.OwnerUserID, jm.JobID)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
