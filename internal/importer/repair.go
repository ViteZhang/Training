package importer

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/store"
)

// 补全真题信息（3.8）：早先的切题规则认不出「## 2024 北师大 808 中国语言文学真题」这类标题（年份后没有「年」字、
// 带 Markdown 符号），这样导入的真题没有年份、分值和专业分组，入库后算练习题，考情分析显示「还没有导入真题」。
// 这里用现在的切题规则重新切一遍资料原文（不调用模型、不扣额度），按题干对上题库里的题，只补空着的字段：
// 年份、分值、专业分组、共用材料；用户改过的题型、题干、答案和来源都不动。

// RepairResult 是补全的结果。
type RepairResult struct {
	Questions int   // 补全了的题数
	Years     []int // 涉及的真题年份（这些年份的真题卷已重新组卷）
	Papers    int   // 重新组好的真题卷数
}

// repairQ 是一道待补全的题（数据库行里匹配用到的字段）。
type repairQ struct {
	ID          uint64
	QType       string
	Stem        string
	ContentHash string
	HasYear     bool
	HasScore    bool
	HasGroup    bool
	HasPassage  bool
}

// repairFix 是对一道题要补的字段；零值表示不补。
type repairFix struct {
	ID          uint64
	Year        int
	Score       float64
	Group       string
	GroupMajors string
	Passage     string
}

// Repairable 返回一门课里按资料原文能补全的题数（不写库），考情分析据此提示「重新识别年份和分值」。
func (s *Service) Repairable(ctx context.Context, userID, subjectID uint64) (int, error) {
	pl, err := s.planRepairFor(ctx, userID, subjectID)
	return len(pl.fixes), err
}

type repairPlan struct {
	bank  dbq.GetSubjectBankRow
	fixes []repairFix
	cur   map[uint64]dbq.ListExamRepairCandidatesRow
}

func (s *Service) planRepairFor(ctx context.Context, userID, subjectID uint64) (repairPlan, error) {
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	b, err := s.q.GetSubjectBank(ctx, dbq.GetSubjectBankParams{ID: subjectID, OwnerUserID: userID, OwnerUserID_2: owner})
	if errors.Is(err, sql.ErrNoRows) {
		return repairPlan{}, apperr.NotFoundErr()
	}
	if err != nil {
		return repairPlan{}, err
	}
	pl := repairPlan{bank: b, cur: map[uint64]dbq.ListExamRepairCandidatesRow{}}
	cands, err := s.q.ListExamRepairCandidates(ctx, dbq.ListExamRepairCandidatesParams{BankID: b.BankID, OwnerUserID: owner})
	if err != nil || len(cands) == 0 {
		return pl, err
	}
	keys, err := s.q.ListBankExamKeys(ctx, dbq.ListBankExamKeysParams{BankID: b.BankID, OwnerUserID: owner})
	if err != nil {
		return pl, err
	}
	taken := map[string]bool{}
	for _, k := range keys {
		taken[examKey(k.ContentHash, int(k.ExamYear.Int16))] = true
	}
	byMaterial := map[uint64][]repairQ{}
	var order []uint64
	for _, c := range cands {
		mid := uint64(c.SourceMaterialID.Int64)
		if _, ok := byMaterial[mid]; !ok {
			order = append(order, mid)
		}
		byMaterial[mid] = append(byMaterial[mid], repairQ{ID: c.ID, QType: string(c.Qtype), Stem: c.Stem, ContentHash: c.ContentHash,
			HasYear: c.ExamYear.Valid, HasScore: c.Score.Valid, HasGroup: c.GroupLabel.Valid && c.GroupLabel.String != "",
			HasPassage: c.Passage.Valid && strings.TrimSpace(c.Passage.String) != ""})
		pl.cur[c.ID] = c
	}
	for _, mid := range order {
		pages, err := s.q.ListMaterialPages(ctx, dbq.ListMaterialPagesParams{MaterialID: mid, OwnerUserID: userID})
		if err != nil {
			return pl, err
		}
		in := make([]Page, len(pages))
		for i, p := range pages {
			in[i] = Page{No: int(p.PageNo), Text: p.Text}
		}
		pl.fixes = append(pl.fixes, planRepair(byMaterial[mid], Split(in), taken)...)
	}
	return pl, nil
}

// RepairExamInfo 按资料原文补全一门课里缺年份、分值的题，并重新组卷、重算知识点的真题次数。可重复执行。
func (s *Service) RepairExamInfo(ctx context.Context, userID, subjectID uint64) (RepairResult, error) {
	pl, err := s.planRepairFor(ctx, userID, subjectID)
	if err != nil || len(pl.fixes) == 0 {
		return RepairResult{}, err
	}
	b, fixes, cur := pl.bank, pl.fixes, pl.cur
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}

	var res RepairResult
	years := map[int]bool{}
	mats := map[uint64]bool{}
	job := dbq.GetImportJobRow{OwnerUserID: userID, BankID: b.BankID, SubjectID: sql.NullInt64{Int64: int64(subjectID), Valid: true},
		SubjectName: b.Name, SubjectFullScore: b.FullScore}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		for _, f := range fixes {
			c := cur[f.ID]
			p := dbq.FillQuestionExamInfoParams{Source: c.Source, ExamYear: c.ExamYear, Score: c.Score, GroupLabel: c.GroupLabel,
				GroupMajors: c.GroupMajors, Passage: c.Passage, PassageKey: sql.NullString{}, ID: f.ID, OwnerUserID: owner}
			if f.Year > 0 {
				p.ExamYear = sql.NullInt16{Int16: int16(f.Year), Valid: true}
			}
			if f.Score > 0 {
				p.Score = sql.NullString{String: decimal(f.Score), Valid: true}
			}
			if f.Group != "" {
				p.GroupLabel = sql.NullString{String: truncate(f.Group, 32), Valid: true}
				p.GroupMajors = sql.NullString{String: truncate(f.GroupMajors, 255), Valid: f.GroupMajors != ""}
			}
			if f.Passage != "" {
				p.Passage = sql.NullString{String: f.Passage, Valid: true}
			}
			if p.Passage.Valid {
				p.PassageKey = sql.NullString{String: PassageKey(p.Passage.String), Valid: true}
			}
			if err := q.FillQuestionExamInfo(ctx, p); err != nil {
				return err
			}
			res.Questions++
			if p.Source == dbq.QuestionsSourceExam && p.ExamYear.Valid {
				years[int(p.ExamYear.Int16)] = true
			}
			mats[uint64(c.SourceMaterialID.Int64)] = true
		}
		for y := range years {
			res.Years = append(res.Years, y)
		}
		sort.Ints(res.Years)
		for _, y := range res.Years {
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
		return q.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: b.BankID, OwnerUserID: owner})
	})
	if err != nil {
		return RepairResult{}, err
	}
	if s.AfterConfirm != nil && len(res.Years) > 0 {
		s.AfterConfirm(ctx, userID, subjectID)
	}
	return res, nil
}

