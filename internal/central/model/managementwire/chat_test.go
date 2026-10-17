package managementwire

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/media"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func managementChatPDF() []byte {
	stream := "BT /F1 12 Tf 20 100 Td (Owned PDF) Tj ET\n"
	objects := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Count 1/Kids[3 0 R]>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>\nstream\n%sendstream", len(stream), stream),
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func managementChatAttachments(t *testing.T) []*media.Prepared {
	t.Helper()
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	var out []*media.Prepared
	for _, tc := range []struct {
		name, mime string
		raw        []byte
	}{
		{"pixel.png", "image/png", png},
		{"sample.pdf", "application/pdf", managementChatPDF()},
		{"readme.txt", "text/plain", []byte("owned attachment text")},
	} {
		p, err := media.Prepare(context.Background(), tc.name, tc.mime, tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Clear)
		out = append(out, p)
	}
	return out
}

func managementChatReply(text, finish string) string {
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": text}, "finish_reason": finish}}})
	return string(body)
}

func TestManagementChatAnswerUTF8AndLimit(t *testing.T) {
	for _, tc := range []struct {
		text, finish string
		truncated    bool
	}{
		{"OK", "stop", false},
		{"incomplete", "length", true},
		{strings.Repeat("界", 2800), "stop", true},
	} {
		result := parse(context.Background(), Request{Action: "test", Protocol: chatProtocol}, []byte(managementChatReply(tc.text, tc.finish)))
		if result.Reason != "" || result.Answer == "" || len(result.Answer) > media.MaxAnswerBytes || !utf8.ValidString(result.Answer) || result.Truncated != tc.truncated || !strings.HasPrefix(tc.text, result.Answer) {
			t.Fatal("answer lost a complete UTF-8 prefix or truncation state")
		}
	}
}

func TestManagementChatNativeOwnedTLS(t *testing.T) {
	nonce := rand.Text()
	secret := "owned-chat-" + nonce
	material, err := sc.NewSecretMaterial([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	var requests atomic.Int32
	var reply atomic.Value
	reply.Store(validChat)
	bodies := make(chan []byte, 4)
	server, trust := ownedManagementTLS(t, nonce, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v1%2Fowned/chat/completions" || r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("saved native endpoint or exact-origin credential changed")
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, media.MaxRequestBytes+1))
		if err != nil || len(body) > media.MaxRequestBytes {
			t.Error("owned request body unavailable")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply.Load().(string))
	}))
	transport, call, endpoint := wireFixture(t, server, true)
	transport.Trust = trust
	input := Request{Action: "test", Protocol: chatProtocol, Endpoint: endpoint, Model: "custom-model", Chat: &ChatInput{Text: strings.Repeat("describe ", 700), Attachments: managementChatAttachments(t), Capabilities: mc.Capabilities{InputModalities: []string{"text", "image", "file"}}}}
	result, err := transport.Run(context.Background(), input, &material, call)
	if err != nil || result.Reason != "" || result.Answer != "OK" || result.Truncated || result.fullAnswer != "" {
		t.Fatal("owned attachment call did not return its safe answer", err)
	}
	body := <-bodies
	var request struct {
		Messages []struct {
			Role    string
			Content json.RawMessage
		}
		Stream bool
	}
	if json.Unmarshal(body, &request) != nil || request.Stream || len(request.Messages) != 1 || request.Messages[0].Role != "user" || len(body) <= 4096 {
		t.Fatal("management test did not use the bounded custom input")
	}
	var parts []media.ContentPart
	if json.Unmarshal(request.Messages[0].Content, &parts) != nil || len(parts) != 4 || parts[0].Text != input.Chat.Text || parts[1].ImageURL == nil || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") || parts[2].File == nil || parts[2].File.Filename != "sample.pdf" || !strings.HasPrefix(parts[2].File.FileData, "data:application/pdf;base64,") || !strings.Contains(parts[3].Text, "owned attachment text") {
		t.Fatal("System test diverged from the runtime native content encoder")
	}
	clear(body)
	input.Chat = &ChatInput{Text: "plain custom prompt", Capabilities: mc.Capabilities{InputModalities: []string{"text"}}}
	result, err = transport.Run(context.Background(), input, &material, call)
	if err != nil || result.Reason != "" || result.Answer != "OK" {
		t.Fatal("plain custom call failed", err)
	}
	body = <-bodies
	if json.Unmarshal(body, &request) != nil || string(request.Messages[0].Content) != `"plain custom prompt"` {
		t.Fatal("plain custom prompt lost its string wire shape")
	}
	clear(body)
	// Check the complete decoded answer before clipping: a reflected credential
	// spanning the visible limit must not leak a partial credential prefix.
	reply.Store(managementChatReply(strings.Repeat("a", media.MaxAnswerBytes-4)+secret, "stop"))
	result, err = transport.Run(context.Background(), input, &material, call)
	if err != nil || result.Reason != "invalid_response" || result.Answer != "" || result.Truncated || result.fullAnswer != "" {
		t.Fatal("credential reflection escaped answer filtering")
	}
	clear(<-bodies)
	before := requests.Load()
	for _, bad := range []Request{
		{Action: "test", Protocol: embedProtocol, Endpoint: endpoint, Model: "custom-model", Chat: input.Chat},
		{Action: "test", Protocol: chatProtocol, Endpoint: endpoint, Model: "custom-model", Chat: &ChatInput{Text: "read", Attachments: managementChatAttachments(t), Capabilities: mc.Capabilities{InputModalities: []string{"text"}}}},
	} {
		if result, err := transport.Run(context.Background(), bad, &material, call); err == nil || result.Answer != "" || requests.Load() != before {
			t.Fatal("invalid custom input reached outbound")
		}
	}
}

func TestManagementChatBudgets(t *testing.T) {
	input := Request{Action: "test", Protocol: chatProtocol, Chat: &ChatInput{Text: "plain"}}
	if Timeout(input) != 20*time.Second || probeLimits(input, testLimit).ResponseHeaders != 15*time.Second {
		t.Fatal("plain Chat budget changed")
	}
	input.Chat.Attachments = managementChatAttachments(t)
	limits := probeLimits(input, testLimit)
	if Timeout(input) != 120*time.Second || limits.ResponseHeaders != 120*time.Second || limits.ReadIdle != 30*time.Second || limits.RequestBodyBytes != media.MaxRequestBytes {
		t.Fatal("attachment Chat did not receive its finite media budget")
	}
}
