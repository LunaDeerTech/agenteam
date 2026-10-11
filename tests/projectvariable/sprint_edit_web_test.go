//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Edit an existing planned/current Sprint through the original default App and real Owner.
// The explicit setup initializer and all original owner retirement gates remain.
func TestSprintEditWeb(t *testing.T) {
	for _, mode := range []string{"sprint-edit-normal", "sprint-edit-lost"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("Sprint Edit original App handoff incomplete")
				}
			})
			a := newAgentCreateFixture(t)
			original := a.p2.base
			project, err := a.p2.creator.GetSchedulerConfig(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID)
			agentAppRequire(t, err)
			if project.Config.Enabled || project.Project.CurrentSprintID != nil {
				t.Fatal("original Project must be paused without a current Sprint")
			}
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			var username string
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
			if original.afterOwners != nil {
				t.Fatal("original handoff occupied")
			}
			original.afterOwners = func() {
				entered = true
				if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("original setup owners not joined")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("original guard still held")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("original process not retired")
				}
				w := newSprintEditWeb(t, a, project, mode)
				defer w.dispose(t)
				withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
					defer w.close(t)
					w.start(t, address)
					w.prepare(t, ctx)
					w.material(t, username)
					w.browser(t, ctx)
					w.verify(t, ctx)
					w.close(t)
				})
				completed = true
			}
		})
	}
}

const sprintEditSeedTitle = "First delivery sprint"
const sprintEditSeedDescription = "Plan the first Sprint.\nKeep lifecycle explicit."
const sprintEditTitle = "  e\u0301 Delivery sprint  "
const sprintEditDescription = "Updated Sprint.\nKeep lifecycle explicit."

type sprintEditIntent struct {
	sprintWebIntent
	before    wc.Sprint
	request   wc.UpdateFields
	expected  f.Version
	requestID f.ID[f.Request]
}
type sprintEditIntentKey struct{}
type sprintEditWeb struct {
	*milestoneCreateWeb
	seed, current                          wc.Sprint
	parent                                 wc.Milestone
	emptyBoards, parentReads               int
	writes                                 []sprintEditIntent
	results                                []wc.StructureMutation
	updates, changed, lookups, gets, lists int
	fixed, afterCommit                     string
}

func (w *sprintEditWeb) prepare(t *testing.T, ctx context.Context) {
	w.prepareEmpty(t, ctx)
	parentInput := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: milestoneCreateTitle, Description: milestoneCreateDescription}
	parentKey := f.IdempotencyKey(id[struct{}](t).String())
	reply := w.request(t, http.MethodPost, variableHTTPPath(w.project.Project.ID, "/milestones"), jsonBytes(t, map[string]any{"request": parentInput}), parentKey)
	var parent wc.StructureMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &parent) != nil || parent.Validate() != nil || parent.Milestone == nil {
		t.Fatal("formal HTTP seed did not create the parent Milestone")
	}
	rid, err := f.ParseID[f.Request](reply.header.Get("X-Request-ID"))
	agentAppRequire(t, err)
	agentAppRequire(t, w.milestoneCreateWeb.committedFacts(ctx, milestoneCreateIntent{sprintWebIntent: sprintWebIntent{key: parentKey}, request: parentInput, requestID: rid}, parent))
	w.mu.Lock()
	w.parent = *parent.Milestone
	w.mu.Unlock()
	input := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: w.parent.ID, Title: sprintEditSeedTitle, Description: sprintEditSeedDescription}
	key := f.IdempotencyKey(id[struct{}](t).String())
	reply = w.request(t, http.MethodPost, variableHTTPPath(w.project.Project.ID, "/sprints"), jsonBytes(t, map[string]any{"request": input}), key)
	var created wc.StructureMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &created) != nil || created.Validate() != nil || created.Sprint == nil {
		t.Fatal("formal HTTP seed did not create the planned Sprint")
	}
	rid, err = f.ParseID[f.Request](reply.header.Get("X-Request-ID"))
	agentAppRequire(t, err)
	agentAppRequire(t, w.seedFacts(ctx, sprintCreateIntent{sprintWebIntent: sprintWebIntent{key: key}, request: input, requestID: rid}, created))
	current := created.Sprint.Clone()
	w.mu.Lock()
	w.before = current.Clone()
	w.mu.Unlock()
	if w.mode == "sprint-edit-lost" {
		startKey := f.IdempotencyKey(id[struct{}](t).String())
		reply = w.request(t, http.MethodPost, w.sprintPath()+"/start", jsonBytes(t, map[string]any{"expected_version": current.Version, "request": map[string]any{}}), startKey)
		var started wc.SprintStartMutation
		if reply.status != 200 || json.Unmarshal(reply.body, &started) != nil || started.Validate() != nil {
			t.Fatal("formal HTTP Start did not establish the current Sprint")
		}
		agentAppRequire(t, w.sprintStartWeb.committedFacts(ctx, sprintWebIntent{kind: "start", key: startKey}, started))
		w.mu.Lock()
		w.project.Project = started.Project.Clone()
		w.mu.Unlock()
		current = started.Sprint.Clone()
	}
	read := w.request(t, http.MethodGet, w.sprintPath(), nil, "")
	var canonical wc.Sprint
	if read.status != 200 || json.Unmarshal(read.body, &canonical) != nil || !reflect.DeepEqual(canonical, current) {
		t.Fatal("formal HTTP seed Get differs from original Create/Start receipt")
	}
	fixed, err := w.fixedFacts(ctx)
	agentAppRequire(t, err)
	w.mu.Lock()
	w.seed, w.current, w.fixed = current.Clone(), current.Clone(), fixed
	w.mu.Unlock()
}

