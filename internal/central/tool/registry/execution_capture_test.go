package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled SQL, Source and Execution ports exercise this provider's actual
// public calls. They do not establish a real preparing witness, PG commit,
// runtime backend or complete Core Tool set; those require the joint fixture.
type captureTestRow func(...any) error

func (r captureTestRow) Scan(dest ...any) error { return r(dest...) }
func captureValues(values ...any) postgres.Row {
	return captureTestRow(func(dest ...any) error {
		if len(dest) != len(values) {
			return errors.New("unexpected capture row shape")
		}
		for n, v := range values {
			reflect.ValueOf(dest[n]).Elem().Set(reflect.ValueOf(v))
		}
		return nil
	})
}

type captureTestStore struct {
	*metadataStore
	request       tc.ExecutionToolCaptureRequest
	version       f.Version
	allowed, core []id.ToolID
	sources       map[id.ToolID]*metadataSource
	headMissing   bool
	unknown       bool
	after         func()
	returned      bool
	within        int
	attempt       f.ID[f.TransactionAttempt]
	cause         f.TransactionCause
	owner         *captureTestOwner
}

func (s *captureTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.metadataStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *captureTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.within++
	s.cause = cause
	err := fn(ctx, s.tx)
	if s.after != nil {
		s.after()
	}
	s.returned = true
	if s.unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	if err != nil {
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}
func captureRawIDs(ids []id.ToolID) []string {
	out := make([]string, 0, len(ids))
	for _, v := range ids {
		out = append(out, v.String())
	}
	return out
}
func (s *captureTestStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	s.queries++
	if err := ctx.Err(); err != nil {
		return captureTestRow(func(...any) error { return err })
	}
	switch {
	case strings.Contains(sql, "FROM agenteam_tool.agent_configurations h"):
		if s.headMissing {
			return metadataRow{err: pgx.ErrNoRows}
		}
		return captureValues(s.request.ProjectID.String(), int64(s.version), captureRawIDs(s.allowed))
	case strings.HasPrefix(sql, "SELECT ARRAY("):
		return captureValues(captureRawIDs(s.core))
	case strings.Contains(sql, "JOIN agenteam_tool.registrations"):
		for tool, p := range s.sources {
			if tool.String() == args[0] {
				b, err := tc.CanonicalDefinition(ctx, p.registration.Definition)
				if err != nil {
					return captureTestRow(func(...any) error { return err })
				}
				return currentBuiltinRow{registration: p.registration, spec: tc.SpecRef{ToolID: tool, SpecRevision: 1}, definition: b}
			}
		}
		return metadataRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "FROM agenteam_tool.execution_configurations"):
		if len(s.args) == 0 {
			return metadataRow{err: pgx.ErrNoRows}
		}
		v := s.args[0]
		return captureValues(v[1].(string), v[2].(string), v[3].(int64), v[4].(string), v[5].(string), int64(v[6].(int)))
	case strings.HasPrefix(sql, "SELECT count(*)"):
		return captureValues(int64(len(s.args) - 1))
	case strings.Contains(sql, "FROM agenteam_tool.execution_references"):
		for _, v := range s.args[1:] {
			if v[1] == args[1] {
				return captureValues(v[2].(int64), v[3].(string), v[4].(string), v[5].(int64), v[6].(string), v[7].(string), v[8].(string))
			}
		}
		return metadataRow{err: pgx.ErrNoRows}
	default:
		panic("unexpected capture query")
	}
}
func (s *captureTestStore) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}
	if s.owner.finalCalls == 0 {
		return pgconn.CommandTag{}, errors.New("write before original final proof")
	}
	return s.metadataStore.Exec(ctx, sql, args...)
}

type captureTestOwner struct {
	request                    tc.ExecutionToolCaptureRequest
	discovery                  tc.ExecutionToolDiscoveryFacts
	final                      tc.ExecutionToolCaptureFacts
	tx                         f.Tx
	discoveryCalls, finalCalls int
	discoveryErr, finalErr     error
	finalWait                  func(context.Context) error
}

