package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/asr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/rules"
)

// 背诵：4.14 挖空、4.15 默写、4.16 口述、4.17 背诵完成（T20）。
// 背诵只改掌握分与背诵复习日，不算作答验证（PRD 11.1 补充）；复习间隔按 PRD 11.3。

const (
	reciteBatch    = 10 // 一轮背 10 条
	maxKeywords    = 8
	audioMaxSize   = 20 << 20
	reciteAudioDir = "/recite/"
)

var audioExt = map[string]string{"audio/m4a": "m4a", "audio/mp4": "m4a", "audio/aac": "aac", "audio/wav": "wav", "audio/mpeg": "mp3"}

// Segment 是原文的一段；Blank 为 true 的是采分关键词，挖空时遮住。
type Segment struct {
	Text  string
	Blank bool
}

// ReciteItem 是一条要背的知识点。
type ReciteItem struct {
	KPID         uint64
	Name         string
	Path         []string
	OriginalText string
	Segments     []Segment
	Keywords     []string
	MaterialID   uint64
	FileName     string
	Page         int
	Result       string
}

// ReciteSession 是一轮背诵。
type ReciteSession struct {
	ID, SubjectID uint64
	Title         string
	Items         []ReciteItem
	DoneCount     int
	OralEnabled   bool
}

