package secret

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func accountOwner(owner sc.CredentialLeaseOwner) bool {
	return owner.Details().Kind == sc.AccountDeliveryOwner || owner.Details().Kind == sc.AccountResponseOwner
}
func (s *Service) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	if r.Validate() != nil {
		return sc.UsageDependencies{}, invalid()
	}
	port, ok := s.state().auth.Usage.(sc.UsagePlanner)
	if !ok || nilPort(port) {
		return sc.UsageDependencies{}, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	if r.Action == sc.ReleaseLeaseUsage || r.Action == sc.ReadLeaseUsage {
		lease, e := loadLease(ctx, s.state().store, r.LeaseID)
		if e != nil {
			return sc.UsageDependencies{}, e
		}
		if !lease.ref.Equal(r.Ref) || !lease.owner.Equal(r.LeaseOwner) || lease.consumer != r.Purpose {
			return sc.UsageDependencies{}, failure(AuthorizationRejected, foundation.Forbidden, nil)
		}
		if r.Action == sc.ReadLeaseUsage && lease.released {
			return sc.UsageDependencies{}, failure(AuthorizationRejected, foundation.Forbidden, nil)
		}
	}
	plan, e := port.DiscoverUsage(ctx, r)
	if e != nil {
		return sc.UsageDependencies{}, plannedAuthorization(e)
	}
	binding, e := sc.UsageBinding(r)
	if e != nil || plan.Validate() != nil || plan.Binding() != binding {
		return sc.UsageDependencies{}, invalid()
	}
	locks := plan.RequiredLocks()
	mode := foundation.Exclusive
	if r.Action == sc.ReadLeaseUsage {
		mode = foundation.Shared
	}
	key, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, r.Ref.Details().ID.String())
	locks = append(locks, foundation.LockRequest{Key: key, Mode: mode})
	if r.Ref.Details().Scope.Details().Kind == identity.ProjectScope {
		key, _ := foundation.ProjectLock(r.Ref.Details().Scope.Details().ProjectID)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	record := "secret-ref:" + r.Ref.Details().ID.String() + ":" + string(r.Purpose) + ":" + r.ReferenceOwner
	if r.LeaseID.Validate() == nil {
		record = "secret-lease:" + r.LeaseID.String()
	}
	rk, e := foundation.RecordLock(foundation.ReferenceRecordLock, record)
	if e != nil {
		return sc.UsageDependencies{}, invalid()
	}
	locks = append(locks, foundation.LockRequest{Key: rk, Mode: mode})
	return sc.WrapUsageDependencies(s.state().usageIssuer, plan, locks)
}
func (s *Service) validateUsage(ctx context.Context, tx foundation.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	binding, e := sc.UsageBinding(r)
	if e != nil || d.Validate() != nil || !d.Matches(s.state().usageIssuer, binding, d.Mapping()) || d.ProviderPlan().Validate() != nil {
		return invalid()
	}
	if _, e = s.state().store.InTx(tx); e != nil {
		return unavailable(e)
	}
	if e = s.state().store.RequireHeldLocks(ctx, tx, d.RequiredLocks()); e != nil {
		return unavailable(e)
	}
	port, ok := s.state().auth.Usage.(sc.UsagePlanner)
	if !ok || nilPort(port) {
		return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	if e = port.ValidateUsageInTx(ctx, tx, r, d.ProviderPlan()); e != nil {
		return plannedAuthorization(e)
	}
	return nil
}
func (s *Service) ApplyUsageInTx(ctx context.Context, tx foundation.Tx, r sc.UsageRequest, d sc.UsageDependencies) (sc.UsageResult, error) {
	empty := sc.UsageResult{}
	if r.Action == sc.ReadLeaseUsage {
		return empty, invalid()
	}
	if e := s.validateUsage(ctx, tx, r, d); e != nil {
		return empty, e
	}
	x, e := s.state().store.InTx(tx)
	if e != nil {
		return empty, unavailable(e)
	}
	metadata, _, e := loadMetadata(ctx, x, r.Ref)
	if e != nil {
		return empty, e
	}
	if metadata.Purpose != r.Purpose {
		return empty, failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	ref := r.Ref.Details()
	switch r.Action {
	case sc.RetainReferenceUsage, sc.ReleaseReferenceUsage:
		if r.Retain {
			if e = s.writable(); e != nil {
				return empty, e
			}
			_, e = x.Exec(ctx, `INSERT INTO agenteam_secret.secret_references(credential_id,scope,project_id,consumer,owner_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, ref.ID.String(), string(ref.Scope.Details().Kind), null(ref.Scope.Details().ProjectID), string(r.Purpose), r.ReferenceOwner)
		} else {
			_, e = x.Exec(ctx, `DELETE FROM agenteam_secret.secret_references WHERE credential_id=$1 AND consumer=$2 AND owner_id=$3`, ref.ID.String(), string(r.Purpose), r.ReferenceOwner)
		}
		if e != nil {
			return empty, unavailable(e)
		}
	case sc.AcquireLeaseUsage:
		g, e := s.leaseGrant(ctx, tx, r.Actor, r.Ref, r.LeaseOwner, sc.AcquireLease)
		if e != nil {
			return empty, e
		}
		if g.Consumer != r.Purpose {
			return empty, failure(AuthorizationRejected, foundation.Forbidden, nil)
		}
		if e = s.writable(); e != nil {
			return empty, e
		}
		owner := r.LeaseOwner.Details()
		var existing string
		var released bool
		e = x.QueryRow(ctx, `SELECT id::text,released FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND owner_kind=$2 AND owner_id=$3`, ref.ID.String(), string(owner.Kind), owner.ID).Scan(&existing, &released)
		if e == nil {
			if existing != r.LeaseID.String() {
				return empty, failure(Busy, foundation.ResourceBusy, nil)
			}
			if released {
				return empty, failure(AuthorizationRejected, foundation.InvalidState, nil)
			}
			old, e := loadLease(ctx, x, r.LeaseID)
			if e != nil {
				return empty, e
			}
			if old.consumer != r.Purpose || !old.ref.Equal(r.Ref) || !old.owner.Equal(r.LeaseOwner) {
				return empty, failure(Busy, foundation.ResourceBusy, nil)
			}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return empty, unavailable(e)
		} else {
			_, e = x.Exec(ctx, `INSERT INTO agenteam_secret.secret_leases(id,credential_id,scope,project_id,owner_kind,owner_id,consumer) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.LeaseID.String(), ref.ID.String(), string(ref.Scope.Details().Kind), null(ref.Scope.Details().ProjectID), string(owner.Kind), owner.ID, string(r.Purpose))
			if e != nil {
				return empty, unavailable(e)
			}
		}
		return sc.NewUsageResult(r.Action, sc.CredentialLease{LeaseID: r.LeaseID, CredentialRef: r.Ref})
	case sc.ReleaseLeaseUsage:
		old, e := loadLease(ctx, x, r.LeaseID)
		if e != nil {
			return empty, e
		}
		if old.consumer != r.Purpose || !old.ref.Equal(r.Ref) || !old.owner.Equal(r.LeaseOwner) {
			return empty, failure(Busy, foundation.ResourceBusy, nil)
		}
		g, e := s.leaseGrant(ctx, tx, r.Actor, r.Ref, r.LeaseOwner, sc.ReleaseLease)
		if e != nil {
			return empty, e
		}
		if g.Consumer != r.Purpose {
			return empty, failure(AuthorizationRejected, foundation.Forbidden, nil)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_secret.secret_leases SET released=true WHERE id=$1`, r.LeaseID.String()); e != nil {
			return empty, unavailable(e)
		}
	default:
		return empty, invalid()
	}
	return sc.NewUsageResult(r.Action, sc.CredentialLease{})
}

var _ sc.UsageOperations = (*Service)(nil)
