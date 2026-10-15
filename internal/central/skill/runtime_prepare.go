package skill

import (
	"bytes"
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"slices"
	"sync"
	"time"
)

// Preparer retains the original local call through Transfer retirement. It
// owns no second installation/Operation ledger and never automatically retries
// a Runner dispatch or an unknown Issue transaction.
type Preparer struct{ data func() *preparerState }

func (s *Preparer) state() *preparerState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}

type preparerState struct {
	core       *Service
	authority  *PreparationAuthority
	transfers  oc.Transfers
	observer   oc.TransferCommandObserver
	mounts     *mount.SkillPrepareResolver
	dispatcher *runnerhttp.SkillPrepareDispatcher
	mu         sync.Mutex
	pending    map[string]*preparationWitness
}

func NewPreparer(core *Service, authority *PreparationAuthority, transfers oc.Transfers, mounts *mount.SkillPrepareResolver, dispatcher *runnerhttp.SkillPrepareDispatcher) (*Preparer, error) {
	if core.state() == nil || authority == nil || authority.core != core.state().authority || nilPort(transfers) || mounts == nil || dispatcher == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if _, ok := authority.runtime.(sc.InvocationRetirementAuthority); !ok {
		return nil, fault(f.DependencyUnbound)
	}
	observer, ok := transfers.(oc.TransferCommandObserver)
	if !ok || nilPort(observer) {
		return nil, fault(f.DependencyUnbound)
	}
	state := &preparerState{core: core, authority: authority, transfers: transfers, observer: observer, mounts: mounts, dispatcher: dispatcher, pending: map[string]*preparationWitness{}}
	return &Preparer{data: func() *preparerState { return state }}, nil
}
func (s *Preparer) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if s.state() == nil || s.state().core == nil {
		return fault(f.DependencyUnbound)
	}
	return s.state().core.RequireSkillBuiltinBindingInTx(ctx, tx, store)
}
func (a *PreparationAuthority) transaction(ctx context.Context, w *preparationWitness, work func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	cause, err := f.NewRecoveryCause("skill.prepare", w.invocation.Invocation.ExecutionID.String(), w.invocation.Invocation.OperationID)
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
func preparationKeyFor(inv sc.PreparationInvocation) string {
	return inv.Invocation.OperationID + "/" + inv.Invocation.AttemptID + "/" + string(inv.RequestID)
}
func (s *Preparer) Execute(ctx context.Context, inv sc.PreparationInvocation, raw []byte) (out []byte, err error) {
	if s.state() == nil || s.state().core == nil || inv.Validate() != nil {
		return nil, invalid()
	}
	args, err := sc.DecodePrepareToolArguments(raw)
	if err != nil {
		return nil, err
	}
	plan, err := s.state().authority.runtime.DiscoverSkillInvocation(ctx, inv.Invocation)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(plan) {
		return nil, fault(f.DependencyUnbound)
	}
	call, err := s.state().core.begin(ctx, false)
	if err != nil {
		return nil, err
	}
	w := &preparationWitness{authority: s.state().authority, invocation: inv, plan: plan, arguments: bytes.Clone(raw), args: args, serviceCall: call}
	w.active.Store(true)
	w.mu.Lock()
	defer w.mu.Unlock()
	key := preparationKeyFor(inv)
	s.state().mu.Lock()
	_, exists := s.state().pending[key]
	if !exists {
		s.state().pending[key] = w
	}
	s.state().mu.Unlock()
	if exists {
		clear(w.arguments)
		s.state().core.end(call)
		return nil, fault(f.InvalidState)
	}
	defer func() {
		w.returned = true
		w.lookupNeeded = preparationUncertain(err)
		w.payload.Transfer = p.SkillTransfer{}
		w.grant.Material = oc.TransferMaterial{}
		w.materialReleased = true
	}()
	a := s.state().authority
	w.locks = w.plan.RequiredLocks()
	err = a.transaction(call.ctx, w, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		return a.runtime.WithSkillPreparationInTx(ctx, tx, inv, w.plan, func(_ context.Context, original []byte, bindings []sc.SkillBinding, version f.Version) error {
			if !bytes.Equal(original, w.arguments) {
				return fault(f.Forbidden)
			}
			w.bindings = slices.Clone(bindings)
			w.operationVersion = version
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	for _, binding := range w.bindings {
		if binding.SkillID == args.SkillID {
			w.binding = binding
		}
	}
	if w.binding.SkillID != args.SkillID {
		return nil, fault(f.Forbidden)
	}
	initial, err := loadInitialization(call.ctx, a.core.state().store, inv.Invocation.ProjectID)
	if err != nil {
		return nil, err
	}
	if initial == nil {
		return nil, fault(f.Forbidden)
	}
	w.locks, err = oc.NormalizeAccessLocks(append(w.locks, skillLock(initial.skill, f.Shared), skillLock(w.binding.SkillID, f.Shared)))
	if err != nil {
		return nil, err
	}
	err = a.transaction(call.ctx, w, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return a.runtime.WithSkillPreparationInTx(ctx, tx, inv, w.plan, func(current context.Context, original []byte, bindings []sc.SkillBinding, version f.Version) error {
			if !bytes.Equal(original, w.arguments) || !slices.Equal(bindings, w.bindings) || version != w.operationVersion {
				return fault(f.Forbidden)
			}
			valid, e := (&AssignmentValidation{store: a.core.state().store}).ValidateAssignmentInTx(current, tx, inv.Invocation.ProjectID, inv.Invocation.AgentID, w.binding.AssignmentID, w.binding.SkillID, w.binding.RevisionID, w.binding.Revision)
			if e != nil {
				return e
			}
			if valid != sc.AssignmentValid {
				return fault(f.Forbidden)
			}
			_, w.metadata, e = loadAssignmentRevision(current, x, inv.Invocation.ProjectID, w.binding.SkillID)
			if e != nil {
				return e
			}
			if !runtimeMetadataMatches(w.metadata, w.binding) {
				return fault(f.ConfirmationStale)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	w.locks, err = oc.NormalizeAccessLocks(append(w.locks, objectLock(w.metadata.Object.ID, f.Shared)))
	if err != nil {
		return nil, err
	}
	operation, _ := f.ParseID[tc.Operation](inv.Invocation.OperationID)
	w.intent = mc.SkillPrepareIntent{Actor: inv.Invocation.Actor, ProjectID: inv.Invocation.ProjectID, AgentID: inv.Invocation.AgentID, ExecutionID: inv.Invocation.ExecutionID, OperationID: operation, RequestID: inv.RequestID, MountID: args.MountID, SkillID: p.ID(w.binding.SkillID.String()), RevisionID: p.ID(w.binding.RevisionID.String()), Revision: w.binding.Revision, PackageSHA256: w.binding.PackageSHA256}
	owned := context.WithValue(call.ctx, preparationKey{}, w)
	first, err := s.state().mounts.Resolve(owned, w.intent)
	if err != nil {
		return nil, err
	}
	provisional, err := s.state().dispatcher.Prepare(first)
	if err != nil {
		return nil, err
	}
	runnerLocks, err := provisional.RequiredLocks()
	if err != nil {
		return nil, err
	}
	if err = provisional.CloseUnsent(); err != nil {
		return nil, err
	}
	w.locks, err = oc.NormalizeAccessLocks(append(append(w.locks, first.RequiredLocks()...), runnerLocks...))
	if err != nil {
		return nil, err
	}
	// Refine the precollected plan before signing. This is another read-only
	// metadata ticket, not a second wire attempt or a replacement of any capture.
	w.ticket, err = s.state().mounts.Resolve(owned, w.intent)
	if err != nil {
		return nil, err
	}
	before, _ := first.Target()
	target, err := w.ticket.Target()
	if err != nil {
		return nil, err
	}
	if target != before {
		return nil, fault(f.ConfirmationStale)
	}
	w.mounts = s.state().mounts
	w.dispatch, err = s.state().dispatcher.Prepare(w.ticket)
	if err != nil {
		return nil, err
	}
	err = a.transaction(owned, w, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		return a.current(ctx, tx, w, func(current context.Context) error {
			var e error
			w.runnerGeneration, e = w.dispatch.RequireCurrentInTx(current, tx)
			return e
		})
	})
	if err != nil {
		return nil, err
	}
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, w.metadata.ID.String(), inv.Invocation.ProjectID.String())
	runner, _ := f.ParseID[oc.Runner](target.RunnerID.String())
	transferOperation, _ := f.ParseID[oc.Operation](inv.Invocation.OperationID)
	w.spec, err = oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: runner, OperationID: transferOperation, Direction: oc.TransferGET, Owner: owner, ObjectID: w.metadata.Object.ID, ExpiresInSeconds: 60})
	if err != nil {
		return nil, err
	}
	requestID, _ := f.ParseID[f.Request](string(inv.RequestID))
	version := w.operationVersion
	w.command = f.CommandMeta{RequestID: requestID, IdempotencyKey: f.IdempotencyKey("skill-prepare-" + inv.Invocation.OperationID), ExpectedVersion: &version}
	w.cancelCommand = f.CommandMeta{RequestID: requestID, IdempotencyKey: f.IdempotencyKey("skill-prepare-stop-" + inv.Invocation.OperationID)}
	evidence, err := f.NewID[oc.TransferEvidence]()
	if err != nil {
		return nil, err
	}
	w.completion = oc.TransferEvidenceRef{ID: evidence, Kind: oc.TransferCompletedEvidence}
	retirement, err := f.NewID[oc.TransferEvidence]()
	if err != nil {
		return nil, err
	}
	w.retirement = oc.TransferEvidenceRef{ID: retirement, Kind: oc.TransferStoppedEvidence}
	w.grant, err = s.state().transfers.IssueTransfer(owned, inv.Invocation.Actor, w.command, w.spec)
	if err != nil {
		w.issueError = err
		return nil, err
	}
	if w.grant.Status.ID != w.transfer || w.grant.Status.OperationID != transferOperation || w.grant.Status.RunnerID != runner || w.grant.Status.ObjectID != w.metadata.Object.ID || !w.grant.Status.LeaseActive || w.grant.SHA256 != w.metadata.PackageSHA256 || w.grant.ByteSize != w.metadata.Object.ByteSize {
		return nil, fault(f.InvalidState)
	}
	w.issued = true
	material, err := w.grant.Material.ForRunner(runner, w.transfer)
	if err != nil {
		return nil, err
	}
	manifest, err := w.metadata.Manifest.Details()
	if err != nil {
		return nil, err
	}
	files := make([]p.SkillFile, len(manifest.Files))
	for n, file := range manifest.Files {
		files[n] = p.SkillFile{Path: file.Path, MediaType: file.MediaType, ByteSize: p.Decimal(file.ByteSize.String()), SHA256: file.SHA256.String()}
	}
	expires, err := p.NewInstant(material.WireExpiresAt.Time())
	if err != nil {
		return nil, err
	}
	w.payload = p.SkillPreparePayload{WorkspaceName: target.WorkspaceName, SkillID: w.intent.SkillID, RevisionID: w.intent.RevisionID, Revision: p.Decimal(w.intent.Revision.String()), ArchiveBytes: p.Decimal(w.grant.ByteSize.String()), ArchiveSHA256: w.grant.SHA256.String(), Manifest: p.SkillManifest{Format: manifest.Format, EntryPath: manifest.EntryPath, Files: files}, Transfer: p.SkillTransfer{ID: p.ID(w.transfer.String()), Method: material.Method, URL: material.URL, Headers: material.Headers, ExpiresAt: expires}}
	result, err := w.dispatch.Dispatch(owned, w.payload)
	w.payload.Transfer = p.SkillTransfer{}
	material = oc.RunnerTransferMaterial{}
	w.grant.Material = oc.TransferMaterial{}
	w.materialReleased = true
	if err != nil {
		return nil, err
	}
	if result.Terminal == nil {
		return nil, f.NewFault(f.CommitUnknown, f.Unknown)
	}
	if result.Terminal.Outcome == p.Unknown {
		// A received terminal proves physical return, but not durable package
		// publication. Preserve this distinction through the original Tool result.
		if !w.dispatch.Retirement() {
			return nil, f.NewFault(f.CommitUnknown, f.Unknown)
		}
		unknown := sc.PrepareUnknownResult{SkillID: w.binding.SkillID, RevisionID: w.binding.RevisionID, Revision: w.binding.Revision, MountID: args.MountID, Outcome: "unknown", MayHavePrepared: true}
		unknown.Error.Code = f.DependencyUnavailable
		return sc.EncodePrepareUnknownResult(unknown)
	}
	if result.Terminal.Outcome != p.Success {
		return nil, fault(f.DependencyUnavailable)
	}
	prepared, err := p.DecodeSkillPrepareResult(result.Terminal.Payload)
	if err != nil {
		return nil, fault(f.InvalidState)
	}
	w.terminal = &prepared
	w.returned = true
	w.resultBody, err = json.Marshal(struct {
		SkillID    sc.SkillID    `json:"skill_id"`
		RevisionID sc.RevisionID `json:"revision_id"`
		Revision   f.Revision    `json:"revision"`
		MountID    string        `json:"mount_id"`
		Path       string        `json:"path"`
		Reused     bool          `json:"reused"`
	}{w.binding.SkillID, w.binding.RevisionID, w.binding.Revision, args.MountID.String(), prepared.RelativePath, prepared.Reused})
	if err != nil {
		return nil, err
	}
	w.completionDigest, _, err = captureDigest([]any{w.invocation.Invocation.OperationID, string(inv.RequestID), prepared.SkillID, prepared.RevisionID, prepared.TransferID, prepared.ArchiveSHA256, prepared.RelativePath, prepared.Reused, prepared.Durable})
	if err != nil {
		return nil, err
	}
	_, err = s.state().transfers.CompleteTransfer(owned, inv.Invocation.Actor, w.transfer, w.completion)
	if err != nil {
		// A failed/uncertain bookkeeping call cannot negate the returned
		// durable Runner result. Retain that exact result until cleanup joins.
		return nil, f.NewFault(f.CommitUnknown, f.Unknown).WithCause(err)
	}
	w.completed = true
	return bytes.Clone(w.resultBody), nil
}
func (s *Preparer) Join(ctx context.Context, inv sc.PreparationInvocation) error {
	if s.state() == nil || inv.Validate() != nil {
		return invalid()
	}
	if err := s.state().authority.runtime.(sc.InvocationRetirementAuthority).RequireSkillInvocationRetirement(ctx, inv.Invocation); err != nil {
		return portError(err)
	}
	key := preparationKeyFor(inv)
	s.state().mu.Lock()
	w := s.state().pending[key]
	s.state().mu.Unlock()
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.resourcesJoined {
		return nil
	}
	if !w.returned || !w.active.Load() {
		return fault(f.ResourceBusy)
	}
	w.joining = true
	owned := context.WithValue(ctx, preparationKey{}, w)
	if w.dispatch != nil {
		if err := w.dispatch.CloseUnsent(); err != nil {
			return err
		}
		if !w.dispatch.Retirement() {
			return f.NewFault(f.CommitUnknown, f.Unknown)
		}
	}
	if err := s.retirePreparationTransfer(owned, w); err != nil {
		return err
	}
	w.resourcesJoined = true
	w.active.Store(false)
	clear(w.arguments)
	w.payload = p.SkillPreparePayload{}
	w.grant = oc.TransferGrant{}
	if !w.lookupNeeded {
		clear(w.resultBody)
		s.state().mu.Lock()
		delete(s.state().pending, key)
		s.state().mu.Unlock()
	}
	s.state().core.end(w.serviceCall)
	return nil
}
