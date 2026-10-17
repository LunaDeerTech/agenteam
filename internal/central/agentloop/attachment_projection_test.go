package agentloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestTaskAttachmentProjectionUsesFrozenSource(t *testing.T) {
	fields := requestFields(t, "task/work")
	source := ec.CapturedArtifactAttachment{ProjectID: fields.Request.Launch.ProjectID, ArtifactID: requestID[ac.ArtifactEntity](t, 850), FileID: requestID[ac.File](t, 851), ArtifactVersion: 1, ObjectID: requestID[oc.StoredObject](t, 852), ObjectVersion: 1, SHA256: f.Digest("sha256:" + strings.Repeat("a", 64)), MediaType: "image/png", ByteSize: 8, Filename: "input.png"}
	var envelope map[string]json.RawMessage
	requestOK(t, json.Unmarshal(fields.Trigger.Data(), &envelope))
	var input map[string]json.RawMessage
	requestOK(t, json.Unmarshal(envelope["input"], &input))
	var task map[string]json.RawMessage
	requestOK(t, json.Unmarshal(input["task"], &task))
	task["attachments"], _ = json.Marshal([]ac.ArtifactID{source.ArtifactID})
	input["task"], _ = json.Marshal(task)
	input["attachments"], _ = json.Marshal([]ec.CapturedArtifactAttachment{source})
	envelope["input"], _ = json.Marshal(input)
	raw, err := json.Marshal(envelope)
	requestOK(t, err)
	ref := fields.Trigger.Ref()
	ref.Digest = ec.TriggerInputDigest(raw)
	fields.Trigger, err = ec.NewCapturedTriggerInput(ref, raw)
	requestOK(t, err)
	captured := requestContext(t, fields)
	messages, err := projectMessages(context.Background(), captured)
	requestOK(t, err)
	if len(messages) != 2 || len(messages[1].Parts) != 3 || messages[1].Parts[2].Image == nil || !source.MatchesReference(messages[1].Parts[2].Image.Ref) {
		t.Fatal("Task attachment remained text or changed business identity")
	}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Text != nil && (strings.Contains(part.Text.Text, source.ObjectID.String()) || strings.Contains(part.Text.Text, source.SHA256.String())) {
				t.Fatal("backend source metadata became model conversation text")
			}
		}
	}
}
