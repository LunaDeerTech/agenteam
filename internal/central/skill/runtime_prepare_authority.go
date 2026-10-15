package skill

import (
	"bytes"
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"slices"
	"sync"
	"sync/atomic"
)

// PreparationAuthority can be constructed before Object. Its dependency is the
// same immutable domain Authority later used by Service, never a late locator.
type PreparationAuthority struct {
	core    *Authority
	runtime sc.PreparationInvocationAuthority
}

func NewPreparationAuthority(core *Authority) (*PreparationAuthority, error) {
	if core.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	runtime, ok := core.state().runtime.(sc.PreparationInvocationAuthority)
	if !ok || nilPort(runtime) {
		return nil, fault(f.DependencyUnbound)
	}
	return &PreparationAuthority{core: core, runtime: runtime}, nil
}

type preparationKey struct{}
type preparationWitness struct {
	mu                                            sync.Mutex
	mounts                                        *mount.SkillPrepareResolver
	ticket                                        mount.SkillPrepareTicket
	issueError                                    error
	issueRequest, cancelRequest, stopRequest      oc.TransferAccessRequest
	cancelUnknown                                 error
	lookupNeeded, resourcesJoined                 bool
	resultBody                                    []byte
	authority                                     *PreparationAuthority
	invocation                                    sc.PreparationInvocation
	plan                                          sc.InvocationPlan
	arguments                                     []byte
	args                                          sc.PrepareToolArguments
	bindings                                      []sc.SkillBinding
	binding                                       sc.SkillBinding
	metadata                                      sc.RevisionMetadata
	locks                                         []f.LockRequest
	intent                                        mc.SkillPrepareIntent
	active                                        atomic.Bool
	joining                                       bool
	serviceCall                                   *serviceCall
	dispatch                                      *runnerhttp.SkillPrepareCall
	runnerGeneration, operationVersion            f.Version
	spec                                          oc.TransferSpec
	command, cancelCommand                        f.CommandMeta
	transfer                                      oc.TransferID
	grant                                         oc.TransferGrant
	payload                                       p.SkillPreparePayload
	issued, materialReleased, completed, returned bool
	cancelled                                     bool
	completion, retirement                        oc.TransferEvidenceRef
	completionDigest, retirementDigest            f.Digest
	terminal                                      *p.SkillPrepareResult
	cleanupUnknown                                error
}

