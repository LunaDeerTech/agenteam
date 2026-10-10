package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/jackc/pgx/v5"
)

// These SQL/Project doubles exercise the actual Directory's boundaries. They
// do not prove PostgreSQL locking, real Session authentication or Agent facts.
type secretDirectoryStore struct {
	*secretAuthorityStore
	rows map[string]secretControlRow
	fail error
	read func()
}

func (s *secretDirectoryStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.secretAuthorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *secretDirectoryStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.queries++
	if s.read != nil {
		s.read()
	}
	if s.fail != nil {
		return secretControlRow{err: s.fail}
	}
	if query != `SELECT `+secretVariableColumns+` FROM agenteam_projectvariable.variables WHERE project_id=$1 AND id=$2 AND type='secret'` || len(args) != 2 {
		return secretControlRow{err: errors.New("unexpected directory query")}
	}
	row, exists := s.rows[args[1].(string)]
	if !exists || row.values[1] != args[0] || row.values[2] != c.SecretVariableType {
		return secretControlRow{err: pgx.ErrNoRows}
	}
	return row
}
func secretDirectoryFixture(t *testing.T) (*SecretDirectory, *secretDirectoryStore, *secretAuthorityProject, c.SecretDirectoryRequest) {
	t.Helper()
	_, old, project, _, record, actor := secretReaderFixture(t)
	store := &secretDirectoryStore{secretAuthorityStore: &secretAuthorityStore{}, rows: map[string]secretControlRow{record.Target.String(): old.row}}
	directory, err := NewSecretDirectory(store, project)
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("project", []string{record.Project.String()}, "agent.create", "directory-command")
	if err != nil {
		t.Fatal(err)
	}
	return directory, store, project, c.SecretDirectoryRequest{Actor: actor, ProjectID: record.Project, Command: command, IDs: []c.VariableID{record.Target}}
}
func secretDirectoryFinal(t *testing.T, d *SecretDirectory, store *secretDirectoryStore, r c.SecretDirectoryRequest, plan c.SecretDirectoryPlan) (c.SecretDirectoryFacts, error) {
	t.Helper()
	store.tx, store.locks = f.NewTx(), plan.RequiredLocks()
	defer func() { store.tx, store.locks = f.Tx{}, nil }()
	return d.RequireSecretVariablesInTx(context.Background(), store.tx, r, plan)
}

func TestSecretDirectoryCompleteStatusesAndSafeMetadata(t *testing.T) {
	d, store, _, r := secretDirectoryFixture(t)
	live := store.rows[r.IDs[0].String()]
	missing, foreign, ordinary, removed := testID[i.ProjectVariable](201), testID[i.ProjectVariable](202), testID[i.ProjectVariable](203), testID[i.ProjectVariable](204)
	for _, item := range []struct {
		id     c.VariableID
		change func([]any)
	}{
		{foreign, func(v []any) { v[1] = testID[i.Project](205).String() }},
		{ordinary, func(v []any) { v[2] = "string" }},
		{removed, func(v []any) {
			v[4], v[5], v[8], v[9], v[10] = "", int64(2), ptrDirectoryTime(v[7].(time.Time)), nil, nil
		}},
	} {
		v := slices.Clone(live.values)
		v[0] = item.id.String()
		item.change(v)
		store.rows[item.id.String()] = secretControlRow{values: v}
	}
	r.IDs = append(r.IDs, missing, foreign, ordinary, removed)
	slices.SortFunc(r.IDs, func(a, b c.VariableID) int { return strings.Compare(a.String(), b.String()) })
	plan, err := d.DiscoverSecretVariables(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.RequiredLocks()) != 4 {
		t.Fatal("base plus live credential lock")
	}
	facts, err := secretDirectoryFinal(t, d, store, r, plan)
	if err != nil {
		t.Fatal(err)
	}
	entries := facts.Entries()
	if len(entries) != len(r.IDs) {
		t.Fatal("incomplete directory")
	}
	for n, e := range entries {
		if e.ID != r.IDs[n] {
			t.Fatal("order changed")
		}
		switch e.ID {
		case removed:
			if e.Status != c.SecretDirectoryRemoved || e.Variable != nil {
				t.Fatal("tombstone disclosure")
			}
		case missing, foreign, ordinary:
			if e.Status != c.SecretDirectoryNotInScope || e.Variable != nil {
				t.Fatal("scope/type oracle")
			}
		default:
			if e.Status != c.SecretDirectoryValid || e.Variable == nil {
				t.Fatal("live metadata missing")
			}
		}
	}
	credential := *live.values[9].(*string)
	for _, value := range []any{facts, plan, d, map[string]any{"nested": []any{facts, plan, d}}, entries} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), credential) || strings.Contains(string(raw), "credential_id") || strings.Contains(fmt.Sprintf("%#v", value), credential) {
			t.Fatal("credential mapping escaped")
		}
	}
	entries[0].Variable = nil
	if facts.Entries()[0].Variable == nil {
		t.Fatal("facts alias")
	}
	if store.transactions != 1 {
		t.Fatal("InTx opened another transaction")
	}
}
func ptrDirectoryTime(v time.Time) *time.Time { return &v }

