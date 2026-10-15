package object

import (
	"bytes"
	"context"
	"crypto/sha256"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// LookupSkillTransferRetirement is read-only. In particular it cannot turn a
// missing Issue into settled absence or release a lease on process death alone.
func (t *TransferService) LookupSkillTransferRetirement(ctx context.Context, request oc.SkillTransferRetirementRequest) (oc.SkillTransferRetirementObservation, error) {
	if request.Validate() != nil {
		return oc.SkillTransferRetirementObservation{}, invalid()
	}
	if t == nil || t.data == nil || t.state().objects == nil {
		return oc.SkillTransferRetirementObservation{}, failure(f.DependencyUnbound, nil)
	}
	authority, ok := t.state().authority.(oc.SkillTransferRetirementAuthority)
	if !ok || nilPort(authority) {
		return oc.SkillTransferRetirementObservation{}, failure(f.DependencyUnbound, nil)
	}
	s := t.state().objects
	op, done, err := s.begin(ctx)
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	defer done()
	ctx = op.ctx
	if request.Details().OldProcess == s.state().process {
		return oc.SkillTransferRetirementObservation{}, failure(f.ResourceBusy, nil)
	}
	plan, err := authority.DiscoverSkillTransferRetirement(ctx, request)
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, portError(err)
	}
	if plan.Validate() != nil {
		return oc.SkillTransferRetirementObservation{}, failure(f.DependencyUnbound, nil)
	}
	d := request.Details()
	command, err := transferCommand(d.Owner, d.Command.IdempotencyKey)
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	prior, found, err := loadTransferCommand(ctx, s.state().store, command)
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	if !found {
		return oc.SkillTransferRetirementObservation{}, failure(f.ResourceBusy, nil)
	}
	if err = requireSkillTransferRetirementIdentity(prior, d); err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	read, err := prior.request(d.Actor, oc.TransferInspect, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	facts, err := s.transferAccessFacts(ctx, s.state().store, read.Details().Transfer)
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	locks, err := oc.NormalizeAccessLocks(append(facts.locks, plan.Locks()...))
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	var out oc.SkillTransferRetirementObservation
	var callbackErr error
	result := s.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		guard, ok := s.state().auth.Processes.(interface {
			RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
		})
		if !ok || nilPort(guard) {
			return failure(f.DependencyUnbound, nil)
		}
		if err = guard.RequireCurrentProcessInTx(ctx, tx, s.state().process); err != nil {
			return portError(err)
		}
		if err = authority.ValidateSkillTransferRetirementInTx(ctx, tx, request, plan); err != nil {
			return portError(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, found, err := loadTransferCommand(ctx, x, command)
		if err != nil {
			return err
		}
		if !found {
			return failure(f.ResourceBusy, nil)
		}
		if err = requireSkillTransferRetirementIdentity(current, d); err != nil {
			return err
		}
		before, err := maintenanceRowBinding(prior)
		if err != nil {
			return err
		}
		after, err := maintenanceRowBinding(current)
		if err != nil || before != after {
			return accessChanged()
		}
		out, err = observeSkillTransferRetirement(ctx, x, request, current)
		return err
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return oc.SkillTransferRetirementObservation{}, callbackErr
	}
	if err = commitError(result); err != nil {
		return oc.SkillTransferRetirementObservation{}, err
	}
	if !out.ClosedFor(request) {
		return oc.SkillTransferRetirementObservation{}, failure(f.InvalidState, nil)
	}
	return out, nil
}

func requireSkillTransferRetirementIdentity(r transferRow, d oc.SkillTransferRetirementDetails) error {
	spec := r.spec.Details()
	if d.Command.ExpectedVersion == nil || spec.Direction != oc.TransferGET || !spec.Owner.Equal(d.Owner) || spec.ObjectID != d.Object || r.object != d.Object || spec.OperationID != d.Operation ||
		!r.actor.Equal(d.Actor) || r.stable != stableActor(d.Actor) || r.execution.String() != d.Actor.Details().ExecutionID ||
		r.issue.RequestID != d.Command.RequestID || r.issue.IdempotencyKey != d.Command.IdempotencyKey || r.issue.ExpectedVersion == nil ||
		*r.issue.ExpectedVersion != *d.Command.ExpectedVersion || r.operationVersion != *d.Command.ExpectedVersion || !r.manifest.Equal(d.Manifest) {
		return failure(f.Forbidden, nil)
	}
	semantic, err := transferSemantic(d.Actor, d.Command, r.spec)
	if err != nil || !bytes.Equal(semantic, r.digest) {
		return failure(f.Forbidden, nil)
	}
	return nil
}

func observeSkillTransferRetirement(ctx context.Context, x postgres.SQLExecutor, request oc.SkillTransferRetirementRequest, r transferRow) (oc.SkillTransferRetirementObservation, error) {
	evidence := oc.TransferEvidenceRef{ID: r.retirement, Kind: r.retirementKind}
	if evidence.Validate() != nil || len(r.retirementDigest) != sha256.Size || r.leaseActive {
		return oc.SkillTransferRetirementObservation{}, failure(f.ResourceBusy, nil)
	}
	var released bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=$1 AND object_id=$2 AND owner_kind='transfer' AND owner_id=$3 AND process_id IS NULL AND state='released')`, r.lease.String(), r.object.String(), r.id.String()).Scan(&released)
	if err != nil {
		return oc.SkillTransferRetirementObservation{}, unavailable(err)
	}
	if !released {
		return oc.SkillTransferRetirementObservation{}, failure(f.ResourceBusy, nil)
	}
	return oc.NewSkillTransferRetirementObservation(oc.SkillTransferRetirementObservationDetails{Request: request, Transfer: r.id, Lease: r.lease, Runner: r.spec.Details().RunnerID, RunnerGeneration: r.runnerGeneration, Evidence: evidence, EvidenceDigest: newDigest(r.retirementDigest)})
}

var _ oc.SkillTransferRetirementObserver = (*TransferService)(nil)
