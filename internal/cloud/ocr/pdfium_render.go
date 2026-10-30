package ocr

import (
	"context"
	"errors"
	"fmt"
	"time"

	pdferrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/requests"
)

var ErrPDFTooManyPages = errors.New("PDF 超过页数限制")

// RenderAllPages 专供官方教材视觉识别：不访问文字层，不调用 Recognizer。
// callback 收到真实总页数和从 1 开始的页序；每次只保留一页图像，避免整册 PDF 图片堆积。
func (p *PDFium) RenderAllPages(ctx context.Context, data []byte, maxPages int, callback func(total, page int, jpeg []byte) error) error {
	if err := p.init(); err != nil {
		return err
	}
	ictx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	inst, err := p.pool.GetInstanceWithContext(ictx)
	if err != nil {
		return err
	}
	defer inst.Close()
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		if errors.Is(err, pdferrors.ErrPassword) || errors.Is(err, pdferrors.ErrSecurity) {
			return ErrPDFEncrypted
		}
		return ErrPDFBroken
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck // 释放文档资源
	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return err
	}
	if count.PageCount < 1 {
		return ErrPDFBroken
	}
	if count.PageCount > maxPages {
		return ErrPDFTooManyPages
	}
	for i := 0; i < count.PageCount; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		img, err := inst.RenderToFile(&requests.RenderToFile{RenderPageInDPI: &requests.RenderPageInDPI{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}}, DPI: scanDPI}, OutputFormat: requests.RenderToFileOutputFormatJPG, OutputTarget: requests.RenderToFileOutputTargetBytes, OutputQuality: 85, MaxFileSize: 8 << 20})
		if err != nil {
			return fmt.Errorf("render page %d: %w", i+1, err)
		}
		if img.ImageBytes == nil {
			return ErrPDFBroken
		}
		if err := callback(count.PageCount, i+1, *img.ImageBytes); err != nil {
			return err
		}
	}
	return nil
}
