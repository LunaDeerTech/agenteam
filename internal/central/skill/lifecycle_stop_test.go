package skill

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// These controlled Store/Project ports exercise local ownership, current-gate
// ordering and original commit outcomes. They are not real PG lifecycle proof.
type stopStore struct {
	*initializationWriteStore
	raw []string
}

func (s *stopStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.initializationWriteStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *stopStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.HasPrefix(q, "SELECT COALESCE(array_agg") {
		if !s.live {
			return skillRowValues{err: errors.New("stop discovery outside transaction")}
		}
		if s.raw != nil {
			return skillRowValues{values: []any{s.raw}}
		}
		var raw []string
		for key, v := range s.work {
			if v[1] == args[0] && v[5] != "joined" && (args[1].(bool) || v[4] == "initialization") {
				raw = append(raw, key)
			}
		}
		sort.Strings(raw)
		if len(raw) > 101 {
			raw = raw[:101]
		}
		return skillRowValues{values: []any{raw}}
	}
	return s.initializationWriteStore.QueryRow(ctx, q, args...)
}

type stopProjects struct {
	ProjectPorts
	t      *testing.T
	store  *stopStore
	actor  id.Actor
	cause  pc.LifecycleCause
	scope  pc.ScopeRef
	err    error
	calls  int
	denyAt int
}

func (p *stopProjects) ValidateLifecycleInTx(ctx context.Context, tx f.Tx, actor id.Actor, cause pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
	p.calls++
	if !p.store.live || tx != p.store.tx || ctx.Err() != nil || !actor.Equal(p.actor) || cause != p.cause || participant != pc.SkillsParticipant || phase != pc.StopPhase {
		p.t.Fatal("lifecycle source context, actor, transaction or exact cause changed")
	}
	row, e := loadInitialization(ctx, p.store, p.scope.ProjectID)
	if e != nil {
		p.t.Fatal(e)
	}
	locks := []f.LockRequest{projectLock(p.scope.ProjectID, f.Exclusive)}
	if row != nil {
		locks, e = row.locks(f.Exclusive, row.object)
	}
	if e != nil || p.store.RequireHeldLocks(ctx, tx, locks) != nil {
		p.t.Fatal("lifecycle gate without original complete locks", e)
	}
	if p.denyAt == 0 || p.calls == p.denyAt {
		return p.err
	}
	return nil
}

type stopProcesses struct {
	t        *testing.T
	store    *stopStore
	projects *stopProjects
	want     oc.ProcessID
	err      error
	calls    int
	after    func()
}

func (p *stopProcesses) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	p.calls++
	if p.store.live || p.projects.calls == 0 || process != p.want || ctx.Err() != nil {
		p.t.Fatal("process proof skipped current gate or ran inside SQL")
	}
	if p.after != nil {
		p.after()
	}
	return p.err
}

func bindStopFixture(t *testing.T, s *Service, base *initializationWriteStore, action pc.LifecycleAction) (*stopStore, *stopProjects, *stopProcesses) {
	t.Helper()
	row, e := loadInitialization(context.Background(), base, stateRow(t).request.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	store := &stopStore{initializationWriteStore: base}
	if store.work == nil {
		store.work = map[string][]any{}
	}
	cause := pc.LifecycleCause{OperationID: stateID[pc.Operation](210), Action: action, ProjectVersion: 4}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: row.request.ProjectID}
	registration, _ := id.RegisterService(id.ProjectLifecycle)
	actorScope, _ := id.InProject(scope.ProjectID)
	actor, e := registration.Actor(cause.OperationID.String(), actorScope)
	if e != nil {
		t.Fatal(e)
	}
	projects := &stopProjects{ProjectPorts: s.state().authority.state().projects, t: t, store: store, actor: actor, cause: cause, scope: scope}
	processes := &stopProcesses{t: t, store: store, projects: projects, want: stateID[oc.Process](211)}
	s.state().authority.state().store = store
	s.state().authority.state().projects = projects
	s.state().processes = processes
	return store, projects, processes
}

