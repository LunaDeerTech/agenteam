//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestExecutionRuntimeAccessAudit(t *testing.T) {
	t.Run("policy-denial-requires-live-runtime-handoff", func(t *testing.T) {
		x := newFirstRoundFixtureWithRuntimeAudit(t, false, false, true)
		v := x.capture.v
		if x.projects != v.agent.providers.Projects || x.capture.projectAuthority != x.projects || x.auditProjects == nil {
			t.Fatal("runtime audit did not retain the original Project before Execution construction")
		}
		captured := x.prepare(t)
		// Change the very Policy instance consumed by the real wire adapter.
		// An empty allowlist denies the owned private address before any socket.
		rules, err := outbound.NewRules()
		firstRoundRequire(t, err)
		actor := v.base.adminBrowser.actor
		command, err := f.NewCommandIdentity("outbound-policy", []string{actor.Details().UserID}, "update", f.IdempotencyKey(id[struct{}](t).String()))
		firstRoundRequire(t, err)
		status := x.outboundPolicy.Status()
		if status.Version == nil {
			t.Fatal("original outbound Policy has no current version")
		}
		denied, err := x.outboundPolicy.UpdatePolicy(ctxFor(t), actor, outbound.CommandMeta{Identity: command, ExpectedVersion: *status.Version, HTTPTraceID: id[struct{}](t).String()}, rules)
		firstRoundRequire(t, err)
		if denied.RuleCount != 0 {
			t.Fatal("formal empty Policy was not committed")
		}
		driver := x.directDriver(t)
		receipt, callErr := driver.Start(ctxFor(t), x.capture.created.ID)
		var outcome *mc.ModelError
		if !errors.As(callErr, &outcome) || outcome.Validate() != nil || outcome.Category != "permission" || outcome.Retryable || receipt.Status != ec.Failed {
			t.Fatal("actual outbound denial did not finish the original Execution with its typed permission outcome")
		}
		firstRoundDriverJoined(t, driver)
		invocation, metadata := requireRuntimeAccessDenial(t, x, captured, receipt, denied.Version)
		x.requireWire(t, 0, true)
		x.reader.requireDestroyed(t, 1)
		requireRetiredRuntimeAuditForbidden(t, x, invocation, metadata)
		// This is the original fixture-wide cleanup, after the scoped owner and
		// durable retirement assertions. It cannot manufacture their success.
		x.close(t)
		if !x.runtime.Joined() || !x.budget.Joined() {
			t.Fatal("original Runtime or outbound budget remained owned")
		}
	})
	t.Run("default-factory-completed-one-turn", func(t *testing.T) {
		// Exercise the old default through the same small delegating helpers.
		// It remains the established real successful wire/retirement graph.
		x := newFirstRoundFixture(t, false)
		captured := x.prepare(t)
		driver := x.directDriver(t)
		receipt, err := driver.Start(ctxFor(t), x.capture.created.ID)
		firstRoundRequire(t, err)
		x.requireTerminal(t, captured, receipt, ec.Succeeded)
		firstRoundDriverJoined(t, driver)
	})
}

