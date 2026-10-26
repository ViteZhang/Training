package importer

// 缺答案处理（PRD v3 1.7、1.7c、11.16，10 月 26 日更新）：
//   1. 导入时，原文没有答案的题先在同一门课的其他资料里找（不额外计费），找到的标「来自你的资料」并请用户核对
//   2. 还缺的，用户可以用 1.7c 补充一份答案文件：先按年份 + 题号配，对不上再按题干配，用户核对后写进题目
//   3. 再缺的，做到时由 AI 给参考答案（practice 包，计入每日 AI 出题额度）

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
	"peetraining-server/internal/notify"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// 匹配门槛：题干几乎相同才算同一道题；交给模型的候选原文至少要和题目有这么多重合；模型把握低于它的不采用。
const (
	sameStemOverlap   = 0.9
	stemPairOverlap   = 0.6
	candidateOverlap  = 0.3
	matchMinConfident = 0.6
	maxCandidates     = 5
)

// termOf 是名词解释要解释的那个词：去掉「名词解释：」之类的前缀和题号。
func termOf(stem string) string {
	s := reLeadNo.ReplaceAllString(strings.TrimSpace(stem), "")
	for _, sep := range []string{"：", ":"} {
		if i := strings.Index(s, sep); i >= 0 && len([]rune(s[:i])) <= 6 {
			s = s[i+len(sep):]
		}
	}
	return Normalize(s)
}

func sameYearPtr(a sql.NullInt16, b *int) bool {
	if !a.Valid || b == nil {
		return true // 有一边没有年份时不以年份排除
	}
	return int(a.Int16) == *b
}

// matchFromOtherMaterials 给还缺答案的题在同一门课的其他资料里找答案（PRD v3 11.16 第 2 条）：
// 题库里同一道题已有的答案 → 以前导入的答案（按年份 + 题号）→ 名词解释与知识点同名 → 模型从相关的知识点原文里逐字摘。
// 找到的直接写进 q，返回哪些找到了。模型调用失败的跳过，留给后面的步骤。
func (s *Service) matchFromOtherMaterials(ctx context.Context, job dbq.GetImportJobRow, missing []*QuestionDraft) (map[*QuestionDraft]bool, error) {
	userID := job.OwnerUserID
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	found := map[*QuestionDraft]bool{}
	set := func(q *QuestionDraft, answer string, materialID uint64, page int) {
		q.Answer, q.AnswerOrigin, q.AnswerMatch, q.AnswerMaterialID, q.AnswerPage = strings.TrimSpace(answer), "imported", MatchOtherMaterial, materialID, page
		found[q] = true
	}
	bank, err := s.q.ListBankAnsweredQuestions(ctx, dbq.ListBankAnsweredQuestionsParams{BankID: job.BankID, OwnerUserID: owner})
	if err != nil {
		return nil, err
	}
	prev, err := s.q.ListBankImportAnswers(ctx, dbq.ListBankImportAnswersParams{BankID: job.BankID, OwnerUserID: userID, JobID: job.ID})
	if err != nil {
		return nil, err
	}
	kps, err := s.q.ListBankKPTexts(ctx, dbq.ListBankKPTextsParams{BankID: job.BankID, OwnerUserID: owner})
	if err != nil {
		return nil, err
	}
	var forAI []*QuestionDraft
	for _, q := range missing {
		h := ContentHash(q.Stem)
		done := false
		for _, b := range bank {
			if !sameYearPtr(b.ExamYear, q.ExamYear) {
				continue
			}
			if b.ContentHash == h || (string(b.Qtype) == q.QType && ai.Overlap(q.Stem, b.Stem) >= sameStemOverlap && ai.Overlap(b.Stem, q.Stem) >= sameStemOverlap) {
				mid, page := b.AnswerMaterialID, b.AnswerPage
				if !mid.Valid {
					mid, page = b.SourceMaterialID, b.SourcePage
				}
				set(q, b.Answer.String, uint64(mid.Int64), int(page.Int32))
				done = true
				break
			}
		}
		if done {
			continue
		}
		for _, a := range prev {
			byNo := q.QuestionNo != "" && a.QuestionNo == q.QuestionNo && a.ExamYear.Valid && q.ExamYear != nil && int(a.ExamYear.Int16) == *q.ExamYear
			byStem := a.Stem.Valid && ai.Overlap(q.Stem, a.Stem.String) >= sameStemOverlap && ai.Overlap(a.Stem.String, q.Stem) >= sameStemOverlap
			if byNo || byStem {
				set(q, a.Answer, a.MaterialID, int(a.Page))
				done = true
				break
			}
		}
		if done {
			continue
		}
		if q.QType == "term" {
			t := termOf(q.Stem)
			for _, k := range kps {
				if t != "" && Normalize(k.Name) == t {
					set(q, k.OriginalText.String, uint64(k.SourceMaterialID.Int64), int(k.SourcePage.Int32))
					done = true
					break
				}
			}
		}
		if !done && ai.Subjective(q.QType) && len(kps) > 0 {
			forAI = append(forAI, q)
		}
	}
	if len(forAI) == 0 {
		return found, nil
	}
	type pick struct {
		kp  dbq.ListBankKPTextsRow
		out ai.MatchAnswerOut
	}
	results, _, err := runBatches(ctx, len(forAI), func(ctx context.Context, i int) (pick, error) {
		q := forAI[i]
		cands := relatedKPs(q.Stem, kps, maxCandidates)
		if len(cands) == 0 {
			return pick{}, nil
		}
		in := ai.MatchAnswerIn{Subject: job.SubjectName, QType: q.QType, Stem: q.fullStem()}
		for j, k := range cands {
			in.Candidates = append(in.Candidates, ai.AnswerCandidate{ID: j + 1, Text: truncate(k.OriginalText.String, 800)})
		}
		out, _, err := ai.MatchAnswer.Run(ctx, s.ai, userID, in)
		if err != nil || out.CandidateID == 0 {
			return pick{}, err
		}
		return pick{kp: cands[out.CandidateID-1], out: out}, nil
	})
	if err != nil {
		return nil, err
	}
	for i, r := range results {
		if r.out.CandidateID > 0 && r.out.Confidence >= matchMinConfident {
			set(forAI[i], r.out.Answer, uint64(r.kp.SourceMaterialID.Int64), int(r.kp.SourcePage.Int32))
		}
	}
	return found, nil
}

