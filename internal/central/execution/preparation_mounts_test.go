package execution

import (
	"context"
	"reflect"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// Only Mount storage/material is controlled here. The original preparation
// driver, private Execution authority, stored input codec and physical-result
// controls run unchanged. Real material persistence belongs to the PG test.
type preparationMountControl struct {
	*completeCaptureControl
	metadata    mt.ExecutionMountMetadata
	material    mt.ExecutionMountSet
	materialErr error
	verifyCalls int
	verifyCtx   context.Context
	verifyTx    f.Tx
}

func newPreparationMountControl(t *testing.T) *preparationMountControl {
	t.Helper()
	r := &preparationMountControl{completeCaptureControl: newCompleteCaptureControl(t)}
	r.metadata = mt.ExecutionMountMetadata{ID: newTestID[i.Mount](t), WorkspaceID: newTestID[mt.Workspace](t), RunnerID: newTestID[rc.Runner](t), RunnerVersion: 1, RunnerName: "captured-runner", Name: "source", Workspace: "workspace", Version: 1}
	fields := r.p.launch.config.Fields()
	fields.AllowedMountIDs = []i.MountID{r.metadata.ID}
	var err error
	r.p.launch.config, err = ac.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	r.p.driver.state.mounts = r
	return r
}

func (r *preparationMountControl) CaptureExecutionMountsInTx(ctx context.Context, tx f.Tx, request mt.ExecutionMountCaptureRequest, plan mt.ExecutionMountCapturePlan) (mt.ExecutionMountSet, error) {
	facts, err := r.p.launch.authority.RequireMountCaptureInTx(ctx, tx, request)
	if err != nil {
		return mt.ExecutionMountSet{}, err
	}
	if err = r.requirePlan(ctx, tx, plan, "mount", facts.Scope.AttemptBinding); err != nil {
		return mt.ExecutionMountSet{}, err
	}
	runnerRequest := rc.ExecutionMetadataRequest{ProjectID: request.ProjectID, AgentID: request.AgentID, ExecutionID: request.ExecutionID, RunnerIDs: []rc.RunnerID{r.metadata.RunnerID}}
	if err = r.p.launch.authority.RequireExecutionMetadataInTx(ctx, tx, runnerRequest); err != nil {
		return mt.ExecutionMountSet{}, err
	}
	requireCode(r.p.launch.t, r.p.launch.authority.RequireExecutionMetadataInTx(context.Background(), tx, runnerRequest), f.Forbidden)
	r.material = mt.ExecutionMountSet{Request: request, AgentVersion: facts.AgentVersion, Mounts: []mt.ExecutionMountMetadata{r.metadata}, AttemptBinding: facts.Scope.AttemptBinding}
	return r.material.Clone(), nil
}

func (r *preparationMountControl) VerifyExecutionMountMaterialInTx(ctx context.Context, tx f.Tx, expected mt.ExecutionMountSet) error {
	a, t := r.p.launch.authority, r.p.launch.t
	if err := a.RequireMountMaterialInTx(ctx, tx, expected); err != nil {
		return err
	}
	r.verifyCalls++
	r.verifyCtx, r.verifyTx = ctx, tx
	requireCode(t, a.RequireMountMaterialInTx(context.Background(), tx, expected), f.Forbidden)
	requireCode(t, a.RequireMountMaterialInTx(ctx, f.NewTx(), expected), f.Forbidden)
	foreign := expected.Clone()
	foreign.AttemptBinding = c.TriggerInputDigest([]byte("foreign-material-attempt"))
	requireCode(t, a.RequireMountMaterialInTx(ctx, tx, foreign), f.Forbidden)
	if r.materialErr != nil {
		return r.materialErr
	}
	if !reflect.DeepEqual(expected, r.material) {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}

type captureWithoutMaterialVerifier struct {
	mt.ExecutionMountCaptureProvider
}

func TestExecutionPreparationMountMaterialRecovery(t *testing.T) {
	t.Run("current-changes-do-not-recapture", func(t *testing.T) {
		r := newPreparationMountControl(t)
		p := r.p
		if err := p.driver.Run(context.Background(), p.execution); err != nil {
			t.Fatal(err)
		}
		calls := len(r.order)
		if r.verifyCalls == 0 || p.store.rows[p.execution.String()][3] != "preparing" {
			t.Fatal("nonempty input skipped material or started runtime")
		}
		// Keep the original private value but remove the caller cancellation:
		// this independently proves witness expiry, not context precedence.
		requireCode(t, p.launch.authority.RequireMountMaterialInTx(context.WithoutCancel(r.verifyCtx), r.verifyTx, r.material), f.Forbidden)
		fields := p.launch.config.Fields()
		fields.AllowedMountIDs = []i.MountID{}
		var err error
		p.launch.config, err = ac.NewAgentConfig(fields)
		if err != nil {
			t.Fatal(err)
		}
		verified := r.verifyCalls
		if err = p.driver.Run(context.Background(), p.execution); err != nil || len(r.order) != calls || r.verifyCalls != verified+1 {
			t.Fatal("historical input recaptured current sources or skipped material", err)
		}
		r.materialErr = fault(f.DependencyUnavailable)
		requireCode(t, p.driver.Run(context.Background(), p.execution), f.DependencyUnavailable)
		if len(r.order) != calls {
			t.Fatal("missing historical material caused recapture")
		}
	})
	t.Run("unknown-requires-original-material", func(t *testing.T) {
		r := newPreparationMountControl(t)
		p := r.p
		p.store.unknownAt = "capture"
		original := p.driver.Run(context.Background(), p.execution)
		if _, ok := UnknownAttempt(original); !ok {
			t.Fatal("capture did not preserve original Unknown")
		}
		p.driver.Stop()
		calls := len(r.order)
		s := p.driver.state
		s.mu.Lock()
		run := s.calls[p.execution]
		validUnknown := run != nil && run.claim != nil && run.unresolved == original && run.returned
		var claim preparationClaim
		if validUnknown {
			claim = *run.claim
		}
		s.mu.Unlock()
		if !validUnknown {
			t.Fatal("original Unknown lost its returned claim")
		}
		r.materialErr = fault(f.DependencyUnavailable)
		requireCode(t, p.driver.ResolveUnknown(context.Background(), p.execution), f.DependencyUnavailable)
		s.mu.Lock()
		retained := s.calls[p.execution]
		keptUnknown := retained == run && retained.claim != nil && *retained.claim == claim && retained.unresolved == original && retained.returned && !retained.resolving
		s.mu.Unlock()
		if !keptUnknown || p.driver.Joined() || len(r.order) != calls {
			t.Fatal("unavailable material changed original Unknown ownership or recaptured")
		}
		r.materialErr = nil
		if err := p.driver.ResolveUnknown(context.Background(), p.execution); err != nil || !p.driver.Joined() || len(r.order) != calls {
			t.Fatal("historical material recovery recaptured or failed to join", err)
		}
		s.mu.Lock()
		remaining := len(s.calls)
		s.mu.Unlock()
		if remaining != 0 {
			t.Fatal("verified material did not retire original call")
		}
	})
	t.Run("missing-required-verifier-rolls-back", func(t *testing.T) {
		r := newPreparationMountControl(t)
		p := r.p
		p.driver.state.mounts = captureWithoutMaterialVerifier{r}
		requireCode(t, p.driver.Run(context.Background(), p.execution), f.DependencyUnbound)
		if len(p.store.inputs) != 0 || p.store.providerRefs != 0 {
			t.Fatal("unverified capture escaped original rollback")
		}
	})
}
