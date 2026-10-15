package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type runtimeProjectionPublications struct {
	projects   []i.ProjectID
	executions []i.ExecutionID
}

func (*runtimeProjectionPublications) InvalidateMeeting(i.ProjectID, string) {}
func (p *runtimeProjectionPublications) InvalidateExecution(project i.ProjectID, execution i.ExecutionID) {
	p.projects = append(p.projects, project)
	p.executions = append(p.executions, execution)
}

func TestRuntimeProjectionCheckpointPublishesOnlyKnownCommit(t *testing.T) {
	for _, state := range []f.CommitState{f.Committed, f.NotCommitted, f.Unknown} {
		t.Run(string(state), func(t *testing.T) {
			p := &runtimeProjectionPublications{}
			s := &directTextState{deps: DirectTextDependencies{Realtime: p}}
			e, _ := f.NewID[i.Execution]()
			project, _ := f.NewID[i.Project]()
			run := &directTextCall{request: ec.PreparationRequest{ExecutionID: e}, toolFacts: &toolCallCandidate{version: 3}}
			run.request.Launch.ProjectID = project
			result := f.CommitResult{}
			if state == f.Committed {
				result = f.CommittedResult()
			} else if state == f.NotCommitted {
				result = f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
			}
			_, err := s.completeToolCallCheckpoint(run, result, false, nil)
			if state == f.Committed {
				if err != nil || len(p.executions) != 1 || p.executions[0] != e || p.projects[0] != project || !run.toolPending {
					t.Fatal("original committed checkpoint did not publish its own identity")
				}
			} else if err == nil || len(p.executions) != 0 {
				t.Fatal("unconfirmed write published a false notification")
			}
		})
	}
}

func TestRuntimeProjectionRequiresOriginalMeetingReader(t *testing.T) {
	var reader *MeetingExecutionReader
	if _, err := reader.ReadMeetingRuntimeProjectionInTx(context.Background(), f.Tx{}, i.ProjectID{}, ec.AssociatedMeeting{}); err == nil {
		t.Fatal("public tuple replaced the original source authority")
	}
}

func projectionFixture(t *testing.T) (*runtimeProjectionBuilder, string, string) {
	t.Helper()
	e, _ := f.NewID[i.Execution]()
	p, _ := f.NewID[i.Project]()
	s, _ := f.NewID[ec.Snapshot]()
	r, _ := f.NewID[ec.Round]()
	b, err := newRuntimeProjection(ec.MeetingExecutionResult{Summary: ec.Summary{ID: e, ProjectID: p, Version: 3, Status: ec.Running, SnapshotID: &s}})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.accept(1, nil, "input", nil, "", nil); err != nil {
		t.Fatal(err)
	}
	return b, r.String(), s.String()
}

func TestRuntimeProjectionUsesCommittedPublicPartsAndOriginalToolPair(t *testing.T) {
	b, round, snapshot := projectionFixture(t)
	message := mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "public text"}}, {ToolCall: &mc.ToolCallPart{ID: "call-original", Name: "memory.recall", Arguments: json.RawMessage(`{"private":"do not project arguments"}`)}}, {Metadata: &mc.ProviderMetadataPart{AdapterID: "adapter", Version: "1", OpaqueRef: "private-reference"}}}}
	raw, err := directTextJSON(message)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.accept(2, &round, "assistant", raw, string(ec.TriggerInputDigest(raw)), &snapshot); err != nil {
		t.Fatal(err)
	}
	result := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "call-original", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: strings.Repeat("中", 4000)}}}}}}}
	raw, err = directTextJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.accept(3, &round, "tool_result", raw, string(ec.TriggerInputDigest(raw)), &snapshot); err != nil {
		t.Fatal(err)
	}
	view, err := b.finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 3 || view.TranscriptThrough != 3 || !view.Items[2].Tool.ResultAvailable || view.Items[2].Revision != 2 || view.Items[2].Tool.ResultSequence != 3 || !view.Items[2].Tool.TextTruncated || view.Items[2].Tool.ResultTextBytes != 12000 {
		t.Fatal("lost canonical sequence or bounded result provenance")
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do not project arguments") || strings.Contains(string(encoded), "private-reference") {
		t.Fatal("private arguments or provider metadata escaped")
	}
	if b.accept(4, &round, "tool_result", raw, string(ec.TriggerInputDigest(raw)), &snapshot) == nil {
		t.Fatal("repeated original result accepted")
	}
}

func TestRuntimeProjectionRejectsUnboundOrForeignTranscript(t *testing.T) {
	for _, change := range []string{"digest", "snapshot", "role", "gap", "unknown-result"} {
		t.Run(change, func(t *testing.T) {
			b, round, snapshot := projectionFixture(t)
			m := mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "public"}}}}
			sequence := int64(2)
			kind := "assistant"
			if change == "role" {
				m.Role = "system"
			}
			if change == "gap" {
				sequence = 3
			}
			if change == "snapshot" {
				s, _ := f.NewID[ec.Snapshot]()
				snapshot = s.String()
			}
			if change == "unknown-result" {
				kind = "tool_result"
				m = mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "unknown", Parts: []mc.ResultPart{}}}}}
			}
			raw, err := directTextJSON(m)
			if err != nil {
				t.Fatal(err)
			}
			digest := string(ec.TriggerInputDigest(raw))
			if change == "digest" {
				digest = "sha256:" + strings.Repeat("0", 64)
			}
			if b.accept(sequence, &round, kind, raw, digest, &snapshot) == nil {
				t.Fatal("foreign or noncanonical source accepted")
			}
		})
	}
}
