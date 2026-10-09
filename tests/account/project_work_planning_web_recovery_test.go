//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type projectWorkRecoveryStage struct {
	Kind, Project, Target, ExpectedVersion, Text              string
	Before                                                    map[string]any
	Counts                                                    map[string]any
	Original                                                  *projectWorkPlanningWebObservation
	Hijacked, Closed, NotForwarded, LookupObserved, Completed bool
	FaultHits                                                 int64
	FaultInstalled                                            bool
	WireAttempts, ClosedAttempts                              int
	Rejected                                                  bool
	RejectionReason                                           projectWorkRecoveryRejection
}

const projectWorkUnforwardedAttemptLimit = 4

type projectWorkRecoveryRejection string

const (
	projectWorkFirstBinding  projectWorkRecoveryRejection = "first_binding"
	projectWorkRepeatBinding projectWorkRecoveryRejection = "repeat_binding"
	projectWorkAttemptLimit  projectWorkRecoveryRejection = "attempt_limit"
)

func closeProjectWorkUnforwarded(w http.ResponseWriter) (bool, bool, error) {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return false, false, errors.New("owned before-forward hijack failed")
	}
	if err = conn.Close(); err != nil {
		return true, false, errors.New("owned before-forward close failed")
	}
	return true, true, nil
}

func (f *projectWorkPlanningWebFixture) recoveryStageSnapshot(project string) projectWorkRecoveryStage {
	f.guard.Lock()
	defer f.guard.Unlock()
	if stage := f.recoveryStages[project]; stage != nil {
		return *stage
	}
	return projectWorkRecoveryStage{}
}

func projectWorkRecoveryStageMatches(stage *projectWorkRecoveryStage, o projectWorkPlanningWebObservation) bool {
	if stage == nil || stage.Original != nil || o.Method != http.MethodPatch || o.Domain != "structure" || o.Command != "work.milestone.update" || o.ProjectID != stage.Project || o.TargetID != stage.Target || o.RawPath != projectOwnerWebPath+"/"+stage.Project+"/milestones/"+stage.Target || o.RawQuery != "" || foundation.IdempotencyKey(o.Key).Validate() != nil {
		return false
	}
	var body map[string]any
	return json.Unmarshal(o.Body, &body) == nil && reflect.DeepEqual(body, map[string]any{"expected_version": stage.ExpectedVersion, "request": map[string]any{"title": stage.Text}})
}

