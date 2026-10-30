package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"unicode"
)

// TextbookRetrievalParams 是官方教材库 5.4/7.2 的版本化检索配置，需经真实评测校准。
type TextbookRetrievalParams struct {
	Version      string `json:"version"`
	ChunkRunes   int    `json:"chunk_runes"`
	OverlapRunes int    `json:"overlap_runes"`
	RecallK      int    `json:"recall_k"`
	ResultK      int    `json:"result_k"`
	ContextRunes int    `json:"context_runes"`
}

func ValidateTextbookRetrieval(p TextbookRetrievalParams) error {
	if strings.TrimSpace(p.Version) == "" || len(p.Version) > 32 || p.ChunkRunes < 200 || p.ChunkRunes > 1500 || p.OverlapRunes < 0 || p.OverlapRunes > 300 || p.OverlapRunes >= p.ChunkRunes/2 || p.RecallK < 1 || p.RecallK > 50 || p.ResultK < 1 || p.ResultK > 20 || p.ResultK > p.RecallK || p.ContextRunes < 1000 || p.ContextRunes > 16000 {
		return errors.New("教材检索参数超出允许范围")
	}
	return nil
}

type TextbookChunk struct {
	Ordinal, Section, Start, End int
	Heading, Text, Hash          string
}

func TextbookChunks(sections []TextbookSection, p TextbookRetrievalParams) ([]TextbookChunk, error) {
	if err := ValidateTextbookRetrieval(p); err != nil {
		return nil, err
	}
	out := []TextbookChunk{}
	for i, s := range sections {
		r := []rune(s.Text)
		for start := 0; start < len(r); {
			end := min(start+p.ChunkRunes, len(r))
			if end < len(r) {
				for j := end; j > start+p.ChunkRunes/2; j-- {
					if strings.ContainsRune("。！？；\n", r[j-1]) {
						end = j
						break
					}
				}
			}
			text := string(r[start:end])
			hash := sha256.Sum256([]byte(text))
			// 不丢弃全空白范围，保证定位可以精确还原；合法单元含正文，空白片段用确定性合并边界规避。
			if strings.TrimSpace(text) == "" {
				return nil, errors.New("原文包含超长空白单元，请先整理校对")
			}
			out = append(out, TextbookChunk{len(out) + 1, i + 1, start, end, s.Heading, text, hex.EncodeToString(hash[:])})
			if len(out) > 20000 {
				return nil, errors.New("单修订片段数超过 20000，请按章节拆为多个讲义版本")
			}
			if end == len(r) {
				break
			}
			start = end - p.OverlapRunes
		}
	}
	if len(out) == 0 {
		return nil, errors.New("没有可索引正文")
	}
	return out, nil
}

type TextbookRank struct {
	Ordinal                  uint32
	FulltextRank, VectorRank int
	Score                    float64
}

// FuseTextbookRanks 只融合排名，不把全文分数和向量距离直接相加当概率。PRD 7.2。
func FuseTextbookRanks(fulltext, vector []uint32) []TextbookRank {
	m := map[uint32]*TextbookRank{}
	for list, ids := range [][]uint32{fulltext, vector} {
		seen := map[uint32]bool{}
		for i, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			r := m[id]
			if r == nil {
				r = &TextbookRank{Ordinal: id}
				m[id] = r
			}
			if list == 0 {
				r.FulltextRank = i + 1
			} else {
				r.VectorRank = i + 1
			}
			r.Score += 1 / float64(60+i+1)
		}
	}
	out := make([]TextbookRank, 0, len(m))
	for _, r := range m {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Ordinal < out[j].Ordinal
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// NormalizeTextbookName 仅做书名字符标准化，不对近似同名或版次作猜测。
func NormalizeTextbookName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("《》〈〉", r) {
			return -1
		}
		if r >= 'Ａ' && r <= 'Ｚ' || r >= 'ａ' && r <= 'ｚ' || r >= '０' && r <= '９' {
			r -= 0xfee0
		}
		return unicode.ToLower(r)
	}, s)
}
