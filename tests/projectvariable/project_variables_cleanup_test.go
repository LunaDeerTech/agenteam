//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
)

// Accepted Delete and the other participants remain controlled prerequisites.
// The fixed driver really confirms an empty Agent set, cleans the real Skills
// package, and then consumes the original owned Variables pair. D04 and closed
// business calls remain owned, and the combined parent stays pending/domains.
func TestProjectVariablesCleanup(t *testing.T) {
	t.Run("owned-lanes-batches-and-original-return", testProjectVariablesCleanupReturn)
	t.Run("original-unknown-and-metadata-only-recovery", testProjectVariablesCleanupUnknown)
}

type variablesCleanupTap struct {
	vc.ProjectCleanupDomain
	calls  atomic.Int64
	mu     sync.Mutex
	saved  context.Context
	actor  i.Actor
	cause  pc.LifecycleCause
	scope  pc.ScopeRef
	err    error
	report pc.CleanupReport
}

func (p *variablesCleanupTap) Cleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	p.calls.Add(1)
	report, err := p.ProjectCleanupDomain.Cleanup(ctx, actor, cause, scope, cp)
	p.mu.Lock()
	p.saved, p.actor, p.cause, p.scope = context.WithoutCancel(ctx), actor, cause, scope
	p.report, p.err = report, err
	p.mu.Unlock()
	return report, err
}

type variablesCleanupPG struct {
	*skillsCleanupPG
	deps                          variableOwnershipDependencies
	pair                          *pv.ProjectCallOwnership
	consumer                      *agent.ProjectRetirement
	variables                     *variablesCleanupTap
	prior                         []byte
	d04, ledger, foreignVariables string
	closeOnce                     sync.Once
}

func newVariablesCleanupPG(t *testing.T) *variablesCleanupPG {
	t.Helper()
	return newVariablesCleanupPGWith(t, nil, nil)
}

// The default metadata fixture remains unchanged. The terminal-owner slice
// adds actual Agent calls before accepted Delete and stops that original pair
// in the same real stopping window; it never manufactures closed ledger rows.
func newVariablesCleanupPGWith(t *testing.T, prepare func(*variablesCleanupPG), stop func(i.Actor, pc.LifecycleCause, pc.ScopeRef), bases ...*variableHTTPFixture) *variablesCleanupPG {
	t.Helper()
	return newVariablesCleanupPGShape(t, true, prepare, stop, bases...)
}

// The empty-business shape deliberately makes no Variable calls. Its real
// pairs and initialized Project/Skills still traverse their original owners.
func newVariablesCleanupPGShape(t *testing.T, seed bool, prepare func(*variablesCleanupPG), stop func(i.Actor, pc.LifecycleCause, pc.ScopeRef), bases ...*variableHTTPFixture) *variablesCleanupPG {
	t.Helper()
	return newVariablesCleanupPGConfigured(t, seed, false, prepare, stop, bases...)
}

