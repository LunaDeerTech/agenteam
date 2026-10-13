package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func workCleanupCheckpoint(t *testing.T, r lifecycleCleanupRound, stage string) c.CleanupCheckpoint {
	t.Helper()
	p := wc.ProjectCleanupProgress{ProjectID: r.claim.project, OperationID: r.cause.OperationID, ProjectVersion: r.cause.ProjectVersion, ProcessID: testID[oc.Process](t), InstanceID: testID[struct{}](t).String(), Stage: stage}
	cp, err := wc.NewProjectCleanupCheckpoint(p, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project})
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

func TestLifecycleCleanupWorkConstruction(t *testing.T) {
	d, _, s, _, _ := workCleanupFixture(t)
	for _, mode := range []string{"nil-life", "foreign-store", "nil-participant", "wrong-name", "invalid-process"} {
		t.Run(mode, func(t *testing.T) {
			store, life, processes, p := Store(s), d.state.authority, d.state.processes, d.state.work
			switch mode {
			case "nil-life":
				life = nil
			case "foreign-store":
				store = &stopRoundGateStore{}
			case "nil-participant":
				var absent *cleanupDriverPort
				p = absent
			case "wrong-name":
				p = &cleanupDriverPort{name: c.ArtifactObjectParticipant}
			case "invalid-process":
				processes = &testProcess{}
			}
			if _, err := NewLifecycleCleanupDriverForWork(store, life, processes, p); err == nil {
				t.Fatal("invalid fixed assembly accepted")
			}
		})
	}
	if _, err := NewWorkProjectCleanupAuthority(d.state.authority, &stopRoundGateStore{}); err == nil || s.transactions.Load() != 0 || len(s.seen()) != 0 {
		t.Fatal("constructor manufactured authority or I/O", err)
	}
}

func TestLifecycleCleanupWorkCheckpoint(t *testing.T) {
	_, _, _, r, _ := workCleanupFixture(t)
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project}
	for _, stage := range []string{"task_events", "task_blockers", "task_launch_failures", "task_busy_compensations", "task_scheduler_claims", "task_scheduler_relaunches", "task_blocker_commands", "task_transition_commands", "task_commands", "sprint_start_commands", "structure_commands", "tasks", "task_order_groups", "task_query_generations", "sprints", "sprint_order_groups", "milestones", "milestone_order_groups", "project_calls", "completed"} {
		cp := workCleanupCheckpoint(t, r, stage)
		got, err := wc.DecodeProjectCleanupCheckpoint(&cp, r.cause, scope)
		if err != nil || got.Stage != stage {
			t.Fatal("valid closed stage rejected", stage, err)
		}
	}
	cp := workCleanupCheckpoint(t, r, "project_calls")
	for _, mode := range []string{"project", "operation", "version", "process", "instance", "stage", "unknown-field", "duplicate-field", "schema", "completed-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(cp.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			raw, schema := cp.Bytes(), f.Version(1)
			var field, value string
			switch mode {
			case "project":
				field, value = "project_id", testID[i.Project](t).String()
			case "operation":
				field, value = "operation_id", testID[c.Operation](t).String()
			case "version":
				fields["project_version"] = json.RawMessage("999")
			case "process":
				field, value = "process_id", "invalid-process"
			case "instance":
				field, value = "instance_id", "invalid-instance"
			case "stage":
				field, value = "stage", "outbox"
			case "unknown-field":
				field, value = "success", "true"
			case "schema":
				schema = 2
			}
			if field != "" {
				v, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				fields[field] = v
			}
			if mode != "schema" && mode != "completed-mismatch" {
				var err error
				raw, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "duplicate-field" {
				raw = bytes.Replace(cp.Bytes(), []byte(`"stage":"project_calls"`), []byte(`"stage":"project_calls","stage":"project_calls"`), 1)
			}
			changed, err := c.NewCleanupCheckpoint(c.WorkParticipant, r.cause, scope, schema, raw)
			if err != nil {
				t.Fatal("valid envelope did not reach Work decoder", err)
			}
			if mode == "completed-mismatch" {
				report, err := c.NewCleanupReport(c.WorkParticipant, r.cause, scope, c.CleanupDetails{State: c.CleanupCompleted, Checkpoint: &changed})
				if err != nil {
					t.Fatal(err)
				}
				if cleanupRoundReport(r, report) == nil {
					t.Fatal("nonterminal checkpoint completed Work")
				}
			} else if _, err = wc.DecodeProjectCleanupCheckpoint(&changed, r.cause, scope); err == nil {
				t.Fatal("invalid checkpoint accepted")
			}
		})
	}
}

