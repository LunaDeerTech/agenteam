package execution

import (
	"context"
	"errors"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// These are controlled owner/transaction and canonical-input checks. They do
// not claim a physical ProcessGuard death or a committed retirement in PG.
type meetingRetirementTestAuthority struct {
	store *executionCallStore
	err   error
	calls int
}

func (a *meetingRetirementTestAuthority) RequireBindingInTx(_ context.Context, tx f.Tx, store any) error {
	if store != a.store {
		return fault(f.Forbidden)
	}
	_, err := a.store.InTx(tx)
	return err
}
func (a *meetingRetirementTestAuthority) RequireMeetingDeletionInTx(context.Context, f.Tx, i.ProjectID, ec.AssociatedMeeting) error {
	a.calls++
	return a.err
}
func meetingRetirementTestIdentity(t *testing.T) (i.ProjectID, ec.AssociatedMeeting) {
	t.Helper()
	p, d := associatedTestIdentity(t)
	return p, ec.AssociatedMeeting{ExecutionID: d.ExecutionID, AgentID: d.AgentID, Key: d.Key, Digest: d.Digest, Generation: 1, Attempt: 1,
		Trigger: ec.Trigger{Kind: "meeting", MeetingID: newTestID[struct{}](t).String(), TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}}
}
func TestMeetingRetirementRequiresPrivateDeletionAndOriginalTransaction(t *testing.T) {
	v := newExecutionCalls(t)
	p, m := meetingRetirementTestIdentity(t)
	denied := fault(f.Forbidden)
	authority := &meetingRetirementTestAuthority{store: v.store, err: denied}
	r, err := NewMeetingExecutionRetirement(v.pair, authority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewMeetingExecutionRetirement(nil, authority); err == nil {
		t.Fatal("new owner graph accepted")
	}
	tx := f.NewTx()
	locks, err := ec.MeetingExecutionRetirementLocks(p, m)
	if err != nil {
		t.Fatal(err)
	}
	v.store.live[tx] = locks
	if err = r.RequireMeetingExecutionRetiredInTx(context.Background(), tx, p, m); !errors.Is(err, denied) || authority.calls != 1 {
		t.Fatal("public tuple substituted for deletion loan", err)
	}
	// The private callback must not even run with a weaker lock set or ended Tx.
	v.store.live[tx], _ = ec.MeetingExecutionReadLocks(p, m)
	if err = r.RequireMeetingExecutionRetiredInTx(context.Background(), tx, p, m); err == nil || authority.calls != 1 {
		t.Fatal("shared Project lock accepted", err)
	}
	delete(v.store.live, tx)
	if err = r.RequireMeetingExecutionRetiredInTx(context.Background(), tx, p, m); err == nil || authority.calls != 1 {
		t.Fatal("ended transaction accepted", err)
	}
}
func TestMeetingRetirementClosedDatabasePhaseDoesNotHideReturnTail(t *testing.T) {
	v := newExecutionCalls(t)
	p, m := meetingRetirementTestIdentity(t)
	r := &MeetingExecutionRetirement{original: v.pair.state}
	s := r.original
	if !r.localReturned(p, m) {
		t.Fatal("empty original registries")
	}
	id := newTestID[projectCallID](t)
	// Even bodyReturned/closed is earlier than removal by the original returned hook.
	s.owners[id] = &projectCallEntry{record: projectCallRecord{Project: p, Phase: "business_closed"}, execution: m.ExecutionID, bodyReturned: true, closed: true}
	if r.localReturned(p, m) {
		t.Fatal("database closure hid physical tail")
	}
	delete(s.owners, id)
	s.preparation.state.calls[m.ExecutionID] = &preparationCall{}
	if r.localReturned(p, m) {
		t.Fatal("preparation owner ignored")
	}
	delete(s.preparation.state.calls, m.ExecutionID)
	s.direct.state.calls[m.ExecutionID] = &directTextCall{}
	if r.localReturned(p, m) {
		t.Fatal("direct owner ignored")
	}
	delete(s.direct.state.calls, m.ExecutionID)
	s.associated.state.jobs[m.ExecutionID] = &associatedCall{}
	if r.localReturned(p, m) {
		t.Fatal("associated owner ignored")
	}
	delete(s.associated.state.jobs, m.ExecutionID)
	s.associated.state.admissions = map[i.ExecutionID]*associatedAdmission{m.ExecutionID: {}}
	if r.localReturned(p, m) {
		t.Fatal("pending original admission ignored")
	}
	delete(s.associated.state.admissions, m.ExecutionID)
	if !r.localReturned(p, m) {
		t.Fatal("returned graph rejected")
	}
}
func TestMeetingRetirementRequiresOriginalProcessProof(t *testing.T) {
	v := newExecutionCalls(t)
	r := &MeetingExecutionRetirement{original: v.pair.state}
	s := r.original
	current := meetingRetirementOwner{Process: s.process, Instance: s.instance, Launch: true}
	if err := r.requireOwnersReturned(context.Background(), []meetingRetirementOwner{current}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*meetingRetirementOwner){func(v *meetingRetirementOwner) { v.Running = true }, func(v *meetingRetirementOwner) { v.Instance = newTestID[projectCallInstance](t) }} {
		bad := current
		mutate(&bad)
		if err := r.requireOwnersReturned(context.Background(), []meetingRetirementOwner{bad}, nil, nil); err == nil {
			t.Fatal("current owner tail accepted")
		}
	}
	old := current
	old.Process = newTestID[oc.Process](t)
	if err := r.requireOwnersReturned(context.Background(), []meetingRetirementOwner{old}, nil, nil); err == nil {
		t.Fatal("closed stale row substituted for stopped proof")
	}
	observed := 0
	v.proof.confirm = func(_ context.Context, process oc.ProcessID) error {
		if process != old.Process {
			t.Fatal("wrong original process")
		}
		observed++
		return nil
	}
	old.Running = true
	if err := r.requireOwnersReturned(context.Background(), []meetingRetirementOwner{old}, nil, nil); err != nil || observed != 1 {
		t.Fatal("exact controlled stopped proof rejected", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.requireOwnersReturned(ctx, []meetingRetirementOwner{current}, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation became retirement", err)
	}
}
func TestMeetingRetirementTerminalRequiresOriginalCaptureAndCancellationShape(t *testing.T) {
	run := runtimeCandidate(t, "terminal")
	snapshot := run.snapshot.Fields()
	request := run.request
	claim := &preparationClaim{execution: request.ExecutionID, project: request.Launch.ProjectID, agent: request.Launch.AgentID, attempt: newTestID[preparationAttempt](t), process: snapshot.ProcessID, fence: 1, phase: "terminal"}
	input := &preparationInputRecord{claim: *claim, input: snapshot.Context.Input()}
	input.claim.phase = "running"
	row := &executionRecord{launch: request.Launch, summary: ec.Summary{ID: request.ExecutionID, ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID, Status: run.terminal.status, Version: run.terminal.version, SnapshotID: &snapshot.ID, StartedAt: &snapshot.CreatedAt, CompletedAt: &run.terminal.at}}
	stored := &directTextRecord{snapshot: run.snapshot, terminal: run.terminal}
	if err := meetingRetirementTerminal(row, claim, input, stored); err != nil {
		t.Fatal(err)
	}
	bad := *row
	bad.summary = row.summary.Clone()
	bad.summary.Version++
	if err := meetingRetirementTerminal(&bad, claim, input, stored); err == nil {
		t.Fatal("terminal version drift accepted")
	}
	badInput := *input
	badInput.claim.fence++
	if err := meetingRetirementTerminal(row, claim, &badInput, stored); err == nil {
		t.Fatal("foreign preparation accepted")
	}
	if err := meetingRetirementTerminal(row, claim, input, nil); err == nil {
		t.Fatal("status alone proved terminal retirement")
	}
	// Cancellation before any round is a separate atomic writer, not success
	// inferred from an absent Model call or an empty local owner map.
	before := *row
	before.summary = row.summary.Clone()
	before.summary.Status = ec.Cancelled
	before.summary.SnapshotID = nil
	before.summary.StartedAt = nil
	before.summary.CancelRequestedAt = &snapshot.CreatedAt
	if err := meetingRetirementTerminal(&before, nil, nil, nil); err != nil {
		t.Fatal("pre-capture cancellation shape", err)
	}
	if err := meetingRetirementTerminal(&before, claim, input, nil); err != nil {
		t.Fatal("captured cancellation shape", err)
	}
	before.summary.CancelRequestedAt = nil
	if err := meetingRetirementTerminal(&before, nil, nil, nil); err == nil {
		t.Fatal("unproven no-round terminal accepted")
	}
}
