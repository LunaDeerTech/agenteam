package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Explicit Store and Execution controls test this service boundary; neither
// proves a real Execution, SQL constraints, an Attempt, or production permission.
type operationStoreControl struct {
	t                *testing.T
	rows             map[string][]any
	tx               f.Tx
	alive            bool
	held             []f.LockRequest
	checked          bool
	queries, inserts int
	unknown          bool
	attempt          f.ID[f.TransactionAttempt]
}

func (s *operationStoreControl) WithinTx(ctx context.Context, c f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.tx = f.NewTx()
	s.alive = true
	s.held = nil
	s.checked = false
	defer func() { s.alive = false }()
	before := make(map[string][]any, len(s.rows))
	for k, v := range s.rows {
		before[k] = slices.Clone(v)
	}
	if err := fn(ctx, s.tx); err != nil {
		s.rows = before
		var fault *f.Fault
		if !errors.As(err, &fault) {
			fault = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		return f.NotCommittedResult(fault)
	}
	if s.unknown {
		return f.UnknownResult(s.attempt, c)
	}
	return f.CommittedResult()
}
func (s *operationStoreControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.alive || s.tx != tx {
		return nil, fail(f.InvalidState)
	}
	return s, nil
}
func (s *operationStoreControl) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.alive || tx != s.tx {
		return fail(f.InvalidState)
	}
	for n, v := range locks {
		if v.Key.Validate() != nil || !v.Mode.Valid() || n > 0 && f.CompareLockKeys(locks[n-1].Key, v.Key) >= 0 {
			s.t.Fatal("noncanonical lock union")
		}
	}
	s.held = slices.Clone(locks)
	return nil
}
func (s *operationStoreControl) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.alive || tx != s.tx {
		return fail(f.InvalidState)
	}
	for _, want := range locks {
		found := false
		for _, got := range s.held {
			found = found || f.CompareLockKeys(got.Key, want.Key) == 0 && (got.Mode == f.Exclusive || got.Mode == want.Mode)
		}
		if !found {
			return fail(f.Forbidden)
		}
	}
	return nil
}
func rowKey(parts []any) string { raw, _ := json.Marshal(parts); return string(raw) }

type controlRow struct {
	values []any
	err    error
}

func (r controlRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("control_scan_arity")
	}
	for n, p := range dest {
		v := reflect.ValueOf(p).Elem()
		source := reflect.ValueOf(r.values[n])
		if source.Type().AssignableTo(v.Type()) {
			v.Set(source)
		} else if source.Type().ConvertibleTo(v.Type()) {
			v.Set(source.Convert(v.Type()))
		} else {
			return errors.New("control_scan_type")
		}
	}
	return nil
}
func (s *operationStoreControl) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	if !s.checked {
		s.t.Fatal("SQL preceded current Execution check")
	}
	s.queries++
	if !strings.HasPrefix(q, "SELECT operation_id,") {
		s.t.Fatal("unexpected query")
	}
	row, ok := s.rows[rowKey(args)]
	if !ok {
		return controlRow{err: pgx.ErrNoRows}
	}
	return controlRow{values: slices.Clone(row)}
}
func (s *operationStoreControl) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.t.Fatal("unexpected Rows query")
	return nil, nil
}
func (s *operationStoreControl) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !s.checked {
		s.t.Fatal("write preceded current Execution check")
	}
	if !strings.HasPrefix(q, "INSERT INTO agenteam_tool.operations(") || len(args) != 14 {
		s.t.Fatal("unexpected write")
	}
	key := rowKey([]any{args[3], args[4], args[5], args[6]})
	if s.rows[key] != nil {
		return pgconn.CommandTag{}, errors.New("control_unique")
	}
	s.rows[key] = append(slices.Clone(args), "created", int64(1))
	s.inserts++
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type executionControl struct {
	store         *operationStoreControl
	deny          error
	afterDiscover func()
	extra         []f.LockRequest
}
type executionPlanControl struct {
	owner   *executionControl
	binding tc.ToolCallBinding
	locks   []f.LockRequest
}

func (p executionPlanControl) RequiredLocks() []f.LockRequest { return slices.Clone(p.locks) }
func (a *executionControl) DiscoverToolCall(_ context.Context, b tc.ToolCallBinding) (tc.ToolCallPlan, error) {
	if a.afterDiscover != nil {
		a.afterDiscover()
	}
	return executionPlanControl{a, b, slices.Clone(a.extra)}, nil
}
func (a *executionControl) RequireToolCallInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, p tc.ToolCallPlan) error {
	plan, ok := p.(executionPlanControl)
	if !ok || plan.owner != a || !plan.binding.Actor.Equal(b.Actor) {
		return fail(f.Forbidden)
	}
	want, _ := json.Marshal(bindingProjection(plan.binding))
	got, _ := json.Marshal(bindingProjection(b))
	if !bytes.Equal(want, got) {
		return fail(f.Forbidden)
	}
	if _, err := a.store.InTx(tx); err != nil {
		return err
	}
	if err := a.store.RequireHeldLocks(ctx, tx, plan.locks); err != nil {
		return err
	}
	if a.deny != nil {
		return a.deny
	}
	a.store.checked = true
	return nil
}

