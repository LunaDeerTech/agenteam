package projectvariable

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// CurrentProcessEnvironmentProvider is a metadata-only participant. Project
// SH protects the complete ordinary directory, including absence; Agent SH
// protects the full reverse-reference set. Corresponding writers hold EX.
type CurrentProcessEnvironmentProvider struct {
	store Store
	owner c.CurrentProcessEnvironmentAuthority
}

func NewCurrentProcessEnvironment(store Store, owner c.CurrentProcessEnvironmentAuthority) (*CurrentProcessEnvironmentProvider, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &CurrentProcessEnvironmentProvider{store: store, owner: owner}, nil
}
func (p *CurrentProcessEnvironmentProvider) RequireEmptyInTx(ctx context.Context, tx f.Tx, r c.CurrentProcessEnvironmentRequest) error {
	if err := environmentContext(ctx); err != nil {
		return err
	}
	if p == nil || nilPort(p.store) || nilPort(p.owner) {
		return fault(f.DependencyUnbound)
	}
	if r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	x, err := p.store.InTx(tx)
	if err != nil || nilPort(x) {
		return fault(f.Forbidden)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, r.RequiredLocks()); err != nil {
		return environmentError(err)
	}
	facts, err := p.owner.RequireCurrentProcessEnvironmentInTx(ctx, tx, r)
	if err != nil {
		return environmentError(err)
	}
	if facts.Validate() != nil {
		return fault(f.Forbidden)
	}
	facts = facts.Clone()
	// Never select ordinary values. Even a stored empty string is an entry and
	// prevents this explicitly env-free command profile from being dispatched.
	var ordinary []string
	if err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),'{}'::text[]) FROM (SELECT id FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='variable' AND deleted_at IS NULL ORDER BY id LIMIT 4097) AS command_ordinary`, r.ProjectID.String()).Scan(&ordinary); err != nil {
		return unavailable(err)
	}
	if len(ordinary) > c.MaxVariables {
		return fault(f.PayloadTooLarge)
	}
	names := map[string]bool{}
	for n, raw := range ordinary {
		if err = environmentContext(ctx); err != nil {
			return err
		}
		id, parseErr := f.ParseID[i.ProjectVariable](raw)
		if parseErr != nil || n > 0 && ordinary[n-1] >= raw {
			return internal(nil)
		}
		v, readErr := readCommandOrdinaryMetadata(ctx, x, r.ProjectID, id)
		if readErr != nil {
			return readErr
		}
		if names[v.Name] {
			return internal(nil)
		}
		names[v.Name] = true
	}
	// Do not filter by project: foreign-project rows owned by this Agent must
	// fail instead of disappearing from an apparently empty reference set.
	var projects, ids []string
	var versions []int64
	if err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(project_id::text ORDER BY project_id,variable_id),'{}'::text[]),COALESCE(array_agg(variable_id::text ORDER BY project_id,variable_id),'{}'::text[]),COALESCE(array_agg(owner_version ORDER BY project_id,variable_id),'{}'::bigint[]) FROM (SELECT project_id,variable_id,owner_version FROM agenteam_projectvariable.secret_references WHERE owner_kind='agent' AND owner_id=$1 ORDER BY project_id,variable_id LIMIT 257) AS command_owned`, r.AgentID.String()).Scan(&projects, &ids, &versions); err != nil {
		return unavailable(err)
	}
	if len(ids) > c.MaxSecretReferences || len(projects) != len(ids) || len(versions) != len(ids) || len(ids) != len(facts.AllowedSecretVariableIDs) {
		return fault(f.ConfirmationStale)
	}
	for n, raw := range ids {
		if err = environmentContext(ctx); err != nil {
			return err
		}
		if projects[n] != r.ProjectID.String() || raw != facts.AllowedSecretVariableIDs[n].String() || versions[n] != int64(facts.AgentVersion) {
			return fault(f.ConfirmationStale)
		}
		row, readErr := loadSecretVariable(ctx, x, r.ProjectID, facts.AllowedSecretVariableIDs[n], false)
		if readErr != nil {
			return environmentError(readErr)
		}
		fields := row.Variable.Fields()
		if fields.ID != facts.AllowedSecretVariableIDs[n] || fields.ProjectID != r.ProjectID || names[fields.Name] {
			return internal(nil)
		}
		names[fields.Name] = true
	}
	if err = environmentContext(ctx); err != nil {
		return err
	}
	// Verify the same still-active original witness after all synchronous SQL.
	after, err := p.owner.RequireCurrentProcessEnvironmentInTx(ctx, tx, r)
	if err != nil {
		return environmentError(err)
	}
	if after.Validate() != nil || !sameCurrentEnvironmentFacts(facts, after) {
		return fault(f.Forbidden)
	}
	if len(ordinary) != 0 || len(ids) != 0 {
		return fault(f.CapabilityUnsupported)
	}
	return environmentContext(ctx)
}
func sameCurrentEnvironmentFacts(a, b c.CurrentProcessEnvironmentFacts) bool {
	if a.AgentVersion != b.AgentVersion || len(a.AllowedSecretVariableIDs) != len(b.AllowedSecretVariableIDs) {
		return false
	}
	for n, id := range a.AllowedSecretVariableIDs {
		if id != b.AllowedSecretVariableIDs[n] {
			return false
		}
	}
	return true
}
func readCommandOrdinaryMetadata(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, id i.ProjectVariableID) (c.VariableSummaryFields, error) {
	var rawID, project, typ, name, description string
	var version int64
	var created, updated time.Time
	var deleted *time.Time
	var credential *string
	var credentialVersion *int64
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,type,name,description,version,created_at,updated_at,deleted_at,credential_id::text,credential_version FROM agenteam_projectvariable.variables WHERE project_id=$1 AND id=$2 AND type='variable'`, p.String(), id.String()).Scan(&rawID, &project, &typ, &name, &description, &version, &created, &updated, &deleted, &credential, &credentialVersion)
	if err != nil {
		return c.VariableSummaryFields{}, unavailable(err)
	}
	if rawID != id.String() || project != p.String() || deleted != nil || credential != nil || credentialVersion != nil {
		return c.VariableSummaryFields{}, internal(nil)
	}
	first, err := f.NewInstant(created)
	if err != nil {
		return c.VariableSummaryFields{}, internal(nil)
	}
	last, err := f.NewInstant(updated)
	if err != nil {
		return c.VariableSummaryFields{}, internal(nil)
	}
	value, err := c.NewVariableSummary(c.VariableSummaryFields{ID: id, ProjectID: p, Type: typ, Name: name, Description: description, Version: f.Version(version), CreatedAt: first, UpdatedAt: last})
	if err != nil {
		return c.VariableSummaryFields{}, internal(nil)
	}
	return value.Fields(), nil
}
func (CurrentProcessEnvironmentProvider) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "current_process_environment_provider")
}
func (CurrentProcessEnvironmentProvider) LogValue() slog.Value {
	return slog.StringValue("current_process_environment_provider")
}
func (CurrentProcessEnvironmentProvider) MarshalJSON() ([]byte, error) {
	return []byte(`"current_process_environment_provider"`), nil
}

var _ c.CurrentProcessEnvironment = (*CurrentProcessEnvironmentProvider)(nil)
