package object

import (
	"context"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type mcpMappingRow struct {
	values []any
	err    error
}

func (r mcpMappingRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		panic("unexpected mapping columns")
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

type mcpMappingSQL struct {
	postgres.SQLExecutor
	upload  uploadRow
	attempt attemptRow
	calls   int
	err     error
	missing bool
}

func (s *mcpMappingSQL) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.calls++
	if s.err != nil {
		return mcpMappingRow{err: s.err}
	}
	if s.missing {
		return mcpMappingRow{err: pgx.ErrNoRows}
	}
	if strings.Contains(query, "upload_attempts") {
		a := s.attempt
		if len(args) != 1 || args[0] != s.upload.attempt.String() {
			panic("wrong attempt")
		}
		return mcpMappingRow{values: []any{a.id.String(), a.upload.String(), a.object.String(), a.process.String(), a.key, a.phase, a.ordinal, a.size, make([]byte, 32), a.late, a.closed, a.cleaning, a.kind, ""}}
	}
	u := s.upload
	command, _ := commandIdentity(u.owner, u.command)
	if len(args) != 1 || !reflect.DeepEqual(args[0], commandHash(command)) {
		panic("wrong original command")
	}
	return mcpMappingRow{values: []any{u.id.String(), u.object.String(), u.receipt.String(), u.attempt.String(), u.owner.Details().Kind, u.owner.Details().ID, u.owner.Details().ProjectID, u.command, u.digest, u.actor, u.state, u.disposition, u.creation, u.existence, u.initiator, u.initiatorID, u.executionID, u.expected}}
}

type mcpMappingStore struct {
	Store
	outside, inside *mcpMappingSQL
	tx              f.Tx
	inTx            int
}

func (s *mcpMappingStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	return s.outside.QueryRow(ctx, q, args...)
}
func (s *mcpMappingStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.inTx++
	if tx != s.tx {
		return nil, f.NewFault(f.Forbidden, f.NotCommitted)
	}
	return s.inside, nil
}
func mappingFixture(t *testing.T) (*mcpMappingStore, oc.ObjectOwner, f.IdempotencyKey) {
	t.Helper()
	materialization, project := auditID[oc.Upload](t), auditID[id.Project](t)
	owner, err := oc.NewObjectOwner(oc.MCPContent, materialization.String(), project.String())
	if err != nil {
		t.Fatal(err)
	}
	key := f.IdempotencyKey(materialization.String())
	u := uploadRow{id: auditID[oc.Upload](t), object: auditID[oc.StoredObject](t), receipt: auditID[oc.Receipt](t), attempt: auditID[oc.Attempt](t), owner: owner, command: key, state: "unknown", disposition: "revoked", creation: auditID[oc.Upload](t).String(), existence: oc.ProspectiveOwner, initiator: id.AgentRun, initiatorID: auditID[id.Agent](t).String(), executionID: auditID[id.Execution](t).String(), digest: make([]byte, 32)}
	a := attemptRow{id: u.attempt, upload: u.id, object: u.object, process: auditID[oc.Process](t), kind: "private_candidate", cleaning: true}
	return &mcpMappingStore{outside: &mcpMappingSQL{upload: u, attempt: a}, inside: &mcpMappingSQL{upload: u, attempt: a}, tx: f.NewTx()}, owner, key
}

func TestMCPUploadResolverRechecksOriginalTransaction(t *testing.T) {
	s, owner, key := mappingFixture(t)
	r, err := NewMCPUploadResolver(s)
	if err != nil {
		t.Fatal(err)
	}
	discovered, found, err := r.Discover(context.Background(), owner, key)
	if err != nil || !found || discovered.Validate() != nil {
		t.Fatal("discover", found, err)
	}
	s.inside.upload.disposition = "attached"
	current, found, err := r.ResolveInTx(context.Background(), s.tx, owner, key)
	if err != nil || !found || current.Disposition != "attached" || s.outside.calls != 2 || s.inside.calls != 2 {
		t.Fatal("did not re-read current tuple", found, err)
	}
	if _, _, err := r.ResolveInTx(context.Background(), f.NewTx(), owner, key); !hasCode(err, f.Forbidden) || s.inside.calls != 2 {
		t.Fatal("foreign Tx reached SQL", err)
	}
	s.outside.missing = true
	missing, found, err := r.Discover(context.Background(), owner, key)
	if err != nil || found || missing.Validate() == nil {
		t.Fatal("missing became a mapping/death proof", found, err)
	}
}

func TestMCPUploadResolverRejectsForeignTuple(t *testing.T) {
	for name, change := range map[string]func(*mcpMappingSQL){
		"wrong-attempt-upload": func(s *mcpMappingSQL) { s.attempt.upload = auditID[oc.Upload](t) },
		"wrong-attempt-object": func(s *mcpMappingSQL) { s.attempt.object = auditID[oc.StoredObject](t) },
		"foreign-kind":         func(s *mcpMappingSQL) { s.attempt.kind = "transfer" },
		"missing-process":      func(s *mcpMappingSQL) { s.attempt.process = oc.ProcessID{} },
		"wrong-actor":          func(s *mcpMappingSQL) { s.upload.initiator = id.Human },
		"missing-execution":    func(s *mcpMappingSQL) { s.upload.executionID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s, owner, key := mappingFixture(t)
			change(s.outside)
			r, _ := NewMCPUploadResolver(s)
			if m, found, err := r.Discover(context.Background(), owner, key); err == nil || found || m.Validate() == nil {
				t.Fatal("foreign tuple accepted", found, err)
			}
		})
	}
	s, owner, key := mappingFixture(t)
	r, _ := NewMCPUploadResolver(s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		if _, _, err := r.Discover(ctx, owner, key); err == nil || s.outside.calls != 0 {
			t.Fatal("invalid context reached SQL")
		}
	}
	if _, _, err := r.Discover(context.Background(), owner, "different-key"); err == nil || s.outside.calls != 0 {
		t.Fatal("foreign command reached SQL")
	}
}