// relatedKPs 按与题目的字面重合度挑出最相关的几条知识点原文（交给模型判断，也用作 AI 参考答案的资料依据）。
func relatedKPs(stem string, kps []dbq.ListBankKPTextsRow, n int) []dbq.ListBankKPTextsRow {
	core := termOf(stem)
	if core == "" {
		core = Normalize(stem)
	}
	type scored struct {
		kp dbq.ListBankKPTextsRow
		v  float64
	}
	var all []scored
	for _, k := range kps {
		v := max(ai.Overlap(core, Normalize(k.Name)), ai.Overlap(core, Normalize(k.Name+k.OriginalText.String)))
		if v >= candidateOverlap {
			all = append(all, scored{k, v})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].v > all[j].v })
	out := make([]dbq.ListBankKPTextsRow, 0, n)
	for _, s := range all {
		if len(out) == n {
			break
		}
		out = append(out, s.kp)
	}
	return out
}

// AnswerInput 组装生成 AI 参考答案的输入（PRD v3 11.16 第 5 条）：同课资料里相关的知识点原文 → 用户填的参考教材 → 通用知识。
// 1.7b、3.3b、4.4 与整卷批改都用它。
func AnswerInput(ctx context.Context, q *dbq.Queries, userID, bankID uint64, qtype, stem string, score float64) (ai.GenerateAnswerIn, error) {
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	in := ai.GenerateAnswerIn{QType: qtype, Stem: stem, Score: score}
	sub, err := q.GetBankSubjectInfo(ctx, dbq.GetBankSubjectInfoParams{ID: bankID, OwnerUserID: owner})
	switch {
	case err == nil:
		in.Subject = sub.Name
		if len(sub.Textbooks) > 0 {
			_ = json.Unmarshal(sub.Textbooks, &in.Textbooks)
		}
	case !errors.Is(err, sql.ErrNoRows):
		return in, err
	}
	kps, err := q.ListBankKPTexts(ctx, dbq.ListBankKPTextsParams{BankID: bankID, OwnerUserID: owner})
	if err != nil {
		return in, err
	}
	for _, k := range relatedKPs(stem, kps, 3) {
		in.Context = append(in.Context, ai.AnswerContext{Source: k.Name, Text: truncate(k.OriginalText.String, 600)})
	}
	return in, nil
}

