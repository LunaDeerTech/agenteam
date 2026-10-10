//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const firstRoundAnswer = "owned-first-round-answer-中文"

func TestExecutionFirstRound(t *testing.T) {
	t.Run("completed-one-turn", func(t *testing.T) {
		x := newFirstRoundFixture(t, false)
		captured := x.prepare(t)
		driver := x.directDriver(t)
		receipt, err := driver.Start(ctxFor(t), x.capture.created.ID)
		firstRoundRequire(t, err)
		x.requireTerminal(t, captured, receipt, ec.Succeeded)
		firstRoundDriverJoined(t, driver)
	})
	t.Run("start-receipt-loss-recovery", func(t *testing.T) {
		x := newFirstRoundFixture(t, false)
		captured := x.prepare(t)
		driver := x.directDriver(t)
		store := x.capture.v.base.tracked
		ready := false
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if ready || d.Kind != f.JobCause || d.JobType != "execution-direct-text-start" || d.JobID != x.capture.created.ID.String() {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var count int
			err = sql.QueryRow(ctx, `SELECT count(*) FROM agenteam_execution.executions e
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id,s.started_version)=(e.id,e.snapshot_id,e.version)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id,r.start_id)=(e.id,s.id,s.start_id)
 JOIN agenteam_execution.transcript_entries t ON(t.execution_id,t.round_id,t.sequence)=(e.id,r.id,1)
 JOIN agenteam_outbox.events o ON o.id=s.started_event_id
 WHERE e.id=$1 AND e.project_id=$2 AND e.agent_id=$3 AND e.status='running'
 AND e.started_at=s.created_at AND e.completed_at IS NULL AND s.start_id=$4
 AND r.terminal_status IS NULL AND r.transcript_through=1 AND t.kind='input'
 AND o.event_type='execution.started' AND o.aggregate_id=e.id::uuid AND o.aggregate_version=e.version
 AND o.project_id=e.project_id::uuid AND o.schema_version=1
 AND convert_from(o.payload,'UTF8')::jsonb->>'start_id'=s.start_id
 AND convert_from(o.payload,'UTF8')::jsonb->>'round_id'=r.id
 AND convert_from(o.payload,'UTF8')::jsonb->>'call_id'=r.call_id`,
				x.capture.created.ID.String(), x.capture.created.ProjectID.String(), x.capture.created.AgentID.String(), d.JobAttemptID).Scan(&count)
			if err != nil {
				return err
			}
			ready = count == 1
			return nil
		})
		original := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: original, attempt: id[f.TransactionAttempt](t), match: func(cause f.TransactionCause) bool {
			d := cause.Details()
			return ready && d.Kind == f.JobCause && d.JobType == "execution-direct-text-start" && d.JobID == x.capture.created.ID.String()
		}}
		store.fixtureStore = loss
		defer func() { store.setAfter(nil); store.fixtureStore = original }()
		_, err := driver.Start(ctxFor(t), x.capture.created.ID)
		store.setAfter(nil)
		store.fixtureStore = original
		requireCode(t, err, f.CommitUnknown)
		fired, physical := loss.observed()
		if !ready || !fired || physical.State() != f.Committed {
			t.Fatal("start receipt loss did not follow the actual successful COMMIT")
		}
		x.requireWire(t, 0, true)
		x.reader.requireDestroyed(t, 0)
		receipt, err := driver.ResolveUnknown(ctxFor(t), x.capture.created.ID)
		firstRoundRequire(t, err)
		x.requireTerminal(t, captured, receipt, ec.Succeeded)
		firstRoundDriverJoined(t, driver)
	})
	t.Run("cancel-joins-current-call", func(t *testing.T) {
		x := newFirstRoundFixture(t, true)
		captured := x.prepare(t)
		driver := x.directDriver(t)
		ctx, cancel := context.WithCancel(ctxFor(t))
		defer cancel()
		type outcome struct {
			receipt ec.DirectTextReceipt
			err     error
		}
		done := make(chan outcome, 1)
		go func() {
			receipt, err := driver.Start(ctx, x.capture.created.ID)
			done <- outcome{receipt, err}
		}()
		x.requireWire(t, 1, false) // The real provider has sent headers and is holding its body.
		cancel()
		var result outcome
		select {
		case result = <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("cancel did not return the original Execution call")
		}
		if !errors.Is(result.err, context.Canceled) || result.receipt.Status != ec.Cancelled {
			t.Fatal("original cancellation did not retain its cause and committed terminal receipt")
		}
		firstRoundDriverJoined(t, driver)
		x.requireTerminal(t, captured, result.receipt, ec.Cancelled)
	})
}

