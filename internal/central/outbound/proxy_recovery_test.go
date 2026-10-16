package outbound

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These tests exercise the local recovery state machine. PostgreSQL tests
// separately establish that the command locks join a still-running COMMIT.
type proxyRecoveryStore struct {
	Store
	t             *testing.T
	active        foundation.Tx
	held          map[string]foundation.LockMode
	required      []foundation.LockKey
	observed      map[string]bool
	exists        map[string]bool
	rules         Rules
	policyVersion int64
	proxyVersion  int64
	failCurrent   bool
	unknown       bool
	digest        string
	receipt       UpdateResult
}

func (s *proxyRecoveryStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.active = foundation.NewTx()
	s.held = make(map[string]foundation.LockMode)
	defer func() { s.active = foundation.Tx{} }()
	if err := fn(ctx, s.active); err != nil {
		var fault *foundation.Fault
		if !errors.As(err, &fault) {
			fault = foundation.NewFault(foundation.InternalError, foundation.NotCommitted).WithCause(err)
		}
		return foundation.NotCommittedResult(fault)
	}
	if s.unknown {
		attempt, err := foundation.NewID[foundation.TransactionAttempt]()
		if err != nil {
			s.t.Fatal(err)
		}
		return foundation.UnknownResult(attempt, cause)
	}
	return foundation.CommittedResult()
}
func (s *proxyRecoveryStore) AcquireAll(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	if !tx.Valid() || tx != s.active || len(s.held) != 0 {
		s.t.Fatal("locks must be acquired together in the active transaction")
	}
	for _, lock := range locks {
		s.held[lock.Key.Canonical()] = lock.Mode
	}
	return nil
}
func (s *proxyRecoveryStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if !tx.Valid() || tx != s.active {
		s.t.Fatal("executor escaped its transaction")
	}
	return s, nil
}

type proxyRecoveryRow func(...any) error

func (r proxyRecoveryRow) Scan(out ...any) error { return r(out...) }
func (s *proxyRecoveryStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	return proxyRecoveryRow(func(out ...any) error {
		if !s.active.Valid() || len(s.held) == 0 {
			s.t.Fatal("read outside a locked transaction")
		}
		if strings.HasPrefix(query, "SELECT semantic_digest,") {
			*out[0].(*string) = s.digest
			*out[1].(*int64) = int64(s.receipt.Version)
			*out[2].(*int64) = int64(s.receipt.RuleCount)
			*out[3].(*string) = s.receipt.AuditID.String()
			*out[4].(*time.Time) = s.receipt.CreatedAt.Time()
			return nil
		}
		for _, key := range s.required {
			if s.held[key.Canonical()] != foundation.Shared {
				s.t.Fatal("current observation omitted a pending command or configuration lock")
			}
		}
		switch {
		case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_outbound."):
			key := args[0].(string)
			*out[0].(*bool) = s.exists[key]
			s.observed[key] = true
		case query == `SELECT version,rules FROM agenteam_outbound.outbound_policy WHERE singleton=true`:
			if s.failCurrent {
				return errors.New("controlled current snapshot failure")
			}
			*out[0].(*int64) = s.policyVersion
			*out[1].(*[]byte) = s.rules.JSON()
		case query == `SELECT version,mode,gateway,coalesce(credential_id::text,'') FROM agenteam_outbound.proxy_settings WHERE singleton=true`:
			*out[0].(*int64) = s.proxyVersion
			*out[1].(*string) = "direct"
			*out[2].(*[]byte) = []byte("null")
			*out[3].(*string) = ""
		default:
			s.t.Fatal("unexpected recovery query")
		}
		return nil
	})
}