func (w *sprintEditWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.project.Project.ID, "project_version": w.project.Project.Version, "milestone_id": w.parent.ID, "sprint_id": w.seed.ID, "version": w.seed.Version, "route": "/" + username + "/" + w.project.Project.NormalizedName + "/tasks/sprints/" + w.seed.ID.String(), "title": w.seed.Title, "description": w.seed.Description, "updated_title": sprintEditTitle, "updated_description": sprintEditDescription, "sprint": w.seed, "milestone": w.parent, "input_hash": w.inputHash})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "sprint-edit-material.json"), raw, 0600))
	clear(raw)
}

func (w *sprintEditWeb) path() string {
	return variableHTTPPath(w.project.Project.ID, "/sprints/"+w.seed.ID.String())
}
func (w *sprintEditWeb) lookupPath() string {
	return variableHTTPPath(w.project.Project.ID, "/structure-commands/lookup")
}

func (w *sprintEditWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.Method != http.MethodPatch && r.Method != http.MethodPost {
		return errors.New("undeclared Sprint Edit request")
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	closeErr := r.Body.Close()
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if readErr != nil || closeErr != nil || len(raw) > 16<<10 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("original Sprint Edit request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var wire struct {
		Command  wc.CommandName  `json:"command,omitempty"`
		Target   wc.SprintID     `json:"target_id,omitempty"`
		Expected f.Version       `json:"expected_version"`
		Request  wc.UpdateFields `json:"request"`
	}
	var fields map[string]json.RawMessage
	if agentWebDecode(raw, &wire) != nil || wire.Request.Validate() != nil || wire.Expected.Validate() != nil || json.Unmarshal(raw, &fields) != nil {
		return errors.New("Sprint Edit envelope lost exact fields")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	in := sprintEditIntent{sprintWebIntent: sprintWebIntent{key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}, request: wire.Request, expected: wire.Expected, before: w.current}
	if r.Method == http.MethodPatch && r.URL.Path == w.path() {
		if len(fields) != 2 || wire.Command != "" || wire.Target.Validate() == nil || wire.Expected != w.current.Version || len(w.writes) != len(w.results) {
			return errors.New("Sprint Edit changed original target/version or overlapped a write")
		}
		for _, old := range w.writes {
			if old.key == key {
				return errors.New("browser repeated Sprint Edit")
			}
		}
		title, description, empty := sprintEditTitle, sprintEditDescription, ""
		want := wc.UpdateFields{Title: &title, Description: &description}
		switch len(w.writes) {
		case 0:
		case 1:
			if w.mode != "sprint-edit-normal" {
				return errors.New("browser retransmitted uncertain Sprint Edit")
			}
			want = wc.UpdateFields{Title: &title}
		case 2:
			if w.mode != "sprint-edit-normal" {
				return errors.New("browser added an uncertain Sprint Edit")
			}
			want = wc.UpdateFields{Description: &empty}
		default:
			return errors.New("browser added an undeclared Sprint Edit")
		}
		if !reflect.DeepEqual(wire.Request, want) {
			return errors.New("Sprint Edit changed original field presence or Unicode bytes")
		}
		in.kind = "update"
		w.writes = append(w.writes, in)
		w.updates++
	} else if r.Method == http.MethodPost && r.URL.Path == w.lookupPath() {
		if w.mode != "sprint-edit-lost" || len(fields) != 4 || len(w.writes) != 1 || len(w.results) != 1 || w.lookups != 0 || w.cut != 1 {
			return errors.New("Sprint Lookup has no declared uncertain original write")
		}
		original := w.writes[0]
		if wire.Command != wc.SprintUpdate || wire.Target != w.seed.ID || wire.Expected != original.expected || key != original.key || !reflect.DeepEqual(wire.Request, original.request) {
			return errors.New("Sprint Lookup lost original key/version/target/full patch")
		}
		in.kind = "lookup"
		in.before = original.before
		in.requestID = original.requestID
		w.lookups++
	} else {
		return errors.New("browser used a foreign Sprint Edit route")
	}
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), sprintEditIntentKey{}, in))
	return nil
}

