package execution

import (
	"context"
	"reflect"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mountc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	runnerc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func (a *Authority) RequireMountCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, request mountc.ExecutionMountCaptureRequest) (mountc.ExecutionMountCaptureScope, error) {
	project, binding, err := a.requireResourceDiscovery(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID, "mount")
	if err != nil {
		return mountc.ExecutionMountCaptureScope{}, err
	}
	return mountc.ExecutionMountCaptureScope{Project: project, AttemptBinding: binding}, nil
}

func (a *Authority) RequireMountCaptureInTx(ctx context.Context, tx f.Tx, request mountc.ExecutionMountCaptureRequest) (mountc.ExecutionMountCaptureFacts, error) {
	w, binding, err := a.requireResourceCapture(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID)
	if err != nil {
		return mountc.ExecutionMountCaptureFacts{}, err
	}
	config := w.agent.Fields()
	return mountc.ExecutionMountCaptureFacts{Scope: mountc.ExecutionMountCaptureScope{Project: clonePreparationProject(w.project), AttemptBinding: binding}, AgentVersion: config.Core.Version, AllowedMountIDs: config.AllowedMountIDs}, nil
}

var _ mountc.ExecutionMountCaptureAuthority = (*Authority)(nil)

// Runner metadata is a preparing input, not a Human configuration read or an
// operation grant. The original private discovery/capture witness remains its
// authority; the Runner domain separately checks its complete lock union.
func (a *Authority) RequireExecutionMetadataInTx(ctx context.Context, tx f.Tx, request runnerc.ExecutionMetadataRequest) error {
	if ctx == nil || request.Validate() != nil {
		return invalid()
	}
	if _, ok := ctx.Value(preparationDiscoveryKey{}).(*preparationDiscovery); ok {
		_, _, err := a.requireResourceDiscovery(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID, "mount")
		return err
	}
	_, _, err := a.requireResourceCapture(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID)
	return err
}

type preparationMountMaterialKey struct{}
type preparationMountMaterialWitness struct {
	driver *preparationState
	tx     f.Tx
	record *preparationInputRecord
	active atomic.Bool
}

// verifyMountMaterial never resolves current configuration. Its expected
// value came from the original immutable input (or the original same-Tx
// capture about to save that input), including the physical attempt binding.
func (s *preparationState) verifyMountMaterial(ctx context.Context, tx f.Tx, record *preparationInputRecord) error {
	if record == nil || record.input.Validate() != nil {
		return fault(f.InvalidState)
	}
	expected := record.input.Fields().Mounts
	if len(expected.Mounts) == 0 {
		return nil // Preserve historical empty captures and their zero writes.
	}
	verifier, ok := s.mounts.(mountc.ExecutionMountMaterialVerifier)
	if !ok || nilPort(verifier) {
		return fault(f.DependencyUnbound)
	}
	w := &preparationMountMaterialWitness{driver: s, tx: tx, record: record}
	w.active.Store(true)
	defer w.active.Store(false)
	if err := verifier.VerifyExecutionMountMaterialInTx(context.WithValue(ctx, preparationMountMaterialKey{}, w), tx, expected.Clone()); err != nil {
		return portError(err)
	}
	return ctx.Err()
}

func (a *Authority) RequireMountMaterialInTx(ctx context.Context, tx f.Tx, expected mountc.ExecutionMountSet) error {
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || expected.Validate() != nil || len(expected.Mounts) == 0 {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := ctx.Value(directTextMountKey{}).(*directTextMountWitness); ok {
		_, err := a.directTextMountProof(ctx, tx, expected)
		return err
	}
	w, ok := ctx.Value(preparationMountMaterialKey{}).(*preparationMountMaterialWitness)
	if !ok || w == nil || !w.active.Load() || w.driver == nil || w.driver.authority != a || w.driver.store != a.state.store || w.tx != tx || w.record == nil || w.record.input.Validate() != nil {
		return fault(f.Forbidden)
	}
	v := w.record.input.Fields()
	if !reflect.DeepEqual(v.Mounts, expected) || v.AttemptBinding != expected.AttemptBinding || v.Request.ExecutionID != w.record.claim.execution || v.Request.Launch.ProjectID != w.record.claim.project || v.Request.Launch.AgentID != w.record.claim.agent {
		return fault(f.Forbidden)
	}
	if _, err := a.state.store.InTx(tx); err != nil {
		return portError(err)
	}
	locks, err := preparationLocks(v.Request)
	if err != nil {
		return err
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if w.driver.processes.CurrentProcess() != w.driver.process || !w.active.Load() {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

var _ mountc.ExecutionMountMaterialAuthority = (*Authority)(nil)
var _ runnerc.ExecutionMetadataAuthority = (*Authority)(nil)