// ---------- 1.7c 补充答案文件 ----------

// CreateAnswerJob 开始解析一份答案文件（1.7c）：规则同 1.5，计入解析额度。targetJobID 是从哪次导入的确认页进来的，
// 为 0 时（从 3.3b、3.1b 进来）配题库里缺答案的题。
func (s *Service) CreateAnswerJob(ctx context.Context, userID, subjectID, targetJobID uint64, materialIDs []uint64) (Job, error) {
	if targetJobID != 0 {
		t, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: targetJobID, OwnerUserID: userID})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && uint64(t.SubjectID.Int64) != subjectID) {
			return Job{}, apperr.NotFoundErr()
		}
		if err != nil {
			return Job{}, err
		}
	}
	j, err := s.CreateJob(ctx, userID, subjectID, string(dbq.ImportJobsModeAnswers), materialIDs)
	if err != nil {
		return Job{}, err
	}
	if targetJobID != 0 {
		if err := s.q.SetImportJobTarget(ctx, dbq.SetImportJobTargetParams{TargetJobID: sql.NullInt64{Int64: int64(targetJobID), Valid: true}, ID: j.ID, OwnerUserID: userID}); err != nil {
			return Job{}, err
		}
	}
	return s.Get(ctx, userID, j.ID)
}

// pairTarget 是一道缺答案的题（还没入库的导入条目，或题库里的题）。
type pairTarget struct {
	Kind       dbq.AnswerPairingsTargetKind
	ID         uint64
	QType      string
	Stem       string
	ExamYear   *int
	QuestionNo string
	Score      *float64
}