func (w *sprintEditWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	in, write := r.Request.Context().Value(sprintEditIntentKey{}).(sprintEditIntent)
	lost := false
	if write {
		if r.StatusCode != 200 {
			return errors.New("original Sprint Edit/Lookup failed")
		}
		switch in.kind {
		case "update":
			var result wc.StructureMutation
			if json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
				return errors.New("original Sprint Edit receipt invalid")
			}
			in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
			if err != nil {
				return err
			}
			if err = w.updateFacts(r.Request.Context(), in, result, true); err != nil {
				return err
			}
			snapshot, err := w.snapshot(r.Request.Context())
			if err != nil {
				return err
			}
			w.mu.Lock()
			w.current = result.Sprint.Clone()
			w.writes[len(w.writes)-1] = in
			w.results = append(w.results, result.Clone())
			if result.Changed {
				w.changed++
			}
			w.afterCommit = snapshot
			lost = w.mode == "sprint-edit-lost"
			w.mu.Unlock()
		case "lookup":
			var found wc.CommandLookup
			w.mu.Lock()
			original, snapshot := w.results[0].Clone(), w.afterCommit
			w.mu.Unlock()
			if json.Unmarshal(raw, &found) != nil || found.Validate() != nil || found.State != wc.LookupCommitted || found.Result == nil || !reflect.DeepEqual(*found.Result, original) {
				return errors.New("Lookup did not recover original historical Sprint receipt")
			}
			if err = w.updateFacts(r.Request.Context(), in, original, false); err != nil {
				return err
			}
			after, err := w.snapshot(r.Request.Context())
			if err != nil {
				return err
			}
			if snapshot != after {
				return errors.New("read-only Sprint Lookup changed original facts")
			}
		default:
			return errors.New("unknown Sprint Edit proof")
		}
	} else if r.Request.Method == http.MethodGet && r.StatusCode == 200 {
		if err = w.observeRead(r.Request, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == 200 && (write || r.Request.Method == http.MethodGet) {
		if err = w.proof(r, raw, in.sprintWebIntent, write); err != nil {
			return err
		}
	}
	// The declared cut follows the complete original App/TLS response and its
	// committed database facts. It changes only delivery to the browser.
	if lost {
		return &reviewWebDrop{r.Header.Clone(), len(raw)}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *sprintEditWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.project.Project.ID
	switch r.URL.Path {
	case variableHTTPPath(p, ""):
		var project pc.ProjectRef
		if json.Unmarshal(raw, &project) != nil || !reflect.DeepEqual(project, w.project.Project) {
			return errors.New("Sprint Edit changed current Project")
		}
		w.projectReads++
	case w.path():
		var current wc.Sprint
		if json.Unmarshal(raw, &current) != nil || !reflect.DeepEqual(current, w.current) {
			return errors.New("Sprint current Get differs from canonical postimage")
		}
		w.gets++
	case variableHTTPPath(p, "/milestones/"+w.parent.ID.String()):
		var parent wc.Milestone
		if json.Unmarshal(raw, &parent) != nil || !reflect.DeepEqual(parent, w.parent) {
			return errors.New("Sprint Edit changed its parent Milestone")
		}
		w.parentReads++
	case variableHTTPPath(p, "/milestones"):
		var page f.Page[map[string]any]
		item, err := sprintCreateSummary(w.parent, "description")
		if err != nil || json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" || !reflect.DeepEqual(page.Items, []map[string]any{item}) {
			return errors.New("Sprint Edit changed parent Milestone list")
		}
	case variableHTTPPath(p, "/sprints"):
		var page f.Page[map[string]any]
		item, err := sprintCreateSummary(w.current, "description", "started_at", "started_by", "completed_at", "completed_by")
		if err != nil || r.URL.Query().Get("milestone_id") != w.parent.ID.String() || json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" || !reflect.DeepEqual(page.Items, []map[string]any{item}) {
			return errors.New("Sprint List lost exact current summary/lifecycle/rank")
		}
		w.lists++
	case variableHTTPPath(p, "/tasks"):
		var page f.Page[map[string]any]
		if r.URL.Query().Get("sprint_id") != w.seed.ID.String() || json.Unmarshal(raw, &page) != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
			return errors.New("Sprint Edit changed its empty Task board")
		}
		w.emptyBoards++
	}
	return nil
}

