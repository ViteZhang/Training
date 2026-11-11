package official

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/notify"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// Bank 是 App 里可添加的官方题库。
type Bank struct {
	BankID        uint64
	Title         string
	School        string
	Major         string
	SubjectCode   string
	SubjectName   string
	Version       string
	KPCount       int
	QuestionCount int
	// AddedTo 是已添加到哪门专业课（0 表示没添加）；Suggested 是代码相同、建议添加的专业课。
	AddedTo   uint64
	Suggested uint64
	// UpdatedAt 是最近一次发布的时间；IsNew 表示 NewWindow 内上线或更新过（App 标「新」）。
	UpdatedAt time.Time
	IsNew     bool
}

// NewWindow 是官方题库标「新」的时长。
const NewWindow = 7 * 24 * time.Hour

// Banks 列出已发布的官方题库，标出用户添加到了哪门课、建议添加到哪门课（专业课代码相同）。
func (s *Service) Banks(ctx context.Context, userID uint64) ([]Bank, error) {
	rows, err := s.q.ListPublishedOfficialBanks(ctx)
	if err != nil {
		return nil, err
	}
	subs, err := s.q.ListMySubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	added := map[uint64]uint64{}
	for _, sb := range subs {
		added[sb.BankID] = sb.SubjectID
	}
	subjects, err := s.q.ListSubjects(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Bank, 0, len(rows))
	for _, r := range rows {
		b := Bank{BankID: r.BankID, Title: r.Title, School: r.School, Major: r.Major, SubjectCode: r.SubjectCode.String, SubjectName: r.SubjectName,
			Version: str(r.Version), KPCount: int(r.KpCount), QuestionCount: int(r.QuestionCount), AddedTo: added[r.BankID],
			UpdatedAt: r.UpdatedAt, IsNew: now.Sub(r.UpdatedAt) < NewWindow}
		for _, sj := range subjects {
			if sj.Code.Valid && sj.Code.String == r.SubjectCode.String {
				b.Suggested = sj.ID
				break
			}
		}
		out = append(out, b)
	}
	return out, nil
}

func str(v any) string {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return ""
	}
}

// Subscribe 把官方题库添加到用户的一门专业课（11.15）：复制官方内容到这门课的题库，同名知识点合并。
func (s *Service) Subscribe(ctx context.Context, userID, bankID, subjectID uint64) error {
	// 只有发布上线了的官方题库才能添加。
	if p, err := s.q.GetOfficialProjectByBank(ctx, sql.NullInt64{Int64: int64(bankID), Valid: true}); errors.Is(err, sql.ErrNoRows) || (err == nil && p.Stage != dbq.OfficialProjectsStagePublished) {
		return apperr.NotFoundErr()
	} else if err != nil {
		return err
	}
	if _, err := s.q.GetBankForSubject(ctx, dbq.GetBankForSubjectParams{ID: subjectID, OwnerUserID: userID, OwnerUserID_2: sql.NullInt64{Int64: int64(userID), Valid: true}}); errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	} else if err != nil {
		return err
	}
	if _, err := s.q.GetSubscription(ctx, dbq.GetSubscriptionParams{OwnerUserID: userID, BankID: bankID}); err == nil {
		return apperr.New(apperr.Conflict, "已经添加过这个官方题库")
	}
	if err := s.q.InsertSubscription(ctx, dbq.InsertSubscriptionParams{OwnerUserID: userID, BankID: bankID, SubjectID: subjectID}); err != nil {
		return err
	}
	// 整套复制逐条写库，题多时（生产跨机房访问数据库）要几十秒。不跟着请求取消：
	// App 等不及断开后这里照样复制完，用户再点一次得到「已经添加过」，直接往下走；
	// 否则断开会让事务回滚、订阅被撤掉，每次重试都从头再来、永远失败。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), SubscribeTimeout)
	defer cancel()
	if _, err := s.Sync(ctx, userID, bankID, true); err != nil {
		// 复制失败时撤掉订阅，用户可以重试；已复制的部分一并清掉。
		_ = s.Unsubscribe(ctx, userID, bankID)
		return err
	}
	return nil
}

// SubscribeTimeout 是添加官方题库时整套复制最多等多久。
const SubscribeTimeout = 5 * time.Minute

