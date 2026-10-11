//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// This fixture reuses the actual Account/P2/Object/Agent/Scheduler producers.
// Launch stops at created. No Model, fake ingress authority, canonical SQL
// seed or replacement Outbox appender supplies a successful domain result.
type skillAssignmentIngressFixture struct {
	launch   *schedulerLaunchFixture
	commands *skill.AssignmentCommands
	skill    sc.SkillID
	version  f.Version
	before   skillAssignmentIngressState
}

func newSkillAssignmentIngressFixture(t *testing.T) *skillAssignmentIngressFixture {
	t.Helper()
	launch := newSchedulerLaunchFixture(t)
	task, b := launch.task, launch.task.base
	a := task.agent
	// Give the Secret reference owner one real selected reference. The original
	// Tool/Model sets and the Mount owner's initialized empty head stay intact.
	secrets := assembleSecretOwnerFixture(t, b)
	request := secretCreateInput(t, "ASSIGNMENT_SELECTED_SECRET", []byte("owned-assignment-material"))
	secret, err := secrets.owner.CreateSecretVariable(ctxFor(t), b.ownerBrowser.actor, meta(t, "assignment-secret", nil), b.project.ID, request)
	request.Destroy()
	firstRoundRequire(t, err)
	current, err := a.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, task.agentID)
	firstRoundRequire(t, err)
	selected := []i.ProjectVariableID{secret.Fields().Variable.ID}
	change, err := ag.NewAgentUpdate(ag.AgentUpdateFields{AllowedSecretVariableIDs: &selected})
	firstRoundRequire(t, err)
	version := current.Fields().Core.Version
	updated, err := a.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "assignment-select-secret", &version), b.project.ID, task.agentID, change)
	firstRoundRequire(t, err)
	if updated.Validate() != nil || !reflect.DeepEqual(updated.Fields().Agent.Fields().AllowedSecretVariableIDs, selected) {
		t.Fatal("formal Agent command did not retain the selected Secret reference")
	}
	install, err := skill.NewInstallRequest(ctxFor(t), id[sc.Skill](t), skillInstallationPackage(t))
	firstRoundRequire(t, err)
	installed, err := a.p2.service.Install(ctxFor(t), b.ownerBrowser.actor, meta(t, "assignment-publish-skill", nil), b.project.ID, install)
	firstRoundRequire(t, err)
	if installed.Validate() != nil || installed.Revision != 1 {
		t.Fatal("ordinary Skill was not published by the original installation service")
	}
	// The old Project graph has no assignment Audit route. Build a fixed facade
	// over the same Store and actual providers; never alter its copied route map.
	proof, err := skill.NewAssignmentAuditFacts(b.tracked)
	firstRoundRequire(t, err)
	projects, err := project.NewAuthority(b.tracked, project.AuthorityDependencies{Sessions: b.accounts, Routes: b.accounts, AuditFacts: map[au.Producer]au.ProjectFactAuthority{au.SkillProducer: proof}})
	firstRoundRequire(t, err)
	owner, err := skill.NewAuthority(b.tracked, projects)
	firstRoundRequire(t, err)
	process, err := a.guard.CurrentProcess()
	firstRoundRequire(t, err)
	bundle, err := skill.AddSkills(ctxFor(t))
	firstRoundRequire(t, err)
	service, err := skill.New(skill.Dependencies{Authority: owner, Objects: a.p2.objects, Processes: a.guard, ProcessID: process, Bundle: bundle})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil || !service.Joined() {
			t.Error("original assignment Skill calls did not actually join", err)
		}
	})
	catalog := event.NewCatalog()
	types, err := sc.RegisterAssignmentEvents(catalog)
	firstRoundRequire(t, err)
	facts, err := skill.NewAssignmentAuthority(owner, types)
	firstRoundRequire(t, err)
	ingress, err := execution.NewSkillAssignmentIngress(b.tracked, facts)
	firstRoundRequire(t, err)
	toolRefs, err := registry.NewSkillAssignmentReferences(b.tracked, a.providers.Agents)
	firstRoundRequire(t, err)
	mountRefs, err := mount.NewSkillAssignmentReferences(b.tracked, a.providers.Agents)
	firstRoundRequire(t, err)
	modelRefs, err := model.NewSkillAssignmentReferences(b.tracked, a.providers.Agents)
	firstRoundRequire(t, err)
	secretRefs, err := pv.NewSkillAssignmentReferences(b.tracked, a.providers.Agents)
	firstRoundRequire(t, err)
	agents, err := agent.NewSkillAssignmentAuthority(a.providers.Agents, []ag.SkillAssignmentVersionReferences{toolRefs, mountRefs, modelRefs, secretRefs})
	firstRoundRequire(t, err)
	auditor, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Projects: projects})
	firstRoundRequire(t, err)
	processID, err := f.ParseID[ob.Process](process.String())
	firstRoundRequire(t, err)
	box, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]ob.ProducerAuthority{sc.AssignmentProducer: facts}, Sessions: b.accounts, System: b.accounts, Projects: projects, Audit: auditor, Cursors: b.keys, Processes: fixtureProcess{processID}})
	firstRoundRequire(t, err)
	commands, err := skill.NewAssignmentCommands(skill.AssignmentDependencies{Service: service, Agents: agents, CurrentAgents: a.providers.Agents, Runtime: ingress, Audit: auditor, Outbox: box, Events: types}, facts)
	firstRoundRequire(t, err)
	v := &skillAssignmentIngressFixture{launch: launch, commands: commands, skill: installed.SkillID, version: updated.Fields().Agent.Fields().Core.Version}
	v.before, err = v.read(ctxFor(t), b.raw)
	firstRoundRequire(t, err)
	return v
}

