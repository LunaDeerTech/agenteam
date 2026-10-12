package artifact

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Original live Tx tokens, held locks and Scan outputs are controlled here.
// These tests do not establish a real Project grant, source registry membership,
// PostgreSQL rollback, or physical Object deletion.
type cleanupOwnerStore struct {
	Store
	mu      sync.Mutex
	live    map[f.Tx][]f.LockRequest
	before  func(context.Context, f.TransactionCause) *f.CommitResult
	after   func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
	fixture *cleanupOwnerFixture
}

func (s *cleanupOwnerStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	if s.before != nil {
		if result := s.before(ctx, cause); result != nil {
			return *result
		}
	}
	tx := f.NewTx()
	s.mu.Lock()
	s.live[tx] = nil
	s.mu.Unlock()
	err := callback(ctx, tx)
	s.mu.Lock()
	delete(s.live, tx)
	s.mu.Unlock()
	result := f.CommittedResult()
	if err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			fault = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		result = f.NotCommittedResult(fault)
	}
	if s.after != nil {
		return s.after(ctx, cause, result)
	}
	return result
}
func (s *cleanupOwnerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	_, ok := s.live[tx]
	s.mu.Unlock()
	if !tx.Valid() || !ok {
		return nil, failure(f.Forbidden, nil)
	}
	return cleanupOwnerExecutor{s, tx}, nil
}
func (s *cleanupOwnerStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.mu.Lock()
	s.live[tx] = append([]f.LockRequest(nil), locks...)
	s.mu.Unlock()
	return ctx.Err()
}
func (s *cleanupOwnerStore) held(ctx context.Context, tx f.Tx, project i.ProjectID) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, lock := range s.live[tx] {
		if lock.Key.Canonical() == projectExclusive(project).Key.Canonical() && lock.Mode == f.Exclusive {
			return ctx.Err()
		}
	}
	return failure(f.Forbidden, nil)
}
func (s *cleanupOwnerStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.fixture.discovery++
	return nil, errors.New("Rows and physical cleanup belong to integration")
}

type cleanupOwnerExecutor struct {
	store *cleanupOwnerStore
	tx    f.Tx
}

func (e cleanupOwnerExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("unexpected controlled Rows")
}
func (e cleanupOwnerExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if _, err := e.store.InTx(e.tx); err != nil {
		return cleanupOwnerRow{err: err}
	}
	return e.store.fixture.row(ctx, e.tx, query, args)
}
func (e cleanupOwnerExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if _, err := e.store.InTx(e.tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	return e.store.fixture.exec(ctx, e.tx, query, args)
}

type cleanupOwnerRow struct {
	values []any
	err    error
}

func (r cleanupOwnerRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected controlled cleanup shape")
	}
	for n, value := range r.values {
		out, in := reflect.ValueOf(dest[n]).Elem(), reflect.ValueOf(value)
		if !in.Type().AssignableTo(out.Type()) {
			return errors.New("unexpected controlled cleanup type")
		}
		out.Set(in)
	}
	return nil
}

type cleanupOwnerFact struct {
	actor                 i.Actor
	cause                 oc.ProjectCleanupCause
	state                 string
	source                bool
	remaining             bool
	checks, reads, writes int
}
type cleanupOwnerFixture struct {
	store     *cleanupOwnerStore
	service   *Service
	state     *serviceState
	facts     map[i.ProjectID]*cleanupOwnerFact
	gate      map[f.Tx]i.ProjectID
	mu        sync.Mutex
	deny      bool
	discovery int
}

