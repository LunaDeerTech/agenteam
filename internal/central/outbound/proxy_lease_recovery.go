package outbound

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type proxyCredentialOperation struct {
	id, process, lease, credential string
	version                        foundation.Version
}

func proxyOperationLock(id string) foundation.LockKey {
	k, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "outbound-proxy-operation:"+id)
	return k
}

func readProxyOperation(ctx context.Context, x postgres.SQLExecutor, id string) (proxyCredentialOperation, bool, error) {
	var op proxyCredentialOperation
	var version int64
	err := x.QueryRow(ctx, `SELECT operation_id::text,process_id::text,lease_id::text,credential_id::text,proxy_version FROM agenteam_outbound.proxy_credential_operations WHERE operation_id=$1`, id).Scan(&op.id, &op.process, &op.lease, &op.credential, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, false, nil
	}
	op.version = foundation.Version(version)
	if err != nil {
		return op, false, unavailable(err)
	}
	if op.version.Validate() != nil {
		return op, false, unavailable(nil)
	}
	return op, true, nil
}

func (a *ProxySecretAuthority) validateOperation(ctx context.Context, tx foundation.Tx, w *proxyUse) error {
	x, err := a.store.InTx(tx)
	if err != nil {
		return err
	}
	op, exists, err := readProxyOperation(ctx, x, w.owner.Details().ID)
	if err != nil {
		return err
	}
	if !exists || op.process != w.process || op.credential != w.ref.Details().ID.String() || op.lease != w.lease.String() || op.version != w.version {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return nil
}

// releaseProxyOperation never invents a lease after an ambiguous acquire. The
// operation and lease are created atomically; absence means there is nothing
// to release. The caller proves actual local join or a stopped original guard.
func (s *PolicyService) releaseProxyOperation(ctx context.Context, expected proxyCredentialOperation) error {
	st := s.state()
	deps := st.proxyDependencies
	cause, err := foundation.NewRecoveryCause("outbound-proxy-credential", expected.id, "")
	if err != nil {
		return err
	}
	var op proxyCredentialOperation
	var exists bool
	// This lock also belongs to acquire. Even a lost COMMIT response must settle
	// before absence can be interpreted as an acquire that did not commit.
	observed := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: proxyOperationLock(expected.id), Mode: foundation.Shared}}); err != nil {
			return err
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return err
		}
		op, exists, err = readProxyOperation(ctx, x, expected.id)
		return err
	})
	err = resultError(observed)
	if err != nil || !exists {
		return err
	}
	if op != expected {
		return unavailable(nil)
	}
	cid, err := foundation.ParseID[sc.Credential](op.credential)
	if err != nil {
		return unavailable(err)
	}
	ref, err := sc.NewCredentialRef(cid, identity.SystemScope())
	if err != nil {
		return err
	}
	lease, err := foundation.ParseID[sc.Lease](op.lease)
	if err != nil {
		return unavailable(err)
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.OutboundProxyOwner, op.id)
	if err != nil {
		return err
	}
	registration, err := identity.RegisterService(identity.SecretService)
	if err != nil {
		return err
	}
	actor, err := registration.Actor(op.id, identity.SystemScope())
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, proxyUseKey{}, &proxyUse{authority: deps.Authority, version: op.version, actor: actor, owner: owner, ref: ref, process: op.process, lease: lease})
	req := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.System, LeaseOwner: owner, LeaseID: lease, Action: sc.ReleaseLeaseUsage}
	plan, err := deps.Secrets.DiscoverUsage(ctx, req)
	if err != nil {
		_, stillExists, readErr := readProxyOperation(ctx, st.store, op.id)
		if readErr == nil && !stillExists {
			return nil
		}
		return err
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return err
		}
		current, exists, err := readProxyOperation(ctx, x, op.id)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if current != op {
			return unavailable(nil)
		}
		if _, err := deps.Secrets.ApplyUsageInTx(ctx, tx, req, plan); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `DELETE FROM agenteam_outbound.proxy_credential_operations WHERE operation_id=$1 AND process_id=$2 AND lease_id=$3 AND credential_id=$4 AND proxy_version=$5`, op.id, op.process, op.lease, op.credential, int64(op.version))
		return err
	})
	return resultError(result)
}

// The exclusive send gate is required: each local credential callback holds
// its shared side until it actually returns, including independent cleanup.
// A foreign operation additionally requires its original ProcessGuard proof.
func (s *PolicyService) reconcileProxyLeasesLocked(ctx context.Context) error {
	st := s.state()
	deps := st.proxyDependencies
	if deps == nil {
		return nil
	}
	if nilPort(deps.Processes) {
		return unavailable(nil)
	}
	current, err := deps.Processes.CurrentProxyProcess()
	if err != nil {
		return err
	}
	rows, err := st.store.Query(ctx, `SELECT operation_id::text,process_id::text,lease_id::text,credential_id::text,proxy_version FROM agenteam_outbound.proxy_credential_operations ORDER BY operation_id LIMIT 1025`)
	if err != nil {
		return unavailable(err)
	}
	var operations []proxyCredentialOperation
	for rows.Next() {
		var op proxyCredentialOperation
		var version int64
		if err = rows.Scan(&op.id, &op.process, &op.lease, &op.credential, &version); err != nil {
			break
		}
		op.version = foundation.Version(version)
		operations = append(operations, op)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	if len(operations) > 1024 {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	for _, op := range operations {
		if op.process != current {
			if err = deps.Processes.ConfirmProxyProcessStopped(ctx, op.process); err != nil {
				var fault *foundation.Fault
				if errors.As(err, &fault) && fault.Code == foundation.ResourceBusy {
					continue
				}
				return err
			}
		}
		if err = s.releaseProxyOperation(ctx, op); err != nil {
			return err
		}
	}
	return nil
}

// RecoverProxyCredentials runs after the composition root binds its original
// Object ProcessGuard. It preserves still-active foreign process leases.
func (s *PolicyService) RecoverProxyCredentials(ctx context.Context) error {
	release, err := s.state().gate.acquire(ctx, true)
	if err != nil {
		return unavailable(err)
	}
	defer release()
	if err = s.reconcileProxyLeasesLocked(ctx); err != nil {
		s.unavailable()
		return err
	}
	return nil
}
