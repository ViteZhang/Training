package official

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"golang.org/x/sync/errgroup"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/store"
)

// 后台 AI 解析导入（PRD v3 增补 7.17，T47）：团队上传真题卷、参考书目录、有授权的讲义，AI 解析出题目与知识点，
// 编辑逐条「接受、修改、驳回」，接受的生成 7.12 草稿并提交 7.13 审核。
//   - 只处理团队上传的资料：文件放在 OSS 的 official-imports/ 前缀下，解析结果存 official_import_*，与用户端资料完全分开
//   - 解析能力复用用户端：取文本（Word / PDF / 拍照）、规则切题、结构化、采分点提取、参考答案生成（优先引用官方参考素材）
//   - 采分点合计不等于分值时标红，不能接受；解析效果不理想时可以用表格导入（增补「导入数据结构」）
//   - 缺答案、采分点的题：解析后由编辑点「生成答案 / 提取采分点」，或整份「一键补全」（T49）；上传时勾选「缺答案时由 AI 生成」才在解析时自动补

// ImportQueue 排 7.17 解析任务。
type ImportQueue interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// ImportDeps 是 7.17 需要的云服务。
type ImportDeps struct {
	OSS   oss.Store
	OCR   ocr.Recognizer
	PDF   ocr.PDFParser
	Queue ImportQueue
	// NewTask 创建解析任务（jobs.NewOfficialImportTask）；放在这里避免 official 依赖 jobs。
	NewTask func(fileID uint64) (*asynq.Task, error)
	// NewFillTask 创建一键补全任务（jobs.NewOfficialImportFillTask）。
	NewFillTask func(fileID uint64) (*asynq.Task, error)
}

// ConfigureImports 接上 7.17 的云服务；没配置时只能粘贴文字与表格导入，且同步解析。
func (s *Service) ConfigureImports(d ImportDeps) { s.imports = d }

const (
	maxImportBytes = 50 << 20
	// importAIParallel 是一份资料同时调用模型的请求数，与用户端导入（importer.aiParallel）一致，不至于触发平台限流。
	importAIParallel = 3
	// maxFillRateLimits 是补答案时累计限流的容忍次数；超过就停止整份资料，避免 429 风暴。
	maxFillRateLimits = 3
	maxImportRunes    = 400000
	importPrefix      = "official-imports/"
	// tagBatch 是归知识点时每批交给模型的题数（与用户端导入一致）。
	tagBatch = 30
	// structureRetryDelay 是结构化遇到平台错误后再试一次前的等待。
	structureRetryDelay = 20 * time.Second
)

// ImportFile 是一份团队上传的资料。
type ImportFile struct {
	ID                                         uint64
	Kind, FileName, Format, Status, FailReason string
	QuestionCount, KPCount, ReviewedCount      int
	ExamYear                                   int
	CreatedAt                                  time.Time
	UploadURL                                  string
	UploadHeaders                              map[string]string
	// AutoAnswer 是上传时勾选了「缺答案时由 AI 生成」。
	AutoAnswer bool
	// Fill 是一键补全的进度（T49）。
	Fill ImportFill
}

// ImportFill 是一键补全答案与采分点的进度：running 时 Done / Total 题已处理，其中 Failed 题没补上。
type ImportFill struct {
	Status              string
	Total, Done, Failed int
}

// ImportFileInput 是新增一份资料：文件（先拿直传地址再确认上传）或直接粘贴文字 / 表格。
type ImportFileInput struct {
	Kind, FileName, Format string
	Size                   int64
	Text                   string
	ExamYear               int
	Recollection           bool
	// AutoAnswer 解析时缺答案、采分点的题由 AI 补全；不勾时解析后由编辑逐题或一键补全（T49）。
	AutoAnswer bool
}

var importContentTypes = map[string]string{"pdf": "application/pdf", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"image": "image/jpeg", "csv": "text/csv", "text": "text/plain", "md": "text/markdown"}

// CreateImportFile 新增一份资料。粘贴的文字与表格直接开始解析；文件返回直传地址，上传后调用 ConfirmImportFile。
func (s *Service) CreateImportFile(ctx context.Context, adminID, projectID uint64, in ImportFileInput) (ImportFile, error) {
	if _, err := s.projectExists(ctx, projectID); err != nil {
		return ImportFile{}, err
	}
	kind := dbq.OfficialImportFilesKind(in.Kind)
	format := dbq.OfficialImportFilesFormat(in.Format)
	if !kind.Valid() || !format.Valid() {
		return ImportFile{}, apperr.New(apperr.BadRequest, "资料类型或格式不正确")
	}
	if kind == dbq.OfficialImportFilesKindTable && format != dbq.OfficialImportFilesFormatCsv {
		return ImportFile{}, apperr.New(apperr.BadRequest, "表格导入请用 CSV（Excel 另存为 CSV UTF-8）")
	}
	name := strings.TrimSpace(in.FileName)
	if name == "" {
		name = "粘贴的内容"
	}
	p := dbq.InsertOfficialImportFileParams{ProjectID: projectID, Kind: kind, FileName: cutRunes(name, 255), Format: format,
		ExamYear: sql.NullInt16{Int16: int16(in.ExamYear), Valid: in.ExamYear > 0}, IsRecollection: in.Recollection, AutoAnswer: in.AutoAnswer,
		CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID > 0}}
	text := strings.TrimSpace(in.Text)
	// Markdown 既可以上传 .md 文件，也可以直接粘贴（带 text）。
	inline := format == dbq.OfficialImportFilesFormatText || format == dbq.OfficialImportFilesFormatCsv ||
		(format == dbq.OfficialImportFilesFormatMd && in.Size == 0 && text != "")
	if inline {
		if text == "" {
			return ImportFile{}, apperr.New(apperr.BadRequest, "内容不能为空")
		}
		if len([]rune(text)) > maxImportRunes {
			return ImportFile{}, apperr.New(apperr.BadRequest, "内容太长，请分几次导入")
		}
		p.Content, p.Status, p.SizeBytes = sql.NullString{String: text, Valid: true}, dbq.OfficialImportFilesStatusParsing, uint64(len(text))
	} else {
		if s.imports.OSS == nil {
			return ImportFile{}, apperr.New(apperr.BadRequest, "文件上传还没配置，请粘贴文字或用表格导入")
		}
		if in.Size <= 0 || in.Size > maxImportBytes {
			return ImportFile{}, apperr.New(apperr.BadRequest, "文件不能超过 50MB")
		}
		p.Status, p.SizeBytes = dbq.OfficialImportFilesStatusUploading, uint64(in.Size)
	}
	id, err := s.q.InsertOfficialImportFile(ctx, p)
	if err != nil {
		return ImportFile{}, err
	}
	out := ImportFile{ID: uint64(id), Kind: string(kind), FileName: p.FileName, Format: string(format), Status: string(p.Status), ExamYear: in.ExamYear,
		AutoAnswer: in.AutoAnswer, Fill: ImportFill{Status: "idle"}}
	if inline {
		return out, s.enqueueImport(ctx, uint64(id))
	}
	key := importKey(projectID, uint64(id))
	if err := s.setImportKey(ctx, uint64(id), key); err != nil {
		return ImportFile{}, err
	}
	signed, err := s.imports.OSS.PresignPut(ctx, key, importContentTypes[string(format)], in.Size, 30*time.Minute)
	if err != nil {
		return ImportFile{}, err
	}
	out.UploadURL, out.UploadHeaders = signed.URL, signed.Headers
	return out, nil
}