func requireRuntimeAccessDenial(t *testing.T, x *firstRoundFixture, captured ec.ExecutionContext, receipt ec.DirectTextReceipt, policyVersion f.Version) (mc.InvocationID, ac.Metadata) {
	t.Helper()
	c := x.capture
	if receipt.ExecutionID != c.created.ID || receipt.StartID.Validate() != nil || receipt.SnapshotID.Validate() != nil || receipt.RoundID.Validate() != nil || receipt.CallID.Validate() != nil || receipt.TranscriptThrough != 1 {
		t.Fatal("denied first round lost its original durable receipt")
	}
	var terminal []byte
	var terminalRows int
	err := c.v.base.raw.QueryRow(ctxFor(t), `SELECT terminal.payload,
 (SELECT count(*) FROM agenteam_execution.transcript_entries t WHERE t.execution_id=e.id)
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id,r.start_id)=(e.id,s.id,s.start_id)
 JOIN agenteam_outbox.events started ON started.id=s.started_event_id
 JOIN agenteam_outbox.events terminal ON terminal.id=r.terminal_event_id
 WHERE e.id=$1 AND e.project_id=$2 AND e.agent_id=$3 AND e.status='failed' AND e.version=$4
 AND e.started_at=s.created_at AND e.completed_at=r.finished_at AND e.completed_at>=e.started_at
 AND s.id=$5 AND s.start_id=$6 AND s.input_digest=$7 AND s.context_digest=$8
 AND r.id=$9 AND r.call_id=$10 AND r.context_digest=s.context_digest
 AND r.terminal_status=e.status AND r.terminal_version=e.version AND r.terminal_reason='model_failed'
 AND r.transcript_through=1 AND r.response IS NULL
 AND started.event_type='execution.started' AND started.aggregate_id=e.id::uuid AND started.aggregate_version=s.started_version
 AND terminal.event_type='execution.failed' AND terminal.aggregate_id=e.id::uuid AND terminal.aggregate_version=e.version
 AND started.project_id=e.project_id::uuid AND terminal.project_id=e.project_id::uuid
 AND started.schema_version=1 AND terminal.schema_version=1
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions active WHERE active.agent_id=e.agent_id AND active.status IN('created','preparing','running','waiting'))`,
		c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String(), int64(receipt.Version),
		receipt.SnapshotID.String(), receipt.StartID.String(), string(captured.InputDigest()), string(captured.Digest()), receipt.RoundID.String(), receipt.CallID.String()).Scan(&terminal, &terminalRows)
	firstRoundRequire(t, err)
	var lifecycle ec.DirectTextLifecycle
	firstRoundRequire(t, json.Unmarshal(terminal, &lifecycle))
	firstRoundRequire(t, lifecycle.Validate())
	if terminalRows != 1 || lifecycle.ExecutionID != receipt.ExecutionID || lifecycle.StartID != receipt.StartID || lifecycle.RoundID != receipt.RoundID || lifecycle.CallID != receipt.CallID || lifecycle.Status != ec.Failed || lifecycle.Reason != "model_failed" || lifecycle.TranscriptThrough != 1 {
		t.Fatal("denied Model fabricated an assistant response or lost its atomic terminal event")
	}
	var invocation string
	var metadata []byte
	var reads, denies, attempts, invocations int
	var input, output, total *int64
	model := captured.Input().Fields().Model
	err = c.v.base.raw.QueryRow(ctxFor(t), `SELECT a.id::text,d.metadata,
 (SELECT count(*) FROM agenteam_audit.audit_records n WHERE n.action='secret.resolve' AND n.request_id=a.id),
 (SELECT count(*) FROM agenteam_audit.audit_records n WHERE n.producer='outbound.access' AND n.cause_ref=a.id::text),
 (SELECT count(*) FROM agenteam_model.runtime_attempts n WHERE n.call_id=c.id),
 (SELECT count(*) FROM agenteam_model.invocations n WHERE n.call_id=c.id),
 u.input_tokens,u.output_tokens,u.total_tokens
 FROM agenteam_model.calls c
 JOIN agenteam_model.runtime_attempts a ON(a.call_id,a.id,a.project_id,a.process_id,a.fence)=(c.id,c.invocation_id,c.project_id,c.process_id,c.fence)
 JOIN agenteam_model.invocations u ON(u.id,u.call_id,u.attempt_index)=(a.id,a.call_id,a.ordinal)
 JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id
 JOIN agenteam_audit.audit_records d ON d.scope='project' AND d.project_id=c.project_id AND d.producer='outbound.access' AND d.cause_ref=a.id::text AND d.ordinal=0
 WHERE c.id=$1::text::uuid AND c.project_id=$2::text::uuid AND c.snapshot_id=$3::text::uuid AND c.lease_id=$4::text::uuid
 AND c.request_data#>>'{Initiator,AgentID}'=$5 AND c.request_data#>>'{Initiator,ExecutionID}'=$6
 AND c.phase='failed' AND c.retired AND c.finished_at IS NOT NULL
 AND a.ordinal=1 AND a.dispatch='not_sent' AND u.dispatch=a.dispatch AND u.last_sequence=a.sequence AND u.final_status='failed' AND u.usage_source='unknown'
 AND l.owner_kind='execution' AND l.owner_id=$6::text::uuid AND l.released
 AND d.actor_kind='service' AND d.actor_project_id=d.project_id AND d.service_name='outbound' AND d.service_cause=a.id::text
 AND d.action='outbound.access.deny' AND d.outcome='denied' AND d.resource_kind='outbound_policy' AND d.resource_id IS NULL
 AND d.operation_id IS NULL AND d.metadata->>'consumer'='model' AND d.metadata->>'reason'='private_not_allowed'`,
		receipt.CallID.String(), c.created.ProjectID.String(), model.Snapshot.ID.String(), model.CredentialLease.LeaseID.String(), c.created.AgentID.String(), c.created.ID.String()).
		Scan(&invocation, &metadata, &reads, &denies, &attempts, &invocations, &input, &output, &total)
	firstRoundRequire(t, err)
	if reads != 1 || denies != 1 || attempts != 1 || invocations != 1 || input != nil || output != nil || total != nil {
		t.Fatal("denied call lost same-invocation audit/Usage facts or retried a permanent refusal")
	}
	actualMetadata, err := ac.DecodeMetadata(ac.AccessDeny, metadata)
	firstRoundRequire(t, err)
	wantMetadata, err := ac.DenialMetadata(ac.Model, ac.PrivateNotAllowed, policyVersion)
	firstRoundRequire(t, err)
	if !bytes.Equal(actualMetadata.JSON(), wantMetadata.JSON()) {
		t.Fatal("denial audit did not capture the actual committed Policy version")
	}
	invocationID, err := f.ParseID[mc.Invocation](invocation)
	firstRoundRequire(t, err)

	var environmentRetired int
	environment := captured.Input().Fields().Environment.Fields()
	err = c.v.base.raw.QueryRow(ctxFor(t), `SELECT count(*)
 FROM agenteam_secret.project_variable_execution_leases l
 JOIN agenteam_projectvariable.execution_secret_references r ON(r.execution_id,r.variable_id,r.lease_id)=(l.execution_id,l.variable_id,l.lease_id)
 WHERE l.execution_id=$1 AND l.project_id=$2 AND l.agent_id=$3 AND l.lease_id=$4::text::uuid
 AND l.variable_id=$5::text::uuid AND l.released AND l.released_at IS NOT NULL`,
		c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String(), environment.Secrets[0].LeaseID.String(), c.secret.Fields().ID.String()).Scan(&environmentRetired)
	firstRoundRequire(t, err)
	counts, err := captureInputCounts(ctxFor(t), c.v.base.raw, c.created.ID.String())
	firstRoundRequire(t, err)
	if environmentRetired != 1 || counts != [11]int64{1, 1, 1, 0, 1, 1, 0, 1, 1, 1, 1} {
		t.Fatal("denied Execution retained live credential access or lost protected capture history")
	}
	originalTask, err := work.DecodeTaskContext(captured.Trigger())
	firstRoundRequire(t, err)
	currentTask, err := c.v.taskReader.GetTask(ctxFor(t), c.v.base.ownerBrowser.actor, c.created.ProjectID, c.v.task.ID)
	firstRoundRequire(t, err)
	before, err := json.Marshal(originalTask.Task())
	firstRoundRequire(t, err)
	after, err := json.Marshal(currentTask)
	firstRoundRequire(t, err)
	if currentTask.State != wc.TaskStateInProgress || !bytes.Equal(before, after) {
		t.Fatal("Model denial changed the independently owned Work Task")
	}
	return invocationID, actualMetadata
}

