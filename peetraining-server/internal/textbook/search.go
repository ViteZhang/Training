package textbook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

type SearchHit struct {
	Ordinal      uint32  `json:"ordinal"`
	SectionNo    uint32  `json:"section_no"`
	StartRune    uint32  `json:"start_rune"`
	EndRune      uint32  `json:"end_rune"`
	Heading      string  `json:"heading"`
	Text         string  `json:"text"`
	TextHash     string  `json:"text_hash"`
	SourcePage   *int    `json:"source_page,omitempty"`
	PrintedPage  *string `json:"printed_page,omitempty"`
	FulltextRank int     `json:"fulltext_rank"`
	VectorRank   int     `json:"vector_rank"`
	FusionScore  float64 `json:"fusion_score"`
}
type SearchResult struct {
	GenerationID uint64      `json:"generation_id"`
	ProfileID    string      `json:"profile_id"`
	Fusion       string      `json:"fusion"`
	Hits         []SearchHit `json:"hits"`
	Notice       string      `json:"notice"`
}

// SearchRevision 为后台显式试用入口；APP 必须先取得已匹配的发布快照，不能调用此草稿入口。
func (s *Service) SearchRevision(ctx context.Context, book, edition, revision, generation uint64, query string) (SearchResult, error) {
	out := SearchResult{GenerationID: generation, Fusion: "rrf-v1", Hits: []SearchHit{}, Notice: "召回片段仅作检索诊断；相关性、依据充分性及答案引用由答案生成链路另行校验。"}
	if utf8.RuneCountInString(query) > 1800 || strings.TrimSpace(query) == "" {
		return out, apperr.New(apperr.BadRequest, "检索问题需为 1–1800 字")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, err := s.GetRevision(ctx, book, edition, revision)
	if err != nil {
		return out, err
	}
	q := dbq.New(s.db)
	e, err := q.GetTextbookEdition(ctx, dbq.GetTextbookEditionParams{ID: edition, BookID: book})
	if err != nil {
		return out, mapError(err)
	}
	if !validLicense(e, s.now().UTC()) {
		return out, apperr.New(apperr.BadRequest, "该版本授权不可用")
	}
	g, err := q.GetTextbookIndex(ctx, generation)
	if err != nil {
		return out, mapError(err)
	}
	if g.BookID != book || g.EditionID != edition || g.RevisionID != revision {
		return out, apperr.NotFoundErr()
	}
	if g.Status != "ready" || g.ContentHash != r.ContentHash || !r.ReviewedAt.Valid {
		return out, apperr.New(apperr.BadRequest, "本修订索引未就绪或已失效")
	}
	if s.indexes.Embedding == nil || g.ProfileHash != s.indexes.Embedding.Profile().ID() {
		return out, apperr.New(apperr.BadRequest, "查询与文档嵌入配置不兼容，请构建新代次")
	}
	out.ProfileID = g.ProfileHash
	var p rules.TextbookRetrievalParams
	if json.Unmarshal(g.SplitConfig, &p) != nil {
		return out, errors.New("索引配置损坏")
	}
	if err = rules.ValidateTextbookRetrieval(p); err != nil {
		return out, err
	}
	fulltext, err := q.SearchTextbookChunks(ctx, dbq.SearchTextbookChunksParams{Query: query, GenerationID: g.ID, Limit: int32(p.RecallK)})
	if err != nil {
		return out, err
	}
	embeddings, err := s.indexes.Embedding.Embed(ctx, []string{query})
	if err != nil {
		return out, err
	}
	if s.indexes.Vector == nil {
		return out, errors.New("向量服务未配置")
	}
	hits, err := s.indexes.Vector.Search(ctx, g.CollectionName, indexScope(g), embeddings[0], p.RecallK)
	if err != nil {
		return out, err
	}
	ft, vec := []uint32{}, []uint32{}
	for _, h := range fulltext {
		ft = append(ft, h.Ordinal)
	}
	for _, h := range hits {
		if h.ID < 1 || h.ID > uint64(g.ChunkCount) || math.IsInf(h.Score, 0) || math.IsNaN(h.Score) {
			return out, errors.New("向量结果无效")
		}
		vec = append(vec, uint32(h.ID))
	}
	used := 0
	for _, rank := range rules.FuseTextbookRanks(ft, vec) {
		chunk, err := q.GetTextbookChunk(ctx, dbq.GetTextbookChunkParams{GenerationID: g.ID, Ordinal: rank.Ordinal})
		if err != nil {
			return out, err
		}
		if chunk.SectionNo < 1 || int(chunk.SectionNo) > len(r.Sections) {
			return out, errors.New("片段定位损坏")
		}
		sec := r.Sections[chunk.SectionNo-1]
		runes := []rune(sec.Text)
		if chunk.EndRune > uint32(len(runes)) || chunk.StartRune >= chunk.EndRune || string(runes[chunk.StartRune:chunk.EndRune]) != chunk.Body {
			return out, errors.New("片段与校对原文不一致")
		}
		hash := sha256.Sum256([]byte(chunk.Body))
		if hex.EncodeToString(hash[:]) != chunk.TextHash {
			return out, errors.New("片段校验失败")
		}
		if used+int(chunk.EndRune-chunk.StartRune) > p.ContextRunes {
			continue
		}
		used += int(chunk.EndRune - chunk.StartRune)
		out.Hits = append(out.Hits, SearchHit{Ordinal: chunk.Ordinal, SectionNo: chunk.SectionNo, StartRune: chunk.StartRune, EndRune: chunk.EndRune, Heading: chunk.Heading, Text: chunk.Body, TextHash: chunk.TextHash, SourcePage: sec.SourcePage, PrintedPage: sec.PrintedPage, FulltextRank: rank.FulltextRank, VectorRank: rank.VectorRank, FusionScore: rank.Score})
		if len(out.Hits) >= p.ResultK {
			break
		}
	}
	// 长检索期间校对被修改或授权被撤销时拒绝返回旧草稿结果。
	current, err := q.GetTextbookRevision(ctx, dbq.GetTextbookRevisionParams{ID: revision, EditionID: edition})
	if err != nil {
		return out, err
	}
	e, err = q.GetTextbookEdition(ctx, dbq.GetTextbookEditionParams{ID: edition, BookID: book})
	if err != nil {
		return out, err
	}
	if current.ContentHash != r.ContentHash || !current.ReviewedAt.Valid || !validLicense(e, s.now().UTC()) {
		return out, apperr.New(apperr.Conflict, "原文或授权已变化，请重新检索")
	}
	return out, nil
}
