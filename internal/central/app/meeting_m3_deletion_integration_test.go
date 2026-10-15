//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// The Owner uses the public HTTP commands throughout. A completed first Turn
// preserves historical executions; the held fourth model response gives the
// same deletion an actual current owner and a not-yet-launched participant.
func TestMeetingM3AppDeletionRetiresOriginalOwners(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	rejected := make(chan meetingM3RejectedTransaction, 1)
	w, _ := newM1WorkflowWithOptions(t, func(cause f.TransactionCause, result f.CommitResult) {
		if failure := result.Fault(); result.State() == f.NotCommitted && failure != nil && failure.Code == f.InternalError {
			meetingM3SafeError(t, "delete-original-transaction", failure)
			select {
			case rejected <- meetingM3RejectedTransaction{cause.Details().Owner, failure.Code}:
			default:
			}
		}
	}, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	summary := string(workAppJSON(t, map[string]string{"title": "Original deletion discussion", "goals": "Retain the original execution evidence", "decisions": "", "unresolved": "", "facts": "Both participants contributed"}))
	hold := 0
	fourth := meetingM3Reply(t, 4, "This cancelled response must never become a Meeting message.")
	fourth.HoldAfter = &hold
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		meetingM3Reply(t, 1, "A supplied the original contribution."), meetingM3Reply(t, 2, "B supplied the original contribution."), meetingM3Reply(t, 3, summary), fourth,
	}})
	workAppCheck(t, "delete-owned-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()
	var first mt.Receipt
	meetingDeletionHTTP(t, v, "start", http.MethodPost, "/meetings", guardID[struct{}](t).String(), map[string]any{
		"agents": agents, "mode": "sequential", "content_nodes": []mt.InlineNode{{Type: "text", Text: "Finish the original discussion before a later deletion."}},
	}, &first)
	if first.TurnID == nil {
		t.Fatal("Owner Start omitted its original Turn")
	}
	initial := meetingM3AwaitCompleted(t, v, first, rejected)
	if initial.Session.Summary == nil || initial.Session.Summary.Version != 1 || len(initial.Turns) != 1 || len(initial.Turns[0].Contributions) != 3 {
		t.Fatal("the historical Turn did not finish before deletion")
	}
	meetingPath := "/meetings/" + first.MeetingID.String()
	var next mt.Receipt
	meetingDeletionHTTP(t, v, "send", http.MethodPost, meetingPath+"/turns", guardID[struct{}](t).String(), map[string]any{
		"expected_version": initial.Session.Version, "mode": "sequential", "interrupt": false, "content_nodes": []mt.InlineNode{{Type: "text", Text: "Start one current contribution that the Owner will cancel by deleting this meeting."}},
	}, &next)
	if next.TurnID == nil {
		t.Fatal("Owner Send omitted its original Turn")
	}
	await := func(stage string, observe func(context.Context) (bool, error)) {
		t.Helper()
		ctx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
		defer cancel()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			done, err := observe(ctx)
			workAppCheck(t, "delete-"+stage, err)
			if done {
				return
			}
			select {
			case failure := <-rejected:
				t.Fatalf("delete transaction rejected owner=%s code=%s", meetingM3SafeIdentifier(failure.owner), failure.code.Safe())
			case <-v.root.done:
				meetingM3SafeError(t, "delete-early-app-return", v.root.err)
				t.Fatal("original App exited before deletion settled")
			case <-ctx.Done():
				t.Fatal("original deletion did not settle", stage)
			case <-ticker.C:
			}
		}
	}
	await("current-owner-at-provider", func(ctx context.Context) (bool, error) {
		state, err := w.network.State(ctx, scenario)
		if err != nil {
			return false, err
		}
		if len(state.Requests) > 4 {
			t.Fatal("unreleased current Turn launched extra model calls")
		}
		return len(state.Requests) == 4 && state.ActiveHandlers == 1, nil
	})
	var current mt.Detail
	meetingDeletionHTTP(t, v, "current-detail", http.MethodGet, meetingPath, "", nil, &current)
	if len(current.Turns) != 2 || current.Turns[1].ID != *next.TurnID || len(current.Turns[1].Contributions) != 3 || current.Turns[1].Contributions[1].ExecutionID == nil || current.Turns[1].Contributions[2].ExecutionID != nil {
		t.Fatal("deletion did not capture the actual current A and undispatched B")
	}
	originalExecution := *current.Turns[1].Contributions[1].ExecutionID
	key := guardID[struct{}](t).String()
	body := map[string]any{"expected_version": current.Session.Version}
	response, err := v.request(v.ctx, http.MethodPost, "/api/v1/projects/"+v.a.String()+meetingPath+"/deletion", workAppJSON(t, body), key)
	workAppCheck(t, "delete-owner-command-return", err)
	var accepted mt.DeleteReceipt
	if response.status == http.StatusOK {
		workAppCheck(t, "delete-owner-command-body", json.Unmarshal(response.body, &accepted))
		workAppCheck(t, "delete-owner-command-receipt", accepted.Validate())
	} else {
		var problem struct {
			Code f.Code `json:"code"`
		}
		if json.Unmarshal(response.body, &problem) != nil || problem.Code != f.CommitUnknown {
			t.Fatalf("delete Owner command status=%d code=%s", response.status, problem.Code.Safe())
		}
		// Unknown is observed using this exact intent. No second Delete call
		// is used to discover whether the first command committed.
	}
	var deleted mt.DeleteReceipt
	await("original-lookup-completed", func(ctx context.Context) (bool, error) {
		lookup, err := v.request(ctx, http.MethodPost, "/api/v1/projects/"+v.a.String()+meetingPath+"/deletion/lookup", workAppJSON(t, body), key)
		if err != nil {
			return false, err
		}
		var value struct {
			Status  string            `json:"status"`
			Receipt *mt.DeleteReceipt `json:"receipt"`
		}
		if lookup.status != http.StatusOK || json.Unmarshal(lookup.body, &value) != nil {
			t.Fatal("original delete lookup failed", lookup.status)
		}
		if value.Status == "not_found" && value.Receipt == nil {
			return false, nil
		}
		if value.Status != "committed" || value.Receipt == nil || value.Receipt.Validate() != nil || value.Receipt.MeetingID != first.MeetingID || value.Receipt.AcceptedVersion != current.Session.Version+1 || !value.Receipt.Replayed {
			t.Fatal("delete lookup lost its original receipt")
		}
		if accepted.DeletionID.Validate() == nil && accepted.DeletionID != value.Receipt.DeletionID {
			t.Fatal("lookup substituted a different deletion")
		}
		if value.Receipt.Phase != mt.DeletionCompleted {
			return false, nil
		}
		deleted = *value.Receipt
		return true, nil
	})
	var remaining, executions, succeeded, cancelled, calls, retired, released, acceptedAudits, completedAudits, events int
	var sameTransaction, exactActor, exactCurrent bool
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_meeting.sessions WHERE id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.participants WHERE meeting_id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.turns WHERE meeting_id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.contributions WHERE meeting_id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.messages WHERE meeting_id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.inputs WHERE meeting_id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.launch_intents WHERE meeting_id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.summary_generations WHERE meeting_id=$2::text)+
 (SELECT count(*) FROM agenteam_meeting.summary_model_calls WHERE meeting_id=$2::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text AND status='succeeded'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request#>>'{trigger,meeting_id}'=$2::text AND status='cancelled'),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1::text AND l.released),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1::text AND operation_id::text=$3::text AND action='meeting.delete.accepted'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1::text AND operation_id::text=$3::text AND action='meeting.delete.completed'),
 (SELECT count(*) FROM agenteam_meeting.lifecycle_events WHERE meeting_id=$2::text AND event_type='meeting.deleted'),
 EXISTS(SELECT 1 FROM agenteam_meeting.deletion_requests d JOIN agenteam_meeting.lifecycle_events e ON e.meeting_id=d.meeting_id AND e.event_type='meeting.deleted' JOIN agenteam_outbox.events o ON o.id::text=e.event_id JOIN agenteam_audit.audit_records a ON a.operation_id::text=d.id AND a.action='meeting.delete.completed' WHERE d.id=$3::text AND d.xmin=e.xmin AND d.xmin=o.xmin AND d.xmin=a.xmin AND d.deleted_records=(a.metadata->>'deleted_rows')::bigint AND d.deleted_records>0),
 EXISTS(SELECT 1 FROM agenteam_audit.audit_records a JOIN agenteam_meeting.commands c ON c.project_id=a.project_id::text AND c.idempotency_key=$5::text WHERE a.operation_id::text=$3::text AND a.action='meeting.delete.accepted' AND a.actor_kind='human' AND a.user_id::text=$4::text AND a.xmin=c.xmin AND a.metadata->>'version'=$6::text)
 AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE operation_id::text=$3::text AND action='meeting.delete.completed' AND actor_kind='service' AND service_name='meeting-runtime' AND service_cause=$3::text AND metadata->>'initiator_id'=$4::text AND metadata->>'version'=$6::text),
 EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE id=$7::text AND status='cancelled' AND launch_request#>>'{trigger,meeting_id}'=$2::text)`, v.a.String(), first.MeetingID.String(), deleted.DeletionID.String(), v.actor.Details().UserID, key, deleted.AcceptedVersion.String(), originalExecution.String()).Scan(&remaining, &executions, &succeeded, &cancelled, &calls, &retired, &released, &acceptedAudits, &completedAudits, &events, &sameTransaction, &exactActor, &exactCurrent)
	workAppCheck(t, "delete-original-durable-postimage", err)
	if remaining != 0 || executions != 3 || succeeded != 2 || cancelled != 1 || calls != 4 || retired != 4 || released != 4 || acceptedAudits != 1 || completedAudits != 1 || events != 1 || !sameTransaction || !exactActor || !exactCurrent {
		t.Fatalf("delete original facts domain=%d executions=%d succeeded=%d cancelled=%d calls=%d retired=%d released=%d accepted=%d completed=%d events=%d same_tx=%t actors=%t current=%t", remaining, executions, succeeded, cancelled, calls, retired, released, acceptedAudits, completedAudits, events, sameTransaction, exactActor, exactCurrent)
	}
	read, err := v.request(v.ctx, http.MethodGet, "/api/v1/projects/"+v.a.String()+meetingPath, nil, "")
	workAppCheck(t, "delete-owner-read-after-purge", err)
	if read.status != http.StatusNotFound {
		t.Fatal("deleted Meeting remained readable", read.status)
	}
	late, err := v.request(v.ctx, http.MethodPost, "/api/v1/projects/"+v.a.String()+meetingPath+"/turns", workAppJSON(t, map[string]any{"expected_version": deleted.AcceptedVersion, "mode": "sequential", "interrupt": false, "content_nodes": []mt.InlineNode{{Type: "text", Text: "This late command must not reopen the meeting."}}}), guardID[struct{}](t).String())
	workAppCheck(t, "delete-owner-late-send", err)
	if late.status != http.StatusNotFound {
		t.Fatal("late Send reopened the deleted Meeting", late.status)
	}
	var replay mt.DeleteReceipt
	meetingDeletionHTTP(t, v, "replay-after-purge", http.MethodPost, meetingPath+"/deletion", key, body, &replay)
	if replay.DeletionID != deleted.DeletionID || replay.Phase != mt.DeletionCompleted || !replay.Replayed {
		t.Fatal("deleted original receipt was not replayable")
	}
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "delete-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "delete-original-app-wait")
	workAppCheck(t, "delete-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.meetingRuntime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("deletion App returned before all original owners joined")
	}
	w.joinedPeer(t, scenario, 4, 4)
	meetingDeletionGuardReleased(t, v.workAppFixture)
	t.Log("M3_MEETING_DELETE original_owner_http=true original_lookup=true current_cancelled=true pending_never_dispatched=true domain_purged=true external_executions_retained=true audit_event_same_tx=true original_app_joined=true external_model=false")
}

// App Wait/Joined and the original flock are prerequisite gates. Observing a
// closed PostgreSQL client's server backend exit is a separate bounded read;
// it never terminates a connection or substitutes for the original App Wait.
func meetingDeletionGuardReleased(t *testing.T, v *workAppFixture) {
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

func meetingDeletionHTTP(t *testing.T, v *executionSchedulerApp, stage, method, suffix, key string, body, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, "/api/v1/projects/"+v.a.String()+suffix, raw, key)
	workAppCheck(t, "delete-"+stage+"-http-return", err)
	if response.status != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("delete HTTP %s status=%d code=%s", stage, response.status, problem.Code.Safe())
	}
	workAppCheck(t, "delete-"+stage+"-http-body", json.Unmarshal(response.body, out))
}
