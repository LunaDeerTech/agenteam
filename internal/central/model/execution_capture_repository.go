package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type executionModelSource struct {
	Version   f.Version
	Selection mc.AgentModelSelection
}

func (s executionModelSource) equal(other executionModelSource) bool {
	return s.Version == other.Version && s.Selection.Equal(other.Selection)
}

// Read the whole Model-owned Agent index. Filtering by Project or primary
// role would hide inconsistent/foreign references. This is not Agent authority.
func readExecutionModelSource(ctx context.Context, x postgres.SQLExecutor, r mc.ExecutionModelCaptureRequest) (executionModelSource, error) {
	var zero executionModelSource
	var roles, projects, models, efforts []string
	var versions []int64
	err := x.QueryRow(ctx, `SELECT array_agg(role ORDER BY role),array_agg(project_id::text ORDER BY role),array_agg(model_id::text ORDER BY role),array_agg(owner_version ORDER BY role),array_agg(reasoning_effort ORDER BY role) FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1`, r.AgentID.String()).Scan(&roles, &projects, &models, &versions, &efforts)
	if err != nil {
		return zero, unavailable(err)
	}
	if len(roles) == 0 {
		return zero, fault(f.DependencyUnbound)
	}
	if len(roles) > 2 || len(projects) != len(roles) || len(models) != len(roles) || len(versions) != len(roles) || len(efforts) != len(roles) {
		return zero, unavailable(nil)
	}
	out := executionModelSource{Version: f.Version(versions[0])}
	for n, role := range roles {
		if projects[n] != r.ProjectID.String() || f.Version(versions[n]) != out.Version || n > 0 && roles[n-1] >= role {
			return zero, unavailable(nil)
		}
		model, err := f.ParseID[mc.Model](models[n])
		if err != nil {
			return zero, unavailable(err)
		}
		switch role {
		case "agent_model":
			out.Selection.ModelID = model
			if efforts[n] != "" {
				effort := efforts[n]
				out.Selection.ReasoningEffort = &effort
			}
		case "approval_model":
			if efforts[n] != "" {
				return zero, unavailable(nil)
			}
			out.Selection.ApprovalModelID = &model
		default:
			return zero, unavailable(nil)
		}
	}
	if out.Version.Validate() != nil || out.Selection.Validate() != nil {
		return zero, unavailable(nil)
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return out, nil
}
func executionModelResolveRequest(r mc.ExecutionModelCaptureRequest, source executionModelSource) (mc.ResolveRequest, error) {
	var zero mc.ResolveRequest
	actor, err := id.NewAgentRun(r.ProjectID, r.AgentID, r.ExecutionID)
	if err != nil {
		return zero, portError(err)
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, r.ExecutionID.String())
	if err != nil {
		return zero, portError(err)
	}
	model := source.Selection.ModelID
	out := mc.ResolveRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.AgentConsumer, ProjectID: r.ProjectID, AgentID: &r.AgentID, ExecutionID: &r.ExecutionID, Purpose: mc.AgentGeneration}, Purpose: mc.AgentGeneration, Source: mc.CurrentSelectionSource, ModelRef: &model, Selection: &mc.SelectionRef{Kind: "direct"}, LeaseOwner: owner}
	if source.Selection.ReasoningEffort != nil {
		out.ReasoningEffort = *source.Selection.ReasoningEffort
	}
	if out.Validate() != nil {
		return zero, unavailable(nil)
	}
	return out, nil
}