func (w *sprintEditWeb) fixedFacts(ctx context.Context) (string, error) {
	var facts string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'milestones',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_work.milestones m WHERE project_id=$1::text::uuid),
 'sprint_fixed',(SELECT jsonb_agg(to_jsonb(s)-'title'-'description'-'version'-'updated_at' ORDER BY id) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'starts',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.sprint_start_commands c WHERE project_id=$1::text::uuid),
 'seed_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.structure_commands c WHERE project_id=$1::text::uuid AND command_name<>'work.sprint.update'),
 'seed_events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid AND producer='work' AND (event_type<>'work.sprint_changed' OR aggregate_version=1)),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid),
 'milestone_order',(SELECT to_jsonb(g) FROM agenteam_work.milestone_order_groups g WHERE project_id=$1::text::uuid),
 'sprint_order',(SELECT jsonb_agg(to_jsonb(g) ORDER BY milestone_id) FROM agenteam_work.sprint_order_groups g WHERE project_id=$1::text::uuid))::text`, w.project.Project.ID.String()).Scan(&facts)
	return facts, err
}

func (w *sprintEditWeb) snapshot(ctx context.Context) (string, error) {
	var snapshot string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'sprints',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.structure_commands c WHERE project_id=$1::text::uuid),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid AND producer='work'),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2::uuid))::text`, w.project.Project.ID.String(), w.a.p2.base.ownerBrowser.actor.Details().SessionID).Scan(&snapshot)
	return snapshot, err
}

