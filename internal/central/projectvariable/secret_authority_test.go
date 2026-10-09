package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// Controlled rows/Tx/Project port isolate the real D10 authority's decisions.
// These controls do not authenticate a Session or execute PostgreSQL.
type secretControlRow struct {
	values []any
	err    error
}

func (r secretControlRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("controlled column count")
	}
	for n, value := range r.values {
		out := reflect.ValueOf(dest[n]).Elem()
		if value == nil {
			out.SetZero()
		} else {
			out.Set(reflect.ValueOf(value))
		}
	}
	return nil
}
func secretControlCommand(r *secretCommandRecord) secretControlRow {
	if r == nil {
		return secretControlRow{err: pgx.ErrNoRows}
	}
	o, _ := r.Observation.Result()
	m := r.Receipt.Fields()
	raw, _ := json.Marshal(r.Receipt)
	var expected *int64
	var event, audit *string
	if r.Expected != nil {
		v := int64(*r.Expected)
		expected = &v
	}
	if m.EventID != nil {
		v := m.EventID.String()
		event = &v
	}
	if m.AuditID != nil {
		v := m.AuditID.String()
		audit = &v
	}
	return secretControlRow{values: []any{r.ID.String(), r.Project.String(), r.User.String(), string(r.Command), string(r.Key), r.Target.String(), expected, r.NamePresent, r.DescriptionPresent, r.ValuePresent, o.ReceiptID.String(), o.Ref.Details().ID.String(), int64(o.Version), string(o.Effect), o.Deleted, raw, r.CommittedAt.Time(), event, audit}}
}

type secretAuthorityStore struct {
	Store
	tx                    f.Tx
	locks                 []f.LockRequest
	record                *secretCommandRecord
	queries, transactions int
	used                  bool
	unknown               bool
}

