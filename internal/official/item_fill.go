package official

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

// 单题「生成答案 / 重新生成」放到 Worker 里执行：检索教材、生成、核对引用加起来常常超过一分钟，
// 同步接口会被反向代理按超时断开（后台显示网络错误，答案也没写回）。接口只排队并在题目上记下排队时间，
// 核对区显示「生成中」并自动刷新，生成完或失败（原因记在条目上）后标记去掉。

// itemFillStale 是排队后多久还没生成完就当作中断（Worker 重启丢了任务），编辑可以再点。
const itemFillStale = 15 * time.Minute

// fillingAt 是这道题在 now 时是否还在排队生成。
func fillingAt(p QuestionPayload, now time.Time) bool {
	return p.FillQueuedAt != nil && now.Sub(*p.FillQueuedAt) < itemFillStale
}

// asyncItemFill 是否把单题生成放到 Worker（没有队列时同步执行：本地与测试）。
func (s *Service) asyncItemFill() bool {
	return s.imports.Queue != nil && s.imports.NewItemFillTask != nil
}

// queueItemFill 在题目上记下排队时间（regenerate 时同时打上重新生成标记）并排任务，返回排队后的条目。
func (s *Service) queueItemFill(ctx context.Context, it dbq.OfficialImportItem, p QuestionPayload, regenerate bool) (ImportItem, error) {
	now := s.now().UTC()
	p.FillQueuedAt = &now
	if regenerate {
		p.Regenerate = true
	}
	b, _ := json.Marshal(p)
	n, err := s.q.UpdateOfficialImportItemFill(ctx, dbq.UpdateOfficialImportItemFillParams{Payload: b, Citations: it.Citations,
		Confidence: it.Confidence, ID: it.ID})
	if err != nil {
		return ImportItem{}, err
	}
	if n == 0 {
		return ImportItem{}, apperr.New(apperr.Conflict, "这一条已经核对过了")
	}
	t, err := s.imports.NewItemFillTask(it.ID, regenerate)
	if err == nil {
		_, err = s.imports.Queue.EnqueueContext(ctx, t)
	}
	if err != nil {
		// 没排上：恢复原样，编辑可以再点。
		_, _ = s.q.UpdateOfficialImportItemFill(ctx, dbq.UpdateOfficialImportItemFillParams{Payload: it.Payload, Citations: it.Citations,
			Confidence: it.Confidence, FillError: it.FillError, ID: it.ID})
		return ImportItem{}, err
	}
	it, err = s.q.GetOfficialImportItem(ctx, it.ID)
	if err != nil {
		return ImportItem{}, err
	}
	return toImportItem(it), nil
}

// ProcessImportItemFill 执行单题生成（Worker 调用）。只处理还带着排队标记的题：已经核对、已经生成完的不再处理，
// 所以重复执行没有副作用。失败时原因记在条目上，原来的答案不变；不重试，编辑可以再点。
func (s *Service) ProcessImportItemFill(ctx context.Context, itemID uint64, regenerate bool) error {
	it, err := s.q.GetOfficialImportItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if it.Status != dbq.OfficialImportItemsStatusPending || it.ItemType != dbq.OfficialImportItemsItemTypeQuestion {
		return nil
	}
	var p QuestionPayload
	_ = json.Unmarshal(it.Payload, &p) // 格式不对的题没有排队标记，下面直接跳过
	if p.FillQueuedAt == nil {
		return nil
	}
	// 失败时写回的内容：去掉排队标记；重新生成失败的保留原来的答案，不再标成待重新生成。
	p.FillQueuedAt = nil
	if regenerate {
		p.Regenerate = false
	}
	it.Payload, _ = json.Marshal(p)
	p.Regenerate = p.Regenerate || regenerate
	defaults, err := s.defaultScores(ctx, it.ProjectID)
	if err == nil {
		err = s.fillItemWith(ctx, it, p, defaults)
	} else {
		_, _ = s.q.UpdateOfficialImportItemFill(ctx, dbq.UpdateOfficialImportItemFillParams{Payload: it.Payload, Citations: it.Citations,
			Confidence: it.Confidence, FillError: nullStr(fillErrorText(err)), ID: it.ID})
	}
	if err != nil {
		s.d.Log.WarnContext(ctx, "official import item fill failed", "item_id", itemID, "regenerate", regenerate, "err", err)
	}
	return nil
}
