//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Acceptance only resolves the complete registry. Any participant invocation
// is an error, never a successful substitute for stop, cleanup or Skills.
type blockerInteropParticipant struct {
	name  c.ParticipantName
	calls atomic.Int32
}

func (p *blockerInteropParticipant) Name() c.ParticipantName { return p.name }
func (p *blockerInteropParticipant) RequestStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.calls.Add(1)
	return c.StopReport{}, errors.New("archive acceptance must not invoke RequestStop")
}
func (p *blockerInteropParticipant) InspectStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.calls.Add(1)
	return c.StopReport{}, errors.New("archive acceptance must not invoke InspectStop")
}
func (p *blockerInteropParticipant) Cleanup(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
	p.calls.Add(1)
	return c.CleanupReport{}, errors.New("archive acceptance must not invoke Cleanup")
}

func blockerInteropProject(t *testing.T, f *blockerFixture, pause *blockerInteropGate) *project.Service {
	t.Helper()
	entries := []c.ParticipantRegistration{
		{Name: c.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []c.ReferenceKind{"object"}},
		{Name: c.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []c.ReferenceKind{"secret"}},
		{Name: c.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant}},
		{Name: c.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []c.ParticipantName{c.OutboxParticipant}},
	}
	manifest, err := c.NewRequiredManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range entries {
		port := &blockerInteropParticipant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: port})
		t.Cleanup(func() {
			if port.calls.Load() != 0 {
				t.Errorf("acceptance invoked participant %s", port.name)
			}
		})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(f.store, f.keys, audit.Authorizations{Sessions: f.accounts, System: f.accounts, Accounts: f.accounts, Projects: f.projectAuthority})
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	typed, err := c.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := fixtureProcess{id[oc.Process](t)}
	box, err := outbox.New(f.store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.ProjectProducer: f.projectAuthority}, Projects: f.projectAuthority, Sessions: f.accounts, System: f.accounts, Processes: process, Audit: aud, Cursors: f.keys})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := project.New(f.store, project.Dependencies{Authority: f.projectAuthority, Activity: f.accounts, Audit: aud, Events: blockerInteropAppender(box, pause), ProjectEvents: typed, Initializer: f.skills, Processes: process, Cursors: f.keys, LifecycleRegistry: registry}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		svc.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := svc.Drain(ctx); err != nil {
			t.Error("interop Project Drain", err)
		}
	})
	return svc
}

type blockerInteropGate struct {
	reached, release chan struct{}
	once, freed      sync.Once
}

func newBlockerInteropGate(t *testing.T) *blockerInteropGate {
	t.Helper()
	g := &blockerInteropGate{reached: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(g.free)
	return g
}
func (g *blockerInteropGate) free() { g.freed.Do(func() { close(g.release) }) }
func (g *blockerInteropGate) wait(ctx context.Context) error {
	g.once.Do(func() { close(g.reached) })
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func blockerInteropAppender(box oc.Appender, gate *blockerInteropGate) oc.Appender {
	if gate == nil {
		return box
	}
	return &capturingAppender{Appender: box, after: func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
		return gate.wait(ctx)
	}}
}

type blockerInteropActivity struct {
	work.ActivityAuthority
	calls atomic.Int32
}

func (a *blockerInteropActivity) TouchActivityInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	a.calls.Add(1)
	return a.ActivityAuthority.TouchActivityInTx(ctx, tx, actor)
}

// This observer runs only after the actual final callback has persisted its
// completed command, while the real transaction and its locks are still live.
func holdBlockerInteropFinal(t *testing.T, store *hookStore, command foundation.CommandIdentity, p c.ProjectID, name, key string, archive bool) (*blockerInteropGate, *atomic.Int32) {
	t.Helper()
	gate, pid := newBlockerInteropGate(t), new(atomic.Int32)
	var armed atomic.Bool
	store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
		if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() || armed.Load() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		query := `SELECT EXISTS(SELECT 1 FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`
		if archive {
			query = `SELECT EXISTS(SELECT 1 FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2 AND key=$3 AND state='completed')`
		}
		var completed bool
		if err = x.QueryRow(ctx, query, p.String(), name, key).Scan(&completed); err != nil {
			return err
		}
		if completed && armed.CompareAndSwap(false, true) {
			var backend int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backend); err != nil {
				return err
			}
			pid.Store(backend)
			return gate.wait(ctx)
		}
		return nil
	})
	return gate, pid
}

