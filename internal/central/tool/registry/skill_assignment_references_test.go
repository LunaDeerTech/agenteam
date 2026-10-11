package registry

import (
	"context"
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
)

// Only pre-read ordering is controlled here: postgres.Rows remains owned by
// its real implementation. Successful complete reference reading/CAS belongs
// to the real PG chain, not a fabricated SQL reader or canonical Agent witness.
type skillVersionWitness struct{}
type skillVersionOwner struct {
	calls  int
	denied bool
}

func (o *skillVersionOwner) RequireSkillAssignmentVersionAppliedInTx(ctx context.Context, _ f.Tx, _ ac.SkillAssignmentVersionChange) error {
	o.calls++
	if o.denied || ctx.Value(skillVersionWitness{}) != o {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

type skillVersionRow struct{ values []any }

func (r skillVersionRow) Scan(out ...any) error {
	if len(out) != len(r.values) {
		return fmt.Errorf("controlled row shape")
	}
	for n, value := range r.values {
		reflect.ValueOf(out[n]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

type skillVersionStore struct {
	Store
	t      *testing.T
	tx     f.Tx
	owner  *skillVersionOwner
	change ac.SkillAssignmentVersionChange
	held   []f.LockRequest
	mode   string
	reads  int
}

func (s *skillVersionStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return s, nil
}
func (s *skillVersionStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx || !slices.EqualFunc(s.held, locks, func(a, b f.LockRequest) bool { return f.CompareLockKeys(a.Key, b.Key) == 0 && a.Mode == b.Mode }) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (s *skillVersionStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if s.owner.calls != 1 || ctx.Value(skillVersionWitness{}) != s.owner {
		s.t.Fatal("SQL before owner witness")
	}
	if !strings.Contains(query, "agent_configurations WHERE agent_id=$1") || len(args) != 1 || args[0] != s.change.Request.AgentID.String() {
		s.t.Fatal("incomplete Tool owner head query")
	}
	s.reads++
	p := s.change.Request.ProjectID.String()
	if s.mode == "foreign-scope" {
		p = skillVersionID[i.Project](s.t, 99).String()
	}
	return skillVersionRow{[]any{p, s.change.Request.ExpectedVersion}}
}
func (s *skillVersionStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	if s.owner.calls != 1 || ctx.Value(skillVersionWitness{}) != s.owner {
		s.t.Fatal("SQL reader before owner witness")
	}
	if !strings.Contains(query, "agent_references WHERE agent_id=$1") || len(args) != 1 || args[0] != s.change.Request.AgentID.String() {
		s.t.Fatal("incomplete Tool reference query")
	}
	s.reads++
	return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
}
func skillVersionID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func skillVersionChange(t *testing.T) ac.SkillAssignmentVersionChange {
	t.Helper()
	p, a := skillVersionID[i.Project](t, 1), skillVersionID[i.Agent](t, 2)
	actor, err := i.NewHuman(skillVersionID[i.User](t, 3), skillVersionID[i.Session](t, 4))
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("project", []string{p.String(), a.String()}, "skill.assign", "skill-version-control")
	if err != nil {
		t.Fatal(err)
	}
	at, err := f.NewInstant(time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	config, err := ac.NewAgentConfig(ac.AgentConfigFields{Core: ac.AgentCore{ID: a, ProjectID: p, Name: "skill-agent", NormalizedName: "skill-agent", ModelRef: skillVersionID[mc.Model](t, 5), ApprovalPolicy: ac.ApprovalDefault, Lifecycle: ac.AgentActive, Version: 7, CreatedAt: at, UpdatedAt: at}, AllowedToolIDs: []i.ToolID{skillVersionID[i.Tool](t, 7)}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	change := ac.SkillAssignmentVersionChange{Request: ac.SkillAssignmentRequest{Actor: actor, ProjectID: p, AgentID: a, Command: command, ExpectedVersion: 7}, Before: config, ResultVersion: 8}
	if err = change.Validate(); err != nil {
		t.Fatal(err)
	}
	return change
}
func TestSkillAssignmentToolReferencesRequireCurrentWitness(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "missing-locks", "owner-denied", "foreign-scope", "original-reader-failure"} {
		t.Run(mode, func(t *testing.T) {
			change := skillVersionChange(t)
			owner := &skillVersionOwner{denied: mode == "owner-denied"}
			store := &skillVersionStore{t: t, tx: f.NewTx(), owner: owner, change: change, mode: mode}
			adapter, err := NewSkillAssignmentReferences(store, owner)
			if err != nil {
				t.Fatal(err)
			}
			locks, err := adapter.SkillAssignmentVersionLocks(change)
			if err != nil {
				t.Fatal(err)
			}
			if len(locks) != 7 || store.reads != 0 || owner.calls != 0 {
				t.Fatal("lock collection performed SQL or omitted original scope")
			}
			store.held = locks
			tx := store.tx
			if mode == "foreign-tx" {
				tx = f.NewTx()
			}
			if mode == "missing-locks" {
				store.held = nil
			}
			ctx := context.WithValue(context.Background(), skillVersionWitness{}, owner)
			if err = adapter.AdvanceSkillAssignmentVersionInTx(ctx, tx, change); err == nil {
				t.Fatal("unproven Tool index advanced")
			}
			want := 0
			if mode == "foreign-scope" {
				want = 1
			}
			if mode == "original-reader-failure" {
				want = 2
			}
			if store.reads != want {
				t.Fatal("private read ordering changed")
			}
		})
	}
}
