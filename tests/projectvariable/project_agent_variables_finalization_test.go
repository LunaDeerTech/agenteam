//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Earlier participants and accepted Delete remain controlled prerequisites.
// Both pairs below create their own real registered business calls; a row is
// never inserted or marked returned by this fixture. Completing this combined
// participant is not a complete Project Delete or an App registry binding.
type agentVariablesTerminalPG struct {
	*variablesCleanupPG
	agentPair        *agent.ProjectCallOwnership
	otherPair        *agent.ProjectCallOwnership
	before           ac.AgentConfig
	command          ac.AgentCommandID
	prior            []byte
	agentTerminal    *agent.ProjectTerminalCleanup
	variableTerminal *pv.ProjectTerminalCleanup
	closed           sync.Once
	bRead            *terminalOtherRead
	bReturned        bool
	emptyBusiness    bool
	newAgentPair     func() (*agent.ProjectCallOwnership, error)
	extraAgents      []*agent.ProjectCallOwnership
	extraVariables   []*pv.ProjectCallOwnership
}

func newAgentVariablesTerminalPG(t *testing.T, upgrade, foreign bool) *agentVariablesTerminalPG {
	t.Helper()
	return newAgentVariablesTerminalPGShape(t, upgrade, foreign, false)
}
func newAgentVariablesTerminalPGShape(t *testing.T, upgrade, foreign, empty bool) *agentVariablesTerminalPG {
	t.Helper()
	return newAgentVariablesTerminalPGConfigured(t, upgrade, foreign, empty, false, nil)
}

// The Secret slice retains this actual prefix and seeds only through formal
// owners before accepted Delete. Legacy terminal cases keep nil/default ports.
func newAgentVariablesTerminalPGConfigured(t *testing.T, upgrade, foreign, empty, originalSecretAuthority bool, prepare func(*agentVariablesTerminalPG)) *agentVariablesTerminalPG {
	t.Helper()
	v := &agentVariablesTerminalPG{emptyBusiness: empty}
	var bases []*variableHTTPFixture
	if upgrade {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00077"))
		raw := openStore(t, db.Config(t, nil))
		var base *variableHTTPFixture
		t.Cleanup(func() {
			if base != nil && base.afterOwners != nil && !t.Failed() {
				base.afterOwners()
			}
		})
		base = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
		bases = append(bases, base)
	}
	v.variablesCleanupPG = newVariablesCleanupPGConfigured(t, !empty, originalSecretAuthority, func(base *variablesCleanupPG) {
		v.variablesCleanupPG = base
		t.Cleanup(func() { v.close(t) })
		v.prepareAgent(t, foreign)
		if prepare != nil {
			prepare(v)
		}
		if upgrade {
			before, metadata, d04 := v.ownerSnapshot(t), v.metadataSnapshot(t, v.p2.project.ID), v.d04Snapshot(t)
			migrate(t, v.p2.base.db, migrationPrefix(t, "00078"))
			migrate(t, v.p2.base.db, migrationPrefix(t, "00078"))
			if v.ownerSnapshot(t) != before || v.metadataSnapshot(t, v.p2.project.ID) != metadata || v.d04Snapshot(t) != d04 {
				t.Fatal("migration or repeat changed original commands, returned calls or Variables")
			}
			v.activeGuards(t)
		}
	}, func(actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
		for _, pair := range []*agent.ProjectCallOwnership{v.agentPair, v.otherPair} {
			if pair == nil {
				continue
			}
			stopped, err := pair.LocalStopper().RequestStop(ctxFor(t), actor, cause, scope)
			outboxCleanupRequire(t, err)
			if !stopped.Matches(cause, scope) || !stopped.Details().LocalJoined || stopped.Details().PendingCalls != 0 {
				t.Fatal("original Agent pair did not finish its selected Project calls")
			}
			observed, err := pair.ObserveStop(ctxFor(t), actor, cause, scope)
			outboxCleanupRequire(t, err)
			if !observed.Matches(cause, scope) || !observed.Details().RegisteredJoined || observed.Details().PendingCalls != 0 || observed.Details().HasMore {
				t.Fatal("original Agent business return was not confirmed")
			}
		}
	}, bases...)
	return v
}

