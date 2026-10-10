package mount

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
)

// Controlled owner callbacks exercise the provider protocol, not real
// Execution preparation authority or a committed environment snapshot.
type mountCaptureOwner struct {
	store            *controlledStore
	request          mc.ExecutionMountCaptureRequest
	facts            mc.ExecutionMountCaptureFacts
	err              error
	cancel           context.CancelFunc
	discovery, final int
}

func (o *mountCaptureOwner) check(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) error {
	if tx != o.store.tx || r != o.request {
		return fault(f.Forbidden)
	}
	if err := o.store.RequireHeldLocks(ctx, tx, mountCaptureLocks(r)); err != nil {
		return err
	}
	if o.cancel != nil {
		o.cancel()
	}
	return o.err
}
func (o *mountCaptureOwner) RequireMountCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) (mc.ExecutionMountCaptureScope, error) {
	o.discovery++
	return o.facts.Scope, o.check(ctx, tx, r)
}
func (o *mountCaptureOwner) RequireMountCaptureInTx(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) (mc.ExecutionMountCaptureFacts, error) {
	o.final++
	return o.facts, o.check(ctx, tx, r)
}
func mountCaptureFixture(t *testing.T) (*ExecutionCapture, *controlledStore, *mountCaptureOwner, mc.ExecutionMountCaptureRequest) {
	t.Helper()
	_, s, projects, _, change := fixture(t)
	s.head = referenceState{exists: true, version: 1, ids: []i.MountID{}}
	r := mc.ExecutionMountCaptureRequest{ProjectID: change.ProjectID, AgentID: change.AgentID, ExecutionID: testID[i.Execution](t, 50)}
	o := &mountCaptureOwner{store: s, request: r, facts: mc.ExecutionMountCaptureFacts{Scope: mc.ExecutionMountCaptureScope{Project: projects.access.Project(), AttemptBinding: f.Digest(strings.Repeat("a", 64))}, AgentVersion: 1, AllowedMountIDs: []i.MountID{}}}
	p, err := NewExecutionCapture(s, o)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, o, r
}
func TestExecutionMountCaptureRequiresRealEmptyHead(t *testing.T) {
	p, s, o, r := mountCaptureFixture(t)
	plan, err := p.DiscoverExecutionMounts(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Exclusive
	if s.RequireHeldLocks(context.Background(), s.tx, plan.RequiredLocks()) != nil {
		t.Fatal("plan aliases caller locks")
	}
	out, err := p.CaptureExecutionMountsInTx(context.Background(), s.tx, r, plan)
	if err != nil || out.Validate() != nil || out.Request != r || out.AgentVersion != 1 || out.Mounts == nil {
		t.Fatal("real empty head rejected", err)
	}
	if o.discovery != 1 || o.final != 1 || s.acquires != 1 || s.reads != 4 || s.writes != 0 {
		t.Fatal("capture skipped source or opened a new writer")
	}
	copy := out.Clone()
	copy.Mounts = nil
	if out.Mounts == nil || copy.Validate() == nil {
		t.Fatal("nil became valid empty metadata")
	}
	for _, nonempty := range []bool{false, true} {
		p, s, _, r := mountCaptureFixture(t)
		s.head.exists = nonempty
		if nonempty {
			s.head.ids = []i.MountID{testID[i.Mount](t, 60)}
		}
		plan, err := p.DiscoverExecutionMounts(context.Background(), r)
		requireCode(t, err, f.DependencyUnbound)
		if plan != nil || s.writes != 0 {
			t.Fatal("missing or nonempty source was fabricated")
		}
	}
}
func TestExecutionMountCaptureRejectsStaleAndForeignPlans(t *testing.T) {
	for _, mode := range []string{"attempt", "agent-version", "head-version", "missing-head", "nonempty-head", "nil-allowlist", "nonempty-allowlist", "foreign-tx", "missing-lock", "foreign-issuer", "missing-proof"} {
		p, s, o, r := mountCaptureFixture(t)
		plan, err := p.DiscoverExecutionMounts(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		tx := s.tx
		switch mode {
		case "attempt":
			o.facts.Scope.AttemptBinding = f.Digest(strings.Repeat("b", 64))
		case "agent-version":
			o.facts.AgentVersion++
		case "head-version":
			s.head.version++
		case "missing-head":
			s.head.exists = false
		case "nonempty-head":
			s.head.ids = []i.MountID{testID[i.Mount](t, 60)}
		case "nil-allowlist":
			o.facts.AllowedMountIDs = nil
		case "nonempty-allowlist":
			o.facts.AllowedMountIDs = []i.MountID{testID[i.Mount](t, 60)}
		case "foreign-tx":
			tx = f.NewTx()
		case "missing-lock":
			s.missingLocks = true
		case "foreign-issuer":
			p, _ = NewExecutionCapture(s, o)
		case "missing-proof":
			o.err = fault(f.Forbidden)
		}
		out, err := p.CaptureExecutionMountsInTx(context.Background(), tx, r, plan)
		if err == nil || out.Mounts != nil || out.Request.Validate() == nil || s.writes != 0 || s.acquires != 1 {
			t.Fatal("stale authority/source published", mode, err)
		}
	}
}
func TestExecutionMountCaptureCancellationAndPhysicalUnknown(t *testing.T) {
	p, s, o, r := mountCaptureFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	plan, err := p.DiscoverExecutionMounts(ctx, r)
	if !errors.Is(err, context.Canceled) || plan != nil || s.reads != 0 {
		t.Fatal("callback cancellation published discovery", err)
	}
	p, s, o, r = mountCaptureFixture(t)
	plan, err = p.DiscoverExecutionMounts(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	o.cancel = cancel
	out, err := p.CaptureExecutionMountsInTx(ctx, s.tx, r, plan)
	if !errors.Is(err, context.Canceled) || out.Mounts != nil || s.reads != 2 || s.writes != 0 {
		t.Fatal("callback cancellation published final", err)
	}
	p, s, _, r = mountCaptureFixture(t)
	attempt := testID[f.TransactionAttempt](t, 61)
	cause, err := f.NewJobCause("mount-capture-test", r.ExecutionID.String(), attempt.String())
	if err != nil {
		t.Fatal(err)
	}
	physical := f.UnknownResult(attempt, cause)
	s.physical = &physical
	plan, err = p.DiscoverExecutionMounts(context.Background(), r)
	got, ok := UnknownAttempt(err)
	if plan != nil || !ok || got.AttemptID() != attempt || got.Cause().Validate() != nil || s.writes != 0 {
		t.Fatal("physical Unknown was replaced", err)
	}
}