// This runs before the existing ReverseProxy receives the declared request.
// A zero-response close is kept distinct from the completed-response truncation
// fixture: no fabricated headers, receipt, successful status, or body is emitted.
func (f *projectWorkPlanningWebFixture) recoveryBeforeForward(w http.ResponseWriter, r *http.Request, o projectWorkPlanningWebObservation) (bool, error) {
	f.guard.Lock()
	stage := f.recoveryStages[o.ProjectID]
	armedUnforwarded := stage != nil && stage.Kind == "not-observed" && !stage.LookupObserved && o.Method == http.MethodPatch && o.RawPath == projectOwnerWebPath+"/"+stage.Project+"/milestones/"+stage.Target
	first := stage != nil && stage.Original == nil
	if armedUnforwarded {
		matches := projectWorkRecoveryStageMatches(stage, o)
		if !first {
			original := stage.Original
			matches = o.ProjectID == original.ProjectID && o.TargetID == original.TargetID && o.Domain == original.Domain && o.Command == original.Command && o.RawQuery == "" && o.Key == original.Key && o.CSRF == original.CSRF && bytes.Equal(o.Body, original.Body)
		}
		if !matches || stage.WireAttempts >= projectWorkUnforwardedAttemptLimit {
			stage.Rejected = true
			// Preserve only the first rejection's closed branch, never request
			// material or an arbitrary error string. This does not alter the gate.
			if stage.RejectionReason == "" {
				switch {
				case !matches && first:
					stage.RejectionReason = projectWorkFirstBinding
				case !matches:
					stage.RejectionReason = projectWorkRepeatBinding
				default:
					stage.RejectionReason = projectWorkAttemptLimit
				}
			}
			f.guard.Unlock()
			return false, errors.New("owned unforwarded original changed or exceeded its bound")
		}
		stage.WireAttempts++
	} else if !projectWorkRecoveryStageMatches(stage, o) {
		f.guard.Unlock()
		return true, nil
	}
	if first {
		copy := o
		copy.Body = append([]byte(nil), o.Body...)
		stage.Original = &copy
	}
	f.guard.Unlock()
	if stage.Kind == "in-progress" {
		return true, f.installRecoveryOutboxFault(r.Context(), stage)
	}
	if stage.Kind != "not-observed" {
		return false, errors.New("closed recovery stage unavailable")
	}
	if first {
		var count int
		if err := f.store.QueryRow(r.Context(), `SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, o.ProjectID, o.Command, o.Key).Scan(&count); err != nil || count != 0 {
			return false, errors.New("unforwarded original key already has a command")
		}
	}
	hijacked, closed, closeErr := closeProjectWorkUnforwarded(w)
	f.guard.Lock()
	stage.Hijacked = stage.Hijacked || hijacked
	stage.Closed = stage.Closed || closed
	if hijacked && closed && closeErr == nil {
		stage.ClosedAttempts++
	}
	stage.NotForwarded = true
	f.guard.Unlock()
	if closeErr != nil {
		return false, errors.New("owned before-forward close failed")
	}
	return false, nil
}

func projectWorkUnforwardedComplete(stage projectWorkRecoveryStage) bool {
	return stage.Original != nil && stage.NotForwarded && stage.Hijacked && stage.Closed && !stage.Rejected && stage.WireAttempts >= 1 && stage.WireAttempts <= projectWorkUnforwardedAttemptLimit && stage.ClosedAttempts == stage.WireAttempts
}

// Called only after stopProxy has joined. This projection contains no request
// material, IDs, arbitrary errors or caller-controlled strings, including on FAIL.
func (f *projectWorkPlanningWebFixture) writeRecoveryStageEvidence() {
	if f.mode != "recovery" {
		return
	}
	type safeStage struct {
		Kind            string                       `json:"kind"`
		Bound           bool                         `json:"bound"`
		WireAttempts    int                          `json:"wire_attempts"`
		ClosedAttempts  int                          `json:"closed_attempts"`
		Rejected        bool                         `json:"rejected"`
		RejectionReason projectWorkRecoveryRejection `json:"rejection_reason"`
		LookupObserved  bool                         `json:"lookup_observed"`
		Completed       bool                         `json:"completed"`
		FaultInstalled  bool                         `json:"fault_installed"`
		FaultHits       int64                        `json:"fault_hits"`
	}
	stages := make([]safeStage, 0, 2)
	f.guard.Lock()
	for _, kind := range []string{"not-observed", "in-progress"} {
		if seed, ok := f.seeds[kind]; ok {
			if stage := f.recoveryStages[seed.ProjectID]; stage != nil {
				stages = append(stages, safeStage{kind, stage.Original != nil, stage.WireAttempts, stage.ClosedAttempts, stage.Rejected, stage.RejectionReason, stage.LookupObserved, stage.Completed, stage.FaultInstalled, stage.FaultHits})
			}
		}
	}
	f.guard.Unlock()
	raw, err := json.Marshal(stages)
	if err != nil || os.WriteFile(filepath.Join(f.evidence, "work-recovery-stage-evidence.json"), raw, 0600) != nil {
		f.t.Error("safe recovery stage evidence could not be saved")
	}
}

func (f *projectWorkPlanningWebFixture) installRecoveryOutboxFault(ctx context.Context, stage *projectWorkRecoveryStage) error {
	// Trigger identifiers and SQL are fixed. Every argument is validated and
	// additionally quoted; command/body material never enters logs or the DOM.
	o := stage.Original
	if o == nil || foundation.IdempotencyKey(o.Key).Validate() != nil {
		return errors.New("owned planned trigger has no original key")
	}
	conn, err := f.db.Fixture.Connect(ctx, f.db.Name)
	if err != nil {
		return errors.New("owned final-Outbox setup connection unavailable")
	}
	defer func() {
		if err := conn.Close(ctx); err != nil {
			f.t.Error("owned final-Outbox setup connection close failed")
		}
	}()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	_, err = conn.Exec(ctx, `CREATE SEQUENCE owner_web_fixture.work_ui_planned_hits;
CREATE FUNCTION owner_web_fixture.work_ui_fail_final_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.producer='work' AND NEW.project_id::text=TG_ARGV[0] AND NEW.aggregate_id::text=TG_ARGV[1]
    AND EXISTS(SELECT 1 FROM agenteam_work.structure_commands c
      WHERE c.project_id::text=TG_ARGV[0] AND c.command_name=TG_ARGV[2] AND c.idempotency_key=TG_ARGV[3]
        AND c.state='planned' AND c.receipt IS NULL AND c.event_id=NEW.id) THEN
   PERFORM nextval('owner_web_fixture.work_ui_planned_hits');
   RAISE EXCEPTION USING ERRCODE='P0001',MESSAGE='work-ui-owned-final-outbox-failure';
 END IF;
 RETURN NEW;
END $$;`)
	if err != nil {
		return errors.New("owned planned fault setup failed")
	}
	// These objects belong only to the original per-top private database. A
	// failing case still retires that database through the existing fixture;
	// no new resource cleaner or concurrent cleanup against live handlers is
	// installed. Successful external continuation removes them explicitly.
	query := `CREATE TRIGGER work_ui_planned_failure AFTER INSERT ON agenteam_outbox.events FOR EACH ROW EXECUTE FUNCTION owner_web_fixture.work_ui_fail_final_outbox(` + quote(o.ProjectID) + "," + quote(o.TargetID) + "," + quote(o.Command) + "," + quote(o.Key) + ")"
	if _, err = conn.Exec(ctx, query); err != nil {
		return errors.New("owned final-Outbox trigger installation failed")
	}
	f.guard.Lock()
	stage.FaultInstalled = true
	f.guard.Unlock()
	return nil
}

func (f *projectWorkPlanningWebFixture) removeRecoveryOutboxFault(ctx context.Context, stage *projectWorkRecoveryStage) error {
	if !f.recoveryStageSnapshot(stage.Project).FaultInstalled {
		return errors.New("owned final-Outbox fault was not installed")
	}
	conn, err := f.db.Fixture.Connect(ctx, f.db.Name)
	if err != nil {
		return errors.New("owned final-Outbox removal connection unavailable")
	}
	defer func() {
		if err := conn.Close(ctx); err != nil {
			f.t.Error("owned final-Outbox removal connection close failed")
		}
	}()
	var called bool
	var hits int64
	if err := conn.QueryRow(ctx, `SELECT last_value,is_called FROM owner_web_fixture.work_ui_planned_hits`).Scan(&hits, &called); err != nil || !called || hits != 1 {
		return errors.New("real final-Outbox failure boundary was not reached exactly once")
	}
	if _, err := conn.Exec(ctx, `DROP TRIGGER work_ui_planned_failure ON agenteam_outbox.events; DROP FUNCTION owner_web_fixture.work_ui_fail_final_outbox(); DROP SEQUENCE owner_web_fixture.work_ui_planned_hits`); err != nil {
		return errors.New("owned final-Outbox fault removal failed")
	}
	f.guard.Lock()
	stage.FaultInstalled = false
	stage.FaultHits = hits
	f.guard.Unlock()
	return nil
}

func (f *projectWorkPlanningWebFixture) recoveryOriginalHTTP(ctx context.Context, original projectWorkPlanningWebObservation, body []byte, want int) map[string]any {
	f.t.Helper()
	r, err := http.NewRequestWithContext(ctx, original.Method, f.origin+original.RawPath, bytes.NewReader(body))
	if err != nil {
		f.t.Fatal("owned original continuation request unavailable")
	}
	r.Header.Set("Origin", f.origin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.ownerCSRF)
	r.Header.Set("Idempotency-Key", original.Key)
	response, err := f.ownerClient.Do(r)
	if err != nil {
		f.t.Fatal("owned original continuation transport failed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	closeErr := response.Body.Close()
	defer clear(raw)
	var value map[string]any
	if readErr != nil || closeErr != nil || len(raw) > 1024*1024 || response.StatusCode != want || json.Unmarshal(raw, &value) != nil {
		f.t.Fatal("owned original continuation status or complete body failed")
	}
	return value
}

func projectWorkRecoveryRequest(mode string, r projectWorkPlanningWebIPC) bool {
	if mode != "recovery" || r.Domain != "" || r.Resource != "" || r.Target != "" || r.Text != nil {
		return false
	}
	switch r.Action {
	case "arm-recovery-stage", "observe-recovery-stage":
		return r.Project == "not-observed" || r.Project == "in-progress"
	case "complete-planned-original":
		return r.Project == "in-progress"
	case "reject-changed-meaning":
		return r.Project == "structure" || r.Project == "task" || r.Project == "blocker"
	default:
		return false
	}
}
func (f *projectWorkPlanningWebFixture) recoveryIPC(ctx context.Context, r projectWorkPlanningWebIPC) map[string]any {
	if !projectWorkRecoveryRequest(f.mode, r) {
		f.t.Fatal("closed author recovery stimulus required")
	}
	seed, ok := f.seeds[r.Project]
	if !ok {
		f.t.Fatal("owned recovery Project unavailable")
	}
	out := map[string]any{"sequence": r.Sequence}
	switch r.Action {
	case "arm-recovery-stage":
		if r.Project != "not-observed" && r.Project != "in-progress" {
			f.t.Fatal("closed recovery stage rejected")
		}
		if f.recoveryStageSnapshot(seed.ProjectID).Project != "" {
			f.t.Fatal("recovery stage already armed")
		}
		before := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, projectOwnerWebPath+"/"+seed.ProjectID+"/milestones/"+seed.MilestoneID, nil, "", false, http.StatusOK)
		stage := &projectWorkRecoveryStage{Kind: r.Project, Project: seed.ProjectID, Target: seed.MilestoneID, ExpectedVersion: httpString(f.t, before, "version"), Text: "恢复状态原命令 " + r.Project, Before: before, Counts: f.facts(ctx, seed.ProjectID)}
		f.guard.Lock()
		f.recoveryStages[seed.ProjectID] = stage
		f.guard.Unlock()
		out["expected_version"], out["title"] = stage.ExpectedVersion, stage.Text
	case "observe-recovery-stage":
		stage := f.recoveryStageSnapshot(seed.ProjectID)
		if stage.Original == nil || stage.LookupObserved {
			f.t.Fatal("original recovery stage unavailable or repeated")
		}
		original := stage.Original
		observed := false
		for _, o := range f.observations() {
			if o.ProjectID != original.ProjectID || o.Key != original.Key || o.Command != original.Command || o.TargetID != original.TargetID || o.RawPath != projectOwnerWebPath+"/"+original.ProjectID+"/structure-commands/lookup" || o.Method != http.MethodPost || o.Status != http.StatusOK || o.CSRF != original.CSRF {
				continue
			}
			var response, request, source map[string]any
			if json.Unmarshal(o.Response, &response) != nil || json.Unmarshal(o.Body, &request) != nil || json.Unmarshal(original.Body, &source) != nil || !reflect.DeepEqual(request, map[string]any{"command": original.Command, "target_id": original.TargetID, "expected_version": source["expected_version"], "request": source["request"]}) || !reflect.DeepEqual(response, map[string]any{"state": strings.ReplaceAll(stage.Kind, "-", "_"), "result": nil}) {
				f.t.Fatal("actual original Lookup did not prove the expected nonterminal state")
			}
			observed = true
		}
		if !observed {
			f.t.Fatal("actual original nonterminal Lookup absent")
		}
		var planned, total int
		if err := f.store.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE state='planned' AND plan IS NOT NULL AND receipt IS NULL AND committed_at IS NULL) FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, original.ProjectID, original.Command, original.Key).Scan(&total, &planned); err != nil {
			f.t.Fatal("actual recovery command observation failed")
		}
		if stage.Kind == "not-observed" {
			if total != 0 || !projectWorkUnforwardedComplete(stage) {
				f.t.Fatal("not_observed original was forwarded or acquired a fact")
			}
		} else if total != 1 || planned != 1 || !stage.FaultInstalled {
			f.t.Fatal("original request did not persist only planned")
		}
		current := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, original.RawPath, nil, "", false, http.StatusOK)
		if !reflect.DeepEqual(current, stage.Before) || !reflect.DeepEqual(f.facts(ctx, seed.ProjectID), stage.Counts) {
			f.t.Fatal("nonterminal observation changed canonical/history/Outbox facts")
		}
		f.guard.Lock()
		live := f.recoveryStages[seed.ProjectID]
		if stage.Kind == "not-observed" && !projectWorkUnforwardedComplete(*live) {
			f.guard.Unlock()
			f.t.Fatal("unforwarded original still closing or rejected before release")
		}
		live.LookupObserved = true
		f.guard.Unlock()
		out["state"] = strings.ReplaceAll(stage.Kind, "-", "_")
	case "complete-planned-original":
		current := f.recoveryStageSnapshot(seed.ProjectID)
		if current.Kind != "in-progress" || current.Original == nil || !current.LookupObserved || current.Completed {
			f.t.Fatal("external original continuation unavailable")
		}
		f.guard.Lock()
		stage := f.recoveryStages[seed.ProjectID]
		f.guard.Unlock()
		if err := f.removeRecoveryOutboxFault(ctx, stage); err != nil {
			f.t.Fatal("actual final-Outbox failure/removal incomplete")
		}
		out["receipt"] = f.recoveryOriginalHTTP(ctx, *stage.Original, stage.Original.Body, http.StatusOK)
		f.guard.Lock()
		stage.Completed = true
		f.guard.Unlock()
	case "reject-changed-meaning":
		f.guard.Lock()
		original, ok := f.lost[r.Project]
		already := f.changedMeaning[r.Project] != nil
		f.guard.Unlock()
		if !ok || r.Project != "structure" && r.Project != "task" && r.Project != "blocker" || already {
			f.t.Fatal("original meaning counterexample unavailable")
		}
		var body map[string]any
		if json.Unmarshal(original.Body, &body) != nil {
			f.t.Fatal("private original meaning unavailable")
		}
		request, ok := body["request"].(map[string]any)
		if !ok {
			f.t.Fatal("private original request missing")
		}
		field := "title"
		if r.Project == "blocker" {
			field = "description"
		}
		request[field] = "不同含义不得提交"
		raw, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal("counterexample encoding failed")
		}
		defer clear(raw)
		before := f.facts(ctx, seed.ProjectID)
		problem := f.recoveryOriginalHTTP(ctx, original, raw, http.StatusConflict)
		if problem["code"] != "IDEMPOTENCY_KEY_REUSED" || !reflect.DeepEqual(before, f.facts(ctx, seed.ProjectID)) {
			f.t.Fatal("changed meaning was not rejected without new facts")
		}
		for _, o := range f.observations() {
			if o.Key == original.Key && o.RawPath == original.RawPath && o.Method == original.Method && o.Status == http.StatusConflict && o.CSRF == sha256.Sum256([]byte(f.ownerCSRF)) && bytes.Equal(o.Body, raw) {
				f.guard.Lock()
				repeated := f.changedMeaning[r.Project] != nil
				copy := o
				f.changedMeaning[r.Project] = &copy
				f.guard.Unlock()
				if repeated {
					f.t.Fatal("changed meaning counterexample repeated")
				}
			}
		}
		f.guard.Lock()
		matched := f.changedMeaning[r.Project] != nil
		f.guard.Unlock()
		if !matched {
			f.t.Fatal("changed meaning original request correlation missing")
		}
		out["rejected"] = true
	default:
		f.t.Fatal("unknown recovery stimulus")
	}
	return out
}

func assertProjectWorkRecoveryStages(t *testing.T, f *projectWorkPlanningWebFixture) {
	t.Helper()
	f.guard.Lock()
	stages := make([]projectWorkRecoveryStage, 0, len(f.recoveryStages))
	for _, stage := range f.recoveryStages {
		stages = append(stages, *stage)
	}
	f.guard.Unlock()
	if len(stages) != 2 {
		t.Fatal("both real nonterminal recovery stages required")
	}
	for _, stage := range stages {
		if stage.Original == nil || !stage.LookupObserved || stage.FaultInstalled {
			t.Fatal("real recovery stage incomplete")
		}
		if stage.Kind == "in-progress" && (!stage.Completed || stage.FaultHits != 1) {
			t.Fatal("planned original never completed after the one actual rollback")
		}
		original := stage.Original
		var commandID string
		var stored []byte
		if err := f.store.QueryRow(f.ctx, `SELECT id,receipt FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND actor_user_id=$4 AND request->>'target_id'=$5 AND state='completed'`, original.ProjectID, original.Command, original.Key, f.owner.UserID, stage.Target).Scan(&commandID, &stored); err != nil {
			t.Fatal("completed original stage receipt absent")
		}
		var receipt map[string]any
		if json.Unmarshal(stored, &receipt) != nil || receipt["command"] != original.Command || receipt["changed"] != true {
			t.Fatal("completed original stage receipt malformed")
		}
		object, ok := receipt["milestone"].(map[string]any)
		if !ok || object["id"] != stage.Target || object["title"] != stage.Text || object["version"] == stage.ExpectedVersion {
			t.Fatal("completed recovery changed original meaning")
		}
		var count int
		if err := f.store.QueryRow(f.ctx, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1 AND project_id=$2 AND aggregate_id=$3 AND aggregate_version=$4 AND producer='work'`, receipt["event_id"], stage.Project, stage.Target, object["version"]).Scan(&count); err != nil || count != 1 {
			t.Fatal("completed original stage Outbox missing")
		}
		for name, value := range f.facts(f.ctx, stage.Project) {
			delta := 0
			if name == "structure_commands" || name == "outbox" {
				delta = 1
			}
			if value.(int) != stage.Counts[name].(int)+delta {
				t.Fatal("recovery stage produced duplicate or missing facts", name)
			}
		}
		committedLookup, nonterminalLookup, successfulMutation, nonterminalMutation := 0, 0, 0, 0
		privateCSRF := sha256.Sum256([]byte(f.ownerCSRF))
		if original.CSRF == privateCSRF || original.CSRF == [32]byte{} {
			t.Fatal("original stage did not come from the distinct genuine browser Session")
		}
		var source map[string]any
		if json.Unmarshal(original.Body, &source) != nil {
			t.Fatal("original stage request unavailable")
		}
		for _, o := range f.observations() {
			if o.Key != original.Key {
				continue
			}
			if o.ProjectID != original.ProjectID || o.Command != original.Command || o.TargetID != original.TargetID || o.RawQuery != "" {
				t.Fatal("recovery original identity changed")
			}
			if o.RawPath == projectOwnerWebPath+"/"+original.ProjectID+"/structure-commands/lookup" {
				var observed, request map[string]any
				if o.Method != http.MethodPost || o.Status != http.StatusOK || o.CSRF != original.CSRF || json.Unmarshal(o.Response, &observed) != nil || json.Unmarshal(o.Body, &request) != nil || !reflect.DeepEqual(request, map[string]any{"command": original.Command, "target_id": original.TargetID, "expected_version": source["expected_version"], "request": source["request"]}) {
					t.Fatal("original Lookup method/body/Session/status changed")
				}
				if observed["state"] == "committed" {
					if !reflect.DeepEqual(observed, map[string]any{"state": "committed", "result": receipt}) {
						t.Fatal("Lookup replaced the actual original receipt")
					}
					committedLookup++
				} else {
					if !reflect.DeepEqual(observed, map[string]any{"state": strings.ReplaceAll(stage.Kind, "-", "_"), "result": nil}) {
						t.Fatal("original nonterminal Lookup changed its closed meaning")
					}
					nonterminalLookup++
				}
				continue
			}
			if o.RawPath != original.RawPath || o.Method != original.Method || !bytes.Equal(o.Body, original.Body) {
				t.Fatal("original mutation method/path/body changed")
			}
			if o.Status == http.StatusOK {
				var observed map[string]any
				wantCSRF := original.CSRF
				if stage.Kind == "in-progress" {
					wantCSRF = privateCSRF
				}
				if o.CSRF != wantCSRF || json.Unmarshal(o.Response, &observed) != nil || !reflect.DeepEqual(observed, receipt) {
					t.Fatal("successful original continuation changed its actor Session or receipt")
				}
				successfulMutation++
			} else {
				if o.CSRF != original.CSRF {
					t.Fatal("nonterminal original did not use the browser Session")
				}
				if stage.Kind == "not-observed" {
					if o.Status != 0 || len(o.Response) != 0 {
						t.Fatal("unforwarded original unexpectedly received a service response")
					}
				} else {
					var problem map[string]any
					if o.Status != http.StatusServiceUnavailable || json.Unmarshal(o.Response, &problem) != nil || problem["code"] != "DEPENDENCY_UNAVAILABLE" {
						t.Fatal("actual final-Outbox rollback response differs from the formal service mapping")
					}
				}
				nonterminalMutation++
			}
		}
		wantInitial := 1
		if stage.Kind == "not-observed" {
			if !projectWorkUnforwardedComplete(stage) {
				t.Fatal("unforwarded physical attempts did not all close within their bound")
			}
			wantInitial = stage.ClosedAttempts
		}
		if committedLookup != 1 || nonterminalLookup != 1 || successfulMutation != 1 || nonterminalMutation != wantInitial {
			t.Fatal("original operations differ from closed physical attempts, one nonterminal Lookup, explicit continuation and committed Lookup")
		}
		_ = commandID // the actual unique command is observed above; no synthetic ID is supplied.
	}
}

func TestProjectWorkUnforwardedClose(t *testing.T) {
	for _, mode := range []string{"success", "hijack", "close"} {
		t.Run(mode, func(t *testing.T) {
			local, peer := net.Pipe()
			t.Cleanup(func() { _ = local.Close(); _ = peer.Close() })
			w := &projectWorkCutWriter{header: make(http.Header), mode: mode, conn: local}
			hijacked, closed, err := closeProjectWorkUnforwarded(w)
			want := []string{"hijack"}
			if mode != "hijack" {
				want = append(want, "close")
			}
			if !reflect.DeepEqual(w.trace, want) || len(w.header) != 0 || hijacked != (mode != "hijack") || closed != (mode == "success") || (err == nil) != (mode == "success") {
				t.Fatal("unforwarded close wrote response bytes, retried or lost actual returns")
			}
		})
	}
}

func TestProjectWorkRecoveryStageOriginalBinding(t *testing.T) {
	const p = "01900000-0000-7000-8000-000000000001"
	const target = "01900000-0000-7000-8000-000000000002"
	s := &projectWorkRecoveryStage{Kind: "in-progress", Project: p, Target: target, ExpectedVersion: "4", Text: "exact"}
	o := projectWorkPlanningWebObservation{Method: http.MethodPatch, Domain: "structure", Command: "work.milestone.update", ProjectID: p, TargetID: target, RawPath: projectOwnerWebPath + "/" + p + "/milestones/" + target, Key: "original-key", Body: []byte(`{"expected_version":"4","request":{"title":"exact"}}`)}
	if !projectWorkRecoveryStageMatches(s, o) {
		t.Fatal("exact original denied")
	}
	for index, change := range []func(*projectWorkPlanningWebObservation){
		func(o *projectWorkPlanningWebObservation) { o.Method = http.MethodPost },
		func(o *projectWorkPlanningWebObservation) { o.RawPath += "/reorder" },
		func(o *projectWorkPlanningWebObservation) { o.RawQuery = "x=1" },
		func(o *projectWorkPlanningWebObservation) { o.ProjectID = target },
		func(o *projectWorkPlanningWebObservation) { o.TargetID = p },
		func(o *projectWorkPlanningWebObservation) { o.Key = "" },
		func(o *projectWorkPlanningWebObservation) { o.Command = "work.milestone.reorder" },
		func(o *projectWorkPlanningWebObservation) {
			o.Body = []byte(`{"expected_version":"5","request":{"title":"exact"}}`)
		},
		func(o *projectWorkPlanningWebObservation) {
			o.Body = []byte(`{"expected_version":"4","request":{"title":"other"}}`)
		},
		func(o *projectWorkPlanningWebObservation) {
			o.Body = []byte(`{"expected_version":"4","request":{"title":"exact","description":"extra"}}`)
		},
	} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			copy := o
			change(&copy)
			if projectWorkRecoveryStageMatches(s, copy) {
				t.Fatal("changed original admitted")
			}
		})
	}
	s.Original = &o
	if projectWorkRecoveryStageMatches(s, o) {
		t.Fatal("same arm admitted a second original")
	}
}

