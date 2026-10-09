//go:build integration

package account_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	workhttp "github.com/LunaDeerTech/agenteam/internal/central/work/http"
)

// Raw bodies, command keys and correlation are retained only in this process.
// Independent Go assertions can inspect exact facts without trusting a fixture success flag.
type projectWorkPlanningWebObservation struct {
	Method, RawPath, RawQuery, ProjectID, TargetID, Domain, Command, Key string
	CSRF                                                                 [32]byte
	Body, Response, StoredReceipt                                        []byte
	Status                                                               int
	Cut                                                                  projectOwnerWebLossObservation
}
type projectWorkPlanningWebRequestKey struct{}
type projectWorkPlanningWebSeed struct {
	ProjectID   string `json:"project_id"`
	MilestoneID string `json:"milestone_id"`
	SprintID    string `json:"sprint_id"`
	TaskID      string `json:"task_id"`
	RelatedID   string `json:"related_id"`
	BlockerID   string `json:"blocker_id"`
}
type projectWorkPlanningWebFixture struct {
	ctx context.Context
	*projectOwnerWebFixture
	mode                    string
	pendingProject          foundation.ID[identity.Project]
	readCursorMutation      *projectWorkPlanningWebObservation
	cutInvalid              bool
	readCanceled            bool
	planningBaseline        map[string]any
	recoveryBaselines       map[string]map[string]any
	recoveryStages          map[string]*projectWorkRecoveryStage
	changedMeaning          map[string]*projectWorkPlanningWebObservation
	identityFacts           projectWorkIdentityFacts
	activitySession         string
	activityBefore          time.Time
	guard                   sync.Mutex
	enabled                 bool
	records                 []projectWorkPlanningWebObservation
	bytes                   int
	seeds                   map[string]projectWorkPlanningWebSeed
	lossProject, lossDomain string
	hold                    *projectOwnerWebHold
	dropped, replayed       int
	lost                    map[string]projectWorkPlanningWebObservation
}

var projectWorkPlanningWebCases = map[string]bool{
	"read": true, "planning": true, "blockers": true, "recovery": true, "identity": true, "layouts": true,
	"independent-recovery": true, "independent-authority": true,
}

