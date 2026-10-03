package export

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/logx"
)

// 下载链接与文件保留时长（PRD 6.4：生成后保存到手机或分享到微信，24 小时后删除）。
const (
	linkTTL = time.Hour
	keepFor = 24 * time.Hour
)

// Enqueuer 排后台任务。
type Enqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

type Service struct {
	q     *dbq.Queries
	oss   oss.Store
	queue Enqueuer
	fonts Fonts
	now   func() time.Time
}

// Deps 是创建服务的依赖。Queue 为空时同步生成（测试与本地）。
type Deps struct {
	DB    *sql.DB
	OSS   oss.Store
	Queue Enqueuer
	Fonts Fonts
	Now   func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{q: dbq.New(d.DB), oss: d.OSS, queue: d.Queue, fonts: d.Fonts, now: now}
}

// Options 是导出内容（6.4 可勾选）；AI 出的变式题默认不勾。
type Options struct {
	Questions  bool `json:"questions"`
	KPs        bool `json:"kps"`
	Wrong      bool `json:"wrong"`
	AIVariants bool `json:"ai_variants"`
}

var qtypeNames = map[string]string{"single_choice": "单选题", "multi_choice": "多选题", "true_false": "判断题", "fill_blank": "填空题", "term": "名词解释",
	"short_answer": "简答题", "discussion": "论述题", "essay": "作文", "calculation": "计算题", "other": "其他"}

var lossNames = map[string]string{"knowledge": "知识没掌握", "norm": "答题不规范", "time": "时间不够"}

type subject struct {
	ID, BankID uint64
	Name       string
}