func TestLifecycleCleanupWorkProviderOriginalReturn(t *testing.T) {
	for _, mode := range []string{"pending", "completed", "cancelled", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, _, _, r, _ := workCleanupFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			var scopeContext context.Context
			original := commitError(f.UnknownResult(testID[f.TransactionAttempt](t), r.claim.cause()))
			cp := workCleanupCheckpoint(t, r, "project_calls")
			if mode == "completed" {
				cp = workCleanupCheckpoint(t, r, "completed")
			}
			d.state.work = &cleanupDriverPort{name: c.WorkParticipant, call: func(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef, prior *c.CleanupCheckpoint) (c.CleanupReport, error) {
				scopeContext = ctx
				if actor.Details().ServiceName != i.ProjectLifecycle || cause != r.cause || target.ProjectID != r.claim.project || prior != nil {
					t.Error("changed provider input")
				}
				close(entered)
				<-release
				if mode == "unknown" {
					return c.CleanupReport{}, original
				}
				state := c.CleanupPending
				if mode == "completed" {
					state = c.CleanupCompleted
				}
				return c.NewCleanupReport(c.WorkParticipant, cause, target, c.CleanupDetails{State: state, Checkpoint: &cp})
			}}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					<-done
				}
			})
			go func() { _, err := d.state.invoke(ctx, r); done <- err }()
			retirementPrepAwait(t, entered)
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				joined = true
				t.Fatal("provider return invented")
			default:
			}
			free()
			err := <-done
			joined = true
			if mode == "unknown" && !errors.Is(err, original) || mode == "cancelled" && !errors.Is(err, context.Canceled) || (mode == "pending" || mode == "completed") && err != nil {
				t.Fatal("provider original result changed", err)
			}
			w, _ := scopeContext.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
			private, _ := scopeContext.Value(workCleanupScopeKey{}).(*lifecycleCleanupScope)
			if w == nil || w.open || private != w {
				t.Fatal("saved provider context retained authority")
			}
		})
	}
}

// A controlled retained round enters the real Run recovery branch. Its original
// claim reads are real synchronous calls; canonical SQL stops at a sentinel, so
// this does not claim a successful provider confirmation or physical COMMIT.
func TestLifecycleCleanupWorkRetainedRound(t *testing.T) {
	for _, mode := range []string{"canonical-error", "unconfirmed-committed", "new-unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, _, s, r, _ := workCleanupFixture(t)
			original := f.UnknownResult(testID[f.TransactionAttempt](t), r.claim.cause())
			r.workUnknown, r.workError = true, commitError(original)
			d.state.owners[r.claim.operation] = lifecycleCleanupOwner{project: r.claim.project, claim: &r.claim, work: &workCleanupRound{round: r, registered: true}}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				actual, expected := cause.Details(), r.claim.cause().Details()
				if actual.Kind != expected.Kind || actual.JobType != expected.JobType || actual.JobID != expected.JobID || actual.JobAttemptID != expected.JobAttemptID {
					t.Error("changed original confirmation cause")
				}
				result := f.CommittedResult()
				if mode == "canonical-error" {
					err := fn(ctx, s.tx)
					if !errors.Is(err, s.readErr) {
						t.Error("did not reach exact canonical read", err)
					}
					result = f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
				} else if mode == "new-unknown" {
					result = f.UnknownResult(testID[f.TransactionAttempt](t), cause)
				}
				close(entered)
				<-release
				return result
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					<-done
				}
			})
			go func() { done <- d.Run(ctx, r.claim.project, r.claim.operation) }()
			retirementPrepAwait(t, entered)
			other, err := NewLifecycleCleanupDriverForWork(s, d.state.authority, d.state.processes, d.state.work)
			if err != nil {
				t.Fatal(err)
			}
			hasCode(t, other.Run(context.Background(), r.claim.project, r.claim.operation), f.ResourceBusy)
			if err = other.state.canJoin(context.Background(), &r.claim); err == nil {
				t.Fatal("another driver borrowed retained claim")
			}
			b, err := d.state.authority.beginCleanupCall(context.Background(), testID[i.Project](t), nil)
			if err != nil {
				t.Fatal("unrelated B blocked", err)
			}
			b()
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced original confirmation Store return")
			default:
			}
			free()
			err = <-done
			joined = true
			if !errors.Is(err, r.workError) {
				t.Fatal("lost first provider Unknown", err)
			}
			kept := d.state.owners[r.claim.operation]
			if kept.work == nil || kept.work.round.claim != r.claim || kept.work.round.workError != r.workError || len(d.state.calls) != 0 || s.transactions.Load() != 1 || slices.Contains(s.seen(), "forbidden-write") {
				t.Fatal("confirmation mutated or replaced retained round")
			}
		})
	}
}

