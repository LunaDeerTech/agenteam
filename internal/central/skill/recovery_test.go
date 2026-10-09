package skill

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

type recoveryStore struct {
	*initializationWriteStore
	passes      map[string]int64
	raw         []string
	passFailure bool
}

func (s *recoveryStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.initializationWriteStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *recoveryStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	before := map[string]int64{}
	for k, v := range s.passes {
		before[k] = v
	}
	r := s.initializationWriteStore.WithinTx(ctx, cause, fn)
	if r.State() == f.NotCommitted {
		s.passes = before
	}
	return r
}
func (s *recoveryStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.HasPrefix(q, "SELECT COALESCE(array_agg") {
		if s.raw != nil {
			return skillRowValues{values: []any{s.raw}}
		}
		var ids []string
		for key, v := range s.work {
			if v[5] != "joined" {
				ids = append(ids, key)
			}
		}
		sort.Slice(ids, func(i, j int) bool {
			if s.passes[ids[i]] != s.passes[ids[j]] {
				return s.passes[ids[i]] < s.passes[ids[j]]
			}
			return ids[i] < ids[j]
		})
		if len(ids) > MaxRecoveryWork {
			ids = ids[:MaxRecoveryWork]
		}
		return skillRowValues{values: []any{ids}}
	}
	return s.initializationWriteStore.QueryRow(ctx, q, args...)
}
func (s *recoveryStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(q, "UPDATE agenteam_skill.work SET recovery_pass=") {
		if s.passFailure {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		v := s.work[args[0].(string)]
		if len(v) != 9 || v[3] != args[1] || v[6] != args[2] || v[5] == "joined" {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		var max int64
		for _, p := range s.passes {
			if p > max {
				max = p
			}
		}
		s.passes[args[0].(string)] = max + 1
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return s.initializationWriteStore.Exec(ctx, q, args...)
}

type recoveryProcesses struct {
	t          *testing.T
	store      *recoveryStore
	want       oc.ProcessID
	err        error
	calls      int
	afterProof func()
}

func (p *recoveryProcesses) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	p.calls++
	if ctx.Err() != nil || p.store.live || process != p.want {
		p.t.Fatal("not exact former process before original transaction")
	}
	if p.afterProof != nil {
		p.afterProof()
	}
	return p.err
}
func recoveryFixture(t *testing.T) (*Service, *recoveryStore, *initializationReadProjects, *recoveryProcesses, workFact) {
	t.Helper()
	s, base, projects, _, _ := readFixture(t)
	row, e := loadInitialization(context.Background(), base, stateRow(t).request.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	w := workFact{id: stateID[skillWork](191), project: row.request.ProjectID, skill: row.skill, process: stateID[oc.Process](192), kind: initializationWork, phase: workRunning, fence: 1, created: row.created}
	store := &recoveryStore{initializationWriteStore: &initializationWriteStore{initializationReadStore: base, work: map[string][]any{w.id.String(): {w.id.String(), w.project.String(), w.skill.String(), w.process.String(), string(w.kind), string(w.phase), int64(1), w.created.Time(), (*time.Time)(nil)}}}, passes: map[string]int64{}}
	p := &recoveryProcesses{t: t, store: store, want: w.process}
	s.state().authority.state().store = store
	s.state().processes = p
	return s, store, projects, p, w
}

func TestSkillRecoveryExactDeathOriginalTransactionAndCurrentGate(t *testing.T) {
	for _, name := range []string{"foreign_dead", "foreign_live", "same_process_missing", "same_process_active", "same_process_returned", "gate_denied", "fence_changed", "commit_unknown", "pass_conflict", "already_joined"} {
		t.Run(name, func(t *testing.T) {
			s, store, projects, processes, w := recoveryFixture(t)
			ctx := context.Background()
			sentinel := fault(f.ResourceBusy)
			var local *ownedWork
			switch name {
			case "foreign_live":
				processes.err = sentinel
			case "same_process_missing", "same_process_active", "same_process_returned":
				w.process = s.state().process
				store.work[w.id.String()][3] = w.process.String()
				if name != "same_process_missing" {
					row, e := loadInitialization(ctx, store, w.project)
					if e != nil {
						t.Fatal(e)
					}
					local = &ownedWork{fact: w, row: *row, returned: name == "same_process_returned"}
					s.state().work[w.id] = local
				}
			case "gate_denied":
				projects.gate = sentinel
			case "fence_changed":
				processes.afterProof = func() { store.work[w.id.String()][6] = int64(2) }
			case "commit_unknown":
				store.unknownAt = 1
			case "pass_conflict":
				store.passFailure = true
			case "already_joined":
				processes.afterProof = func() {
					store.work[w.id.String()][5] = "joined"
					at := w.created.Time()
					store.work[w.id.String()][8] = &at
				}
			}
			report, e := s.Recover(ctx)
			ok := name == "foreign_dead" || name == "same_process_returned" || name == "already_joined"
			if report.Examined != 1 {
				t.Fatal("not bounded actual candidate")
			}
			if ok {
				if e != nil || report.Joined != 1 || report.Pending != 0 || store.work[w.id.String()][5] != "joined" {
					t.Fatal("known terminal not joined", e, report)
				}
				if local != nil && s.state().work[w.id] != nil {
					t.Fatal("local returned ownership retained after commit")
				}
			} else {
				if e == nil || report.Joined != 0 || report.Pending != 1 {
					t.Fatal("unproven join", e, report)
				}
				if name == "foreign_live" || name == "gate_denied" {
					if e != sentinel || store.passes[w.id.String()] != 1 {
						t.Fatal("error identity/fairness lost")
					}
				}
				if name == "commit_unknown" {
					if _, ok := UnknownAttempt(e); !ok {
						t.Fatal("Unknown lost")
					}
				}
				if name != "commit_unknown" && store.work[w.id.String()][5] == "joined" {
					t.Fatal("false durable join")
				}
			}
			if strings.HasPrefix(name, "same_process") && processes.calls != 0 {
				t.Fatal("self death inference")
			}
			if !strings.HasPrefix(name, "same_process") && processes.calls != 1 {
				t.Fatal("missing exact death proof")
			}
		})
	}
}

func TestSkillRecoveryBusyHeadPersistsPositionAndLeavesNoBodyWork(t *testing.T) {
	s, store, _, processes, w := recoveryFixture(t)
	// A full first batch is occupied by busy same-process records. The next
	// durable pass must reach the 101st foreign dead record without replacing
	// the service or relying on an in-memory cursor.
	delete(store.work, w.id.String())
	for n := 1; n <= 101; n++ {
		key := stateID[skillWork](n).String()
		process := s.state().process
		if n == 101 {
			process = w.process
		}
		store.work[key] = []any{key, w.project.String(), w.skill.String(), process.String(), string(w.kind), "running", int64(1), w.created.Time(), (*time.Time)(nil)}
	}
	one, e := s.Recover(context.Background())
	if e == nil || one.Examined != 100 || one.Pending != 100 || one.Joined != 0 || processes.calls != 0 {
		t.Fatal("first bound", one, e)
	}
	// Recreate Service with the same real persistent Store and fixed process.
	next, e := New(Dependencies{Authority: s.state().authority, Objects: s.state().objects, Processes: processes, ProcessID: s.state().process, Bundle: s.state().bundle})
	if e != nil {
		t.Fatal(e)
	}
	two, e := next.Recover(context.Background())
	if e == nil || two.Examined != 100 || two.Joined != 1 || two.Pending != 99 || processes.calls != 1 {
		t.Fatal("busy head starved durable tail", two, e)
	}
	if store.work[stateID[skillWork](101).String()][5] != "joined" {
		t.Fatal("tail not actually joined")
	}
}

func TestSkillRecoveryRejectsMalformedCandidatesAndOriginalBudget(t *testing.T) {
	for _, name := range []string{"duplicate", "invalid", "oversize", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			s, store, _, processes, w := recoveryFixture(t)
			ctx := context.Background()
			switch name {
			case "duplicate":
				store.raw = []string{w.id.String(), w.id.String()}
			case "invalid":
				store.raw = []string{"foreign"}
			case "oversize":
				for n := 0; n <= MaxRecoveryWork; n++ {
					store.raw = append(store.raw, w.id.String())
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			report, e := s.Recover(ctx)
			if e == nil || report.Examined != 0 || store.txs != 0 || processes.calls != 0 {
				t.Fatal("invalid discovery performed work")
			}
			if name == "cancelled" && !errors.Is(e, context.Canceled) {
				t.Fatal("original caller error lost")
			}
		})
	}
}
