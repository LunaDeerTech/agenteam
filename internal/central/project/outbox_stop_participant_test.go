package project

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// These are explicit Store/participant controls. The PG tests independently
// exercise the canonical rows and the real D06 service.
type outboxStopControl struct {
	name    string
	request func(context.Context, identity.Actor, oc.LifecycleCause) (oc.StopReport, error)
	inspect func(context.Context, identity.Actor, oc.LifecycleCause) (oc.StopReport, error)
}

func (p *outboxStopControl) Name() string { return p.name }
func (p *outboxStopControl) RequestStop(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
	return p.request(ctx, actor, cause)
}
func (p *outboxStopControl) InspectStop(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
	return p.inspect(ctx, actor, cause)
}
func (*outboxStopControl) Cleanup(context.Context, identity.Actor, oc.LifecycleCause) (oc.CleanupReport, error) {
	panic("Outbox stop adapter must not call Cleanup")
}

// The minimal deletion receipt supplies a real strict-decoder preflight for
// Inspect only. Current stopping/Request canonical rows are covered by PG;
// postgres.Rows is deliberately not replaced with a test-only fake interface.
type outboxStopControlSQL struct {
	postgres.SQLExecutor
	receipt []any
}

func (x *outboxStopControlSQL) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	switch {
	case strings.Contains(query, "FROM agenteam_project.projects "):
		return rowFunc(func(...any) error { return pgx.ErrNoRows })
	case strings.Contains(query, "FROM agenteam_project.deletion_receipts "):
		return valuesRow(x.receipt...)
	default:
		return rowFunc(func(...any) error { return errors.New("unexpected controlled query") })
	}
}

type outboxStopFixture struct {
	actor       identity.Actor
	cause       c.LifecycleCause
	scope       c.ScopeRef
	store       *barrierControlStore
	x           *outboxStopControlSQL
	provider    *outboxStopControl
	participant c.ProjectLifecycleParticipant
	txContext   context.Context
	returned    bool
	calls       int
}

