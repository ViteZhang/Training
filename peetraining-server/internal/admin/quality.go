package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/notify"
)

// statWindow 是质量统计的时间窗口：近 7 天。
const statWindow = 7 * 24 * time.Hour

// ---------- 7.5 资料解析监控 ----------

// FormatStat 是一种格式的解析结果。
type FormatStat struct {
	Format  string
	Files   int
	Pages   int
	OK      int
	Partial int
	Failed  int
	Rate    float64
}

// ParseStats 是 7.5 顶部：成功率（目标 ≥ 95%）、解析页数、平均与 P95 耗时、按格式的成功率与页数。
type ParseStats struct {
	SuccessRate float64
	Pages       int
	Files       int
	AvgSeconds  int
	P95Seconds  int
	Formats     []FormatStat
}

func (s *Service) ParseStats(ctx context.Context) (ParseStats, error) {
	since := s.now().UTC().Add(-statWindow)
	rows, err := s.q.AdminParseByFormat(ctx, since)
	if err != nil {
		return ParseStats{}, err
	}
	byFmt := map[string]*FormatStat{}
	var out ParseStats
	ok := 0
	for _, r := range rows {
		f := byFmt[string(r.Format)]
		if f == nil {
			f = &FormatStat{Format: string(r.Format)}
			byFmt[string(r.Format)] = f
		}
		f.Files += int(r.Files)
		f.Pages += int(r.Pages)
		switch r.Status {
		case dbq.MaterialsStatusParsed:
			f.OK += int(r.Files)
			ok += int(r.Files)
		case dbq.MaterialsStatusPartial:
			f.Partial += int(r.Files)
		default:
			f.Failed += int(r.Files)
		}
		out.Files += int(r.Files)
		out.Pages += int(r.Pages)
	}
	for _, f := range byFmt {
		if f.Files > 0 {
			f.Rate = float64(f.OK) / float64(f.Files)
		}
		out.Formats = append(out.Formats, *f)
	}
	sort.Slice(out.Formats, func(i, j int) bool { return out.Formats[i].Files > out.Formats[j].Files })
	if out.Files > 0 {
		out.SuccessRate = float64(ok) / float64(out.Files)
	}
	durs, err := s.q.AdminImportDurations(ctx, since)
	if err != nil {
		return ParseStats{}, err
	}
	if len(durs) > 0 {
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		sum := 0
		for _, d := range durs {
			sum += int(d)
		}
		out.AvgSeconds = sum / len(durs)
		out.P95Seconds = int(durs[min(len(durs)-1, len(durs)*95/100)])
	}
	return out, nil
}

// ImportJobRow 是失败或部分成功的解析任务。
type ImportJobRow struct {
	ID           uint64
	UserID       uint64
	Mode         string
	Status       string
	Files        int
	FailedFiles  int
	PartialFiles int
	BilledPages  int
	CreatedAt    time.Time
	FinishedAt   *time.Time
}

func (s *Service) FailedImports(ctx context.Context, before uint64) ([]ImportJobRow, error) {
	rows, err := s.q.AdminListImportJobs(ctx, dbq.AdminListImportJobsParams{BeforeID: int64(before)})
	if err != nil {
		return nil, err
	}
	out := make([]ImportJobRow, len(rows))
	for i, r := range rows {
		out[i] = ImportJobRow{ID: r.ID, UserID: r.OwnerUserID, Mode: string(r.Mode), Status: string(r.Status), Files: int(r.Files), FailedFiles: int(r.FailedFiles),
			PartialFiles: int(r.PartialFiles), BilledPages: int(r.BilledPages), CreatedAt: r.CreatedAt, FinishedAt: nullTimePtr(r.FinishedAt)}
	}
	return out, nil
}

// ImportFile 是任务里一个文件的识别日志：格式、页数、失败环节、重试次数、失败页（不含文件名与内容）。
type ImportFile struct {
	MaterialID  uint64
	Format      string
	Pages       int
	Step        string
	Status      string
	Attempts    int
	FailReason  string
	FailedPages []int
	UpdatedAt   time.Time
}

// ImportJobDetail 是 7.5 任务详情。
type ImportJobDetail struct {
	ImportJobRow
	ReservedPages  int
	PromptVersions map[string]string
	FileLogs       []ImportFile
}

