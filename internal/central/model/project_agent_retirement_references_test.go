package model

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls provide SQL rows and a deliberately controlled applied owner.
// They prove this participant's ordering and exact index checks, not a real
// Project claim, canonical Agent deletion or PostgreSQL commit.
type modelRetirementWitness struct{}
type modelRetirementOwner struct {
	tx     f.Tx
	change ac.ProjectRetirementChange
	live   bool
	calls  int
}

func (*modelRetirementOwner) RequireProjectRetirementPreparedInTx(context.Context, f.Tx, ac.ProjectRetirementChange) error {
	return f.NewFault(f.Forbidden, f.NotStarted)
}
func (o *modelRetirementOwner) RequireProjectRetirementAppliedInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	o.calls++
	r, expected := change.Request, o.change.Request
	if !o.live || tx != o.tx || ctx.Value(modelRetirementWitness{}) != o || !r.Actor.Equal(expected.Actor) || r.ProjectID != expected.ProjectID || r.AgentID != expected.AgentID || r.Cause != expected.Cause || r.ExpectedVersion != expected.ExpectedVersion || !reflect.DeepEqual(change.Before.Fields(), o.change.Before.Fields()) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

type modelRetirementStore struct {
	Store      // Any unplanned AcquireAll/WithinTx/ordinary query is a test failure.
	t          *testing.T
	tx         f.Tx
	live       bool
	owner      *modelRetirementOwner
	held       map[string]f.LockMode
	refs       []referenceRecord
	reads      int
	writes     int
	readErr    error
	writeErr   error
	countMiss  bool
	entered    chan struct{}
	release    chan struct{}
	badColumns bool
}

