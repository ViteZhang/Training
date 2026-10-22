package ocr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	pdferrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
)

// PDF 本身的问题：重试也不会好，调用方转成面向用户的提示。
var (
	ErrPDFEncrypted = errors.New("PDF 设置了密码")
	ErrPDFBroken    = errors.New("PDF 文件损坏或格式不对")
)

// scannedMaxRunes：一页的文字层少于这么多个非空白字符、且页面里有图片时，按扫描页处理（只有页码、页眉的扫描页也算）。
const scannedMaxRunes = 20

// scanDPI 是扫描页渲染成图片交给 OCR 的分辨率：A4 约 1650×2340 像素，在 OCR 的尺寸与大小上限内，印刷体足够清晰。
const scanDPI = 200

// PDFium 用内嵌的 PDFium（Chrome 的 PDF 引擎，编译成 WebAssembly 由 wazero 运行，不需要 cgo 和系统库）
// 取 PDF 的文字层；没有文字层的扫描页在 OCRScanned 时渲染成图片交给 Recognizer。
// 替代 ADR 0007 放弃的纯 Go 解析库：中文字体编码按 ToUnicode 还原，效果与浏览器里复制文字一致。
type PDFium struct {
	ocr Recognizer

	once    sync.Once
	pool    pdfium.Pool
	initErr error
}

var _ PDFParser = (*PDFium)(nil)

// NewPDFium 创建解析器；ocr 为空时扫描页不能识别（扫描版 PDF 功能开关打开时会报错）。
// WebAssembly 运行时在第一次解析时才初始化，不拖慢 API 启动。
func NewPDFium(ocr Recognizer) *PDFium { return &PDFium{ocr: ocr} }

// maxInstances 限制同时解析的 PDF 数，控制内存（每个实例单独一份 WebAssembly 内存）。
const maxInstances = 2

func (p *PDFium) init() error {
	p.once.Do(func() {
		p.pool, p.initErr = webassembly.Init(webassembly.Config{
			MinIdle: 0, MaxIdle: 1, MaxTotal: maxInstances,
			// 不挂载宿主机文件系统：只从内存读 PDF。
			FSConfig: wazero.NewFSConfig(),
		})
	})
	return p.initErr
}

func (p *PDFium) ParsePDF(ctx context.Context, data []byte, opt PDFOptions) ([]PDFPage, error) {
	if err := p.init(); err != nil {
		return nil, fmt.Errorf("pdfium: 初始化：%w", err)
	}
	ictx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	inst, err := p.pool.GetInstanceWithContext(ictx)
	if err != nil {
		return nil, fmt.Errorf("pdfium: 取实例：%w", err)
	}
	defer inst.Close()

	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		switch {
		case errors.Is(err, pdferrors.ErrPassword), errors.Is(err, pdferrors.ErrSecurity):
			return nil, ErrPDFEncrypted
		case errors.Is(err, pdferrors.ErrFormat), errors.Is(err, pdferrors.ErrFile):
			return nil, ErrPDFBroken
		}
		return nil, fmt.Errorf("pdfium: 打开文件：%w", err)
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck // 关闭失败不影响结果

	cnt, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("pdfium: 读页数：%w", err)
	}
	pages := make([]PDFPage, cnt.PageCount)
	for i := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := p.page(ctx, inst, doc.Document, i, opt)
		if err != nil {
			return nil, fmt.Errorf("第 %d 页：%w", i+1, err)
		}
		pages[i] = page
	}
	return pages, nil
}

func (p *PDFium) page(ctx context.Context, inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, i int, opt PDFOptions) (PDFPage, error) {
	ref := requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: i}}
	t, err := inst.GetPageText(&requests.GetPageText{Page: ref})
	if err != nil {
		return PDFPage{}, fmt.Errorf("pdfium: 取文字：%w", err)
	}
	text := normalizeText(t.Text)
	if visibleRunes(text) >= scannedMaxRunes {
		return PDFPage{Text: text}, nil
	}
	hasImage, err := pageHasImage(inst, ref)
	if err != nil {
		return PDFPage{}, err
	}
	if !hasImage {
		// 空白页或只有少量文字的页（如章节扉页），不是扫描页。
		return PDFPage{Text: text}, nil
	}
	if !opt.OCRScanned {
		return PDFPage{Scanned: true}, nil
	}
	if p.ocr == nil {
		return PDFPage{}, errors.New("扫描页需要文字识别服务，但 OCR_PROVIDER 未启用")
	}
	img, err := inst.RenderToFile(&requests.RenderToFile{
		RenderPageInDPI: &requests.RenderPageInDPI{Page: ref, DPI: scanDPI},
		OutputFormat:    requests.RenderToFileOutputFormatJPG,
		OutputTarget:    requests.RenderToFileOutputTargetBytes,
		OutputQuality:   85,
		MaxFileSize:     8 << 20,
	})
	if err != nil {
		return PDFPage{}, fmt.Errorf("pdfium: 渲染扫描页：%w", err)
	}
	r, err := p.ocr.Recognize(ctx, Image{Data: *img.ImageBytes})
	if err != nil {
		return PDFPage{}, err
	}
	return PDFPage{Text: r.Text, LowConfidence: r.LowConfidence, Scanned: true}, nil
}

func pageHasImage(inst pdfium.Pdfium, ref requests.Page) (bool, error) {
	n, err := inst.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: ref})
	if err != nil {
		return false, fmt.Errorf("pdfium: 读页面对象：%w", err)
	}
	for j := 0; j < n.Count; j++ {
		o, err := inst.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: ref, Index: j})
		if err != nil {
			return false, fmt.Errorf("pdfium: 读页面对象：%w", err)
		}
		ty, err := inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: o.PageObject})
		if err != nil {
			return false, fmt.Errorf("pdfium: 读页面对象：%w", err)
		}
		if ty.Type == enums.FPDF_PAGEOBJ_IMAGE {
			return true, nil
		}
	}
	return false, nil
}

// normalizeText 统一换行、去掉每行末尾空白与 PDFium 输出的控制字符（软连字符、空字符等）。
func normalizeText(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\x00", "", "\u00ad", "", "\ufffe", "", "\x02", "").Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func visibleRunes(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