func (s *Service) pairTargets(ctx context.Context, job dbq.GetImportJobRow) ([]pairTarget, error) {
	userID := job.OwnerUserID
	var out []pairTarget
	if job.TargetJobID.Valid {
		items, err := s.q.ListJobItems(ctx, dbq.ListJobItemsParams{JobID: uint64(job.TargetJobID.Int64), OwnerUserID: userID})
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.ItemType != dbq.ImportItemsItemTypeQuestion || it.CreatedEntityID.Valid || it.Status == dbq.ImportItemsStatusDeleted {
				continue
			}
			var d QuestionDraft
			if json.Unmarshal(it.Payload, &d) != nil {
				continue
			}
			if strings.TrimSpace(d.Answer) != "" && d.AnswerOrigin != "ai_generated" {
				continue
			}
			out = append(out, pairTarget{Kind: dbq.AnswerPairingsTargetKindItem, ID: it.ID, QType: d.QType, Stem: d.Stem, ExamYear: d.ExamYear, QuestionNo: d.QuestionNo, Score: d.Score})
		}
	}
	qs, err := s.q.ListBankMissingAnswerQuestions(ctx, dbq.ListBankMissingAnswerQuestionsParams{BankID: job.BankID, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if err != nil {
		return nil, err
	}
	for _, q := range qs {
		t := pairTarget{Kind: dbq.AnswerPairingsTargetKindQuestion, ID: q.ID, QType: string(q.Qtype), Stem: q.Stem, QuestionNo: q.QuestionNo.String}
		if q.ExamYear.Valid {
			y := int(q.ExamYear.Int16)
			t.ExamYear = &y
		}
		if v, err := strconv.ParseFloat(q.Score.String, 64); err == nil && q.Score.Valid {
			t.Score = &v
		}
		out = append(out, t)
	}
	return out, nil
}

// pairAnswers 先按年份 + 题号配，对不上的再按题干配（请核对）；每份答案只配一道题（PRD v3 1.7c）。
func pairAnswers(targets []pairTarget, answers []dbq.ImportAnswer) map[int]struct {
	answer int
	method dbq.AnswerPairingsMethod
} {
	out := map[int]struct {
		answer int
		method dbq.AnswerPairingsMethod
	}{}
	used := map[int]bool{}
	for ti, t := range targets {
		if t.QuestionNo == "" {
			continue
		}
		for ai_, a := range answers {
			if used[ai_] || a.QuestionNo == "" || a.QuestionNo != t.QuestionNo || !sameYear(a.ExamYear, t.ExamYear) {
				continue
			}
			out[ti] = struct {
				answer int
				method dbq.AnswerPairingsMethod
			}{ai_, dbq.AnswerPairingsMethodNumber}
			used[ai_] = true
			break
		}
	}
	for ti, t := range targets {
		if _, ok := out[ti]; ok {
			continue
		}
		best, score := -1, 0.0
		for ai_, a := range answers {
			if used[ai_] || !a.Stem.Valid || !sameYearPtr(a.ExamYear, t.ExamYear) {
				continue
			}
			v := min(ai.Overlap(t.Stem, a.Stem.String), ai.Overlap(a.Stem.String, t.Stem))
			if v >= stemPairOverlap && v > score {
				best, score = ai_, v
			}
		}
		if best >= 0 {
			out[ti] = struct {
				answer int
				method dbq.AnswerPairingsMethod
			}{best, dbq.AnswerPairingsMethodStem}
			used[best] = true
		}
	}
	return out
}

// finalizeAnswers 是答案文件全部解析完后的配对：结果存成待确认的配对，用户在 1.7c 核对后再写进题目。可重复执行。
func (s *Service) finalizeAnswers(ctx context.Context, job dbq.GetImportJobRow, ms []dbq.ListImportJobMaterialsRow) error {
	userID := job.OwnerUserID
	allFailed := len(ms) > 0
	for _, m := range ms {
		if m.Status == dbq.ImportJobMaterialsStatusPending || m.Status == dbq.ImportJobMaterialsStatusRunning {
			return nil
		}
		if m.Status != dbq.ImportJobMaterialsStatusFailed {
			allFailed = false
		}
	}
	if job.Status == dbq.ImportJobsStatusConfirmed {
		return nil
	}
	targets, err := s.pairTargets(ctx, job)
	if err != nil {
		return err
	}
	answers, err := s.q.ListImportAnswers(ctx, dbq.ListImportAnswersParams{JobID: job.ID, OwnerUserID: userID})
	if err != nil {
		return err
	}
	pairs := pairAnswers(targets, answers)
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := q.DeleteProposedPairings(ctx, dbq.DeleteProposedPairingsParams{JobID: job.ID, OwnerUserID: userID}); err != nil {
			return err
		}
		for ti, p := range pairs {
			t := targets[ti]
			if err := q.InsertAnswerPairing(ctx, dbq.InsertAnswerPairingParams{OwnerUserID: userID, JobID: job.ID, TargetKind: t.Kind, TargetID: t.ID,
				ImportAnswerID: sql.NullInt64{Int64: int64(answers[p.answer].ID), Valid: true}, Method: p.method}); err != nil {
				return err
			}
		}
		st, reason := dbq.ImportJobsStatusReviewing, sql.NullString{}
		if allFailed {
			st, reason = dbq.ImportJobsStatusFailed, sql.NullString{String: "答案文件没能解析", Valid: true}
		}
		return q.UpdateImportJobStatus(ctx, dbq.UpdateImportJobStatusParams{Status: st, FailReason: reason,
			FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: job.ID, OwnerUserID: userID})
	})
	if err != nil || allFailed || !notify.TaskDoneEnabled(ctx, s.q) {
		return err
	}
	return s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: userID, Mtype: dbq.MessagesMtypeImportDone, Title: "答案文件配对好了",
		Body: fmt.Sprintf("配上了 %d 道题的答案，核对后就能入库", len(pairs)), Link: notify.Link("answer_file", map[string]any{"job_id": job.ID}),
		DedupeKey: sql.NullString{String: "answer_file:" + strconv.FormatUint(job.ID, 10), Valid: true}})
}

