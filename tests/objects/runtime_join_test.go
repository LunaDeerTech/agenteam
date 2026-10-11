//go:build integration

package objects_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Account/Project authority uses this package's explicit controlled SQL fixture;
// this is not default App/production authorization evidence.
// This is ordinary lock contention after a committed work registration. It
// does not emulate a lost ROLLBACK response or preserve an abandoned backend.
func TestObjectProjectWorkPendingJoinKeepsRuntimeGuard(t *testing.T) {
	f := newFixture(t, false)
	process := id[oc.Process](t)
	spoolPath := filepath.Join(t.TempDir(), "spool")
	runtime, service, _ := runtimeAt(t, f, spoolPath, process, f.authority)
	if err := runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal("normal runtime initialization", err)
	}

	body := &stopBlockedBody{entered: make(chan struct{}), release: make(chan struct{})}
	var bodyOnce, holderOnce sync.Once
	releaseBody := func() { bodyOnce.Do(func() { close(body.release) }) }
	holderRelease := make(chan struct{})
	releaseHolder := func() { holderOnce.Do(func() { close(holderRelease) }) }
	publicDone := make(chan error, 1)
	publicObserved := false
	holderDone := make(chan foundation.CommitResult, 1)
	holderStarted, holderObserved := false, false
	var holderCancel context.CancelFunc
	t.Cleanup(func() {
		releaseBody()
		releaseHolder()
		if holderCancel != nil {
			defer holderCancel()
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if holderStarted && !holderObserved {
			select {
			case <-holderDone:
				holderObserved = true
			case <-cleanup.Done():
				if holderCancel != nil {
					holderCancel()
				}
				t.Error("ordinary holder did not finish within cleanup budget")
			}
		}
		if !publicObserved {
			select {
			case <-publicDone:
				publicObserved = true
			case <-cleanup.Done():
				t.Error("local body call did not return within cleanup budget")
			}
		}
	})
	publicCtx := contextFor(t)
	go func() {
		_, err := service.PreparePayload(publicCtx, f.actor, f.owner, "text/plain", 1, nil, body)
		publicDone <- err
	}()
	select {
	case <-body.entered:
	case err := <-publicDone:
		publicObserved = true
		t.Fatal("preparation returned before local body barrier", err)
	case <-publicCtx.Done():
		t.Fatal("local body barrier not reached")
	}

	var workID string
	var unjoined bool
	if err := f.store.QueryRow(contextFor(t), `SELECT id::text,joined_at IS NULL FROM agenteam_object.project_work WHERE project_id=$1 AND process_id=$2 AND kind='preparation'`, f.project.String(), process.String()).Scan(&workID, &unjoined); err != nil || !unjoined {
		t.Fatal("work was not committed before the local Read", err)
	}
	workCommand, err := foundation.NewCommandIdentity("object", []string{workID}, "project-work", "lifetime")
	if err != nil {
		t.Fatal(err)
	}
	workKey, err := foundation.CommandLock(workCommand)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := foundation.NewRecoveryCause("object", workID, "")
	if err != nil {
		t.Fatal(err)
	}
	holderCtx, cancelHolder := context.WithTimeout(contextFor(t), 15*time.Second)
	holderCancel = cancelHolder
	holderReady := make(chan int, 1)
	holderStarted = true
	go func() {
		result := f.store.WithinTx(holderCtx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: workKey, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var pid int
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			holderReady <- pid
			select {
			case <-holderRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		holderDone <- result
	}()
	var holderPID int
	select {
	case holderPID = <-holderReady:
	case result := <-holderDone:
		holderObserved = true
		t.Fatal("ordinary holder failed before acquiring work mutex", result.State())
	case <-holderCtx.Done():
		t.Fatal("ordinary holder did not acquire work mutex")
	}
	t.Logf("ordinary holder ready: work=%s backend=%d; no proxy", workID, holderPID)
	releaseBody()
	select {
	case err = <-publicDone:
		publicObserved = true
		if err == nil {
			t.Fatal("short local body unexpectedly prepared a payload")
		}
		t.Log("public preparation returned its normal short-body error:", err)
	case <-publicCtx.Done():
		t.Fatal("public preparation did not return after local body release")
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT joined_at IS NULL FROM agenteam_object.project_work WHERE id=$1`, workID).Scan(&unjoined); err != nil || !unjoined {
		t.Fatal("ordinary work mutex did not preserve pending join", err)
	}
	var holderStillInTx bool
	if err = f.store.QueryRow(contextFor(t), `SELECT state='idle in transaction' AND xact_start IS NOT NULL FROM pg_stat_activity WHERE pid=$1`, holderPID).Scan(&holderStillInTx); err != nil || !holderStillInTx {
		t.Fatal("normal holder was not still in its explicitly owned transaction", err)
	}

	runtime.StopAdmission()
	drainCtx, cancelDrain := context.WithTimeout(contextFor(t), 150*time.Millisecond)
	drainErr := runtime.Drain(drainCtx)
	cancelDrain()
	if drainErr == nil {
		t.Error("Runtime.Drain completed while real registered work was still unjoined")
	}
	var claim string
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&claim); err != nil {
		t.Fatal(err)
	}
	if claim != "claimed" {
		t.Errorf("pending work did not keep process claim active: state=%s", claim)
	}
	claimFile, err := os.OpenFile(filepath.Join(spoolPath+".processes", process.String()+".claim"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal("open the exact owned claim file", err)
	}
	lockErr := syscall.Flock(int(claimFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if lockErr == nil {
		_ = syscall.Flock(int(claimFile.Fd()), syscall.LOCK_UN)
		t.Error("ProcessGuard flock was released before pending work joined")
	} else if !errors.Is(lockErr, syscall.EWOULDBLOCK) && !errors.Is(lockErr, syscall.EAGAIN) {
		t.Error("unexpected local flock check error", lockErr)
	}
	if err = claimFile.Close(); err != nil {
		t.Error(err)
	}
	t.Logf("while ordinary holder remains: work_unjoined=%t drain_error=%v claim=%s flock_error=%v", unjoined, drainErr, claim, lockErr)

	releaseHolder()
	select {
	case result := <-holderDone:
		holderObserved = true
		if result.State() != foundation.Committed {
			t.Fatal("ordinary holder did not finish by confirmed commit", result.State())
		}
	case <-holderCtx.Done():
		t.Fatal("released holder did not finish")
	}
	finalCtx, cancelFinal := context.WithTimeout(contextFor(t), 2*time.Second)
	defer cancelFinal()
	if err = runtime.Drain(finalCtx); err != nil {
		t.Error("runtime did not reconcile an ended work after ordinary lock release", err)
	}
	var joined bool
	if err = f.store.QueryRow(contextFor(t), `SELECT joined_at IS NOT NULL FROM agenteam_object.project_work WHERE id=$1`, workID).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	if !joined {
		t.Error("runtime exited without a confirmed project_work join checkpoint")
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&claim); err != nil || claim != "stopped" {
		t.Error("confirmed joined runtime did not finalize its claim", err, claim)
	}
	claimFile, err = os.OpenFile(filepath.Join(spoolPath+".processes", process.String()+".claim"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(claimFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Error("confirmed joined runtime retained flock", err)
	} else {
		_ = syscall.Flock(int(claimFile.Fd()), syscall.LOCK_UN)
	}
	if err = claimFile.Close(); err != nil {
		t.Error(err)
	}
	t.Logf("after confirmed normal holder release: work_joined=%t claim=%s", joined, claim)
}

// These controls decorate formal Store/Authority ports. PostgreSQL owns every
// real transaction and its normal COMMIT/ROLLBACK. A return barrier is a local
// channel after the underlying WithinTx has actually returned, never a network
// fault, retained connection, or production hook.
type runtimeJoinObservation struct {
	kind, target     string
	tx               foundation.Tx
	pid              int
	batches          [][]foundation.LockRequest
	actual, reported foundation.CommitResult
}
type runtimeJoinGate struct {
	kind, target      string
	rollback, unknown bool
	attempt           foundation.ID[foundation.TransactionAttempt]
	reached           chan runtimeJoinObservation
	release           chan struct{}
	once              sync.Once
	claimed           bool // protected by runtimeJoinStore.mu
}

func (g *runtimeJoinGate) open() { g.once.Do(func() { close(g.release) }) }
func (g *runtimeJoinGate) wait(t *testing.T) runtimeJoinObservation {
	t.Helper()
	select {
	case v := <-g.reached:
		t.Logf("formal Store return barrier: kind=%s target=%s backend=%d underlying=%s exposed=%s", v.kind, v.target, v.pid, v.actual.State(), v.reported.State())
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("formal Store return barrier not reached", g.kind, g.target)
		return runtimeJoinObservation{}
	}
}

type runtimeJoinTx struct {
	tx          foundation.Tx
	observation runtimeJoinObservation
	gate        *runtimeJoinGate
}
type runtimeJoinNoCallback struct{ used atomic.Bool }
type runtimeJoinNoCallbackKey struct{}
type runtimeJoinStore struct {
	object.Store
	mu            sync.Mutex
	active        map[foundation.Tx]*runtimeJoinTx
	history       []*runtimeJoinTx
	gates         []*runtimeJoinGate
	rollbackJoins bool
}

func newRuntimeJoinStore(f *fixture) *runtimeJoinStore {
	return &runtimeJoinStore{Store: f.store, active: make(map[foundation.Tx]*runtimeJoinTx)}
}
func (s *runtimeJoinStore) gate(t *testing.T, kind, target string, rollback, unknown bool) *runtimeJoinGate {
	t.Helper()
	g := &runtimeJoinGate{kind: kind, target: target, rollback: rollback, unknown: unknown, attempt: id[foundation.TransactionAttempt](t), reached: make(chan runtimeJoinObservation, 1), release: make(chan struct{})}
	s.mu.Lock()
	s.gates = append(s.gates, g)
	s.mu.Unlock()
	t.Cleanup(g.open)
	return g
}
func (s *runtimeJoinStore) releaseAll() {
	s.mu.Lock()
	gates := append([]*runtimeJoinGate(nil), s.gates...)
	s.rollbackJoins = false
	s.mu.Unlock()
	for _, g := range gates {
		g.open()
	}
}
func (s *runtimeJoinStore) rollbacks(enabled bool) {
	s.mu.Lock()
	s.rollbackJoins = enabled
	s.mu.Unlock()
}
func (s *runtimeJoinStore) mark(tx foundation.Tx, kind, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.active[tx]; r != nil {
		r.observation.kind, r.observation.target = kind, target
	}
}
func (s *runtimeJoinStore) observations(kind, target string) []runtimeJoinObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []runtimeJoinObservation
	for _, r := range s.history {
		if r.observation.kind == kind && (target == "" || r.observation.target == target) {
			out = append(out, r.observation)
		}
	}
	return out
}
func (s *runtimeJoinStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	if control, ok := ctx.Value(runtimeJoinNoCallbackKey{}).(*runtimeJoinNoCallback); ok && control.used.CompareAndSwap(false, true) {
		// This exact public call is rejected before PostgreSQL/callback admission.
		return foundation.NotCommittedResult(foundation.NewFault(foundation.Forbidden, foundation.NotStarted))
	}
	r := &runtimeJoinTx{}
	actual := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		r.tx = tx
		s.mu.Lock()
		r.observation.tx = tx
		s.active[tx] = r
		s.history = append(s.history, r)
		s.mu.Unlock()
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var pid int
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		s.mu.Lock()
		r.observation.pid = pid
		s.mu.Unlock()
		err = fn(ctx, tx)
		s.mu.Lock()
		for _, g := range s.gates {
			if !g.claimed && g.kind == r.observation.kind && (g.target == "" || g.target == r.observation.target) {
				g.claimed = true
				r.gate = g
				break
			}
		}
		rollback := r.observation.kind == "join" && s.rollbackJoins || r.gate != nil && r.gate.rollback
		s.mu.Unlock()
		if err == nil && rollback {
			return foundation.NewFault(foundation.ResourceBusy, foundation.NotCommitted)
		}
		return err
	})
	reported := actual
	if r.gate != nil && r.gate.unknown && actual.State() == foundation.Committed {
		// Explicit result-state decoration, AFTER the real Committed result.
		reported = foundation.UnknownResult(r.gate.attempt, cause)
	}
	s.mu.Lock()
	r.observation.actual, r.observation.reported = actual, reported
	v := r.observation
	s.mu.Unlock()
	if r.gate != nil {
		r.gate.reached <- v
		<-r.gate.release
	}
	s.mu.Lock()
	delete(s.active, r.tx)
	s.mu.Unlock()
	return reported
}
func runtimeJoinWorkID(locks []foundation.LockRequest) string {
	for _, lock := range locks {
		var key struct {
			Namespace string   `json:"namespace"`
			OwnerIDs  []string `json:"owner_ids"`
			Command   string   `json:"command"`
			Key       string   `json:"key"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(lock.Key.Canonical(), "command:")), &key) == nil && key.Namespace == "object" && key.Command == "project-work" && key.Key == "lifetime" && len(key.OwnerIDs) == 1 {
			return key.OwnerIDs[0]
		}
	}
	return ""
}
func (s *runtimeJoinStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	if r := s.active[tx]; r != nil {
		r.observation.batches = append(r.observation.batches, append([]foundation.LockRequest(nil), locks...))
		allExclusive := len(locks) > 0
		for _, lock := range locks {
			allExclusive = allExclusive && lock.Mode == foundation.Exclusive
		}
		if work := runtimeJoinWorkID(locks); work != "" && allExclusive {
			r.observation.kind, r.observation.target = "join", work
		}
	}
	s.mu.Unlock()
	return s.Store.AcquireAll(ctx, tx, locks)
}
func (s *runtimeJoinStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	x, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return runtimeJoinExecutor{SQLExecutor: x, store: s, tx: tx}, nil
}