func newVariablesCleanupPGConfigured(t *testing.T, seed, originalSecretAuthority bool, prepare func(*variablesCleanupPG), stop func(i.Actor, pc.LifecycleCause, pc.ScopeRef), bases ...*variableHTTPFixture) *variablesCleanupPG {
	t.Helper()
	v := &variablesCleanupPG{skillsCleanupPG: newSkillsCleanupPG(t, bases...)}
	t.Cleanup(func() { v.close(t) })
	var err error
	var secretProjects []*project.Authority
	if originalSecretAuthority {
		secretProjects = append(secretProjects, v.projects)
	}
	v.deps, err = variableOwnershipAssemble(v.p2.base.tracked, v.p2.base.accounts, v.p2.base.keys, v.p2.manifest, v.process, secretProjects...)
	outboxCleanupRequire(t, err)
	outboxCleanupRequire(t, v.deps.d04.Initialize(ctxFor(t)))
	v.pair, err = pv.NewProjectCallOwnership(v.p2.base.tracked, v.deps.ordinary, v.deps.secrets, v.guard, v.process)
	outboxCleanupRequire(t, err)
	cleanup, err := pv.NewProjectCleanup(v.pair, v.projects)
	outboxCleanupRequire(t, err)
	v.variables = &variablesCleanupTap{ProjectCleanupDomain: cleanup}
	v.consumer = v.emptyAgentConsumer(t)
	var foreignPlain, foreignSecret i.ProjectVariableID
	if seed {
		v.seed(t, v.p2.project.ID, true)
		foreignPlain, foreignSecret = v.seed(t, v.p2.base.project.ID, false)
	}
	if prepare != nil {
		prepare(v)
	}
	v.captureObjects(t, 1)
	_, cause, _ := v.p2.enterCleanupWith(t, func(actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
		if stop != nil {
			stop(actor, cause, scope)
		}
		stopped, err := v.pair.LocalStopper().RequestStop(ctxFor(t), actor, cause, scope)
		outboxCleanupRequire(t, err)
		if !stopped.Matches(cause, scope) || !stopped.Details().LocalJoined || stopped.Details().PendingCalls != 0 {
			t.Fatal("original two-lane local stop did not join")
		}
		observed, err := v.pair.ObserveStop(ctxFor(t), actor, cause, scope)
		outboxCleanupRequire(t, err)
		if !observed.Matches(cause, scope) || !observed.Details().RegisteredProjectBusinessJoined || observed.Details().PendingCalls != 0 || observed.Details().More {
			t.Fatal("original registered business stop was not confirmed")
		}
	})
	v.cause = cause
	v.otherParticipants = v.otherSnapshot(t)
	// Stopping A must not globally stop these instances: B still uses both real
	// services. The returned calls are included in the subsequent immutable tail.
	if seed {
		plain, err := v.pair.Service().GetVariable(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.base.project.ID, foreignPlain)
		outboxCleanupRequire(t, err)
		secret, err := v.pair.SecretService().GetSecretVariable(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.base.project.ID, foreignSecret)
		outboxCleanupRequire(t, err)
		if plain.Fields().ID != foreignPlain || secret.Fields().ID != foreignSecret {
			t.Fatal("other Project's original services did not remain usable")
		}
	}
	v.foreign = v.foreignSnapshot(t)
	v.foreignVariables = v.metadataSnapshot(t, v.p2.base.project.ID)
	v.d04 = v.d04Snapshot(t)
	v.ledger = v.callSnapshot(t)
	var total, closed, instances int
	err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT count(*),count(*) FILTER(WHERE phase='business_closed'),count(DISTINCT instance_id) FROM agenteam_projectvariable.project_calls WHERE project_id IN ($1,$2)`, v.p2.project.ID.String(), v.p2.base.project.ID.String()).Scan(&total, &closed, &instances)
	outboxCleanupRequire(t, err)
	if !seed {
		if total != 0 || closed != 0 || instances != 0 || v.counts(t) != [7]int{} || !v.pair.Joined() {
			t.Fatal("empty-business setup manufactured Variables or registered calls")
		}
		return v
	}
	if total != 14 || closed != 14 || instances != 1 || !v.pair.Joined() {
		t.Fatal("formal two-lane calls have not actually returned with their original durable tail", total, closed, instances)
	}
	if got := v.counts(t); got != [7]int{5, 5, 5, 5, 4, 1, 1} {
		t.Fatal("nonempty formal metadata fixture differs", got)
	}
	var ordinaryLive, secretLive, tombstones, receipts, credentials, receiptPayloads int
	err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='variable' AND deleted_at IS NULL AND version=2),
 (SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='secret' AND deleted_at IS NULL AND version=2),
 (SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1 AND deleted_at IS NOT NULL AND version=3),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_secret.secrets WHERE project_id=$1 AND purpose='project_variable'),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts r JOIN agenteam_secret.secret_payloads p ON p.payload_id=r.digest_payload_id AND p.project_id=r.project_id AND p.owner_id=r.id AND p.owner_kind=3 WHERE r.project_id=$1)`, v.p2.project.ID.String()).Scan(&ordinaryLive, &secretLive, &tombstones, &receipts, &credentials, &receiptPayloads)
	outboxCleanupRequire(t, err)
	if ordinaryLive != 1 || secretLive != 1 || tombstones != 2 || receipts != 5 || credentials != 1 || receiptPayloads != 5 {
		t.Fatal("formal live/tombstone rows or nonempty retained D04 ownership differs")
	}
	return v
}

