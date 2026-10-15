//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Run independently, and before provisioning the App below. This is the same
// complete original deployment configuration used by newM1WorkflowWithOptions;
// no database connection or owned network descriptor is needed for preflight.
func TestMeetingM3AppConfiguration(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
}

func meetingM3ConfigurationPreflight(t *testing.T) {
	t.Helper()
	cfg := testConfig(t, "5s", map[string]string{
		"PUBLIC_ORIGIN":                          "http://localhost:8080",
		"EXECUTION_RUNTIME":                      strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1),
		"SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS":    "2",
		"SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF": "100ms",
		"SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF":     "1s",
	})
	workAppCheck(t, "meeting-complete-config", cfg.Validate())
	runtime, enabled := cfg.ExecutionRuntime()
	_, retry := cfg.SchedulerLaunchRetryPolicy()
	if !enabled || !runtime.ToolLoop() || !retry || len(cfg.OutboundProxyOrigins()) != 0 {
		t.Fatal("Meeting fixture lost the original runtime, scheduler retry or isolated peer route")
	}
	workAppCheck(t, "meeting-runtime-config", runtime.Validate())
}

// The model peer is deterministic; all other participants are the production
// App graph. Owner Start is the sole stimulus: the background Meeting driver
// captures A, publishes A, captures B, and invokes the system Summary model.
// There is no direct Advance, synthetic Execution, or alternate engine here.
func TestMeetingM3AppSequentialCollaboration(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	rejected := make(chan meetingM3RejectedTransaction, 1)
	var v *executionSchedulerApp
	// Registered first, so the original fixture's Stop/Wait/cleanup runs before
	// this observation. Reading root.err before its original done would race.
	t.Cleanup(func() {
		if v == nil {
			return
		}
		select {
		case <-v.root.done:
			meetingM3SafeError(t, "original-app-return", v.root.err)
			t.Logf("M3_MEETING_APP returned=true runtime_joined=%t assembly_joined=%t producers_joined=%t", v.runtime.Joined(), v.assembly.Joined(), v.root.owned.producersJoined())
		default:
			t.Error("M3_MEETING_APP original return absent after fixture cleanup")
		}
	})
	var rejectedCount atomic.Int32
	w, _ := newM1WorkflowWithOptions(t, func(cause f.TransactionCause, result f.CommitResult) {
		if failure := result.Fault(); result.State() == f.NotCommitted && failure != nil && failure.Code == f.InternalError {
			if rejectedCount.Add(1) <= 16 {
				t.Logf("M3_MEETING_TX owner=%s job=%s", meetingM3SafeIdentifier(cause.Details().Owner), meetingM3SafeIdentifier(cause.Details().JobType))
				meetingM3SafeError(t, "original-transaction", failure)
			}
			select {
			case rejected <- meetingM3RejectedTransaction{cause.Details().Owner, failure.Code}:
			default:
			}
		}
	}, false)
	v = w.app
	if v.runtime.meetings == nil || v.runtime.meetingRuntime == nil {
		t.Fatal("original App Meeting Service and Runtime are unbound")
	}
	const first = "Agent A proposes reserving 12 calibration slots."
	const second = "Agent B incorporates A's proposal: reserve 12 calibration slots and review the schedule."
	const title = "Calibration planning"
	summary := string(workAppJSON(t, map[string]string{
		"title": title, "goals": "Plan calibration", "decisions": "Reserve 12 calibration slots",
		"unresolved": "Review schedule", "facts": "Two agents contributed",
	}))
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		meetingM3Reply(t, 1, first), meetingM3Reply(t, 2, second), meetingM3Reply(t, 3, summary),
	}})
	workAppCheck(t, "meeting-owned-model-peer", err)
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()
	receipt, err := v.runtime.meetings.Start(v.ctx, v.actor, mt.StartRequest{
		ProjectID: v.a, Agents: agents, Mode: "sequential",
		Content: []mt.InlineNode{{Type: "text", Text: "Plan the calibration capacity together. Agent B should consider Agent A's contribution."}},
		Meta:    executionSchedulerAppMeta(t, nil),
	})
	workAppCheck(t, "meeting-owner-start", err)
	if receipt.TurnID == nil {
		t.Fatal("Owner Start did not create a Turn")
	}
	meetingM3VerifyOwnedDatabase(t, v)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	detail := meetingM3AwaitCompleted(t, v, receipt, rejected)
	if detail.Session.Title == nil || *detail.Session.Title != title || detail.Session.Summary == nil || detail.Session.Summary.Version != 1 || detail.Session.Summary.PendingUpdate || detail.Session.Summary.Decisions != "Reserve 12 calibration slots" {
		t.Fatal("initial rolling Summary and title were not published together")
	}
	turn := detail.Turns[0]
	if len(turn.Contributions) != 3 || turn.Contributions[0].Kind != "user" {
		t.Fatal("Turn must retain the Owner message and both Agent contributions")
	}
	for n, expected := range []string{first, second} {
		contribution := turn.Contributions[n+1]
		if contribution.Status != "completed" || contribution.ExecutionID == nil || contribution.Message == nil || len(contribution.Message.Content) != 1 || contribution.Message.Content[0].Type != "text" || contribution.Message.Content[0].Text != expected {
			t.Fatalf("Agent contribution %d did not publish its original complete final text", n+1)
		}
		if n == 1 && *contribution.ExecutionID == *turn.Contributions[1].ExecutionID {
			t.Fatal("distinct Agents shared an Execution identity")
		}
	}
	meetingM3CheckDurableFacts(t, v, receipt)

	// Stop/join the original Runtime before the rest of the App. The fixture's
	// failure cleanup retains dependencies until this original run has returned.
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "meeting-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "meeting-original-app-return")
	workAppCheck(t, "meeting-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.meetingRuntime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Meeting App returned before original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	state := w.joinedPeer(t, scenario, 3, 3)
	meetingM3CheckRequests(t, state, first, second)
	t.Log("M3_MEETING_SEQUENTIAL actual_app_executor_model=true owner_start_background_driver=true agent_b_reads_a=true summary_published=true model_http_calls=3 original_app_wait=true external_model=false")
}