func importKey(projectID, fileID uint64) string {
	return fmt.Sprintf("%s%d/%d", importPrefix, projectID, fileID)
}

func (s *Service) setImportKey(ctx context.Context, id uint64, key string) error {
	_, err := s.d.DB.ExecContext(ctx, "UPDATE official_import_files SET object_key = ? WHERE id = ?", key, id)
	return err
}

func (s *Service) importFile(ctx context.Context, projectID, fileID uint64) (dbq.OfficialImportFile, error) {
	f, err := s.q.GetOfficialImportFile(ctx, fileID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && projectID != 0 && f.ProjectID != projectID) {
		return f, apperr.NotFoundErr()
	}
	return f, err
}

// DeleteImportFile 删除一份导入的资料，连同解析出的题、核对时生成的草稿与审核任务（重新导入前清理）。
// 有题已经发布到题库时不删：发布的内容要走下线流程。正在解析、补全的后台任务发现资料不在了会自行结束。
func (s *Service) DeleteImportFile(ctx context.Context, projectID, fileID uint64) error {
	var key string
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		f, err := q.LockOfficialImportFile(ctx, fileID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && f.ProjectID != projectID) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		n, err := q.CountPublishedOfficialImportDrafts(ctx, fileID)
		if err != nil {
			return err
		}
		if n > 0 {
			return apperr.New(apperr.Conflict, fmt.Sprintf("这份资料里有 %d 道题已经发布到题库，不能删除", n))
		}
		if err := q.DeleteOfficialImportReviewTasks(ctx, fileID); err != nil {
			return err
		}
		if err := q.DeleteOfficialImportDrafts(ctx, fileID); err != nil {
			return err
		}
		key = f.ObjectKey.String
		return q.DeleteOfficialImportFile(ctx, fileID)
	})
	if err != nil {
		return err
	}
	// 原文件删不掉不影响结果，只是 OSS 里多留一个文件。
	if key != "" && s.imports.OSS != nil {
		if err := s.imports.OSS.Delete(ctx, key); err != nil {
			slog.WarnContext(ctx, "删除导入原文件失败", "file_id", fileID, "err", err)
		}
	}
	return nil
}

// ConfirmImportFile 确认文件已上传，开始解析。
func (s *Service) ConfirmImportFile(ctx context.Context, projectID, fileID uint64) error {
	f, err := s.importFile(ctx, projectID, fileID)
	if err != nil {
		return err
	}
	if f.Status != dbq.OfficialImportFilesStatusUploading && f.Status != dbq.OfficialImportFilesStatusFailed {
		return apperr.New(apperr.Conflict, "这份资料已经在解析了")
	}
	if f.Status == dbq.OfficialImportFilesStatusUploading && s.imports.OSS != nil {
		if _, err := s.imports.OSS.Head(ctx, f.ObjectKey.String); err != nil {
			return apperr.New(apperr.BadRequest, "文件还没上传完成")
		}
	}
	if err := s.q.SetOfficialImportStatus(ctx, dbq.SetOfficialImportStatusParams{Status: dbq.OfficialImportFilesStatusParsing, ID: fileID}); err != nil {
		return err
	}
	return s.enqueueImport(ctx, fileID)
}

// enqueueImport 排解析任务；没有队列时同步解析（本地与测试）。
func (s *Service) enqueueImport(ctx context.Context, fileID uint64) error {
	if s.imports.Queue == nil || s.imports.NewTask == nil {
		return s.ProcessImport(ctx, fileID)
	}
	t, err := s.imports.NewTask(fileID)
	if err != nil {
		return err
	}
	_, err = s.imports.Queue.EnqueueContext(ctx, t)
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		// 同一份资料已有解析任务在排队/运行：不重复入队，避免并发重跑同一份资料。
		return nil
	}
	return err
}

// ImportFiles 列出项目下的资料（文件卡：解析状态与结果数）。
func (s *Service) ImportFiles(ctx context.Context, projectID uint64) ([]ImportFile, error) {
	if _, err := s.projectExists(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListOfficialImportFiles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ImportFile, len(rows))
	for i, r := range rows {
		out[i] = ImportFile{ID: r.ID, Kind: string(r.Kind), FileName: r.FileName, Format: string(r.Format), Status: string(r.Status), FailReason: r.FailReason.String,
			QuestionCount: int(r.QuestionCount), KPCount: int(r.KpCount), ReviewedCount: int(r.ReviewedCount), ExamYear: int(r.ExamYear.Int16), CreatedAt: r.CreatedAt,
			AutoAnswer: r.AutoAnswer, Fill: ImportFill{Status: string(r.FillStatus), Total: int(r.FillTotal), Done: int(r.FillDone), Failed: int(r.FillFailed)}}
	}
	return out, nil
}

// --- 解析 ---

