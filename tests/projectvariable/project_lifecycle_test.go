//go:build integration

package projectvariable_test

import (
	"context"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type lifecycleOriginalCall struct {
	hold     *variableLifecycleHold
	returned chan struct{}
	err      error
}

func beginLifecycleCall(t *testing.T, fn func(context.Context) error) *lifecycleOriginalCall {
	t.Helper()
	g := newLifecycleHold()
	out := &lifecycleOriginalCall{hold: g, returned: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	ctx = context.WithValue(ctx, lifecycleHoldKey{}, g)
	go func() { defer close(out.returned); out.err = fn(ctx) }()
	t.Cleanup(func() { cancel(); g.unhold(); await(t, out.returned) })
	select {
	case <-g.entered:
		g.mu.Lock()
		pid := g.pid
		g.mu.Unlock()
		if pid <= 0 {
			t.Fatal("original BEGIN backend PID missing")
		}
	case <-out.returned:
		t.Fatal("call returned before original BEGIN barrier")
	case <-time.After(5 * time.Second):
		t.Fatal("original BEGIN barrier not reached")
	}
	return out
}
func (c *lifecycleOriginalCall) requireCanceled(t *testing.T, expected bool) {
	t.Helper()
	c.hold.mu.Lock()
	err := c.hold.ctx.Err()
	c.hold.mu.Unlock()
	if (err == context.Canceled) != expected {
		t.Fatal("original callback cancellation scope mismatch")
	}
	select {
	case <-c.returned:
		t.Fatal("held original call returned early")
	default:
	}
}
func (c *lifecycleOriginalCall) join(t *testing.T, success bool) {
	t.Helper()
	c.hold.unhold()
	await(t, c.returned)
	if (c.err == nil) != success {
		t.Fatal("original call outcome mismatch")
	}
	c.hold.mu.Lock()
	callbacks, result := c.hold.callbacks, c.hold.result
	c.hold.mu.Unlock()
	if callbacks != 1 || success && result.State() != f.Committed || !success && result.State() != f.NotCommitted {
		t.Fatal("original callback/physical transaction not preserved")
	}
}

type lifecycleSeed struct {
	ordinary       vc.Variable
	secret         vc.SecretVariable
	ordinaryLookup vc.VariableCommandLookupRequest
	secretLookup   vc.SecretVariableCommandLookupRequest
}

func (v *variableLifecycleFixture) seed(t *testing.T, project pc.ProjectRef) lifecycleSeed {
	t.Helper()
	input := createInput(t, "PLAIN", "plain-value")
	m := meta(t, "lifecycle-plain", nil)
	plain, err := v.ordinary.CreateVariable(ctxFor(t), v.ownerBrowser.actor, m, project.ID, input)
	if err != nil {
		t.Fatal("seed ordinary", err)
	}
	digest, err := vc.VariableCommandDigest(v.ownerBrowser.actor, m, project.ID, input.Fields().ID, vc.CreateCommand, input)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := vc.NewVariableCommandLookupRequest(vc.VariableCommandLookupFields{ProjectID: project.ID, Command: vc.CreateCommand, IdempotencyKey: m.IdempotencyKey, SemanticDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	secretInput := secretCreateInput(t, "SECRET", []byte("lifecycle-owned-secret"))
	sm := meta(t, "lifecycle-secret", nil)
	secret, err := v.secretOwner.CreateSecretVariable(ctxFor(t), v.ownerBrowser.actor, sm, project.ID, secretInput)
	if err != nil {
		t.Fatal("seed Secret", err)
	}
	return lifecycleSeed{plain.Fields().Variable, secret.Fields().Variable, lookup, secretLookup(t, project.ID, secretInput.Fields().ID, vc.SecretCreateCommand, sm)}
}
func requireLocalStop(t *testing.T, r pv.ProjectCallStopReport, err error, cause pc.LifecycleCause, scope pc.ScopeRef, pending int) {
	t.Helper()
	if err != nil || !r.Matches(cause, scope) || r.Details().PendingCalls != pending || r.Details().LocalJoined != (pending == 0) {
		t.Fatal("local stop report/cause/pending mismatch", err)
	}
}

func TestProjectVariableLocalLifecycleStop(t *testing.T) {
	v := newVariableLifecycleFixture(t)
	foreign, _, _ := v.createProject(t, v.ownerBrowser.actor, "lifecycle-foreign")
	t.Run("archive-calls-and-project-isolation", func(t *testing.T) {
		project := v.project
		seed := v.seed(t, project)
		plainInput := createInput(t, "CANCELED_PLAIN", "value")
		secretInput := secretCreateInput(t, "CANCELED_SECRET", []byte("value"))
		foreignPlain := createInput(t, "FOREIGN_PLAIN", "value")
		foreignSecret := secretCreateInput(t, "FOREIGN_SECRET", []byte("value"))
		plainMeta, secretMeta := meta(t, "archive-write-plain", nil), meta(t, "archive-write-secret", nil)
		foreignPM, foreignSM := meta(t, "foreign-write-plain", nil), meta(t, "foreign-write-secret", nil)
		calls := []*lifecycleOriginalCall{
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.ordinary.CreateVariable(ctx, v.ownerBrowser.actor, plainMeta, project.ID, plainInput)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.secretOwner.CreateSecretVariable(ctx, v.ownerBrowser.actor, secretMeta, project.ID, secretInput)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.ordinary.GetVariable(ctx, v.ownerBrowser.actor, project.ID, seed.ordinary.Fields().ID)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.secretOwner.GetSecretVariable(ctx, v.ownerBrowser.actor, project.ID, seed.secret.Fields().ID)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.ordinary.CreateVariable(ctx, v.ownerBrowser.actor, foreignPM, foreign.ID, foreignPlain)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.secretOwner.CreateSecretVariable(ctx, v.ownerBrowser.actor, foreignSM, foreign.ID, foreignSecret)
				return e
			}),
		}
		cause, scope, actor, result := v.stage(ctxFor(t), t, project, pc.Archive, nil)
		if result.State() != f.Committed {
			t.Fatal("normative stopping fixture did not commit", result.Fault())
		}
		wrong := cause
		wrong.ProjectVersion++
		if r, err := v.stopper.RequestStop(ctxFor(t), actor, wrong, scope); err == nil || r.Valid() {
			t.Fatal("wrong accepted version authorized")
		}
		v.tracked.setAfter(func(_ context.Context, _ f.Tx, c f.TransactionCause) error {
			if c.Details().Owner == "projectvariable-stop" {
				return f.NewFault(f.DependencyUnavailable, f.NotStarted)
			}
			return nil
		})
		t.Cleanup(func() { v.tracked.setAfter(nil) })
		if r, err := v.stopper.RequestStop(ctxFor(t), actor, cause, scope); err == nil || r.Valid() {
			t.Fatal("rolled-back authorization canceled")
		}
		for _, call := range calls {
			call.requireCanceled(t, false)
		}
		v.tracked.setAfter(nil)
		r, err := v.stopper.RequestStop(ctxFor(t), actor, cause, scope)
		requireLocalStop(t, r, err, cause, scope, 2)
		for n, call := range calls {
			call.requireCanceled(t, n < 2)
		}
		r, err = v.stopper.InspectStop(ctxFor(t), actor, cause, scope)
		requireLocalStop(t, r, err, cause, scope, 2)
		calls[0].join(t, false)
		calls[1].join(t, false)
		r, err = v.stopper.InspectStop(ctxFor(t), actor, cause, scope)
		requireLocalStop(t, r, err, cause, scope, 0)
		for _, call := range calls[2:] {
			call.requireCanceled(t, false)
			call.join(t, true)
		}
		var count int
		if err = v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_projectvariable.variables WHERE id=ANY($1::uuid[])`, []string{plainInput.Fields().ID.String(), secretInput.Fields().ID.String()}).Scan(&count); err != nil || count != 0 {
			t.Fatal("canceled writes left canonical variables")
		}
	})
	t.Run("delete-read-calls", func(t *testing.T) {
		project, _, _ := v.createProject(t, v.ownerBrowser.actor, "lifecycle-delete")
		seed := v.seed(t, project)
		calls := []*lifecycleOriginalCall{
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.ordinary.GetVariable(ctx, v.ownerBrowser.actor, project.ID, seed.ordinary.Fields().ID)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.ordinary.ListVariables(ctx, v.ownerBrowser.actor, project.ID, f.DefaultPageRequest())
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.ordinary.LookupVariableCommand(ctx, v.ownerBrowser.actor, seed.ordinaryLookup)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.secretOwner.GetSecretVariable(ctx, v.ownerBrowser.actor, project.ID, seed.secret.Fields().ID)
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.secretOwner.ListSecretVariables(ctx, v.ownerBrowser.actor, project.ID, f.DefaultPageRequest())
				return e
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, e := v.secretOwner.LookupSecretVariableCommand(ctx, v.ownerBrowser.actor, seed.secretLookup)
				return e
			}),
		}
		cause, scope, actor, result := v.stage(ctxFor(t), t, project, pc.Delete, nil)
		if result.State() != f.Committed {
			t.Fatal("delete phase fixture", result.Fault())
		}
		r, err := v.stopper.RequestStop(ctxFor(t), actor, cause, scope)
		requireLocalStop(t, r, err, cause, scope, 6)
		for _, call := range calls {
			call.requireCanceled(t, true)
		}
		r, err = v.stopper.InspectStop(ctxFor(t), actor, cause, scope)
		requireLocalStop(t, r, err, cause, scope, 6)
		for _, call := range calls {
			call.join(t, false)
		}
		r, err = v.stopper.InspectStop(ctxFor(t), actor, cause, scope)
		requireLocalStop(t, r, err, cause, scope, 0)
	})
	t.Run("confirmed-phase-and-rejection", func(t *testing.T) {
		project, _, _ := v.createProject(t, v.ownerBrowser.actor, "lifecycle-lock")
		input, m := createInput(t, "HELD", "value"), meta(t, "held-write", nil)
		call := beginLifecycleCall(t, func(ctx context.Context) error {
			_, e := v.ordinary.CreateVariable(ctx, v.ownerBrowser.actor, m, project.ID, input)
			return e
		})
		cause := pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: pc.Archive, ProjectVersion: project.Version + 1}
		scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: project.ID}
		registration, _ := i.RegisterService(i.ProjectLifecycle)
		actorScope, _ := i.InProject(project.ID)
		actor, err := registration.Actor(cause.OperationID.String(), actorScope)
		if err != nil {
			t.Fatal(err)
		}
		phaseEntered, release := make(chan int32, 1), make(chan struct{})
		phaseReturned := make(chan struct{})
		var phaseResult f.CommitResult
		phaseCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var once sync.Once
		unhold := func() { once.Do(func() { close(release) }) }
		go func() {
			defer close(phaseReturned)
			phaseResult = v.writeStage(phaseCtx, project, cause, func(pid int32) { phaseEntered <- pid; <-release })
		}()
		t.Cleanup(func() { cancel(); unhold(); await(t, phaseReturned) })
		var blocker int32
		select {
		case blocker = <-phaseEntered:
		case <-phaseReturned:
			t.Fatal("phase did not reach uncommitted EX boundary")
		case <-time.After(5 * time.Second):
			t.Fatal("phase EX not reached")
		}
		key, _ := f.ProjectLock(project.ID.String())
		lock := observeLock(v.tracked, key)
		t.Cleanup(func() { v.tracked.mu.Lock(); v.tracked.beforeLocks = nil; v.tracked.mu.Unlock() })
		stopped := make(chan struct{})
		var report pv.ProjectCallStopReport
		var stopErr error
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 20*time.Second)
		go func() { defer close(stopped); report, stopErr = v.stopper.RequestStop(stopCtx, actor, cause, scope) }()
		t.Cleanup(func() { cancelStop(); unhold(); await(t, stopped) })
		var attempt lockAttempt
		select {
		case attempt = <-lock:
		case <-stopped:
			t.Fatal("stop returned before held Project gate")
		case <-time.After(5 * time.Second):
			t.Fatal("stop did not request Project SH")
		}
		v.waitLock(t, attempt, false, blocker)
		call.requireCanceled(t, false)
		select {
		case <-stopped:
			t.Fatal("uncommitted phase accepted")
		default:
		}
		unhold()
		await(t, phaseReturned)
		if phaseResult.State() != f.Committed {
			t.Fatal("phase did not commit", phaseResult.Fault())
		}
		await(t, stopped)
		requireLocalStop(t, report, stopErr, cause, scope, 1)
		call.requireCanceled(t, true)
		call.join(t, false)
		r, err := v.stopper.InspectStop(ctxFor(t), actor, cause, scope)
		requireLocalStop(t, r, err, cause, scope, 0)
	})
}