// Concurrent original Model requests prove parallel execution. A queued Turn
// must remain undispatched through cancellation, then follow the exact Turn
// that interrupted its predecessor; it cannot steal that successor's slot.
func TestMeetingM3AppParallelQueueInterrupt(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	w, _ := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	hold := 0
	firstA := meetingM3Reply(t, 1, "Cancelled original parallel response.")
	firstB := meetingM3Reply(t, 2, "Cancelled original parallel response.")
	interruptA := meetingM3Reply(t, 3, "The interrupting discussion completed.")
	firstA.HoldAfter, firstB.HoldAfter, interruptA.HoldAfter = &hold, &hold, &hold
	initialSummary := string(workAppJSON(t, map[string]string{"title": "Interrupt discussion", "goals": "The immediate discussion completed", "decisions": "", "unresolved": "", "facts": "Both Agents contributed"}))
	queuedSummary := string(workAppJSON(t, map[string]string{"goals": "The queued discussion followed the interruption", "decisions": "", "unresolved": "", "facts": "The queued request was preserved"}))
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		firstA, firstB, interruptA, meetingM3Reply(t, 4, "The other interrupting contribution completed."), meetingM3Reply(t, 5, initialSummary),
		meetingM3Reply(t, 6, "The preserved queued request completed."), meetingM3Reply(t, 7, "The other queued contribution completed."), meetingM3Reply(t, 8, queuedSummary),
	}})
	workAppCheck(t, "parallel-owned-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()
	first, err := v.runtime.meetings.Start(v.ctx, v.actor, mt.StartRequest{ProjectID: v.a, Agents: agents, Mode: "parallel", Content: []mt.InlineNode{{Type: "text", Text: "Begin both original contributions in parallel."}}, Meta: executionSchedulerAppMeta(t, nil)})
	workAppCheck(t, "parallel-owner-start", err)
	if first.TurnID == nil {
		t.Fatal("parallel Start omitted its original Turn")
	}
	var counts string
	await := func(stage string, observe func(context.Context) (bool, error)) {
		t.Helper()
		ctx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
		defer cancel()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			done, err := observe(ctx)
			if err != nil {
				meetingM3SafeError(t, stage, err)
				t.Fatal("parallel original observation failed")
			}
			meetingM3Progress(t, v, first, &counts, true)
			if done {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("parallel stage did not settle: %s", stage)
			case <-v.root.done:
				meetingM3SafeError(t, stage, v.root.err)
				t.Fatal("original App exited before parallel work settled")
			case <-tick.C:
			}
		}
	}
	await("two-concurrent-original-requests", func(ctx context.Context) (bool, error) {
		state, err := w.network.State(ctx, scenario)
		if err != nil {
			return false, err
		}
		if len(state.Requests) > 2 {
			t.Fatal("held parallel Turn unexpectedly advanced")
		}
		return len(state.Requests) == 2 && state.ActiveHandlers == 2, nil
	})
	var common bool
	err = v.admin.QueryRow(v.ctx, `SELECT count(*)=2 AND count(DISTINCT l.input_id)=1 AND count(DISTINCT l.execution_id)=2 FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id WHERE c.turn_id=$1`, first.TurnID.String()).Scan(&common)
	workAppCheck(t, "parallel-common-input", err)
	if !common {
		t.Fatal("parallel Agents did not share one immutable input and distinct executions")
	}
	detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, first.MeetingID)
	workAppCheck(t, "parallel-before-enqueue", err)
	queued, err := v.runtime.meetings.Send(v.ctx, v.actor, mt.SendRequest{ProjectID: v.a, MeetingID: first.MeetingID, ExpectedVersion: detail.Session.Version, Mode: "parallel", Content: []mt.InlineNode{{Type: "text", Text: "Keep this request queued until its actual turn."}}, Meta: executionSchedulerAppMeta(t, nil)})
	workAppCheck(t, "parallel-owner-enqueue", err)
	if queued.TurnID == nil {
		t.Fatal("queued request omitted its original Turn")
	}
	var waiting bool
	err = v.admin.QueryRow(v.ctx, `SELECT t.status='queued' AND NOT EXISTS(SELECT 1 FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id WHERE c.turn_id=t.id) FROM agenteam_meeting.turns t WHERE t.id=$1`, queued.TurnID.String()).Scan(&waiting)
	workAppCheck(t, "parallel-queued-not-launched", err)
	if !waiting {
		t.Fatal("queued Turn launched while the original parallel requests were held")
	}
	interrupted, err := v.runtime.meetings.Send(v.ctx, v.actor, mt.SendRequest{ProjectID: v.a, MeetingID: first.MeetingID, ExpectedVersion: queued.Version, Mode: "parallel", Interrupt: true, Content: []mt.InlineNode{{Type: "text", Text: "Immediately interrupt the original discussion with this request."}}, Meta: executionSchedulerAppMeta(t, nil)})
	workAppCheck(t, "parallel-owner-interrupt", err)
	if interrupted.TurnID == nil {
		t.Fatal("interrupt omitted its original successor Turn")
	}
	await("original-cancel-before-exact-successor", func(ctx context.Context) (bool, error) {
		state, err := w.network.State(ctx, scenario)
		if err != nil {
			return false, err
		}
		if len(state.Requests) > 4 {
			t.Fatal("interrupt bypassed its held original request")
		}
		if len(state.Requests) != 4 || state.ActiveHandlers != 1 {
			return false, nil
		}
		var ordered bool
		err = v.admin.QueryRow(ctx, `SELECT old.status='cancelled' AND old.interrupted_by_id=current.id AND current.status='running' AND queued.status='queued'
 AND NOT EXISTS(SELECT 1 FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id WHERE c.turn_id=queued.id)
 AND (SELECT count(*) FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id JOIN agenteam_execution.executions e ON e.id::text=l.execution_id WHERE c.turn_id=old.id AND e.status='cancelled')=2
 AND (SELECT count(*) FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id WHERE c.turn_id=current.id)=2
 AND NOT EXISTS(SELECT 1 FROM agenteam_meeting.contributions c WHERE c.turn_id=old.id AND c.kind='agent' AND c.current_message_id IS NOT NULL)
 FROM agenteam_meeting.turns old,agenteam_meeting.turns current,agenteam_meeting.turns queued WHERE old.id=$1 AND current.id=$2 AND queued.id=$3`, first.TurnID.String(), interrupted.TurnID.String(), queued.TurnID.String()).Scan(&ordered)
		if err == nil && !ordered {
			t.Fatal("older queued Turn stole the interrupt slot or original cancellation was bypassed")
		}
		return ordered, err
	})
	workAppCheck(t, "parallel-release-original-successor", w.network.Release(v.ctx, scenario))
	await("preserved-fifo-after-interrupt", func(ctx context.Context) (bool, error) {
		var done bool
		err := v.admin.QueryRow(ctx, `SELECT a.status='completed' AND b.status='completed' AND s.summary IS NOT NULL
 FROM agenteam_meeting.turns a JOIN agenteam_meeting.turns b ON b.meeting_id=a.meeting_id JOIN agenteam_meeting.sessions s ON s.id=a.meeting_id WHERE a.id=$1 AND b.id=$2`, interrupted.TurnID.String(), queued.TurnID.String()).Scan(&done)
		return done, err
	})
	detail, err = v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, first.MeetingID)
	workAppCheck(t, "parallel-final-detail", err)
	if len(detail.Turns) != 3 || detail.Turns[0].ID != *first.TurnID || detail.Turns[0].Status != "cancelled" || detail.Turns[1].ID != *queued.TurnID || detail.Turns[1].Status != "completed" || detail.Turns[2].ID != *interrupted.TurnID || detail.Turns[2].Status != "completed" || detail.Session.Title == nil || *detail.Session.Title != "Interrupt discussion" || detail.Session.Summary == nil || detail.Session.Summary.Version != 2 || detail.Session.Summary.Goals != "The queued discussion followed the interruption" {
		t.Fatal("parallel interruption lost original Timeline order or deferred queued work")
	}
	var groups, cancelled, succeeded, calls, retired, leases, summaries int
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM(SELECT c.turn_id FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id WHERE l.meeting_id=$2 GROUP BY c.turn_id HAVING count(*)=2 AND count(DISTINCT l.input_id)=1 AND count(DISTINCT l.execution_id)=2) inputs),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='cancelled'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='succeeded'),count(*),count(*) FILTER(WHERE c.retired),count(*) FILTER(WHERE l.released),
 (SELECT count(*) FROM agenteam_meeting.summary_generations WHERE meeting_id=$2 AND status='published')
 FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1`, v.a.String(), first.MeetingID.String()).Scan(&groups, &cancelled, &succeeded, &calls, &retired, &leases, &summaries)
	workAppCheck(t, "parallel-original-terminal-facts", err)
	if groups != 3 || cancelled != 2 || succeeded != 4 || calls != 8 || retired != 8 || leases != 8 || summaries != 2 {
		t.Fatalf("parallel terminal facts inputs=%d cancelled=%d succeeded=%d calls=%d retired=%d leases=%d summaries=%d", groups, cancelled, succeeded, calls, retired, leases, summaries)
	}
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "parallel-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "parallel-original-app-wait")
	workAppCheck(t, "parallel-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.meetingRuntime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("parallel App returned before its original owners joined")
	}
	w.joinedPeer(t, scenario, 8, 8)
	meetingM3GuardReleased(t, v.workAppFixture)
	t.Log("M3_MEETING_PARALLEL concurrent_model_requests=true common_input=true queued_not_launched=true original_cancelled=true exact_interrupt_first=true fifo_preserved=true original_app_wait=true external_model=false")
}

func meetingM3Reply(t *testing.T, ordinal int, content string) netfixture.WireScenario {
	t.Helper()
	raw := workAppJSON(t, map[string]any{
		"id": "owned-meeting-response", "object": "chat.completion", "created": 1, "model": "meeting-collaboration",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20},
	})
	return netfixture.WireScenario{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json", "X-Request-Id": fmt.Sprintf("meeting-round-%d", ordinal)}, Chunks: [][]byte{raw}}
}

// One logical generation retains its first snapshot despite an administrator
// selecting a different model before an explicit retry. A later Turn cancels
// an actually sent Summary request and must release its pin without publishing.
func TestMeetingM3AppSummarySnapshotLifecycle(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	rejected := make(chan meetingM3RejectedTransaction, 1)
	w, _ := newM1WorkflowWithOptions(t, func(cause f.TransactionCause, result f.CommitResult) {
		if failure := result.Fault(); result.State() == f.NotCommitted && failure != nil && failure.Code == f.InternalError {
			meetingM3SafeError(t, "summary-original-transaction", failure)
			select {
			case rejected <- meetingM3RejectedTransaction{cause.Details().Owner, failure.Code}:
			default:
			}
		}
	}, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	valid := string(workAppJSON(t, map[string]string{"title": "Retained summary", "goals": "Keep original configuration", "decisions": "", "unresolved": "", "facts": "Two contributions"}))
	hold := 0
	held := meetingM3Reply(t, 7, "must not become a published summary")
	held.HoldAfter = &hold
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		meetingM3Reply(t, 1, "A proposed the first plan."), meetingM3Reply(t, 2, "B reviewed the first plan."), meetingM3Reply(t, 3, "invalid summary envelope"), meetingM3Reply(t, 4, valid),
		meetingM3Reply(t, 5, "A proposed another plan."), meetingM3Reply(t, 6, "B reviewed the other plan."), held,
	}})
	workAppCheck(t, "summary-owned-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	agentModel := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, agentModel)
	var originalSelection mc.MeetingSummarySelection
	meetingM3SystemCall(t, v, http.MethodGet, "model-selection/meeting-summary", nil, &originalSelection)
	if originalSelection.Model == nil || *originalSelection.Model == agentModel {
		t.Fatal("Summary did not start with its separate text-only selection")
	}
	// Changing selection must stay inside the supported Summary profile. The
	// Agent model has tool_calls=true and is intentionally outside that profile.
	var originalModel struct {
		ID         mc.ModelID      `json:"id"`
		ProviderID mc.ProviderID   `json:"provider_id"`
		Input      json.RawMessage `json:"input"`
	}
	meetingM3SystemCall(t, v, http.MethodGet, "models/"+originalSelection.Model.String(), nil, &originalModel)
	if originalModel.ID != *originalSelection.Model || originalModel.ProviderID.Validate() != nil {
		t.Fatal("original Summary fixture profile differs")
	}
	replacementRequest := meetingM3ReplacementModelRequest(t, originalModel.ProviderID, originalModel.Input)
	var replacement mc.CommandReceipt
	meetingM3SystemCall(t, v, http.MethodPost, "models", replacementRequest, &replacement)
	replacementModel, err := f.ParseID[mc.Model](replacement.ResourceID)
	workAppCheck(t, "summary-replacement-model", err)
	if replacementModel == originalModel.ID || replacementModel == agentModel {
		t.Fatal("replacement Summary Model identity was reused")
	}
	v.store.releaseDirectory()
	first, err := v.runtime.meetings.Start(v.ctx, v.actor, mt.StartRequest{ProjectID: v.a, Agents: agents, Mode: "sequential", Content: []mt.InlineNode{{Type: "text", Text: "Discuss a plan together."}}, Meta: executionSchedulerAppMeta(t, nil)})
	workAppCheck(t, "summary-owner-start", err)
	generation, snapshot := meetingM3AwaitSummaryState(t, v, first, "failed", 1, rejected)
	meetingM3CheckSummaryPin(t, v, generation, snapshot, "retained", 1)
	detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, first.MeetingID)
	workAppCheck(t, "summary-failed-detail", err)
	if detail.Session.Summary != nil || detail.Session.Title != nil || detail.Turns[0].Status != "finalizing" {
		t.Fatal("invalid Summary published a fallback or ended the Turn")
	}
	var selected mc.CommandReceipt
	meetingM3SystemCall(t, v, http.MethodPut, "model-selection/meeting-summary", map[string]any{"id": originalSelection.ID, "expected_version": originalSelection.Version, "model": replacementModel}, &selected)
	retry := mt.ControlRequest{ProjectID: v.a, MeetingID: first.MeetingID, TurnID: first.TurnID, ExpectedVersion: detail.Session.Version, Action: "retry-summary", Meta: executionSchedulerAppMeta(t, nil)}
	_, err = v.runtime.meetings.Control(v.ctx, v.actor, retry)
	workAppCheck(t, "summary-explicit-retry", err)
	if _, found, err := v.runtime.meetings.LookupControl(v.ctx, v.actor, retry); err != nil || !found {
		t.Fatal("Summary retry original lookup was not found")
	}
	retried, retained := meetingM3AwaitSummaryState(t, v, first, "published", 2, rejected)
	if retried != generation || retained != snapshot {
		t.Fatal("Summary retry replaced its logical generation or snapshot")
	}
	meetingM3CheckSummaryPin(t, v, generation, snapshot, "released", 2)
	var calls int
	var same bool
	err = v.admin.QueryRow(v.ctx, `SELECT count(*),coalesce(bool_and(c.snapshot_id=$2 AND c.retired_at IS NOT NULL AND c.returned_at IS NOT NULL AND s.snapshot_data->>'SelectionVersion'=$3),false) FROM agenteam_meeting.summary_model_calls c JOIN agenteam_model.snapshots s ON s.id::text=c.snapshot_id WHERE c.generation_id=$1`, generation, snapshot, fmt.Sprint(originalSelection.Version)).Scan(&calls, &same)
	workAppCheck(t, "summary-original-two-call-snapshot", err)
	if calls != 2 || !same {
		t.Fatal("retry did not preserve the original selection and retire both calls")
	}
	detail, err = v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, first.MeetingID)
	workAppCheck(t, "summary-retry-detail", err)
	if detail.Session.Summary == nil || detail.Session.Summary.Version != 1 || detail.Session.Title == nil || *detail.Session.Title != "Retained summary" {
		t.Fatal("explicit Summary retry did not publish the first version")
	}
	second, err := v.runtime.meetings.Send(v.ctx, v.actor, mt.SendRequest{ProjectID: v.a, MeetingID: first.MeetingID, ExpectedVersion: detail.Session.Version, Mode: "sequential", Content: []mt.InlineNode{{Type: "text", Text: "Discuss a second plan."}}, Meta: executionSchedulerAppMeta(t, nil)})
	workAppCheck(t, "summary-second-turn", err)
	secondGeneration, secondSnapshot := meetingM3AwaitSummaryState(t, v, second, "generating", 1, rejected)
	meetingM3CheckSummaryPin(t, v, secondGeneration, secondSnapshot, "retained", 1)
	var selectedCurrent bool
	err = v.admin.QueryRow(v.ctx, `SELECT COALESCE(snapshot_data#>>'{Identity,model_id}'=$2 AND snapshot_data->>'SelectionVersion'=$3,false) FROM agenteam_model.snapshots WHERE id::text=$1`, secondSnapshot, replacementModel.String(), fmt.Sprint(selected.Version)).Scan(&selectedCurrent)
	workAppCheck(t, "summary-next-generation-selection", err)
	if !selectedCurrent || secondSnapshot == snapshot {
		t.Fatal("new Summary generation did not capture the new supported selection")
	}
	// The owned peer's live seventh request proves cancellation reaches an
	// actual admitted Summary call, rather than only its prepared domain row.
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := w.network.State(v.ctx, scenario)
		workAppCheck(t, "summary-held-original-request", err)
		if len(state.Requests) == 7 && state.ActiveHandlers == 1 {
			break
		}
		if len(state.Requests) > 7 {
			t.Fatal("Summary was automatically reissued")
		}
		var status string
		workAppCheck(t, "summary-held-generation", v.admin.QueryRow(v.ctx, `SELECT status FROM agenteam_meeting.summary_generations WHERE id=$1`, secondGeneration).Scan(&status))
		if status != "generating" {
			t.Fatalf("Summary returned before its original request was held status=%s", meetingM3SafeIdentifier(status))
		}
		select {
		case <-v.ctx.Done():
			t.Fatal("original Summary never entered its owned peer")
		case failure := <-rejected:
			t.Fatalf("Summary transaction rejected owner=%s code=%s", failure.owner, failure.code.Safe())
		case <-tick.C:
		}
	}
	detail, err = v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, first.MeetingID)
	workAppCheck(t, "summary-before-cancel", err)
	_, err = v.runtime.meetings.Control(v.ctx, v.actor, mt.ControlRequest{ProjectID: v.a, MeetingID: first.MeetingID, TurnID: second.TurnID, ExpectedVersion: detail.Session.Version, Action: "cancel-turn", Meta: executionSchedulerAppMeta(t, nil)})
	workAppCheck(t, "summary-owner-cancel-original", err)
	cancelledGeneration, cancelledSnapshot := meetingM3AwaitSummaryState(t, v, second, "cancelled", 1, rejected)
	if cancelledGeneration != secondGeneration || cancelledSnapshot != secondSnapshot {
		t.Fatal("cancellation replaced the original Summary owner")
	}
	meetingM3CheckSummaryPin(t, v, secondGeneration, secondSnapshot, "released", 2)
	detail, err = v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, first.MeetingID)
	workAppCheck(t, "summary-cancelled-detail", err)
	if detail.Session.Summary == nil || detail.Session.Summary.Version != 1 || len(detail.Turns) != 2 || detail.Turns[1].Status != "cancelled" {
		t.Fatal("cancelled Summary published or failed to close its Turn")
	}
	var modelCalls, retired, leases int
	err = v.admin.QueryRow(v.ctx, `SELECT count(*),count(*) FILTER(WHERE c.retired),count(*) FILTER(WHERE l.released) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1`, v.a.String()).Scan(&modelCalls, &retired, &leases)
	workAppCheck(t, "summary-model-secret-retired", err)
	if modelCalls != 7 || retired != 7 || leases != 7 {
		t.Fatalf("original Summary calls retained: model=%d retired=%d leases=%d", modelCalls, retired, leases)
	}
	v.runtime.StopAdmission()
	drain, stop := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	stop()
	workAppCheck(t, "summary-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "summary-original-app-wait")
	workAppCheck(t, "summary-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.meetingRuntime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Summary App returned before its original owners joined")
	}
	w.joinedPeer(t, scenario, 7, 7)
	meetingM3GuardReleased(t, v.workAppFixture)
	t.Log("M3_MEETING_SUMMARY fixed_generation_snapshot=true changed_selector_not_used=true explicit_retry=true original_sent_call_cancelled=true pins_released=true original_app_wait=true external_model=false")
}