func TestProjectWorkRecoveryStageDefaultNoop(t *testing.T) {
	f := &projectWorkPlanningWebFixture{mode: "identity"}
	w := &projectWorkCutWriter{header: make(http.Header)}
	proceed, err := f.recoveryBeforeForward(w, nil, projectWorkPlanningWebObservation{})
	if !proceed || err != nil || len(w.trace) != 0 || len(w.header) != 0 {
		t.Fatal("unconfigured recovery hook changed default forwarding")
	}
}

func TestProjectWorkNotObservedRepeatBoundary(t *testing.T) {
	const project = "01900000-0000-7000-8000-000000000001"
	const target = "01900000-0000-7000-8000-000000000002"
	const key, csrf = "original-key", "private-test-csrf"
	path := projectOwnerWebPath + "/" + project + "/milestones/" + target
	body := []byte(`{"expected_version":"1","request":{"title":"original"}}`)
	original := projectWorkPlanningWebObservation{Method: http.MethodPatch, RawPath: path, ProjectID: project, TargetID: target, Domain: "structure", Command: "work.milestone.update", Key: key, CSRF: sha256.Sum256([]byte(csrf)), Body: body}
	stage := &projectWorkRecoveryStage{Kind: "not-observed", Project: project, Target: target, ExpectedVersion: "1", Text: "original", Original: &original, NotForwarded: true, Hijacked: true, Closed: true, WireAttempts: 1, ClosedAttempts: 1}
	f := &projectWorkPlanningWebFixture{mode: "recovery", enabled: true, recoveryStages: map[string]*projectWorkRecoveryStage{project: stage}, records: []projectWorkPlanningWebObservation{original}}
	owner := &projectOwnerWebFixture{work: f, authenticationWebFixture: &authenticationWebFixture{t: t}}
	f.projectOwnerWebFixture = owner
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("X-CSRF-Token", csrf)
		return r
	}
	local, peer := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = peer.Close() })
	w := &projectWorkCutWriter{header: make(http.Header), conn: local}
	if owner.observeRequest(w, request()) {
		t.Fatal("same original wire attempt escaped the armed not_observed boundary")
	}
	if !reflect.DeepEqual(w.trace, []string{"hijack", "close"}) || len(w.header) != 0 {
		t.Fatal("repeated original was not really closed without a response")
	}
	if stage.WireAttempts != 2 || stage.ClosedAttempts != 2 || !projectWorkUnforwardedComplete(*stage) {
		t.Fatal("actual repeated close was not counted")
	}
	// This models only the release flag set after the dynamic fixture has
	// verified the public original Lookup and actual same-key SQL absence.
	// It does not claim to establish those PG facts in this pure test.
	stage.LookupObserved = true
	replayed := request()
	untouched := &projectWorkCutWriter{header: make(http.Header)}
	if !owner.observeRequest(untouched, replayed) || len(untouched.trace) != 0 {
		t.Fatal("explicit replay after verified Lookup was still intercepted")
	}
	forwarded, err := io.ReadAll(replayed.Body)
	if err != nil || !bytes.Equal(forwarded, body) || replayed.Header.Get("Idempotency-Key") != key || replayed.Header.Get("X-CSRF-Token") != csrf {
		t.Fatal("released original body/key/Session changed")
	}
	_ = replayed.Body.Close()
}

