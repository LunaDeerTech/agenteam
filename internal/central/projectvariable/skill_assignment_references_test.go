package projectvariable

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
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls use a deliberately controlled owner callback and SQL rows.
// They prove participant ordering/CAS, not a real Agent write or PG commit.
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
	t             *testing.T
	tx            f.Tx
	owner         *skillVersionOwner
	change        ac.SkillAssignmentVersionChange
	held          []f.LockRequest
	mode          string
	reads, writes int
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
		s.t.Fatal("SQL before current owner witness")
	}
	s.reads++
	if !strings.Contains(query, "secret_references WHERE owner_kind='agent' AND owner_id=$1") || strings.Contains(query, "project_id=$") || len(args) != 1 || args[0] != s.change.Request.AgentID.String() {
		s.t.Fatal("incomplete Secret owner query")
	}
	projects, ids, versions := []string{}, []string{}, []int64{}
	for _, id := range s.change.Before.Fields().AllowedSecretVariableIDs {
		projects = append(projects, s.change.Request.ProjectID.String())
		ids = append(ids, id.String())
		versions = append(versions, int64(s.change.Request.ExpectedVersion))
	}
	if s.mode == "stale" {
		versions[0]++
	}
	if s.mode == "foreign-scope" {
		projects[0] = skillVersionID[i.Project](s.t, 99).String()
	}
	if s.mode == "wrong-refs" {
		ids[0] = skillVersionID[i.ProjectVariable](s.t, 99).String()
	}
	return skillVersionRow{[]any{projects, ids, versions}}
}
func (s *skillVersionStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if s.owner.calls != 1 || ctx.Value(skillVersionWitness{}) != s.owner {
		s.t.Fatal("write before owner witness")
	}
	s.writes++
	if !strings.HasPrefix(query, "UPDATE ") || len(args) != 4 || args[0] != s.change.Request.AgentID.String() || args[1] != s.change.Request.ProjectID.String() || args[2] != int64(s.change.ResultVersion) || args[3] != int64(s.change.Request.ExpectedVersion) {
		s.t.Fatal("reference replacement or changed version binding")
	}
	if !strings.Contains(query, "SET owner_version=$3") || !strings.Contains(query, "AND owner_version=$4") {
		s.t.Fatal("missing version-only compare-and-set")
	}
	n := len(s.change.Before.Fields().AllowedSecretVariableIDs)
	if s.mode == "cas-miss" {
		n = 0
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}
func skillVersionID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func skillVersionChange(t *testing.T, empty bool) ac.SkillAssignmentVersionChange {
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
	effort := "low"
	approval := skillVersionID[mc.Model](t, 6)
	fields := ac.AgentConfigFields{Core: ac.AgentCore{ID: a, ProjectID: p, Name: "skill-agent", NormalizedName: "skill-agent", ModelRef: skillVersionID[mc.Model](t, 5), ReasoningEffort: &effort, ApprovalPolicy: ac.ApprovalAuto, ApprovalModelRef: &approval, Lifecycle: ac.AgentActive, Version: 7, CreatedAt: at, UpdatedAt: at},
		AllowedToolIDs: []i.ToolID{skillVersionID[i.Tool](t, 7)}, AllowedMountIDs: []i.MountID{skillVersionID[i.Mount](t, 8)}, AllowedSecretVariableIDs: []i.ProjectVariableID{skillVersionID[i.ProjectVariable](t, 9)}}
	if empty {
		fields.AllowedToolIDs = []i.ToolID{}
		fields.AllowedMountIDs = []i.MountID{}
		fields.AllowedSecretVariableIDs = []i.ProjectVariableID{}
		fields.Core.ApprovalPolicy = ac.ApprovalDefault
		fields.Core.ApprovalModelRef = nil
	}
	config, err := ac.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	change := ac.SkillAssignmentVersionChange{Request: ac.SkillAssignmentRequest{Actor: actor, ProjectID: p, AgentID: a, Command: command, ExpectedVersion: 7}, Before: config, ResultVersion: 8}
	if err = change.Validate(); err != nil {
		t.Fatal(err)
	}
	return change
}
func TestSkillAssignmentSecretReferencesAdvanceOnlyVersion(t *testing.T) {
	for _, mode := range []string{"success", "empty", "foreign-tx", "missing-locks", "owner-denied", "stale", "foreign-scope", "wrong-refs", "cas-miss"} {
		t.Run(mode, func(t *testing.T) {
			change := skillVersionChange(t, mode == "empty")
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
			if store.reads != 0 || store.writes != 0 || owner.calls != 0 {
				t.Fatal("pure lock collection performed I/O")
			}
			store.held = locks
			ctx := context.WithValue(context.Background(), skillVersionWitness{}, owner)
			tx := store.tx
			if mode == "missing-locks" {
				store.held = nil
			}
			if mode == "foreign-tx" {
				tx = f.NewTx()
			}
			err = adapter.AdvanceSkillAssignmentVersionInTx(ctx, tx, change)
			success := mode == "success" || mode == "empty"
			if success != (err == nil) {
				t.Fatalf("unexpected controlled outcome: %v", err)
			}
			if success && store.writes != 1 {
				t.Fatal("version advance not exactly once")
			}
			if !success && mode != "cas-miss" && store.writes != 0 {
				t.Fatal("rejected preimage reached write")
			}
			if (mode == "owner-denied" || mode == "missing-locks" || mode == "foreign-tx") && store.reads != 0 {
				t.Fatal("rejected witness reached private SQL")
			}
		})
	}
}