func (v *skillAssignmentIngressFixture) launchCreated(t *testing.T) i.ExecutionID {
	t.Helper()
	result, err := v.launch.handoff.LaunchOnce(ctxFor(t), v.launch.request.ProjectID, v.launch.dispatch)
	firstRoundRequire(t, err)
	v.launch.requireAssociated(t, result)
	calls, _, created := v.launch.launcher.observed()
	if calls != 1 || created.Status != ec.Created || created.Version != 1 {
		t.Fatal("original Launch did not stop at its one created Execution")
	}
	return created.ID
}

func (v *skillAssignmentIngressFixture) intent() skill.AssignmentRequest {
	enabled, target := true, v.skill
	return skill.AssignmentRequest{ProjectID: v.launch.task.base.project.ID, AgentID: v.launch.task.agentID, SkillID: &target, Enabled: &enabled}
}

// Counts: completed/planned commands, changes/current, target grants/live,
// ingress/pending references, assignment Audit/Outbox. Initial creation facts
// are deliberately outside these mutation counts and compared byte for byte.
type skillAssignmentIngressState struct {
	Version            int64
	Current            bool
	Selection, Initial []byte
	Counts             [10]int64
}

func (v *skillAssignmentIngressFixture) read(ctx context.Context, x postgres.SQLExecutor) (skillAssignmentIngressState, error) {
	var out skillAssignmentIngressState
	err := x.QueryRow(ctx, `SELECT a.version,
 EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations h WHERE h.project_id::text=a.project_id::text AND h.agent_id::text=a.id::text AND h.config_version=a.version)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads h WHERE h.project_id=a.project_id AND h.agent_id=a.id AND h.owner_version=a.version)
 AND (SELECT count(*)=2 AND bool_and(r.project_id=a.project_id AND r.owner_version=a.version) FROM agenteam_model.references r WHERE r.owner_kind='agent' AND r.owner_id=a.id)
 AND (SELECT count(*)=1 AND bool_and(r.project_id=a.project_id AND r.owner_version=a.version) FROM agenteam_projectvariable.secret_references r WHERE r.owner_kind='agent' AND r.owner_id=a.id),
 jsonb_build_object('agent',to_jsonb(a)-'version'-'updated_at',
 'tools',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.tool_id) FROM agenteam_tool.agent_references r WHERE r.agent_id::text=a.id::text),
 'mounts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.mount_id) FROM agenteam_mount.agent_mount_refs r WHERE r.agent_id=a.id),
 'models',(SELECT jsonb_agg(to_jsonb(r)-'owner_version' ORDER BY r.role) FROM agenteam_model.references r WHERE r.owner_kind='agent' AND r.owner_id=a.id),
 'secrets',(SELECT jsonb_agg(to_jsonb(r)-'owner_version' ORDER BY r.variable_id) FROM agenteam_projectvariable.secret_references r WHERE r.owner_kind='agent' AND r.owner_id=a.id))::text,
 (SELECT to_jsonb(h)::text FROM agenteam_skill.agent_assignment_heads h WHERE h.project_id=a.project_id AND h.agent_id=a.id),
 (SELECT count(*) FROM agenteam_skill.assignment_commands WHERE agent_id=a.id AND state='completed'),
 (SELECT count(*) FROM agenteam_skill.assignment_commands WHERE agent_id=a.id AND state='planned'),
 (SELECT count(*) FROM agenteam_skill.assignment_changes WHERE agent_id=a.id),
 (SELECT count(*) FROM agenteam_skill.assignment_current WHERE agent_id=a.id),
 (SELECT count(*) FROM agenteam_skill.agent_assignments WHERE agent_id=a.id AND skill_id::text=$3),
 (SELECT count(*) FROM agenteam_skill.agent_assignments WHERE agent_id=a.id AND skill_id::text=$3 AND removed_at IS NULL),
 (SELECT count(*) FROM agenteam_execution.skill_assignment_ingress WHERE agent_id::text=a.id::text),
 (SELECT count(*) FROM agenteam_skill.assignment_pending_refs WHERE agent_id=a.id),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=a.project_id AND resource_id=a.id AND producer='skill' AND action IN ('skill.assign','skill.remove')),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=a.project_id AND aggregate_id=a.id AND producer='skill' AND aggregate_type='skill.assignment')
 FROM agenteam_agent.agents a WHERE a.project_id::text=$1 AND a.id::text=$2`, v.launch.task.base.project.ID.String(), v.launch.task.agentID.String(), v.skill.String()).Scan(
		&out.Version, &out.Current, &out.Selection, &out.Initial,
		&out.Counts[0], &out.Counts[1], &out.Counts[2], &out.Counts[3], &out.Counts[4], &out.Counts[5], &out.Counts[6], &out.Counts[7], &out.Counts[8], &out.Counts[9])
	return out, err
}