func (v *agentVariablesTerminalPG) prepareAgent(t *testing.T, foreign bool) {
	t.Helper()
	b := v.p2.base
	source, err := toolruntime.NewInstallSource(v.installer, v.p2.service)
	outboxCleanupRequire(t, err)
	lifecycle, err := project.NewLifecycleAuthority(b.tracked, v.p2.manifest, nil)
	outboxCleanupRequire(t, err)
	graph, err := assembly.New(b.tracked, assembly.Options{Accounts: b.accounts, Lifecycle: lifecycle, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	outboxCleanupRequire(t, err)
	configuration := &agentCreateFixture{p2: v.p2, guard: v.guard, providers: graph.Providers()}
	b.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if err := v.p2.service.RequireInstallBindingInTx(ctx, tx, v.installer, v.process); err != nil {
			return err
		}
		registration, err := source.DescribeInTx(ctx, tx)
		if err != nil {
			return err
		}
		if err = source.CheckBindingInTx(ctx, tx, registration); err != nil {
			return err
		}
		configuration.tool, err = configuration.providers.Registry.ReconcileBuiltinInTx(ctx, tx, builtin.SkillInstallStableKey)
		return err
	})
	processes := captureProviderProcesses{guard: v.guard}
	v.newAgentPair = func() (*agent.ProjectCallOwnership, error) {
		return graph.NewProjectCallOwnership(b.keys, processes, v.guard, v.process)
	}
	v.agentPair, err = v.newAgentPair()
	outboxCleanupRequire(t, err)
	v.consumer = v.agentConsumer(t, graph.Providers().Agents)
	if v.emptyBusiness {
		return
	}
	configuration.modelID = configuration.createModel(t, fixtureProcess{processes.CurrentProcess()})
	var secretID string
	outboxCleanupRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='secret' AND deleted_at IS NULL`, v.p2.project.ID.String()).Scan(&secretID))
	secret, err := f.ParseID[i.ProjectVariable](secretID)
	outboxCleanupRequire(t, err)
	enabled, disabled := true, false
	target := id[i.Agent](t)
	request, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: target, Name: "terminal-agent", Description: "Original registered owner", Instructions: "No Execution or Model request starts", ModelRef: configuration.modelID, ApprovalPolicy: ac.ApprovalAuto, ApprovalModelRef: &configuration.modelID, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{secret}, AddSkillsEnabled: &enabled, InstallSkillEnabled: &disabled})
	outboxCleanupRequire(t, err)
	receipt, err := v.agentPair.Service().CreateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "terminal-owner-agent-create", nil), v.p2.project.ID, request)
	outboxCleanupRequire(t, err)
	if receipt.Validate() != nil || receipt.Fields().Agent.Fields().Core.ID != target {
		t.Fatal("actual owned Agent creation did not return its complete configuration")
	}
	v.before = receipt.Fields().Agent.Clone()
	current, err := v.agentPair.Service().GetAgent(ctxFor(t), b.ownerBrowser.actor, v.p2.project.ID, target)
	outboxCleanupRequire(t, err)
	directory, err := v.agentPair.DirectoryReader().GetAgent(ctxFor(t), b.ownerBrowser.actor, v.p2.project.ID, target)
	outboxCleanupRequire(t, err)
	if !reflect.DeepEqual(current.Fields(), v.before.Fields()) || directory.Validate() != nil || directory.ID != target {
		t.Fatal("original Agent service/directory reads changed their actual target")
	}
	page, err := v.agentPair.DirectoryReader().ListAgents(ctxFor(t), b.ownerBrowser.actor, b.project.ID, f.PageRequest{Limit: 10})
	outboxCleanupRequire(t, err)
	if len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("other Project's real Agent directory changed")
	}
	encoded, err := json.Marshal(v.before)
	outboxCleanupRequire(t, err)
	var command string
	outboxCleanupRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_agent.commands WHERE project_id=$1 AND target_id=$2 AND state='completed' AND after_config=$3::jsonb`, v.p2.project.ID.String(), target.String(), encoded).Scan(&command))
	v.command, err = f.ParseID[ac.AgentCommand](command)
	outboxCleanupRequire(t, err)
	if foreign {
		// The second instance is real and returns its original read. It remains
		// in this live process, so the first pair has no stopped-owner proof.
		v.otherPair, err = graph.NewProjectCallOwnership(b.keys, processes, v.guard, v.process)
		outboxCleanupRequire(t, err)
		_, err = v.otherPair.Service().GetAgent(ctxFor(t), b.ownerBrowser.actor, v.p2.project.ID, target)
		outboxCleanupRequire(t, err)
	}
	var service, reads, foreignCount, commands, pending int
	err = b.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1 AND lane='service' AND phase='joined'),
 (SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1 AND lane='directory' AND phase='joined'),
 (SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$2 AND phase='joined'),
 (SELECT count(*) FROM agenteam_agent.commands WHERE project_id=$1 AND state='completed'),
 (SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id IN ($1,$2) AND phase!='joined')`, v.p2.project.ID.String(), b.project.ID.String()).Scan(&service, &reads, &foreignCount, &commands, &pending)
	outboxCleanupRequire(t, err)
	want := 2
	if foreign {
		want++
	}
	if service != want || reads != 1 || foreignCount != 1 || commands != 1 || pending != 0 {
		t.Fatal("formal Agent commands and original returned ledger differ", service, reads, foreignCount, commands, pending)
	}
}

func (v *agentVariablesTerminalPG) ownerSnapshot(t *testing.T) string {
	t.Helper()
	var out string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_agent.commands x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_agent.project_calls x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.project_calls x))::text`).Scan(&out)
	outboxCleanupRequire(t, err)
	return out
}

func (v *agentVariablesTerminalPG) close(t *testing.T) {
	t.Helper()
	v.closed.Do(func() {
		if v.variablesCleanupPG == nil {
			return
		}
		if v.barrier != nil {
			v.barrier.open()
		}
		if v.bRead != nil {
			v.bRead.open()
			v.bRead.cancel()
		}
		// Releasing a test barrier must let its original confirmation return
		// before global Stop can cancel it and correctly retain an Unknown.
		for _, call := range v.calls {
			outboxCleanupWait(t, call.done, "original terminal driver before owner cleanup")
		}
		for _, driver := range v.drivers {
			driver.Stop()
		}
		if v.bRead != nil {
			v.bRead.open()
			v.bRead.cancel()
			outboxCleanupWait(t, v.bRead.done, "original other-Project directory return")
		}
		store := v.p2.base.tracked
		store.mu.Lock()
		store.beforeLocks = nil
		store.mu.Unlock()
		for _, pair := range append(v.extraAgents, v.otherPair, v.agentPair) {
			if pair != nil {
				if !drainAgentCallPair(t, pair) {
					// Retain the original guard/runtime; never convert this into a join.
					select {}
				}
			}
		}
		for _, pair := range v.extraVariables {
			variableOwnershipDrain(t, pair)
		}
		v.variablesCleanupPG.close(t)
	})
}

