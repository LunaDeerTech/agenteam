package skill

import (
	"bytes"
	"context"
	"slices"
	"sync/atomic"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type preparationColdKey struct{}
type preparationColdWitness struct {
	authority  *PreparationAuthority
	invocation sc.PreparationInvocation
	plan       sc.InvocationPlan
	input      sc.PreparationColdInput
	locks      []f.LockRequest
	binding    sc.SkillBinding
	metadata   sc.RevisionMetadata
	request    oc.SkillTransferRetirementRequest
	active     atomic.Bool
}

func (a *PreparationAuthority) coldRuntime() (sc.PreparationColdAuthority, error) {
	if a == nil || a.core.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	runtime, ok := a.runtime.(sc.PreparationColdAuthority)
	if !ok || nilPort(runtime) {
		return nil, fault(f.DependencyUnbound)
	}
	return runtime, nil
}
func (a *PreparationAuthority) coldTransaction(ctx context.Context, w *preparationColdWitness, work func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	cause, err := f.NewRecoveryCause("skill.prepare-cold", w.invocation.Invocation.ExecutionID.String(), w.invocation.Invocation.OperationID)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var callbackErr error
	result := a.core.state().store.WithinTx(bounded, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = a.core.state().store.AcquireAll(ctx, tx, w.locks); err != nil {
			return portError(err)
		}
		x, err := a.core.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return work(ctx, tx, x)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}

func samePreparationColdInput(a, b sc.PreparationColdInput) bool {
	return a.OperationVersion.Validate() == nil && a.OldProcess.Validate() == nil && a.OperationVersion == b.OperationVersion && a.OldProcess == b.OldProcess && bytes.Equal(a.Arguments, b.Arguments) && slices.Equal(a.Bindings, b.Bindings)
}

// LookupCold never reconstructs a success path or re-enters Execute. It can
// report only an honest Unknown after Object confirms the original GET's
// independent retirement proof and exact released lease.
func (s *Preparer) LookupCold(ctx context.Context, inv sc.PreparationInvocation, raw []byte) ([]byte, bool, error) {
	if s.state() == nil || inv.Validate() != nil {
		return nil, false, invalid()
	}
	a := s.state().authority
	runtime, err := a.coldRuntime()
	if err != nil {
		return nil, false, err
	}
	observer, ok := s.state().transfers.(oc.SkillTransferRetirementObserver)
	if !ok || nilPort(observer) {
		return nil, false, fault(f.DependencyUnbound)
	}
	args, err := sc.DecodePrepareToolArguments(raw)
	if err != nil {
		return nil, false, err
	}
	call, err := s.state().core.begin(ctx, false)
	if err != nil {
		return nil, false, err
	}
	defer s.state().core.end(call)
	plan, err := runtime.DiscoverSkillPreparationCold(call.ctx, inv)
	if err != nil {
		return nil, false, portError(err)
	}
	if nilPort(plan) {
		return nil, false, fault(f.DependencyUnbound)
	}
	w := &preparationColdWitness{authority: a, invocation: inv, plan: plan, locks: plan.RequiredLocks()}
	w.active.Store(true)
	defer func() { w.active.Store(false); clear(w.input.Arguments) }()
	err = a.coldTransaction(call.ctx, w, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		return runtime.WithSkillPreparationColdInTx(ctx, tx, inv, plan, func(_ context.Context, input sc.PreparationColdInput) error {
			if !bytes.Equal(input.Arguments, raw) || input.OperationVersion.Validate() != nil || input.OldProcess.Validate() != nil {
				return fault(f.Forbidden)
			}
			w.input = input
			w.input.Arguments, w.input.Bindings = bytes.Clone(input.Arguments), slices.Clone(input.Bindings)
			return nil
		})
	})
	if err != nil {
		return nil, false, err
	}
	for _, binding := range w.input.Bindings {
		if binding.SkillID == args.SkillID {
			w.binding = binding
		}
	}
	if w.binding.Validate() != nil || w.binding.SkillID != args.SkillID {
		return nil, false, fault(f.Forbidden)
	}
	initial, err := loadInitialization(call.ctx, a.core.state().store, inv.Invocation.ProjectID)
	if err != nil {
		return nil, false, err
	}
	if initial == nil {
		return nil, false, fault(f.ResourceBusy)
	}
	w.locks, err = oc.NormalizeAccessLocks(append(w.locks, skillLock(initial.skill, f.Shared), skillLock(w.binding.SkillID, f.Shared)))
	if err != nil {
		return nil, false, err
	}
	err = a.coldTransaction(call.ctx, w, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return runtime.WithSkillPreparationColdInTx(ctx, tx, inv, plan, func(current context.Context, input sc.PreparationColdInput) error {
			if !samePreparationColdInput(w.input, input) {
				return fault(f.Forbidden)
			}
			_, metadata, err := loadAssignmentRevision(current, x, inv.Invocation.ProjectID, w.binding.SkillID)
			if err != nil {
				return err
			}
			if !runtimeMetadataMatches(metadata, w.binding) {
				return fault(f.ConfirmationStale)
			}
			w.metadata = metadata
			return nil
		})
	})
	if err != nil {
		return nil, false, err
	}
	w.locks, err = oc.NormalizeAccessLocks(append(w.locks, objectLock(w.metadata.Object.ID, f.Shared)))
	if err != nil {
		return nil, false, err
	}
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, w.binding.RevisionID.String(), inv.Invocation.ProjectID.String())
	operation, _ := f.ParseID[oc.Operation](inv.Invocation.OperationID)
	requestID, _ := f.ParseID[f.Request](string(inv.RequestID))
	manifest, err := oc.NewTransferManifest(oc.TransferManifestDetails{MediaType: w.metadata.Object.MediaType, Length: int64(w.metadata.Object.ByteSize), SHA256: w.metadata.Object.SHA256})
	if err != nil {
		return nil, false, err
	}
	version := w.input.OperationVersion
	w.request, err = oc.NewSkillTransferRetirementRequest(oc.SkillTransferRetirementDetails{Actor: inv.Invocation.Actor, OldProcess: w.input.OldProcess, Operation: operation, Command: f.CommandMeta{RequestID: requestID, IdempotencyKey: f.IdempotencyKey("skill-prepare-" + inv.Invocation.OperationID), ExpectedVersion: &version}, Owner: owner, Object: w.metadata.Object.ID, Manifest: manifest})
	if err != nil {
		return nil, false, err
	}
	owned := context.WithValue(call.ctx, preparationColdKey{}, w)
	observed, err := observer.LookupSkillTransferRetirement(owned, w.request)
	if err != nil {
		return nil, false, portError(err)
	}
	if !observed.ClosedFor(w.request) {
		return nil, false, fault(f.ResourceBusy)
	}
	result := sc.PrepareUnknownResult{SkillID: w.binding.SkillID, RevisionID: w.binding.RevisionID, Revision: w.binding.Revision, MountID: args.MountID, Outcome: "unknown", MayHavePrepared: true}
	result.Error.Code = f.DependencyUnavailable
	body, err := sc.EncodePrepareUnknownResult(result)
	return body, err == nil, err
}

