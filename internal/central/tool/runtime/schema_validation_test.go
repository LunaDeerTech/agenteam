package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/jackc/pgx/v5"
)

// Only SQL/lock storage is controlled. The Registry historical reader and the
// standard schema compiler/validator are real; this is not PG or authorization.
type schemaStoreControl struct {
	*operationStoreControl
	ctx       context.Context
	ref       tc.SpecRef
	versions  map[f.Version][]byte
	read      []tc.SpecRef
	readError error
	readHook  func()
}

func (s *schemaStoreControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.operationStoreControl.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *schemaStoreControl) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if ctx != s.ctx {
		s.t.Fatal("schema metadata replaced original caller context")
	}
	return s.operationStoreControl.RequireHeldLocks(ctx, tx, locks)
}
func (*schemaStoreControl) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	panic("schema adapter must not begin a transaction")
}
func (*schemaStoreControl) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("schema adapter must not extend the caller lock set")
}
func (s *schemaStoreControl) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	tool, _ := f.AggregateLock(f.ToolSpecAggregate, s.ref.ToolID.String())
	if ctx != s.ctx || s.RequireHeldLocks(ctx, s.tx, []f.LockRequest{registry.RegistryLock(f.Shared), {Key: tool, Mode: f.Shared}}) != nil {
		s.t.Fatal("historical read preceded the original Tx/complete held locks")
	}
	if !strings.Contains(query, "agenteam_tool.spec_revisions s") || !strings.Contains(query, "s.spec_revision=$2") || strings.Contains(query, "registrations") || strings.Contains(query, "latest_revision") || len(args) != 2 || args[0] != s.ref.ToolID.String() {
		s.t.Fatal("schema reader replaced the original historical identity")
	}
	revision, ok := args[1].(int64)
	if !ok {
		s.t.Fatal("schema revision was not bound as the original integer")
	}
	s.read = append(s.read, tc.SpecRef{ToolID: s.ref.ToolID, SpecRevision: f.Version(revision)})
	if s.readHook != nil {
		s.readHook()
	}
	if s.readError != nil {
		return controlRow{err: s.readError}
	}
	raw, found := s.versions[f.Version(revision)]
	if !found {
		return controlRow{err: pgx.ErrNoRows}
	}
	return controlRow{values: []any{builtin.SkillInstallStableKey, bytes.Clone(raw)}}
}

func schemaAdapterFixture(t *testing.T) (*RegistrySchemaValidator, *schemaStoreControl) {
	t.Helper()
	tool, err := f.ParseID[id.Tool]("01997e80-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	s := &schemaStoreControl{
		operationStoreControl: &operationStoreControl{t: t, tx: f.NewTx(), alive: true},
		ctx:                   context.WithValue(context.Background(), struct{}{}, "original-schema-call"),
		ref:                   tc.SpecRef{ToolID: tool, SpecRevision: 1},
		versions:              map[f.Version][]byte{},
	}
	lock, _ := f.AggregateLock(f.ToolSpecAggregate, tool.String())
	s.held = []f.LockRequest{registry.RegistryLock(f.Shared), {Key: lock, Mode: f.Shared}}
	definition := builtin.SkillInstallDefinition()
	raw, err := tc.CanonicalDefinition(s.ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	s.versions[1] = raw
	// No current Source, registration or Actor is manufactured: the real
	// Registry deliberately permits only historical metadata through this port.
	r, err := registry.New(s, registry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewRegistrySchemaValidator(s, r)
	if err != nil {
		t.Fatal(err)
	}
	return v, s
}

func schemaFault(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("expected safe %s, got %v", code, err)
	}
}

const schemaInstallInput = `{"source":{"kind":"text_files","files":[{"path":"SKILL.md","utf8_text":"原文。"}]},"mode":"create"}`
const schemaInstallOutput = `{"skill_id":"01997e80-0000-7000-8000-000000000002","revision":"1","version":"1"}`

func TestRegistrySchemaValidatorHistoricalDefinitionAndOriginalTx(t *testing.T) {
	v, s := schemaAdapterFixture(t)
	raw := []byte(schemaInstallInput)
	before := bytes.Clone(raw)
	if err := v.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, raw); err != nil || !bytes.Equal(raw, before) {
		t.Fatal("real Registry/standard validation changed or rejected input", err)
	}
	schemaFault(t, v.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, []byte(`{"mode":"create"}`)), f.InvalidArgument)
	// A different later revision must neither replace the original schema nor
	// prevent accounting of output using that original historical definition.
	definition := builtin.SkillInstallDefinition()
	definition.InputSchema = json.RawMessage(`{"type":"integer","const":2}`)
	definition.OutputSchema = json.RawMessage(`{"const":"second-revision"}`)
	var err error
	s.versions[2], err = tc.CanonicalDefinition(s.ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	later := tc.SpecRef{ToolID: s.ref.ToolID, SpecRevision: 2}
	if err = v.ValidateInstallInputInTx(s.ctx, s.tx, later, []byte(`2`)); err != nil {
		t.Fatal(err)
	}
	if err = v.ValidateInstallOutputInTx(s.ctx, s.tx, s.ref, json.RawMessage(schemaInstallOutput)); err != nil {
		t.Fatal("terminal output lost original schema revision", err)
	}
	schemaFault(t, v.ValidateInstallOutputInTx(s.ctx, s.tx, later, json.RawMessage(schemaInstallOutput)), f.InvalidArgument)
	if !slices.Equal(s.read, []tc.SpecRef{s.ref, s.ref, later, s.ref, later}) {
		t.Fatal("a repeated call skipped its original metadata transaction")
	}
	delete(s.versions, 1)
	schemaFault(t, v.ValidateInstallOutputInTx(s.ctx, s.tx, s.ref, json.RawMessage(schemaInstallOutput)), f.NotFound)
}