func firstRoundDriverJoined(t *testing.T, driver *execution.DirectTextDriver) {
	t.Helper()
	driver.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := driver.Drain(ctx); err != nil || !driver.Joined() {
		t.Fatal("original Execution owner did not join", err)
	}
}

func (x *firstRoundFixture) requireTerminal(t *testing.T, captured ec.ExecutionContext, receipt ec.DirectTextReceipt, status ec.Status) {
	t.Helper()
	c := x.capture
	if receipt.ExecutionID != c.created.ID || receipt.Status != status || receipt.StartID.Validate() != nil || receipt.SnapshotID.Validate() != nil || receipt.RoundID.Validate() != nil || receipt.CallID.Validate() != nil || receipt.Version.Validate() != nil || receipt.TranscriptThrough.Validate() != nil {
		t.Fatal("original first-round receipt lost its exact identity or terminal state")
	}
	var actualStatus, snapshot string
	var version, active int64
	var started, completed *time.Time
	err := c.v.base.raw.QueryRow(ctxFor(t), `SELECT e.status,e.version,e.snapshot_id,e.started_at,e.completed_at,
 (SELECT count(*) FROM agenteam_execution.executions n WHERE n.agent_id=e.agent_id AND n.status IN('created','preparing','running','waiting'))
 FROM agenteam_execution.executions e WHERE e.id=$1 AND e.project_id=$2 AND e.agent_id=$3`,
		c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String()).
		Scan(&actualStatus, &version, &snapshot, &started, &completed, &active)
	firstRoundRequire(t, err)
	if actualStatus != string(status) || version != int64(receipt.Version) || snapshot != receipt.SnapshotID.String() || started == nil || completed == nil || completed.Before(*started) || active != 0 {
		t.Fatal("terminal Execution did not atomically retire its original Agent slot")
	}
	x.requireRound(t, captured, receipt)
	originalTask, err := work.DecodeTaskContext(captured.Trigger())
	firstRoundRequire(t, err)
	currentTask, err := c.v.taskReader.GetTask(ctxFor(t), c.v.base.ownerBrowser.actor, c.created.ProjectID, c.v.task.ID)
	firstRoundRequire(t, err)
	beforeTask, err := json.Marshal(originalTask.Task())
	firstRoundRequire(t, err)
	afterTask, err := json.Marshal(currentTask)
	firstRoundRequire(t, err)
	if currentTask.State != wc.TaskStateInProgress || !bytes.Equal(beforeTask, afterTask) {
		t.Fatal("Execution completion changed the independently owned Work Task")
	}
	model := captured.Input().Fields().Model
	var phase, final, dispatch, usageDispatch, usageSource string
	var retired, leaseReleased bool
	var ordinal, sequence, usageSequence, attempts, invocations, reads int64
	var inputTokens, outputTokens, totalTokens *int64
	err = c.v.base.raw.QueryRow(ctxFor(t), `SELECT c.phase,c.retired,l.released,a.ordinal,a.dispatch,a.sequence,i.final_status,i.dispatch,i.last_sequence,
 (SELECT count(*) FROM agenteam_model.runtime_attempts n WHERE n.call_id=c.id),
 (SELECT count(*) FROM agenteam_model.invocations n WHERE n.call_id=c.id),
 (SELECT count(*) FROM agenteam_audit.audit_records n WHERE n.action='secret.resolve' AND n.request_id=a.id),
 i.usage_source,i.input_tokens,i.output_tokens,i.total_tokens
 FROM agenteam_model.calls c
 JOIN agenteam_model.runtime_attempts a ON(a.call_id,a.id,a.project_id,a.process_id,a.fence)=(c.id,c.invocation_id,c.project_id,c.process_id,c.fence)
 JOIN agenteam_model.invocations i ON(i.id,i.call_id,i.attempt_index)=(a.id,a.call_id,a.ordinal)
 JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id
 WHERE c.id=$1::text::uuid AND c.project_id=$2::text::uuid AND c.snapshot_id=$3::text::uuid AND c.lease_id=$4::text::uuid
 AND c.request_data#>>'{Initiator,AgentID}'=$5 AND c.request_data#>>'{Initiator,ExecutionID}'=$6
 AND l.owner_kind='execution' AND l.owner_id=$6::text::uuid AND c.finished_at IS NOT NULL`,
		receipt.CallID.String(), c.created.ProjectID.String(), model.Snapshot.ID.String(), model.CredentialLease.LeaseID.String(), c.created.AgentID.String(), c.created.ID.String()).
		Scan(&phase, &retired, &leaseReleased, &ordinal, &dispatch, &sequence, &final, &usageDispatch, &usageSequence,
			&attempts, &invocations, &reads, &usageSource, &inputTokens, &outputTokens, &totalTokens)
	firstRoundRequire(t, err)
	if phase != string(status) || final != string(status) || !retired || !leaseReleased || ordinal != 1 || attempts != 1 || invocations != 1 || reads != 1 || dispatch != "sent" || usageDispatch != dispatch || sequence != usageSequence {
		t.Fatal("terminal Execution lost original Model, Invocation, Usage or lease retirement facts")
	}
	if status == ec.Succeeded && (usageSource != "provider" || inputTokens == nil || *inputTokens != 3 || outputTokens == nil || *outputTokens != 2 || totalTokens == nil || *totalTokens != 5) {
		t.Fatal("first round did not persist the actual provider Usage")
	}
	if status == ec.Cancelled && (inputTokens != nil || outputTokens != nil || totalTokens != nil) {
		t.Fatal("cancelled held response invented provider Usage")
	}
	var environmentRetired int64
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
		t.Fatal("lease retirement lost historical resource references or retained live access")
	}
	x.requireWire(t, 1, true)
	x.reader.requireDestroyed(t, 1)
}

