package outbound

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// recordPending runs under the exclusive send gate. Neither resource's
// ordinary publication may restore admission while any command is unresolved.
func (s *PolicyService) recordPending(identity foundation.CommandIdentity) {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.pending == nil {
		st.pending = make(map[string]foundation.CommandIdentity)
	}
	st.pending[digestBytes([]byte(identity.Canonical()))] = identity
	st.available = false
	st.proxyAvailable = false
}

func (s *PolicyService) recoveryRequired() bool {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	return !st.available || !st.proxyAvailable || len(st.pending) != 0
}

func (s *PolicyService) commandPending(identity foundation.CommandIdentity) bool {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	_, ok := st.pending[digestBytes([]byte(identity.Canonical()))]
	return ok
}

// committedRecoveryError preserves the already confirmed external fact. A
// failed mirror refresh cannot turn a durable command into NotStarted.
func committedRecoveryError(cause error) error {
	fault := foundation.NewFault(foundation.DependencyUnavailable, foundation.Committed).WithCause(cause)
	fault.RetryHint = "replay"
	return &Error{code: foundation.DependencyUnavailable, reason: ac.PolicyUnavailable, cause: func() error { return fault }}
}

// recoverCurrentLocked requires the exclusive send gate. It does not resolve
// credential leases; the lifecycle owner joins those before invoking recovery.
func (s *PolicyService) recoverCurrentLocked(ctx context.Context) error {
	st := s.state()
	st.mu.Lock()
	st.available = false
	st.proxyAvailable = false
	pending := make([]foundation.CommandIdentity, 0, len(st.pending))
	for _, identity := range st.pending {
		pending = append(pending, identity)
	}
	bound := st.proxyDependencies != nil
	st.mu.Unlock()
	slices.SortFunc(pending, func(a, b foundation.CommandIdentity) int {
		if a.Canonical() < b.Canonical() {
			return -1
		}
		if a.Canonical() > b.Canonical() {
			return 1
		}
		return 0
	})
	locks := []foundation.LockRequest{{Key: policyLock(), Mode: foundation.Shared}}
	if bound {
		locks = append(locks, foundation.LockRequest{Key: proxyLock(), Mode: foundation.Shared})
	}
	for _, identity := range pending {
		if identity.Validate() != nil || identity.Command() != "update" || identity.Namespace() != "outbound-policy" && identity.Namespace() != "outbound-proxy" || identity.Namespace() == "outbound-proxy" && !bound {
			return unavailable(nil)
		}
		key, err := foundation.CommandLock(identity)
		if err != nil {
			return unavailable(err)
		}
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	cause, err := recoveryCause()
	if err != nil {
		return err
	}
	var current Policy
	var proxy proxyRecord
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		executor, err := st.store.InTx(tx)
		if err != nil {
			return err
		}
		for _, identity := range pending {
			if err := observePendingCommand(ctx, executor, identity); err != nil {
				return err
			}
		}
		current, err = readPolicy(ctx, executor)
		if err != nil {
			return err
		}
		if bound {
			proxy, err = readProxy(ctx, executor)
		}
		return err
	})
	if result.State() != foundation.Committed {
		return resultError(result)
	}
	// Both rows came from the same locked observation. Keep admission closed
	// through validation/publication and clear pending only after both succeed.
	if err = s.publish(current); err != nil {
		s.unavailable()
		return err
	}
	if bound {
		if err = s.publishProxy(proxy); err != nil {
			s.unavailable()
			return err
		}
	}
	st.mu.Lock()
	clear(st.pending)
	st.available = true
	st.proxyAvailable = true
	st.mu.Unlock()
	return nil
}

func observePendingCommand(ctx context.Context, executor postgres.SQLExecutor, identity foundation.CommandIdentity) error {
	var query string
	switch identity.Namespace() {
	case "outbound-policy":
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_outbound.outbound_policy_receipts WHERE command_digest=$1)`
	case "outbound-proxy":
		query = `SELECT EXISTS(SELECT 1 FROM agenteam_outbound.proxy_receipts WHERE command_digest=$1)`
	default:
		return unavailable(nil)
	}
	// Existence proves a committed command. Absence while holding its command
	// lock proves the original writer has settled without that receipt. Neither
	// outcome is used as a configuration snapshot: both current rows are read.
	var exists bool
	if err := executor.QueryRow(ctx, query, digestBytes([]byte(identity.Canonical()))).Scan(&exists); err != nil {
		return unavailable(err)
	}
	return nil
}

func (s *PolicyService) recoverAfterPolicyCommandLocked(ctx context.Context) error {
	if s.state().proxyDependencies != nil {
		if err := s.reconcileProxyLeasesLocked(ctx); err != nil {
			return err
		}
	}
	return s.recoverCurrentLocked(ctx)
}