// The full canonical SQL remains a sentinel. These tests exercise the actual
// Run registration/checkpoint Store calls returning rollback ACK Unknown, then
// the original read-only confirmation call and its real return barrier. They
// do not fabricate a successful full-manifest SQL fixture or physical COMMIT.
func TestLifecycleCleanupWorkProjectUnknownReturn(t *testing.T) {
	for _, mode := range []string{"registration-absent", "registration-terminal", "checkpoint-pending", "checkpoint-completed"} {
		t.Run(mode, func(t *testing.T) {
			d, _, s, r, _ := workCleanupFixture(t)
			w := &workCleanupRound{round: r, before: &workCleanupImage{rows: []byte(`{"prior":true}`)}}
			if mode == "registration-terminal" {
				previous := r.claim
				previous.phase = "terminal"
				// Use a valid older fence; the prepared new round is the second attempt.
				previous.fence = 1
				w.round.claim.fence = 2
				w.round.previous, w.before.claim = &previous, &previous
				s.claim = &previous
			} else {
				s.claimMissing = true
			}
			stage := "project_calls"
			if mode == "checkpoint-completed" {
				stage = "completed"
			}
			cp := workCleanupCheckpoint(t, w.round, stage)
			state := c.CleanupPending
			if stage == "completed" {
				state = c.CleanupCompleted
			}
			report, err := c.NewCleanupReport(c.WorkParticipant, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project}, c.CleanupDetails{State: state, Checkpoint: &cp})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "checkpoint-pending" || mode == "checkpoint-completed" {
				w.registered, w.report = true, &report
				w.registeredImage = &workCleanupImage{claim: &w.round.claim, rows: []byte(`{"registered":true}`)}
			}
			d.state.owners[r.claim.operation] = lifecycleCleanupOwner{project: r.claim.project, work: w}
			invoked := 0
			d.state.work = &cleanupDriverPort{name: c.WorkParticipant, call: func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
				invoked++
				return c.CleanupReport{}, errors.New("unexpected provider")
			}}
			original := f.UnknownResult(testID[f.TransactionAttempt](t), w.round.claim.cause())
			// This round reads the operation first; the inherited fixture must not
			// decode that row as its optional first-claim projection.
			s.claim, s.claimMissing = nil, false
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				if err := fn(ctx, s.tx); !errors.Is(err, s.readErr) {
					t.Error("did not reach original canonical SQL", err)
				}
				return original
			}
			err = d.Run(context.Background(), r.claim.project, r.claim.operation)
			requireFinalizeUnknown(t, err, original)
			if s.transactions.Load() != 1 || invoked != 0 || d.state.owners[r.claim.operation].work != w || d.state.authority.cleanupCalls.works[r.claim.project] != w {
				t.Fatal("original transaction round was not retained")
			}
			registration := mode == "registration-absent" || mode == "registration-terminal"
			if registration && w.registrationUnknown == nil || !registration && w.checkpointUnknown == nil {
				t.Fatal("wrong Unknown phase")
			}
			other, err := NewLifecycleCleanupDriverForWork(s, d.state.authority, d.state.processes, d.state.work)
			if err != nil {
				t.Fatal(err)
			}
			hasCode(t, other.Run(context.Background(), r.claim.project, r.claim.operation), f.ResourceBusy)
			if s.transactions.Load() != 1 {
				t.Fatal("second driver reached SQL after original Run returned")
			}
			b, err := d.state.authority.beginCleanupCall(context.Background(), testID[i.Project](t))
			if err != nil {
				t.Fatal("B blocked", err)
			}
			b()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				got, want := cause.Details(), original.Cause().Details()
				if got.Kind != want.Kind || got.JobType != want.JobType || got.JobID != want.JobID || got.JobAttemptID != want.JobAttemptID {
					t.Error("confirmation changed original cause")
				}
				err := fn(ctx, s.tx)
				if !errors.Is(err, s.readErr) {
					t.Error("confirmation skipped original current gate", err)
				}
				result := f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
				close(entered)
				<-release
				return result
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					<-done
				}
			})
			go func() { done <- d.Run(ctx, r.claim.project, r.claim.operation) }()
			retirementPrepAwait(t, entered)
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced original confirmation return")
			default:
			}
			hasCode(t, other.Run(context.Background(), r.claim.project, r.claim.operation), f.ResourceBusy)
			if !registration && w.report != &report {
				t.Fatal("accepted provider report lost while Store held")
			}
			free()
			err = <-done
			joined = true
			requireFinalizeUnknown(t, err, original)
			if invoked != 0 || s.transactions.Load() != 2 || slices.Contains(s.seen(), "forbidden-write") || d.state.owners[r.claim.operation].work != w {
				t.Fatal("confirmation wrote, invoked, or replaced round")
			}
			hasCode(t, other.Run(context.Background(), r.claim.project, r.claim.operation), f.ResourceBusy)
		})
	}
}

