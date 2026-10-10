package execution

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func (a *Authority) RequireEnvironmentDiscoveryInTx(ctx context.Context, tx f.Tx, request pvc.EnvironmentCaptureRequest) (pvc.EnvironmentDiscoveryFacts, error) {
	project, binding, err := a.requireResourceDiscovery(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID, "environment")
	if err != nil {
		return pvc.EnvironmentDiscoveryFacts{}, err
	}
	d := ctx.Value(preparationDiscoveryKey{}).(*preparationDiscovery)
	return pvc.EnvironmentDiscoveryFacts{Project: project, AttemptBinding: binding, ScopeConstraints: d.request.Launch.Policy.Clone().AllowedResourceConstraints}, nil
}

func (a *Authority) RequireEnvironmentCaptureInTx(ctx context.Context, tx f.Tx, request pvc.EnvironmentCaptureRequest) (pvc.EnvironmentCaptureFacts, error) {
	w, binding, err := a.requireResourceCapture(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID)
	if err != nil {
		return pvc.EnvironmentCaptureFacts{}, err
	}
	config := w.agent.Fields()
	return pvc.EnvironmentCaptureFacts{EnvironmentDiscoveryFacts: pvc.EnvironmentDiscoveryFacts{Project: clonePreparationProject(w.project), AttemptBinding: binding, ScopeConstraints: w.request.Launch.Policy.Clone().AllowedResourceConstraints}, AgentVersion: config.Core.Version, AllowedSecretVariableIDs: config.AllowedSecretVariableIDs}, nil
}

var _ pvc.ExecutionEnvironmentAuthority = (*Authority)(nil)