// These are negative transactions only. They consume no private issuer and
// must leave the actual pre-migration business facts byte-for-byte unchanged.
func (v *agentVariablesTerminalPG) activeGuards(t *testing.T) {
	t.Helper()
	before := v.ownerSnapshot(t)
	for _, query := range []string{
		`DELETE FROM agenteam_agent.commands WHERE project_id=$1`,
		`DELETE FROM agenteam_agent.project_calls WHERE project_id=$1`,
		`DELETE FROM agenteam_projectvariable.project_calls WHERE project_id=$1`,
	} {
		key, _ := f.ProjectLock(v.p2.project.ID.String())
		reached := false
		result := v.p2.base.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			if err := v.p2.base.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
				return err
			}
			x, err := v.p2.base.tracked.InTx(tx)
			if err != nil {
				return err
			}
			reached = true
			if _, err = x.Exec(ctx, query, v.p2.project.ID.String()); err != nil {
				return err
			}
			return errors.New("ordinary deletion unexpectedly bypassed the terminal guard")
		})
		var pg *pgconn.PgError
		if !reached || result.State() != f.NotCommitted || !errors.As(result.Fault(), &pg) || pg.Code != "23514" || v.ownerSnapshot(t) != before {
			agentRetirementDiagnostic(t, "terminal-active-schema-guard", result.Fault())
			t.Fatal("active Project terminal data was not protected by the actual database guard")
		}
	}
}

func (v *agentVariablesTerminalPG) driver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	var err error
	if v.agentTerminal == nil {
		v.agentTerminal, err = agent.NewProjectTerminalCleanup(v.agentPair, v.consumer)
		outboxCleanupRequire(t, err)
		v.variableTerminal, err = pv.NewProjectTerminalCleanup(v.pair, v.projects)
		outboxCleanupRequire(t, err)
	}
	d, err := project.NewLifecycleCleanupDriverWithTerminal(v.p2.base.tracked, v.projects, captureProviderProcesses{guard: v.guard}, v.consumer, agentRetirementSkillsDiagnostic{SkillsCleanupDomain: v.tap, t: t}, v.variables, v.agentTerminal, v.variableTerminal)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, d)
	return d
}

type terminalPGState struct {
	Schema                          int64
	Raw                             []byte
	Parent, Phase, Process, Attempt string
	Fence                           int64
}