// No Agent is manufactured for this slice. These are the real fixed reference
// owners consumed by the real empty enumeration/claim step, not a fake report.
func (v *variablesCleanupPG) emptyAgentConsumer(t *testing.T) *agent.ProjectRetirement {
	t.Helper()
	store := v.p2.base.tracked
	source, err := toolruntime.NewInstallSource(v.installer, v.p2.service)
	outboxCleanupRequire(t, err)
	graph, err := assembly.New(store, assembly.Options{Accounts: v.p2.base.accounts, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	outboxCleanupRequire(t, err)
	return v.agentConsumer(t, graph.Providers().Agents)
}

func (v *variablesCleanupPG) agentConsumer(t *testing.T, owner *agent.Authority) *agent.ProjectRetirement {
	t.Helper()
	store := v.p2.base.tracked
	wg, err := work.NewProjectAgentRetirement(store, owner)
	outboxCleanupRequire(t, err)
	eg, err := execution.NewProjectAgentRetirement(store, owner)
	outboxCleanupRequire(t, err)
	models, err := model.NewProjectAgentRetirementReferences(store, owner)
	outboxCleanupRequire(t, err)
	tools, err := registry.NewProjectAgentRetirementReferences(store, owner)
	outboxCleanupRequire(t, err)
	mounts, err := mount.NewProjectAgentRetirementReferences(store, owner)
	outboxCleanupRequire(t, err)
	secrets, err := pv.NewProjectAgentRetirementReferences(store, owner)
	outboxCleanupRequire(t, err)
	ingress, err := execution.NewProjectAgentIngressRetirement(store, owner)
	outboxCleanupRequire(t, err)
	skills, err := skill.NewProjectAgentRetirementReferencesWithIngress(store, owner, ingress)
	outboxCleanupRequire(t, err)
	result, err := agent.NewProjectRetirement(owner, v.projects, agent.ProjectRetirementDependencies{Work: wg, Execution: eg, Models: models, Tools: tools, Mounts: mounts, Secrets: secrets, Skills: skills})
	outboxCleanupRequire(t, err)
	return result
}

func (v *variablesCleanupPG) seed(t *testing.T, p i.ProjectID, full bool) (i.ProjectVariableID, i.ProjectVariableID) {
	t.Helper()
	actor := v.p2.base.ownerBrowser.actor
	var plainID, secretID i.ProjectVariableID
	n := 1
	if full {
		n = 2
	}
	for index := 0; index < n; index++ {
		suffix := "LIVE"
		if index == 1 {
			suffix = "DEAD"
		}
		plain := createInput(t, "CLEANUP_PLAIN_"+suffix, "owned ordinary value")
		secret := secretCreateInput(t, "CLEANUP_SECRET_"+suffix, []byte("owned write-only secret material"))
		a, err := v.pair.Service().CreateVariable(ctxFor(t), actor, meta(t, "plain-create-"+suffix, nil), p, plain)
		outboxCleanupRequire(t, err)
		b, err := v.pair.SecretService().CreateSecretVariable(ctxFor(t), actor, meta(t, "secret-create-"+suffix, nil), p, secret)
		outboxCleanupRequire(t, err)
		if a.Validate() != nil || b.Validate() != nil || !a.Fields().Changed || !b.Fields().Changed || a.Fields().Variable.Fields().Version != 1 || b.Fields().Variable.Fields().Version != 1 {
			t.Fatal("formal create receipts differ")
		}
		plainID, secretID = plain.Fields().ID, secret.Fields().ID
		if !full {
			continue
		}
		version := f.Version(1)
		description := "Updated safe metadata"
		a, err = v.pair.Service().UpdateVariable(ctxFor(t), actor, meta(t, "plain-update-"+suffix, &version), p, plainID, updateInput(t, vc.VariableUpdateFields{Description: &description}))
		outboxCleanupRequire(t, err)
		b, err = v.pair.SecretService().UpdateSecretVariable(ctxFor(t), actor, meta(t, "secret-update-"+suffix, &version), p, secretID, secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description}))
		outboxCleanupRequire(t, err)
		if a.Validate() != nil || b.Validate() != nil || !a.Fields().Changed || !b.Fields().Changed || a.Fields().Variable.Fields().Version != 2 || b.Fields().Variable.Fields().Version != 2 {
			t.Fatal("formal update receipts differ")
		}
		if index == 1 {
			version = 2
			a, err = v.pair.Service().DeleteVariable(ctxFor(t), actor, meta(t, "plain-delete-"+suffix, &version), p, plainID)
			outboxCleanupRequire(t, err)
			b, err = v.pair.SecretService().DeleteSecretVariable(ctxFor(t), actor, meta(t, "secret-delete-"+suffix, &version), p, secretID)
			outboxCleanupRequire(t, err)
			if a.Validate() != nil || b.Validate() != nil || a.Fields().Deleted == nil || b.Fields().Deleted == nil || a.Fields().Deleted.Version != 3 || b.Fields().Deleted.Version != 3 {
				t.Fatal("formal original tombstone receipts differ")
			}
		}
	}
	return plainID, secretID
}