// keywordsOf 取知识点的采分关键词：采分点上标注的关键词；没有时用能在原文里找到的采分点内容；
// 再没有时从原文里按标点切出 2–10 字的短语。只保留原文里找得到的，最多 8 个。
func (s *Service) keywordsOf(ctx context.Context, userID, kpID uint64, original string) ([]string, error) {
	rp, err := s.q.ListKPRubric(ctx, dbq.ListKPRubricParams{KpID: sql.NullInt64{Int64: int64(kpID), Valid: true}, OwnerUserID: owner(userID)})
	if err != nil {
		return nil, err
	}
	var cands []string
	for _, p := range rp {
		var kws []string
		if p.Keywords != nil {
			_ = json.Unmarshal(p.Keywords, &kws)
		}
		cands = append(cands, kws...)
	}
	if len(cands) == 0 {
		for _, p := range rp {
			cands = append(cands, p.Content)
		}
	}
	if len(cands) == 0 {
		for _, part := range strings.FieldsFunc(original, func(r rune) bool { return strings.ContainsRune("，。、；：,.;:！？（）()「」“”\n ", r) }) {
			if n := utf8.RuneCountInString(part); n >= 2 && n <= 10 {
				cands = append(cands, part)
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, k := range cands {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] || !strings.Contains(original, k) {
			continue
		}
		seen[k] = true
		out = append(out, k)
		if len(out) >= maxKeywords {
			break
		}
	}
	return out, nil
}

// segmentsOf 把原文按关键词切段：每个关键词取第一次出现、互不重叠的位置。
func segmentsOf(original string, keywords []string) []Segment {
	type span struct{ start, end int }
	var spans []span
	for _, k := range keywords {
		from := 0
		for {
			i := strings.Index(original[from:], k)
			if i < 0 {
				break
			}
			st, en := from+i, from+i+len(k)
			overlap := false
			for _, sp := range spans {
				if st < sp.end && en > sp.start {
					overlap = true
					break
				}
			}
			if !overlap {
				spans = append(spans, span{st, en})
				break
			}
			from = st + len(k)
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var out []Segment
	pos := 0
	for _, sp := range spans {
		if sp.start > pos {
			out = append(out, Segment{Text: original[pos:sp.start]})
		}
		out = append(out, Segment{Text: original[sp.start:sp.end], Blank: true})
		pos = sp.end
	}
	if pos < len(original) {
		out = append(out, Segment{Text: original[pos:]})
	}
	return out
}

// KeywordHit 是一个关键词是否写到 / 说到。
type KeywordHit struct {
	Text string `json:"text"`
	Hit  bool   `json:"hit"`
}

// coverage 是默写与口述的关键词比对（规则，不调模型，PRD 12.1）：去掉空白与标点后包含即算写到。
func coverage(text string, keywords []string) []KeywordHit {
	t := normText(text)
	out := make([]KeywordHit, len(keywords))
	for i, k := range keywords {
		out[i] = KeywordHit{Text: k, Hit: normText(k) != "" && strings.Contains(t, normText(k))}
	}
	return out
}

// resultOf 按关键词覆盖判定背诵结果：全部写到为记住了，过半为模糊，其余没记住。
func resultOf(hits []KeywordHit) rules.ReciteResult {
	if len(hits) == 0 {
		return rules.ReciteVague
	}
	n := 0
	for _, h := range hits {
		if h.Hit {
			n++
		}
	}
	switch {
	case n == len(hits):
		return rules.ReciteRemembered
	case n*2 >= len(hits):
		return rules.ReciteVague
	}
	return rules.ReciteForgot
}

func (s *Service) oralEnabled(ctx context.Context, userID uint64) bool {
	if s.flags == nil {
		return false
	}
	ok, err := s.flags.Enabled(ctx, flags.OralRecite, userID)
	return err == nil && ok
}

// reciteDue 报告知识点的背诵是否到期：从没背过，或背诵复习日已到。
func reciteDue(next sql.NullTime, today rules.Day) bool {
	return !next.Valid || rules.DayFromDateColumn(next.Time) <= today
}

// CreateRecite 开始一轮背诵：今日计划里背诵组的知识点优先，其余到期或从没背过的按掌握分从低到高，最多 10 条；
// retryOf 不为 0 时只背那一轮里没记住的。当天已有进行中的一轮（不是再背）时继续那一轮。
func (s *Service) CreateRecite(ctx context.Context, userID, subjectID, retryOf uint64) (ReciteSession, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return ReciteSession{}, err
	}
	sub := sql.NullInt64{Int64: int64(b.SubjectID), Valid: true}
	var ids []uint64
	if retryOf != 0 {
		prev, err := s.q.GetPracticeSession(ctx, dbq.GetPracticeSessionParams{ID: retryOf, OwnerUserID: userID})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (prev.Kind != dbq.PracticeSessionsKindRecite || prev.SubjectID != sub)) {
			return ReciteSession{}, apperr.NotFoundErr()
		}
		if err != nil {
			return ReciteSession{}, err
		}
		recs, err := s.q.ListSessionRecites(ctx, dbq.ListSessionRecitesParams{PracticeSessionID: sql.NullInt64{Int64: int64(retryOf), Valid: true}, OwnerUserID: userID})
		if err != nil {
			return ReciteSession{}, err
		}
		last := map[uint64]string{}
		var order []uint64
		for _, r := range recs {
			if _, ok := last[r.KpID]; !ok {
				order = append(order, r.KpID)
			}
			last[r.KpID] = string(r.Result.ReciteRecordsResult)
		}
		for _, id := range order {
			if last[id] == string(rules.ReciteForgot) {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return ReciteSession{}, apperr.New(apperr.BadRequest, "这一轮没有没记住的")
		}
	} else {
		cur, err := s.q.LatestInProgressSession(ctx, dbq.LatestInProgressSessionParams{OwnerUserID: userID, SubjectID: sub})
		if err == nil && cur.Kind == dbq.PracticeSessionsKindRecite && rules.DayOf(cur.StartedAt) == s.today() {
			return s.GetRecite(ctx, userID, cur.ID)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ReciteSession{}, err
		}
		cands, err := s.q.ListReciteCandidates(ctx, dbq.ListReciteCandidatesParams{BankID: b.BankID, OwnerUserID: owner(userID)})
		if err != nil {
			return ReciteSession{}, err
		}
		planned := map[uint64]int{}
		if pl, err := s.plan.Today(ctx, userID); err == nil {
			for i, it := range pl.Items {
				if it.SubjectID == b.SubjectID && it.Group == string(rules.GroupRecite) {
					planned[it.KPID] = i + 1
				}
			}
		} else {
			return ReciteSession{}, err
		}
		today := s.today()
		var due []dbq.ListReciteCandidatesRow
		for _, c := range cands {
			if planned[c.ID] > 0 || reciteDue(c.ReciteNextReviewOn, today) {
				due = append(due, c)
			}
		}
		sort.SliceStable(due, func(i, j int) bool {
			pi, pj := planned[due[i].ID], planned[due[j].ID]
			if (pi > 0) != (pj > 0) {
				return pi > 0
			}
			if pi != pj {
				return pi < pj
			}
			mi, _ := strconv.ParseFloat(due[i].M, 64)
			mj, _ := strconv.ParseFloat(due[j].M, 64)
			return mi < mj
		})
		for _, c := range due[:min(len(due), reciteBatch)] {
			ids = append(ids, c.ID)
		}
		if len(ids) == 0 {
			return ReciteSession{}, apperr.New(apperr.BadRequest, "今天没有到期要背的内容")
		}
	}
	idsJSON, _ := json.Marshal(ids)
	title := "背诵"
	if retryOf != 0 {
		title = "再背没记住的"
	}
	id, err := s.q.InsertPracticeSession(ctx, dbq.InsertPracticeSessionParams{OwnerUserID: userID, SubjectID: sub, Kind: dbq.PracticeSessionsKindRecite,
		Title: title, Config: dbtypes.NullJSON("{}"), QuestionIds: idsJSON, StartedAt: s.now().UTC()})
	if err != nil {
		return ReciteSession{}, err
	}
	return s.GetRecite(ctx, userID, uint64(id))
}

// reciteSession 读出一轮背诵（不是背诵会话的当作不存在）。
func (s *Service) reciteSession(ctx context.Context, q *dbq.Queries, userID, id uint64, lock bool) (dbq.PracticeSession, []uint64, error) {
	row, ids, _, err := s.session(ctx, q, userID, id, lock)
	if err != nil {
		return row, nil, err
	}
	if row.Kind != dbq.PracticeSessionsKindRecite {
		return row, nil, apperr.NotFoundErr()
	}
	return row, ids, nil
}

// GetRecite 返回一轮背诵与每条的原文、挖空与关键词。被删除的知识点跳过。
func (s *Service) GetRecite(ctx context.Context, userID, id uint64) (ReciteSession, error) {
	row, ids, err := s.reciteSession(ctx, s.q, userID, id, false)
	if err != nil {
		return ReciteSession{}, err
	}
	out := ReciteSession{ID: row.ID, SubjectID: uint64(row.SubjectID.Int64), Title: row.Title, Items: []ReciteItem{}, OralEnabled: s.oralEnabled(ctx, userID)}
	recs, err := s.q.ListSessionRecites(ctx, dbq.ListSessionRecitesParams{PracticeSessionID: sql.NullInt64{Int64: int64(id), Valid: true}, OwnerUserID: userID})
	if err != nil {
		return ReciteSession{}, err
	}
	last := map[uint64]string{}
	for _, r := range recs {
		last[r.KpID] = string(r.Result.ReciteRecordsResult)
	}
	var tree map[uint64]dbq.ListBankKPsFullRow
	files := map[uint64]string{}
	for _, kid := range ids {
		kp, err := s.q.GetKP(ctx, dbq.GetKPParams{ID: kid, OwnerUserID: owner(userID)})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return ReciteSession{}, err
		}
		if tree == nil {
			rows, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: kp.BankID, Owner: owner(userID)})
			if err != nil {
				return ReciteSession{}, err
			}
			tree = map[uint64]dbq.ListBankKPsFullRow{}
			for _, r := range rows {
				tree[r.ID] = r
			}
		}
		it := ReciteItem{KPID: kp.ID, Name: kp.Name, OriginalText: kp.OriginalText.String, Result: last[kp.ID], Path: []string{}}
		for p, depth := kp.ParentID, 0; p.Valid && depth < 8; depth++ {
			n, ok := tree[uint64(p.Int64)]
			if !ok {
				break
			}
			it.Path = append([]string{n.Name}, it.Path...)
			p = n.ParentID
		}
		if it.Keywords, err = s.keywordsOf(ctx, userID, kp.ID, it.OriginalText); err != nil {
			return ReciteSession{}, err
		}
		it.Segments = segmentsOf(it.OriginalText, it.Keywords)
		if kp.SourceMaterialID.Valid {
			mid := uint64(kp.SourceMaterialID.Int64)
			name, ok := files[mid]
			if !ok {
				if m, err := s.q.GetMaterial(ctx, dbq.GetMaterialParams{ID: mid, OwnerUserID: userID}); err == nil {
					name = m.FileName
				}
				files[mid] = name
			}
			if name != "" {
				it.MaterialID, it.FileName, it.Page = mid, name, int(kp.SourcePage.Int32)
			}
		}
		if it.Result != "" {
			out.DoneCount++
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}

// ReciteInput 是一条背诵结果。
type ReciteInput struct {
	KPID     uint64
	Mode     string
	Result   string
	Text     string
	AudioKey string
	Key      string
}

// ReciteRecord 是记一条背诵的结果。
type ReciteRecord struct {
	Result     string
	Coverage   []KeywordHit
	Transcript string
	NextReview rules.Day
	M          float64
}

func reciteAudioPrefix(userID uint64) string {
	return "u/" + strconv.FormatUint(userID, 10) + reciteAudioDir
}

// ReciteAudioUpload 申请口述录音的直传地址（功能开关关闭时 404）。
func (s *Service) ReciteAudioUpload(ctx context.Context, userID, sessionID uint64, contentType string, size int64) (UploadTarget, error) {
	if !s.oralEnabled(ctx, userID) {
		return UploadTarget{}, apperr.NotFoundErr()
	}
	if _, _, err := s.reciteSession(ctx, s.q, userID, sessionID, false); err != nil {
		return UploadTarget{}, err
	}
	ext, ok := audioExt[contentType]
	if !ok {
		return UploadTarget{}, apperr.New(apperr.BadRequest, "录音格式不支持")
	}
	if size <= 0 || size > audioMaxSize {
		return UploadTarget{}, apperr.New(apperr.BadRequest, "录音不能超过 20MB")
	}
	key := reciteAudioPrefix(userID) + strconv.FormatUint(sessionID, 10) + "-" + strconv.FormatInt(s.now().UnixNano(), 36) + "." + ext
	p, err := s.oss.PresignPut(ctx, key, contentType, size, handwritingTTL)
	if err != nil {
		return UploadTarget{}, err
	}
	return UploadTarget{ObjectKey: key, URL: p.URL, Headers: p.Headers, ExpiresAt: p.ExpiresAt}, nil
}

// RecordRecite 记一条背诵：挖空按自评；默写与口述按关键词覆盖判定。按 PRD 11.1 改掌握分（记住了 +8、模糊 +2、没记住 −8），
// 按 11.3 排背诵复习日（没记住 1 天后并重置、模糊 2 天后、记住了 3 → 7 → 15 → 30 天）。同一个幂等键只记一次。
func (s *Service) RecordRecite(ctx context.Context, userID, sessionID uint64, in ReciteInput) (ReciteRecord, error) {
	if len(in.Key) < 8 {
		return ReciteRecord{}, apperr.New(apperr.BadRequest, "缺少幂等键")
	}
	if prev, err := s.q.GetReciteRecordByKey(ctx, dbq.GetReciteRecordByKeyParams{OwnerUserID: userID, IdempotencyKey: sql.NullString{String: in.Key, Valid: true}}); err == nil {
		return s.replayRecite(ctx, userID, prev)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ReciteRecord{}, err
	}
	row, ids, err := s.reciteSession(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return ReciteRecord{}, err
	}
	idx := indexOf(ids, in.KPID)
	if idx < 0 {
		return ReciteRecord{}, apperr.NotFoundErr()
	}
	kp, err := s.q.GetKP(ctx, dbq.GetKPParams{ID: in.KPID, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return ReciteRecord{}, apperr.NotFoundErr()
	}
	if err != nil {
		return ReciteRecord{}, err
	}
	var out ReciteRecord
	var result rules.ReciteResult
	switch in.Mode {
	case "cloze":
		result = rules.ReciteResult(in.Result)
		if result != rules.ReciteForgot && result != rules.ReciteVague && result != rules.ReciteRemembered {
			return ReciteRecord{}, apperr.New(apperr.BadRequest, "请选择没记住、模糊或记住了")
		}
	case "dictation", "oral":
		text := in.Text
		if in.Mode == "oral" {
			if !s.oralEnabled(ctx, userID) {
				return ReciteRecord{}, apperr.NotFoundErr()
			}
			if !strings.HasPrefix(in.AudioKey, reciteAudioPrefix(userID)) || strings.Contains(in.AudioKey, "..") {
				return ReciteRecord{}, apperr.NotFoundErr()
			}
			if info, err := s.oss.Head(ctx, in.AudioKey); err != nil || !info.Exists {
				return ReciteRecord{}, apperr.NotFoundErr()
			}
			if text, err = s.asr.Transcribe(ctx, asr.Audio{ObjectKey: in.AudioKey, Format: strings.TrimPrefix(in.AudioKey[strings.LastIndex(in.AudioKey, "."):], ".")}); err != nil {
				return ReciteRecord{}, err
			}
			out.Transcript = text
		}
		if strings.TrimSpace(text) == "" {
			return ReciteRecord{}, apperr.New(apperr.BadRequest, "没有写下或说出内容")
		}
		kws, err := s.keywordsOf(ctx, userID, kp.ID, kp.OriginalText.String)
		if err != nil {
			return ReciteRecord{}, err
		}
		out.Coverage = coverage(text, kws)
		result = resultOf(out.Coverage)
	default:
		return ReciteRecord{}, apperr.New(apperr.BadRequest, "背诵方式不正确")
	}
	out.Result = string(result)
	p, err := s.params.Rules(ctx)
	if err != nil {
		return ReciteRecord{}, err
	}
	today := s.today()
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		m, err := q.GetKPMasteryForUpdate(ctx, dbq.GetKPMasteryForUpdateParams{OwnerUserID: userID, KpID: kp.ID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		cur, _ := strconv.ParseFloat(m.M, 64)
		out.M = rules.ApplyMastery(cur, rules.MasteryEvent{Kind: rules.EventRecite, Recite: result}, m.Answered, p.Mastery)
		next, step := rules.NextReview(rules.ReciteOutcome(result), int(m.ReciteIntervalStep), today, p.Review)
		out.NextReview = next
		state := rules.StateOf(rules.MasteryFacts{M: out.M, Viewed: true, Assessed: m.LastSelfAssess.Valid, Answered: m.Answered, CorrectDays: decodeDays(m.CorrectDates)}, today, p.MasteryState)
		if err := q.UpsertKPRecite(ctx, dbq.UpsertKPReciteParams{OwnerUserID: userID, KpID: kp.ID, M: fmtM(out.M), State: dbq.KpMasteryState(state),
			ReciteNextReviewOn: nullDay(next), ReciteIntervalStep: uint8(step)}); err != nil {
			return err
		}
		cov := dbtypes.NullJSON(nil)
		if out.Coverage != nil {
			cov, _ = json.Marshal(map[string]any{"keywords": out.Coverage, "transcript": out.Transcript})
		}
		if _, err := q.InsertReciteRecord(ctx, dbq.InsertReciteRecordParams{OwnerUserID: userID, KpID: kp.ID, PracticeSessionID: sql.NullInt64{Int64: int64(sessionID), Valid: true},
			Mode: dbq.ReciteRecordsMode(in.Mode), Result: dbq.NullReciteRecordsResult{ReciteRecordsResult: dbq.ReciteRecordsResult(result), Valid: true},
			Coverage: cov, IdempotencyKey: sql.NullString{String: in.Key, Valid: true}, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
		if next := uint32(idx + 1); next > row.CursorIndex {
			return q.SetSessionCursor(ctx, dbq.SetSessionCursorParams{CursorIndex: next, ID: sessionID, OwnerUserID: userID})
		}
		return nil
	})
	return out, err
}

// replayRecite 重放已记过的一条（幂等）：结果与覆盖取当时的记录，掌握分与复习日取现在的。
func (s *Service) replayRecite(ctx context.Context, userID uint64, r dbq.ReciteRecord) (ReciteRecord, error) {
	out := ReciteRecord{Result: string(r.Result.ReciteRecordsResult)}
	if r.Coverage != nil {
		var c struct {
			Keywords   []KeywordHit `json:"keywords"`
			Transcript string       `json:"transcript"`
		}
		_ = json.Unmarshal(r.Coverage, &c)
		out.Coverage, out.Transcript = c.Keywords, c.Transcript
	}
	m, err := s.q.GetKPMastery(ctx, dbq.GetKPMasteryParams{OwnerUserID: userID, KpID: r.KpID})
	if err != nil {
		return out, err
	}
	out.M, _ = strconv.ParseFloat(m.M, 64)
	if m.ReciteNextReviewOn.Valid {
		out.NextReview = rules.DayFromDateColumn(m.ReciteNextReviewOn.Time)
	}
	return out, nil
}

// ReciteSummary 是 4.17 背诵完成。
type ReciteSummary struct {
	Remembered         int  `json:"remembered"`
	Vague              int  `json:"vague"`
	Forgot             int  `json:"forgot"`
	PreviousRemembered *int `json:"previous_remembered,omitempty"`
	Tomorrow           int  `json:"tomorrow"`
	TwoDays            int  `json:"two_days"`
	Later              int  `json:"later"`
}

// FinishRecite 结束一轮背诵：按每条最后一次结果统计，与同一门课上一轮对比，给出下次复习安排。已结束的返回保存的结果。
func (s *Service) FinishRecite(ctx context.Context, userID, sessionID uint64) (ReciteSummary, error) {
	row, _, err := s.reciteSession(ctx, s.q, userID, sessionID, false)
	if err != nil {
		return ReciteSummary{}, err
	}
	if row.Status == dbq.PracticeSessionsStatusFinished && row.Summary != nil {
		var sum ReciteSummary
		if json.Unmarshal(row.Summary, &sum) == nil {
			return sum, nil
		}
	}
	recs, err := s.q.ListSessionRecites(ctx, dbq.ListSessionRecitesParams{PracticeSessionID: sql.NullInt64{Int64: int64(sessionID), Valid: true}, OwnerUserID: userID})
	if err != nil {
		return ReciteSummary{}, err
	}
	last := map[uint64]string{}
	for _, r := range recs {
		last[r.KpID] = string(r.Result.ReciteRecordsResult)
	}
	var sum ReciteSummary
	for _, res := range last {
		switch rules.ReciteResult(res) {
		case rules.ReciteRemembered:
			sum.Remembered++
			sum.Later++
		case rules.ReciteVague:
			sum.Vague++
			sum.TwoDays++
		default:
			sum.Forgot++
			sum.Tomorrow++
		}
	}
	prev, err := s.q.PreviousFinishedSession(ctx, dbq.PreviousFinishedSessionParams{OwnerUserID: userID, SubjectID: row.SubjectID, Kind: dbq.PracticeSessionsKindRecite, ID: sessionID})
	if err == nil && prev != nil {
		var p ReciteSummary
		if json.Unmarshal(prev, &p) == nil {
			sum.PreviousRemembered = &p.Remembered
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReciteSummary{}, err
	}
	raw, _ := json.Marshal(sum)
	if err := s.q.FinishPracticeSession(ctx, dbq.FinishPracticeSessionParams{Summary: raw, FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: sessionID, OwnerUserID: userID}); err != nil {
		return ReciteSummary{}, err
	}
	return sum, nil
}
