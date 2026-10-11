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

// Edit an existing Milestone through the original default App and real Owner.
// The explicit setup initializer and all original owner retirement gates remain.
func TestMilestoneEditWeb(t *testing.T) {
	for _, mode := range []string{"milestone-edit-normal", "milestone-edit-lost"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("Milestone Edit original App handoff incomplete")
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
				w := newMilestoneEditWeb(t, a, project, mode)
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

const milestoneEditTitle = "  e\u0301 Delivery milestone  "
const milestoneEditDescription = "Updated milestone.\nKeep Sprint planning explicit."

type milestoneEditIntent struct {
	sprintWebIntent
	before    wc.Milestone
	request   wc.UpdateFields
	expected  f.Version
	requestID f.ID[f.Request]
}
type milestoneEditIntentKey struct{}
type milestoneEditWeb struct {
	*milestoneCreateWeb
	seed, current                          wc.Milestone
	writes                                 []milestoneEditIntent
	results                                []wc.StructureMutation
	updates, changed, lookups, gets, lists int
	fixed, afterCommit                     string
}

func (w *milestoneEditWeb) prepare(t *testing.T, ctx context.Context) {
	w.prepareEmpty(t, ctx)
	input := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: milestoneCreateTitle, Description: milestoneCreateDescription}
	key := f.IdempotencyKey(id[struct{}](t).String())
	reply := w.request(t, http.MethodPost, variableHTTPPath(w.project.Project.ID, "/milestones"), jsonBytes(t, map[string]any{"request": input}), key)
	var seed wc.StructureMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &seed) != nil || seed.Validate() != nil || seed.Milestone == nil {
		t.Fatal("formal HTTP seed did not create the original Milestone")
	}
	rid, err := f.ParseID[f.Request](reply.header.Get("X-Request-ID"))
	agentAppRequire(t, err)
	in := milestoneCreateIntent{sprintWebIntent: sprintWebIntent{key: key}, request: input, requestID: rid}
	agentAppRequire(t, w.milestoneCreateWeb.committedFacts(ctx, in, seed))
	read := w.request(t, http.MethodGet, variableHTTPPath(w.project.Project.ID, "/milestones/"+seed.Milestone.ID.String()), nil, "")
	var current wc.Milestone
	if read.status != 200 || json.Unmarshal(read.body, &current) != nil || !reflect.DeepEqual(current, *seed.Milestone) {
		t.Fatal("formal HTTP seed Get differs from original Create receipt")
	}
	fixed, err := w.fixedFacts(ctx)
	agentAppRequire(t, err)
	w.mu.Lock()
	w.created, w.seed, w.current, w.fixed = seed.Clone(), current, current, fixed
	w.mu.Unlock()
}

func (w *milestoneEditWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.project.Project.ID, "project_version": w.project.Project.Version, "milestone_id": w.seed.ID, "version": w.seed.Version, "route": "/" + username + "/" + w.project.Project.NormalizedName + "/tasks", "title": w.seed.Title, "description": w.seed.Description, "updated_title": milestoneEditTitle, "updated_description": milestoneEditDescription, "milestone": w.seed, "input_hash": w.inputHash})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "milestone-edit-material.json"), raw, 0600))
	clear(raw)
}

func (w *milestoneEditWeb) path() string {
	return variableHTTPPath(w.project.Project.ID, "/milestones/"+w.seed.ID.String())
}
func (w *milestoneEditWeb) lookupPath() string {
	return variableHTTPPath(w.project.Project.ID, "/structure-commands/lookup")
}

