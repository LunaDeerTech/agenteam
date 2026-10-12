package registry

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestToolWriteFileMetadataRequiresExplicitBoundSource(t *testing.T) {
	ctx := context.Background()
	s := &metadataStore{tx: f.NewTx(), held: []f.LockRequest{RegistryLock(f.Exclusive)}}
	write, err := NewWriteFileMetadataSource(s)
	if err != nil {
		t.Fatal("explicit write source rejected")
	}
	read, err := NewReadFileMetadataSource(s)
	if err != nil {
		t.Fatal("read source rejected")
	}
	if read.CheckBindingInTx(ctx, s.tx, tc.WriteFileRegistration()) == nil || write.CheckBindingInTx(ctx, s.tx, tc.ReadFileRegistration()) == nil {
		t.Fatal("read/write source bindings interchangeable")
	}
	reg, err := New(s, Options{RunnerSources: map[string]RunnerSource{tc.RunnerWriteFileKey: write}})
	if err != nil {
		t.Fatal("explicit write registry rejected")
	}
	for _, tx := range []f.Tx{f.Tx{}, f.NewTx()} {
		if _, err := reg.ReconcileRunnerInTx(ctx, tx, tc.RunnerWriteFileKey); err == nil || s.queries != 0 || len(s.writes) != 0 {
			t.Fatal("foreign Tx reached metadata")
		}
	}
	s.held = []f.LockRequest{RegistryLock(f.Shared)}
	if _, err := reg.ReconcileRunnerInTx(ctx, s.tx, tc.RunnerWriteFileKey); err == nil || s.queries != 0 {
		t.Fatal("write lacked registry EX")
	}
	s.held = []f.LockRequest{RegistryLock(f.Exclusive)}
	ref, err := reg.ReconcileRunnerInTx(ctx, s.tx, tc.RunnerWriteFileKey)
	if err != nil || ref.Validate() != nil || len(s.writes) != 3 {
		t.Fatal("explicit metadata not persisted")
	}
	if len(s.args[2]) != 8 || s.args[2][5] != "write-file" || s.args[2][6] != "1" || s.args[2][7] != 1 || !strings.Contains(s.writes[2], "NULL,NULL") {
		t.Fatal("write used read or Builtin registration")
	}
	raw, err := tc.CanonicalDefinition(ctx, tc.WriteFileDefinition())
	if err != nil {
		t.Fatal("fixed definition invalid")
	}
	s.row = metadataRow{tool: ref.ToolID.String(), key: tc.RunnerWriteFileKey, revision: 1, definition: raw}
	s.writes, s.args = nil, nil
	again, err := reg.ReconcileRunnerInTx(ctx, s.tx, tc.RunnerWriteFileKey)
	if err != nil || again != ref || len(s.writes) != 1 {
		t.Fatal("same definition changed identity/revision")
	}
	for name, options := range map[string]Options{
		"default":                     {},
		"read-source-under-write-key": {RunnerSources: map[string]RunnerSource{tc.RunnerWriteFileKey: read}},
	} {
		t.Run(name, func(t *testing.T) {
			other, err := New(s, options)
			if err != nil {
				t.Fatal("registry constructor unexpectedly failed")
			}
			s.writes = nil
			if _, err := other.ReconcileRunnerInTx(ctx, s.tx, tc.RunnerWriteFileKey); err == nil || len(s.writes) != 0 {
				t.Fatal("missing/wrong source granted write metadata")
			}
		})
	}
	wrong, err := New(s, Options{RunnerSources: map[string]RunnerSource{tc.RunnerReadFileKey: write}})
	if err != nil {
		t.Fatal("fixture registry failed")
	}
	if _, err = wrong.ReconcileRunnerInTx(ctx, s.tx, tc.RunnerReadFileKey); err == nil {
		t.Fatal("write source substituted read identity")
	}
}

// This controlled SQL reuses the real capture writer and readers. It proves
// algorithm/transaction boundaries, not a real PostgreSQL or Execution grant.
type writeFactsStore struct {
	*runnerFactsStore
	historyOperation string
}