// ProcessImport 解析一份资料（Worker 调用，可重复执行：重新解析时只替换还没核对的条目）。
func (s *Service) ProcessImport(ctx context.Context, fileID uint64) error {
	f, err := s.q.GetOfficialImportFile(ctx, fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.Status != dbq.OfficialImportFilesStatusParsing {
		return nil
	}
	items, text, err := s.parseImport(ctx, f)
	if err != nil {
		reason := "解析失败，请稍后重试或改用表格导入"
		var ue *extract.UserError
		var ae *apperr.Error
		switch {
		case errors.Is(err, cloudai.ErrInsufficientBalance):
			reason = "AI 服务余额不足，本次解析已停止，充值后重新解析"
		case errors.Is(err, cloudai.ErrRateLimited):
			reason = "AI 服务限流，本次解析已停止，请稍后重试"
		case errors.As(err, &ue):
			reason = ue.Msg
		case errors.As(err, &ae) && ae.Kind == apperr.BadRequest:
			reason = ae.Message
		}
		s.d.Log.WarnContext(context.WithoutCancel(ctx), "official import failed", "file_id", fileID, "err", err)
		return s.failImport(ctx, fileID, reason)
	}
	if err := s.q.DeleteOfficialImportItems(ctx, fileID); err != nil {
		return s.failImport(ctx, fileID, "解析结果保存失败，请稍后重试")
	}
	qs, ks := 0, 0
	for i, it := range items {
		if it.ItemType == dbq.OfficialImportItemsItemTypeQuestion {
			qs++
		} else {
			ks++
		}
		it.FileID, it.ProjectID, it.Seq = fileID, f.ProjectID, uint32(i+1)
		if err := s.q.InsertOfficialImportItem(ctx, it); err != nil {
			return s.failImport(ctx, fileID, "解析结果保存失败，请稍后重试")
		}
	}
	if err := s.q.SetOfficialImportParsed(ctx, dbq.SetOfficialImportParsedParams{Content: sql.NullString{String: cutRunes(text, maxImportRunes), Valid: true},
		QuestionCount: uint32(qs), KpCount: uint32(ks), ID: fileID}); err != nil {
		return s.failImport(ctx, fileID, "解析结果保存失败，请稍后重试")
	}
	return nil
}

// failImport 写失败状态。任务超时或取消后 ctx 已经失效，必须用 WithoutCancel 落库；
// 否则状态会停在 parsing，恢复任务每 15 分钟重排一次同一份资料，反复调用模型（2026-11-04 重复烧 token 的原因之一）。
func (s *Service) failImport(ctx context.Context, fileID uint64, reason string) error {
	return s.q.SetOfficialImportStatus(context.WithoutCancel(ctx), dbq.SetOfficialImportStatusParams{
		Status: dbq.OfficialImportFilesStatusFailed, FailReason: nullStr(cutRunes(reason, 255)), ID: fileID})
}

// importFailReason 是资料卡上显示的失败原因（只给后台编辑看）：分清是模型服务、超时还是资料本身的问题，
// 不然一律「解析失败」，没法判断该重试、拆分资料还是改用表格。
func importFailReason(err error) string {
	var ue *extract.UserError
	var ae *apperr.Error
	switch {
	case errors.As(err, &ue):
		return ue.Msg
	case errors.As(err, &ae) && ae.Kind == apperr.BadRequest:
		return ae.Message
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return "解析超时，请重新解析；资料很长时可按年份拆成几份上传"
	case errors.Is(err, cloudai.ErrRateLimited) || errors.Is(err, cloudai.ErrIdleTimeout):
		return "模型服务暂时不可用（限流或响应超时），请稍后重新解析"
	case errors.As(err, &ae) && ae.Kind == apperr.AIFailed:
		return "模型输出不合格，请稍后重新解析或改用表格导入"
	}
	return "解析失败，请稍后重试或改用表格导入"
}

// readPages 取出资料的文字，按页。
func (s *Service) readPages(ctx context.Context, f dbq.OfficialImportFile) ([]importer.Page, error) {
	switch {
	case f.Format == dbq.OfficialImportFilesFormatCsv:
		return []importer.Page{{No: 1, Text: f.Content.String}}, nil
	case f.Format == dbq.OfficialImportFilesFormatText || (f.Format == dbq.OfficialImportFilesFormatMd && !f.ObjectKey.Valid):
		// 粘贴的文字常是从 Markdown 编辑器复制的：一并去掉排版标记（T52）。
		return []importer.Page{{No: 1, Text: markdownText(f.Content.String)}}, nil
	}
	if s.imports.OSS == nil {
		return nil, apperr.New(apperr.BadRequest, "文件上传还没配置")
	}
	rc, err := s.imports.OSS.Get(ctx, f.ObjectKey.String)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(rc, maxImportBytes+1))
	_ = rc.Close()
	if err != nil {
		return nil, err
	}
	var pages []importer.Page
	switch f.Format {
	case dbq.OfficialImportFilesFormatDocx:
		r, err := extract.Docx(data, 1500)
		if err != nil {
			return nil, err
		}
		for _, p := range r.Pages {
			pages = append(pages, importer.Page{No: p.No, Text: p.Text})
		}
	case dbq.OfficialImportFilesFormatPdf:
		if s.imports.PDF == nil {
			return nil, apperr.New(apperr.BadRequest, "PDF 解析还没配置")
		}
		ps, err := s.imports.PDF.ParsePDF(ctx, data, ocr.PDFOptions{OCRScanned: true})
		if err != nil {
			return nil, apperr.New(apperr.BadRequest, "PDF 读不出来，请确认没有加密，或改用表格导入").Wrap(err)
		}
		for i, p := range ps {
			pages = append(pages, importer.Page{No: i + 1, Text: p.Text})
		}
	case dbq.OfficialImportFilesFormatMd:
		if !utf8.Valid(data) {
			return nil, apperr.New(apperr.BadRequest, "Markdown 文件请用 UTF-8 编码保存")
		}
		pages = []importer.Page{{No: 1, Text: markdownText(string(data))}}
	case dbq.OfficialImportFilesFormatImage:
		if s.imports.OCR == nil {
			return nil, apperr.New(apperr.BadRequest, "拍照识别还没配置")
		}
		r, err := s.imports.OCR.Recognize(ctx, ocr.Image{ObjectKey: f.ObjectKey.String, Data: data})
		if err != nil {
			return nil, apperr.New(apperr.BadRequest, "照片识别不出文字，请换一张清晰的").Wrap(err)
		}
		pages = []importer.Page{{No: 1, Text: r.Text}}
	}
	return pages, nil
}

func joinPages(pages []importer.Page) string {
	parts := make([]string, len(pages))
	for i, p := range pages {
		parts[i] = p.Text
	}
	return strings.Join(parts, "\n\n")
}

func (s *Service) parseImport(ctx context.Context, f dbq.OfficialImportFile) ([]dbq.InsertOfficialImportItemParams, string, error) {
	pages, err := s.readPages(ctx, f)
	if err != nil {
		return nil, "", err
	}
	text := joinPages(pages)
	if strings.TrimSpace(text) == "" {
		return nil, "", apperr.New(apperr.BadRequest, "没有读出文字")
	}
	var items []dbq.InsertOfficialImportItemParams
	switch f.Kind {
	case dbq.OfficialImportFilesKindCatalog:
		items, err = s.parseCatalog(ctx, f.ProjectID, text)
	case dbq.OfficialImportFilesKindTable:
		items, err = s.parseTable(ctx, f.ProjectID, text, f.AutoAnswer)
	default:
		items, err = s.parseQuestions(ctx, f, pages)
	}
	return items, text, err
}

// parseCatalog 参考书目录：AI 按目录生成知识点框架（复用 7.12 的知识点候选，与现有知识点树对齐）。
func (s *Service) parseCatalog(ctx context.Context, projectID uint64, text string) ([]dbq.InsertOfficialImportItemParams, error) {
	cands, err := s.KPCandidates(ctx, projectID, text)
	if err != nil {
		return nil, err
	}
	out := make([]dbq.InsertOfficialImportItemParams, 0, len(cands))
	for _, c := range cands {
		conf := dbq.OfficialImportItemsConfidenceHigh
		if c.Alignment != "new" {
			conf = dbq.OfficialImportItemsConfidenceMedium // 与已有知识点同名或表述不同：要人看一眼
		}
		if len(c.Rubric) == 0 {
			conf = dbq.OfficialImportItemsConfidenceLow
		}
		b, _ := json.Marshal(c.KPPayload)
		out = append(out, dbq.InsertOfficialImportItemParams{ItemType: dbq.OfficialImportItemsItemTypeKnowledgePoint, Payload: b,
			SourceText: nullStr(cutRunes(c.OriginalText, 2000)), Confidence: conf})
	}
	return out, nil
}

// confidenceOf 把模型给的 0–1 置信度分成高 / 中 / 低；AI 补了答案、采分点合计不等于分值的最多算中。
func confidenceOf(v float64, aiFilled bool, p QuestionPayload) dbq.OfficialImportItemsConfidence {
	c := dbq.OfficialImportItemsConfidenceLow
	switch {
	case v >= 0.85:
		c = dbq.OfficialImportItemsConfidenceHigh
	case v >= 0.6:
		c = dbq.OfficialImportItemsConfidenceMedium
	}
	if c == dbq.OfficialImportItemsConfidenceHigh && (aiFilled || !RubricMatches(p)) {
		c = dbq.OfficialImportItemsConfidenceMedium
	}
	return c
}

// RubricMatches 判断主观题的采分点合计是否等于题目分值（增补「导入数据结构」）；客观题或没有分值时不检查。
func RubricMatches(p QuestionPayload) bool {
	if !ai.Subjective(p.QType) || p.Score == nil {
		return true
	}
	sum := 0.0
	for _, r := range p.Rubric {
		sum += r.Score
	}
	return math.Abs(sum-*p.Score) < 0.01
}

