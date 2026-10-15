//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Both processes use the installed App, original database, deployment keys and
// spool. Only a completed transaction return / controlled HTTP response is
// held. The parent never changes business rows or fabricates a stopped claim.
func TestModelM3AppStoppedAgentCall(t *testing.T) {
	for _, mode := range []string{"authorized", "sent"} {
		t.Run(mode, func(t *testing.T) {
			w, _ := newM1WorkflowWithObserver(t, nil)
			w.seed(t)
			w.startRunner(t)
			w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
				response := w.responses(t)[0]
				// The owned sequence API requires at least two ordinals. Any
				// second request is still a failure of the exact 0/1 count below.
				return []netfixture.WireScenario{response, {Status: 503, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{[]byte(`{"error":"unexpected_cold_redispatch"}`)}}}
			}
			w.createModelAndTask(t, "https://fixture.test:8443/model-cold-not-yet-scheduled")
			taskExecutionJoinEnabled(t, w.app)
			first := m3ModelStartChild(t, w, mode, "")
			m2RecoveryEvent(t, first, "m3_model_"+mode+"_held", nil)
			call, invocation, execution, beforeSequence := m3ModelAwaitCall(t, w, first, mode)
			var immutable, leaseBefore string
			workAppCheck(t, "model-cold-original-binding", w.app.admin.QueryRow(w.app.ctx, `SELECT jsonb_build_array(c.id,c.project_id,c.snapshot_id,c.preparation_id,c.lease_id,c.invocation_id,c.process_id,c.fence,c.request_binding,c.request_data,c.accepted_at,a.id,a.call_id,a.project_id,a.ordinal,a.process_id,a.fence,((a.fact_data->'Value') - ARRAY['final','dispatch']::text[]))::text, to_jsonb(l)::text FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, call).Scan(&immutable, &leaseBefore))
			m2RecoveryGuardHeld(t, w, first.process)
			m2KillRecoveryChild(t, w, first)
			workAppCheck(t, "model-cold-peer-release-after-death", w.network.Release(w.app.ctx, w.scenario))
			requests := 0
			if mode == "sent" {
				requests = 1
			}
			w.joinedPeer(t, w.scenario, requests, requests)

			gate := filepath.Join(t.TempDir(), "release-initialize")
			second := m3ModelStartChild(t, w, "recover", gate)
			m2RecoveryEvent(t, second, "m3_model_recovered_held", nil)
			// This barrier is after the real Model Initialize transaction and
			// before Executor startup. Model must not retire the E lease.
			var afterImmutable, afterLease, phase, dispatch, final string
			var retired, borrowersClosed bool
			var sequence, receiptCount, summaryUnknown, totalCalls, totalAttempts int64
			workAppCheck(t, "model-cold-recovered-original-facts", w.app.admin.QueryRow(w.app.ctx, `SELECT jsonb_build_array(c.id,c.project_id,c.snapshot_id,c.preparation_id,c.lease_id,c.invocation_id,c.process_id,c.fence,c.request_binding,c.request_data,c.accepted_at,a.id,a.call_id,a.project_id,a.ordinal,a.process_id,a.fence,((a.fact_data->'Value') - ARRAY['final','dispatch']::text[]))::text,to_jsonb(l)::text,c.phase,c.retired,a.dispatch,a.sequence,u.final_status,
 (SELECT count(*) FROM agenteam_model.invocation_observations WHERE invocation_id=c.invocation_id),
 NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls WHERE lane='invocation' AND call_id=c.id AND process_id=c.process_id AND phase<>'business_closed'),
 (SELECT unknown FROM agenteam_model.execution_usage_summaries WHERE execution_id=$2),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id=c.project_id),
 (SELECT count(*) FROM agenteam_model.runtime_attempts WHERE project_id=c.project_id)
 FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_model.invocations u ON u.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, call, execution).Scan(&afterImmutable, &afterLease, &phase, &retired, &dispatch, &sequence, &final, &receiptCount, &borrowersClosed, &summaryUnknown, &totalCalls, &totalAttempts))
			steps, wantDispatch := int64(1), "sent"
			if mode == "authorized" {
				steps, wantDispatch = 2, "unknown"
			}
			if immutable != afterImmutable || leaseBefore != afterLease || phase != "unknown" || !retired || dispatch != wantDispatch || final != "unknown" || sequence != beforeSequence+steps || receiptCount != sequence || !borrowersClosed || summaryUnknown != 1 || totalCalls != 1 || totalAttempts != 1 {
				t.Fatalf("Model cold facts mismatch: immutable=%t execution_lease_preserved=%t phase=%s retired=%t dispatch=%s final=%s steps=%d receipts=%d closed=%t unknown=%d calls=%d attempts=%d", immutable == afterImmutable, leaseBefore == afterLease, phase, retired, dispatch, final, sequence-beforeSequence, receiptCount, borrowersClosed, summaryUnknown, totalCalls, totalAttempts)
			}
			_ = invocation // Captured by the exact original parent tuple query.
			workAppCheck(t, "model-cold-release-initialize-return", os.WriteFile(gate, []byte("continue\n"), 0600))
			ready := m2RecoveryEvent(t, second, "m2_recovery_ready", nil)
			second.process, _ = ready["process_id"].(string)
			if second.process == first.process {
				t.Fatal("Model restart reused old ProcessGuard identity")
			}
			m2StopRecoveryChild(t, w, second)
			w.joinedPeer(t, w.scenario, requests, requests)
			m2StopRecoveryRunner(t, w)
			t.Logf("MODEL_M3_COLD mode=%s actual_sigkill_wait=true exact_old_call_attempt=true usage_receipt_steps=%d borrowers_closed=true execution_lease_preserved_at_model_boundary=true no_resend=true app_join=true", mode, steps)
		})
	}
}