type runtimeJoinExecutor struct {
	postgres.SQLExecutor
	store *runtimeJoinStore
	tx    foundation.Tx
}

func (x runtimeJoinExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "UPDATE agenteam_object.project_work SET joined_at=") && len(args) == 1 {
		if target, ok := args[0].(string); ok {
			x.store.mark(x.tx, "join", target)
		}
	}
	if strings.HasPrefix(query, "UPDATE agenteam_object.process_claims SET state='stopped'") && len(args) == 1 {
		if target, ok := args[0].(string); ok {
			x.store.mark(x.tx, "guard", target)
		}
	}
	return x.SQLExecutor.Exec(ctx, query, args...)
}

// A separate real transaction owns one exact captured lock. Release always
// returns nil from its callback and observes the original Committed result.
type runtimeJoinHolder struct {
	release chan struct{}
	done    chan struct{}
	once    sync.Once
	result  foundation.CommitResult
	tx      foundation.Tx
	pid     int
	cancel  context.CancelFunc
}

func (h *runtimeJoinHolder) open() { h.once.Do(func() { close(h.release) }) }
func (h *runtimeJoinHolder) finish(t *testing.T) {
	t.Helper()
	h.open()
	select {
	case <-h.done:
		if h.result.State() != foundation.Committed {
			t.Fatal("normal holder did not commit", h.result.State())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("normal holder did not actually return")
	}
}
func runtimeJoinHold(t *testing.T, f *fixture, key foundation.LockKey) *runtimeJoinHolder {
	t.Helper()
	ctx, cancel := context.WithTimeout(contextFor(t), 15*time.Second)
	h := &runtimeJoinHolder{release: make(chan struct{}), done: make(chan struct{}), cancel: cancel}
	ready := make(chan int, 1)
	t.Cleanup(func() {
		h.open()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			cancel()
			t.Error("ordinary holder exceeded cleanup budget")
			<-h.done
		}
		cancel()
	})
	transactionCause := cause(t)
	go func() {
		defer close(h.done)
		h.result = f.store.WithinTx(ctx, transactionCause, func(ctx context.Context, tx foundation.Tx) error {
			h.tx = tx
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var pid int
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			ready <- pid
			select {
			case <-h.release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case h.pid = <-ready:
	case <-h.done:
		t.Fatal("holder returned before lock acquisition", h.result.State())
	case <-ctx.Done():
		t.Fatal("holder lock acquisition expired")
	}
	var held bool
	keyHash := uint64(key.AdvisoryKey())
	if err := f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND granted AND classid::bigint=$2 AND objid::bigint=$3 AND objsubid=1)`, h.pid, int64(keyHash>>32), int64(keyHash&0xffffffff)).Scan(&held); err != nil || !held {
		t.Fatal("holder lacks exact granted advisory tuple", err)
	}
	t.Logf("ordinary canary backend=%d advisory=(%d,%d,1) granted=true", h.pid, keyHash>>32, keyHash&0xffffffff)
	return h
}

type runtimeJoinCall struct {
	done   chan struct{}
	err    error
	cancel context.CancelFunc
}

func runtimeJoinAsync(t *testing.T, store *runtimeJoinStore, budget time.Duration, fn func(context.Context) error) *runtimeJoinCall {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	c := &runtimeJoinCall{done: make(chan struct{}), cancel: cancel}
	go func() { defer close(c.done); c.err = fn(ctx) }()
	t.Cleanup(func() {
		store.releaseAll()
		cancel()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			t.Error("owned call exceeded cleanup budget")
			<-c.done
		}
	})
	return c
}
func (c *runtimeJoinCall) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-c.done:
		return c.err
	case <-time.After(5 * time.Second):
		t.Fatal("owned call did not actually return")
		return nil
	}
}
func (c *runtimeJoinCall) blocked(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
		t.Fatal("original owner returned before local barrier release", c.err)
	default:
	}
}
func runtimeJoinShortWaiter(t *testing.T, store *runtimeJoinStore, fn func(context.Context) error) {
	t.Helper()
	start := time.Now()
	c := runtimeJoinAsync(t, store, 80*time.Millisecond, fn)
	select {
	case <-c.done:
		if c.err == nil {
			t.Fatal("short waiter retired an original blocked owner")
		}
		if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
			t.Fatal("short waiter exceeded its own context", elapsed)
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("short waiter remained blocked behind another caller's context")
	}
}

// This mirrors runtimeAt's real assembly, adding only a formal Store port.
// standalone is used solely for the separately declared recovery-port control.
func runtimeJoinInstance(t *testing.T, f *fixture, store *runtimeJoinStore, planner oc.AccessPlanner, standalone bool, processPort func(*object.ProcessGuard) oc.ProcessAuthority) (*object.Runtime, *object.Service, oc.ProcessID, string) {
	t.Helper()
	process := id[oc.Process](t)
	path := filepath.Join(t.TempDir(), "spool")
	backend, err := object.NewBackend(f.config)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(path, process)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auditing, err := audit.New(f.store, keys, audit.Authorizations{Projects: auditAuthority{f.authority}})
	if err != nil {
		t.Fatal(err)
	}
	var processes oc.ProcessAuthority = guard
	if processPort != nil {
		processes = processPort(guard)
	}
	service, err := object.New(store, backend, spool, auditing, object.Authorizations{Planner: planner, Processes: processes, Resources: f.authority, Read: f.authority, Gate: f.authority, Cleanup: f.authority, Leases: f.authority})
	if err != nil {
		t.Fatal(err)
	}
	if standalone {
		t.Cleanup(func() {
			store.releaseAll()
			service.StopAdmission()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := service.Drain(ctx); err != nil {
				t.Error("standalone Service actual Drain", err)
				return // Never release its constructor guard after an unjoined Service.
			}
			if err := guard.Close(); err != nil {
				t.Error("unbound constructor guard Close", err)
			}
		})
		if err := service.Initialize(contextFor(t)); err != nil {
			t.Fatal(err)
		}
		return nil, service, process, path
	}
	endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, f.config)
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := object.NewTransferService(service, nil, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := object.NewRuntime(service, guard, transfers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.releaseAll()
		runtime.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtime.Drain(ctx); err != nil {
			_ = runtime.Force(ctx)
			t.Error("Runtime actual Drain", err)
		}
	})
	if err := runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return runtime, service, process, path
}
func runtimeJoinPending(t *testing.T, f *fixture, store *runtimeJoinStore, service *object.Service, process oc.ProcessID, count int) []string {
	t.Helper()
	store.rollbacks(true)
	for i := 0; i < count; i++ {
		_, err := service.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 1, nil, io.NopCloser(strings.NewReader("")))
		requireCode(t, err, foundation.InvalidArgument)
	}
	store.rollbacks(false)
	rows, err := f.store.Query(contextFor(t), `SELECT id::text FROM agenteam_object.project_work WHERE project_id=$1 AND process_id=$2 AND kind='preparation' AND joined_at IS NULL ORDER BY id`, f.project.String(), process.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != count {
		t.Fatal("normal failed preparations did not retain exact pending work", len(ids), count)
	}
	return ids
}
func runtimeJoinCheckpoint(t *testing.T, f *fixture, work string) (string, string) {
	t.Helper()
	var identity string
	var joined *string
	if err := f.store.QueryRow(contextFor(t), `SELECT jsonb_build_array(id,project_id,process_id,kind,resource_id,object_id,admission_version,admission_operation,revoked_by,cleanup_claim_fence)::text,joined_at::text FROM agenteam_object.project_work WHERE id=$1`, work).Scan(&identity, &joined); err != nil {
		t.Fatal(err)
	}
	if joined == nil {
		return identity, ""
	}
	return identity, *joined
}
func runtimeJoinGuard(t *testing.T, f *fixture, process oc.ProcessID, path, state string, held bool) {
	t.Helper()
	var actual string
	if err := f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&actual); err != nil || actual != state {
		t.Fatal("exact process claim", err, actual, state)
	}
	file, err := os.OpenFile(filepath.Join(path+".processes", process.String()+".claim"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}
	if held && !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
		t.Fatal("original claim flock no longer held", err)
	}
	if !held && err != nil {
		t.Fatal("completed guard retained flock", err)
	}
}
func runtimeJoinCounts(t *testing.T, f *fixture) [4]int64 {
	t.Helper()
	var out [4]int64
	if err := f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.project_work),(SELECT count(*) FROM agenteam_object.object_leases),(SELECT count(*) FROM agenteam_object.objects),(SELECT count(*) FROM agenteam_object.uploads)`).Scan(&out[0], &out[1], &out[2], &out[3]); err != nil {
		t.Fatal(err)
	}
	return out
}
func runtimeJoinUnion(t *testing.T, observation runtimeJoinObservation, original []foundation.LockRequest) {
	t.Helper()
	if len(observation.batches) != 1 {
		t.Fatal("confirmation did not use one complete AcquireAll", len(observation.batches))
	}
	locks := observation.batches[0]
	found := make(map[string]bool, len(locks))
	for i, lock := range locks {
		if lock.Mode != foundation.Exclusive {
			t.Fatal("confirmation union was not entirely Exclusive")
		}
		if i > 0 && foundation.CompareLockKeys(locks[i-1].Key, lock.Key) >= 0 {
			t.Fatal("confirmation union not normalized once")
		}
		found[lock.Key.Canonical()] = true
	}
	for _, lock := range original {
		if !found[lock.Key.Canonical()] {
			t.Fatal("confirmation dropped an original union dependency", lock.Key.Canonical())
		}
	}
	if observation.target == "" || runtimeJoinWorkID(locks) != observation.target {
		t.Fatal("confirmation changed original work identity")
	}
}

func TestObjectRuntimeJoinConcurrentDrainAndForceBudget(t *testing.T) {
	for _, stage := range []string{"join", "guard"} {
		t.Run(stage+"-return-barrier", func(t *testing.T) {
			f := newFixture(t, false)
			store := newRuntimeJoinStore(f)
			runtime, service, process, path := runtimeJoinInstance(t, f, store, f.authority, false, nil)
			work := runtimeJoinPending(t, f, store, service, process, 1)[0]
			identity, _ := runtimeJoinCheckpoint(t, f, work)
			target := work
			if stage == "guard" {
				target = process.String()
			}
			gate := store.gate(t, stage, target, false, false)
			before := len(store.observations(stage, target))
			runtime.StopAdmission()
			owner := runtimeJoinAsync(t, store, 5*time.Second, runtime.Drain)
			at := gate.wait(t)
			if at.actual.State() != foundation.Committed || at.reported.State() != foundation.Committed {
				t.Fatal("barrier must follow real Committed", at.actual.State(), at.reported.State())
			}
			if stage == "join" {
				runtimeJoinUnion(t, at, nil)
			}
			actual, joined := runtimeJoinCheckpoint(t, f, work)
			if actual != identity || joined == "" {
				t.Fatal("real join was not durably committed before return barrier")
			}
			claim := "claimed"
			if stage == "guard" {
				claim = "stopped"
			}
			runtimeJoinGuard(t, f, process, path, claim, true)
			runtimeJoinShortWaiter(t, store, runtime.Force)
			owner.blocked(t)
			if got := len(store.observations(stage, target)); got != before+1 {
				t.Fatal("parallel waiter entered duplicate confirmation", got, before)
			}
			runtimeJoinGuard(t, f, process, path, claim, true)
			gate.open()
			if err := owner.wait(t); err != nil {
				t.Fatal("original Drain failed after actual return release", err)
			}
			if err := runtime.Drain(contextFor(t)); err != nil {
				t.Fatal("repeat Drain", err)
			}
			if err := runtime.Force(contextFor(t)); err != nil {
				t.Fatal("repeat Force", err)
			}
			runtimeJoinGuard(t, f, process, path, "stopped", false)
			if finalIdentity, finalJoined := runtimeJoinCheckpoint(t, f, work); finalIdentity != identity || finalJoined != joined {
				t.Fatal("repeat exits changed original work checkpoint")
			}
		})
	}
}

type runtimeJoinReadPlanner struct {
	oc.AccessPlanner
	store        *runtimeJoinStore
	operation    oc.AccessOperation
	object       oc.ObjectID
	mu           sync.Mutex
	dependencies []foundation.LockRequest
	validations  int
}

func (p *runtimeJoinReadPlanner) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, dependencies oc.AccessDependencies) error {
	if err := p.AccessPlanner.ValidateInTx(ctx, tx, request, dependencies); err != nil {
		return err
	}
	d := request.Details()
	if d.Operation != p.operation || d.ObjectID != p.object {
		return nil
	}
	p.mu.Lock()
	p.dependencies = dependencies.Locks()
	p.validations++
	p.mu.Unlock()
	p.store.mark(tx, "read-admission", d.ObjectID.String())
	return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
}
func TestObjectRuntimeJoinFirstReadWriterBeforeRegistration(t *testing.T) {
	cases := []struct {
		name           string
		operation      oc.AccessOperation
		beforeCallback bool
	}{
		{"read-authority-before-register", oc.ReadAccess, false},
		{"source-authority-before-register", oc.OpenSourceAccess, false},
		{"read-before-callback", oc.ReadAccess, true},
		{"source-before-callback", oc.OpenSourceAccess, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.operation == oc.OpenSourceAccess)
			put := f.put(t, "runtime-join-first-read", "real MinIO content")
			store := newRuntimeJoinStore(f)
			planner := &runtimeJoinReadPlanner{AccessPlanner: f.authority, store: store, operation: tc.operation, object: put.Meta.ID}
			var access oc.AccessPlanner = planner
			if tc.beforeCallback {
				access = f.authority
			}
			runtime, service, process, path := runtimeJoinInstance(t, f, store, access, false, nil)
			before := runtimeJoinCounts(t, f)
			var reader *oc.ObjectReader
			callRead := func(ctx context.Context) error {
				var err error
				if tc.operation == oc.OpenSourceAccess {
					reader, err = service.OpenUploadSource(ctx, f.actor, f.owner, put.Receipt)
				} else {
					reader, err = service.ReadObject(ctx, f.actor, f.owner, put.Meta.ID, nil)
				}
				return err
			}
			checkError := func(err error) {
				requireCode(t, err, foundation.Forbidden)
				var fault *foundation.Fault
				if !errors.As(err, &fault) || fault.CommitState != foundation.NotCommitted {
					t.Fatal("original read rejection changed its public commit state", err)
				}
				if reader != nil {
					_ = reader.Close()
					t.Fatal("rejected read produced a reader")
				}
				if after := runtimeJoinCounts(t, f); after != before {
					t.Fatal("rejected read added work, lease, object or upload rows", before, after)
				}
			}
			if tc.beforeCallback {
				control := &runtimeJoinNoCallback{}
				checkError(callRead(context.WithValue(contextFor(t), runtimeJoinNoCallbackKey{}, control)))
				if !control.used.Load() {
					t.Fatal("exact original call did not reach pre-callback rejection")
				}
				if len(store.observations("join", "")) != 0 {
					t.Fatal("no-callback rejection fabricated confirmation ownership")
				}
				runtime.StopAdmission()
				if err := runtime.Drain(contextFor(t)); err != nil {
					t.Fatal("no-callback failure retained an owner", err)
				}
				runtimeJoinGuard(t, f, process, path, "stopped", false)
				t.Log("exact formal Store rejection preceded callback; nil reader and zero new work/lease; no independent MinIO GET counter")
				return
			}
			gate := store.gate(t, "read-admission", put.Meta.ID.String(), false, false)
			public := runtimeJoinAsync(t, store, 10*time.Second, callRead)
			at := gate.wait(t)
			if at.actual.State() != foundation.NotCommitted || len(at.batches) != 1 {
				t.Fatal("read authority did not reject after its first complete real Acquire", at.actual.State(), len(at.batches))
			}
			original := at.batches[0]
			work := runtimeJoinWorkID(original)
			if work == "" {
				t.Fatal("original read union omitted its exact work mutex")
			}
			planner.mu.Lock()
			dependencies := append([]foundation.LockRequest(nil), planner.dependencies...)
			validations := planner.validations
			planner.mu.Unlock()
			if validations != 1 {
				t.Fatal("authority denial was not exact original operation", validations)
			}
			found := make(map[string]foundation.LockMode)
			for _, lock := range original {
				found[lock.Key.Canonical()] = lock.Mode
			}
			for _, lock := range dependencies {
				if mode, ok := found[lock.Key.Canonical()]; !ok || mode != lock.Mode && mode != foundation.Exclusive {
					t.Fatal("first captured union lost authority dependency")
				}
			}
			canaryKey, err := foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-owner:"+f.owner.Details().ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode, ok := found[canaryKey.Canonical()]; !ok || mode != foundation.Shared {
				t.Fatal("selected canary is not original shared owner dependency")
			}
			holder := runtimeJoinHold(t, f, canaryKey)
			// The original PG transaction has already rolled back. The normal
			// pool may reuse its idle backend for this distinct canary Tx;
			// PID inequality is not a transaction-ownership contract.
			if holder.tx == at.tx || !holder.tx.Valid() || !at.tx.Valid() {
				t.Fatal("canary did not enter a distinct real callback-local Tx")
			}
			gate.open()
			waitAdvisory(t, f, canaryKey)
			checkError(public.wait(t))
			// The public operation has ended. Only the failed first writer and
			// its conservative confirmation can now keep the Runtime alive.
			runtime.StopAdmission()
			runtimeJoinShortWaiter(t, store, runtime.Drain)
			runtimeJoinGuard(t, f, process, path, "claimed", true)
			confirmations := store.observations("join", work)
			if len(confirmations) == 0 {
				t.Fatal("failed first writer never entered same-work confirmation")
			}
			for _, confirmation := range confirmations {
				runtimeJoinUnion(t, confirmation, original)
			}
			holder.finish(t)
			if err := runtime.Drain(contextFor(t)); err != nil {
				t.Fatal("same-lock absence confirmation did not finish", err)
			}
			confirmations = store.observations("join", work)
			last := confirmations[len(confirmations)-1]
			runtimeJoinUnion(t, last, original)
			if last.actual.State() != foundation.Committed || last.reported.State() != foundation.Committed {
				t.Fatal("serialized absence did not receive original Committed return")
			}
			if after := runtimeJoinCounts(t, f); after != before {
				t.Fatal("absence confirmation fabricated durable work/lease", before, after)
			}
			runtimeJoinGuard(t, f, process, path, "stopped", false)
			t.Logf("original read backend=%d, exact canary backend=%d, original work=%s; nil reader/unchanged Forbidden/not_committed and zero row additions", at.pid, holder.pid, work)
			t.Log("MinIO GET count is not independently instrumented: openRead returns this original failed transaction before backend.get; no proxy or production hook")
		})
	}
}