func TestRegistrySchemaValidatorRejectsUnboundTransactionAndLocks(t *testing.T) {
	v, s := schemaAdapterFixture(t)
	raw := []byte(schemaInstallInput)
	schemaFault(t, v.ValidateInstallInputInTx(s.ctx, f.NewTx(), s.ref, raw), f.InvalidState)
	s.alive = false
	schemaFault(t, v.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, raw), f.InvalidState)
	s.alive = true
	all := s.held
	for _, held := range [][]f.LockRequest{nil, all[:1], all[1:]} {
		s.held = held
		schemaFault(t, v.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, raw), f.Forbidden)
	}
	s.held = all
	if len(s.read) != 0 {
		t.Fatal("foreign/closed Tx or missing locks reached historical SQL")
	}
	// The adapter and real Registry must each recognize the same Tx. Supplying
	// a correctly typed Registry from a different Store must still fail.
	other, _ := schemaAdapterFixture(t)
	foreign, err := NewRegistrySchemaValidator(s, other.data().reader)
	if err != nil {
		t.Fatal(err)
	}
	schemaFault(t, foreign.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, raw), f.InvalidState)
	var absent *registry.Registry
	_, err = NewRegistrySchemaValidator(s, absent)
	schemaFault(t, err, f.DependencyUnbound)
	_, err = NewRegistrySchemaValidator(nil, v.data().reader)
	schemaFault(t, err, f.DependencyUnbound)
	var zero RegistrySchemaValidator
	schemaFault(t, zero.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, raw), f.DependencyUnbound)
	schemaFault(t, v.ValidateInstallInputInTx(nil, s.tx, s.ref, raw), f.InvalidArgument)
	schemaFault(t, v.ValidateInstallInputInTx(s.ctx, s.tx, tc.SpecRef{}, raw), f.InvalidArgument)
}

func TestRegistrySchemaValidatorSafeFailuresAndCancellation(t *testing.T) {
	v, s := schemaAdapterFixture(t)
	const canary = "private-schema-and-instance-canary"
	s.readError = errors.New(canary)
	err := v.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, []byte(schemaInstallInput))
	schemaFault(t, err, f.DependencyUnavailable)
	if errors.Unwrap(err) != nil {
		t.Fatal("reader retained a sensitive diagnostic cause")
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("fixture", "nested", map[string]any{"values": []any{v, err}})
	encoded, _ := json.Marshal(struct{ Validator *RegistrySchemaValidator }{v})
	if strings.Contains(log.String()+string(encoded)+fmt.Sprintf("%#v %+v", v, err), canary) {
		t.Fatal("schema metadata escaped default projection")
	}
	s.readError = nil
	definition := builtin.SkillInstallDefinition()
	definition.InputSchema = json.RawMessage(`{"$ref":"https://must-not-resolve.invalid/schema"}`)
	s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	schemaFault(t, v.ValidateInstallInputInTx(s.ctx, s.tx, s.ref, []byte(schemaInstallInput)), f.SchemaUnsupported)
	definition.OutputSchema = nil
	s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	schemaFault(t, v.ValidateInstallOutputInTx(s.ctx, s.tx, s.ref, json.RawMessage(schemaInstallOutput)), f.SchemaUnsupported)
	ctx, cancel := context.WithCancel(s.ctx)
	s.ctx = ctx
	s.readHook = cancel
	before := len(s.read)
	if err = v.ValidateInstallInputInTx(ctx, s.tx, s.ref, []byte(schemaInstallInput)); !errors.Is(err, context.Canceled) || len(s.read) != before+1 {
		t.Fatal("original synchronous metadata read lost cancellation", err)
	}
	if err = v.ValidateInstallInputInTx(ctx, s.tx, s.ref, []byte(schemaInstallInput)); !errors.Is(err, context.Canceled) || len(s.read) != before+1 {
		t.Fatal("cancelled caller started another metadata read", err)
	}
}
