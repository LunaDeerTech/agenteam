//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// The inherited producer performs one controlled first Model call and one real
// WSS read-file Operation. This stage consumes that actual result, then only
// determines durable input: no Model/Runner port is installed on the determiner.
type roundInputFixture struct {
	result     *toolResultConsumptionFixture
	facts      *toolFactsFixture
	commands   *skill.AssignmentCommands
	validation *skill.AssignmentValidation
	determiner *execution.RoundInputDeterminer
	source     ec.ToolResultConsumptionReceipt
	initial    sc.InitialSkillBindings
	grantA     sc.AssignmentMutation
	skill      sc.SkillID
	version    f.Version
	baseline   toolResultConsumptionState
}

func newRoundInputFixture(t *testing.T) *roundInputFixture {
	t.Helper()
	v := &roundInputFixture{}
	v.result = newToolResultConsumptionFixtureWithPreparation(t, func(facts *toolFactsFixture) {
		v.facts = facts
		v.commands = roundInputAssignmentCommands(t, facts)
		c, b := facts.capture, facts.capture.v.base
		request, err := skill.NewInstallRequest(ctxFor(t), id[pc.Skill](t), skillInstallationPackage(t))
		firstRoundRequire(t, err)
		published, err := c.v.agent.p2.service.Install(ctxFor(t), b.ownerBrowser.actor, meta(t, "round-input-publish", nil), b.project.ID, request)
		firstRoundRequire(t, err)
		v.skill, v.version = published.SkillID, facts.version
		v.grantA, err = v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, meta(t, "round-input-before-capture-A", &v.version), b.project.ID, c.v.agentID, v.skill, true)
		firstRoundRequire(t, err)
		v.requireGrant(t, v.grantA)
		current, err := facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, c.v.agentID)
		firstRoundRequire(t, err)
		if current.Fields().Core.Version != v.grantA.AgentVersion {
			t.Fatal("capture callback did not read the actual assigned Agent version")
		}
		v.version = current.Fields().Core.Version
		facts.version = v.version
	})
	c := v.facts.capture
	consumer := v.result.consumer(t)
	var err error
	v.source, err = consumer.Consume(ctxFor(t), c.created.ID)
	firstRoundRequire(t, err)
	v.baseline = v.result.requireDurable(t, true)
	v.result.requireReceipt(t, v.source, v.baseline)
	v.initial = v.facts.input.Fields().Skills.Clone()
	if v.initial.AssignmentSequence != v.grantA.AssignmentSequence || len(v.initial.Bindings) != 2 {
		t.Fatal("actual Preparation did not capture the initial and ordinary Skill grants")
	}
	found := false
	for _, binding := range v.initial.Bindings {
		if binding.SkillID == v.skill {
			found = binding.AssignmentID == v.grantA.Assignment.ID && binding.Revision == 1
		}
	}
	if !found {
		t.Fatal("ordinary grant A is absent from the original canonical capture")
	}
	initial, err := skill.NewInitialRoundBindings(c.v.agent.providers.SkillAuthority)
	firstRoundRequire(t, err)
	v.validation, err = skill.NewAssignmentValidation(c.v.agent.providers.SkillAuthority)
	firstRoundRequire(t, err)
	v.determiner, err = execution.NewRoundInputDeterminer(c.v.base.tracked, c.authority, execution.RoundInputDependencies{Projects: c.projectAuthority, Agents: c.configuration, InitialSkills: initial, Assignments: v.validation})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		v.determiner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := v.determiner.Drain(ctx); err != nil || !v.determiner.Joined() {
			t.Error("original RoundInput calls did not join", err)
		}
	})
	return v
}