func (v *agentVariablesTerminalPG) state(t *testing.T) terminalPGState {
	t.Helper()
	var out terminalPGState
	var lifecycle, operation, stage string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT p.lifecycle,o.state,o.cleanup_stage,r.cleanup_state,r.checkpoint_schema,r.checkpoint,w.phase,w.process_id::text,w.attempt_id::text,w.fence FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id JOIN agenteam_project.lifecycle_participants r ON r.operation_id=o.id AND r.participant_name='agent-skills-variables' JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id WHERE p.id=$1 AND o.id=$2`, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&lifecycle, &operation, &stage, &out.Parent, &out.Schema, &out.Raw, &out.Phase, &out.Process, &out.Attempt, &out.Fence)
	outboxCleanupRequire(t, err)
	if lifecycle != "deleting" || operation != "cleaning" || stage != "domains" || out.Process != v.process.String() || out.Fence < 1 {
		t.Fatal("terminal owner changed the original Project operation or process")
	}
	return out
}
func terminalPGDigest(raw []byte) f.Digest {
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// Decode only documented wire fields, independently of the production codec.
func (v *agentVariablesTerminalPG) pending(t *testing.T, state terminalPGState) (bool, string, string) {
	t.Helper()
	if state.Parent != "pending" || state.Phase != "terminal" {
		t.Fatal("prerequisite did not return with pending parent and terminal claim")
	}
	agents := state.Raw
	variables := ""
	if state.Schema == 5 {
		var cp struct {
			Project   string    `json:"project_id"`
			Operation string    `json:"operation_id"`
			Version   f.Version `json:"project_version"`
			Schema    f.Version `json:"agents_schema"`
			Agents    []byte    `json:"agents_checkpoint"`
			State     string    `json:"variables_state"`
		}
		outboxCleanupRequire(t, json.Unmarshal(state.Raw, &cp))
		if cp.Project != v.p2.project.ID.String() || cp.Operation != v.cause.OperationID.String() || cp.Version != v.cause.ProjectVersion || cp.Schema != 3 || len(cp.Agents) == 0 || cp.State != "pending" && cp.State != "completed" {
			t.Fatal("Variables prerequisite lost its complete Agent checkpoint")
		}
		agents, variables = cp.Agents, cp.State
	} else if state.Schema != 3 {
		t.Fatal("unexpected complete original Agent checkpoint schema")
	}
	var cp struct {
		Project   string    `json:"project_id"`
		Operation string    `json:"operation_id"`
		Version   f.Version `json:"project_version"`
		Done      bool      `json:"agents_done"`
		Applied   *struct {
			Agent   i.AgentID         `json:"agent_id"`
			Version f.Version         `json:"version"`
			Command ac.AgentCommandID `json:"command_id"`
			Digest  f.Digest          `json:"before_digest"`
		} `json:"agent_applied"`
		Skills struct {
			State string `json:"skills_state"`
		} `json:"skills_progress"`
	}
	outboxCleanupRequire(t, json.Unmarshal(agents, &cp))
	if cp.Project != v.p2.project.ID.String() || cp.Operation != v.cause.OperationID.String() || cp.Version != v.cause.ProjectVersion || cp.Skills.State != "pending" && cp.Skills.State != "completed" {
		t.Fatal("original complete Before anchor lost its Project or Skills scope")
	}
	if v.emptyBusiness {
		if cp.Applied != nil || !cp.Done {
			t.Fatal("empty Agent enumeration manufactured an immutable command anchor")
		}
	} else {
		before, err := json.Marshal(v.before)
		outboxCleanupRequire(t, err)
		if cp.Applied == nil || cp.Applied.Agent != v.before.Fields().Core.ID || cp.Applied.Version != v.before.Fields().Core.Version || cp.Applied.Command != v.command || cp.Applied.Digest != terminalPGDigest(before) {
			t.Fatal("original complete Before anchor was lost before terminal replacement")
		}
	}
	if variables != "" && (!cp.Done || cp.Skills.State != "completed") {
		t.Fatal("Variables overtook preceding original returns")
	}
	return cp.Done, cp.Skills.State, variables
}
func (v *agentVariablesTerminalPG) ready(t *testing.T, d *project.LifecycleCleanupDriver) {
	t.Helper()
	originalOwners := v.ownerSnapshot(t)
	want := v.counts(t)
	agentRounds, skillRounds, variableRounds := 0, 0, 0
	wantAgents, wantVariables := 2, 8
	if v.emptyBusiness {
		wantAgents, wantVariables = 1, 1
	}
	for round := 0; round < 34; round++ {
		oldSkills, oldVariables := v.tap.calls.Load(), v.variables.calls.Load()
		call := v.run(t, d)
		outboxCleanupWait(t, call.done, "original combined prerequisite return")
		agentRetirementDiagnostic(t, "terminal-prerequisite-return", call.err)
		outboxCleanupRequire(t, call.err)
		state := v.state(t)
		done, skills, variables := v.pending(t, state)
		switch {
		case v.variables.calls.Load() == oldVariables+1:
			variableRounds++
			wasEmpty := want == [7]int{}
			for index, n := range want {
				if n != 0 {
					want[index] = 0
					break
				}
			}
			if (variables == "completed") != wasEmpty {
				t.Fatal("metadata completion did not follow its actual empty confirmation")
			}
		case v.tap.calls.Load() == oldSkills+1:
			skillRounds++
		default:
			agentRounds++
		}
		if agentRounds > 2 || skillRounds > 24 || variableRounds > 8 || v.counts(t) != want || v.ownerSnapshot(t) != originalOwners {
			t.Fatal("prerequisite exceeded its existing bound or prematurely removed terminal data")
		}
		v.retained(t)
		if variables == "completed" {
			if !done || skills != "completed" || state.Schema != 5 || want != [7]int{} || agentRounds != wantAgents || variableRounds != wantVariables {
				t.Fatal("terminal step lacked actual completed predecessors")
			}
			v.empty(t)
			v.prior = bytes.Clone(state.Raw)
			return
		}
	}
	t.Fatal("original bounded predecessor rounds did not reach terminal cleanup")
}

type terminalPGReceipt struct {
	Project     string                          `json:"project_id"`
	Operation   string                          `json:"operation_id"`
	Version     f.Version                       `json:"project_version"`
	Process     string                          `json:"process_id"`
	Attempt     string                          `json:"attempt_id"`
	Fence       int64                           `json:"fence"`
	Transaction string                          `json:"transaction_id"`
	Previous    f.Digest                        `json:"previous_checkpoint_digest"`
	Agent       pc.ProjectTerminalOwnerIdentity `json:"agent_owner"`
	Variables   pc.ProjectTerminalOwnerIdentity `json:"variables_owner"`
}

func (v *agentVariablesTerminalPG) receipt(t *testing.T) terminalPGState {
	t.Helper()
	state := v.state(t)
	var cp terminalPGReceipt
	outboxCleanupRequire(t, json.Unmarshal(state.Raw, &cp))
	if state.Schema != 6 || state.Parent != "completed" || state.Phase != "terminal" || cp.Project != v.p2.project.ID.String() || cp.Operation != v.cause.OperationID.String() || cp.Version != v.cause.ProjectVersion || cp.Process != state.Process || cp.Attempt != state.Attempt || cp.Fence != state.Fence || cp.Transaction == "" || cp.Previous != terminalPGDigest(v.prior) || cp.Agent != v.agentTerminal.ProjectTerminalCleanupIdentity() || cp.Variables != v.variableTerminal.ProjectTerminalCleanupIdentity() {
		t.Fatal("atomic terminal receipt lost its original claim, owners or previous checkpoint")
	}
	var remaining int
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_agent.commands WHERE project_id=$1)+(SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1)+(SELECT count(*) FROM agenteam_projectvariable.project_calls WHERE project_id=$1)`, v.p2.project.ID.String()).Scan(&remaining)
	outboxCleanupRequire(t, err)
	if remaining != 0 || v.counts(t) != [7]int{} {
		t.Fatal("terminal receipt did not atomically retire its real command and both ledgers")
	}
	v.preserved(t)
	return state
}
func (v *agentVariablesTerminalPG) preserved(t *testing.T) {
	t.Helper()
	if v.d04Snapshot(t) != v.d04 || v.metadataSnapshot(t, v.p2.base.project.ID) != v.foreignVariables {
		t.Fatal("terminal cleanup changed D04 or another Project's Variables")
	}
	v.isolated(t)
	var agents, running, variables int
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1 AND phase='joined'),(SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1 AND phase='running'),(SELECT count(*) FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND phase='business_closed')`, v.p2.base.project.ID.String()).Scan(&agents, &running, &variables)
	outboxCleanupRequire(t, err)
	wantJoined, wantRunning, wantVariables := 1, 0, 4
	if v.emptyBusiness {
		wantJoined, wantVariables = 0, 0
	}
	if v.bRead != nil {
		if v.bReturned {
			wantJoined++
		} else {
			wantRunning++
		}
	}
	if agents != wantJoined || running != wantRunning || variables != wantVariables {
		t.Fatal("terminal cleanup lost another Project's actual original calls")
	}
}
func (v *agentVariablesTerminalPG) lifecycle(cause f.TransactionCause) bool {
	d := cause.Details()
	return cause.Kind() == f.JobCause && d.JobType == "project-lifecycle" && d.JobID == v.cause.OperationID.String()
}

// Observe the real caller Tx after both deletions and its receipt checkpoint,
// never manufacture an outcome or issue authority from the visible row.
type terminalPGObservation struct {
	applied     atomic.Bool
	transaction atomic.Value
}

func (v *agentVariablesTerminalPG) observeTerminal(t *testing.T, reject ...error) *terminalPGObservation {
	t.Helper()
	seen := &terminalPGObservation{}
	store := v.p2.base.tracked
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.lifecycle(cause) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var applied bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='agent-skills-variables' AND cleanup_state='completed' AND checkpoint_schema=6)`, v.cause.OperationID.String()).Scan(&applied)
		if err == nil && applied && !seen.applied.Load() {
			var transaction string
			if err = x.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&transaction); err == nil {
				seen.transaction.Store(transaction)
				seen.applied.Store(true)
				if len(reject) == 1 {
					return reject[0]
				}
			}
		}
		return err
	})
	return seen
}