func (x *firstRoundFixture) requireRound(t *testing.T, captured ec.ExecutionContext, receipt ec.DirectTextReceipt) {
	t.Helper()
	c := x.capture
	var snapshotRaw, roundRaw, inputMessage, responseRaw, assistantMessage, startedPayload, terminalPayload []byte
	var snapshotDigest, roundDigest, reason, terminalName string
	var entries, events, startedVersion, terminalVersion int64
	err := c.v.base.raw.QueryRow(ctxFor(t), `SELECT s.content,s.digest,r.content,r.digest,t.message,r.response,a.message,
 started.payload,terminal.payload,r.terminal_reason,terminal.event_type,s.started_version,r.terminal_version,
 (SELECT count(*) FROM agenteam_execution.transcript_entries n WHERE n.execution_id=e.id),
 (SELECT count(*) FROM agenteam_outbox.events n WHERE n.aggregate_type='execution.execution' AND n.aggregate_id=e.id::uuid)
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id,r.start_id)=(e.id,s.id,s.start_id)
 JOIN agenteam_execution.transcript_entries t ON(t.execution_id,t.round_id,t.sequence)=(e.id,r.id,1)
 LEFT JOIN agenteam_execution.transcript_entries a ON(a.execution_id,a.round_id,a.sequence)=(e.id,r.id,2)
 JOIN agenteam_outbox.events started ON started.id=s.started_event_id
 JOIN agenteam_outbox.events terminal ON terminal.id=r.terminal_event_id
 WHERE e.id=$1 AND e.project_id=$2 AND e.agent_id=$3
 AND s.start_id=$4 AND r.id=$5 AND r.call_id=$6
 AND s.input_digest=$7 AND s.context_digest=$8 AND r.context_digest=s.context_digest
 AND s.process_id=$9 AND s.schema_version=1 AND r.schema_version=1
 AND r.terminal_status=e.status AND r.terminal_version=e.version AND r.finished_at=e.completed_at
 AND s.created_at=e.started_at AND r.created_at=s.created_at
 AND started.event_type='execution.started' AND started.aggregate_version=s.started_version
 AND terminal.aggregate_version=e.version AND started.aggregate_type='execution.execution' AND terminal.aggregate_type='execution.execution'
 AND started.aggregate_id=e.id::uuid AND terminal.aggregate_id=e.id::uuid
 AND started.project_id=e.project_id::uuid AND terminal.project_id=e.project_id::uuid
 AND started.schema_version=1 AND terminal.schema_version=1`,
		c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String(), receipt.StartID.String(), receipt.RoundID.String(), receipt.CallID.String(),
		string(captured.InputDigest()), string(captured.Digest()), (captureProviderProcesses{c.v.agent.guard}).CurrentProcess().String()).
		Scan(&snapshotRaw, &snapshotDigest, &roundRaw, &roundDigest, &inputMessage, &responseRaw, &assistantMessage,
			&startedPayload, &terminalPayload, &reason, &terminalName, &startedVersion, &terminalVersion, &entries, &events)
	firstRoundRequire(t, err)
	snapshot, err := ec.DecodeDirectTextSnapshot(snapshotRaw)
	firstRoundRequire(t, err)
	round, err := ec.DecodeDirectTextRound(roundRaw)
	firstRoundRequire(t, err)
	sv, rv := snapshot.Fields(), round.Fields()
	if sv.ID != receipt.SnapshotID || sv.StartID != receipt.StartID || !bytes.Equal(sv.Context.CanonicalBytes(), captured.CanonicalBytes()) ||
		snapshot.Digest() != f.Digest(snapshotDigest) || round.Digest() != f.Digest(roundDigest) || rv.ID != receipt.RoundID || rv.CallID != receipt.CallID || rv.SnapshotID != sv.ID || rv.StartID != sv.StartID ||
		startedVersion != int64(c.created.Version)+2 || terminalVersion != startedVersion+1 || terminalVersion != int64(receipt.Version) || events != 2 {
		t.Fatal("sealed Snapshot, original Round and lifecycle did not retain their exact input and versions")
	}
	actor, err := i.NewAgentRun(c.created.ProjectID, c.created.AgentID, c.created.ID)
	firstRoundRequire(t, err)
	request, err := agentloop.BuildDirectTextRequest(ctxFor(t), captured, actor, receipt.RoundID.String(), receipt.CallID)
	firstRoundRequire(t, err)
	wantMessages, err := json.Marshal(request.Request().Messages)
	firstRoundRequire(t, err)
	actualMessages, err := json.Marshal(rv.Messages)
	firstRoundRequire(t, err)
	var initial mc.Message
	firstRoundRequire(t, json.Unmarshal(inputMessage, &initial))
	actualInput, err := json.Marshal(initial)
	firstRoundRequire(t, err)
	wantInput, err := json.Marshal(request.InitialInput())
	firstRoundRequire(t, err)
	if !bytes.Equal(actualMessages, wantMessages) || !bytes.Equal(actualInput, wantInput) || bytes.Contains(actualMessages, []byte("owned-model-capture-material")) || bytes.Contains(actualMessages, []byte("owned-environment-capture-material")) {
		t.Fatal("persisted Round or input Transcript changed the actual Loop projection")
	}
	for n, raw := range [][]byte{startedPayload, terminalPayload} {
		var payload ec.DirectTextLifecycle
		firstRoundRequire(t, json.Unmarshal(raw, &payload))
		firstRoundRequire(t, payload.Validate())
		wantStatus, wantReason, through := receipt.Status, reason, receipt.TranscriptThrough
		if n == 0 {
			wantStatus, wantReason, through = ec.Running, "started", 1
		}
		if payload.ExecutionID != receipt.ExecutionID || payload.AgentID != c.created.AgentID || payload.StartID != receipt.StartID || payload.SnapshotID != receipt.SnapshotID || payload.RoundID != receipt.RoundID || payload.CallID != receipt.CallID || payload.Status != wantStatus || payload.Reason != wantReason || payload.TranscriptThrough != through {
			t.Fatal("typed lifecycle was not bound to the original start and terminal outcome")
		}
	}
	if receipt.Status == ec.Succeeded {
		var response mc.ModelResponse
		var assistant mc.Message
		firstRoundRequire(t, json.Unmarshal(responseRaw, &response))
		firstRoundRequire(t, response.Validate())
		firstRoundRequire(t, json.Unmarshal(assistantMessage, &assistant))
		actual, err := json.Marshal(assistant)
		firstRoundRequire(t, err)
		want, err := json.Marshal(response.Message)
		firstRoundRequire(t, err)
		if entries != 2 || receipt.TranscriptThrough != 2 || reason != "completed" || terminalName != "execution.succeeded" || response.CallID != receipt.CallID || response.FinishReason != "stop" || len(response.Message.Parts) != 1 || response.Message.Parts[0].Text == nil || response.Message.Parts[0].Text.Text != firstRoundAnswer || !bytes.Equal(actual, want) {
			t.Fatal("completed first turn did not persist its actual normal response exactly once")
		}
	} else if entries != 1 || receipt.TranscriptThrough != 1 || reason != "cancelled" || terminalName != "execution.cancelled" || responseRaw != nil || assistantMessage != nil {
		t.Fatal("cancelled first turn invented a completed response or assistant Transcript")
	}
}

