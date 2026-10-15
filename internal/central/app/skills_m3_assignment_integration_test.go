//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	skc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestSkillsM3AssignmentConfiguration(t *testing.T) {
	TestSkillsM3AppConfiguration(t)
	source, err := postgres.EmbeddedSource()
	skillsAssignmentCheck(t, "skills-assignment-embedded-migrations", err)
	if source.Target() < 128 {
		t.Fatal("Agent assignment migration is missing from the embedded source")
	}
}

// The Agent itself performs the canonical assignment through the deployed Tool.
// Only the fixture Provider's future response is updated with the random Skill
// identity returned by the real install; no domain success is seeded.
func TestSkillsM3AppAgentAssignment(t *testing.T) {
	TestSkillsM3AssignmentConfiguration(t)
	network, err := netfixture.Load()
	skillsAssignmentCheck(t, "skills-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	skillsAssignmentCheck(t, "skills-owned-peer-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range skillsM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, env)
		v.store.observeCommit = skillsAssignmentCommitObserver(t)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	stage := "app"
	defer func() {
		if t.Failed() {
			skillsM3Diagnose(t, v, stage)
			var planned, completed int
			if err := v.admin.QueryRow(v.ctx, `SELECT count(*) FILTER(WHERE state='planned'),count(*) FILTER(WHERE state='completed') FROM agenteam_skill.assignment_commands WHERE project_id=$1 AND actor_kind='agent_run'`, v.a.String()).Scan(&planned, &completed); err == nil {
				t.Logf("SKILLS_ASSIGN canonical_commands planned=%d completed=%d", planned, completed)
			}
		}
	}()
	if transport == nil {
		t.Fatal("original outbound Runtime missing")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	tools := map[string]id.ToolID{}
	for _, name := range []string{"install-skill", "list-skills", "read-skill", "assign-skill"} {
		var raw string
		skillsAssignmentCheck(t, "skills-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, "builtin:"+name).Scan(&raw))
		tools[name], err = f.ParseID[id.Tool](raw)
		skillsAssignmentCheck(t, "skills-tool-id", err)
	}
	install := map[string]any{"mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": skillsM3Text}}}}
	hold := 0
	second := skillsM3ToolReply(t, tools["list-skills"], map[string]any{}, 2)
	second.HoldAfter = &hold
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		skillsM3ToolReply(t, tools["install-skill"], install, 1), second,
		skillsM3TextReply(t, "Future assignment has not been configured.", 3),
		skillsM3ToolReply(t, tools["list-skills"], map[string]any{}, 4),
		skillsM3TextReply(t, "Future read response has not been configured.", 5),
		skillsM3TextReply(t, "Verified the newly assigned checklist.", 6),
	}})
	skillsAssignmentCheck(t, "skills-agent-peer", err)
	defer func() { _ = network.Release(context.Background(), scenario) }()
	model := skillsM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agent := guardID[id.Agent](t)
	disabled, enabled := false, true
	allowed := []id.ToolID{tools["list-skills"], tools["read-skill"], tools["assign-skill"]}
	slices.SortFunc(allowed, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
	intent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agent, Name: "agent-assignment-skills", Description: "Dynamic Skills participant", Instructions: "Install the requested package, use only explicitly assigned skills, and read the checklist before answering.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: allowed, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &enabled, InjectAgentsMD: false})
	skillsAssignmentCheck(t, "skills-agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
	skillsAssignmentCheck(t, "skills-agent-create", err)
	v.agent = agent
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, agent, "Install the checklist, explicitly assign it to yourself, then list and read the assigned skill.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	skillsAssignmentCheck(t, "skills-start-sprint", err)
	v.store.releaseDirectory()
	stage = "original-install"
	state := skillsAssignmentAwaitPeer(t, v, network, scenario, 2)
	var receipt struct {
		SkillID  skc.SkillID `json:"skill_id"`
		Revision f.Revision  `json:"revision"`
		Version  f.Version   `json:"version"`
	}
	skillsAssignmentCheck(t, "skills-original-install-result", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[1].Body)), &receipt))
	if receipt.SkillID.Validate() != nil || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("original installation did not return a canonical Skill identity")
	}
	var assigned int
	skillsAssignmentCheck(t, "skills-install-not-autoassigned", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND skill_id=$3`, v.a.String(), agent.String(), receipt.SkillID.String()).Scan(&assigned))
	if assigned != 0 {
		t.Fatal("installation implicitly assigned the Skill")
	}
	stage = "agent-assignment"
	skillsAssignmentCheck(t, "skills-future-assignment", network.UpdateFutureWire(v.ctx, scenario, 3, skillsM3ToolReply(t, tools["assign-skill"], map[string]any{"action": "assign", "agent_id": agent, "expected_version": f.Version(1), "skill_id": receipt.SkillID, "enabled": true}, 3)))
	skillsAssignmentCheck(t, "skills-future-read", network.UpdateFutureWire(v.ctx, scenario, 5, skillsM3ToolReply(t, tools["read-skill"], map[string]any{"skill_id": receipt.SkillID, "path": "SKILL.md"}, 5)))
	skillsAssignmentCheck(t, "skills-release-original-request", network.Release(v.ctx, scenario))
	stage = "next-round-read"
	execution := memoryM3AwaitExecution(t, v, task.ID)
	stage = "original-join"
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	skillsAssignmentCheck(t, "skills-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "skills-original-app-return")
	skillsAssignmentCheck(t, "skills-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Skills App returned before original owners joined")
	}
	peer := &m1Workflow{app: v, network: network}
	state = peer.joinedPeer(t, scenario, 6, 6)
	var assignmentFault struct {
		Error *struct {
			Code f.Code `json:"code"`
		} `json:"error"`
	}
	skillsAssignmentCheck(t, "skills-agent-tool-result", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[3].Body)), &assignmentFault))
	if assignmentFault.Error != nil {
		t.Fatalf("assign-skill returned original Tool error code=%s", assignmentFault.Error.Code.Safe())
	}
	var assignment skc.AssignmentMutation
	skillsAssignmentCheck(t, "skills-agent-receipt", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[3].Body)), &assignment))
	if assignment.Validate() != nil || !assignment.Changed || assignment.AgentVersion != 2 || assignment.AssignmentSequence != 2 || assignment.Runtime == nil || !assignment.Runtime.Queued || assignment.Runtime.ExecutionID == nil || *assignment.Runtime.ExecutionID != execution {
		t.Fatal("Agent Tool did not return original queued canonical receipt")
	}
	var before, after struct {
		Skills []struct {
			SkillID skc.SkillID `json:"skill_id"`
		} `json:"skills"`
	}
	skillsAssignmentCheck(t, "skills-old-round-list", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[2].Body)), &before))
	skillsAssignmentCheck(t, "skills-new-round-list", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[4].Body)), &after))
	if len(before.Skills) != 0 || len(after.Skills) != 1 || after.Skills[0].SkillID != receipt.SkillID {
		t.Fatal("dynamic assignment changed a captured round or failed to enter its successor")
	}
	var read struct {
		SkillID skc.SkillID `json:"skill_id"`
		Text    string      `json:"text"`
		Error   *struct {
			Code f.Code `json:"code"`
		} `json:"error"`
	}
	skillsAssignmentCheck(t, "skills-model-received-read", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[5].Body)), &read))
	if read.Error != nil {
		t.Fatalf("read-skill returned original Tool error code=%s", read.Error.Code.Safe())
	}
	if read.SkillID != receipt.SkillID || read.Text != skillsM3Text {
		t.Fatal("Model did not receive the actual canonical package text")
	}
	var inputs, controls, pending int
	skillsAssignmentCheck(t, "skills-durable-next-round-inputs", v.admin.QueryRow(v.ctx, `SELECT count(*),count(control_sequence) FROM agenteam_execution.loop_skill_inputs WHERE execution_id=$1`, execution.String()).Scan(&inputs, &controls))
	skillsAssignmentCheck(t, "skills-ingress-consumed", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_execution.skill_assignment_ingress i WHERE i.execution_id=$1 AND i.outcome='queued' AND NOT EXISTS(SELECT 1 FROM agenteam_execution.loop_skill_inputs l, jsonb_array_elements(convert_from(l.content,'UTF8')::jsonb->'dispositions') d WHERE l.execution_id=i.execution_id AND d->>'change_id'=i.change_id AND d->>'state'='applied')`, execution.String()).Scan(&pending))
	if inputs != 5 || controls != 1 || pending != 0 {
		t.Fatalf("Skill round facts inputs=%d controls=%d queued=%d", inputs, controls, pending)
	}
	var readers, liveReaders, liveWork int
	skillsAssignmentCheck(t, "skills-actual-object-reader-joined", v.admin.QueryRow(v.ctx, `SELECT count(*),count(*) FILTER(WHERE l.state<>'released') FROM agenteam_object.object_leases l JOIN agenteam_skill.installations i ON i.object_id=l.object_id WHERE i.skill_id=$1 AND l.owner_kind='reader'`, receipt.SkillID.String()).Scan(&readers, &liveReaders))
	skillsAssignmentCheck(t, "skills-domain-work-joined", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_skill.work WHERE skill_id=$1 AND phase<>'joined'`, receipt.SkillID.String()).Scan(&liveWork))
	if readers == 0 || liveReaders != 0 || liveWork != 0 {
		t.Fatal("canonical package reader or original Skill work did not join")
	}
	var sourceFacts, auditFacts int
	skillsAssignmentCheck(t, "skills-exact-agent-origin", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_skill.assignment_commands c
 JOIN agenteam_tool.operations o ON o.operation_id::text=c.run_origin->>'operation_id'
 JOIN agenteam_tool.attempts a ON a.attempt_id::text=c.run_origin->>'attempt_id' AND a.operation_id=o.operation_id
 WHERE c.project_id=$1 AND c.agent_id=$2 AND c.id=$3 AND c.actor_kind='agent_run' AND c.actor_user_id IS NULL AND c.state='completed'
 AND c.run_origin->>'caller_agent_id'=o.agent_id::text AND c.run_origin->>'execution_id'=o.execution_id::text
 AND c.run_origin->>'request_id'=a.backend_request_id::text AND c.run_origin->>'tool_id'=o.tool_id::text AND c.run_origin->>'spec_revision'=o.spec_revision::text
 AND c.run_origin->>'tool_call_id'=convert_from(o.input_data,'UTF8')::jsonb->>'Payload'
 AND c.run_origin->>'input_digest'=convert_from(o.input_data,'UTF8')::jsonb->>'PayloadDigest'
 AND o.operation_kind='skill.assign' AND o.phase='succeeded' AND a.retired AND a.ordinal=1 AND a.fence=1`, v.a.String(), agent.String(), assignment.ChangeID.String()).Scan(&sourceFacts))
	skillsAssignmentCheck(t, "skills-exact-agent-audit", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_audit.audit_records a JOIN agenteam_skill.assignment_commands c ON c.id::text=a.metadata->>'change_id'
 WHERE c.id=$1 AND a.producer='skill' AND a.action='skill.assign' AND a.actor_kind='agent_run' AND a.resource_kind='agent' AND a.resource_id=$2
 AND a.agent_id::text=c.run_origin->>'caller_agent_id' AND a.actor_execution_id::text=c.run_origin->>'execution_id' AND a.execution_id=a.actor_execution_id
 AND a.operation_id::text=c.run_origin->>'operation_id' AND a.tool_call_id::text=c.run_origin->>'tool_call_id' AND a.tool_id::text=c.run_origin->>'tool_id'`, assignment.ChangeID.String(), agent.String()).Scan(&auditFacts))
	if sourceFacts != 1 || auditFacts != 1 {
		t.Fatalf("Agent source/audit facts source=%d audit=%d", sourceFacts, auditFacts)
	}
	var canonicalVersion, toolVersion, mountVersion, modelVersion int64
	err = v.admin.QueryRow(v.ctx, `SELECT a.version,t.config_version,m.owner_version,r.owner_version FROM agenteam_agent.agents a JOIN agenteam_tool.agent_configurations t ON t.agent_id=a.id::text JOIN agenteam_mount.agent_mount_heads m ON m.agent_id=a.id JOIN agenteam_model.references r ON r.owner_kind='agent' AND r.owner_id=a.id AND r.role='agent_model' WHERE a.project_id=$1 AND a.id=$2`, v.a.String(), agent.String()).Scan(&canonicalVersion, &toolVersion, &mountVersion, &modelVersion)
	if err != nil {
		skillsAssignmentReferenceFacts(t, v, agent)
	}
	skillsAssignmentCheck(t, "skills-four-reference-version", err, v.ctx)
	if canonicalVersion != 2 || toolVersion != 2 || mountVersion != 2 || modelVersion != 2 {
		t.Fatalf("canonical/reference versions agent=%d tool=%d mount=%d model=%d want=2", canonicalVersion, toolVersion, mountVersion, modelVersion)
	}
	skillsAssignmentGuardReleased(t, v.workAppFixture)
	t.Log("M3_SKILLS_AGENT_ASSIGN actual_scheduler_tool=true original_full_source_and_audit=true self_assignment_cas=true captured_input_unchanged=true next_round_skill_input=true canonical_object_read=true original_app_joined=true external_model=false other_target=false permission_revocation=false")
}

func skillsAssignmentCommitObserver(t *testing.T) func(f.TransactionCause, f.CommitResult) {
	original := skillsM3CommitObserver(t)
	var count atomic.Int32
	return func(cause f.TransactionCause, result f.CommitResult) {
		original(cause, result)
		d := cause.Details()
		stage := ""
		if d.Kind == f.CommandsCause && (d.Primary.Command() == "skill.assign" || d.Primary.Command() == "skill.remove") {
			stage = "command"
		}
		if d.Kind == f.JobCause && d.JobType == "skill-assignment-event" {
			stage = "event"
		}
		if result.State() != f.NotCommitted || stage == "" || count.Add(1) > 8 {
			return
		}
		if fault := result.Fault(); fault != nil {
			t.Logf("SKILLS_ASSIGN original_rejected stage=%s code=%s outcome=%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
		}
	}
}
func skillsAssignmentGuardReleased(t *testing.T, v *workAppFixture) {
	t.Helper()
	skillsAssignmentCheck(t, "skills-assignment-guard-release", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	skillsAssignmentCheck(t, "skills-assignment-observer-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		var stopped bool
		var connections int
		err := v.admin.QueryRow(ctx, `SELECT coalesce((SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid),false),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, v.objects.process.String()).Scan(&stopped, &connections)
		skillsAssignmentCheck(t, "skills-assignment-actual-exit", err, ctx)
		if stopped && connections == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Skills original exit unconfirmed stopped=%t connections=%d", stopped, connections)
		case <-tick.C:
		}
	}
}