// needsFill 判断一道题要不要补：主观题缺答案或缺采分点，客观题缺答案。
func needsFill(p QuestionPayload) bool {
	if p.Regenerate || strings.TrimSpace(p.Answer) == "" {
		return true
	}
	return ai.Subjective(p.QType) && len(p.Rubric) == 0
}

// fillFailureFatal 判断补答案错误是否应停止整份资料：余额不足立即停；累计限流达到阈值也停，
// 避免余额为 0 或平台限流时继续为剩余题目逐条发请求。
func fillFailureFatal(err error, rateLimits int) bool {
	if errors.Is(err, cloudai.ErrInsufficientBalance) {
		return true
	}
	return errors.Is(err, cloudai.ErrRateLimited) && rateLimits >= maxFillRateLimits
}

// fillAnswer 补参考答案与采分点（T49）：
//   - 主观题有答案、缺采分点：从答案提采分点，答案仍算原文的
//   - 缺答案：AI 生成参考答案（主观题连同采分点；已经填了采分点的保留），优先引用项目关联的官方教材，标「AI 生成」（answer_ai）
//   - 主观题没有分值：参考同项目同题型的给分（defaults），标「分值待核对」；同题型一道有分值的都没有时提示先填分值
//
// 返回引用与是否由 AI 补了内容；不需要补的原样返回。
func (s *Service) fillAnswer(ctx context.Context, projectID uint64, p *QuestionPayload, defaults map[string]float64) ([]Citation, bool, error) {
	if !needsFill(*p) {
		return nil, false, nil
	}
	subjective := ai.Subjective(p.QType)
	if p.Regenerate {
		// 按关联教材重新生成（T53）：去掉 AI 给的答案与采分点，原文给的采分点留着。
		p.Answer, p.AnswerSnapshotID, p.AnswerBasis = "", 0, nil
		if !p.RubricKept {
			p.Rubric = nil
		}
	}
	score := 0.0
	if subjective {
		if p.Score == nil || *p.Score <= 0 {
			v, ok := defaults[p.QType]
			if !ok {
				return nil, false, apperr.New(apperr.BadRequest, "这道题没有分值，同项目里也没有同题型的题可以参考，请先在「修改」里填分值").With("reason", "score_missing")
			}
			p.Score, p.ScoreInferred = &v, true
		}
		score = *p.Score
	}
	if strings.TrimSpace(p.Answer) != "" {
		pts, err := s.RubricCandidates(ctx, p.QType, p.Stem, p.Answer, score)
		if err != nil {
			return nil, false, err
		}
		p.Rubric = pts
		return nil, true, nil
	}
	c, err := s.answerCandidates(ctx, projectID, p.QType, p.Stem, p.Options, score, p.Rubric)
	if err != nil {
		return nil, false, err
	}
	p.RubricKept = subjective && len(p.Rubric) > 0
	p.Answer, p.AnswerSnapshotID, p.AnswerAI, p.AnswerBasis, p.Regenerate = c.Answer, c.SnapshotID, true, c.Basis, false
	if subjective {
		p.Rubric = c.Points // 已有采分点的保留原样（answerCandidates 原样返回）
	}
	return c.Citations, true, nil
}