func m3ModelAwaitCall(t *testing.T, w *m1Workflow, child *m2RecoveryChild, dispatch string) (string, string, string, int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 20*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var exists bool
		workAppCheck(t, "model-cold-dispatch-barrier", w.app.admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id WHERE c.project_id=$1 AND c.process_id=$2 AND NOT c.retired AND c.phase='running' AND a.dispatch=$3)`, w.app.a.String(), child.process, dispatch).Scan(&exists))
		if exists {
			var call, invocation, execution string
			var sequence int64
			workAppCheck(t, "model-cold-original-call", w.app.admin.QueryRow(ctx, `SELECT c.id,c.invocation_id,c.request_data#>>'{Consumer,execution_id}',a.sequence FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id WHERE c.project_id=$1 AND c.process_id=$2 AND NOT c.retired`, w.app.a.String(), child.process).Scan(&call, &invocation, &execution, &sequence))
			return call, invocation, execution, sequence
		}
		select {
		case <-ctx.Done():
			t.Fatal("original Model did not reach its bounded dispatch barrier")
		case <-child.done:
			m2RecoveryChildWaitLog(t, child, "model-barrier")
			t.Fatal("original Model child exited before dispatch")
		case <-tick.C:
		}
	}
}

// These are two real gaps around admission/return, not invented call rows.
// Recovery must close the exact Usage borrower without writing an Invocation.
func TestModelM3AppStoppedBorrower(t *testing.T) {
	for _, mode := range []string{"borrowed", "retired"} {
		t.Run(mode, func(t *testing.T) {
			w, _ := newM1WorkflowWithObserver(t, nil)
			w.seed(t)
			w.startRunner(t)
			w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
				return []netfixture.WireScenario{w.responses(t)[0], {Status: 503, Chunks: [][]byte{[]byte(`{"error":"unexpected_cold_redispatch"}`)}}}
			}
			w.createModelAndTask(t, "https://fixture.test:8443/model-tail-not-yet-scheduled")
			taskExecutionJoinEnabled(t, w.app)
			first := m3ModelStartChild(t, w, mode, "")
			m2RecoveryEvent(t, first, "m3_model_"+mode+"_held", nil)
			var owner, call, originalOwner string
			workAppCheck(t, "model-tail-original-owner", w.app.admin.QueryRow(w.app.ctx, `SELECT id,call_id,(to_jsonb(u)-ARRAY['phase','closed_at']::text[])::text FROM agenteam_model.usage_project_calls u WHERE project_id=$1 AND process_id=$2 AND lane='invocation' AND phase='running'`, w.app.a.String(), first.process).Scan(&owner, &call, &originalOwner))
			facts := func() string {
				var value string
				workAppCheck(t, "model-tail-original-call-facts", w.app.admin.QueryRow(w.app.ctx, `SELECT jsonb_build_array(
 (SELECT to_jsonb(c) FROM agenteam_model.calls c WHERE id=$1),
 (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_model.runtime_attempts a WHERE call_id=$1),
 (SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM agenteam_model.invocations i WHERE call_id=$1),
 (SELECT jsonb_agg(to_jsonb(o) ORDER BY sequence) FROM agenteam_model.invocation_observations o WHERE invocation_id IN(SELECT id FROM agenteam_model.runtime_attempts WHERE call_id=$1)),
 (SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM agenteam_secret.secret_leases l))::text`, call).Scan(&value))
				return value
			}
			before := facts()
			var calls, invocations int
			workAppCheck(t, "model-tail-original-shape", w.app.admin.QueryRow(w.app.ctx, `SELECT (SELECT count(*) FROM agenteam_model.calls WHERE id=$1),(SELECT count(*) FROM agenteam_model.invocations WHERE call_id=$1)`, call).Scan(&calls, &invocations))
			want := 0
			if mode == "retired" {
				want = 1
			}
			if calls != want || invocations != want {
				t.Fatal("tail barrier did not capture the original admission/return gap")
			}
			m2RecoveryGuardHeld(t, w, first.process)
			m2KillRecoveryChild(t, w, first)
			w.joinedPeer(t, w.scenario, want, want)
			gate := filepath.Join(t.TempDir(), "release-initialize")
			second := m3ModelStartChild(t, w, "recover", gate)
			m2RecoveryEvent(t, second, "m3_model_recovered_held", nil)
			var afterOwner string
			var closed bool
			workAppCheck(t, "model-tail-closed-original-owner", w.app.admin.QueryRow(w.app.ctx, `SELECT (to_jsonb(u)-ARRAY['phase','closed_at']::text[])::text,phase='business_closed' AND closed_at IS NOT NULL FROM agenteam_model.usage_project_calls u WHERE id=$1`, owner).Scan(&afterOwner, &closed))
			if originalOwner != afterOwner || !closed || before != facts() {
				t.Fatal("tail closure changed immutable owner, Model/Usage facts or lease")
			}
			workAppCheck(t, "model-tail-release-initialize", os.WriteFile(gate, []byte("continue\n"), 0600))
			ready := m2RecoveryEvent(t, second, "m2_recovery_ready", nil)
			second.process, _ = ready["process_id"].(string)
			m2StopRecoveryChild(t, w, second)
			w.joinedPeer(t, w.scenario, want, want)
			m2StopRecoveryRunner(t, w)
			t.Logf("MODEL_M3_TAIL mode=%s actual_sigkill_wait=true original_borrower_closed=true model_usage_lease_unchanged=true no_resend=true app_join=true", mode)
		})
	}
}

