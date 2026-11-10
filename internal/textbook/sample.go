package textbook

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

// maxSampleExcerpts 是批量出体验题时最多取的原文片段数（所有关联的书合计），按相关度从高到低。
const maxSampleExcerpts = 12

// SampleMaterial 取后台「AI 批量生成体验题」关联的已发布资料：书名（存到题目的参考教材）和按方向检索到的原文片段。
// 只认当前可检索的已发布版本；选了不能用的版本直接报错，免得编辑以为题目依据了教材。
func (s *Service) SampleMaterial(ctx context.Context, editionIDs []uint64, query string) ([]string, []string, error) {
	if len(editionIDs) == 0 {
		return nil, nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	rows, err := dbq.New(s.db).ListPublishedTextbookEditions(ctx, sql.NullTime{Time: s.now().UTC(), Valid: true})
	if err != nil {
		return nil, nil, err
	}
	var editions []dbq.ListPublishedTextbookEditionsRow
	seen := map[uint64]bool{}
	for _, id := range editionIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, e := range rows {
			if e.ID == id && e.CurrentRevisionID.Valid && e.CurrentGenerationID.Valid {
				editions = append(editions, e)
				break
			}
		}
	}
	if details, _ := s.unavailableEditions(ctx, editionIDs, editions); len(details) > 0 {
		return nil, nil, apperr.New(apperr.BadRequest, "所选教材暂不能用于出题："+strings.Join(details, "；"))
	}
	if len(editions) < len(seen) {
		return nil, nil, apperr.New(apperr.BadRequest, "所选教材不存在")
	}
	names := make([]string, len(editions))
	for i, e := range editions {
		names[i] = strings.TrimSpace(e.Title + " " + e.Author + " " + e.Edition)
	}
	type candidate struct {
		text  string
		score float64
		order int
	}
	var list []candidate
	var failed []string
	for i, r := range s.searchEditions(ctx, editions, query) {
		if r.err != nil {
			failed = append(failed, editions[i].Title+"："+errText(r.err))
			continue
		}
		score := bookOrderScores(r.result.Hits)
		for j, h := range r.result.Hits {
			text := h.Text
			if h.Heading != "" {
				text = "【" + editions[i].Title + " · " + h.Heading + "】" + text
			} else {
				text = "【" + editions[i].Title + "】" + text
			}
			list = append(list, candidate{text: text, score: score[j], order: len(list)})
		}
	}
	if len(list) == 0 {
		if len(failed) > 0 {
			return nil, nil, apperr.New(apperr.BadRequest, "教材检索失败："+strings.Join(failed, "；"))
		}
		return nil, nil, apperr.New(apperr.BadRequest, "所选教材里没有检索到和这个方向相关的内容，请换一本教材或不关联教材")
	}
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].score != list[b].score {
			return list[a].score > list[b].score
		}
		return list[a].order < list[b].order
	})
	excerpts := []string{}
	for _, c := range list[:min(len(list), maxSampleExcerpts)] {
		excerpts = append(excerpts, c.text)
	}
	return names, excerpts, nil
}