func TestLifecycleCleanupWorkKnownCheckpointRollback(t *testing.T) {
	d, _, s, r, _ := workCleanupFixture(t)
	cp := workCleanupCheckpoint(t, r, "task_commands")
	report, err := c.NewCleanupReport(c.WorkParticipant, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project}, c.CleanupDetails{State: c.CleanupPending, Checkpoint: &cp})
	if err != nil {
		t.Fatal(err)
	}
	w := &workCleanupRound{round: r, registered: true, report: &report, registeredImage: &workCleanupImage{claim: &r.claim, rows: []byte(`{"registered":true}`)}}
	d.state.owners[r.claim.operation] = lifecycleCleanupOwner{project: r.claim.project, work: w}
	d.state.work = &cleanupDriverPort{name: c.WorkParticipant, call: func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
		t.Error("repeated original provider")
		return c.CleanupReport{}, nil
	}}
	// Checkpoint validation starts with the operation, not a claim row.
	s.claim, s.claimMissing = nil, false
	s.within = func(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
		err := fn(ctx, s.tx)
		if !errors.Is(err, s.readErr) {
			t.Error("checkpoint did not reach current gate", err)
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	for n := 0; n < 2; n++ {
		if err = d.Run(context.Background(), r.claim.project, r.claim.operation); !errors.Is(err, s.readErr) {
			t.Fatal(err)
		}
		if d.state.owners[r.claim.operation].work != w || w.report != &report || w.checkpointUnknown != nil || w.registrationUnknown != nil || !w.registered {
			t.Fatal("known rollback lost accepted report or invented Unknown")
		}
	}
	if s.transactions.Load() != 2 || slices.Contains(s.seen(), "forbidden-write") {
		t.Fatal("retry performed unrelated work")
	}
}

func TestLifecycleCleanupWorkInvalidOriginalUnknown(t *testing.T) {
	for _, mode := range []string{"zero-result", "foreign-cause", "committed-without-callback"} {
		t.Run(mode, func(t *testing.T) {
			d, _, s, r, _ := workCleanupFixture(t)
			w := &workCleanupRound{round: r, before: &workCleanupImage{rows: []byte(`{"prior":true}`)}}
			d.state.owners[r.claim.operation] = lifecycleCleanupOwner{project: r.claim.project, work: w}
			s.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
				if mode == "committed-without-callback" {
					return f.CommittedResult()
				}
				if mode == "foreign-cause" {
					cause, e := f.NewJobCause("foreign", testID[struct{}](t).String(), testID[struct{}](t).String())
					if e != nil {
						t.Fatal(e)
					}
					return f.UnknownResult(testID[f.TransactionAttempt](t), cause)
				}
				return f.CommitResult{}
			}
			if err := d.Run(context.Background(), r.claim.project, r.claim.operation); err == nil {
				t.Fatal("unconfirmed Store result granted registration")
			}
			if !w.invalidResult || w.registrationUnknown != nil || w.registered {
				t.Fatal("invalid result became a raw Unknown or Committed proof")
			}
			if err := d.Run(context.Background(), r.claim.project, r.claim.operation); err == nil || s.transactions.Load() != 1 {
				t.Fatal("invalid result authorized a later write", err)
			}
		})
	}
}