func (s *Service) subject(ctx context.Context, userID, subjectID uint64) (subject, error) {
	r, err := s.q.GetSubject(ctx, dbq.GetSubjectParams{ID: subjectID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return subject{}, apperr.NotFoundErr()
	}
	if err != nil {
		return subject{}, err
	}
	return subject{ID: r.ID, BankID: r.BankID, Name: r.Name}, nil
}

func owner(userID uint64) sql.NullInt64 { return sql.NullInt64{Int64: int64(userID), Valid: true} }

// parts 是按导出内容分开的几部分文档（预览时分别算题数与页数）。
type parts struct {
	questions, ai, kps, wrong     Document
	nQuestions, nAI, nKPs, nWrong int
}

// build 读出一门课自己的内容并排版（只查本人的数据）。
func (s *Service) build(ctx context.Context, userID uint64, sub subject) (parts, error) {
	var p parts
	qs, err := s.q.ExportQuestions(ctx, dbq.ExportQuestionsParams{BankID: sub.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return p, err
	}
	rps, err := s.q.ExportRubricPoints(ctx, dbq.ExportRubricPointsParams{BankID: sub.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return p, err
	}
	points := map[uint64][]string{}
	for _, r := range rps {
		if !r.QuestionID.Valid {
			continue
		}
		t := strconv.Itoa(int(r.Seq)) + ". " + r.Content
		if r.Score.Valid {
			t += "（" + strings.TrimRight(strings.TrimRight(r.Score.String, "0"), ".") + " 分）"
		}
		points[uint64(r.QuestionID.Int64)] = append(points[uint64(r.QuestionID.Int64)], t)
	}
	write := func(d *Document, n *int, last *string, q dbq.ExportQuestionsRow, tag string) {
		if string(q.Qtype) != *last {
			*last = string(q.Qtype)
			d.add(Sub, qtypeNames[string(q.Qtype)])
		}
		*n++
		head := strconv.Itoa(*n) + ". " + q.Stem
		var meta []string
		if q.ExamYear.Valid {
			meta = append(meta, strconv.Itoa(int(q.ExamYear.Int16))+" 年真题")
		}
		if tag != "" {
			meta = append(meta, tag)
		}
		if q.Score.Valid {
			meta = append(meta, strings.TrimRight(strings.TrimRight(q.Score.String, "0"), ".")+" 分")
		}
		d.add(Para, head)
		if len(meta) > 0 {
			d.add(Note, strings.Join(meta, " · "))
		}
		var opts []struct{ Key, Text string }
		if len(q.Options) > 0 && json.Unmarshal(q.Options, &opts) == nil {
			for _, o := range opts {
				d.add(Para, o.Key+". "+o.Text)
			}
		}
		if q.Answer.Valid && strings.TrimSpace(q.Answer.String) != "" {
			d.add(Para, "参考答案："+q.Answer.String)
		}
		if ps := points[q.ID]; len(ps) > 0 {
			d.add(Note, "采分点：\n"+strings.Join(ps, "\n"))
		}
	}
	p.questions.add(Heading, "题目和参考答案")
	p.ai.add(Heading, "AI 出的变式题")
	var lastQ, lastAI string
	for _, q := range qs {
		if q.Source == dbq.QuestionsSourceAiGenerated {
			write(&p.ai, &p.nAI, &lastAI, q, "AI 出题")
		} else {
			write(&p.questions, &p.nQuestions, &lastQ, q, "")
		}
	}

	kps, err := s.q.ExportKnowledgePoints(ctx, dbq.ExportKnowledgePointsParams{BankID: sub.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return p, err
	}
	p.kps.add(Heading, "知识点卡片")
	children := map[uint64][]dbq.ExportKnowledgePointsRow{}
	var roots []dbq.ExportKnowledgePointsRow
	ids := map[uint64]bool{}
	for _, k := range kps {
		ids[k.ID] = true
	}
	for _, k := range kps {
		if k.ParentID.Valid && ids[uint64(k.ParentID.Int64)] {
			children[uint64(k.ParentID.Int64)] = append(children[uint64(k.ParentID.Int64)], k)
		} else {
			roots = append(roots, k)
		}
	}
	var walk func(k dbq.ExportKnowledgePointsRow)
	walk = func(k dbq.ExportKnowledgePointsRow) {
		switch k.Level {
		case dbq.KnowledgePointsLevelSection:
			p.kps.add(Sub, k.Name)
		case dbq.KnowledgePointsLevelChapter:
			p.kps.add(Para, "【"+k.Name+"】")
		default:
			p.nKPs++
			p.kps.add(Para, "· "+k.Name)
			if k.OriginalText.Valid {
				p.kps.add(Note, k.OriginalText.String)
			}
		}
		for _, c := range children[k.ID] {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}

	wrong, err := s.q.ExportWrongBook(ctx, dbq.ExportWrongBookParams{OwnerUserID: userID, BankID: sub.BankID})
	if err != nil {
		return p, err
	}
	p.wrong.add(Heading, "错题和我的作答")
	for i, w := range wrong {
		p.nWrong++
		p.wrong.add(Para, strconv.Itoa(i+1)+". 【"+qtypeNames[string(w.Qtype)]+"】"+w.Stem)
		meta := "错 " + strconv.Itoa(int(w.WrongCount)) + " 次"
		if w.LastLossType.Valid {
			meta = "失分原因：" + lossNames[string(w.LastLossType.WrongBookLastLossType)] + " · " + meta
		}
		p.wrong.add(Note, meta)
		if w.MyAnswer.Valid && strings.TrimSpace(w.MyAnswer.String) != "" {
			p.wrong.add(Para, "我的作答："+w.MyAnswer.String)
		}
		if w.Answer.Valid && strings.TrimSpace(w.Answer.String) != "" {
			p.wrong.add(Para, "参考答案："+w.Answer.String)
		}
	}
	return p, nil
}

func (p parts) document(title string, o Options, now time.Time) Document {
	d := Document{Title: title, Subtitle: "导出于 " + now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02") + " · 只包含你自己的资料和作答记录"}
	if o.Questions && p.nQuestions > 0 {
		d.Blocks = append(d.Blocks, p.questions.Blocks...)
	}
	if o.KPs && p.nKPs > 0 {
		d.Blocks = append(d.Blocks, p.kps.Blocks...)
	}
	if o.Wrong && p.nWrong > 0 {
		d.Blocks = append(d.Blocks, p.wrong.Blocks...)
	}
	if o.AIVariants && p.nAI > 0 {
		d.Blocks = append(d.Blocks, p.ai.Blocks...)
	}
	return d
}

// Item 是可勾选的一项：条数与预计页数。
type Item struct {
	Count int
	Pages int
}

// Preview 是 6.4 的可选内容与预计页数。
type Preview struct {
	SubjectID                         uint64
	Questions, KPs, Wrong, AIVariants Item
}

func (s *Service) Preview(ctx context.Context, userID, subjectID uint64) (Preview, error) {
	sub, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Preview{}, err
	}
	p, err := s.build(ctx, userID, sub)
	if err != nil {
		return Preview{}, err
	}
	pages := func(d Document, n int) Item {
		if n == 0 {
			return Item{}
		}
		return Item{Count: n, Pages: d.Pages()}
	}
	return Preview{SubjectID: subjectID, Questions: pages(p.questions, p.nQuestions), KPs: pages(p.kps, p.nKPs), Wrong: pages(p.wrong, p.nWrong),
		AIVariants: pages(p.ai, p.nAI)}, nil
}

// Job 是一次导出。
type Job struct {
	ID          uint64
	SubjectID   uint64
	Format      string
	Options     Options
	Status      string // queued / running / done / failed / expired
	Pages       int
	DownloadURL string
	FileName    string
	ExpiresAt   *time.Time
	CreatedAt   time.Time
}

// Create 开始导出：至少选一项、选中的内容不能全为空；后台生成，完成后在 Get 里给下载链接。
func (s *Service) Create(ctx context.Context, userID, subjectID uint64, o Options, format string) (Job, error) {
	if format != "pdf" && format != "docx" {
		return Job{}, apperr.New(apperr.BadRequest, "格式只能是 PDF 或 Word")
	}
	if !o.Questions && !o.KPs && !o.Wrong && !o.AIVariants {
		return Job{}, apperr.New(apperr.BadRequest, "至少选一项导出内容")
	}
	sub, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Job{}, err
	}
	p, err := s.build(ctx, userID, sub)
	if err != nil {
		return Job{}, err
	}
	d := p.document(sub.Name, o, s.now())
	if len(d.Blocks) == 0 {
		return Job{}, apperr.New(apperr.BadRequest, "选中的内容还是空的，先导入资料或练几道题")
	}
	raw, _ := json.Marshal(o)
	id, err := s.q.InsertExportJob(ctx, dbq.InsertExportJobParams{OwnerUserID: userID, SubjectID: subjectID, Options: raw, Format: dbq.ExportJobsFormat(format),
		PageEstimate: sql.NullInt32{Int32: int32(d.Pages()), Valid: true}, CreatedAt: s.now().UTC()})
	if err != nil {
		return Job{}, err
	}
	if s.queue == nil {
		if err := s.Generate(ctx, userID, uint64(id), true); err != nil {
			return Job{}, err
		}
	} else {
		t, err := jobs.NewExportTask(userID, uint64(id))
		if err != nil {
			return Job{}, err
		}
		if _, err := s.queue.EnqueueContext(ctx, t); err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
			return Job{}, fmt.Errorf("排导出任务：%w", err)
		}
	}
	return s.Get(ctx, userID, uint64(id))
}

func ext(format string) string {
	if format == "docx" {
		return "docx"
	}
	return "pdf"
}

func (s *Service) fileName(ctx context.Context, userID uint64, j dbq.ExportJob) string {
	name := "题库"
	if sub, err := s.subject(ctx, userID, j.SubjectID); err == nil {
		name = sub.Name + "题库"
	}
	return name + "_" + j.CreatedAt.In(time.FixedZone("CST", 8*3600)).Format("20060102") + "." + ext(string(j.Format))
}

// Get 返回导出进度；完成且文件还在时给 1 小时有效的下载链接。别人的导出返回 404。
func (s *Service) Get(ctx context.Context, userID, id uint64) (Job, error) {
	j, err := s.q.GetExportJob(ctx, dbq.GetExportJobParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Job{}, err
	}
	out := Job{ID: j.ID, SubjectID: j.SubjectID, Format: string(j.Format), Status: string(j.Status), Pages: int(j.PageEstimate.Int32), CreatedAt: j.CreatedAt,
		FileName: s.fileName(ctx, userID, j)}
	_ = json.Unmarshal(j.Options, &out.Options)
	if j.ExpiresAt.Valid {
		t := j.ExpiresAt.Time
		out.ExpiresAt = &t
	}
	if j.Status == dbq.ExportJobsStatusDone {
		if !j.ObjectKey.Valid || (j.ExpiresAt.Valid && !s.now().Before(j.ExpiresAt.Time)) {
			out.Status = "expired"
			return out, nil
		}
		link, err := s.oss.PresignGet(ctx, j.ObjectKey.String, out.FileName, linkTTL)
		if err != nil {
			return Job{}, err
		}
		out.DownloadURL = link.URL
	}
	return out, nil
}

func objectKey(userID, jobID uint64, format string) string {
	return "u/" + strconv.FormatUint(userID, 10) + "/exports/" + strconv.FormatUint(jobID, 10) + "." + ext(format)
}

// Generate 是后台生成：排版、上传 OSS、记下 24 小时后删除。可重复执行；最后一次重试仍失败时标为失败。
func (s *Service) Generate(ctx context.Context, userID, id uint64, last bool) error {
	j, err := s.q.GetExportJob(ctx, dbq.GetExportJobParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if j.Status == dbq.ExportJobsStatusDone || j.Status == dbq.ExportJobsStatusFailed {
		return nil
	}
	if err := s.q.SetExportJobRunning(ctx, dbq.SetExportJobRunningParams{ID: id, OwnerUserID: userID}); err != nil {
		return err
	}
	err = s.generate(ctx, userID, j)
	if err != nil && last {
		logx.From(ctx).Error("export failed", "err", err, "job_id", id)
		return s.q.FailExportJob(ctx, dbq.FailExportJobParams{FinishedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id, OwnerUserID: userID})
	}
	return err
}

func (s *Service) generate(ctx context.Context, userID uint64, j dbq.ExportJob) error {
	sub, err := s.subject(ctx, userID, j.SubjectID)
	if err != nil {
		return err
	}
	var o Options
	_ = json.Unmarshal(j.Options, &o)
	p, err := s.build(ctx, userID, sub)
	if err != nil {
		return err
	}
	d := p.document(sub.Name, o, j.CreatedAt)
	var data []byte
	ctype := "application/pdf"
	if j.Format == dbq.ExportJobsFormatDocx {
		data, err = RenderDOCX(d)
		ctype = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	} else {
		data, err = RenderPDF(d, s.fonts)
	}
	if err != nil {
		return err
	}
	key := objectKey(userID, j.ID, string(j.Format))
	if err := s.oss.Put(ctx, key, data, ctype); err != nil {
		return err
	}
	now := s.now().UTC()
	return s.q.FinishExportJob(ctx, dbq.FinishExportJobParams{ObjectKey: sql.NullString{String: key, Valid: true}, PageEstimate: sql.NullInt32{Int32: int32(d.Pages()), Valid: true},
		ExpiresAt: sql.NullTime{Time: now.Add(keepFor), Valid: true}, FinishedAt: sql.NullTime{Time: now, Valid: true}, ID: j.ID, OwnerUserID: userID})
}

// Cleanup 删除 24 小时前生成的导出文件（定时任务，每小时一次）。
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	rows, err := s.q.ListExpiredExports(ctx, sql.NullTime{Time: s.now().UTC(), Valid: true})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if err := s.oss.Delete(ctx, r.ObjectKey.String); err != nil {
			return n, err
		}
		if err := s.q.ClearExportObject(ctx, dbq.ClearExportObjectParams{ID: r.ID, OwnerUserID: r.OwnerUserID}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