// Unsubscribe 移除官方题库（11.15「可随时移除」）：没改过的官方副本连同作答记录删除；用户改过的留下来，变成用户自己的内容。
func (s *Service) Unsubscribe(ctx context.Context, userID, bankID uint64) error {
	sub, err := s.q.GetSubscription(ctx, dbq.GetSubscriptionParams{OwnerUserID: userID, BankID: bankID})
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		ub, err := q.GetBankForSubject(ctx, dbq.GetBankForSubjectParams{ID: sub.SubjectID, OwnerUserID: userID, OwnerUserID_2: sql.NullInt64{Int64: int64(userID), Valid: true}})
		if err != nil {
			return err
		}
		uc, err := loadUser(ctx, q, userID, ub.ID)
		if err != nil {
			return err
		}
		owner := sql.NullInt64{Int64: int64(userID), Valid: true}
		for _, x := range uc.questions {
			if uc.questionUnmodified(x) {
				if err := q.DeleteUserQuestion(ctx, dbq.DeleteUserQuestionParams{ID: x.ID, OwnerUserID: owner}); err != nil {
					return err
				}
			} else if err := q.UnlinkUserQuestion(ctx, dbq.UnlinkUserQuestionParams{ID: x.ID, OwnerUserID: owner}); err != nil {
				return err
			}
		}
		// 从知识点往上删：章节、板块下面还有用户自己的内容时只解除关联。
		children := map[uint64]int{}
		for _, k := range uc.kps {
			if k.ParentID.Valid {
				children[uint64(k.ParentID.Int64)]++
			}
		}
		for _, lv := range []dbq.KnowledgePointsLevel{dbq.KnowledgePointsLevelPoint, dbq.KnowledgePointsLevelChapter, dbq.KnowledgePointsLevelSection} {
			for _, k := range uc.kps {
				if k.Level != lv || !k.OfficialKpID.Valid {
					continue
				}
				if uc.kpUnmodified(k) && children[k.ID] == 0 {
					if err := q.DeleteUserKP(ctx, dbq.DeleteUserKPParams{ID: k.ID, OwnerUserID: owner}); err != nil {
						return err
					}
					if k.ParentID.Valid {
						children[uint64(k.ParentID.Int64)]--
					}
				} else if err := q.UnlinkUserKP(ctx, dbq.UnlinkUserKPParams{ID: k.ID, OwnerUserID: owner}); err != nil {
					return err
				}
			}
		}
		if _, err := q.DeleteSubscription(ctx, dbq.DeleteSubscriptionParams{OwnerUserID: userID, BankID: bankID}); err != nil {
			return err
		}
		// 官方真题没了：那几年的真题卷改用用户自己导入的真题（没有就清空），考频重算（T57）。
		years := map[int]bool{}
		for _, x := range uc.questions {
			if x.ExamYear.Valid {
				years[int(x.ExamYear.Int16)] = true
			}
		}
		return rebuildExamData(ctx, q, userID, sub.SubjectID, ub.ID, years)
	})
}