func TestLifecycleCleanupWorkOriginalImages(t *testing.T) {
	_, _, _, r, _ := workCleanupFixture(t)
	before := &workCleanupImage{claim: &r.claim, rows: []byte(`{"operation":{"version":4},"participant":{"version":2,"attempt_count":1,"checkpoint":"original","safe_pending_refs":[],"next_attempt_at":null}}`)}
	for _, mode := range []string{"same", "nil-before", "nil-after", "missing-claim", "phase", "fence", "attempt", "process", "operation-row", "checkpoint", "refs", "next-at"} {
		t.Run(mode, func(t *testing.T) {
			claim := r.claim
			actual := &workCleanupImage{claim: &claim, rows: bytes.Clone(before.rows)}
			expected := before
			switch mode {
			case "nil-before":
				expected = nil
			case "nil-after":
				actual = nil
			case "missing-claim":
				actual.claim = nil
			case "phase":
				claim.phase = "terminal"
			case "fence":
				claim.fence++
			case "attempt":
				claim.attempt = testID[struct{}](t).String()
			case "process":
				claim.process = testID[ob.Process](t)
			case "operation-row":
				actual.rows = bytes.Replace(actual.rows, []byte(`"version":4`), []byte(`"version":5`), 1)
			case "checkpoint":
				actual.rows = bytes.Replace(actual.rows, []byte(`"original"`), []byte(`"foreign"`), 1)
			case "refs":
				actual.rows = bytes.Replace(actual.rows, []byte(`"safe_pending_refs":[]`), []byte(`"safe_pending_refs":["changed"]`), 1)
			case "next-at":
				actual.rows = bytes.Replace(actual.rows, []byte(`"next_attempt_at":null`), []byte(`"next_attempt_at":"later"`), 1)
			}
			if sameWorkCleanupImage(actual, expected) != (mode == "same") {
				t.Fatal("partial image accepted", mode)
			}
		})
	}
}