// PairingTarget 是配对结果里的一道题。
type PairingTarget struct {
	Kind       string
	ID         uint64
	QType      string
	Stem       string
	ExamYear   *int
	QuestionNo string
}

// FileRef 是出处：文件 + 页码。
type FileRef struct {
	MaterialID uint64
	FileName   string
	Page       int
}

// PairingAnswer 是答案文件里的一份答案。
type PairingAnswer struct {
	ID         uint64
	Text       string
	ExamYear   *int
	QuestionNo string
	Source     FileRef
}

// Pairing 是一条配对：按题号配上 / 按题干配上 · 请核对 / 用户改配；Answer 为空表示用户取消了。
type Pairing struct {
	ID     uint64
	Target PairingTarget
	Answer *PairingAnswer
	Method string
	Status string
}

// Pairings 是 1.7c 的配对结果：配上的、仍缺答案的题、没用上的答案（用户改配时选）。
type Pairings struct {
	Job          Job
	Pairings     []Pairing
	StillMissing []PairingTarget
	Spare        []PairingAnswer
}

func (s *Service) answerJob(ctx context.Context, userID, jobID uint64) (Job, error) {
	j, err := s.Get(ctx, userID, jobID)
	if err != nil {
		return Job{}, err
	}
	if j.Mode != dbq.ImportJobsModeAnswers {
		return Job{}, apperr.NotFoundErr()
	}
	return j, nil
}

// Pairings 返回答案文件的配对结果（1.7c）。
func (s *Service) Pairings(ctx context.Context, userID, jobID uint64) (Pairings, error) {
	j, err := s.answerJob(ctx, userID, jobID)
	if err != nil {
		return Pairings{}, err
	}
	out := Pairings{Job: j, Pairings: []Pairing{}, StillMissing: []PairingTarget{}, Spare: []PairingAnswer{}}
	if j.Status != dbq.ImportJobsStatusReviewing && j.Status != dbq.ImportJobsStatusConfirmed {
		return out, nil
	}
	targets, err := s.pairTargets(ctx, dbq.GetImportJobRow{ID: j.ID, OwnerUserID: userID, BankID: j.BankID, TargetJobID: j.TargetJobID})
	if err != nil {
		return Pairings{}, err
	}
	byKey := map[string]pairTarget{}
	for _, t := range targets {
		byKey[string(t.Kind)+":"+strconv.FormatUint(t.ID, 10)] = t
	}
	answers, err := s.q.ListImportAnswers(ctx, dbq.ListImportAnswersParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return Pairings{}, err
	}
	files := map[uint64]string{}
	for _, m := range j.Materials {
		files[m.MaterialID] = m.FileName
	}
	toAnswer := func(a dbq.ImportAnswer) PairingAnswer {
		pa := PairingAnswer{ID: a.ID, Text: a.Answer, QuestionNo: a.QuestionNo, Source: FileRef{MaterialID: a.MaterialID, FileName: files[a.MaterialID], Page: int(a.Page)}}
		if a.ExamYear.Valid {
			y := int(a.ExamYear.Int16)
			pa.ExamYear = &y
		}
		return pa
	}
	ansByID := map[uint64]dbq.ImportAnswer{}
	for _, a := range answers {
		ansByID[a.ID] = a
	}
	rows, err := s.q.ListAnswerPairings(ctx, dbq.ListAnswerPairingsParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return Pairings{}, err
	}
	paired := map[string]bool{}
	usedAns := map[uint64]bool{}
	for _, r := range rows {
		key := string(r.TargetKind) + ":" + strconv.FormatUint(r.TargetID, 10)
		t, ok := byKey[key]
		if !ok && r.Status != dbq.AnswerPairingsStatusApplied {
			continue // 这道题已经有答案了（比如用户刚手动补上）
		}
		p := Pairing{ID: r.ID, Method: string(r.Method), Status: string(r.Status),
			Target: PairingTarget{Kind: string(r.TargetKind), ID: r.TargetID, QType: t.QType, Stem: t.Stem, ExamYear: t.ExamYear, QuestionNo: t.QuestionNo}}
		if r.ImportAnswerID.Valid {
			if a, ok := ansByID[uint64(r.ImportAnswerID.Int64)]; ok {
				pa := toAnswer(a)
				p.Answer = &pa
				usedAns[a.ID] = true
			}
		}
		if p.Answer != nil {
			paired[key] = true
		}
		out.Pairings = append(out.Pairings, p)
	}
	for _, t := range targets {
		if !paired[string(t.Kind)+":"+strconv.FormatUint(t.ID, 10)] {
			out.StillMissing = append(out.StillMissing, PairingTarget{Kind: string(t.Kind), ID: t.ID, QType: t.QType, Stem: t.Stem, ExamYear: t.ExamYear, QuestionNo: t.QuestionNo})
		}
	}
	for _, a := range answers {
		if !usedAns[a.ID] {
			out.Spare = append(out.Spare, toAnswer(a))
		}
	}
	return out, nil
}