// rebuildExamData 在官方内容变化后重组这几年的真题卷、重算知识点考频（T57：官方真题进真题卷、计入考频，真题卷的成绩参与估分）。
func rebuildExamData(ctx context.Context, q *dbq.Queries, userID, subjectID, bankID uint64, years map[int]bool) error {
	if len(years) > 0 {
		sub, err := q.GetSubject(ctx, dbq.GetSubjectParams{ID: subjectID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		ys := make([]int, 0, len(years))
		for y := range years {
			ys = append(ys, y)
		}
		sort.Ints(ys)
		if err := importer.BuildRealExamPapers(ctx, q, userID, bankID, subjectID, sub.FullScore, ys); err != nil {
			return err
		}
	}
	return q.RecountKPExamCounts(ctx, dbq.RecountKPExamCountsParams{BankID: bankID, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
}

// userContent 是用户题库里与官方有关联的副本与它们的采分点。
type userContent struct {
	kps       []dbq.ListUserKPCopiesRow
	questions []dbq.ListUserQuestionCopiesRow
	kpRubric  map[uint64][]string
	qRubric   map[uint64][]string
}

func loadUser(ctx context.Context, q *dbq.Queries, userID, bankID uint64) (userContent, error) {
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	var u userContent
	var err error
	if u.kps, err = q.ListUserKPCopies(ctx, dbq.ListUserKPCopiesParams{BankID: bankID, OwnerUserID: owner}); err != nil {
		return u, err
	}
	if u.questions, err = q.ListUserQuestionCopies(ctx, dbq.ListUserQuestionCopiesParams{BankID: bankID, OwnerUserID: owner}); err != nil {
		return u, err
	}
	rub, err := q.ListUserRubricForCopies(ctx, dbq.ListUserRubricForCopiesParams{Owner: owner, BankID: bankID})
	if err != nil {
		return u, err
	}
	u.kpRubric, u.qRubric = map[uint64][]string{}, map[uint64][]string{}
	for _, r := range rub {
		if r.QuestionID.Valid {
			u.qRubric[uint64(r.QuestionID.Int64)] = append(u.qRubric[uint64(r.QuestionID.Int64)], r.Content)
		} else if r.KpID.Valid {
			u.kpRubric[uint64(r.KpID.Int64)] = append(u.kpRubric[uint64(r.KpID.Int64)], r.Content)
		}
	}
	return u, nil
}

// kpUnmodified 判断用户副本没被改过：当前内容的哈希等于上次同步时的官方哈希。同名合并的（没有哈希）算改过，永不覆盖。
func (u userContent) kpUnmodified(k dbq.ListUserKPCopiesRow) bool {
	return k.OfficialHash.Valid && kpHashOf(k.Name, k.OriginalText.String, rubricText(u.kpRubric[k.ID]), k.ReferenceText.String) == k.OfficialHash.String
}

func (u userContent) questionUnmodified(x dbq.ListUserQuestionCopiesRow) bool {
	return x.OfficialHash.Valid && questionHashOf(string(x.Qtype), x.Stem, x.Answer.String, rubricText(u.qRubric[x.ID]), x.AnswerOrigin,
		groupKey(x.GroupLabel, x.GroupMajors), x.Analysis.String, x.ReferenceText.String) == x.OfficialHash.String
}

// otherGroups 是与报考方向不匹配、不复制给这位用户的限选题（T51；回答非报考专业不得分）：
// 同一年（没有年份的算一组）的限选题里有与方向匹配的组时，其他组都不要；一组都对不上时全部保留，免得整年的论述题都没了。
// 没填报考方向时全部保留，与用户自己导入时选「不区分方向」一致。
func otherGroups(qs []dbq.ListOfficialQuestionsRow, mine string) map[uint64]bool {
	out := map[uint64]bool{}
	if strings.TrimSpace(mine) == "" {
		return out
	}
	year := func(x dbq.ListOfficialQuestionsRow) int {
		if x.ExamYear.Valid {
			return int(x.ExamYear.Int16)
		}
		return -1
	}
	matched := map[int]bool{}
	for _, x := range qs {
		if x.GroupLabel.String != "" && importer.MatchMajors(mine, x.GroupMajors.String) {
			matched[year(x)] = true
		}
	}
	for _, x := range qs {
		if x.GroupLabel.String != "" && matched[year(x)] && !importer.MatchMajors(mine, x.GroupMajors.String) {
			out[x.ID] = true
		}
	}
	return out
}

// ResyncSubject 报考方向改了以后，重新对这门专业课上添加的官方题库：限选题换成新方向的那一组（T51）。
// 不匹配的组下线（作答记录保留），改回来时恢复。
func (s *Service) ResyncSubject(ctx context.Context, userID, subjectID uint64) error {
	banks, err := s.q.SubscriptionsOfSubject(ctx, dbq.SubscriptionsOfSubjectParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return err
	}
	for _, b := range banks {
		if _, err := s.Sync(ctx, userID, b, true); err != nil {
			return err
		}
	}
	return nil
}

// ResyncUser 对这位用户添加的全部官方题库重新对一遍（导入时在核对页选了报考方向之后用，T51）。
func (s *Service) ResyncUser(ctx context.Context, userID uint64) error {
	subs, err := s.q.ListMySubscriptions(ctx, userID)
	if err != nil {
		return err
	}
	for _, sb := range subs {
		if _, err := s.Sync(ctx, userID, sb.BankID, true); err != nil {
			return err
		}
	}
	return nil
}

// copyOrigin 是用户副本的答案来源：官方题的答案由 AI 生成的，副本也记 ai_generated，用户端显示「AI 参考答案」（T49）；其余记 official。
func copyOrigin(o dbq.NullQuestionsAnswerOrigin) dbq.NullQuestionsAnswerOrigin {
	if o.Valid && o.QuestionsAnswerOrigin == dbq.QuestionsAnswerOriginAiGenerated {
		return o
	}
	return dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginOfficial, Valid: true}
}

// SyncResult 是一次对账的结果。
type SyncResult struct {
	Added, Updated, Offline, Removed, Skipped int
}

// Sync 按官方题库的当前内容对一个用户做对账（添加时、每次发布或回滚后）。可重复执行。
func (s *Service) Sync(ctx context.Context, userID, bankID uint64, initial bool) (SyncResult, error) {
	var res SyncResult
	sub, err := s.q.GetSubscription(ctx, dbq.GetSubscriptionParams{OwnerUserID: userID, BankID: bankID})
	if err != nil {
		return res, err
	}
	now := s.now().UTC()
	err = store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		res = SyncResult{}
		ub, err := q.GetBankForSubject(ctx, dbq.GetBankForSubjectParams{ID: sub.SubjectID, OwnerUserID: userID, OwnerUserID_2: sql.NullInt64{Int64: int64(userID), Valid: true}})
		if err != nil {
			return err
		}
		oc, err := loadContent(ctx, q, bankID)
		if err != nil {
			return err
		}
		uc, err := loadUser(ctx, q, userID, ub.ID)
		if err != nil {
			return err
		}
		mine, err := q.GetSubjectMyMajors(ctx, dbq.GetSubjectMyMajorsParams{ID: sub.SubjectID, OwnerUserID: userID})
		if err != nil {
			return err
		}
		skip := otherGroups(oc.questions, mine.String)
		owner := sql.NullInt64{Int64: int64(userID), Valid: true}
		linked := map[uint64]dbq.ListUserKPCopiesRow{}
		byName := map[string]dbq.ListUserKPCopiesRow{}
		for _, k := range uc.kps {
			if k.OfficialKpID.Valid {
				linked[uint64(k.OfficialKpID.Int64)] = k
			} else if prev, ok := byName[string(k.Level)+"\x00"+k.Name]; !ok || (prev.OriginalText.String == "" && k.OriginalText.String != "") {
				// 同名的有多个时（比如真题里拆出的和讲义里的），合并到有原文表述的那个。
				byName[string(k.Level)+"\x00"+k.Name] = k
			}
		}
		newUntil := sql.NullTime{}
		if !initial {
			newUntil = sql.NullTime{Time: now.AddDate(0, 0, newMarkDays), Valid: true}
		}
		today := sql.NullTime{Time: rules.DayOf(now).Date(), Valid: true}
		kpMap := map[uint64]uint64{} // 官方知识点 → 用户的知识点
		officialKPs := map[uint64]bool{}
		// 新增的副本攒起来批量写（见 dbq.InsertRows）；采分点等题目也写完后一起写。
		var newKPs, rubrics [][]any
		var newKPIDs []uint64
		pending := map[uint64]bool{}
		flushKPs := func() error {
			if len(newKPs) == 0 {
				return nil
			}
			if err := q.InsertRows(ctx, insertKPCopies, newKPs, nil); err != nil {
				return err
			}
			rows, err := q.ListUserCopyKPIDs(ctx, dbq.ListUserCopyKPIDsParams{BankID: ub.ID, OwnerUserID: owner})
			if err != nil {
				return err
			}
			got := make(map[uint64]uint64, len(rows))
			for _, r := range rows {
				got[uint64(r.OfficialKpID.Int64)] = r.ID
			}
			for _, oid := range newKPIDs {
				id, ok := got[oid]
				if !ok {
					return fmt.Errorf("知识点副本没有写进去：官方知识点 %d", oid)
				}
				kpMap[oid] = id
				rubrics = appendRubricRows(rubrics, owner, sql.NullInt64{}, sql.NullInt64{Int64: int64(id), Valid: true}, oc.kpRubric[oid])
			}
			newKPs, newKPIDs = nil, nil
			clear(pending)
			return nil
		}
		for i, o := range oc.kps {
			officialKPs[o.ID] = true
			oh := oc.kpHash(o)
			// 父节点还在待写的一批里时先写掉，拿到它的 id（按层级排过序，正常只在换层级时发生）。
			if o.ParentID.Valid && pending[uint64(o.ParentID.Int64)] {
				if err := flushKPs(); err != nil {
					return err
				}
			}
			parent := sql.NullInt64{}
			if o.ParentID.Valid {
				if p, ok := kpMap[uint64(o.ParentID.Int64)]; ok {
					parent = sql.NullInt64{Int64: int64(p), Valid: true}
				}
			}
			u, ok := linked[o.ID]
			if !ok {
				if same, ok := byName[string(o.Level)+"\x00"+o.Name]; ok {
					// 同名合并：保留用户自己的表述和采分点（11.15）。
					if err := q.LinkUserKP(ctx, dbq.LinkUserKPParams{OfficialKpID: sql.NullInt64{Int64: int64(o.ID), Valid: true}, ID: same.ID, OwnerUserID: owner}); err != nil {
						return err
					}
					delete(byName, string(o.Level)+"\x00"+o.Name)
					kpMap[o.ID] = same.ID
					continue
				}
				newKPs = append(newKPs, []any{owner, ub.ID, parent, o.Level, o.Name, o.OriginalText, o.ReferenceText, "official",
					sql.NullInt64{Int64: int64(o.ID), Valid: true}, nullStr(oh), newUntil, uint32(i)})
				newKPIDs = append(newKPIDs, o.ID)
				pending[o.ID] = true
				res.Added++
				continue
			}
			kpMap[o.ID] = u.ID
			if !u.OfficialHash.Valid || u.OfficialHash.String == oh {
				continue // 同名合并的不覆盖；官方没变不用动
			}
			if !uc.kpUnmodified(u) {
				res.Skipped++
				continue
			}
			rubricChanged := rubricText(uc.kpRubric[u.ID]) != rubricText(contents(oc.kpRubric[o.ID]))
			if err := q.UpdateUserKPCopy(ctx, dbq.UpdateUserKPCopyParams{Name: o.Name, OriginalText: o.OriginalText, ReferenceText: o.ReferenceText, OfficialHash: nullStr(oh), ID: u.ID, OwnerUserID: owner}); err != nil {
				return err
			}
			if rubricChanged {
				if err := q.DeleteUserKPRubric(ctx, dbq.DeleteUserKPRubricParams{KpID: sql.NullInt64{Int64: int64(u.ID), Valid: true}, OwnerUserID: owner}); err != nil {
					return err
				}
				if err := copyRubric(ctx, q, owner, sql.NullInt64{}, sql.NullInt64{Int64: int64(u.ID), Valid: true}, oc.kpRubric[o.ID]); err != nil {
					return err
				}
				// 采分点有实质变化：对应背诵重新进入复习。
				if err := q.ReciteReviewAgain(ctx, dbq.ReciteReviewAgainParams{ReciteNextReviewOn: today, KpID: u.ID, OwnerUserID: userID}); err != nil {
					return err
				}
			}
			res.Updated++
		}
		if err := flushKPs(); err != nil {
			return err
		}
		// 官方已删除的知识点（回滚了新增）：没改过的删掉，改过的解除关联。
		for _, k := range uc.kps {
			if !k.OfficialKpID.Valid || officialKPs[uint64(k.OfficialKpID.Int64)] {
				continue
			}
			if uc.kpUnmodified(k) {
				if err := q.DeleteUserKP(ctx, dbq.DeleteUserKPParams{ID: k.ID, OwnerUserID: owner}); err != nil {
					return err
				}
				res.Removed++
			} else if err := q.UnlinkUserKP(ctx, dbq.UnlinkUserKPParams{ID: k.ID, OwnerUserID: owner}); err != nil {
				return err
			}
		}

		linkedQ := map[uint64]dbq.ListUserQuestionCopiesRow{}
		for _, x := range uc.questions {
			linkedQ[uint64(x.OfficialQuestionID.Int64)] = x
		}
		officialQs := map[uint64]bool{}
		var newQs []dbq.ListOfficialQuestionsRow
		for _, o := range oc.questions {
			officialQs[o.ID] = true
			u, ok := linkedQ[o.ID]
			if o.Status != dbq.QuestionsStatusActive || skip[o.ID] {
				// 下线的、不是报考方向那一组的限选题归档，不再出题。
				if ok && u.Status == dbq.QuestionsStatusActive {
					if err := q.SetUserQuestionStatus(ctx, dbq.SetUserQuestionStatusParams{Status: dbq.QuestionsStatusOffline, ID: u.ID, OwnerUserID: owner}); err != nil {
						return err
					}
					res.Offline++
				}
				continue
			}
			oh := oc.questionHash(o)
			if !ok {
				newQs = append(newQs, o)
				res.Added++
				continue
			}
			if u.OfficialHash.String == oh && u.Status == dbq.QuestionsStatusActive {
				continue
			}
			if !uc.questionUnmodified(u) {
				res.Skipped++
				continue
			}
			if err := q.UpdateUserQuestionCopy(ctx, dbq.UpdateUserQuestionCopyParams{Qtype: o.Qtype, Stem: o.Stem, Options: o.Options, Answer: o.Answer, AnswerOrigin: copyOrigin(o.AnswerOrigin), Analysis: o.Analysis,
				ReferenceText: o.ReferenceText, Score: o.Score, ExamYear: o.ExamYear, ContentHash: importer.ContentHash(o.Stem), OfficialHash: nullStr(oh), GroupLabel: o.GroupLabel, GroupMajors: o.GroupMajors,
				ID: u.ID, OwnerUserID: owner}); err != nil {
				return err
			}
			if err := q.DeleteUserQuestionRubric(ctx, dbq.DeleteUserQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(u.ID), Valid: true}, OwnerUserID: owner}); err != nil {
				return err
			}
			if err := copyRubric(ctx, q, owner, sql.NullInt64{Int64: int64(u.ID), Valid: true}, sql.NullInt64{}, oc.qRubric[o.ID]); err != nil {
				return err
			}
			if err := q.DeleteUserQuestionKPs(ctx, dbq.DeleteUserQuestionKPsParams{QuestionID: u.ID, OwnerUserID: owner}); err != nil {
				return err
			}
			if err := copyQuestionKPs(ctx, q, owner, u.ID, oc.qKPs[o.ID], kpMap); err != nil {
				return err
			}
			if o.AnswerSnapshotID.Valid {
				if err := s.copyQuestionEvidence(ctx, q, uint64(o.AnswerSnapshotID.Int64), userID, u.ID); err != nil {
					return err
				}
			} else {
				if err := q.SetQuestionAnswerSnapshot(ctx, dbq.SetQuestionAnswerSnapshotParams{ID: u.ID, OwnerUserID: owner}); err != nil {
					return err
				}
			}
			res.Updated++
		}
		if rubrics, err = s.insertQuestionCopies(ctx, q, oc, newQs, userID, ub.ID, kpMap, rubrics); err != nil {
			return err
		}
		if err := q.InsertRows(ctx, insertRubricCopies, rubrics, nil); err != nil {
			return err
		}
		for _, x := range uc.questions {
			if officialQs[uint64(x.OfficialQuestionID.Int64)] {
				continue
			}
			if uc.questionUnmodified(x) {
				if err := q.DeleteUserQuestion(ctx, dbq.DeleteUserQuestionParams{ID: x.ID, OwnerUserID: owner}); err != nil {
					return err
				}
				res.Removed++
			} else if err := q.UnlinkUserQuestion(ctx, dbq.UnlinkUserQuestionParams{ID: x.ID, OwnerUserID: owner}); err != nil {
				return err
			}
		}
		years := map[int]bool{}
		for _, o := range oc.questions {
			if o.ExamYear.Valid {
				years[int(o.ExamYear.Int16)] = true
			}
		}
		for _, x := range uc.questions {
			if x.ExamYear.Valid {
				years[int(x.ExamYear.Int16)] = true
			}
		}
		if err := rebuildExamData(ctx, q, userID, sub.SubjectID, ub.ID, years); err != nil {
			return err
		}
		if res.Skipped > 0 && !initial {
			// 用户改过的不覆盖，提示「官方内容已更新，是否查看」（11.15）。
			return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeOfficialBank, Title: "官方内容已更新",
				Body:      "你修改过的 " + strconv.Itoa(res.Skipped) + " 处内容官方有新版本，保留了你的修改，可以到题库里对照查看",
				Link:      notify.Link("bank", map[string]any{"subject_id": sub.SubjectID}),
				DedupeKey: sql.NullString{String: "official_update:" + strconv.FormatUint(bankID, 10) + ":" + now.Format("20060102150405"), Valid: true}})
		}
		return nil
	})
	return res, err
}

