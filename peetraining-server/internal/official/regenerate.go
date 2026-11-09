package official

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

// T53 按关联教材重新生成：先导入、后关联教材时，已经由 AI 生成的答案没用上教材，
// 编辑可以逐题或整份重新生成。只换 AI 生成的答案（原文给的答案不动），原文给的采分点保留。

// regenerable 是还没核对、答案由 AI 生成的题。
func regenerable(it dbq.OfficialImportItem) (QuestionPayload, bool) {
	var p QuestionPayload
	if it.Status != dbq.OfficialImportItemsStatusPending || it.ItemType != dbq.OfficialImportItemsItemTypeQuestion {
		return p, false
	}
	if json.Unmarshal(it.Payload, &p) != nil {
		return p, false
	}
	return p, p.AnswerAI && strings.TrimSpace(p.Answer) != ""
}

// RegenerateImportItem 重新生成一道题的 AI 答案（有队列时在 Worker 里执行，见 item_fill.go）；失败时原来的答案不变，原因记在条目上。
func (s *Service) RegenerateImportItem(ctx context.Context, projectID, itemID uint64) (ImportItem, error) {
	it, err := s.q.GetOfficialImportItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && it.ProjectID != projectID) {
		return ImportItem{}, apperr.NotFoundErr()
	}
	if err != nil {
		return ImportItem{}, err
	}
	if it.Status != dbq.OfficialImportItemsStatusPending {
		return ImportItem{}, apperr.New(apperr.Conflict, "这一条已经核对过了")
	}
	p, ok := regenerable(it)
	if !ok {
		return ImportItem{}, apperr.New(apperr.BadRequest, "只有 AI 生成的答案可以重新生成")
	}
	f, err := s.q.GetOfficialImportFile(ctx, it.FileID)
	if err != nil {
		return ImportItem{}, err
	}
	if f.FillStatus == dbq.OfficialImportFilesFillStatusRunning {
		return ImportItem{}, apperr.New(apperr.Conflict, "正在补全，请稍候")
	}
	if fillingAt(p, s.now()) {
		return ImportItem{}, apperr.New(apperr.Conflict, "这道题正在生成答案，请稍候")
	}
	if s.asyncItemFill() {
		return s.queueItemFill(ctx, it, p, true)
	}
	defaults, err := s.defaultScores(ctx, projectID)
	if err != nil {
		return ImportItem{}, err
	}
	p.Regenerate = true
	if err := s.fillItemWith(ctx, it, p, defaults); err != nil {
		return ImportItem{}, syncFillError(err)
	}
	it, err = s.q.GetOfficialImportItem(ctx, itemID)
	if err != nil {
		return ImportItem{}, err
	}
	return toImportItem(it), nil
}

// RegenerateImportFile 整份重新生成：这份资料里还没核对、答案由 AI 生成的题都按关联教材重新生成，
// 与「一键补全」共用 Worker 任务与进度（缺答案、缺采分点的题一起补上）。
func (s *Service) RegenerateImportFile(ctx context.Context, projectID, fileID uint64) (ImportFile, error) {
	f, err := s.importFile(ctx, projectID, fileID)
	if err != nil {
		return ImportFile{}, err
	}
	if f.Status != dbq.OfficialImportFilesStatusParsed {
		return ImportFile{}, apperr.New(apperr.Conflict, "资料还没解析完")
	}
	if f.FillStatus == dbq.OfficialImportFilesFillStatusRunning {
		return ImportFile{}, apperr.New(apperr.Conflict, "正在补全，请稍候")
	}
	rows, err := s.q.ListOfficialImportItems(ctx, fileID)
	if err != nil {
		return ImportFile{}, err
	}
	var marks []dbq.OfficialImportItem
	total := 0
	for _, r := range rows {
		if p, ok := regenerable(r); ok {
			if fillingAt(p, s.now()) {
				continue // 单题生成中的交给它自己的任务
			}
			if !p.Regenerate {
				marks = append(marks, r)
			}
			total++
		} else if r.Status == dbq.OfficialImportItemsStatusPending && r.ItemType == dbq.OfficialImportItemsItemTypeQuestion {
			var p QuestionPayload
			if json.Unmarshal(r.Payload, &p) == nil && needsFill(p) {
				total++
			}
		}
	}
	if total == 0 {
		return ImportFile{}, apperr.New(apperr.BadRequest, "没有 AI 生成的答案可以重新生成")
	}
	// 先占住补全状态，再打标记：并发点两次时只有一次能开始。
	n, err := s.q.StartOfficialImportFill(ctx, dbq.StartOfficialImportFillParams{FillTotal: uint32(total), ID: fileID})
	if err != nil {
		return ImportFile{}, err
	}
	if n == 0 {
		return ImportFile{}, apperr.New(apperr.Conflict, "正在补全，请稍候")
	}
	for _, r := range marks {
		var p QuestionPayload
		_ = json.Unmarshal(r.Payload, &p)
		p.Regenerate = true
		b, _ := json.Marshal(p)
		if _, err := s.q.UpdateOfficialImportItemFill(ctx, dbq.UpdateOfficialImportItemFillParams{Payload: b, Citations: r.Citations,
			Confidence: r.Confidence, ID: r.ID}); err != nil {
			_ = s.q.FinishOfficialImportFill(ctx, fileID)
			return ImportFile{}, err
		}
	}
	if err := s.enqueueFill(ctx, fileID); err != nil {
		_ = s.q.FinishOfficialImportFill(ctx, fileID)
		return ImportFile{}, err
	}
	d, err := s.ImportItems(ctx, projectID, fileID)
	if err != nil {
		return ImportFile{}, err
	}
	return d.File, nil
}