func newStopFixture(t *testing.T, action pc.LifecycleAction) (*Service, *stopStore, *stopProjects, *stopProcesses) {
	t.Helper()
	s, base, _, _, _ := readFixture(t)
	store, projects, processes := bindStopFixture(t, s, &initializationWriteStore{initializationReadStore: base}, action)
	return s, store, projects, processes
}

func TestSkillLifecycleStopCurrentGateAndExactCalls(t *testing.T) {
	for _, name := range []string{"archive", "delete", "denied", "commit_unknown", "missing_domain", "wrong_actor", "wrong_scope", "meeting", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			action := pc.Archive
			if name == "delete" {
				action = pc.Delete
			}
			s, store, projects, processes := newStopFixture(t, action)
			initialize, e := s.beginProjectWork(context.Background(), projects.scope.ProjectID, initializationWork)
			if e != nil {
				t.Fatal(e)
			}
			reader, _ := s.beginProjectWork(context.Background(), projects.scope.ProjectID, packageReaderWork)
			other, _ := s.beginProjectWork(context.Background(), stateID[id.Project](212), initializationWork)
			t.Cleanup(func() { s.end(initialize); s.end(reader); s.end(other) })
			actor, scope, ctx := projects.actor, projects.scope, context.Background()
			sentinel := fault(f.Forbidden)
			switch name {
			case "denied":
				projects.err = sentinel
			case "commit_unknown":
				store.unknownAt = 1
			case "missing_domain":
				store.row = skillRowValues{err: pgx.ErrNoRows}
			case "wrong_actor":
				actor = id.Actor{}
			case "wrong_scope":
				scope.ProjectID = other.project
			case "meeting":
				scope.Kind = pc.MeetingScope
				meeting := stateID[pc.Meeting](213)
				scope.MeetingID = &meeting
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			report, e := s.RequestStop(ctx, actor, projects.cause, scope)
			if name != "archive" && name != "delete" {
				if e == nil || report.Matches(pc.SkillsParticipant, projects.cause, scope) || initialize.ctx.Err() != nil || reader.ctx.Err() != nil {
					t.Fatal("unconfirmed authorization cancelled a captured call", e)
				}
				if name == "denied" && e != sentinel {
					t.Fatal("current gate error identity lost")
				}
				if name == "commit_unknown" {
					if _, ok := UnknownAttempt(e); !ok {
						t.Fatal("original stop transaction Unknown lost")
					}
				}
			} else {
				want := 1
				if action == pc.Delete {
					want = 2
				}
				if e != nil || !report.Matches(pc.SkillsParticipant, projects.cause, scope) || report.Details().State != pc.StopPending || len(report.Details().ActiveRefs) != want || initialize.ctx.Err() != context.Canceled || (reader.ctx.Err() == context.Canceled) != (action == pc.Delete) {
					t.Fatal("scope/action/cancel is not actual join", e, report.Details())
				}
				// Inspect is observational: a new captured call remains uncancelled.
				late, _ := s.beginProjectWork(context.Background(), scope.ProjectID, initializationWork)
				t.Cleanup(func() { s.end(late) })
				if _, e = s.InspectStop(ctx, actor, projects.cause, scope); e != nil || late.ctx.Err() != nil {
					t.Fatal("inspection started a second stop", e)
				}
			}
			if other.ctx.Err() != nil || s.state().stopped || processes.calls != 0 {
				t.Fatal("Project stop became global stop or invented death proof")
			}
		})
	}
}

func putStopWork(store *stopStore, w workFact) {
	store.work[w.id.String()] = []any{w.id.String(), w.project.String(), w.skill.String(), w.process.String(), string(w.kind), string(w.phase), int64(w.fence), w.created.Time(), (*time.Time)(nil)}
}