// 批量写副本用的 INSERT 头，列顺序与各 append 的行一致；每行都带 owner_user_id。
const (
	insertKPCopies       = "INSERT INTO knowledge_points (owner_user_id, bank_id, parent_id, level, name, original_text, reference_text, origin, official_kp_id, official_hash, official_new_until, sort_order) VALUES"
	insertQuestionCopies = "INSERT INTO questions (owner_user_id, bank_id, qtype, stem, options, answer, answer_origin, analysis, reference_text, score, source, exam_year, content_hash, " +
		"official_question_id, official_hash, group_label, group_majors, answer_snapshot_id) VALUES"
	insertRubricCopies   = "INSERT INTO rubric_points (owner_user_id, question_id, kp_id, seq, content, keywords, score, origin) VALUES"
	insertQuestionKPs    = "INSERT IGNORE INTO question_kps (question_id, kp_id, is_primary, owner_user_id) VALUES"
	insertSnapshotCopies = "INSERT INTO answer_evidence_snapshots (owner_user_id, answer_hash, payload, created_at) VALUES"
)

func appendRubricRows(rows [][]any, owner, questionID, kpID sql.NullInt64, rs []dbq.ListOfficialRubricRow) [][]any {
	for _, r := range rs {
		rows = append(rows, []any{owner, questionID, kpID, r.Seq, r.Content, r.Keywords, r.Score, "official"})
	}
	return rows
}