// The resolver only maps the nonce-owned fixture host to its real private
// container address. Production D04 classification, policy and TLS still run.
type firstRoundResolver struct{ address netip.Addr }

func (r firstRoundResolver) Lookup(ctx context.Context, _ string) ([]netip.Addr, error) {
	return []netip.Addr{r.address}, ctx.Err()
}

type firstRoundFixture struct {
	capture          *modelEnvironmentFixture
	runtimeAuthority *model.RuntimeAuthority
	projects         *project.Authority
	audit            *audit.Service
	secrets          *secret.Service
	reader           *firstRoundCredentialReads
	ledger           *usage.Service
	runtime          *model.Runtime
	budget           *wire.Budget
	network          *netfixture.Descriptor
	scenario         string
	closed           sync.Once
}

// The tap retains only the same opaque material cell returned by D03. It
// grants no lease and never reads/copies the secret while it is live.
type firstRoundCredentialReads struct {
	sc.CredentialUsageReader
	mu        sync.Mutex
	materials []sc.SecretMaterial
}

func (r *firstRoundCredentialReads) ReadCredentialForUsage(ctx context.Context, request sc.UsageRequest) (sc.SecretMaterial, error) {
	material, err := r.CredentialUsageReader.ReadCredentialForUsage(ctx, request)
	if err == nil {
		r.mu.Lock()
		r.materials = append(r.materials, material)
		r.mu.Unlock()
	}
	return material, err
}

