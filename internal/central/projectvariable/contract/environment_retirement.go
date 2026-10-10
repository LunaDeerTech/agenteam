package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// EnvironmentRetirementBinding identifies the exact original capture, including
// ordinary values. It exposes only a digest; it is not a retirement grant.
func EnvironmentRetirementBinding(v EnvironmentCapture) (f.Digest, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Format string
		Value  EnvironmentCaptureFields
	}{"execution_environment_retirement_v1", v.Fields()})
	if err != nil {
		return "", invalid("", "INVALID_ENVIRONMENT_CAPTURE")
	}
	h := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(h[:])), nil
}

// Execution validates its private live owner and exact immutable input. Planning
// requires the original calls to have actually joined and freezes only locks.
// Final additionally proves the original terminal candidate in this same live
// transaction, current Project gate and full held union. A DTO, cancellation,
// elapsed timeout or a returned Chat error cannot substitute for this proof.
type ExecutionEnvironmentRetirementAuthority interface {
	CheckEnvironmentRetirementPlan(context.Context, EnvironmentCapture) error
	CheckEnvironmentRetirementInTx(context.Context, f.Tx, EnvironmentCapture) error
}

type EnvironmentRetirementPlan interface{ RequiredLocks() []f.LockRequest }

// Retirement ends only Execution's permission to use the captured environment
// leases. It retains immutable historical mappings and their original FKs;
// historical references can still block physical Secret/Variable deletion.
// Unknown is observed through EnvironmentRetiredInTx, never by replaying writes.
type EnvironmentRetirement interface {
	DiscoverEnvironmentRetirement(context.Context, EnvironmentCapture) (EnvironmentRetirementPlan, error)
	RetireEnvironmentInTx(context.Context, f.Tx, EnvironmentCapture, EnvironmentRetirementPlan) error
	EnvironmentRetiredInTx(context.Context, f.Tx, EnvironmentCapture, EnvironmentRetirementPlan) (bool, error)
}