// Only the exact declared former process receives a controlled affirmative
// ProcessAuthority input. This tests recovery-created handle ownership, not
// actual process death, foreign join, or default App authorization.
type runtimeJoinRecoveryProcess struct {
	oc.ProcessAuthority
	former oc.ProcessID
	calls  atomic.Int64
}

func (p *runtimeJoinRecoveryProcess) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	if process == p.former {
		p.calls.Add(1)
		return nil
	}
	return p.ProcessAuthority.ConfirmStopped(ctx, process)
}
func TestObjectRuntimeJoinConfirmationFailureRetainsOwnership(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "callback-rollback"
		if unknown {
			name = "committed-decorated-unknown"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, false)
			store := newRuntimeJoinStore(f)
			runtime, service, process, path := runtimeJoinInstance(t, f, store, f.authority, false, nil)
			work := runtimeJoinPending(t, f, store, service, process, 1)[0]
			identity, _ := runtimeJoinCheckpoint(t, f, work)
			first := store.gate(t, "join", work, !unknown, unknown)
			second := store.gate(t, "join", work, false, false)
			runtime.StopAdmission()
			owner := runtimeJoinAsync(t, store, 5*time.Second, runtime.Drain)
			at := first.wait(t)
			runtimeJoinUnion(t, at, nil)
			if unknown {
				if at.actual.State() != foundation.Committed || at.reported.State() != foundation.Unknown || at.reported.AttemptID() != first.attempt || at.reported.Cause().Validate() != nil {
					t.Fatal("Unknown did not follow the actual original Committed result and cause")
				}
			} else if at.actual.State() != foundation.NotCommitted || at.reported.State() != foundation.NotCommitted || at.reported.Fault() == nil || at.reported.Fault().Code != foundation.ResourceBusy {
				t.Fatal("normal callback rollback changed its original result")
			}
			current, firstJoined := runtimeJoinCheckpoint(t, f, work)
			if current != identity || (firstJoined != "") != unknown {
				t.Fatal("underlying transaction did not match canonical checkpoint")
			}
			runtimeJoinShortWaiter(t, store, runtime.Drain)
			owner.blocked(t)
			runtimeJoinGuard(t, f, process, path, "claimed", true)
			first.open()
			retry := second.wait(t)
			runtimeJoinUnion(t, retry, at.batches[0])
			if retry.actual.State() != foundation.Committed {
				t.Fatal("normal retry did not actually commit")
			}
			current, joined := runtimeJoinCheckpoint(t, f, work)
			if current != identity || joined == "" || unknown && joined != firstJoined {
				t.Fatal("retry replaced identity or original joined checkpoint")
			}
			runtimeJoinShortWaiter(t, store, runtime.Force)
			owner.blocked(t)
			runtimeJoinGuard(t, f, process, path, "claimed", true)
			second.open()
			if err := owner.wait(t); err != nil {
				t.Fatal("same Runtime failed to retire confirmed work", err)
			}
			runtimeJoinGuard(t, f, process, path, "stopped", false)
			if finalIdentity, finalJoined := runtimeJoinCheckpoint(t, f, work); finalIdentity != identity || finalJoined != joined {
				t.Fatal("final exit changed canonical checkpoint")
			}
		})
	}
	t.Run("recovery-handle-confirmation-owner", func(t *testing.T) {
		f := newFixture(t, false)
		originalStore := newRuntimeJoinStore(f)
		_, original, former, _ := runtimeJoinInstance(t, f, originalStore, f.authority, true, nil)
		work := runtimeJoinPending(t, f, originalStore, original, former, 1)[0]
		identity, _ := runtimeJoinCheckpoint(t, f, work)
		store := newRuntimeJoinStore(f)
		var authority *runtimeJoinRecoveryProcess
		_, service, _, _ := runtimeJoinInstance(t, f, store, f.authority, true, func(guard *object.ProcessGuard) oc.ProcessAuthority {
			authority = &runtimeJoinRecoveryProcess{ProcessAuthority: guard, former: former}
			return authority
		})
		t.Logf("controlled ProcessAuthority input ONLY for exact process=%s; both Services are real standalone instances; this is not process-death/foreign-join proof", former.String())
		first := store.gate(t, "join", work, false, true)
		recovery := runtimeJoinAsync(t, store, 5*time.Second, service.Recover)
		at := first.wait(t)
		if at.actual.State() != foundation.Committed || at.reported.State() != foundation.Unknown {
			t.Fatal("recovery confirmation did not reach explicit committed-Unknown boundary")
		}
		if authority.calls.Load() != 1 {
			t.Fatal("recovery did not construct handle using the exact controlled process port")
		}
		runtimeJoinUnion(t, at, nil)
		_, joined := runtimeJoinCheckpoint(t, f, work)
		if joined == "" {
			t.Fatal("recovery checkpoint was not actually committed")
		}
		runtimeJoinShortWaiter(t, store, service.Drain)
		recovery.blocked(t)
		first.open()
		requireCode(t, recovery.wait(t), foundation.CommitUnknown)
		// Recover's next SQL query excludes this already-joined row. Drain
		// therefore has to retain the recovery-constructed confirmation owner.
		second := store.gate(t, "join", work, false, false)
		service.StopAdmission()
		owner := runtimeJoinAsync(t, store, 5*time.Second, service.Drain)
		retry := second.wait(t)
		runtimeJoinUnion(t, retry, at.batches[0])
		if retry.actual.State() != foundation.Committed {
			t.Fatal("recovery owner retry did not commit")
		}
		runtimeJoinShortWaiter(t, store, service.Force)
		owner.blocked(t)
		second.open()
		if err := owner.wait(t); err != nil {
			t.Fatal("recovery-created owner failed actual Drain", err)
		}
		original.StopAdmission()
		if err := original.Drain(contextFor(t)); err != nil {
			t.Fatal("original standalone Service failed its own true Drain", err)
		}
		if finalIdentity, finalJoined := runtimeJoinCheckpoint(t, f, work); finalIdentity != identity || finalJoined != joined {
			t.Fatal("recovery changed original identity/checkpoint")
		}
	})
}