func (s *Service) ImportJob(ctx context.Context, id uint64) (ImportJobDetail, error) {
	j, err := s.q.AdminGetImportJob(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportJobDetail{}, apperr.NotFoundErr()
	}
	if err != nil {
		return ImportJobDetail{}, err
	}
	files, err := s.q.AdminImportJobFiles(ctx, id)
	if err != nil {
		return ImportJobDetail{}, err
	}
	out := ImportJobDetail{ImportJobRow: ImportJobRow{ID: j.ID, UserID: j.OwnerUserID, Mode: string(j.Mode), Status: string(j.Status), Files: len(files),
		BilledPages: int(j.BilledPages), CreatedAt: j.CreatedAt, FinishedAt: nullTimePtr(j.FinishedAt)}, ReservedPages: int(j.ReservedPages)}
	if len(j.PromptVersions) > 0 {
		_ = json.Unmarshal(j.PromptVersions, &out.PromptVersions)
	}
	for _, f := range files {
		x := ImportFile{MaterialID: f.MaterialID, Format: string(f.Format), Pages: int(f.PageCount), Step: string(f.Step), Status: string(f.Status), Attempts: int(f.Attempts),
			FailReason: f.FailReason.String, UpdatedAt: f.UpdatedAt}
		if len(f.FailedPages) > 0 {
			_ = json.Unmarshal(f.FailedPages, &x.FailedPages)
		}
		switch f.Status {
		case dbq.ImportJobMaterialsStatusFailed:
			out.FailedFiles++
		case dbq.ImportJobMaterialsStatusPartial:
			out.PartialFiles++
		}
		out.FileLogs = append(out.FileLogs, x)
	}
	return out, nil
}

