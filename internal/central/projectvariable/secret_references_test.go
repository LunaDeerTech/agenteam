package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// The SQL and owner controls below prove participant ordering and complete
// data handling, not a real Agent canonical witness or PostgreSQL commit.
type secretReferencesFixture struct {
	store      *secretReferencesStore
	project    *secretAuthorityProject
	service    *SecretReferenceService
	change     c.SecretReferenceChange
	issuer     c.SecretPlanIssuer
	ownerError error
	checks     int
}
type secretReferencesWitnessKey struct{}
type secretReferencesStore struct {
	*secretDirectoryStore
	fixture      *secretReferencesFixture
	index        []secretReferenceIndexEntry
	indexErr     error
	writeErr     error
	acquisitions int
	execCalls    int
}

func (s *secretReferencesStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.secretAuthorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *secretReferencesStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquisitions++
	return s.secretAuthorityStore.AcquireAll(ctx, tx, locks)
}
func (s *secretReferencesStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "FROM agenteam_projectvariable.secret_references") {
		if len(args) != 2 || args[0] != s.fixture.change.ProjectID.String() || args[1] != s.fixture.change.AgentID.String() {
			panic("wrong reference owner query")
		}
		if s.indexErr != nil {
			return secretControlRow{err: s.indexErr}
		}
		var ids []string
		var versions []int64
		for _, entry := range s.index {
			ids, versions = append(ids, entry.ID.String()), append(versions, int64(entry.Version))
		}
		return secretControlRow{values: []any{ids, versions}}
	}
	return s.secretDirectoryStore.QueryRow(ctx, query, args...)
}
func (s *secretReferencesStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if s.fixture.checks == 0 || ctx.Value(secretReferencesWitnessKey{}) != s.fixture {
		panic("reference write before original owner callback")
	}
	s.execCalls++
	if s.writeErr != nil {
		return pgconn.CommandTag{}, s.writeErr
	}
	if strings.HasPrefix(query, "DELETE FROM agenteam_projectvariable.secret_references") && len(args) == 2 {
		if args[0] != s.fixture.change.ProjectID.String() || args[1] != s.fixture.change.AgentID.String() {
			panic("wrong reference deletion")
		}
		count := len(s.index)
		s.index = nil
		return pgconn.NewCommandTag("DELETE " + strconv.Itoa(count)), nil
	}
	if strings.HasPrefix(query, "INSERT INTO agenteam_projectvariable.secret_references") && len(args) == 4 {
		if args[0] != s.fixture.change.ProjectID.String() || args[2] != s.fixture.change.AgentID.String() {
			panic("wrong reference insertion")
		}
		id, err := f.ParseID[i.ProjectVariable](args[1].(string))
		if err != nil {
			panic("invalid inserted variable ID")
		}
		s.index = append(s.index, secretReferenceIndexEntry{id, f.Version(args[3].(int64))})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	panic("unexpected non-reference write")
}
func (x *secretReferencesFixture) Discover(_ context.Context, r c.SecretReferenceChange) (c.SecretReferenceOwnerPlan, error) {
	agent, _ := f.AgentLock(r.AgentID.String())
	locks := []f.LockRequest{commandLock(r.Command), userLock(r.Actor.Details().UserID, f.Exclusive), projectLock(r.ProjectID, f.Shared), {Key: agent, Mode: f.Exclusive}}
	return c.NewSecretReferenceOwnerPlan(x.issuer, r, digest([]byte("controlled-original-owner-plan")), locks)
}
func (x *secretReferencesFixture) CheckAppliedInTx(ctx context.Context, tx f.Tx, r c.SecretReferenceChange, plan c.SecretReferenceOwnerPlan) error {
	if x.ownerError != nil {
		return x.ownerError
	}
	binding, _ := c.SecretReferenceBinding(r)
	if tx != x.store.tx || ctx.Value(secretReferencesWitnessKey{}) != x || !plan.Matches(x.issuer, binding, digest([]byte("controlled-original-owner-plan"))) {
		return fault(f.Forbidden)
	}
	x.checks++
	return nil
}
func newSecretReferencesFixture(t *testing.T) *secretReferencesFixture {
	t.Helper()
	_, directoryStore, project, request := secretDirectoryFixture(t)
	x := &secretReferencesFixture{project: project, issuer: c.NewSecretPlanIssuer()}
	x.store = &secretReferencesStore{secretDirectoryStore: directoryStore, fixture: x}
	directory, err := NewSecretDirectory(x.store, project)
	if err != nil {
		t.Fatal(err)
	}
	x.service, err = NewSecretReferences(x.store, directory, x)
	if err != nil {
		t.Fatal(err)
	}
	x.change = c.SecretReferenceChange{Actor: request.Actor, ProjectID: request.ProjectID, AgentID: testID[i.Agent](240), Command: request.Command, Operation: c.SecretReferenceCreate, ResultOwnerVersion: 1, After: request.IDs}
	return x
}
func (x *secretReferencesFixture) update() {
	version := f.Version(3)
	x.change.Operation, x.change.ExpectedOwnerVersion, x.change.ResultOwnerVersion = c.SecretReferenceUpdate, &version, 4
	x.change.Before = slices.Clone(x.change.After)
	x.change.Command, _ = f.NewCommandIdentity("project", []string{x.change.ProjectID.String()}, "agent.update", "reference-update")
	x.store.index = []secretReferenceIndexEntry{{x.change.Before[0], version}}
}
func (x *secretReferencesFixture) discover(t *testing.T) c.SecretReferencePlan {
	t.Helper()
	plan, err := x.service.DiscoverSecretReferences(context.Background(), x.change)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func (x *secretReferencesFixture) final(plan c.SecretReferencePlan) (context.Context, f.Tx) {
	x.store.tx, x.store.locks = f.NewTx(), plan.RequiredLocks()
	return context.WithValue(context.Background(), secretReferencesWitnessKey{}, x), x.store.tx
}
func requireSecretReferencesCode(t *testing.T, err error, want f.Code) {
	t.Helper()
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != want {
		t.Fatalf("reference error %v, want %s", err, want)
	}
}

func TestSecretReferencesCompleteSetAndOwnerVersion(t *testing.T) {
	for _, name := range []string{"create", "owner-version", "remove", "no-op", "empty-create"} {
		t.Run(name, func(t *testing.T) {
			x := newSecretReferencesFixture(t)
			if name != "create" && name != "empty-create" {
				x.update()
			}
			if name == "remove" || name == "empty-create" {
				x.change.After = nil
			}
			if name == "no-op" {
				x.change.ResultOwnerVersion = *x.change.ExpectedOwnerVersion
			}
			plan := x.discover(t)
			ctx, tx := x.final(plan)
			transactions, acquisitions := x.store.transactions, x.store.acquisitions
			if err := x.service.ApplySecretReferencesInTx(ctx, tx, x.change, plan); err != nil {
				t.Fatal(err)
			}
			if x.checks != 1 || len(x.store.index) != len(x.change.After) || x.store.transactions != transactions || x.store.acquisitions != acquisitions {
				t.Fatal("lost complete set, owner check or original caller Tx")
			}
			for n, entry := range x.store.index {
				if entry.ID != x.change.After[n] || entry.Version != x.change.ResultOwnerVersion {
					t.Fatal("retained reference did not advance with owner version")
				}
			}
			if name == "no-op" && x.store.execCalls != 0 {
				t.Fatal("no-op rewrote index")
			}
		})
	}
}

func TestSecretReferencesOriginalCallerAndCanonicalGate(t *testing.T) {
	for _, name := range []string{"foreign-tx", "foreign-issuer", "actor-session", "missing-lock", "missing-witness", "owner-reject", "current-owner", "missing-before", "old-version", "extra-before", "mapping-changed", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			x := newSecretReferencesFixture(t)
			x.update()
			plan := x.discover(t)
			ctx, tx := x.final(plan)
			r, service, want := x.change.Clone(), x.service, f.Forbidden
			switch name {
			case "foreign-tx":
				tx = f.NewTx()
			case "foreign-issuer":
				service, _ = NewSecretReferences(x.store, x.service.state().directory, x)
			case "actor-session":
				r.Actor, _ = i.NewHuman(r.Actor.Details().UserID, testID[i.Session](239))
			case "missing-lock":
				x.store.locks = nil
			case "missing-witness":
				ctx = context.Background()
			case "owner-reject":
				x.ownerError = fault(f.Forbidden)
			case "current-owner":
				x.project.denyMutate = true
			case "missing-before":
				x.store.index, want = nil, f.ResourceBusy
			case "old-version":
				x.store.index[0].Version++
				want = f.ResourceBusy
			case "extra-before":
				x.store.index = append(x.store.index, secretReferenceIndexEntry{testID[i.ProjectVariable](235), 3})
				want = f.ResourceBusy
			case "mapping-changed":
				row := x.store.rows[r.After[0].String()]
				row.values[4] = "updated metadata"
				want = f.ResourceBusy
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = f.DependencyUnavailable
			}
			transactions, acquisitions := x.store.transactions, x.store.acquisitions
			err := service.ApplySecretReferencesInTx(ctx, tx, r, plan)
			requireSecretReferencesCode(t, err, want)
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation cause lost")
			}
			if x.store.execCalls != 0 || x.store.transactions != transactions || x.store.acquisitions != acquisitions {
				t.Fatal("refusal wrote index, began a Tx or acquired late locks")
			}
		})
	}
}

