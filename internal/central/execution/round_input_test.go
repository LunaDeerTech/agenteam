package execution

import (
	"bytes"
	"context"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// These are selection/codec and ownership controls, not canonical Skill,
// complete SQL, real PG or Model HTTP success. The PG chain supplies those facts.
func roundInputSelectionControl(t *testing.T) (*roundInputSource, []sc.RuntimeAssignmentChange) {
	t.Helper()
	x := newReadFileAuthorityControl(t)
	r := x.input.Fields().Request
	digest := c.TriggerInputDigest([]byte("round-input-control"))
	a := sc.SkillBinding{SkillID: newTestID[pc.Skill](t), RevisionID: newTestID[sc.Revision](t), Revision: 1, AssignmentID: newTestID[sc.Assignment](t), AssignmentSequence: 2, Name: "controlled skill", Description: "", PackageSHA256: digest, EntryPath: sc.EntryPath}
	initial := sc.InitialSkillBindings{Request: sc.SkillCaptureRequest{ProjectID: r.Launch.ProjectID, AgentID: r.Launch.AgentID, ExecutionID: r.ExecutionID}, AssignmentSequence: 2, Bindings: []sc.SkillBinding{a}}
	receipt := c.ToolResultConsumptionReceipt{ExecutionID: r.ExecutionID, RoundID: x.stored.round.Fields().ID, CallID: x.stored.toolPending.call.ID, OperationID: newTestID[tc.Operation](t), AttemptID: newTestID[tc.Attempt](t), Version: 4, TranscriptThrough: 3, MessageDigest: digest, ResultDigest: digest, CreatedAt: x.stored.toolPending.at}
	source := &roundInputSource{row: &executionRecord{summary: c.Summary{ID: r.ExecutionID, ProjectID: r.Launch.ProjectID, AgentID: r.Launch.AgentID, Version: 4, Status: c.Running}}, original: &x.stored, initial: sc.InitialRoundBindingsRequest{Initial: initial, CaptureAttemptBinding: digest}, receipt: receipt, prefix: digest}
	changes := make([]sc.RuntimeAssignmentChange, 3)
	for n, seq := range []f.Version{2, 4, 6} {
		b := a
		b.AssignmentSequence = seq
		if n > 0 {
			b.AssignmentID = newTestID[sc.Assignment](t)
			b.RevisionID = newTestID[sc.Revision](t)
			b.Revision = f.Revision(n + 1)
		}
		changes[n] = sc.RuntimeAssignmentChange{ID: newTestID[sc.AssignmentChange](t), ProjectID: r.Launch.ProjectID, AgentID: r.Launch.AgentID, AssignmentSequence: seq, AgentVersion: seq, Binding: b, RecordedAt: receipt.CreatedAt}
	}
	return source, changes
}
func TestExecutionRoundInputSelectionUsesRealCutoffAndReplacement(t *testing.T) {
	source, changes := roundInputSelectionControl(t)
	v, err := buildRoundInput(source, newTestID[c.Round](t), 7, changes, []sc.AssignmentValidity{"", sc.AssignmentRemoved, sc.AssignmentValid})
	if err != nil {
		t.Fatal(err)
	}
	q := v.Fields()
	if q.AssignmentCutoff != 7 || q.TranscriptThrough != 4 || len(q.Bindings) != 1 || q.Bindings[0] != changes[2].Binding || len(q.AppliedChangeIDs) != 1 || q.AppliedChangeIDs[0] != changes[2].ID || q.Dispositions[0].State != "covered" || q.Dispositions[1].Reason != "removed" || q.Dispositions[2].State != "applied" || q.Control == nil || len(q.Control.Payload) != 1 || q.Control.Payload[0].ReplacedAssignmentID == nil || *q.Control.Payload[0].ReplacedAssignmentID != source.initial.Initial.Bindings[0].AssignmentID {
		t.Fatal("cutoff/covered/replaced identity lost")
	}
	if source.initial.Initial.Bindings[0].AssignmentID != changes[0].Binding.AssignmentID || source.original.toolPending.version != 3 {
		t.Fatal("source mutated")
	}
	for _, status := range []sc.AssignmentValidity{sc.AssignmentRemoved, sc.AssignmentDisabled, sc.AssignmentDeleted} {
		t.Run(string(status), func(t *testing.T) {
			empty, err := buildRoundInput(source, newTestID[c.Round](t), 7, changes, []sc.AssignmentValidity{"", status, status})
			if err != nil {
				t.Fatal(err)
			}
			f := empty.Fields()
			if f.Control != nil || f.TranscriptThrough != 3 || len(f.AppliedChangeIDs) != 0 || len(f.Bindings) != 1 || f.Bindings[0] != source.initial.Initial.Bindings[0] {
				t.Fatal("revocation scrubbed historical context or made an empty control")
			}
		})
	}
}
func TestExecutionRoundInputCodecRejectsChangedProof(t *testing.T) {
	source, changes := roundInputSelectionControl(t)
	v, err := buildRoundInput(source, newTestID[c.Round](t), 7, changes, []sc.AssignmentValidity{"", sc.AssignmentRemoved, sc.AssignmentValid})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := c.DecodeRoundInput(v.CanonicalBytes())
	if err != nil || decoded.Digest() != v.Digest() {
		t.Fatal("canonical round trip", err)
	}
	original := v.CanonicalBytes()
	copy := v.Fields()
	copy.Bindings[0].Name = "changed"
	copy.Control.Payload[0].Binding.Name = "changed"
	*copy.Control.Payload[0].ReplacedAssignmentID = newTestID[sc.Assignment](t)
	if !bytes.Equal(original, v.CanonicalBytes()) {
		t.Fatal("mutable alias escaped")
	}
	for name, alter := range map[string]func(*c.RoundInputFields){
		"cutoff":           func(q *c.RoundInputFields) { q.AssignmentCutoff = 5 },
		"source-round":     func(q *c.RoundInputFields) { q.Source.RoundID = newTestID[c.Round](t) },
		"removed-grant":    func(q *c.RoundInputFields) { q.Bindings[0] = source.initial.Initial.Bindings[0] },
		"replacement":      func(q *c.RoundInputFields) { q.Control.Payload[0].ReplacedAssignmentID = nil },
		"duplicate-change": func(q *c.RoundInputFields) { q.Dispositions[1].ChangeID = q.Dispositions[0].ChangeID },
		"empty-control":    func(q *c.RoundInputFields) { q.Control.Payload = []c.SkillAddition{} },
	} {
		t.Run(name, func(t *testing.T) {
			q := v.Fields()
			alter(&q)
			if _, err := c.NewRoundInput(q); err == nil {
				t.Fatal("changed proof accepted")
			}
		})
	}
}

func TestExecutionRoundInputRejectsDetachedPlanAndWitness(t *testing.T) {
	store := &launchStore{t: t, rows: map[string][]any{}}
	d := &RoundInputDeterminer{store: store}
	authority := &Authority{state: &authorityState{store: store}}
	d.authority = authority
	e, next := newTestID[i.Execution](t), newTestID[c.Round](t)
	p := &roundInputPlan{owner: d, execution: e, next: next, locks: []f.LockRequest{executionLock(e)}}
	if _, err := d.determineRoundInputInTx(context.Background(), f.NewTx(), e, next, p); err == nil {
		t.Fatal("foreign transaction accepted")
	}
	store.tx, store.live = f.NewTx(), true
	if _, err := d.determineRoundInputInTx(context.Background(), store.tx, e, next, p); err == nil {
		t.Fatal("missing full union accepted")
	}
	other := *p
	other.owner = &RoundInputDeterminer{}
	if _, err := d.determineRoundInputInTx(context.Background(), store.tx, e, next, &other); err == nil {
		t.Fatal("foreign plan accepted")
	}
	w := &roundCurrentWitness{owner: d, tx: store.tx}
	ctx := context.WithValue(context.Background(), roundCurrentKey{}, w)
	if authority.requireRoundCurrentConfiguration(ctx, store.tx, ac.ExecutionConfigurationRequest{}) == nil {
		t.Fatal("expired witness accepted")
	}
	w.active.Store(true)
	if authority.requireRoundCurrentConfiguration(ctx, f.NewTx(), ac.ExecutionConfigurationRequest{}) == nil {
		t.Fatal("witness changed transaction")
	}
	w.active.Store(false)
}

type roundBlockedStore struct {
	*launchStore
	entered, cancelled, release chan struct{}
	calls                       int
}

func (s *roundBlockedStore) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	close(s.entered)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.InvalidState, f.NotCommitted))
}
func TestExecutionRoundInputUnknownAndStopOwnOriginalReturn(t *testing.T) {
	source, _ := roundInputSelectionControl(t)
	e, next := source.row.summary.ID, newTestID[c.Round](t)
	s := &roundBlockedStore{launchStore: &launchStore{t: t, rows: map[string][]any{}}, entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	d := &RoundInputDeterminer{store: s, active: make(map[i.ExecutionID]context.CancelFunc), unknown: make(map[i.ExecutionID]*roundInputUnknown), changed: make(chan struct{})}
	t.Cleanup(func() {
		d.Stop()
		select {
		case <-s.release:
		default:
			close(s.release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Drain(ctx); err != nil {
			t.Error("original blocked store cleanup", err)
		}
	})
	cause, _ := f.NewJobCause("execution.round-input.determine", e.String(), next.String())
	errUnknown := commitError(f.UnknownResult(newTestID[f.TransactionAttempt](t), cause))
	d.unknown[e] = &roundInputUnknown{round: next, err: errUnknown}
	if _, err := d.Determine(context.Background(), e, newTestID[c.Round](t)); err != errUnknown || s.calls != 0 {
		t.Fatal("Unknown became retry")
	}
	delete(d.unknown, e)
	done := make(chan error, 1)
	go func() { _, err := d.Determine(context.Background(), e, next); done <- err }()
	<-s.entered
	d.Stop()
	<-s.cancelled
	if d.Joined() {
		t.Fatal("cancel mistaken for return")
	}
	select {
	case <-done:
		t.Fatal("returned before original store")
	default:
	}
	close(s.release)
	if err := <-done; err == nil {
		t.Fatal("cancelled discovery succeeded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Drain(ctx); err != nil || !d.Joined() {
		t.Fatal("original store did not join", err)
	}
}