// Preserve the actual GET wire representation. Domain Capabilities omits nil
// counts when marshalled; the HTTP write contract requires explicit nulls.
func meetingM3ReplacementModelRequest(t *testing.T, provider mc.ProviderID, original json.RawMessage) map[string]any {
	t.Helper()
	var configured mc.ModelInput
	workAppCheck(t, "summary-original-model-input", json.Unmarshal(original, &configured))
	if provider.Validate() != nil || configured.Validate() != nil || configured.Type != mc.ChatModel || !configured.Enabled || configured.Capabilities.ToolCalls || configured.Capabilities.ParallelToolCalls || configured.Capabilities.Reasoning {
		t.Fatal("original Summary fixture profile differs")
	}
	var input map[string]json.RawMessage
	workAppCheck(t, "summary-original-http-input", json.Unmarshal(original, &input))
	input["name"] = workAppJSON(t, "Replacement text-only Summary model")
	return map[string]any{"provider_id": provider, "input": input}
}

func TestMeetingM3SummaryReplacementModelWire(t *testing.T) {
	provider := guardID[mc.Provider](t)
	original := json.RawMessage(`{"name":"Original Summary","provider_model_id":"meeting-collaboration","type":"chat","enabled":true,"parameters":{},"request_overwrite":{},"header_overwrite":{},"capabilities":{"tool_calls":false,"parallel_tool_calls":false,"streaming":true,"reasoning":false,"input_modalities":["text"],"output_modalities":["text"],"reasoning_efforts":[],"structured_output_modes":[],"context_length":null,"max_output":"2048"}}`)
	// Exercise the same request constructor and encoder as the actual POST.
	raw := workAppJSON(t, meetingM3ReplacementModelRequest(t, provider, original))
	var encoded struct {
		Provider mc.ProviderID              `json:"provider_id"`
		Input    map[string]json.RawMessage `json:"input"`
	}
	workAppCheck(t, "summary-replacement-http-wire", json.Unmarshal(raw, &encoded))
	var before map[string]json.RawMessage
	workAppCheck(t, "summary-original-http-wire", json.Unmarshal(original, &before))
	if encoded.Provider != provider || len(encoded.Input) != len(before) || string(encoded.Input["name"]) != `"Replacement text-only Summary model"` {
		t.Fatal("replacement request changed its HTTP envelope")
	}
	for field, value := range before {
		if field != "name" && !bytes.Equal(value, encoded.Input[field]) {
			t.Fatal("replacement request changed an original HTTP field", field)
		}
	}
}