func (s *writeFactsStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.metadataStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *writeFactsStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	b := s.request.Tools[0].BindingSnapshot
	switch {
	case strings.Contains(sql, "FROM agenteam_tool.execution_references") && !strings.HasPrefix(sql, "SELECT count(*)"):
		s.queries++
		return captureValues(s.historyRevision, s.request.Tools[0].ModelVisibleName, string(b.ScopeResolverID), string(b.RiskClassifierID), string(b.Class), s.historyKind, s.historyOperation, "1", int64(1), true)
	case strings.Contains(sql, "JOIN agenteam_tool.registrations"):
		s.queries++
		raw, err := tc.CanonicalDefinition(ctx, tc.WriteFileDefinition())
		if err != nil {
			return metadataRow{err: err}
		}
		return captureValues(tc.RunnerWriteFileKey, s.currentRevision, raw, "", int64(0), b.ScopeResolverID, b.RiskClassifierID, b.Class)
	default:
		return s.runnerFactsStore.QueryRow(ctx, sql, args...)
	}
}

func writeFactsFixture(t *testing.T) (*Registry, *writeFactsStore) {
	t.Helper()
	_, base := runnerFactsFixture(t)
	v := tc.WriteFileRegistration()
	base.request.Tools[0].BindingSnapshot = tc.BuiltinExecutionBinding{RunnerBinding: v.Binding, ScopeResolverID: v.ScopeResolverID, RiskClassifierID: v.RiskClassifierID, Class: v.Class}
	history := &metadataStore{tx: f.NewTx()}
	r := base.request
	if err := writeExecutionToolReferences(context.Background(), history, r.Capture, tc.ExecutionToolCaptureFacts{AgentVersion: r.CaptureAgentVersion, AttemptBinding: r.AttemptBinding, Selection: r.Selection}, r.Tools); err != nil || len(history.args) != 2 {
		t.Fatal("actual write capture writer rejected")
	}
	base.capturedDigest = history.args[0][5].(string)
	base.currentOperation = "write-file"
	s := &writeFactsStore{runnerFactsStore: base, historyOperation: "write-file"}
	source, err := NewWriteFileMetadataSource(s)
	if err != nil {
		t.Fatal("write source rejected")
	}
	reg, err := New(s, Options{RunnerSources: map[string]RunnerSource{tc.RunnerWriteFileKey: source}})
	if err != nil {
		t.Fatal("write registry rejected")
	}
	return reg, s
}

func TestToolWriteFileCurrentAndHistoricalBindingsRejectReadMixing(t *testing.T) {
	ctx := context.Background()
	r, s := writeFactsFixture(t)
	if err := r.RequireExecutionToolFactsInTx(ctx, s.tx, s.request); err != nil || len(s.writes) != 0 || s.queries != 6 {
		t.Fatal("write current/history not checked completely")
	}
	for name, mutate := range map[string]func(*writeFactsStore){
		"missing-locks":    func(s *writeFactsStore) { s.held = nil },
		"current-read":     func(s *writeFactsStore) { s.currentOperation = "read-file" },
		"historical-read":  func(s *writeFactsStore) { s.historyOperation = "read-file" },
		"history-revision": func(s *writeFactsStore) { s.historyRevision++ },
		"current-version":  func(s *writeFactsStore) { s.currentVersion++ },
		"original-digest":  func(s *writeFactsStore) { s.capturedDigest = "sha256:" + strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			r, s := writeFactsFixture(t)
			mutate(s)
			if err := r.RequireExecutionToolFactsInTx(ctx, s.tx, s.request); err == nil || len(s.writes) != 0 {
				t.Fatal("mixed or changed write capture accepted")
			}
		})
	}
	r, s = writeFactsFixture(t)
	if err := r.RequireExecutionToolFactsInTx(ctx, f.NewTx(), s.request); err == nil || s.queries != 0 {
		t.Fatal("foreign transaction reached write facts")
	}
	w := tc.WriteFileRegistration()
	if err := r.RequireCurrentRunnerInTx(ctx, s.tx, tc.SpecRef{ToolID: s.request.Tools[0].ToolID, SpecRevision: s.request.Tools[0].SpecRevision}, w.Binding, w.ScopeResolverID, w.RiskClassifierID); err != nil {
		t.Fatal("current write registration rejected")
	}
	read := tc.ReadFileRegistration()
	if err := r.RequireCurrentRunnerInTx(ctx, s.tx, tc.SpecRef{ToolID: s.request.Tools[0].ToolID, SpecRevision: s.request.Tools[0].SpecRevision}, read.Binding, read.ScopeResolverID, read.RiskClassifierID); err == nil {
		t.Fatal("current write tool satisfied a read binding")
	}
}