// UpdatePairing 改配（换成答案文件里的另一份答案）或取消一条（answerID 为 0）。
func (s *Service) UpdatePairing(ctx context.Context, userID, jobID, pairingID, answerID uint64) error {
	if _, err := s.answerJob(ctx, userID, jobID); err != nil {
		return err
	}
	p, err := s.q.GetAnswerPairing(ctx, dbq.GetAnswerPairingParams{ID: pairingID, JobID: jobID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	if p.Status != dbq.AnswerPairingsStatusProposed {
		return apperr.New(apperr.Conflict, "这一条已经确认过了")
	}
	ref := sql.NullInt64{}
	if answerID != 0 {
		a, err := s.q.GetImportAnswer(ctx, dbq.GetImportAnswerParams{ID: answerID, OwnerUserID: userID})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && a.JobID != jobID) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		ref = sql.NullInt64{Int64: int64(answerID), Valid: true}
	}
	return s.q.UpdateAnswerPairing(ctx, dbq.UpdateAnswerPairingParams{ImportAnswerID: ref, Method: dbq.AnswerPairingsMethodManual, ID: pairingID, OwnerUserID: userID})
}

// ConfirmPairings 「确认配对 N 题」：这些题的答案来源变为「来自你的资料」，主观题由 AI 从答案中提取采分点、在 1.7b 核对（PRD v3 1.7c）。
// 返回写入的题数。模型提取采分点失败的题只写答案，用户可在 1.7b 手动加采分点。
func (s *Service) ConfirmPairings(ctx context.Context, userID, jobID uint64) (int, error) {
	j, err := s.answerJob(ctx, userID, jobID)
	if err != nil {
		return 0, err
	}
	if j.Status != dbq.ImportJobsStatusReviewing {
		return 0, apperr.New(apperr.Conflict, "答案文件还没解析完")
	}
	view, err := s.Pairings(ctx, userID, jobID)
	if err != nil {
		return 0, err
	}
	type todo struct {
		p      Pairing
		target pairTarget
		rubric []ai.RubricPoint
	}
	targets, err := s.pairTargets(ctx, dbq.GetImportJobRow{ID: j.ID, OwnerUserID: userID, BankID: j.BankID, TargetJobID: j.TargetJobID})
	if err != nil {
		return 0, err
	}
	byKey := map[string]pairTarget{}
	for _, t := range targets {
		byKey[string(t.Kind)+":"+strconv.FormatUint(t.ID, 10)] = t
	}
	var list []*todo
	for _, p := range view.Pairings {
		t, ok := byKey[p.Target.Kind+":"+strconv.FormatUint(p.Target.ID, 10)]
		if p.Status != string(dbq.AnswerPairingsStatusProposed) || p.Answer == nil || !ok {
			continue
		}
		list = append(list, &todo{p: p, target: t})
	}
	_, _, err = runBatches(ctx, len(list), func(ctx context.Context, i int) (struct{}, error) {
		w := list[i]
		if !ai.Subjective(w.target.QType) {
			return struct{}{}, nil
		}
		score := 0.0
		if w.target.Score != nil {
			score = *w.target.Score
		}
		out, _, err := ai.ExtractRubric.Run(ctx, s.ai, userID, ai.RubricIn{QType: w.target.QType, Stem: w.target.Stem, Answer: w.p.Answer.Text, Score: score})
		if err == nil {
			w.rubric = out.Points
		}
		return struct{}{}, err
	})
	if err != nil {
		return 0, err
	}
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	n := 0
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		for _, w := range list {
			a := w.p.Answer
			switch w.target.Kind {
			case dbq.AnswerPairingsTargetKindItem:
				it, err := q.GetImportItem(ctx, dbq.GetImportItemParams{ID: w.target.ID, OwnerUserID: userID})
				if err != nil {
					return err
				}
				var d QuestionDraft
				if err := json.Unmarshal(it.Payload, &d); err != nil {
					return err
				}
				d.Answer, d.AnswerOrigin, d.AnswerMatch, d.AnswerMaterialID, d.AnswerPage = a.Text, "imported", MatchAnswerFile, a.Source.MaterialID, a.Source.Page
				if w.rubric != nil {
					d.RubricPoints, d.RubricOrigin = w.rubric, "ai_extracted"
				}
				reasons := reviewReasons(d, parseReasons(it.ReviewReasons))
				b, _ := json.Marshal(d)
				if err := q.UpdateImportItem(ctx, dbq.UpdateImportItemParams{Payload: b, Status: it.Status, NeedsReview: needsReview(reasons) || w.p.Method == string(dbq.AnswerPairingsMethodStem),
					ReviewReasons: reasonsJSON(reasons), DuplicateOfQuestionID: it.DuplicateOfQuestionID, ID: it.ID, OwnerUserID: userID}); err != nil {
					return err
				}
			case dbq.AnswerPairingsTargetKindQuestion:
				if err := q.SetQuestionAnswer(ctx, dbq.SetQuestionAnswerParams{Answer: sql.NullString{String: a.Text, Valid: true},
					AnswerOrigin:     dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginImported, Valid: true},
					AnswerMatch:      dbq.NullQuestionsAnswerMatch{QuestionsAnswerMatch: dbq.QuestionsAnswerMatchAnswerFile, Valid: true},
					AnswerMaterialID: sql.NullInt64{Int64: int64(a.Source.MaterialID), Valid: true}, AnswerPage: sql.NullInt32{Int32: int32(a.Source.Page), Valid: a.Source.Page > 0},
					Analysis: sql.NullString{}, ID: w.target.ID, OwnerUserID: owner}); err != nil {
					return err
				}
				if w.rubric != nil {
					qid := sql.NullInt64{Int64: int64(w.target.ID), Valid: true}
					if err := q.DeleteQuestionRubric(ctx, dbq.DeleteQuestionRubricParams{QuestionID: qid, OwnerUserID: owner}); err != nil {
						return err
					}
					if err := insertRubric(ctx, q, owner, qid, sql.NullInt64{}, w.rubric, dbq.RubricPointsOriginAiExtracted); err != nil {
						return err
					}
					reasons := []string{ReasonRubricUnconfirmed}
					if w.p.Method == string(dbq.AnswerPairingsMethodStem) {
						reasons = append(reasons, ReasonAnswerMatched)
					}
					if err := q.SetQuestionRubricPending(ctx, dbq.SetQuestionRubricPendingParams{ReviewReasons: reasonsJSON(reasons), ID: w.target.ID, OwnerUserID: owner}); err != nil {
						return err
					}
				}
			}
			if err := q.MarkPairingApplied(ctx, dbq.MarkPairingAppliedParams{ID: w.p.ID, OwnerUserID: userID}); err != nil {
				return err
			}
			n++
		}
		return q.UpdateImportJobStatus(ctx, dbq.UpdateImportJobStatusParams{Status: dbq.ImportJobsStatusConfirmed, FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true},
			ID: jobID, OwnerUserID: userID})
	})
	return n, err
}