func (s *secretAuthorityStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !tx.Valid() || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *secretAuthorityStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	for _, required := range locks {
		found := false
		for _, held := range s.locks {
			if f.CompareLockKeys(held.Key, required.Key) == 0 && held.Mode == required.Mode {
				found = true
			}
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *secretAuthorityStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = append([]f.LockRequest(nil), locks...)
	return nil
}
func (s *secretAuthorityStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	s.tx = f.NewTx()
	defer func() { s.tx = f.Tx{}; s.locks = nil }()
	if err := fn(ctx, s.tx); err != nil {
		var problem *f.Fault
		if !errors.As(err, &problem) {
			problem = fault(f.InternalError)
		}
		return f.NotCommittedResult(problem)
	}
	if s.unknown {
		return f.UnknownResult(testID[f.TransactionAttempt](99), cause)
	}
	return f.CommittedResult()
}
func (s *secretAuthorityStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	s.queries++
	if strings.Contains(query, "FROM agenteam_projectvariable.secret_commands") {
		return secretControlCommand(s.record)
	}
	if strings.HasPrefix(query, "SELECT EXISTS") {
		return secretControlRow{values: []any{s.used}}
	}
	return secretControlRow{err: errors.New("unexpected controlled SQL")}
}

type secretAuthorityProject struct {
	pc.ProjectAuthority
	calls                []i.AccessIntent
	denyRead, denyMutate bool
	actors               []i.Actor
}

func (p *secretAuthorityProject) RequireOwnerInTx(_ context.Context, _ f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	p.calls = append(p.calls, intent)
	p.actors = append(p.actors, actor)
	if intent == i.Read && p.denyRead || intent == i.Mutate && p.denyMutate {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	at, _ := f.ParseInstant("2026-10-09T10:00:00Z")
	user, _ := f.ParseID[i.User](actor.Details().UserID)
	return pc.NewProjectAccess(actor, pc.ProjectRef{ID: project, OwnerUserID: user, Name: "Project", NormalizedName: "project", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}, at)
}
func secretAuthorityRequest(t *testing.T, r *secretCommandRecord, session int) sc.ProjectVariableWriteRequest {
	t.Helper()
	actor, _ := i.NewHuman(r.User, testID[i.Session](session))
	identity, _ := c.SecretVariableCommandIdentity(r.Project, r.Command, r.Key)
	kind, _ := secretMutationKind(r.Command)
	request, err := sc.NewProjectVariableWriteRequest(sc.ProjectVariableWriteFields{Actor: actor, ProjectID: r.Project, VariableID: r.Target, Identity: identity, Kind: kind, ExpectedVersion: r.Expected})
	if err != nil {
		t.Fatal(err)
	}
	return request
}
func TestSecretOwnerRepositoryRejectsCorruptSafeRecords(t *testing.T) {
	for _, command := range []c.SecretCommandName{c.SecretCreateCommand, c.SecretUpdateCommand, c.SecretDeleteCommand} {
		r := secretStoredRecord(t, command, command != c.SecretDeleteCommand, true)
		got, err := scanSecretCommand(secretControlCommand(r))
		if err != nil || !sameSecretObservation(got.Observation, r.Observation) {
			t.Fatal("roundtrip", err)
		}
		for _, index := range []int{0, 2, 3, 4, 5, 10, 11, 13, 17, 18} {
			row := secretControlCommand(r)
			if index >= 17 {
				bad := "invalid"
				row.values[index] = &bad
			} else {
				row.values[index] = "invalid"
			}
			if index == 4 {
				row.values[index] = "invalid key"
			}
			if _, err = scanSecretCommand(row); err == nil {
				t.Fatalf("corrupt column %d accepted", index)
			}
		}
	}
	if got, err := scanSecretCommand(secretControlCommand(nil)); err != nil || got != nil {
		t.Fatal("absence not distinct")
	}
}
func TestSecretOwnerAuthorityPrivatePlanAndCurrentStages(t *testing.T) {
	r := secretStoredRecord(t, c.SecretCreateCommand, true, true)
	request := secretAuthorityRequest(t, r, 30)
	s := &secretAuthorityStore{}
	p := &secretAuthorityProject{}
	a, err := NewSecretWriteAuthority(s, p)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	basis, _ := plan.Basis()
	if basis.Receipt.Observed() || basis.VariableVersion != 0 || basis.CredentialVersion != 0 || s.queries != 2 || s.transactions != 1 {
		t.Fatal("new create basis")
	}
	before := s.queries
	if err = a.CheckPlan(request, plan); err != nil || s.queries != before {
		t.Fatal("pure plan check")
	}
	other, _ := NewSecretWriteAuthority(s, p)
	code(t, other.CheckPlan(request, plan), f.Forbidden)
	code(t, a.CheckPlan(secretAuthorityRequest(t, r, 31), plan), f.Forbidden)
	foreign, _ := sc.NewProjectVariableWritePlan(sc.NewPlanIssuer(), basis, mustSecretLocks(t, plan))
	code(t, a.CheckPlan(request, foreign), f.Forbidden)
	s.tx = f.NewTx()
	s.locks = mustSecretLocks(t, plan)
	p.calls = nil
	if err = a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableReceiptRead); err != nil {
		t.Fatal(err)
	}
	if len(p.calls) != 1 || p.calls[0] != i.Read {
		t.Fatal("ReceiptRead used write authority")
	}
	p.denyMutate = true
	code(t, a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableNewWrite), f.Forbidden)
	p.denyMutate = false
	s.used = true
	code(t, a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableNewWrite), f.ResourceBusy)
	s.used = false
	s.locks = nil
	before = s.queries
	code(t, a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableReceiptRead), f.Forbidden)
	if s.queries != before {
		t.Fatal("SQL before lock validation")
	}
	s.locks = mustSecretLocks(t, plan)
	s.record = r
	err = a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableReceiptRead)
	var retry secretPreparationChanged
	if !errors.As(err, &retry) {
		t.Fatal("new historical Credential requires outer reprepare", err)
	}
	if s.transactions != 1 {
		t.Fatal("CheckInTx opened nested transaction")
	}
}
func mustSecretLocks(t *testing.T, plan sc.ProjectVariableWritePlan) []f.LockRequest {
	t.Helper()
	locks, err := plan.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	return locks
}
func TestSecretOwnerAuthorityHistoryNeedsCurrentSessionBeforeSQL(t *testing.T) {
	r := secretStoredRecord(t, c.SecretUpdateCommand, true, true)
	request := secretAuthorityRequest(t, r, 30)
	s := &secretAuthorityStore{record: r}
	p := &secretAuthorityProject{denyRead: true}
	a, _ := NewSecretWriteAuthority(s, p)
	_, err := a.Discover(context.Background(), request)
	code(t, err, f.Forbidden)
	if s.queries != 0 {
		t.Fatal("history read before current gate")
	}
	p.denyRead = false
	p.denyMutate = true
	p.calls = nil
	plan, err := a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	basis, _ := plan.Basis()
	if !sameSecretObservation(basis.Receipt, r.Observation) || basis.VariableVersion != 0 || len(p.calls) != 1 || p.calls[0] != i.Read {
		t.Fatal("history consulted new-write gate/canonical")
	}
	s.tx = f.NewTx()
	s.locks = mustSecretLocks(t, plan)
	if err = a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableReceiptRead); err != nil {
		t.Fatal(err)
	}
	code(t, a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableNewWrite), f.Forbidden)
	s.record = nil
	code(t, a.CheckInTx(context.Background(), s.tx, request, plan, sc.ProjectVariableReceiptRead), f.InternalError)
	s.record = r
	s.unknown = true
	_, err = a.Discover(context.Background(), request)
	var failure *f.Fault
	if !errors.As(err, &failure) || failure.CommitState != f.Unknown {
		t.Fatal("discovery unknown lost", err)
	}
	for _, actor := range p.actors {
		if actor.Details().SessionID != request.Fields().Actor.Details().SessionID {
			t.Fatal("Session substituted")
		}
	}
}
