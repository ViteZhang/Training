package ai

import (
	"context"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/asr"
)

// TranscribeVoice 只转写原话；模型/接口与生成类能力分离，不用答案或采分点补写考生原话。
func (e *Engine) TranscribeVoice(ctx context.Context, userID uint64, transcriber asr.Transcriber, audio asr.Audio) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 170*time.Second)
	defer cancel()
	start := time.Now()
	model := "asr"
	if named, ok := transcriber.(interface{ ModelName() string }); ok {
		model = named.ModelName()
	}
	text, err := transcriber.Transcribe(ctx, audio)
	if err == nil && (strings.TrimSpace(text) == "" || len([]rune(text)) > 20000) {
		err = ErrInvalidOutput
	}
	kind := ""
	if err != nil {
		kind = "asr_failed"
	}
	e.record(ctx, Def{Name: "voice_transcribe"}, Meta{Model: model, Version: "v1"}, userID, cloudai.Response{}, start, kind, false)
	if err != nil {
		return "", apperr.New(apperr.AIFailed, "语音识别失败，录音已保存，请重试")
	}
	return text, nil
}
