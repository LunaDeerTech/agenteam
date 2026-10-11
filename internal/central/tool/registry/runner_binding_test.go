package registry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

func TestToolRunnerExplicitMetadataSourceAndStableReconcile(t *testing.T) {
	s := &metadataStore{tx: f.NewTx(), held: []f.LockRequest{RegistryLock(f.Exclusive)}}
	source, err := NewReadFileMetadataSource(s)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(s, Options{RunnerSources: map[string]RunnerSource{tc.RunnerReadFileKey: source}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range []f.Tx{f.Tx{}, f.NewTx()} {
		if _, err = r.ReconcileRunnerInTx(context.Background(), tx, tc.RunnerReadFileKey); err == nil || s.queries != 0 || len(s.writes) != 0 {
			t.Fatal("foreign transaction reached source or SQL")
		}
	}
	s.held = []f.LockRequest{RegistryLock(f.Shared)}
	if _, err = r.ReconcileRunnerInTx(context.Background(), s.tx, tc.RunnerReadFileKey); err == nil || s.queries != 0 {
		t.Fatal("metadata write skipped Registry EX")
	}
	s.held = []f.LockRequest{RegistryLock(f.Exclusive)}
	ref, err := r.ReconcileRunnerInTx(context.Background(), s.tx, tc.RunnerReadFileKey)
	if err != nil || ref.Validate() != nil || len(s.writes) != 3 {
		t.Fatal("Runner metadata did not persist")
	}
	if !strings.Contains(s.writes[2], "NULL,NULL") || !strings.Contains(s.writes[2], "'runner'") || len(s.args[2]) != 8 || s.args[2][5] != "read-file" || s.args[2][6] != "1" || s.args[2][7] != 1 {
		t.Fatal("Runner metadata used a Builtin disguise")
	}
	raw, err := tc.CanonicalDefinition(context.Background(), tc.ReadFileDefinition())
	if err != nil {
		t.Fatal(err)
	}
	s.row = metadataRow{tool: ref.ToolID.String(), key: tc.RunnerReadFileKey, revision: 1, definition: raw}
	s.writes = nil
	s.args = nil
	again, err := r.ReconcileRunnerInTx(context.Background(), s.tx, tc.RunnerReadFileKey)
	if err != nil || again != ref || len(s.writes) != 1 {
		t.Fatal("same Runner definition allocated new identity or revision")
	}
	if _, err = r.ReconcileBuiltinInTx(context.Background(), s.tx, tc.RunnerReadFileKey); err == nil {
		t.Fatal("Runner used Builtin writer")
	}
	if _, err = New(s, Options{RunnerSources: map[string]RunnerSource{"runner:command": source}}); err == nil {
		t.Fatal("undeclared Runner source accepted")
	}
	unbound, err := New(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unbound.ReconcileRunnerInTx(context.Background(), s.tx, tc.RunnerReadFileKey); err == nil {
		t.Fatal("default Registry supplied Runner source")
	}
}

// Controlled SQL proves the metadata boundary algorithm, not real PG or a
// Model/Execution authority. The joint fixture must use the actual writers.
type runnerFactsStore struct {
	*metadataStore
	request          tc.ExecutionToolFactsRequest
	currentVersion   f.Version
	allowed          []id.ToolID
	currentRevision  int64
	historyRevision  int64
	historyKind      string
	currentOperation string
	historyCount     int64
	capturedVersion  int64
	capturedBinding  string
	capturedDigest   string
}

func (s *runnerFactsStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.metadataStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *runnerFactsStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	panic("facts read must not begin a transaction")
}
func (s *runnerFactsStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("facts read must not acquire locks")
}
func (s *runnerFactsStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	s.queries++
	r := s.request
	v := r.Tools[0]
	b := v.BindingSnapshot
	switch {
	case strings.Contains(sql, "FROM agenteam_tool.agent_configurations h"):
		if args[0] != r.Capture.AgentID.String() {
			return metadataRow{err: pgx.ErrNoRows}
		}
		return captureValues(r.Capture.ProjectID.String(), int64(s.currentVersion), captureRawIDs(s.allowed))
	case strings.Contains(sql, "FROM agenteam_tool.execution_configurations"):
		if args[0] != r.Capture.ExecutionID.String() {
			return metadataRow{err: pgx.ErrNoRows}
		}
		return captureValues(r.Capture.ProjectID.String(), r.Capture.AgentID.String(), s.capturedVersion, s.capturedBinding, s.capturedDigest, int64(len(r.Tools)))
	case strings.HasPrefix(sql, "SELECT count(*)"):
		return captureValues(s.historyCount)
	case strings.Contains(sql, "FROM agenteam_tool.execution_references"):
		return captureValues(s.historyRevision, v.ModelVisibleName, string(b.ScopeResolverID), string(b.RiskClassifierID), string(b.Class), s.historyKind, "read-file", "1", int64(1), true)
	case strings.Contains(sql, "JOIN agenteam_tool.registrations"):
		raw, err := tc.CanonicalDefinition(ctx, tc.ReadFileDefinition())
		if err != nil {
			return metadataRow{err: err}
		}
		return captureValues(tc.RunnerReadFileKey, s.currentRevision, raw, "", int64(0), b.ScopeResolverID, b.RiskClassifierID, b.Class)
	case strings.Contains(sql, "SELECT binding_kind,operation_name"):
		return captureValues("runner", s.currentOperation, "1", int64(1))
	default:
		panic("unexpected Runner fact read")
	}
}

func runnerFactsFixture(t *testing.T) (*Registry, *runnerFactsStore) {
	t.Helper()
	tool := captureID[id.Tool](t, 11)
	v := tc.ReadFileRegistration()
	r := tc.ExecutionToolFactsRequest{Capture: tc.ExecutionToolCaptureRequest{ProjectID: captureID[id.Project](t, 1), AgentID: captureID[id.Agent](t, 2), ExecutionID: captureID[id.Execution](t, 3)}, AgentVersion: 4, AllowedToolIDs: []id.ToolID{tool}, Tools: []tc.ExecutionTool{{ToolID: tool, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{RunnerBinding: v.Binding, ScopeResolverID: v.ScopeResolverID, RiskClassifierID: v.RiskClassifierID, Class: v.Class}}}}
	r.CaptureAgentVersion = 1
	r.AttemptBinding, _ = f.ParseDigest("sha256:" + strings.Repeat("a", 64))
	r.Selection = tc.ToolSnapshotSelection{AllowedToolIDs: []id.ToolID{tool}, DeniedToolIDs: []id.ToolID{}, ScopeConstraints: []json.RawMessage{}}
	// Seed the original head with the actual capture writer, then exercise the
	// later read independently. This is controlled SQL, not a claimed PG run.
	history := &metadataStore{tx: f.NewTx()}
	if err := writeExecutionToolReferences(context.Background(), history, r.Capture, tc.ExecutionToolCaptureFacts{AgentVersion: r.CaptureAgentVersion, AttemptBinding: r.AttemptBinding, Selection: r.Selection}, r.Tools); err != nil {
		t.Fatal(err)
	}
	if len(history.args) != 2 {
		t.Fatal("original capture writer did not produce the head and reference")
	}
	head := history.args[0]
	s := &runnerFactsStore{metadataStore: &metadataStore{tx: f.NewTx()}, request: r, currentVersion: 4, allowed: append([]id.ToolID{}, r.AllowedToolIDs...), currentRevision: 1, historyRevision: 1, historyKind: "runner", currentOperation: "read-file", historyCount: 1}
	s.capturedVersion, s.capturedBinding, s.capturedDigest = head[3].(int64), head[4].(string), head[5].(string)
	s.held = r.RequiredLocks()
	source, err := NewReadFileMetadataSource(s)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := New(s, Options{RunnerSources: map[string]RunnerSource{tc.RunnerReadFileKey: source}})
	if err != nil {
		t.Fatal(err)
	}
	return registry, s
}

func TestToolRunnerCurrentAndHistoricalFactsUseOriginalTx(t *testing.T) {
	r, s := runnerFactsFixture(t)
	if err := r.RequireExecutionToolFactsInTx(context.Background(), s.tx, s.request); err != nil {
		t.Fatal(err)
	}
	if len(s.writes) != 0 || s.queries != 6 {
		t.Fatal("facts check wrote or skipped current/history reads")
	}
	for _, mutate := range []func(*runnerFactsStore){
		func(s *runnerFactsStore) { s.held = nil },
		func(s *runnerFactsStore) { s.currentVersion++ },
		func(s *runnerFactsStore) { s.allowed = []id.ToolID{} },
		func(s *runnerFactsStore) { s.currentRevision++ },
		func(s *runnerFactsStore) { s.historyRevision++ },
		func(s *runnerFactsStore) { s.historyKind = "builtin" },
		func(s *runnerFactsStore) { s.currentOperation = "command" },
		func(s *runnerFactsStore) { s.historyCount++ },
		func(s *runnerFactsStore) { s.capturedVersion++ },
		func(s *runnerFactsStore) { s.capturedBinding = "sha256:" + strings.Repeat("b", 64) },
		func(s *runnerFactsStore) { s.capturedDigest = "sha256:" + strings.Repeat("c", 64) },
	} {
		r, s := runnerFactsFixture(t)
		mutate(s)
		if err := r.RequireExecutionToolFactsInTx(context.Background(), s.tx, s.request); err == nil || len(s.writes) != 0 {
			t.Fatal("changed current/history facts accepted")
		}
	}
	r, s = runnerFactsFixture(t)
	if err := r.RequireExecutionToolFactsInTx(context.Background(), f.NewTx(), s.request); err == nil || s.queries != 0 {
		t.Fatal("foreign Tx reached facts")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.RequireExecutionToolFactsInTx(ctx, s.tx, s.request); !errors.Is(err, context.Canceled) || s.queries != 0 {
		t.Fatal("cancelled fact read advanced")
	}
	request := s.request.Clone()
	request.Capture.ExecutionID = captureID[id.Execution](t, 80)
	s.held = request.RequiredLocks()
	if err := r.RequireExecutionToolFactsInTx(context.Background(), s.tx, request); err == nil {
		t.Fatal("foreign Execution obtained history")
	}
}

func TestToolRunnerFactsBindOriginalSelectionAndAttempt(t *testing.T) {
	for _, mutate := range []func(*tc.ExecutionToolFactsRequest){
		func(r *tc.ExecutionToolFactsRequest) { r.CaptureAgentVersion++ },
		func(r *tc.ExecutionToolFactsRequest) {
			r.AttemptBinding, _ = f.ParseDigest("sha256:" + strings.Repeat("b", 64))
		},
		func(r *tc.ExecutionToolFactsRequest) { r.Selection.AllowedToolIDs = []id.ToolID{} },
		func(r *tc.ExecutionToolFactsRequest) { r.Selection.DeniedToolIDs = []id.ToolID{r.Tools[0].ToolID} },
	} {
		r, s := runnerFactsFixture(t)
		request := s.request.Clone()
		mutate(&request)
		if request.Validate() != nil {
			t.Fatal("counterexample must remain structurally valid")
		}
		if err := r.RequireExecutionToolFactsInTx(context.Background(), s.tx, request); err == nil || s.queries != 2 || len(s.writes) != 0 {
			t.Fatal("different original capture advanced beyond the head")
		}
	}
	// Current configuration may advance while its original captured facts stay
	// fixed. Do not compare the historical version with the current Agent head.
	r, s := runnerFactsFixture(t)
	request := s.request.Clone()
	request.AgentVersion++
	request.AllowedToolIDs = append(request.AllowedToolIDs, captureID[id.Tool](t, 12))
	s.currentVersion = request.AgentVersion
	s.allowed = append([]id.ToolID{}, request.AllowedToolIDs...)
	if err := r.RequireExecutionToolFactsInTx(context.Background(), s.tx, request); err != nil {
		t.Fatal("current configuration change replaced or invalidated original capture")
	}
}
