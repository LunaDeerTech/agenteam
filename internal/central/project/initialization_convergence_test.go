package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type convergenceStore struct {
	Store
	t           *testing.T
	ctx         context.Context
	tx          foundation.Tx
	project     c.ProjectID
	creation    c.CreationID
	creationRow []any
	projectRow  []any
	order       []string
	unexpected  int
	lockErr     error
	executorErr error
	creationErr error
	projectErr  error
}

func (s *convergenceStore) RequireHeldLocks(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.order = append(s.order, "held")
	if ctx != s.ctx || tx != s.tx || len(locks) != 1 || locks[0].Key.Validate() != nil || foundation.CompareLockKeys(locks[0].Key, projectLock(s.project, foundation.Exclusive).Key) != 0 || locks[0].Mode != foundation.Exclusive {
		s.t.Fatal("convergence changed context, transaction or required Project EX")
	}
	if s.lockErr != nil {
		return s.lockErr
	}
	return ctx.Err()
}
func (s *convergenceStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	s.order = append(s.order, "executor")
	if tx != s.tx {
		s.t.Fatal("convergence borrowed another transaction")
	}
	if s.executorErr != nil {
		return nil, s.executorErr
	}
	return s, nil
}
func (s *convergenceStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if ctx != s.ctx || len(args) != 1 {
		s.t.Fatal("convergence changed query context or scope")
	}
	switch query {
	case "SELECT " + creationColumns + " FROM agenteam_project.creations WHERE id=$1":
		s.order = append(s.order, "creation")
		if args[0] != s.creation.String() {
			s.t.Fatal("convergence followed a different Creation")
		}
		if s.creationErr != nil {
			return rowFunc(func(...any) error { return s.creationErr })
		}
		return valuesRow(s.creationRow...)
	case "SELECT " + projectColumns + " FROM agenteam_project.projects WHERE id=$1":
		s.order = append(s.order, "project")
		if args[0] != s.project.String() {
			s.t.Fatal("convergence followed a different Project")
		}
		if s.projectErr != nil {
			return rowFunc(func(...any) error { return s.projectErr })
		}
		return valuesRow(s.projectRow...)
	default:
		s.unexpected++
		return rowFunc(func(...any) error { return errors.New("unexpected convergence query") })
	}
}
func (s *convergenceStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.unexpected++
	return pgconn.CommandTag{}, errors.New("convergence attempted a write")
}
func (s *convergenceStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.unexpected++
	return nil, errors.New("convergence attempted a bulk read")
}
func (s *convergenceStore) AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error {
	s.unexpected++
	return errors.New("convergence attempted to acquire locks")
}
func (s *convergenceStore) WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.unexpected++
	return foundation.CommitResult{}
}

type convergenceFixture struct {
	t         *testing.T
	store     *convergenceStore
	authority *Authority
	request   c.InitializationRequest
	actor     identity.Actor
	initial   c.ProjectRef
}