func (r *firstRoundCredentialReads) requireDestroyed(t *testing.T, want int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.materials) != want {
		t.Fatal("unexpected number of actual credential reads")
	}
	for _, material := range r.materials {
		called := false
		err := material.Use(func([]byte) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("credential material survived original wire retirement")
		}
	}
}

// All business and invocation authority comes from the real Execution
// instance shared with preparation. There is no test ConsumerAuthority and
// no fixture table supplying running, Round, Call, or credential permission.
func newFirstRoundFixture(t *testing.T, held bool) *firstRoundFixture {
	t.Helper()
	x := &firstRoundFixture{}
	x.capture = newModelEnvironmentFixtureWithPorts(t,
		func(v *taskTransitionFixture, owner *execution.Authority) captureModelPorts {
			modelActor, err := i.RegisterService(i.ModelRuntime)
			firstRoundRequire(t, err)
			secretActor, err := i.RegisterService(i.SecretService)
			firstRoundRequire(t, err)
			outboundActor, err := i.RegisterService(i.OutboundService)
			firstRoundRequire(t, err)
			x.runtimeAuthority, err = model.NewRuntimeAuthority(v.base.tracked, model.RuntimeAuthorizations{
				Consumers: owner, Process: v.agent.guard, ModelRuntime: modelActor,
				SecretService: secretActor, OutboundService: outboundActor,
			})
			firstRoundRequire(t, err)
			secretFacts, err := secret.NewProjectAuditAuthority(v.base.tracked)
			firstRoundRequire(t, err)
			x.projects, err = project.NewAuthority(v.base.tracked, project.AuthorityDependencies{
				Sessions: v.base.accounts, Routes: v.base.accounts,
				AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{
					ac.SecretProducer: secretFacts, ac.AccessProducer: x.runtimeAuthority,
				},
			})
			firstRoundRequire(t, err)
			return captureModelPorts{
				projects: x.projects,
				usageFactory: func(authority *model.Authority) (sc.UsageAuthority, error) {
					return model.NewRuntimeSecretUsageRouter(authority, x.runtimeAuthority, v.base.accounts)
				},
				ready: func(_ *model.Service, secrets *secret.Service, audit *audit.Service) {
					x.secrets, x.audit = secrets, audit
				},
			}
		},
		func(v *taskTransitionFixture, models *model.Service, policy *ec.Policy) {
			// Construction-failure fallback; the successful registration below
			// retires Runtime before the original providers and Object guard.
			t.Cleanup(func() { x.close(t) })
			x.configureWire(t, held)
			current, err := models.GetProjectModel(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, v.agent.modelID)
			firstRoundRequire(t, err)
			provider, err := models.GetProjectProvider(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, current.ProviderID)
			firstRoundRequire(t, err)
			input := provider.Input.Clone()
			input.BaseURL = "https://fixture.test:8443/case/" + x.scenario
			scope, err := i.InProject(v.base.project.ID)
			firstRoundRequire(t, err)
			_, err = models.UpdateProvider(ctxFor(t), mc.UpdateProviderRequest{
				CommandMeta: mc.CommandMeta{Actor: v.base.ownerBrowser.actor, Scope: scope, Key: "first-round-real-wire"},
				ID:          provider.ID, ExpectedVersion: provider.Version, Input: input,
			})
			firstRoundRequire(t, err)
			// The actual capture observes this policy; neither Context nor the
			// resulting Model request has its captured Tool list edited later.
			policy.DeniedToolIDs = []i.ToolID{v.agent.tool.ToolID}
			firstRoundRequire(t, policy.Validate())
		})
	t.Cleanup(func() { x.close(t) })
	v := x.capture.v
	invocations, err := usage.NewAuthority(v.base.tracked, usage.Authorizations{
		Sessions: v.base.accounts, Projects: x.projects, Invocations: x.runtimeAuthority,
	})
	firstRoundRequire(t, err)
	x.ledger, err = usage.New(v.base.tracked, invocations, usage.Dependencies{Cursors: v.base.keys})
	firstRoundRequire(t, err)
	firstRoundRequire(t, x.ledger.Initialize(ctxFor(t)))
	timing, err := mc.NewAgentRetryTiming(mc.AgentRetryTimingFields{
		InitialRequestTimeout: 10 * time.Second, MaxRequestTimeout: 20 * time.Second,
		TimeoutMultiplier: 2, InitialBackoff: 100 * time.Millisecond, MaxBackoff: time.Second,
	})
	firstRoundRequire(t, err)
	adapter, err := wire.NewOpenAIChat(x.transport(t, v), x.budget)
	firstRoundRequire(t, err)
	x.reader = &firstRoundCredentialReads{CredentialUsageReader: x.secrets}
	x.runtime, err = model.NewRuntimeWithAgentRetry(v.base.tracked, x.runtimeAuthority, model.RuntimeDependencies{
		Usage: x.ledger, SecretReader: x.reader, SecretUsage: x.secrets, Adapter: adapter,
	}, timing)
	firstRoundRequire(t, err)
	firstRoundRequire(t, x.runtime.Initialize(ctxFor(t)))
	return x
}

