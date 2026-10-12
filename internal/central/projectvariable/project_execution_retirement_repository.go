package projectvariable

import (
	"context"
	"errors"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// The capture digest is frozen as originally persisted. It includes capture-time
// mappings; recomputing it from today's variable values would change the facts.
func readProjectExecutionEnvironment(ctx context.Context, x postgres.SQLExecutor, r ec.ProjectExecutionRetirementRequest) (sc.ProjectExecutionLeaseRetirementRequest, error) {
	b := r.Binding
	v := sc.ProjectExecutionLeaseRetirementRequest{ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID}
	var project, agent, attempt, capture string
	var version int64
	err := x.QueryRow(ctx, `SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,ordinary_count,secret_count FROM agenteam_projectvariable.execution_environments WHERE execution_id=$1`, b.ExecutionID.String()).Scan(&project, &agent, &version, &attempt, &capture, &v.OrdinaryCount, &v.SecretCount)
	if canceled := environmentContext(ctx); canceled != nil {
		return v, canceled
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return v, unavailable(err)
	}
	if err == nil {
		if project != b.ProjectID.String() || agent != b.AgentID.String() {
			return v, fault(f.VersionConflict)
		}
		v.EnvironmentPresent, v.AgentVersion, v.AttemptBinding, v.CaptureDigest = true, f.Version(version), f.Digest(attempt), f.Digest(capture)
	}
	var variables, leases []string
	err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(variable_id::text ORDER BY variable_id),'{}'::text[]),COALESCE(array_agg(lease_id::text ORDER BY variable_id),'{}'::text[]) FROM (SELECT variable_id,lease_id FROM agenteam_projectvariable.execution_secret_references WHERE execution_id=$1 ORDER BY variable_id LIMIT 257) refs`, b.ExecutionID.String()).Scan(&variables, &leases)
	if canceled := environmentContext(ctx); canceled != nil {
		return v, canceled
	}
	if err != nil {
		return v, unavailable(err)
	}
	if len(variables) != len(leases) || len(variables) > 256 {
		return v, fault(f.VersionConflict)
	}
	v.References = make([]sc.ProjectExecutionLeaseReference, len(variables))
	for n, raw := range variables {
		variable, ve := f.ParseID[i.ProjectVariable](raw)
		lease, le := f.ParseID[sc.Lease](leases[n])
		if ve != nil || le != nil || variable.String() != raw || lease.String() != leases[n] {
			return v, fault(f.VersionConflict)
		}
		v.References[n] = sc.ProjectExecutionLeaseReference{VariableID: variable, LeaseID: lease}
	}
	if v.Validate() != nil {
		return v, fault(f.VersionConflict)
	}
	return v, nil
}

func deleteProjectExecutionReferences(ctx context.Context, x postgres.SQLExecutor, r sc.ProjectExecutionLeaseRetirementRequest) error {
	for _, ref := range r.References {
		if err := environmentContext(ctx); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_projectvariable.execution_secret_references WHERE execution_id=$1 AND variable_id=$2 AND lease_id=$3`, r.ExecutionID.String(), ref.VariableID.String(), ref.LeaseID.String())
		if canceled := environmentContext(ctx); canceled != nil {
			return canceled
		}
		if err != nil || tag.RowsAffected() != 1 {
			return unavailable(err)
		}
	}
	return nil
}
func deleteProjectExecutionEnvironment(ctx context.Context, x postgres.SQLExecutor, r sc.ProjectExecutionLeaseRetirementRequest) error {
	if !r.EnvironmentPresent {
		return environmentContext(ctx)
	}
	if err := environmentContext(ctx); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_projectvariable.execution_environments WHERE execution_id=$1 AND project_id=$2 AND agent_id=$3 AND agent_version=$4 AND attempt_binding=$5 AND capture_digest=$6 AND ordinary_count=$7 AND secret_count=$8`, r.ExecutionID.String(), r.ProjectID.String(), r.AgentID.String(), int64(r.AgentVersion), string(r.AttemptBinding), string(r.CaptureDigest), r.OrdinaryCount, r.SecretCount)
	if canceled := environmentContext(ctx); canceled != nil {
		return canceled
	}
	if err != nil || tag.RowsAffected() != 1 {
		return unavailable(err)
	}
	return nil
}