// defaultScores 是同项目里每个题型最常见的分值（各资料解析出的题、草稿、已发布的官方题），给没有分值的题参考。
// 一样多时取较小的，免得把名词解释按论述题给分。
func (s *Service) defaultScores(ctx context.Context, projectID uint64) (map[string]float64, error) {
	counts := map[string]map[float64]int{}
	add := func(qtype string, v float64) {
		if v <= 0 || !ai.Subjective(qtype) {
			return
		}
		if counts[qtype] == nil {
			counts[qtype] = map[float64]int{}
		}
		counts[qtype][v]++
	}
	addPayloads := func(rows []json.RawMessage) {
		for _, raw := range rows {
			var p QuestionPayload
			if json.Unmarshal(raw, &p) == nil && p.Score != nil && !p.ScoreInferred {
				add(p.QType, *p.Score)
			}
		}
	}
	items, err := s.q.ListProjectImportQuestionPayloads(ctx, projectID)
	if err != nil {
		return nil, err
	}
	addPayloads(items)
	drafts, err := s.q.ListProjectDraftQuestionPayloads(ctx, projectID)
	if err != nil {
		return nil, err
	}
	addPayloads(drafts)
	proj, err := s.projectExists(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if proj.BankID.Valid {
		qs, err := s.q.ListOfficialQuestions(ctx, uint64(proj.BankID.Int64))
		if err != nil {
			return nil, err
		}
		for _, q := range qs {
			if q.Status != dbq.QuestionsStatusActive || !q.Score.Valid {
				continue
			}
			if v, err := strconv.ParseFloat(q.Score.String, 64); err == nil {
				add(string(q.Qtype), v)
			}
		}
	}
	out := map[string]float64{}
	for qt, m := range counts {
		best, bestN := 0.0, 0
		for v, n := range m {
			if n > bestN || (n == bestN && v < best) {
				best, bestN = v, n
			}
		}
		out[qt] = best
	}
	return out, nil
}

// parseQuestions 真题卷与讲义：规则切题 → AI 结构化 → 补答案与采分点 → 归知识点。
func (s *Service) parseQuestions(ctx context.Context, f dbq.OfficialImportFile, pages []importer.Page) ([]dbq.InsertOfficialImportItemParams, error) {
	proj, err := s.projectExists(ctx, f.ProjectID)
	if err != nil {
		return nil, err
	}
	blocks := importer.Split(pages)
	byID := map[int]ai.Block{}
	for _, b := range blocks {
		byID[b.ID] = b
	}
	type parsed struct {
		p    QuestionPayload
		conf float64
		src  string
		page int
		cite []Citation
		ai   bool
		// fillErr 是解析时自动补答案失败的原因（勾了「缺答案时由 AI 生成」时）。
		fillErr string
	}
	var qs []parsed
	// 多年真题合集有几十批、几百道题，各批同时调用模型，才能在任务时限（1 小时）内解析完。
	chunks := importer.Chunk(blocks, 6000, 40)
	outs := make([][]ai.QItem, len(chunks))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(importAIParallel)
	for i, chunk := range chunks {
		g.Go(func() error {
			// 与用户导入相同：一批输出不合格时拆半重试，单块还不合格就按规则照录原文（低置信度，编辑核对），
			// 不让一批坏掉就整份解析失败（模型输出每次不同，同一份资料时好时坏）。
			items, err := importer.StructureEach(gctx, chunk, func(ctx context.Context, bs []ai.Block) ([]ai.QItem, error) {
				byID := make(map[int]ai.Block, len(bs))
				for _, b := range bs {
					byID[b.ID] = b
				}
				in := ai.StructureIn{Subject: proj.SubjectName, Blocks: bs}
				out, _, err := ai.Structure.Run(ctx, s.d.AI, 0, in)
				if err != nil && !apperr.IsKind(err, apperr.AIFailed) && ctx.Err() == nil {
					// 平台偶发限流、超时（三批并行时实测出现过）：等一会儿再试一次，不让一次抖动整份失败。
					s.d.Log.WarnContext(ctx, "official import structure retry", "file_id", f.ID, "err", err)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(structureRetryDelay):
					}
					out, _, err = ai.Structure.Run(ctx, s.d.AI, 0, in)
				}
				if err != nil {
					if len(bs) == 1 && apperr.IsKind(err, apperr.AIFailed) {
						s.d.Log.WarnContext(ctx, "official import block fallback", "file_id", f.ID, "block", bs[0].ID, "err", err)
						return importer.RuleFallback([]int{bs[0].ID}, byID), nil
					}
					return nil, err
				}
				return append(out.Items, importer.RuleFallback(out.Dropped, byID)...), nil
			})
			outs[i] = items
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	for _, items := range outs {
		for _, it := range items {
			if it.Kind != "question" || strings.TrimSpace(it.Stem) == "" {
				continue
			}
			b := byID[it.Block]
			p := QuestionPayload{QType: it.QType, Stem: strings.TrimSpace(it.Stem), Answer: strings.TrimSpace(it.Answer), Score: it.Score}
			if b.QType != "" {
				p.QType = b.QType
			}
			if p.Score == nil && b.Score > 0 {
				v := b.Score
				p.Score = &v
			}
			// 按专业分组的题（规则识别）是限选题（T51）。
			if b.Group != "" {
				p.Group = &QuestionGroup{Label: b.Group, Majors: b.GroupMajors}
			}
			if f.Kind == dbq.OfficialImportFilesKindExam {
				switch {
				case it.ExamYear != nil:
					p.ExamYear = it.ExamYear
				case b.Year > 0:
					y := b.Year
					p.ExamYear = &y
				case f.ExamYear.Valid:
					y := int(f.ExamYear.Int16)
					p.ExamYear = &y
				}
			}
			if len(it.Options) > 0 {
				p.Options = it.Options
			}
			if !slices.Contains(ai.QTypes, p.QType) {
				p.QType = "other"
			}
			qs = append(qs, parsed{p: p, conf: it.Confidence, src: b.Text, page: b.Page})
		}
	}
	if f.AutoAnswer {
		defaults, err := s.defaultScores(ctx, f.ProjectID)
		if err != nil {
			return nil, err
		}
		var rateLimits atomic.Int32
		fill, fctx := errgroup.WithContext(ctx)
		fill.SetLimit(importAIParallel)
		for i := range qs {
			fill.Go(func() error {
				cite, filled, err := s.fillAnswer(fctx, f.ProjectID, &qs[i].p, defaults)
				if err != nil {
					n := 0
					if errors.Is(err, cloudai.ErrRateLimited) {
						n = int(rateLimits.Add(1))
					}
					if fillFailureFatal(err, n) {
						// 余额不足或累计限流：停止整份资料，避免继续逐题空打请求。
						return err
					}
					// 单题补答案失败不拖垮整份：原因记在条目上，编辑可以再点「生成答案」。
					s.d.Log.WarnContext(fctx, "official import fill answer failed", "file_id", f.ID, "err", err)
					qs[i].fillErr = fillErrorText(err)
					return nil
				}
				qs[i].cite, qs[i].ai = cite, filled
				return nil
			})
		}
		if err := fill.Wait(); err != nil {
			return nil, err // 余额不足/累计限流：整份停止，交给任务层稍后重试
		}
		if err := ctx.Err(); err != nil {
			return nil, err // 任务超时或被取消：交给重试，不保存一半没补答案的结果
		}
	}
	// 归知识点：按现有知识点树（已发布的与草稿里的）给每道题打板块 / 章节 / 知识点。
	if len(qs) > 0 {
		var tagIn ai.TagIn
		tagIn.Subject = proj.SubjectName
		tagIn.Existing, err = s.kpPaths(ctx, f.ProjectID)
		if err != nil {
			return nil, err
		}
		// 按 tagBatch 题一批：几百道题一次交给模型容易超时或回不全，结果就全是「未归类」。
		var batches []ai.TagIn
		for i, q := range qs {
			if i%tagBatch == 0 {
				batches = append(batches, ai.TagIn{Subject: tagIn.Subject, Existing: tagIn.Existing})
			}
			b := &batches[len(batches)-1]
			b.Questions = append(b.Questions, ai.TagQ{ID: i, QType: q.p.QType, Stem: q.p.Stem, Answer: q.p.Answer})
		}
		tags, tctx := errgroup.WithContext(ctx)
		tags.SetLimit(importAIParallel)
		for _, in := range batches {
			tags.Go(func() error {
				out, _, err := ai.TagQuestions.Run(tctx, s.d.AI, 0, in)
				if err != nil {
					s.d.Log.WarnContext(tctx, "official import tag failed", "file_id", f.ID, "err", err)
					return nil // 归不上的留「未归类」，编辑修改时再选
				}
				for _, t := range out.Tags {
					if t.ID >= 0 && t.ID < len(qs) && len(t.Path) == 3 {
						qs[t.ID].p.KPNames = []string{t.Path[2]}
					}
				}
				return nil
			})
		}
		_ = tags.Wait()
	}
	out := make([]dbq.InsertOfficialImportItemParams, 0, len(qs))
	for _, q := range qs {
		b, _ := json.Marshal(q.p)
		var cites dbtypes.NullJSON
		if len(q.cite) > 0 {
			cites, _ = json.Marshal(q.cite)
		}
		conf := confidenceOf(q.conf, q.ai, q.p)
		if q.fillErr != "" {
			conf = dbq.OfficialImportItemsConfidenceLow
		}
		out = append(out, dbq.InsertOfficialImportItemParams{ItemType: dbq.OfficialImportItemsItemTypeQuestion, Payload: b, Citations: cites,
			SourceText: nullStr(cutRunes(q.src, 2000)), Page: uint32(max(q.page, 0)), Confidence: conf, FillError: nullStr(q.fillErr)})
	}
	return out, nil
}

// kpPaths 是项目官方题库现有的知识点路径（已发布的与草稿里的），给 AI 归类用。
func (s *Service) kpPaths(ctx context.Context, projectID uint64) ([][]string, error) {
	var out [][]string
	seen := map[string]bool{}
	add := func(raw json.RawMessage) {
		var k KPPayload
		if json.Unmarshal(raw, &k) != nil || k.Name == "" || seen[k.Name] {
			return
		}
		seen[k.Name] = true
		out = append(out, []string{k.Section, k.Chapter, k.Name})
	}
	items, err := s.Items(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.EntityType == "knowledge_point" {
			add(it.Payload)
		}
	}
	drafts, err := s.Drafts(ctx, projectID, "", 0)
	if err != nil {
		return nil, err
	}
	for _, d := range drafts {
		if d.EntityType == "knowledge_point" {
			add(d.Payload)
		}
	}
	return out, nil
}

// 表格导入的列（增补「导入数据结构」）。院校、专业课名称、来源类型、方向在官方题库项目里已确定，不从表里读。
var tableColumns = map[string]string{"年份": "year", "题号": "no", "题型": "qtype", "分值": "score", "题干": "stem", "参考答案": "answer",
	"采分点": "rubric", "知识点": "kp", "出处": "source", "来源类型": "kind", "限选分组": "group", "适用专业": "group_majors"}

var tableQTypes = map[string]string{"名词解释": "term", "简答": "short_answer", "简答题": "short_answer", "论述": "discussion", "论述题": "discussion",
	"客观题": "single_choice", "单选": "single_choice", "选择": "single_choice"}

// reRubricLine 认一行采分点：「要点（3 分）」「要点 3分」「要点：3」。
var reRubricLine = regexp.MustCompile(`^(.+?)[\s：:（(]*([0-9]+(?:\.[0-9]+)?)\s*分?[)）]?\s*$`)

// ParseRubricCell 解析表格里的采分点：每行一条「要点 + 分值」。
func ParseRubricCell(cell string) ([]Point, error) {
	var out []Point
	for _, line := range strings.Split(strings.ReplaceAll(cell, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := reRubricLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("采分点「%s」没有分值", cutRunes(line, 20))
		}
		v, _ := strconv.ParseFloat(m[2], 64)
		out = append(out, Point{Content: strings.TrimSpace(strings.TrimRight(m[1], "，,；; ")), Score: v})
	}
	return out, nil
}

// parseTable 表格导入：CSV 第一行是表头。选填字段缺失时由 AI 补全（答案、采分点），审核后生效。
func (s *Service) parseTable(ctx context.Context, projectID uint64, text string, autoAnswer bool) ([]dbq.InsertOfficialImportItemParams, error) {
	r := csv.NewReader(bytes.NewReader([]byte(strings.TrimPrefix(text, "\uFEFF"))))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, apperr.New(apperr.BadRequest, "表格格式不正确，请另存为 CSV（UTF-8）")
	}
	if len(rows) < 2 {
		return nil, apperr.New(apperr.BadRequest, "表格里没有题目")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		if k, ok := tableColumns[strings.TrimSpace(h)]; ok {
			col[k] = i
		}
	}
	for _, need := range []string{"qtype", "score", "stem"} {
		if _, ok := col[need]; !ok {
			return nil, apperr.New(apperr.BadRequest, "表头缺少必填列：题型、分值、题干")
		}
	}
	get := func(row []string, k string) string {
		if i, ok := col[k]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var out []dbq.InsertOfficialImportItemParams
	for n, row := range rows[1:] {
		line := n + 2
		stem := get(row, "stem")
		if stem == "" {
			continue
		}
		qt, ok := tableQTypes[get(row, "qtype")]
		if !ok {
			return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("第 %d 行题型不正确（名词解释 / 简答 / 论述 / 客观题）", line))
		}
		sc, err := strconv.Atoi(get(row, "score"))
		if err != nil || sc <= 0 {
			return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("第 %d 行分值要是正整数", line))
		}
		score := float64(sc)
		p := QuestionPayload{QType: qt, Stem: stem, Answer: get(row, "answer"), Score: &score}
		if y, err := strconv.Atoi(get(row, "year")); err == nil && y > 1977 {
			p.ExamYear = &y
		} else if get(row, "kind") == "真题" {
			return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("第 %d 行是真题，年份必填", line))
		}
		if kp := get(row, "kp"); kp != "" {
			p.KPNames = []string{kp}
		}
		// 选填两列：「限选分组」（如 第一组）与「适用专业」；不填是必答题（T51）。
		if g := get(row, "group"); g != "" {
			p.Group = &QuestionGroup{Label: g, Majors: get(row, "group_majors")}
		} else if get(row, "group_majors") != "" {
			return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("第 %d 行填了适用专业，也要填限选分组（如 第一组）", line))
		}
		if cell := get(row, "rubric"); cell != "" {
			pts, err := ParseRubricCell(cell)
			if err != nil {
				return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("第 %d 行%s", line, err.Error()))
			}
			p.Rubric = pts
		}
		var cite []Citation
		filled, fillErr := false, ""
		if autoAnswer {
			cite, filled, err = s.fillAnswer(ctx, projectID, &p, nil)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				fillErr = fillErrorText(err)
			}
		}
		conf := dbq.OfficialImportItemsConfidenceHigh
		if filled || !RubricMatches(p) || needsFill(p) {
			conf = dbq.OfficialImportItemsConfidenceMedium
		}
		if fillErr != "" {
			conf = dbq.OfficialImportItemsConfidenceLow
		}
		b, _ := json.Marshal(p)
		var cites dbtypes.NullJSON
		if len(cite) > 0 {
			cites, _ = json.Marshal(cite)
		}
		src := stem
		if s := get(row, "source"); s != "" {
			src += "\n出处：" + s
		}
		out = append(out, dbq.InsertOfficialImportItemParams{ItemType: dbq.OfficialImportItemsItemTypeQuestion, Payload: b, Citations: cites,
			SourceText: nullStr(cutRunes(src, 2000)), Page: uint32(line), Confidence: conf, FillError: nullStr(fillErr)})
	}
	return out, nil
}