// The real Memory Tool supplies the private ModelCallOwner. Its business
// recovery is deliberately outside this boundary: the second process is killed
// after Model Initialize, before Executor can attempt domain reconciliation.
func TestModelM3AppStoppedModelCallLease(t *testing.T) {
	w, agentScenario, memoryScenario, embeddingScenario := newM3ModelCallColdWorkflow(t)
	v := w.app
	first := m3ModelStartChild(t, w, "memory-authorized", "")
	m2RecoveryEvent(t, first, "m3_model_memory-authorized_held", nil)
	var call, lease, immutable, leaseImmutable string
	var sequence int64
	workAppCheck(t, "model-call-original-private-owner", v.admin.QueryRow(v.ctx, `SELECT c.id,l.id,jsonb_build_array(c.request_binding,c.request_data,c.snapshot_id,c.preparation_id,c.invocation_id,c.process_id,c.fence,((a.fact_data->'Value')-ARRAY['dispatch','final']::text[]))::text,(to_jsonb(l)-'released')::text,a.sequence FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.process_id=$1 AND c.request_data#>>'{Consumer,kind}'='memory' AND NOT c.retired AND l.owner_kind='model_call' AND l.owner_id=c.id AND NOT l.released`, first.process).Scan(&call, &lease, &immutable, &leaseImmutable, &sequence))
	m2RecoveryGuardHeld(t, w, first.process)
	m2KillRecoveryChild(t, w, first)
	w.joinedPeer(t, agentScenario, 1, 1)
	w.joinedPeer(t, memoryScenario, 0, 0)
	w.joinedPeer(t, embeddingScenario, 0, 0)
	second := m3ModelStartChild(t, w, "recover", filepath.Join(t.TempDir(), "unreleased-model-boundary"))
	event := m2RecoveryEvent(t, second, "m3_model_recovered_held", nil)
	second.process, _ = event["process_id"].(string)
	var after, afterLease string
	var retired, released, closed bool
	var afterSequence, receipts int64
	workAppCheck(t, "model-call-cold-secret-retired", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(c.request_binding,c.request_data,c.snapshot_id,c.preparation_id,c.invocation_id,c.process_id,c.fence,((a.fact_data->'Value')-ARRAY['dispatch','final']::text[]))::text,(to_jsonb(l)-'released')::text,c.retired AND c.phase='unknown' AND i.final_status='unknown',l.released,a.sequence,(SELECT count(*) FROM agenteam_model.invocation_observations WHERE invocation_id=c.invocation_id),NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls WHERE call_id=c.id AND process_id=c.process_id AND lane='invocation' AND phase<>'business_closed') FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1 AND l.id=$2`, call, lease).Scan(&after, &afterLease, &retired, &released, &afterSequence, &receipts, &closed))
	if after != immutable || afterLease != leaseImmutable || !retired || !released || !closed || afterSequence != sequence+2 || receipts != afterSequence {
		t.Fatalf("ModelCall cold boundary: immutable=%t lease_identity=%t retired=%t released=%t closed=%t steps=%d receipts=%d", after == immutable, afterLease == leaseImmutable, retired, released, closed, afterSequence-sequence, receipts)
	}
	m2KillRecoveryChild(t, w, second)
	w.joinedPeer(t, agentScenario, 1, 1)
	w.joinedPeer(t, memoryScenario, 0, 0)
	w.joinedPeer(t, embeddingScenario, 0, 0)
	t.Log("MODEL_M3_MODEL_CALL_LEASE actual_memory_tool_owner=true original_sigkill_wait=true original_secret_lease_released=true usage_closed=true no_model_redispatch=true second_model_boundary_sigkill_wait=true memory_business_recovery_not_asserted=true graceful_app_join_not_asserted=true")
}

func newM3ModelCallColdWorkflow(t *testing.T) (*m1Workflow, string, string, string) {
	t.Helper()
	network, err := netfixture.Load()
	workAppCheck(t, "model-call-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "model-call-peer-address", err)
	w := &m1Workflow{network: network}
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range memoryM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		w.environment = slices.Clone(env)
		v.root.cfg = guardConfiguration(t, env)
		v.store.observeCommit = m1CommitObserver(t)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver, transport = appFixtureResolver{address}, actual
			return nil
		}
	})
	w.app = v
	m1AllowOwnedPeer(t, v, transport, network)
	var rawTool string
	workAppCheck(t, "model-call-retain-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:retain'`).Scan(&rawTool))
	tool, err := f.ParseID[id.Tool](rawTool)
	workAppCheck(t, "model-call-retain-id", err)
	sentinel := netfixture.WireScenario{Status: 503, Chunks: [][]byte{[]byte(`{"error":"unexpected_cold_dispatch"}`)}}
	agentScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{memoryM3ToolReply(t, tool, map[string]any{"content": memoryM3Fact}, 1), sentinel}})
	workAppCheck(t, "model-call-agent-peer", err)
	memoryScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{sentinel, sentinel}})
	workAppCheck(t, "model-call-memory-peer", err)
	embeddingScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &sentinel})
	workAppCheck(t, "model-call-embedding-peer", err)
	model := memoryM3ConfigureModels(t, v, agentScenario, memoryScenario, embeddingScenario)
	v.agent = guardID[id.Agent](t)
	disabled := false
	intent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: v.agent, Name: "model-cold-memory", Description: "Original Memory ModelCall owner", Instructions: "Retain the requested lesson.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled})
	workAppCheck(t, "model-call-agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
	workAppCheck(t, "model-call-agent-create", err)
	sprint := memoryM3CreateSprint(t, v)
	_ = memoryM3CreateTask(t, v, sprint.ID, v.agent, "Retain the migration lesson.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "model-call-sprint-start", err)
	taskExecutionJoinEnabled(t, v)
	return w, agentScenario, memoryScenario, embeddingScenario
}

func m3ModelStartChild(t *testing.T, w *m1Workflow, mode, gate string, target ...string) *m2RecoveryChild {
	t.Helper()
	executable, err := os.Executable()
	workAppCheck(t, "model-cold-test-executable", err)
	address, err := url.Parse(w.app.address)
	workAppCheck(t, "model-cold-listener", err)
	env := slices.Clone(w.environment)
	for n, value := range env {
		if strings.HasPrefix(value, "AGENTEAM_CENTRAL_HTTP_ADDR=") {
			env[n] = "AGENTEAM_CENTRAL_HTTP_ADDR=" + address.Host
		}
	}
	p := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestModelM3ColdChild$", "-test.timeout=80s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(env, "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env), "AGENTEAM_M3_MODEL_COLD_CHILD="+mode, "AGENTEAM_M3_MODEL_COLD_DATABASE="+w.app.root.db.Name, "AGENTEAM_M3_MODEL_COLD_GATE="+gate)
	if len(target) > 1 {
		t.Fatal("too many original Model targets")
	}
	if len(target) == 1 {
		p.cmd.Env = append(p.cmd.Env, "AGENTEAM_M3_MODEL_COLD_TARGET="+target[0])
	}
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	workAppCheck(t, "model-cold-child-start", p.cmd.Start())
	t.Logf("MODEL_M3_CHILD_START mode=%s pid=%d", mode, p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		defer m2RecoveryChildWaitLog(t, p, "model-cleanup")
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			usageAppWait(t, p.done, "model-cold-owned-child-cleanup-wait")
		}
	})
	if !strings.HasPrefix(mode, "recover") {
		ready := m2RecoveryEvent(t, p, "m2_recovery_ready", nil)
		p.process, _ = ready["process_id"].(string)
		_, err = f.ParseID[oc.Process](p.process)
		workAppCheck(t, "model-cold-child-process", err)
	}
	return p
}