func newOperationID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func operationArguments(t *testing.T, text string) []byte {
	t.Helper()
	v := map[string]any{"mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": text}}}}
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

const operationText = "---\nname: Example\ndescription: Controlled package\n---\nprivate-operation-canary\n"

func operationFixture(t *testing.T) (*Service, *operationStoreControl, *executionControl, tc.ToolCallBinding, []byte) {
	t.Helper()
	raw := operationArguments(t, operationText)
	project, agent, execution := newOperationID[id.Project](t), newOperationID[id.Agent](t), newOperationID[id.Execution](t)
	actor, e := id.NewAgentRun(project, agent, execution)
	if e != nil {
		t.Fatal(e)
	}
	canonical, e := CanonicalInstallArguments(context.Background(), raw)
	if e != nil {
		t.Fatal(e)
	}
	tool := newOperationID[id.Tool](t)
	b := tc.ToolCallBinding{Actor: actor, ProjectID: project, AgentID: agent, ExecutionID: execution, RoundID: newOperationID[struct{}](t).String(), SnapshotID: newOperationID[struct{}](t).String(), InputBindingID: newOperationID[struct{}](t).String(), LogicalCallID: newOperationID[mc.Call](t), InvocationID: newOperationID[mc.Invocation](t), CallID: "original-model-call", ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1}, Binding: tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}, Input: tc.ToolInputReference{PayloadID: newOperationID[struct{}](t).String(), SHA256: digest(raw), Bytes: f.Progress(len(raw))}, CanonicalArguments: canonical}
	store := &operationStoreControl{t: t, rows: map[string][]any{}, attempt: newOperationID[f.TransactionAttempt](t)}
	user, _ := f.UserLock(newOperationID[id.User](t).String())
	a := &executionControl{store: store, extra: []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: user, Mode: f.Exclusive}}}
	s, e := New(store, a)
	if e != nil {
		t.Fatal(e)
	}
	return s, store, a, b, raw
}

func TestOperationInstallCanonicalInput(t *testing.T) {
	_, _, _, b, raw := operationFixture(t)
	canonical, e := CanonicalInstallArguments(context.Background(), append([]byte(" \n"), raw...))
	if e != nil || canonical != b.CanonicalArguments {
		t.Fatal("whitespace altered fixed canonical arguments")
	}
	// This literal is the independently fixed canonical-v1 byte sequence:
	// sorted object keys, original array/text order and no trailing newline.
	want := `{"mode":"create","source":{"files":[{"path":"SKILL.md","utf8_text":"---\nname: Example\ndescription: Controlled package\n---\nprivate-operation-canary\n"}],"kind":"text_files"}}`
	if canonical != digest([]byte(want)) {
		t.Fatal("arguments do not use canonical-v1 bytes")
	}
	reordered := `{"source":{"kind":"text_files","files":[{"utf8_text":"---\nname: Example\ndescription: Controlled package\n---\nprivate-operation-canary\n","path":"SKILL.md"}]},"mode":"create"}`
	if got, err := CanonicalInstallArguments(context.Background(), []byte(reordered)); err != nil || got != canonical {
		t.Fatal("object key ordering changed argument semantics")
	}
	changed := operationArguments(t, operationText+"changed body\n")
	other, e := CanonicalInstallArguments(context.Background(), changed)
	if e != nil || other == canonical {
		t.Fatal("file text omitted from canonical arguments")
	}
	if _, e = prepareInput(context.Background(), b, changed); e == nil {
		t.Fatal("changed original payload accepted")
	}
	broken := bytes.Replace(raw, []byte(`"mode":"create"`), []byte(`"mode":"create","mode":"create"`), 1)
	if _, e = CanonicalInstallArguments(context.Background(), broken); e == nil {
		t.Fatal("duplicate JSON passed canonicalization")
	}
	// A fixed full fingerprint projection catches struct-order encoding at
	// every nested level independently of the arguments helper above.
	fixedID := "01900000-0000-7000-8000-000000000001"
	fixedDigest := "sha256:" + strings.Repeat("a", 64)
	fingerprintBytes := fmt.Sprintf(`{"Format":"canonical-v1","Input":{"Binding":{"Agent":"%[1]s","ArgumentsDigest":"%[2]s","Binding":{"contract_revision":"1","handler_id":"skill.install"},"Call":"call","Execution":"%[1]s","InputBinding":"%[1]s","Invocation":"%[1]s","LogicalCall":"%[1]s","ModelName":"tool_01900000000070008000000000000001","Payload":"%[1]s","PayloadBytes":"1","PayloadDigest":"%[2]s","Project":"%[1]s","Round":"%[1]s","Snapshot":"%[1]s","Spec":{"SpecRevision":"1","ToolID":"%[1]s"},"format_version":1},"ManifestDigest":"%[2]s","NormalizedName":"example","PackageBytes":"1","PackageDigest":"%[2]s"},"Skill":"%[1]s"}`, fixedID, fixedDigest)
	var vector struct {
		Input installData
		Skill sc.SkillID
	}
	if err := json.Unmarshal([]byte(fingerprintBytes), &vector); err != nil {
		t.Fatal(err)
	}
	if got, err := fingerprint(vector.Input, vector.Skill); err != nil || got != digest([]byte(fingerprintBytes)) {
		t.Fatal("fingerprint does not use canonical-v1 bytes")
	}
}