func TestLifecycleCleanupWorkRetainedCapacity(t *testing.T) {
	d, _, _, r, _ := workCleanupFixture(t)
	a := d.state.authority
	rounds := make([]*workCleanupRound, 4)
	for n := range rounds {
		p := testID[i.Project](t)
		w := &workCleanupRound{round: r}
		w.round.claim.project = p
		rounds[n] = w
		end, err := a.beginCleanupCall(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		if err = a.retainWorkCleanup(w); err != nil {
			t.Fatal(err)
		}
		end()
	}
	_, err := a.beginCleanupCall(context.Background(), testID[i.Project](t))
	hasCode(t, err, f.ResourceBusy)
	w := rounds[0]
	_, err = a.beginCleanupOwnedCall(context.Background(), w.round.claim.project, nil, nil, &workCleanupRound{round: w.round})
	hasCode(t, err, f.ResourceBusy)
	end, err := a.beginCleanupOwnedCall(context.Background(), w.round.claim.project, nil, nil, w)
	if err != nil {
		t.Fatal("original pointer blocked at capacity", err)
	}
	end()
	a.releaseWorkCleanup(w)
	end, err = a.beginCleanupCall(context.Background(), testID[i.Project](t))
	if err != nil {
		t.Fatal("settled slot leaked", err)
	}
	end()
	for _, w = range rounds[1:] {
		a.releaseWorkCleanup(w)
	}
}

// Control the private completion phase after its readonly callback. This is
// deliberately not a positive canonical SQL fixture: it verifies that a valid
// observation cannot retire the first Unknown before Store's original return
// or when that outer context was cancelled while the return was held.
func TestLifecycleCleanupWorkConfirmationAcceptance(t *testing.T) {
	for _, mode := range []string{"registration-applied", "registration-not-applied", "checkpoint-applied", "checkpoint-not-applied", "cancelled-registration", "cancelled-checkpoint", "unvalidated", "new-unknown"} {
		t.Run(mode, func(t *testing.T) {
			_, _, s, r, _ := workCleanupFixture(t)
			original := f.UnknownResult(testID[f.TransactionAttempt](t), r.claim.cause())
			w := &workCleanupRound{round: r, registrationUnknown: &original, checkpointUnknown: &original, registeredImage: &workCleanupImage{rows: []byte(`{"registered":true}`)}, finishedImage: &workCleanupImage{rows: []byte(`{"finished":true}`)}}
			registration := mode == "registration-applied" || mode == "registration-not-applied" || mode == "cancelled-registration"
			applied := mode == "registration-applied" || mode == "checkpoint-applied"
			valid := mode != "unvalidated"
			cancelled := mode == "cancelled-registration" || mode == "cancelled-checkpoint"
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				if err := fn(ctx, s.tx); err != nil {
					t.Error(err)
				}
				result := f.CommittedResult()
				if mode == "new-unknown" {
					result = f.UnknownResult(testID[f.TransactionAttempt](t), cause)
				}
				close(entered)
				<-release
				return result
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					<-done
				}
			})
			go func() {
				result := s.WithinTx(ctx, original.Cause(), func(context.Context, f.Tx) error { return nil })
				got, err := w.acceptWorkConfirmation(ctx, registration, result, valid, applied)
				if err == nil && got != applied {
					t.Error("observation result changed")
				}
				done <- err
			}()
			retirementPrepAwait(t, entered)
			if cancelled {
				cancel()
			}
			if w.registrationUnknown != &original || w.checkpointUnknown != &original {
				t.Fatal("physical COMMIT replaced original return")
			}
			select {
			case <-done:
				joined = true
				t.Fatal("completed before original Store return")
			default:
			}
			free()
			err := <-done
			joined = true
			if cancelled || !valid || mode == "new-unknown" {
				requireFinalizeUnknown(t, err, original)
				if cancelled && !errors.Is(err, context.Canceled) {
					t.Fatal("lost caller cancellation", err)
				}
				if w.registrationUnknown != &original || w.checkpointUnknown != &original {
					t.Fatal("unconfirmed/cancelled observation released original Unknown")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if registration {
					if w.registrationUnknown != nil || w.registered != applied || w.checkpointUnknown != &original || !applied && w.registeredImage != nil {
						t.Fatal("registration observation changed another phase")
					}
				} else if w.checkpointUnknown != nil || w.registrationUnknown != &original || !applied && w.finishedImage != nil {
					t.Fatal("checkpoint observation changed another phase")
				}
			}
			if len(s.seen()) != 0 {
				t.Fatal("completion phase performed SQL")
			}
		})
	}
}