// insertQuestionCopies 批量写新增的题目副本：先复制仍有效的答案依据快照，题目带上快照 id 一次写入，
// 再取回题目 id，写题目与知识点的关联；题目的采分点追加到 rubrics 里返回，由调用方一起写。
func (s *Service) insertQuestionCopies(ctx context.Context, q *dbq.Queries, oc content, qs []dbq.ListOfficialQuestionsRow, userID, bankID uint64,
	kpMap map[uint64]uint64, rubrics [][]any) ([][]any, error) {
	if len(qs) == 0 {
		return rubrics, nil
	}
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	snap, err := s.copySnapshots(ctx, q, qs, owner)
	if err != nil {
		return nil, err
	}
	rows := make([][]any, len(qs))
	for i, o := range qs {
		rows[i] = []any{owner, bankID, o.Qtype, o.Stem, o.Options, o.Answer, copyOrigin(o.AnswerOrigin), o.Analysis, o.ReferenceText, o.Score, "official", o.ExamYear,
			importer.ContentHash(o.Stem), sql.NullInt64{Int64: int64(o.ID), Valid: true}, nullStr(oc.questionHash(o)), o.GroupLabel, o.GroupMajors, snap[o.ID]}
	}
	if err := q.InsertRows(ctx, insertQuestionCopies, rows, nil); err != nil {
		return nil, err
	}
	ids, err := q.ListUserCopyQuestionIDs(ctx, dbq.ListUserCopyQuestionIDsParams{BankID: bankID, OwnerUserID: owner})
	if err != nil {
		return nil, err
	}
	got := make(map[uint64]uint64, len(ids))
	for _, r := range ids {
		got[uint64(r.OfficialQuestionID.Int64)] = r.ID
	}
	var links [][]any
	for _, o := range qs {
		id, ok := got[o.ID]
		if !ok {
			return nil, fmt.Errorf("题目副本没有写进去：官方题目 %d", o.ID)
		}
		qid := sql.NullInt64{Int64: int64(id), Valid: true}
		rubrics = appendRubricRows(rubrics, owner, qid, sql.NullInt64{}, oc.qRubric[o.ID])
		for _, l := range oc.qKPs[o.ID] {
			if kp, ok := kpMap[l.KpID]; ok {
				links = append(links, []any{id, kp, l.IsPrimary, owner})
			}
		}
	}
	if err := q.InsertRows(ctx, insertQuestionKPs, links, nil); err != nil {
		return nil, err
	}
	return rubrics, nil
}

