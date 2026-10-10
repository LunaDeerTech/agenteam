//go:build integration

package account_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Keep the accepted fixture and its actual browser/Node/HTTP cleanup ownership.
// Independent assertions take their baseline before the browser can act.
func runIndependentWorkWeb(t *testing.T, mode string, required ...string) (*projectWorkPlanningWebFixture, map[string]map[string]any, map[string]any) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("independent Work browser exceeded the original 120s including cleanup")
		}
	})
	f := newProjectWorkPlanningWebFixture(t, ctx, mode)
	before := make(map[string]map[string]any, len(f.seeds))
	for key, seed := range f.seeds {
		before[key] = f.facts(ctx, seed.ProjectID)
	}
	result := f.browser(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatal("independent browser boundary was not observed", name)
		}
	}
	return f, before, result
}

func TestIndependentProjectWorkPlanningWebRecovery(t *testing.T) {
	f, baseline, _ := runIndependentWorkWeb(t, "independent-recovery", "three_domains", "canceled_discard", "current_history", "archive_replay", "read_only_snapshots")
	f.guard.Lock()
	lost := make(map[string]projectWorkPlanningWebObservation, len(f.lost))
	for domain, original := range f.lost {
		lost[domain] = original
	}
	completeCuts := !f.cutInvalid && f.dropped == 3 && f.replayed == 3 && len(lost) == 3
	f.guard.Unlock()
	if !completeCuts {
		t.Fatal("independent recovery requires three actual original cuts and three same-key replays")
	}
	observed := f.observations()
	for _, domain := range []string{"blocker", "task", "structure"} {
		original, ok := lost[domain]
		seed := f.seeds[domain]
		if !ok || original.ProjectID != seed.ProjectID || original.Key == "" || original.Status != http.StatusOK || original.Cut.Written != 1 || !original.Cut.WriteOK || !original.Cut.FlushOK || !original.Cut.HijackOK || !original.Cut.CloseOK {
			t.Fatal("independent original lacked its exact committed response and completed physical cut", domain)
		}
		var wire, receipt, request map[string]any
		if json.Unmarshal(original.Response, &wire) != nil || json.Unmarshal(original.StoredReceipt, &receipt) != nil || json.Unmarshal(original.Body, &request) != nil || !reflect.DeepEqual(wire, receipt) {
			t.Fatal("independent original response differs from its persisted receipt", domain)
		}
		var count int
		var operation string
		var durable []byte
		table := workCommandTable(domain)
		if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM `+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, seed.ProjectID, original.Command, original.Key).Scan(&count); err != nil || count != 1 {
			t.Fatal("independent original did not retain exactly one completed operation", domain)
		}
		if err := f.store.QueryRow(f.ctx, `SELECT id::text,receipt FROM `+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, seed.ProjectID, original.Command, original.Key).Scan(&operation, &durable); err != nil {
			t.Fatal("independent persisted original unavailable", domain)
		}
		var finalReceipt map[string]any
		if json.Unmarshal(durable, &finalReceipt) != nil || !reflect.DeepEqual(finalReceipt, receipt) {
			t.Fatal("later changes or replay rewrote the independent historical receipt", domain)
		}
		lookups, writes := 0, 0
		for _, call := range observed {
			if call.Key != original.Key {
				continue
			}
			if call.ProjectID != original.ProjectID || call.TargetID != original.TargetID || call.Domain != domain || call.Command != original.Command || call.CSRF != original.CSRF || call.Status != http.StatusOK {
				t.Fatal("independent original changed identity, target or outcome", domain)
			}
			var body, response map[string]any
			if json.Unmarshal(call.Body, &body) != nil || json.Unmarshal(call.Response, &response) != nil {
				t.Fatal("independent original lost its complete private wire evidence", domain)
			}
			if strings.HasSuffix(call.RawPath, "/lookup") {
				state, historical := "status", "receipt"
				if domain == "structure" {
					state, historical = "state", "result"
				}
				if call.Method != http.MethodPost || body["command"] != original.Command || !reflect.DeepEqual(body["request"], request["request"]) || !reflect.DeepEqual(body["expected_version"], request["expected_version"]) || response[state] != "committed" || !reflect.DeepEqual(response[historical], receipt) {
					t.Fatal("independent lookup substituted current data or changed the original meaning", domain)
				}
				lookups++
			} else {
				if call.Method != original.Method || call.RawPath != original.RawPath || !reflect.DeepEqual(body, request) || !reflect.DeepEqual(response, receipt) {
					t.Fatal("independent replay changed original bytes or historical outcome", domain)
				}
				writes++
			}
		}
		if lookups != 2 || writes != 2 {
			t.Fatal("independent recovery emitted a missing or extra original attempt", domain)
		}

		objectName, currentTable, currentField := "task", "tasks", "plan"
		if domain == "structure" {
			objectName, currentTable, currentField = "milestone", "milestones", "title"
		}
		object := httpObject(t, receipt, objectName)
		if httpString(t, object, "id") != original.TargetID {
			t.Fatal("independent receipt target mismatch", domain)
		}
		var currentText, currentVersion, lifecycle string
		if err := f.store.QueryRow(f.ctx, `SELECT `+currentField+`,version::text FROM agenteam_work.`+currentTable+` WHERE project_id=$1 AND id=$2`, seed.ProjectID, original.TargetID).Scan(&currentText, &currentVersion); err != nil || currentText != "独验后继当前值" || currentVersion == object["version"] {
			t.Fatal("independent current value did not remain separate from historical receipt", domain)
		}
		if err := f.store.QueryRow(f.ctx, `SELECT lifecycle FROM agenteam_project.projects WHERE id=$1`, seed.ProjectID).Scan(&lifecycle); err != nil || lifecycle != "archived" {
			t.Fatal("independent replay did not cross the owned archived input", domain)
		}
		eventID := ""
		if domain == "structure" {
			eventID = httpString(t, receipt, "event_id")
		} else {
			events, ok := receipt["event_ids"].([]any)
			if !ok || len(events) != 1 {
				t.Fatal("independent original event shape invalid", domain)
			}
			eventID, ok = events[0].(string)
			if !ok || eventID == "" {
				t.Fatal("independent original event identity missing", domain)
			}
			operationColumn := "operation_id"
			if domain == "blocker" {
				operationColumn = "blocker_operation_id"
			}
			if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM agenteam_work.task_events WHERE id=$1 AND project_id=$2 AND task_id=$3 AND `+operationColumn+`=$4 AND task_version=$5`, receipt["task_event_id"], seed.ProjectID, original.TargetID, operation, object["version"]).Scan(&count); err != nil || count != 1 {
				t.Fatal("independent history lost the original operation/version", domain)
			}
		}
		if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1 AND producer='work' AND project_id=$2 AND aggregate_id=$3 AND aggregate_version=$4`, eventID, seed.ProjectID, original.TargetID, object["version"]).Scan(&count); err != nil || count != 1 {
			t.Fatal("independent original Outbox identity/version mismatch", domain)
		}
		if domain == "blocker" {
			blocker := httpObject(t, receipt, "blocker")
			var resolved bool
			if blocker["resolved_at"] != nil || f.store.QueryRow(f.ctx, `SELECT resolved_at IS NOT NULL AND resolution_comment='独验后继解除' FROM agenteam_work.task_blockers WHERE project_id=$1 AND id=$2`, seed.ProjectID, blocker["id"]).Scan(&resolved) != nil || !resolved {
				t.Fatal("independent blocker history was confused with its later resolution")
			}
		}
		// Exact deltas reject extra facts even if every observed response was OK.
		delta := map[string]int{"structure_commands": 0, "task_commands": 0, "blocker_commands": 0, "history": 0, "outbox": 2}
		switch domain {
		case "structure":
			delta["structure_commands"] = 2
		case "task":
			delta["task_commands"], delta["history"] = 2, 2
		case "blocker":
			delta["task_commands"], delta["blocker_commands"], delta["history"], delta["outbox"] = 1, 2, 3, 3
		}
		for name, actual := range f.facts(f.ctx, seed.ProjectID) {
			if actual.(int) != baseline[domain][name].(int)+delta[name] {
				t.Fatal("independent recovery created extra or missing durable facts", domain, name)
			}
		}
	}
}

func TestIndependentProjectWorkPlanningWebAuthority(t *testing.T) {
	f, baseline, result := runIndependentWorkWeb(t, "independent-authority", "identity", "old_tail", "project_isolation", "installed_guards", "owner_gate")
	oldSession, oldOK := result["revoked_session"].(string)
	newSession, newOK := result["renewed_session"].(string)
	for _, raw := range []string{oldSession, newSession} {
		if _, err := foundation.ParseID[identity.Session](raw); err != nil {
			t.Fatal("independent browser Session identity malformed")
		}
	}
	if !oldOK || !newOK || oldSession == newSession || oldSession == f.ownerActor.Details().SessionID || newSession == f.ownerActor.Details().SessionID {
		t.Fatal("independent browser did not replace its own genuine Session")
	}
	var oldRevoked, newLive bool
	if err := f.store.QueryRow(f.ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='logout' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, oldSession, f.owner.UserID).Scan(&oldRevoked); err != nil || !oldRevoked {
		t.Fatal("old browser Session was not actually revoked in this Store")
	}
	if err := f.store.QueryRow(f.ctx, `SELECT revoked_at IS NULL AND absolute_expires_at>clock_timestamp() AND last_activity_at+idle_seconds*interval '1 second'>clock_timestamp() FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, newSession, f.owner.UserID).Scan(&newLive); err != nil || !newLive {
		t.Fatal("replacement browser Session is not a distinct current real Session")
	}
	f.guard.Lock()
	joined := f.hold != nil && f.hold.path == projectOwnerWebPath+"/"+f.seeds["main"].ProjectID+"/tasks/"+f.seeds["main"].TaskID && f.hold.started && f.hold.finished && f.readCanceled
	f.guard.Unlock()
	if !joined {
		t.Fatal("old exact Task consumer failed to actually cancel and join")
	}
	seenProjects := map[string]bool{}
	for _, call := range f.observations() {
		if call.Method != http.MethodGet || call.ProjectID != f.seeds["main"].ProjectID && call.ProjectID != f.seeds["duplicate"].ProjectID {
			t.Fatal("independent authority browser emitted a write or crossed its two owned Project scopes")
		}
		seenProjects[call.ProjectID] = true
	}
	for key, seed := range f.seeds {
		if !seenProjects[seed.ProjectID] || !reflect.DeepEqual(f.facts(f.ctx, seed.ProjectID), baseline[key]) {
			t.Fatal("independent authority changed Work facts or never observed the selected Project", key)
		}
		var providers int
		if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM agenteam_model.providers WHERE project_id=$1`, seed.ProjectID).Scan(&providers); err != nil || providers != 0 {
			t.Fatal("canceled independent Model draft created a Provider")
		}
		initial, ok := f.initial[key].(map[string]any)
		if !ok {
			t.Fatal("independent Owner baseline is missing its actual Project response")
		}
		var description string
		if err := f.store.QueryRow(f.ctx, `SELECT description FROM agenteam_project.projects WHERE id=$1`, seed.ProjectID).Scan(&description); err != nil || description != initial["description"] {
			t.Fatal("canceled independent Owner draft changed Project data")
		}
	}
}
