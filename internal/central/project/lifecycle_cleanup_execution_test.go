package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Only construction and early rejection use this port. Positive provider
// authorization/retirement is the concrete owner and real PG suite's job.
type executionRetirementShapePort struct {
	ec.ProjectExecutionRetirementConsumer
	identity ec.ProjectExecutionRetirementIdentity
}

func (p *executionRetirementShapePort) ProjectExecutionRetirementIdentity() ec.ProjectExecutionRetirementIdentity {
	return p.identity
}

func executionRetirementRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	r := schedulerCleanupRecord(t)
	entries := r.manifest.Entries()
	entries = append(entries, c.ParticipantRegistration{Name: c.ExecutionParticipant, ContractVersion: 1, OwnerModule: "execution", CleanupAfter: []c.ParticipantName{c.SchedulerParticipant}})
	for n := range entries {
		if entries[n].Name == c.OutboxParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.ExecutionParticipant)
		}
	}
	r.manifest = registryTestManifest(t, entries)
	r.participants = append(r.participants, lifecycleParticipantRecord{name: c.ExecutionParticipant, version: 1, stop: "stopped", cleanup: "required"})
	skillsCleanupSet(r, c.SchedulerParticipant, "stopped", "completed")
	return r
}

func executionRetirementFixture(t *testing.T) (*LifecycleCleanupDriver, *ExecutionProjectRetirementAuthority, *artifactSourceStopStore, *executionCleanupRound, i.Actor) {
	t.Helper()
	d, _, s, r, actor := schedulerCleanupFixture(t)
	life, err := NewLifecycleAuthority(s, executionRetirementRecord(t).manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	port := &executionRetirementShapePort{identity: ec.ProjectExecutionRetirementIdentity{ProcessID: d.state.process.String(), InstanceID: testID[struct{}](t).String()}}
	driver, err := NewLifecycleCleanupDriverForExecutions(s, life, d.state.processes, port)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewExecutionProjectRetirementAuthority(life, s)
	if err != nil {
		t.Fatal(err)
	}
	r.participant = c.ExecutionParticipant
	// This gate reaches canonical lifecycle SQL before reading its claim. Keep
	// the controlled first Scan an error rather than a five-column claim row.
	s.claim = nil
	return driver, gate, s, &executionCleanupRound{round: r, registered: true}, actor
}

func TestExecutionRetirementConstruction(t *testing.T) {
	d, a, s, _, _ := executionRetirementFixture(t)
	for _, mode := range []string{"nil-life", "foreign-store", "nil-consumer", "invalid-identity", "invalid-process", "gate-store"} {
		t.Run(mode, func(t *testing.T) {
			store, life, processes, consumer := Store(s), d.state.authority, d.state.processes, d.state.executions
			switch mode {
			case "nil-life":
				life = nil
			case "foreign-store":
				store = &stopRoundGateStore{}
			case "nil-consumer":
				consumer = (*executionRetirementShapePort)(nil)
			case "invalid-identity":
				consumer = &executionRetirementShapePort{}
			case "invalid-process":
				processes = &testProcess{}
			case "gate-store":
				if _, err := NewExecutionProjectRetirementAuthority(a.authority, &stopRoundGateStore{}); err == nil {
					t.Fatal("foreign Store accepted")
				}
				return
			}
			if _, err := NewLifecycleCleanupDriverForExecutions(store, life, processes, consumer); err == nil {
				t.Fatal("invalid assembly accepted")
			}
		})
	}
	if s.transactions.Load() != 0 || len(s.seen()) != 0 {
		t.Fatal("constructor performed SQL")
	}
}

func TestExecutionRetirementProgress(t *testing.T) {
	for _, state := range []string{"required", "pending", "completed"} {
		t.Run(state, func(t *testing.T) {
			r := executionRetirementRecord(t)
			skillsCleanupSet(r, c.ExecutionParticipant, "stopped", state)
			if err := executionCleanupProgress(r, true); err != nil {
				t.Fatal(err)
			}
			if state == "completed" && executionCleanupProgress(r, false) == nil {
				t.Fatal("completed replay became mutation")
			}
			from, to, err := cleanupStages(c.ExecutionParticipant)
			if err != nil || from != "domains" || to != "domains" {
				t.Fatal("parent advanced")
			}
		})
	}
	for _, mode := range []string{"archive", "stopping", "outbox", "stop-pending", "scheduler-pending", "missing", "duplicate", "foreign-row", "failed", "undeclared-scheduler"} {
		t.Run(mode, func(t *testing.T) {
			r := executionRetirementRecord(t)
			switch mode {
			case "archive":
				r.operation.Action = c.Archive
			case "stopping":
				r.operation.State = c.OperationStopping
			case "outbox":
				r.cleanupStage = "outbox"
			case "stop-pending":
				r.participants[0].stop = "pending"
			case "scheduler-pending":
				skillsCleanupSet(r, c.SchedulerParticipant, "stopped", "pending")
			case "missing":
				r.participants = r.participants[1:]
			case "duplicate":
				r.participants = append(r.participants, r.participants[0])
			case "foreign-row":
				r.participants[0].name = "foreign"
			case "failed":
				skillsCleanupSet(r, c.ExecutionParticipant, "stopped", "failed")
			case "undeclared-scheduler":
				entries := r.manifest.Entries()
				for n := range entries {
					if entries[n].Name == c.ExecutionParticipant {
						entries[n].CleanupAfter = nil
					}
				}
				r.manifest = registryTestManifest(t, entries)
			}
			if executionCleanupProgress(r, true) == nil {
				t.Fatal("invalid prerequisites accepted")
			}
		})
	}
}

func executionRetirementTestCheckpoint(t *testing.T, e *executionCleanupRound, stage string) executionRetirementCheckpoint {
	t.Helper()
	r := e.round
	v := executionRetirementCheckpoint{Stage: stage, Commit: ec.ProjectExecutionRetirementCommit{ProjectID: r.claim.project, OperationID: r.claim.operation, ProjectVersion: r.cause.ProjectVersion, ProcessID: r.claim.process.String(), AttemptID: r.claim.attempt, Fence: r.claim.fence, TransactionID: "12345"}, Owner: ec.ProjectExecutionRetirementIdentity{ProcessID: r.claim.process.String(), InstanceID: testID[struct{}](t).String()}}
	if stage == "applied" {
		v.Binding = &ec.ProjectExecutionRetirementBinding{ProjectID: r.claim.project, AgentID: testID[i.Agent](t), ExecutionID: testID[i.Execution](t), Version: 1, CanonicalDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	}
	return v
}

func TestExecutionRetirementCheckpoint(t *testing.T) {
	_, _, _, e, _ := executionRetirementFixture(t)
	for _, stage := range []string{"applied", "empty"} {
		t.Run(stage, func(t *testing.T) {
			v := executionRetirementTestCheckpoint(t, e, stage)
			report, err := executionRetirementReport(e.round, v)
			if err != nil {
				t.Fatal(err)
			}
			cp := report.Details().Checkpoint
			got, err := decodeExecutionRetirementCheckpoint(cp, e.round.cause, e.round.claim.project)
			if err != nil || got.Stage != stage || executionRetirementRoundReport(e.round, report) != nil {
				t.Fatal("valid original checkpoint rejected", err)
			}
			if report.Details().State == c.CleanupCompleted && stage != "empty" {
				t.Fatal("one E completed whole participant")
			}
		})
	}
	for _, mode := range []string{"project", "operation", "version", "process", "attempt", "fence", "transaction", "owner", "binding", "stage", "schema", "unknown-field", "duplicate", "noncanonical"} {
		t.Run(mode, func(t *testing.T) {
			v := executionRetirementTestCheckpoint(t, e, "applied")
			schema := f.Version(1)
			switch mode {
			case "project":
				v.Commit.ProjectID = testID[i.Project](t)
			case "operation":
				v.Commit.OperationID = testID[c.Operation](t)
			case "version":
				v.Commit.ProjectVersion++
			case "process":
				v.Commit.ProcessID = "invalid"
			case "attempt":
				v.Commit.AttemptID = "invalid"
			case "fence":
				v.Commit.Fence = 0
			case "transaction":
				v.Commit.TransactionID = "01"
			case "owner":
				v.Owner.InstanceID = "invalid"
			case "binding":
				v.Binding.ProjectID = testID[i.Project](t)
			case "stage":
				v.Stage = "batch"
			case "schema":
				schema = 2
			}
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "unknown-field":
				raw = append([]byte(`{"extra":true,`), raw[1:]...)
			case "duplicate":
				raw = bytes.Replace(raw, []byte(`"stage":"applied"`), []byte(`"stage":"applied","stage":"applied"`), 1)
			case "noncanonical":
				raw = append([]byte(" "), raw...)
			}
			cp, err := c.NewCleanupCheckpoint(c.ExecutionParticipant, e.round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: e.round.claim.project}, schema, raw)
			if err != nil {
				t.Fatal("envelope prevented decoder assertion", err)
			}
			if _, err = decodeExecutionRetirementCheckpoint(&cp, e.round.cause, e.round.claim.project); err == nil {
				t.Fatal("invalid atomic checkpoint accepted")
			}
		})
	}
}