func TestSkillLifecycleStopRequiresActualLocalOrForeignProof(t *testing.T) {
	for _, name := range []string{"local_active", "local_missing", "local_returned", "local_registration_absent", "foreign_dead", "foreign_live", "changed_fence", "retirement_unknown", "gate_changed"} {
		t.Run(name, func(t *testing.T) {
			s, store, projects, processes := newStopFixture(t, pc.Delete)
			row, _ := loadInitialization(context.Background(), store, projects.scope.ProjectID)
			w := workFact{id: stateID[skillWork](214), project: row.request.ProjectID, skill: row.skill, process: processes.want, kind: initializationWork, phase: workRunning, fence: 1, created: row.created}
			var local *ownedWork
			if strings.HasPrefix(name, "local_") || name == "retirement_unknown" {
				w.process = s.state().process
				if name != "local_missing" {
					original, _ := s.beginProjectWork(context.Background(), w.project, w.kind)
					w.id = original.workID
					local = &ownedWork{fact: w, row: *row, call: original, returned: name != "local_active"}
					s.state().work[w.id] = local
					if local.returned {
						s.end(original)
					} else {
						t.Cleanup(func() { s.end(original) })
					}
				}
			}
			putStopWork(store, w)
			if name == "local_registration_absent" {
				delete(store.work, w.id.String())
			}
			sentinel := fault(f.ResourceBusy)
			switch name {
			case "foreign_live":
				processes.err = sentinel
			case "changed_fence":
				processes.after = func() { store.work[w.id.String()][6] = int64(2) }
			case "retirement_unknown":
				store.unknownAt = 2
			case "gate_changed":
				projects.err, projects.denyAt = sentinel, 2
			}
			report, e := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
			complete := name == "local_returned" || name == "local_registration_absent" || name == "foreign_dead"
			if complete {
				joined := name == "local_registration_absent" && store.work[w.id.String()] == nil || name != "local_registration_absent" && store.work[w.id.String()][5] == "joined"
				if e != nil || report.Details().State != pc.Stopped || !joined || s.state().work[w.id] != nil {
					t.Fatal("proven original work did not retire", e, report.Details())
				}
			} else if name == "retirement_unknown" {
				if _, ok := UnknownAttempt(e); !ok || s.state().work[w.id] != local {
					t.Fatal("unconfirmed retirement forgot original owner", e)
				}
				if next, e := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope); e != nil || next.Details().State != pc.Stopped || s.state().work[w.id] != nil {
					t.Fatal("original retirement could not be reinspected", e)
				}
			} else {
				if store.work[w.id.String()][5] == "joined" || report.Details().State == pc.Stopped {
					t.Fatal("unproven work reported stopped")
				}
				if (name == "foreign_live" || name == "gate_changed") && e != sentinel {
					t.Fatal("proof or current-gate error identity lost", e)
				}
			}
			if w.process == s.state().process && processes.calls != 0 {
				t.Fatal("same process missing record used death oracle")
			}
		})
	}
}

func TestSkillLifecycleStopBoundDoesNotInventEmptyTail(t *testing.T) {
	s, store, projects, processes := newStopFixture(t, pc.Delete)
	row, _ := loadInitialization(context.Background(), store, projects.scope.ProjectID)
	for n := 1; n <= 101; n++ {
		putStopWork(store, workFact{id: stateID[skillWork](n), project: row.request.ProjectID, skill: row.skill, process: processes.want, kind: initializationWork, phase: workRunning, fence: 1, created: row.created})
	}
	first, e := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
	if e != nil || first.Details().State != pc.StopPending || processes.calls != 100 || store.work[stateID[skillWork](101).String()][5] == "joined" {
		t.Fatal("bounded first page invented completion", e, first.Details())
	}
	last, e := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
	if e != nil || last.Details().State != pc.Stopped || processes.calls != 101 {
		t.Fatal("remaining actual tail not reached", e, last.Details())
	}
}