func (x *firstRoundFixture) configureWire(t *testing.T, held bool) {
	t.Helper()
	var err error
	x.network, err = netfixture.Load()
	firstRoundRequire(t, err)
	response, err := json.Marshal(map[string]any{
		"id": "first-round-owned", "object": "chat.completion", "created": 1, "model": "fixture-native-model",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": firstRoundAnswer}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
	})
	firstRoundRequire(t, err)
	scenario := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{response}}
	if held {
		beforeBody := 0
		scenario.HoldAfter = &beforeBody
	}
	x.scenario, err = x.network.Create(ctxFor(t), netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &scenario})
	firstRoundRequire(t, err)
	x.budget = wire.NewBudget()
}

func (x *firstRoundFixture) transport(t *testing.T, v *taskTransitionFixture) wire.Transport {
	t.Helper()
	policy, err := outbound.NewPolicyService(v.base.tracked, x.audit, outbound.Authorizations{Sessions: v.base.accounts, System: v.base.accounts})
	firstRoundRequire(t, err)
	firstRoundRequire(t, policy.Reload(ctxFor(t)))
	ports, err := outbound.SelectedPorts(8443)
	firstRoundRequire(t, err)
	rule, err := outbound.NewRule(x.network.PrivateIP+"/32", ports, false)
	firstRoundRequire(t, err)
	rules, err := outbound.NewRules(rule)
	firstRoundRequire(t, err)
	actor := v.base.adminBrowser.actor
	command, err := f.NewCommandIdentity("outbound-policy", []string{actor.Details().UserID}, "update", f.IdempotencyKey(id[struct{}](t).String()))
	firstRoundRequire(t, err)
	status := policy.Status()
	if status.Version == nil {
		t.Fatal("actual outbound policy version missing")
	}
	_, err = policy.UpdatePolicy(ctxFor(t), actor, outbound.CommandMeta{Identity: command, ExpectedVersion: *status.Version, HTTPTraceID: id[struct{}](t).String()}, rules)
	firstRoundRequire(t, err)
	trust, err := outbound.LoadTrustStore(x.network.CAFile)
	firstRoundRequire(t, err)
	address, err := netip.ParseAddr(x.network.PrivateIP)
	firstRoundRequire(t, err)
	return wire.Transport{Policy: policy, Trust: trust, Resolver: firstRoundResolver{address}}
}

