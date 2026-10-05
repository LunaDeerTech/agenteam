package secret

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// A nil named channel really satisfies both interfaces. No method may run.
type nilModelUsage chan struct{}

func (nilModelUsage) CheckReferenceInTx(context.Context, foundation.Tx, identity.Actor, sc.CredentialRef, sc.Purpose, string, bool) error {
	panic("nil usage called")
}
func (nilModelUsage) AuthorizeLeaseInTx(context.Context, foundation.Tx, identity.Actor, sc.CredentialRef, sc.CredentialLeaseOwner, sc.LeaseAction) (sc.UseGrant, error) {
	panic("nil usage called")
}
func (nilModelUsage) DiscoverUsage(context.Context, sc.UsageRequest) (sc.UsageDependencies, error) {
	panic("nil planner called")
}
func (nilModelUsage) ValidateUsageInTx(context.Context, foundation.Tx, sc.UsageRequest, sc.UsageDependencies) error {
	panic("nil planner called")
}

type nilModelAudit chan struct{}

func (nilModelAudit) AppendInTx(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) (ac.AppendReceipt, error) {
	panic("nil audit called")
}

func unitModelRequest(t *testing.T) sc.UsageRequest {
	t.Helper()
	owner, _ := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, projectAuditID[struct{}](t).String())
	reg, _ := identity.RegisterService(identity.SecretService)
	actor, _ := reg.Actor(owner.Details().ID, identity.SystemScope())
	ref, _ := sc.NewCredentialRef(projectAuditID[sc.Credential](t), identity.SystemScope())
	return sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.Model, LeaseOwner: owner, LeaseID: projectAuditID[sc.Lease](t), Action: sc.ReadLeaseUsage, RequestID: projectAuditID[struct{}](t).String()}
}

func TestModelUsageNilAndInvalidNeverCallDependencies(t *testing.T) {
	r := unitModelRequest(t)
	for _, service := range []*Service{nil, {}, {data: func() *serviceState { return nil }}, {data: func() *serviceState {
		return &serviceState{store: &projectAuditUnitStore{}, audit: nilModelAudit(nil), auth: Authorizations{Usage: nilModelUsage(nil)}, initialized: true}
	}}, {data: func() *serviceState {
		return &serviceState{store: &projectAuditUnitStore{}, audit: make(nilModelAudit), auth: Authorizations{Usage: nilModelUsage(nil)}, initialized: true}
	}}, {data: func() *serviceState {
		return &serviceState{store: &projectAuditUnitStore{}, audit: make(nilModelAudit), auth: Authorizations{Usage: struct{ sc.UsageAuthority }{}}, initialized: true}
	}}} {
		material, err := service.ReadCredentialForUsage(context.Background(), r)
		projectAuditCode(t, err, foundation.DependencyUnbound)
		if material.Use(func([]byte) error { t.Fatal("material escaped"); return nil }) == nil {
			t.Fatal("invalid material usable")
		}
	}
	if !modelUsageNilPort(nilModelUsage(nil)) || !modelUsageNilPort(nilModelAudit(nil)) || modelUsageNilPort(make(nilModelUsage)) {
		t.Fatal("named channel dependency detection")
	}
	for _, action := range []sc.UsageAction{sc.AcquireLeaseUsage, sc.ReleaseLeaseUsage, sc.RetainReferenceUsage} {
		bad := r
		bad.Action = action
		_, err := (*Service)(nil).ReadCredentialForUsage(context.Background(), bad)
		projectAuditCode(t, err, foundation.InvalidArgument)
	}
}

func TestModelUsageUnknownPreservesReturnedIdentityAndSafeProjection(t *testing.T) {
	attempt := projectAuditID[foundation.TransactionAttempt](t)
	cause, _ := foundation.NewRecoveryCause("secret-resolution", projectAuditID[struct{}](t).String(), "private-checkpoint")
	result := foundation.UnknownResult(attempt, cause)
	err := failure(CommitUnknown, foundation.CommitUnknown, modelUsageUnknown{result.AttemptID(), result.Cause()})
	var original interface {
		error
		AttemptID() foundation.ID[foundation.TransactionAttempt]
		Cause() foundation.TransactionCause
	}
	if !errors.As(err, &original) || original.AttemptID() != attempt || original.Cause().Details().RecoveryRunID != cause.Details().RecoveryRunID || original.Cause().Details().CheckpointRef != "private-checkpoint" {
		t.Fatal("original returned Unknown identity lost")
	}
	projectAuditCode(t, err, foundation.CommitUnknown)
	var b bytes.Buffer
	for _, v := range []any{err, original, struct{ X any }{original}, struct{ private any }{original}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			fmt.Fprintf(&b, format, v)
		}
		data, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		b.Write(data)
		slog.New(slog.NewJSONHandler(&b, nil)).Info("result", "value", v)
	}
	for _, forbidden := range []string{attempt.String(), cause.Details().RecoveryRunID, "private-checkpoint"} {
		if strings.Contains(b.String(), forbidden) {
			t.Fatal("unsafe Unknown projection")
		}
	}
}