func meetingM3AwaitSummaryState(t *testing.T, v *executionSchedulerApp, receipt mt.Receipt, expected string, expectedAttempt int, rejected <-chan meetingM3RejectedTransaction) (string, string) {
	t.Helper()
	if receipt.TurnID == nil {
		t.Fatal("Summary stimulus has no Turn")
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		var generation, state, turnState, code string
		var snapshot *string
		var attempt int
		err := v.admin.QueryRow(v.ctx, `SELECT g.id,g.status,g.snapshot_id,g.attempt,t.status,g.error_code FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.turns t ON t.id=g.turn_id WHERE g.meeting_id=$1 AND g.turn_id=$2 ORDER BY g.created_at DESC LIMIT 1`, receipt.MeetingID.String(), receipt.TurnID.String()).Scan(&generation, &state, &snapshot, &attempt, &turnState, &code)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			meetingM3SafeError(t, "summary-state", err)
			t.Fatal("Summary original state observation failed")
		}
		if err == nil && state == expected && attempt == expectedAttempt && snapshot != nil && (expected != "cancelled" || turnState == "cancelled") {
			return generation, *snapshot
		}
		if err == nil && state == "failed" && expected == "failed" && snapshot == nil {
			meetingM3SummaryFailureFacts(t, v, generation)
			t.Fatal("Summary failed before retaining its original snapshot")
		}
		if err == nil && (state == "cancelled" && expected != "cancelled" || state == "failed" && expected != "failed") {
			meetingM3SummaryFailureFacts(t, v, generation)
			t.Fatalf("unexpected Summary transition status=%s attempt=%d code=%s snapshot=%t", meetingM3SafeIdentifier(state), attempt, f.Code(code).Safe(), snapshot != nil)
		}
		select {
		case failure := <-rejected:
			t.Fatalf("Summary transaction rejected owner=%s code=%s", failure.owner, failure.code.Safe())
		case <-v.root.done:
			meetingM3SafeError(t, "summary-early-app-return", v.root.err)
			t.Fatal("original App returned before Summary transition")
		case <-v.ctx.Done():
			t.Fatalf("Summary transition not observed status=%s attempt=%d code=%s snapshot=%t", meetingM3SafeIdentifier(state), attempt, f.Code(code).Safe(), snapshot != nil)
		case <-tick.C:
		}
	}
}

