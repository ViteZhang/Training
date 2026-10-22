package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/config"
)

// CheckResult 是一项外部服务的自检结果。
type CheckResult struct {
	Name     string
	Provider string
	// Skipped 为 true 表示这项没有实际调用（关闭、mock 或调用会产生明显费用）。
	Skipped bool
	Err     error
	Note    string
}

// checkKey 是 OSS 自检用的对象键，放在用户前缀 u/ 之外，不会被注销清理误删或与用户文件冲突。
const checkKey = "selfcheck/cloudcheck.txt"

// minimalPDF 是一页空白 PDF，用来确认 PDFium 能在本机初始化与解析（不调用外部服务）。
const minimalPDF = "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n"

// Check 逐项试调已配置的外部服务，供运维在服务器上执行 `api cloudcheck` 排查「上传失败」「识别服务暂时不可用」。
// 只写入并删除一个自检对象、送审一句固定文字、让模型返回一个很短的 JSON，费用可以忽略；文字识别按次计费，默认不调用。
func Check(ctx context.Context, cfg *config.Config, c *Clients, withOCR bool) []CheckResult {
	var out []CheckResult
	run := func(name, provider string, skip bool, note string, f func(ctx context.Context) error) {
		r := CheckResult{Name: name, Provider: provider, Note: note}
		if skip {
			r.Skipped = true
		} else {
			cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			r.Err = f(cctx)
			cancel()
		}
		out = append(out, r)
	}
	off := func(p string) bool { return p == config.ProviderMock || p == config.ProviderDisabled }

	run("文件存储 OSS", cfg.OSS.Provider, off(cfg.OSS.Provider), "写入、预签名直传、读取、删除一个自检对象", func(ctx context.Context) error {
		return checkOSS(ctx, c)
	})
	run("内容安全", cfg.Moderation.Provider, off(cfg.Moderation.Provider), "送审一句固定文字", func(ctx context.Context) error {
		v, err := c.Moderation.CheckText(ctx, "考研专业课资料导入自检：简述社会契约论的基本观点。")
		if err != nil {
			return err
		}
		if !v.Pass {
			return fmt.Errorf("正常文字被判为不通过（%s），请在内容安全控制台检查规则或调高 MODERATION_REJECT_LEVEL", v.Reason)
		}
		return nil
	})
	pdfProvider := cfg.OCR.PDF()
	run("PDF 解析", pdfProvider, off(pdfProvider), "本机解析一页空白 PDF", func(ctx context.Context) error {
		pages, err := c.PDF.ParsePDF(ctx, []byte(minimalPDF), ocr.PDFOptions{})
		if err == nil && len(pages) != 1 {
			err = fmt.Errorf("应解析出 1 页，实际 %d 页", len(pages))
		}
		return err
	})
	ocrNote := "按次计费，加 --ocr 才实际调用"
	if withOCR {
		ocrNote = "识别一张空白图片（返回空文字也算成功）"
	}
	run("文字识别 OCR", cfg.OCR.Provider, off(cfg.OCR.Provider) || !withOCR, ocrNote, func(ctx context.Context) error {
		_, err := c.OCR.Recognize(ctx, ocr.Image{Data: blankPNG()})
		if errors.Is(err, ocr.ErrBadImage) {
			// 能返回「图片里没有文字」之类的业务错误，说明鉴权与开通都没问题。
			return nil
		}
		return err
	})

	model := cfg.AI.ModelStrong
	if cfg.AI.Provider == "relay" {
		model = cfg.AI.RelayModelStrong
	}
	run("AI 主用平台", cfg.AI.Provider, cfg.AI.Provider == config.ProviderMock, "模型 "+model+"（后台按能力改过模型的，以后台为准）", func(ctx context.Context) error {
		return checkAI(ctx, c.AI, model)
	})
	if c.AIFallback != nil {
		run("AI 备用平台（百炼）", "bailian", false, "模型 "+cfg.AI.ModelStrong, func(ctx context.Context) error {
			return checkAI(ctx, c.AIFallback, cfg.AI.ModelStrong)
		})
	}
	return out
}

func checkOSS(ctx context.Context, c *Clients) error {
	data := []byte("cloudcheck " + time.Now().UTC().Format(time.RFC3339))
	if err := c.OSS.Put(ctx, checkKey, data, "text/plain"); err != nil {
		return fmt.Errorf("服务端写入：%w", err)
	}
	defer c.OSS.Delete(context.WithoutCancel(ctx), checkKey) //nolint:errcheck // 自检对象删不掉不影响业务
	rc, err := c.OSS.Get(ctx, checkKey)
	if err != nil {
		return fmt.Errorf("服务端读取：%w", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return fmt.Errorf("服务端读取：%w", err)
	}
	if !bytes.Equal(got, data) {
		return errors.New("服务端读取：内容与写入的不一致")
	}
	// 按 App 的方式用预签名地址直传一次：验证公网地址、签名与请求头。
	p, err := c.OSS.PresignPut(ctx, checkKey, "text/plain", int64(len(data)), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("生成上传地址：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("预签名直传（App 上传走这条路）：%s", strings.SplitN(err.Error(), "?", 2)[0])
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("预签名直传（App 上传走这条路）：HTTP %d %s", resp.StatusCode, ossErrorCode(body))
	}
	info, err := c.OSS.Head(ctx, checkKey)
	if err != nil {
		return fmt.Errorf("确认上传：%w", err)
	}
	if !info.Exists || info.Size != int64(len(data)) {
		return fmt.Errorf("确认上传：对象不存在或大小不对（%+v）", info)
	}
	return nil
}

// ossErrorCode 从 OSS 的 XML 错误里取出 Code 与 Message，不打印整段响应（里面有请求签名信息）。
func ossErrorCode(body []byte) string {
	s := string(body)
	pick := func(tag string) string {
		i := strings.Index(s, "<"+tag+">")
		j := strings.Index(s, "</"+tag+">")
		if i < 0 || j < i {
			return ""
		}
		return s[i+len(tag)+2 : j]
	}
	return strings.TrimSpace(pick("Code") + " " + pick("Message"))
}

func checkAI(ctx context.Context, client ai.Client, model string) error {
	resp, err := client.Complete(ctx, ai.Request{
		Capability: "cloudcheck", Model: model, JSONMode: true,
		Messages: []ai.Message{{Role: "system", Content: "只输出 JSON。"}, {Role: "user", Content: `返回 {"ok": true}`}},
	})
	if err != nil {
		return err
	}
	if !strings.Contains(resp.Content, "ok") {
		return fmt.Errorf("返回内容不符合预期（%d 字）", len([]rune(resp.Content)))
	}
	return nil
}

// blankPNG 是一张 64×64 的白色 PNG。
func blankPNG() []byte {
	var b bytes.Buffer
	_ = encodeBlankPNG(&b)
	return b.Bytes()
}

func encodeBlankPNG(w io.Writer) error {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	return png.Encode(w, img)
}