// Same explicit graph as 67, bound to the inherited actual Store/guard/Objects.
// This is neither a second P2 fixture nor a substitute authority/typed producer.
func roundInputAssignmentCommands(t *testing.T, facts *toolFactsFixture) *skill.AssignmentCommands {
	t.Helper()
	c, b := facts.capture, facts.capture.v.base
	a := c.v.agent
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
			t.Error("original assignment calls did not join", err)
		}
	})
	catalog := event.NewCatalog()
	types, err := sc.RegisterAssignmentEvents(catalog)
	firstRoundRequire(t, err)
	producer, err := skill.NewAssignmentAuthority(owner, types)
	firstRoundRequire(t, err)
	ingress, err := execution.NewSkillAssignmentIngress(b.tracked, producer)
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
	box, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]ob.ProducerAuthority{sc.AssignmentProducer: producer}, Sessions: b.accounts, System: b.accounts, Projects: projects, Audit: auditor, Cursors: b.keys, Processes: fixtureProcess{processID}})
	firstRoundRequire(t, err)
	commands, err := skill.NewAssignmentCommands(skill.AssignmentDependencies{Service: service, Agents: agents, CurrentAgents: a.providers.Agents, Runtime: ingress, Audit: auditor, Outbox: box, Events: types}, producer)
	firstRoundRequire(t, err)
	return commands
}

func (v *roundInputFixture) requireGrant(t *testing.T, grant sc.AssignmentMutation) {
	t.Helper()
	if grant.Validate() != nil || !grant.Changed || grant.Assignment == nil || grant.Assignment.SkillID != v.skill || !grant.Assignment.Enabled || grant.Runtime == nil || !grant.Runtime.Queued || grant.Runtime.ExecutionID == nil || *grant.Runtime.ExecutionID != v.facts.capture.created.ID {
		t.Fatal("actual Skill producer did not queue the original enabled grant")
	}
}

func (v *roundInputFixture) remove(t *testing.T, grant sc.AssignmentMutation, key string) {
	t.Helper()
	c, b := v.facts.capture, v.facts.capture.v.base
	removed, err := v.commands.RemoveSkill(ctxFor(t), b.ownerBrowser.actor, meta(t, key, &v.version), b.project.ID, c.v.agentID, grant.Assignment.ID)
	firstRoundRequire(t, err)
	if removed.Validate() != nil || !removed.Changed || removed.AgentVersion != v.version+1 || removed.Runtime != nil {
		t.Fatal("actual Remove did not revoke the original grant")
	}
	v.version = removed.AgentVersion
}

type roundInputState struct {
	version, inputs, dispositions, controls, transcripts int64
	raw, control, dispositionJSON, protected             []byte
	digest                                               string
	sameTx                                               bool
}

func (v *roundInputFixture) read(ctx context.Context, x postgres.SQLExecutor) (roundInputState, error) {
	var s roundInputState
	err := x.QueryRow(ctx, `SELECT e.version,
 (SELECT count(*) FROM agenteam_execution.round_inputs WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.skill_assignment_dispositions WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.control_inputs WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.transcript_entries WHERE execution_id=e.id),
 COALESCE(q.content,'\x'::bytea),COALESCE(q.digest,''),COALESCE(c.content,'\x'::bytea),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('change_id',d.change_id,'assignment_sequence',d.assignment_sequence::text,'state',d.state,'reason',d.reason) ORDER BY d.assignment_sequence)::text FROM agenteam_execution.skill_assignment_dispositions d WHERE d.execution_id=e.id),'[]'),
 COALESCE(q.xmin=e.xmin AND q.checkpoint_version=e.version
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_dispositions d WHERE d.execution_id=e.id AND (d.xmin<>q.xmin OR d.round_input_id<>q.id))
 AND (c.id IS NULL OR (c.xmin=q.xmin AND c.round_input_id=q.id AND t.xmin=q.xmin AND t.round_id IS NULL AND t.message=c.content AND t.digest=c.digest AND t.created_at=q.created_at)),false),
 jsonb_build_object(
 'preparation',(SELECT to_jsonb(p) FROM agenteam_execution.preparation_inputs p WHERE p.execution_id=e.id),
 'capture',(SELECT to_jsonb(h) FROM agenteam_skill.execution_binding_heads h WHERE h.execution_id::text=e.id),
 'bindings',(SELECT jsonb_agg(to_jsonb(z) ORDER BY z.skill_id) FROM agenteam_skill.execution_bindings z WHERE z.execution_id::text=e.id),
 'consumption',(SELECT to_jsonb(k) FROM agenteam_execution.tool_result_consumptions k WHERE k.execution_id=e.id),
 'prefix',(SELECT jsonb_agg(to_jsonb(z) ORDER BY z.sequence) FROM agenteam_execution.transcript_entries z WHERE z.execution_id=e.id AND z.sequence<=3))::text
 FROM agenteam_execution.executions e
 LEFT JOIN agenteam_execution.round_inputs q ON q.execution_id=e.id
 LEFT JOIN agenteam_execution.control_inputs c ON c.execution_id=e.id
 LEFT JOIN agenteam_execution.transcript_entries t ON t.execution_id=e.id AND t.sequence=4
 WHERE e.id=$1`, v.facts.capture.created.ID.String()).Scan(&s.version, &s.inputs, &s.dispositions, &s.controls, &s.transcripts, &s.raw, &s.digest, &s.control, &s.dispositionJSON, &s.sameTx, &s.protected)
	return s, err
}