// Closed counts from the original owned database distinguish preparation,
// resolution, admission and cleanup without returning request or Secret data.
func meetingM3SummaryFailureFacts(t *testing.T, v *executionSchedulerApp, generation string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	var calls, snapshots, unknown, returned, retired, admitted, released int
	err := v.admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE s.snapshot_id IS NOT NULL),count(*) FILTER(WHERE s.resolution_unknown),count(*) FILTER(WHERE s.returned_at IS NOT NULL),count(*) FILTER(WHERE s.retired_at IS NOT NULL),count(c.id),count(*) FILTER(WHERE l.released) FROM agenteam_meeting.summary_model_calls s LEFT JOIN agenteam_model.calls c ON c.id::text=s.call_id LEFT JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE s.generation_id=$1 AND s.project_id=$2`, generation, v.a.String()).Scan(&calls, &snapshots, &unknown, &returned, &retired, &admitted, &released)
	if err != nil {
		meetingM3SafeError(t, "summary-failure-facts", err)
		return
	}
	t.Logf("M3_SUMMARY_FAILURE calls=%d resolved=%d resolution_unknown=%d returned=%d retired=%d model_admitted=%d model_leases_released=%d", calls, snapshots, unknown, returned, retired, admitted, released)
}

func meetingM3CheckSummaryPin(t *testing.T, v *executionSchedulerApp, generation, snapshot, state string, version int) {
	t.Helper()
	var exact, reference bool
	err := v.admin.QueryRow(v.ctx, `SELECT g.snapshot_id=$2 AND g.retention_version=$4 AND g.retained_at IS NOT NULL AND (g.released_at IS NOT NULL)=($3='released') AND r.snapshot_id::text=g.snapshot_id AND r.state=$3 AND r.consumer_kind='meeting' AND r.agent_id IS NULL,
 EXISTS(SELECT 1 FROM agenteam_secret.secret_references s WHERE s.consumer='model' AND s.owner_id::text=r.reference_id::text AND s.credential_id::text=r.credential_id::text)
 FROM agenteam_meeting.summary_generations g JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id::text=g.id AND r.project_id::text=$5 WHERE g.id=$1`, generation, snapshot, state, version, v.a.String()).Scan(&exact, &reference)
	workAppCheck(t, "summary-exact-generation-pin", err)
	if !exact || reference != (state == "retained") {
		t.Fatalf("Summary retention facts differ expected=%s exact=%t reference=%t", state, exact, reference)
	}
}

// These are new paths through the original live Builtin/Execution owner. A
// Human resolution cannot manufacture a successor or another Execution.
func TestMeetingM3AppDecision(t *testing.T) {
	for _, action := range []string{"answer", "skip", "cancel"} {
		t.Run(action, func(t *testing.T) {
			meetingM3ConfigurationPreflight(t)
			rejected := make(chan meetingM3RejectedTransaction, 1)
			w, _ := newM1WorkflowWithOptions(t, func(cause f.TransactionCause, result f.CommitResult) {
				if failure := result.Fault(); result.State() == f.NotCommitted && failure != nil && failure.Code == f.InternalError {
					meetingM3SafeError(t, "decision-original-transaction", failure)
					select {
					case rejected <- meetingM3RejectedTransaction{cause.Details().Owner, failure.Code}:
					default:
					}
				}
			}, false)
			v := w.app
			meetingM3VerifyOwnedDatabase(t, v)
			var tool string
			workAppCheck(t, "decision-real-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:request-decision'`).Scan(&tool))
			if _, err := f.ParseID[id.Tool](tool); err != nil {
				t.Fatal("Decision Tool identity invalid")
			}
			const question = "Which calibration batch should we use?"
			const answer = "Use the smaller calibration batch."
			const final = "I recorded the owner's calibration decision."
			summary := string(workAppJSON(t, map[string]string{"title": "Calibration decision", "goals": "Select a batch", "decisions": "Owner decision recorded", "unresolved": "", "facts": "One contribution"}))
			args := string(workAppJSON(t, map[string]any{"question": question, "options": []string{"smaller", "larger"}}))
			initial := workAppJSON(t, map[string]any{
				"id": "owned-meeting-decision", "object": "chat.completion", "created": 1, "model": "meeting-collaboration",
				"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "meeting_decision_call", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool, "-", ""), "arguments": args}}}}, "finish_reason": "tool_calls"}},
				"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20},
			})
			sequence := []netfixture.WireScenario{{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{initial}}}
			if action != "cancel" {
				sequence = append(sequence, meetingM3Reply(t, 2, final), meetingM3Reply(t, 3, summary))
			}
			peer := netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence}
			if len(sequence) == 1 {
				peer.Wire, peer.WireSequence = &sequence[0], nil
			}
			scenario, err := w.network.Create(v.ctx, peer)
			workAppCheck(t, "decision-owned-model-peer", err)
			defer func() {
				if t.Failed() {
					meetingM3FailureObservation(t, w, scenario)
				}
			}()
			model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
			agents := meetingM3CreateAgents(t, v, model)
			v.store.releaseDirectory()
			receipt, err := v.runtime.meetings.Start(v.ctx, v.actor, mt.StartRequest{ProjectID: v.a, Agents: agents[:1], Mode: "sequential", Content: []mt.InlineNode{{Type: "text", Text: "Ask the owner to select the calibration batch."}}, Meta: executionSchedulerAppMeta(t, nil)})
			workAppCheck(t, "decision-owner-start", err)
			detail := meetingM3AwaitDecision(t, v, receipt, rejected, false)
			decision := detail.Decisions[0]
			if decision.Question != question || decision.Status != "pending" || decision.Version != 1 || len(detail.Turns) != 1 || len(detail.Turns[0].Contributions) != 2 || detail.Turns[0].Contributions[1].Status != "running" || detail.Turns[0].Contributions[1].Message != nil {
				t.Fatal("Decision did not suspend the original contribution")
			}
			var waiting, consumed int
			workAppCheck(t, "decision-durable-wait", v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_execution.executions WHERE id=$1 AND status='waiting' AND waiting_reason='decision' AND waiting_reference=$2),(SELECT count(*) FROM agenteam_execution.loop_decision_results WHERE execution_id=$1)`, decision.ExecutionID.String(), decision.ID.String()).Scan(&waiting, &consumed))
			if waiting != 1 || consumed != 0 {
				t.Fatal("Decision was consumed before explicit Human response")
			}
			response := mt.RespondDecisionRequest{ProjectID: v.a, MeetingID: receipt.MeetingID, DecisionID: decision.ID, Action: action, ExpectedVersion: decision.Version, Meta: executionSchedulerAppMeta(t, nil)}
			if action == "answer" {
				value := answer
				response.Answer = &value
			}
			if action == "cancel" {
				_, err = v.runtime.meetings.Control(v.ctx, v.actor, mt.ControlRequest{ProjectID: v.a, MeetingID: receipt.MeetingID, Action: "cancel-turn", TurnID: receipt.TurnID, ExpectedVersion: detail.Session.Version, Meta: executionSchedulerAppMeta(t, nil)})
				workAppCheck(t, "decision-owner-cancel", err)
				response.Action = "skip"
				if _, err = v.runtime.meetings.RespondDecision(v.ctx, v.actor, response); err == nil {
					t.Fatal("late response revived cancelled decision")
				}
				detail = meetingM3AwaitDecision(t, v, receipt, rejected, true)
				if detail.Decisions[0].Status != "cancelled" || detail.Session.Summary != nil {
					t.Fatal("cancelled Decision published a summary")
				}
			} else {
				resolved, err := v.runtime.meetings.RespondDecision(v.ctx, v.actor, response)
				workAppCheck(t, "decision-owner-response", err)
				looked, found, err := v.runtime.meetings.LookupRespondDecision(v.ctx, v.actor, response)
				workAppCheck(t, "decision-original-response-lookup", err)
				if !found || !looked.Replayed || looked.Version != resolved.Version {
					t.Fatal("Decision response did not retain its original receipt")
				}
				detail = meetingM3AwaitCompleted(t, v, receipt, rejected)
				if detail.Turns[0].Contributions[1].ExecutionID == nil || *detail.Turns[0].Contributions[1].ExecutionID != decision.ExecutionID || detail.Turns[0].Contributions[1].Message == nil || detail.Turns[0].Contributions[1].Message.Content[0].Text != final {
					t.Fatal("Decision did not continue the same Execution")
				}
			}
			meetingM3CheckDecisionFacts(t, v, receipt, decision, action)
			v.runtime.StopAdmission()
			drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = v.runtime.Drain(drain)
			cancel()
			workAppCheck(t, "decision-original-runtime-drain", err)
			v.requestStop()
			usageAppWait(t, v.root.done, "decision-original-app-return")
			workAppCheck(t, "decision-original-app-exit", v.root.err)
			if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
				t.Fatal("Decision owners did not join")
			}
			count := 3
			if action == "cancel" {
				count = 1
			}
			state := w.joinedPeer(t, scenario, count, count)
			meetingM3GuardReleased(t, v.workAppFixture)
			if action != "cancel" {
				var request struct {
					Messages []struct {
						Role       string
						Content    json.RawMessage
						ToolCallID string `json:"tool_call_id"`
					} `json:"messages"`
				}
				workAppCheck(t, "decision-successor-request", json.Unmarshal([]byte(state.Requests[1].Body), &request))
				toolResult := ""
				toolResults := 0
				for _, message := range request.Messages {
					if message.Role == "tool" {
						var content []struct{ Type, Text string }
						workAppCheck(t, "decision-tool-result-text", json.Unmarshal(message.Content, &content))
						if message.ToolCallID != "meeting_decision_call" || len(content) != 1 || content[0].Type != "text" {
							t.Fatal("successor changed the original Decision Tool result envelope")
						}
						toolResult = content[0].Text
						toolResults++
					}
				}
				if toolResults != 1 {
					t.Fatal("successor must contain exactly one original Decision Tool result")
				}
				var resolved mt.DecisionResult
				workAppCheck(t, "decision-successor-original-result", json.Unmarshal([]byte(toolResult), &resolved))
				if resolved.DecisionID != decision.ID || resolved.Version != 2 || resolved.Status != map[string]string{"answer": "answered", "skip": "skipped"}[action] || action == "answer" && (resolved.Answer == nil || *resolved.Answer != answer) || action == "skip" && resolved.Answer != nil {
					t.Fatal("successor omitted the original resolved Tool result")
				}
			}
			t.Logf("M3_MEETING_DECISION action=%s original_execution=true original_tool_operation=true original_app_wait=true external_model=false", action)
		})
	}
}

// App Wait/Joined and the original flock are prerequisite gates. Observing a
// closed PostgreSQL client's server backend exit is a separate bounded read;
// it never terminates a connection or substitutes for the original App Wait.
func meetingM3GuardReleased(t *testing.T, v *workAppFixture) {
	t.Helper()
	workAppCheck(t, "meeting-original-guard-release", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "meeting-guard-observer-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	observations := 0
	var stopped bool
	var connections int
	for {
		err := v.admin.QueryRow(ctx, `SELECT coalesce((SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid),false),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, v.objects.process.String()).Scan(&stopped, &connections)
		if err != nil {
			t.Logf("MEETING_EXIT_FACTS stopped=%t database_gone=%t connections=%d observations=%d observed=%t observation_failed=true", stopped, connections == 0, connections, observations, observations > 0)
			workAppCheck(t, "meeting-original-exit-observation", err)
		}
		observations++
		if observations == 1 || stopped && connections == 0 {
			t.Logf("MEETING_EXIT_FACTS stopped=%t database_gone=%t connections=%d observations=%d", stopped, connections == 0, connections, observations)
		}
		if stopped && connections == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Meeting original exit facts unconfirmed: stopped=%t database_gone=%t connections=%d observations=%d", stopped, connections == 0, connections, observations)
		case <-tick.C:
		}
	}
}