func convergenceServiceActor(t *testing.T, name identity.ServiceName, scope identity.Scope, cause string) identity.Actor {
	t.Helper()
	registered, err := identity.RegisterService(name)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registered.Actor(cause, scope)
	if err != nil {
		t.Fatal(err)
	}
	return actor
}
func newConvergenceFixture(t *testing.T) *convergenceFixture {
	t.Helper()
	project, creation, owner := testID[identity.Project](t), testID[c.Creation](t), testID[identity.User](t)
	now := time.Date(2026, 10, 8, 1, 2, 3, 0, time.UTC)
	name, description := "Original", "original body"
	st := &convergenceStore{t: t, ctx: context.Background(), tx: foundation.NewTx(), project: project, creation: creation}
	st.projectRow = []any{project.String(), owner.String(), name, "original", description, "active", int64(1), nil, now, now, nil, creation.String(), false, nil}
	st.creationRow = []any{creation.String(), project.String(), owner.String(), "create-command", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", &name, &description, "initializing", "original-initialization", nil, nil, nil, int64(2), now, now, testID[struct{}](t).String(), nil, nil, nil}
	a, err := NewAuthority(st, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, foundation.Tx, identity.Actor) error {
		st.unexpected++
		return errors.New("convergence borrowed a Human Session gate")
	})})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := identity.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	f := &convergenceFixture{t: t, store: st, authority: a, request: c.InitializationRequest{CreationID: creation, ProjectID: project, InitializationKey: "original-initialization"}}
	f.actor = convergenceServiceActor(t, identity.ProjectInitialization, scope, creation.String())
	f.initial = c.ProjectRef{ID: project, OwnerUserID: owner, Name: name, NormalizedName: "original", Description: description, Lifecycle: c.Active, Version: 1, CreatedAt: instant(now), UpdatedAt: instant(now)}
	return f
}
func (f *convergenceFixture) state(state c.CreationState) {
	f.t.Helper()
	f.store.creationRow[7] = string(state)
	if state == c.CreationFailed {
		reason := string(c.ReasonOperationFailed)
		f.store.creationRow[11] = &reason
	}
	if state != c.CreationCompleted {
		return
	}
	skill, revision := testID[c.Skill](f.t).String(), int64(1)
	f.store.projectRow[12] = true
	f.store.creationRow[5], f.store.creationRow[6] = nil, nil
	f.store.creationRow[9], f.store.creationRow[10] = &skill, &revision
	f.store.creationRow[16], f.store.creationRow[17] = []byte(`{}`), []byte(`{}`)
	f.result(f.initial)
}
func (f *convergenceFixture) result(ref c.ProjectRef) {
	f.t.Helper()
	raw, err := json.Marshal(ref)
	if err != nil {
		f.t.Fatal(err)
	}
	f.store.creationRow[18] = raw
}
func (f *convergenceFixture) snapshot() string {
	f.t.Helper()
	raw, err := json.Marshal([][]any{f.store.creationRow, f.store.projectRow})
	if err != nil {
		f.t.Fatal(err)
	}
	return string(raw)
}
func (f *convergenceFixture) check(want foundation.Code, order ...string) {
	f.t.Helper()
	before := f.snapshot()
	err := f.authority.ValidateInitializationConvergenceInTx(f.store.ctx, f.store.tx, f.actor, f.request)
	if want == "" {
		if err != nil {
			f.t.Fatal("valid convergence context rejected", err)
		}
	} else {
		hasCode(f.t, err, want)
	}
	if !reflect.DeepEqual(f.store.order, order) {
		f.t.Fatalf("order=%v want=%v", f.store.order, order)
	}
	if f.store.unexpected != 0 || f.snapshot() != before {
		f.t.Fatal("convergence escaped read-only transaction scope")
	}
}

func TestInitializationConvergenceAuthorityContract(t *testing.T) {
	for _, kind := range []string{"nil", "zero"} {
		t.Run("unbound_"+kind, func(t *testing.T) {
			f := newConvergenceFixture(t)
			f.authority = nil
			if kind == "zero" {
				f.authority = &Authority{}
			}
			f.check(foundation.DependencyUnbound)
		})
	}
	for _, kind := range []string{"tx", "actor", "creation", "project", "key"} {
		t.Run("invalid_"+kind, func(t *testing.T) {
			f := newConvergenceFixture(t)
			switch kind {
			case "tx":
				f.store.tx = foundation.Tx{}
			case "actor":
				f.actor = identity.Actor{}
			case "creation":
				f.request.CreationID = c.CreationID{}
			case "project":
				f.request.ProjectID = c.ProjectID{}
			case "key":
				f.request.InitializationKey = "has space"
			}
			f.check(foundation.InvalidArgument)
		})
	}
	for _, kind := range []string{"human", "agent", "service", "system", "project", "creation", "digest"} {
		t.Run("forbidden_actor_"+kind, func(t *testing.T) {
			f := newConvergenceFixture(t)
			scope, _ := identity.InProject(f.request.ProjectID)
			name, cause := identity.ProjectInitialization, f.request.CreationID.String()
			switch kind {
			case "human":
				f.actor = testActor(t)
			case "agent":
				var err error
				f.actor, err = identity.NewAgentRun(f.request.ProjectID, testID[identity.Agent](t), testID[identity.Execution](t))
				if err != nil {
					t.Fatal(err)
				}
			case "service":
				name = identity.ProjectLifecycle
			case "system":
				scope = identity.SystemScope()
			case "project":
				scope, _ = identity.InProject(testID[identity.Project](t))
			case "creation":
				cause = testID[c.Creation](t).String()
			case "digest":
				cause = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			}
			if kind != "human" && kind != "agent" {
				f.actor = convergenceServiceActor(t, name, scope, cause)
			}
			f.check(foundation.Forbidden)
		})
	}
	for _, phase := range []string{"held", "executor", "creation", "project"} {
		t.Run("dependency_"+phase, func(t *testing.T) {
			f := newConvergenceFixture(t)
			original := errors.New("controlled dependency failure")
			order := []string{"held"}
			switch phase {
			case "held":
				f.store.lockErr = original
			case "executor":
				f.store.executorErr = original
				order = append(order, "executor")
			case "creation":
				f.store.creationErr = original
				order = append(order, "executor", "creation")
			case "project":
				f.store.projectErr = original
				order = append(order, "executor", "creation", "project")
			}
			before := f.snapshot()
			err := f.authority.ValidateInitializationConvergenceInTx(f.store.ctx, f.store.tx, f.actor, f.request)
			hasCode(t, err, foundation.DependencyUnavailable)
			if !errors.Is(err, original) || !reflect.DeepEqual(f.store.order, order) || f.store.unexpected != 0 || f.snapshot() != before {
				t.Fatal("dependency failure lost cause/order or changed state")
			}
		})
	}
	t.Run("cancelled_context", func(t *testing.T) {
		f := newConvergenceFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f.store.ctx = ctx
		f.check(foundation.DependencyUnavailable, "held")
	})
	for _, kind := range []string{"creation_missing", "project_missing", "key", "command_key", "forward_project"} {
		t.Run("request_mapping_"+kind, func(t *testing.T) {
			f := newConvergenceFixture(t)
			order := []string{"held", "executor", "creation"}
			switch kind {
			case "creation_missing":
				f.store.creationErr = pgx.ErrNoRows
			case "project_missing":
				f.store.projectErr = pgx.ErrNoRows
				order = append(order, "project")
			case "key":
				f.request.InitializationKey = "different-original-key"
			case "command_key":
				f.request.InitializationKey = "create-command"
			case "forward_project":
				f.store.creationRow[1] = testID[identity.Project](t).String()
			}
			f.check(foundation.Forbidden, order...)
		})
	}
}