func newOutboxStopFixture(t *testing.T) *outboxStopFixture {
	t.Helper()
	actor, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	v := &outboxStopFixture{actor: actor, cause: cause, scope: c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}}
	manifest := registryTestManifest(t, registryTestEntries())
	now := time.Now().UTC().Truncate(time.Microsecond)
	owner := testID[identity.User](t).String()
	v.x = &outboxStopControlSQL{receipt: []any{cause.OperationID.String(), project.String(), owner, string(digest([]byte("delete-key"))), string(digest([]byte("delete-request"))), now, "completed"}}
	tx := f.NewTx()
	v.store = &barrierControlStore{}
	v.store.within = func(ctx context.Context, _ f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
		v.txContext, v.returned = ctx, false
		defer func() { v.returned = true }()
		if err := callback(ctx, tx); err != nil {
			var fault *f.Fault
			if !errors.As(err, &fault) {
				t.Fatal("unsafe callback error", err)
			}
			return f.NotCommittedResult(fault)
		}
		return f.CommittedResult()
	}
	checkLock := func(_ context.Context, got f.Tx, locks []f.LockRequest) error {
		if got != tx || len(locks) != 1 || locks[0].Mode != f.Shared || locks[0].Key.Canonical() != projectLock(project, f.Shared).Key.Canonical() {
			return errors.New("not the same transaction and exact Project SH")
		}
		return nil
	}
	v.store.acquire, v.store.held = checkLock, checkLock
	v.store.inTx = func(got f.Tx) (postgres.SQLExecutor, error) {
		if got != tx {
			return nil, errors.New("foreign transaction")
		}
		return v.x, nil
	}
	authority, err := NewLifecycleAuthority(v.store, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	v.provider = &outboxStopControl{name: "outbox"}
	call := func(ctx context.Context, got identity.Actor, gotCause oc.LifecycleCause) (oc.StopReport, error) {
		v.calls++
		d := gotCause.Details()
		if !v.returned || ctx != v.txContext || !got.Equal(actor) || d.ProjectID != project || d.OperationID.String() != cause.OperationID.String() || string(d.Action) != string(cause.Action) || d.ProjectVersion != cause.ProjectVersion {
			t.Fatal("lost actual preflight return, original context or complete cause")
		}
		return oc.StopReport{Stopped: true}, nil
	}
	v.provider.request, v.provider.inspect = call, call
	v.participant, err = NewOutboxStopParticipant(authority, v.provider)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOutboxStopParticipantConstructionAndInput(t *testing.T) {
	v := newOutboxStopFixture(t)
	authority := v.participant.(*outboxStopParticipant).authority
	var absent *outboxStopControl
	for _, dependency := range []oc.ProjectLifecycleParticipant{nil, absent} {
		p, err := NewOutboxStopParticipant(authority, dependency)
		hasCode(t, err, f.DependencyUnbound)
		if p != nil {
			t.Fatal("nil participant admitted")
		}
	}
	for _, a := range []*LifecycleAuthority{nil, {}} {
		_, err := NewOutboxStopParticipant(a, v.provider)
		hasCode(t, err, f.DependencyUnbound)
	}
	_, err := NewOutboxStopParticipant(authority, &outboxStopControl{name: "audit"})
	hasCode(t, err, f.InvalidArgument)
	if v.participant.Name() != c.OutboxParticipant {
		t.Fatal("fixed name lost")
	}
	meeting := testID[c.Meeting](t)
	for _, tc := range []struct {
		name  string
		actor identity.Actor
		cause c.LifecycleCause
		scope c.ScopeRef
		code  f.Code
	}{
		{"actor", testActor(t), v.cause, v.scope, f.Forbidden},
		{"cause", v.actor, c.LifecycleCause{}, v.scope, f.InvalidArgument},
		{"scope", v.actor, v.cause, c.ScopeRef{}, f.InvalidArgument},
		{"foreign-project", v.actor, v.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: testID[identity.Project](t)}, f.Forbidden},
		{"meeting", v.actor, v.cause, c.ScopeRef{Kind: c.MeetingScope, ProjectID: v.scope.ProjectID, MeetingID: &meeting}, f.DependencyUnbound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, call := range []func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error){v.participant.RequestStop, v.participant.InspectStop} {
				report, err := call(context.Background(), tc.actor, tc.cause, tc.scope)
				hasCode(t, err, tc.code)
				if report.Details().State != "" || v.calls != 0 || v.txContext != nil {
					t.Fatal("invalid input reached a dependency or exposed a report")
				}
			}
		})
	}
	_, err = v.participant.RequestStop(nil, v.actor, v.cause, v.scope)
	hasCode(t, err, f.InvalidArgument)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = v.participant.InspectStop(ctx, v.actor, v.cause, v.scope); err == nil {
		t.Fatal("cancelled input admitted")
	}
	cleanup, err := v.participant.Cleanup(context.Background(), v.actor, v.cause, v.scope, nil)
	hasCode(t, err, f.DependencyUnbound)
	if cleanup.Details().State != "" || v.calls != 0 {
		t.Fatal("Cleanup delegated or fabricated progress")
	}
	v.provider.name = "changed"
	_, err = v.participant.RequestStop(context.Background(), v.actor, v.cause, v.scope)
	hasCode(t, err, f.DependencyUnavailable)
}

func TestOutboxStopParticipantPreflightFailures(t *testing.T) {
	for _, stage := range []string{"acquire", "foreign-tx", "held-lock", "not-committed", "unknown", "empty-committed", "old-cause", "request-after-delete"} {
		t.Run(stage, func(t *testing.T) {
			v := newOutboxStopFixture(t)
			attempt := testID[f.TransactionAttempt](t)
			var original f.TransactionCause
			switch stage {
			case "acquire":
				v.store.acquire = func(context.Context, f.Tx, []f.LockRequest) error { return errors.New("controlled acquire") }
			case "foreign-tx":
				v.store.inTx = func(f.Tx) (postgres.SQLExecutor, error) { return nil, errors.New("foreign store") }
			case "held-lock":
				v.store.held = func(context.Context, f.Tx, []f.LockRequest) error { return errors.New("missing lock") }
			case "not-committed", "unknown":
				next := v.store.within
				v.store.within = func(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
					original = cause
					if result := next(ctx, cause, callback); result.State() != f.Committed {
						t.Fatal("controlled valid preflight failed", result.Fault())
					}
					if stage == "unknown" {
						return f.UnknownResult(attempt, cause)
					}
					return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
				}
			case "empty-committed":
				v.store.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
					return f.CommittedResult()
				}
			case "old-cause":
				v.cause.Action = c.Archive
			}
			invoke := v.participant.InspectStop
			if stage == "request-after-delete" {
				invoke = v.participant.RequestStop
			}
			report, err := invoke(context.Background(), v.actor, v.cause, v.scope)
			if err == nil || report.Details().State != "" || v.calls != 0 {
				t.Fatal("failed preflight delegated or exposed report", err)
			}
			want := f.DependencyUnavailable
			switch stage {
			case "unknown":
				want = f.CommitUnknown
				got, ok := UnknownAttempt(err)
				gotCause, wantCause := got.Cause().Details(), original.Details()
				if !ok || got.AttemptID() != attempt || gotCause.Kind != f.RecoveryCause || gotCause.Kind != wantCause.Kind || gotCause.Owner != wantCause.Owner || gotCause.RecoveryRunID != wantCause.RecoveryRunID || gotCause.CheckpointRef != wantCause.CheckpointRef {
					t.Fatal("original Unknown provenance lost")
				}
			case "old-cause", "not-committed":
				want = f.Forbidden
			case "request-after-delete":
				want = f.InvalidState
			}
			hasCode(t, err, want)
		})
	}
}