func (x *firstRoundFixture) prepare(t *testing.T) ec.ExecutionContext {
	t.Helper()
	driver := x.capture.newDriver(t, true)
	firstRoundRequire(t, driver.Run(ctxFor(t), x.capture.created.ID))
	x.capture.requireProviderCalls(t, true)
	// The older capture test intentionally requires its nonempty Tool set;
	// this profile reads its own real empty-set input without relaxing it.
	input := x.preparedInput(t)
	firstRoundRequire(t, x.capture.models.readErr)
	firstRoundRequire(t, x.capture.environment.readErr)
	fields := input.Fields()
	if fields.Tools == nil || len(fields.Tools) != 0 || len(fields.Skills.Bindings) != 1 || fields.Agent.Fields().Core.InjectAgentsMD || len(fields.Mounts.Mounts) != 0 {
		t.Fatal("formal preparation did not capture the supported direct-text profile")
	}
	counts, err := captureInputCounts(ctxFor(t), x.capture.v.base.raw, x.capture.created.ID.String())
	firstRoundRequire(t, err)
	if counts != [11]int64{1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1} {
		t.Fatal("formal preparation input and actual resource references were not committed together")
	}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	built, err := builder.BuildContext(ctxFor(t), input)
	firstRoundRequire(t, err)
	return built
}

func (x *firstRoundFixture) preparedInput(t *testing.T) ec.PreparationInput {
	t.Helper()
	c := x.capture
	var raw []byte
	var digest, binding, launch, command, request string
	err := c.v.base.raw.QueryRow(ctxFor(t), `SELECT p.input,p.input_digest,p.attempt_binding,p.launch_digest,p.command_identity,p.request_id
 FROM agenteam_execution.preparation_inputs p
 JOIN agenteam_execution.preparation_claims c ON(c.execution_id,c.attempt_id,c.process_id,c.fence)=(p.execution_id,p.attempt_id,p.process_id,p.fence)
 JOIN agenteam_execution.preparation_attempts a ON(a.execution_id,a.attempt_id,a.process_id,a.fence)=(p.execution_id,p.attempt_id,p.process_id,p.fence)
 JOIN agenteam_execution.executions e ON(e.id,e.project_id,e.agent_id)=(p.execution_id,p.project_id,p.agent_id)
 WHERE p.execution_id=$1 AND p.project_id=$2 AND p.agent_id=$3 AND p.process_id=$4 AND p.fence=1 AND p.schema_version=1
 AND e.status='preparing' AND e.snapshot_id IS NULL AND e.started_at IS NULL AND e.completed_at IS NULL AND e.cancel_requested_at IS NULL
 AND e.version=$5 AND e.request_digest=p.launch_digest AND a.phase='terminal'`,
		c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String(),
		(captureProviderProcesses{c.v.agent.guard}).CurrentProcess().String(), int64(c.created.Version)+1).
		Scan(&raw, &digest, &binding, &launch, &command, &request)
	firstRoundRequire(t, err)
	input, err := ec.DecodePreparationInput(raw)
	firstRoundRequire(t, err)
	wantDigest, err := c.request.Digest()
	firstRoundRequire(t, err)
	wantCommand, err := c.request.Command()
	firstRoundRequire(t, err)
	if !bytes.Equal(raw, input.CanonicalBytes()) || digest != string(input.Digest()) || binding != string(input.Fields().AttemptBinding) ||
		launch != string(wantDigest) || command != wantCommand.Canonical() || request != c.request.Meta.RequestID.String() ||
		!input.Fields().Request.Equal(ec.PreparationRequest{ExecutionID: c.created.ID, Launch: c.request}) {
		t.Fatal("complete input lost the actual preparation claim or original Launch identity")
	}
	return input
}