func TestSecretDirectoryCurrentGatePlanAndHeldLocks(t *testing.T) {
	d, store, project, r := secretDirectoryFixture(t)
	project.denyRead = true
	if plan, err := d.DiscoverSecretVariables(context.Background(), r); err == nil || plan.Validate() == nil || store.queries != 0 {
		t.Fatal("discovery bypassed current Owner")
	}
	project.denyRead = false
	plan, err := d.DiscoverSecretVariables(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	before := store.queries
	project.denyMutate = true
	if facts, err := secretDirectoryFinal(t, d, store, r, plan); err == nil || facts.Validate() == nil || store.queries != before {
		t.Fatal("final bypassed current Mutate gate")
	}
	project.denyMutate = false
	changed := r.Clone()
	changed.Actor, _ = i.NewHuman(testID[i.User](3), testID[i.Session](209))
	if facts, err := secretDirectoryFinal(t, d, store, changed, plan); err == nil || facts.Validate() == nil || store.queries != before {
		t.Fatal("plan crossed actor/session")
	}
	other, _ := NewSecretDirectory(store, project)
	if _, err := secretDirectoryFinal(t, other, store, r, plan); err == nil || store.queries != before {
		t.Fatal("plan crossed issuer")
	}
	store.tx = f.NewTx()
	store.locks = nil
	if _, err := d.RequireSecretVariablesInTx(context.Background(), store.tx, r, plan); err == nil || store.queries != before {
		t.Fatal("missing held union accepted")
	}
	store.locks = plan.RequiredLocks()
	if _, err := d.RequireSecretVariablesInTx(context.Background(), f.NewTx(), r, plan); err == nil || store.queries != before {
		t.Fatal("foreign/inactive transaction accepted")
	}
	store.tx, store.locks = f.Tx{}, nil
	row := store.rows[r.IDs[0].String()]
	row.values[3] = "changed-name"
	if facts, err := secretDirectoryFinal(t, d, store, r, plan); err == nil || facts.Validate() == nil {
		t.Fatal("stale metadata plan accepted")
	}
}

func TestSecretDirectoryEmptyStillAuthorizedAndInputBounded(t *testing.T) {
	d, store, project, r := secretDirectoryFixture(t)
	r.IDs = nil
	plan, err := d.DiscoverSecretVariables(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := secretDirectoryFinal(t, d, store, r, plan)
	if err != nil || facts.Validate() != nil || len(facts.Entries()) != 0 || len(project.calls) != 2 || store.queries != 0 {
		t.Fatal("empty request did not retain authorization", err)
	}
	project.denyMutate = true
	if out, err := secretDirectoryFinal(t, d, store, r, plan); err == nil || out.Validate() == nil {
		t.Fatal("empty gate bypass")
	}
	transactions := store.transactions
	r.IDs = make([]c.VariableID, c.MaxSecretReferences+1)
	if _, err := d.DiscoverSecretVariables(context.Background(), r); err == nil || store.transactions != transactions {
		t.Fatal("limit not enforced before SQL")
	}
	if _, err := NewSecretDirectory(nil, project); err == nil {
		t.Fatal("nil store")
	}
	if _, err := NewSecretDirectory(store, (*secretAuthorityProject)(nil)); err == nil {
		t.Fatal("typed nil authority")
	}
	if _, err := (*SecretDirectory)(nil).DiscoverSecretVariables(context.Background(), c.SecretDirectoryRequest{}); err == nil {
		t.Fatal("zero receiver")
	}
}

func TestSecretDirectoryFailuresReturnZeroAndKeepUnknown(t *testing.T) {
	d, store, _, r := secretDirectoryFixture(t)
	canary := "directory-private-sql-canary"
	store.fail = errors.New(canary)
	plan, err := d.DiscoverSecretVariables(context.Background(), r)
	if err == nil || plan.Validate() == nil || strings.Contains(fmt.Sprintf("%+v", err), canary) {
		t.Fatal("query failure escaped or became facts")
	}
	store.fail = nil
	store.unknown = true
	plan, err = d.DiscoverSecretVariables(context.Background(), r)
	var problem *f.Fault
	if err == nil || plan.Validate() == nil || !errors.As(err, &problem) || problem.Code != f.CommitUnknown || problem.CauseID == "" {
		t.Fatal("Unknown became a usable plan")
	}
	store.unknown = false
	ctx, cancel := context.WithCancel(context.Background())
	store.read = cancel
	plan, err = d.DiscoverSecretVariables(ctx, r)
	if !errors.Is(err, context.Canceled) || plan.Validate() == nil {
		t.Fatal("cancelled call published plan", err)
	}
	store.read = nil
	plan, err = d.DiscoverSecretVariables(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	store.tx, store.locks, store.read = f.NewTx(), plan.RequiredLocks(), cancel
	defer func() { store.tx, store.locks, store.read = f.Tx{}, nil, nil }()
	facts, err := d.RequireSecretVariablesInTx(ctx, store.tx, r, plan)
	if !errors.Is(err, context.Canceled) || facts.Validate() == nil {
		t.Fatal("cancelled call published facts", err)
	}
}