func TestExecutionRetirementPrivateFamily(t *testing.T) {
	for _, mode := range []string{"public", "closed", "old-stop", "old-cleanup", "foreign-life", "wrong-participant", "agent", "terminal", "wrong-cause", "archive", "wrong-tx", "expired", "nil-executor", "no-project-lock", "no-schedule-lock", "shared-schedule", "original-scan"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, e, actor := executionRetirementFixture(t)
			w := &executionRetirementScope{state: d.state, round: e, open: true}
			ctx := context.WithValue(context.Background(), executionRetirementScopeKey{}, w)
			tx := s.tx
			switch mode {
			case "public":
				ctx = context.Background()
			case "closed":
				w.open = false
			case "old-stop":
				ctx = context.WithValue(context.Background(), executionStopLoanKey{}, &executionStopLoan{})
			case "old-cleanup":
				ctx = context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, &lifecycleCleanupScope{state: d.state, round: e.round, open: true})
			case "foreign-life":
				_, a, _, _, _ = executionRetirementFixture(t)
			case "wrong-participant":
				e.round.participant = c.SchedulerParticipant
			case "agent":
				e.round.agentStep = true
			case "terminal":
				e.round.terminalStep = true
			case "wrong-cause":
				e.round.cause.ProjectVersion++
			case "archive":
				e.round.cause.Action = c.Archive
			case "wrong-tx":
				tx = f.NewTx()
			case "expired":
				s.live = false
			case "nil-executor":
				s.nilExecutor = true
			case "no-project-lock":
				delete(s.held, projectLock(e.round.claim.project, f.Exclusive).Key.Canonical())
			case "no-schedule-lock":
				delete(s.held, schedulerLock(e.round.claim.project).Key.Canonical())
			case "shared-schedule":
				s.held[schedulerLock(e.round.claim.project).Key.Canonical()] = f.Shared
			}
			cause := e.round.cause
			if mode == "wrong-cause" {
				cause.ProjectVersion--
			}
			err := a.RequireProjectExecutionCleanupInTx(ctx, tx, actor, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: e.round.claim.project})
			if err == nil {
				t.Fatal("invalid scope accepted")
			}
			if mode == "original-scan" && !errors.Is(err, s.readErr) {
				t.Fatal("valid private scope failed before real Scan", err)
			}
		})
	}
}