// copySnapshots 给新增题目复制答案依据快照，返回官方题目 → 用户快照 id（依据失效的不复制，题目照常添加）。
func (s *Service) copySnapshots(ctx context.Context, q *dbq.Queries, qs []dbq.ListOfficialQuestionsRow, owner sql.NullInt64) (map[uint64]sql.NullInt64, error) {
	out := map[uint64]sql.NullInt64{}
	var ids []uint64
	for _, o := range qs {
		if o.AnswerSnapshotID.Valid {
			ids = append(ids, uint64(o.AnswerSnapshotID.Int64))
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListOfficialAnswerSnapshots(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]dbq.AnswerEvidenceSnapshot, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	type item struct {
		qid uint64
		row dbq.AnswerEvidenceSnapshot
	}
	var items []item
	var snaps []*ai.AnswerSnapshot
	for _, o := range qs {
		r, ok := byID[uint64(o.AnswerSnapshotID.Int64)]
		if !o.AnswerSnapshotID.Valid || !ok {
			continue
		}
		var sn ai.AnswerSnapshot
		if err := json.Unmarshal(r.Payload, &sn); err != nil {
			return nil, err
		}
		items = append(items, item{o.ID, r})
		snaps = append(snaps, &sn)
	}
	valid, err := s.d.AI.ValidateAnswerSnapshots(ctx, q, snaps)
	if err != nil {
		return nil, err
	}
	// 回查新快照 id 靠「这位用户 + 这个创建时间」：取到毫秒，和 DATETIME(3) 存的一致。
	at := s.now().UTC().Truncate(time.Millisecond)
	var copies [][]any
	var qids []uint64
	for i, it := range items {
		if valid[i] {
			copies = append(copies, []any{owner, it.row.AnswerHash, it.row.Payload, at})
			qids = append(qids, it.qid)
		}
	}
	next := 0
	err = q.InsertRows(ctx, insertSnapshotCopies, copies, func(chunk [][]any, first uint64) error {
		got, err := q.ListUserAnswerSnapshotsSince(ctx, dbq.ListUserAnswerSnapshotsSinceParams{OwnerUserID: owner, CreatedAt: at, FirstID: first})
		if err != nil {
			return err
		}
		if len(got) != len(chunk) {
			// 同一位用户同一毫秒还有别的写入（几乎不会），对不上就整体失败，事务回滚，用户重试即可。
			return fmt.Errorf("答案依据快照回查数量不符：写入 %d，查到 %d", len(chunk), len(got))
		}
		for _, id := range got {
			out[qids[next]] = sql.NullInt64{Int64: int64(id), Valid: true}
			next++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func copyRubric(ctx context.Context, q *dbq.Queries, owner, questionID, kpID sql.NullInt64, rs []dbq.ListOfficialRubricRow) error {
	return q.InsertRows(ctx, insertRubricCopies, appendRubricRows(nil, owner, questionID, kpID, rs), nil)
}

func copyQuestionKPs(ctx context.Context, q *dbq.Queries, owner sql.NullInt64, questionID uint64, links []dbq.ListOfficialQuestionKPsRow, kpMap map[uint64]uint64) error {
	var rows [][]any
	for _, l := range links {
		if kp, ok := kpMap[l.KpID]; ok {
			rows = append(rows, []any{questionID, kp, l.IsPrimary, owner})
		}
	}
	return q.InsertRows(ctx, insertQuestionKPs, rows, nil)
}

// SyncBank 发布或回滚后给所有订阅用户对账，逐个用户各自一个事务；单个用户失败只记日志。
func (s *Service) SyncBank(ctx context.Context, bankID uint64) (int, error) {
	subs, err := s.q.ListSubscribers(ctx, bankID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sb := range subs {
		if _, err := s.Sync(ctx, sb.OwnerUserID, bankID, false); err != nil {
			s.d.Log.ErrorContext(ctx, "official sync", "bank_id", bankID, "err", err)
			continue
		}
		n++
	}
	return n, nil
}
