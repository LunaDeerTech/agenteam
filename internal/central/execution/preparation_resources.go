package execution

import (
	"context"
	"encoding/json"
	"sync/atomic"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This context lives only for one synchronous provider discovery. Its public
// request contains identities; the original preparing call owns every proof.
type preparationDiscoveryKey struct{}
type preparationDiscovery struct {
	driver  *preparationState
	request c.PreparationRequest
	claim   preparationClaim
	kind    string
	active  atomic.Bool
}

func (s *preparationState) discoverResource(ctx context.Context, request c.PreparationRequest, claim preparationClaim, kind string, discover func(context.Context) error) error {
	d := &preparationDiscovery{driver: s, request: request.Clone(), claim: claim, kind: kind}
	d.active.Store(true)
	defer d.active.Store(false)
	if err := discover(context.WithValue(ctx, preparationDiscoveryKey{}, d)); err != nil {
		return err
	}
	return ctx.Err()
}

func preparationDiscoveryLocks(project i.ProjectID, agent i.AgentID, execution i.ExecutionID) []f.LockRequest {
	schedule, _ := f.ProjectScheduleLock(project.String())
	return []f.LockRequest{projectLock(project), agentLock(agent, f.Shared), executionLock(execution), {Key: schedule, Mode: f.Exclusive}}
}

// The digest distinguishes the complete original request, physical preparing
// ownership and Project observation. It is a plan comparison, never a grant.
func preparationResourceBinding(request c.PreparationRequest, claim preparationClaim, project pc.ProjectRef) (f.Digest, error) {
	digest, err := request.Launch.Digest()
	if err != nil {
		return "", err
	}
	command, err := request.Launch.Command()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Format    string
		Execution i.ExecutionID
		Launch    f.Digest
		RequestID string
		Command   string
		Attempt   string
		Process   string
		Fence     int64
		Project   pc.ProjectRef
	}{"execution-preparation-resource-v1", request.ExecutionID, digest, request.Launch.Meta.RequestID.String(), command.Canonical(), claim.attempt.String(), claim.process.String(), claim.fence, clonePreparationProject(project)})
	if err != nil {
		return "", unavailable(nil)
	}
	return c.TriggerInputDigest(raw), nil
}

func (a *Authority) requireResourceDiscovery(ctx context.Context, tx f.Tx, project i.ProjectID, agent i.AgentID, execution i.ExecutionID, kind string) (pc.ProjectRef, f.Digest, error) {
	if a == nil || a.state == nil {
		return pc.ProjectRef{}, "", fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil || agent.Validate() != nil || execution.Validate() != nil {
		return pc.ProjectRef{}, "", invalid()
	}
	d, ok := ctx.Value(preparationDiscoveryKey{}).(*preparationDiscovery)
	if !ok || d == nil || d.driver == nil || d.driver.authority != a || !d.active.Load() || d.kind != kind || d.request.ExecutionID != execution || d.request.Launch.ProjectID != project || d.request.Launch.AgentID != agent {
		return pc.ProjectRef{}, "", fault(f.Forbidden)
	}
	proof := &preparationWitness{owner: a.state, driver: d.driver, tx: tx, request: d.request.Clone(), claim: d.claim, locks: preparationDiscoveryLocks(project, agent, execution)}
	if err := proof.require(ctx, tx, d.request); err != nil {
		return pc.ProjectRef{}, "", err
	}
	view, err := d.driver.projects.RequirePreparingProjectInTx(ctx, tx, project)
	if err != nil {
		return pc.ProjectRef{}, "", portError(err)
	}
	if view.Validate() != nil || view.ID != project || view.Lifecycle != pc.Active {
		return pc.ProjectRef{}, "", fault(f.InvalidState)
	}
	if err = ctx.Err(); err != nil {
		return pc.ProjectRef{}, "", err
	}
	if !d.active.Load() {
		return pc.ProjectRef{}, "", fault(f.Forbidden)
	}
	binding, err := preparationResourceBinding(d.request, d.claim, view)
	if err != nil {
		return pc.ProjectRef{}, "", err
	}
	return clonePreparationProject(view), binding, nil
}

func (a *Authority) requireResourceCapture(ctx context.Context, tx f.Tx, project i.ProjectID, agent i.AgentID, execution i.ExecutionID) (*preparationWitness, f.Digest, error) {
	if a == nil || a.state == nil {
		return nil, "", fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil || agent.Validate() != nil || execution.Validate() != nil {
		return nil, "", invalid()
	}
	w, ok := ctx.Value(preparationWitnessKey{}).(*preparationWitness)
	if !ok || w == nil || w.owner != a.state || !w.resourcesOpen.Load() || !w.projectChecked || !w.sourceCaptured || !w.agentCaptured || w.input.Validate() != nil || w.agent.Validate() != nil || w.request.ExecutionID != execution || w.request.Launch.ProjectID != project || w.request.Launch.AgentID != agent {
		return nil, "", fault(f.Forbidden)
	}
	if err := w.require(ctx, tx, w.request); err != nil {
		return nil, "", err
	}
	binding, err := preparationResourceBinding(w.request, w.claim, w.project)
	if err != nil {
		return nil, "", err
	}
	return w, binding, nil
}

func (a *Authority) RequireSkillCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, request sc.SkillCaptureRequest) (sc.SkillCaptureScope, error) {
	project, binding, err := a.requireResourceDiscovery(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID, "skill")
	if err != nil {
		return sc.SkillCaptureScope{}, err
	}
	return sc.SkillCaptureScope{Project: project, AttemptBinding: binding}, nil
}

func (a *Authority) RequireSkillCaptureInTx(ctx context.Context, tx f.Tx, request sc.SkillCaptureRequest) (sc.SkillCaptureScope, error) {
	w, binding, err := a.requireResourceCapture(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID)
	if err != nil {
		return sc.SkillCaptureScope{}, err
	}
	return sc.SkillCaptureScope{Project: clonePreparationProject(w.project), AttemptBinding: binding}, nil
}

var _ sc.SkillCaptureAuthority = (*Authority)(nil)

func (a *Authority) RequireToolDiscoveryInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest) (tc.ExecutionToolDiscoveryFacts, error) {
	project, binding, err := a.requireResourceDiscovery(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID, "tool")
	if err != nil {
		return tc.ExecutionToolDiscoveryFacts{}, err
	}
	d := ctx.Value(preparationDiscoveryKey{}).(*preparationDiscovery)
	policy := d.request.Launch.Policy.Clone()
	return tc.ExecutionToolDiscoveryFacts{Project: project, AttemptBinding: binding, DeniedToolIDs: policy.DeniedToolIDs, ScopeConstraints: policy.AllowedResourceConstraints}, nil
}

func (a *Authority) RequireToolCaptureInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest) (tc.ExecutionToolCaptureFacts, error) {
	w, binding, err := a.requireResourceCapture(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID)
	if err != nil {
		return tc.ExecutionToolCaptureFacts{}, err
	}
	config, policy := w.agent.Fields(), w.request.Launch.Policy.Clone()
	return tc.ExecutionToolCaptureFacts{Project: clonePreparationProject(w.project), AttemptBinding: binding, AgentVersion: config.Core.Version, Selection: tc.ToolSnapshotSelection{AllowedToolIDs: config.AllowedToolIDs, DeniedToolIDs: policy.DeniedToolIDs, ScopeConstraints: policy.AllowedResourceConstraints}}, nil
}

var _ tc.ExecutionToolCaptureAuthority = (*Authority)(nil)