// This is the original B directory call, registered by the same pair. The
// second AcquireAll is its business transaction, after the registration Tx
// returned. Holding it must not make A's project-scoped terminal check global.
type terminalOtherReadKey struct{}
type terminalOtherRead struct {
	entered, release, done chan struct{}
	once                   sync.Once
	cancel                 context.CancelFunc
	err                    error
	page                   f.Page[ac.DirectoryEntry]
}

func (r *terminalOtherRead) open() { r.once.Do(func() { close(r.release) }) }
func (v *agentVariablesTerminalPG) holdOtherRead(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	r := &terminalOtherRead{entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{}), cancel: cancel}
	v.bRead = r
	store := v.p2.base.tracked
	var locks atomic.Int32
	store.mu.Lock()
	store.beforeLocks = func(ctx context.Context, _ f.Tx, _ []f.LockRequest) error {
		if ctx.Value(terminalOtherReadKey{}) != r || locks.Add(1) != 2 {
			return nil
		}
		close(r.entered)
		<-r.release
		return ctx.Err()
	}
	store.mu.Unlock()
	go func() {
		defer close(r.done)
		defer cancel()
		r.page, r.err = v.agentPair.DirectoryReader().ListAgents(context.WithValue(ctx, terminalOtherReadKey{}, r), v.p2.base.ownerBrowser.actor, v.p2.base.project.ID, f.PageRequest{Limit: 10})
	}()
	wait, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	select {
	case <-r.entered:
	case <-r.done:
		agentRetirementDiagnostic(t, "terminal-other-read-before-business", r.err)
		t.Fatal("other Project directory call returned before its actual business barrier")
	case <-wait.Done():
		t.Fatal("other Project's actual business barrier was not reached")
	}
	v.preserved(t)
	v.requireOtherReadHeld(t)
}

// A returned business call is established by its original function and durable
// joined record. Agent.Joined additionally requires a global Stop, so it is
// intentionally used only by the final real Stop/Drain cleanup helper.
func (v *agentVariablesTerminalPG) requireOtherReadHeld(t *testing.T) {
	t.Helper()
	select {
	case <-v.bRead.done:
		t.Fatal("B original reader returned before its release")
	default:
	}
}

// These public request fields supply no Project authority. The call must be
// rejected by the exact concrete owner's already-active/retained private call,
// before either authorization SQL or this deliberately inert callback can run.
func (v *agentVariablesTerminalPG) requireAgentTerminalRetained(t *testing.T, want f.Code) {
	t.Helper()
	registration, err := i.RegisterService(i.ProjectLifecycle)
	outboxCleanupRequire(t, err)
	scope, err := i.InProject(v.p2.project.ID)
	outboxCleanupRequire(t, err)
	actor, err := registration.Actor(v.cause.OperationID.String(), scope)
	outboxCleanupRequire(t, err)
	req := pc.ProjectTerminalCleanupRequest{Actor: actor, Cause: v.cause, ProjectID: v.p2.project.ID, Owner: v.agentTerminal.ProjectTerminalCleanupIdentity()}
	outboxCleanupRequire(t, req.Validate())
	called := false
	result, err := v.agentTerminal.WithProjectTerminalCleanup(ctxFor(t), req, func(context.Context) (f.CommitResult, error) {
		called = true
		return f.NotCommittedResult(nil), errors.New("unexpected retained-owner admission")
	})
	var problem *f.Fault
	if called || result.State() != f.NotCommitted || !errors.As(err, &problem) || problem.Code != want {
		agentRetirementDiagnostic(t, "terminal-original-owner-reentry", err)
		t.Fatal("original Agent terminal owner did not retain its exact private call")
	}
}

func (v *agentVariablesTerminalPG) releaseOtherRead(t *testing.T) {
	t.Helper()
	v.bRead.open()
	outboxCleanupWait(t, v.bRead.done, "original other-Project directory call")
	outboxCleanupRequire(t, v.bRead.err)
	if len(v.bRead.page.Items) != 0 || v.bRead.page.NextCursor != "" {
		t.Fatal("other Project directory changed during A terminal cleanup")
	}
	v.bReturned = true
	v.preserved(t)
}
func (v *agentVariablesTerminalPG) observedReceipt(t *testing.T, seen *terminalPGObservation) terminalPGState {
	t.Helper()
	state := v.receipt(t)
	var cp terminalPGReceipt
	outboxCleanupRequire(t, json.Unmarshal(state.Raw, &cp))
	transaction, ok := seen.transaction.Load().(string)
	if !seen.applied.Load() || !ok || transaction == "" || cp.Transaction != transaction {
		t.Fatal("terminal receipt did not bind the actual original database transaction")
	}
	return state
}