// ---------- AI 参考答案（1.7b、3.3b、4.4、整卷，PRD v3 11.16 第 4 条） ----------

// EnsureAIAnswer 给还没有答案的题生成 AI 参考答案并缓存在题目上，返回题目的新答案。已有答案的直接返回。
// chargeAI 为 true 时计入每日 AI 出题额度（单题作答）；整卷里的题计入整卷额度，传 false。
// 额度用完返回 402（quota_type=ai_questions）；生成失败返回 AI_FAILED，不扣额度。
func (s *Service) EnsureAIAnswer(ctx context.Context, userID uint64, qr dbq.GetQuestionFullRow, chargeAI bool) (bool, error) {
	if strings.TrimSpace(qr.Answer.String) != "" {
		return false, nil
	}
	return GenerateQuestionAnswer(ctx, s.db, s.q, s.ai, s.quota, userID, qr, chargeAI, s.now().UTC())
}

// GenerateQuestionAnswer 是 EnsureAIAnswer 的实现，practice 包在没有 importer.Service 的地方也能用。
func GenerateQuestionAnswer(ctx context.Context, db *sql.DB, q *dbq.Queries, engine *ai.Engine, qs *quota.Service, userID uint64, qr dbq.GetQuestionFullRow, chargeAI bool, now time.Time) (bool, error) {
	if chargeAI {
		if left, err := qs.Remaining(ctx, userID, quota.AIQuestions); err != nil {
			return false, err
		} else if left != nil && *left <= 0 {
			return false, apperr.New(apperr.QuotaExceeded, "今天的 AI 出题次数用完了，明天 0 点恢复").With("quota_type", string(quota.AIQuestions)).With("remaining", 0).
				With("reason", "ai_answer")
		}
	}
	score := 0.0
	if v, err := strconv.ParseFloat(qr.Score.String, 64); err == nil && qr.Score.Valid {
		score = v
	}
	in, err := AnswerInput(ctx, q, userID, qr.BankID, string(qr.Qtype), FullStem(qr.Stem, qr.Passage.String), score)
	if err != nil {
		return false, err
	}
	if !ai.Subjective(string(qr.Qtype)) {
		in.Score = 0 // 客观题只要答案与解析，不要采分点
	}
	out, _, err := ai.GenerateAnswer.Run(ctx, engine, userID, in)
	if err != nil {
		return false, err
	}
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	qid := sql.NullInt64{Int64: int64(qr.ID), Valid: true}
	err = store.WithTx(ctx, db, func(tx *dbq.Queries) error {
		if chargeAI {
			if err := qs.Consume(ctx, tx, quota.Charge{UserID: userID, Type: quota.AIQuestions, Amount: 1,
				Ref: quota.Ref{Type: "question", ID: qr.ID}, Key: quota.KeyFor("ai_answer", qr.ID)}); err != nil {
				return err
			}
		}
		if err := tx.SetQuestionAnswer(ctx, dbq.SetQuestionAnswerParams{Answer: sql.NullString{String: out.Answer, Valid: true},
			AnswerOrigin: dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginAiGenerated, Valid: true},
			Analysis:     sql.NullString{}, AiAnswerAt: sql.NullTime{Time: now, Valid: true}, ID: qr.ID, OwnerUserID: owner}); err != nil {
			return err
		}
		if !ai.Subjective(string(qr.Qtype)) || len(out.Points) == 0 {
			return nil
		}
		cur, err := tx.ListQuestionRubric(ctx, dbq.ListQuestionRubricParams{QuestionID: qid, OwnerUserID: owner})
		if err != nil || len(cur) > 0 {
			return err // 用户已经填了采分点的，保留用户的
		}
		return insertRubric(ctx, tx, owner, qid, sql.NullInt64{}, out.Points, dbq.RubricPointsOriginAiGenerated)
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