func TestSecretReferencesUnboundEmptyAndWholeBefore(t *testing.T) {
	x := newSecretReferencesFixture(t)
	var absent *secretReferencesFixture
	for _, owner := range []c.SecretReferenceOwnerAuthority{nil, absent} {
		_, err := NewSecretReferences(x.store, x.service.state().directory, owner)
		requireSecretReferencesCode(t, err, f.DependencyUnbound)
	}
	_, err := NewSecretReferences(&secretAuthorityStore{}, x.service.state().directory, x)
	requireSecretReferencesCode(t, err, f.DependencyUnbound)
	var zero *SecretReferenceService
	_, err = zero.DiscoverSecretReferences(context.Background(), x.change)
	requireSecretReferencesCode(t, err, f.DependencyUnbound)
	x.change.After = nil
	plan := x.discover(t)
	ctx, tx := x.final(plan)
	x.ownerError = fault(f.Forbidden)
	requireSecretReferencesCode(t, x.service.ApplySecretReferencesInTx(ctx, tx, x.change, plan), f.Forbidden)
	if x.store.execCalls != 0 {
		t.Fatal("empty set bypassed owner")
	}
	x.ownerError = nil
	x.store.index = []secretReferenceIndexEntry{{testID[i.ProjectVariable](235), 1}}
	_, err = x.service.DiscoverSecretReferences(context.Background(), x.change)
	requireSecretReferencesCode(t, err, f.ResourceBusy)
}

func TestSecretReferencesUnavailableTargetAndSQLFailure(t *testing.T) {
	x := newSecretReferencesFixture(t)
	row := x.store.rows[x.change.After[0].String()]
	delete(x.store.rows, x.change.After[0].String())
	_, err := x.service.DiscoverSecretReferences(context.Background(), x.change)
	requireSecretReferencesCode(t, err, f.NotFound)
	x.store.rows[x.change.After[0].String()] = row
	plan := x.discover(t)
	ctx, tx := x.final(plan)
	x.store.writeErr = errors.New("task-only-SQL-canary")
	err = x.service.ApplySecretReferencesInTx(ctx, tx, x.change, plan)
	requireSecretReferencesCode(t, err, f.DependencyUnavailable)
	if len(x.store.index) != 0 || strings.Contains(fmt.Sprintf("%+v", err), "task-only-SQL-canary") {
		t.Fatal("SQL failure accepted or diagnostic leaked")
	}
	for _, value := range []any{x.service, plan, map[string]any{"nested": []any{x.service, plan}}} {
		encoded, err := json.Marshal(value)
		if err != nil || strings.Contains(string(encoded), x.change.After[0].String()) {
			t.Fatal("private reference plan escaped default JSON")
		}
	}
}