type m3ModelColdStore struct {
	*postgres.Store
	mode, gate string
	held       atomic.Bool
	process    func() string
	proxy      *commitproxy.Proxy
	target     string
	armed      atomic.Bool
	original   f.CommitResult
}

func (s *m3ModelColdStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if s.proxy != nil {
		return s.withUnknown(ctx, cause, fn)
	}
	return s.observeCommitted(ctx, cause, s.Store.WithinTx(ctx, cause, fn))
}

func (s *m3ModelColdStore) observeCommitted(ctx context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
	if result.State() != f.Committed {
		return result
	}
	d := cause.Details()
	if s.mode == "borrowed" && d.Kind == f.RecoveryCause && d.Owner == "usage-call-register" {
		var borrowed bool
		err := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls u WHERE u.lane='invocation' AND u.phase='running' AND NOT EXISTS(SELECT 1 FROM agenteam_model.calls c WHERE c.id=u.call_id))`).Scan(&borrowed)
		if err != nil {
			m2RecoveryFailure("model-borrower-observation", err)
		} else if borrowed && s.held.CompareAndSwap(false, true) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_model_borrowed_held"})
			select {}
		}
	}
	if (s.mode == "retired" || s.mode == "memory-authorized") && d.Kind == f.CommandsCause && d.Primary.Namespace() == "model.usage" && d.Primary.Command() == "call-ledger" {
		query := `SELECT EXISTS(SELECT 1 FROM agenteam_model.calls c JOIN agenteam_model.usage_project_calls u ON u.call_id=c.id AND u.process_id=c.process_id WHERE c.retired AND u.lane='invocation' AND u.phase='running')`
		if s.mode == "memory-authorized" {
			query = `SELECT EXISTS(SELECT 1 FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id WHERE NOT c.retired AND c.phase='running' AND a.dispatch='authorized' AND c.request_data#>>'{Consumer,kind}'='memory' AND a.fact_data#>'{Value,final}'='null'::jsonb)`
		}
		var reached bool
		if err := s.Store.QueryRow(ctx, query).Scan(&reached); err != nil {
			m2RecoveryFailure("model-owned-boundary-observation", err)
		} else if reached && s.held.CompareAndSwap(false, true) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_model_" + s.mode + "_held"})
			select {}
		}
	}
	if (s.mode == "authorized" || s.mode == "sent") && d.Kind == f.CommandsCause && d.Primary.Namespace() == "model.usage" && d.Primary.Command() == "call-ledger" {
		var heldDispatch bool
		err := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id WHERE NOT c.retired AND c.phase='running' AND a.dispatch=$1 AND a.fact_data#>'{Value,final}'='null'::jsonb)`, s.mode).Scan(&heldDispatch)
		if err != nil {
			m2RecoveryFailure("model-dispatch-observation", err)
		} else if heldDispatch && s.held.CompareAndSwap(false, true) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_model_" + s.mode + "_held"})
			select {} // Only the parent's actual SIGKILL releases this boundary.
		}
	}
	if strings.HasPrefix(s.mode, "recover") && d.Kind == f.RecoveryCause && d.Owner == "model.runtime-initialize" && s.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_model_recovered_held", "process_id": s.process()})
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := os.Stat(s.gate); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return result
			case <-tick.C:
			}
		}
	}
	return result
}

