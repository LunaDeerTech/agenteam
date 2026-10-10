package execution

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQL, Project, Task and process owners are explicit controls here. Launch,
// preparation, the original Tx witness and recovery methods are actual code.
// These tests do not establish PostgreSQL constraints, real Task launchability,
// ProcessGuard death, a full preparation input, or production wiring.
type preparationStoreControl struct {
	*launchStore
	claims       map[string]preparationClaim
	attempts     map[string]preparationClaim
	unknownAt    string
	phase        string
	providerRefs int
	inputs       map[string][]any
}

func (s *preparationStoreControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func copyClaims(values map[string]preparationClaim) map[string]preparationClaim {
	out := map[string]preparationClaim{}
	for k, v := range values {
		out[k] = v
	}
	return out
}
func (s *preparationStoreControl) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	claims, attempts, refs := copyClaims(s.claims), copyClaims(s.attempts), s.providerRefs
	inputs := make(map[string][]any, len(s.inputs))
	for key, value := range s.inputs {
		inputs[key] = append([]any(nil), value...)
	}
	s.phase = ""
	var callbackErr error
	result := s.launchStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		callbackErr = fn(ctx, tx)
		return callbackErr
	})
	if result.State() == f.NotCommitted {
		s.claims, s.attempts, s.providerRefs = claims, attempts, refs
		s.inputs = inputs
		// The older Launch control does not preserve a non-Fault callback
		// cause. Match postgres.rejected here so cancellation remains visible
		// through the real preparation call, without changing that old fixture.
		var known *f.Fault
		if callbackErr != nil && !errors.As(callbackErr, &known) {
			result = f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(callbackErr))
		}
	}
	if result.State() == f.Committed && s.unknownAt != "" && s.phase == s.unknownAt {
		s.unknownAt = ""
		return f.UnknownResult(newTestID[f.TransactionAttempt](s.t), cause)
	}
	return result
}
func claimValues(c preparationClaim) []any {
	return []any{c.execution.String(), c.project.String(), c.agent.String(), c.attempt.String(), c.process.String(), c.fence, c.phase}
}
func (s *preparationStoreControl) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "FROM agenteam_execution.preparation_inputs WHERE") {
		return launchScan{values: s.inputs[args[0].(string)]}
	}
	if query == "SELECT clock_timestamp()" {
		return launchScan{values: []any{time.Now().UTC().Truncate(time.Microsecond)}}
	}
	if strings.Contains(query, "agenteam_execution.preparation_claims c") {
		claim, ok := s.claims[args[0].(string)]
		if !ok {
			return launchScan{}
		}
		attempt, ok := s.attempts[claim.attempt.String()]
		if !ok {
			return launchScan{}
		}
		claim.phase = attempt.phase
		return launchScan{values: claimValues(claim)}
	}
	if strings.Contains(query, "FROM agenteam_execution.preparation_attempts WHERE") {
		claim, ok := s.attempts[args[1].(string)]
		if !ok || claim.execution.String() != args[0] {
			return launchScan{}
		}
		return launchScan{values: claimValues(claim)}
	}
	// This is only the driver's initial non-authorizing identity discovery.
	if !s.live && strings.HasSuffix(query, "WHERE id=$1") {
		return launchScan{values: s.rows[args[0].(string)]}
	}
	return s.launchStore.QueryRow(ctx, query, args...)
}
func (s *preparationStoreControl) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !s.live || ctx.Err() != nil {
		s.t.Fatal("preparation SQL escaped its live original transaction")
	}
	switch {
	case strings.HasPrefix(query, "INSERT INTO agenteam_execution.preparation_inputs("):
		if s.inputs == nil {
			s.inputs = map[string][]any{}
		}
		if s.inputs[args[0].(string)] != nil {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		s.inputs[args[0].(string)] = append([]any(nil), args...)
		s.phase = "capture"
		s.writes++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(query, "UPDATE agenteam_execution.executions SET status='preparing'"):
		row := s.rows[args[0].(string)]
		if row == nil || row[3] != "created" || row[4] != args[1] || row[5] != (*time.Time)(nil) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		row[3], row[4] = "preparing", row[4].(int64)+1
		s.writes++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.HasPrefix(query, "INSERT INTO agenteam_execution.preparation_attempts"):
		e, _ := f.ParseID[i.Execution](args[0].(string))
		p, _ := f.ParseID[i.Project](args[1].(string))
		a, _ := f.ParseID[i.Agent](args[2].(string))
		attempt, _ := f.ParseID[preparationAttempt](args[3].(string))
		process, _ := f.ParseID[oc.Process](args[4].(string))
		s.attempts[attempt.String()] = preparationClaim{e, p, a, attempt, process, args[5].(int64), "running"}
		s.phase = "claim"
		s.writes++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(query, "INSERT INTO agenteam_execution.preparation_claims"):
		claim := s.attempts[args[3].(string)]
		s.claims[args[0].(string)] = claim
		s.writes++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(query, "UPDATE agenteam_execution.preparation_claims SET"):
		old, ok := s.claims[args[0].(string)]
		if !ok || old.attempt.String() != args[4] || old.process.String() != args[5] || old.fence != args[6] {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.claims[args[0].(string)] = s.attempts[args[1].(string)]
		s.writes++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.HasPrefix(query, "UPDATE agenteam_execution.preparation_attempts SET"):
		old, ok := s.attempts[args[1].(string)]
		if !ok || old.execution.String() != args[0] || old.process.String() != args[2] || old.fence != args[3] || old.phase != "running" {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		old.phase = "terminal"
		s.attempts[args[1].(string)] = old
		s.phase = "finish"
		s.writes++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	default:
		s.t.Fatal("unexpected preparation SQL")
	}
	return pgconn.CommandTag{}, nil
}

type preparationProcesses struct {
	process  oc.ProcessID
	stopped  bool
	confirms int
}

func (p *preparationProcesses) CurrentProcess() oc.ProcessID { return p.process }
func (p *preparationProcesses) ConfirmStopped(context.Context, oc.ProcessID) error {
	p.confirms++
	if !p.stopped {
		return fault(f.ResourceBusy)
	}
	return nil
}

type preparationControl struct {
	launch           *launchControl
	store            *preparationStoreControl
	driver           *PreparationDriver
	execution        i.ExecutionID
	processes        *preparationProcesses
	project          pc.ProjectRef
	captures         int
	stale            bool
	entered, release chan struct{}
	savedCtx         context.Context
	savedTx          f.Tx
}

func (p *preparationControl) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.ProjectRef, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project)}); err != nil {
		return pc.ProjectRef{}, err
	}
	if project != p.project.ID {
		return pc.ProjectRef{}, fault(f.NotFound)
	}
	if p.project.Lifecycle != pc.Active {
		return pc.ProjectRef{}, fault(f.ProjectNotActive)
	}
	return clonePreparationProject(p.project), nil
}

