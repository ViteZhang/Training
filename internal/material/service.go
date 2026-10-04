// Package material 是用户上传的资料（T08）：申请直传、确认上传、粘贴文字、列表、删除与连带规则、内容安全。
package material

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/params"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// Limits 是 rule_params.import_limits（PRD 11.12）。
type Limits struct {
	MaxFiles        int `json:"max_files"`
	MaxPagesPerFile int `json:"max_pages_per_file"`
	MaxMBPerFile    int `json:"max_mb_per_file"`
	MaxImages       int `json:"max_images"`
	MaxPasteChars   int `json:"max_paste_chars"`
	CharsPerPage    int `json:"chars_per_page"`
	RowsPerPage     int `json:"rows_per_page"`
}

// Service 是资料服务。
type Service struct {
	db         *sql.DB
	q          *dbq.Queries
	oss        oss.Store
	moderation moderation.Checker
	quota      *quota.Service
	params     *params.Store
	ocr        ocr.Recognizer
	pdf        ocr.PDFParser
	flags      FlagChecker
	now        func() time.Time
	// OnDeleted 在删除资料后调用（T22 用来触发预估分重算）。
	OnDeleted func(ctx context.Context, userID, subjectID uint64)
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB         *sql.DB
	OSS        oss.Store
	Moderation moderation.Checker
	Quota      *quota.Service
	Params     *params.Store
	OCR        ocr.Recognizer
	PDF        ocr.PDFParser
	Flags      FlagChecker
	Now        func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: d.DB, q: dbq.New(d.DB), oss: d.OSS, moderation: d.Moderation, quota: d.Quota, params: d.Params, ocr: d.OCR, pdf: d.PDF, flags: d.Flags, now: now}
}

func (s *Service) limits(ctx context.Context) (Limits, error) {
	var l Limits
	err := s.params.Get(ctx, "import_limits", &l)
	return l, err
}

// bank 返回专业课的自建题库；不是自己的专业课返回 404。
func (s *Service) bank(ctx context.Context, userID, subjectID uint64) (dbq.Bank, error) {
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	b, err := s.q.GetBankForSubject(ctx, dbq.GetBankForSubjectParams{ID: subjectID, OwnerUserID: userID, OwnerUserID_2: owner})
	if errors.Is(err, sql.ErrNoRows) {
		return dbq.Bank{}, apperr.NotFoundErr()
	}
	return b, err
}

// File 是申请上传的一个文件。
type File struct {
	Name        string
	Format      string // docx / xlsx / pdf / image
	Size        int64
	SHA256      string
	PageCount   int // PDF 由客户端读出，服务端解析时复核；图片为 1
	ContentType string
}

// Target 是一个文件的上传目标。
type Target struct {
	Index      int
	MaterialID uint64
	Duplicate  bool
	Upload     *oss.Presigned
}

const uploadTTL = 30 * time.Minute