func TestModelUsageProofCopiesRequiredLocks(t *testing.T) {
	r := unitModelRequest(t)
	binding, _ := sc.UsageBinding(r)
	key, _ := foundation.UserLock(r.RequestID)
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}
	proof := modelUsageReadProof{request: r, binding: binding, mapping: binding, locks: locks}
	frozen := proof.copy()
	locks[0].Mode = foundation.Shared
	proof.request.RequestID = r.LeaseOwner.Details().ID
	if frozen.locks[0].Mode != foundation.Exclusive || frozen.request.RequestID != r.RequestID || frozen.binding != binding {
		t.Fatal("provider/caller mutation changed frozen proof")
	}
}

// The legacy public entry point must keep its pre-existing authority errors.
// A real-looking metadata row is deliberately available before authorization.
type modelLegacyPriorityStore struct {
	Store
	tx      foundation.Tx
	payload string
	queries int
}

func (s *modelLegacyPriorityStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign tx")
	}
	return s, nil
}
func (s *modelLegacyPriorityStore) AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error {
	return nil
}
func (s *modelLegacyPriorityStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	return projectAuditUnitRow{values: []any{"model", int64(1), s.payload}}
}

type modelLegacyGrant struct {
	sc.UsageAuthority
	grant sc.UseGrant
}

func (g modelLegacyGrant) AuthorizeLeaseInTx(context.Context, foundation.Tx, identity.Actor, sc.CredentialRef, sc.CredentialLeaseOwner, sc.LeaseAction) (sc.UseGrant, error) {
	return g.grant, nil
}
func TestModelLegacyAcquirePreservesEarlierErrors(t *testing.T) {
	for _, mode := range []string{"unbound", "wrong-cause", "human", "grant-purpose", "uninitialized", "unavailable", "authorized-model"} {
		t.Run(mode, func(t *testing.T) {
			r := unitModelRequest(t)
			store := &modelLegacyPriorityStore{tx: foundation.NewTx(), payload: projectAuditID[payloadMarker](t).String()}
			grant := sc.UseGrant{Subject: r.Actor, Consumer: sc.Model}
			var usage sc.UsageAuthority = modelLegacyGrant{grant: grant}
			want := foundation.Forbidden
			switch mode {
			case "unbound":
				usage = nil
				want = foundation.DependencyUnbound
			case "wrong-cause":
				reg, _ := identity.RegisterService(identity.SecretService)
				r.Actor, _ = reg.Actor(projectAuditID[struct{}](t).String(), r.Ref.Details().Scope)
			case "human":
				r.Actor, _ = identity.NewHuman(projectAuditID[identity.User](t), projectAuditID[identity.Session](t))
			case "grant-purpose":
				grant.Consumer = sc.MCP
				usage = modelLegacyGrant{grant: grant}
			case "authorized-model":
				want = foundation.ResourceBusy
			}
			state := &serviceState{store: store, auth: Authorizations{Usage: usage}, initialized: true}
			if mode == "uninitialized" || mode == "unavailable" {
				state.initialized = mode != "uninitialized"
				state.unavailable = mode == "unavailable"
				want = foundation.DependencyUnavailable
			}
			s := &Service{data: func() *serviceState { return state }}
			_, err := s.AcquireCredentialLeaseInTx(context.Background(), store.tx, r.Actor, r.Ref, r.LeaseOwner)
			projectAuditCode(t, err, want)
			if (mode == "unbound" || mode == "wrong-cause" || mode == "human") && store.queries != 0 {
				t.Fatal("metadata consulted before earlier authority rejection")
			}
			if (mode == "uninitialized" || mode == "unavailable") && store.queries != 1 {
				t.Fatal("availability checked without existing authorized Model reference or after lease write")
			}
		})
	}
}

func TestModelUsageCancellationRetainsCommittedFact(t *testing.T) {
	err := modelUsageCancelledAfterCommit(context.Canceled)
	var fault *foundation.Fault
	if !errors.Is(err, context.Canceled) || !errors.As(err, &fault) || fault.Code != foundation.DependencyUnavailable || fault.CommitState != foundation.Committed {
		t.Fatal("cancelled delivery changed committed database fact", err)
	}
}
