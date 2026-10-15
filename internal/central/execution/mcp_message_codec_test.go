package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestMCPMessagePersistencePreservesReferenceAndTextBytes(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	fields := facts.Round.Fields()
	tool := mc.Tool{Name: "original", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}
	fields.Input.SchemaVersion = mc.ToolInputSchemaVersion
	digest, err := mc.ToolInputDigest(context.Background(), fields.Messages, []mc.Tool{tool}, mc.ToolChoice{Kind: "required"})
	if err != nil {
		t.Fatal(err)
	}
	fields.Input.Digest = digest
	base := c.LoopRoundFields{Round: fields, Tools: []mc.Tool{tool}, Choice: mc.ToolChoice{Kind: "required"}, Ordinal: 1, TranscriptStart: 1}
	legacy, err := json.Marshal(struct {
		Schema f.Version         `json:"schema_version"`
		Fields c.LoopRoundFields `json:"fields"`
	}{c.LoopRoundSchemaVersion, base})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = cursor.CanonicalJSON(legacy)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := c.NewLoopRound(base)
	if err != nil || !bytes.Equal(initial.CanonicalBytes(), legacy) {
		t.Fatal("existing round bytes changed", err)
	}
	for _, m := range fields.Messages {
		old, _ := json.Marshal(m)
		old, _ = cursor.CanonicalJSON(old)
		got, e := directTextJSON(m)
		if e != nil || !bytes.Equal(old, got) {
			t.Fatal("existing transcript bytes changed", e)
		}
	}
	project, file := newTestID[i.Project](t), newTestID[struct{}](t).String()
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: file})
	if err != nil {
		t.Fatal(err)
	}
	message := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "original", Parts: []mc.ResultPart{{File: &mc.MediaPart{Ref: ref, MediaType: "application/octet-stream"}}}}}}}
	raw, err := directTextJSON(message)
	if err != nil || !bytes.Contains(raw, []byte(file)) {
		t.Fatal("transcript lost file reference", err)
	}
	sameRef := func(v oc.BusinessFileRef) bool {
		d := v.Details()
		return v.Validate() == nil && d.Kind == oc.MCPFile && d.ProjectID == project && d.FileID == file
	}
	var decoded mc.Message
	if err = decodeDirectTextJSON(raw, &decoded); err != nil || !sameRef(decoded.Parts[0].ToolResult.Parts[0].File.Ref) {
		t.Fatal("transcript did not preserve exact reference", err)
	}
	if !sameLoopResultMessage(message, message.Clone()) || !sameLoopResultMessage(message, decoded) {
		t.Fatal("successor rejected the same opaque file reference after clone or persistence")
	}
	for _, change := range []func(*mc.Message){
		func(m *mc.Message) { m.Parts[0].ToolResult.CallID = "different-call" },
		func(m *mc.Message) { m.Parts[0].ToolResult.IsError = true },
		func(m *mc.Message) { m.Parts[0].ToolResult.Parts[0].File.MediaType = "image/png" },
		func(m *mc.Message) {
			other, e := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: newTestID[struct{}](t).String()})
			if e != nil {
				t.Fatal(e)
			}
			m.Parts[0].ToolResult.Parts[0].File.Ref = other
		},
		func(m *mc.Message) {
			other, e := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: newTestID[i.Project](t), FileID: file})
			if e != nil {
				t.Fatal(e)
			}
			m.Parts[0].ToolResult.Parts[0].File.Ref = other
		},
		func(m *mc.Message) { m.Parts[0].ToolResult.Parts[0].File.Ref = oc.BusinessFileRef{} },
	} {
		changed := message.Clone()
		change(&changed)
		if sameLoopResultMessage(message, changed) {
			t.Fatal("successor accepted changed or invalid result provenance")
		}
	}
	base.Round.Messages = append(base.Round.Messages, message)
	round, err := c.NewLoopRound(base)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := c.DecodeLoopRound(round.CanonicalBytes())
	if err != nil || restored.Digest() != round.Digest() {
		t.Fatal("round media codec failed", err)
	}
	got := restored.Fields().Round.Messages[2].Parts[0].ToolResult.Parts[0].File
	if !sameRef(got.Ref) || got.MediaType != "application/octet-stream" {
		t.Fatal("round changed media identity")
	}
	bad := bytes.Replace(raw, []byte(`"kind":"mcp_file"`), []byte(`"kind":"artifact"`), 1)
	if err = decodeDirectTextJSON(bad, &decoded); err == nil {
		t.Fatal("unregistered source persistence admitted")
	}
}