func TestObjectRuntimeJoinReadyWorkMakesProgress(t *testing.T) {
	t.Run("blocked-low-id-ready-later-id", func(t *testing.T) {
		f := newFixture(t, false)
		store := newRuntimeJoinStore(f)
		runtime, service, process, path := runtimeJoinInstance(t, f, store, f.authority, false, nil)
		works := runtimeJoinPending(t, f, store, service, process, 2)
		lowIdentity, _ := runtimeJoinCheckpoint(t, f, works[0])
		highIdentity, _ := runtimeJoinCheckpoint(t, f, works[1])
		before := runtimeJoinCounts(t, f)
		command, err := foundation.NewCommandIdentity("object", []string{works[0]}, "project-work", "lifetime")
		if err != nil {
			t.Fatal(err)
		}
		key, err := foundation.CommandLock(command)
		if err != nil {
			t.Fatal(err)
		}
		holder := runtimeJoinHold(t, f, key)
		runtime.StopAdmission()
		owner := runtimeJoinAsync(t, store, 400*time.Millisecond, runtime.Drain)
		waitAdvisory(t, f, key)
		if err := owner.wait(t); err == nil {
			t.Fatal("bounded Drain ignored blocked low work")
		}
		currentLow, lowJoined := runtimeJoinCheckpoint(t, f, works[0])
		currentHigh, highJoined := runtimeJoinCheckpoint(t, f, works[1])
		if currentLow != lowIdentity || currentHigh != highIdentity || lowJoined != "" || highJoined == "" {
			t.Fatal("low-ID contention starved ready later work or changed work identity")
		}
		select {
		case <-holder.done:
			t.Fatal("ordinary holder ended before fairness observation")
		default:
		}
		runtimeJoinGuard(t, f, process, path, "claimed", true)
		if after := runtimeJoinCounts(t, f); after != before {
			t.Fatal("Drain started new durable work/lease/object/upload", before, after)
		}
		t.Logf("within original 400ms Drain: lower=%s still pending behind backend=%d; later=%s joined; no payload was prepared or sent", works[0], holder.pid, works[1])
		holder.finish(t)
		if err := runtime.Drain(contextFor(t)); err != nil {
			t.Fatal("same Runtime did not finish after holder's normal commit", err)
		}
		if finalLow, joined := runtimeJoinCheckpoint(t, f, works[0]); finalLow != lowIdentity || joined == "" {
			t.Fatal("released lower work did not join")
		}
		if finalHigh, joined := runtimeJoinCheckpoint(t, f, works[1]); finalHigh != highIdentity || joined != highJoined {
			t.Fatal("already joined later checkpoint changed")
		}
		if after := runtimeJoinCounts(t, f); after != before {
			t.Fatal("final Drain introduced business work", before, after)
		}
		runtimeJoinGuard(t, f, process, path, "stopped", false)
	})
}
