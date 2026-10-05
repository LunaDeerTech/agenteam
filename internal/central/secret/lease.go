package secret

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

var _ sc.CredentialLeases = (*Service)(nil)
var _ sc.References = (*Service)(nil)

func validService(actor identity.Actor, scope identity.Scope) bool {
	d := actor.Details()
	return actor.Validate() == nil && d.Kind == identity.Service && d.ServiceName == identity.SecretService && validScope(scope) && d.ProjectID == scope.Details().ProjectID
}
func (s *Service) referenceLocks(ctx context.Context, tx foundation.Tx, ref sc.CredentialRef, mode foundation.LockMode) error {
	if ref.Validate() != nil {
		return invalid()
	}
	locks := []foundation.LockRequest{}
	if ref.Details().Scope.Details().Kind == identity.ProjectScope {
		key, _ := foundation.ProjectLock(ref.Details().Scope.Details().ProjectID)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	key, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, ref.Details().ID.String())
	locks = append(locks, foundation.LockRequest{Key: key, Mode: mode})
	if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *Service) RetainReferenceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, consumer sc.Purpose, owner string) error {
	return s.reference(ctx, tx, actor, ref, consumer, owner, true)
}
func (s *Service) ReleaseReferenceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, consumer sc.Purpose, owner string) error {
	return s.reference(ctx, tx, actor, ref, consumer, owner, false)
}
func (s *Service) reference(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, consumer sc.Purpose, owner string, retain bool) error {
	if ref.Validate() != nil || !consumer.Valid() || actor.Validate() != nil {
		return invalid()
	}
	if _, err := foundation.ParseID[struct{}](owner); err != nil {
		return invalid()
	}
	if actor.Details().Kind != identity.Human && !validService(actor, ref.Details().Scope) {
		return failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	state := s.state()
	if nilPort(state.auth.Usage) {
		return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	e, err := state.store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	if err = s.referenceLocks(ctx, tx, ref, foundation.Exclusive); err != nil {
		return err
	}
	if err = state.auth.Usage.CheckReferenceInTx(ctx, tx, actor, ref, consumer, owner, retain); err != nil {
		if _, planned := state.auth.Usage.(sc.UsagePlanner); planned {
			var f *foundation.Fault
			if errors.As(err, &f) && f != nil && f.Code == foundation.ResourceBusy {
				return failure(PreparationRequired, foundation.ResourceBusy, err)
			}
		}
		return authorization(err)
	}
	metadata, _, err := loadMetadata(ctx, e, ref)
	if err != nil {
		return err
	}
	if metadata.Purpose != consumer {
		return failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	d := ref.Details()
	if retain {
		if err = s.writable(); err != nil {
			return err
		}
		_, err = e.Exec(ctx, `INSERT INTO agenteam_secret.secret_references(credential_id,scope,project_id,consumer,owner_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, d.ID.String(), string(d.Scope.Details().Kind), null(d.Scope.Details().ProjectID), string(consumer), owner)
	} else {
		_, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secret_references WHERE credential_id=$1 AND consumer=$2 AND owner_id=$3`, d.ID.String(), string(consumer), owner)
	}
	if err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *Service) leaseGrant(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if ref.Validate() != nil || owner.Validate() != nil {
		return sc.UseGrant{}, invalid()
	}
	if !validService(actor, ref.Details().Scope) || actor.Details().CauseRef != owner.Details().ID {
		return sc.UseGrant{}, failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	port := s.state().auth.Usage
	if nilPort(port) {
		return sc.UseGrant{}, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	g, err := port.AuthorizeLeaseInTx(ctx, tx, actor, ref, owner, action)
	if err != nil {
		return sc.UseGrant{}, authorization(err)
	}
	if g.Validate(ref.Details().Scope) != nil {
		return sc.UseGrant{}, unavailable(nil)
	}
	return g, nil
}
func (s *Service) AcquireCredentialLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner) (sc.CredentialLease, error) {
	empty := sc.CredentialLease{}
	if ref.Validate() != nil || owner.Validate() != nil {
		return empty, invalid()
	}
	if accountOwner(owner) {
		return empty, failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	state := s.state()
	e, err := state.store.InTx(tx)
	if err != nil {
		return empty, unavailable(err)
	}
	if err = s.referenceLocks(ctx, tx, ref, foundation.Exclusive); err != nil {
		return empty, err
	}
	g, err := s.leaseGrant(ctx, tx, actor, ref, owner, sc.AcquireLease)
	if err != nil {
		return empty, err
	}
	metadata, _, err := loadMetadata(ctx, e, ref)
	if err != nil {
		return empty, err
	}
	if metadata.Purpose != g.Consumer {
		return empty, failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	if err = s.writable(); err != nil {
		return empty, err
	}
	// Keep the original dependency, authority and availability errors first.
	// Model owners must still use the planned mutation before any lease write.
	if modelLeaseUsage(metadata.Purpose, owner) {
		return empty, failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	id, err := foundation.NewID[sc.Lease]()
	if err != nil {
		return empty, unavailable(err)
	}
	var raw string
	d, o := ref.Details(), owner.Details()
	err = e.QueryRow(ctx, `INSERT INTO agenteam_secret.secret_leases(id,credential_id,scope,project_id,owner_kind,owner_id,consumer) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(credential_id,owner_kind,owner_id) DO UPDATE SET released=false WHERE secret_leases.consumer=excluded.consumer RETURNING id::text`, id.String(), d.ID.String(), string(d.Scope.Details().Kind), null(d.Scope.Details().ProjectID), string(o.Kind), o.ID, string(g.Consumer)).Scan(&raw)
	if err != nil {
		return empty, unavailable(err)
	}
	id, err = foundation.ParseID[sc.Lease](raw)
	if err != nil {
		return empty, unavailable(err)
	}
	return sc.CredentialLease{LeaseID: id, CredentialRef: ref}, nil
}

type leaseRecord struct {
	ref      sc.CredentialRef
	owner    sc.CredentialLeaseOwner
	consumer sc.Purpose
	released bool
}

func loadLease(ctx context.Context, e postgres.SQLExecutor, id sc.LeaseID) (leaseRecord, error) {
	if id.Validate() != nil {
		return leaseRecord{}, invalid()
	}
	var raw, scope, project, kind, owner, consumer string
	var released bool
	err := e.QueryRow(ctx, `SELECT credential_id::text,scope,coalesce(project_id::text,''),owner_kind,owner_id::text,consumer,released FROM agenteam_secret.secret_leases WHERE id=$1`, id.String()).Scan(&raw, &scope, &project, &kind, &owner, &consumer, &released)
	if errors.Is(err, pgx.ErrNoRows) {
		return leaseRecord{}, failure(NotFound, foundation.NotFound, nil)
	}
	if err != nil {
		return leaseRecord{}, unavailable(err)
	}
	s, err := decodeScope(scope, project)
	if err != nil {
		return leaseRecord{}, err
	}
	refID, err := foundation.ParseID[sc.Credential](raw)
	if err != nil {
		return leaseRecord{}, unavailable(err)
	}
	ref, err := sc.NewCredentialRef(refID, s)
	if err != nil {
		return leaseRecord{}, unavailable(err)
	}
	o, err := sc.NewCredentialLeaseOwner(sc.LeaseOwnerKind(kind), owner)
	if err != nil || !sc.Purpose(consumer).Valid() {
		return leaseRecord{}, unavailable(err)
	}
	return leaseRecord{ref, o, sc.Purpose(consumer), released}, nil
}
func (s *Service) ReleaseCredentialLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, id sc.LeaseID) error {
	state := s.state()
	if nilPort(state.auth.Usage) {
		return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	e, err := state.store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	lease, err := loadLease(ctx, e, id)
	if err != nil {
		return err
	}
	if accountOwner(lease.owner) || modelLeaseUsage(lease.consumer, lease.owner) {
		return failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	if err = s.referenceLocks(ctx, tx, lease.ref, foundation.Exclusive); err != nil {
		return err
	}
	// The immutable owner/scope is re-read after acquiring the deletion locks.
	lease, err = loadLease(ctx, e, id)
	if err != nil {
		return err
	}
	if modelLeaseUsage(lease.consumer, lease.owner) {
		return failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	g, err := s.leaseGrant(ctx, tx, actor, lease.ref, lease.owner, sc.ReleaseLease)
	if err != nil {
		return err
	}
	if g.Consumer != lease.consumer {
		return failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	if _, err = e.Exec(ctx, `UPDATE agenteam_secret.secret_leases SET released=true WHERE id=$1`, id.String()); err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *Service) ReadCredentialForRequest(ctx context.Context, actor identity.Actor, id sc.LeaseID) (sc.SecretMaterial, error) {
	state := s.state()
	if nilPort(state.auth.Usage) {
		return sc.SecretMaterial{}, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	state.mu.Lock()
	initialized := state.initialized
	state.mu.Unlock()
	if !initialized {
		return sc.SecretMaterial{}, unavailable(nil)
	}
	// A failed rotation does not waive any read checks. Only this authenticated,
	// currently bound value can be returned after both AEAD and Audit succeed.
	resolution, err := foundation.NewID[struct{}]()
	if err != nil {
		return sc.SecretMaterial{}, unavailable(err)
	}
	cause, err := foundation.NewRecoveryCause("secret-resolution", resolution.String(), "")
	if err != nil {
		return sc.SecretMaterial{}, invalid()
	}
	before, err := loadLease(ctx, state.store, id)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	if modelLeaseUsage(before.consumer, before.owner) {
		return sc.SecretMaterial{}, failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	var request sc.UsageRequest
	var plan sc.UsageDependencies
	planned := accountOwner(before.owner)
	if planned {
		request = sc.UsageRequest{Actor: actor, Ref: before.ref, Purpose: before.consumer, LeaseOwner: before.owner, LeaseID: id, Action: sc.ReadLeaseUsage}
		plan, err = s.DiscoverUsage(ctx, request)
		if err != nil {
			return sc.SecretMaterial{}, err
		}
	}
	var plaintext []byte
	defer func() { clear(plaintext) }()
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		lease, err := loadLease(ctx, e, id)
		if err != nil {
			return err
		}
		if planned {
			if err = state.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
				return unavailable(err)
			}
			if err = s.validateUsage(ctx, tx, request, plan); err != nil {
				return err
			}
		} else if err = s.referenceLocks(ctx, tx, lease.ref, foundation.Shared); err != nil {
			return err
		}
		lease, err = loadLease(ctx, e, id)
		if err != nil {
			return err
		}
		if planned && (!lease.ref.Equal(request.Ref) || !lease.owner.Equal(request.LeaseOwner) || lease.consumer != request.Purpose) {
			return failure(Busy, foundation.ResourceBusy, nil)
		}
		if modelLeaseUsage(lease.consumer, lease.owner) {
			return failure(PreparationRequired, foundation.ResourceBusy, nil)
		}
		if lease.released {
			return failure(AuthorizationRejected, foundation.Forbidden, nil)
		}
		g, err := s.leaseGrant(ctx, tx, actor, lease.ref, lease.owner, sc.ReadLease)
		if err != nil {
			return err
		}
		plaintext, err = s.resolveCredentialInTx(ctx, tx, e, resolution.String(), actor, id, lease, g, nil)
		return err
	})
	if err = commitError(result); err != nil {
		return sc.SecretMaterial{}, err
	}
	material, err := sc.NewSecretMaterial(plaintext)
	if err != nil {
		return sc.SecretMaterial{}, unavailable(err)
	}
	return material, nil
}
