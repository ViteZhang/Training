package official

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

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
}

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
	out := make([]Bank, 0, len(rows))
	for _, r := range rows {
		b := Bank{BankID: r.BankID, Title: r.Title, School: r.School, Major: r.Major, SubjectCode: r.SubjectCode.String, SubjectName: r.SubjectName,
			Version: str(r.Version), KPCount: int(r.KpCount), QuestionCount: int(r.QuestionCount), AddedTo: added[r.BankID]}
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
	if _, err := s.Sync(ctx, userID, bankID, true); err != nil {
		// 复制失败时撤掉订阅，用户可以重试；已复制的部分一并清掉。
		_ = s.Unsubscribe(context.WithoutCancel(ctx), userID, bankID)
		return err
	}
	return nil
}

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
		_, err = q.DeleteSubscription(ctx, dbq.DeleteSubscriptionParams{OwnerUserID: userID, BankID: bankID})
		return err
	})
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
	return k.OfficialHash.Valid && hashOf(k.Name, k.OriginalText.String, rubricText(u.kpRubric[k.ID])) == k.OfficialHash.String
}

func (u userContent) questionUnmodified(x dbq.ListUserQuestionCopiesRow) bool {
	return x.OfficialHash.Valid && hashOf(string(x.Qtype), x.Stem, x.Answer.String, rubricText(u.qRubric[x.ID])) == x.OfficialHash.String
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
		for i, o := range oc.kps {
			officialKPs[o.ID] = true
			oh := oc.kpHash(o)
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
				id, err := q.InsertUserKPCopy(ctx, dbq.InsertUserKPCopyParams{OwnerUserID: owner, BankID: ub.ID, ParentID: parent, Level: o.Level, Name: o.Name,
					OriginalText: o.OriginalText, OfficialKpID: sql.NullInt64{Int64: int64(o.ID), Valid: true}, OfficialHash: nullStr(oh), OfficialNewUntil: newUntil,
					SortOrder: uint32(i)})
				if err != nil {
					return err
				}
				kpMap[o.ID] = uint64(id)
				if err := copyRubric(ctx, q, owner, sql.NullInt64{}, sql.NullInt64{Int64: id, Valid: true}, oc.kpRubric[o.ID]); err != nil {
					return err
				}
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
			if err := q.UpdateUserKPCopy(ctx, dbq.UpdateUserKPCopyParams{Name: o.Name, OriginalText: o.OriginalText, OfficialHash: nullStr(oh), ID: u.ID, OwnerUserID: owner}); err != nil {
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
		for _, o := range oc.questions {
			officialQs[o.ID] = true
			u, ok := linkedQ[o.ID]
			if o.Status != dbq.QuestionsStatusActive {
				// 下线的归档，不再出题。
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
				id, err := q.InsertUserQuestionCopy(ctx, dbq.InsertUserQuestionCopyParams{OwnerUserID: owner, BankID: ub.ID, Qtype: o.Qtype, Stem: o.Stem, Options: o.Options,
					Answer: o.Answer, Analysis: o.Analysis, Score: o.Score, ExamYear: o.ExamYear, ContentHash: importer.ContentHash(o.Stem),
					OfficialQuestionID: sql.NullInt64{Int64: int64(o.ID), Valid: true}, OfficialHash: nullStr(oh)})
				if err != nil {
					return err
				}
				if err := copyRubric(ctx, q, owner, sql.NullInt64{Int64: id, Valid: true}, sql.NullInt64{}, oc.qRubric[o.ID]); err != nil {
					return err
				}
				if err := copyQuestionKPs(ctx, q, owner, uint64(id), oc.qKPs[o.ID], kpMap); err != nil {
					return err
				}
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
			if err := q.UpdateUserQuestionCopy(ctx, dbq.UpdateUserQuestionCopyParams{Qtype: o.Qtype, Stem: o.Stem, Options: o.Options, Answer: o.Answer, Analysis: o.Analysis,
				Score: o.Score, ExamYear: o.ExamYear, ContentHash: importer.ContentHash(o.Stem), OfficialHash: nullStr(oh), ID: u.ID, OwnerUserID: owner}); err != nil {
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
			res.Updated++
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

func copyRubric(ctx context.Context, q *dbq.Queries, owner, questionID, kpID sql.NullInt64, rs []dbq.ListOfficialRubricRow) error {
	for _, r := range rs {
		if err := q.InsertOfficialRubricPoint(ctx, dbq.InsertOfficialRubricPointParams{OwnerUserID: owner, QuestionID: questionID, KpID: kpID, Seq: r.Seq, Content: r.Content,
			Keywords: r.Keywords, Score: r.Score}); err != nil {
			return err
		}
	}
	return nil
}

func copyQuestionKPs(ctx context.Context, q *dbq.Queries, owner sql.NullInt64, questionID uint64, links []dbq.ListOfficialQuestionKPsRow, kpMap map[uint64]uint64) error {
	for _, l := range links {
		kp, ok := kpMap[l.KpID]
		if !ok {
			continue
		}
		if err := q.InsertOfficialQuestionKP(ctx, dbq.InsertOfficialQuestionKPParams{QuestionID: questionID, KpID: kp, IsPrimary: l.IsPrimary, OwnerUserID: owner}); err != nil {
			return err
		}
	}
	return nil
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
