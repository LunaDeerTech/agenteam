package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type environmentRows struct {
	variables []c.Variable
	index     []secretReferenceIndexEntry
	secrets   []*secretVariableRow
	mapping   f.Digest
}

// Project SH protects the complete live directory and the absent-name range;
// Agent SH protects the complete owned reference set. No outside Agent SQL is
// read. In particular, an empty index is never treated as proof of an Agent.
func readEnvironment(ctx context.Context, x postgres.SQLExecutor, r c.EnvironmentCaptureRequest) (environmentRows, error) {
	empty := environmentRows{}
	var ids []string
	if err := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY name COLLATE "C",id),'{}'::text[]) FROM (SELECT id,name FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='variable' AND deleted_at IS NULL ORDER BY name COLLATE "C",id LIMIT 4097) AS directory`, r.ProjectID.String()).Scan(&ids); err != nil {
		return empty, unavailable(err)
	}
	if len(ids) > c.MaxVariables {
		return empty, fault(f.PayloadTooLarge)
	}
	v := environmentRows{variables: make([]c.Variable, 0, len(ids)), secrets: make([]*secretVariableRow, 0)}
	bytes := 0
	for _, raw := range ids {
		if err := environmentContext(ctx); err != nil {
			return empty, err
		}
		id, err := f.ParseID[i.ProjectVariable](raw)
		if err != nil {
			return empty, internal(nil)
		}
		variable, _, err := loadVariable(ctx, x, r.ProjectID, id, false)
		if err != nil {
			return empty, environmentError(err)
		}
		body, err := json.Marshal(variable)
		if err != nil {
			return empty, internal(nil)
		}
		bytes += len(body)
		if bytes > c.MaxExecutionEnvironmentBytes {
			return empty, fault(f.PayloadTooLarge)
		}
		v.variables = append(v.variables, variable)
	}
	var err error
	v.index, err = readSecretReferenceIndex(ctx, x, c.SecretReferenceChange{ProjectID: r.ProjectID, AgentID: r.AgentID})
	if err != nil {
		return empty, environmentError(err)
	}
	for n, entry := range v.index {
		if n > 0 && entry.Version != v.index[0].Version {
			return empty, internal(nil)
		}
		row, err := loadSecretVariable(ctx, x, r.ProjectID, entry.ID, false)
		if err != nil {
			return empty, environmentError(err)
		}
		v.secrets = append(v.secrets, row)
	}
	type secretMapping struct {
		Variable   c.SecretVariableFields
		Credential string
		Version    f.Version
	}
	secrets := make([]secretMapping, len(v.secrets))
	for n, row := range v.secrets {
		secrets[n] = secretMapping{row.Variable.Fields(), row.Ref.Details().ID.String(), row.CredentialVersion}
	}
	body, err := canonical(struct {
		Request   c.EnvironmentCaptureRequest
		Variables []c.Variable
		Index     []secretReferenceIndexEntry
		Secrets   []secretMapping
	}{r, v.variables, v.index, secrets})
	if err != nil {
		return empty, err
	}
	if len(body) > c.MaxExecutionEnvironmentBytes {
		return empty, fault(f.PayloadTooLarge)
	}
	v.mapping = digest(body)
	return v, environmentContext(ctx)
}
func environmentRequests(r c.EnvironmentCaptureRequest, facts c.EnvironmentDiscoveryFacts, rows environmentRows) []sc.ProjectVariableLeaseRequest {
	requests := make([]sc.ProjectVariableLeaseRequest, len(rows.index))
	for n, entry := range rows.index {
		row := rows.secrets[n]
		requests[n] = sc.ProjectVariableLeaseRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, ExecutionID: r.ExecutionID, VariableID: entry.ID, VariableVersion: row.Variable.Fields().Version, AgentVersion: entry.Version, Ref: row.Ref, CredentialVersion: row.CredentialVersion, AttemptBinding: facts.AttemptBinding}
	}
	return requests
}
func environmentIDs(rows environmentRows) []i.ProjectVariableID {
	ids := make([]i.ProjectVariableID, len(rows.index))
	for n, entry := range rows.index {
		ids[n] = entry.ID
	}
	return ids
}

func persistEnvironment(ctx context.Context, x postgres.SQLExecutor, value c.EnvironmentCapture, rows environmentRows) error {
	v := value.Fields()
	type ref struct {
		Variable          string
		Version           f.Version
		Credential, Lease string
	}
	refs := make([]ref, len(v.Secrets))
	for n, s := range v.Secrets {
		refs[n] = ref{s.Variable.Fields().ID.String(), s.Variable.Fields().Version, s.CredentialRef.Details().ID.String(), s.LeaseID.String()}
	}
	body, err := canonical(struct {
		Request c.EnvironmentCaptureRequest
		Attempt f.Digest
		Version f.Version
		Mapping f.Digest
		Refs    []ref
	}{v.Request, v.AttemptBinding, v.AgentVersion, rows.mapping, refs})
	if err != nil {
		return err
	}
	mapping := digest(body)
	var project, agent, attempt, stored string
	var version int64
	var ordinary, secrets int
	err = x.QueryRow(ctx, `SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,ordinary_count,secret_count FROM agenteam_projectvariable.execution_environments WHERE execution_id=$1`, v.Request.ExecutionID.String()).Scan(&project, &agent, &version, &attempt, &stored, &ordinary, &secrets)
	if err == nil {
		if project != v.Request.ProjectID.String() || agent != v.Request.AgentID.String() || version != int64(v.AgentVersion) || attempt != string(v.AttemptBinding) || stored != string(mapping) || ordinary != len(v.Variables) || secrets != len(v.Secrets) {
			return fault(f.VersionConflict)
		}
		// The DB deferred constraints tie every reference to its original lease;
		// still verify the complete set on a same-capture replay.
		var variables, leases []string
		if err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(variable_id::text ORDER BY variable_id),'{}'::text[]),COALESCE(array_agg(lease_id::text ORDER BY variable_id),'{}'::text[]) FROM agenteam_projectvariable.execution_secret_references WHERE execution_id=$1`, v.Request.ExecutionID.String()).Scan(&variables, &leases); err != nil {
			return unavailable(err)
		}
		wantVariables, wantLeases := make([]string, len(refs)), make([]string, len(refs))
		for n, r := range refs {
			wantVariables[n], wantLeases[n] = r.Variable, r.Lease
		}
		if !slices.Equal(variables, wantVariables) || !slices.Equal(leases, wantLeases) {
			return internal(nil)
		}
		return environmentContext(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return unavailable(err)
	}
	if err = affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.execution_environments(execution_id,project_id,agent_id,agent_version,attempt_binding,capture_digest,ordinary_count,secret_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, v.Request.ExecutionID.String(), v.Request.ProjectID.String(), v.Request.AgentID.String(), int64(v.AgentVersion), string(v.AttemptBinding), string(mapping), len(v.Variables), len(v.Secrets))); err != nil {
		return err
	}
	for _, s := range v.Secrets {
		if err = affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.execution_secret_references(execution_id,variable_id,lease_id) VALUES($1,$2,$3)`, v.Request.ExecutionID.String(), s.Variable.Fields().ID.String(), s.LeaseID.String())); err != nil {
			return err
		}
	}
	return environmentContext(ctx)
}