func (v *variablesCleanupPG) counts(t *testing.T) [7]int {
	t.Helper()
	var out [7]int
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.project_generations WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_project_generations WHERE project_id=$1)`, v.p2.project.ID.String()).Scan(&out[0], &out[1], &out[2], &out[3], &out[4], &out[5], &out[6])
	outboxCleanupRequire(t, err)
	return out
}
func (v *variablesCleanupPG) metadataSnapshot(t *testing.T, p i.ProjectID) string {
	t.Helper()
	var out string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.variables x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.history x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.secret_history x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.commands x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.secret_commands x WHERE project_id=$1),
 (SELECT to_jsonb(x) FROM agenteam_projectvariable.project_generations x WHERE project_id=$1),
 (SELECT to_jsonb(x) FROM agenteam_projectvariable.secret_project_generations x WHERE project_id=$1)
 )::text`, p.String()).Scan(&out)
	outboxCleanupRequire(t, err)
	return out
}
func (v *variablesCleanupPG) d04Snapshot(t *testing.T) string {
	t.Helper()
	var out string
	// Compare owned persisted bytes, never decrypt or print them. This includes
	// the nonce high-water and both data/digest payloads of original receipts.
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM agenteam_secret.secret_master_registry x),
 (SELECT jsonb_agg(to_jsonb(x)) FROM agenteam_secret.secret_control x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY payload_id) FROM agenteam_secret.secret_payloads x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_secret.secrets x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_secret.project_variable_receipts x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY credential_id,consumer,owner_id) FROM agenteam_secret.secret_references x),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_secret.secret_leases x)
 )::text`).Scan(&out)
	outboxCleanupRequire(t, err)
	return out
}
func (v *variablesCleanupPG) callSnapshot(t *testing.T) string {
	t.Helper()
	var out string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]')::text FROM agenteam_projectvariable.project_calls x`).Scan(&out)
	outboxCleanupRequire(t, err)
	return out
}
func (v *variablesCleanupPG) retained(t *testing.T) {
	t.Helper()
	if v.d04Snapshot(t) != v.d04 || v.callSnapshot(t) != v.ledger || v.metadataSnapshot(t, v.p2.base.project.ID) != v.foreignVariables {
		t.Fatal("metadata cleanup changed D04, original closed calls or the other Project")
	}
	v.isolated(t)
}
func (v *variablesCleanupPG) driver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	d, err := project.NewLifecycleCleanupDriverWithVariables(v.p2.base.tracked, v.projects, captureProviderProcesses{guard: v.guard}, v.consumer, agentRetirementSkillsDiagnostic{SkillsCleanupDomain: v.tap, t: t}, v.variables)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, d)
	return d
}