func (w *sprintEditWeb) updateFacts(ctx context.Context, in sprintEditIntent, result wc.StructureMutation, canonical bool) error {
	if result.Validate() != nil || result.Command != wc.SprintUpdate || result.Sprint == nil || result.Milestone != nil {
		return errors.New("Sprint Update lost original Structure receipt")
	}
	expected := in.before
	var changed []wc.ChangedField
	if in.request.Description != nil && *in.request.Description != expected.Description {
		expected.Description = *in.request.Description
		changed = append(changed, wc.DescriptionChanged)
	}
	if in.request.Title != nil && *in.request.Title != expected.Title {
		expected.Title = *in.request.Title
		changed = append(changed, wc.TitleChanged)
	}
	if len(changed) != 0 {
		expected.Version++
		expected.UpdatedAt = result.Sprint.UpdatedAt
	}
	if in.expected != in.before.Version || !reflect.DeepEqual(*result.Sprint, expected) || result.Changed != (len(changed) != 0) || (result.EventID != nil) != result.Changed {
		return errors.New("Sprint Update altered omitted fields, original bytes, version or no-op semantics")
	}
	actor := w.a.p2.base.ownerBrowser.actor
	semantic, err := wc.UpdateSprintDigest(actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &in.expected}, w.project.Project.ID, w.seed.ID, in.request)
	if err != nil {
		return err
	}
	var commandID, user, digest string
	var eventID *string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.sprint.update' AND idempotency_key=$2 AND state='completed' AND committed_at IS NOT NULL`, w.project.Project.ID.String(), string(in.key)).Scan(&commandID, &eventID, &user, &digest, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var input map[string]any
	var receipt wc.StructureMutation
	want := map[string]any{"command": wc.SprintUpdate, "project_id": w.project.Project.ID, "target_id": w.seed.ID.String(), "actor_user_id": actor.Details().UserID, "expected_version": in.expected, "create_milestone": nil, "create_sprint": nil, "update": in.request, "reorder_milestone": nil, "reorder_sprint": nil}
	if json.Unmarshal(requestRaw, &input) != nil || !sprintCreateEqual(input, want) || json.Unmarshal(receiptRaw, &receipt) != nil || !reflect.DeepEqual(receipt, result) || revision < 1 || user != actor.Details().UserID || digest != semantic.String() {
		return errors.New("Sprint command lost exact original input, actor or receipt")
	}
	if !result.Changed {
		if eventID != nil || len(planRaw) != 0 {
			return errors.New("Sprint no-op created a plan or event")
		}
	} else {
		var plan struct {
			BeforeMilestone *wc.Milestone        `json:"before_milestone"`
			BeforeSprint    *wc.Sprint           `json:"before_sprint"`
			After           wc.StructureMutation `json:"after"`
			GroupBefore     *f.Version           `json:"group_before"`
			Header          eventc.Header        `json:"header"`
			Payload         wc.SprintChanged     `json:"payload"`
		}
		if eventID == nil || *eventID != result.EventID.String() || json.Unmarshal(planRaw, &plan) != nil || plan.BeforeSprint == nil || !reflect.DeepEqual(*plan.BeforeSprint, in.before) || plan.BeforeMilestone != nil || plan.GroupBefore != nil || !reflect.DeepEqual(plan.After, result) {
			return errors.New("Sprint Update plan lost original preimage/postimage")
		}
		h, p := plan.Header, plan.Payload
		if h.Validate() != nil || p.Validate() != nil || h.EventID != *result.EventID || h.EventType != wc.SprintChangedName || h.AggregateType != wc.SprintAggregate || h.AggregateID.String() != w.seed.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != expected.Version || h.AggregateSequence != nil || h.SchemaVersion != wc.WorkSchemaVersion || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.project.Project.ID.String() || h.OccurredAt != expected.UpdatedAt || p.CommandID.String() != commandID || p.ActorUserID.String() != user || p.MilestoneID != w.parent.ID || p.Change != wc.UpdatedChange || !reflect.DeepEqual(p.ChangedFields, changed) || p.Position != nil {
			return errors.New("Sprint event lost exact fields, actor, version or stable rank")
		}
		var headerRaw, payloadRaw []byte
		var columns bool
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.sprint_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.sprint' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5 FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND producer='work'`, w.project.Project.ID.String(), w.seed.ID.String(), *eventID, int64(expected.Version), expected.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &columns)
		if err != nil {
			return err
		}
		actualHeader, err := eventc.DecodeHeader(headerRaw)
		var actualPayload wc.SprintChanged
		if err != nil || !columns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, p) {
			return errors.New("Sprint Outbox differs from original command plan")
		}
	}
	if canonical {
		var matches bool
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT milestone_id=$9::text::uuid AND title=$3 AND description=$4 AND manual_rank=$5 AND version=$6 AND created_at=$7 AND updated_at=$8 FROM agenteam_work.sprints WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.project.Project.ID.String(), w.seed.ID.String(), expected.Title, expected.Description, expected.ManualRank, int64(expected.Version), expected.CreatedAt.Time(), expected.UpdatedAt.Time(), w.parent.ID.String()).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return errors.New("current Sprint row differs from original Update postimage")
		}
	}
	fixed, err := w.fixedFacts(ctx)
	if err != nil {
		return err
	}
	if fixed != w.fixed {
		return errors.New("Sprint Edit changed Project, parent, lifecycle, ordering, Audit or Execution facts")
	}
	return nil
}