func newProjectWorkPlanningWebFixture(t *testing.T, ctx context.Context, mode string) *projectWorkPlanningWebFixture {
	t.Helper()
	if !projectWorkPlanningWebCases[mode] {
		t.Fatal("closed Work browser case required")
	}
	f := &projectWorkPlanningWebFixture{ctx: ctx, mode: mode, seeds: map[string]projectWorkPlanningWebSeed{}, lost: map[string]projectWorkPlanningWebObservation{}, recoveryStages: map[string]*projectWorkRecoveryStage{}, changedMeaning: map[string]*projectWorkPlanningWebObservation{}}
	if mode == "read" {
		f.pendingProject = id[identity.Project](t)
	}
	newProjectOwnerWebFixtureWithWork(t, ctx, "work-"+mode, f)
	t.Cleanup(func() {
		f.releaseRead()
		f.stopProxy()
		f.guard.Lock()
		defer f.guard.Unlock()
		for i := range f.records {
			clear(f.records[i].Body)
			clear(f.records[i].Response)
			clear(f.records[i].StoredReceipt)
			f.records[i].Key = ""
		}
		for k, v := range f.lost {
			clear(v.Body)
			clear(v.Response)
			clear(v.StoredReceipt)
			delete(f.lost, k)
		}
		for key, stage := range f.recoveryStages {
			if stage.Original != nil {
				clear(stage.Original.Body)
				stage.Original.Key = ""
			}
			delete(f.recoveryStages, key)
		}
		for key, changed := range f.changedMeaning {
			clear(changed.Body)
			changed.Key = ""
			delete(f.changedMeaning, key)
		}
		f.records = nil
	})
	// Every seed below crosses the default root's formal Work HTTP and current
	// Account cookie boundary; there is no second Work service/catalog or SQL seed.
	keys := []string{"main", "duplicate"}
	if mode == "recovery" || mode == "independent-recovery" {
		keys = []string{"structure", "task", "blocker"}
	}
	if mode == "recovery" {
		keys = append(keys, "not-observed", "in-progress")
	}
	for _, key := range keys {
		if f.ids[key] == "" {
			f.ids[key] = f.create(ctx, f.ownerActor, "work."+key).ID.String()
		}
		f.seeds[key] = f.seed(ctx, key)
		f.initial[key] = f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, projectOwnerWebPath+"/"+f.ids[key], nil, "", false, http.StatusOK)
	}
	if mode == "read" || mode == "layouts" {
		dotted := f.create(ctx, f.ownerActor, "work.dot-project")
		f.ids["dotted"] = dotted.ID.String()
		f.seeds["dotted"] = f.seed(ctx, "dotted")
		f.initial["dotted"] = f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, projectOwnerWebPath+"/"+dotted.ID.String(), nil, "", false, http.StatusOK)
	}
	if mode == "read" {
		// The exact private Skills dependency intentionally remains pending for
		// this one ID. The real Project service still owns every creation fact.
		pending, err := f.projects.CreateProject(ctx, f.ownerActor, foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(id[struct{}](t).String())}, pc.CreateProjectRequest{ProjectID: f.pendingProject, Name: "work.pending"})
		if err != nil || pending.State != pc.CreationPending || pending.Operation == nil || pending.Operation.ProjectID != f.pendingProject {
			t.Fatal("formal pending Project stimulus unavailable", err)
		}
		f.ids["pending"] = f.pendingProject.String()
		for n := 0; n < 50; n++ {
			f.command(ctx, "main", http.MethodPost, "milestones", map[string]any{"request": map[string]any{"milestone_id": id[struct{}](t).String(), "title": "分页里程碑"}})
		}
	}
	if mode == "blockers" {
		seed := f.seeds["main"]
		version := "1"
		for n := 0; n < 51; n++ {
			blocker := id[struct{}](t).String()
			if n == 0 {
				blocker = seed.BlockerID
			}
			receipt := f.command(ctx, "main", http.MethodPost, "tasks/"+seed.TaskID+"/blockers", map[string]any{
				"expected_version": version,
				"request":          map[string]any{"blocker_id": blocker, "type": "waiting_for_human", "metadata": map[string]any{}, "description": "分页人工阻塞"},
			})
			version = httpString(t, httpObject(t, receipt, "task"), "version")
		}
	}
	if mode == "planning" {
		f.planningBaseline = f.facts(ctx, f.seeds["main"].ProjectID)
	}
	if mode == "recovery" || mode == "independent-recovery" {
		f.recoveryBaselines = make(map[string]map[string]any, len(f.seeds))
		for key, seed := range f.seeds {
			f.recoveryBaselines[key] = f.facts(ctx, seed.ProjectID)
		}
	}
	f.private("project-work-planning-material.json", map[string]any{"admin": f.admin, "owner": f.owner, "other": f.other, "ids": f.ids, "projects": f.initial, "work": f.seeds})
	f.guard.Lock()
	f.enabled = true
	f.guard.Unlock()
	return f
}
func (f *projectWorkPlanningWebFixture) command(ctx context.Context, key, method, suffix string, body any) map[string]any {
	f.t.Helper()
	return f.setup.setupRequest(ctx, f.ownerClient, method, projectOwnerWebPath+"/"+f.ids[key]+"/"+suffix, body, f.ownerCSRF, true, http.StatusOK)
}
func (f *projectWorkPlanningWebFixture) seed(ctx context.Context, key string) projectWorkPlanningWebSeed {
	t := f.t
	s := projectWorkPlanningWebSeed{ProjectID: f.ids[key], MilestoneID: id[struct{}](t).String(), SprintID: id[struct{}](t).String(), TaskID: id[struct{}](t).String(), RelatedID: id[struct{}](t).String(), BlockerID: id[struct{}](t).String()}
	f.command(ctx, key, http.MethodPost, "milestones", map[string]any{"request": map[string]any{"milestone_id": s.MilestoneID, "title": "规划里程碑", "description": "初始里程碑"}})
	f.command(ctx, key, http.MethodPost, "sprints", map[string]any{"request": map[string]any{"sprint_id": s.SprintID, "milestone_id": s.MilestoneID, "title": "规划 Sprint"}})
	for _, target := range []string{s.TaskID, s.RelatedID} {
		priority := "medium"
		if f.mode == "planning" && target == s.RelatedID {
			priority = "high"
		}
		f.command(ctx, key, http.MethodPost, "tasks", map[string]any{"request": map[string]any{"task_id": target, "sprint_id": s.SprintID, "title": "规划任务", "type": "task", "priority": priority, "plan": "初始 Plan"}})
	}
	return s
}
func (f *projectWorkPlanningWebFixture) handlesRequest(r *http.Request) bool {
	f.guard.Lock()
	enabled := f.enabled
	f.guard.Unlock()
	return enabled && r != nil && workhttp.HandlesPath(r.URL.Path)
}
func (f *projectWorkPlanningWebFixture) handlesResponse(r *http.Response) bool {
	return r != nil && r.Request != nil && r.Request.Context().Value(projectWorkPlanningWebRequestKey{}) != nil
}
func (f *projectWorkPlanningWebFixture) observeRequest(w http.ResponseWriter, r *http.Request) bool {
	var raw []byte
	if r.Body != nil {
		var err error
		raw, err = io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
		closed := r.Body.Close()
		if err != nil || closed != nil || len(raw) > 1024*1024 {
			clear(raw)
			http.Error(w, "owned Work request observation failed", http.StatusBadRequest)
			return false
		}
		r.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		http.Error(w, "owned Work path unavailable", http.StatusBadRequest)
		return false
	}
	obs := projectWorkPlanningWebObservation{Method: r.Method, RawPath: r.URL.EscapedPath(), RawQuery: r.URL.RawQuery, ProjectID: parts[3], Key: r.Header.Get("Idempotency-Key"), CSRF: sha256.Sum256([]byte(r.Header.Get("X-CSRF-Token"))), Body: append([]byte(nil), raw...)}
	entity := parts[4]
	obs.Domain = "structure"
	if entity == "tasks" || entity == "task-commands" {
		obs.Domain = "task"
	}
	if entity == "tasks" && len(parts) > 6 && (parts[6] == "blockers" || parts[6] == "blocker-commands") {
		obs.Domain = "blocker"
	}
	if len(parts) > 5 {
		obs.TargetID = parts[5]
	}
	if r.Method != http.MethodGet {
		var body struct {
			Command string                     `json:"command"`
			Target  string                     `json:"target_id"`
			Request map[string]json.RawMessage `json:"request"`
		}
		if json.Unmarshal(raw, &body) != nil {
			http.Error(w, "owned Work request JSON unavailable", http.StatusBadRequest)
			return false
		}
		if strings.HasSuffix(r.URL.Path, "/lookup") {
			obs.Command = body.Command
			if obs.Domain != "blocker" {
				obs.TargetID = body.Target
				for _, kind := range []string{"milestone", "sprint", "task"} {
					if body.Command == "work."+kind+".create" {
						_ = json.Unmarshal(body.Request[kind+"_id"], &obs.TargetID)
					}
				}
			}
		} else if obs.Domain == "blocker" {
			obs.Command = "work.task.blocker.add"
			if strings.HasSuffix(r.URL.Path, "/resolve") {
				obs.Command = "work.task.blocker.resolve"
			}
		} else {
			singular := strings.TrimSuffix(entity, "s")
			action := "create"
			if r.Method == http.MethodPatch {
				action = "update"
			} else if strings.HasSuffix(r.URL.Path, "/reorder") {
				action = "reorder"
			}
			obs.Command = "work." + singular + "." + action
			if action == "create" {
				_ = json.Unmarshal(body.Request[singular+"_id"], &obs.TargetID)
			}
		}
	}
	f.guard.Lock()
	if len(f.records) >= 256 || f.bytes+len(raw) > 32*1024*1024 {
		clear(obs.Body)
		f.guard.Unlock()
		http.Error(w, "owned Work observation bound reached", http.StatusServiceUnavailable)
		return false
	}
	index := len(f.records)
	f.records = append(f.records, obs)
	f.bytes += len(raw)
	*r = *r.WithContext(context.WithValue(r.Context(), projectWorkPlanningWebRequestKey{}, index))
	f.guard.Unlock()
	proceed, err := f.recoveryBeforeForward(w, r, obs)
	if err != nil {
		f.t.Error("owned recovery before-forward stimulus failed")
		http.Error(w, "owned recovery stimulus unavailable", http.StatusServiceUnavailable)
		return false
	}
	return proceed
}