func newCleanupOwnerFixture(t *testing.T) (*cleanupOwnerFixture, i.ProjectID) {
	t.Helper()
	v := &cleanupOwnerFixture{facts: make(map[i.ProjectID]*cleanupOwnerFact), gate: make(map[f.Tx]i.ProjectID)}
	v.store = &cleanupOwnerStore{live: make(map[f.Tx][]f.LockRequest), fixture: v}
	owners, err := NewOwnerProvider(v.store, v)
	if err != nil {
		t.Fatal(err)
	}
	v.state = &serviceState{store: v.store, owners: owners, leases: make(map[f.Digest]oc.SourceLease), leaseProjects: make(map[f.Digest]i.ProjectID)}
	v.service = &Service{data: func() *serviceState { return v.state }}
	project := v.add(t)
	t.Cleanup(func() {
		v.store.before, v.store.after, v.deny = nil, nil, false
		v.state.cleanupMu.Lock()
		var pending []i.ProjectID
		for p, owner := range v.state.cleanupCalls {
			if owner.active {
				t.Error("original cleanup still active")
			} else if owner.retained {
				pending = append(pending, p)
			}
		}
		v.state.cleanupMu.Unlock()
		for _, p := range pending {
			if _, err := v.run(context.Background(), v.service, p); err != nil {
				t.Error("original cleanup did not confirm", err)
			}
		}
	})
	return v, project
}
func (v *cleanupOwnerFixture) add(t *testing.T) i.ProjectID {
	t.Helper()
	p := newTestID[i.Project](t)
	cause, err := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: p, OperationID: newTestID[oc.CleanupOperation](t), Version: 7})
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	actor, err := registration.Actor(cause.Details().OperationID.String(), identityScope(p))
	if err != nil {
		t.Fatal(err)
	}
	v.facts[p] = &cleanupOwnerFact{actor: actor, cause: cause}
	return p
}
func (v *cleanupOwnerFixture) Discover(ctx context.Context, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	return oc.NewAccessDependencies(digestBytes([]byte(subject.ProjectID.String())), []f.LockRequest{projectExclusive(subject.ProjectID)})
}
func (v *cleanupOwnerFixture) DiscoverInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	if err := v.store.held(ctx, tx, subject.ProjectID); err != nil {
		return oc.AccessDependencies{}, err
	}
	return v.Discover(ctx, subject)
}
func (v *cleanupOwnerFixture) AuthorizeInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	if err := v.store.held(ctx, tx, subject.ProjectID); err != nil {
		return i.AccessGrant{}, err
	}
	if intent != i.Lifecycle {
		return i.AccessGrant{}, failure(f.Forbidden, nil)
	}
	at, _ := f.ParseInstant("2026-10-12T10:00:00Z")
	return i.NewAccessGrant(subject.Actor, identityScope(subject.ProjectID), intent, at, 7)
}
func (v *cleanupOwnerFixture) CheckProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause oc.ProjectCleanupCause) error {
	if err := v.store.held(ctx, tx, cause.Details().ProjectID); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.facts[cause.Details().ProjectID]
	if p == nil || !p.actor.Equal(actor) || p.cause.Details() != cause.Details() {
		return failure(f.Forbidden, nil)
	}
	p.checks++
	if v.deny {
		return failure(f.Forbidden, nil)
	}
	v.gate[tx] = cause.Details().ProjectID
	return ctx.Err()
}
func (v *cleanupOwnerFixture) fact(tx f.Tx, args []any) (*cleanupOwnerFact, error) {
	p, exists := v.gate[tx]
	if !exists || len(args) == 0 || args[0] != p.String() {
		return nil, errors.New("cleanup SQL before current Project gate")
	}
	return v.facts[p], nil
}
func (v *cleanupOwnerFixture) row(_ context.Context, tx f.Tx, query string, args []any) postgres.Row {
	v.mu.Lock()
	defer v.mu.Unlock()
	p, err := v.fact(tx, args)
	if err != nil {
		return cleanupOwnerRow{err: err}
	}
	p.reads++
	switch {
	case strings.HasPrefix(query, "SELECT jsonb_build_object(\n 'Work'"):
		return cleanupOwnerRow{values: []any{[]byte(`{"Work":[],"Stops":[]}`)}}
	case strings.Contains(query, "source_lease_id IS NOT NULL"):
		return cleanupOwnerRow{values: []any{p.source}}
	case strings.HasPrefix(query, "SELECT operation_id::text"):
		if p.state == "" {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
		return cleanupOwnerRow{values: []any{p.cause.Details().OperationID.String(), int64(p.cause.Details().Version), p.state}}
	case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts"):
		return cleanupOwnerRow{values: []any{p.remaining}}
	default:
		return cleanupOwnerRow{err: errors.New("unexpected cleanup read")}
	}
}
func (v *cleanupOwnerFixture) exec(_ context.Context, tx f.Tx, query string, args []any) (pgconn.CommandTag, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	p, err := v.fact(tx, args)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	p.writes++
	if !strings.HasPrefix(query, "INSERT INTO agenteam_artifact.cleanup") {
		return pgconn.CommandTag{}, errors.New("unexpected cleanup mutation")
	}
	p.state = "pending"
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (v *cleanupOwnerFixture) run(ctx context.Context, service *Service, p i.ProjectID) (oc.ProjectCleanupResult, error) {
	fact := v.facts[p]
	return service.CleanupProject(ctx, fact.actor, fact.cause)
}
func cleanupOwnerWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case out := <-ch:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("original Artifact cleanup did not return")
		var zero T
		return zero
	}
}
func requireCleanupOwnerUnknown(t *testing.T, out oc.ProjectCleanupResult, err error, original f.CommitResult) {
	t.Helper()
	var fault *f.Fault
	var private *projectCleanupCommitFailure
	if out != (oc.ProjectCleanupResult{}) || !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != original.AttemptID().String() || !errors.As(err, &private) || private.result.AttemptID() != original.AttemptID() || !reflect.DeepEqual(private.result.Cause().Details(), original.Cause().Details()) {
		t.Fatal("Artifact lost original Unknown", err)
	}
}
func (v *cleanupOwnerFixture) unknown(t *testing.T, p i.ProjectID) f.CommitResult {
	t.Helper()
	attempt := newTestID[f.TransactionAttempt](t)
	var original f.CommitResult
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if result.State() != f.Committed {
			return result
		}
		original = f.UnknownResult(attempt, cause)
		return original
	}
	out, err := v.run(context.Background(), v.service, p)
	v.store.after = nil
	requireCleanupOwnerUnknown(t, out, err, original)
	return original
}