func requireRetiredRuntimeAuditForbidden(t *testing.T, x *firstRoundFixture, invocation mc.InvocationID, metadata ac.Metadata) {
	t.Helper()
	c := x.capture
	scope, err := i.InProject(c.created.ProjectID)
	firstRoundRequire(t, err)
	service, err := i.RegisterService(i.OutboundService)
	firstRoundRequire(t, err)
	actor, err := service.Actor(invocation.String(), scope)
	firstRoundRequire(t, err)
	resource, err := ac.NewResource(ac.PolicyResource, "")
	firstRoundRequire(t, err)
	entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: ac.AccessDeny, Outcome: ac.Denied, Resource: resource, Metadata: metadata})
	firstRoundRequire(t, err)
	key, err := ac.NewAppendKey(ac.AccessProducer, invocation.String(), 0)
	firstRoundRequire(t, err)
	lock, err := f.ProjectLock(c.created.ProjectID.String())
	firstRoundRequire(t, err)
	cause, err := f.NewRecoveryCause("runtime-audit-without-live-handoff", invocation.String(), "")
	firstRoundRequire(t, err)
	var checked error
	// This fresh caller has all public original identities and the real lock,
	// but none of the retired Runtime's private context. Call the authority
	// before any Audit idempotency lookup; an existing row is not a grant.
	result := c.v.base.tracked.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		if err := c.v.base.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: lock, Mode: f.Shared}}); err != nil {
			return err
		}
		checked = x.auditProjects.CheckAppendInTx(ctx, tx, entry, key)
		return checked
	})
	if result.State() != f.NotCommitted || checked == nil {
		t.Fatal("retired public Invocation identities acquired fresh append authority")
	}
	requireCode(t, checked, f.Forbidden)
	var count int
	firstRoundRequire(t, c.v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='outbound.access' AND cause_ref=$1`, invocation.String()).Scan(&count))
	if count != 1 {
		t.Fatal("rejected context-free check changed original audit history")
	}
}