type blockerInteropReply struct {
	blocker wc.TaskBlockerMutation
	sprint  wc.StructureMutation
	archive c.LifecycleOperation
	err     error
}

func callBlockerInterop(t *testing.T, fn func(context.Context) blockerInteropReply) <-chan blockerInteropReply {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out, joined := make(chan blockerInteropReply, 1), make(chan struct{})
	go func() { defer close(joined); out <- fn(ctx) }()
	t.Cleanup(func() { cancel(); await(t, joined) })
	return out
}
func joinBlockerInterop(t *testing.T, ch <-chan blockerInteropReply) blockerInteropReply {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("interop call did not join")
	}
	return blockerInteropReply{}
}
func awaitBlockerInterop(t *testing.T, reached <-chan struct{}, result <-chan blockerInteropReply) {
	t.Helper()
	select {
	case <-reached:
	case r := <-result:
		t.Fatal("interop call returned before its real barrier", r.err)
	case <-time.After(10 * time.Second):
		t.Fatal("interop barrier not reached")
	}
}

func assertBlockerInteropProjectLock(t *testing.T, f *blockerFixture, pid int32, p c.ProjectID, mode foundation.LockMode) {
	t.Helper()
	key, err := foundation.ProjectLock(p.String())
	if err != nil {
		t.Fatal(err)
	}
	lock := uint64(key.AdvisoryKey())
	name := "ShareLock"
	if mode == foundation.Exclusive {
		name = "ExclusiveLock"
	}
	var held bool
	err = f.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND objsubid=1 AND classid::bigint=$1 AND objid::bigint=$2 AND pid=$3 AND mode=$4 AND granted)`, int64(uint32(lock>>32)), int64(uint32(lock)), pid, name).Scan(&held)
	if err != nil || !held {
		t.Fatal("final writer does not hold exact Project mode", pid, name, err)
	}
}

// Excludes durable prepared plans and other domains' authorized changes.
func blockerInteropWorkSnapshot(t *testing.T, f *blockerFixture, p c.ProjectID) string {
	t.Helper()
	var raw string
	err := f.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_work.tasks x WHERE project_id=$1),
 'blockers',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_work.task_blockers x WHERE project_id=$1),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_work.task_events x WHERE project_id=$1),
 'query',(SELECT to_jsonb(x) FROM agenteam_work.task_query_generations x WHERE project_id=$1),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY sprint_id,state,priority),'[]') FROM agenteam_work.task_order_groups x WHERE project_id=$1),
 'receipts',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_work.task_blocker_commands x WHERE project_id=$1 AND state='completed'),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_outbox.events x WHERE project_id=$1 AND producer='work'))::text`, p.String()).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func blockerInteropPlacement(t *testing.T, f *blockerFixture, p c.ProjectID) string {
	t.Helper()
	var raw string
	err := f.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'tasks',(SELECT jsonb_agg(to_jsonb(x)-'version'-'updated_at' ORDER BY id) FROM agenteam_work.tasks x WHERE project_id=$1),
 'sprints',(SELECT jsonb_agg(to_jsonb(x)-'version'-'updated_at'-'title' ORDER BY id) FROM agenteam_work.sprints x WHERE project_id=$1),
 'task_groups',(SELECT jsonb_agg(to_jsonb(x) ORDER BY sprint_id,state,priority) FROM agenteam_work.task_order_groups x WHERE project_id=$1),
 'sprint_groups',(SELECT jsonb_agg(to_jsonb(x) ORDER BY milestone_id) FROM agenteam_work.sprint_order_groups x WHERE project_id=$1),
 'milestone_groups',(SELECT to_jsonb(x) FROM agenteam_work.milestone_order_groups x WHERE project_id=$1))::text`, p.String()).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertBlockerInteropFact(t *testing.T, f *blockerFixture, a identity.Actor, before wc.Task, result wc.TaskBlockerMutation) {
	t.Helper()
	if result.Validate() != nil || result.Task.Version != before.Version+1 {
		t.Fatal("Blocker did not produce one valid version increment")
	}
	actual, err := f.taskReader.GetTask(ctxFor(t), a, before.ProjectID, before.ID)
	if err != nil || string(jsonBytes(t, actual)) != string(jsonBytes(t, result.Task)) {
		t.Fatal("Blocker receipt does not match durable Task", err)
	}
	var count int
	var receipt []byte
	err = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_blockers b JOIN agenteam_work.task_blocker_commands c ON c.id=b.created_operation_id AND c.project_id=b.project_id JOIN agenteam_work.task_events h ON h.id=c.task_event_id AND h.blocker_operation_id=c.id AND h.project_id=c.project_id JOIN agenteam_outbox.events e ON e.id=c.event_id WHERE b.id=$1 AND b.task_id=$2 AND c.state='completed' AND h.task_version=$3 AND h.id=$4 AND e.id=$5`, result.Blocker.ID.String(), before.ID.String(), int64(result.Task.Version), result.TaskEventID.String(), result.EventIDs[0].String()).Scan(&count)
	if err != nil || count != 1 {
		t.Fatal("Blocker canonical fact/history/Outbox/receipt identity is not unique", err, count)
	}
	err = f.raw.QueryRow(ctxFor(t), `SELECT receipt FROM agenteam_work.task_blocker_commands WHERE task_event_id=$1`, result.TaskEventID.String()).Scan(&receipt)
	var saved wc.TaskBlockerMutation
	if err != nil || json.Unmarshal(receipt, &saved) != nil {
		t.Fatal("Blocker durable receipt decode", err)
	}
	equalBlockerMutation(t, result, saved)
}