func TestModelM3ColdChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_M3_MODEL_COLD_CHILD")
	if mode == "" {
		return
	}
	if mode != "authorized" && mode != "sent" && mode != "recover" && mode != "borrowed" && mode != "retired" && mode != "memory-authorized" && mode != "recover-commit" && mode != "recover-rollback" {
		t.Fatal("unknown owned Model cold child mode")
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "model-database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_M3_MODEL_COLD_DATABASE"), nil)
	m2RecoveryCheck(t, "model-database-config", err)
	network, err := netfixture.Load()
	m2RecoveryCheck(t, "model-network-fixture", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	m2RecoveryCheck(t, "model-network-address", err)
	env := os.Environ()
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, env)
	m2RecoveryCheck(t, "model-config", err)
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	var owned *resources
	store := &m3ModelColdStore{mode: mode, gate: os.Getenv("AGENTEAM_M3_MODEL_COLD_GATE"), target: os.Getenv("AGENTEAM_M3_MODEL_COLD_TARGET")}
	if strings.HasPrefix(mode, "recover-") {
		_, err := f.ParseID[mc.Call](store.target)
		m2RecoveryCheck(t, "model-unknown-original-target", err)
	}
	d := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			var err error
			if strings.HasPrefix(mode, "recover-") {
				store.proxy, err = commitproxy.New(ctx, net.JoinHostPort("127.0.0.1", fixture.Port))
				if err != nil {
					return nil, err
				}
				t.Cleanup(func() {
					store.proxy.Release()
					joined, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					m2RecoveryCheck(t, "model-original-proxy-join", store.proxy.Close(joined))
				})
				address, err := url.Parse(fixture.URL(os.Getenv("AGENTEAM_M3_MODEL_COLD_DATABASE")))
				if err != nil {
					return nil, err
				}
				address.Host = store.proxy.Address()
				cfg, err = fixture.Config(os.Getenv("AGENTEAM_M3_MODEL_COLD_DATABASE"), map[string]string{"URL": address.String(), "TLS_MODE": "disable"})
				if err != nil {
					return nil, err
				}
			}
			store.Store, err = postgres.Open(ctx, cfg)
			return store, err
		},
		bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, deps *dependencies) error {
			owned = actual
			store.process = func() string { return actual.objects().(*objectAssembly).process.String() }
			return bindAccounts(ctx, cfg, db, actual, deps)
		},
		outboundConstruct: func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			return nil
		},
	}
	logs := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(os.Stderr, logs))
	m2RecoveryCheck(t, "model-logger", err)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, d); close(done) }()
	startup := time.NewTimer(40 * time.Second)
	defer startup.Stop()