func (s *modelRetirementStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.tx {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return s, nil
}
func (s *modelRetirementStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.live || tx != s.tx {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	for _, lock := range locks {
		mode, ok := s.held[lock.Key.Canonical()]
		if !ok || mode != lock.Mode && mode != f.Exclusive {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
	}
	return nil
}
func (s *modelRetirementStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if s.owner.calls == 0 || ctx.Value(modelRetirementWitness{}) != s.owner || len(args) != 1 || args[0] != s.owner.change.Request.AgentID.String() || strings.Contains(query, "project_id=$") {
		s.t.Fatal("private Model SQL before exact owner or with an incomplete owner filter")
	}
	s.reads++
	return scanFunc(func(out ...any) error {
		if s.entered != nil {
			close(s.entered)
			<-s.release // Cancellation is not the original Scan return.
		}
		if s.readErr != nil {
			return s.readErr
		}
		var roles, projects, models, efforts []string
		var versions []int64
		for _, ref := range s.refs {
			roles, projects, models, efforts = append(roles, ref.Role), append(projects, ref.Project), append(models, ref.Model), append(efforts, ref.Effort)
			versions = append(versions, int64(ref.Version))
		}
		if s.badColumns {
			projects = append(projects, "unexpected-column-item")
		}
		*out[0].(*[]string), *out[1].(*[]string), *out[2].(*[]string), *out[3].(*[]int64), *out[4].(*[]string) = roles, projects, models, versions, efforts
		return nil
	})
}
func (s *modelRetirementStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	request := s.owner.change.Request
	if s.owner.calls == 0 || ctx.Value(modelRetirementWitness{}) != s.owner || !strings.HasPrefix(query, "DELETE FROM agenteam_model.references") || len(args) != 3 || args[0] != request.AgentID.String() || args[1] != request.ProjectID.String() || args[2] != int64(request.ExpectedVersion) {
		s.t.Fatal("write escaped the original Agent index or owner version")
	}
	s.writes++
	if s.writeErr != nil {
		return pgconn.CommandTag{}, s.writeErr
	}
	n := len(s.refs)
	if s.countMiss {
		n = 0
	} else {
		s.refs = nil
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", n)), nil
}

func modelRetirementFixture(t *testing.T, approval bool) (*ProjectAgentRetirementReferences, *modelRetirementStore, context.Context, ac.ProjectRetirementChange) {
	t.Helper()
	before := skillVersionChange(t, !approval).Before
	core := before.Fields().Core
	op := skillVersionID[pc.Operation](t, 20)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(core.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	change := ac.ProjectRetirementChange{Request: ac.ProjectRetirementRequest{Actor: actor, ProjectID: core.ProjectID, AgentID: core.ID, Cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: 2}, ExpectedVersion: core.Version}, Before: before}
	if err = change.Validate(); err != nil {
		t.Fatal(err)
	}
	owner := &modelRetirementOwner{tx: f.NewTx(), change: change.Clone(), live: true}
	store := &modelRetirementStore{t: t, tx: owner.tx, live: true, owner: owner, held: map[string]f.LockMode{}}
	effort := ""
	if core.ReasoningEffort != nil {
		effort = *core.ReasoningEffort
	}
	store.refs = []referenceRecord{{Kind: "agent", Owner: core.ID.String(), Role: "agent_model", Project: core.ProjectID.String(), Model: core.ModelRef.String(), Version: core.Version, Effort: effort}}
	if core.ApprovalModelRef != nil {
		store.refs = append(store.refs, referenceRecord{Kind: "agent", Owner: core.ID.String(), Role: "approval_model", Project: core.ProjectID.String(), Model: core.ApprovalModelRef.String(), Version: core.Version})
	}
	adapter, err := NewProjectAgentRetirementReferences(store, owner)
	if err != nil {
		t.Fatal(err)
	}
	locks, err := adapter.ProjectRetirementLocks(change)
	if err != nil {
		t.Fatal(err)
	}
	for _, lock := range locks {
		store.held[lock.Key.Canonical()] = lock.Mode
	}
	return adapter, store, context.WithValue(context.Background(), modelRetirementWitness{}, owner), change
}

func TestProjectAgentRetirementModelCompleteIndex(t *testing.T) {
	for _, mode := range []string{"primary", "approval", "same-model-two-roles"} {
		t.Run(mode, func(t *testing.T) {
			adapter, store, ctx, change := modelRetirementFixture(t, mode != "primary")
			if mode == "same-model-two-roles" {
				fields := change.Before.Fields()
				fields.Core.ApprovalModelRef = &fields.Core.ModelRef
				var err error
				change.Before, err = ac.NewAgentConfig(fields)
				if err != nil {
					t.Fatal(err)
				}
				store.owner.change = change.Clone()
				store.refs[1].Model = store.refs[0].Model
			}
			locks, err := adapter.ProjectRetirementLocks(change)
			if err != nil || store.reads != 0 || store.writes != 0 || store.owner.calls != 0 || adapter.ProjectRetirementReferenceKind() != ac.ProjectRetirementModelReferences {
				t.Fatal("lock collection performed I/O or changed the fixed kind", err)
			}
			registry, _ := f.SystemConfigLock("model-references")
			if !slices.ContainsFunc(locks, func(lock f.LockRequest) bool {
				return lock.Key.Canonical() == registry.Canonical() && lock.Mode == f.Exclusive
			}) {
				t.Fatal("retirement omitted the reference registry EX gate")
			}
			if err = adapter.RequireProjectAgentReferencesRetiredInTx(ctx, store.tx, change); err == nil {
				t.Fatal("present references were reported retired")
			}
			if err = adapter.RetireProjectAgentReferencesInTx(ctx, store.tx, change); err != nil || store.writes != 1 || len(store.refs) != 0 {
				t.Fatal("complete original Model pair was not retired", err)
			}
			if err = adapter.RequireProjectAgentReferencesRetiredInTx(ctx, store.tx, change); err != nil || store.writes != 1 {
				t.Fatal("read-only scoped confirmation changed the index", err)
			}
			if err = adapter.RetireProjectAgentReferencesInTx(ctx, store.tx, change); err == nil || store.writes != 1 {
				t.Fatal("an empty index was treated as an original preimage")
			}
			store.owner.live = false
			before := store.reads
			if err = adapter.RequireProjectAgentReferencesRetiredInTx(context.WithoutCancel(ctx), store.tx, change); err == nil || store.reads != before {
				t.Fatal("empty index bypassed the expired original owner")
			}
		})
	}
}

func TestProjectAgentRetirementModelRejectsPreimage(t *testing.T) {
	for _, mode := range []string{"empty", "partial", "extra-role", "foreign-project", "stale-version", "wrong-model", "wrong-effort", "malformed-columns", "scan-error", "write-error", "count-miss"} {
		t.Run(mode, func(t *testing.T) {
			adapter, store, ctx, change := modelRetirementFixture(t, true)
			sentinel := errors.New("controlled original SQL error")
			switch mode {
			case "empty":
				store.refs = nil
			case "partial":
				store.refs = store.refs[:1]
			case "extra-role":
				store.refs = append(store.refs, store.refs[0])
			case "foreign-project":
				store.refs[0].Project = skillVersionID[i.Project](t, 90).String()
			case "stale-version":
				store.refs[1].Version++
			case "wrong-model":
				store.refs[0].Model = skillVersionID[mc.Model](t, 91).String()
			case "wrong-effort":
				store.refs[1].Effort = "unexpected-approval-effort"
			case "malformed-columns":
				store.badColumns = true
			case "scan-error":
				store.readErr = sentinel
			case "write-error":
				store.writeErr = sentinel
			case "count-miss":
				store.countMiss = true
			}
			before := slices.Clone(store.refs)
			err := adapter.RetireProjectAgentReferencesInTx(ctx, store.tx, change)
			if err == nil || !slices.Equal(before, store.refs) {
				t.Fatal("invalid original index was accepted or changed", err)
			}
			if mode != "write-error" && mode != "count-miss" && store.writes != 0 {
				t.Fatal("rejected preimage reached DELETE")
			}
			if (mode == "scan-error" || mode == "write-error") && !errors.Is(err, sentinel) {
				t.Fatal("original SQL failure was replaced", err)
			}
		})
	}
}

func TestProjectAgentRetirementModelRequiresOriginalOwner(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "expired-tx", "missing-lock", "registry-sh", "saved-owner", "public-change-only", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			adapter, store, ctx, change := modelRetirementFixture(t, false)
			tx := store.tx
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				store.live = false
			case "missing-lock":
				store.held = nil
			case "registry-sh":
				key, _ := f.SystemConfigLock("model-references")
				store.held[key.Canonical()] = f.Shared
			case "saved-owner":
				store.owner.live = false
				ctx = context.WithoutCancel(ctx)
			case "public-change-only":
				ctx = context.Background()
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := adapter.RetireProjectAgentReferencesInTx(ctx, tx, change); err == nil {
				t.Fatal("invalid original owner admitted mutation")
			}
			store.refs = nil
			if err := adapter.RequireProjectAgentReferencesRetiredInTx(ctx, tx, change); err == nil || store.reads != 0 || store.writes != 0 {
				t.Fatal("empty index bypassed current authority", err)
			}
		})
	}
	var absent *modelRetirementOwner
	if _, err := NewProjectAgentRetirementReferences(&modelRetirementStore{}, absent); err == nil {
		t.Fatal("typed nil owner accepted")
	}
	if _, err := NewProjectAgentRetirementReferences(nil, &modelRetirementOwner{}); err == nil {
		t.Fatal("absent Store accepted")
	}
}

func TestProjectAgentRetirementModelWaitsForOriginalScan(t *testing.T) {
	for _, confirmation := range []bool{false, true} {
		t.Run(fmt.Sprintf("confirmation-%t", confirmation), func(t *testing.T) {
			adapter, store, ctx, change := modelRetirementFixture(t, false)
			if confirmation {
				store.refs = nil
			}
			store.entered, store.release = make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			joined, released := false, false
			t.Cleanup(func() {
				cancel()
				if !released {
					close(store.release)
				}
				if !joined {
					<-done
				}
			})
			go func() {
				if confirmation {
					done <- adapter.RequireProjectAgentReferencesRetiredInTx(ctx, store.tx, change)
				} else {
					done <- adapter.RetireProjectAgentReferencesInTx(ctx, store.tx, change)
				}
			}()
			select {
			case <-store.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original controlled Scan was not entered")
			}
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancellation substituted for original Scan return")
			default:
			}
			close(store.release)
			released = true
			select {
			case err := <-done:
				joined = true
				if !errors.Is(err, context.Canceled) || store.writes != 0 {
					t.Fatal("cancelled original read published progress", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("original controlled Scan did not return")
			}
		})
	}
}
