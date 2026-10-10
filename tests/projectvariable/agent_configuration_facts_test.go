//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This observer only reads facts produced by the real CreateAgent call. The
// final transaction hook also uses its original SQLExecutor before forcing a
// rollback; an earlier, committed planned intent is deliberately a separate
// count. No query here seeds an Agent or substitutes for a provider.
// Order: canonical; three allowlists; Model roles; Tool head/refs; Mount
// head/refs; Secret refs; Skill head/assignments; Audit; Outbox; completed/planned.
func agentCreateFacts(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, target i.AgentID, key f.IdempotencyKey) ([16]int64, error) {
	var facts [16]int64
	if ctx == nil || x == nil || project.Validate() != nil || target.Validate() != nil || key.Validate() != nil {
		return facts, errors.New("agent_create_facts_input")
	}
	err := x.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2),
 (SELECT count(*) FROM agenteam_agent.tool_allowlist WHERE project_id::text=$1 AND agent_id::text=$2),
 (SELECT count(*) FROM agenteam_agent.mount_allowlist WHERE project_id::text=$1 AND agent_id::text=$2),
 (SELECT count(*) FROM agenteam_agent.secret_allowlist WHERE project_id::text=$1 AND agent_id::text=$2),
 (SELECT count(*) FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2),
 (SELECT count(*) FROM agenteam_tool.agent_configurations WHERE agent_id::text=$2),
 (SELECT count(*) FROM agenteam_tool.agent_references WHERE agent_id::text=$2),
 (SELECT count(*) FROM agenteam_mount.agent_mount_heads WHERE agent_id::text=$2),
 (SELECT count(*) FROM agenteam_mount.agent_mount_refs WHERE agent_id::text=$2),
 (SELECT count(*) FROM agenteam_projectvariable.secret_references WHERE owner_kind='agent' AND owner_id::text=$2),
 (SELECT count(*) FROM agenteam_skill.agent_assignment_heads WHERE agent_id::text=$2),
 (SELECT count(*) FROM agenteam_skill.agent_assignments WHERE agent_id::text=$2),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND producer='agent' AND action='agent.create' AND resource_id::text=$2),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND producer='agent' AND aggregate_id::text=$2),
 (SELECT count(*) FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name='agent.create' AND idempotency_key=$3 AND state='completed'),
 (SELECT count(*) FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name='agent.create' AND idempotency_key=$3 AND state='planned')`,
		project.String(), target.String(), string(key)).Scan(
		&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5], &facts[6], &facts[7],
		&facts[8], &facts[9], &facts[10], &facts[11], &facts[12], &facts[13], &facts[14], &facts[15])
	if err != nil || ctx.Err() != nil {
		return [16]int64{}, errors.New("agent_create_facts_read")
	}
	return facts, nil
}

// Relations bind the v1 receipt to the complete canonical row and all provider
// facts in the original final transaction, rather than treating row counts as
// proof of the right command, version, publication or event.
func agentCreateRelations(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, target i.AgentID, key f.IdempotencyKey, modelID mc.ModelID, toolID i.ToolID, command f.CommandIdentity) error {
	expected, err := agentc.AgentCommandIdentity(project, agentc.CreateAgentCommand, key)
	if ctx == nil || x == nil || err != nil || command.Validate() != nil || command.Canonical() != expected.Canonical() || target.Validate() != nil || modelID.Validate() != nil || toolID.Validate() != nil {
		return errors.New("agent_create_relations_input")
	}
	var commandID, user string
	var revision int64
	var receiptRaw []byte
	var samePostimage, defaults bool
	err = x.QueryRow(ctx, `SELECT id::text,actor_user_id::text,plan_revision,receipt,
 after_config=receipt->'agent',add_skills_enabled AND install_skill_enabled AND before_config IS NULL AND expected_version IS NULL
 FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2
 AND command_name='agent.create' AND idempotency_key=$3 AND state='completed'`,
		project.String(), target.String(), string(key)).Scan(&commandID, &user, &revision, &receiptRaw, &samePostimage, &defaults)
	if err != nil || revision < 1 || !samePostimage || !defaults {
		return errors.New("agent_create_command_relation")
	}
	if _, err = f.ParseID[agentc.AgentCommand](commandID); err != nil {
		return errors.New("agent_create_command_identity")
	}
	receipt, err := agentc.DecodeAgentMutation(receiptRaw)
	if err != nil {
		return errors.New("agent_create_receipt_decode")
	}
	mutation := receipt.Fields()
	config := mutation.Agent.Fields()
	core := config.Core
	if !mutation.Changed || len(mutation.EventIDs) != 1 || mutation.EventIDs[0].String() != commandID ||
		core.ID != target || core.ProjectID != project || core.Version != 1 || core.Lifecycle != agentc.AgentActive ||
		core.ModelRef != modelID || core.ReasoningEffort != nil || core.ApprovalPolicy != agentc.ApprovalAuto || core.ApprovalModelRef == nil || *core.ApprovalModelRef != modelID ||
		!core.CreatedAt.Time().Equal(core.UpdatedAt.Time()) || !slices.Equal(config.AllowedToolIDs, []i.ToolID{toolID}) || len(config.AllowedMountIDs) != 0 || len(config.AllowedSecretVariableIDs) != 0 {
		return errors.New("agent_create_receipt_relation")
	}
	var name, normalized, description, instructions, model, policy, approval, lifecycle string
	var display, color, effort *string
	var inject bool
	var version int64
	var created, updated time.Time
	err = x.QueryRow(ctx, `SELECT name,normalized_name,display_name,tag_color,description,instructions,inject_agents_md,
 model_id::text,reasoning_effort,approval_policy,COALESCE(approval_model_id::text,''),lifecycle,version,created_at,updated_at
 FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, project.String(), target.String()).Scan(
		&name, &normalized, &display, &color, &description, &instructions, &inject,
		&model, &effort, &policy, &approval, &lifecycle, &version, &created, &updated)
	if err != nil || name != core.Name || normalized != core.NormalizedName || !agentCreateOptionalEqual(display, core.DisplayName) || !agentCreateOptionalEqual(color, core.TagColor) ||
		description != core.Description || instructions != core.Instructions || inject != core.InjectAgentsMD || model != modelID.String() || effort != nil ||
		policy != string(core.ApprovalPolicy) || approval != modelID.String() || lifecycle != string(core.Lifecycle) || version != 1 || !created.Equal(core.CreatedAt.Time()) || !updated.Equal(core.UpdatedAt.Time()) {
		return errors.New("agent_create_canonical_relation")
	}
	var references bool
	err = x.QueryRow(ctx, `SELECT
 (SELECT count(*)=2 AND count(DISTINCT role)=2 AND bool_and(project_id::text=$1 AND model_id::text=$3 AND owner_version=1 AND reasoning_effort='' AND role IN ('agent_model','approval_model'))
  FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_agent.tool_allowlist WHERE project_id::text=$1 AND agent_id::text=$2 AND tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations h JOIN agenteam_tool.agent_references r ON r.agent_id=h.agent_id
  WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.config_version=1 AND r.tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE project_id::text=$1 AND agent_id::text=$2 AND owner_version=1)`,
		project.String(), target.String(), modelID.String(), toolID.String()).Scan(&references)
	if err != nil || !references {
		return errors.New("agent_create_provider_references")
	}
	var skillCommand, skillBinding string
	var skillRevision int64
	var skillPublished bool
	err = x.QueryRow(ctx, `SELECT h.creation_command,h.plan_revision,h.request_binding,
 h.add_skills_enabled AND h.assignment_sequence=1 AND h.observed_revision=s.current_revision
 AND a.id=h.initial_assignment_id AND a.skill_id=h.initial_skill_id AND a.enabled AND a.assignment_sequence=1 AND a.removed_at IS NULL AND a.created_at=h.created_at
 AND s.protected AND s.serving AND s.name='Add Skills' AND s.normalized_name='add-skills'
 AND r.revision=h.observed_revision AND z.phase='published' AND z.bundle_id='builtin.add-skills.v1' AND z.object_id=r.object_id
 FROM agenteam_skill.agent_assignment_heads h
 JOIN agenteam_skill.agent_assignments a ON a.project_id=h.project_id AND a.agent_id=h.agent_id AND a.id=h.initial_assignment_id
 JOIN agenteam_skill.skills s ON s.project_id=h.project_id AND s.id=h.initial_skill_id
 JOIN agenteam_skill.revisions r ON r.project_id=s.project_id AND r.skill_id=s.id AND r.id=s.revision_id
 JOIN agenteam_skill.initializations z ON z.project_id=s.project_id AND z.skill_id=s.id AND z.revision_id=r.id AND z.creation_id=s.creation_id
 WHERE h.project_id::text=$1 AND h.agent_id::text=$2`, project.String(), target.String()).Scan(&skillCommand, &skillRevision, &skillBinding, &skillPublished)
	if err != nil || skillCommand != command.Canonical() || skillRevision != revision || f.Digest(skillBinding).Validate() != nil || !skillPublished {
		return errors.New("agent_create_skill_publication_relation")
	}
	var auditRaw []byte
	var auditUser, session, cause string
	err = x.QueryRow(ctx, `SELECT metadata,user_id::text,session_id::text,cause_ref FROM agenteam_audit.audit_records
 WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent' AND action='agent.create'
 AND scope='project' AND actor_kind='human' AND outcome='success' AND resource_kind='agent' AND ordinal=0`,
		project.String(), target.String()).Scan(&auditRaw, &auditUser, &session, &cause)
	if err != nil || auditUser != user {
		return errors.New("agent_create_audit_identity")
	}
	metadata, err := ac.DecodeMetadata(ac.AgentCreate, auditRaw)
	if err != nil {
		return errors.New("agent_create_audit_decode")
	}
	fields, err := metadata.AgentFields()
	digest, digestErr := cursor.Digest([]byte(command.Canonical()))
	if err != nil || digestErr != nil || cause != digest.String() || fields.AgentID != target.String() || fields.CommandID != commandID || fields.Version != 1 || !slices.Equal(fields.ChangedFields, agentc.EditableFields()) {
		return errors.New("agent_create_audit_relation")
	}
	// Activity belongs to the real Human Session. Its 60-second throttle means
	// a successful create need not change the timestamp. The caller separately
	// compares this timestamp before/after the forced rollback.
	var activity bool
	err = x.QueryRow(ctx, `SELECT user_id::text=$2 AND revoked_at IS NULL AND last_activity_at>=issued_at
 AND last_activity_at<=clock_timestamp() AND absolute_expires_at>clock_timestamp()
 FROM agenteam_account.sessions WHERE id::text=$1`, session, user).Scan(&activity)
	if err != nil || !activity {
		return errors.New("agent_create_activity_relation")
	}
	var headerRaw, payloadRaw []byte
	var eventColumns bool
	err = x.QueryRow(ctx, `SELECT header,payload,
 id::text=$3 AND event_type='agent.config_changed' AND schema_version=1 AND scope='project'
 AND aggregate_type='agent.config' AND aggregate_version=1 AND aggregate_sequence IS NULL AND occurred_at=$4
 FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent'`,
		project.String(), target.String(), commandID, updated).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil || !eventColumns {
		return errors.New("agent_create_event_columns")
	}
	header, err := ec.DecodeHeader(headerRaw)
	var payload agentc.ConfigChangedPayload
	if err != nil || agentc.ValidateAgentEventHeader(header) != nil || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil ||
		header.EventID != mutation.EventIDs[0] || header.AggregateID.String() != target.String() || header.Scope.ProjectID.String() != project.String() || *header.AggregateVersion != 1 || !header.OccurredAt.Time().Equal(updated) ||
		payload.CommandID.String() != commandID || payload.ActorUserID.String() != user || payload.Operation != agentc.ConfigCreated || !slices.Equal(payload.ChangedFields, fields.ChangedFields) {
		return errors.New("agent_create_event_receipt_relation")
	}
	if ctx.Err() != nil {
		return errors.New("agent_create_relations_cancelled")
	}
	return nil
}

func agentCreateOptionalEqual(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
