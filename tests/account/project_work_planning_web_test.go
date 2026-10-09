//go:build integration

package account_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func runProjectWorkPlanningWeb(t *testing.T, mode string, required ...string) *projectWorkPlanningWebFixture {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Work browser top exceeded original 120-second budget including cleanup")
		}
	})
	f := newProjectWorkPlanningWebFixture(t, ctx, mode)
	result := f.browser(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Work browser evidence incomplete: %s", name)
		}
	}
	// Successful browser commands must have actual same-key committed facts.
	// A result flag or a response prefix alone never satisfies this check.
	for _, observed := range f.observations() {
		if observed.Method == "GET" || strings.HasSuffix(observed.RawPath, "/lookup") || observed.Status != 200 {
			continue
		}
		table := workCommandTable(observed.Domain)
		if table == "" {
			t.Fatal("observed Work mutation has no closed command domain")
		}
		var count int
		if err := f.store.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, observed.ProjectID, observed.Command, observed.Key).Scan(&count); err != nil || count != 1 {
			t.Fatal("observed successful Work mutation lacks its unique actual completed command")
		}
	}
	return f
}
func TestAccountProjectWorkPlanningWebReadAndNavigation(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "read", "pagination", "deep_links", "refresh", "raw_route", "parent", "permissions", "cursor", "filters", "initialization", "wrong_parent")
	if f.readCursorMutation == nil {
		t.Fatal("read cursor stimulus was not actually observed")
	}
	externalWrites := 0
	for _, observed := range f.observations() {
		if observed.Method != "GET" {
			if !reflect.DeepEqual(observed, *f.readCursorMutation) {
				t.Fatal("read-only browser emitted a Work command")
			}
			externalWrites++
		}
		if observed.ProjectID == f.ids["pending"] || observed.ProjectID == f.ids["deleting"] {
			t.Fatal("unqualified Project published a Work request")
		}
	}
	if externalWrites != 1 {
		t.Fatal("read cursor stimulus did not remain a single exact external mutation")
	}
	var pending, deleting bool
	if err := f.store.QueryRow(f.ctx, `SELECT initialized_at IS NULL AND lifecycle='active' FROM agenteam_project.projects WHERE id=$1`, f.ids["pending"]).Scan(&pending); err != nil || !pending {
		t.Fatal("pending initialization gate lost its actual input")
	}
	if err := f.store.QueryRow(f.ctx, `SELECT lifecycle='deleting' FROM agenteam_project.projects WHERE id=$1`, f.ids["deleting"]).Scan(&deleting); err != nil || !deleting {
		t.Fatal("real BeginDelete gate lost its actual input")
	}
	for _, key := range []string{"pending", "deleting"} {
		for _, count := range f.facts(f.ctx, f.ids[key]) {
			if count.(int) != 0 {
				t.Fatal("denied Project acquired Work command/history/outbox facts")
			}
		}
	}
}
func TestAccountProjectWorkPlanningWebStructureAndTasks(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "planning", "structure", "task", "plan", "ordering", "conflict", "current_receipt")
	assertProjectWorkPlanningFacts(t, f)

}

