package material

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/store"
)

// FlagChecker 查功能开关（flags.Service 实现）。
type FlagChecker interface {
	Enabled(ctx context.Context, key string, userID uint64) (bool, error)
}

// FlagScannedPDF 控制扫描版 PDF 导入（dev-spec 第六节：评测达标才开）。
const FlagScannedPDF = "scanned_pdf"

// ErrNotUploaded 表示文件还没有上传完成，不能取文本。
var ErrNotUploaded = errors.New("material: 文件还没有上传完成")

// Extract 是导入流水线第 3 步「取文本」（dev-spec 第六节）：按格式取出带页码的文字写入 material_pages，
// 并写入实际页数与计费页数（PRD 11.12）。可重复执行：重跑覆盖上次的结果。
//
// 文件本身的问题（格式不支持、损坏、扫描版未开放、照片里没有文字）返回 *extract.UserError，
// 资料标为 failed 并写明原因，重试也不会好；其他错误（网络、识别服务）原样返回，由任务重试。
func (s *Service) Extract(ctx context.Context, userID, id uint64) error {
	m, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if m.Status == dbq.MaterialsStatusUploading {
		return ErrNotUploaded
	}
	if m.Status == dbq.MaterialsStatusRejected {
		return nil
	}
	if m.Format == dbq.MaterialsFormatText {
		// 粘贴的文字创建时已分好页。
		return nil
	}
	l, err := s.limits(ctx)
	if err != nil {
		return err
	}
	if err := s.setStatus(ctx, userID, id, dbq.MaterialsStatusParsing, ""); err != nil {
		return err
	}
	res, err := s.extract(ctx, userID, m, l)
	if ue, ok := extract.AsUserError(err); ok {
		if err := s.setStatus(ctx, userID, id, dbq.MaterialsStatusFailed, ue.Msg); err != nil {
			return err
		}
		return ue
	}
	if err != nil {
		return err
	}
	return store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		for _, p := range res.Pages {
			arg := dbq.UpsertMaterialPageParams{MaterialID: id, PageNo: uint32(p.No), OwnerUserID: userID, Text: p.Text}
			if arg.Tables, err = nullJSON(p.Tables); err != nil {
				return err
			}
			if arg.LowConfidence, err = nullJSON(p.LowConfidence); err != nil {
				return err
			}
			if err := q.UpsertMaterialPage(ctx, arg); err != nil {
				return err
			}
		}
		if err := q.DeleteMaterialPagesAfter(ctx, dbq.DeleteMaterialPagesAfterParams{MaterialID: id, OwnerUserID: userID, PageNo: uint32(len(res.Pages))}); err != nil {
			return err
		}
		return q.UpdateMaterialPages(ctx, dbq.UpdateMaterialPagesParams{
			PageCount: uint32(len(res.Pages)), BilledPages: uint32(res.BilledPages), ID: id, OwnerUserID: userID,
		})
	})
}

func (s *Service) extract(ctx context.Context, userID uint64, m Material, l Limits) (extract.Result, error) {
	data, err := s.read(ctx, m.ObjectKey.String)
	if err != nil {
		return extract.Result{}, err
	}
	switch m.Format {
	case dbq.MaterialsFormatDocx:
		return extract.Docx(data, l.CharsPerPage)
	case dbq.MaterialsFormatXlsx:
		return extract.Xlsx(data, l.RowsPerPage)
	case dbq.MaterialsFormatImage:
		r, err := s.ocr.Recognize(ctx, ocr.Image{ObjectKey: m.ObjectKey.String, Data: data})
		if err != nil {
			return extract.Result{}, err
		}
		if r.Text == "" {
			return extract.Result{}, &extract.UserError{Msg: "照片里没有识别到文字。请对准题目重新拍照，保持光线充足、字迹清晰、不要反光"}
		}
		return extract.Result{Pages: []extract.Page{{No: 1, Text: r.Text, LowConfidence: spans(r.LowConfidence)}}, BilledPages: 1}, nil
	case dbq.MaterialsFormatPdf:
		return s.extractPDF(ctx, userID, data, l)
	}
	return extract.Result{}, fmt.Errorf("material: 未知格式 %s", m.Format)
}

func (s *Service) extractPDF(ctx context.Context, userID uint64, data []byte, l Limits) (extract.Result, error) {
	pages, err := s.pdf.ParsePDF(ctx, data)
	if err != nil {
		return extract.Result{}, err
	}
	if len(pages) > l.MaxPagesPerFile {
		return extract.Result{}, &extract.UserError{Msg: fmt.Sprintf("这份 PDF 有 %d 页，超过单个文件 %d 页的上限，请拆分后再上传", len(pages), l.MaxPagesPerFile)}
	}
	scanned, empty := false, true
	for _, p := range pages {
		scanned = scanned || p.Scanned
		empty = empty && p.Text == ""
	}
	if scanned {
		on, err := s.flags.Enabled(ctx, FlagScannedPDF, userID)
		if err != nil {
			return extract.Result{}, err
		}
		if !on {
			return extract.Result{}, &extract.UserError{Msg: "这是扫描版 PDF（由图片做成），暂时还不支持。可以把纸质资料直接拍照导入，或上传文字版 PDF、Word"}
		}
	}
	if empty {
		return extract.Result{}, &extract.UserError{Msg: "PDF 里没有读到文字，请检查文件是否完整"}
	}
	out := extract.Result{Pages: make([]extract.Page, len(pages)), BilledPages: len(pages)}
	for i, p := range pages {
		out.Pages[i] = extract.Page{No: i + 1, Text: p.Text, LowConfidence: spans(p.LowConfidence)}
	}
	return out, nil
}

// read 从 OSS 读原件，最多读单文件上限加 1 MB。
func (s *Service) read(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.oss.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 51<<20))
}

func (s *Service) setStatus(ctx context.Context, userID, id uint64, st dbq.MaterialsStatus, reason string) error {
	return s.q.UpdateMaterialStatus(ctx, dbq.UpdateMaterialStatusParams{
		Status: st, FailReason: sql.NullString{String: reason, Valid: reason != ""}, ID: id, OwnerUserID: userID,
	})
}

func spans(in []ocr.Span) []extract.Span {
	out := make([]extract.Span, len(in))
	for i, s := range in {
		out[i] = extract.Span{Start: s.Start, End: s.End}
	}
	return out
}

func nullJSON[T any](v []T) (dbtypes.NullJSON, error) {
	if len(v) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(v)
	return dbtypes.NullJSON(b), err
}

// IsPermanent 判断取文本的错误是否重试也不会好：文件本身的问题（原因已写进资料记录）、资料不存在或还没上传。
func IsPermanent(err error) bool {
	if _, ok := extract.AsUserError(err); ok {
		return true
	}
	if e, ok := apperr.As(err); ok && e.Kind == apperr.NotFound {
		return true
	}
	return errors.Is(err, ErrNotUploaded)
}

// SetStatus 更新资料状态与面向用户的原因（导入流水线结束时调用）。
func (s *Service) SetStatus(ctx context.Context, userID, id uint64, st dbq.MaterialsStatus, reason string) error {
	return s.setStatus(ctx, userID, id, st, reason)
}