func TestProjectWorkNotObservedBoundaryControls(t *testing.T) {
	const project = "01900000-0000-7000-8000-000000000001"
	const target = "01900000-0000-7000-8000-000000000002"
	original := projectWorkPlanningWebObservation{Method: http.MethodPatch, RawPath: projectOwnerWebPath + "/" + project + "/milestones/" + target, ProjectID: project, TargetID: target, Domain: "structure", Command: "work.milestone.update", Key: "original-key", CSRF: sha256.Sum256([]byte("original-session")), Body: []byte(`{"expected_version":"1","request":{"title":"original"}}`)}
	newStage := func() *projectWorkRecoveryStage {
		return &projectWorkRecoveryStage{Kind: "not-observed", Project: project, Target: target, ExpectedVersion: "1", Text: "original", Original: &original, NotForwarded: true, Hijacked: true, Closed: true, WireAttempts: 1, ClosedAttempts: 1}
	}
	for _, sample := range []struct {
		name   string
		change func(*projectWorkRecoveryStage, *projectWorkPlanningWebObservation)
	}{
		{"key", func(_ *projectWorkRecoveryStage, o *projectWorkPlanningWebObservation) { o.Key = "changed-key" }},
		{"raw-body", func(_ *projectWorkRecoveryStage, o *projectWorkPlanningWebObservation) {
			o.Body = append([]byte(" "), o.Body...)
		}},
		{"csrf", func(_ *projectWorkRecoveryStage, o *projectWorkPlanningWebObservation) {
			o.CSRF = sha256.Sum256([]byte("another-session"))
		}},
		{"query", func(_ *projectWorkRecoveryStage, o *projectWorkPlanningWebObservation) { o.RawQuery = "extra=1" }},
		{"command", func(_ *projectWorkRecoveryStage, o *projectWorkPlanningWebObservation) {
			o.Command = "work.milestone.reorder"
		}},
		{"target", func(_ *projectWorkRecoveryStage, o *projectWorkPlanningWebObservation) { o.TargetID = project }},
		{"limit", func(s *projectWorkRecoveryStage, _ *projectWorkPlanningWebObservation) {
			s.WireAttempts, s.ClosedAttempts = 4, 4
		}},
		{"first-meaning", func(s *projectWorkRecoveryStage, o *projectWorkPlanningWebObservation) {
			s.Original = nil
			o.Body = []byte(`{"expected_version":"1","request":{"title":"changed"}}`)
		}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			stage, observed := newStage(), original
			sample.change(stage, &observed)
			f := &projectWorkPlanningWebFixture{recoveryStages: map[string]*projectWorkRecoveryStage{project: stage}}
			w := &projectWorkCutWriter{header: make(http.Header)}
			proceed, err := f.recoveryBeforeForward(w, nil, observed)
			if proceed || err == nil || !stage.Rejected || len(w.trace) != 0 || len(w.header) != 0 || projectWorkUnforwardedComplete(*stage) {
				t.Fatal("changed or excessive stage request was admitted or fabricated a response")
			}
			wantReason := projectWorkRepeatBinding
			if sample.name == "limit" {
				wantReason = projectWorkAttemptLimit
			} else if sample.name == "first-meaning" {
				wantReason = projectWorkFirstBinding
			}
			if stage.RejectionReason != wantReason {
				t.Fatal("safe rejection reason does not identify the original gate")
			}
			// A later, different failure must not overwrite the first observed
			// reason; neither is permission to release the existing barrier.
			stage.Original = &original
			stage.WireAttempts = projectWorkUnforwardedAttemptLimit
			proceed, err = f.recoveryBeforeForward(w, nil, original)
			if proceed || err == nil || stage.RejectionReason != wantReason || projectWorkUnforwardedComplete(*stage) {
				t.Fatal("subsequent rejection rewrote the first branch or released")
			}
		})
	}
	t.Run("physical-limit-and-close-failure", func(t *testing.T) {
		stage := newStage()
		f := &projectWorkPlanningWebFixture{recoveryStages: map[string]*projectWorkRecoveryStage{project: stage}}
		for want := 2; want <= 4; want++ {
			local, peer := net.Pipe()
			w := &projectWorkCutWriter{header: make(http.Header), conn: local}
			proceed, err := f.recoveryBeforeForward(w, nil, original)
			_ = peer.Close()
			_ = local.Close()
			if proceed || err != nil || stage.WireAttempts != want || stage.ClosedAttempts != want || !projectWorkUnforwardedComplete(*stage) {
				t.Fatal("bounded physical attempt did not really close")
			}
		}
		stage = newStage()
		f.recoveryStages[project] = stage
		w := &projectWorkCutWriter{header: make(http.Header), mode: "hijack"}
		proceed, err := f.recoveryBeforeForward(w, nil, original)
		if proceed || err == nil || stage.WireAttempts != 2 || stage.ClosedAttempts != 1 || projectWorkUnforwardedComplete(*stage) {
			t.Fatal("failed physical close was accepted as a completed barrier")
		}
	})
	t.Run("other-target-read-and-lookup", func(t *testing.T) {
		f := &projectWorkPlanningWebFixture{recoveryStages: map[string]*projectWorkRecoveryStage{project: newStage()}}
		for _, o := range []projectWorkPlanningWebObservation{
			{ProjectID: project, Method: http.MethodGet, RawPath: original.RawPath},
			{ProjectID: project, Method: http.MethodPost, RawPath: projectOwnerWebPath + "/" + project + "/structure-commands/lookup"},
			{ProjectID: project, Method: http.MethodPatch, RawPath: projectOwnerWebPath + "/" + project + "/milestones/" + project},
		} {
			w := &projectWorkCutWriter{header: make(http.Header)}
			proceed, err := f.recoveryBeforeForward(w, nil, o)
			if !proceed || err != nil || len(w.trace) != 0 {
				t.Fatal("declared not_observed barrier captured an unrelated operation")
			}
		}
	})
}

