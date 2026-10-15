package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// LookupTransferCommand never calls Issue/material/sign, Cancel or Confirm.
// The separate private authority proves the original provider call returned;
// acquiring its original command lock then serializes any uncertain writer.
func (t *TransferService) LookupTransferCommand(ctx context.Context, request oc.TransferObservationRequest) (oc.TransferCommandObservation, error) {
	if request.Validate() != nil {
		return oc.TransferCommandObservation{}, invalid()
	}
	if t == nil || t.data == nil || t.state().objects == nil {
		return oc.TransferCommandObservation{}, failure(f.DependencyUnbound, nil)
	}
	authority, ok := t.state().authority.(oc.TransferObservationAuthority)
	if !ok || nilPort(authority) {
		return oc.TransferCommandObservation{}, failure(f.DependencyUnbound, nil)
	}
	s := t.state().objects
	op, done, err := s.begin(ctx)
	if err != nil {
		return oc.TransferCommandObservation{}, err
	}
	defer done()
	ctx = op.ctx
	plan, err := authority.DiscoverTransferObservation(ctx, request)
	if err != nil {
		return oc.TransferCommandObservation{}, portError(err)
	}
	if plan.Validate() != nil {
		return oc.TransferCommandObservation{}, failure(f.DependencyUnbound, nil)
	}
	original := request.Details().Original
	facts, err := s.transferAccessFacts(ctx, s.state().store, original)
	if err != nil {
		return oc.TransferCommandObservation{}, err
	}
	locks, err := oc.NormalizeAccessLocks(append(facts.locks, plan.Locks()...))
	if err != nil {
		return oc.TransferCommandObservation{}, err
	}
	var out oc.TransferCommandObservation
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
		bound, err := authority.ValidateTransferObservationInTx(ctx, tx, request, plan)
		if err != nil {
			return portError(err)
		}
		if bound.Validate() != nil {
			return failure(f.Forbidden, nil)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		out, err = observeTransferCommandInTx(ctx, x, request, bound)
		return err
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return oc.TransferCommandObservation{}, callbackErr
	}
	if err = commitError(result); err != nil {
		return oc.TransferCommandObservation{}, err
	}
	if !out.SettledFor(request) {
		return oc.TransferCommandObservation{}, failure(f.InvalidState, nil)
	}
	return out, nil
}

func observeTransferCommandInTx(ctx context.Context, x postgres.SQLExecutor, request oc.TransferObservationRequest, bound oc.TransferObservationFacts) (oc.TransferCommandObservation, error) {
	d := request.Details().Original.Details()
	command, err := transferCommand(d.Spec.Details().Owner, d.IssueCommand.IdempotencyKey)
	if err != nil {
		return oc.TransferCommandObservation{}, err
	}
	r, found, err := loadTransferCommand(ctx, x, command)
	if err != nil {
		return oc.TransferCommandObservation{}, err
	}
	if !found {
		// The preallocated Issue LeaseID is not the final GET lease. Check all
		// leases of this exact Transfer owner, as well as both durable identities.
		var exists bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_transfers WHERE id=$1) OR EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE owner_kind='transfer' AND owner_id=$1)`, d.ID.String()).Scan(&exists)
		if err != nil {
			return oc.TransferCommandObservation{}, unavailable(err)
		}
		if exists || d.Operation != oc.TransferIssue {
			return oc.TransferCommandObservation{}, failure(f.InvalidState, nil)
		}
		return oc.NewTransferCommandObservation(oc.TransferCommandObservationDetails{Request: request})
	}
	if err = requireObservedTransferIdentity(r, d, bound); err != nil {
		return oc.TransferCommandObservation{}, err
	}
	// A persisted transfer FK alone is insufficient: validate its actual lease
	// owner/object/state, including released rows, under the original lease lock.
	var objectID, ownerKind, ownerID, state string
	err = x.QueryRow(ctx, `SELECT object_id::text,owner_kind,owner_id::text,state FROM agenteam_object.object_leases WHERE id=$1`, r.lease.String()).Scan(&objectID, &ownerKind, &ownerID, &state)
	if err != nil {
		return oc.TransferCommandObservation{}, unavailable(err)
	}
	if objectID != r.object.String() || ownerKind != "transfer" || ownerID != r.id.String() || state != "active" && state != "released" || (state == "active") != r.leaseActive {
		return oc.TransferCommandObservation{}, failure(f.InvalidState, nil)
	}
	applied := false
	switch d.Operation {
	case oc.TransferIssue:
		applied = true
	case oc.TransferCancel:
		var key *string
		var digest []byte
		if err = x.QueryRow(ctx, `SELECT cancel_key,cancel_digest FROM agenteam_object.object_transfers WHERE id=$1`, r.id.String()).Scan(&key, &digest); err != nil {
			return oc.TransferCommandObservation{}, unavailable(err)
		}
		if key != nil {
			expected := ""
			if d.CancelCommand.ExpectedVersion != nil {
				expected = d.CancelCommand.ExpectedVersion.String()
			}
			raw, _ := json.Marshal([]string{stableActor(d.Actor), d.ID.String(), expected})
			want := sha256.Sum256(raw)
			if *key != string(d.CancelCommand.IdempotencyKey) || !bytes.Equal(digest, want[:]) || !r.revoked {
				return oc.TransferCommandObservation{}, failure(f.IdempotencyKeyReused, nil)
			}
			applied = true
		} else if len(digest) != 0 {
			return oc.TransferCommandObservation{}, failure(f.InvalidState, nil)
		}
	case oc.TransferConfirmTerminal:
		if r.retirement.Validate() == nil {
			if r.retirement != d.Evidence.ID || r.retirementKind != d.Evidence.Kind || !bytes.Equal(r.retirementDigest, digestBytes(request.Details().EvidenceDigest)) {
				return oc.TransferCommandObservation{}, failure(f.IdempotencyKeyReused, nil)
			}
			applied = !r.leaseActive
		}
	default:
		return oc.TransferCommandObservation{}, invalid()
	}
	status := r.status()
	return oc.NewTransferCommandObservation(oc.TransferCommandObservationDetails{Request: request, Applied: applied, Status: &status})
}

func requireObservedTransferIdentity(r transferRow, d oc.TransferAccessDetails, bound oc.TransferObservationFacts) error {
	semantic, err := transferSemantic(d.Actor, d.IssueCommand, d.Spec)
	if err != nil {
		return err
	}
	actual, _ := json.Marshal(oc.TransferSpecProjection(r.spec))
	want, _ := json.Marshal(oc.TransferSpecProjection(d.Spec))
	if r.id != d.ID || r.object != d.ObjectID || r.stable != stableActor(d.Actor) || !r.actor.Equal(d.Actor) || r.issue.RequestID != d.IssueCommand.RequestID || !bytes.Equal(r.digest, semantic) || !bytes.Equal(actual, want) || !r.manifest.Equal(d.Manifest) || r.runnerGeneration != bound.RunnerGeneration || r.operationVersion != bound.OperationVersion || r.execution != bound.ExecutionID {
		return failure(f.Forbidden, nil)
	}
	if d.Operation != oc.TransferIssue && (r.lease != d.LeaseID || r.upload != d.UploadID || r.staging != d.StagingID || r.candidate != d.CandidateID || r.sourceLease != d.SourceLeaseID) {
		return failure(f.Forbidden, nil)
	}
	return nil
}

var _ oc.TransferCommandObserver = (*TransferService)(nil)
