package project

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type testProcess struct {
	id        oc.ProcessID
	calls     int
	stopped   bool
	requested oc.ProcessID
}

func (p *testProcess) CurrentProcess() oc.ProcessID { return p.id }
func (p *testProcess) ConfirmStopped(_ context.Context, id oc.ProcessID) error {
	p.calls++
	p.requested = id
	if !p.stopped {
		return fault(foundation.ResourceBusy)
	}
	return nil
}
func TestClaimRequiresActualJoinOrExactDeath(t *testing.T) {
	process := &testProcess{id: testID[oc.Process](t)}
	st := &serviceState{deps: Dependencies{Processes: process}, joined: map[string]bool{}}
	s := &Service{data: func() *serviceState { return st }}
	claim := &workClaim{process: process.id, attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}
	hasCode(t, s.claimCanJoin(context.Background(), claim), foundation.ResourceBusy)
	if process.calls != 0 {
		t.Fatal("asked remote death for local active call")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	hasCode(t, s.claimCanJoin(cancelled, claim), foundation.ResourceBusy)
	s.joined(claim.attempt)
	if e := s.claimCanJoin(context.Background(), claim); e != nil {
		t.Fatal(e)
	}
	claim.process = testID[oc.Process](t)
	hasCode(t, s.claimCanJoin(context.Background(), claim), foundation.ResourceBusy)
	if process.requested != claim.process {
		t.Fatal("death proof target changed")
	}
	process.stopped = true
	if e := s.claimCanJoin(context.Background(), claim); e != nil {
		t.Fatal(e)
	}
	claim.phase = "terminal"
	process.stopped = false
	if e := s.claimCanJoin(context.Background(), claim); e != nil {
		t.Fatal("canonical terminal cannot recover", e)
	}
}
func TestUnknownPreservesOriginalAttemptAndCause(t *testing.T) {
	id, _ := c.CommandIdentity(testProject(t).ID, c.CreateCommand, "same-key")
	attempt := testID[foundation.TransactionAttempt](t)
	result := foundation.UnknownResult(attempt, commandCause(id))
	err := commitError(result)
	hasCode(t, err, foundation.CommitUnknown)
	saved, ok := UnknownAttempt(err)
	if !ok || saved.AttemptID() != attempt || saved.Cause().Details().Primary.Canonical() != id.Canonical() {
		t.Fatal("lost original provenance")
	}
	if commitError(foundation.CommittedResult()) != nil {
		t.Fatal("committed became error")
	}
}
func TestStopCancellationIsNotAJoin(t *testing.T) {
	st := &serviceState{calls: map[*call]struct{}{}, changed: make(chan struct{}), slots: make(chan struct{}, 1), joined: map[string]bool{}}
	s := &Service{data: func() *serviceState { return st }}
	ctx, done, e := s.begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	s.Force()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("force did not cancel")
	}
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if s.Drain(deadline) == nil {
		t.Fatal("cancel fabricated completed join")
	}
	done()
	done()
	if e = s.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
	_, _, e = s.begin(context.Background())
	hasCode(t, e, foundation.ShuttingDown)
}

type roundTestStore struct {
	*authorityStore
	result                               foundation.CommitResult
	firstUnknown                         bool
	unknownBefore, unknownAfter, txCalls int
	acquireCalls                         int
	acquirePlans                         [][]foundation.LockRequest
	executedStates                       []string
	exec                                 func(string, ...any) (pgconn.CommandTag, error)
	acquireErr                           error
}

