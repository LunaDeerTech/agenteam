package execution

import (
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"testing"
)

func TestMeetingFinalResultRequiresCompleteOriginalProjection(t *testing.T) {
	run := runtimeCandidate(t, "literal <public> 中文")
	snapshot, err := finalRuntimeSnapshot(run.round, run.terminal)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := runtimeSnapshotBytes(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	round := run.round.Fields()
	started := round.CreatedAt
	completed := run.terminal.at
	summary := ec.Summary{ID: round.ExecutionID, Status: ec.Succeeded, Version: run.terminal.version, SnapshotID: &round.SnapshotID, StartedAt: &started, CompletedAt: &completed}
	read := func(s ec.Summary, b []byte, digest string) (string, error) {
		return meetingFinalText(s, b, digest, round.SnapshotID.String(), round.ID.String(), "succeeded", int64(run.terminal.version), completed.Time())
	}
	text, err := read(summary, raw, string(ec.TriggerInputDigest(raw)))
	if err != nil || text != "literal <public> 中文\n末尾" {
		t.Fatal("lost complete published final text", err)
	}
	changed := summary
	changed.Version++
	if _, err = read(changed, raw, string(ec.TriggerInputDigest(raw))); err == nil {
		t.Fatal("accepted terminal version drift")
	}
	if _, err = read(summary, raw[:len(raw)-1], string(ec.TriggerInputDigest(raw))); err == nil {
		t.Fatal("accepted truncated published snapshot")
	}
}
