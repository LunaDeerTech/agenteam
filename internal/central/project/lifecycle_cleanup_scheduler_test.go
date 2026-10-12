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
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/scheduler/contract"
)

func schedulerCleanupCheckpoint(t *testing.T, r lifecycleCleanupRound, stage string) c.CleanupCheckpoint {
	t.Helper()
	p := sc.ProjectCleanupProgress{ProjectID: r.claim.project, OperationID: r.cause.OperationID, ProjectVersion: r.cause.ProjectVersion, ProcessID: testID[oc.Process](t), InstanceID: testID[struct{}](t).String(), Stage: stage}
	cp, err := sc.NewProjectCleanupCheckpoint(p, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project})
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

func TestLifecycleCleanupSchedulerConstruction(t *testing.T) {
	d, _, s, _, _ := schedulerCleanupFixture(t)
	for _, mode := range []string{"nil-life", "foreign-store", "nil-participant", "wrong-name", "invalid-process"} {
		t.Run(mode, func(t *testing.T) {
			store, life, processes, p := Store(s), d.state.authority, d.state.processes, d.state.scheduler
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
			if _, err := NewLifecycleCleanupDriverForScheduler(store, life, processes, p); err == nil {
				t.Fatal("invalid fixed assembly accepted")
			}
		})
	}
	if _, err := NewSchedulerProjectCleanupAuthority(d.state.authority, &stopRoundGateStore{}); err == nil || s.transactions.Load() != 0 || len(s.seen()) != 0 {
		t.Fatal("constructor manufactured authority or I/O", err)
	}
}

func TestLifecycleCleanupSchedulerCheckpoint(t *testing.T) {
	_, _, _, r, _ := schedulerCleanupFixture(t)
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project}
	for _, stage := range []string{"runtimes", "relaunch_visits", "dispatches", "calls", "completed"} {
		cp := schedulerCleanupCheckpoint(t, r, stage)
		got, err := sc.DecodeProjectCleanupCheckpoint(&cp, r.cause, scope)
		if err != nil || got.Stage != stage {
			t.Fatal("valid closed stage rejected", stage, err)
		}
	}
	cp := schedulerCleanupCheckpoint(t, r, "calls")
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
				raw = bytes.Replace(cp.Bytes(), []byte(`"stage":"calls"`), []byte(`"stage":"calls","stage":"calls"`), 1)
			}
			changed, err := c.NewCleanupCheckpoint(c.SchedulerParticipant, r.cause, scope, schema, raw)
			if err != nil {
				t.Fatal("valid envelope did not reach Scheduler decoder", err)
			}
			if mode == "completed-mismatch" {
				report, err := c.NewCleanupReport(c.SchedulerParticipant, r.cause, scope, c.CleanupDetails{State: c.CleanupCompleted, Checkpoint: &changed})
				if err != nil {
					t.Fatal(err)
				}
				if cleanupRoundReport(r, report) == nil {
					t.Fatal("nonterminal checkpoint completed Scheduler")
				}
			} else if _, err = sc.DecodeProjectCleanupCheckpoint(&changed, r.cause, scope); err == nil {
				t.Fatal("invalid checkpoint accepted")
			}
		})
	}
}

func TestLifecycleCleanupSchedulerProviderOriginalReturn(t *testing.T) {
	for _, mode := range []string{"pending", "completed", "cancelled", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, _, _, r, _ := schedulerCleanupFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			var scopeContext context.Context
			original := commitError(f.UnknownResult(testID[f.TransactionAttempt](t), r.claim.cause()))
			cp := schedulerCleanupCheckpoint(t, r, "calls")
			if mode == "completed" {
				cp = schedulerCleanupCheckpoint(t, r, "completed")
			}
			d.state.scheduler = &cleanupDriverPort{name: c.SchedulerParticipant, call: func(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef, prior *c.CleanupCheckpoint) (c.CleanupReport, error) {
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
				return c.NewCleanupReport(c.SchedulerParticipant, cause, target, c.CleanupDetails{State: state, Checkpoint: &cp})
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
			if w == nil || w.open {
				t.Fatal("saved provider context retained authority")
			}
		})
	}
}

// A controlled retained round enters the real Run recovery branch. Its original
// claim reads are real synchronous calls; canonical SQL stops at a sentinel, so
// this does not claim a successful provider confirmation or physical COMMIT.
func TestLifecycleCleanupSchedulerRetainedRound(t *testing.T) {
	for _, mode := range []string{"canonical-error", "unconfirmed-committed", "new-unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, _, s, r, _ := schedulerCleanupFixture(t)
			original := f.UnknownResult(testID[f.TransactionAttempt](t), r.claim.cause())
			r.schedulerUnknown, r.schedulerError = true, commitError(original)
			d.state.owners[r.claim.operation] = lifecycleCleanupOwner{project: r.claim.project, claim: &r.claim, scheduler: &r}
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
					result = f.NotCommittedResult(err)
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
			other, err := NewLifecycleCleanupDriverForScheduler(s, d.state.authority, d.state.processes, d.state.scheduler)
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
			if !errors.Is(err, r.schedulerError) {
				t.Fatal("lost first provider Unknown", err)
			}
			kept := d.state.owners[r.claim.operation]
			if kept.scheduler == nil || kept.scheduler.claim != r.claim || kept.scheduler.schedulerError != r.schedulerError || len(d.state.calls) != 0 || s.transactions.Load() != 1 || slices.Contains(s.seen(), "forbidden-write") {
				t.Fatal("confirmation mutated or replaced retained round")
			}
		})
	}
}