func (x *firstRoundFixture) directDriver(t *testing.T) *execution.DirectTextDriver {
	t.Helper()
	v := x.capture.v
	contextBuilder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	loop, err := agentloop.NewDirectTextController(x.runtime)
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		loop.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := loop.Drain(ctx); err != nil || !loop.Joined() {
			t.Error("original Loop sessions did not join", err)
		}
	})
	skills, err := skill.NewInitialRoundBindings(v.agent.providers.SkillAuthority)
	firstRoundRequire(t, err)
	models, err := model.NewExecutionLeaseRetirement(x.runtimeAuthority, x.secrets)
	firstRoundRequire(t, err)
	environmentAuthority, err := pv.NewEnvironmentRetirementAuthority(v.base.authority, x.capture.authority)
	firstRoundRequire(t, err)
	secretRetirement, err := secret.NewProjectVariableLeaseRetirement(x.capture.environmentSecrets, environmentAuthority)
	firstRoundRequire(t, err)
	environment, err := pv.NewEnvironmentRetirement(environmentAuthority, secretRetirement)
	firstRoundRequire(t, err)
	catalog := event.NewCatalog()
	lifecycle, err := ec.RegisterDirectTextEvents(catalog)
	firstRoundRequire(t, err)
	eventAuthority, err := execution.NewDirectTextEventAuthority(x.capture.authority)
	firstRoundRequire(t, err)
	box, err := outbox.New(v.base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{ec.ExecutionProducer: eventAuthority},
		Sessions:  v.base.accounts, System: v.base.accounts, Projects: v.base.projectAuthority,
		Audit: v.base.audit, Cursors: v.base.keys, Processes: captureProviderProcesses{v.agent.guard},
	})
	firstRoundRequire(t, err)
	driver, err := execution.NewDirectTextDriver(v.base.tracked, x.capture.authority, execution.DirectTextDependencies{
		Context: contextBuilder, Loop: loop, Processes: captureProviderProcesses{v.agent.guard},
		Projects: v.base.projectAuthority, Agents: x.capture.configuration, Skills: skills,
		Events: box, Lifecycle: lifecycle, Models: models, Environment: environment,
	})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil || !driver.Joined() {
			t.Error("original Execution calls did not join before Loop and Model cleanup", err)
		}
	})
	return driver
}

func firstRoundRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("real first-round dependency", err)
	}
}

func (x *firstRoundFixture) requireWire(t *testing.T, count int, joined bool) netfixture.State {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := x.network.State(ctx, x.scenario)
		if err == nil && len(state.Requests) > count {
			t.Fatal("original Execution dispatched an extra provider request")
		}
		ready := err == nil && len(state.Requests) == count
		if joined {
			ready = ready && state.ActiveHandlers == 0 && state.CompletedHandlers == count
			for _, request := range state.Requests {
				ready = ready && state.Closed[request.Connection]
			}
		} else {
			ready = ready && state.ActiveHandlers == count
		}
		if ready {
			for _, request := range state.Requests {
				if request.Method != "POST" || request.Headers.Get("Authorization") != "Bearer owned-model-capture-material" {
					t.Fatal("real model credential was not handed to the owned HTTPS provider")
				}
			}
			return state
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("original owned provider call did not reach its required phase")
		}
	}
}

func (x *firstRoundFixture) close(t *testing.T) {
	t.Helper()
	x.closed.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// This is fixture-wide fallback only. Per-Execution completion must
		// independently join its own scoped call before writing terminal facts.
		if x.runtime != nil {
			x.runtime.StopAdmission()
			if err := x.runtime.Drain(ctx); err != nil || !x.runtime.Joined() {
				t.Error("original shared Model Runtime did not join", err)
				return
			}
		}
		if x.budget != nil {
			if err := x.budget.Force(ctx); err != nil || !x.budget.Joined() {
				t.Error("original D04 transport did not join", err)
				return
			}
		}
		if x.network != nil && x.scenario != "" {
			if err := x.network.Release(ctx, x.scenario); err != nil {
				t.Error("owned wire release", err)
			}
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				state, err := x.network.State(ctx, x.scenario)
				settled := err == nil && state.ActiveHandlers == 0 && state.CompletedHandlers == len(state.Requests)
				for _, request := range state.Requests {
					settled = settled && state.Closed[request.Connection]
				}
				if settled {
					break
				}
				select {
				case <-tick.C:
				case <-ctx.Done():
					t.Error("owned D04 handler and connection tails did not join")
					return
				}
			}
		}
	})
}
