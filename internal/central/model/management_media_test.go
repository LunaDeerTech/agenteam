package model

import (
	"context"
	"encoding/base64"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
	"time"

	media "github.com/LunaDeerTech/agenteam/internal/central/model/media"
)

func TestManagementChatInputClosedAndBoundToIntent(t *testing.T) {
	valid := `{"text":"read the attachments","attachments":[{"name":"first.txt","media_type":"text/plain","data_base64":"Zmlyc3Q="}]}`
	input, err := decodeManagementMedia(json.RawMessage(valid))
	if err != nil {
		t.Fatal(err)
	}
	_, prepared, intent, err := prepareManagementMedia(context.Background(), []*media.Input{input})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range prepared {
			p.Clear()
		}
	}()
	if intent.Attachments[0].SHA256 != hash([]byte("first")) {
		t.Fatal("intent did not bind original decoded bytes")
	}
	original, _ := json.Marshal(intent)
	for _, change := range []string{"name", "mime", "bytes", "text"} {
		copyInput := *input
		copyInput.Attachments = append([]media.Attachment(nil), input.Attachments...)
		switch change {
		case "name":
			copyInput.Attachments[0].Name = "other.txt"
		case "mime":
			copyInput.Attachments[0].MediaType = "text/markdown"
		case "bytes":
			copyInput.Attachments[0].DataBase64 = base64.StdEncoding.EncodeToString([]byte("other"))
		case "text":
			copyInput.Text = "different"
		}
		_, values, changed, e := prepareManagementMedia(context.Background(), []*media.Input{&copyInput})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(changed)
		for _, p := range values {
			p.Clear()
		}
		if string(raw) == string(original) {
			t.Fatal("intent omitted", change)
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"text":"x","attachments":null}`, `{"text":"x","attachments":[],"url":"https://unexpected.invalid"}`, `{"text":"x","attachments":[{"name":"a.txt","media_type":"text/plain","data_base64":"YQ==","url":"x"}]}`} {
		if _, e := decodeManagementMedia(json.RawMessage(raw)); e == nil {
			t.Fatal("accepted malformed input", raw)
		}
	}
	if _, _, _, e := prepareManagementMedia(context.Background(), []*media.Input{{Text: " ", Attachments: []media.Attachment{}}}); e == nil {
		t.Fatal("accepted explicit empty intent")
	}
}
func TestManagementChatAnswerProjection(t *testing.T) {
	for _, test := range []struct {
		r      ManagementResult
		answer bool
	}{{ManagementResult{Outcome: "failed"}, true}, {ManagementResult{Items: &[]ManagementItem{}}, false}} {
		test.r.ProviderVersion = 1
		test.r.CreatedAt, _ = f.NewInstant(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
		raw, e := json.Marshal(test.r)
		if e != nil {
			t.Fatal(e)
		}
		var fields map[string]json.RawMessage
		if e = json.Unmarshal(raw, &fields); e != nil {
			t.Fatal(e)
		}
		_, present := fields["answer_text"]
		if present != test.answer {
			t.Fatal("answer DTO scope")
		}
		if present && string(fields["answer_text"]) != "null" {
			t.Fatal("unsuccessful answer must be null")
		}
	}
}