type preparationPlanControl struct {
	owner   *preparationControl
	request c.PreparationRequest
}

func (p *preparationPlanControl) RequiredLocks() []f.LockRequest {
	key, _ := f.AggregateLock(f.TaskAggregate, p.request.Launch.Trigger.TaskID)
	return []f.LockRequest{{Key: key, Mode: f.Shared}}
}
func (p *preparationControl) DiscoverCapture(ctx context.Context, e i.ExecutionID, r c.LaunchRequest) (c.CapturePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &preparationPlanControl{p, c.PreparationRequest{ExecutionID: e, Launch: r.Clone()}}, nil
}
func (p *preparationControl) CaptureInputInTx(ctx context.Context, tx f.Tx, e i.ExecutionID, r c.LaunchRequest, plan c.CapturePlan) (c.CapturedTriggerInput, error) {
	q, ok := plan.(*preparationPlanControl)
	if !ok || q.owner != p || !q.request.Equal(c.PreparationRequest{ExecutionID: e, Launch: r}) {
		return c.CapturedTriggerInput{}, fault(f.Forbidden)
	}
	if _, err := p.launch.authority.RequireTriggerCaptureInTx(ctx, tx, e, r); err != nil {
		return c.CapturedTriggerInput{}, err
	}
	actor, _ := i.NewAgentRun(r.ProjectID, r.AgentID, e)
	premature := ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: r.ProjectID, AgentID: r.AgentID, ExecutionID: e, Stage: ac.ExecutionConfigurationCapture}
	requireCode(p.launch.t, p.launch.authority.RequireExecutionConfigurationInTx(ctx, tx, premature), f.Forbidden)
	p.captures++
	p.savedCtx, p.savedTx = ctx, tx
	if p.entered != nil {
		close(p.entered)
		<-p.release
	}
	if err := ctx.Err(); err != nil {
		return c.CapturedTriggerInput{}, err
	}
	if p.stale {
		return c.CapturedTriggerInput{}, fault(f.ConfirmationStale)
	}
	p.store.providerRefs++ // Explicit controlled same-Tx reference write.
	raw := []byte(`{"task_version":1,"body":"original-private-source"}`)
	return c.NewCapturedTriggerInput(c.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: newTestID[c.TriggerInput](p.launch.t), Digest: c.TriggerInputDigest(raw)}, raw)
}
func newPreparationControl(t *testing.T) *preparationControl {
	t.Helper()
	x := newLaunchControl(t)
	launched, err := x.service.Launch(context.Background(), x.actor, x.request)
	if err != nil {
		t.Fatal(err)
	}
	p := &preparationControl{launch: x, execution: launched.Execution.ID, processes: &preparationProcesses{process: newTestID[oc.Process](t)}}
	p.store = &preparationStoreControl{launchStore: x.store, claims: map[string]preparationClaim{}, attempts: map[string]preparationClaim{}}
	at, _ := f.NewInstant(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	user, _ := f.ParseID[i.User](x.actor.Details().UserID)
	p.project = pc.ProjectRef{ID: x.request.ProjectID, OwnerUserID: user, Name: "Execution-Test", NormalizedName: "execution-test", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}
	x.authority, err = NewAuthority(p.store, launchProjects{fixture: x}, nil)
	if err != nil {
		t.Fatal(err)
	}
	x.captures = 0
	p.driver, err = NewPreparationDriver(p.store, x.authority, PreparationDependencies{Agents: x, Projects: p, Processes: p.processes, Task: p})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.driver.Stop()
		if !p.driver.Joined() {
			t.Error("test left original preparation ownership unresolved")
		}
	})
	return p
}