func (v *roundInputFixture) requireStored(t *testing.T, input ec.RoundInput, before roundInputState) {
	t.Helper()
	s, err := v.read(ctxFor(t), v.facts.capture.v.base.raw)
	firstRoundRequire(t, err)
	firstRoundRequire(t, v.validateStored(input, before, s))
	v.requireOriginal(t, s.version, s.transcripts)
}

func (v *roundInputFixture) validateStored(input ec.RoundInput, before, got roundInputState) error {
	if input.Validate() != nil {
		return errors.New("round_input_invalid_receipt")
	}
	q := input.Fields()
	controls := int64(0)
	if q.Control != nil {
		controls = 1
	}
	if got.version != before.version+1 || got.inputs != 1 || got.dispositions != int64(len(q.Dispositions)) || got.controls != controls || got.transcripts != 3+controls || !got.sameTx || !bytes.Equal(got.raw, input.CanonicalBytes()) || got.digest != string(input.Digest()) || !bytes.Equal(got.protected, before.protected) || q.Source != v.source || q.CheckpointVersion != f.Version(got.version) || !reflect.DeepEqual(q.Initial, v.initial) {
		return errors.New("round_input_commit_source_pair")
	}
	var dispositions []ec.SkillChangeDisposition
	if json.Unmarshal(got.dispositionJSON, &dispositions) != nil || !reflect.DeepEqual(dispositions, q.Dispositions) {
		return errors.New("round_input_disposition_pair")
	}
	if q.Control != nil {
		raw, err := json.Marshal(q.Control)
		if err != nil {
			return err
		}
		raw, err = cursor.CanonicalJSON(raw)
		if err != nil || !bytes.Equal(raw, got.control) {
			return errors.New("round_input_control_pair")
		}
	} else if len(got.control) != 0 {
		return errors.New("round_input_empty_control")
	}
	return nil
}

func (v *roundInputFixture) requireOriginal(t *testing.T, version, transcripts int64) {
	t.Helper()
	got, err := v.result.read(ctxFor(t), v.facts.capture.v.base.raw)
	firstRoundRequire(t, err)
	if got.version != version || got.transcripts != transcripts || (version == v.baseline.version) != got.sameTx {
		t.Fatal("successor checkpoint affected the wrong Execution version or transaction")
	}
	// These three fields are the explicit successor effects; every original
	// Snapshot/Round/Call/Attempt/payload/result/lease/seq1-3 field stays exact.
	got.version, got.transcripts, got.sameTx = v.baseline.version, v.baseline.transcripts, v.baseline.sameTx
	if !reflect.DeepEqual(got, v.baseline) {
		t.Fatal("RoundInput changed the original Tool result checkpoint")
	}
	v.result.requireOriginalWork(t)
}

