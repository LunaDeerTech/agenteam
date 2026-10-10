//go:build integration

package objects_test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This observer records SQL actually issued by the Service. It delegates the
// original Store/Tx and never returns a fabricated row or authorization. Plans
// are measured separately, after the business call has actually returned; their
// execution time must not be hidden inside (or extend) that call's 2s budget.
type metadataPlanStore struct {
	*postgres.Store
	mu      sync.Mutex
	queries map[string]metadataPlanQuery
}

type metadataPlanQuery struct {
	sql  string
	args []any
}

type metadataPlanExecutor struct {
	postgres.SQLExecutor
	owner *metadataPlanStore
}

func metadataPlanName(query string) string {
	q := strings.Join(strings.Fields(query), " ")
	switch {
	case strings.HasPrefix(q, "SELECT id::text FROM ( (SELECT id FROM agenteam_object.upload_attempts"):
		return "gate-two-pending-sets"
	case strings.HasPrefix(q, "SELECT r.id::text FROM agenteam_object.project_work r WHERE r.project_id=$1"):
		return "stop-work"
	case strings.HasPrefix(q, "SELECT r.object_id::text FROM agenteam_object.uploads r WHERE r.project_id=$1"):
		return "stop-reserved"
	case strings.HasPrefix(q, "SELECT r.id::text FROM agenteam_object.objects o JOIN agenteam_object.object_leases r"):
		return "stop-leases"
	case strings.HasPrefix(q, "SELECT r.id::text FROM agenteam_download.grants r WHERE r.project_id=$1"):
		return "stop-grants"
	case strings.HasPrefix(q, "SELECT id::text FROM ( (SELECT r.id FROM agenteam_object.object_transfers"):
		return "stop-transfers"
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.project_id=$1"):
		return "stop-full-pending"
	case strings.HasPrefix(q, "SELECT t.id::text FROM agenteam_object.object_transfers t WHERE t.object_id=$1 ORDER BY t.id LIMIT 32"):
		return "metadata-transfers"
	case strings.HasPrefix(q, "SELECT l.id::text FROM agenteam_object.object_leases l WHERE l.object_id=$1 ORDER BY l.id LIMIT 32"):
		return "metadata-leases"
	case strings.HasPrefix(q, "SELECT w.id::text FROM agenteam_object.project_work w WHERE w.object_id=$1 ORDER BY w.id LIMIT 32"):
		return "metadata-work"
	case strings.HasPrefix(q, "SELECT a.id::text FROM agenteam_object.upload_attempts a WHERE a.object_id=$1 AND a.id<>$2 ORDER BY a.id LIMIT 16"):
		return "metadata-old-attempts"
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1)"):
		if strings.Contains(q, "w.id=ANY($2::uuid[])") {
			return "physical-full-pending"
		}
		if strings.Contains(q, "retirement_evidence IS NULL") {
			return "metadata-full-pending"
		}
	}
	return ""
}

func (s *metadataPlanStore) record(query string, args []any) {
	name := metadataPlanName(query)
	if name == "" {
		return
	}
	copyArgs := append([]any(nil), args...)
	for i, arg := range copyArgs {
		if ids, ok := arg.([]string); ok {
			copyArgs[i] = append([]string{}, ids...)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queries == nil {
		s.queries = make(map[string]metadataPlanQuery)
	}
	// Preserve the first invocation, including a nil initial cursor. Retaining
	// only the final empty page could hide a costly first-page scan.
	if _, exists := s.queries[name]; !exists {
		s.queries[name] = metadataPlanQuery{query, copyArgs}
	}
}

func (s *metadataPlanStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	e, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return &metadataPlanExecutor{e, s}, nil
}

func (s *metadataPlanStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	s.record(query, args)
	return s.Store.Query(ctx, query, args...)
}

func (s *metadataPlanStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	s.record(query, args)
	return s.Store.QueryRow(ctx, query, args...)
}

func (e *metadataPlanExecutor) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	e.owner.record(query, args)
	return e.SQLExecutor.Query(ctx, query, args...)
}

func (e *metadataPlanExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	e.owner.record(query, args)
	return e.SQLExecutor.QueryRow(ctx, query, args...)
}

func (s *metadataPlanStore) explain(t *testing.T, stage string, names ...string) {
	t.Helper()
	s.mu.Lock()
	queries := make(map[string]metadataPlanQuery, len(s.queries))
	for name, q := range s.queries {
		queries[name] = q
	}
	s.mu.Unlock()
	if len(names) == 0 {
		for name := range queries {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		t.Fatal("no actual bounded SQL observed")
	}
	for _, name := range names {
		query, found := queries[name]
		if !found {
			t.Fatal("required actual SQL was never reached", name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var raw []byte
		err := s.Store.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.sql, query.args...).Scan(&raw)
		cancel()
		if err != nil || !json.Valid(raw) {
			t.Fatal("actual bounded query plan", stage, name, err)
		}
		// Plans are evidence for review, not an assertion that LIMIT bounds the
		// scan. Index pruning and missing-index decisions require these actual
		// plans plus the separate large-other-Project/FK-trigger matrix.
		t.Logf("D05 actual EXPLAIN stage=%s query=%s plan=%s", stage, name, raw)
	}
}

func TestObjectMetadataCleanupPlanCapture(t *testing.T) {
	s := &metadataPlanStore{}
	query := "SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1) OR EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.id=ANY($2::uuid[]))"
	workers := []string{"original-worker"}
	args := []any{"original-object", workers}
	s.record(query, args)
	args[0], workers[0] = "changed-object", "changed-worker"
	s.record(query, []any{"later-page-object", []string{"later-worker"}})
	s.record("DELETE FROM agenteam_object.objects WHERE id=$1", []any{"not-a-plan-read"})
	s.record("SELECT unrelated_private_material", nil)
	if len(s.queries) != 1 {
		t.Fatal("unselected or mutating query entered the read-plan set")
	}
	got := s.queries["physical-full-pending"]
	if got.sql != query || got.args[0] != "original-object" || got.args[1].([]string)[0] != "original-worker" {
		t.Fatal("query capture lost the original immutable first invocation")
	}
	s = &metadataPlanStore{}
	s.record(query, []any{"original-object", []string{}})
	if s.queries["physical-full-pending"].args[1].([]string) == nil {
		t.Fatal("empty UUID exclusion array became SQL NULL")
	}
	firstPage := "SELECT r.id::text FROM agenteam_object.project_work r WHERE r.project_id=$1 AND ($2::uuid IS NULL OR r.id>$2)"
	s.record(firstPage, []any{"original-project", nil, true})
	s.record(firstPage, []any{"original-project", "late-cursor", true})
	if s.queries["stop-work"].args[1] != nil {
		t.Fatal("empty final page replaced the real first-page plan input")
	}
}