// RequestUploads 为每个文件建资料记录并返回预签名直传地址（1.5）。
//
//   - 必须确认「对资料有合法使用权」
//   - 单次最多 10 个文件、每个不超过 200 页与 50 MB；图片每次最多 30 张（PRD 11.12）
//   - 同一用户上传过相同内容（sha256）的文件直接返回已有资料，不再上传、不重复扣额度
//   - 解析额度不足时在开始前提示（402），不让用户等解析完才失败；精确计费在解析时按实际页数预占
func (s *Service) RequestUploads(ctx context.Context, userID, subjectID uint64, category string, files []File, rightConfirmed bool) ([]Target, error) {
	if !rightConfirmed {
		return nil, apperr.New(apperr.BadRequest, "请先确认对这些资料有合法的使用权").With("reason", "right_not_confirmed")
	}
	l, err := s.limits(ctx)
	if err != nil {
		return nil, err
	}
	images, docs, knownPages := 0, 0, 0
	for _, f := range files {
		if f.Format == "image" {
			images++
			knownPages++
		} else {
			docs++
		}
		if f.Size > int64(l.MaxMBPerFile)<<20 {
			return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("「%s」超过 %d MB，请压缩或拆分后再传", f.Name, l.MaxMBPerFile)).With("reason", "file_too_large")
		}
		if f.Format == "pdf" && f.PageCount > l.MaxPagesPerFile {
			return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("「%s」超过 %d 页，请拆分后再传", f.Name, l.MaxPagesPerFile)).With("reason", "too_many_pages")
		}
		if f.Format == "pdf" {
			knownPages += f.PageCount
		}
	}
	if docs > l.MaxFiles {
		return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("单次最多选 %d 个文件", l.MaxFiles)).With("reason", "too_many_files")
	}
	if images > l.MaxImages {
		return nil, apperr.New(apperr.BadRequest, fmt.Sprintf("图片每次最多 %d 张", l.MaxImages)).With("reason", "too_many_images")
	}
	b, err := s.bank(ctx, userID, subjectID)
	if err != nil {
		return nil, err
	}
	if left, err := s.quota.Remaining(ctx, userID, quota.ParsePages); err != nil {
		return nil, err
	} else if left != nil && (*left == 0 || knownPages > *left) {
		return nil, apperr.New(apperr.QuotaExceeded, "资料解析额度不足").
			With("quota_type", string(quota.ParsePages)).With("remaining", *left).With("need", knownPages)
	}

	out := make([]Target, len(files))
	now := s.now().UTC()
	for i, f := range files {
		out[i].Index = i
		if existing, err := s.q.GetMaterialBySha(ctx, dbq.GetMaterialByShaParams{OwnerUserID: userID, Sha256: f.SHA256}); err == nil {
			out[i].MaterialID, out[i].Duplicate = existing.ID, true
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		pages := f.PageCount
		if f.Format == "image" {
			pages = 1
		}
		id, err := s.q.CreateMaterial(ctx, dbq.CreateMaterialParams{
			OwnerUserID: userID, BankID: b.ID, Category: nullCategory(category), FileName: truncate(f.Name, 255),
			Format: dbq.MaterialsFormat(f.Format), SizeBytes: uint64(f.Size), PageCount: uint32(max(pages, 0)),
			Sha256: f.SHA256, Status: dbq.MaterialsStatusUploading, RightConfirmedAt: now,
		})
		if isDuplicate(err) {
			// 同一请求里选了两份相同的文件。
			existing, gerr := s.q.GetMaterialBySha(ctx, dbq.GetMaterialByShaParams{OwnerUserID: userID, Sha256: f.SHA256})
			if gerr != nil {
				return nil, gerr
			}
			out[i].MaterialID, out[i].Duplicate = existing.ID, true
			continue
		}
		if err != nil {
			return nil, err
		}
		mid := uint64(id)
		key := oss.MaterialKey(userID, b.ID, mid, extension(f))
		if err := s.q.SetMaterialObjectKey(ctx, dbq.SetMaterialObjectKeyParams{ObjectKey: sql.NullString{String: key, Valid: true}, ID: mid, OwnerUserID: userID}); err != nil {
			return nil, err
		}
		p, err := s.oss.PresignPut(ctx, key, contentType(f), f.Size, uploadTTL)
		if err != nil {
			return nil, err
		}
		out[i].MaterialID, out[i].Upload = mid, &p
	}
	return out, nil
}

// Material 是资料及统计。
type Material = dbq.GetMaterialRow

// Get 返回资料；不是自己的返回 404。
func (s *Service) Get(ctx context.Context, userID, id uint64) (Material, error) {
	m, err := s.q.GetMaterial(ctx, dbq.GetMaterialParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Material{}, apperr.NotFoundErr()
	}
	return m, err
}

// ConfirmUploaded 客户端直传完成后回调：核对对象存在、大小一致，状态改为已上传。
func (s *Service) ConfirmUploaded(ctx context.Context, userID, id uint64) (Material, error) {
	m, err := s.Get(ctx, userID, id)
	if err != nil {
		return Material{}, err
	}
	if m.Status != dbq.MaterialsStatusUploading {
		return m, nil
	}
	info, err := s.oss.Head(ctx, m.ObjectKey.String)
	if err != nil {
		return Material{}, err
	}
	if !info.Exists {
		return Material{}, apperr.New(apperr.BadRequest, "文件还没有上传完成，请重试").With("reason", "not_uploaded")
	}
	if info.Size != int64(m.SizeBytes) {
		return Material{}, apperr.New(apperr.BadRequest, "文件上传不完整，请重新上传").With("reason", "size_mismatch")
	}
	if err := s.q.UpdateMaterialStatus(ctx, dbq.UpdateMaterialStatusParams{Status: dbq.MaterialsStatusUploaded, ID: id, OwnerUserID: userID}); err != nil {
		return Material{}, err
	}
	return s.Get(ctx, userID, id)
}