func (v *roundInputFixture) requireReplacement(t *testing.T, input ec.RoundInput, next ec.RoundID, grantB sc.AssignmentMutation) {
	t.Helper()
	q := input.Fields()
	if input.Validate() != nil || q.RoundID != next || q.AssignmentCutoff != grantB.AssignmentSequence || len(q.Dispositions) != 2 || q.Dispositions[0] != (ec.SkillChangeDisposition{ChangeID: v.grantA.ChangeID, Sequence: v.grantA.AssignmentSequence, State: "covered"}) || q.Dispositions[1] != (ec.SkillChangeDisposition{ChangeID: grantB.ChangeID, Sequence: grantB.AssignmentSequence, State: "applied"}) || len(q.AppliedChangeIDs) != 1 || q.AppliedChangeIDs[0] != grantB.ChangeID || q.Control == nil || len(q.Control.Payload) != 1 || q.TranscriptThrough != 4 {
		t.Fatal("successor did not cover A once and apply B once")
	}
	addition := q.Control.Payload[0]
	if addition.ChangeID != grantB.ChangeID || addition.Binding.SkillID != v.skill || addition.Binding.AssignmentID != grantB.Assignment.ID || addition.ReplacedAssignmentID == nil || *addition.ReplacedAssignmentID != v.grantA.Assignment.ID || addition.Binding.Revision != 1 || len(q.Bindings) != len(v.initial.Bindings) {
		t.Fatal("new grant B did not replace only A's fixed directory position")
	}
	for _, binding := range q.Bindings {
		if binding.SkillID == v.skill {
			if binding != addition.Binding {
				t.Fatal("replacement directory differs from typed control")
			}
			continue
		}
		found := false
		for _, old := range v.initial.Bindings {
			found = found || binding == old
		}
		if !found {
			t.Fatal("unrelated initial directory entry changed")
		}
	}
}

func (v *roundInputFixture) requireRevokedA(t *testing.T) {
	t.Helper()
	c, b := v.facts.capture, v.facts.capture.v.base
	projectLock, _ := f.ProjectLock(b.project.ID.String())
	schedule, _ := f.ProjectScheduleLock(b.project.ID.String())
	agentLock, _ := f.AgentLock(c.v.agentID.String())
	skillLock, _ := f.AggregateLock(f.SkillAggregate, v.skill.String())
	var original sc.SkillBinding
	for _, binding := range v.initial.Bindings {
		if binding.SkillID == v.skill {
			original = binding
		}
	}
	b.tx(t, []f.LockRequest{{Key: projectLock, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}, {Key: agentLock, Mode: f.Exclusive}, {Key: skillLock, Mode: f.Shared}}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		validity, err := v.validation.ValidateAssignmentInTx(ctx, tx, b.project.ID, c.v.agentID, original.AssignmentID, original.SkillID, original.RevisionID, original.Revision)
		if err != nil {
			return err
		}
		if validity != sc.AssignmentRemoved {
			return errors.New("historical_capture_reauthorized_removed_grant")
		}
		return nil
	})
}

type roundInputReply[T any] struct {
	value T
	err   error
}

func roundInputAsync[T any](t *testing.T, fn func(context.Context) (T, error)) <-chan roundInputReply[T] {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan roundInputReply[T], 1)
	done := make(chan struct{})
	go func() { defer close(done); v, err := fn(ctx); out <- roundInputReply[T]{v, err} }()
	t.Cleanup(func() { cancel(); await(t, done) })
	return out
}
func roundInputWait[T any](t *testing.T, out <-chan roundInputReply[T]) roundInputReply[T] {
	t.Helper()
	select {
	case got := <-out:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("original round-input related call did not return")
	}
	return roundInputReply[T]{}
}
func roundInputLockAttempt(t *testing.T, attempts <-chan lockAttempt) lockAttempt {
	t.Helper()
	select {
	case got := <-attempts:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("original caller did not attempt the expected gate")
	}
	return lockAttempt{}
}
func roundInputPID(t *testing.T, reached <-chan int32) int32 {
	t.Helper()
	select {
	case pid := <-reached:
		if pid > 0 {
			return pid
		}
	case <-time.After(10 * time.Second):
		t.Fatal("original final callback did not reach the held transaction")
	}
	t.Fatal("missing original backend PID")
	return 0
}
func roundInputClearHooks(store *hookStore) {
	store.setAfter(nil)
	store.mu.Lock()
	store.beforeLocks = nil
	store.afterResult = nil
	store.mu.Unlock()
}