// --- 核对 ---

// ImportItem 是一条解析结果。
type ImportItem struct {
	ID         uint64
	ItemType   string
	Seq        int
	Payload    json.RawMessage
	Citations  []Citation
	SourceText string
	Page       int
	Confidence string
	Status     string
	DraftID    uint64
	// RubricMismatch 是采分点合计不等于分值（标红，不能接受）。
	RubricMismatch bool
	// NeedsFill 是还缺答案或采分点（可以点「生成答案 / 提取采分点」）；FillError 是上次补全失败的原因。
	NeedsFill bool
	FillError string
	// Duplicate 是重复说明（如「与《2025 真题》第 3 题相同」），只标还没核对的题；SeenYears 是别的年份也考过同一题（T52）。
	Duplicate string
	SeenYears []int
	// AnswerAI 是答案由 AI 生成（还没核对的可以「按关联教材重新生成」）；AnswerBasis 是没有引用教材的原因（T53）。
	AnswerAI    bool
	AnswerBasis *ai.AnswerBasis
}

// ImportDetail 是核对区：原文与解析结果。
type ImportDetail struct {
	File  ImportFile
	Text  string
	Items []ImportItem
}

// ImportItems 读出一份资料的原文与解析结果。
func (s *Service) ImportItems(ctx context.Context, projectID, fileID uint64) (ImportDetail, error) {
	f, err := s.importFile(ctx, projectID, fileID)
	if err != nil {
		return ImportDetail{}, err
	}
	rows, err := s.q.ListOfficialImportItems(ctx, fileID)
	if err != nil {
		return ImportDetail{}, err
	}
	out := ImportDetail{File: ImportFile{ID: f.ID, Kind: string(f.Kind), FileName: f.FileName, Format: string(f.Format), Status: string(f.Status),
		FailReason: f.FailReason.String, QuestionCount: int(f.QuestionCount), KPCount: int(f.KpCount), ExamYear: int(f.ExamYear.Int16), CreatedAt: f.CreatedAt},
		Text: f.Content.String, Items: make([]ImportItem, len(rows))}
	out.File.AutoAnswer = f.AutoAnswer
	out.File.Fill = ImportFill{Status: string(f.FillStatus), Total: int(f.FillTotal), Done: int(f.FillDone), Failed: int(f.FillFailed)}
	for i, r := range rows {
		it := toImportItem(r)
		if it.Status != "pending" {
			out.File.ReviewedCount++
		}
		out.Items[i] = it
	}
	if err := s.markDuplicates(ctx, f, out.Items); err != nil {
		return ImportDetail{}, err
	}
	return out, nil
}