func TestSkillLifecycleStopKeepsActualDiscardAndReaderOwners(t *testing.T) {
	t.Run("archive_reader", func(t *testing.T) {
		s, base, _, o := packageFixture(t)
		r, e := s.OpenPackage(context.Background(), o.actor, o.row.request.ProjectID, o.row.skill, 1)
		if e != nil {
			t.Fatal(e)
		}
		_, projects, _ := bindStopFixture(t, s, base, pc.Archive)
		report, e := s.RequestStop(context.Background(), projects.actor, projects.cause, projects.scope)
		if e != nil || report.Details().State != pc.Stopped || o.body.closes.Load() != 0 {
			t.Fatal("archive stopped a lawful existing reader", e)
		}
		body, e := io.ReadAll(r)
		if e != nil || sum(body) != o.meta.PackageSHA256 {
			t.Fatal("archive changed immutable package access", e)
		}
		if e = r.Close(); e != nil || !r.Joined() {
			t.Fatal("archive reader actual close failed", e)
		}
	})
	t.Run("discard", func(t *testing.T) {
		s, read, originalProjects, actor, request := readFixture(t)
		read.row.values = stateValues(t)
		base := &initializationWriteStore{initializationReadStore: read}
		store, projects, _ := bindStopFixture(t, s, base, pc.Delete)
		o := &initializationObjects{t: t, store: base, projects: originalProjects, issuer: oc.NewAccessIssuer(), row: stateRow(t), discardStarted: make(chan struct{}), discardRelease: make(chan struct{})}
		s.state().objects = o
		returned := make(chan error, 1)
		go func() { _, e := s.InitializeProjectSkills(context.Background(), actor, request); returned <- e }()
		<-o.discardStarted
		report, e := s.RequestStop(context.Background(), projects.actor, projects.cause, projects.scope)
		if e != nil || report.Details().State != pc.StopPending || len(report.Details().ActiveRefs) != 1 {
			t.Fatal("actual Discard incorrectly stopped", e)
		}
		for _, row := range store.work {
			if row[5] == "joined" {
				t.Fatal("blocked Discard joined")
			}
		}
		close(o.discardRelease)
		if e := <-returned; !errors.Is(e, context.Canceled) {
			t.Fatal("original cancelled tail lost", e)
		}
		last, e := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
		if e != nil || last.Details().State != pc.Stopped {
			t.Fatal("actual Discard return not joined", e)
		}
	})
	t.Run("reader", func(t *testing.T) {
		s, base, _, o := packageFixture(t)
		o.body.started, o.body.release = make(chan struct{}), make(chan struct{})
		r, e := s.OpenPackage(context.Background(), o.actor, o.row.request.ProjectID, o.row.skill, 1)
		if e != nil {
			t.Fatal(e)
		}
		_, projects, _ := bindStopFixture(t, s, base, pc.Delete)
		readDone := make(chan error, 1)
		go func() { _, e := r.Read(make([]byte, 1)); readDone <- e }()
		<-o.body.started
		report, e := s.RequestStop(context.Background(), projects.actor, projects.cause, projects.scope)
		if e != nil || report.Details().State != pc.StopPending || len(report.Details().ActiveRefs) != 1 {
			t.Fatal("actual Read incorrectly stopped", e)
		}
		<-o.body.closed
		if report, e = s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope); e != nil || report.Details().State != pc.StopPending {
			t.Fatal("Close/cancel joined blocked Read", e)
		}
		close(o.body.release)
		if e := <-readDone; !errors.Is(e, io.ErrClosedPipe) {
			t.Fatal(e)
		}
		waitPackageCalls(t, s)
		if report, e = s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope); e != nil || report.Details().State != pc.Stopped || o.body.closes.Load() != 1 {
			t.Fatal("actual reader return did not retire", e)
		}
	})
}