func (v *skillAssignmentIngressFixture) requireState(t *testing.T, version f.Version, counts [10]int64) skillAssignmentIngressState {
	t.Helper()
	got, err := v.read(ctxFor(t), v.launch.task.base.raw)
	firstRoundRequire(t, err)
	if got.Version != int64(version) || !got.Current || got.Counts != counts || !bytes.Equal(got.Selection, v.before.Selection) || !bytes.Equal(got.Initial, v.before.Initial) {
		t.Fatalf("assignment current/unchanged-selection/creation-receipt facts: version=%d current=%t counts=%v", got.Version, got.Current, got.Counts)
	}
	return got
}

// This is an independent SQL postcondition. Matching counts alone cannot show
// that the actual Agent, four providers, typed event and ingress committed in
// the same transaction as the original command receipt.
func (v *skillAssignmentIngressFixture) relations(ctx context.Context, x postgres.SQLExecutor, r sc.AssignmentMutation, key f.IdempotencyKey, removed bool) error {
	if r.Validate() != nil || !r.Changed {
		return errors.New("assignment_relations_receipt")
	}
	var same bool
	var receipt, payload []byte
	err := x.QueryRow(ctx, `SELECT c.receipt,e.payload,
 c.state='completed' AND c.idempotency_key=$4 AND c.actor_user_id::text=$5
 AND c.project_id=a.project_id AND c.agent_id=a.id
 AND ch.agent_version=a.version AND ch.assignment_sequence=$6
 AND h.assignment_sequence=ch.assignment_sequence AND h.state_digest=ch.state_digest
 AND c.xmin=ch.xmin AND c.xmin=h.xmin AND c.xmin=a.xmin AND c.xmin=e.xmin AND c.xmin=au.xmin
 AND e.producer='skill' AND e.event_type=$7 AND e.aggregate_type='skill.assignment'
 AND e.aggregate_id=a.id AND e.aggregate_version=a.version AND e.project_id=a.project_id
 AND au.action=c.command_name AND au.user_id::text=$5 AND au.resource_id=a.id
 AND au.metadata->>'skill_id'=$8 AND au.metadata->>'version'=a.version::text
 AND au.metadata->>'assignment_sequence'=ch.assignment_sequence::text
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations z WHERE z.agent_id::text=a.id::text AND z.config_version=a.version AND z.xmin=c.xmin)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads z WHERE z.agent_id=a.id AND z.owner_version=a.version AND z.xmin=c.xmin)
 AND (SELECT count(*)=2 AND bool_and(z.owner_version=a.version AND z.xmin=c.xmin) FROM agenteam_model.references z WHERE z.owner_kind='agent' AND z.owner_id=a.id)
 AND (SELECT count(*)=1 AND bool_and(z.owner_version=a.version AND z.xmin=c.xmin) FROM agenteam_projectvariable.secret_references z WHERE z.owner_kind='agent' AND z.owner_id=a.id)
 AND EXISTS(SELECT 1 FROM agenteam_skill.agent_assignments z WHERE z.agent_id=a.id AND z.skill_id::text=$8 AND z.xmin=c.xmin AND (z.removed_at IS NOT NULL)=$9)
 FROM agenteam_skill.assignment_commands c
 JOIN agenteam_skill.assignment_changes ch ON ch.id=c.id
 JOIN agenteam_skill.assignment_current h ON h.change_id=ch.id
 JOIN agenteam_agent.agents a ON a.id=c.agent_id
 JOIN agenteam_outbox.events e ON e.id=c.id
 JOIN agenteam_audit.audit_records au ON au.producer='skill' AND au.metadata->>'change_id'=c.id::text
 WHERE c.project_id::text=$1 AND c.agent_id::text=$2 AND c.id::text=$3`, r.ProjectID.String(), r.AgentID.String(), r.ChangeID.String(), string(key), v.launch.task.base.ownerBrowser.actor.Details().UserID, int64(r.AssignmentSequence), map[bool]string{false: string(sc.AssignmentAddedEvent), true: string(sc.AssignmentRemovedEvent)}[removed], v.skill.String(), removed).Scan(&receipt, &payload, &same)
	if err != nil {
		return err
	}
	var stored sc.AssignmentMutation
	var event sc.AssignmentEventPayload
	if !same || json.Unmarshal(receipt, &stored) != nil || !reflect.DeepEqual(stored, r) || json.Unmarshal(payload, &event) != nil || event.Validate() != nil || event.ChangeID != r.ChangeID || event.AgentID != r.AgentID || event.SkillID != v.skill || event.AssignmentSequence != r.AssignmentSequence || (event.FixedRevision == nil) != removed {
		return errors.New("assignment_receipt_event_same_transaction")
	}
	if removed {
		if r.Runtime != nil || r.Assignment != nil {
			return errors.New("removed_assignment_retained_authority")
		}
		return nil
	}
	if r.Assignment == nil || event.AssignmentID != r.Assignment.ID || event.FixedRevision == nil || *event.FixedRevision != 1 || r.Runtime == nil {
		return errors.New("assignment_revision_receipt")
	}
	var ingress bool
	err = x.QueryRow(ctx, `SELECT i.xmin=c.xmin AND convert_from(i.change_data,'UTF8')::jsonb=convert_from(ch.runtime_data,'UTF8')::jsonb
 AND i.project_id::text=c.project_id::text AND i.agent_id::text=c.agent_id::text
 AND i.outcome=$2 AND COALESCE(i.execution_id::text,'')=$3
 AND (CASE WHEN i.execution_id IS NULL THEN NOT EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs p WHERE p.change_id=c.id)
 ELSE EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs p JOIN agenteam_skill.revisions r ON r.id=p.revision_id
  WHERE p.change_id=c.id AND p.xmin=c.xmin AND p.execution_id::text=i.execution_id::text AND p.project_id=c.project_id AND p.agent_id=c.agent_id
  AND p.skill_id::text=$4 AND p.assignment_id::text=$5 AND p.assignment_sequence=ch.assignment_sequence
  AND p.revision=1 AND r.revision=p.revision AND r.skill_id=p.skill_id AND r.project_id=p.project_id AND r.object_id=p.object_id) END)
 FROM agenteam_skill.assignment_commands c JOIN agenteam_skill.assignment_changes ch ON ch.id=c.id
 JOIN agenteam_execution.skill_assignment_ingress i ON i.change_id::text=c.id::text WHERE c.id::text=$1`, r.ChangeID.String(), map[bool]string{false: "no_active_execution", true: "queued"}[r.Runtime.Queued], assignmentExecutionString(r.Runtime.ExecutionID), v.skill.String(), r.Assignment.ID.String()).Scan(&ingress)
	if err != nil {
		return err
	}
	if !ingress {
		return errors.New("assignment_original_ingress_and_revision_reference")
	}
	return nil
}

