package bank

import (
	"context"
	"strconv"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

// 3.1e 补充资料提示（PRD v3 增补「补充资料提示」，T48）：只对已添加官方题库的专业课提示；规则在 rules.PickSupplementPrompt。

// SupplementPrompt 是题库页顶部的一条提示。
type SupplementPrompt struct {
	Reason      string // section_gap / dispute / wrong_book
	SectionID   uint64
	SectionName string
	Official    int
	Attempts    int
	ScoreRate   float64
}

func promptKey(reason string, section uint64) string {
	if reason == rules.SupplementSectionGap {
		return "section:" + strconv.FormatUint(section, 10)
	}
	return reason
}

// SupplementPrompt 计算这门课现在要不要显示补充资料提示；不显示时返回 nil。
func (s *Service) SupplementPrompt(ctx context.Context, userID, subjectID uint64) (*SupplementPrompt, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return nil, err
	}
	if n, err := s.q.HasOfficialSubscription(ctx, dbq.HasOfficialSubscriptionParams{OwnerUserID: userID, SubjectID: subjectID}); err != nil || n == 0 {
		return nil, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return nil, err
	}
	cfg := p.OfficialFirst.Supplement
	now := s.now()
	reg, err := s.q.UserCreatedAt(ctx, userID)
	if err != nil {
		return nil, err
	}
	snoozed := map[string]bool{}
	ds, err := s.q.ListSupplementDismissals(ctx, dbq.ListSupplementDismissalsParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return nil, err
	}
	for _, d := range ds {
		if d.SnoozeUntil.After(now) {
			snoozed[d.PromptKey] = true
		}
	}
	// 板块：知识点树的根（板块）以及每个知识点所属的板块。
	kps, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: b.BankID, Owner: owner(userID)})
	if err != nil {
		return nil, err
	}
	parent := map[uint64]uint64{}
	names := map[uint64]string{}
	for _, k := range kps {
		names[k.ID] = k.Name
		if k.ParentID.Valid {
			parent[k.ID] = uint64(k.ParentID.Int64)
		}
	}
	sectionOf := func(id uint64) uint64 {
		for depth := 0; depth < 8; depth++ {
			p, ok := parent[id]
			if !ok {
				return id
			}
			id = p
		}
		return id
	}
	stats := map[uint64]*rules.SectionStat{}
	stat := func(sec uint64) *rules.SectionStat {
		if st, ok := stats[sec]; ok {
			return st
		}
		st := &rules.SectionStat{ID: sec, Name: names[sec], Snoozed: snoozed[promptKey(rules.SupplementSectionGap, sec)]}
		stats[sec] = st
		return st
	}
	srcs, err := s.q.KPQuestionSources(ctx, dbq.KPQuestionSourcesParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return nil, err
	}
	for _, r := range srcs {
		stat(sectionOf(r.KpID)).Official += int(r.OfficialCount)
	}
	atts, err := s.q.RecentAttemptsWithKP(ctx, dbq.RecentAttemptsWithKPParams{OwnerUserID: userID, BankID: b.BankID, AnsweredAt: now.UTC().AddDate(0, 0, -cfg.WindowDays)})
	if err != nil {
		return nil, err
	}
	for _, a := range atts {
		if a.KpID == 0 {
			continue
		}
		rate, ok := attemptRate(a)
		if !ok {
			continue
		}
		st := stat(sectionOf(uint64(a.KpID)))
		st.Attempts++
		st.ScoreSum += rate
	}
	in := rules.SupplementInput{RegisteredAt: reg, Now: now, DisputeSnoozed: snoozed[rules.SupplementDispute], WrongBookSnoozed: snoozed[rules.SupplementWrongBook]}
	for _, st := range stats {
		in.Sections = append(in.Sections, *st)
	}
	n, err := s.q.HasTeacherDispute(ctx, dbq.HasTeacherDisputeParams{OwnerUserID: userID, BankID: b.BankID})
	if err != nil {
		return nil, err
	}
	in.TeacherDispute = n > 0
	wb, err := s.q.CountActiveWrongBook(ctx, dbq.CountActiveWrongBookParams{OwnerUserID: userID, BankID: b.BankID})
	if err != nil {
		return nil, err
	}
	in.WrongBook = int(wb)
	got := rules.PickSupplementPrompt(in, cfg)
	if got.Reason == "" {
		return nil, nil
	}
	return &SupplementPrompt{Reason: got.Reason, SectionID: got.Section.ID, SectionName: got.Section.Name, Official: got.Section.Official,
		Attempts: got.Section.Attempts, ScoreRate: got.Section.Rate()}, nil
}

// attemptRate 是一次作答的得分率：有分数按得分 / 满分，客观题按对错。
func attemptRate(a dbq.RecentAttemptsWithKPRow) (float64, bool) {
	if a.Score.Valid && a.FullScore.Valid {
		sc, err1 := strconv.ParseFloat(a.Score.String, 64)
		full, err2 := strconv.ParseFloat(a.FullScore.String, 64)
		if err1 == nil && err2 == nil && full > 0 {
			return sc / full, true
		}
	}
	if a.IsCorrect.Valid {
		if a.IsCorrect.Bool {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// DismissSupplementPrompt 是「以后再说」：同一条提示（同一板块）SnoozeDays 天内不再出现。
func (s *Service) DismissSupplementPrompt(ctx context.Context, userID, subjectID uint64, reason string, sectionID uint64) error {
	if _, err := s.subject(ctx, userID, subjectID); err != nil {
		return err
	}
	switch reason {
	case rules.SupplementSectionGap:
		if sectionID == 0 {
			return apperr.New(apperr.BadRequest, "缺少板块")
		}
	case rules.SupplementDispute, rules.SupplementWrongBook:
	default:
		return apperr.New(apperr.BadRequest, "提示类型不正确")
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return err
	}
	until := s.now().UTC().AddDate(0, 0, p.OfficialFirst.Supplement.SnoozeDays)
	return s.q.UpsertSupplementDismissal(ctx, dbq.UpsertSupplementDismissalParams{OwnerUserID: userID, SubjectID: subjectID,
		PromptKey: promptKey(reason, sectionID), SnoozeUntil: until})
}
