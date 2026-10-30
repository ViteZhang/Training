package ai

import (
	"context"
	"peetraining-server/internal/cloud/asr"
	"strings"
	"testing"
)

func TestVoiceTranscriptionOnlyValidatesOriginalWords(t *testing.T) {
	var calls []Call
	e := NewEngine(Config{UseMock: true, OnCall: func(c Call) { calls = append(calls, c) }})
	text, err := e.TranscribeVoice(context.Background(), 1, &asr.Mock{Text: "考生说：风骨。"}, asr.Audio{})
	if err != nil || text != "考生说：风骨。" {
		t.Fatalf("must preserve original: %q %v", text, err)
	}
	for _, input := range []string{" ", strings.Repeat("字", 20001)} {
		if _, err := e.TranscribeVoice(context.Background(), 1, &asr.Mock{Text: input}, asr.Audio{}); err == nil {
			t.Fatal("must reject invalid transcription")
		}
	}
	if len(calls) != 3 {
		t.Fatalf("must audit each call: %+v", calls)
	}
	for _, c := range calls {
		if c.Capability != "voice_transcribe" {
			t.Fatalf("must not call answer generation: %+v", c)
		}
	}
}
