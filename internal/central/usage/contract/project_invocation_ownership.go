package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// ProjectInvocationOwnership explicitly selects an owned borrower. A legacy
// Service returns nil even though its concrete type has the Acquire method.
// An error from a selected borrower never permits fallback to an unowned call.
type ProjectInvocationOwnership interface {
	ProjectInvocationBorrower() ProjectInvocationBorrower
}

// ProjectInvocationAdmissionAuthority is fixed to the original Project/Store.
// It checks only the current initialized/active Project under its shared lock
// before a fresh Runtime registration. It grants no permission to invoke a
// model, and cannot be used to reject an already retained call's legal tail.
type ProjectInvocationAdmissionAuthority interface {
	RequireProjectInvocationAdmissionInTx(context.Context, f.Tx, i.ProjectID) error
}

// ProjectInvocationBorrower is optional on the original Writer. Its concrete
// owner must validate the original Runtime's private admission through the
// fixed InvocationFacts before registering anything. A request is not authority.
// Once registered, including an uncertain registration, Acquire returns its
// nonnil original handle even with an error so the admission's actual return
// cannot be lost. Legacy Writers do not acquire this contract implicitly.
type ProjectInvocationBorrower interface {
	AcquireProjectInvocation(context.Context, any, mc.ModelRequest) (ProjectInvocationCall, error)
}

// ProjectInvocationCall is an opaque whole-call owner. WithinTx delegates to
// the exact caller Store and preserves its original result/cause/attempt.
// Returned consumes original return facts, never a caller-supplied boolean.
// The handle spans all material, wire, mutation, confirmation and outer tails;
// a transaction callback returning does not close this owner.
type ProjectInvocationCall interface {
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	Returned(context.Context) error
}

// ProjectInvocationFacts is implemented by the same original Runtime
// authority already supplying InvocationFacts. Acquire passes a nil handle;
// later checks require its exact original handle and private admission/call.
// These checks supply lifetime evidence only: the original InvocationFacts,
// immutable plan and same-Tx writer permissions remain mandatory.
type ProjectInvocationFacts interface {
	RequireOriginalInvocationCall(context.Context, any, mc.ModelRequest, ProjectInvocationCall) error
	RequireOriginalInvocationReturned(context.Context, any, mc.ModelRequest, ProjectInvocationCall) error
	// Cancel targets only this original call; it cannot Close, Invoke or retire it.
	CancelOriginalInvocation(context.Context, any, mc.ModelRequest, ProjectInvocationCall) error
}