// Cross-check the browser's actual original keys and receipts against persisted
// commands, typed history, Outbox and current objects. Counters alone cannot
// substitute for these joins; final deltas additionally reject extra commands.
func assertProjectWorkPlanningFacts(t *testing.T, f *projectWorkPlanningWebFixture) {
	t.Helper()
	ctx := f.ctx
	counts := map[string]int{}
	latest := map[string]map[string]any{}
	seenKeys := map[string]bool{}
	conflict := 0
	reads := map[string]bool{}
	for _, o := range f.observations() {
		if o.Method == "GET" && o.Status == 200 {
			parts := strings.Split(strings.Trim(o.RawPath, "/"), "/")
			if o.Domain == "blocker" {
				reads["blockers"] = true
			} else if len(parts) == 5 {
				reads[parts[4]+"-list"] = true
			} else if len(parts) == 6 {
				reads[parts[4]+"-get"] = true
			}
		}
		if o.Method == "GET" || strings.HasSuffix(o.RawPath, "/lookup") {
			continue
		}
		table := workCommandTable(o.Domain)
		if table == "" || o.Domain == "blocker" || o.ProjectID != f.seeds["main"].ProjectID {
			t.Fatal("unexpected planning mutation domain")
		}
		if o.Status != 200 {
			if o.Status != 409 || o.Command != "work.task.update" {
				t.Fatal("unexpected planning rejection")
			}
			var n int
			if err := f.store.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, o.ProjectID, o.Command, o.Key).Scan(&n); err != nil || n != 0 {
				t.Fatal("version conflict left a command fact")
			}
			conflict++
			continue
		}
		if seenKeys[o.Key] {
			t.Fatal("planning emitted a duplicate command attempt")
		}
		seenKeys[o.Key] = true
		counts[o.Command]++
		var stored []byte
		var commandID string
		if err := f.store.QueryRow(ctx, `SELECT id,receipt FROM `+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, o.ProjectID, o.Command, o.Key).Scan(&commandID, &stored); err != nil {
			t.Fatal("planning durable receipt missing")
		}
		var receipt, response map[string]any
		if json.Unmarshal(stored, &receipt) != nil || json.Unmarshal(o.Response, &response) != nil || !reflect.DeepEqual(receipt, response) || receipt["changed"] != true {
			t.Fatal("planning same-body changed receipt mismatch")
		}
		kind := strings.Split(o.Command, ".")[1]
		object, ok := receipt[kind].(map[string]any)
		if !ok || object["id"] != o.TargetID || object["project_id"] != o.ProjectID {
			t.Fatal("planning receipt target mismatch")
		}
		latest[kind] = object
		eventID, _ := receipt["event_id"].(string)
		if kind == "task" {
			events, ok := receipt["event_ids"].([]any)
			if !ok || len(events) != 1 {
				t.Fatal("planning receipt Outbox shape")
			}
			eventID, _ = events[0].(string)
			var n int
			if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_events WHERE id=$1 AND project_id=$2 AND task_id=$3 AND operation_id=$4 AND task_version=$5`, receipt["task_event_id"], o.ProjectID, o.TargetID, commandID, object["version"]).Scan(&n); err != nil || n != 1 {
				t.Fatal("planning typed history is not the same operation/version")
			}
		}
		var n int
		if eventID == "" {
			t.Fatal("planning event missing")
		}
		if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1 AND producer='work' AND project_id=$2 AND aggregate_id=$3 AND aggregate_version=$4`, eventID, o.ProjectID, o.TargetID, object["version"]).Scan(&n); err != nil || n != 1 {
			t.Fatal("planning Outbox is not the same target/version")
		}
	}
	for _, name := range []string{"milestones-list", "milestones-get", "sprints-list", "sprints-get", "tasks-list", "tasks-get", "blockers"} {
		if !reads[name] {
			t.Fatal("planning missing actual successful read capability", name)
		}
	}
	if conflict != 1 || len(latest) != 3 || len(counts) != 9 {
		t.Fatal("planning command/conflict capability set incomplete")
	}
	for _, kind := range []string{"milestone", "sprint", "task"} {
		for _, action := range []string{"create", "update", "reorder"} {
			want := 1
			if action == "reorder" {
				want = 2
			}
			if counts["work."+kind+"."+action] != want {
				t.Fatal("planning mutation count mismatch", kind, action)
			}
		}
		o := latest[kind]
		var title, description, rank, version string
		if err := f.store.QueryRow(ctx, `SELECT title,description,manual_rank,version::text FROM agenteam_work.`+kind+`s WHERE project_id=$1 AND id=$2`, o["project_id"], o["id"]).Scan(&title, &description, &rank, &version); err != nil || title != o["title"] || description != o["description"] || rank != o["manual_rank"] || version != o["version"] || version != "4" {
			t.Fatal("planning current object differs from final receipt")
		}
		if kind == "milestone" && description != "" {
			t.Fatal("Milestone description was not cleared")
		}
		if kind == "sprint" {
			var parent string
			if err := f.store.QueryRow(ctx, `SELECT milestone_id FROM agenteam_work.sprints WHERE id=$1`, o["id"]).Scan(&parent); err != nil || parent != f.seeds["main"].MilestoneID {
				t.Fatal("planning Sprint parent mismatch")
			}
		}
		if kind == "task" {
			var plan, priority, state, parent string
			if err := f.store.QueryRow(ctx, `SELECT plan,priority,state,sprint_id FROM agenteam_work.tasks WHERE id=$1`, o["id"]).Scan(&plan, &priority, &state, &parent); err != nil || plan != "" || priority != "high" || state != "backlog" || parent != f.seeds["main"].SprintID {
				t.Fatal("planning Task current fields mismatch")
			}
		}
	}
	after := f.facts(ctx, f.seeds["main"].ProjectID)
	// One separately issued formal Task update is the deliberate conflict stimulus.
	for name, delta := range map[string]int{"structure_commands": 8, "task_commands": 5, "blocker_commands": 0, "history": 5, "outbox": 13} {
		if after[name].(int) != f.planningBaseline[name].(int)+delta {
			t.Fatal("planning produced unexpected fact delta", name)
		}
	}
	var pending int
	if err := f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1 AND state<>'completed')+(SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1 AND state<>'completed')`, f.seeds["main"].ProjectID).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("planning left an unexpected prepared command")
	}
	var activity time.Time
	if f.activitySession == "" || f.activityBefore.IsZero() {
		t.Fatal("real Session Activity stimulus absent")
	}
	if err := f.store.QueryRow(ctx, `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, f.activitySession, f.owner.UserID).Scan(&activity); err != nil || !activity.After(f.activityBefore) {
		t.Fatal("browser Work commands did not advance actual Session Activity")
	}
}

