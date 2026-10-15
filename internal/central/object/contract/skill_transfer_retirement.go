package contract

import (
	"context"
	"fmt"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SkillTransferRetirementRequest locates an original prepare GET by its
// committed command identity. It carries no permission and cannot establish
// absence: a missing row or unfinished retirement remains unresolved.
type SkillTransferRetirementRequest struct {
	data func() SkillTransferRetirementDetails
}

type SkillTransferRetirementDetails struct {
	Actor      id.Actor
	OldProcess ProcessID
	Operation  OperationID
	Command    f.CommandMeta
	Owner      ObjectOwner
	Object     ObjectID
	Manifest   TransferManifest
}

func NewSkillTransferRetirementRequest(d SkillTransferRetirementDetails) (SkillTransferRetirementRequest, error) {
	if d.Actor.Validate() != nil || d.Actor.Details().Kind != id.AgentRun || d.OldProcess.Validate() != nil || d.Operation.Validate() != nil ||
		d.Command.Validate() != nil || d.Command.ExpectedVersion == nil || d.Owner.Validate() != nil || d.Owner.Details().Kind != SkillRevision ||
		d.Owner.Details().ProjectID != d.Actor.Details().ProjectID || d.Object.Validate() != nil || d.Manifest.Validate() != nil ||
		d.Command.IdempotencyKey != f.IdempotencyKey("skill-prepare-"+d.Operation.String()) {
		return SkillTransferRetirementRequest{}, bad()
	}
	d.Command = *copyTransferCommand(&d.Command)
	return SkillTransferRetirementRequest{func() SkillTransferRetirementDetails {
		out := d
		out.Command = *copyTransferCommand(&d.Command)
		return out
	}}, nil
}
func (r SkillTransferRetirementRequest) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r SkillTransferRetirementRequest) Details() SkillTransferRetirementDetails {
	if r.data == nil {
		return SkillTransferRetirementDetails{}
	}
	return r.data()
}
func (r SkillTransferRetirementRequest) Equal(other SkillTransferRetirementRequest) bool {
	if r.Validate() != nil || other.Validate() != nil {
		return false
	}
	a, b := r.Details(), other.Details()
	return a.Actor.Equal(b.Actor) && a.OldProcess == b.OldProcess && a.Operation == b.Operation &&
		a.Command.RequestID == b.Command.RequestID && a.Command.IdempotencyKey == b.Command.IdempotencyKey &&
		*a.Command.ExpectedVersion == *b.Command.ExpectedVersion && a.Owner.Equal(b.Owner) && a.Object == b.Object && a.Manifest.Equal(b.Manifest)
}
func (SkillTransferRetirementRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_transfer_retirement")
}
func (SkillTransferRetirementRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_transfer_retirement"`), nil
}
func (*SkillTransferRetirementRequest) UnmarshalJSON([]byte) error { return bad() }

// A nonzero observation proves only the original transfer's stored independent
// retirement evidence and released lease. It says nothing about package
// publication or a reusable Runner path; cold callers must report Unknown.
type SkillTransferRetirementObservation struct {
	data func() SkillTransferRetirementObservationDetails
}
type SkillTransferRetirementObservationDetails struct {
	Request          SkillTransferRetirementRequest
	Transfer         TransferID
	Lease            LeaseID
	Runner           RunnerID
	RunnerGeneration f.Version
	Evidence         TransferEvidenceRef
	EvidenceDigest   f.Digest
}

func NewSkillTransferRetirementObservation(d SkillTransferRetirementObservationDetails) (SkillTransferRetirementObservation, error) {
	if d.Request.Validate() != nil || d.Transfer.Validate() != nil || d.Lease.Validate() != nil || d.Runner.Validate() != nil ||
		d.RunnerGeneration.Validate() != nil || d.Evidence.Validate() != nil || d.EvidenceDigest.Validate() != nil {
		return SkillTransferRetirementObservation{}, bad()
	}
	return SkillTransferRetirementObservation{func() SkillTransferRetirementObservationDetails { return d }}, nil
}
func (o SkillTransferRetirementObservation) ClosedFor(r SkillTransferRetirementRequest) bool {
	return o.data != nil && o.data().Request.Equal(r)
}
func (o SkillTransferRetirementObservation) Details() SkillTransferRetirementObservationDetails {
	if o.data == nil {
		return SkillTransferRetirementObservationDetails{}
	}
	return o.data()
}

// The domain authority must hold an original stopped-Builtin private witness,
// not merely these public fields. Validation repeats its original committed
// input/Skill binding and physical old-process proof under the complete locks.
type SkillTransferRetirementAuthority interface {
	DiscoverSkillTransferRetirement(context.Context, SkillTransferRetirementRequest) (AccessDependencies, error)
	ValidateSkillTransferRetirementInTx(context.Context, f.Tx, SkillTransferRetirementRequest, AccessDependencies) error
}
type SkillTransferRetirementObserver interface {
	LookupSkillTransferRetirement(context.Context, SkillTransferRetirementRequest) (SkillTransferRetirementObservation, error)
}