func (w *sprintEditWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure, updates, changed, lookups, cuts, gets, lists := w.failure, w.updates, w.changed, w.lookups, w.cut, w.gets, w.lists
	writes := append([]sprintEditIntent(nil), w.writes...)
	results := append([]wc.StructureMutation(nil), w.results...)
	snapshot := w.afterCommit
	boards, parents, projectReads := w.emptyBoards, w.parentReads, w.projectReads
	tails := make([]*reviewWebTail, 0, len(w.tails))
	for _, tail := range w.tails {
		tails = append(tails, tail)
	}
	w.mu.Unlock()
	wantUpdates, wantChanged, wantLookups, wantReads := 3, 2, 0, 7
	if w.mode == "sprint-edit-lost" {
		wantUpdates, wantChanged, wantLookups, wantReads = 1, 1, 1, 3
	}
	if failure != nil || updates != wantUpdates || len(results) != updates || changed != wantChanged || lookups != wantLookups || cuts != wantLookups || gets < wantReads || lists < wantReads || boards < 7 || parents < 1 || projectReads < 1 {
		t.Fatal("Sprint Edit original browser chain incomplete", failure)
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original Sprint Edit handler still running")
		}
		if !tail.returned {
			t.Fatal("original Sprint Edit handler did not return")
		}
	}
	for index, in := range writes {
		agentAppRequire(t, w.updateFacts(ctx, in, results[index], index == len(writes)-1))
	}
	var commands, events, milestones, sprints int64
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND producer='work'),
 (SELECT count(*) FROM agenteam_work.milestones WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.sprints WHERE project_id=$1::text::uuid)`, w.project.Project.ID.String()).Scan(&commands, &events, &milestones, &sprints))
	seedEvents := 2
	if w.mode == "sprint-edit-lost" {
		seedEvents++
	}
	if commands != int64(2+wantUpdates) || events != int64(seedEvents+wantChanged) || milestones != 1 || sprints != 1 {
		t.Fatal("Sprint Edit duplicated command/event or created another Sprint/Milestone")
	}
	after, err := w.snapshot(ctx)
	agentAppRequire(t, err)
	if snapshot == "" || snapshot != after {
		t.Fatal("Sprint current reads or Lookup changed original committed facts")
	}
}

func newSprintEditWeb(t *testing.T, a *agentCreateFixture, project pc.SchedulerProject, mode string) *sprintEditWeb {
	t.Helper()
	const prefix = "AGENTEAM_SPRINT_EDIT_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Sprint Edit inputs required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen index missing")
	}
	directory, err := os.MkdirTemp(runtime, "sew-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &milestoneCreateWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: base, project: project.Clone()}}
	published = true
	return &sprintEditWeb{milestoneCreateWeb: w}
}
func (w *sprintEditWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned Agent Edit TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse, proxy.ErrorHandler = w.response, w.proxyError
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned Sprint Edit observation failed", 500)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if w.dist == "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(out, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(w.dist, clean)); err == nil && info.Mode().IsRegular() {
			static.ServeHTTP(out, r)
			return
		}
		http.ServeFile(out, r, filepath.Join(w.dist, "index.html"))
	})}
	go func() { w.serve <- w.front.Serve(w.listener) }()
}

func (w *sprintEditWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "sprint-edit.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_SPRINT_EDIT_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Sprint Edit Playwright did not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	err = <-done
	joined = true
	t.Logf("Sprint Edit Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Sprint Edit browser call failed")
	}
}

func (w *sprintEditWeb) proof(r *http.Response, raw []byte, intent sprintWebIntent, write bool) error {
	record := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if write {
		record["kind"] = intent.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", intent.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", intent.keyDigest)
		record["committed_verified"] = true
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.evidence, "sprint-edit-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}

func (w *sprintEditWeb) seedFacts(ctx context.Context, in sprintCreateIntent, result wc.StructureMutation) error {
	if result.Validate() != nil || result.Command != wc.SprintCreate || !result.Changed || result.Milestone != nil || result.Sprint == nil || result.EventID == nil {
		return errors.New("Create lost its original complete Structure receipt")
	}
	v := result.Sprint
	expected := wc.Sprint{ID: in.request.SprintID, ProjectID: w.project.Project.ID, MilestoneID: w.parent.ID, Title: in.request.Title, Description: in.request.Description, ManualRank: v.ManualRank, Version: 1, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt, State: wc.Planned}
	if !reflect.DeepEqual(*v, expected) || v.CreatedAt.Time().Before(w.parent.UpdatedAt.Time()) {
		return errors.New("Create did not append a first planned v1 Sprint")
	}
	actor := w.a.p2.base.ownerBrowser.actor
	semantic, err := wc.CreateSprintDigest(actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key}, w.project.Project.ID, in.request)
	if err != nil {
		return err
	}
	var commandID, eventID, user, digest string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.sprint.create' AND idempotency_key=$2 AND state='completed' AND committed_at IS NOT NULL`, w.project.Project.ID.String(), string(in.key)).Scan(&commandID, &eventID, &user, &digest, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var input map[string]any
	var stored wc.StructureMutation
	var plan struct {
		BeforeMilestone *wc.Milestone        `json:"before_milestone"`
		BeforeSprint    *wc.Sprint           `json:"before_sprint"`
		After           wc.StructureMutation `json:"after"`
		GroupBefore     *f.Version           `json:"group_before"`
		Header          eventc.Header        `json:"header"`
		Payload         wc.SprintChanged     `json:"payload"`
	}
	expectedInput := map[string]any{"command": wc.SprintCreate, "project_id": w.project.Project.ID, "target_id": v.ID.String(), "actor_user_id": actor.Details().UserID, "expected_version": nil, "create_milestone": nil, "create_sprint": in.request, "update": nil, "reorder_milestone": nil, "reorder_sprint": nil}
	if json.Unmarshal(requestRaw, &input) != nil || !sprintCreateEqual(input, expectedInput) || json.Unmarshal(receiptRaw, &stored) != nil || !reflect.DeepEqual(stored, result) || json.Unmarshal(planRaw, &plan) != nil || plan.BeforeMilestone != nil || plan.BeforeSprint != nil || plan.GroupBefore == nil || *plan.GroupBefore != w.generation || !reflect.DeepEqual(plan.After, result) || revision < 1 || digest != semantic.String() || user != actor.Details().UserID || eventID != result.EventID.String() {
		return errors.New("Create command/plan differs from original identity and full intent")
	}
	h, p := plan.Header, plan.Payload
	if h.Validate() != nil || p.Validate() != nil || h.EventID != *result.EventID || h.EventType != wc.SprintChangedName || h.AggregateType != wc.SprintAggregate || h.AggregateID.String() != v.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != 1 || h.AggregateSequence != nil || h.SchemaVersion != wc.WorkSchemaVersion || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.project.Project.ID.String() || h.OccurredAt != v.CreatedAt || p.CommandID.String() != commandID || p.ActorUserID.String() != user || p.MilestoneID != w.parent.ID || p.Change != wc.CreatedChange || !reflect.DeepEqual(p.ChangedFields, []wc.ChangedField{wc.DescriptionChanged, wc.RankChanged, wc.TitleChanged}) || p.Position == nil || p.Position.OrderGeneration != w.generation+1 || p.Position.PreviousID != nil || p.Position.NextID != nil {
		return errors.New("Create event lost exact first Sprint position/actor/version")
	}
	var headerRaw, payloadRaw []byte
	var eventColumns bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.sprint_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.sprint' AND aggregate_version=1 AND aggregate_sequence IS NULL AND occurred_at=$4 FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND producer='work'`, w.project.Project.ID.String(), v.ID.String(), eventID, v.CreatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil {
		return err
	}
	actualHeader, err := eventc.DecodeHeader(headerRaw)
	var actualPayload wc.SprintChanged
	if err != nil || !eventColumns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, p) {
		return errors.New("Create Outbox differs from original command plan")
	}
	var canonical bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT milestone_id=$3::text::uuid AND title=$4 AND description=$5 AND manual_rank=$6 AND version=1 AND created_at=$7 AND updated_at=$7 AND started_at IS NULL AND started_by IS NULL AND completed_at IS NULL AND completed_by IS NULL FROM agenteam_work.sprints WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.project.Project.ID.String(), v.ID.String(), w.parent.ID.String(), v.Title, v.Description, v.ManualRank, v.CreatedAt.Time()).Scan(&canonical)
	if err != nil {
		return err
	}
	if !canonical {
		return errors.New("new canonical Sprint differs from receipt")
	}
	var facts [6]int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.sprint.create' AND request->>'target_id'=$2::text),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND producer='work'),
 (SELECT order_generation FROM agenteam_work.sprint_order_groups WHERE project_id=$1::text::uuid AND milestone_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_work.sprint_start_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, w.project.Project.ID.String(), v.ID.String(), w.parent.ID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5])
	if err != nil {
		return err
	}
	if facts != ([6]int64{1, 1, int64(w.generation) + 1, 0, 0, 0}) {
		return errors.New("Create duplicated command/event or started dispatch")
	}
	return nil
}
