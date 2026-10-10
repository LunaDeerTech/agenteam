package execution

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mountc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
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