// Preserve the original 30-second wait and its SQL/peer observations. Only the
// failure projection differs: no SQL, arguments, credentials or body are logged.
func skillsAssignmentAwaitPeer(t *testing.T, v *executionSchedulerApp, network *netfixture.Descriptor, scenario string, requests int) netfixture.State {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	observed := 0
	reject := func(stage string, err error) {
		if err == nil {
			return
		}
		state, code := "none", "none"
		var pg *pgconn.PgError
		if errors.As(err, &pg) && len(pg.Code) == 5 {
			valid := true
			for _, r := range pg.Code {
				if r < '0' || r > '9' && r < 'A' || r > 'Z' {
					valid = false
				}
			}
			if valid {
				state = pg.Code
			}
		}
		var fault *f.Fault
		if errors.As(err, &fault) {
			code = string(fault.Code.Safe())
		}
		t.Fatalf("SKILLS_ASSIGN wait stage=%s elapsed_ms=%d error_deadline=%t error_cancelled=%t context_deadline=%t context_cancelled=%t sqlstate=%s fault=%s project=%s agent=%s peers=%d want=%d", stage, time.Since(started).Milliseconds(), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(ctx.Err(), context.Canceled), state, code, v.a.String(), v.agent.String(), observed, requests)
	}
	for {
		state, err := network.State(ctx, scenario)
		reject("peer-state", err)
		observed = len(state.Requests)
		if observed >= requests {
			return state
		}
		var terminal bool
		err = v.admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1 AND status IN ('failed','cancelled'))`, v.a.String()).Scan(&terminal)
		reject("execution-state", err)
		if terminal {
			t.Fatalf("SKILLS_ASSIGN original Execution terminated elapsed_ms=%d peers=%d want=%d", time.Since(started).Milliseconds(), observed, requests)
		}
		select {
		case <-ctx.Done():
			reject("deadline", ctx.Err())
		case <-tick.C:
		}
	}
}

// Only fixture-owned counts and versions are projected; no business input,
// query text, credentials or arbitrary database error messages are exposed.
func skillsAssignmentReferenceFacts(t *testing.T, v *executionSchedulerApp, agent id.AgentID) {
	t.Helper()
	var raw []byte
	err := v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
 jsonb_build_object('kind','agent','count',(SELECT count(*) FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2),'versions',(SELECT array_agg(version ORDER BY version) FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2)),
 jsonb_build_object('kind','tool','count',(SELECT count(*) FROM agenteam_tool.agent_configurations WHERE project_id::text=$1 AND agent_id::text=$2),'versions',(SELECT array_agg(config_version ORDER BY config_version) FROM agenteam_tool.agent_configurations WHERE project_id::text=$1 AND agent_id::text=$2)),
 jsonb_build_object('kind','mount','count',(SELECT count(*) FROM agenteam_mount.agent_mount_heads WHERE project_id::text=$1 AND agent_id::text=$2),'versions',(SELECT array_agg(owner_version ORDER BY owner_version) FROM agenteam_mount.agent_mount_heads WHERE project_id::text=$1 AND agent_id::text=$2)),
 jsonb_build_object('kind','model','count',(SELECT count(*) FROM agenteam_model.references WHERE project_id::text=$1 AND owner_kind='agent' AND owner_id::text=$2 AND role='agent_model'),'versions',(SELECT array_agg(owner_version ORDER BY owner_version) FROM agenteam_model.references WHERE project_id::text=$1 AND owner_kind='agent' AND owner_id::text=$2 AND role='agent_model')))`, v.a.String(), agent.String()).Scan(&raw)
	if err != nil {
		t.Logf("SKILLS_ASSIGN reference_projection sqlstate=%s no_rows=%t error_deadline=%t error_cancelled=%t context_deadline=%t context_cancelled=%t", skillsAssignmentSQLState(err), errors.Is(err, pgx.ErrNoRows), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), errors.Is(v.ctx.Err(), context.DeadlineExceeded), errors.Is(v.ctx.Err(), context.Canceled))
		return
	}
	type facts struct {
		Kind     string  `json:"kind"`
		Count    int     `json:"count"`
		Versions []int64 `json:"versions"`
	}
	var parsed []facts
	if json.Unmarshal(raw, &parsed) != nil || len(parsed) != 4 {
		t.Log("SKILLS_ASSIGN reference_projection invalid")
		return
	}
	for i, x := range parsed {
		if x.Kind != []string{"agent", "tool", "mount", "model"}[i] || x.Count < 0 || x.Count > 1 || len(x.Versions) > 1 {
			t.Log("SKILLS_ASSIGN reference_projection invalid")
			return
		}
	}
	safe, _ := json.Marshal(parsed)
	t.Logf("SKILLS_ASSIGN reference_projection=%s", safe)
}

func skillsAssignmentSQLState(err error) string {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || len(pg.Code) != 5 {
		return "none"
	}
	for _, r := range pg.Code {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z') {
			return "none"
		}
	}
	return pg.Code
}

func skillsAssignmentCheck(t *testing.T, stage string, err error, contexts ...context.Context) {
	t.Helper()
	if err == nil {
		return
	}
	code := "none"
	var fault *f.Fault
	if errors.As(err, &fault) {
		code = string(fault.Code.Safe())
	}
	var scan pgx.ScanArgError
	scanFailure := errors.As(err, &scan)
	column := -1
	if scanFailure {
		column = scan.ColumnIndex
	}
	var ctxErr error
	if len(contexts) == 1 && contexts[0] != nil {
		ctxErr = contexts[0].Err()
	}
	t.Fatalf("SKILLS_ASSIGN check stage=%s sqlstate=%s no_rows=%t scan=%t column=%d error_deadline=%t error_cancelled=%t context_deadline=%t context_cancelled=%t fault=%s", stage, skillsAssignmentSQLState(err), errors.Is(err, pgx.ErrNoRows), scanFailure, column, errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), errors.Is(ctxErr, context.DeadlineExceeded), errors.Is(ctxErr, context.Canceled), code)
}