// CreatePasted 粘贴文字导入（1.5b）：单次最多 2 万字；按每 1500 字分页保存，计费页数同样按 1500 字一页（PRD 11.12）。
func (s *Service) CreatePasted(ctx context.Context, userID, subjectID uint64, category, title, text string, rightConfirmed bool) (Material, error) {
	if !rightConfirmed {
		return Material{}, apperr.New(apperr.BadRequest, "请先确认对这些资料有合法的使用权").With("reason", "right_not_confirmed")
	}
	l, err := s.limits(ctx)
	if err != nil {
		return Material{}, err
	}
	text = strings.TrimSpace(text)
	n := utf8.RuneCountInString(text)
	if n == 0 {
		return Material{}, apperr.New(apperr.BadRequest, "请粘贴要导入的文字")
	}
	if n > l.MaxPasteChars {
		return Material{}, apperr.New(apperr.BadRequest, fmt.Sprintf("单次最多粘贴 %d 字", l.MaxPasteChars)).With("reason", "too_long")
	}
	b, err := s.bank(ctx, userID, subjectID)
	if err != nil {
		return Material{}, err
	}
	pages := extract.Text(text, l.CharsPerPage).Pages
	if title = strings.TrimSpace(title); title == "" {
		title = "粘贴的文字 " + s.now().In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04")
	}
	sum := sha(text)
	if existing, err := s.q.GetMaterialBySha(ctx, dbq.GetMaterialByShaParams{OwnerUserID: userID, Sha256: sum}); err == nil {
		return s.Get(ctx, userID, existing.ID)
	}
	var id uint64
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		res, err := q.CreateMaterial(ctx, dbq.CreateMaterialParams{
			OwnerUserID: userID, BankID: b.ID, Category: nullCategory(category), FileName: truncate(title, 255),
			Format: dbq.MaterialsFormatText, SizeBytes: uint64(len(text)), PageCount: uint32(len(pages)),
			BilledPages: uint32(len(pages)), Sha256: sum, Status: dbq.MaterialsStatusUploaded, RightConfirmedAt: s.now().UTC(),
		})
		if err != nil {
			return err
		}
		id = uint64(res)
		for _, p := range pages {
			if err := q.UpsertMaterialPage(ctx, dbq.UpsertMaterialPageParams{MaterialID: id, PageNo: uint32(p.No), OwnerUserID: userID, Text: p.Text}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Material{}, err
	}
	return s.Get(ctx, userID, id)
}

// SplitPages 按字数分页，尽量在换行处断开。
var SplitPages = extract.SplitPages

// ListItem 是资料列表里的一项。
type ListItem = dbq.ListMaterialsByBankRow

// List 返回某专业课的全部资料（3.1c、6.3）。
func (s *Service) List(ctx context.Context, userID, subjectID uint64) ([]ListItem, error) {
	b, err := s.bank(ctx, userID, subjectID)
	if err != nil {
		return nil, err
	}
	return s.q.ListMaterialsByBank(ctx, dbq.ListMaterialsByBankParams{BankID: b.ID, OwnerUserID: userID})
}

// Impact 是删除资料的连带影响（3.1d）。
type Impact struct {
	Questions, Attempts, Wrong, KPDelete, KPKeep, PaperSessions int
}

// DeletionImpact 计算删除资料的连带影响。
func (s *Service) DeletionImpact(ctx context.Context, userID, id uint64) (Impact, error) {
	m, err := s.Get(ctx, userID, id)
	if err != nil {
		return Impact{}, err
	}
	c, err := s.q.CountMaterialImpact(ctx, dbq.CountMaterialImpactParams{MaterialID: sql.NullInt64{Int64: int64(id), Valid: true}})
	if err != nil {
		return Impact{}, err
	}
	only, err := s.q.ListKPsOnlyFromMaterial(ctx, dbq.ListKPsOnlyFromMaterialParams{BankID: m.BankID, MaterialID: id})
	if err != nil {
		return Impact{}, err
	}
	all, err := s.q.CountKPsFromMaterial(ctx, id)
	if err != nil {
		return Impact{}, err
	}
	return Impact{
		Questions: int(c.QuestionCount), Attempts: int(c.AttemptCount), Wrong: int(c.WrongCount),
		KPDelete: len(only), KPKeep: int(all) - len(only), PaperSessions: int(c.PaperSessionCount),
	}, nil
}

// Delete 删除资料（PRD 11.12）：从它识别出的题连同作答记录和错题一起删除；只来自它的知识点删除，其他资料也有的保留；
// 用它做过的整卷成绩保留，预估分重算；OSS 原件一并删除。
func (s *Service) Delete(ctx context.Context, userID, id uint64) error {
	m, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		only, err := q.ListKPsOnlyFromMaterial(ctx, dbq.ListKPsOnlyFromMaterialParams{BankID: m.BankID, MaterialID: id})
		if err != nil {
			return err
		}
		mid := sql.NullInt64{Int64: int64(id), Valid: true}
		if err := q.DeleteQuestionsFromMaterial(ctx, dbq.DeleteQuestionsFromMaterialParams{SourceMaterialID: mid, BankID: m.BankID}); err != nil {
			return err
		}
		if err := q.DetachPapersFromMaterial(ctx, dbq.DetachPapersFromMaterialParams{SourceMaterialID: mid, BankID: m.BankID}); err != nil {
			return err
		}
		if err := q.RepointKPSources(ctx, dbq.RepointKPSourcesParams{MaterialID: id, BankID: m.BankID, SourceMaterialID: mid}); err != nil {
			return err
		}
		for _, kp := range only {
			if err := q.DeleteKnowledgePoint(ctx, dbq.DeleteKnowledgePointParams{ID: kp, BankID: m.BankID}); err != nil {
				return err
			}
		}
		n, err := q.DeleteMaterial(ctx, dbq.DeleteMaterialParams{ID: id, OwnerUserID: userID})
		if err == nil && n == 0 {
			return apperr.NotFoundErr()
		}
		return err
	})
	if err != nil {
		return err
	}
	if m.ObjectKey.Valid {
		// 数据库已删除；原件删除失败只影响存储空间，由注销或巡检任务兜底，不影响用户。
		_ = s.oss.Delete(ctx, m.ObjectKey.String)
	}
	if s.OnDeleted != nil && m.SubjectID.Valid {
		s.OnDeleted(ctx, userID, uint64(m.SubjectID.Int64))
	}
	return nil
}