func meetingM3AwaitDecision(t *testing.T, v *executionSchedulerApp, receipt mt.Receipt, rejected <-chan meetingM3RejectedTransaction, cancelled bool) mt.Detail {
	t.Helper()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var last string
	for {
		detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, receipt.MeetingID)
		if err != nil {
			meetingM3SafeError(t, "decision-detail", err)
		}
		workAppCheck(t, "decision-current-detail", err)
		meetingM3Progress(t, v, receipt, &last, cancelled)
		if !cancelled && len(detail.Decisions) == 1 && detail.Decisions[0].Status == "pending" {
			return detail
		}
		if cancelled && len(detail.Turns) == 1 && detail.Turns[0].Status == "cancelled" {
			return detail
		}
		for _, turn := range detail.Turns {
			for _, contribution := range turn.Contributions {
				if contribution.Kind == "agent" && contribution.Status == "failed" {
					t.Fatal("Decision contribution failed before requested transition")
				}
			}
		}
		select {
		case failure := <-rejected:
			t.Fatalf("Decision original transaction rejected owner=%s code=%s", failure.owner, failure.code.Safe())
		case <-v.ctx.Done():
			t.Fatal("Decision transition exceeded original App budget")
		case <-v.root.done:
			meetingM3SafeError(t, "decision-early-app-return", v.root.err)
			t.Fatal("App returned before Decision transition")
		case <-tick.C:
		}
	}
}

func meetingM3CheckDecisionFacts(t *testing.T, v *executionSchedulerApp, receipt mt.Receipt, decision mt.DecisionView, action string) {
	t.Helper()
	var executions, terminal, messages, results, pairs, toolCalls, modelCalls, retired, released int
	status, expectedMessages, expectedResults, expectedCalls := "succeeded", 2, 1, 3
	if action == "cancel" {
		status, expectedMessages, expectedResults, expectedCalls = "cancelled", 1, 0, 1
	}
	err := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request#>>'{trigger,meeting_id}'=$2),
 (SELECT count(*) FROM agenteam_execution.executions WHERE id=$3 AND status=$4),
 (SELECT count(*) FROM agenteam_meeting.messages WHERE meeting_id=$2),
 (SELECT count(*) FROM agenteam_execution.loop_decision_results WHERE execution_id=$3),
 (SELECT count(*) FROM agenteam_execution.loop_decision_results d JOIN agenteam_execution.loop_tool_results t ON t.execution_id=d.execution_id AND t.round_id=d.round_id AND t.operation_id=d.operation_id AND t.call_id=d.call_id AND t.message_digest=d.message_digest AND t.transcript_sequence=d.transcript_sequence AND t.checkpoint_version=d.checkpoint_version JOIN agenteam_meeting.decision_requests m ON m.id=d.decision_id AND m.operation_id=d.operation_id AND m.version=d.decision_version WHERE d.execution_id=$3 AND d.decision_id=$5),
 (SELECT count(*) FROM agenteam_execution.tool_calls WHERE execution_id=$3 AND operation_name='meeting.request-decision'),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1 AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1 AND l.released)`, v.a.String(), receipt.MeetingID.String(), decision.ExecutionID.String(), status, decision.ID.String()).Scan(&executions, &terminal, &messages, &results, &pairs, &toolCalls, &modelCalls, &retired, &released)
	workAppCheck(t, "decision-original-durable-pairs", err)
	if executions != 1 || terminal != 1 || messages != expectedMessages || results != expectedResults || pairs != expectedResults || toolCalls != 1 || modelCalls != expectedCalls || retired != expectedCalls || released != expectedCalls {
		t.Fatalf("Decision durable counts differ executions=%d terminal=%d messages=%d results=%d pairs=%d tools=%d models=%d retired=%d released=%d", executions, terminal, messages, results, pairs, toolCalls, modelCalls, retired, released)
	}
}

func meetingM3SystemCall(t *testing.T, v *executionSchedulerApp, method, path string, body, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, "/api/v1/system/"+path, raw, guardID[struct{}](t).String())
	clear(raw)
	workAppCheck(t, "meeting-model-http", err)
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("Meeting model setup route=%s status=%d code=%s", path, response.status, problem.Code.Safe())
	}
	if out != nil {
		workAppCheck(t, "meeting-model-response", json.Unmarshal(response.body, out))
	}
	clear(response.body)
}

func meetingM3ConfigureModel(t *testing.T, v *executionSchedulerApp, endpoint string) mc.ModelID {
	t.Helper()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret Service absent")
	}
	material, err := sc.NewSecretMaterial([]byte("owned-meeting-fixture-credential"))
	workAppCheck(t, "meeting-secret-material", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "meeting-secret-identity", err)
	credential, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	material.Destroy()
	workAppCheck(t, "meeting-secret-create", err)
	var receipt mc.CommandReceipt
	meetingM3SystemCall(t, v, http.MethodPost, "model-providers", map[string]any{"input": map[string]any{
		"name": "Meeting fixture Provider", "protocol": mc.OpenAIChat, "base_url": endpoint, "enabled": true,
		"credential_ref": credential.Metadata.CredentialRef.Details().ID, "options": map[string]any{},
	}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "meeting-provider-id", err)
	models := make([]mc.ModelID, 2)
	for n := range models {
		// Summary deliberately uses a text-only model. Agent tool capability is
		// a separate captured model configuration, even on the same owned peer.
		meetingM3SystemCall(t, v, http.MethodPost, "models", map[string]any{"provider_id": provider, "input": map[string]any{
			"name": fmt.Sprintf("Meeting fixture Model %d", n), "provider_model_id": "meeting-collaboration", "type": mc.ChatModel, "enabled": true,
			"parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{},
			"capabilities": map[string]any{"tool_calls": n == 0, "parallel_tool_calls": false, "streaming": true, "reasoning": false,
				"input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{},
				"structured_output_modes": []string{}, "context_length": nil, "max_output": mc.TokenCount(2048)},
		}}, &receipt)
		models[n], err = f.ParseID[mc.Model](receipt.ResourceID)
		workAppCheck(t, "meeting-model-id", err)
	}
	var selection mc.MeetingSummarySelection
	meetingM3SystemCall(t, v, http.MethodGet, "model-selection/meeting-summary", nil, &selection)
	meetingM3SystemCall(t, v, http.MethodPut, "model-selection/meeting-summary", map[string]any{"id": selection.ID, "expected_version": selection.Version, "model": models[1]}, &receipt)
	return models[0]
}

func meetingM3CreateAgents(t *testing.T, v *executionSchedulerApp, model mc.ModelID) []id.AgentID {
	t.Helper()
	disabled := false
	agents := make([]id.AgentID, 2)
	for n := range agents {
		agents[n] = guardID[id.Agent](t)
		request, err := ac.NewAgentCreate(ac.AgentCreateFields{
			AgentID: agents[n], Name: fmt.Sprintf("meeting-agent-%d", n+1), Description: "Meeting collaboration participant",
			Instructions: "Contribute a concise public response to the current meeting and consider previous participants' messages.",
			ModelRef:     model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{},
			AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false,
		})
		workAppCheck(t, "meeting-agent-intent", err)
		_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, request)
		workAppCheck(t, "meeting-agent-create", err)
	}
	return agents
}

type meetingM3RejectedTransaction struct {
	owner string
	code  f.Code
}

var meetingM3TechnicalIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_.-]{0,128}$`)

func meetingM3SafeIdentifier(value string) string {
	if meetingM3TechnicalIdentifier.MatchString(value) {
		return value
	}
	return "invalid_identifier"
}