func TestExecutionPreparationCapturesInOriginalTransactionAndRollsBackPartialInput(t *testing.T) {
	p := newPreparationControl(t)
	p.launch.revoked = true // Capture must not borrow the old launch Human Session.
	err := p.driver.Run(context.Background(), p.execution)
	requireCode(t, err, f.DependencyUnbound)
	if p.captures != 1 || p.launch.captures != 1 || p.store.providerRefs != 0 {
		t.Fatal("partial source/Agent capture committed or current capture ports were skipped")
	}
	row := p.store.rows[p.execution.String()]
	if row[3] != "preparing" || row[4] != int64(2) || row[6] != (*string)(nil) || row[8] != (*time.Time)(nil) {
		t.Fatal("preparation fabricated Snapshot/running")
	}
	claim := p.store.claims[p.execution.String()]
	if p.store.attempts[claim.attempt.String()].phase != "terminal" {
		t.Fatal("actual returned attempt did not checkpoint")
	}
	actor, _ := i.NewAgentRun(p.launch.request.ProjectID, p.launch.request.AgentID, p.execution)
	r := ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: p.launch.request.ProjectID, AgentID: p.launch.request.AgentID, ExecutionID: p.execution, Stage: ac.ExecutionConfigurationCapture}
	if err := p.launch.authority.RequireExecutionConfigurationInTx(p.savedCtx, p.savedTx, r); err == nil {
		t.Fatal("closed original capture context was reused")
	}
}