func TestAccountProjectWorkPlanningWebBlockers(t *testing.T) {
	runProjectWorkPlanningWeb(t, "blockers", "two_types", "resolve", "cycle", "status", "cursor", "state_unchanged")
}
func TestAccountProjectWorkPlanningWebOriginalRecovery(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "recovery", "three_domains", "lookup_original", "same_replay", "history", "archive", "unique_facts")
	assertProjectWorkOriginalRecovery(t, f)
}
func TestAccountProjectWorkPlanningWebIdentityAndOwnership(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "identity", "logout", "revocation", "owner", "checking", "new_session", "late_read", "confirmations")
	f.guard.Lock()
	defer f.guard.Unlock()
	if f.hold == nil || f.hold.path != projectOwnerWebPath+"/"+f.seeds["main"].ProjectID+"/tasks/"+f.seeds["main"].TaskID || !f.hold.started || !f.hold.finished || !f.readCanceled {
		t.Fatal("declared late read did not observe the exact held Task request cancellation and actual finish")
	}
}
func TestAccountProjectWorkPlanningWebLayouts(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "layouts", "keyboard", "focus", "no_overflow", "readonly", "empty", "error", "no_debug")
	_ = f
}

func assertProjectWorkOriginalRecovery(t *testing.T, f *projectWorkPlanningWebFixture) {
	t.Helper()
	f.guard.Lock()
	lost := make(map[string]projectWorkPlanningWebObservation, len(f.lost))
	for key, value := range f.lost {
		lost[key] = value
	}
	dropped, replayed, cutInvalid := f.dropped, f.replayed, f.cutInvalid
	f.guard.Unlock()
	if cutInvalid || dropped != 3 || replayed != 3 || len(lost) != 3 {
		t.Fatal("three-domain original loss/replay matrix incomplete")
	}
	for _, domain := range []string{"structure", "task", "blocker"} {
		original, ok := lost[domain]
		if !ok || original.Domain != domain || original.Status != 200 {
			t.Fatal("original completed response unavailable")
		}
		cut := original.Cut
		if len(original.Response) <= 1 || cut.Written != 1 || !cut.WriteOK || !cut.FlushOK || !cut.HijackOK || !cut.CloseOK {
			t.Fatal("original response did not actually complete its deliberate one-byte cut and connection close")
		}
		seed := f.seeds[domain]
		var historical, originalResponse map[string]any
		if json.Unmarshal(original.StoredReceipt, &historical) != nil || json.Unmarshal(original.Response, &originalResponse) != nil || !reflect.DeepEqual(historical, originalResponse) {
			t.Fatal("lost original bytes do not match the original durable receipt")
		}
		var commandID, lifecycle string
		var stored []byte
		if err := f.store.QueryRow(f.ctx, `SELECT id,receipt FROM `+workCommandTable(domain)+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, seed.ProjectID, original.Command, original.Key).Scan(&commandID, &stored); err != nil {
			t.Fatal("original historical command missing after replay")
		}
		var currentStored map[string]any
		if json.Unmarshal(stored, &currentStored) != nil || !reflect.DeepEqual(historical, currentStored) {
			t.Fatal("historical receipt rewritten by later commands or replay")
		}
		if err := f.store.QueryRow(f.ctx, `SELECT lifecycle FROM agenteam_project.projects WHERE id=$1`, seed.ProjectID).Scan(&lifecycle); err != nil || lifecycle != "archived" {
			t.Fatal("historical replay did not cross actual archived gate")
		}
		var source map[string]any
		if json.Unmarshal(original.Body, &source) != nil {
			t.Fatal("private original request invalid")
		}
		lookup, mutation := 0, 0
		for _, observed := range f.observations() {
			if observed.Key != original.Key {
				continue
			}
			if observed.ProjectID != original.ProjectID || observed.Domain != domain || observed.Command != original.Command || observed.TargetID != original.TargetID || observed.CSRF != original.CSRF {
				t.Fatal("original identity or target changed")
			}
			var response map[string]any
			if observed.Status != 200 || json.Unmarshal(observed.Response, &response) != nil {
				t.Fatal("original lookup or replay lacks its same successful response")
			}
			if strings.HasSuffix(observed.RawPath, "/lookup") {
				var query map[string]any
				if json.Unmarshal(observed.Body, &query) != nil || !reflect.DeepEqual(query["request"], source["request"]) || !reflect.DeepEqual(query["expected_version"], source["expected_version"]) || query["command"] != original.Command {
					t.Fatal("lookup meaning differs from original mutation")
				}
				stateKey, receiptKey := "status", "receipt"
				if domain == "structure" {
					stateKey, receiptKey = "state", "result"
				}
				if response[stateKey] != "committed" || !reflect.DeepEqual(response[receiptKey], historical) {
					t.Fatal("lookup did not return the exact historical receipt")
				}
				lookup++
			} else {
				var request map[string]any
				if observed.Method != original.Method || observed.RawPath != original.RawPath || json.Unmarshal(observed.Body, &request) != nil || !reflect.DeepEqual(request, source) || !reflect.DeepEqual(response, historical) {
					t.Fatal("replay changed original method/path/body or historical result")
				}
				mutation++
			}
		}
		if lookup != 2 || mutation != 2 {
			t.Fatal("exact original lookup/replay attempt count mismatch")
		}
		objectKey := "task"
		if domain == "structure" {
			objectKey = "milestone"
		}
		object, ok := historical[objectKey].(map[string]any)
		if !ok || object["id"] != original.TargetID {
			t.Fatal("historical target mismatch")
		}
		eventID, _ := historical["event_id"].(string)
		if domain != "structure" {
			eventIDs, ok := historical["event_ids"].([]any)
			if !ok || len(eventIDs) != 1 {
				t.Fatal("historical Outbox shape invalid")
			}
			eventID, _ = eventIDs[0].(string)
			operationColumn := "operation_id"
			if domain == "blocker" {
				operationColumn = "blocker_operation_id"
			}
			var n int
			if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM agenteam_work.task_events WHERE id=$1 AND project_id=$2 AND task_id=$3 AND `+operationColumn+`=$4 AND task_version=$5`, historical["task_event_id"], seed.ProjectID, original.TargetID, commandID, object["version"]).Scan(&n); err != nil || n != 1 {
				t.Fatal("original history not bound to the original operation/version")
			}
		}
		var n int
		if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1 AND producer='work' AND project_id=$2 AND aggregate_id=$3 AND aggregate_version=$4`, eventID, seed.ProjectID, original.TargetID, object["version"]).Scan(&n); err != nil || n != 1 {
			t.Fatal("original Outbox not bound to original version")
		}
		// Independent formal mutations after the lost original advance current
		// facts; original Lookup/replay must retain the earlier exact receipt.
		var currentText, currentVersion string
		column, table := "plan", "tasks"
		if domain == "structure" {
			column, table = "title", "milestones"
		}
		if err := f.store.QueryRow(f.ctx, `SELECT `+column+`,version::text FROM agenteam_work.`+table+` WHERE project_id=$1 AND id=$2`, seed.ProjectID, original.TargetID).Scan(&currentText, &currentVersion); err != nil || currentText != "独立更新后的当前值" || currentVersion == object["version"] {
			t.Fatal("later current value was not kept separate from the original historical receipt")
		}
		deltas := map[string]int{"structure_commands": 0, "task_commands": 0, "blocker_commands": 0, "history": 0, "outbox": 2}
		switch domain {
		case "structure":
			deltas["structure_commands"] = 2
		case "task":
			deltas["task_commands"], deltas["history"] = 2, 2
		case "blocker":
			deltas["task_commands"], deltas["blocker_commands"], deltas["history"], deltas["outbox"] = 1, 2, 3, 3
		}
		after, baseline := f.facts(f.ctx, seed.ProjectID), f.recoveryBaselines[domain]
		if baseline == nil {
			t.Fatal("real recovery seed baseline missing")
		}
		for name, delta := range deltas {
			if after[name].(int) != baseline[name].(int)+delta {
				t.Fatal("original recovery/replay produced an extra or missing fact", domain, name)
			}
		}
		if domain == "blocker" {
			b, ok := historical["blocker"].(map[string]any)
			if !ok || b["resolved_at"] != nil {
				t.Fatal("historical added blocker was replaced with later resolution")
			}
			var resolved bool
			if err := f.store.QueryRow(f.ctx, `SELECT resolved_at IS NOT NULL AND resolution_comment='后继解除' FROM agenteam_work.task_blockers WHERE id=$1 AND project_id=$2`, b["id"], seed.ProjectID).Scan(&resolved); err != nil || !resolved {
				t.Fatal("later real blocker resolution missing")
			}
		}
	}
}