func assertBlockerInteropArchive(t *testing.T, f *blockerFixture, p c.ProjectRef, op c.LifecycleOperation) {
	t.Helper()
	if op.Validate() != nil || op.State != c.OperationAccepted || op.ProjectID != p.ID || op.ProjectVersion != p.Version+1 {
		t.Fatal("BeginArchive did not return its accepted operation")
	}
	var gate, operation string
	var version int64
	var operations, participants, audits, events, receipts int
	err := f.raw.QueryRow(ctxFor(t), `SELECT lifecycle,version,current_lifecycle_operation_id::text,
 (SELECT count(*) FROM agenteam_project.lifecycle_operations WHERE project_id=$1 AND state='accepted'),
 (SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$2),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.archive.accepted'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.lifecycle_changed'),
 (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name='archive' AND state='completed')
 FROM agenteam_project.projects WHERE id=$1`, p.ID.String(), op.ID.String()).Scan(&gate, &version, &operation, &operations, &participants, &audits, &events, &receipts)
	if err != nil || gate != "archiving" || version != int64(p.Version+1) || operation != op.ID.String() || operations != 1 || participants != 4 || audits != 1 || events != 1 || receipts != 1 {
		t.Fatal("archive acceptance is not one atomic durable result", err, gate, version, operations, participants, audits, events, receipts)
	}
}