func TestInitializationConvergenceAuthorityFacts(t *testing.T) {
	for _, state := range []c.CreationState{c.CreationAccepted, c.CreationInitializing, c.CreationFailed, c.CreationCompleted} {
		t.Run("state_"+string(state), func(t *testing.T) {
			f := newConvergenceFixture(t)
			f.state(state)
			f.check("", "held", "executor", "creation", "project")
			f.store.order = nil
			f.check("", "held", "executor", "creation", "project")
			f.store.order = nil
			before := f.snapshot()
			err := f.authority.ValidateInitializationInTx(f.store.ctx, f.store.tx, f.actor, f.request.CreationID, f.request.ProjectID, f.request.InitializationKey)
			if state == c.CreationAccepted || state == c.CreationFailed {
				hasCode(t, err, foundation.InvalidState)
			} else if err != nil {
				t.Fatal("old successful initialization gate changed", err)
			}
			if f.snapshot() != before || f.store.unexpected != 0 {
				t.Fatal("old gate changed facts")
			}
		})
	}
	t.Run("completed_history_survives_current_update", func(t *testing.T) {
		f := newConvergenceFixture(t)
		f.state(c.CreationCompleted)
		f.store.projectRow[2], f.store.projectRow[3], f.store.projectRow[4], f.store.projectRow[6] = "Renamed", "renamed", "new body", int64(8)
		f.check("", "held", "executor", "creation", "project")
	})
	for _, lifecycle := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		t.Run("lifecycle_"+string(lifecycle), func(t *testing.T) {
			f := newConvergenceFixture(t)
			f.state(c.CreationCompleted)
			f.store.projectRow[5] = string(lifecycle)
			if lifecycle == c.Archived {
				now := f.initial.UpdatedAt.Time()
				f.store.projectRow[10] = &now
			}
			f.check(foundation.Forbidden, "held", "executor", "creation", "project")
		})
	}
	cases := []struct {
		name   string
		state  c.CreationState
		mutate func(*convergenceFixture)
	}{
		{"owner", c.CreationInitializing, func(f *convergenceFixture) { f.store.creationRow[2] = testID[identity.User](f.t).String() }},
		{"reverse_creation", c.CreationInitializing, func(f *convergenceFixture) { f.store.projectRow[11] = testID[c.Creation](f.t).String() }},
		{"project_identity", c.CreationInitializing, func(f *convergenceFixture) { f.store.projectRow[0] = testID[identity.Project](f.t).String() }},
		{"initialized_early", c.CreationInitializing, func(f *convergenceFixture) { f.store.projectRow[12] = true }},
		{"incomplete_version", c.CreationInitializing, func(f *convergenceFixture) { f.store.projectRow[6] = int64(2) }},
		{"incomplete_sprint", c.CreationInitializing, func(f *convergenceFixture) { v := testID[c.Sprint](f.t).String(); f.store.projectRow[7] = &v }},
		{"incomplete_operation", c.CreationInitializing, func(f *convergenceFixture) { v := testID[c.Operation](f.t).String(); f.store.projectRow[13] = &v }},
		{"single_skill", c.CreationInitializing, func(f *convergenceFixture) { v := testID[c.Skill](f.t).String(); f.store.creationRow[9] = &v }},
		{"single_revision", c.CreationInitializing, func(f *convergenceFixture) { v := int64(1); f.store.creationRow[10] = &v }},
		{"name_mismatch", c.CreationInitializing, func(f *convergenceFixture) { v := "Other"; f.store.creationRow[5] = &v }},
		{"name_invalid", c.CreationInitializing, func(f *convergenceFixture) { v := "bad name"; f.store.creationRow[5] = &v }},
		{"description_mismatch", c.CreationInitializing, func(f *convergenceFixture) { v := "different body"; f.store.creationRow[6] = &v }},
		{"description_invalid", c.CreationInitializing, func(f *convergenceFixture) { v := "\x00"; f.store.creationRow[6] = &v }},
		{"request_missing", c.CreationInitializing, func(f *convergenceFixture) { f.store.creationRow[5] = nil }},
		{"accepted_reason", c.CreationAccepted, func(f *convergenceFixture) { v := string(c.ReasonWorkPending); f.store.creationRow[11] = &v }},
		{"failed_missing_reason", c.CreationFailed, func(f *convergenceFixture) { f.store.creationRow[11] = nil }},
		{"completed_uninitialized", c.CreationCompleted, func(f *convergenceFixture) { f.store.projectRow[12] = false }},
		{"completed_no_skill", c.CreationCompleted, func(f *convergenceFixture) { f.store.creationRow[9] = nil }},
		{"completed_no_revision", c.CreationCompleted, func(f *convergenceFixture) { f.store.creationRow[10] = nil }},
		{"completed_request_remains", c.CreationCompleted, func(f *convergenceFixture) { v := "Original"; f.store.creationRow[5] = &v }},
		{"completed_result_missing", c.CreationCompleted, func(f *convergenceFixture) { f.store.creationRow[18] = nil }},
		{"completed_result_malformed", c.CreationCompleted, func(f *convergenceFixture) { f.store.creationRow[18] = []byte(`{}`) }},
		{"completed_result_owner", c.CreationCompleted, func(f *convergenceFixture) { v := f.initial; v.OwnerUserID = testID[identity.User](f.t); f.result(v) }},
		{"completed_result_project", c.CreationCompleted, func(f *convergenceFixture) { v := f.initial; v.ID = testID[identity.Project](f.t); f.result(v) }},
		{"completed_result_version", c.CreationCompleted, func(f *convergenceFixture) { v := f.initial; v.Version = 2; f.result(v) }},
		{"completed_result_sprint", c.CreationCompleted, func(f *convergenceFixture) {
			v := f.initial
			id := testID[c.Sprint](f.t)
			v.CurrentSprintID = &id
			f.result(v)
		}},
		{"completed_result_lifecycle", c.CreationCompleted, func(f *convergenceFixture) {
			v := f.initial
			v.Lifecycle = c.Archived
			at := v.UpdatedAt
			v.ArchivedAt = &at
			f.result(v)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConvergenceFixture(t)
			f.state(tc.state)
			tc.mutate(f)
			order := []string{"held", "executor", "creation", "project"}
			switch tc.name {
			case "request_missing", "failed_missing_reason", "completed_no_skill", "completed_no_revision", "completed_request_remains", "completed_result_missing", "completed_result_malformed":
				order = order[:3] // The original strict Creation scanner rejects first.
			}
			f.check(foundation.DependencyUnavailable, order...)
		})
	}
	t.Run("creation_identity_corrupt", func(t *testing.T) {
		f := newConvergenceFixture(t)
		f.store.creationRow[0] = testID[c.Creation](t).String()
		f.check(foundation.DependencyUnavailable, "held", "executor", "creation")
	})
	for _, slot := range []int{0, 4, 5, 6} {
		t.Run("scanner_project_field_"+string(rune('A'+slot)), func(t *testing.T) {
			f := newConvergenceFixture(t)
			switch slot {
			case 4:
				f.store.projectRow[slot] = "\x00"
			case 6:
				f.store.projectRow[slot] = int64(0)
			default:
				f.store.projectRow[slot] = "invalid"
			}
			f.check(foundation.DependencyUnavailable, "held", "executor", "creation", "project")
		})
	}
	for _, slot := range []int{0, 4, 7, 8, 12, 15} {
		t.Run("scanner_creation_field_"+string(rune('A'+slot)), func(t *testing.T) {
			f := newConvergenceFixture(t)
			if slot == 12 {
				f.store.creationRow[slot] = int64(0)
			} else if slot == 8 {
				f.store.creationRow[slot] = "has space"
			} else {
				f.store.creationRow[slot] = "invalid"
			}
			f.check(foundation.DependencyUnavailable, "held", "executor", "creation")
		})
	}
}
