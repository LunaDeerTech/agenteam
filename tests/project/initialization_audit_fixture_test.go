//go:build integration

package project_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This controlled delegate is deliberately not a Skill provider. It checks
// exact call-through and the original initialization request via real Project
// gates. Its success proves neither a real Skill mapping nor an Object witness.
type initializationAuditControlledFacts struct {
	pg      *initializationConvergencePG
	request c.InitializationRequest
	entry   audit.Entry
	key     audit.AppendKey
	ctx     context.Context
	tx      foundation.Tx
	calls   int
	err     error
	check   func(context.Context, foundation.Tx) error
}

func (p *initializationAuditControlledFacts) CheckProjectAuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	p.calls++
	want, got := p.entry.Fields(), entry.Fields()
	if ctx != p.ctx || tx != p.tx || !want.Scope.Equal(got.Scope) || !want.Actor.Equal(got.Actor) || want.Action != got.Action || want.Outcome != got.Outcome || want.Resource.Details() != got.Resource.Details() || want.Associations != got.Associations || !bytes.Equal(want.Metadata.JSON(), got.Metadata.JSON()) || key.Details() != p.key.Details() {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	registration, err := identity.RegisterService(identity.ProjectInitialization)
	if err != nil {
		return err
	}
	scope, err := identity.InProject(p.request.ProjectID)
	if err != nil {
		return err
	}
	actor, err := registration.Actor(p.request.CreationID.String(), scope)
	if err != nil {
		return err
	}
	if got.Action == audit.ObjectUploadComplete {
		err = p.pg.authority.ValidateInitializationInTx(ctx, tx, actor, p.request.CreationID, p.request.ProjectID, p.request.InitializationKey)
	} else {
		err = p.pg.authority.ValidateInitializationConvergenceInTx(ctx, tx, actor, p.request)
	}
	if err != nil {
		return err
	}
	if p.check != nil {
		return p.check(ctx, tx)
	}
	return p.err
}

type initializationAuditPGCase struct {
	v     initializationConvergenceCase
	entry audit.Entry
	key   audit.AppendKey
	facts *initializationAuditControlledFacts
	gate  audit.ProjectAuthority
}

func newInitializationAuditPGCase(t *testing.T, pg *initializationConvergencePG, state c.CreationState, action audit.Action) *initializationAuditPGCase {
	t.Helper()
	v := pg.seed(t, state)
	scope, _ := identity.InProject(v.request.ProjectID)
	object, cause := id[struct{}](t).String(), id[struct{}](t).String()
	ordinal := int64(0)
	metadata := audit.ObjectMetadataFields{ObjectID: object, InitiatorKind: identity.Service, InitiatorID: v.request.CreationID.String(), MediaType: "text/plain", ByteSize: 7, Phase: audit.PublishedPhase}
	outcome := audit.Success
	if action == audit.ObjectUploadFailed {
		metadata.Phase, metadata.Reason, outcome = audit.FailedPhase, audit.PayloadMissing, audit.Unknown
	}
	if action == audit.ObjectDelete {
		sum := sha256.Sum256([]byte("controlled original cleanup"))
		cause = "sha256:" + hex.EncodeToString(sum[:])
		ordinal = 1
		metadata.Phase = audit.DeletedPhase
	}
	registration, err := identity.RegisterService(identity.ObjectService)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(cause, scope)
	if err != nil {
		t.Fatal(err)
	}
	m, err := audit.ObjectMetadata(action, metadata)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := audit.NewResource(audit.ObjectResource, object)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := audit.NewEntry(audit.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: outcome, Resource: resource, Metadata: m})
	if err != nil {
		t.Fatal(err)
	}
	key, err := audit.NewAppendKey(audit.ObjectProducer, cause, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	provider := &initializationAuditControlledFacts{pg: pg, request: v.request, entry: entry, key: key}
	gate, err := project.NewInitializationAuditAuthority(pg.authority, provider)
	if err != nil {
		t.Fatal(err)
	}
	return &initializationAuditPGCase{v: v, entry: entry, key: key, facts: provider, gate: gate}
}

func (v *initializationAuditPGCase) append(ctx context.Context, tx foundation.Tx) error {
	v.facts.ctx, v.facts.tx = ctx, tx
	return v.gate.CheckAppendInTx(ctx, tx, v.entry, v.key)
}

