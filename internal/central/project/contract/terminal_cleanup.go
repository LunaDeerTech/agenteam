package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ProjectTerminalOwnerKind string

const (
	ProjectTerminalAgentOwner     ProjectTerminalOwnerKind = "agent"
	ProjectTerminalVariablesOwner ProjectTerminalOwnerKind = "variables"
)

// Identity describes the original concrete pair. It is not a returned-call
// proof. A different instance in the same live process cannot claim its tail;
// a foreign process requires the original ProcessGuard's stopped proof.
type ProjectTerminalOwnerIdentity struct {
	Kind       ProjectTerminalOwnerKind `json:"kind"`
	ProcessID  string                   `json:"process_id"`
	InstanceID string                   `json:"instance_id"`
}

func (v ProjectTerminalOwnerIdentity) Validate() error {
	if v.Kind != ProjectTerminalAgentOwner && v.Kind != ProjectTerminalVariablesOwner {
		return invalid("", "INVALID_TERMINAL_OWNER")
	}
	if _, err := f.ParseID[struct{}](v.ProcessID); err != nil {
		return invalid("", "INVALID_TERMINAL_OWNER")
	}
	if _, err := f.ParseID[struct{}](v.InstanceID); err != nil {
		return invalid("", "INVALID_TERMINAL_OWNER")
	}
	return nil
}

// On application Owner is the concrete pair's identity. On confirmation it
// must be the exact identity stored in the atomic terminal receipt, even when
// a new process performs recovery. Public fields never issue cleanup authority.
type ProjectTerminalCleanupRequest struct {
	Actor        i.Actor
	Cause        LifecycleCause
	ProjectID    i.ProjectID
	Owner        ProjectTerminalOwnerIdentity
	Confirmation bool
	NotApplied   bool
}

func (r ProjectTerminalCleanupRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Cause.Validate() != nil || r.ProjectID.Validate() != nil || r.Owner.Validate() != nil || r.NotApplied && !r.Confirmation {
		return invalid("", "INVALID_TERMINAL_CLEANUP")
	}
	d := r.Actor.Details()
	if r.Cause.Action != Delete || d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.Cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	return nil
}

func (r ProjectTerminalCleanupRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	key, _ := f.ProjectLock(r.ProjectID.String())
	return []f.LockRequest{{Key: key, Mode: f.Exclusive}}
}

// The callback includes the original caller-owned transaction's actual Store
// return. Its CommitResult must not be reconstructed from row visibility or an
// error code. Each pair additionally requires its own synchronous InTx method
// to have validated the active private scope before accepting this result.
type ProjectTerminalCleanupCall func(context.Context) (f.CommitResult, error)

type ProjectTerminalCleanupDomain interface {
	ProjectTerminalCleanupIdentity() ProjectTerminalOwnerIdentity
	WithProjectTerminalCleanup(context.Context, ProjectTerminalCleanupRequest, ProjectTerminalCleanupCall) (f.CommitResult, error)
	RetireProjectTerminalInTx(context.Context, f.Tx, ProjectTerminalCleanupRequest) error
	RequireProjectTerminalRetiredInTx(context.Context, f.Tx, ProjectTerminalCleanupRequest) error
	// An Unknown whose exact original claim and unchanged schema 5 remain is
	// confirmed without deleting anything. This is distinct from a schema 6
	// receipt confirming retirement. A later explicit Run may apply again.
	RequireProjectTerminalUnappliedInTx(context.Context, f.Tx, ProjectTerminalCleanupRequest) error
}

// Request.Confirmation selects the receipt-confirmation gate, never permission to
// repeat deletion. Both modes require the original active driver scope,
// current Delete operation, same Store/live Tx and already-held Project EX.
type ProjectTerminalCleanupAuthority interface {
	RequireProjectTerminalCleanupInTx(context.Context, f.Tx, ProjectTerminalCleanupRequest) error
}