// RejectDuplicates 一键驳回这份资料里标为重复、还没核对的题（T52），返回驳回的条数。
func (s *Service) RejectDuplicates(ctx context.Context, editorID, projectID, fileID uint64) (int, error) {
	d, err := s.ImportItems(ctx, projectID, fileID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range d.Items {
		if it.Duplicate == "" {
			continue
		}
		if _, err := s.ReviewImportItem(ctx, editorID, projectID, it.ID, "reject", nil); err != nil {
			var ae *apperr.Error
			if errors.As(err, &ae) && ae.Kind == apperr.Conflict {
				continue // 别人刚核对过这一条
			}
			return n, err
		}
		n++
	}
	return n, nil
}

func toImportItem(r dbq.OfficialImportItem) ImportItem {
	it := ImportItem{ID: r.ID, ItemType: string(r.ItemType), Seq: int(r.Seq), Payload: r.Payload, SourceText: r.SourceText.String, Page: int(r.Page),
		Confidence: string(r.Confidence), Status: string(r.Status), DraftID: uint64(r.DraftID.Int64), FillError: r.FillError.String}
	if len(r.Citations) > 0 {
		_ = json.Unmarshal(r.Citations, &it.Citations)
	}
	if r.ItemType == dbq.OfficialImportItemsItemTypeQuestion {
		var p QuestionPayload
		if json.Unmarshal(r.Payload, &p) == nil {
			it.RubricMismatch = !RubricMatches(p)
			it.NeedsFill = needsFill(p)
			it.AnswerAI, it.AnswerBasis = p.AnswerAI && strings.TrimSpace(p.Answer) != "", p.AnswerBasis
		}
	}
	return it
}

// ReviewImportItem 核对一条：accept 接受（payload 不为空时是「修改后接受」），生成 7.12 草稿并提交 7.13 审核；reject 驳回。
// 采分点合计不等于分值时不能接受。返回生成的草稿 id。
func (s *Service) ReviewImportItem(ctx context.Context, editorID, projectID, itemID uint64, decision string, edited json.RawMessage) (uint64, error) {
	it, err := s.q.GetOfficialImportItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && it.ProjectID != projectID) {
		return 0, apperr.NotFoundErr()
	}
	if err != nil {
		return 0, err
	}
	if it.Status != dbq.OfficialImportItemsStatusPending {
		return 0, apperr.New(apperr.Conflict, "这一条已经核对过了")
	}
	now := sql.NullTime{Time: s.now().UTC(), Valid: true}
	by := sql.NullInt64{Int64: int64(editorID), Valid: editorID > 0}
	switch decision {
	case "reject":
		_, err := s.q.ReviewOfficialImportItem(ctx, dbq.ReviewOfficialImportItemParams{Status: dbq.OfficialImportItemsStatusRejected, Payload: it.Payload,
			ReviewedBy: by, ReviewedAt: now, ID: itemID})
		return 0, err
	case "accept":
	default:
		return 0, apperr.New(apperr.BadRequest, "核对结果不正确")
	}
	payload := it.Payload
	if len(edited) > 0 {
		payload = edited
	}
	if it.ItemType == dbq.OfficialImportItemsItemTypeQuestion {
		var p QuestionPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return 0, apperr.New(apperr.BadRequest, "题目格式不正确")
		}
		if p.Regenerate {
			// 整份重新生成还在跑时先别接受；跑完了还带着标记是重新生成失败，接受原来的答案。
			f, err := s.q.GetOfficialImportFile(ctx, it.FileID)
			if err != nil {
				return 0, err
			}
			if f.FillStatus == dbq.OfficialImportFilesFillStatusRunning {
				return 0, apperr.New(apperr.Conflict, "这道题正在重新生成答案，请稍候")
			}
		}
		// 核对区的说明不进草稿（T53）。
		if p.AnswerBasis != nil || p.RubricKept || p.Regenerate {
			p.AnswerBasis, p.RubricKept, p.Regenerate = nil, false, false
			payload, _ = json.Marshal(p)
		}
		if ai.Subjective(p.QType) && len(p.Rubric) == 0 {
			return 0, apperr.New(apperr.BadRequest, "这道题还没有采分点，先点「生成答案 / 提取采分点」，或在「修改」里填写").With("reason", "needs_fill")
		}
		if !RubricMatches(p) {
			return 0, apperr.New(apperr.BadRequest, "采分点合计和分值不一致，先修改").With("reason", "rubric_sum")
		}
	}
	d, err := s.SaveDraft(ctx, editorID, projectID, 0, string(it.ItemType), "add", 0, payload)
	if err != nil {
		return 0, err
	}
	if _, err := s.Submit(ctx, editorID, projectID, d.ID); err != nil {
		return 0, err
	}
	if _, err := s.q.ReviewOfficialImportItem(ctx, dbq.ReviewOfficialImportItemParams{Status: dbq.OfficialImportItemsStatusAccepted, Payload: payload,
		DraftID: sql.NullInt64{Int64: int64(d.ID), Valid: true}, ReviewedBy: by, ReviewedAt: now, ID: itemID}); err != nil {
		return 0, err
	}
	return d.ID, nil
}

// --- 解析后补答案与采分点（T49） ---

// fillErrorText 是补全失败时给编辑看的原因：业务错误（缺分值、模型输出不合格）原样给，平台错误统一说稍后再试。
func fillErrorText(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) && (ae.Kind == apperr.BadRequest || ae.Kind == apperr.AIFailed) {
		return cutRunes(ae.Message, 255)
	}
	return "AI 暂时不可用，请稍后再试"
}

// filledConfidence 是补全后的置信度：AI 补的最多算中；原来因补全失败记低的回到中，结构化本身把握低的仍是低。
func filledConfidence(it dbq.OfficialImportItem, p QuestionPayload) dbq.OfficialImportItemsConfidence {
	c := it.Confidence
	if c == dbq.OfficialImportItemsConfidenceHigh || (c == dbq.OfficialImportItemsConfidenceLow && it.FillError.Valid) {
		c = dbq.OfficialImportItemsConfidenceMedium
	}
	if c == dbq.OfficialImportItemsConfidenceHigh && !RubricMatches(p) {
		c = dbq.OfficialImportItemsConfidenceMedium
	}
	return c
}

// fillItem 给一条还没核对的题补答案与采分点，结果写回条目；失败的原因也记在条目上。
func (s *Service) fillItem(ctx context.Context, it dbq.OfficialImportItem, defaults map[string]float64) error {
	var p QuestionPayload
	if err := json.Unmarshal(it.Payload, &p); err != nil {
		return apperr.New(apperr.BadRequest, "题目格式不正确")
	}
	return s.fillItemWith(ctx, it, p, defaults)
}