// Independent JSON projection, not the driver's decoder. Schema5 embeds the
// exact prior Agent/Skills bytes and cannot overwrite that recovery anchor.
func (v *variablesCleanupPG) progress(t *testing.T, claimPhase string) (string, string, []byte) {
	t.Helper()
	var lifecycle, state, stage, parent, phase string
	var schema int64
	var raw []byte
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT p.lifecycle,o.state,o.cleanup_stage,r.cleanup_state,w.phase,r.checkpoint_schema,r.checkpoint FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id JOIN agenteam_project.lifecycle_participants r ON r.operation_id=o.id AND r.participant_name='agent-skills-variables' JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id WHERE p.id=$1 AND o.id=$2`, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&lifecycle, &state, &stage, &parent, &phase, &schema, &raw)
	outboxCleanupRequire(t, err)
	if lifecycle != "deleting" || state != "cleaning" || stage != "domains" || parent != "pending" || phase != claimPhase {
		t.Fatal("Variables changed the original pending parent or claim", lifecycle, state, stage, parent, phase)
	}
	agents := raw
	variables := ""
	if schema == 5 {
		var cp struct {
			Project      string    `json:"project_id"`
			Operation    string    `json:"operation_id"`
			Version      f.Version `json:"project_version"`
			AgentsSchema f.Version `json:"agents_schema"`
			Agents       []byte    `json:"agents_checkpoint"`
			State        string    `json:"variables_state"`
		}
		if json.Unmarshal(raw, &cp) != nil || cp.Project != v.p2.project.ID.String() || cp.Operation != v.cause.OperationID.String() || cp.Version != v.cause.ProjectVersion || cp.AgentsSchema != 3 || !bytes.Equal(cp.Agents, v.prior) || (cp.State != "pending" && cp.State != "completed") {
			t.Fatal("Variables checkpoint did not preserve the complete original child checkpoint")
		}
		agents, variables = cp.Agents, cp.State
	} else if schema != 3 {
		t.Fatal("unexpected original empty-Agent checkpoint schema")
	}
	var child struct {
		Project   string          `json:"project_id"`
		Operation string          `json:"operation_id"`
		Version   f.Version       `json:"project_version"`
		Done      bool            `json:"agents_done"`
		Applied   json.RawMessage `json:"agent_applied"`
		Skills    struct {
			State string `json:"skills_state"`
		} `json:"skills_progress"`
	}
	if json.Unmarshal(agents, &child) != nil || child.Project != v.p2.project.ID.String() || child.Operation != v.cause.OperationID.String() || child.Version != v.cause.ProjectVersion || !child.Done || len(child.Applied) != 0 || (child.Skills.State != "pending" && child.Skills.State != "completed") {
		t.Fatal("real empty-Agent/Skills checkpoint differs")
	}
	if variables != "" && child.Skills.State != "completed" {
		t.Fatal("Variables ran before Skills completed")
	}
	return variables, child.Skills.State, agents
}
func (v *variablesCleanupPG) reachVariables(t *testing.T, d *project.LifecycleCleanupDriver) {
	t.Helper()
	for round := 0; round < 25; round++ {
		call := v.run(t, d)
		outboxCleanupWait(t, call.done, "original Agent/Skills prerequisite round")
		agentRetirementDiagnostic(t, "variables-prerequisite-return", call.err)
		outboxCleanupRequire(t, call.err)
		variables, skills, raw := v.progress(t, "terminal")
		if variables != "" || v.variables.calls.Load() != 0 || v.counts(t) != [7]int{5, 5, 5, 5, 4, 1, 1} {
			t.Fatal("Variables cleanup overtook a preceding provider's original return")
		}
		v.retained(t)
		if skills == "completed" {
			v.prior = bytes.Clone(raw)
			v.empty(t)
			return
		}
	}
	t.Fatal("original Skills did not complete within its existing bounded rounds")
}
func (v *variablesCleanupPG) matches(c f.TransactionCause) bool {
	d := c.Details()
	return c.Kind() == f.RecoveryCause && d.Owner == "projectvariable-project-cleanup" && d.RecoveryRunID == v.cause.OperationID.String() && d.CheckpointRef == v.p2.project.ID.String()
}
func (v *variablesCleanupPG) observeFirstBatch(t *testing.T) *atomic.Bool {
	t.Helper()
	store := v.p2.base.tracked
	seen := &atomic.Bool{}
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.matches(cause) || seen.Load() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var first bool
		err = x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.history WHERE project_id=$1) AND (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1)=5`, v.p2.project.ID.String()).Scan(&first)
		if err == nil && first {
			seen.Store(true)
		}
		return err
	})
	return seen
}
func (v *variablesCleanupPG) clearHooks() {
	s := v.p2.base.tracked
	s.setAfter(nil)
	s.mu.Lock()
	s.afterResult, s.transformResult = nil, nil
	s.mu.Unlock()
}
func (v *variablesCleanupPG) finishVariables(t *testing.T, d *project.LifecycleCleanupDriver) {
	t.Helper()
	want := v.counts(t)
	for round := 0; round < 8; round++ {
		wasEmpty := want == [7]int{}
		for index, count := range want {
			if count != 0 {
				want[index] = 0
				break
			}
		}
		call := v.run(t, d)
		outboxCleanupWait(t, call.done, "original Variables batch return")
		agentRetirementDiagnostic(t, "variables-batch-return", call.err)
		outboxCleanupRequire(t, call.err)
		state, _, _ := v.progress(t, "terminal")
		if v.counts(t) != want || (state == "completed") != wasEmpty {
			t.Fatal("actual Variables head batch or completed empty confirmation differs")
		}
		v.retained(t)
		if wasEmpty {
			calls := v.variables.calls.Load()
			call = v.run(t, d)
			outboxCleanupWait(t, call.done, "completed metadata parent-pending replay")
			outboxCleanupRequire(t, call.err)
			state, _, _ = v.progress(t, "terminal")
			if state != "completed" || v.variables.calls.Load() != calls {
				t.Fatal("completed metadata replay re-entered the provider")
			}
			v.retained(t)
			return
		}
	}
	t.Fatal("Variables did not complete the known seven finite metadata tables")
}
func (v *variablesCleanupPG) expired(t *testing.T) {
	t.Helper()
	p := v.variables
	p.mu.Lock()
	ctx, actor, cause, scope := p.saved, p.actor, p.cause, p.scope
	p.mu.Unlock()
	if ctx == nil {
		t.Fatal("original Variables private call was not observed")
	}
	before := v.counts(t)
	report, err := p.ProjectCleanupDomain.Cleanup(ctx, actor, cause, scope, nil)
	if !outboxCleanupFault(err, f.Forbidden) || report.Details().State != "" || v.counts(t) != before {
		t.Fatal("expired Variables callback authorized another cleanup")
	}
}
func (v *variablesCleanupPG) close(t *testing.T) {
	t.Helper()
	v.closeOnce.Do(func() {
		if v.barrier != nil {
			v.barrier.open()
		}
		for _, d := range v.drivers {
			d.Stop()
		}
		for _, c := range v.calls {
			outboxCleanupWait(t, c.done, "original Variables driver return before resources")
		}
		if v.pair != nil {
			variableOwnershipDrain(t, v.pair)
		}
		v.clearHooks()
		if v.deps.d04 != nil {
			v.deps.d04.StopMaintenance()
		}
		v.skillsCleanupPG.close(t)
	})
}