func TestProjectWorkRecoverySafeStageEvidence(t *testing.T) {
	directory := t.TempDir()
	f := &projectWorkPlanningWebFixture{mode: "recovery", projectOwnerWebFixture: &projectOwnerWebFixture{evidence: directory, authenticationWebFixture: &authenticationWebFixture{t: t}}, seeds: map[string]projectWorkPlanningWebSeed{"not-observed": {ProjectID: "private-project"}}, recoveryStages: map[string]*projectWorkRecoveryStage{"private-project": {Kind: "not-observed", Original: &projectWorkPlanningWebObservation{Key: "private-key", Body: []byte("private-body")}, WireAttempts: 2, ClosedAttempts: 2}}}
	f.writeRecoveryStageEvidence()
	raw, err := os.ReadFile(filepath.Join(directory, "work-recovery-stage-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	want := []map[string]any{{"kind": "not-observed", "bound": true, "wire_attempts": float64(2), "closed_attempts": float64(2), "rejected": false, "rejection_reason": "", "lookup_observed": false, "completed": false, "fault_installed": false, "fault_hits": float64(0)}}
	if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, want) || bytes.Contains(raw, []byte("private-")) {
		t.Fatal("recovery evidence changed its closed projection")
	}
	for _, reason := range []projectWorkRecoveryRejection{projectWorkFirstBinding, projectWorkRepeatBinding, projectWorkAttemptLimit} {
		f.recoveryStages["private-project"].RejectionReason = reason
		f.recoveryStages["private-project"].Rejected = true
		f.writeRecoveryStageEvidence()
		raw, err = os.ReadFile(filepath.Join(directory, "work-recovery-stage-evidence.json"))
		want[0]["rejected"], want[0]["rejection_reason"] = true, string(reason)
		if err != nil || json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, want) || bytes.Contains(raw, []byte("private-")) {
			t.Fatal("failure sidecar changed its safe branch projection")
		}
	}
	f.mode, f.evidence = "identity", filepath.Join(directory, "absent")
	f.writeRecoveryStageEvidence() // other cases neither write nor require a directory
}