func TestTaskBlockerInteroperability(t *testing.T) {
	base := newBlockerFixture(t)
	a := base.human(t, "blocker-interop", "user")
	for _, archive := range []bool{true, false} {
		for _, blockerFirst := range []bool{false, true} {
			name := "update-sprint-first"
			if archive {
				name = "archive-first"
			}
			if blockerFirst {
				name = "blocker-before-" + name
			}
			t.Run(name, func(t *testing.T) {
				p, _, _ := base.create(t, a, name)
				m := base.milestone(t, a, p.ID, "m")
				s := base.sprint(t, a, p.ID, m.ID, "before")
				target := base.task(t, a, p.ID, s.ID, "target")
				blockerSide, blockerStore := observedBlockerFixture(t, base)
				otherSide, otherStore := observedBlockerFixture(t, base)
				prepared := newBlockerInteropGate(t)
				defer prepared.free()
				var blockerPause, otherPause *blockerInteropGate
				if blockerFirst {
					otherPause = prepared
				} else {
					blockerPause = prepared
				}
				activity := &blockerInteropActivity{ActivityAuthority: blockerSide.accounts}
				blockers := blockerSide.newBlockerService(t, blockerInteropAppender(blockerSide.blockerOutbox, blockerPause), activity)
				blockerMeta := meta(t, "interop-blocker", &target.Version)
				request := blockerWaiting(t, "formal command interop")
				blockerCall := func(ctx context.Context) blockerInteropReply {
					v, err := blockers.AddTaskBlocker(ctx, a, blockerMeta, p.ID, target.ID, request)
					return blockerInteropReply{blocker: v, err: err}
				}
				otherMeta := meta(t, "interop-other", &s.Version)
				title := "after"
				var otherCall func(context.Context) blockerInteropReply
				var otherIdentity foundation.CommandIdentity
				var otherName string
				var err error
				if archive {
					otherMeta = meta(t, "interop-other", &p.Version)
					projects := blockerInteropProject(t, otherSide, otherPause)
					otherCall = func(ctx context.Context) blockerInteropReply {
						v, err := projects.BeginArchive(ctx, a, otherMeta, p.ID)
						return blockerInteropReply{archive: v, err: err}
					}
					otherIdentity, err = c.CommandIdentity(p.ID, c.ArchiveCommand, otherMeta.IdempotencyKey)
					otherName = string(c.ArchiveCommand)
				} else {
					structure := otherSide.newService(t, blockerInteropAppender(otherSide.events, otherPause), otherSide.accounts)
					otherCall = func(ctx context.Context) blockerInteropReply {
						v, err := structure.UpdateSprint(ctx, a, otherMeta, p.ID, s.ID, wc.UpdateFields{Title: &title})
						return blockerInteropReply{sprint: v, err: err}
					}
					otherIdentity, err = wc.Identity(p.ID, wc.SprintUpdate, otherMeta.IdempotencyKey)
					otherName = string(wc.SprintUpdate)
				}
				if err != nil {
					t.Fatal(err)
				}
				before := blockerInteropWorkSnapshot(t, base, p.ID)
				placement := blockerInteropPlacement(t, base, p.ID)
				g, q := base.generation(t, p.ID, s.ID, target.Priority)
				var reached <-chan struct{}
				var pid *atomic.Int32
				var release func()
				firstCall, secondCall, secondStore := otherCall, blockerCall, blockerStore
				projectMode := foundation.Exclusive
				if blockerFirst {
					firstCall, secondCall, secondStore = blockerCall, otherCall, otherStore
					reached, pid, release = holdBlockerFinal(t, blockerStore, p.ID, wc.TaskBlockerCommandAdd, blockerMeta.IdempotencyKey)
					projectMode = foundation.Shared
				} else {
					gate, backend := holdBlockerInteropFinal(t, otherStore, otherIdentity, p.ID, otherName, string(otherMeta.IdempotencyKey), archive)
					reached, pid, release = gate.reached, backend, gate.free
				}
				defer release()
				second := callBlockerInterop(t, secondCall)
				awaitBlockerInterop(t, prepared.reached, second)
				first := callBlockerInterop(t, firstCall)
				awaitBlockerInterop(t, reached, first)
				assertBlockerInteropProjectLock(t, base, pid.Load(), p.ID, projectMode)
				user, _ := foundation.UserLock(a.Details().UserID)
				attempts := observeLock(secondStore, user, nil)
				prepared.free()
				attempt := awaitLockAttempt(t, attempts)
				waitTaskExactMode(t, base.taskFixture, attempt, foundation.Exclusive, pid.Load())
				release()
				firstResult, secondResult := joinBlockerInterop(t, first), joinBlockerInterop(t, second)
				blockerResult, otherResult := secondResult, firstResult
				if blockerFirst {
					blockerResult, otherResult = firstResult, secondResult
				}
				if otherResult.err != nil {
					t.Fatal("formal peer command failed", otherResult.err)
				}
				if archive {
					assertBlockerInteropArchive(t, base, p, otherResult.archive)
				}
				if archive && !blockerFirst {
					requireCode(t, blockerResult.err, foundation.ProjectNotActive)
					if blockerInteropWorkSnapshot(t, base, p.ID) != before || activity.calls.Load() != 0 {
						t.Fatal("archive-losing prepared Blocker changed business facts or Activity")
					}
					return
				}
				if blockerResult.err != nil {
					t.Fatal("Blocker failed after serialized peer", blockerResult.err)
				}
				assertBlockerInteropFact(t, base, a, target, blockerResult.blocker)
				ag, aq := base.generation(t, p.ID, s.ID, target.Priority)
				if ag != g || aq != q+1 || blockerInteropPlacement(t, base, p.ID) != placement || activity.calls.Load() != 1 {
					t.Fatal("interop changed placement/order or duplicated query generation/Activity")
				}
				if !archive {
					if otherResult.sprint.Sprint == nil || otherResult.sprint.Sprint.Version != s.Version+1 || otherResult.sprint.Sprint.Title != title || !otherResult.sprint.Changed {
						t.Fatal("real UpdateSprint did not apply exactly once")
					}
					actual, err := base.reader.GetSprint(ctxFor(t), a, p.ID, s.ID)
					if err != nil || string(jsonBytes(t, actual)) != string(jsonBytes(t, *otherResult.sprint.Sprint)) {
						t.Fatal("Sprint receipt differs from durable Sprint", err)
					}
					var structureEvents, blockerEvents int
					err = base.raw.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE event_type='work.sprint_changed'),count(*) FILTER(WHERE event_type='work.task_blockers_changed') FROM agenteam_outbox.events WHERE project_id=$1`, p.ID.String()).Scan(&structureEvents, &blockerEvents)
					// One Sprint create preceded the one title update.
					if err != nil || structureEvents != 2 || blockerEvents != 1 {
						t.Fatal("interop event counts changed", err, structureEvents, blockerEvents)
					}
					return
				}
				// Archiving retains current reads and the original completed receipt.
				stable := base.blockerSnapshot(t, a)
				rows, err := blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
				if err != nil || len(rows) != 1 || rows[0].ID != request.BlockerID {
					t.Fatal("archiving lost Blocker read", err)
				}
				digest, err := wc.TaskBlockerAddDigest(a, blockerMeta, p.ID, target.ID, request)
				if err != nil {
					t.Fatal(err)
				}
				looked, err := blockers.LookupTaskBlockerCommand(ctxFor(t), a, wc.TaskBlockerCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: blockerMeta.IdempotencyKey, SemanticDigest: digest})
				if err != nil || looked.Status != wc.LookupCommitted || looked.Receipt == nil {
					t.Fatal("archiving lost completed lookup", err)
				}
				equalBlockerMutation(t, blockerResult.blocker, *looked.Receipt)
				replay, err := blockers.AddTaskBlocker(ctxFor(t), a, blockerMeta, p.ID, target.ID, request)
				if err != nil {
					t.Fatal("archiving lost completed replay", err)
				}
				equalBlockerMutation(t, blockerResult.blocker, replay)
				_, err = blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "archiving-new-add", &replay.Task.Version), p.ID, target.ID, blockerWaiting(t, "denied"))
				requireCode(t, err, foundation.ProjectNotActive)
				_, err = blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "archiving-new-resolve", &replay.Task.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: replay.Blocker.ID})
				requireCode(t, err, foundation.ProjectNotActive)
				if base.blockerSnapshot(t, a) != stable || activity.calls.Load() != 1 {
					t.Fatal("archiving reads/replay/rejections changed durable facts or Activity")
				}
			})
		}
	}
	t.Run("uninitialized-owner-gate-before-work", func(t *testing.T) {
		// The accepted fixture leaves the real Project creation pending. It
		// supplies no Work directory and does not claim a real Skills binding.
		base.skills.setMode("pending")
		defer base.skills.setMode("")
		projectID := id[identity.Project](t)
		created, err := base.projects.CreateProject(ctxFor(t), a, meta(t, "interop-pending", nil), c.CreateProjectRequest{ProjectID: projectID, Name: "interop-pending"})
		if err != nil || created.State == c.CreationReady || created.Operation == nil {
			t.Fatal("real creation did not retain a pending initialization", err)
		}
		var pending bool
		if err = base.raw.QueryRow(ctxFor(t), `SELECT initialized_at IS NULL AND lifecycle='active' FROM agenteam_project.projects WHERE id=$1`, projectID.String()).Scan(&pending); err != nil || !pending {
			t.Fatal("pending Project canonical gate not present", err)
		}
		activity := &blockerInteropActivity{ActivityAuthority: base.accounts}
		blockers := base.newBlockerService(t, base.blockerOutbox, activity)
		before := base.blockerSnapshot(t, a)
		version := foundation.Version(1)
		taskID := id[wc.Task](t)
		request := blockerWaiting(t, "uninitialized is not a directory grant")
		command := meta(t, "pending-add", &version)
		_, err = blockers.AddTaskBlocker(ctxFor(t), a, command, projectID, taskID, request)
		requireCode(t, err, foundation.ProjectNotActive)
		_, err = blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "pending-resolve", &version), projectID, taskID, wc.TaskBlockerResolve{BlockerID: request.BlockerID})
		requireCode(t, err, foundation.ProjectNotActive)
		_, err = blockers.ListTaskBlockers(ctxFor(t), a, projectID, taskID, wc.TaskBlockersAll)
		requireCode(t, err, foundation.ProjectNotActive)
		digest, err := wc.TaskBlockerAddDigest(a, command, projectID, taskID, request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = blockers.LookupTaskBlockerCommand(ctxFor(t), a, wc.TaskBlockerCommandLookupRequest{ProjectID: projectID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: command.IdempotencyKey, SemanticDigest: digest})
		requireCode(t, err, foundation.ProjectNotActive)
		if base.blockerSnapshot(t, a) != before || activity.calls.Load() != 0 {
			t.Fatal("uninitialized gate allowed Work facts or Activity")
		}
	})
	t.Run("deleting-denies-retained-blocker-history", func(t *testing.T) {
		p, _, _ := base.create(t, a, "interop-delete")
		m := base.milestone(t, a, p.ID, "m")
		s := base.sprint(t, a, p.ID, m.ID, "s")
		target := base.task(t, a, p.ID, s.ID, "retained")
		request := blockerWaiting(t, "retained before delete acceptance")
		command := meta(t, "delete-original-add", &target.Version)
		original, err := base.blockers.AddTaskBlocker(ctxFor(t), a, command, p.ID, target.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		projects := blockerInteropProject(t, base, nil)
		accepted, err := projects.BeginDeleteProject(ctxFor(t), a, meta(t, "interop-delete", &p.Version), p.ID, c.DeleteProjectRequest{NormalizedCurrentPath: "blocker-interop/" + p.NormalizedName, Permanent: true})
		if err != nil || accepted.Operation == nil || accepted.Operation.State != c.OperationAccepted || accepted.Operation.ProjectVersion != p.Version+1 {
			t.Fatal("real delete acceptance failed", err)
		}
		var gate string
		var version int64
		var operations, participants, audits, events, receipts int
		err = base.raw.QueryRow(ctxFor(t), `SELECT lifecycle,version,
 (SELECT count(*) FROM agenteam_project.lifecycle_operations WHERE project_id=$1 AND action='delete' AND state='accepted'),
 (SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$2),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.delete.accepted'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.lifecycle_changed'),
 (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name='delete' AND state='completed')
 FROM agenteam_project.projects WHERE id=$1`, p.ID.String(), accepted.Operation.ID.String()).Scan(&gate, &version, &operations, &participants, &audits, &events, &receipts)
		if err != nil || gate != "deleting" || version != int64(p.Version+1) || operations != 1 || participants != 4 || audits != 1 || events != 1 || receipts != 1 {
			t.Fatal("delete acceptance did not persist its exact gate and facts", err)
		}
		activity := &blockerInteropActivity{ActivityAuthority: base.accounts}
		blockers := base.newBlockerService(t, base.blockerOutbox, activity)
		before := base.blockerSnapshot(t, a)
		_, err = blockers.AddTaskBlocker(ctxFor(t), a, command, p.ID, target.ID, request)
		requireCode(t, err, foundation.ProjectNotActive)
		_, err = blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "deleting-new-add", &original.Task.Version), p.ID, target.ID, blockerWaiting(t, "denied"))
		requireCode(t, err, foundation.ProjectNotActive)
		_, err = blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "deleting-resolve", &original.Task.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: original.Blocker.ID})
		requireCode(t, err, foundation.ProjectNotActive)
		_, err = blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
		requireCode(t, err, foundation.ProjectNotActive)
		digest, err := wc.TaskBlockerAddDigest(a, command, p.ID, target.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = blockers.LookupTaskBlockerCommand(ctxFor(t), a, wc.TaskBlockerCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: command.IdempotencyKey, SemanticDigest: digest})
		requireCode(t, err, foundation.ProjectNotActive)
		if base.blockerSnapshot(t, a) != before || activity.calls.Load() != 0 {
			t.Fatal("deleting let retained receipt bypass current authority")
		}
	})
}