func TestExecutionRoundInput(t *testing.T) {
	t.Run("assignment-first-replacement-and-replay", func(t *testing.T) {
		v := newRoundInputFixture(t)
		c, b := v.facts.capture, v.facts.capture.v.base
		store := b.tracked
		v.remove(t, v.grantA, "round-input-remove-A")
		v.requireRevokedA(t)
		next := id[ec.Round](t)
		stale, err := v.determiner.DiscoverRoundInput(ctxFor(t), c.created.ID, next)
		firstRoundRequire(t, err)
		original := meta(t, "round-input-assign-B-first", &v.version)
		identity, err := f.NewCommandIdentity("project", []string{b.project.ID.String(), c.v.agentID.String()}, "skill.assign", original.IdempotencyKey)
		firstRoundRequire(t, err)
		reached, release := make(chan int32, 1), make(chan struct{})
		var releaseOnce sync.Once
		resume := func() { releaseOnce.Do(func() { close(release) }) }
		defer resume()
		defer roundInputClearHooks(store)
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.assignment_commands WHERE project_id::text=$1 AND agent_id::text=$2 AND idempotency_key=$3 AND state='completed')`, b.project.ID.String(), c.v.agentID.String(), string(original.IdempotencyKey)).Scan(&completed); err != nil || !completed {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			reached <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		assign := roundInputAsync(t, func(ctx context.Context) (sc.AssignmentMutation, error) {
			return v.commands.AssignSkill(ctx, b.ownerBrowser.actor, original, b.project.ID, c.v.agentID, v.skill, true)
		})
		blocker := roundInputPID(t, reached)
		schedule, _ := f.ProjectScheduleLock(b.project.ID.String())
		attempts := observeLock(store, schedule)
		cause, err := f.NewJobCause("execution.round-input.determine", c.created.ID.String(), next.String())
		firstRoundRequire(t, err)
		try := roundInputAsync(t, func(ctx context.Context) (f.CommitResult, error) {
			result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				if err := store.AcquireAll(ctx, tx, stale.RequiredLocks()); err != nil {
					return err
				}
				_, err := v.determiner.DetermineRoundInputInTx(ctx, tx, c.created.ID, next, stale)
				return err
			})
			return result, nil
		})
		attempt := roundInputLockAttempt(t, attempts)
		b.waitLock(t, attempt, false, blocker)
		resume()
		assigned := roundInputWait(t, assign)
		firstRoundRequire(t, assigned.err)
		v.requireGrant(t, assigned.value)
		v.version = assigned.value.AgentVersion
		rejected := roundInputWait(t, try)
		firstRoundRequire(t, rejected.err)
		if rejected.value.State() != f.NotCommitted {
			t.Fatal("stale final plan was not rolled back")
		}
		requireCode(t, rejected.value.Fault(), f.ConfirmationStale)
		roundInputClearHooks(store)
		before, err := v.read(ctxFor(t), b.raw)
		firstRoundRequire(t, err)
		if before.inputs != 0 || before.dispositions != 0 || before.controls != 0 || before.version != v.baseline.version || before.transcripts != 3 {
			t.Fatal("stale plan published partial successor facts")
		}
		input, err := v.determiner.Determine(ctxFor(t), c.created.ID, next)
		firstRoundRequire(t, err)
		v.requireReplacement(t, input, next, assigned.value)
		v.requireStored(t, input, before)
		v.requireRevokedA(t)
		replay, err := v.determiner.Determine(ctxFor(t), c.created.ID, next)
		firstRoundRequire(t, err)
		if !bytes.Equal(replay.CanonicalBytes(), input.CanonicalBytes()) {
			t.Fatal("same successor ID recomputed committed input")
		}
		_, err = v.determiner.Determine(ctxFor(t), c.created.ID, id[ec.Round](t))
		requireCode(t, err, f.ConfirmationStale)
		v.requireStored(t, input, before)
	})
	t.Run("rollback-commit-loss-and-determination-first", func(t *testing.T) {
		v := newRoundInputFixture(t)
		c, b := v.facts.capture, v.facts.capture.v.base
		store := b.tracked
		v.remove(t, v.grantA, "round-input-loss-remove-A")
		grantB, err := v.commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, meta(t, "round-input-loss-assign-B", &v.version), b.project.ID, c.v.agentID, v.skill, true)
		firstRoundRequire(t, err)
		v.requireGrant(t, grantB)
		v.version = grantB.AgentVersion
		// A second ordinary publication gives the concurrent writer a genuinely
		// new grant. It is never in this capture or sent to a second Model round.
		files, err := sc.NewTextFiles([]sc.TextFile{{Path: sc.EntryPath, UTF8Text: "---\nname: Later round guide\ndescription: Owned post-cut assignment\n---\nKeep this pending until a real later predecessor exists.\n"}})
		firstRoundRequire(t, err)
		pkg, err := skill.BuildPackage(ctxFor(t), files)
		firstRoundRequire(t, err)
		install, err := skill.NewInstallRequest(ctxFor(t), id[pc.Skill](t), pkg)
		firstRoundRequire(t, err)
		published, err := c.v.agent.p2.service.Install(ctxFor(t), b.ownerBrowser.actor, meta(t, "round-input-late-publication", nil), b.project.ID, install)
		firstRoundRequire(t, err)
		before, err := v.read(ctxFor(t), b.raw)
		firstRoundRequire(t, err)
		next := id[ec.Round](t)
		matches := func(cause f.TransactionCause) bool {
			d := cause.Details()
			return d.Kind == f.JobCause && d.JobType == "execution.round-input.determine" && d.JobID == c.created.ID.String() && d.JobAttemptID == next.String()
		}
		marker := errors.New("round-input-final-callback-rollback")
		observed, returned := false, false
		var physical f.CommitResult
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !matches(cause) {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			s, err := v.read(ctx, x)
			if err != nil {
				return err
			}
			candidate, err := ec.DecodeRoundInput(s.raw)
			if err != nil {
				return err
			}
			if err = v.validateStored(candidate, before, s); err != nil {
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
		defer roundInputClearHooks(store)
		_, err = v.determiner.Determine(ctxFor(t), c.created.ID, next)
		roundInputClearHooks(store)
		if err == nil || !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) {
			t.Fatal("complete RoundInput callback did not actually roll back", err)
		}
		afterRollback, err := v.read(ctxFor(t), b.raw)
		firstRoundRequire(t, err)
		if !reflect.DeepEqual(afterRollback, before) {
			t.Fatal("rollback left partial input/dispositions/control/transcript/checkpoint facts")
		}
		v.requireOriginal(t, before.version, 3)
		_, found, err := v.determiner.Lookup(ctxFor(t), c.created.ID, next)
		firstRoundRequire(t, err)
		if found {
			t.Fatal("rolled-back successor became observable")
		}
		// Explicitly retry only the known rollback. The response-loss boundary
		// observes real final facts and hides the real committed return once.
		reached, release := make(chan int32, 1), make(chan struct{})
		var releaseOnce sync.Once
		resume := func() { releaseOnce.Do(func() { close(release) }) }
		defer resume()
		eligible := false
		var candidate ec.RoundInput
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !matches(cause) {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			s, err := v.read(ctx, x)
			if err != nil {
				return err
			}
			candidate, err = ec.DecodeRoundInput(s.raw)
			if err != nil {
				return err
			}
			if err = v.validateStored(candidate, before, s); err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			eligible = true
			reached <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		underlying := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: underlying, attempt: id[f.TransactionAttempt](t), match: func(cause f.TransactionCause) bool { return matches(cause) && eligible }}
		store.fixtureStore = loss
		// Keep this delegate fixed until both original calls have actually
		// returned. Changing the shared Store while the contender runs is a race.
		// Registered before the asynchronous calls so their original returns are
		// joined first, including when a lock assertion aborts this test body.
		t.Cleanup(func() { roundInputClearHooks(store); store.fixtureStore = underlying })
		determine := roundInputAsync(t, func(ctx context.Context) (ec.RoundInput, error) {
			return v.determiner.Determine(ctx, c.created.ID, next)
		})
		blocker := roundInputPID(t, reached)
		schedule, _ := f.ProjectScheduleLock(b.project.ID.String())
		attempts := observeLock(store, schedule)
		lateMeta := meta(t, "round-input-after-cut", &v.version)
		assign := roundInputAsync(t, func(ctx context.Context) (sc.AssignmentMutation, error) {
			return v.commands.AssignSkill(ctx, b.ownerBrowser.actor, lateMeta, b.project.ID, c.v.agentID, published.SkillID, true)
		})
		attempt := roundInputLockAttempt(t, attempts)
		b.waitLock(t, attempt, false, blocker)
		resume()
		uncertain := roundInputWait(t, determine)
		late := roundInputWait(t, assign)
		roundInputClearHooks(store)
		store.fixtureStore = underlying
		requireCode(t, uncertain.err, f.CommitUnknown)
		firstRoundRequire(t, late.err)
		fired, committed := loss.observed()
		if !fired || !eligible || committed.State() != f.Committed || uncertain.value.Validate() == nil {
			t.Fatal("Unknown was not loss of the actual committed return")
		}
		if late.value.Validate() != nil || !late.value.Changed || late.value.Assignment == nil || late.value.Assignment.SkillID != published.SkillID || late.value.AssignmentSequence != grantB.AssignmentSequence+1 || late.value.Runtime == nil || !late.value.Runtime.Queued || late.value.Runtime.ExecutionID == nil || *late.value.Runtime.ExecutionID != c.created.ID {
			t.Fatal("post-cut actual Assign did not retain pending ingress")
		}
		// Unknown recovery is only Lookup of this exact successor ID. No second
		// Determine, Model call, Tool send or made-up next predecessor follows.
		stored, found, err := v.determiner.Lookup(ctxFor(t), c.created.ID, next)
		firstRoundRequire(t, err)
		if !found || !bytes.Equal(stored.CanonicalBytes(), candidate.CanonicalBytes()) {
			t.Fatal("original Lookup changed the committed candidate")
		}
		v.requireReplacement(t, stored, next, grantB)
		v.requireStored(t, stored, before)
		v.requireRevokedA(t)
		var pending bool
		err = b.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*)=3 FROM agenteam_execution.skill_assignment_ingress WHERE execution_id=$1)
 AND EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress g JOIN agenteam_skill.assignment_pending_refs p ON p.change_id::text=g.change_id WHERE g.execution_id=$1 AND g.change_id=$2 AND p.execution_id::text=g.execution_id AND p.assignment_id::text=$3)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_dispositions WHERE execution_id=$1 AND change_id=$2)
 AND (SELECT count(*)=1 AND bool_and(assignment_cutoff=$4) FROM agenteam_execution.round_inputs WHERE execution_id=$1)`, c.created.ID.String(), late.value.ChangeID.String(), late.value.Assignment.ID.String(), int64(grantB.AssignmentSequence)).Scan(&pending)
		firstRoundRequire(t, err)
		if !pending {
			t.Fatal("late assignment was lost or retroactively consumed")
		}
		again, found, err := v.determiner.Lookup(ctxFor(t), c.created.ID, next)
		firstRoundRequire(t, err)
		if !found || !bytes.Equal(again.CanonicalBytes(), stored.CanonicalBytes()) {
			t.Fatal("historical input changed after the later assignment")
		}
	})
}
