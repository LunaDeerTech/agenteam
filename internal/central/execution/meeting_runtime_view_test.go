package execution

import (
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func runtimeMeetingID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMeetingRuntimeDecodesOnlyOriginalCompleteDurableSnapshot(t *testing.T) {
	run := runtimeCandidate(t, "literal public result")
	snapshot, err := finalRuntimeSnapshot(run.round, run.terminal)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := runtimeSnapshotBytes(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	round := run.round.Fields()
	started, completed := round.CreatedAt, run.terminal.at
	text := snapshot.Items[0].Data.Content
	value := ec.MeetingExecutionResult{Summary: ec.Summary{ID: round.ExecutionID, ProjectID: runtimeMeetingID[i.Project](t), AgentID: runtimeMeetingID[i.Agent](t), Trigger: ec.Trigger{Kind: "meeting", MeetingID: runtimeMeetingID[struct{}](t).String(), TurnID: runtimeMeetingID[struct{}](t).String(), ContributionID: runtimeMeetingID[struct{}](t).String(), ParticipantID: runtimeMeetingID[struct{}](t).String()}, Purpose: "meeting/response", Status: ec.Succeeded, Version: run.terminal.version, SnapshotID: &round.SnapshotID, CreatedAt: started, StartedAt: &started, CompletedAt: &completed}, FinalText: &text}
	read := func(v ec.MeetingExecutionResult, body []byte, digest, originalRound string) (*ec.RuntimeSnapshot, error) {
		return decodeMeetingRuntimeSnapshot(v, body, digest, round.SnapshotID.String(), originalRound, "succeeded", int64(run.terminal.version), completed.Time())
	}
	got, err := read(value, raw, string(ec.TriggerInputDigest(raw)), round.ID.String())
	if err != nil || got.Items[0].Data.Content != text || got.Generation != round.SnapshotID {
		t.Fatal("original complete public runtime was lost", err)
	}
	if _, err := read(value, raw, string(ec.TriggerInputDigest(raw)), runtimeMeetingID[ec.Round](t).String()); err == nil {
		t.Fatal("foreign round was accepted")
	}
	if _, err := read(value, raw[:len(raw)-1], string(ec.TriggerInputDigest(raw)), round.ID.String()); err == nil {
		t.Fatal("truncated original snapshot was accepted")
	}
	changed := value
	changed.Summary.Version++
	if _, err := read(changed, raw, string(ec.TriggerInputDigest(raw)), round.ID.String()); err == nil {
		t.Fatal("terminal version drift was accepted")
	}
	noncanonical := append(append([]byte(nil), raw...), '\n')
	if _, err := read(value, noncanonical, string(ec.TriggerInputDigest(noncanonical)), round.ID.String()); err == nil {
		t.Fatal("digest-matched but noncanonical snapshot was accepted")
	}
}