func (w *milestoneEditWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.Method != http.MethodPatch && r.Method != http.MethodPost {
		return errors.New("undeclared Milestone Edit request")
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	closeErr := r.Body.Close()
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if readErr != nil || closeErr != nil || len(raw) > 16<<10 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("original Milestone Edit request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var wire struct {
		Command  wc.CommandName  `json:"command,omitempty"`
		Target   wc.MilestoneID  `json:"target_id,omitempty"`
		Expected f.Version       `json:"expected_version"`
		Request  wc.UpdateFields `json:"request"`
	}
	var fields map[string]json.RawMessage
	if agentWebDecode(raw, &wire) != nil || wire.Request.Validate() != nil || wire.Expected.Validate() != nil || json.Unmarshal(raw, &fields) != nil {
		return errors.New("Milestone Edit envelope lost exact fields")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	in := milestoneEditIntent{sprintWebIntent: sprintWebIntent{key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}, request: wire.Request, expected: wire.Expected, before: w.current}
	if r.Method == http.MethodPatch && r.URL.Path == w.path() {
		if len(fields) != 2 || wire.Command != "" || wire.Target.Validate() == nil || wire.Expected != w.current.Version || len(w.writes) != len(w.results) {
			return errors.New("Milestone Edit changed original target/version or overlapped a write")
		}
		for _, old := range w.writes {
			if old.key == key {
				return errors.New("browser repeated Milestone Edit")
			}
		}
		title, description, empty := milestoneEditTitle, milestoneEditDescription, ""
		want := wc.UpdateFields{Title: &title, Description: &description}
		switch len(w.writes) {
		case 0:
		case 1:
			if w.mode != "milestone-edit-normal" {
				return errors.New("browser retransmitted uncertain Milestone Edit")
			}
			want = wc.UpdateFields{Title: &title}
		case 2:
			if w.mode != "milestone-edit-normal" {
				return errors.New("browser added an uncertain Milestone Edit")
			}
			want = wc.UpdateFields{Description: &empty}
		default:
			return errors.New("browser added an undeclared Milestone Edit")
		}
		if !reflect.DeepEqual(wire.Request, want) {
			return errors.New("Milestone Edit changed original field presence or Unicode bytes")
		}
		in.kind = "update"
		w.writes = append(w.writes, in)
		w.updates++
	} else if r.Method == http.MethodPost && r.URL.Path == w.lookupPath() {
		if w.mode != "milestone-edit-lost" || len(fields) != 4 || len(w.writes) != 1 || len(w.results) != 1 || w.lookups != 0 || w.cut != 1 {
			return errors.New("Milestone Lookup has no declared uncertain original write")
		}
		original := w.writes[0]
		if wire.Command != wc.MilestoneUpdate || wire.Target != w.seed.ID || wire.Expected != original.expected || key != original.key || !reflect.DeepEqual(wire.Request, original.request) {
			return errors.New("Milestone Lookup lost original key/version/target/full patch")
		}
		in.kind = "lookup"
		in.before = original.before
		in.requestID = original.requestID
		w.lookups++
	} else {
		return errors.New("browser used a foreign Milestone Edit route")
	}
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), milestoneEditIntentKey{}, in))
	return nil
}

func (w *milestoneEditWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	in, write := r.Request.Context().Value(milestoneEditIntentKey{}).(milestoneEditIntent)
	lost := false
	if write {
		if r.StatusCode != 200 {
			return errors.New("original Milestone Edit/Lookup failed")
		}
		switch in.kind {
		case "update":
			var result wc.StructureMutation
			if json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
				return errors.New("original Milestone Edit receipt invalid")
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
			w.current = *result.Milestone
			w.writes[len(w.writes)-1] = in
			w.results = append(w.results, result.Clone())
			if result.Changed {
				w.changed++
			}
			w.afterCommit = snapshot
			lost = w.mode == "milestone-edit-lost"
			w.mu.Unlock()
		case "lookup":
			var found wc.CommandLookup
			w.mu.Lock()
			original, snapshot := w.results[0].Clone(), w.afterCommit
			w.mu.Unlock()
			if json.Unmarshal(raw, &found) != nil || found.Validate() != nil || found.State != wc.LookupCommitted || found.Result == nil || !reflect.DeepEqual(*found.Result, original) {
				return errors.New("Lookup did not recover original historical Milestone receipt")
			}
			if err = w.updateFacts(r.Request.Context(), in, original, false); err != nil {
				return err
			}
			after, err := w.snapshot(r.Request.Context())
			if err != nil {
				return err
			}
			if snapshot != after {
				return errors.New("read-only Milestone Lookup changed original facts")
			}
		default:
			return errors.New("unknown Milestone Edit proof")
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

func (w *milestoneEditWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.project.Project.ID
	switch r.URL.Path {
	case variableHTTPPath(p, ""):
		var project pc.ProjectRef
		if json.Unmarshal(raw, &project) != nil || !reflect.DeepEqual(project, w.project.Project) {
			return errors.New("Milestone Edit changed current Project")
		}
		w.projectReads++
	case w.path():
		var current wc.Milestone
		if json.Unmarshal(raw, &current) != nil || !reflect.DeepEqual(current, w.current) {
			return errors.New("Milestone current Get differs from canonical postimage")
		}
		w.gets++
	case variableHTTPPath(p, "/milestones"):
		var page f.Page[map[string]any]
		item, err := sprintCreateSummary(w.current, "description")
		if err != nil {
			return err
		}
		if json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" || !reflect.DeepEqual(page.Items, []map[string]any{item}) {
			return errors.New("Milestone List lost exact current summary")
		}
		w.lists++
	case variableHTTPPath(p, "/sprints"):
		var page f.Page[map[string]any]
		if r.URL.Query().Get("milestone_id") != w.seed.ID.String() || json.Unmarshal(raw, &page) != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
			return errors.New("Milestone Edit changed empty Sprint list")
		}
	}
	return nil
}

func (w *milestoneEditWeb) fixedFacts(ctx context.Context) (string, error) {
	unchanged, err := w.unchanged(ctx)
	if err != nil {
		return "", err
	}
	var facts string
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid),
 'milestone_order',(SELECT to_jsonb(g) FROM agenteam_work.milestone_order_groups g WHERE project_id=$1::text::uuid),
 'sprint_order',(SELECT jsonb_agg(to_jsonb(g) ORDER BY milestone_id) FROM agenteam_work.sprint_order_groups g WHERE project_id=$1::text::uuid))::text`, w.project.Project.ID.String()).Scan(&facts)
	return unchanged + facts, err
}

func (w *milestoneEditWeb) snapshot(ctx context.Context) (string, error) {
	var snapshot string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'milestones',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_work.milestones m WHERE project_id=$1::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.structure_commands c WHERE project_id=$1::text::uuid),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid AND producer='work'),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2::uuid))::text`, w.project.Project.ID.String(), w.a.p2.base.ownerBrowser.actor.Details().SessionID).Scan(&snapshot)
	return snapshot, err
}