func TestProjectAgentVariablesFinalization(t *testing.T) {
	t.Run("terminal-receipt-and-original-return", testAgentVariablesTerminalReturn)
	t.Run("original-unknown-and-receipt-confirmation", testAgentVariablesTerminalUnknown)
	t.Run("foreign-live-instance-retains-terminal-data", testAgentVariablesTerminalForeign)
	t.Run("empty-not-applied-original-return-fences-new-pairs", testAgentVariablesTerminalEmptyConfirmation)
}
func testAgentVariablesTerminalReturn(t *testing.T) {
	v := newAgentVariablesTerminalPG(t, true, false)
	driver := v.driver(t)
	v.ready(t, driver)
	v.holdOtherRead(t)
	seen := v.observeTerminal(t)
	v.barrier = newSchedulerStopBarrier()
	defer v.barrier.open()
	store := v.p2.base.tracked
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if v.lifecycle(c) && seen.applied.Load() && r.State() == f.Committed {
			v.barrier.hold(r)
		}
	}
	store.mu.Unlock()
	call := v.run(t, driver)
	wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case r := <-v.barrier.entered:
		if r.State() != f.Committed {
			t.Fatal("original terminal transaction was not committed")
		}
	case <-call.done:
		agentRetirementDiagnostic(t, "terminal-before-original-return-barrier", call.err)
		t.Fatal("terminal call returned before its original Store-return barrier")
	case <-wait.Done():
		t.Fatal("terminal original Store-return barrier was not reached")
	}
	committed := v.observedReceipt(t, seen)
	v.requireAgentTerminalRetained(t, f.ResourceBusy)
	if v.pair.Joined() {
		t.Fatal("physical terminal commit released original owner controls")
	}
	select {
	case <-v.bRead.done:
		t.Fatal("B original read returned before being released")
	default:
	}
	driver.Stop()
	bounded, end := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := driver.Drain(bounded)
	end()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("terminal driver drained before its original Store call returned", err)
	}
	select {
	case <-call.done:
		t.Fatal("held terminal original call returned early")
	default:
	}
	v.barrier.open()
	outboxCleanupWait(t, call.done, "cancelled original terminal transaction return")
	if call.err == nil {
		t.Fatal("cancelled original terminal callback reported success")
	}
	outboxCleanupRequire(t, driver.Drain(ctxFor(t)))
	v.clearHooks()
	v.requireOtherReadHeld(t)
	if !v.pair.Joined() {
		t.Fatal("A controls or the independently held B reader lost their original return boundaries")
	}
	// A new driver confirms the durable typed receipt; the original immutable
	// Agent command is already absent and cannot supply a recovery anchor.
	call = v.run(t, v.driver(t))
	outboxCleanupWait(t, call.done, "actual terminal receipt confirmation")
	agentRetirementDiagnostic(t, "terminal-confirm-after-cancel", call.err)
	outboxCleanupRequire(t, call.err)
	if state := v.receipt(t); !reflect.DeepEqual(state, committed) {
		t.Fatal("receipt confirmation rewrote its original transaction or claim")
	}
	v.requireOtherReadHeld(t)
	v.releaseOtherRead(t)
	if !v.pair.Joined() {
		t.Fatal("original pairs failed to join after all real calls returned")
	}
	v.close(t)
	t.Log("PROJECT_AGENT_VARIABLES_TERMINAL_RETURN migration77_to78_repeat=true ordinary_deletes_rejected=true real_agent_and_variable_pairs=true actual_predecessors=true commands_ledgers_receipt_same_tx=true original_return_held=true other_project_active=true receipt_only_confirmation=true parent_completed_domains=true")
}
func testAgentVariablesTerminalUnknown(t *testing.T) {
	v := newAgentVariablesTerminalPG(t, false, false)
	driver := v.driver(t)
	v.ready(t, driver)
	store := v.p2.base.tracked
	// Execute the real terminal callback, then force its original caller Tx
	// to roll back. Only the returned acknowledgement is projected to Unknown.
	// A private NotApplied confirmation must not consume the still-needed rows.
	rollback := errors.New("controlled terminal callback rollback")
	notApplied := v.observeTerminal(t, rollback)
	unappliedAttempt := id[f.TransactionAttempt](t)
	var rollbackMasked atomic.Bool
	var rolledBack f.CommitResult
	var rollbackCause f.TransactionCause
	beforeOwners, beforeState := v.ownerSnapshot(t), v.state(t)
	store.mu.Lock()
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if v.lifecycle(c) && notApplied.applied.Load() && r.State() == f.NotCommitted && rollbackMasked.CompareAndSwap(false, true) {
			rolledBack, rollbackCause = r, c
			return f.UnknownResult(unappliedAttempt, c)
		}
		return r
	}
	store.mu.Unlock()
	unapplied := v.run(t, driver)
	outboxCleanupWait(t, unapplied.done, "original rolled-back terminal acknowledgement Unknown")
	var unappliedFault *f.Fault
	if !rollbackMasked.Load() || rolledBack.State() != f.NotCommitted || !errors.Is(rolledBack.Fault(), rollback) || !v.lifecycle(rollbackCause) || !errors.As(unapplied.err, &unappliedFault) || unappliedFault.Code != f.CommitUnknown || unappliedFault.CauseID != unappliedAttempt.String() || unappliedFault.CommitState != f.Unknown {
		agentRetirementDiagnostic(t, "terminal-original-rollback-unknown", unapplied.err)
		t.Fatal("original terminal rollback or local Unknown identity was lost")
	}
	v.requireAgentTerminalRetained(t, f.InvalidState)
	running := v.state(t)
	if running.Schema != 5 || running.Parent != "pending" || running.Phase != "running" || !bytes.Equal(running.Raw, beforeState.Raw) || v.ownerSnapshot(t) != beforeOwners || v.pair.Joined() {
		t.Fatal("rolled-back Unknown changed metadata or discarded its original controls")
	}
	v.preserved(t)
	v.clearHooks()
	unapplied = v.run(t, driver)
	outboxCleanupWait(t, unapplied.done, "actual terminal NotApplied confirmation")
	agentRetirementDiagnostic(t, "terminal-not-applied-confirmation", unapplied.err)
	outboxCleanupRequire(t, unapplied.err)
	confirmed := v.state(t)
	if confirmed.Schema != 5 || confirmed.Parent != "pending" || confirmed.Phase != "running" || confirmed.Attempt != running.Attempt || confirmed.Fence != running.Fence || !bytes.Equal(confirmed.Raw, beforeState.Raw) || v.ownerSnapshot(t) != beforeOwners || !v.pair.Joined() {
		t.Fatal("NotApplied confirmation mutated data, replaced its claim or did not join original controls")
	}
	v.preserved(t)
	// Only a subsequent explicit Run may apply. Its physical Commit is now
	// acknowledged Unknown, exercising the distinct receipt-only branch.
	seen := v.observeTerminal(t)
	attempt := id[f.TransactionAttempt](t)
	var masked atomic.Bool
	var original f.TransactionCause
	store.mu.Lock()
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if v.lifecycle(c) && seen.applied.Load() && r.State() == f.Committed && masked.CompareAndSwap(false, true) {
			original = c
			return f.UnknownResult(attempt, c) // In-process projection after actual Commit; no network fault.
		}
		return r
	}
	store.mu.Unlock()
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "terminal original acknowledgement Unknown")
	var fault *f.Fault
	if !masked.Load() || !v.lifecycle(original) || !errors.As(call.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != attempt.String() {
		agentRetirementDiagnostic(t, "terminal-original-unknown", call.err)
		t.Fatal("terminal acknowledgement Unknown lost its original local identity")
	}
	committed := v.observedReceipt(t, seen)
	v.requireAgentTerminalRetained(t, f.InvalidState)
	if v.pair.Joined() {
		t.Fatal("Unknown discarded its original Agent or Variables controls")
	}
	v.clearHooks()
	call = v.run(t, driver)
	outboxCleanupWait(t, call.done, "terminal Unknown receipt-only confirmation")
	agentRetirementDiagnostic(t, "terminal-unknown-confirmation", call.err)
	outboxCleanupRequire(t, call.err)
	if !v.pair.Joined() {
		t.Fatal("actual confirmation return did not retire original Unknown controls")
	}
	if state := v.receipt(t); !reflect.DeepEqual(state, committed) {
		t.Fatal("Unknown confirmation replaced the original terminal receipt")
	}
	// A later replay also uses the same persisted receipt and no deleted command.
	call = v.run(t, v.driver(t))
	outboxCleanupWait(t, call.done, "later original terminal receipt replay")
	outboxCleanupRequire(t, call.err)
	if state := v.receipt(t); !reflect.DeepEqual(state, committed) {
		t.Fatal("terminal replay changed its immutable receipt")
	}
	v.close(t)
	t.Log("PROJECT_AGENT_VARIABLES_TERMINAL_UNKNOWN actual_rollback_ackmask=true not_applied_readonly_confirmation=true subsequent_explicit_apply=true local_committed_ackmask=true exact_original_attempt_cause=true original_both_pair_controls_retained=true deleted_command_not_required=true receipt_only_actual_confirmation=true d04_and_other_project_retained=true parent_completed_domains=true")
}
func testAgentVariablesTerminalForeign(t *testing.T) {
	v := newAgentVariablesTerminalPG(t, false, true)
	driver := v.driver(t)
	v.ready(t, driver)
	before, state := v.ownerSnapshot(t), v.state(t)
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "original live foreign-instance terminal refusal")
	var busy *f.Fault
	if !errors.As(call.err, &busy) || busy.Code != f.ResourceBusy {
		agentRetirementDiagnostic(t, "terminal-foreign-live-instance", call.err)
		t.Fatal("live process with another real instance was accepted as the original stopped owner")
	}
	after := v.state(t)
	if after.Schema != 5 || after.Parent != "pending" || !bytes.Equal(after.Raw, state.Raw) || v.ownerSnapshot(t) != before || v.counts(t) != [7]int{} {
		t.Fatal("foreign owner refusal removed terminal data or changed parent progress")
	}
	if !v.pair.Joined() {
		t.Fatal("known foreign-owner refusal detached an original call")
	}
	v.preserved(t)
	v.close(t)
	t.Log("PROJECT_AGENT_VARIABLES_TERMINAL_FOREIGN actual_same_process_other_instance=true real_returned_ledger=true no_stopped_proof=true resource_busy=true original_commands_and_ledgers_retained=true parent_pending_domains=true")
}