func TestExecutionPreparationUnknownOwnsOriginalAttemptUntilObserved(t *testing.T) {
	for _, phase := range []string{"claim", "finish"} {
		t.Run(phase, func(t *testing.T) {
			p := newPreparationControl(t)
			p.store.unknownAt = phase
			err := p.driver.Run(context.Background(), p.execution)
			original, ok := UnknownAttempt(err)
			if !ok || original.AttemptID().Validate() != nil {
				t.Fatal("lost original physical Unknown", err)
			}
			if phase == "claim" && (p.captures != 0 || p.launch.captures != 0) {
				t.Fatal("unknown claim invoked a provider")
			}
			p.driver.Stop()
			if p.driver.Joined() {
				t.Fatal("Unknown retired process ownership")
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if !errors.Is(p.driver.Drain(cancelled), context.Canceled) {
				t.Fatal("Unknown drained before resolution")
			}
			claim := p.store.claims[p.execution.String()]
			attempt := p.store.attempts[claim.attempt.String()]
			delete(p.store.attempts, claim.attempt.String()) // Explicit not-observed control, never a negative proof.
			err = p.driver.ResolveUnknown(context.Background(), p.execution)
			result, still := UnknownAttempt(err)
			if !still || result.AttemptID() != original.AttemptID() || p.driver.Joined() {
				t.Fatal("not-observed replaced/retired original Unknown", err)
			}
			p.store.attempts[claim.attempt.String()] = attempt
			captures := p.captures
			if err = p.driver.ResolveUnknown(context.Background(), p.execution); err != nil || !p.driver.Joined() {
				t.Fatal("observed original returned attempt did not converge", err)
			}
			if p.captures != captures || len(p.store.attempts) != 1 {
				t.Fatal("resolution reissued capture/new attempt")
			}
		})
	}
}

func TestExecutionPreparationSourceRejectionAndPrivateProof(t *testing.T) {
	for _, scope := range []string{"source-rejected", "missing-source", "cancelled-row", "project-inactive"} {
		t.Run(scope, func(t *testing.T) {
			p := newPreparationControl(t)
			code := f.DependencyUnbound
			switch scope {
			case "source-rejected":
				p.stale = true
				code = f.ConfirmationStale
			case "missing-source":
				p.driver.state.task = nil
			case "cancelled-row":
				at := time.Date(2026, 10, 10, 0, 0, 1, 0, time.UTC)
				p.store.rows[p.execution.String()][5] = &at
				code = f.InvalidState
			case "project-inactive":
				p.project.Lifecycle = pc.Archiving
				code = f.ProjectNotActive
			}
			requireCode(t, p.driver.Run(context.Background(), p.execution), code)
			if p.launch.captures != 0 || p.store.providerRefs != 0 {
				t.Fatal("rejected source issued Agent witness or retained references")
			}
			_, err := p.launch.authority.RequireTriggerCaptureInTx(context.Background(), f.NewTx(), p.execution, p.launch.request)
			requireCode(t, err, f.Forbidden)
		})
	}
}

func TestExecutionPreparationStopWaitsForOriginalSourceAndCheckpoint(t *testing.T) {
	p := newPreparationControl(t)
	p.entered, p.release = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- p.driver.Run(context.Background(), p.execution) }()
	<-p.entered
	p.driver.Stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(p.driver.Drain(cancelled), context.Canceled) || p.driver.Joined() {
		t.Fatal("Stop signal replaced original source return")
	}
	close(p.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("original cancellation lost", err)
	}
	if !p.driver.Joined() {
		t.Fatal("actual original return did not release ownership")
	}
	claim := p.store.claims[p.execution.String()]
	if p.store.attempts[claim.attempt.String()].phase != "terminal" {
		t.Fatal("Drain passed before original checkpoint")
	}
}

func TestExecutionPreparationForeignClaimRequiresDeathAndExactFence(t *testing.T) {
	p := newPreparationControl(t)
	requireCode(t, p.driver.Run(context.Background(), p.execution), f.DependencyUnbound)
	old := p.store.claims[p.execution.String()]
	old.process = newTestID[oc.Process](t)
	old.phase = "running"
	p.store.claims[p.execution.String()] = old
	p.store.attempts[old.attempt.String()] = old
	requireCode(t, p.driver.Run(context.Background(), p.execution), f.ResourceBusy)
	if len(p.store.attempts) != 1 {
		t.Fatal("live foreign claim was replaced")
	}
	p.processes.stopped = true
	requireCode(t, p.driver.Run(context.Background(), p.execution), f.DependencyUnbound)
	next := p.store.claims[p.execution.String()]
	if p.processes.confirms != 2 || next.fence != old.fence+1 || next.attempt == old.attempt || p.store.attempts[old.attempt.String()].phase != "terminal" {
		t.Fatal("foreign death/fence/current tuple not consumed")
	}
}

func TestExecutionPreparationCapturedInputCopiesAndSafeProjection(t *testing.T) {
	raw := []byte(`{"body":"private-trigger-canary"}`)
	ref := c.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: newTestID[c.TriggerInput](t), Digest: c.TriggerInputDigest(raw)}
	input, err := c.NewCapturedTriggerInput(ref, raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[2] = 'x'
	returned := input.Data()
	returned[2] = 'y'
	if string(input.Data()) != `{"body":"private-trigger-canary"}` || input.Ref() != ref {
		t.Fatal("captured source aliases caller storage")
	}
	encoded, err := json.Marshal(map[string]any{"nested": []c.CapturedTriggerInput{input}})
	if err != nil || strings.Contains(string(encoded), "canary") {
		t.Fatal("implicit source projection leaked")
	}
	ref.Digest = c.TriggerInputDigest([]byte(`{}`))
	if _, err = c.NewCapturedTriggerInput(ref, input.Data()); err == nil {
		t.Fatal("mismatched original body digest accepted")
	}
}
