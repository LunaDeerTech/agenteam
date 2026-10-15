//go:build integration

// These tests exercise real PostgreSQL Meeting transactions with explicit
// dependency doubles. They do not claim an actual Executor or model call.
package meeting_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func ident[K any](t *testing.T) f.ID[K] {
	t.Helper()
	v, e := f.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func meta(t *testing.T) f.CommandMeta {
	return f.CommandMeta{RequestID: ident[f.Request](t), IdempotencyKey: f.IdempotencyKey(ident[struct{}](t).String())}
}
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

type fixtureAuthority struct {
	pc.ProjectAuthority
	store   *postgres.Store
	project pc.ProjectRef
	actor   i.Actor
	agents  []ac.DirectoryEntry
}

func (v *fixtureAuthority) RequireOwnerInTx(ctx context.Context, tx f.Tx, a i.Actor, p i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	if !a.Equal(v.actor) || p != v.project.ID {
		return pc.ProjectAccess{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	if _, e := v.store.InTx(tx); e != nil {
		return pc.ProjectAccess{}, e
	}
	return pc.NewProjectAccess(a, v.project, v.project.CreatedAt)
}
func (v *fixtureAuthority) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, p i.ProjectID) (pc.ProjectRef, error) {
	if _, e := v.store.InTx(tx); e != nil {
		return pc.ProjectRef{}, e
	}
	if p != v.project.ID {
		return pc.ProjectRef{}, f.NewFault(f.NotFound, f.NotStarted)
	}
	return v.project, nil
}
func (v *fixtureAuthority) GetAgent(ctx context.Context, a i.Actor, p i.ProjectID, id i.AgentID) (ac.DirectoryEntry, error) {
	for _, entry := range v.agents {
		if a.Equal(v.actor) && p == v.project.ID && entry.ID == id {
			return entry, nil
		}
	}
	return ac.DirectoryEntry{}, f.NewFault(f.NotFound, f.NotStarted)
}
func (v *fixtureAuthority) ListAgents(context.Context, i.Actor, i.ProjectID, f.PageRequest) (f.Page[ac.DirectoryEntry], error) {
	return f.Page[ac.DirectoryEntry]{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
}
func (v *fixtureAuthority) RequireCurrentInTx(ctx context.Context, tx f.Tx, a i.Actor, p i.ProjectID, id i.AgentID) (ac.AgentRef, error) {
	if _, e := v.store.InTx(tx); e != nil {
		return ac.AgentRef{}, e
	}
	entry, e := v.GetAgent(ctx, a, p, id)
	if e != nil {
		return ac.AgentRef{}, e
	}
	return ac.AgentRef{ProjectID: p, AgentID: id, ConfigVersion: entry.Version}, nil
}

type fixture struct {
	store     *postgres.Store
	authority *fixtureAuthority
	service   *meeting.Service
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	m, e := postgres.NewMigrator(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(testContext(t)); !r.Migrated {
		t.Fatal("global migration", r.Fault)
	}
	store, e := postgres.Open(testContext(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := store.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	actor, e := i.NewHuman(ident[i.User](t), ident[i.Session](t))
	if e != nil {
		t.Fatal(e)
	}
	user, _ := f.ParseID[i.User](actor.Details().UserID)
	at, _ := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
	project := pc.ProjectRef{ID: ident[i.Project](t), OwnerUserID: user, Name: "Meeting-fixture", NormalizedName: "meeting-fixture", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}
	if e := project.Validate(); e != nil {
		t.Fatal("fixture project", e)
	}
	a := &fixtureAuthority{store: store, project: project, actor: actor}
	for _, name := range []string{"alpha", "beta"} {
		a.agents = append(a.agents, ac.DirectoryEntry{ID: ident[i.Agent](t), ProjectID: project.ID, Name: name, Version: 1, CreatedAt: at, UpdatedAt: at})
	}
	service, e := meeting.NewService(store, meeting.Dependencies{Projects: a, Agents: a, Directory: a})
	if e != nil {
		t.Fatal(e)
	}
	return &fixture{store, a, service}
}
func (v *fixture) start(t *testing.T, mode string) (c.StartRequest, c.Receipt) {
	t.Helper()
	r := c.StartRequest{ProjectID: v.authority.project.ID, Agents: []i.AgentID{v.authority.agents[0].ID, v.authority.agents[1].ID}, Mode: mode, Content: []c.InlineNode{{Type: "text", Text: "Discuss the migration plan."}}, Meta: meta(t)}
	out, e := v.service.Start(testContext(t), v.authority.actor, r)
	if e != nil {
		t.Fatal("start", e)
	}
	return r, out
}
func TestMeetingDomainTransactions(t *testing.T) {
	v := setup(t)
	request, receipt := v.start(t, "sequential")
	ctx := testContext(t)
	a := v.authority
	replay, e := v.service.Start(ctx, a.actor, request)
	if e != nil || !replay.Replayed || replay.MeetingID != receipt.MeetingID {
		t.Fatal("replay", e)
	}
	found, ok, e := v.service.LookupStart(ctx, a.actor, request)
	if e != nil || !ok || found.MeetingID != receipt.MeetingID {
		t.Fatal("lookup", e)
	}
	changed := request
	changed.Content = []c.InlineNode{{Type: "text", Text: "different"}}
	_, e = v.service.Start(ctx, a.actor, changed)
	var fault *f.Fault
	if !errors.As(e, &fault) || fault.Code != f.IdempotencyKeyReused {
		t.Fatal("semantic conflict", e)
	}
	detail, e := v.service.GetDetail(ctx, a.actor, a.project.ID, receipt.MeetingID)
	if e != nil {
		t.Fatal("detail", e)
	}
	if len(detail.Participants) != 3 || len(detail.Turns) != 1 || len(detail.Turns[0].Contributions) != 3 || detail.Turns[0].Contributions[0].Message == nil {
		t.Fatal("atomic start postimage")
	}
	send := c.SendRequest{ProjectID: a.project.ID, MeetingID: receipt.MeetingID, ExpectedVersion: detail.Session.Version, Mode: "parallel", Content: []c.InlineNode{{Type: "text", Text: "Follow-up topic."}}, Meta: meta(t)}
	if _, e = v.service.Send(ctx, a.actor, send); e != nil {
		t.Fatal("queue", e)
	}
	detail, e = v.service.GetDetail(ctx, a.actor, a.project.ID, receipt.MeetingID)
	if e != nil || len(detail.Turns) != 2 || detail.Turns[1].Status != "queued" {
		t.Fatal("queued postimage", e)
	}
	listed, e := v.service.List(ctx, a.actor, a.project.ID)
	if e != nil || len(listed) != 1 {
		t.Fatal("list", e)
	}
	message := detail.Turns[0].Contributions[0].Message.ID
	if _, e = v.store.Exec(ctx, `UPDATE agenteam_meeting.messages SET content='[]' WHERE id=$1`, message.String()); e == nil {
		t.Fatal("immutable message updated")
	}
	foreign, _ := i.NewHuman(ident[i.User](t), ident[i.Session](t))
	if _, e = v.service.GetDetail(ctx, foreign, a.project.ID, receipt.MeetingID); e == nil {
		t.Fatal("foreign owner read accepted")
	}
	var count, max int
	if e = v.store.QueryRow(ctx, `SELECT count(*),max(version) FROM agenteam_meta.migration_journal WHERE state='applied'`).Scan(&count, &max); e != nil || count != max || max < 111 {
		t.Fatal("migration sequence", count, max, e)
	}
}

// Controlled completion isolates domain ordering/capture and at-most-once
// publication. Real Executor/Model coverage belongs to the App integration top.
type controlledExecution struct {
	mu            sync.Mutex
	started       chan struct{}
	release       <-chan struct{}
	store         *postgres.Store
	authority     *meeting.Authority
	entries       map[i.ExecutionID]ec.Summary
	inputs        []meeting.FrozenContext
	summaryInputs []meeting.SummaryInput
}

func (v *controlledExecution) Launch(ctx context.Context, a i.Actor, r ec.LaunchRequest) (ec.LaunchResult, error) {
	var raw []byte
	if e := v.store.QueryRow(ctx, `SELECT i.input_data FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.inputs i ON i.id=l.input_id WHERE l.contribution_id=$1 AND l.generation=$2 AND l.attempt=$3`, r.Trigger.ContributionID, int64(*r.Lineage.ContributionGeneration), int64(*r.Lineage.ContributionAttempt)).Scan(&raw); e != nil {
		return ec.LaunchResult{}, e
	}
	var input meeting.FrozenContext
	if e := json.Unmarshal(raw, &input); e != nil {
		return ec.LaunchResult{}, e
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.inputs = append(v.inputs, input)
	id, e := f.NewID[i.Execution]()
	if e != nil {
		return ec.LaunchResult{}, e
	}
	at, _ := f.NewInstant(time.Now().UTC())
	s := ec.Summary{ID: id, ProjectID: r.ProjectID, AgentID: r.AgentID, Trigger: r.Trigger, Purpose: r.Purpose, Status: ec.Created, Version: 1, CreatedAt: at}
	v.entries[id] = s
	return ec.LaunchResult{Execution: s}, nil
}
func (v *controlledExecution) LookupLaunch(context.Context, i.Actor, ec.LaunchLookupKey, f.Digest) (ec.LaunchLookup, error) {
	return ec.LaunchLookup{}, nil
}
func (v *controlledExecution) AdvanceMeeting(ctx context.Context, p i.ProjectID, m ec.AssociatedMeeting) (ec.ExecutionAdvance, error) {
	if v.started != nil {
		select {
		case v.started <- struct{}{}:
		case <-ctx.Done():
			return ec.ExecutionAdvance{}, ctx.Err()
		}
		select {
		case <-v.release:
		case <-ctx.Done():
			return ec.ExecutionAdvance{}, ctx.Err()
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	s := v.entries[m.ExecutionID]
	s.Status = ec.Succeeded
	s.Version++
	v.entries[m.ExecutionID] = s
	return ec.ExecutionAdvance{Status: ec.Succeeded}, nil
}
func (v *controlledExecution) ReadMeetingExecutionInTx(ctx context.Context, tx f.Tx, p i.ProjectID, m ec.AssociatedMeeting) (ec.MeetingExecutionResult, error) {
	if e := v.authority.RequireBindingInTx(ctx, tx, v.store); e != nil {
		return ec.MeetingExecutionResult{}, e
	}
	if e := v.authority.RequireMeetingResultInTx(ctx, tx, p, m); e != nil {
		return ec.MeetingExecutionResult{}, e
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.entries[m.ExecutionID].Status != ec.Succeeded {
		return ec.MeetingExecutionResult{Summary: v.entries[m.ExecutionID]}, nil
	}
	text := "Public contribution " + m.AgentID.String()
	return ec.MeetingExecutionResult{Summary: v.entries[m.ExecutionID], FinalText: &text}, nil
}
func (v *controlledExecution) GenerateSummary(ctx context.Context, in meeting.SummaryInput) (meeting.SummaryCandidate, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.summaryInputs = append(v.summaryInputs, in)
	title := "Migration discussion"
	return meeting.SummaryCandidate{Title: &title, Goals: "Agree migration steps", Decisions: "Keep immutable inputs", Unresolved: "Rollout timing", Facts: "Two agents contributed"}, nil
}
func TestMeetingDomainSequentialAndParallelInput(t *testing.T) {
	for _, mode := range []string{"sequential", "parallel"} {
		t.Run(mode, func(t *testing.T) {
			v := setup(t)
			_, receipt := v.start(t, mode)
			authority, e := meeting.NewAuthority(v.store)
			if e != nil {
				t.Fatal(e)
			}
			controlled := &controlledExecution{store: v.store, authority: authority, entries: map[i.ExecutionID]ec.Summary{}}
			actor, _ := i.RegisterService(i.MeetingRuntime)
			runtime, e := meeting.NewRuntime(v.store, meeting.RuntimeDependencies{Projects: v.authority, Launcher: controlled, Executor: controlled, Results: controlled, Authority: authority, Summaries: controlled, Actor: actor})
			if e != nil {
				t.Fatal(e)
			}
			for n := 0; n < 5; n++ {
				if _, e = runtime.Advance(testContext(t), v.authority.project.ID, receipt.MeetingID); e != nil {
					t.Fatal("advance", n, e)
				}
			}
			if len(controlled.inputs) != 2 {
				t.Fatal("wrong launches", len(controlled.inputs))
			}
			wantSecond := 2
			if mode == "parallel" {
				wantSecond = 1
			}
			if len(controlled.inputs[0].Messages) != 1 || len(controlled.inputs[1].Messages) != wantSecond {
				t.Fatal("incorrect immutable input boundary", len(controlled.inputs[0].Messages), len(controlled.inputs[1].Messages))
			}
			if len(controlled.summaryInputs) != 1 || len(controlled.summaryInputs[0].Messages) != 3 {
				t.Fatal("summary must see all three current messages")
			}
			detail, e := v.service.GetDetail(testContext(t), v.authority.actor, v.authority.project.ID, receipt.MeetingID)
			if e != nil || detail.Turns[0].Status != "completed" || detail.Session.Summary == nil || detail.Session.Summary.Version != 1 || detail.Session.Title == nil {
				t.Fatal("summary postimage", e)
			}
			var count int
			if e = v.store.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_meeting.messages WHERE meeting_id=$1`, receipt.MeetingID.String()).Scan(&count); e != nil || count != 3 {
				t.Fatal("duplicate message", count, e)
			}
			runtime.Stop()
			if !runtime.Joined() {
				t.Fatal("runtime still admitted")
			}
		})
	}
}
