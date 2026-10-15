package contract

import (
	"fmt"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func meetingRuntimeID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func meetingRuntimeProjection(t *testing.T) MeetingRuntimeResult {
	t.Helper()
	at, _ := f.NewInstant(time.Unix(1, 0).UTC())
	done, _ := f.NewInstant(time.Unix(2, 0).UTC())
	snapshot, round := meetingRuntimeID[Snapshot](t, 10), meetingRuntimeID[Round](t, 11)
	text := "Public result <literal>"
	e := Summary{ID: meetingRuntimeID[i.Execution](t, 1), ProjectID: meetingRuntimeID[i.Project](t, 2), AgentID: meetingRuntimeID[i.Agent](t, 3), Trigger: Trigger{Kind: "meeting", MeetingID: meetingRuntimeID[struct{}](t, 4).String(), TurnID: meetingRuntimeID[struct{}](t, 5).String(), ContributionID: meetingRuntimeID[struct{}](t, 6).String(), ParticipantID: meetingRuntimeID[struct{}](t, 7).String()}, Purpose: "meeting/response", Status: Succeeded, Version: 4, CreatedAt: at, StartedAt: &at, CompletedAt: &done, SnapshotID: &snapshot}
	return MeetingRuntimeResult{Execution: MeetingExecutionResult{Summary: e, FinalText: &text}, Availability: RuntimeAvailable, Snapshot: &RuntimeSnapshot{Schema: DurableFinalTextV2, Generation: snapshot, IncludedUpdateProgress: 1, ExecutionVersion: 4, Items: []RuntimeTextItem{{ID: round, ExecutionID: e.ID, Seq: 1, Type: "text", Status: "completed", Revision: 1, StartedAt: at, UpdatedAt: done, CompletedAt: done, SourceReference: RuntimeRoundReference{Kind: "execution_round", ID: round}, Data: RuntimeTextData{Content: text}}}}}
}

func TestMeetingRuntimeProjectionPreservesOriginalTerminalIdentity(t *testing.T) {
	if err := meetingRuntimeProjection(t).ValidateProjection(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*MeetingRuntimeResult){
		func(v *MeetingRuntimeResult) { v.Execution.Summary.Trigger.Kind = "task" },
		func(v *MeetingRuntimeResult) { v.Execution.Summary.Status = Running },
		func(v *MeetingRuntimeResult) { v.Execution.Summary.Version++ },
		func(v *MeetingRuntimeResult) { v.Snapshot.Generation = meetingRuntimeID[Snapshot](t, 30) },
		func(v *MeetingRuntimeResult) { v.Snapshot.IncludedUpdateProgress++ },
		func(v *MeetingRuntimeResult) { v.Snapshot.Items[0].ExecutionID = meetingRuntimeID[i.Execution](t, 31) },
		func(v *MeetingRuntimeResult) { v.Snapshot.Items[0].Data.Content = "different public result" },
		func(v *MeetingRuntimeResult) { v.Availability = RuntimePending },
		func(v *MeetingRuntimeResult) { v.Availability, v.Snapshot = RuntimeNotRecorded, nil },
	} {
		value := meetingRuntimeProjection(t)
		change(&value)
		if value.ValidateProjection() == nil {
			t.Fatal("runtime accepted a foreign, partial or falsely live projection")
		}
	}
}

func TestMeetingRuntimePendingAndUnrecordedDoNotClaimContent(t *testing.T) {
	v := meetingRuntimeProjection(t)
	v.Execution.Summary.Status, v.Execution.Summary.CompletedAt = Running, nil
	v.Execution.FinalText, v.Snapshot, v.Availability = nil, nil, RuntimePending
	if err := v.ValidateProjection(); err != nil {
		t.Fatal(err)
	}
	v.Availability = RuntimeAvailable
	if v.ValidateProjection() == nil {
		t.Fatal("pending runtime advertised an active or terminal snapshot")
	}
	v = meetingRuntimeProjection(t)
	v.Execution.Summary.Status = Cancelled
	v.Execution.FinalText, v.Snapshot, v.Availability = nil, nil, RuntimeNotRecorded
	if err := v.ValidateProjection(); err != nil {
		t.Fatal(err)
	}
}
