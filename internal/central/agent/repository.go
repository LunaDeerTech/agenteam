package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const agentColumns = `id::text,project_id::text,name,normalized_name,display_name,tag_color,description,instructions,inject_agents_md,model_id::text,reasoning_effort,approval_policy,approval_model_id::text,lifecycle,version,created_at,updated_at`

func loadAgent(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID) (*c.AgentConfig, error) {
	var d c.AgentConfigFields
	v := &d.Core
	var rawID, rawProject, rawModel, policy, lifecycle string
	var rawApproval *string
	var version int64
	var created, updated time.Time
	err := x.QueryRow(ctx, `SELECT `+agentColumns+` FROM agenteam_agent.agents WHERE project_id=$1 AND id=$2`, project.String(), agent.String()).Scan(
		&rawID, &rawProject, &v.Name, &v.NormalizedName, &v.DisplayName, &v.TagColor, &v.Description, &v.Instructions, &v.InjectAgentsMD, &rawModel, &v.ReasoningEffort, &policy, &rawApproval, &lifecycle, &version, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if v.ID, err = f.ParseID[i.Agent](rawID); err != nil || v.ID != agent {
		return nil, unavailable(nil)
	}
	if v.ProjectID, err = f.ParseID[i.Project](rawProject); err != nil || v.ProjectID != project {
		return nil, unavailable(nil)
	}
	if v.ModelRef, err = f.ParseID[mc.Model](rawModel); err != nil {
		return nil, unavailable(nil)
	}
	if rawApproval != nil {
		id, err := f.ParseID[mc.Model](*rawApproval)
		if err != nil {
			return nil, unavailable(nil)
		}
		v.ApprovalModelRef = &id
	}
	v.ApprovalPolicy, v.Lifecycle, v.Version = c.ApprovalPolicy(policy), c.AgentLifecycle(lifecycle), f.Version(version)
	if v.CreatedAt, err = f.NewInstant(created); err != nil {
		return nil, unavailable(nil)
	}
	if v.UpdatedAt, err = f.NewInstant(updated); err != nil {
		return nil, unavailable(nil)
	}
	d.AllowedToolIDs, err = loadAllowlist[i.Tool](ctx, x, project, agent, "tool_allowlist", "tool_id")
	if err != nil {
		return nil, err
	}
	d.AllowedMountIDs, err = loadAllowlist[i.Mount](ctx, x, project, agent, "mount_allowlist", "mount_id")
	if err != nil {
		return nil, err
	}
	d.AllowedSecretVariableIDs, err = loadAllowlist[i.ProjectVariable](ctx, x, project, agent, "secret_allowlist", "variable_id")
	if err != nil {
		return nil, err
	}
	value, err := c.NewAgentConfig(d)
	if err != nil {
		return nil, unavailable(err)
	}
	return &value, nil
}

func allowlistTable(table, column string) bool {
	return table == "tool_allowlist" && column == "tool_id" || table == "mount_allowlist" && column == "mount_id" || table == "secret_allowlist" && column == "variable_id"
}
func loadAllowlist[K any](ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID, table, column string) ([]f.ID[K], error) {
	if !allowlistTable(table, column) {
		return nil, invalid()
	}
	rows, err := x.Query(ctx, `SELECT `+column+`::text FROM agenteam_agent.`+table+` WHERE project_id=$1 AND agent_id=$2 ORDER BY `+column+` LIMIT 129`, project.String(), agent.String())
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	out := []f.ID[K]{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, unavailable(err)
		}
		id, err := f.ParseID[K](raw)
		if err != nil || len(out) == c.MaxCapabilityReferences {
			return nil, unavailable(nil)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	return out, nil
}

func canonicalWriteError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		var field f.FieldError
		switch pg.ConstraintName {
		case "agents_pkey":
			field = f.FieldError{Path: "/agent_id", Code: "TARGET_OCCUPIED"}
		case "agents_project_name":
			field = f.FieldError{Path: "/name", Code: "NAME_OCCUPIED"}
		default:
			return unavailable(err)
		}
		out := fault(f.ResourceBusy)
		out.FieldErrors = []f.FieldError{field}
		return out
	}
	return unavailable(err)
}