func (v *initializationAuditPGCase) check(t *testing.T, pg *initializationConvergencePG, want foundation.Code, calls int) {
	t.Helper()
	before := pg.snapshot(t, v.v)
	v.facts.calls = 0
	var gateError error
	reached := false
	result := pg.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := pg.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.v.request.ProjectID, foundation.Exclusive)}); err != nil {
			return err
		}
		reached = true
		gateError = v.append(ctx, tx)
		return gateError
	})
	if !reached {
		t.Fatal("fixture did not reach wrapper", result.State(), result.Fault())
	}
	if want == "" {
		if gateError != nil {
			t.Fatal("valid Project facts rejected", gateError)
		}
		convergenceCommitted(t, result)
	} else {
		requireCode(t, gateError, want)
		if result.State() != foundation.NotCommitted {
			t.Fatal("rejected authorization committed", result.State())
		}
	}
	if v.facts.calls != calls {
		t.Fatalf("controlled delegate calls=%d want=%d", v.facts.calls, calls)
	}
	if before != pg.snapshot(t, v.v) {
		t.Fatal("wrapper changed persistent facts")
	}
}

func initializationAuditLockSurvives(t *testing.T, f *initializationConvergencePG, mode foundation.LockMode) {
	t.Helper()
	v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
	before := f.snapshot(t, v.v)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	ownerDone, waiterDone := make(chan struct{}), make(chan struct{})
	ownerResults, waiterResults := make(chan foundation.CommitResult, 1), make(chan foundation.CommitResult, 1)
	ownerPID, waiterPID := make(chan int, 1), make(chan int, 1)
	ownerCause, waiterCause := cause(t), cause(t)
	waiterStarted := false
	t.Cleanup(func() {
		cancel()
		letGo()
		<-ownerDone
		if waiterStarted {
			<-waiterDone
		}
	})
	go func() {
		defer close(ownerDone)
		ownerResults <- f.store.WithinTx(ctx, ownerCause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.v.request.ProjectID, foundation.Exclusive)}); err != nil {
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
			if err = v.append(ctx, tx); err != nil {
				return err
			}
			ownerPID <- pid // wrapper returned; caller still owns the original Tx/EX.
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var owner int
	select {
	case owner = <-ownerPID:
	case result := <-ownerResults:
		t.Fatal("owner exited before wrapper return", result.State(), result.Fault())
	case <-ctx.Done():
		t.Fatal("owner did not reach wrapper return")
	}
	waiterStarted = true
	go func() {
		defer close(waiterDone)
		waiterResults <- f.other.WithinTx(ctx, waiterCause, func(ctx context.Context, tx foundation.Tx) error {
			x, err := f.other.InTx(tx)
			if err != nil {
				return err
			}
			var pid int
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			waiterPID <- pid
			return f.other.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.v.request.ProjectID, mode)})
		})
	}()
	var waiter int
	select {
	case waiter = <-waiterPID:
	case result := <-waiterResults:
		t.Fatal("waiter exited before lock", result.State(), result.Fault())
	case <-ctx.Done():
		t.Fatal("waiter did not start")
	}
	if owner <= 0 || waiter <= 0 || owner == waiter {
		t.Fatal("not distinct actual backends")
	}
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var blocked bool
		err := f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.pid=$1 AND a.wait_event_type='Lock' AND l.locktype='advisory' AND NOT l.granted AND $2=ANY(pg_blocking_pids(a.pid)))`, waiter, owner).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case result := <-waiterResults:
			t.Fatal("waiter escaped EX after wrapper return", result.State(), result.Fault())
		case <-ctx.Done():
			t.Fatal("no physical wait on exact holder")
		case <-poll.C:
		}
	}
	letGo()
	for _, results := range []chan foundation.CommitResult{ownerResults, waiterResults} {
		select {
		case result := <-results:
			convergenceCommitted(t, result)
		case <-ctx.Done():
			t.Fatal("released holder did not finish")
		}
	}
	<-ownerDone
	<-waiterDone
	if v.facts.calls != 1 || before != f.snapshot(t, v.v) {
		t.Fatal("lock-survival call count or persistent facts changed")
	}
}