// fillItemWith 按 p 补全并写回；失败时条目内容保持 it.Payload 不变，只记原因。
func (s *Service) fillItemWith(ctx context.Context, it dbq.OfficialImportItem, p QuestionPayload, defaults map[string]float64) error {
	cite, _, err := s.fillAnswer(ctx, it.ProjectID, &p, defaults)
	if err != nil {
		if ctx.Err() == nil {
			_, _ = s.q.UpdateOfficialImportItemFill(ctx, dbq.UpdateOfficialImportItemFillParams{Payload: it.Payload, Citations: it.Citations,
				Confidence: it.Confidence, FillError: nullStr(fillErrorText(err)), ID: it.ID})
		}
		return err
	}
	b, _ := json.Marshal(p)
	var cites dbtypes.NullJSON
	if cite != nil { // AI 生成了答案：引用整个换掉（重新生成后没有引用时清空旧的）
		cites, _ = json.Marshal(cite)
	} else {
		cites = it.Citations
	}
	n, err := s.q.UpdateOfficialImportItemFill(ctx, dbq.UpdateOfficialImportItemFillParams{Payload: b, Citations: cites,
		Confidence: filledConfidence(it, p), ID: it.ID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.New(apperr.Conflict, "这一条已经核对过了")
	}
	return nil
}

// FillImportItem 「生成答案 / 提取采分点」：给一道还没核对的题补上（缺答案的 AI 生成，有答案缺采分点的从答案提），返回补好的条目。
// 失败时返回原因（缺分值且同题型没有可参考的、模型输出不合格等），原因同时记在条目上。
func (s *Service) FillImportItem(ctx context.Context, projectID, itemID uint64) (ImportItem, error) {
	it, err := s.q.GetOfficialImportItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && it.ProjectID != projectID) {
		return ImportItem{}, apperr.NotFoundErr()
	}
	if err != nil {
		return ImportItem{}, err
	}
	if it.Status != dbq.OfficialImportItemsStatusPending {
		return ImportItem{}, apperr.New(apperr.Conflict, "这一条已经核对过了")
	}
	if it.ItemType != dbq.OfficialImportItemsItemTypeQuestion {
		return ImportItem{}, apperr.New(apperr.BadRequest, "只有题目可以生成答案")
	}
	var p QuestionPayload
	if err := json.Unmarshal(it.Payload, &p); err != nil {
		return ImportItem{}, apperr.New(apperr.BadRequest, "题目格式不正确")
	}
	if !needsFill(p) {
		return ImportItem{}, apperr.New(apperr.BadRequest, "这道题已经有答案和采分点了")
	}
	defaults, err := s.defaultScores(ctx, projectID)
	if err != nil {
		return ImportItem{}, err
	}
	if err := s.fillItem(ctx, it, defaults); err != nil {
		return ImportItem{}, syncFillError(err)
	}
	it, err = s.q.GetOfficialImportItem(ctx, itemID)
	if err != nil {
		return ImportItem{}, err
	}
	return toImportItem(it), nil
}

// syncFillError：同步接口没有重试，模型平台的原始错误（超时、限流）转成「生成失败」，不变成 500。
func syncFillError(err error) error {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return err
	}
	return apperr.New(apperr.AIFailed, "AI 暂时不可用，请稍后再试").Wrap(err)
}

// pendingFills 是一份资料里还没核对、缺答案或采分点的题。
func (s *Service) pendingFills(ctx context.Context, fileID uint64) ([]dbq.OfficialImportItem, error) {
	rows, err := s.q.ListOfficialImportItems(ctx, fileID)
	if err != nil {
		return nil, err
	}
	var out []dbq.OfficialImportItem
	for _, r := range rows {
		if r.Status != dbq.OfficialImportItemsStatusPending || r.ItemType != dbq.OfficialImportItemsItemTypeQuestion {
			continue
		}
		var p QuestionPayload
		if json.Unmarshal(r.Payload, &p) == nil && needsFill(p) {
			out = append(out, r)
		}
	}
	return out, nil
}

// FillImportFile 「一键补全」：这份资料里还没核对、缺答案或采分点的题都补上，在 Worker 里排队执行，进度见资料卡。
func (s *Service) FillImportFile(ctx context.Context, projectID, fileID uint64) (ImportFile, error) {
	f, err := s.importFile(ctx, projectID, fileID)
	if err != nil {
		return ImportFile{}, err
	}
	if f.Status != dbq.OfficialImportFilesStatusParsed {
		return ImportFile{}, apperr.New(apperr.Conflict, "资料还没解析完")
	}
	if f.FillStatus == dbq.OfficialImportFilesFillStatusRunning {
		return ImportFile{}, apperr.New(apperr.Conflict, "正在补全，请稍候")
	}
	todo, err := s.pendingFills(ctx, fileID)
	if err != nil {
		return ImportFile{}, err
	}
	if len(todo) == 0 {
		return ImportFile{}, apperr.New(apperr.BadRequest, "没有缺答案或采分点的题")
	}
	n, err := s.q.StartOfficialImportFill(ctx, dbq.StartOfficialImportFillParams{FillTotal: uint32(len(todo)), ID: fileID})
	if err != nil {
		return ImportFile{}, err
	}
	if n == 0 {
		return ImportFile{}, apperr.New(apperr.Conflict, "正在补全，请稍候")
	}
	if err := s.enqueueFill(ctx, fileID); err != nil {
		_ = s.q.FinishOfficialImportFill(ctx, fileID)
		return ImportFile{}, err
	}
	d, err := s.ImportItems(ctx, projectID, fileID)
	if err != nil {
		return ImportFile{}, err
	}
	return d.File, nil
}

// enqueueFill 排一键补全任务；没有队列时同步执行（本地与测试）。
func (s *Service) enqueueFill(ctx context.Context, fileID uint64) error {
	if s.imports.Queue == nil || s.imports.NewFillTask == nil {
		return s.ProcessImportFill(ctx, fileID)
	}
	t, err := s.imports.NewFillTask(fileID)
	if err != nil {
		return err
	}
	_, err = s.imports.Queue.EnqueueContext(ctx, t)
	return err
}

// ProcessImportFill 执行一键补全（Worker 调用，可重复执行：已补好的题不再补，重试时接着补剩下的）。
// 单题失败不影响其余，原因记在条目上；全部处理完把状态改回 idle。
func (s *Service) ProcessImportFill(ctx context.Context, fileID uint64) error {
	f, err := s.q.GetOfficialImportFile(ctx, fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.FillStatus != dbq.OfficialImportFilesFillStatusRunning {
		return nil
	}
	todo, err := s.pendingFills(ctx, fileID)
	if err != nil {
		return err
	}
	defaults, err := s.defaultScores(ctx, f.ProjectID)
	if err != nil {
		return err
	}
	total := max(int(f.FillTotal), len(todo))
	var mu sync.Mutex
	done, failed := total-len(todo), 0
	progress := func(ok bool) {
		mu.Lock()
		defer mu.Unlock()
		done++
		if !ok {
			failed++
		}
		_ = s.q.SetOfficialImportFillProgress(ctx, dbq.SetOfficialImportFillProgressParams{FillDone: uint32(done), FillFailed: uint32(failed), ID: fileID})
	}
	if err := s.q.SetOfficialImportFillProgress(ctx, dbq.SetOfficialImportFillProgressParams{FillDone: uint32(done), ID: fileID}); err != nil {
		return err
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(importAIParallel)
	for _, it := range todo {
		g.Go(func() error {
			err := s.fillItem(gctx, it, defaults)
			if err != nil && gctx.Err() == nil {
				s.d.Log.WarnContext(gctx, "official import fill failed", "file_id", fileID, "item_id", it.ID, "err", err)
			}
			if gctx.Err() == nil {
				progress(err == nil)
			}
			return nil
		})
	}
	_ = g.Wait()
	if err := ctx.Err(); err != nil {
		return err // 超时或被取消：交给重试，接着补剩下的
	}
	return s.q.FinishOfficialImportFill(ctx, fileID)
}

// RecoverImports 把卡在解析中超过一小时的资料重新排队（Worker 重启丢任务时）。
func (s *Service) RecoverImports(ctx context.Context) error {
	ids, err := s.q.ListStaleOfficialImports(ctx, s.now().UTC().Add(-time.Hour))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.enqueueImport(ctx, id); err != nil {
			return err
		}
	}
	// 一键补全一小时没有进度的，同样重新排队。
	fills, err := s.q.ListStaleOfficialImportFills(ctx, s.now().UTC().Add(-time.Hour))
	if err != nil {
		return err
	}
	for _, id := range fills {
		if err := s.enqueueFill(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