func testProjectVariablesCleanupReturn(t *testing.T) {
	v := newVariablesCleanupPG(t)
	driver := v.driver(t)
	v.reachVariables(t, driver)
	seen := v.observeFirstBatch(t)
	v.barrier = newSchedulerStopBarrier()
	store := v.p2.base.tracked
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if v.matches(c) && seen.Load() && r.State() == f.Committed {
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
			t.Fatal("original first metadata batch did not commit")
		}
	case <-call.done:
		agentRetirementDiagnostic(t, "variables-before-barrier", call.err)
		t.Fatal("original batch returned before the required barrier")
	case <-wait.Done():
		t.Fatal("original metadata return barrier was not reached")
	}
	if v.counts(t) != [7]int{0, 5, 5, 5, 4, 1, 1} || v.pair.Joined() {
		t.Fatal("physical batch completion was confused with original owner return")
	}
	state, _, raw := v.progress(t, "running")
	if state != "" || !bytes.Equal(raw, v.prior) {
		t.Fatal("parent progress overtook the original Variables Store return")
	}
	v.retained(t)
	driver.Stop()
	bounded, end := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := driver.Drain(bounded)
	end()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("driver drained before the original metadata call returned", err)
	}
	select {
	case <-call.done:
		t.Fatal("held metadata call returned early")
	default:
	}
	v.barrier.open()
	outboxCleanupWait(t, call.done, "cancelled original Variables batch return")
	if call.err == nil {
		t.Fatal("cancelled held call reported successful progress")
	}
	outboxCleanupRequire(t, driver.Drain(ctxFor(t)))
	v.clearHooks()
	state, _, _ = v.progress(t, "terminal")
	if state != "" || !v.pair.Joined() {
		t.Fatal("failed original batch returned false progress or retained active business")
	}
	v.expired(t)
	v.finishVariables(t, v.driver(t))
	v.close(t)
	t.Log("PROJECT_VARIABLES_CLEANUP_RETURN real_owned_lanes=true live_and_tombstone=true real_skills_complete=true first_batch_committed=true original_return_held=true seven_head_batches=true d04_and_closed_calls_retained=true other_project_usable=true parent_pending_domains=true")
}
func testProjectVariablesCleanupUnknown(t *testing.T) {
	v := newVariablesCleanupPG(t)
	driver := v.driver(t)
	v.reachVariables(t, driver)
	seen := v.observeFirstBatch(t)
	store := v.p2.base.tracked
	attempt := id[f.TransactionAttempt](t)
	var masked atomic.Bool
	var original f.TransactionCause
	store.mu.Lock()
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if v.matches(c) && seen.Load() && r.State() == f.Committed && masked.CompareAndSwap(false, true) {
			original = c
			return f.UnknownResult(attempt, c)
		}
		return r
	}
	store.mu.Unlock()
	defer v.clearHooks()
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "original local acknowledgement Unknown")
	v.variables.mu.Lock()
	originalErr, report := v.variables.err, v.variables.report
	v.variables.mu.Unlock()
	var fault *f.Fault
	if !masked.Load() || !v.matches(original) || !errors.As(call.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != attempt.String() || !errors.Is(call.err, originalErr) || report.Details().State != "" {
		agentRetirementDiagnostic(t, "variables-original-unknown", call.err)
		t.Fatal("original Variable Unknown identity or invalid report was replaced")
	}
	if v.counts(t) != [7]int{0, 5, 5, 5, 4, 1, 1} {
		t.Fatal("local acknowledgement mask changed the physical first batch")
	}
	state, _, raw := v.progress(t, "terminal")
	if state != "" || !bytes.Equal(raw, v.prior) || v.pair.Joined() {
		t.Fatal("Unknown advanced metadata progress or released its original lane owners")
	}
	v.retained(t)
	v.clearHooks()
	// An explicit authorized confirmation must return without deleting another
	// table. Only that actual Store return releases both original lane owners.
	before := v.metadataSnapshot(t, v.p2.project.ID)
	calls := v.variables.calls.Load()
	call = v.run(t, driver)
	outboxCleanupWait(t, call.done, "original read-only Unknown confirmation")
	agentRetirementDiagnostic(t, "variables-unknown-confirmation", call.err)
	outboxCleanupRequire(t, call.err)
	state, _, _ = v.progress(t, "terminal")
	if state != "pending" || !v.pair.Joined() || v.variables.calls.Load() != calls+1 || v.metadataSnapshot(t, v.p2.project.ID) != before {
		t.Fatal("Unknown confirmation deleted metadata or failed to release its original owners")
	}
	v.retained(t)
	v.expired(t)
	v.finishVariables(t, driver)
	v.close(t)
	t.Log("PROJECT_VARIABLES_CLEANUP_UNKNOWN actual_commit_local_ackmask=true original_safe_attempt_and_error=true original_store_cause_observed=true zero_unknown_progress=true original_owners_retained=true readonly_confirmation_actual_return=true no_decryption=true d04_and_closed_calls_retained=true parent_pending_domains=true")
}