// The only additional same-key attempt is the independently issued, closed
// changed-meaning counterexample. Every original successful replay still uses
// the exact original browser Session/body and is checked by the existing oracle.
func projectWorkChangedMeaningMatches(original, observed projectWorkPlanningWebObservation, domain string, privateCSRF [32]byte) bool {
	if domain != "structure" && domain != "task" && domain != "blocker" {
		return false
	}
	if observed.Method != original.Method || observed.RawPath != original.RawPath || observed.RawQuery != "" || observed.ProjectID != original.ProjectID || observed.TargetID != original.TargetID || observed.Domain != domain || observed.Command != original.Command || observed.Key != original.Key || observed.CSRF != privateCSRF || observed.Status != http.StatusConflict {
		return false
	}
	var source, changed, problem map[string]any
	if json.Unmarshal(original.Body, &source) != nil || json.Unmarshal(observed.Body, &changed) != nil || json.Unmarshal(observed.Response, &problem) != nil || problem["code"] != "IDEMPOTENCY_KEY_REUSED" {
		return false
	}
	request, ok := source["request"].(map[string]any)
	if !ok {
		return false
	}
	field := "title"
	if domain == "blocker" {
		field = "description"
	}
	request[field] = "不同含义不得提交"
	return reflect.DeepEqual(source, changed)
}

