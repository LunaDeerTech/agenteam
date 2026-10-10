package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func runtimeTextMessages(text string) []mc.Message {
	return []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: text}}}}}
}

func TestRuntimeTextInputCanonicalBytes(t *testing.T) {
	messages := runtimeTextMessages("中\n<&")
	got, err := TextInputDigest(messages)
	if err != nil {
		t.Fatal(err)
	}
	// Hand-authored protocol bytes; this does not call the production encoder.
	raw := `{"format":1,"messages":[{"role":"user","parts":[{"text":{"text":"中\n\u003c\u0026"}}]}],"tool_choice":{"kind":"none"},"response_format":{"kind":"text"}}`
	sum := sha256.Sum256([]byte(raw))
	if got.String() != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("input projection changed")
	}
	for _, text := range []string{"中\r\n<&", "中\n<& ", "中\n<&\x00"} {
		other, err := TextInputDigest(runtimeTextMessages(text))
		if err != nil || other == got {
			t.Fatal("input bytes were normalized")
		}
	}
	if messages[0].Parts[0].Text.Text != "中\n<&" {
		t.Fatal("input was mutated")
	}
}

func TestRuntimeTextInputRejectsBeforeLargeClone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input []mc.Message
		code  f.Code
	}{
		{"empty", nil, f.InvalidArgument},
		{"utf8", runtimeTextMessages("\xff"), f.InvalidArgument},
		{"escape-budget", runtimeTextMessages(strings.Repeat("\x00", runtimeTextLimit/6)), f.PayloadTooLarge},
		{"role", []mc.Message{{Role: "tool", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "value"}}}}}, f.CapabilityUnsupported},
		{"parts", []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "a"}}, {Text: &mc.TextPart{Text: "b"}}}}}, f.CapabilityUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TextInputDigest(tc.input)
			var fault *f.Fault
			if got != "" || !errors.As(err, &fault) || fault.Code != tc.code {
				t.Fatal("invalid input returned a digest or wrong safe code")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := runtimeInputDigest(ctx, runtimeTextMessages("cancelled-canary"))
	if got != "" || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "canary") {
		t.Fatal("cancellation lost its cause or leaked input")
	}
}