func writeAgentRow(ctx context.Context, x postgres.SQLExecutor, before *c.AgentConfig, after c.AgentConfig) error {
	d := after.Fields()
	v := d.Core
	var approval *string
	if v.ApprovalModelRef != nil {
		text := v.ApprovalModelRef.String()
		approval = &text
	}
	args := []any{v.ID.String(), v.ProjectID.String(), v.Name, v.NormalizedName, v.DisplayName, v.TagColor, v.Description, v.Instructions, v.InjectAgentsMD, v.ModelRef.String(), v.ReasoningEffort, string(v.ApprovalPolicy), approval, string(v.Lifecycle), int64(v.Version), v.CreatedAt.Time(), v.UpdatedAt.Time()}
	query := `INSERT INTO agenteam_agent.agents(id,project_id,name,normalized_name,display_name,tag_color,description,instructions,inject_agents_md,model_id,reasoning_effort,approval_policy,approval_model_id,lifecycle,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`
	if before != nil {
		args = append(args, int64(before.Fields().Core.Version))
		query = `UPDATE agenteam_agent.agents SET name=$3,normalized_name=$4,display_name=$5,tag_color=$6,description=$7,instructions=$8,inject_agents_md=$9,model_id=$10,reasoning_effort=$11,approval_policy=$12,approval_model_id=$13,lifecycle=$14,version=$15,created_at=$16,updated_at=$17 WHERE id=$1 AND project_id=$2 AND version=$18`
	}
	tag, err := x.Exec(ctx, query, args...)
	if err != nil {
		return canonicalWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	if err := replaceAllowlist(ctx, x, v.ProjectID, v.ID, "tool_allowlist", "tool_id", d.AllowedToolIDs); err != nil {
		return err
	}
	if err := replaceAllowlist(ctx, x, v.ProjectID, v.ID, "mount_allowlist", "mount_id", d.AllowedMountIDs); err != nil {
		return err
	}
	return replaceAllowlist(ctx, x, v.ProjectID, v.ID, "secret_allowlist", "variable_id", d.AllowedSecretVariableIDs)
}

func replaceAllowlist[K any](ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID, table, column string, ids []f.ID[K]) error {
	if !allowlistTable(table, column) || len(ids) > c.MaxCapabilityReferences {
		return invalid()
	}
	if _, err := x.Exec(ctx, `DELETE FROM agenteam_agent.`+table+` WHERE project_id=$1 AND agent_id=$2`, project.String(), agent.String()); err != nil {
		return unavailable(err)
	}
	for _, id := range ids {
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_agent.`+table+`(project_id,agent_id,`+column+`) VALUES($1,$2,$3)`, project.String(), agent.String(), id.String()); err != nil {
			return unavailable(err)
		}
	}
	return nil
}

type commandRecord struct {
	ID                  c.AgentCommandID
	Project             i.ProjectID
	User                i.UserID
	Target              i.AgentID
	Name                c.CommandName
	Key                 f.IdempotencyKey
	Semantic            f.Digest
	Input               json.RawMessage
	Expected            *f.Version
	Revision            f.Version
	Before              *c.AgentConfig
	After               c.AgentConfig
	AddSkillsEnabled    *bool
	InstallSkillEnabled *bool
	ChangedFields       []string
	State               string
	Receipt             json.RawMessage
	Created             f.Instant
	Committed           *f.Instant
}

func (r commandRecord) identity() f.CommandIdentity {
	identity, _ := c.AgentCommandIdentity(r.Project, r.Name, r.Key)
	return identity
}
func (r commandRecord) validate() error {
	if r.ID.Validate() != nil || r.Project.Validate() != nil || r.User.Validate() != nil || r.Target.Validate() != nil || r.Name.Validate() != nil || r.Key.Validate() != nil || r.Semantic.Validate() != nil || r.Revision.Validate() != nil || r.After.Validate() != nil || r.Created.Validate() != nil || !json.Valid(r.Input) || len(r.Input) > c.MaxAgentCoreBytes {
		return unavailable(nil)
	}
	a := r.After.Fields().Core
	if a.ID != r.Target || a.ProjectID != r.Project || a.Lifecycle != c.AgentActive {
		return unavailable(nil)
	}
	if r.Name == c.CreateAgentCommand {
		if r.Before != nil || r.Expected != nil || a.Version != 1 || r.AddSkillsEnabled == nil || r.InstallSkillEnabled == nil {
			return unavailable(nil)
		}
	} else {
		if r.Before == nil || r.Before.Validate() != nil || r.Expected == nil || r.Expected.Validate() != nil || r.AddSkillsEnabled != nil || r.InstallSkillEnabled != nil {
			return unavailable(nil)
		}
		b := r.Before.Fields().Core
		if b.ID != r.Target || b.ProjectID != r.Project || b.Version != *r.Expected || b.Lifecycle != c.AgentActive || a.Version < b.Version || a.Version-b.Version > 1 || !a.CreatedAt.Time().Equal(b.CreatedAt.Time()) {
			return unavailable(nil)
		}
		if a.Version == b.Version && !sameValue(*r.Before, r.After) || a.Version != b.Version && !a.UpdatedAt.Time().After(b.UpdatedAt.Time()) {
			return unavailable(nil)
		}
	}
	for n, name := range r.ChangedFields {
		if !slices.Contains([]string{"allowed_mount_ids", "allowed_secret_variable_ids", "allowed_tool_ids", "approval_model_ref", "approval_policy", "description", "display_name", "inject_agents_md", "instructions", "model_ref", "name", "reasoning_effort", "tag_color"}, name) || n > 0 && r.ChangedFields[n-1] >= name {
			return unavailable(nil)
		}
	}
	if r.ChangedFields == nil || (r.Before == nil || r.After.Fields().Core.Version != r.Before.Fields().Core.Version) != (len(r.ChangedFields) > 0) {
		return unavailable(nil)
	}
	if r.State == "planned" {
		if r.Committed != nil || len(r.Receipt) != 0 {
			return unavailable(nil)
		}
	} else if r.State != "completed" || r.Committed == nil || r.Committed.Validate() != nil || r.Committed.Time().Before(r.Created.Time()) || !json.Valid(r.Receipt) || len(r.Receipt) > c.MaxAgentCoreBytes {
		return unavailable(nil)
	}
	return nil
}

func loadCommand(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, command c.CommandName, key f.IdempotencyKey) (*commandRecord, error) {
	var r commandRecord
	var id, p, user, target, name, semantic string
	var expected *int64
	var revision int64
	var before, after []byte
	var created time.Time
	var committed *time.Time
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,actor_user_id::text,target_id::text,command_name,semantic_digest,input,expected_version,plan_revision,before_config,after_config,add_skills_enabled,install_skill_enabled,changed_fields,state,receipt,created_at,committed_at FROM agenteam_agent.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, project.String(), string(command), string(key)).Scan(&id, &p, &user, &target, &name, &semantic, &r.Input, &expected, &revision, &before, &after, &r.AddSkillsEnabled, &r.InstallSkillEnabled, &r.ChangedFields, &r.State, &r.Receipt, &created, &committed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if r.ID, err = f.ParseID[c.AgentCommand](id); err != nil {
		return nil, unavailable(nil)
	}
	if r.Project, err = f.ParseID[i.Project](p); err != nil || r.Project != project {
		return nil, unavailable(nil)
	}
	if r.User, err = f.ParseID[i.User](user); err != nil {
		return nil, unavailable(nil)
	}
	if r.Target, err = f.ParseID[i.Agent](target); err != nil {
		return nil, unavailable(nil)
	}
	r.Name, r.Key, r.Semantic, r.Revision = c.CommandName(name), key, f.Digest(semantic), f.Version(revision)
	if r.Name != command {
		return nil, unavailable(nil)
	}
	if expected != nil {
		value := f.Version(*expected)
		r.Expected = &value
	}
	if len(before) != 0 {
		value, err := c.DecodeAgentConfig(before)
		if err != nil {
			return nil, unavailable(err)
		}
		r.Before = &value
	}
	if r.After, err = c.DecodeAgentConfig(after); err != nil {
		return nil, unavailable(err)
	}
	if r.Created, err = f.NewInstant(created); err != nil {
		return nil, unavailable(nil)
	}
	if committed != nil {
		value, err := f.NewInstant(*committed)
		if err != nil {
			return nil, unavailable(nil)
		}
		r.Committed = &value
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}