func TestProjectWorkRecoveryIPCClosedInputs(t *testing.T) {
	for _, sample := range []struct{ action, project string }{
		{"arm-recovery-stage", "not-observed"}, {"arm-recovery-stage", "in-progress"},
		{"observe-recovery-stage", "not-observed"}, {"observe-recovery-stage", "in-progress"},
		{"complete-planned-original", "in-progress"},
		{"reject-changed-meaning", "structure"}, {"reject-changed-meaning", "task"}, {"reject-changed-meaning", "blocker"},
	} {
		t.Run(sample.action+"/"+sample.project, func(t *testing.T) {
			original := projectWorkPlanningWebIPC{Action: sample.action, Project: sample.project}
			if !projectWorkRecoveryRequest("recovery", original) || projectWorkRecoveryRequest("identity", original) {
				t.Fatal("closed recovery mode mismatch")
			}
			for _, change := range []func(*projectWorkPlanningWebIPC){
				func(r *projectWorkPlanningWebIPC) { r.Project = "main" },
				func(r *projectWorkPlanningWebIPC) { r.Action = "unknown" },
				func(r *projectWorkPlanningWebIPC) { r.Domain = "structure" },
				func(r *projectWorkPlanningWebIPC) { r.Resource = "milestone" },
				func(r *projectWorkPlanningWebIPC) { r.Target = "caller-target" },
				func(r *projectWorkPlanningWebIPC) { v := "caller-body"; r.Text = &v },
			} {
				value := original
				change(&value)
				if projectWorkRecoveryRequest("recovery", value) {
					t.Fatal("extra input changed the owned recovery stimulus")
				}
			}
		})
	}
	if projectWorkRecoveryRequest("recovery", projectWorkPlanningWebIPC{Action: "complete-planned-original", Project: "not-observed"}) {
		t.Fatal("not_observed cannot invoke private planned continuation")
	}
}

