package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/store"
)

// 按专业分组的论述题（「第一组：请选择文艺学专业的考生作答」，回答非报考专业不得分）：
// 1.7 让用户选报考方向，只保留这一组；选择记在专业课上（subjects.my_majors），下次导入同一门课时默认选好，
// 真题卷每个分组只按这一组计分。

// GroupSummary 是任务里一个分组的概况（1.7 顶部的分组卡）。
type GroupSummary struct {
	Label   string
	Majors  string
	Total   int  // 这一组识别出的题数
	Kept    int  // 还没删除的题数
	Matches bool // 与专业课上记的报考方向匹配
}

var majorSeps = strings.NewReplacer("、", "|", "，", "|", ",", "|", "/", "|", "／", "|", "；", "|", ";", "|", "和", "|", "及", "|", " ", "|")

// majorTokens 把「中国古代文学、中国古典文献学专业」拆成专业名，去掉「专业」「方向」等后缀。
func majorTokens(s string) []string {
	var out []string
	for _, t := range strings.Split(majorSeps.Replace(s), "|") {
		t = strings.TrimSpace(t)
		for _, suf := range []string{"专业", "方向"} {
			t = strings.TrimSuffix(t, suf)
		}
		if len([]rune(t)) >= 2 {
			out = append(out, t)
		}
	}
	return out
}

// MatchMajors 判断报考方向与分组适用专业是否对得上：任意两个专业名互相包含即可（「古代文学」对「中国古代文学」）。
func MatchMajors(mine, group string) bool {
	for _, a := range majorTokens(mine) {
		for _, b := range majorTokens(group) {
			if strings.Contains(a, b) || strings.Contains(b, a) {
				return true
			}
		}
	}
	return false
}

func (s *Service) myMajors(ctx context.Context, userID uint64, subjectID sql.NullInt64) (string, error) {
	if !subjectID.Valid {
		return "", nil
	}
	sub, err := s.q.GetSubject(ctx, dbq.GetSubjectParams{ID: uint64(subjectID.Int64), OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return sub.MyMajors.String, err
}

// Groups 返回任务里按专业分组的题的概况，按原卷顺序；没有分组时为空。
func (s *Service) Groups(ctx context.Context, userID, jobID uint64) ([]GroupSummary, error) {
	job, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: jobID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundErr()
	}
	if err != nil {
		return nil, err
	}
	items, err := s.q.ListJobItemsWithDeleted(ctx, dbq.ListJobItemsWithDeletedParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return nil, err
	}
	mine, err := s.myMajors(ctx, userID, job.SubjectID)
	if err != nil {
		return nil, err
	}
	var out []GroupSummary
	idx := map[string]int{}
	for _, it := range items {
		g := itemGroup(it)
		if g == nil {
			continue
		}
		i, ok := idx[g.Label]
		if !ok {
			i = len(out)
			idx[g.Label] = i
			out = append(out, GroupSummary{Label: g.Label, Majors: g.Majors, Matches: mine != "" && MatchMajors(mine, g.Majors)})
		}
		out[i].Total++
		if it.Status != dbq.ImportItemsStatusDeleted {
			out[i].Kept++
		}
	}
	return out, nil
}

func itemGroup(it dbq.ImportItem) *GroupInfo {
	if it.ItemType != dbq.ImportItemsItemTypeQuestion {
		return nil
	}
	var d QuestionDraft
	if json.Unmarshal(it.Payload, &d) != nil || d.Group == nil || d.Group.Label == "" {
		return nil
	}
	return d.Group
}

// KeepGroups 只保留选中的分组（1.7「选你报考的方向」）：其他组还没入库的题标为删除，选中组之前删掉的题恢复；
// 选中组的适用专业记为这门课的报考方向。labels 为空表示全部保留（不区分方向）。返回这次删掉与恢复的条数。
func (s *Service) KeepGroups(ctx context.Context, userID, jobID uint64, labels []string) (deleted, restored int, err error) {
	job, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: jobID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, apperr.NotFoundErr()
	}
	if err != nil {
		return 0, 0, err
	}
	items, err := s.q.ListJobItemsWithDeleted(ctx, dbq.ListJobItemsWithDeletedParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return 0, 0, err
	}
	known := map[string]string{}
	for _, it := range items {
		if g := itemGroup(it); g != nil {
			known[g.Label] = g.Majors
		}
	}
	var majors []string
	for _, l := range labels {
		m, ok := known[l]
		if !ok {
			return 0, 0, apperr.New(apperr.BadRequest, "没有这个分组："+l)
		}
		if m != "" && !slices.Contains(majors, m) {
			majors = append(majors, m)
		}
	}
	all := len(labels) == 0
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		for _, it := range items {
			g := itemGroup(it)
			if g == nil || it.CreatedEntityID.Valid {
				continue
			}
			keep := all || slices.Contains(labels, g.Label)
			var st dbq.ImportItemsStatus
			switch {
			case keep && it.Status == dbq.ImportItemsStatusDeleted:
				st = dbq.ImportItemsStatusPending
				restored++
			case !keep && it.Status != dbq.ImportItemsStatusDeleted:
				st = dbq.ImportItemsStatusDeleted
				deleted++
			default:
				continue
			}
			if err := q.UpdateImportItem(ctx, dbq.UpdateImportItemParams{Payload: it.Payload, Status: st, NeedsReview: it.NeedsReview,
				ReviewReasons: it.ReviewReasons, DuplicateOfQuestionID: it.DuplicateOfQuestionID, ID: it.ID, OwnerUserID: userID}); err != nil {
				return err
			}
		}
		if job.SubjectID.Valid {
			v := sql.NullString{String: truncate(strings.Join(majors, "、"), 255), Valid: len(majors) > 0}
			return q.SetSubjectMajors(ctx, dbq.SetSubjectMajorsParams{MyMajors: v, ID: uint64(job.SubjectID.Int64), OwnerUserID: userID})
		}
		return nil
	})
	return deleted, restored, err
}
