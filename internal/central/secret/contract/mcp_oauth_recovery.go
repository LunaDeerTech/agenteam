package contract

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ValidateMCPOAuthDeleteRecoveryRequest restricts recovery to deletion of an
// original flow credential. It grants neither observation nor mutation rights.
// The original request, command key, expected version and binding are retained.
func ValidateMCPOAuthDeleteRecoveryRequest(r MCPOAuthWriteRequest) error {
	if r.Validate() != nil {
		return bad()
	}
	v := r.Fields()
	if v.Kind != Delete || v.OwnerKind != MCPOAuthFlow {
		return bad()
	}
	return nil
}

// MCPOAuthDeleteRecoveryAuthority is an additional capability of the already
// bound MCP write authority. Its private current recovery witness must prove
// the actual stopped original writer, canonical cleanup claim, same Store/Tx
// and complete immutable request tuple. A Service actor or request DTO cannot
// satisfy it. This check must also work after the Project is closed, without
// authorizing a new credential, token read, exchange or refresh.
type MCPOAuthDeleteRecoveryAuthority interface {
	ValidateMCPOAuthDeleteRecoveryInTx(context.Context, f.Tx, MCPOAuthWriteRequest, WriteDependencies) error
}

// All three handles are issued by one actual MCPOAuthWriter instance. Caller
// implementations and handles from another instance must be rejected.
type MCPOAuthDeleteRecoveryPlan interface {
	RequiredLocks() []f.LockRequest
}
type MCPOAuthDeleteContinuation interface {
	RequiredLocks() []f.LockRequest
}
type PreparedMCPOAuthDeleteRecovery interface {
	RequiredLocks() []f.LockRequest
}

// Continuation is non-nil only when a read-only observation under the complete
// original locks proved no canonical receipt and the exact original current
// MCP credential/version. Absence alone, observation failure or a changed
// credential must never produce a continuation. No Secret value is read.
type MCPOAuthDeleteRecoveryObservation struct {
	Found        bool
	Result       MutationResult
	Continuation MCPOAuthDeleteContinuation
}

type MCPOAuthDeleteRecovery interface {
	DiscoverMCPOAuthDeleteRecovery(context.Context, MCPOAuthWriteRequest) (MCPOAuthDeleteRecoveryPlan, error)
	ObserveMCPOAuthDeleteRecoveryInTx(context.Context, f.Tx, MCPOAuthDeleteRecoveryPlan) (MCPOAuthDeleteRecoveryObservation, error)
	PrepareMCPOAuthDeleteRecovery(context.Context, MCPOAuthDeleteContinuation) (PreparedMCPOAuthDeleteRecovery, error)
	ApplyMCPOAuthDeleteRecoveryInTx(context.Context, f.Tx, PreparedMCPOAuthDeleteRecovery) (MCPOAuthWriteObservation, error)
}

// MCPSecretCleanupAuditAuthority identifies only the Secret engine's active
// private recovery-Delete postimage witness. False selects the ordinary Project
// gate; true still requires the complete CheckProjectAuditInTx proof. An error
// is terminal and must not fall back to a different authorization path.
type MCPSecretCleanupAuditAuthority interface {
	CheckMCPOAuthCleanupAuditInTx(context.Context, f.Tx, ac.Entry, ac.AppendKey) (bool, error)
}