func TestOutboxStopParticipantDelegationAndReports(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report oc.StopReport
		want   c.StopState
	}{
		{"stopped", oc.StopReport{Stopped: true}, c.Stopped},
		{"pending", oc.StopReport{Pending: 3}, c.StopPending},
		{"not-proved", oc.StopReport{}, c.StopPending},
		{"negative", oc.StopReport{Pending: -1}, ""},
		{"contradiction", oc.StopReport{Stopped: true, Pending: 1}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newOutboxStopFixture(t)
			next := v.provider.inspect
			v.provider.request = func(context.Context, identity.Actor, oc.LifecycleCause) (oc.StopReport, error) {
				t.Fatal("Inspect called RequestStop")
				return oc.StopReport{}, nil
			}
			v.provider.inspect = func(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
				_, _ = next(ctx, actor, cause)
				return tc.report, nil
			}
			report, err := v.participant.InspectStop(context.Background(), v.actor, v.cause, v.scope)
			if v.calls != 1 {
				t.Fatal("missing downstream call or replay")
			}
			if tc.want == "" {
				hasCode(t, err, f.DependencyUnavailable)
				if report.Details().State != "" {
					t.Fatal("invalid report projected")
				}
				return
			}
			d := report.Details()
			if err != nil || !report.Matches(c.OutboxParticipant, v.cause, v.scope) || d.State != tc.want || len(d.ActiveRefs) != 0 || len(d.UnknownRefs) != 0 || (d.State == c.StopPending) != (d.SafeReason == c.ReasonWorkPending) {
				t.Fatal("report identity, reason or count projection changed", err)
			}
		})
	}
}

func TestOutboxStopParticipantPreservesDownstreamError(t *testing.T) {
	v := newOutboxStopFixture(t)
	original := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("controlled private provenance"))
	original.CauseID = testID[f.TransactionAttempt](t).String()
	original.RetryHint = "lookup"
	call := func(context.Context, identity.Actor, oc.LifecycleCause) (oc.StopReport, error) {
		return oc.StopReport{Stopped: true}, original
	}
	v.provider.request, v.provider.inspect = call, call
	report, err := v.participant.InspectStop(context.Background(), v.actor, v.cause, v.scope)
	if err != original || report.Details().State != "" {
		t.Fatal("downstream Unknown was wrapped, erased or projected as success")
	}
}

func TestOutboxStopParticipantWaitsForActualReturn(t *testing.T) {
	for _, stage := range []string{"preflight", "inspect"} {
		t.Run(stage, func(t *testing.T) {
			v := newOutboxStopFixture(t)
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			hold := func(got context.Context) {
				gotDeadline, ok := got.Deadline()
				if !ok || !gotDeadline.Equal(deadline) || got.Err() != nil {
					t.Error("caller deadline lost or preflight context cancelled before delegation")
				}
				close(entered)
				<-release
			}
			if stage == "preflight" {
				next := v.store.within
				v.store.within = func(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
					result := next(ctx, cause, callback)
					hold(ctx)
					return result
				}
			} else {
				next := v.provider.request
				call := func(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
					report, err := next(ctx, actor, cause)
					hold(ctx)
					return report, err
				}
				v.provider.request, v.provider.inspect = call, call
			}
			var report c.StopReport
			var err error
			go func() {
				defer close(returned)
				report, err = v.participant.InspectStop(ctx, v.actor, v.cause, v.scope)
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("original dependency did not enter")
			}
			cancel()
			select {
			case <-returned:
				close(release)
				t.Fatal("cancellation substituted for actual return")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("released original dependency did not return")
			}
			if err == nil || report.Details().State != "" || stage == "preflight" && v.calls != 0 {
				t.Fatal("late success exposed after cancellation or preflight delegated")
			}
		})
	}
}

