package secret

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func modelLeaseUsage(purpose sc.Purpose, owner sc.CredentialLeaseOwner) bool {
	return purpose == sc.Model && (owner.Details().Kind == sc.ExecutionOwner || owner.Details().Kind == sc.ModelCallOwner)
}

// Only the new entry point uses this stricter dependency check. In particular,
// a named nil channel may implement a port; calling it is not initialization.
func modelUsageNilPort(v any) bool {
	if v == nil {
		return true
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(v).IsNil()
	}
	return false
}

// Minted only after validateUsage in the current read transaction. It contains
// no provider plan, material, keyring or mutable provider-owned lock slice.
type modelUsageReadProof struct {
	request          sc.UsageRequest
	binding, mapping foundation.Digest
	locks            []foundation.LockRequest
}

func (p modelUsageReadProof) copy() *modelUsageReadProof {
	p.locks = append([]foundation.LockRequest(nil), p.locks...)
	return &p
}

func (s *Service) ReadCredentialForUsage(ctx context.Context, request sc.UsageRequest) (sc.SecretMaterial, error) {
	if request.Validate() != nil || request.Action != sc.ReadLeaseUsage || !modelLeaseUsage(request.Purpose, request.LeaseOwner) {
		return sc.SecretMaterial{}, invalid()
	}
	if s == nil || s.data == nil {
		return sc.SecretMaterial{}, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	state := s.state()
	if state == nil || modelUsageNilPort(state.store) || modelUsageNilPort(state.audit) || modelUsageNilPort(state.auth.Usage) {
		return sc.SecretMaterial{}, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	planner, ok := state.auth.Usage.(sc.UsagePlanner)
	if !ok || modelUsageNilPort(planner) {
		return sc.SecretMaterial{}, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	state.mu.Lock()
	initialized := state.initialized
	state.mu.Unlock()
	if !initialized {
		return sc.SecretMaterial{}, unavailable(nil)
	}
	plan, err := s.DiscoverUsage(ctx, request)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	resolution, err := foundation.NewID[struct{}]()
	if err != nil {
		return sc.SecretMaterial{}, unavailable(err)
	}
	cause, err := foundation.NewRecoveryCause("secret-resolution", resolution.String(), "")
	if err != nil {
		return sc.SecretMaterial{}, invalid()
	}
	var plaintext []byte
	defer func() { clear(plaintext) }()
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return unavailable(err)
		}
		if err := s.validateUsage(ctx, tx, request, plan); err != nil {
			return err
		}
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		lease, err := loadLease(ctx, e, request.LeaseID)
		if err != nil {
			return err
		}
		if !lease.ref.Equal(request.Ref) || !lease.owner.Equal(request.LeaseOwner) || lease.consumer != request.Purpose {
			return failure(Busy, foundation.ResourceBusy, nil)
		}
		if lease.released {
			return failure(AuthorizationRejected, foundation.Forbidden, nil)
		}
		grant, err := s.leaseGrant(ctx, tx, request.Actor, lease.ref, lease.owner, sc.ReadLease)
		if err != nil {
			return err
		}
		if grant.RequestID != "" || lease.ref.Details().Scope.Details().Kind == identity.System &&
			(grant.Subject.Details().Kind != identity.Service || grant.Subject.Details().ServiceName != identity.SecretService) {
			return failure(AuthorizationRejected, foundation.Forbidden, nil)
		}
		proof := modelUsageReadProof{request: request, binding: plan.Binding(), mapping: plan.Mapping(), locks: plan.RequiredLocks()}
		plaintext, err = s.resolveCredentialInTx(ctx, tx, e, resolution.String(), request.Actor, request.LeaseID, lease, grant, &proof)
		return err
	})
	if result.State() != foundation.Committed {
		if result.State() == foundation.NotCommitted && result.Fault() != nil {
			return sc.SecretMaterial{}, result.Fault()
		}
		return sc.SecretMaterial{}, failure(CommitUnknown, foundation.CommitUnknown, modelUsageUnknown{result.AttemptID(), result.Cause()})
	}
	if err = ctx.Err(); err != nil {
		return sc.SecretMaterial{}, modelUsageCancelledAfterCommit(err)
	}
	material, err := sc.NewSecretMaterial(plaintext)
	if err != nil {
		return sc.SecretMaterial{}, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		material.Destroy()
		return sc.SecretMaterial{}, modelUsageCancelledAfterCommit(err)
	}
	return material, nil
}

