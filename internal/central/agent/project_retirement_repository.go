package agent

import (
	"context"
	"encoding/json"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func loadRetirementCommand(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, id c.AgentCommandID) (*commandRecord, error) {
	var name, key string
	err := x.QueryRow(ctx, `SELECT command_name,idempotency_key FROM agenteam_agent.commands WHERE project_id=$1 AND id=$2 AND state='completed'`, p.String(), id.String()).Scan(&name, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fault(f.InvalidState)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	record, err := loadCommand(ctx, x, p, c.CommandName(name), f.IdempotencyKey(key))
	if err != nil {
		return nil, err
	}
	if record == nil || record.ID != id {
		return nil, unavailable(nil)
	}
	if _, err = commandReceipt(record); err != nil {
		return nil, err
	}
	return record, nil
}
func checkRetirementCommand(ctx context.Context, x postgres.SQLExecutor, plan c.ProjectRetirementPlan) error {
	record, err := loadRetirementCommand(ctx, x, plan.Change.Request.ProjectID, plan.CommandID)
	if err != nil {
		return err
	}
	if record.Target != plan.Change.Request.AgentID {
		return fault(f.VersionConflict)
	}
	if err = validateRetirementCommandBefore(record.After, plan); err != nil {
		return err
	}
	return portError(ctx.Err())
}

func validateRetirementCommandBefore(after c.AgentConfig, plan c.ProjectRetirementPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	before, err := retirementBeforeFromCommand(after, plan.Change.Request.ExpectedVersion, plan.BeforeUpdatedAt)
	if err != nil {
		return err
	}
	if !sameValue(before, plan.Change.Before) {
		return fault(f.VersionConflict)
	}
	return nil
}

// Assignment mutations change only these two canonical scalars. The immutable
// command remains unchanged; the caller's actual complete Before and digest
// must still match this reconstruction before any retirement can be applied.
func retirementBeforeFromCommand(after c.AgentConfig, version f.Version, updatedAt *f.Instant) (c.AgentConfig, error) {
	if after.Validate() != nil || version.Validate() != nil {
		return c.AgentConfig{}, invalid()
	}
	fields := after.Fields()
	if updatedAt == nil {
		if version != fields.Core.Version {
			return c.AgentConfig{}, fault(f.VersionConflict)
		}
		return after.Clone(), nil
	}
	if updatedAt.Validate() != nil || version <= fields.Core.Version || !updatedAt.Time().After(fields.Core.UpdatedAt.Time()) {
		return c.AgentConfig{}, fault(f.VersionConflict)
	}
	fields.Core.Version, fields.Core.UpdatedAt = version, *updatedAt
	return c.NewAgentConfig(fields)
}
func (r *ProjectRetirement) DiscoverProjectRetirement(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, p i.ProjectID) (*c.ProjectRetirementPlan, error) {
	var plan *c.ProjectRetirementPlan
	err := r.read(ctx, actor, cause, p, func(ctx context.Context, x postgres.SQLExecutor) error {
		var raw string
		err := x.QueryRow(ctx, `SELECT id::text FROM agenteam_agent.agents WHERE project_id=$1 ORDER BY id LIMIT 1`, p.String()).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return unavailable(err)
		}
		id, err := f.ParseID[i.Agent](raw)
		if err != nil {
			return unavailable(err)
		}
		before, err := initializedAgentState(ctx, x, p, id, true)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(before)
		if err != nil {
			return unavailable(err)
		}
		// Prefer an exact immutable postimage. Otherwise require every field
		// except version/time to match, and retain both actual current scalars.
		// The complete typed command is independently reloaded below.
		core := before.Fields().Core
		var exact bool
		err = x.QueryRow(ctx, `SELECT id::text,after_config=$3::jsonb FROM agenteam_agent.commands
 WHERE project_id=$1 AND target_id=$2 AND state='completed'
 AND (after_config=$3::jsonb OR (
  after_config-'version'-'updated_at'=$3::jsonb-'version'-'updated_at'
  AND (after_config->>'version')::bigint<$4 AND (after_config->>'updated_at')::timestamptz<$5))
 ORDER BY (after_config=$3::jsonb) DESC,(after_config->>'version')::bigint DESC,id DESC LIMIT 1`, p.String(), id.String(), encoded, int64(core.Version), core.UpdatedAt.Time()).Scan(&raw, &exact)
		if errors.Is(err, pgx.ErrNoRows) {
			return fault(f.ResourceBusy)
		}
		if err != nil {
			return unavailable(err)
		}
		command, err := f.ParseID[c.AgentCommand](raw)
		if err != nil {
			return unavailable(err)
		}
		digest, err := c.ProjectRetirementBeforeDigest(before)
		if err != nil {
			return err
		}
		value := c.ProjectRetirementPlan{Change: c.ProjectRetirementChange{Request: c.ProjectRetirementRequest{Actor: actor, ProjectID: p, AgentID: id, Cause: cause, ExpectedVersion: before.Fields().Core.Version}, Before: before}, CommandID: command, BeforeDigest: digest}
		if !exact {
			at := core.UpdatedAt
			value.BeforeUpdatedAt = &at
		}
		if err = value.Validate(); err != nil {
			return err
		}
		if err = checkRetirementCommand(ctx, x, value); err != nil {
			return err
		}
		plan = &value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}
func (r *ProjectRetirement) RestoreProjectRetirement(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, p i.ProjectID, a i.AgentID, version f.Version, command c.AgentCommandID, digest f.Digest, updatedAt *f.Instant) (c.ProjectRetirementPlan, error) {
	var plan c.ProjectRetirementPlan
	if a.Validate() != nil || version.Validate() != nil || command.Validate() != nil || digest.Validate() != nil {
		return plan, invalid()
	}
	if updatedAt != nil {
		at := *updatedAt
		if at.Validate() != nil {
			return plan, invalid()
		}
		updatedAt = &at
	}
	err := r.read(ctx, actor, cause, p, func(ctx context.Context, x postgres.SQLExecutor) error {
		record, err := loadRetirementCommand(ctx, x, p, command)
		if err != nil {
			return err
		}
		before, err := retirementBeforeFromCommand(record.After, version, updatedAt)
		if err != nil {
			return err
		}
		plan = c.ProjectRetirementPlan{Change: c.ProjectRetirementChange{Request: c.ProjectRetirementRequest{Actor: actor, ProjectID: p, AgentID: a, Cause: cause, ExpectedVersion: version}, Before: before}, CommandID: command, BeforeDigest: digest, BeforeUpdatedAt: updatedAt}
		if record.Target != a {
			return fault(f.Forbidden)
		}
		return validateRetirementCommandBefore(record.After, plan)
	})
	if err != nil {
		return c.ProjectRetirementPlan{}, err
	}
	return plan, nil
}
func (r *ProjectRetirement) RequireProjectAgentsEmptyInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, p i.ProjectID) error {
	if !r.bound() {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return invalid()
	}
	if err := r.projects.RequireAgentRetirementScopeInTx(ctx, tx, actor, cause, p); err != nil {
		return portError(err)
	}
	x, err := r.agents.state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	var exists bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_agent.agents WHERE project_id=$1)`, p.String()).Scan(&exists); err != nil {
		return unavailable(err)
	}
	if exists {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func requireRetirementCanonicalAbsent(ctx context.Context, x postgres.SQLExecutor, change c.ProjectRetirementChange) error {
	var exists bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_agent.agents WHERE id=$1) OR EXISTS(SELECT 1 FROM agenteam_agent.tool_allowlist WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_agent.mount_allowlist WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_agent.secret_allowlist WHERE agent_id=$1)`, change.Request.AgentID.String()).Scan(&exists)
	if err != nil {
		return unavailable(err)
	}
	if exists {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func deleteRetirementCanonical(ctx context.Context, x postgres.SQLExecutor, change c.ProjectRetirementChange) error {
	before := change.Before.Fields()
	for _, entry := range []struct {
		table string
		count int
	}{{"tool_allowlist", len(before.AllowedToolIDs)}, {"mount_allowlist", len(before.AllowedMountIDs)}, {"secret_allowlist", len(before.AllowedSecretVariableIDs)}} {
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_agent.`+entry.table+` WHERE project_id=$1 AND agent_id=$2`, change.Request.ProjectID.String(), change.Request.AgentID.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != int64(entry.count) {
			return fault(f.VersionConflict)
		}
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_agent.agents WHERE project_id=$1 AND id=$2 AND version=$3 AND lifecycle='active'`, change.Request.ProjectID.String(), change.Request.AgentID.String(), int64(change.Request.ExpectedVersion))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return requireRetirementCanonicalAbsent(ctx, x, change)
}