type outboxStopNameControl struct {
	oc.ProjectLifecycleParticipant
	name func() string
}

func (p *outboxStopNameControl) Name() string { return p.name() }

func TestOutboxStopParticipantOpaquePanicBoundary(t *testing.T) {
	for _, stage := range []string{"constructor-name", "request-name", "inspect-name", "inspect-after-preflight"} {
		t.Run(stage, func(t *testing.T) {
			v := newOutboxStopFixture(t)
			authority := v.participant.(*outboxStopParticipant).authority
			port := &outboxStopNameControl{ProjectLifecycleParticipant: v.provider, name: func() string { return "outbox" }}
			var report c.StopReport
			var err error
			var escaped any
			var participant c.ProjectLifecycleParticipant
			panicName := func() string { panic("private name panic marker") }
			func() {
				defer func() { escaped = recover() }()
				if stage == "constructor-name" {
					port.name = panicName
					participant, err = NewOutboxStopParticipant(authority, port)
					return
				}
				participant, err = NewOutboxStopParticipant(authority, port)
				if err != nil {
					t.Fatal(err)
				}
				if stage == "inspect-after-preflight" {
					next := v.provider.inspect
					v.provider.inspect = func(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
						_, _ = next(ctx, actor, cause)
						panic("private downstream panic marker")
					}
				} else {
					port.name = panicName
				}
				if stage == "request-name" {
					report, err = participant.RequestStop(context.Background(), v.actor, v.cause, v.scope)
				} else {
					report, err = participant.InspectStop(context.Background(), v.actor, v.cause, v.scope)
				}
			}()
			if escaped != nil {
				t.Fatal("opaque dependency panic escaped the adapter")
			}
			hasCode(t, err, f.DependencyUnavailable)
			if report.Details().State != "" || strings.Contains(err.Error(), "private") {
				t.Fatal("panic exposed a report or private panic detail")
			}
			if stage == "constructor-name" && participant != nil {
				t.Fatal("panicking declaration returned a participant")
			}
			if stage == "inspect-after-preflight" {
				if !v.returned || v.calls != 1 {
					t.Fatal("stop boundary did not follow actual preflight return")
				}
			} else if v.returned || v.calls != 0 {
				t.Fatal("panicking declaration reached preflight or stop")
			}
		})
	}

	t.Run("transaction-panic-keeps-original-unwind", func(t *testing.T) {
		v := newOutboxStopFixture(t)
		marker := errors.New("original transaction panic")
		v.store.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
			panic(marker)
		}
		var escaped any
		func() {
			defer func() { escaped = recover() }()
			_, _ = v.participant.InspectStop(context.Background(), v.actor, v.cause, v.scope)
		}()
		if escaped != marker || v.calls != 0 {
			t.Fatal("opaque boundary swallowed the original transaction unwind")
		}
	})

	t.Run("request-call-boundary", func(t *testing.T) {
		v := newOutboxStopFixture(t)
		operation, err := f.ParseID[oc.LifecycleOperation](v.cause.OperationID.String())
		if err != nil {
			t.Fatal(err)
		}
		cause, err := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: v.scope.ProjectID, OperationID: operation, Action: oc.DeleteProject, ProjectVersion: v.cause.ProjectVersion})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		calls := 0
		v.provider.request = func(got context.Context, actor identity.Actor, original oc.LifecycleCause) (oc.StopReport, error) {
			calls++
			if got != ctx || !actor.Equal(v.actor) || original.Details() != cause.Details() {
				t.Fatal("request call boundary changed original arguments")
			}
			panic("private RequestStop panic marker")
		}
		// This directly exercises the opaque RequestStop call boundary. The
		// canonical stopping preflight and its wiring are the real PG test's
		// responsibility; the pure receipt fixture only authorizes Inspect.
		report, err := callOutboxStop(v.provider, ctx, v.actor, cause, false)
		hasCode(t, err, f.DependencyUnavailable)
		if calls != 1 || report != (oc.StopReport{}) || strings.Contains(err.Error(), "private") {
			t.Fatal("RequestStop panic leaked a report, detail or changed call count")
		}
	})
}