func (w *milestoneEditWeb) updateFacts(ctx context.Context, in milestoneEditIntent, result wc.StructureMutation, canonical bool) error {
	if result.Validate() != nil || result.Command != wc.MilestoneUpdate || result.Milestone == nil || result.Sprint != nil {
		return errors.New("Milestone Update lost original Structure receipt")
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
		expected.UpdatedAt = result.Milestone.UpdatedAt
	}
	if in.expected != in.before.Version || !reflect.DeepEqual(*result.Milestone, expected) || result.Changed != (len(changed) != 0) || (result.EventID != nil) != result.Changed {
		return errors.New("Milestone Update altered omitted fields, original bytes, version or no-op semantics")
	}
	actor := w.a.p2.base.ownerBrowser.actor
	semantic, err := wc.UpdateMilestoneDigest(actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &in.expected}, w.project.Project.ID, w.seed.ID, in.request)
	if err != nil {
		return err
	}
	var commandID, user, digest string
	var eventID *string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.milestone.update' AND idempotency_key=$2 AND state='completed' AND committed_at IS NOT NULL`, w.project.Project.ID.String(), string(in.key)).Scan(&commandID, &eventID, &user, &digest, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var input map[string]any
	var receipt wc.StructureMutation
	want := map[string]any{"command": wc.MilestoneUpdate, "project_id": w.project.Project.ID, "target_id": w.seed.ID.String(), "actor_user_id": actor.Details().UserID, "expected_version": in.expected, "create_milestone": nil, "create_sprint": nil, "update": in.request, "reorder_milestone": nil, "reorder_sprint": nil}
	if json.Unmarshal(requestRaw, &input) != nil || !sprintCreateEqual(input, want) || json.Unmarshal(receiptRaw, &receipt) != nil || !reflect.DeepEqual(receipt, result) || revision < 1 || user != actor.Details().UserID || digest != semantic.String() {
		return errors.New("Milestone command lost exact original input, actor or receipt")
	}
	if !result.Changed {
		if eventID != nil || len(planRaw) != 0 {
			return errors.New("Milestone no-op created a plan or event")
		}
	} else {
		var plan struct {
			BeforeMilestone *wc.Milestone        `json:"before_milestone"`
			BeforeSprint    *wc.Sprint           `json:"before_sprint"`
			After           wc.StructureMutation `json:"after"`
			GroupBefore     *f.Version           `json:"group_before"`
			Header          eventc.Header        `json:"header"`
			Payload         wc.MilestoneChanged  `json:"payload"`
		}
		if eventID == nil || *eventID != result.EventID.String() || json.Unmarshal(planRaw, &plan) != nil || plan.BeforeMilestone == nil || !reflect.DeepEqual(*plan.BeforeMilestone, in.before) || plan.BeforeSprint != nil || plan.GroupBefore != nil || !reflect.DeepEqual(plan.After, result) {
			return errors.New("Milestone Update plan lost original preimage/postimage")
		}
		h, p := plan.Header, plan.Payload
		if h.Validate() != nil || p.Validate() != nil || h.EventID != *result.EventID || h.EventType != wc.MilestoneChangedName || h.AggregateType != wc.MilestoneAggregate || h.AggregateID.String() != w.seed.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != expected.Version || h.AggregateSequence != nil || h.SchemaVersion != wc.WorkSchemaVersion || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.project.Project.ID.String() || h.OccurredAt != expected.UpdatedAt || p.CommandID.String() != commandID || p.ActorUserID.String() != user || p.Change != wc.UpdatedChange || !reflect.DeepEqual(p.ChangedFields, changed) || p.Position != nil {
			return errors.New("Milestone event lost exact fields, actor, version or stable rank")
		}
		var headerRaw, payloadRaw []byte
		var columns bool
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.milestone_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.milestone' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5 FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND producer='work'`, w.project.Project.ID.String(), w.seed.ID.String(), *eventID, int64(expected.Version), expected.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &columns)
		if err != nil {
			return err
		}
		actualHeader, err := eventc.DecodeHeader(headerRaw)
		var actualPayload wc.MilestoneChanged
		if err != nil || !columns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, p) {
			return errors.New("Milestone Outbox differs from original command plan")
		}
	}
	if canonical {
		var matches bool
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT title=$3 AND description=$4 AND manual_rank=$5 AND version=$6 AND created_at=$7 AND updated_at=$8 FROM agenteam_work.milestones WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.project.Project.ID.String(), w.seed.ID.String(), expected.Title, expected.Description, expected.ManualRank, int64(expected.Version), expected.CreatedAt.Time(), expected.UpdatedAt.Time()).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return errors.New("current Milestone row differs from original Update postimage")
		}
	}
	fixed, err := w.fixedFacts(ctx)
	if err != nil {
		return err
	}
	if fixed != w.fixed {
		return errors.New("Milestone Edit changed Project, ordering, Audit, Sprint or Execution facts")
	}
	return nil
}