func TestExecutionRetirementOriginalScanReturn(t *testing.T) {
	d, a, s, e, actor := executionRetirementFixture(t)
	s.readEntered = make(chan struct{})
	s.readRelease = make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(s.readRelease) }) }
	t.Cleanup(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &executionRetirementScope{state: d.state, round: e, open: true}
	ctx = context.WithValue(ctx, executionRetirementScopeKey{}, w)
	done := make(chan error, 1)
	go func() {
		done <- a.RequireProjectExecutionCleanupInTx(ctx, s.tx, actor, e.round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: e.round.claim.project})
	}()
	retirementPrepAwait(t, s.readEntered)
	cancel()
	if w.mu.TryLock() {
		w.mu.Unlock()
		t.Fatal("scope retired before original Scan")
	}
	select {
	case <-done:
		t.Fatal("cancel replaced original Scan return")
	default:
	}
	release()
	if err := <-done; !errors.Is(err, s.readErr) {
		t.Fatal("original error changed", err)
	}
}

func TestExecutionRetirementRetainsOriginalAdmission(t *testing.T) {
	a := lifecycleTestAuthority(t)
	p := testID[i.Project](t)
	ctx := context.Background()
	release, err := a.beginCleanupCall(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	e := &executionCleanupRound{round: lifecycleCleanupRound{claim: lifecycleStopClaim{project: p}}}
	if err = a.retainExecutionCleanup(e); err != nil {
		t.Fatal(err)
	}
	release()
	_, err = a.beginCleanupCall(ctx, p)
	hasCode(t, err, f.ResourceBusy)
	_, err = a.beginCleanupOwnedCall(ctx, p, nil, &executionCleanupRound{})
	hasCode(t, err, f.ResourceBusy)
	other, err := a.beginCleanupCall(ctx, testID[i.Project](t))
	if err != nil {
		t.Fatal(err)
	}
	other()
	original, err := a.beginCleanupOwnedCall(ctx, p, nil, e)
	if err != nil {
		t.Fatal(err)
	}
	a.releaseExecutionCleanup(&executionCleanupRound{round: e.round})
	if a.cleanupCalls.executions[p] != e {
		t.Fatal("foreign round released original")
	}
	a.releaseExecutionCleanup(e)
	_, err = a.beginCleanupCall(ctx, p)
	hasCode(t, err, f.ResourceBusy)
	original()
	next, err := a.beginCleanupCall(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestExecutionRetirementPreservesOnlyOriginalUnknown(t *testing.T) {
	for _, mode := range []string{"zero", "missing-attempt", "missing-cause", "original"} {
		t.Run(mode, func(t *testing.T) {
			attempt := testID[f.TransactionAttempt](t)
			cause, err := f.NewRecoveryCause("execution-retirement", testID[struct{}](t).String(), "")
			if err != nil {
				t.Fatal(err)
			}
			result := f.UnknownResult(attempt, cause)
			switch mode {
			case "zero":
				result = f.CommitResult{}
			case "missing-attempt":
				result = f.UnknownResult(f.ID[f.TransactionAttempt]{}, cause)
			case "missing-cause":
				result = f.UnknownResult(attempt, f.TransactionCause{})
			}
			if executionRetirementUnknown(result) != (mode == "original") {
				t.Fatal("unattributed zero result became retained Unknown")
			}
		})
	}
	private := errors.New("original controlled provider failure")
	for _, err := range []error{private, fault(f.ResourceBusy), context.Canceled} {
		result := executionRetirementRejected(err)
		if result.State() != f.NotCommitted || executionRetirementUnknown(result) {
			t.Fatal("known rejection became Unknown")
		}
		var expected *f.Fault
		if errors.As(err, &expected) {
			if result.Fault().Code != expected.Code {
				t.Fatal("original safe code changed")
			}
			continue
		}
		if !errors.Is(result.Fault(), err) {
			t.Fatal("original cause changed")
		}
	}
}

type executionRetirementErrorPort struct {
	executionRetirementShapePort
	mode      string
	original  f.CommitResult
	err       error
	confirmed bool
}

func (p *executionRetirementErrorPort) WithProjectExecutionRetirement(ctx context.Context, actor i.Actor, cause c.LifecycleCause, scope c.ScopeRef, fn ec.ProjectExecutionRetirementCall) (f.CommitResult, error) {
	if p.mode == "preflight" {
		return executionRetirementRejected(p.err), p.err
	}
	result, err := fn(ctx, false)
	if p.mode == "unknown" {
		return p.original, p.err
	}
	return result, err
}
func (p *executionRetirementErrorPort) DiscoverProjectExecutionRetirement(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef) (ec.ProjectExecutionRetirementPlan, error) {
	return nil, p.err
}
func (p *executionRetirementErrorPort) ConfirmProjectExecutionRetirementInTx(context.Context, f.Tx, ec.ProjectExecutionRetirementPlan) (bool, error) {
	p.confirmed = true
	return false, p.err
}

func TestExecutionRetirementKnownProviderErrorDoesNotCreateUnknown(t *testing.T) {
	for _, mode := range []string{"preflight", "discovery", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, _, store, e, _ := executionRetirementFixture(t)
			cause, err := f.NewRecoveryCause("execution-retirement-discover", e.round.claim.operation.String(), e.round.claim.project.String())
			if err != nil {
				t.Fatal(err)
			}
			original := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
			failure := error(fault(f.ResourceBusy))
			if mode == "unknown" {
				failure = commitError(original)
			}
			port := &executionRetirementErrorPort{executionRetirementShapePort: executionRetirementShapePort{identity: d.state.executions.ProjectExecutionRetirementIdentity()}, mode: mode, original: original, err: failure}
			d.state.executions = port
			got, settled, err := d.state.runExecutionRetirement(context.Background(), e.round.claim.project, e.round.claim.operation, e)
			if got != e || settled || port.confirmed || store.transactions.Load() != 0 {
				t.Fatal("known early failure crossed SQL/confirmation", err)
			}
			if mode == "unknown" {
				if e.unknown == nil || e.unknown.AttemptID() != original.AttemptID() || e.unknown.Cause().Details().RecoveryRunID != cause.Details().RecoveryRunID {
					t.Fatal("original Unknown replaced")
				}
			} else if e.unknown != nil || !errors.Is(err, failure) {
				t.Fatal("known error became Unknown", err)
			}
		})
	}
}

func TestExecutionRetirementAcceptedCheckpointSurvivesReadFailure(t *testing.T) {
	d, _, store, e, _ := executionRetirementFixture(t)
	report, err := executionRetirementReport(e.round, executionRetirementTestCheckpoint(t, e, "applied"))
	if err != nil {
		t.Fatal(err)
	}
	e.accepted = true
	e.acceptedCheckpoint = report.Details().Checkpoint
	original := e.acceptedCheckpoint
	for n := 0; n < 2; n++ {
		got, settled, err := d.state.runExecutionRetirement(context.Background(), e.round.claim.project, e.round.claim.operation, e)
		if got != e || settled || err == nil || !e.accepted || e.acceptedCheckpoint != original || e.restored {
			t.Fatal("temporary prepare read lost original accepted checkpoint", err)
		}
	}
	if store.transactions.Load() != 0 {
		t.Fatal("failed claim lookup advanced into a new transaction")
	}
}

// This Store runs the real registration/prepare callbacks and rejects their
// first lock acquisition. It does not emulate successful canonical SQL or a
// successful registration; those require the real PG fixture.
type executionRegistrationRollbackStore struct {
	readerStore
	prior      lifecycleStopClaim
	lockErr    error
	claimReads int
	causes     []f.TransactionCause
}

func (s *executionRegistrationRollbackStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.causes = append(s.causes, cause)
	s.tx = f.NewTx()
	if err := fn(ctx, s.tx); err != nil {
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted))
}
func (s *executionRegistrationRollbackStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	return s.lockErr
}
func (s *executionRegistrationRollbackStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	if !strings.Contains(query, "FROM agenteam_project.work_claims") || len(args) != 1 || args[0] != s.prior.operation.String() {
		return rowFunc(func(...any) error { return errors.New("unexpected registration fixture read") })
	}
	s.claimReads++
	p := s.prior
	return valuesRow(p.project.String(), p.process.String(), p.attempt, p.fence, p.phase)
}

