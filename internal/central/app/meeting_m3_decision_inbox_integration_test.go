//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	hc "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// One original Meeting exercises three distinct original Executions. Inbox is
// the canonical action projection; only the existing Owner HTTP command may
// answer, skip or cancel the original waiting Decision.
func TestMeetingM3AppDecisionInboxLifecycle(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	rejected := make(chan meetingM3RejectedTransaction, 1)
	successorRejected := make(chan f.Code, 1)
	w, _ := newM1WorkflowWithOptions(t, func(cause f.TransactionCause, result f.CommitResult) {
		if failure := result.Fault(); result.State() == f.NotCommitted && failure != nil && cause.Details().JobType == "execution-loop-successor" {
			select {
			case successorRejected <- failure.Code:
			default:
			}
		}
		if failure := result.Fault(); result.State() == f.NotCommitted && failure != nil && failure.Code == f.InternalError {
			meetingM3SafeError(t, "decision-inbox-original-transaction", failure)
			select {
			case rejected <- meetingM3RejectedTransaction{cause.Details().Owner, failure.Code}:
			default:
			}
		}
	}, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	var tool string
	workAppCheck(t, "decision-inbox-registered-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:request-decision'`).Scan(&tool))
	const question = "Which calibration should the original contribution use?"
	const answer = "Use the smaller calibration."
	const final = "The original contribution recorded the Owner response."
	decisionReply := func(ordinal int) netfixture.WireScenario {
		args := string(workAppJSON(t, map[string]any{"question": question, "options": []string{"smaller", "larger"}}))
		raw := workAppJSON(t, map[string]any{"id": "owned-decision-inbox", "object": "chat.completion", "created": ordinal, "model": "meeting-collaboration", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "meeting_decision_call", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool, "-", ""), "arguments": args}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
		return netfixture.WireScenario{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{raw}}
	}
	firstSummary := string(workAppJSON(t, map[string]string{"title": "Owner calibration decisions", "goals": "Resolve the original choices", "decisions": "First response recorded", "unresolved": "", "facts": "One completed contribution"}))
	nextSummary := string(workAppJSON(t, map[string]string{"goals": "Resolve the original choices", "decisions": "A later choice was skipped", "unresolved": "", "facts": "Two completed contributions"}))
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{decisionReply(1), meetingM3Reply(t, 2, final), meetingM3Reply(t, 3, firstSummary), decisionReply(4), meetingM3Reply(t, 5, final), meetingM3Reply(t, 6, nextSummary), decisionReply(7)}})
	workAppCheck(t, "decision-inbox-owned-peer", err)
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()
	var receipt mt.Receipt
	meetingDeletionHTTP(t, v, "decision-inbox-start", http.MethodPost, "/meetings", guardID[struct{}](t).String(), map[string]any{"agents": agents[:1], "mode": "sequential", "content_nodes": []mt.InlineNode{{Type: "text", Text: "Ask the Owner to resolve the first calibration."}}}, &receipt)
	if receipt.TurnID == nil {
		t.Fatal("original Start omitted Turn")
	}
	path := "/meetings/" + receipt.MeetingID.String()
	var detail mt.Detail
	var decision mt.DecisionView
	ordinal, action, phase := -1, "start", "started"
	var latest, lastDetail, lastCounts string
	var observedAt time.Time
	capture := func(force bool) {
		if v.ctx.Err() != nil || !force && time.Since(observedAt) < time.Second {
			return
		}
		observedAt = time.Now()
		if decision.ID.Validate() == nil {
			ctx, cancel := context.WithTimeout(v.ctx, time.Second)
			state, err := meetingDecisionInboxState(ctx, v, receipt.MeetingID, decision.ID)
			cancel()
			if err != nil {
				meetingM3SafeError(t, "decision-inbox-safe-state", err)
			} else if state != latest {
				latest = state
				t.Logf("M3_DECISION_STATE ordinal=%d action=%s phase=%s facts=%s", ordinal+1, action, phase, latest)
			}
		}
		meetingM3Progress(t, v, receipt, &lastCounts, action == "cancel")
	}
	defer func() {
		if t.Failed() {
			// The original budget may already be exhausted. Print only facts
			// observed while its context was live, never query after expiry.
			t.Logf("M3_DECISION_LAST ordinal=%d action=%s phase=%s observed=%t detail=%s facts=%s", ordinal+1, action, phase, !observedAt.IsZero(), lastDetail, latest)
		}
	}()
	await := func(stage string, observe func() bool) {
		t.Helper()
		phase = stage
		t.Logf("M3_DECISION_STAGE ordinal=%d action=%s phase=%s", ordinal+1, action, phase)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			done := observe()
			select {
			case code := <-successorRejected:
				t.Logf("M3_DECISION_SUCCESSOR ordinal=%d action=%s phase=%s code=%s outcome=not_committed", ordinal+1, action, phase, code.Safe())
				capture(true)
			default:
				capture(false)
			}
			if done {
				return
			}
			select {
			case failure := <-rejected:
				t.Fatalf("decision inbox transaction owner=%s code=%s", meetingM3SafeIdentifier(failure.owner), failure.code.Safe())
			case <-v.root.done:
				meetingM3SafeError(t, "decision-inbox-early-return", v.root.err)
				t.Fatal("original App returned before ", stage)
			case <-v.ctx.Done():
				t.Fatal("decision inbox exceeded original App budget: ", stage)
			case <-tick.C:
			}
		}
	}
	readDetail := func() {
		meetingDeletionHTTP(t, v, "decision-inbox-detail", http.MethodGet, path, "", nil, &detail)
		var states []string
		for _, turn := range detail.Turns {
			if turn.ID == *receipt.TurnID {
				states = append(states, "turn="+meetingM3SafeIdentifier(turn.Status), "summary_error="+meetingM3SafeIdentifier(turn.SummaryError))
				for _, contribution := range turn.Contributions {
					states = append(states, meetingM3SafeIdentifier(contribution.Kind)+"="+meetingM3SafeIdentifier(contribution.Status))
				}
			}
		}
		state := strings.Join(states, "/")
		if state != lastDetail {
			lastDetail = state
			t.Logf("M3_DECISION_DETAIL ordinal=%d action=%s phase=%s %s", ordinal+1, action, phase, lastDetail)
		}
	}
	seen := map[string]string{}
	for n, nextAction := range []string{"answer", "skip", "cancel"} {
		ordinal, action = n, nextAction
		decision = mt.DecisionView{}
		latest, lastDetail = "", ""
		if ordinal != 0 {
			readDetail()
			meetingDeletionHTTP(t, v, "decision-inbox-next-turn", http.MethodPost, path+"/turns", guardID[struct{}](t).String(), map[string]any{"expected_version": detail.Session.Version, "mode": "sequential", "interrupt": false, "content_nodes": []mt.InlineNode{{Type: "text", Text: "Ask the Owner for the next calibration choice."}}}, &receipt)
			if receipt.TurnID == nil {
				t.Fatal("original Send omitted Turn")
			}
		}
		await("original-pending-decision", func() bool {
			readDetail()
			for _, turn := range detail.Turns {
				if turn.ID == *receipt.TurnID {
					for _, contribution := range turn.Contributions {
						for _, d := range detail.Decisions {
							if d.ContributionID == contribution.ID && d.Status == "pending" {
								decision = d
								return true
							}
						}
					}
				}
			}
			return false
		})
		if decision.Question != question || decision.Version != 1 {
			t.Fatal("pending Decision lost its original question/version")
		}
		var open hc.Page
		await("canonical-inbox-open", func() bool {
			meetingDeletionHTTP(t, v, "decision-inbox-open", http.MethodGet, "/inbox?status=open&limit=50", "", nil, &open)
			return len(open.Items) == 1
		})
		item := open.Items[0]
		if item.SourceType != "decision_request" || item.ActionType != "decision.answer" || item.SourceID != decision.ID.String() || item.Context.MeetingID != receipt.MeetingID.String() || item.SourceVersion != 1 || !item.Blocking || item.Dismissible || item.Status != "open" {
			t.Fatal("Inbox did not preserve the exact actionable Decision source")
		}
		if _, exists := seen[item.SourceID]; exists {
			t.Fatal("new Turn reused an old Decision")
		}
		seen[item.SourceID] = action
		key := guardID[struct{}](t).String()
		suffix := path + "/decisions"
		body := map[string]any{"decision_id": decision.ID, "action": action, "expected_version": decision.Version}
		if action == "answer" {
			body["answer"] = answer
		}
		if action == "cancel" {
			readDetail()
			suffix = path + "/controls"
			body = map[string]any{"action": "cancel-turn", "turn_id": receipt.TurnID, "expected_version": detail.Session.Version}
		}
		var committed mt.Receipt
		meetingDeletionHTTP(t, v, "decision-inbox-owner-"+action, http.MethodPost, suffix, key, body, &committed)
		var lookup struct {
			Status  string     `json:"status"`
			Receipt mt.Receipt `json:"receipt"`
		}
		meetingDeletionHTTP(t, v, "decision-inbox-original-lookup", http.MethodPost, suffix+"/lookup", key, body, &lookup)
		if lookup.Status != "committed" || !lookup.Receipt.Replayed || lookup.Receipt.MeetingID != receipt.MeetingID || lookup.Receipt.Version != committed.Version {
			t.Fatal("Inbox action lost its original receipt")
		}
		wanted := map[string]string{"answer": "answered", "skip": "skipped", "cancel": "cancelled"}[action]
		await("original-terminal-turn", func() bool {
			readDetail()
			for _, d := range detail.Decisions {
				if d.ID == decision.ID && d.Status == wanted && d.Version == 2 {
					for _, turn := range detail.Turns {
						if turn.ID == *receipt.TurnID {
							if action == "cancel" {
								return turn.Status == "cancelled"
							}
							if turn.Status != "completed" {
								return false
							}
							for _, contribution := range turn.Contributions {
								if contribution.ID == decision.ContributionID {
									if contribution.ExecutionID == nil || *contribution.ExecutionID != decision.ExecutionID || contribution.Message == nil || len(contribution.Message.Content) != 1 || contribution.Message.Content[0].Text != final {
										t.Fatal("answer resumed a replacement Execution")
									}
									return true
								}
							}
						}
					}
				}
			}
			return false
		})
		await("canonical-inbox-resolved", func() bool {
			var page hc.Page
			meetingDeletionHTTP(t, v, "decision-inbox-resolved", http.MethodGet, "/inbox?status=resolved&limit=50", "", nil, &page)
			for _, resolved := range page.Items {
				if resolved.SourceID == decision.ID.String() {
					if resolved.ID != item.ID || resolved.SourceVersion != 2 || resolved.Context.MeetingID != receipt.MeetingID.String() || resolved.Status != "resolved" {
						t.Fatal("resolution replaced the canonical Inbox item")
					}
					return true
				}
			}
			return false
		})
	}
	var rebuilt struct {
		Rebuilt bool `json:"rebuilt"`
	}
	meetingDeletionHTTP(t, v, "decision-inbox-rebuild", http.MethodPost, "/inbox/rebuild", "", struct{}{}, &rebuilt)
	if !rebuilt.Rebuilt {
		t.Fatal("Owner rebuild did not complete")
	}
	var open, resolved hc.Page
	meetingDeletionHTTP(t, v, "decision-inbox-no-reopen", http.MethodGet, "/inbox?status=open&limit=50", "", nil, &open)
	meetingDeletionHTTP(t, v, "decision-inbox-history", http.MethodGet, "/inbox?status=resolved&limit=50", "", nil, &resolved)
	if len(open.Items) != 0 || len(resolved.Items) != 3 || detail.Session.Summary == nil || detail.Session.Summary.Version != 2 {
		t.Fatal("rebuild reopened an old Decision or cancelled Turn advanced Summary")
	}
	for _, item := range resolved.Items {
		if _, exists := seen[item.SourceID]; !exists || item.Context.MeetingID != receipt.MeetingID.String() || item.SourceType != "decision_request" || item.ActionType != "decision.answer" {
			t.Fatal("rebuild changed the original source pair")
		}
	}
	var events, requested, closed, atomicEvents, atomicTerminal, executions, succeeded, cancelled, results, calls, retired, released int
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_meeting.decision_events WHERE project_id=$1::text AND meeting_id=$2::text),
 (SELECT count(*) FROM agenteam_meeting.decision_events WHERE project_id=$1::text AND meeting_id=$2::text AND event_type='meeting.decision.requested'),
 (SELECT count(*) FROM agenteam_meeting.decision_events WHERE project_id=$1::text AND meeting_id=$2::text AND status IN('answered','skipped','cancelled')),
 (SELECT count(*) FROM agenteam_meeting.decision_events e JOIN agenteam_outbox.events o ON o.id::text=e.event_id WHERE e.project_id=$1::text AND e.meeting_id=$2::text AND e.xmin=o.xmin),
 (SELECT count(*) FROM agenteam_meeting.decision_events e JOIN agenteam_meeting.decision_requests d ON d.id=e.decision_id WHERE e.project_id=$1::text AND e.meeting_id=$2::text AND e.status=d.status AND e.version=d.version AND e.xmin=d.xmin),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text AND status='succeeded'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text AND status='cancelled'),
 (SELECT count(*) FROM agenteam_execution.loop_decision_results r JOIN agenteam_execution.executions e ON e.id=r.execution_id WHERE e.project_id=$1::text AND e.launch_request#>>'{trigger,meeting_id}'=$2::text),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1::text AND l.released)`, v.a.String(), receipt.MeetingID.String()).Scan(&events, &requested, &closed, &atomicEvents, &atomicTerminal, &executions, &succeeded, &cancelled, &results, &calls, &retired, &released)
	if err != nil {
		meetingM3SafeError(t, "decision-inbox-original-facts", err)
	}
	workAppCheck(t, "decision-inbox-original-facts", err)
	if events != 6 || requested != 3 || closed != 3 || atomicEvents != 6 || atomicTerminal != 3 || executions != 3 || succeeded != 2 || cancelled != 1 || results != 2 || calls != 7 || retired != 7 || released != 7 {
		t.Fatalf("decision inbox facts events=%d requested=%d closed=%d atomic_events=%d atomic_terminal=%d executions=%d succeeded=%d cancelled=%d results=%d calls=%d retired=%d released=%d", events, requested, closed, atomicEvents, atomicTerminal, executions, succeeded, cancelled, results, calls, retired, released)
	}
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "decision-inbox-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "decision-inbox-app-wait")
	workAppCheck(t, "decision-inbox-app-return", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Decision/Inbox original owners did not join")
	}
	state := w.joinedPeer(t, scenario, 7, 7)
	for _, index := range []int{1, 4} {
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		workAppCheck(t, "decision-inbox-original-successor", json.Unmarshal([]byte(state.Requests[index].Body), &request))
		count := 0
		for _, message := range request.Messages {
			if message.Role == "tool" {
				count++
				var parts []struct{ Type, Text string }
				workAppCheck(t, "decision-inbox-tool-text", json.Unmarshal(message.Content, &parts))
				if len(parts) != 1 || parts[0].Type != "text" {
					t.Fatal("Decision successor did not use original Tool content")
				}
				var result mt.DecisionResult
				workAppCheck(t, "decision-inbox-tool-result", json.Unmarshal([]byte(parts[0].Text), &result))
				if result.Validate() != nil || seen[result.DecisionID.String()] == "" || result.Version != 2 {
					t.Fatal("successor substituted another Decision result")
				}
			}
		}
		if count != 1 {
			t.Fatal("successor omitted the original Decision Tool result")
		}
	}
	meetingDeletionGuardReleased(t, v.workAppFixture)
	t.Log("M3_MEETING_DECISION_INBOX original_owner_http=true requested_answered_skipped_cancelled=true canonical_outbox_same_tx=true strict_source=true rebuilt_without_reopen=true original_execution=true original_app_joined=true external_model=false")
}