func meetingM3SafeError(t *testing.T, stage string, original error) {
	t.Helper()
	var fault *f.Fault
	if errors.As(original, &fault) {
		t.Logf("M3_MEETING_ERROR stage=%s code=%s outcome=%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
	}
	var storeFailure *postgres.Error
	if errors.As(original, &storeFailure) {
		t.Logf("M3_MEETING_STORE stage=%s code=%s sqlstate=%s", stage, storeFailure.Code(), meetingM3SafeIdentifier(storeFailure.SQLState()))
	}
	var database *pgconn.PgError
	if errors.As(original, &database) {
		// Never log Message, Detail, SQL, arguments, request bodies or identities.
		t.Logf("M3_MEETING_SQL stage=%s sqlstate=%s position=%d table=%s constraint=%s", stage, meetingM3SafeIdentifier(database.Code), database.Position, meetingM3SafeIdentifier(database.TableName), meetingM3SafeIdentifier(database.ConstraintName))
	}
	t.Logf("M3_MEETING_ERROR_KIND stage=%s present=%t cancelled=%t deadline=%t", stage, original != nil, errors.Is(original, context.Canceled), errors.Is(original, context.DeadlineExceeded))
}

func meetingM3VerifyOwnedDatabase(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	db := v.root.db
	if db == nil || db.Fixture == nil || len(db.Fixture.Nonce) != 32 || db.Fixture.Verify() != nil ||
		!regexp.MustCompile(`^d03_`+regexp.QuoteMeta(db.Fixture.Nonce[:12])+`_[0-9a-f]{12}$`).MatchString(db.Name) || v.admin.Config().Database != db.Name {
		t.Fatal("Meeting diagnostics require the exact original owned database")
	}
	var current string
	workAppCheck(t, "meeting-owned-database", v.admin.QueryRow(v.ctx, `SELECT current_database()`).Scan(&current))
	if current != db.Name {
		t.Fatal("Meeting diagnostics database ownership differs")
	}
}

func meetingM3FailureObservation(t *testing.T, w *m1Workflow, scenario string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 2*time.Second)
	defer cancel()
	// Same safe Model fields used by the original M1 diagnostic. ModelError
	// persists a closed category and bounded technical code, never its cause or
	// provider body. No credential/request/provider-request ID is selected.
	rows, err := w.app.admin.Query(ctx, `SELECT COALESCE(error_data->>'category',''),COALESCE(error_data->>'code',''),usage_source FROM agenteam_model.invocations WHERE project_id::text=$1 AND final_status IN('failed','unknown','cancelled') ORDER BY started_at LIMIT 4`, w.app.a.String())
	if err != nil {
		meetingM3SafeError(t, "model-failure-observation", err)
	} else {
		for rows.Next() {
			var category, code, usage string
			if err = rows.Scan(&category, &code, &usage); err != nil {
				break
			}
			t.Logf("M3_MEETING_MODEL_FAILURE category=%s code=%s usage=%s", meetingM3SafeIdentifier(category), meetingM3SafeIdentifier(code), meetingM3SafeIdentifier(usage))
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			meetingM3SafeError(t, "model-failure-observation", err)
		}
	}
	state, err := w.network.State(ctx, scenario)
	if err != nil {
		meetingM3SafeError(t, "peer-observation", err)
		return
	}
	t.Logf("M3_MEETING_PEER requests=%d active=%d completed=%d", len(state.Requests), state.ActiveHandlers, state.CompletedHandlers)
	for n, request := range state.Requests {
		if n >= 4 {
			break
		}
		var shape struct {
			Stream   bool              `json:"stream"`
			Messages []json.RawMessage `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
		}
		err := json.Unmarshal([]byte(request.Body), &shape)
		t.Logf("M3_MEETING_PEER_REQUEST ordinal=%d json_valid=%t stream=%t messages=%d tools=%d", n+1, err == nil, shape.Stream, len(shape.Messages), len(shape.Tools))
	}
}

// One SQL statement gives a coherent observation. Every output key is a fixed
// literal and every value a count; input/text/credential/identity columns are
// never returned. It uses only the original verified fixture connection.
func meetingM3Progress(t *testing.T, v *executionSchedulerApp, receipt mt.Receipt, last *string, expectCancellation bool) {
	t.Helper()
	var raw []byte
	err := v.admin.QueryRow(v.ctx, `WITH e AS (
 SELECT * FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request#>>'{trigger,meeting_id}'=$2
), sm AS (SELECT * FROM agenteam_meeting.summary_model_calls WHERE meeting_id=$2),
 mc AS (SELECT * FROM agenteam_model.calls WHERE project_id::text=$1),
 inv AS (SELECT * FROM agenteam_model.invocations WHERE project_id::text=$1)
SELECT
 (SELECT jsonb_build_object('e_created',count(*) FILTER(WHERE status='created'),'e_preparing',count(*) FILTER(WHERE status='preparing'),
 'e_running',count(*) FILTER(WHERE status='running'),'e_waiting',count(*) FILTER(WHERE status='waiting'),
 'e_succeeded',count(*) FILTER(WHERE status='succeeded'),'e_failed',count(*) FILTER(WHERE status='failed'),'e_cancelled',count(*) FILTER(WHERE status='cancelled')) FROM e)
 || (SELECT jsonb_build_object('intent_total',count(*),'intent_bound',count(execution_id),'intent_unknown',count(*) FILTER(WHERE launch_unknown)) FROM agenteam_meeting.launch_intents WHERE meeting_id=$2)
 || jsonb_build_object('preparation_inputs',(SELECT count(*) FROM agenteam_execution.preparation_inputs WHERE execution_id IN(SELECT id FROM e)),
 'preparation_claims',(SELECT count(*) FROM agenteam_execution.preparation_claims WHERE execution_id IN(SELECT id FROM e)),
 'preparation_running',(SELECT count(*) FROM agenteam_execution.preparation_attempts WHERE execution_id IN(SELECT id FROM e) AND phase='running'),
 'preparation_returned',(SELECT count(*) FROM agenteam_execution.preparation_attempts WHERE execution_id IN(SELECT id FROM e) AND phase='terminal'),
 'execution_snapshots',(SELECT count(*) FROM agenteam_execution.snapshots WHERE execution_id IN(SELECT id FROM e)),
 'round_requests',(SELECT count(*) FROM agenteam_execution.round_model_requests WHERE execution_id IN(SELECT id FROM e)),
 'rounds_active',(SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM e) AND terminal_status IS NULL),
 'rounds_model_failed',(SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM e) AND terminal_reason='model_failed'),
 'rounds_runtime_failed',(SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM e) AND terminal_reason='runtime_failed'),
 'rounds_incomplete',(SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM e) AND terminal_reason='incomplete_response'))
 || (SELECT jsonb_build_object('ecall_service',count(*) FILTER(WHERE lane='service' AND phase='running'),
 'ecall_preparation',count(*) FILTER(WHERE lane='preparation' AND phase='running'),'ecall_direct',count(*) FILTER(WHERE lane='direct' AND phase='running'),
 'ecall_associated',count(*) FILTER(WHERE lane='associated' AND phase='running')) FROM agenteam_execution.project_calls WHERE project_id::text=$1)
 || (SELECT jsonb_build_object('model_resolution',count(*),'model_binding',count(*) FILTER(WHERE phase='committed')) FROM agenteam_model.resolution_preparations WHERE project_id::text=$1)
 || (SELECT jsonb_build_object('model_accepted',count(*) FILTER(WHERE phase='accepted'),'model_running',count(*) FILTER(WHERE phase='running'),
 'model_retry_wait',count(*) FILTER(WHERE phase='retry_wait'),'model_succeeded',count(*) FILTER(WHERE phase='succeeded'),
 'model_failed',count(*) FILTER(WHERE phase='failed'),'model_unknown',count(*) FILTER(WHERE phase='unknown'),'model_cancelled',count(*) FILTER(WHERE phase='cancelled'),
 'model_retired',count(*) FILTER(WHERE retired),'model_unretired',count(*) FILTER(WHERE NOT retired)) FROM mc)
 || (SELECT jsonb_build_object('dispatch_reserved',count(*) FILTER(WHERE dispatch='reserved'),'dispatch_authorized',count(*) FILTER(WHERE dispatch='authorized'),
 'dispatch_sent',count(*) FILTER(WHERE dispatch='sent'),'dispatch_not_sent',count(*) FILTER(WHERE dispatch='not_sent'),'dispatch_unknown',count(*) FILTER(WHERE dispatch='unknown')) FROM agenteam_model.runtime_attempts WHERE project_id::text=$1)
 || (SELECT jsonb_build_object('inv_agent',count(*) FILTER(WHERE consumer_kind='agent'),'inv_meeting',count(*) FILTER(WHERE consumer_kind='meeting'),
 'inv_succeeded',count(*) FILTER(WHERE final_status='succeeded'),'inv_failed',count(*) FILTER(WHERE final_status='failed'),
 'inv_unknown',count(*) FILTER(WHERE final_status='unknown'),'inv_cancelled',count(*) FILTER(WHERE final_status='cancelled')) FROM inv)
 || (SELECT jsonb_build_object('secret_released',count(*) FILTER(WHERE released),'secret_unreleased',count(*) FILTER(WHERE NOT released)) FROM agenteam_secret.secret_leases WHERE (owner_kind='execution' AND owner_id::text IN(SELECT id FROM e)) OR (owner_kind='model_call' AND owner_id::text IN(SELECT call_id FROM sm)))
 || (SELECT jsonb_build_object('summary_prepared',count(*) FILTER(WHERE status='prepared'),'summary_generating',count(*) FILTER(WHERE status='generating'),
 'summary_failed',count(*) FILTER(WHERE status='failed'),'summary_published',count(*) FILTER(WHERE status='published')) FROM agenteam_meeting.summary_generations WHERE meeting_id=$2)
 || (SELECT jsonb_build_object('summary_call_prepared',count(*) FILTER(WHERE status='prepared'),'summary_call_resolving',count(*) FILTER(WHERE status='resolving'),
 'summary_call_resolved',count(*) FILTER(WHERE status='resolved'),'summary_call_running',count(*) FILTER(WHERE status='running'),
 'summary_call_unknown',count(*) FILTER(WHERE status='unknown'),'summary_call_failed',count(*) FILTER(WHERE status='failed'),
 'summary_call_succeeded',count(*) FILTER(WHERE status='succeeded'),'summary_call_retired',count(retired_at)) FROM sm)`, v.a.String(), receipt.MeetingID.String()).Scan(&raw)
	if err != nil {
		meetingM3SafeError(t, "progress-read", err)
		t.Fatal("Meeting safe progress observation failed")
	}
	var counts map[string]int64
	workAppCheck(t, "meeting-progress-counts", json.Unmarshal(raw, &counts))
	encoded, err := json.Marshal(counts)
	workAppCheck(t, "meeting-progress-encoding", err)
	if string(encoded) != *last {
		*last = string(encoded)
		t.Logf("M3_MEETING_PROGRESS %s", encoded)
	}
	if counts["e_failed"]+counts["model_unknown"]+counts["summary_call_unknown"]+counts["summary_failed"] > 0 || !expectCancellation && counts["e_cancelled"] > 0 {
		t.Fatal("Meeting original durable failure observed; ending before the budget expires")
	}
}

func meetingM3AwaitCompleted(t *testing.T, v *executionSchedulerApp, receipt mt.Receipt, rejected <-chan meetingM3RejectedTransaction) mt.Detail {
	t.Helper()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var last mt.Detail
	var lastCounts, lastDetail string
	for {
		var err error
		last, err = v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, receipt.MeetingID)
		if err != nil {
			meetingM3SafeError(t, "domain-detail", err)
		}
		workAppCheck(t, "meeting-current-detail", err)
		meetingM3Progress(t, v, receipt, &lastCounts, false)
		if len(last.Turns) == 1 && last.Turns[0].ID == *receipt.TurnID {
			turn := last.Turns[0]
			stage := meetingM3SafeIdentifier(string(turn.Status))
			for _, c := range turn.Contributions {
				stage += fmt.Sprintf("/%s:%s:execution=%t:message=%t", meetingM3SafeIdentifier(string(c.Kind)), meetingM3SafeIdentifier(string(c.Status)), c.ExecutionID != nil, c.Message != nil)
			}
			if stage != lastDetail {
				lastDetail = stage
				t.Logf("M3_MEETING_DETAIL %s", stage)
			}
			if turn.Status == "completed" {
				return last
			}
			if turn.Status == "cancelled" || turn.SummaryError != "" {
				t.Fatalf("Meeting did not complete: status=%s summary_code=%s", turn.Status, turn.SummaryError)
			}
			for _, contribution := range turn.Contributions {
				if contribution.Status == "failed" || contribution.Status == "cancelled" {
					t.Fatalf("Meeting contribution did not succeed: kind=%s status=%s", contribution.Kind, contribution.Status)
				}
			}
		}
		select {
		case failure := <-rejected:
			t.Fatalf("Meeting original transaction rejected: owner=%s code=%s", failure.owner, failure.code.Safe())
		case <-v.ctx.Done():
			t.Fatalf("Meeting did not complete within its original App budget: turns=%d", len(last.Turns))
		case <-v.root.done:
			meetingM3SafeError(t, "early-app-return", v.root.err)
			t.Fatal("original App returned before Meeting completed")
		case <-tick.C:
		}
	}
}

func meetingM3CheckDurableFacts(t *testing.T, v *executionSchedulerApp, receipt mt.Receipt) {
	t.Helper()
	var executions, messages, published, summaryCalls, agentInvocations int
	var closed bool
	err := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request#>>'{trigger,meeting_id}'=$2 AND launch_request->>'purpose'='meeting/response' AND status='succeeded'),
 (SELECT count(*) FROM agenteam_meeting.messages WHERE meeting_id=$2),
 (SELECT count(*) FROM agenteam_meeting.summary_generations WHERE meeting_id=$2 AND turn_id=$3 AND status='published' AND attempt=1),
 (SELECT count(*) FROM agenteam_meeting.summary_model_calls WHERE meeting_id=$2),
 (SELECT count(*) FROM agenteam_model.invocations i JOIN agenteam_execution.executions e ON e.id=i.execution_id::text WHERE e.project_id=$1 AND e.launch_request#>>'{trigger,meeting_id}'=$2 AND i.consumer_kind='agent' AND i.purpose='agent_generation' AND i.final_status='succeeded' AND i.dispatch='sent'),
 (SELECT COALESCE(bool_and(m.status='succeeded' AND m.retired_at IS NOT NULL AND m.terminal_version=1 AND c.phase='succeeded' AND c.retired AND l.released AND i.final_status='succeeded' AND i.dispatch='sent' AND i.consumer_kind='meeting' AND i.purpose='meeting_summary_initial' AND i.operation_id::text=m.generation_id AND i.input_data#>>'{value,digest}'=m.input_digest AND (SELECT count(*) FROM agenteam_model.runtime_attempts a WHERE a.call_id=c.id)=1 AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls u WHERE u.call_id=c.id AND u.phase<>'business_closed')),false)
 FROM agenteam_meeting.summary_model_calls m JOIN agenteam_model.calls c ON c.id::text=m.call_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE m.meeting_id=$2)`, v.a.String(), receipt.MeetingID.String(), receipt.TurnID.String()).Scan(&executions, &messages, &published, &summaryCalls, &agentInvocations, &closed)
	workAppCheck(t, "meeting-durable-execution-model-summary", err)
	if executions != 2 || messages != 3 || published != 1 || summaryCalls != 1 || agentInvocations != 2 || !closed {
		t.Fatalf("Meeting durable facts differ: executions=%d messages=%d summaries=%d calls=%d agent_invocations=%d summary_closed=%t", executions, messages, published, summaryCalls, agentInvocations, closed)
	}
}