// RerunImport 重跑任务里失败的文件（7.5「重跑」；高精度 OCR 接入后在这里切换识别引擎，D38）。
func (s *Service) RerunImport(ctx context.Context, id uint64) (int, error) {
	d, err := s.ImportJob(ctx, id)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range d.FileLogs {
		if f.Status != "failed" && f.Status != "partial" {
			continue
		}
		if err := s.d.Import.RetryAsSystem(ctx, d.UserID, id, f.MaterialID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SendPhotoTips 给用户发拍照建议（7.5）。
func (s *Service) SendPhotoTips(ctx context.Context, jobID uint64) error {
	j, err := s.q.AdminGetImportJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	return s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: j.OwnerUserID, Mtype: dbq.MessagesMtypeAnnouncement, Title: "拍照小建议：让识别更准",
		Body: "光线充足、一张拍一页、页面铺平占满画面、避免反光和阴影；手写或很模糊的页可以改为粘贴文字导入",
		Link: notify.Link("import", nil), DedupeKey: sql.NullString{String: "photo_tips:" + strconv.FormatUint(jobID, 10), Valid: true}})
}

// ---------- 7.6 批改异议 ----------

// DisputeStats 是 7.6 顶部：异议率、本周异议数、重批后分数变化比例、平均处理时长、原因分布。
type DisputeStats struct {
	Disputes     int
	Gradings     int
	Rate         float64
	ChangedRatio float64
	AvgSeconds   int
	Reasons      map[string]int
}

func (s *Service) DisputeStats(ctx context.Context) (DisputeStats, error) {
	since := weekStart(s.now())
	r, err := s.q.AdminDisputeStats(ctx, dbq.AdminDisputeStatsParams{Since: since})
	if err != nil {
		return DisputeStats{}, err
	}
	reasons, err := s.q.AdminDisputeReasons(ctx, since)
	if err != nil {
		return DisputeStats{}, err
	}
	out := DisputeStats{Disputes: int(r.Disputes), Gradings: int(r.Gradings), AvgSeconds: int(r.AvgSeconds), Reasons: map[string]int{}}
	if r.Gradings > 0 {
		out.Rate = float64(r.Disputes) / float64(r.Gradings)
	}
	if r.Rechecked > 0 {
		out.ChangedRatio = float64(r.Changed) / float64(r.Rechecked)
	}
	for _, x := range reasons {
		out.Reasons[string(x.Reason)] = int(x.N)
	}
	return out, nil
}

// DisputeRow 是异议列表的一行（抽检队列）：只有原因、分数变化与状态；GrantID 不为 0 表示用户授权了查看。
type DisputeRow struct {
	ID          uint64
	UserID      uint64
	Reason      string
	Status      string
	ScoreBefore *float64
	ScoreAfter  *float64
	Attribution string
	GrantID     uint64
	CreatedAt   time.Time
	ResolvedAt  *time.Time
}

func parseScore(s sql.NullString) *float64 {
	if !s.Valid {
		return nil
	}
	v, err := strconv.ParseFloat(s.String, 64)
	if err != nil {
		return nil
	}
	return &v
}

func (s *Service) Disputes(ctx context.Context, status string, before uint64) ([]DisputeRow, error) {
	rows, err := s.q.AdminListDisputes(ctx, dbq.AdminListDisputesParams{Now: s.now().UTC(), Status: status, BeforeID: int64(before)})
	if err != nil {
		return nil, err
	}
	out := make([]DisputeRow, len(rows))
	for i, r := range rows {
		out[i] = DisputeRow{ID: r.ID, UserID: r.OwnerUserID, Reason: string(r.Reason), Status: string(r.Status), ScoreBefore: parseScore(r.ScoreBefore),
			ScoreAfter: parseScore(r.ScoreAfter), Attribution: string(r.Attribution.DisputesAttribution), GrantID: uint64(r.GrantID), CreatedAt: r.CreatedAt, ResolvedAt: nullTimePtr(r.ResolvedAt)}
	}
	return out, nil
}

// Attributions 是 7.6 人工归因。
var Attributions = []string{"rubric_incomplete", "model_error", "answer_insufficient"}

// AttributeDispute 写人工抽检结论，进入每周质量复盘。
func (s *Service) AttributeDispute(ctx context.Context, adminID, id uint64, attribution string) error {
	valid := false
	for _, a := range Attributions {
		valid = valid || a == attribution
	}
	if !valid {
		return apperr.New(apperr.BadRequest, "未知归因")
	}
	n, err := s.q.AdminSetDisputeAttribution(ctx, dbq.AdminSetDisputeAttributionParams{
		Attribution: dbq.NullDisputesAttribution{DisputesAttribution: dbq.DisputesAttribution(attribution), Valid: true}, Status: dbq.DisputesStatusSampled,
		HandledBy: sql.NullInt64{Int64: int64(adminID), Valid: true}, ResolvedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFoundErr()
	}
	return nil
}

// ---------- 7.7 用户反馈 ----------

// FeedbackStats 是 7.7 顶部：待回复数、平均首次回复时长、最多的类型、回复满意度。
type FeedbackStats struct {
	Open            int
	AvgReplySeconds int
	TopType         string
	Satisfaction    float64
	Types           map[string]int
}

func (s *Service) FeedbackStats(ctx context.Context) (FeedbackStats, error) {
	since := s.now().UTC().Add(-30 * 24 * time.Hour)
	r, err := s.q.AdminFeedbackStats(ctx, dbq.AdminFeedbackStatsParams{Since: since})
	if err != nil {
		return FeedbackStats{}, err
	}
	types, err := s.q.AdminFeedbackTypes(ctx, since)
	if err != nil {
		return FeedbackStats{}, err
	}
	out := FeedbackStats{Open: int(r.OpenCount), AvgReplySeconds: int(r.AvgReplySeconds), Satisfaction: float64(r.SatisfactionX100) / 100, Types: map[string]int{}}
	for i, t := range types {
		if i == 0 {
			out.TopType = string(t.Ftype)
		}
		out.Types[string(t.Ftype)] = int(t.N)
	}
	return out, nil
}

// FeedbackRow 是反馈列表的一行。反馈是用户写给客服的，显示内容；关联资料要授权才能看。
type FeedbackRow struct {
	ID          uint64
	UserID      uint64
	Type        string
	Content     string
	AllowAccess bool
	Status      string
	CreatedAt   time.Time
	RepliedAt   *time.Time
}

func (s *Service) Feedbacks(ctx context.Context, status string, before uint64) ([]FeedbackRow, error) {
	rows, err := s.q.AdminListFeedbacks(ctx, dbq.AdminListFeedbacksParams{Status: status, BeforeID: int64(before)})
	if err != nil {
		return nil, err
	}
	out := make([]FeedbackRow, len(rows))
	for i, r := range rows {
		out[i] = FeedbackRow{ID: r.ID, UserID: r.OwnerUserID, Type: string(r.Ftype), Content: r.Content, AllowAccess: r.AllowAccess, Status: string(r.Status),
			CreatedAt: r.CreatedAt, RepliedAt: nullTimePtr(r.RepliedAt)}
	}
	return out, nil
}

// FeedbackDetail 是 7.7 详情：反馈本身、截图、授权（截止时间）与关联解析任务。
type FeedbackDetail struct {
	FeedbackRow
	Reply           string
	Screenshots     []string // 预签名地址，1 小时有效
	GrantID         uint64
	GrantExpiresAt  *time.Time
	MaterialID      uint64
	RelatedImportID uint64
}

func (s *Service) Feedback(ctx context.Context, id uint64) (FeedbackDetail, error) {
	f, err := s.q.AdminGetFeedback(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return FeedbackDetail{}, apperr.NotFoundErr()
	}
	if err != nil {
		return FeedbackDetail{}, err
	}
	out := FeedbackDetail{FeedbackRow: FeedbackRow{ID: f.ID, UserID: f.OwnerUserID, Type: string(f.Ftype), Content: f.Content, AllowAccess: f.AllowAccess,
		Status: string(f.Status), CreatedAt: f.CreatedAt, RepliedAt: nullTimePtr(f.RepliedAt)}, Reply: f.Reply.String, MaterialID: uint64(f.RelatedMaterialID.Int64)}
	if len(f.ScreenshotKeys) > 0 && s.d.OSS != nil {
		var keys []string
		_ = json.Unmarshal(f.ScreenshotKeys, &keys)
		for _, k := range keys {
			if u, err := s.d.OSS.PresignGet(ctx, k, "", time.Hour); err == nil {
				out.Screenshots = append(out.Screenshots, u.URL)
			}
		}
	}
	if g, err := s.q.GetGrantBySource(ctx, dbq.GetGrantBySourceParams{Source: dbq.ContentAccessGrantsSourceFeedback, SourceID: id, ExpiresAt: s.now().UTC()}); err == nil {
		out.GrantID = g.ID
		out.GrantExpiresAt = &g.ExpiresAt
	}
	if f.RelatedMaterialID.Valid {
		if j, err := s.q.AdminMaterialJob(ctx, uint64(f.RelatedMaterialID.Int64)); err == nil {
			out.RelatedImportID = j
		}
	}
	return out, nil
}

// ReplyFeedback 回复到用户的消息中心（7.7）。
func (s *Service) ReplyFeedback(ctx context.Context, adminID, id uint64, reply string) error {
	if len([]rune(reply)) < 2 || len([]rune(reply)) > 1000 {
		return apperr.New(apperr.BadRequest, "回复需在 2–1000 字之间")
	}
	return s.d.Notify.Reply(ctx, adminID, id, reply)
}

// ReparseFeedbackMaterial 重新识别反馈关联的资料（7.7）。
func (s *Service) ReparseFeedbackMaterial(ctx context.Context, id uint64) error {
	d, err := s.Feedback(ctx, id)
	if err != nil {
		return err
	}
	if d.MaterialID == 0 || d.RelatedImportID == 0 {
		return apperr.New(apperr.BadRequest, "这条反馈没有关联的资料")
	}
	return s.d.Import.RetryAsSystem(ctx, d.UserID, d.RelatedImportID, d.MaterialID)
}

// ---------- 授权查看（PRD 10.1） ----------

// GrantedMaterial 是授权内的资料：格式、页数与逐页识别文字（最多 30 页）。
type GrantedMaterial struct {
	ID        uint64
	Format    string
	Pages     int
	Status    string
	PageTexts []string
	ExpiresAt time.Time
}

// ViewFeedbackMaterial 读反馈授权的资料：先经 notify.RecordAccess 校验授权、记日志并通知用户，再按授权所属用户读。
func (s *Service) ViewFeedbackMaterial(ctx context.Context, adminID, feedbackID uint64) (GrantedMaterial, error) {
	g, err := s.q.GetGrantBySource(ctx, dbq.GetGrantBySourceParams{Source: dbq.ContentAccessGrantsSourceFeedback, SourceID: feedbackID, ExpiresAt: s.now().UTC()})
	if errors.Is(err, sql.ErrNoRows) {
		return GrantedMaterial{}, notify.ErrNoGrant
	}
	if err != nil {
		return GrantedMaterial{}, err
	}
	f, err := s.q.AdminGetFeedback(ctx, feedbackID)
	if err != nil || !f.RelatedMaterialID.Valid {
		return GrantedMaterial{}, apperr.New(apperr.BadRequest, "这条反馈没有关联的资料")
	}
	mid := uint64(f.RelatedMaterialID.Int64)
	owner, err := s.d.Notify.RecordAccess(ctx, adminID, g.ID, notify.Target{Type: "material", ID: mid})
	if err != nil {
		return GrantedMaterial{}, err
	}
	m, err := s.q.GrantedMaterial(ctx, dbq.GrantedMaterialParams{ID: mid, OwnerUserID: owner})
	if errors.Is(err, sql.ErrNoRows) {
		return GrantedMaterial{}, apperr.NotFoundErr()
	}
	if err != nil {
		return GrantedMaterial{}, err
	}
	pages, err := s.q.GrantedMaterialPages(ctx, dbq.GrantedMaterialPagesParams{MaterialID: mid, OwnerUserID: owner})
	if err != nil {
		return GrantedMaterial{}, err
	}
	out := GrantedMaterial{ID: m.ID, Format: string(m.Format), Pages: int(m.PageCount), Status: string(m.Status), ExpiresAt: g.ExpiresAt}
	for _, p := range pages {
		out.PageTexts = append(out.PageTexts, p.Text)
	}
	return out, nil
}

// GrantedGrading 是授权内的异议详情：题目、采分点来源与内容、作答原文、重批前后分数。
type GrantedGrading struct {
	Stem         string
	QType        string
	Answer       string
	Rubric       json.RawMessage
	PointResults json.RawMessage
	Score        *float64
	FullScore    *float64
	Regrade      *GrantedGradingScores
	ExpiresAt    time.Time
}

// GrantedGradingScores 是重批后的逐点结果与分数。
type GrantedGradingScores struct {
	Rubric       json.RawMessage
	PointResults json.RawMessage
	Score        *float64
}

// ViewDispute 读异议授权的题目与作答（7.6），每次都记日志并通知用户。
func (s *Service) ViewDispute(ctx context.Context, adminID, disputeID uint64) (GrantedGrading, error) {
	g, err := s.q.GetGrantBySource(ctx, dbq.GetGrantBySourceParams{Source: dbq.ContentAccessGrantsSourceDispute, SourceID: disputeID, ExpiresAt: s.now().UTC()})
	if errors.Is(err, sql.ErrNoRows) {
		return GrantedGrading{}, notify.ErrNoGrant
	}
	if err != nil {
		return GrantedGrading{}, err
	}
	d, err := s.q.AdminGetDispute(ctx, disputeID)
	if err != nil {
		return GrantedGrading{}, err
	}
	owner, err := s.d.Notify.RecordAccess(ctx, adminID, g.ID, notify.Target{Type: "grading", ID: d.GradingID})
	if err != nil {
		return GrantedGrading{}, err
	}
	r, err := s.q.GrantedGrading(ctx, dbq.GrantedGradingParams{ID: d.GradingID, OwnerUserID: owner})
	if err != nil {
		return GrantedGrading{}, fmt.Errorf("读取授权的批改：%w", err)
	}
	out := GrantedGrading{Stem: r.Stem, QType: string(r.Qtype), Answer: r.AnswerText.String, Rubric: json.RawMessage(r.RubricSnapshot), PointResults: json.RawMessage(r.PointResults),
		Score: parseScore(r.Score), FullScore: parseScore(r.FullScore), ExpiresAt: g.ExpiresAt}
	if d.RegradeID.Valid {
		if rg, err := s.q.GrantedGrading(ctx, dbq.GrantedGradingParams{ID: uint64(d.RegradeID.Int64), OwnerUserID: owner}); err == nil {
			out.Regrade = &GrantedGradingScores{Rubric: json.RawMessage(rg.RubricSnapshot), PointResults: json.RawMessage(rg.PointResults), Score: parseScore(rg.Score)}
		}
	}
	return out, nil
}

// AccessLogs 是授权查看日志（7.6、7.7 可查；userID 为 0 时全部）。
type AccessLog struct {
	ID         uint64
	GrantID    uint64
	AdminName  string
	UserID     uint64
	TargetType string
	TargetID   uint64
	CreatedAt  time.Time
}

func (s *Service) AccessLogs(ctx context.Context, userID uint64) ([]AccessLog, error) {
	rows, err := s.q.ListContentAccessLogs(ctx, dbq.ListContentAccessLogsParams{UserID: int64(userID)})
	if err != nil {
		return nil, err
	}
	out := make([]AccessLog, len(rows))
	for i, r := range rows {
		out[i] = AccessLog{ID: r.ID, GrantID: r.GrantID, AdminName: r.DisplayName.String, UserID: r.UserID, TargetType: r.TargetType, TargetID: r.TargetID, CreatedAt: r.CreatedAt}
	}
	return out, nil
}
