//go:build integration

package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type independentConfirmationCall struct {
	pid       int32
	original  context.Context
	cancelled <-chan struct{}
	returned  <-chan struct{}
}

type independentForceObservation struct {
	ctx context.Context
	at  time.Time
}

// This wrapper observes the real transaction and real CommitResult. Only the
// confirmation callback is held after BEGIN and PID observation. No outcome,
// authorization, original callback or successful join is substituted.
type independentConfirmationStore struct {
	*postgres.Store
	key         f.IdempotencyKey
	proxy       *commitproxy.Proxy
	entered     chan independentConfirmationCall
	release     chan struct{}
	releaseOnce sync.Once
	unknown     chan f.CommitResult
	forced      chan independentForceObservation
	forceOnce   sync.Once
	writer      atomic.Int32
}

func (s *independentConfirmationStore) unhold() { s.releaseOnce.Do(func() { close(s.release) }) }
func (s *independentConfirmationStore) ForceClose(ctx context.Context) error {
	s.forceOnce.Do(func() { s.forced <- independentForceObservation{ctx, time.Now()} })
	return s.Store.ForceClose(ctx)
}
func (s *independentConfirmationStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	target := cause.Kind() == f.CommandsCause && cause.Details().Primary.Key() == s.key
	if !target {
		return s.Store.WithinTx(ctx, cause, fn)
	}
	confirmation := false
	select {
	case <-s.proxy.Reached():
		confirmation = true
	default:
	}
	returned := make(chan struct{})
	defer close(returned)
	originalContext := ctx
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		x, e := s.Store.InTx(tx)
		if e != nil {
			return e
		}
		var pid int32
		if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
			return e
		}
		if confirmation {
			s.entered <- independentConfirmationCall{pid, originalContext, ctx.Done(), returned}
			<-s.release
			return fn(ctx, tx)
		}
		if e = fn(ctx, tx); e != nil {
			return e
		}
		var completed bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.commands WHERE idempotency_key=$1 AND state='completed')`, string(s.key)).Scan(&completed); e != nil {
			return e
		}
		if completed {
			if e = s.proxy.Arm(pid); e != nil {
				return e
			}
			s.writer.Store(pid)
		}
		return nil
	})
	if !confirmation && result.State() == f.Unknown {
		s.unknown <- result
	}
	return result
}

func independentForceWait(t *testing.T, ch <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(6 * time.Second):
		t.Fatal("independent actual boundary unavailable", stage)
	}
}

func TestIndependentProjectVariablesRootConfirmationForce(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy, e := commitproxy.New(databaseTestContext(t), net.JoinHostPort("127.0.0.1", db.Fixture.Port))
	if e != nil {
		t.Fatal("independent proxy unavailable")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if proxy.Close(ctx) != nil {
			t.Error("independent proxy failed actual join")
		}
	})
	address, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal("owned database URL malformed")
	}
	address.Host = proxy.Address()
	env := slices.DeleteFunc(guardEnvironment(t, db), func(entry string) bool { return strings.HasPrefix(entry, "AGENTEAM_CENTRAL_DATABASE_CA_FILE=") })
	cfg := guardConfiguration(t, append(env, "AGENTEAM_CENTRAL_DATABASE_URL="+address.String(), "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=1s"))
	key := f.IdempotencyKey("independent-force-" + guardID[f.Request](t).String())
	var store *independentConfirmationStore
	var owner *resources
	var accounts *account.Service
	deps := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			raw, e := postgres.Open(ctx, cfg)
			if e != nil {
				return nil, e
			}
			store = &independentConfirmationStore{Store: raw, key: key, proxy: proxy, entered: make(chan independentConfirmationCall, 1), release: make(chan struct{}), unknown: make(chan f.CommitResult, 1), forced: make(chan independentForceObservation, 1)}
			return store, nil
		},
		observeAccount: func(v *account.Service) { accounts = v },
		bind: func(ctx context.Context, cfg config.Config, db database, r *resources, d *dependencies) error {
			owner = r
			return bindAccounts(ctx, cfg, db, r, d)
		},
	}
	output := newEventLog()
	logger, e := logging.New(logging.Central, slog.LevelInfo, output)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	var runError error
	go func() { runError = run(ctx, cfg, logger, signals, deps); close(done) }()
	t.Cleanup(func() {
		if store != nil {
			store.unhold()
		}
		proxy.Release()
		cancel()
		independentForceWait(t, done, "root cleanup")
	})
	output.wait(t, func(v map[string]any) bool { return v["event"] == "listening" })
	actor, e := fixtureAccountActor(databaseTestContext(t), cfg, accounts)
	if e != nil {
		t.Fatal("real Account actor unavailable")
	}
	project := independentForceProject(t, db, cfg, actor)
	variables := owner.accounts().(*accountAssembly).variables.(*projectVariablesAssembly)
	variableID := guardID[identity.ProjectVariable](t)
	request, e := vc.NewVariableCreate(vc.VariableCreateFields{ID: variableID, Name: "INDEPENDENT_FORCE", Description: "", Value: "independent-force-value-canary"})
	if e != nil {
		t.Fatal("independent command request invalid")
	}
	result := make(chan error, 1)
	callJoined := make(chan struct{})
	go func() {
		defer close(callJoined)
		_, e := variables.service.CreateVariable(context.Background(), actor, f.CommandMeta{IdempotencyKey: key}, project.ID, request)
		result <- e
	}()
	t.Cleanup(func() {
		store.unhold()
		proxy.Release()
		cancel()
		independentForceWait(t, callJoined, "mutation callback cleanup")
	})
	independentForceWait(t, proxy.Reached(), "complete original COMMIT")
	var call independentConfirmationCall
	select {
	case call = <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("real confirmation transaction not entered")
	}
	var original f.CommitResult
	select {
	case original = <-store.unknown:
	default:
		t.Fatal("physical original Unknown not observed")
	}
	if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Details().Primary.Key() != key || call.pid <= 0 || call.pid == store.writer.Load() {
		t.Fatal("confirmation did not follow exact physical original attempt")
	}
	admin := db.Connect(t)
	var active bool
	if e = admin.QueryRow(databaseTestContext(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1 AND xact_start IS NOT NULL)`, call.pid).Scan(&active); e != nil || !active {
		t.Fatal("confirmation lacks actual live transaction")
	}
	deadline, bounded := call.original.Deadline()
	signalAt := time.Now()
	if !bounded || call.original.Err() != nil || !signalAt.Before(deadline) {
		t.Fatal("invalid stimulus: confirmation already cancelled or unbounded")
	}
	select {
	case <-call.cancelled:
		t.Fatal("invalid stimulus: transaction already cancelled before signal")
	default:
	}
	signals <- syscall.SIGTERM
	independentForceWait(t, call.original.Done(), "original confirmation cancellation")
	cancelObservedAt := time.Now()
	// All timestamps carry Go's monotonic clock. Observing Done strictly before
	// the unchanged original deadline excludes a natural deadline winning the
	// race after the pre-signal check. A late/equal observation is inconclusive.
	if call.original.Err() != context.Canceled || !signalAt.Before(cancelObservedAt) || !cancelObservedAt.Before(deadline) {
		t.Fatal("invalid stimulus: root cancellation before original deadline unproved")
	}
	independentForceWait(t, call.cancelled, "confirmation context cancellation")
	if variables.Joined() {
		t.Fatal("cancelled confirmation falsely joined")
	}
	independentForceWait(t, done, "bounded root Force return")
	if runError == nil || variables.Joined() {
		t.Fatal("root Force reported success before callback return")
	}
	select {
	case <-call.returned:
		t.Fatal("held confirmation returned without release")
	default:
	}
	select {
	case <-result:
		t.Fatal("mutation owner returned without confirmation")
	default:
	}
	var force independentForceObservation
	select {
	case force = <-store.forced:
	default:
		t.Fatal("DB Force did not receive root context")
	}
	if !cancelObservedAt.Before(force.at) || !force.at.Before(deadline) {
		t.Fatal("invalid stimulus: actual Force before original deadline unproved")
	}
	actual := force.ctx
	owner.mu.Lock()
	expected := owner.forced
	owner.mu.Unlock()
	if actual == nil || actual != expected {
		t.Fatal("DB Force replaced root context")
	}
	got, gok := actual.Deadline()
	want, wok := expected.Deadline()
	if !gok || !wok || !got.Equal(want) {
		t.Fatal("DB Force renewed root deadline")
	}
	store.unhold()
	independentForceWait(t, call.returned, "confirmation callback actual return")
	var failure error
	select {
	case failure = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("Variable call did not return")
	}
	independentForceWait(t, callJoined, "mutation goroutine actual return")
	var fault *f.Fault
	if !errors.As(failure, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.RetryHint != "lookup" || fault.CauseID != original.AttemptID().String() {
		t.Fatal("Force replaced original Unknown")
	}
	joined, stop := context.WithTimeout(context.Background(), time.Second)
	e = variables.Drain(joined)
	stop()
	if e != nil || !variables.Joined() {
		t.Fatal("actual post-return Variable join missing")
	}
	if _, e = variables.service.GetVariable(context.Background(), actor, project.ID, variableID); e == nil {
		t.Fatal("stopped root admitted new Variable call")
	}
	proxy.Release()
	independentForceWait(t, proxy.Committed(), "original backend COMMIT+ReadyForQuery")
	independentForceWait(t, proxy.HeldJoined(), "original proxy return")
	var completed, history, audits, events, backends int
	if e = admin.QueryRow(databaseTestContext(t), `SELECT (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1 AND idempotency_key=$2 AND state='completed'),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='projectvariable'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable'),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, project.ID.String(), string(key)).Scan(&completed, &history, &audits, &events, &backends); e != nil || completed != 1 || history != 1 || audits != 1 || events != 1 || backends != 0 {
		t.Fatal("late original commit or root borrower facts differ")
	}
	if strings.Contains(output.String(), request.Fields().Value) || strings.Contains(output.String(), string(key)) {
		t.Fatal("root diagnostics exposed private command material")
	}
}