func TestArtifactProjectCleanupSourcesRemainPending(t *testing.T) {
	for _, mode := range []string{"durable", "local", "unmapped-local", "other-project-local"} {
		t.Run(mode, func(t *testing.T) {
			v, p := newCleanupOwnerFixture(t)
			if mode == "durable" {
				v.facts[p].source = true
			} else {
				key := digestBytes([]byte("controlled source handle"))
				lease, err := oc.NewSourceLease(oc.NewSourceIssuer(), newTestID[oc.Lease](t))
				if err != nil {
					t.Fatal(err)
				}
				v.state.leases[key] = lease
				if mode == "local" {
					v.state.leaseProjects[key] = p
				}
				if mode == "other-project-local" {
					v.state.leaseProjects[key] = newTestID[i.Project](t)
					v.state.leaseSources = map[f.Digest]string{key: ""} // Known System source; neither side is this Project.
					v.facts[p].state = "completed"
				}
			}
			out, err := v.run(context.Background(), v.service, p)
			want := oc.CleanupPending
			if mode == "other-project-local" {
				want = oc.CleanupCompleted
			}
			if err != nil || out.State != want || v.facts[p].checks != 1 || v.facts[p].writes != 0 || v.discovery != 0 {
				t.Fatal("source owner was bypassed or blocked another Project", err)
			}
		})
	}
}