// RebuildRealExamPapers refreshes current paper composition after a confirmed source or year edit.
func (s *Service) RebuildRealExamPapers(ctx context.Context, userID, subjectID uint64, years ...int) error {
	if len(years) == 0 {
		return nil
	}
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	b, err := s.q.GetSubjectBank(ctx, dbq.GetSubjectBankParams{ID: subjectID, OwnerUserID: userID, OwnerUserID_2: owner})
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	job := dbq.GetImportJobRow{OwnerUserID: userID, BankID: b.BankID, SubjectID: sql.NullInt64{Int64: int64(subjectID), Valid: true},
		SubjectName: b.Name, SubjectFullScore: b.FullScore}
	seen := map[int]bool{}
	return store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		for _, year := range years {
			if year <= 0 || seen[year] {
				continue
			}
			seen[year] = true
			if _, err := s.buildPaper(ctx, q, job, year); err != nil {
				return err
			}
		}
		return nil
	})
}

func examKey(hash string, year int) string { return hash + ":" + strconv.Itoa(year) }

// planRepair 给一份资料里待补全的题找对应的块，算出要补的字段。taken 是题库里已有的真题（内容哈希 + 年份）：
// 补上年份后会与已有真题重复的题（多是后来又导入了一遍）不补，免得同一道真题算两次；补上的会加进 taken。
func planRepair(qs []repairQ, blocks []ai.Block, taken map[string]bool) []repairFix {
	var out []repairFix
	// 多年合在一份资料里时同一道题（如两年都考「意境」）会出现多次；每块先只给一道题用，
	// 否则后面的题都会对上第一年的块，补成同一年后又被当作重复跳过。
	used := map[int]bool{}
	for _, q := range qs {
		i, ok := matchBlock(q, blocks, used)
		if !ok {
			continue
		}
		used[i] = true
		blk := blocks[i]
		f := repairFix{ID: q.ID}
		if !q.HasYear && blk.Year > 0 {
			if taken[examKey(q.ContentHash, blk.Year)] {
				continue
			}
			f.Year = blk.Year
		}
		if !q.HasYear && f.Year == 0 {
			continue // 没识别出年份就不是能统计的真题，分组、材料也不动
		}
		if !q.HasScore && blk.Score > 0 {
			f.Score = blk.Score
		}
		if !q.HasGroup && blk.Group != "" {
			f.Group, f.GroupMajors = blk.Group, blk.GroupMajors
		}
		if !q.HasPassage && blk.Passage != "" {
			f.Passage = blk.Passage
		}
		if f.Year == 0 && f.Score == 0 && f.Group == "" && f.Passage == "" {
			continue
		}
		if f.Year > 0 {
			taken[examKey(q.ContentHash, f.Year)] = true
		}
		out = append(out, f)
	}
	return out
}

// matchBlock 按题干找对应的块：题干与块文字要互相大部分重合（短题干如「神话」只会对上「5. 神话」那一块，
// 不会对上题干里恰好提到神话的长题），同样重合时优先题型相同、还没被别的题用过的块；返回块的下标。
func matchBlock(q repairQ, blocks []ai.Block, used map[int]bool) (int, bool) {
	stem := stemForMatch(q.Stem)
	if stem == "" {
		return 0, false
	}
	best, bestScore := 0, 0.0
	for i, b := range blocks {
		if b.Answer || b.Whole {
			continue
		}
		text := reLeadNo.ReplaceAllString(b.Text, "")
		in, cover := ai.Overlap(stem, text), ai.Overlap(text, stem)
		if in < 0.8 || cover < 0.6 {
			continue
		}
		score := in + cover
		if b.QType != "" && b.QType == q.QType {
			score += 0.01
		}
		if !used[i] {
			score += 0.1
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	return best, bestScore > 0
}

// stemForMatch 去掉旧规则并进题干的标题行（「### 二、简答题（每题 10 分）」「**第二组：请选择……考生作答**」），只留题目文字。
func stemForMatch(stem string) string {
	var keep []string
	for _, line := range strings.Split(stem, "\n") {
		t := cleanLine(line)
		if t == "" {
			continue
		}
		if _, ok := isSectionHead(t); ok {
			continue
		}
		if reGroupHead.MatchString(t) && isGroupHead(t) {
			continue
		}
		keep = append(keep, t)
	}
	return reLeadNo.ReplaceAllString(strings.Join(keep, "\n"), "")
}