// Moderate 对资料做内容安全审核（导入流水线第 4 步，dev-spec 第六节）：文字逐段、图片逐张；
// 不通过的整份资料标为 rejected 并给出原因，返回 false。
func (s *Service) Moderate(ctx context.Context, userID, id uint64) (bool, error) {
	m, err := s.Get(ctx, userID, id)
	if err != nil {
		return false, err
	}
	reject := func(reason string) (bool, error) {
		err := s.q.UpdateMaterialStatus(ctx, dbq.UpdateMaterialStatusParams{
			Status: dbq.MaterialsStatusRejected, FailReason: sql.NullString{String: reason, Valid: true}, ID: id, OwnerUserID: userID,
		})
		return false, err
	}
	if m.Format == dbq.MaterialsFormatImage && m.ObjectKey.Valid {
		v, err := s.moderation.CheckImage(ctx, m.ObjectKey.String)
		if err != nil {
			return false, err
		}
		if !v.Pass {
			return reject("这份资料未通过内容安全审核：" + v.Reason)
		}
	}
	pages, err := s.q.ListMaterialPages(ctx, dbq.ListMaterialPagesParams{MaterialID: id, OwnerUserID: userID})
	if err != nil {
		return false, err
	}
	for _, p := range pages {
		for _, chunk := range SplitPages(p.Text, moderationChunk) {
			v, err := s.moderation.CheckText(ctx, chunk)
			if err != nil {
				return false, err
			}
			if !v.Pass {
				return reject(fmt.Sprintf("这份资料第 %d 页未通过内容安全审核：%s", p.PageNo, v.Reason))
			}
		}
	}
	return true, nil
}

// moderationChunk 是每次送审的字数（阿里云文本审核单次上限内）。
const moderationChunk = 500