func proxyRecoveryFixture(t *testing.T, bound bool) (*PolicyService, *proxyRecoveryStore) {
	t.Helper()
	rules, err := NewRules()
	if err != nil {
		t.Fatal(err)
	}
	store := &proxyRecoveryStore{t: t, rules: rules, policyVersion: 7, proxyVersion: 8, observed: make(map[string]bool), exists: make(map[string]bool)}
	state := &policyState{store: store, mirror: policy(1, rules), available: true, proxy: proxySnapshot{version: 1, mode: "direct"}, proxyAvailable: true}
	if bound {
		// Direct current-state recovery performs no credential operation.
		state.proxyDependencies = &ProxyDependencies{}
	}
	return &PolicyService{data: func() *policyState { return state }}, store
}
func proxyRecoveryCommand(t *testing.T, namespace, owner string) foundation.CommandIdentity {
	t.Helper()
	command, err := foundation.NewCommandIdentity(namespace, []string{owner}, "update", "recovery-original-key")
	if err != nil {
		t.Fatal(err)
	}
	return command
}
func proxyRecoveryOwner(t *testing.T) string {
	t.Helper()
	owner, err := foundation.NewID[identity.User]()
	if err != nil {
		t.Fatal(err)
	}
	return owner.String()
}
func proxyRecoveryPending(t *testing.T, service *PolicyService, store *proxyRecoveryStore, command foundation.CommandIdentity) {
	t.Helper()
	release, err := service.state().gate.acquire(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	service.recordPending(command)
	release()
	key, err := foundation.CommandLock(command)
	if err != nil {
		t.Fatal(err)
	}
	store.required = append(store.required, key)
}

func TestProxyRecoveryOrdinaryPublicationsCannotClearPending(t *testing.T) {
	service, store := proxyRecoveryFixture(t, true)
	command := proxyRecoveryCommand(t, "outbound-proxy", proxyRecoveryOwner(t))
	proxyRecoveryPending(t, service, store, command)
	release, err := service.state().gate.acquire(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err = service.publish(policy(2, store.rules)); err != nil {
		t.Fatal(err)
	}
	if err = service.publishProxy(proxyRecord{settings: ProxySettings{Version: 2, Mode: "direct"}}); err != nil {
		t.Fatal(err)
	}
	if service.Status().Available || !service.commandPending(command) {
		t.Fatal("ordinary saves retired unresolved command")
	}
	if _, err = service.snapshot(); err == nil {
		t.Fatal("pending command admitted a policy snapshot")
	}
	if _, err = service.proxySnapshot(); err == nil {
		t.Fatal("pending command admitted a proxy snapshot")
	}
}

func TestProxyRecoveryRequiresEveryCommandAndBothCurrentRows(t *testing.T) {
	service, store := proxyRecoveryFixture(t, true)
	owner := proxyRecoveryOwner(t)
	commands := []foundation.CommandIdentity{proxyRecoveryCommand(t, "outbound-policy", owner), proxyRecoveryCommand(t, "outbound-proxy", owner)}
	store.required = []foundation.LockKey{policyLock(), proxyLock()}
	for _, command := range commands {
		proxyRecoveryPending(t, service, store, command)
	}
	// One committed receipt and one absent receipt both require their command
	// lock; neither historical outcome is the current configuration.
	store.exists[digestBytes([]byte(commands[0].Canonical()))] = true
	store.failCurrent = true
	if err := service.Reload(context.Background()); err == nil {
		t.Fatal("failed current snapshot recovered admission")
	}
	if service.Status().Available || len(service.state().pending) != 2 {
		t.Fatal("failed reload lost pending commands")
	}
	store.failCurrent = false
	store.unknown = true
	if err := service.Reload(context.Background()); err == nil || service.Status().Available || len(service.state().pending) != 2 {
		t.Fatal("unconfirmed read transaction restored admission")
	}
	store.unknown = false
	if err := service.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !service.Status().Available || len(service.state().pending) != 0 || service.state().mirror.Version() != 7 || service.state().proxy.version != 8 {
		t.Fatal("confirmed current snapshots were not installed together")
	}
	for _, command := range commands {
		if !store.observed[digestBytes([]byte(command.Canonical()))] {
			t.Fatal("pending command was not observed under its lock")
		}
	}
}

type proxyRecoveryAuthority struct{}

func (proxyRecoveryAuthority) RequireCurrentSession(context.Context, foundation.Tx, identity.Actor) error {
	return nil
}
func (proxyRecoveryAuthority) AuthorizeSystem(_ context.Context, _ foundation.Tx, actor identity.Actor, intent identity.AccessIntent) (identity.AccessGrant, error) {
	at, err := foundation.NewInstant(time.Now())
	if err != nil {
		return identity.AccessGrant{}, err
	}
	return identity.NewAccessGrant(actor, identity.SystemScope(), intent, at, 1)
}

func TestProxyRecoveryPolicyReplayPreservesCommittedFactAndUsesCurrentRows(t *testing.T) {
	service, store := proxyRecoveryFixture(t, false)
	user, err := foundation.NewID[identity.User]()
	if err != nil {
		t.Fatal(err)
	}
	session, err := foundation.NewID[identity.Session]()
	if err != nil {
		t.Fatal(err)
	}
	actor, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	command := proxyRecoveryCommand(t, "outbound-policy", user.String())
	meta := CommandMeta{Identity: command, ExpectedVersion: 1}
	service.state().auth = Authorizations{Sessions: proxyRecoveryAuthority{}, System: proxyRecoveryAuthority{}}
	store.required = []foundation.LockKey{policyLock()}
	proxyRecoveryPending(t, service, store, command)
	auditID, err := foundation.NewID[ac.Record]()
	if err != nil {
		t.Fatal(err)
	}
	at, err := foundation.NewInstant(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store.receipt = UpdateResult{Version: 2, AuditID: auditID, CreatedAt: at}
	store.digest = commandDigest(actor, meta, store.rules)
	store.exists[digestBytes([]byte(command.Canonical()))] = true
	store.failCurrent = true
	_, err = service.UpdatePolicy(context.Background(), actor, meta, store.rules)
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.CommitState != foundation.Committed || fault.RetryHint != "replay" {
		t.Fatal("known receipt recovery failure lost committed/replay semantics")
	}
	if service.Status().Available || !service.commandPending(command) {
		t.Fatal("failed replay recovery reopened admission")
	}
	store.failCurrent = false
	out, err := service.UpdatePolicy(context.Background(), actor, meta, store.rules)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 2 || out.AuditID != auditID || !service.Status().Available || service.state().mirror.Version() != 7 {
		t.Fatal("historical receipt replaced current configuration")
	}
}