listening:
	for {
		select {
		case event := <-logs.events:
			if event["event"] == "listening" {
				break listening
			}
		case <-done:
			if mode == "recover-rollback" {
				var unknown *model.UnknownCommandError
				if !errors.As(runErr, &unknown) || store.original.State() != f.Unknown || unknown.AttemptID() != store.original.AttemptID() || unknown.Cause().Details().Primary.Canonical() != store.original.Cause().Details().Primary.Canonical() || owned == nil || !owned.producersJoined() {
					m2RecoveryFailure("model-unknown-original-return", runErr)
					t.Fatal("original rollback Unknown or startup owners were lost")
				}
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_model_unknown_returned", "original_unknown": true, "joined": true, "process_id": store.process()})
				return
			}
			m2RecoveryFailure("model-startup-return", runErr)
			t.Fatal("Model cold child returned before listening")
		case <-startup.C:
			m2RecoveryCheck(t, "model-startup-timeout", context.DeadlineExceeded)
		}
	}
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil {
		m2RecoveryCheck(t, "model-guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_ready", "process_id": objects.process.String()})
	<-done
	m2RecoveryCheck(t, "model-root-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_returned", "joined": joined})
	if !joined {
		t.Fatal("Model cold child returned before its actual owners")
	}
}

// The existing complete-frame proxy loses only the original COMMIT response.
// The parent either releases that same frame or terminates its exact owned
// PostgreSQL backend before forwarding it. No transaction result is replaced.
func TestModelM3AppStoppedCommitUnknown(t *testing.T) {
	for _, mode := range []string{"commit", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			w, agentScenario, memoryScenario, embeddingScenario := newM3ModelCallColdWorkflow(t)
			v := w.app
			first := m3ModelStartChild(t, w, "memory-authorized", "")
			m2RecoveryEvent(t, first, "m3_model_memory-authorized_held", nil)
			var call string
			var sequence int64
			workAppCheck(t, "model-unknown-original-call", v.admin.QueryRow(v.ctx, `SELECT c.id,a.sequence FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.process_id=$1 AND c.request_data#>>'{Consumer,kind}'='memory' AND NOT c.retired AND l.owner_kind='model_call' AND NOT l.released`, first.process).Scan(&call, &sequence))
			original := m3ModelColdFacts(t, w, call)
			m2RecoveryGuardHeld(t, w, first.process)
			m2KillRecoveryChild(t, w, first)
			gate := filepath.Join(t.TempDir(), "unknown-boundary")
			second := m3ModelStartChild(t, w, "recover-"+mode, gate, call)
			event := m2RecoveryEvent(t, second, "m3_model_physical_unknown", nil)
			second.process, _ = event["process_id"].(string)
			pid, ok := event["writer_pid"].(float64)
			if !ok || pid <= 0 || pid != float64(int32(pid)) || event["original_unknown"] != true {
				t.Fatal("original physical Unknown did not identify its writer")
			}
			if got := m3ModelColdFacts(t, w, call); got != original {
				t.Fatal("unforwarded COMMIT changed visible original facts")
			}
			if mode == "rollback" {
				ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
				workAppCheck(t, "model-unknown-owned-writer-stop", v.root.db.Terminate(ctx, int32(pid)))
				tick := time.NewTicker(10 * time.Millisecond)
				for {
					var gone bool
					workAppCheck(t, "model-unknown-writer-actual-exit", v.admin.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_stat_activity WHERE pid=$1)`, int32(pid)).Scan(&gone))
					if gone {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("original owned backend did not exit")
					case <-tick.C:
					}
				}
				tick.Stop()
				cancel()
			}
			workAppCheck(t, "model-unknown-settle-original-writer", os.WriteFile(gate+".settle", []byte("settle"), 0600))
			settled := m2RecoveryEvent(t, second, "m3_model_physical_settled", nil)
			if settled["committed"] != (mode == "commit") || settled["held_joined"] != true {
				t.Fatal("physical transaction outcome or original proxy join differs")
			}
			beforeObserve := m3ModelColdFacts(t, w, call)
			if mode == "rollback" && beforeObserve != original || mode == "commit" && beforeObserve == original {
				t.Fatal("independent physical outcome did not match the original transaction")
			}
			workAppCheck(t, "model-unknown-original-lookup-release", os.WriteFile(gate+".observe", []byte("observe"), 0600))
			if mode == "commit" {
				m2RecoveryEvent(t, second, "m3_model_recovered_held", nil)
				m3ModelAssertColdClosed(t, w, call, sequence+2)
			} else {
				returned := m2RecoveryEvent(t, second, "m3_model_unknown_returned", nil)
				usageAppWait(t, second.done, "model-unknown-original-app-wait")
				m2RecoveryChildWaitLog(t, second, "original-unknown")
				if second.err != nil || returned["original_unknown"] != true || returned["joined"] != true {
					t.Fatal("unapplied original Unknown was retried or lost its owners")
				}
				m2RecoveryGuardReleased(t, w, second.process)
			}
			if m3ModelColdFacts(t, w, call) != beforeObserve {
				t.Fatal("original confirmation changed Model/Usage/Secret rows or xmin")
			}
			if mode == "commit" {
				m2KillRecoveryChild(t, w, second)
			} else {
				// This new App is explicit new cleanup after the original Unknown
				// returned and its process joined. It is not an automatic replay.
				third := m3ModelStartChild(t, w, "recover", filepath.Join(t.TempDir(), "new-cleanup-boundary"))
				event := m2RecoveryEvent(t, third, "m3_model_recovered_held", nil)
				third.process, _ = event["process_id"].(string)
				m3ModelAssertColdClosed(t, w, call, sequence+2)
				m2KillRecoveryChild(t, w, third)
			}
			w.joinedPeer(t, agentScenario, 1, 1)
			w.joinedPeer(t, memoryScenario, 0, 0)
			w.joinedPeer(t, embeddingScenario, 0, 0)
			t.Logf("MODEL_M3_PHYSICAL_UNKNOWN mode=%s real_unknown=true original_writer_settled=true original_confirmation_readonly=true original_model_usage_secret_closed=true no_redispatch=true memory_business_recovery_not_asserted=true", mode)
		})
	}
}

func m3ModelColdFacts(t *testing.T, w *m1Workflow, call string) string {
	t.Helper()
	var facts string
	workAppCheck(t, "model-unknown-original-facts", w.app.admin.QueryRow(w.app.ctx, `SELECT jsonb_build_array(
 jsonb_build_array(to_jsonb(c),c.xmin::text),jsonb_build_array(to_jsonb(a),a.xmin::text),
 jsonb_build_array(to_jsonb(i),i.xmin::text),jsonb_build_array(to_jsonb(l),l.xmin::text),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(o),o.xmin::text) ORDER BY o.sequence) FROM agenteam_model.invocation_observations o WHERE o.invocation_id=c.invocation_id),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(u),u.xmin::text) ORDER BY u.id) FROM agenteam_model.usage_project_calls u WHERE u.call_id=c.id),
 (SELECT jsonb_agg(jsonb_build_array(to_jsonb(s),s.xmin::text) ORDER BY s.execution_id) FROM agenteam_model.execution_usage_summaries s WHERE s.project_id=c.project_id)
 )::text FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, call).Scan(&facts))
	return facts
}

