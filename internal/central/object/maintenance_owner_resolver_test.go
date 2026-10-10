package object

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type maintenanceOwnerSQL struct {
	postgres.SQLExecutor
	kind           oc.OwnerKind
	owner, project string
	err            error
	calls          int
	ctx            context.Context
}

func (s *maintenanceOwnerSQL) QueryRow(ctx context.Context, _ string, args ...any) postgres.Row {
	s.calls++
	s.ctx = ctx
	if len(args) != 1 {
		panic("maintenance owner lookup must bind exactly one object")
	}
	return maintenanceOwnerRow{s}
}

type maintenanceOwnerRow struct{ sql *maintenanceOwnerSQL }

func (r maintenanceOwnerRow) Scan(dest ...any) error {
	if r.sql.err != nil {
		return r.sql.err
	}
	*dest[0].(*oc.OwnerKind) = r.sql.kind
	*dest[1].(*string) = r.sql.owner
	*dest[2].(*string) = r.sql.project
	return nil
}

type maintenanceOwnerStore struct {
	Store
	sql       *maintenanceOwnerSQL
	inside    *maintenanceOwnerSQL
	tx        f.Tx
	err       error
	inTxCalls int
}

func (s *maintenanceOwnerStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	return s.sql.QueryRow(ctx, query, args...)
}
func (s *maintenanceOwnerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.inTxCalls++
	if s.err != nil {
		return nil, s.err
	}
	if tx != s.tx {
		return nil, f.NewFault(f.Forbidden, f.NotCommitted)
	}
	return s.inside, nil
}

func maintenanceRequestForTest(t *testing.T) oc.AccessRequest {
	t.Helper()
	object, err := f.NewID[oc.StoredObject]()
	if err != nil {
		t.Fatal(err)
	}
	process, err := f.NewID[oc.Process]()
	if err != nil {
		t.Fatal(err)
	}
	r, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.InspectAccess, ObjectID: object, InstanceID: process})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMaintenanceOwnerResolverCurrentTransaction(t *testing.T) {
	request := maintenanceRequestForTest(t)
	owner := request.Details().ObjectID.String()
	project := request.Details().InstanceID.String()
	for _, kind := range []oc.OwnerKind{oc.Avatar, oc.Knowledge, oc.SkillRevision} {
		t.Run(string(kind), func(t *testing.T) {
			projectID := project
			if kind == oc.Avatar {
				projectID = ""
			}
			outside := &maintenanceOwnerSQL{kind: kind, owner: owner, project: projectID}
			inside := &maintenanceOwnerSQL{kind: kind, owner: owner, project: projectID}
			store := &maintenanceOwnerStore{sql: outside, inside: inside, tx: f.NewTx()}
			resolver, err := NewMaintenanceOwnerResolver(store)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), struct{}{}, "original")
			found, err := resolver.Discover(ctx, request)
			if err != nil || found.Details().Kind != kind || outside.calls != 1 || inside.calls != 0 || outside.ctx != ctx {
				t.Fatal("discovery did not use original Store/context", err)
			}
			inside.owner = project
			current, err := resolver.ResolveInTx(ctx, store.tx, request)
			if err != nil || current.Details().ID != project || current.Equal(found) || inside.calls != 1 || outside.calls != 1 || inside.ctx != ctx {
				t.Fatal("transaction reused discovered ownership", err)
			}
			if _, err := resolver.ResolveInTx(ctx, f.NewTx(), request); !hasCode(err, f.Forbidden) || inside.calls != 1 {
				t.Fatal("foreign transaction reached SQL")
			}
		})
	}
}

func TestMaintenanceOwnerResolverFailsClosed(t *testing.T) {
	request := maintenanceRequestForTest(t)
	sql := &maintenanceOwnerSQL{kind: oc.Avatar, owner: request.Details().ObjectID.String()}
	store := &maintenanceOwnerStore{sql: sql, inside: sql, tx: f.NewTx()}
	resolver, err := NewMaintenanceOwnerResolver(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{pgx.ErrNoRows, f.NewFault(f.ResourceBusy, f.NotCommitted), errors.New("sql failure")} {
		sql.err = cause
		owner, err := resolver.Discover(context.Background(), request)
		if err == nil || owner.Validate() == nil {
			t.Fatal("failed lookup yielded an owner")
		}
		if fault, ok := cause.(*f.Fault); ok {
			var actual *f.Fault
			if !errors.As(err, &actual) || actual.Code != fault.Code || actual.CommitState != fault.CommitState || !errors.Is(err, cause) {
				t.Fatal("provider fault changed")
			}
		}
	}
	sql.err = nil
	sql.kind = oc.OwnerKind("unknown")
	if owner, err := resolver.Discover(context.Background(), request); err == nil || owner.Validate() == nil {
		t.Fatal("invalid owner accepted")
	}
	before := sql.calls
	if _, err := resolver.Discover(nil, request); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := resolver.ResolveInTx(context.Background(), store.tx, oc.AccessRequest{}); err == nil || store.inTxCalls != 0 {
		t.Fatal("invalid request reached Store")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.Discover(ctx, request); err == nil || sql.calls != before {
		t.Fatal("cancelled request queried SQL")
	}
	if _, err := NewMaintenanceOwnerResolver((*maintenanceOwnerStore)(nil)); err == nil {
		t.Fatal("typed nil Store accepted")
	}
}