func (a *PreparationAuthority) coldWitness(ctx context.Context, request oc.SkillTransferRetirementRequest) (*preparationColdWitness, error) {
	if a == nil || a.core.state() == nil || ctx == nil || request.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(preparationColdKey{}).(*preparationColdWitness)
	if w == nil || w.authority != a || !w.active.Load() || !w.request.Equal(request) {
		return nil, fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	return w, nil
}
func (a *PreparationAuthority) DiscoverSkillTransferRetirement(ctx context.Context, request oc.SkillTransferRetirementRequest) (oc.AccessDependencies, error) {
	w, err := a.coldWitness(ctx, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := request.Details()
	digest, _, err := captureDigest([]any{w.invocation.Invocation, string(w.invocation.RequestID), d.OldProcess.String(), d.Operation.String(), d.Command, d.Owner.Details(), d.Object.String(), d.Manifest.Details()})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest, w.locks)
}
func (a *PreparationAuthority) ValidateSkillTransferRetirementInTx(ctx context.Context, tx f.Tx, request oc.SkillTransferRetirementRequest, plan oc.AccessDependencies) error {
	actual, err := a.DiscoverSkillTransferRetirement(ctx, request)
	if err != nil {
		return err
	}
	if !actual.Equal(plan) {
		return fault(f.Forbidden)
	}
	w, err := a.coldWitness(ctx, request)
	if err != nil {
		return err
	}
	if err = a.core.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	runtime, err := a.coldRuntime()
	if err != nil {
		return err
	}
	return runtime.WithSkillPreparationColdInTx(ctx, tx, w.invocation, w.plan, func(current context.Context, input sc.PreparationColdInput) error {
		if !samePreparationColdInput(w.input, input) {
			return fault(f.Forbidden)
		}
		x, err := a.core.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		_, metadata, err := loadAssignmentRevision(current, x, w.invocation.Invocation.ProjectID, w.binding.SkillID)
		if err != nil {
			return err
		}
		if !runtimeMetadataMatches(metadata, w.binding) || metadata.Object.ID != w.metadata.Object.ID {
			return fault(f.ConfirmationStale)
		}
		return nil
	})
}

var _ oc.SkillTransferRetirementAuthority = (*PreparationAuthority)(nil)