func (o *captureTestOwner) RequireToolDiscoveryInTx(ctx context.Context, tx f.Tx, r tc.ExecutionToolCaptureRequest) (tc.ExecutionToolDiscoveryFacts, error) {
	o.discoveryCalls++
	if tx != o.tx || r != o.request {
		return tc.ExecutionToolDiscoveryFacts{}, fail(f.Forbidden)
	}
	if o.discoveryErr != nil {
		return tc.ExecutionToolDiscoveryFacts{}, o.discoveryErr
	}
	return o.discovery.Clone(), ctx.Err()
}
func (o *captureTestOwner) RequireToolCaptureInTx(ctx context.Context, tx f.Tx, r tc.ExecutionToolCaptureRequest) (tc.ExecutionToolCaptureFacts, error) {
	o.finalCalls++
	if tx != o.tx || r != o.request {
		return tc.ExecutionToolCaptureFacts{}, fail(f.Forbidden)
	}
	if o.finalWait != nil {
		if err := o.finalWait(ctx); err != nil {
			return tc.ExecutionToolCaptureFacts{}, err
		}
	}
	if o.finalErr != nil {
		return tc.ExecutionToolCaptureFacts{}, o.finalErr
	}
	return o.final.Clone(), ctx.Err()
}
func captureID[T any](t *testing.T, n int) f.ID[T] {
	t.Helper()
	v, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func newCaptureTest(t *testing.T) (*ExecutionCapture, *captureTestStore, *captureTestOwner) {
	t.Helper()
	r := tc.ExecutionToolCaptureRequest{ProjectID: captureID[id.Project](t, 1), AgentID: captureID[id.Agent](t, 2), ExecutionID: captureID[id.Execution](t, 3)}
	s := &captureTestStore{metadataStore: &metadataStore{tx: f.NewTx()}, request: r, version: 7, sources: map[id.ToolID]*metadataSource{}, attempt: captureID[f.TransactionAttempt](t, 90)}
	s.allowed = []id.ToolID{captureID[id.Tool](t, 11), captureID[id.Tool](t, 12)}
	s.core = []id.ToolID{captureID[id.Tool](t, 13)}
	now, _ := f.NewInstant(time.Unix(1, 0))
	p := pc.ProjectRef{ID: r.ProjectID, OwnerUserID: captureID[id.User](t, 4), Name: "Project", NormalizedName: "project", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}
	binding, _ := f.ParseDigest("sha256:" + strings.Repeat("a", 64))
	o := &captureTestOwner{request: r, tx: s.tx, discovery: tc.ExecutionToolDiscoveryFacts{Project: p, AttemptBinding: binding, DeniedToolIDs: []id.ToolID{s.allowed[1]}, ScopeConstraints: []json.RawMessage{}}, final: tc.ExecutionToolCaptureFacts{Project: p, AttemptBinding: binding, AgentVersion: 7, Selection: tc.ToolSnapshotSelection{AllowedToolIDs: slices.Clone(s.allowed), DeniedToolIDs: []id.ToolID{s.allowed[1]}, ScopeConstraints: []json.RawMessage{}}}}
	s.owner = o
	sources := map[string]BuiltinSource{}
	for n, tool := range append(slices.Clone(s.allowed), s.core...) {
		class := tc.OrdinaryTool
		if n == 2 {
			class = tc.CoreTool
		}
		key := fmt.Sprintf("builtin:capture.%d", n)
		p := &metadataSource{registration: tc.BuiltinRegistration{Definition: tc.Definition{StableKey: key, Name: "Capture", InputSchema: []byte(`{"type":"object","unevaluatedProperties":false}`)}, Binding: tc.BuiltinBinding{HandlerID: "capture", ContractRevision: 1}, ScopeResolverID: "capture.scope", RiskClassifierID: "capture.risk", Class: class, Active: true}}
		s.sources[tool] = p
		sources[key] = p
	}
	registry, err := New(s, Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewExecutionCapture(registry, o)
	if err != nil {
		t.Fatal(err)
	}
	return c, s, o
}
func discoverCaptureTest(t *testing.T, c *ExecutionCapture, s *captureTestStore) tc.ExecutionToolPlan {
	t.Helper()
	plan, err := c.DiscoverExecutionTools(context.Background(), s.request)
	if err != nil {
		t.Fatal(err)
	}
	s.held = plan.RequiredLocks()
	return plan
}

func TestToolExecutionCaptureFixesRealMetadataAndReferences(t *testing.T) {
	c, s, o := newCaptureTest(t)
	plan := discoverCaptureTest(t, c, s)
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Exclusive
	if plan.RequiredLocks()[0].Mode != f.Shared {
		t.Fatal("plan exported mutable lock storage")
	}
	tools, err := c.ResolveExecutionToolsInTx(context.Background(), s.tx, s.request, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].ToolID != s.allowed[0] || tools[1].ToolID != s.core[0] || tools[1].BindingSnapshot.Class != tc.CoreTool || len(s.writes) != 3 || o.discoveryCalls != 1 || o.finalCalls != 1 || s.within != 1 {
		t.Fatal("fixed ordinary/core selection or same-Tx reference publication changed")
	}
	for _, v := range tools {
		if v.Validate() != nil || v.SpecRevision != 1 {
			t.Fatal("invalid fixed metadata/name mapping")
		}
	}
	if s.sources[s.allowed[1]].checked != 0 {
		t.Fatal("denied tool was resolved")
	}
	for _, sql := range s.writes {
		if !strings.Contains(sql, "agenteam_tool.execution_") {
			t.Fatal("capture wrote outside its domain")
		}
	}
	tools[0].ModelVisibleName = "changed"
	again, err := c.ResolveExecutionToolsInTx(context.Background(), s.tx, s.request, plan)
	if err != nil || len(s.writes) != 3 || again[0].Validate() != nil {
		t.Fatal("same frozen metadata replay replaced references or leaked output mutation")
	}
}

func TestToolExecutionCaptureRejectsUnprovenOrChangedFacts(t *testing.T) {
	for _, name := range []string{"foreign-issuer", "foreign-tx", "missing-lock", "final-proof", "agent-version", "allowed-set", "policy", "constraint", "binding", "registration", "attempt"} {
		t.Run(name, func(t *testing.T) {
			c, s, o := newCaptureTest(t)
			plan := discoverCaptureTest(t, c, s)
			tx := s.tx
			switch name {
			case "foreign-issuer":
				c, _ = NewExecutionCapture(c.state().registry, o)
			case "foreign-tx":
				tx = f.NewTx()
			case "missing-lock":
				s.held = nil
			case "final-proof":
				o.finalErr = fail(f.Forbidden)
			case "agent-version":
				s.version++
			case "allowed-set":
				o.final.Selection.AllowedToolIDs = []id.ToolID{}
			case "policy":
				o.final.Selection.DeniedToolIDs = []id.ToolID{}
			case "constraint":
				o.final.Selection.ScopeConstraints = []json.RawMessage{json.RawMessage(`{}`)}
			case "binding":
				s.sources[s.allowed[0]].registration.Binding.ContractRevision++
			case "registration":
				delete(s.sources, s.allowed[0])
			case "attempt":
				o.final.AttemptBinding, _ = f.ParseDigest("sha256:" + strings.Repeat("b", 64))
			}
			tools, err := c.ResolveExecutionToolsInTx(context.Background(), tx, s.request, plan)
			if err == nil || tools != nil || len(s.writes) != 0 {
				t.Fatal("changed/unproven capture published partial references")
			}
		})
	}
}

func TestToolExecutionCaptureEmptySetStillRequiresOwner(t *testing.T) {
	c, s, o := newCaptureTest(t)
	s.allowed = []id.ToolID{}
	s.core = []id.ToolID{}
	o.final.Selection.AllowedToolIDs = []id.ToolID{}
	if _, err := NewExecutionCapture(c.state().registry, nil); err == nil {
		t.Fatal("empty set bypassed bound owner")
	}
	o.discoveryErr = fail(f.Forbidden)
	if p, err := c.DiscoverExecutionTools(context.Background(), s.request); err == nil || p != nil || s.queries != 0 {
		t.Fatal("unauthorized discovery read metadata")
	}
	o.discoveryErr = nil
	s.headMissing = true
	if p, err := c.DiscoverExecutionTools(context.Background(), s.request); err == nil || p != nil {
		t.Fatal("missing canonical head was treated as empty")
	}
	s.headMissing = false
	plan := discoverCaptureTest(t, c, s)
	tools, err := c.ResolveExecutionToolsInTx(context.Background(), s.tx, s.request, plan)
	if err != nil || tools == nil || len(tools) != 0 || len(s.writes) != 1 || o.finalCalls != 1 {
		t.Fatal("authorized empty selection lacks its real scoped head")
	}
}

func TestToolExecutionCaptureJoinsOriginalCallAndKeepsUnknown(t *testing.T) {
	c, s, _ := newCaptureTest(t)
	s.unknown = true
	plan, err := c.DiscoverExecutionTools(context.Background(), s.request)
	var unknown *UnknownError
	if plan != nil || !errors.As(err, &unknown) || unknown.AttemptID() != s.attempt || !reflect.DeepEqual(unknown.Cause().Details(), s.cause.Details()) || !s.returned || s.within != 1 || len(s.writes) != 0 {
		t.Fatal("physical Unknown lost its original identity or published a plan")
	}
	c, s, _ = newCaptureTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.after = cancel
	if plan, err = c.DiscoverExecutionTools(ctx, s.request); plan != nil || err != context.Canceled || !s.returned {
		t.Fatal("late cancellation upgraded a completed discovery")
	}
	c, s, o := newCaptureTest(t)
	plan = discoverCaptureTest(t, c, s)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	o.finalWait = func(ctx context.Context) error {
		close(entered)
		<-release
		return fmt.Errorf("private-material: %w", ctx.Err())
	}
	go func() {
		values, err := c.ResolveExecutionToolsInTx(ctx, s.tx, s.request, plan)
		if values != nil {
			done <- errors.New("partial result")
			return
		}
		done <- err
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("returned before original authority callback")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err = <-done; err != context.Canceled || strings.Contains(fmt.Sprintf("%+v", err), "private-material") || len(s.writes) != 0 {
		t.Fatal("original cancellation tail or safe projection lost")
	}
}
