package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type referencePlanData struct {
	issuer  *configurationState
	change  ac.ToolReferenceChange
	owner   ac.ToolReferenceOwnerPlan
	locks   []f.LockRequest
	entries []currentTool
}
type referencePlan struct{ data func() referencePlanData }

func (p referencePlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (referencePlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "tool_reference_plan") }
func (referencePlan) MarshalJSON() ([]byte, error) { return []byte(`"tool_reference_plan"`), nil }
func (referencePlan) LogValue() slog.Value         { return slog.StringValue("tool_reference_plan") }
func sameChange(a, b ac.ToolReferenceChange) bool {
	return a.Actor.Details() == b.Actor.Details() && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.Command.Canonical() == b.Command.Canonical() && a.PlanRevision == b.PlanRevision && a.ResultOwnerVersion == b.ResultOwnerVersion && (a.ExpectedOwnerVersion == nil && b.ExpectedOwnerVersion == nil || a.ExpectedOwnerVersion != nil && b.ExpectedOwnerVersion != nil && *a.ExpectedOwnerVersion == *b.ExpectedOwnerVersion) && slices.Equal(a.Before, b.Before) && slices.Equal(a.After, b.After)
}
func sortedTools(in []id.ToolID) []id.ToolID {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
	return out
}
func (c *Configuration) DiscoverToolReferences(ctx context.Context, change ac.ToolReferenceChange) (ac.ToolReferencePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	if err := change.Validate(); err != nil {
		return nil, err
	}
	s := c.state()
	if s == nil || nilPort(s.owner) {
		return nil, fail(f.DependencyUnbound)
	}
	change = change.Clone()
	owner, err := s.owner.DiscoverToolReferenceOwner(ctx, change.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(owner) {
		return nil, fail(f.DependencyUnbound)
	}
	locks := append(baseLocks(change.Actor, change.ProjectID, change.AgentID, f.Exclusive), owner.RequiredLocks()...)
	command, err := f.CommandLock(change.Command)
	if err != nil {
		return nil, fail(f.InvalidArgument)
	}
	user, _ := f.UserLock(change.Actor.Details().UserID)
	locks = append(locks, f.LockRequest{Key: command, Mode: f.Exclusive}, f.LockRequest{Key: user, Mode: f.Exclusive})
	for _, tool := range change.After {
		locks = append(locks, toolLock(tool, f.Shared))
	}
	locks, err = normalizedLocks(locks)
	if err != nil {
		return nil, err
	}
	readLocks, err := normalizedLocks(baseLocks(change.Actor, change.ProjectID, change.AgentID, f.Shared))
	if err != nil {
		return nil, err
	}
	var entries []currentTool
	err = s.registry.read(ctx, readLocks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := s.registry.currentProject(ctx, tx, change.Actor, change.ProjectID); err != nil {
			return err
		}
		for _, tool := range sortedTools(change.After) {
			v, err := s.registry.currentTool(ctx, tx, x, tool)
			if err != nil {
				return err
			}
			entries = append(entries, v)
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	d := referencePlanData{issuer: s, change: change, owner: owner, locks: locks, entries: entries}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	return referencePlan{data: func() referencePlanData { return d }}, nil
}

// ApplyToolReferencesInTx is called only after Agent wrote the canonical
// postimage and installed its private witness in this same callback context.
// All locks come from discovery and are verified, never acquired here. The
// current directory and original reference preimage are rechecked; an applied
// DTO, valid UUID or visible new Agent row cannot replace the owner witness.
func (c *Configuration) ApplyToolReferencesInTx(ctx context.Context, tx f.Tx, change ac.ToolReferenceChange, plan ac.ToolReferencePlan) error {
	if err := change.Validate(); err != nil {
		return err
	}
	s := c.state()
	if s == nil || nilPort(s.owner) {
		return fail(f.DependencyUnbound)
	}
	p, ok := plan.(referencePlan)
	if !ok || p.data == nil {
		return fail(f.InvalidArgument)
	}
	d := p.data()
	if d.issuer != s || !sameChange(d.change, change) {
		return fail(f.InvalidArgument)
	}
	x, err := s.registry.executor(ctx, tx, d.locks)
	if err != nil {
		return err
	}
	if err = s.registry.currentProject(ctx, tx, change.Actor, change.ProjectID); err != nil {
		return err
	}
	// Checking the owner before this domain's first reference SQL also covers
	// explicit empty collections and a newly-created, not-yet-published Agent.
	if err = s.owner.CheckToolReferenceOwnerAppliedInTx(ctx, tx, change.Clone(), d.owner); err != nil {
		return portError(err)
	}
	for _, old := range d.entries {
		now, err := s.registry.currentTool(ctx, tx, x, old.Ref.ToolID)
		if err != nil {
			return err
		}
		if !sameCurrent(old, now) {
			return fail(f.VersionConflict)
		}
	}
	version, before, exists, err := loadAgentReferences(ctx, x, change.ProjectID, change.AgentID)
	if err != nil {
		return err
	}
	if change.ExpectedOwnerVersion == nil {
		if exists {
			return fail(f.VersionConflict)
		}
	} else if !exists || version != *change.ExpectedOwnerVersion || !slices.Equal(before, sortedTools(change.Before)) {
		return fail(f.VersionConflict)
	}
	if exists && version == change.ResultOwnerVersion {
		if !slices.Equal(before, sortedTools(change.After)) {
			return fail(f.VersionConflict)
		}
		return portError(ctx.Err())
	}
	if !exists {
		_, err = x.Exec(ctx, `INSERT INTO agenteam_tool.agent_configurations(agent_id,project_id,config_version) VALUES($1,$2,$3)`, change.AgentID.String(), change.ProjectID.String(), int64(change.ResultOwnerVersion))
	} else {
		_, err = x.Exec(ctx, `UPDATE agenteam_tool.agent_configurations SET config_version=$3 WHERE agent_id=$1 AND project_id=$2`, change.AgentID.String(), change.ProjectID.String(), int64(change.ResultOwnerVersion))
	}
	if err != nil {
		return portError(err)
	}
	if _, err = x.Exec(ctx, `DELETE FROM agenteam_tool.agent_references WHERE agent_id=$1`, change.AgentID.String()); err != nil {
		return portError(err)
	}
	for _, tool := range sortedTools(change.After) {
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_tool.agent_references(agent_id,tool_id) VALUES($1,$2)`, change.AgentID.String(), tool.String()); err != nil {
			return portError(err)
		}
	}
	return portError(ctx.Err())
}
func loadAgentReferences(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, agent id.AgentID) (f.Version, []id.ToolID, bool, error) {
	var ownerProject string
	var version f.Version
	err := x.QueryRow(ctx, `SELECT project_id,config_version FROM agenteam_tool.agent_configurations WHERE agent_id=$1`, agent.String()).Scan(&ownerProject, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, portError(err)
	}
	if ownerProject != project.String() || version.Validate() != nil {
		return 0, nil, false, fail(f.InvalidState)
	}
	rows, err := x.Query(ctx, `SELECT tool_id FROM agenteam_tool.agent_references WHERE agent_id=$1 ORDER BY tool_id LIMIT 129`, agent.String())
	if err != nil {
		return 0, nil, false, portError(err)
	}
	defer rows.Close()
	values := make([]id.ToolID, 0)
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return 0, nil, false, portError(err)
		}
		tool, err := f.ParseID[id.Tool](raw)
		if err != nil {
			return 0, nil, false, fail(f.InvalidState)
		}
		values = append(values, tool)
		if len(values) > 128 {
			return 0, nil, false, fail(f.InvalidState)
		}
	}
	if err = rows.Err(); err != nil {
		return 0, nil, false, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return 0, nil, false, portError(err)
	}
	return version, values, true, nil
}

var _ ac.ToolConfigurationDirectory = (*Configuration)(nil)
var _ ac.AgentToolReferences = (*Configuration)(nil)
