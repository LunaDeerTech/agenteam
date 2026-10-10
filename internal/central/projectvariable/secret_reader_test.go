package projectvariable

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type secretReaderStore struct {
	*secretAuthorityStore
	row       secretControlRow
	lastQuery string
}

func (s *secretReaderStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.secretAuthorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *secretReaderStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "secret_project_generations") {
		s.queries++
		return secretControlRow{values: []any{int64(1)}}
	}
	if strings.Contains(q, "FROM agenteam_projectvariable.variables WHERE project_id") {
		s.queries++
		s.lastQuery = q
		return s.row
	}
	return s.secretAuthorityStore.QueryRow(ctx, q, args...)
}
func (s *secretReaderStore) Query(_ context.Context, q string, _ ...any) (*postgres.Rows, error) {
	s.queries++
	s.lastQuery = q
	return nil, errors.New("controlled list query failure")
}

type secretReaderD04 struct {
	sc.ProjectVariableWrites
	authority   *SecretWriteAuthority
	observation sc.ProjectVariableWriteObservation
	calls       int
}

func (d *secretReaderD04) LookupProjectVariableWriteInTx(ctx context.Context, tx f.Tx, request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan) (sc.ProjectVariableWriteObservation, error) {
	d.calls++
	if err := d.authority.CheckInTx(ctx, tx, request, plan, sc.ProjectVariableReceiptRead); err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	return d.observation, nil
}
func secretReaderFixture(t *testing.T) (*SecretService, *secretReaderStore, *secretAuthorityProject, *secretReaderD04, *secretCommandRecord, i.Actor) {
	t.Helper()
	r := secretStoredRecord(t, c.SecretCreateCommand, true, true)
	v := r.Receipt.Fields().Variable.Fields()
	o, _ := r.Observation.Result()
	cid := o.Ref.Details().ID.String()
	version := int64(o.Version)
	s := &secretReaderStore{secretAuthorityStore: &secretAuthorityStore{record: r}, row: secretControlRow{values: []any{v.ID.String(), v.ProjectID.String(), v.Type, v.Name, v.Description, int64(v.Version), v.CreatedAt.Time(), v.UpdatedAt.Time(), nil, &cid, &version}}}
	p := &secretAuthorityProject{}
	authority, err := NewSecretWriteAuthority(s, p)
	if err != nil {
		t.Fatal(err)
	}
	d := &secretReaderD04{authority: authority, observation: r.Observation}
	st := &secretServiceState{store: s, deps: SecretDependencies{Writes: authority, Projects: p, Secrets: d, Cursors: testKeys(t)}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	actor, _ := i.NewHuman(r.User, testID[i.Session](30))
	return &SecretService{data: func() *secretServiceState { return st }}, s, p, d, r, actor
}
func TestSecretOwnerReadersGateBeforeSQLAndKeepTypeBoundary(t *testing.T) {
	service, store, project, _, r, actor := secretReaderFixture(t)
	project.denyRead = true
	if _, err := service.GetSecretVariable(context.Background(), actor, r.Project, r.Target); err == nil {
		t.Fatal("denied read accepted")
	}
	if store.queries != 0 {
		t.Fatal("SQL before gate")
	}
	if _, err := service.ListSecretVariables(context.Background(), actor, r.Project, f.DefaultPageRequest()); err == nil {
		t.Fatal("denied list accepted")
	}
	if store.queries != 0 {
		t.Fatal("list before gate")
	}
	project.denyRead = false
	got, err := service.GetSecretVariable(context.Background(), actor, r.Project, r.Target)
	if err != nil || !sameValue(got, r.Receipt.Fields().Variable) || !strings.Contains(store.lastQuery, "type='secret'") {
		t.Fatal("safe detail/type", err)
	}
	if _, err = service.ListSecretVariables(context.Background(), actor, r.Project, f.DefaultPageRequest()); err == nil || !strings.Contains(store.lastQuery, "type='secret' AND deleted_at IS NULL") || !strings.Contains(store.lastQuery, `ORDER BY name COLLATE "C" ASC,id ASC LIMIT $2`) {
		t.Fatal("list query boundary", err)
	}
	store.row.values[1] = testID[i.Project](95).String()
	if _, err = service.GetSecretVariable(context.Background(), actor, r.Project, r.Target); err == nil {
		t.Fatal("cross project row accepted")
	}
}
func TestSecretOwnerLookupAbsenceReadOnlyAndHistoricalD04Binding(t *testing.T) {
	service, store, project, d04, r, actor := secretReaderFixture(t)
	query, err := c.NewSecretVariableCommandLookupRequest(c.SecretVariableCommandLookupFields{ProjectID: r.Project, Command: r.Command, TargetID: r.Target, IdempotencyKey: r.Key})
	if err != nil {
		t.Fatal(err)
	}
	store.record = nil
	project.denyMutate = true
	got, err := service.LookupSecretVariableCommand(context.Background(), actor, query)
	if err != nil || got.Status() != c.SecretLookupNotObserved || d04.calls != 0 {
		t.Fatal("absence required target/Mutate/D04", err)
	}
	store.record = r
	got, err = service.LookupSecretVariableCommand(context.Background(), actor, query)
	if err != nil || got.Status() != c.SecretLookupCommitted || !sameValue(*got.Receipt(), r.Receipt) || d04.calls != 1 {
		t.Fatal("history not bound", err)
	}
	d04.observation = sc.ProjectVariableWriteNotObserved()
	if _, err = service.LookupSecretVariableCommand(context.Background(), actor, query); err == nil {
		t.Fatal("D10-only receipt accepted")
	}
	project.denyRead = true
	before := store.queries
	if _, err = service.LookupSecretVariableCommand(context.Background(), actor, query); err == nil || store.queries != before {
		t.Fatal("current gate bypassed for history")
	}
	for _, intent := range project.calls {
		if intent != i.Read {
			t.Fatal("historical Lookup took new-write gate")
		}
	}
}
func TestSecretOwnerCursorSeparatesTypeOwnerProjectAndGeneration(t *testing.T) {
	_, _, _, _, r, _ := secretReaderFixture(t)
	keys := testKeys(t)
	binding, err := secretPageBinding(r.Project, r.User.String())
	if err != nil {
		t.Fatal(err)
	}
	token, err := secretPageToken(keys, binding, 3, r.Receipt.Fields().Variable)
	if err != nil {
		t.Fatal(err)
	}
	position, err := pageAfter(keys, token, binding, 3)
	if err != nil || position.id != r.Target {
		t.Fatal("roundtrip", err)
	}
	ordinary, _ := pageBinding(r.Project, r.User.String())
	if _, err = pageAfter(keys, token, ordinary, 3); err == nil {
		t.Fatal("cross type cursor")
	}
	for _, bad := range []struct {
		project c.ProjectID
		owner   string
	}{{r.Project, testID[i.User](91).String()}, {testID[i.Project](92), r.User.String()}} {
		b, _ := secretPageBinding(bad.project, bad.owner)
		if _, err = pageAfter(keys, token, b, 3); err == nil {
			t.Fatal("cross scope cursor")
		}
	}
	if _, err = pageAfter(keys, token, binding, 4); err == nil {
		t.Fatal("stale generation")
	}
}