func TestProjectWorkChangedMeaningClosedException(t *testing.T) {
	private := sha256.Sum256([]byte("private-test-session"))
	for _, domain := range []string{"structure", "task", "blocker"} {
		t.Run(domain, func(t *testing.T) {
			field := "title"
			if domain == "blocker" {
				field = "description"
			}
			original := projectWorkPlanningWebObservation{Method: http.MethodPatch, RawPath: "/exact-original", ProjectID: "project", TargetID: "target", Command: "original-command", Domain: domain, Key: "original-key", Body: []byte(`{"expected_version":"2","request":{"` + field + `":"original"}}`)}
			actual := original
			actual.Status, actual.CSRF = http.StatusConflict, private
			actual.Body = []byte(`{"expected_version":"2","request":{"` + field + `":"不同含义不得提交"}}`)
			actual.Response = []byte(`{"code":"IDEMPOTENCY_KEY_REUSED"}`)
			if !projectWorkChangedMeaningMatches(original, actual, domain, private) {
				t.Fatal("exact counterexample rejected")
			}
			for _, mutate := range []func(*projectWorkPlanningWebObservation){
				func(o *projectWorkPlanningWebObservation) { o.Key = "new-key" },
				func(o *projectWorkPlanningWebObservation) { o.RawPath = "/other" },
				func(o *projectWorkPlanningWebObservation) { o.RawQuery = "changed=1" },
				func(o *projectWorkPlanningWebObservation) { o.Method = http.MethodDelete },
				func(o *projectWorkPlanningWebObservation) { o.CSRF = [32]byte{} },
				func(o *projectWorkPlanningWebObservation) { o.Status = 200 },
				func(o *projectWorkPlanningWebObservation) { o.Response = []byte(`{"code":"VERSION_CONFLICT"}`) },
				func(o *projectWorkPlanningWebObservation) { o.Body = original.Body },
				func(o *projectWorkPlanningWebObservation) {
					o.Body = []byte(`{"expected_version":"3","request":{"` + field + `":"不同含义不得提交"}}`)
				},
			} {
				candidate := actual
				mutate(&candidate)
				if projectWorkChangedMeaningMatches(original, candidate, domain, private) {
					t.Fatal("changed meaning exception admitted an unrelated failure")
				}
			}
		})
	}
}