func TestOperationPrepareAndLookupBindOriginalInput(t *testing.T) {
	s, store, _, b, raw := operationFixture(t)
	first, e := s.PrepareInstall(context.Background(), b, raw)
	if e != nil {
		t.Fatal(e)
	}
	original, e := first.Facts()
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.PrepareInstall(context.Background(), b, raw)
	if e != nil {
		t.Fatal(e)
	}
	repeat, _ := second.Facts()
	if original != repeat || store.inserts != 1 {
		t.Fatal("same call allocated a second operation or target")
	}
	observed, found, e := s.LookupInstall(context.Background(), b)
	if e != nil || !found {
		t.Fatal("committed operation not observed")
	}
	facts, _ := observed.Facts()
	if facts != original || store.inserts != 1 {
		t.Fatal("lookup mutated original operation")
	}
	drift := b
	drift.RoundID = newOperationID[struct{}](t).String()
	_, e = s.PrepareInstall(context.Background(), drift, raw)
	var fault *f.Fault
	if !errors.As(e, &fault) || fault.Code != f.IdempotencyKeyReused || store.inserts != 1 {
		t.Fatal("same input identity changed its binding")
	}
	newCall := b
	newCall.CallID = "next-model-call"
	next, e := s.PrepareInstall(context.Background(), newCall, raw)
	if e != nil {
		t.Fatal(e)
	}
	nf, _ := next.Facts()
	if nf.ID == original.ID || nf.SkillID == original.SkillID || store.inserts != 2 {
		t.Fatal("distinct model calls were merged")
	}
}

func TestOperationUnknownRetainsCauseAndRequiresLookup(t *testing.T) {
	s, store, _, b, raw := operationFixture(t)
	store.unknown = true
	out, e := s.PrepareInstall(context.Background(), b, raw)
	if _, valid := out.Facts(); valid == nil {
		t.Fatal("uncertain commit exposed success")
	}
	var fault *f.Fault
	var commit CommitFailure
	if !errors.As(e, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || !errors.As(e, &commit) || commit.Result().AttemptID() != store.attempt || commit.Result().Cause().Validate() != nil || store.inserts != 1 {
		t.Fatal("unknown lost original cause or retried")
	}
	store.unknown = false
	_, found, e := s.LookupInstall(context.Background(), b)
	if e != nil || !found || store.inserts != 1 {
		t.Fatal("lookup did not observe original committed fact")
	}
	missing := b
	missing.CallID = "not-observed"
	_, found, e = s.LookupInstall(context.Background(), missing)
	if e != nil || found || store.inserts != 1 {
		t.Fatal("negative lookup became creation")
	}
}

func TestOperationRequiresCurrentExecutionAndSafeMetadata(t *testing.T) {
	s, store, authority, b, raw := operationFixture(t)
	if _, e := New(store, (*executionControl)(nil)); e == nil {
		t.Fatal("typed nil Execution provider accepted")
	}
	authority.deny = f.NewFault(f.Forbidden, f.NotStarted)
	if _, e := s.PrepareInstall(context.Background(), b, raw); e == nil || store.queries != 0 || store.inserts != 0 {
		t.Fatal("current denial reached Tool SQL")
	}
	authority.deny = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authority.afterDiscover = cancel
	if _, e := s.PrepareInstall(ctx, b, raw); !errors.Is(e, context.Canceled) || store.queries != 0 {
		t.Fatal("cancelled discovery reached Tool SQL")
	}
	authority.afterDiscover = nil
	out, e := s.PrepareInstall(context.Background(), b, raw)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := json.Marshal(map[string]any{"result": out, "binding": b})
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encoded, []byte("original-model-call")) || bytes.Contains(encoded, []byte("private-operation-canary")) || strings.Contains(fmt.Sprintf("%+v", out), b.Input.PayloadID) {
		t.Fatal("default metadata formatting leaked private fields")
	}
	for _, row := range store.rows {
		if bytes.Contains(row[9].([]byte), []byte("private-operation-canary")) {
			t.Fatal("file body entered Tool metadata")
		}
	}
}