// Read the exact original Decision/Execution tuple from the verified owned
// fixture. Only closed states, counts, versions and equality booleans leave SQL;
// IDs, arguments, content, credentials and provider output are never selected.
func meetingDecisionInboxState(ctx context.Context, v *executionSchedulerApp, meeting mt.MeetingID, decision mt.DecisionID) (string, error) {
	var raw []byte
	err := v.admin.QueryRow(ctx, `WITH d AS (
 SELECT d.* FROM agenteam_meeting.decision_requests d JOIN agenteam_meeting.sessions s ON s.id=d.meeting_id
 WHERE d.id=$3::text AND d.meeting_id=$2::text AND s.project_id=$1::text
), e AS (
 SELECT * FROM agenteam_execution.executions WHERE id IN(SELECT execution_id FROM d) AND project_id=$1::text
), r AS (
 SELECT * FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM e) ORDER BY round_ordinal DESC LIMIT 1
), op AS (
 SELECT * FROM agenteam_tool.operations WHERE operation_id IN(SELECT operation_id FROM d) AND execution_id IN(SELECT id FROM e) AND project_id=$1::text
)
SELECT jsonb_build_object(
 'decision_status',COALESCE((SELECT status FROM d),'missing'),
 'decision_version',COALESCE((SELECT version FROM d),0),
 'execution_status',COALESCE((SELECT status FROM e),'missing'),
 'execution_version',COALESCE((SELECT version FROM e),0),
 'execution_cancel_requested',COALESCE((SELECT cancel_requested_at IS NOT NULL FROM e),false),
 'waiting_reason',COALESCE((SELECT waiting_reason FROM e),''),
 'waiting_for_original',COALESCE((SELECT waiting_reference::text=$3::text FROM e),false),
 'latest_round_is_original',COALESCE((SELECT r.id=d.round_id FROM r CROSS JOIN d),false),
 'latest_round_ordinal',COALESCE((SELECT round_ordinal FROM r),0),
 'latest_round_terminal',COALESCE((SELECT terminal_status FROM r),''),
 'latest_round_tool_pending',COALESCE((SELECT tool_pending FROM r),false),
 'transcript_through',COALESCE((SELECT transcript_through FROM r),0),
 'original_tool_checkpoint',COALESCE((SELECT max(checkpoint_version) FROM agenteam_execution.tool_calls WHERE execution_id IN(SELECT id FROM e) AND round_id IN(SELECT round_id FROM d)),0),
 'original_tool_phase',COALESCE((SELECT phase FROM op),'missing'),
 'original_tool_outcome',COALESCE((SELECT outcome FROM op),''),
 'original_tool_active_attempt',COALESCE((SELECT active_attempt_id IS NOT NULL FROM op),false),
 'original_tool_attempts',(SELECT count(*) FROM agenteam_tool.attempts WHERE operation_id IN(SELECT operation_id FROM op)),
 'original_tool_retired_attempts',(SELECT count(*) FROM agenteam_tool.attempts WHERE operation_id IN(SELECT operation_id FROM op) AND retired),
 'original_loop_tool_results',(SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id IN(SELECT id FROM e) AND round_id IN(SELECT round_id FROM d)),
 'original_loop_decision_results',(SELECT count(*) FROM agenteam_execution.loop_decision_results WHERE execution_id IN(SELECT id FROM e) AND decision_id=$3::text),
 'current_contribution_matches',EXISTS(SELECT 1 FROM agenteam_meeting.contributions c JOIN d ON c.id=d.contribution_id AND c.meeting_id=d.meeting_id AND c.current_execution_id=d.execution_id),
 'current_launch_matches',EXISTS(SELECT 1 FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id AND c.generation=l.generation AND c.attempt=l.attempt JOIN d ON d.contribution_id=c.id AND d.execution_id=l.execution_id WHERE l.meeting_id=$2::text)
)`, v.a.String(), meeting.String(), decision.String()).Scan(&raw)
	return string(raw), err
}