func TestArtifactProjectCleanupOwnsOriginalReturns(t *testing.T) {
	v, p := newCleanupOwnerFixture(t)
	b := v.add(t)
	v.facts[b].state = "completed"
	copyService := *v.service
	attempt := newTestID[f.TransactionAttempt](t)
	var original f.CommitResult
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().CheckpointRef == p.String() {
			original = f.UnknownResult(attempt, cause)
			close(entered)
			<-release
			return original
		}
		return result
	}
	type output struct {
		out oc.ProjectCleanupResult
		err error
	}
	done := make(chan output, 1)
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			cleanupOwnerWait(t, done)
		}
	})
	go func() { out, err := v.run(context.Background(), v.service, p); done <- output{out, err} }()
	cleanupOwnerWait(t, entered)
	if _, err := v.run(context.Background(), &copyService, p); err == nil {
		t.Fatal("same Service copy borrowed active original")
	} else {
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.ResourceBusy {
			t.Fatal("active original error", err)
		}
	}
	if out, err := v.run(context.Background(), &copyService, b); err != nil || out.State != oc.CleanupCompleted {
		t.Fatal("Project A blocked B", err)
	}
	unblock()
	got := cleanupOwnerWait(t, done)
	joined = true
	v.store.after = nil
	requireCleanupOwnerUnknown(t, got.out, got.err, original)
	if v.facts[p].writes != 1 || v.discovery != 0 {
		t.Fatal("Unknown continued cleanup")
	}
	wrong := v.facts[p].cause.Details()
	wrong.Version++
	wrongCause, _ := oc.NewProjectCleanupCause(wrong)
	if _, err := copyService.CleanupProject(context.Background(), v.facts[p].actor, wrongCause); err == nil {
		t.Fatal("changed cause borrowed a retained original")
	} else {
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.InvalidState || v.facts[p].writes != 1 {
			t.Fatal("changed cause bypassed retained identity", err)
		}
	}
	enteredConfirm, releaseConfirm := make(chan struct{}), make(chan struct{})
	var confirmOnce sync.Once
	unblockConfirm := func() { confirmOnce.Do(func() { close(releaseConfirm) }) }
	confirmed := make(chan output, 1)
	confirmJoined := false
	t.Cleanup(func() {
		unblockConfirm()
		if !confirmJoined {
			cleanupOwnerWait(t, confirmed)
		}
	})
	v.store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
		close(enteredConfirm)
		<-releaseConfirm
		return result
	}
	go func() { out, err := v.run(context.Background(), &copyService, p); confirmed <- output{out, err} }()
	cleanupOwnerWait(t, enteredConfirm)
	v.state.cleanupMu.Lock()
	owner := v.state.cleanupCalls[p]
	retained := owner != nil && owner.retained
	v.state.cleanupMu.Unlock()
	if !retained || v.facts[p].writes != 1 || v.facts[p].checks != 2 {
		t.Fatal("confirmation skipped gate, wrote, or released before original return")
	}
	unblockConfirm()
	got = cleanupOwnerWait(t, confirmed)
	confirmJoined = true
	v.store.after = nil
	if got.err != nil || got.out.State != oc.CleanupPending || v.facts[p].writes != 1 || len(v.state.cleanupCalls) != 0 {
		t.Fatal("read-only confirmation did not settle original", got.err)
	}
}

func TestArtifactProjectCleanupConfirmationKeepsUnknown(t *testing.T) {
	for _, mode := range []string{"gate-denied", "unknown", "not-committed", "cancelled", "skipped-callback"} {
		t.Run(mode, func(t *testing.T) {
			v, p := newCleanupOwnerFixture(t)
			original := v.unknown(t, p)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "gate-denied":
				v.deny = true
			case "unknown":
				attempt := newTestID[f.TransactionAttempt](t)
				v.store.after = func(_ context.Context, cause f.TransactionCause, _ f.CommitResult) f.CommitResult {
					return f.UnknownResult(attempt, cause)
				}
			case "not-committed":
				v.store.after = func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult {
					return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
				}
			case "cancelled":
				cancel()
			case "skipped-callback":
				v.store.before = func(context.Context, f.TransactionCause) *f.CommitResult {
					result := f.CommittedResult()
					return &result
				}
			}
			out, err := v.run(ctx, v.service, p)
			requireCleanupOwnerUnknown(t, out, err, original)
			if v.facts[p].writes != 1 || v.discovery != 0 {
				t.Fatal("confirmation repeated original mutation")
			}
			v.store.before, v.store.after, v.deny = nil, nil, false
			if _, err := v.run(context.Background(), v.service, p); err != nil || len(v.state.cleanupCalls) != 0 {
				t.Fatal("later explicit confirmation did not settle", err)
			}
		})
	}
}

func TestArtifactProjectCleanupRejectsUnboundComposition(t *testing.T) {
	v, p := newCleanupOwnerFixture(t)
	if err := v.service.RequireProjectCleanupBinding(); err == nil {
		t.Fatal("unbound fixed cleanup composition accepted")
	}
	v.state.objects, v.state.cleaner = &object.Service{}, &object.Service{}
	if err := v.service.RequireProjectCleanupBinding(); err == nil {
		t.Fatal("two concrete Object handles accepted")
	}
	v.store.before = func(context.Context, f.TransactionCause) *f.CommitResult {
		result := f.CommittedResult()
		return &result
	}
	if out, err := v.run(context.Background(), v.service, p); err == nil || out.State == oc.CleanupCompleted || v.facts[p].checks != 0 || v.facts[p].writes != 0 {
		t.Fatal("fake Committed callback published cleanup", err)
	}
}
