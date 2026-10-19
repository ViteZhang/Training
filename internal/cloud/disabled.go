package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"peetraining-server/internal/cloud/asr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/cloud/sms"
)

// ErrDisabled 表示运维显式关闭了该能力。关闭时不发送短信、不存储文件，也不放行内容审核。
var ErrDisabled = errors.New("该云服务尚未启用")

type disabledService string

var (
	_ sms.Sender         = disabledService("")
	_ oss.Store          = disabledService("")
	_ ocr.Recognizer     = disabledService("")
	_ ocr.PDFParser      = disabledService("")
	_ asr.Transcriber    = disabledService("")
	_ moderation.Checker = disabledService("")
)

func (d disabledService) err() error { return fmt.Errorf("%s：%w", d, ErrDisabled) }

func (d disabledService) SendCode(context.Context, string, string) error { return d.err() }

func (d disabledService) Recognize(context.Context, ocr.Image) (ocr.Result, error) {
	return ocr.Result{}, d.err()
}

func (d disabledService) ParsePDF(context.Context, []byte) ([]ocr.PDFPage, error) {
	return nil, d.err()
}

func (d disabledService) Transcribe(context.Context, asr.Audio) (string, error) {
	return "", d.err()
}

func (d disabledService) CheckText(context.Context, string) (moderation.Verdict, error) {
	return moderation.Verdict{}, d.err()
}

func (d disabledService) CheckImage(context.Context, string) (moderation.Verdict, error) {
	return moderation.Verdict{}, d.err()
}

func (d disabledService) PresignPut(context.Context, string, string, int64, time.Duration) (oss.Presigned, error) {
	return oss.Presigned{}, d.err()
}

func (d disabledService) PresignGet(context.Context, string, string, time.Duration) (oss.Presigned, error) {
	return oss.Presigned{}, d.err()
}

func (d disabledService) Head(context.Context, string) (oss.ObjectInfo, error) {
	return oss.ObjectInfo{}, d.err()
}

func (d disabledService) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, d.err()
}

func (d disabledService) Put(context.Context, string, []byte, string) error { return d.err() }
func (d disabledService) Delete(context.Context, ...string) error           { return d.err() }

func (d disabledService) DeletePrefix(context.Context, string) (int, error) {
	return 0, d.err()
}