func TestProjectWorkObservationLookupTargets(t *testing.T) {
	const project = "01900000-0000-7000-8000-000000000001"
	const target = "01900000-0000-7000-8000-000000000002"
	const task = "01900000-0000-7000-8000-000000000003"
	for _, example := range []struct{ domain, command, path, body, want string }{
		{"structure", "work.milestone.create", "structure-commands/lookup", `{"command":"work.milestone.create","request":{"milestone_id":"` + target + `"}}`, target},
		{"structure", "work.sprint.create", "structure-commands/lookup", `{"command":"work.sprint.create","request":{"sprint_id":"` + target + `"}}`, target},
		{"structure", "work.milestone.update", "structure-commands/lookup", `{"command":"work.milestone.update","target_id":"` + target + `","expected_version":"2","request":{}}`, target},
		{"structure", "work.sprint.reorder", "structure-commands/lookup", `{"command":"work.sprint.reorder","target_id":"` + target + `","expected_version":"2","request":{}}`, target},
		{"task", "work.task.create", "task-commands/lookup", `{"command":"work.task.create","request":{"task_id":"` + target + `"}}`, target},
		{"task", "work.task.update", "task-commands/lookup", `{"command":"work.task.update","target_id":"` + target + `","expected_version":"2","request":{}}`, target},
		{"blocker", "work.task.blocker.add", "tasks/" + task + "/blocker-commands/lookup", `{"command":"work.task.blocker.add","request":{"blocker_id":"` + target + `"}}`, task},
		{"blocker", "work.task.blocker.resolve", "tasks/" + task + "/blocker-commands/lookup", `{"command":"work.task.blocker.resolve","request":{"blocker_id":"` + target + `"}}`, task},
	} {
		t.Run(example.command, func(t *testing.T) {
			f := &projectWorkPlanningWebFixture{}
			r := httptest.NewRequest(http.MethodPost, projectOwnerWebPath+"/"+project+"/"+example.path, strings.NewReader(example.body))
			if !f.observeRequest(httptest.NewRecorder(), r) {
				t.Fatal("bounded local observation rejected")
			}
			got := f.observations()
			if len(got) != 1 || got[0].Domain != example.domain || got[0].Command != example.command || got[0].TargetID != example.want || !bytes.Equal(got[0].Body, []byte(example.body)) {
				t.Fatal("original Lookup target or immutable request projection lost")
			}
			if err := r.Body.Close(); err != nil {
				t.Fatal("observed request body close failed")
			}
		})
	}
}
func (f *projectWorkPlanningWebFixture) controlResponse(response *http.Response) error {
	index, ok := response.Request.Context().Value(projectWorkPlanningWebRequestKey{}).(int)
	if !ok {
		return errors.New("owned Work response correlation unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 5*1024*1024+1))
	closed := response.Body.Close()
	if err != nil || closed != nil || len(raw) > 5*1024*1024 || !json.Valid(raw) {
		clear(raw)
		return errors.New("owned Work response incomplete")
	}
	response.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	// The existing safe-body writer stores this same completed response. No
	// original request bytes or credentials enter its public evidence sidecar.
	if err := f.saveResponse(response, raw); err != nil {
		return err
	}
	f.guard.Lock()
	if index < 0 || index >= len(f.records) || f.bytes+len(raw) > 32*1024*1024 {
		f.guard.Unlock()
		return errors.New("owned Work response observation bound")
	}
	f.records[index].Status = response.StatusCode
	f.records[index].Response = append([]byte(nil), raw...)
	f.bytes += len(raw)
	obs := f.records[index]
	hold := f.hold
	if obs.Method != http.MethodGet || hold == nil || hold.path != response.Request.URL.Path || hold.started {
		hold = nil
	} else {
		hold.started = true
	}
	drop := obs.Method != http.MethodGet && !strings.HasSuffix(obs.RawPath, "/lookup") && response.StatusCode == http.StatusOK && f.lossProject == obs.ProjectID && f.lossDomain == obs.Domain
	if original, found := f.lost[obs.Domain]; found && original.Key == obs.Key && response.StatusCode == http.StatusOK && bytes.Equal(original.Body, obs.Body) && obs.Method != http.MethodGet && !strings.HasSuffix(obs.RawPath, "/lookup") {
		f.replayed++
	}
	f.guard.Unlock()
	if hold != nil {
		// Root body and Close have already joined. This is only a transport barrier.
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-hold.release:
		case <-response.Request.Context().Done():
		case <-timer.C:
		}
		timer.Stop()
		f.guard.Lock()
		hold.finished = true
		f.readCanceled = response.Request.Context().Err() != nil
		f.guard.Unlock()
	}
	if !drop {
		return nil
	}
	table := workCommandTable(obs.Domain)
	var state string
	var stored []byte
	if table == "" {
		return errors.New("owned Work command domain unavailable")
	}
	if err := f.store.QueryRow(response.Request.Context(), `SELECT state,receipt FROM `+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, obs.ProjectID, obs.Command, obs.Key).Scan(&state, &stored); err != nil || state != "completed" || !json.Valid(stored) {
		return errors.New("actual completed Work command missing before truncation")
	}
	f.guard.Lock()
	f.records[index].StoredReceipt = append([]byte(nil), stored...)
	obs = f.records[index]
	f.lost[obs.Domain] = obs
	f.lossProject, f.lossDomain = "", ""
	f.dropped++
	f.guard.Unlock()
	_ = response.Body.Close()
	return &projectOwnerWebLost{header: response.Header.Clone(), length: len(raw), observe: func(cut projectOwnerWebLossObservation) {
		f.guard.Lock()
		defer f.guard.Unlock()
		original, found := f.lost[obs.Domain]
		if !found || original.Key != obs.Key || index >= len(f.records) || f.records[index].Key != obs.Key {
			f.cutInvalid = true
			return
		}
		original.Cut = cut
		f.lost[obs.Domain] = original
		f.records[index].Cut = cut
	}}
}
func workCommandTable(domain string) string {
	switch domain {
	case "structure":
		return "agenteam_work.structure_commands"
	case "task":
		return "agenteam_work.task_commands"
	case "blocker":
		return "agenteam_work.task_blocker_commands"
	}
	return ""
}
func (f *projectWorkPlanningWebFixture) observations() []projectWorkPlanningWebObservation {
	f.guard.Lock()
	defer f.guard.Unlock()
	out := append([]projectWorkPlanningWebObservation(nil), f.records...)
	for i := range out {
		out[i].Body = append([]byte(nil), out[i].Body...)
		out[i].Response = append([]byte(nil), out[i].Response...)
		out[i].StoredReceipt = append([]byte(nil), out[i].StoredReceipt...)
	}
	return out
}
func (f *projectWorkPlanningWebFixture) redact(value string) string {
	f.guard.Lock()
	defer f.guard.Unlock()
	for _, r := range f.records {
		if r.Key != "" {
			value = strings.ReplaceAll(value, r.Key, "[redacted]")
		}
	}
	return value
}
func (f *projectWorkPlanningWebFixture) releaseRead() {
	f.guard.Lock()
	defer f.guard.Unlock()
	if f.hold != nil {
		f.hold.once.Do(func() { close(f.hold.release) })
	}
}

type projectWorkPlanningWebIPC struct {
	Sequence int     `json:"sequence"`
	Action   string  `json:"action"`
	Project  string  `json:"project,omitempty"`
	Domain   string  `json:"domain,omitempty"`
	Resource string  `json:"resource,omitempty"`
	Target   string  `json:"target,omitempty"`
	Text     *string `json:"text,omitempty"`
}

func (f *projectWorkPlanningWebFixture) ipc(ctx context.Context, r projectWorkPlanningWebIPC) map[string]any {
	key := r.Project
	if key == "" {
		key = "main"
	}
	seed, ok := f.seeds[key]
	if !ok {
		f.t.Fatal("owned Work IPC project rejected")
	}
	out := map[string]any{"sequence": r.Sequence}
	switch r.Action {
	case "arm-recovery-stage", "observe-recovery-stage", "complete-planned-original", "reject-changed-meaning":
		return f.recoveryIPC(ctx, r)
	case "identity-revoked", "identity-expire", "identity-rename", "identity-reuse-name":
		return f.identityIPC(ctx, r)
	case "age-activity":
		if f.mode != "planning" || f.activitySession != "" {
			f.t.Fatal("owned planning Activity stimulus unavailable")
		}
		if _, err := foundation.ParseID[identity.Session](r.Target); err != nil {
			f.t.Fatal("owned planning Activity Session invalid")
		}
		// Age only the legitimately issued browser Session under its real User
		// lock, crossing Account's existing 60s throttle without sleeping or
		// fabricating identity. No Work row or business result is changed.
		lock, err := foundation.UserLock(f.owner.UserID)
		if err != nil {
			f.t.Fatal("owned Activity User lock invalid")
		}
		result := f.store.WithinTx(ctx, cause(f.t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			return x.QueryRow(ctx, `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '2 minutes',last_activity_at=clock_timestamp()-interval '90 seconds' WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND absolute_expires_at>clock_timestamp() RETURNING last_activity_at`, r.Target, f.owner.UserID).Scan(&f.activityBefore)
		})
		if result.State() != foundation.Committed || result.Fault() != nil {
			f.t.Fatal("owned Activity aging did not commit")
		}
		f.activitySession = r.Target
	case "arm-loss":
		if workCommandTable(r.Domain) == "" {
			f.t.Fatal("owned Work loss domain rejected")
		}
		f.guard.Lock()
		f.lossProject, f.lossDomain = seed.ProjectID, r.Domain
		f.guard.Unlock()
	case "fail-session":
		f.mu.Lock()
		f.failSession = true
		f.mu.Unlock()
	case "hold-read":
		suffix := ""
		switch r.Resource {
		case "milestone":
			suffix = "milestones/" + seed.MilestoneID
		case "sprint":
			suffix = "sprints/" + seed.SprintID
		case "task":
			suffix = "tasks/" + seed.TaskID
		default:
			f.t.Fatal("owned Work read hold rejected")
		}
		f.releaseRead()
		f.guard.Lock()
		f.hold = &projectOwnerWebHold{path: projectOwnerWebPath + "/" + seed.ProjectID + "/" + suffix, release: make(chan struct{})}
		f.guard.Unlock()
	case "hold-status":
		f.guard.Lock()
		out["started"], out["finished"] = f.hold != nil && f.hold.started, f.hold != nil && f.hold.finished
		f.guard.Unlock()
	case "release-read":
		f.releaseRead()
	case "archive":
		f.lifecycle(ctx, key, pc.Archived)
		out["fact_only"] = true
	case "reorder-read-page":
		if f.mode != "read" || key != "main" || r.Resource != "" || r.Domain != "" || r.Target != "" || r.Text != nil || f.readCursorMutation != nil {
			f.t.Fatal("read cursor stimulus must be the one exact external Milestone reorder")
		}
		listPath := projectOwnerWebPath + "/" + seed.ProjectID + "/milestones?limit=2"
		before := httpItems(f.t, f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, listPath, nil, "", false, http.StatusOK))
		if len(before) != 2 {
			f.t.Fatal("read cursor stimulus requires two actual ordered Milestones")
		}
		first, firstOK := before[0].(map[string]any)
		second, secondOK := before[1].(map[string]any)
		if !firstOK || !secondOK || first["id"] != seed.MilestoneID || second["id"] == seed.MilestoneID {
			f.t.Fatal("read cursor stimulus actual first two Milestones differ from its seed")
		}
		target := httpString(f.t, second, "id")
		suffix := "milestones/" + target + "/reorder"
		receipt := f.command(ctx, key, http.MethodPost, suffix, map[string]any{"expected_version": httpString(f.t, second, "version"), "request": map[string]any{"before_id": seed.MilestoneID}})
		if receipt["changed"] != true || httpObject(f.t, receipt, "milestone")["id"] != target {
			f.t.Fatal("read cursor stimulus did not change the actual Milestone order")
		}
		after := httpItems(f.t, f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, listPath, nil, "", false, http.StatusOK))
		if len(after) != 2 {
			f.t.Fatal("read cursor stimulus lost its actual ordered pair")
		}
		moved, movedOK := after[0].(map[string]any)
		anchor, anchorOK := after[1].(map[string]any)
		if !movedOK || !anchorOK || moved["id"] != target || anchor["id"] != seed.MilestoneID {
			f.t.Fatal("read cursor stimulus actual re-read did not reverse the pair")
		}
		out["receipt"] = receipt

		// The private preparation client has its own genuine Session/CSRF.
		// Bind its returned receipt to the exact observed request/key; do not
		// exempt arbitrary mutations or browser writes from the read-only gate.
		for _, observed := range f.observations() {
			if observed.Method != http.MethodPost || observed.Status != http.StatusOK || observed.Domain != "structure" || observed.Command != "work.milestone.reorder" || observed.ProjectID != seed.ProjectID || observed.TargetID != target || observed.RawPath != projectOwnerWebPath+"/"+seed.ProjectID+"/"+suffix || observed.Key == "" || observed.CSRF != sha256.Sum256([]byte(f.ownerCSRF)) {
				continue
			}
			var sameBody map[string]any
			if json.Unmarshal(observed.Response, &sameBody) != nil || !reflect.DeepEqual(sameBody, receipt) {
				continue
			}
			if f.readCursorMutation != nil {
				f.t.Fatal("external cursor stimulus matched more than one observed request")
			}
			f.readCursorMutation = &observed
		}
		if f.readCursorMutation == nil {
			f.t.Fatal("external cursor stimulus has no exact original observed request")
		}
	case "update":
		if f.mode == "read" {
			f.t.Fatal("read cursor stimulus requires a real reorder, not a content update")
		}
		if r.Text == nil {
			f.t.Fatal("owned Work update text required")
		}
		suffix := ""
		field := "title"
		switch r.Resource {
		case "milestone":
			suffix = "milestones/" + seed.MilestoneID
		case "sprint":
			suffix = "sprints/" + seed.SprintID
		case "task":
			suffix = "tasks/" + seed.TaskID
			field = "plan"
		default:
			f.t.Fatal("owned Work update resource rejected")
		}
		if r.Target != "" {
			if _, err := foundation.ParseID[struct{}](r.Target); err != nil {
				f.t.Fatal("owned Work update target invalid")
			}
			prefix, _, _ := strings.Cut(suffix, "/")
			suffix = prefix + "/" + r.Target
		}
		current := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, projectOwnerWebPath+"/"+seed.ProjectID+"/"+suffix, nil, "", false, http.StatusOK)
		receipt := f.command(ctx, key, http.MethodPatch, suffix, map[string]any{"expected_version": current["version"], "request": map[string]any{field: *r.Text}})
		out["receipt"] = receipt
	case "resolve":
		target := r.Target
		if _, err := foundation.ParseID[struct{}](target); err != nil {
			f.t.Fatal("owned Work resolve target invalid")
		}
		current := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, projectOwnerWebPath+"/"+seed.ProjectID+"/tasks/"+seed.TaskID, nil, "", false, http.StatusOK)
		var comment any
		if r.Text != nil {
			comment = *r.Text
		}
		out["receipt"] = f.command(ctx, key, http.MethodPost, "tasks/"+seed.TaskID+"/blockers/resolve", map[string]any{"expected_version": current["version"], "request": map[string]any{"blocker_id": target, "resolution_comment": comment}})
	case "observe":
		out["facts"] = f.facts(ctx, seed.ProjectID)
		f.guard.Lock()
		out["dropped"], out["replayed"], out["requests"] = f.dropped, f.replayed, len(f.records)
		f.guard.Unlock()
	default:
		f.t.Fatal("owned Work IPC action rejected")
	}
	return out
}
func (f *projectWorkPlanningWebFixture) facts(ctx context.Context, project string) map[string]any {
	f.t.Helper()
	if _, err := foundation.ParseID[identity.Project](project); err != nil {
		f.t.Fatal("owned Work fact project invalid")
	}
	var structure, tasks, blockers, history, events int
	err := f.store.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1 AND state='completed'),
 (SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1 AND state='completed'),
 (SELECT count(*) FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND state='completed'),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='work')`, project).Scan(&structure, &tasks, &blockers, &history, &events)
	if err != nil {
		f.t.Fatal("owned Work fact observation failed")
	}
	return map[string]any{"structure_commands": structure, "task_commands": tasks, "blocker_commands": blockers, "history": history, "outbox": events}
}

// This controlled writer does not open sockets. Actual downstream truncation
// is separately required by the recovery browser top and recorded callback.
type projectWorkCutWriter struct {
	header http.Header
	mode   string
	trace  []string
	conn   net.Conn
}

func (w *projectWorkCutWriter) Header() http.Header { return w.header }
func (w *projectWorkCutWriter) WriteHeader(code int) {
	if code != http.StatusOK {
		panic("unexpected cut status")
	}
	w.trace = append(w.trace, "status")
}
func (w *projectWorkCutWriter) Write(p []byte) (int, error) {
	w.trace = append(w.trace, "write")
	if string(p) != "{" {
		panic("unexpected cut bytes")
	}
	if w.mode == "write" {
		return 0, errors.New("controlled write")
	}
	if w.mode == "short" {
		return 0, nil
	}
	return len(p), nil
}
func (w *projectWorkCutWriter) FlushError() error {
	w.trace = append(w.trace, "flush")
	if w.mode == "flush" {
		return errors.New("controlled flush")
	}
	return nil
}
func (w *projectWorkCutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.trace = append(w.trace, "hijack")
	if w.mode == "hijack" {
		return nil, nil, errors.New("controlled hijack")
	}
	return &projectWorkCutConn{Conn: w.conn, owner: w}, nil, nil
}

type projectWorkCutConn struct {
	net.Conn
	owner *projectWorkCutWriter
}

func (c *projectWorkCutConn) Close() error {
	c.owner.trace = append(c.owner.trace, "close")
	err := c.Conn.Close()
	if c.owner.mode == "close" {
		return errors.New("controlled close")
	}
	return err
}
func TestProjectWorkLossCutObservation(t *testing.T) {
	for _, mode := range []string{"success", "write", "short", "flush", "hijack", "close", "default-nil"} {
		t.Run(mode, func(t *testing.T) {
			local, peer := net.Pipe()
			t.Cleanup(func() { _ = local.Close(); _ = peer.Close() })
			w := &projectWorkCutWriter{header: make(http.Header), mode: mode, conn: local}
			lost := &projectOwnerWebLost{header: http.Header{"Content-Type": []string{"application/json"}, "Transfer-Encoding": []string{"chunked"}}, length: 123}
			calls := 0
			var got projectOwnerWebLossObservation
			if mode != "default-nil" {
				lost.observe = func(value projectOwnerWebLossObservation) { calls++; got = value }
			}
			writeProjectOwnerWebLost(w, lost)
			want := []string{"status", "write", "flush", "hijack"}
			if mode != "hijack" {
				want = append(want, "close")
			}
			if !reflect.DeepEqual(w.trace, want) || w.header.Get("Content-Length") != "123" || w.header.Get("Connection") != "close" || w.header.Get("Transfer-Encoding") != "" || lost.header.Get("Transfer-Encoding") != "chunked" {
				t.Fatal("cut operation order or framing/default header isolation changed")
			}
			if mode == "default-nil" {
				if calls != 0 {
					t.Fatal("nil callback invoked")
				}
				return
			}
			written := 1
			if mode == "write" || mode == "short" {
				written = 0
			}
			wantCut := projectOwnerWebLossObservation{Written: written, WriteOK: mode != "write", FlushOK: mode != "flush", HijackOK: mode != "hijack", CloseOK: mode != "hijack" && mode != "close"}
			if calls != 1 || got != wantCut {
				t.Fatal("cut return values were not observed exactly once", got, wantCut)
			}
		})
	}
}