func modelUsageCancelledAfterCommit(err error) error {
	fault := foundation.NewFault(foundation.DependencyUnavailable, foundation.Committed).WithCause(err)
	return &Error{code: Unavailable, cause: func() error { return fault }}
}

// The legacy and explicit readers share only their already-authorized
// decryption/Audit tail. Their plan selection and transaction outcome policy
// remain in their respective public entry points.
func (s *Service) resolveCredentialInTx(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, resolution string, actor identity.Actor, id sc.LeaseID, lease leaseRecord, grant sc.UseGrant, proof *modelUsageReadProof) ([]byte, error) {
	state := s.state()
	metadata, payloadID, err := loadMetadata(ctx, e, lease.ref)
	if err != nil {
		return nil, err
	}
	if metadata.Purpose != grant.Consumer || grant.Consumer != lease.consumer {
		return nil, failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	p, err := loadPayload(ctx, e, payloadID)
	if err != nil {
		return nil, err
	}
	if p.ownerKind != valueOwner || p.ownerID != lease.ref.Details().ID.String() || !p.scope.Equal(lease.ref.Details().Scope) {
		return nil, failure(DecryptFailed, foundation.DependencyUnavailable, nil)
	}
	plaintext, err := openEnvelope(state.keys, p)
	if err != nil {
		return nil, err
	}
	deliver := false
	defer func() {
		if !deliver {
			clear(plaintext)
		}
	}()
	subject := grant.Subject
	if subject.Details().Kind == identity.Service {
		registration, _ := identity.RegisterService(identity.SecretService)
		subject, err = registration.Actor(resolution, p.scope)
		if err != nil {
			return nil, unavailable(err)
		}
	}
	requestID := grant.RequestID
	if proof != nil {
		requestID = proof.request.RequestID
	}
	resource, _ := ac.NewResource(ac.SecretResource, p.ownerID)
	auditMetadata, err := ac.SecretResolveMetadata(id.String(), ac.Consumer(grant.Consumer), "")
	if err != nil {
		return nil, unavailable(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: p.scope, Actor: subject, Action: ac.SecretResolve, Outcome: ac.Success, Resource: resource, Metadata: auditMetadata, Associations: ac.Associations{RequestID: requestID, OperationID: grant.OperationID, ToolID: grant.ToolID, RunnerID: grant.RunnerID}})
	if err != nil {
		return nil, unavailable(err)
	}
	key, err := ac.NewAppendKey(ac.SecretProducer, resolution, 0)
	if err != nil {
		return nil, unavailable(err)
	}
	auditCtx := resolutionAuditContext(ctx, state.store, tx, resolution, actor, id, lease, metadata, payloadID, grant, entry, key, proof)
	if _, err = state.audit.AppendInTx(auditCtx, tx, entry, key); err != nil {
		return nil, unavailable(err)
	}
	deliver = true
	return plaintext, nil
}

// The original returned UnknownResult is retained privately. It is not a
// receipt or evidence that the database committed, rolled back or joined.
type modelUsageUnknown struct {
	attempt foundation.ID[foundation.TransactionAttempt]
	cause   foundation.TransactionCause
}

func (e modelUsageUnknown) Error() string { return string(CommitUnknown) }
func (e modelUsageUnknown) AttemptID() foundation.ID[foundation.TransactionAttempt] {
	return e.attempt
}
func (e modelUsageUnknown) Cause() foundation.TransactionCause { return e.cause }
func (modelUsageUnknown) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, string(CommitUnknown))
}
func (modelUsageUnknown) MarshalJSON() ([]byte, error) {
	return []byte(`{"code":"SECRET_COMMIT_UNKNOWN"}`), nil
}
func (modelUsageUnknown) LogValue() slog.Value { return slog.StringValue(string(CommitUnknown)) }

var _ sc.CredentialUsageReader = (*Service)(nil)