func TestExecutionRetirementAcceptedCheckpointSurvivesRegistrationRollback(t *testing.T) {
	d, _, _, e, _ := executionRetirementFixture(t)
	report, err := executionRetirementReport(e.round, executionRetirementTestCheckpoint(t, e, "applied"))
	if err != nil {
		t.Fatal(err)
	}
	original := report.Details().Checkpoint
	e.registered, e.acceptedCheckpoint = false, original
	prior := e.round.claim
	prior.phase = "terminal"
	store := &executionRegistrationRollbackStore{prior: prior, lockErr: errors.New("original registration lock failure")}
	life, err := NewLifecycleAuthority(store, executionRetirementRecord(t).manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	d.state.store, d.state.authority = store, life
	for n := 1; n <= 2; n++ {
		got, settled, err := d.state.runExecutionRetirement(context.Background(), prior.project, prior.operation, e)
		if got != e || settled || !errors.Is(err, store.lockErr) || !e.accepted || e.acceptedCheckpoint != original || e.restored || e.unknown != nil || e.registrationUnknown != nil {
			t.Fatal("known registration rollback lost the accepted checkpoint", err)
		}
		if store.calls != n || store.claimReads != n-1 {
			t.Fatal("next explicit call did not re-enter original preparation")
		}
	}
	if store.causes[0].Details().JobAttemptID == store.causes[1].Details().JobAttemptID {
		t.Fatal("preparation reused the failed registration attempt")
	}
}