func (w *milestoneEditWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure, updates, changed, lookups, cuts, gets, lists := w.failure, w.updates, w.changed, w.lookups, w.cut, w.gets, w.lists
	writes := append([]milestoneEditIntent(nil), w.writes...)
	results := append([]wc.StructureMutation(nil), w.results...)
	snapshot := w.afterCommit
	tails := make([]*reviewWebTail, 0, len(w.tails))
	for _, tail := range w.tails {
		tails = append(tails, tail)
	}
	w.mu.Unlock()
	wantUpdates, wantChanged, wantLookups, wantReads := 3, 2, 0, 5
	if w.mode == "milestone-edit-lost" {
		wantUpdates, wantChanged, wantLookups, wantReads = 1, 1, 1, 3
	}
	if failure != nil || updates != wantUpdates || len(results) != updates || changed != wantChanged || lookups != wantLookups || cuts != wantLookups || gets < wantReads || lists < wantReads {
		t.Fatal("Milestone Edit original browser chain incomplete", failure)
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original Milestone Edit handler still running")
		}
		if !tail.returned {
			t.Fatal("original Milestone Edit handler did not return")
		}
	}
	for index, in := range writes {
		agentAppRequire(t, w.updateFacts(ctx, in, results[index], index == len(writes)-1))
	}
	var commands, events, milestones int64
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND producer='work'),
 (SELECT count(*) FROM agenteam_work.milestones WHERE project_id=$1::text::uuid)`, w.project.Project.ID.String()).Scan(&commands, &events, &milestones))
	if commands != int64(1+wantUpdates) || events != int64(1+wantChanged) || milestones != 1 {
		t.Fatal("Milestone Edit duplicated command/event or created another Milestone")
	}
	after, err := w.snapshot(ctx)
	agentAppRequire(t, err)
	if snapshot == "" || snapshot != after {
		t.Fatal("Milestone current reads or Lookup changed original committed facts")
	}
}

func newMilestoneEditWeb(t *testing.T, a *agentCreateFixture, project pc.SchedulerProject, mode string) *milestoneEditWeb {
	t.Helper()
	const prefix = "AGENTEAM_MILESTONE_EDIT_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Milestone Edit inputs required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen index missing")
	}
	directory, err := os.MkdirTemp(runtime, "mew-")
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
	return &milestoneEditWeb{milestoneCreateWeb: w}
}
func (w *milestoneEditWeb) start(t *testing.T, address string) {
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
				http.Error(out, "owned Milestone Edit observation failed", 500)
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

func (w *milestoneEditWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "milestone-edit.config.js"))
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
	const prefix = "AGENTEAM_MILESTONE_EDIT_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Milestone Edit Playwright did not start")
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
	t.Logf("Milestone Edit Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Milestone Edit browser call failed")
	}
}

func (w *milestoneEditWeb) proof(r *http.Response, raw []byte, intent sprintWebIntent, write bool) error {
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
	return os.WriteFile(filepath.Join(w.evidence, "milestone-edit-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}