func (s *roundTestStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.txCalls++
	if s.firstUnknown || s.txCalls == s.unknownBefore {
		s.firstUnknown = false
		s.result = foundation.UnknownResult(s.result.AttemptID(), cause)
		return s.result
	}
	result := s.authorityStore.WithinTx(ctx, cause, fn)
	if result.State() == foundation.Committed && s.txCalls == s.unknownAfter {
		s.result = foundation.UnknownResult(s.result.AttemptID(), cause)
		return s.result
	}
	return result
}
func (s *roundTestStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.authorityStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *roundTestStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.acquireCalls++
	if s.acquireErr != nil {
		return s.acquireErr
	}
	s.acquirePlans = append(s.acquirePlans, append([]foundation.LockRequest(nil), locks...))
	return s.authorityStore.AcquireAll(ctx, tx, locks)
}
func (s *roundTestStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if s.exec != nil {
		return s.exec(q, args...)
	}
	if strings.Contains(q, "UPDATE agenteam_project.creations SET state=") {
		s.executedStates = append(s.executedStates, args[1].(string))
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func roundTestService(t *testing.T, store *roundTestStore) (*Service, identity.Actor) {
	actor := testActor(t)
	authority, e := NewAuthority(store, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, foundation.Tx, identity.Actor) error { return nil })})
	if e != nil {
		t.Fatal(e)
	}
	st := &serviceState{store: store, deps: Dependencies{Authority: authority}, calls: map[*call]struct{}{}, changed: make(chan struct{}), slots: make(chan struct{}, 1), joined: map[string]bool{}}
	return &Service{data: func() *serviceState { return st }}, actor
}
func TestSerializedAbsenceConvergesWithOriginalProvenance(t *testing.T) {
	for _, cmd := range []c.CommandName{c.CreateCommand, c.UpdateCommand} {
		t.Run(string(cmd), func(t *testing.T) {
			order := []string{}
			store := &roundTestStore{authorityStore: &authorityStore{tx: foundation.NewTx(), order: &order}, firstUnknown: true, result: foundation.UnknownResult(testID[foundation.TransactionAttempt](t), foundation.TransactionCause{})}
			service, actor := roundTestService(t, store)
			p := testProject(t)
			p.OwnerUserID, _ = foundation.ParseID[identity.User](actor.Details().UserID)
			creation := testID[c.Creation](t)
			store.row = func(q string, args ...any) postgres.Row {
				if cmd == c.UpdateCommand && strings.Contains(q, "FROM agenteam_project.projects") {
					return valuesRow(p.ID.String(), p.OwnerUserID.String(), p.Name, p.NormalizedName, p.Description, string(p.Lifecycle), int64(p.Version), nil, p.CreatedAt.Time(), p.UpdatedAt.Time(), nil, creation.String(), true, nil)
				}
				return rowFunc(func(...any) error { return pgx.ErrNoRows })
			}
			meta := foundation.CommandMeta{RequestID: testID[foundation.Request](t), IdempotencyKey: "same-intent"}
			var err error
			if cmd == c.CreateCommand {
				_, err = service.CreateProject(context.Background(), actor, meta, c.CreateProjectRequest{ProjectID: p.ID, Name: p.Name})
			} else {
				meta.ExpectedVersion = &p.Version
				description := "new"
				_, err = service.UpdateProject(context.Background(), actor, meta, p.ID, c.UpdateProjectRequest{Description: &description})
			}
			if store.acquireCalls != 1 || len(store.acquirePlans[0]) != 3 {
				t.Fatalf("confirmation did not acquire full writer union: %#v", store.acquirePlans)
			}
			var f *foundation.Fault
			if !errors.As(err, &f) || f.CommitState != foundation.NotCommitted {
				t.Fatalf("confirmed absence remained unresolved: %#v", f)
			}
			original, ok := UnknownAttempt(err)
			if !ok || original.AttemptID() != store.result.AttemptID() || original.Cause().Details().Primary.Canonical() != store.result.Cause().Details().Primary.Canonical() || f.CauseID != original.AttemptID().String() || f.Code != foundation.DependencyUnavailable {
				t.Fatal("original physical attempt lost")
			}
		})
	}
}
func TestCreationCheckpointSeparatesPendingUnknownAndFailed(t *testing.T) {
	for _, reason := range []c.SafeReason{c.ReasonWorkPending, c.ReasonOutcomeUnknown, c.ReasonOperationFailed} {
		t.Run(string(reason), func(t *testing.T) {
			order := []string{}
			store := &roundTestStore{authorityStore: &authorityStore{tx: foundation.NewTx(), order: &order}}
			service, _ := roundTestService(t, store)
			p := testProject(t)
			now := time.Now().UTC()
			creation := testID[c.Creation](t)
			r := &creationRecord{operation: c.CreationOperation{ID: creation, ProjectID: p.ID, State: c.CreationInitializing, Version: 2, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}, owner: p.OwnerUserID, key: "create", initializationKey: "init", semantic: digest([]byte("intent")), requestName: &p.Name, requestDescription: &p.Description, eventID: testID[struct{}](t).String()}
			claim := &workClaim{creation: creation, project: p.ID, process: testID[oc.Process](t), attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}
			store.row = func(q string, args ...any) postgres.Row {
				switch {
				case strings.Contains(q, "FROM agenteam_project.work_claims"):
					return valuesRow(p.ID.String(), claim.process.String(), claim.attempt, claim.fence, claim.phase)
				case strings.Contains(q, "FROM agenteam_project.creations"):
					return valuesRow(creation.String(), p.ID.String(), p.OwnerUserID.String(), string(r.key), r.semantic.String(), r.requestName, r.requestDescription, "initializing", string(r.initializationKey), nil, nil, nil, int64(2), p.CreatedAt.Time(), p.UpdatedAt.Time(), r.eventID, []byte(nil), []byte(nil), []byte(nil))
				case strings.Contains(q, "FROM agenteam_project.projects"):
					return valuesRow(p.ID.String(), p.OwnerUserID.String(), p.Name, p.NormalizedName, p.Description, "active", int64(1), nil, p.CreatedAt.Time(), p.UpdatedAt.Time(), nil, creation.String(), false, nil)
				case q == "SELECT clock_timestamp()":
					return valuesRow(now)
				default:
					t.Fatalf("unexpected query %s", q)
					return nil
				}
			}
			got, err := service.finishCreationRound(context.Background(), r, claim, reason, nil)
			if reason == c.ReasonOperationFailed {
				var f *foundation.Fault
				if !errors.As(err, &f) || f.CommitState != foundation.Committed {
					t.Fatalf("confirmed failed creation must retain accepted commit and return safe Fault: %#v", f)
				}
				if len(store.executedStates) != 1 || store.executedStates[0] != string(c.CreationFailed) {
					t.Fatalf("real failure not checkpointed: %v", store.executedStates)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Operation == nil || got.Operation.State != c.CreationInitializing || got.Operation.SafeReason != reason {
				t.Fatalf("reason %s projected %#v, want initializing", reason, got.Operation)
			}
		})
	}
}
func TestProviderUnknownStateWinsOverDiagnosticCode(t *testing.T) {
	f := foundation.NewFault(foundation.DependencyUnavailable, foundation.Unknown)
	if got := reasonFor(f); got != c.ReasonOutcomeUnknown {
		t.Fatalf("unknown provider transaction became %s", got)
	}
}

func TestFailedWriterConfirmationKeepsUnknown(t *testing.T) {
	order := []string{}
	store := &roundTestStore{
		authorityStore: &authorityStore{tx: foundation.NewTx(), order: &order},
		firstUnknown:   true,
		result:         foundation.UnknownResult(testID[foundation.TransactionAttempt](t), foundation.TransactionCause{}),
		acquireErr:     &pgconn.PgError{Code: "55P03"},
	}
	service, actor := roundTestService(t, store)
	p := testProject(t)
	_, err := service.CreateProject(context.Background(), actor, foundation.CommandMeta{RequestID: testID[foundation.Request](t), IdempotencyKey: "original"}, c.CreateProjectRequest{ProjectID: p.ID, Name: p.Name})
	var f *foundation.Fault
	if !errors.As(err, &f) || f.CommitState != foundation.Unknown || f.Code != foundation.CommitUnknown || f.RetryHint != "lookup" {
		t.Fatal("lock timeout fabricated a resolved absence", err)
	}
	original, ok := UnknownAttempt(err)
	if !ok || original.AttemptID() != store.result.AttemptID() || original.Cause().Details().Primary.Canonical() != store.result.Cause().Details().Primary.Canonical() {
		t.Fatal("failed confirmation replaced original provenance")
	}
}

type roundInitializer struct {
	c.ProjectSkillInitializer
	inspectErr  error
	state       c.InitializationState
	reason      c.SafeReason
	initialized int
}

func (i *roundInitializer) InspectProjectSkills(_ context.Context, _ identity.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	if i.inspectErr != nil {
		return c.InitializationResult{}, i.inspectErr
	}
	return c.InitializationResult{State: c.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: c.ReasonWorkPending}, nil
}
func (i *roundInitializer) InitializeProjectSkills(_ context.Context, _ identity.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	i.initialized++
	return c.InitializationResult{State: i.state, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: i.reason}, nil
}

func TestCreatePreservesConfirmedFailureAndUncertainInspection(t *testing.T) {
	for _, mode := range []string{"failed", "failed-checkpoint-unknown", "accept-unknown-failed", "inspect-unknown", "pending-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			order := []string{}
			store := &roundTestStore{authorityStore: &authorityStore{tx: foundation.NewTx(), order: &order}}
			service, actor := roundTestService(t, store)
			p := testProject(t)
			p.OwnerUserID, _ = foundation.ParseID[identity.User](actor.Details().UserID)
			creation := testID[c.Creation](t)
			eventID := testID[struct{}](t).String()
			meta := foundation.CommandMeta{RequestID: testID[foundation.Request](t), IdempotencyKey: "same-accepted-creation"}
			request := c.CreateProjectRequest{ProjectID: p.ID, Name: p.Name, Description: p.Description}
			semantic, err := c.CreateDigest(actor, meta, request)
			if err != nil {
				t.Fatal(err)
			}
			state, version, safeReason := string(c.CreationAccepted), int64(1), ""
			if mode == "failed-checkpoint-unknown" || mode == "accept-unknown-failed" {
				store.result = foundation.UnknownResult(testID[foundation.TransactionAttempt](t), foundation.TransactionCause{})
				if mode == "failed-checkpoint-unknown" {
					store.unknownAfter = 3
				} else {
					store.firstUnknown = true
					state, version, safeReason = "failed", 3, string(c.ReasonOperationFailed)
				}
			}
			var claim *workClaim
			now := p.UpdatedAt.Time().Add(time.Millisecond)
			store.row = func(q string, _ ...any) postgres.Row {
				switch {
				case strings.Contains(q, "FROM agenteam_project.deletion_receipts"):
					return rowFunc(func(...any) error { return pgx.ErrNoRows })
				case strings.Contains(q, "FROM agenteam_project.projects"):
					return valuesRow(p.ID.String(), p.OwnerUserID.String(), p.Name, p.NormalizedName, p.Description, "active", int64(1), nil, p.CreatedAt.Time(), p.UpdatedAt.Time(), nil, creation.String(), false, nil)
				case strings.Contains(q, "FROM agenteam_project.creations"):
					var reason *string
					if safeReason != "" {
						reason = &safeReason
					}
					return valuesRow(creation.String(), p.ID.String(), p.OwnerUserID.String(), string(meta.IdempotencyKey), semantic.String(), &p.Name, &p.Description, state, "init-same-key", nil, nil, reason, version, p.CreatedAt.Time(), p.UpdatedAt.Time(), eventID, []byte(nil), []byte(nil), []byte(nil))
				case strings.Contains(q, "FROM agenteam_project.work_claims"):
					if claim == nil {
						return rowFunc(func(...any) error { return pgx.ErrNoRows })
					}
					return valuesRow(p.ID.String(), claim.process.String(), claim.attempt, claim.fence, claim.phase)
				case q == "SELECT clock_timestamp()":
					return valuesRow(now)
				default:
					t.Fatalf("unexpected query: %s", q)
					return nil
				}
			}
			store.exec = func(q string, args ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(q, "INSERT INTO agenteam_project.work_claims"):
					process, e := foundation.ParseID[oc.Process](args[2].(string))
					if e != nil {
						t.Fatal(e)
					}
					claim = &workClaim{creation: creation, project: p.ID, process: process, attempt: args[3].(string), fence: 1, phase: "running"}
				case strings.Contains(q, "SET state='initializing'"):
					state, version, safeReason = "initializing", args[1].(int64), ""
				case strings.Contains(q, "SET state=$2,safe_reason=$3"):
					state, safeReason, version = args[1].(string), args[2].(string), args[3].(int64)
				case strings.Contains(q, "SET phase='terminal'"):
					claim.phase = "terminal"
				default:
					t.Fatalf("unexpected mutation: %s", q)
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}
			initializer := &roundInitializer{state: c.InitializationFailed, reason: c.ReasonOperationFailed}
			switch mode {
			case "inspect-unknown":
				initializer.inspectErr = foundation.NewFault(foundation.DependencyUnavailable, foundation.Unknown)
			case "pending-unavailable":
				initializer.state, initializer.reason = c.InitializationResultPending, c.ReasonDependencyUnavailable
			}
			service.state().deps.Initializer = initializer
			service.state().deps.Processes = &testProcess{id: testID[oc.Process](t)}
			service.state().initRegistration, err = identity.RegisterService(identity.ProjectInitialization)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.CreateProject(context.Background(), actor, meta, request)
			if mode != "accept-unknown-failed" && (claim == nil || claim.phase != "terminal") {
				t.Fatal("returned provider call did not checkpoint its exact claim")
			}
			if mode == "failed" || mode == "failed-checkpoint-unknown" || mode == "accept-unknown-failed" {
				var f *foundation.Fault
				if !errors.As(err, &f) || f.Code != foundation.DependencyUnavailable || f.CommitState != foundation.Committed || f.CauseID != creation.String() || state != "failed" || safeReason != string(c.ReasonOperationFailed) {
					t.Fatal("outer Create erased confirmed failure or accepted fact", err, state, safeReason)
				}
				if mode != "failed" {
					original, ok := UnknownAttempt(err)
					if !ok || original.AttemptID() != store.result.AttemptID() || original.Cause().Validate() != nil {
						t.Fatal("confirmed failure after Unknown lost original physical cause")
					}
				}
				return
			}
			wantReason := c.ReasonDependencyUnavailable
			if mode == "inspect-unknown" {
				wantReason = c.ReasonOutcomeUnknown
				if initializer.initialized != 0 {
					t.Fatal("unknown inspection triggered initialization")
				}
			}
			if err != nil || result.Operation == nil || result.Operation.ID != creation || result.Operation.State != c.CreationInitializing || result.Operation.SafeReason != wantReason || state != "initializing" {
				t.Fatal("uncertain/pending provider became failed or changed identity", err, result.Operation, state)
			}
		})
	}
}