func meetingM3CheckRequests(t *testing.T, state netfixture.State, first, second string) {
	t.Helper()
	if len(state.Requests) != 3 {
		t.Fatal("Meeting requires exactly two Agent requests and one Summary request")
	}
	for n, request := range state.Requests {
		var body struct {
			Messages json.RawMessage `json:"messages"`
			Tools    json.RawMessage `json:"tools"`
			Format   struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		workAppCheck(t, "meeting-observed-request-shape", json.Unmarshal([]byte(request.Body), &body))
		// Decode the messages before searching so nested JSON escaping cannot
		// accidentally satisfy or defeat the public-context assertion.
		var messages []struct {
			Content string `json:"content"`
		}
		workAppCheck(t, "meeting-observed-messages", json.Unmarshal(body.Messages, &messages))
		texts := make([]string, len(messages))
		for k := range messages {
			texts[k] = messages[k].Content
		}
		joined := strings.Join(texts, "\n")
		if n == 0 && (strings.Contains(joined, first) || strings.Contains(joined, second)) {
			t.Fatal("first Agent observed a future contribution")
		}
		if n == 1 && (!strings.Contains(joined, first) || strings.Contains(joined, second)) {
			t.Fatal("second Agent request did not contain exactly the already-published predecessor")
		}
		if n == 2 && (!strings.Contains(joined, first) || !strings.Contains(joined, second) || body.Format.Type != "" && body.Format.Type != "text" || len(body.Tools) > 0 && string(body.Tools) != "[]") {
			t.Fatal("Summary request omitted current contributions or required structured output/tools")
		}
	}
}
