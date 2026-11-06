package official

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
)

// T52 重复试题：同一份真题卷传了两次、或几份资料里有同一道题时，在核对区标出来，由编辑决定驳回。
// 规则与用户端导入一致（importer 第 10 步）：同一年份下题干相同，或同题型且题干互相重合 90% 以上算重复；
// 不同年份考了同一题不算重复（真题本来就会重考），只提示「某年也考过」。

// dupThreshold 是两道题干互相覆盖的比例下限。
const dupThreshold = 0.9

// stemRef 是参与比对的一道题。
type stemRef struct {
	hash, qtype string
	year        int // 0 是不知道年份
	runes       []rune
	grams       map[string]struct{}
	label       string // 说明文字：「与《2025 真题》第 3 题相同」里「与」之后、「相同」之前的部分
	// 解析题的位置：只有排在前面的未核对题才算「已有的」，免得两道互相标重复。
	fileID  uint64
	seq     int
	pending bool
	itemID  uint64
}

func newStemRef(stem, qtype string, year *int) stemRef {
	r := stemRef{hash: importer.ContentHash(stem), qtype: qtype, runes: []rune(ai.Compact(stem))}
	if year != nil {
		r.year = *year
	}
	r.grams = make(map[string]struct{}, len(r.runes))
	for i := 0; i+1 < len(r.runes); i++ {
		r.grams[string(r.runes[i:i+2])] = struct{}{}
	}
	return r
}

// cover 是 a 的相邻两字有多少出现在 b 里（与 ai.Overlap 相同，用预先算好的集合加速）。
func cover(a, b stemRef) float64 {
	if len(a.runes) < 2 {
		if len(a.runes) == 1 && slices.Contains(b.runes, a.runes[0]) {
			return 1
		}
		return 0
	}
	hit := 0
	for i := 0; i+1 < len(a.runes); i++ {
		if _, ok := b.grams[string(a.runes[i:i+2])]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(a.runes)-1)
}

func sameStem(a, b stemRef) bool {
	if a.hash == b.hash {
		return true
	}
	if a.qtype != b.qtype || len(a.runes) == 0 || len(b.runes) == 0 {
		return false
	}
	// 互相覆盖 90% 以上时长度不会差太多，先按长度排除。
	la, lb := float64(len(a.runes)), float64(len(b.runes))
	if la < lb*0.5 || lb < la*0.5 {
		return false
	}
	return cover(a, b) >= dupThreshold && cover(b, a) >= dupThreshold
}

// sameYear 是两道题是否算同一年：有一方不知道年份时按同一年算（与用户端一致）。
func sameYear(a, b stemRef) bool {
	return a.year == 0 || b.year == 0 || a.year == b.year
}

// markDuplicates 给一份资料里还没核对的题标上重复说明与「也考过」的年份。
// 比对的范围：已发布的官方题、还没发布的新增题目草稿、项目里其他资料（以及本资料）还没驳回的解析题。
func (s *Service) markDuplicates(ctx context.Context, f dbq.OfficialImportFile, items []ImportItem) error {
	var mine []int
	for i, it := range items {
		if it.ItemType == string(dbq.OfficialImportItemsItemTypeQuestion) && it.Status == string(dbq.OfficialImportItemsStatusPending) {
			mine = append(mine, i)
		}
	}
	if len(mine) == 0 {
		return nil
	}
	proj, err := s.projectExists(ctx, f.ProjectID)
	if err != nil {
		return err
	}
	// 顺序就是优先级：先说已发布的，再说别的资料里的，最后是草稿。
	var refs []stemRef
	if proj.BankID.Valid {
		qs, err := s.q.ListOfficialQuestions(ctx, uint64(proj.BankID.Int64))
		if err != nil {
			return err
		}
		for _, q := range qs {
			if q.Status == "offline" {
				continue
			}
			var y *int
			if q.ExamYear.Valid {
				v := int(q.ExamYear.Int16)
				y = &v
			}
			r := newStemRef(q.Stem, string(q.Qtype), y)
			r.label = "已发布的官方题"
			refs = append(refs, r)
		}
	}
	imported, err := s.q.ListProjectImportQuestions(ctx, f.ProjectID)
	if err != nil {
		return err
	}
	for _, it := range imported {
		var p QuestionPayload
		if json.Unmarshal(it.Payload, &p) != nil {
			continue
		}
		r := newStemRef(p.Stem, p.QType, p.ExamYear)
		r.fileID, r.seq, r.itemID, r.pending = it.FileID, int(it.Seq), it.ID, it.Status == dbq.OfficialImportItemsStatusPending
		if it.FileID == f.ID {
			r.label = fmt.Sprintf("本资料第 %d 题", it.Seq)
		} else {
			r.label = fmt.Sprintf("《%s》第 %d 题", it.FileName, it.Seq)
		}
		refs = append(refs, r)
	}
	drafts, err := s.q.ListProjectQuestionDrafts(ctx, f.ProjectID)
	if err != nil {
		return err
	}
	for _, d := range drafts {
		var p QuestionPayload
		if json.Unmarshal(d.Payload, &p) != nil {
			continue
		}
		r := newStemRef(p.Stem, p.QType, p.ExamYear)
		r.label = fmt.Sprintf("草稿 #%d", d.ID)
		refs = append(refs, r)
	}

	for _, i := range mine {
		it := &items[i]
		var p QuestionPayload
		if json.Unmarshal(it.Payload, &p) != nil || p.Stem == "" {
			continue
		}
		me := newStemRef(p.Stem, p.QType, p.ExamYear)
		years := map[int]bool{}
		for _, r := range refs {
			if r.itemID == it.ID {
				continue
			}
			if !sameStem(me, r) {
				continue
			}
			if !sameYear(me, r) {
				years[r.year] = true
				continue
			}
			// 两道都还没核对时，只标排在后面的那道。
			if r.pending && (r.fileID > f.ID || (r.fileID == f.ID && r.seq > it.Seq)) {
				continue
			}
			if it.Duplicate == "" {
				it.Duplicate = "与" + r.label + "相同"
			}
		}
		for y := range years {
			it.SeenYears = append(it.SeenYears, y)
		}
		sort.Ints(it.SeenYears)
		if it.Duplicate != "" {
			it.Confidence = string(dbq.OfficialImportItemsConfidenceLow)
		}
	}
	return nil
}