func m3ModelAssertColdClosed(t *testing.T, w *m1Workflow, call string, sequence int64) {
	t.Helper()
	var complete bool
	workAppCheck(t, "model-unknown-formal-closure", w.app.admin.QueryRow(w.app.ctx, `SELECT c.retired AND c.phase='unknown' AND a.dispatch='unknown' AND i.final_status='unknown' AND l.released
 AND a.sequence=$2 AND a.sequence=i.last_sequence AND a.sequence=(SELECT count(*) FROM agenteam_model.invocation_observations o WHERE o.invocation_id=c.invocation_id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls u WHERE u.call_id=c.id AND u.phase<>'business_closed')
 FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, call, sequence).Scan(&complete))
	if !complete {
		t.Fatal("original Model/Usage/Secret did not close together")
	}
}

func (s *m3ModelColdStore) withUnknown(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	selected := false
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		d := cause.Details()
		if d.Kind != f.CommandsCause || d.Primary.Namespace() != "model.usage" || d.Primary.Command() != "call-ledger" || s.armed.Load() {
			return nil
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var complete bool
		var pid int32
		if err = x.QueryRow(ctx, `SELECT c.retired AND c.phase='unknown' AND i.final_status='unknown' AND l.released,pg_backend_pid() FROM agenteam_model.calls c JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, s.target).Scan(&complete, &pid); err != nil {
			return err
		}
		if !complete || !s.armed.CompareAndSwap(false, true) {
			return nil
		}
		if err = s.proxy.Arm(pid); err != nil {
			return err
		}
		selected = true
		return nil
	})
	if !selected {
		return s.observeCommitted(ctx, cause, result)
	}
	if result.State() != f.Unknown || result.AttemptID().Validate() != nil {
		m2RecoveryFailure("model-physical-original-unknown", f.NewFault(f.InvalidState, result.State()))
		return result
	}
	s.original = result
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_model_physical_unknown", "original_unknown": true, "writer_pid": s.proxy.WriterPID(), "process_id": s.process()})
	if err := m3ModelWaitGate(ctx, s.gate+".settle"); err != nil {
		m2RecoveryFailure("model-physical-settle-gate", err)
		return result
	}
	s.proxy.Release()
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if s.mode == "recover-commit" {
		select {
		case <-s.proxy.Committed():
		case <-wait.Done():
			m2RecoveryFailure("model-physical-commit", wait.Err())
			return result
		}
	}
	select {
	case <-s.proxy.HeldJoined():
	case <-wait.Done():
		m2RecoveryFailure("model-physical-writer-join", wait.Err())
		return result
	}
	committed := false
	select {
	case <-s.proxy.Committed():
		committed = true
	default:
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_model_physical_settled", "committed": committed, "held_joined": true})
	if err := m3ModelWaitGate(ctx, s.gate+".observe"); err != nil {
		m2RecoveryFailure("model-physical-observation-gate", err)
	}
	// Preserve the exact original physical outcome. Runtime now confirms it
	// through its original readonly plan; this wrapper never calls Apply again.
	return result
}

func m3ModelWaitGate(ctx context.Context, path string) error {
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-wait.Done():
			return wait.Err()
		case <-tick.C:
		}
	}
}