func assignmentExecutionString(id *i.ExecutionID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (v *skillAssignmentIngressFixture) requireCreatedOnly(t *testing.T, executionID i.ExecutionID) {
	t.Helper()
	var valid bool
	err := v.launch.task.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*)=1 AND bool_and(id::text=$2 AND status='created' AND version=1 AND snapshot_id IS NULL) FROM agenteam_execution.executions WHERE agent_id::text=$1)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.snapshots)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.rounds)
 AND NOT EXISTS(SELECT 1 FROM agenteam_model.calls)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.tool_calls)
 AND NOT EXISTS(SELECT 1 FROM agenteam_tool.operations)`, v.launch.task.agentID.String(), executionID.String()).Scan(&valid)
	firstRoundRequire(t, err)
	if !valid {
		t.Fatal("assignment crossed the created-only / zero-Model boundary")
	}
}

func TestSkillAssignmentIngress(t *testing.T) {
	t.Run("active-assignment-and-current-replay", func(t *testing.T) {
		v := newSkillAssignmentIngressFixture(t)
		b := v.launch.task.base
		e := v.launchCreated(t)
		v.requireState(t, v.version, [10]int64{})
		original := meta(t, "assignment-active-original", &v.version)
		added, err := v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, original, b.project.ID, v.launch.task.agentID, v.skill, true)
		firstRoundRequire(t, err)
		if added.Validate() != nil || !added.Changed || added.AgentVersion != v.version+1 || added.AssignmentSequence != 2 || added.Assignment == nil || added.Assignment.SkillID != v.skill || !added.Assignment.Enabled || added.Runtime == nil || !added.Runtime.Queued || added.Runtime.ExecutionID == nil || *added.Runtime.ExecutionID != e {
			t.Fatal("enabled assignment did not target the original created Execution")
		}
		v.requireState(t, added.AgentVersion, [10]int64{1, 0, 1, 1, 1, 1, 1, 1, 1, 1})
		firstRoundRequire(t, v.relations(ctxFor(t), b.raw, added, original.IdempotencyKey, false))
		replay, err := v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, original, b.project.ID, v.launch.task.agentID, v.skill, true)
		firstRoundRequire(t, err)
		if !reflect.DeepEqual(replay, added) {
			t.Fatal("same-key full-intent replay replaced the original receipt")
		}
		_, err = v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, original, b.project.ID, v.launch.task.agentID, v.skill, false)
		requireCode(t, err, f.IdempotencyKeyReused)
		version := added.AgentVersion
		noop, err := v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, meta(t, "assignment-new-key-noop", &version), b.project.ID, v.launch.task.agentID, v.skill, true)
		firstRoundRequire(t, err)
		if noop.Validate() != nil || noop.Changed || noop.AgentVersion != added.AgentVersion || noop.AssignmentSequence != added.AssignmentSequence || noop.Runtime != nil || noop.ChangeID == added.ChangeID {
			t.Fatal("new-key no-op changed configuration or ingress")
		}
		v.requireState(t, version, [10]int64{2, 0, 1, 1, 1, 1, 1, 1, 1, 1})
		_, err = v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, meta(t, "assignment-stale-version", &v.version), b.project.ID, v.launch.task.agentID, v.skill, false)
		requireCode(t, err, f.VersionConflict)
		_, err = v.commands.AssignSkill(ctxFor(t), b.otherBrowser.actor, meta(t, "assignment-non-owner", &version), b.project.ID, v.launch.task.agentID, v.skill, false)
		requireCode(t, err, f.NotFound)
		v.requireState(t, version, [10]int64{2, 0, 1, 1, 1, 1, 1, 1, 1, 1})
		removeMeta := meta(t, "assignment-remove", &version)
		removed, err := v.commands.RemoveSkill(ctxFor(t), b.ownerBrowser.actor, removeMeta, b.project.ID, v.launch.task.agentID, added.Assignment.ID)
		firstRoundRequire(t, err)
		if removed.Validate() != nil || !removed.Changed || removed.AgentVersion != version+1 || removed.AssignmentSequence != 3 || removed.Runtime != nil {
			t.Fatal("remove did not make one configuration-only change")
		}
		v.requireState(t, removed.AgentVersion, [10]int64{3, 0, 2, 1, 1, 0, 1, 1, 2, 2})
		firstRoundRequire(t, v.relations(ctxFor(t), b.raw, removed, removeMeta.IdempotencyKey, true))
		directory, err := v.commands.ListAssignments(ctxFor(t), b.ownerBrowser.actor, b.project.ID, v.launch.task.agentID)
		firstRoundRequire(t, err)
		if directory.Sequence != 3 {
			t.Fatal("current assignment directory sequence")
		}
		for _, a := range directory.Items {
			if a.SkillID == v.skill {
				t.Fatal("removed grant remains in current directory")
			}
		}
		observed, err := v.commands.LookupAssignment(ctxFor(t), b.ownerBrowser.actor, original, v.intent())
		firstRoundRequire(t, err)
		if observed.State != "committed" || observed.Receipt == nil || !reflect.DeepEqual(*observed.Receipt, added) {
			t.Fatal("remove rewrote the original queued assignment history")
		}
		v.requireCreatedOnly(t, e)
	})
	t.Run("rollback-commit-loss-and-no-active-history", func(t *testing.T) {
		v := newSkillAssignmentIngressFixture(t)
		b := v.launch.task.base
		store := b.tracked
		original := meta(t, "assignment-no-active-original", &v.version)
		command, err := f.NewCommandIdentity("project", []string{b.project.ID.String(), v.launch.task.agentID.String()}, "skill.assign", original.IdempotencyKey)
		firstRoundRequire(t, err)
		matches := func(cause f.TransactionCause) bool {
			return cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical()
		}
		// Discovery and finalization share a command cause. Observe a completed
		// receipt inside the real callback before selecting the final transaction;
		// the separately committed planned candidate must not trigger either fault.
		readFinal := func(ctx context.Context, tx f.Tx) (sc.AssignmentMutation, bool, error) {
			var receipt []byte
			x, err := store.InTx(tx)
			if err != nil {
				return sc.AssignmentMutation{}, false, err
			}
			err = x.QueryRow(ctx, `SELECT COALESCE((SELECT receipt FROM agenteam_skill.assignment_commands WHERE project_id::text=$1 AND agent_id::text=$2 AND command_name='skill.assign' AND idempotency_key=$3 AND state='completed'),'\x'::bytea)`, b.project.ID.String(), v.launch.task.agentID.String(), string(original.IdempotencyKey)).Scan(&receipt)
			if err != nil || len(receipt) == 0 {
				return sc.AssignmentMutation{}, false, err
			}
			var out sc.AssignmentMutation
			if err = json.Unmarshal(receipt, &out); err != nil {
				return out, false, err
			}
			if err = v.relations(ctx, x, out, original.IdempotencyKey, false); err != nil {
				return out, false, err
			}
			return out, true, nil
		}
		marker := errors.New("skill-assignment-final-callback-rollback")
		var tentative sc.AssignmentMutation
		observed, returned := false, false
		var physical f.CommitResult
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !matches(cause) {
				return nil
			}
			var final bool
			var err error
			tentative, final, err = readFinal(ctx, tx)
			if err != nil || !final {
				return err
			}
			observed = true
			return marker
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if matches(cause) && observed {
				returned = true
				physical = result
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		_, err = v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, original, b.project.ID, v.launch.task.agentID, v.skill, true)
		clear()
		if err == nil || !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || tentative.Runtime == nil || tentative.Runtime.Queued || tentative.Runtime.ExecutionID != nil {
			t.Fatal("final callback was not physically rolled back after the real no-active ingress", err)
		}
		v.requireState(t, v.version, [10]int64{0, 1, 0, 0, 0, 0, 0, 0, 0, 0})
		lookup, err := v.commands.LookupAssignment(ctxFor(t), b.ownerBrowser.actor, original, v.intent())
		firstRoundRequire(t, err)
		if lookup.State != "not_observed" || lookup.Receipt != nil {
			t.Fatal("rolled-back candidate became an observed mutation")
		}
		var planned []byte
		err = b.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('id',id,'input',encode(input,'hex'),'candidate',encode(candidate,'hex'),'semantic_digest',semantic_digest)::text FROM agenteam_skill.assignment_commands WHERE project_id::text=$1 AND agent_id::text=$2 AND idempotency_key=$3 AND state='planned'`, b.project.ID.String(), v.launch.task.agentID.String(), string(original.IdempotencyKey)).Scan(&planned)
		firstRoundRequire(t, err)
		// Only this known NotCommitted call is explicitly repeated. Its original
		// plan is retained. Lose the acknowledgement AFTER the real Store returns
		// Committed; from that point onward this scenario performs Lookup only.
		eligible := false
		var committedCandidate sc.AssignmentMutation
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !matches(cause) {
				return nil
			}
			var final bool
			var err error
			committedCandidate, final, err = readFinal(ctx, tx)
			if err == nil && final {
				eligible = true
			}
			return err
		})
		underlying := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: underlying, attempt: id[f.TransactionAttempt](t), match: func(cause f.TransactionCause) bool { return matches(cause) && eligible }}
		store.fixtureStore = loss
		defer func() { clear(); store.fixtureStore = underlying }()
		_, err = v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, original, b.project.ID, v.launch.task.agentID, v.skill, true)
		clear()
		store.fixtureStore = underlying
		requireCode(t, err, f.CommitUnknown)
		unknown, ok := skill.UnknownAttempt(err)
		fired, commit := loss.observed()
		if !ok || unknown.AttemptID() != loss.attempt || !matches(unknown.Cause()) || !fired || !eligible || commit.State() != f.Committed || !reflect.DeepEqual(tentative, committedCandidate) {
			t.Fatal("receipt loss did not preserve the real commit, original cause and fixed candidate")
		}
		v.requireState(t, v.version+1, [10]int64{1, 0, 1, 1, 1, 1, 1, 0, 1, 1})
		firstRoundRequire(t, v.relations(ctxFor(t), b.raw, committedCandidate, original.IdempotencyKey, false))
		var retained []byte
		err = b.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('id',id,'input',encode(input,'hex'),'candidate',encode(candidate,'hex'),'semantic_digest',semantic_digest)::text FROM agenteam_skill.assignment_commands WHERE project_id::text=$1 AND agent_id::text=$2 AND idempotency_key=$3 AND state='completed'`, b.project.ID.String(), v.launch.task.agentID.String(), string(original.IdempotencyKey)).Scan(&retained)
		firstRoundRequire(t, err)
		if !bytes.Equal(planned, retained) {
			t.Fatal("retry after known rollback replaced the original durable plan")
		}
		lookup, err = v.commands.LookupAssignment(ctxFor(t), b.ownerBrowser.actor, original, v.intent())
		firstRoundRequire(t, err)
		if lookup.State != "committed" || lookup.Receipt == nil || !reflect.DeepEqual(*lookup.Receipt, committedCandidate) || lookup.Receipt.Runtime == nil || lookup.Receipt.Runtime.Queued || lookup.Receipt.Runtime.ExecutionID != nil {
			t.Fatal("original Lookup did not preserve explicit no-active history")
		}
		e := v.launchCreated(t)
		afterLaunch, err := v.commands.LookupAssignment(ctxFor(t), b.ownerBrowser.actor, original, v.intent())
		firstRoundRequire(t, err)
		if !reflect.DeepEqual(afterLaunch, lookup) {
			t.Fatal("later Execution redirected the original no-active assignment")
		}
		v.requireState(t, v.version+1, [10]int64{1, 0, 1, 1, 1, 1, 1, 0, 1, 1})
		firstRoundRequire(t, v.relations(ctxFor(t), b.raw, committedCandidate, original.IdempotencyKey, false))
		v.requireCreatedOnly(t, e)
	})
}