func testAgentVariablesTerminalEmptyConfirmation(t *testing.T) {
	// No business call is made through either pair. The Project and protected
	// Skill/Object initialization are real; all original cleanup rounds still
	// execute. Empty rows alone must never become proof of the original return.
	v := newAgentVariablesTerminalPGShape(t, false, false, true)
	driver := v.driver(t)
	v.ready(t, driver)
	var rows int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_agent.agents WHERE project_id=$1)+(SELECT count(*) FROM agenteam_agent.commands WHERE project_id=$1)+(SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1)+(SELECT count(*) FROM agenteam_projectvariable.project_calls WHERE project_id=$1)`, v.p2.project.ID.String()).Scan(&rows))
	if rows != 0 {
		t.Fatal("empty-owner race prerequisite contained an Agent anchor or registered call")
	}
	beforeOwners := v.ownerSnapshot(t)
	rollback := errors.New("controlled empty terminal rollback")
	seen := v.observeTerminal(t, rollback)
	store := v.p2.base.tracked
	attempt := id[f.TransactionAttempt](t)
	var masked atomic.Bool
	var original f.TransactionCause
	var physical f.CommitResult
	store.mu.Lock()
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if v.lifecycle(c) && seen.applied.Load() && r.State() == f.NotCommitted && masked.CompareAndSwap(false, true) {
			original, physical = c, r
			return f.UnknownResult(attempt, c)
		}
		return r
	}
	store.mu.Unlock()
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "original empty terminal rollback Unknown")
	var problem *f.Fault
	if !masked.Load() || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), rollback) || !errors.As(call.err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != attempt.String() {
		agentRetirementDiagnostic(t, "terminal-empty-rollback-unknown", call.err)
		t.Fatal("empty original rollback did not retain its actual local Unknown")
	}
	v.requireAgentTerminalRetained(t, f.InvalidState)
	running := v.state(t)
	if running.Schema != 5 || running.Parent != "pending" || running.Phase != "running" || !bytes.Equal(running.Raw, v.prior) || v.ownerSnapshot(t) != beforeOwners || v.pair.Joined() {
		t.Fatal("empty Unknown lost its original pending owner")
	}
	v.clearHooks()
	v.barrier = newSchedulerStopBarrier()
	defer v.barrier.open()
	var confirmed atomic.Bool
	originalCause := func(c f.TransactionCause) bool {
		return v.lifecycle(c) && reflect.DeepEqual(c.Details(), original.Details())
	}
	store.setAfter(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		// start() allocates a fresh attempt for its initial read transaction.
		// The actual NotApplied callback instead borrows this exact old cause.
		if !originalCause(c) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var schema int64
		var phase, claimAttempt string
		var raw []byte
		err = x.QueryRow(ctx, `SELECT r.checkpoint_schema,r.checkpoint,w.phase,w.attempt_id::text FROM agenteam_project.lifecycle_participants r JOIN agenteam_project.work_claims w ON w.work_id=r.operation_id AND w.work_kind='lifecycle' WHERE r.operation_id=$1 AND r.participant_name='agent-skills-variables'`, v.cause.OperationID.String()).Scan(&schema, &raw, &phase, &claimAttempt)
		if err != nil {
			return err
		}
		if schema != 5 || phase != "running" || claimAttempt != running.Attempt || !bytes.Equal(raw, v.prior) {
			return errors.New("NotApplied callback released or replaced original persisted ownership")
		}
		confirmed.Store(true)
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if originalCause(c) && confirmed.Load() && r.State() == f.Committed {
			v.barrier.hold(r)
		}
	}
	store.mu.Unlock()
	call = v.run(t, driver)
	wait, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	select {
	case <-v.barrier.entered:
	case <-call.done:
		agentRetirementDiagnostic(t, "terminal-empty-before-confirmation-barrier", call.err)
		t.Fatal("empty NotApplied confirmation returned before original Store barrier")
	case <-wait.Done():
		t.Fatal("empty NotApplied original confirmation barrier was not reached")
	}
	v.requireAgentTerminalRetained(t, f.ResourceBusy)
	if !reflect.DeepEqual(v.state(t), running) || v.ownerSnapshot(t) != beforeOwners || v.pair.Joined() {
		t.Fatal("empty confirmation physical commit released its original owner before return")
	}
	newAgents, err := v.newAgentPair()
	outboxCleanupRequire(t, err)
	v.extraAgents = append(v.extraAgents, newAgents)
	newVariables, err := pv.NewProjectCallOwnership(store, v.deps.ordinary, v.deps.secrets, v.guard, v.process)
	outboxCleanupRequire(t, err)
	v.extraVariables = append(v.extraVariables, newVariables)
	agents, err := agent.NewProjectTerminalCleanup(newAgents, v.consumer)
	outboxCleanupRequire(t, err)
	variables, err := pv.NewProjectTerminalCleanup(newVariables, v.projects)
	outboxCleanupRequire(t, err)
	if agents.ProjectTerminalCleanupIdentity() == v.agentTerminal.ProjectTerminalCleanupIdentity() || variables.ProjectTerminalCleanupIdentity() == v.variableTerminal.ProjectTerminalCleanupIdentity() {
		t.Fatal("replacement pair did not obtain a distinct real instance")
	}
	other, err := project.NewLifecycleCleanupDriverWithTerminal(store, v.projects, captureProviderProcesses{guard: v.guard}, v.consumer, agentRetirementSkillsDiagnostic{SkillsCleanupDomain: v.tap, t: t}, v.variables, agents, variables)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, other)
	competitor := v.run(t, other)
	outboxCleanupWait(t, competitor.done, "new driver and pairs while original empty confirmation is held")
	var busy *f.Fault
	if !errors.As(competitor.err, &busy) || busy.Code != f.ResourceBusy || !reflect.DeepEqual(v.state(t), running) || v.ownerSnapshot(t) != beforeOwners || !newVariables.Joined() {
		agentRetirementDiagnostic(t, "terminal-empty-competing-driver", competitor.err)
		t.Fatal("empty ledger allowed another driver or pair to take over before original confirmation returned")
	}
	select {
	case <-call.done:
		t.Fatal("original empty confirmation returned before release")
	default:
	}
	v.barrier.open()
	outboxCleanupWait(t, call.done, "actual empty NotApplied confirmation return")
	outboxCleanupRequire(t, call.err)
	v.clearHooks()
	if !reflect.DeepEqual(v.state(t), running) || v.ownerSnapshot(t) != beforeOwners || !v.pair.Joined() {
		t.Fatal("original read-only confirmation changed its claim or did not release controls at return")
	}
	applied := v.observeTerminal(t)
	call = v.run(t, driver)
	outboxCleanupWait(t, call.done, "original driver explicit apply after empty confirmation")
	agentRetirementDiagnostic(t, "terminal-empty-explicit-apply", call.err)
	outboxCleanupRequire(t, call.err)
	finished := v.observedReceipt(t, applied)
	if finished.Fence != running.Fence+1 || finished.Attempt == running.Attempt {
		t.Fatal("explicit application did not acquire its next real claim")
	}
	v.clearHooks()
	v.close(t)
	t.Log("PROJECT_AGENT_VARIABLES_TERMINAL_EMPTY_CONFIRMATION real_empty_pairs=true no_anchor_or_ledger=true actual_rollback_ackmask=true read_only_confirmation_original_return_held=true running_claim_retained=true new_driver_and_new_pairs_busy=true original_return_then_explicit_apply=true parent_completed_domains=true")
}
