//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	hc "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// The sole stimulus is an Owner-created ordinary Turn. Its real Agent model
// requests two meetings and lists proposals through the registered Builtins.
// No test creates a tool operation, execution, proposal or canonical event.
func TestMeetingM3AppProposalInbox(t *testing.T) {
	runMeetingM3ProposalInbox(t, nil)
}

type meetingProposalBrowserCase struct {
	Reason   string          `json:"reason"`
	Approve  bool            `json:"approve"`
	Proposal mt.ProposalView `json:"proposal"`
}
type meetingProposalBrowserReview struct {
	Key     string     `json:"key"`
	Approve bool       `json:"approve"`
	Receipt mt.Receipt `json:"receipt"`
}
type meetingProposalReviewDriver func(*testing.T, *m1Workflow, net.Listener, []meetingProposalBrowserCase) map[string]meetingProposalBrowserReview

func runMeetingM3ProposalInbox(t *testing.T, review meetingProposalReviewDriver) {
	meetingM3ConfigurationPreflight(t)
	rejected := make(chan meetingM3RejectedTransaction, 1)
	w, listener := newM1WorkflowWithOptions(t, func(cause f.TransactionCause, result f.CommitResult) {
		if failure := result.Fault(); result.State() == f.NotCommitted && failure != nil && failure.Code == f.InternalError {
			meetingM3SafeError(t, "proposal-original-transaction", failure)
			select {
			case rejected <- meetingM3RejectedTransaction{cause.Details().Owner, failure.Code}:
			default:
			}
		}
	}, false)
	v := w.app
	if v.runtime.meetingProposals == nil || v.runtime.meetingInbox == nil {
		t.Fatal("original App Proposal/Inbox unbound")
	}
	meetingM3VerifyOwnedDatabase(t, v)
	tools := map[string]string{}
	for _, name := range []string{"request-meeting", "list-meetings", "add-agent-to-meeting", "remove-agent-from-meeting"} {
		var tool string
		workAppCheck(t, "proposal-real-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, "builtin:"+name).Scan(&tool))
		tools[name] = tool
	}
	const acceptText = "Review calibration capacity with the owner."
	const rejectText = "Consider a separate calibration scheduling meeting."
	const final = "I proposed both meetings for the owner and verified the pending proposals."
	summary := string(workAppJSON(t, map[string]string{"title": "Calibration proposals", "goals": "Ask the owner to review meetings", "decisions": "Two proposals await review", "unresolved": "Owner review", "facts": "One agent proposed two sessions"}))
	toolReply := func(name, call string, args any) netfixture.WireScenario {
		raw := workAppJSON(t, map[string]any{"id": "owned-meeting-proposal", "object": "chat.completion", "created": 1, "model": "meeting-collaboration", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": call, "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tools[name], "-", ""), "arguments": string(workAppJSON(t, args))}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
		return netfixture.WireScenario{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{raw}}
	}
	first := toolReply("request-meeting", "proposal_accept_call", map[string]any{"proposal_content": acceptText})
	hold := 0
	first.HoldAfter = &hold
	sequence := []netfixture.WireScenario{
		first,
		meetingM3Reply(t, 2, "unreachable future response before source identity is fixed"),
		meetingM3Reply(t, 3, "unreachable future response before source identity is fixed"),
		toolReply("request-meeting", "proposal_reject_call", map[string]any{"proposal_content": rejectText}),
		toolReply("list-meetings", "proposal_list_call", map[string]any{"status": "proposed", "limit": 10}),
		meetingM3Reply(t, 6, final), meetingM3Reply(t, 7, summary),
		meetingM3Reply(t, 8, "The restored proposal is now a normal conversation."), meetingM3Reply(t, 9, summary),
	}
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence})
	workAppCheck(t, "proposal-owned-model-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()
	var started mt.Receipt
	meetingProposalHTTP(t, v, "proposal-owner-start", http.MethodPost, "/meetings", guardID[struct{}](t).String(), map[string]any{"agents": agents[:1], "mode": "sequential", "content_nodes": []mt.InlineNode{{Type: "text", Text: "Propose two meetings for the owner to review and list pending meetings."}}}, &started)
	var currentSource mt.Detail
	meetingProposalHTTP(t, v, "proposal-current-source-version", http.MethodGet, "/meetings/"+started.MeetingID.String(), "", nil, &currentSource)
	// Only future provider replies change. The first actual response stays
	// held while its original source identity and version are fixed.
	workAppCheck(t, "proposal-add-agent-future-wire", w.network.UpdateFutureWire(v.ctx, scenario, 2, toolReply("add-agent-to-meeting", "proposal_add_call", map[string]any{"meeting_id": started.MeetingID, "expected_version": currentSource.Session.Version, "agent_id": agents[1]})))
	workAppCheck(t, "proposal-remove-agent-future-wire", w.network.UpdateFutureWire(v.ctx, scenario, 3, toolReply("remove-agent-from-meeting", "proposal_remove_call", map[string]any{"meeting_id": started.MeetingID, "expected_version": currentSource.Session.Version + 1, "agent_id": agents[1]})))
	workAppCheck(t, "proposal-release-original-response", w.network.Release(v.ctx, scenario))
	detail := meetingM3AwaitCompleted(t, v, started, rejected)
	if len(detail.Turns) != 1 || len(detail.Turns[0].Contributions) != 2 || detail.Turns[0].Contributions[1].ExecutionID == nil || detail.Turns[0].Contributions[1].Message == nil || detail.Turns[0].Contributions[1].Message.Content[0].Text != final {
		t.Fatal("original source execution did not publish its final reply")
	}
	removed := false
	for _, participant := range detail.Participants {
		if participant.Kind == "agent" && participant.SourceID == agents[1].String() {
			removed = !participant.Active
		}
	}
	if !removed {
		t.Fatal("ordinary Agent roster tools did not preserve removed participant history")
	}
	source := *detail.Turns[0].Contributions[1].ExecutionID
	open := meetingProposalAwaitInbox(t, v, "open", 2, rejected)
	proposals := map[string]mt.ProposalView{}
	for _, item := range open.Items {
		m, err := f.ParseID[mt.Meeting](item.SourceID)
		workAppCheck(t, "proposal-inbox-source-id", err)
		var proposal mt.ProposalView
		meetingProposalHTTP(t, v, "proposal-owner-source-read", http.MethodGet, "/meetings/"+m.String()+"/proposal", "", nil, &proposal)
		if proposal.Session.Status != "proposed" || proposal.Session.Version != 1 || proposal.Session.ProposalContent == nil || proposal.SourceExecutionID != source || proposal.Origin == nil || proposal.Origin.Kind != "meeting" || proposal.Origin.ID != started.MeetingID.String() {
			t.Fatal("proposal lost immutable original execution provenance")
		}
		if item.SourceType != "meeting" || item.ActionType != "meeting.review_proposal" || item.Dismissible || item.Status != "open" {
			t.Fatal("Inbox did not expose canonical review navigation")
		}
		proposals[*proposal.Session.ProposalContent] = proposal
	}
	if len(proposals) != 2 {
		t.Fatal("requests did not create two distinct proposals")
	}
	// Rebuild after loss of the read projection, without replaying original
	// events. Source rows remain untouched; the Owner controls this repair.
	_, err = v.admin.Exec(v.ctx, `DELETE FROM agenteam_humaninbox.items WHERE project_id=$1`, v.a.String())
	workAppCheck(t, "proposal-owned-projection-loss", err)
	meetingProposalRebuildHTTP(t, v)
	var rebuilt hc.Page
	meetingProposalHTTP(t, v, "proposal-rebuilt-open-list", http.MethodGet, "/inbox?status=open&limit=50", "", nil, &rebuilt)
	if len(rebuilt.Items) != 2 {
		t.Fatal("canonical rebuild lost proposals")
	}
	var browserReviews map[string]meetingProposalBrowserReview
	if review != nil {
		browserReviews = review(t, w, listener, []meetingProposalBrowserCase{{acceptText, true, proposals[acceptText]}, {rejectText, false, proposals[rejectText]}})
		if len(browserReviews) != 2 {
			t.Fatal("browser did not return two actual review receipts")
		}
	}
	for text, approve := range map[string]bool{acceptText: true, rejectText: false} {
		proposal, ok := proposals[text]
		if !ok {
			t.Fatal("original proposal missing")
		}
		path, key := "/meetings/"+proposal.Session.ID.String()+"/proposal", guardID[struct{}](t).String()
		body := workAppJSON(t, map[string]any{"expected_version": f.Version(1), "approve": approve})
		var receipt mt.Receipt
		if review == nil {
			meetingProposalHTTP(t, v, "proposal-owner-review", http.MethodPost, path+"/review", key, json.RawMessage(body), &receipt)
		} else {
			observed, found := browserReviews[proposal.Session.ID.String()]
			if !found || observed.Approve != approve || observed.Receipt.MeetingID != proposal.Session.ID || observed.Receipt.Version != 2 || observed.Receipt.TurnID != nil {
				t.Fatal("browser review receipt does not match its original proposal and choice")
			}
			if _, err := f.ParseIdempotencyKey(observed.Key); err != nil {
				t.Fatal("browser lost original review command key")
			}
			key, receipt = observed.Key, observed.Receipt
		}
		var replay struct {
			Status  string      `json:"status"`
			Receipt *mt.Receipt `json:"receipt"`
		}
		meetingProposalHTTP(t, v, "proposal-original-review-lookup", http.MethodPost, path+"/review/lookup", key, json.RawMessage(body), &replay)
		if replay.Status != "committed" || replay.Receipt == nil || !replay.Receipt.Replayed || replay.Receipt.MeetingID != receipt.MeetingID || replay.Receipt.Version != 2 || receipt.TurnID != nil {
			t.Fatal("review lookup lost the original no-turn receipt")
		}
		var repeated mt.Receipt
		meetingProposalHTTP(t, v, "proposal-repeat-review", http.MethodPost, path+"/review", key, json.RawMessage(body), &repeated)
		if !repeated.Replayed || repeated.Version != 2 {
			t.Fatal("repeated review changed canonical state")
		}
		var current mt.ProposalView
		meetingProposalHTTP(t, v, "proposal-current-owner-state", http.MethodGet, path, "", nil, &current)
		want := "archive"
		if approve {
			want = "active"
		}
		if current.Session.Status != want || current.SourceExecutionID != source || current.Origin == nil || current.Origin.ID != started.MeetingID.String() {
			t.Fatal("review changed provenance or introduced rejection state")
		}
	}
	resolved := meetingProposalAwaitInbox(t, v, "resolved", 2, rejected)
	if len(resolved.Items) != 2 {
		t.Fatal("resolved history lost")
	}
	meetingProposalRebuildHTTP(t, v)
	var empty hc.Page
	meetingProposalHTTP(t, v, "proposal-no-reopen", http.MethodGet, "/inbox?status=open&limit=50", "", nil, &empty)
	if len(empty.Items) != 0 {
		t.Fatal("rebuild reopened a handled proposal")
	}
	var requests, sourceMatches, normalTurns, events, operations, results int
	workAppCheck(t, "proposal-durable-original-facts", v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_meeting.proposal_sources WHERE execution_id=$1),(SELECT count(*) FROM agenteam_meeting.proposal_sources WHERE execution_id=$1 AND origin_kind='meeting' AND origin_id=$2),(SELECT count(*) FROM agenteam_meeting.turns WHERE meeting_id IN(SELECT meeting_id FROM agenteam_meeting.proposal_sources WHERE execution_id=$1)),(SELECT count(*) FROM agenteam_meeting.lifecycle_events WHERE meeting_id IN(SELECT meeting_id FROM agenteam_meeting.proposal_sources WHERE execution_id=$1)),(SELECT count(*) FROM agenteam_meeting.agent_commands WHERE execution_id=$1),(SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1 AND phase='succeeded' AND outcome='success')`, source.String(), started.MeetingID.String()).Scan(&requests, &sourceMatches, &normalTurns, &events, &operations, &results))
	if requests != 2 || sourceMatches != 2 || normalTurns != 0 || events != 4 || operations != 5 || results != 5 {
		t.Fatal("proposal count, provenance, operation receipt or zero-Turn invariant failed")
	}
	// Rejection is archival, not a terminal rejection state. Restoring the
	// reviewed proposal must preserve its Human participant and allow Send.
	archived := proposals[rejectText]
	var restored, continued mt.Receipt
	meetingProposalHTTP(t, v, "proposal-restore-archived-session", http.MethodPost, "/meetings/"+archived.Session.ID.String()+"/controls", guardID[struct{}](t).String(), map[string]any{"expected_version": f.Version(2), "action": "unarchive"}, &restored)
	meetingProposalHTTP(t, v, "proposal-send-after-reject-and-restore", http.MethodPost, "/meetings/"+archived.Session.ID.String()+"/turns", guardID[struct{}](t).String(), map[string]any{"expected_version": restored.Version, "mode": "sequential", "interrupt": false, "content_nodes": []mt.InlineNode{{Type: "text", Text: "Continue the restored discussion."}}}, &continued)
	continuedDetail := meetingM3AwaitCompleted(t, v, continued, rejected)
	if len(continuedDetail.Turns) != 1 || len(continuedDetail.Turns[0].Contributions) != 2 || continuedDetail.Turns[0].Contributions[0].Kind != "user" {
		t.Fatal("restored proposal did not retain the reviewed Owner participant")
	}
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "proposal-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "proposal-original-app-return")
	workAppCheck(t, "proposal-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("proposal App owners failed to join")
	}
	meetingProposalGuardReleased(t, v.workAppFixture)
	state := w.joinedPeer(t, scenario, 9, 9)
	var successor struct {
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	workAppCheck(t, "proposal-original-model-successor", json.Unmarshal([]byte(state.Requests[5].Body), &successor))
	seen := map[string]mt.AgentResult{}
	for _, message := range successor.Messages {
		if message.Role == "tool" {
			var parts []struct{ Type, Text string }
			workAppCheck(t, "proposal-tool-result-text", json.Unmarshal(message.Content, &parts))
			if len(parts) != 1 || parts[0].Type != "text" {
				t.Fatal("original successor lacked the single tool result text part")
			}
			if _, duplicate := seen[message.ToolCallID]; duplicate {
				t.Fatal("original successor duplicated a tool result")
			}
			var result mt.AgentResult
			workAppCheck(t, "proposal-tool-result-value", json.Unmarshal([]byte(parts[0].Text), &result))
			seen[message.ToolCallID] = result
		}
	}
	if len(seen) != 5 {
		t.Fatal("original successor lacked the exact five tool results")
	}
	for call, text := range map[string]string{"proposal_accept_call": acceptText, "proposal_reject_call": rejectText} {
		result, ok := seen[call]
		if !ok || result.Receipt == nil || result.Receipt.MeetingID != proposals[text].Session.ID || result.Receipt.Version != 1 {
			t.Fatal("original successor lacked its exact proposal receipt")
		}
	}
	for _, call := range []string{"proposal_add_call", "proposal_remove_call"} {
		if result, ok := seen[call]; !ok || result.Receipt == nil || result.Receipt.MeetingID != started.MeetingID {
			t.Fatal("original successor lacked roster tool receipt")
		}
	}
	if result, ok := seen["proposal_list_call"]; !ok || len(result.Meetings) != 2 {
		t.Fatal("original successor lacked proposed list result")
	}
	t.Log("M3_MEETING_PROPOSAL original_tool_operation_and_source=true owner_http_inbox_review_lookup=true canonical_rebuild=true proposal_turns=0 original_app_wait=true external_model=false")
}

// App Wait, owner Joined and the original flock remain prerequisite facts.
// PostgreSQL observes client socket closure independently of the client Close.
func meetingProposalGuardReleased(t *testing.T, v *workAppFixture) {
	t.Helper()
	workAppCheck(t, "proposal-original-guard-release", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "proposal-guard-observer-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
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
			t.Logf("PROPOSAL_EXIT_FACTS stopped=%t connections=%d observations=%d observation_failed=true", stopped, connections, observations)
			workAppCheck(t, "proposal-original-exit-observation", err)
		}
		observations++
		if observations == 1 || stopped && connections == 0 {
			t.Logf("PROPOSAL_EXIT_FACTS stopped=%t connections=%d observations=%d", stopped, connections, observations)
		}
		if stopped && connections == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("original Proposal App exit facts unconfirmed: stopped=%t connections=%d observations=%d", stopped, connections, observations)
		case <-tick.C:
		}
	}
}

func meetingProposalAwaitInbox(t *testing.T, v *executionSchedulerApp, status string, count int, rejected <-chan meetingM3RejectedTransaction) hc.Page {
	t.Helper()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	var lastProjection string
	var nextObservation time.Time
	for {
		var page hc.Page
		meetingProposalHTTP(t, v, "proposal-owner-inbox-list", http.MethodGet, "/inbox?status="+status+"&limit=50", "", nil, &page)
		if len(page.Items) == count {
			return page
		}
		if time.Now().After(nextObservation) {
			meetingProposalProjectionProgress(t, v, &lastProjection)
			nextObservation = time.Now().Add(time.Second)
		}
		select {
		case failure := <-rejected:
			t.Fatalf("proposal original transaction rejected owner=%s code=%s", failure.owner, failure.code.Safe())
		case <-v.ctx.Done():
			t.Fatal("proposal inbox exceeded original App budget")
		case <-v.root.done:
			meetingM3SafeError(t, "proposal-early-app-return", v.root.err)
			t.Fatal("original App returned before projection")
		case <-tick.C:
		}
	}
}

// Observe only closed Outbox phases/reasons and counts in the original owned
// database. This does not dispatch, retry, rebuild or replace the projection.
func meetingProposalProjectionProgress(t *testing.T, v *executionSchedulerApp, last *string) {
	t.Helper()
	var raw []byte
	err := v.admin.QueryRow(v.ctx, `SELECT jsonb_build_object('events',(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='meeting'),'handlers',(SELECT count(*) FROM agenteam_outbox.handlers WHERE id='humaninbox.meeting_proposal'),'deliveries',COALESCE((SELECT jsonb_agg(jsonb_build_object('phase',phase,'reason',reason,'count',n) ORDER BY phase,reason) FROM (SELECT phase,COALESCE(safe_reason,'none') AS reason,count(*) AS n FROM agenteam_outbox.deliveries WHERE project_id=$1 AND handler_id='humaninbox.meeting_proposal' GROUP BY phase,safe_reason) d),'[]'::jsonb))`, v.a.String()).Scan(&raw)
	if err != nil {
		meetingM3SafeError(t, "proposal-projection-observation", err)
		return
	}
	if string(raw) != *last {
		*last = string(raw)
		t.Logf("M3_MEETING_PROJECTION %s", raw)
	}
}

// The original App HTTP listener, live Cookie/CSRF and request identity remain
// the sole Owner entrance. Domain reads elsewhere only observe completion.
func meetingProposalHTTP(t *testing.T, v *executionSchedulerApp, stage, method, suffix, key string, body, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, "/api/v1/projects/"+v.a.String()+suffix, raw, key)
	workAppCheck(t, stage+"-http-return", err)
	if response.status != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("Proposal HTTP %s status=%d code=%s", stage, response.status, problem.Code.Safe())
	}
	workAppCheck(t, stage+"-http-body", json.Unmarshal(response.body, out))
}
func meetingProposalRebuildHTTP(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	var out struct {
		Rebuilt bool `json:"rebuilt"`
	}
	meetingProposalHTTP(t, v, "proposal-owner-rebuild", http.MethodPost, "/inbox/rebuild", "", struct{}{}, &out)
	if !out.Rebuilt {
		t.Fatal("original Owner HTTP rebuild did not complete")
	}
}