func (a *PreparationAuthority) witness(ctx context.Context) (*preparationWitness, error) {
	if a == nil || a.core.state() == nil || ctx == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w, _ := ctx.Value(preparationKey{}).(*preparationWitness)
	if w == nil || w.authority != a || !w.active.Load() || w.invocation.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *PreparationAuthority) current(ctx context.Context, tx f.Tx, w *preparationWitness, work func(context.Context) error) error {
	if w == nil || w.authority != a || !w.active.Load() || w.joining {
		return fault(f.Forbidden)
	}
	if err := a.core.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	return a.runtime.WithSkillPreparationInTx(ctx, tx, w.invocation, w.plan, func(current context.Context, raw []byte, bindings []sc.SkillBinding, version f.Version) error {
		if !bytes.Equal(raw, w.arguments) || !slices.Equal(bindings, w.bindings) || version != w.operationVersion {
			return fault(f.Forbidden)
		}
		validity, err := (&AssignmentValidation{store: a.core.state().store}).ValidateAssignmentInTx(current, tx, w.invocation.Invocation.ProjectID, w.invocation.Invocation.AgentID, w.binding.AssignmentID, w.binding.SkillID, w.binding.RevisionID, w.binding.Revision)
		if err != nil {
			return err
		}
		if validity != sc.AssignmentValid {
			return fault(f.Forbidden)
		}
		x, err := a.core.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		_, meta, err := loadAssignmentRevision(current, x, w.invocation.Invocation.ProjectID, w.binding.SkillID)
		if err != nil {
			return err
		}
		if !runtimeMetadataMatches(meta, w.binding) || meta.Object.ID != w.metadata.Object.ID {
			return fault(f.ConfirmationStale)
		}
		return work(current)
	})
}
func (a *PreparationAuthority) cleanup(ctx context.Context, tx f.Tx, w *preparationWitness) error {
	if !w.joining || !w.returned || !w.active.Load() {
		return fault(f.Forbidden)
	}
	if err := a.core.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	version, err := a.runtime.RequireSkillPreparationRetirementInTx(ctx, tx, w.invocation, w.plan)
	if err != nil {
		return portError(err)
	}
	if version != w.operationVersion {
		return fault(f.ConfirmationStale)
	}
	return nil
}
func (a *PreparationAuthority) SkillPrepareLocks(ctx context.Context, in mc.SkillPrepareIntent) ([]f.LockRequest, error) {
	w, err := a.witness(ctx)
	if err != nil {
		return nil, err
	}
	if !samePreparationIntent(in, w.intent) {
		return nil, fault(f.Forbidden)
	}
	return slices.Clone(w.locks), nil
}
func samePreparationIntent(x, y mc.SkillPrepareIntent) bool {
	return x.Validate() == nil && y.Validate() == nil && x.Actor.Equal(y.Actor) && x.ProjectID == y.ProjectID && x.AgentID == y.AgentID && x.ExecutionID == y.ExecutionID && x.OperationID == y.OperationID && x.RequestID == y.RequestID && x.MountID == y.MountID && x.SkillID == y.SkillID && x.RevisionID == y.RevisionID && x.Revision == y.Revision && x.PackageSHA256 == y.PackageSHA256
}
func (a *PreparationAuthority) WithSkillPrepareInTx(ctx context.Context, tx f.Tx, in mc.SkillPrepareIntent, payload *p.SkillPreparePayload, work func(context.Context) error) error {
	w, err := a.witness(ctx)
	if err != nil {
		return err
	}
	if work == nil || !samePreparationIntent(in, w.intent) {
		return fault(f.Forbidden)
	}
	return a.current(ctx, tx, w, func(current context.Context) error {
		if payload != nil {
			if w.dispatch == nil || !w.issued || w.materialReleased {
				return fault(f.Forbidden)
			}
			actual, e := p.EncodeSkillPreparePayload(*payload)
			if e != nil {
				return fault(f.Forbidden)
			}
			defer clear(actual)
			expected, e := p.EncodeSkillPreparePayload(w.payload)
			if e != nil {
				return fault(f.Forbidden)
			}
			defer clear(expected)
			if !bytes.Equal(actual, expected) {
				return fault(f.Forbidden)
			}
			generation, e := w.dispatch.RequireCurrentInTx(current, tx)
			if e != nil {
				return e
			}
			if generation != w.runnerGeneration {
				return fault(f.ConfirmationStale)
			}
		}
		return work(current)
	})
}
func samePreparationCommand(x, y f.CommandMeta) bool {
	if x.Validate() != nil || y.Validate() != nil || x.RequestID != y.RequestID || x.IdempotencyKey != y.IdempotencyKey {
		return false
	}
	return x.ExpectedVersion == nil && y.ExpectedVersion == nil || x.ExpectedVersion != nil && y.ExpectedVersion != nil && *x.ExpectedVersion == *y.ExpectedVersion
}
func (a *PreparationAuthority) transferWitness(ctx context.Context, r oc.TransferAccessRequest) (*preparationWitness, error) {
	w, err := a.witness(ctx)
	if err != nil {
		return nil, err
	}
	if r.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	d := r.Details()
	want := w.metadata.Object
	actual, _ := json.Marshal(oc.TransferSpecProjection(d.Spec))
	expected, _ := json.Marshal(oc.TransferSpecProjection(w.spec))
	if !d.Actor.Equal(w.invocation.Invocation.Actor) || !samePreparationCommand(d.IssueCommand, w.command) || !bytes.Equal(actual, expected) || d.ObjectID != want.ID || d.Manifest.Details().Length != int64(want.ByteSize) || d.Manifest.Details().SHA256 != want.SHA256 || d.Manifest.Details().MediaType != want.MediaType {
		return nil, fault(f.Forbidden)
	}
	if w.transfer == (oc.TransferID{}) {
		// This captures the actual provider-generated identity before its first Tx;
		// it grants neither existence nor commit and is never used to reissue.
		if d.Operation != oc.TransferIssue || w.joining {
			return nil, fault(f.Forbidden)
		}
		w.transfer = d.ID
	} else if w.transfer != d.ID {
		return nil, fault(f.Forbidden)
	}
	var captured *oc.TransferAccessRequest
	switch d.Operation {
	case oc.TransferIssue:
		captured = &w.issueRequest
	case oc.TransferCancel:
		captured = &w.cancelRequest
	case oc.TransferConfirmTerminal:
		captured = &w.stopRequest
	}
	if captured != nil {
		if captured.Validate() == nil && !captured.Equal(r) {
			return nil, fault(f.Forbidden)
		}
		*captured = r
	}
	switch d.Operation {
	case oc.TransferIssue, oc.TransferMaterialize:
		if w.joining || w.issued || w.materialReleased {
			return nil, fault(f.Forbidden)
		}
	case oc.TransferInspect:
	case oc.TransferCapture:
		if !w.returned || w.terminal == nil || d.Evidence == nil || *d.Evidence != w.completion {
			return nil, fault(f.Forbidden)
		}
	case oc.TransferCancel:
		if !w.joining || d.CancelCommand == nil || !samePreparationCommand(*d.CancelCommand, w.cancelCommand) {
			return nil, fault(f.Forbidden)
		}
	case oc.TransferConfirmTerminal:
		if !w.joining || !w.returned || !w.materialReleased || w.dispatch != nil && !w.dispatch.Retirement() || d.Evidence == nil || *d.Evidence != w.retirement {
			return nil, fault(f.Forbidden)
		}
	default:
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *PreparationAuthority) Discover(ctx context.Context, r oc.TransferAccessRequest) (oc.AccessDependencies, error) {
	w, err := a.transferWitness(ctx, r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	digest, _, err := captureDigest([]any{w.invocation.Invocation.OperationID, w.invocation.Invocation.AttemptID, string(w.invocation.RequestID), r.Fingerprint(), w.metadata.ID.String(), w.binding.AssignmentID.String()})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest, w.locks)
}
func (a *PreparationAuthority) ValidateInTx(ctx context.Context, tx f.Tx, r oc.TransferAccessRequest, plan oc.AccessDependencies) (oc.TransferAuthorization, error) {
	actual, err := a.Discover(ctx, r)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	if !actual.Equal(plan) {
		return oc.TransferAuthorization{}, fault(f.Forbidden)
	}
	w, err := a.transferWitness(ctx, r)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	if w.joining {
		err = a.cleanup(ctx, tx, w)
	} else {
		err = a.current(ctx, tx, w, func(current context.Context) error {
			if w.dispatch == nil || w.mounts == nil {
				return fault(f.Forbidden)
			}
			if e := w.mounts.RequireCurrentInTx(current, tx, w.ticket); e != nil {
				return e
			}
			generation, e := w.dispatch.RequireCurrentInTx(current, tx)
			if e != nil {
				return e
			}
			if generation != w.runnerGeneration {
				return fault(f.ConfirmationStale)
			}
			return nil
		})
	}
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	out := oc.TransferAuthorizationDetails{Request: r, RunnerGeneration: w.runnerGeneration, OperationVersion: w.operationVersion, ExecutionID: w.invocation.Invocation.ExecutionID}
	if r.Details().Operation == oc.TransferCapture {
		out.Completed = &oc.TransferCompletion{Evidence: w.completion, Digest: w.completionDigest, Length: int64(w.metadata.Object.ByteSize), SHA256: w.metadata.PackageSHA256}
	}
	if r.Details().Operation == oc.TransferConfirmTerminal {
		out.Retirement = &oc.TransferRetirement{Evidence: w.retirement, Digest: w.retirementDigest, AllRequestsJoined: true, StorageAdmissionClosed: true}
	}
	return oc.NewTransferAuthorization(out)
}

var _ mc.SkillPrepareAuthority = (*PreparationAuthority)(nil)
var _ oc.RunnerTransferAuthority = (*PreparationAuthority)(nil)
