//go:build integration

package memory

import (
	"context"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
	rr "github.com/LunaDeerTech/agenteam/internal/central/retrieval/runtime"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// These collaborators isolate the Memory database/retrieval contract. They do
// not constitute acceptance of Tool, Model, Audit or a real Agent execution.
type pgMemoryTools struct{}

func (pgMemoryTools) DiscoverMemoryTool(context.Context, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, string) ([]f.LockRequest, error) {
	return []f.LockRequest{}, nil
}
func (pgMemoryTools) RequireMemoryToolInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, string) error {
	return nil
}
func (pgMemoryTools) RequireAgentRunBindingInTx(context.Context, f.Tx, any) error { return nil }

type pgMemoryAudit struct{ events []AuditEvent }

func (a *pgMemoryAudit) AppendMemoryInTx(_ context.Context, _ f.Tx, event AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

type pgMemoryEmbedding struct{}

func (pgMemoryEmbedding) EmbedQuery(context.Context, rc.Request) ([]float64, error) {
	return []float64{1, 0}, nil
}

func TestMemoryPostgresCurrentRevisionRecall(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	migrator, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if state := migrator.Migrate(ctx); !state.Migrated {
		t.Fatal("Memory schema migration", state.Fault)
	}
	store, err := postgres.Open(ctx, db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	audit := &pgMemoryAudit{}
	s := &Service{store: store, deps: Dependencies{Tools: pgMemoryTools{}, Audit: audit, Gate: SensitiveDetector{}}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	ns := c.Namespace{ProjectID: testID[i.Project](t), AgentID: testID[i.Agent](t)}
	actor, err := i.NewAgentRun(ns.ProjectID, ns.AgentID, testID[i.Execution](t))
	if err != nil {
		t.Fatal(err)
	}
	run, entry, err := s.begin(ctx, actor, ns, testID[tc.Operation](t), "retain")
	if err != nil {
		t.Fatal(err)
	}
	defer s.end(entry)
	content := c.Content{Type: c.Procedure, Text: "Run schema generator before migration", Tags: []string{"migration"}, Importance: c.High}
	proposal := c.Proposal{Actions: []c.Action{{Kind: c.Add, Content: content, Reason: "durable migration procedure"}}}
	if _, _, err := s.prepareRetain(run, entry, digest([]byte("first retain")), "allow", nil); err != nil {
		t.Fatal("prepare", err)
	}
	if err := s.saveProposal(run, entry, 1, proposal, nil, testID[mc.Call](t)); err != nil {
		t.Fatal("proposal", err)
	}
	result, err := s.applyProposal(run, entry, proposal, nil, 1)
	if err != nil {
		t.Fatal("apply", err)
	}
	if len(result.Added) != 1 || len(audit.events) != 1 {
		t.Fatal("canonical or audit not atomic")
	}
	source, err := s.captureGeneration(run, entry)
	if err != nil {
		t.Fatal("capture", err)
	}
	serving, err := pgMemoryProjectGeneration(s, run, entry, source, testID[mc.Snapshot](t), 2, []generationVector{{Target: result.Added[0], Vector: []float64{1, 0}}})
	if err != nil {
		t.Fatal("publish", err)
	}
	provider, err := NewRetrievalProvider(s, lexical.NativePostgres)
	if err != nil {
		t.Fatal(err)
	}
	search, err := rr.New(provider, pgMemoryEmbedding{}, rr.Timeouts{Lexical: time.Second, Embedding: time.Second, Dense: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	next, err := i.NewAgentRun(ns.ProjectID, ns.AgentID, testID[i.Execution](t))
	if err != nil {
		t.Fatal(err)
	}
	recalled, err := pgMemoryRecall(s, search, ctx, next, ns, testID[tc.Operation](t), c.RecallInput{Query: "schema migration"})
	if err != nil {
		t.Fatal("cross execution recall", err)
	}
	if len(recalled.Memories) != 1 || recalled.Memories[0].MemoryID != result.Added[0].MemoryID || recalled.Memories[0].Provenance.ExecutionID.String() != actor.Details().ExecutionID || recalled.Mode != "hybrid" {
		t.Fatal("recall identity/provenance/mode")
	}
	foreign := ns
	foreign.AgentID = testID[i.Agent](t)
	_, err = pgMemoryRecall(s, search, ctx, next, foreign, testID[tc.Operation](t), c.RecallInput{Query: "schema migration"})
	requireFault(t, err, f.Forbidden)
	old := recalled.Memories[0]
	updateCtx, updateEntry, err := s.begin(ctx, next, ns, testID[tc.Operation](t), "retain")
	if err != nil {
		t.Fatal(err)
	}
	defer s.end(updateEntry)
	proposal = c.Proposal{Actions: []c.Action{{Kind: c.Update, TargetID: old.MemoryID.String(), ExpectedRevision: "1", Content: c.Content{Type: c.Procedure, Text: "Validate schema before migration", Tags: []string{"migration"}, Importance: c.High}, Reason: "correct procedure"}}}
	if _, _, err := s.prepareRetain(updateCtx, updateEntry, digest([]byte("update")), "allow", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.saveProposal(updateCtx, updateEntry, 1, proposal, []c.Revision{old}, testID[mc.Call](t)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.applyProposal(updateCtx, updateEntry, proposal, []c.Revision{old}, 1); err != nil {
		t.Fatal("update", err)
	}
	recalled, err = pgMemoryRecall(s, search, ctx, next, ns, testID[tc.Operation](t), c.RecallInput{Query: "schema migration"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled.Memories) != 0 {
		t.Fatal("superseded projection escaped current revision filter")
	}
	// Publish new current, then delete without physically removing projection.
	source, err = s.captureGeneration(run, entry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pgMemoryProjectGeneration(s, run, entry, source, serving.SnapshotID, 2, []generationVector{{Target: c.Target{MemoryID: old.MemoryID, Revision: 2}, Vector: []float64{1, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	cause, err := readCause("fixture-owner-delete")
	if err != nil {
		t.Fatal(err)
	}
	err = s.transaction(run, entry, true, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return deleteCurrent(ctx, x, ns, old.MemoryID, 2, now())
	})
	if err != nil {
		t.Fatal("delete", err)
	}
	recalled, err = pgMemoryRecall(s, search, ctx, next, ns, testID[tc.Operation](t), c.RecallInput{Query: "schema migration"})
	if err != nil || len(recalled.Memories) != 0 {
		t.Fatal("deleted revision recalled", err)
	}
	var projections int
	if err := store.QueryRow(ctx, `SELECT count(*) FROM agenteam_memory.projections WHERE memory_id=$1`, old.MemoryID.String()).Scan(&projections); err != nil || projections == 0 {
		t.Fatal("test must retain stale physical projection", err)
	}
}

// Exercise the real authorized SQL legs, shared RRF and current-revision
// projection directly. Profile maintenance and Model ownership require the
// installed App and are intentionally outside this synthetic-vector fixture.
func pgMemoryRecall(s *Service, search rc.Searcher, ctx context.Context, actor i.Actor, namespace c.Namespace, operation tc.OperationID, input c.RecallInput) (c.RecallResult, error) {
	run, entry, err := s.begin(ctx, actor, namespace, operation, "recall")
	if err != nil {
		return c.RecallResult{}, err
	}
	defer s.end(entry)
	serving, exists, err := s.serving(run, entry)
	if err != nil {
		return c.RecallResult{}, err
	}
	if !exists {
		return c.RecallResult{Memories: []c.Revision{}, Mode: "empty"}, nil
	}
	agent := namespace.AgentID
	result, err := search.Search(run, rc.Request{Query: input.Query, Scope: rc.Scope{Kind: rc.Memory, ProjectID: namespace.ProjectID, AgentID: &agent}, Serving: serving, Limits: rc.Limits{LexicalK: 8, DenseK: 8, FusedK: 8, ResultK: 8, RRFConstant: 60, MaxResultBytes: 16384}})
	if err != nil {
		return c.RecallResult{}, err
	}
	return s.projectRecall(run, entry, serving, result)
}

// This retrieval fixture supplies a synthetic embedding projection. It does
// not claim a real Model/Secret reference; that boundary is tested with App.
func pgMemoryProjectGeneration(s *Service, ctx context.Context, entry *call, source generationSource, snapshot mc.SnapshotID, dimension int, vectors []generationVector) (rc.ServingIdentity, error) {
	generation, err := f.NewID[struct{}]()
	if err != nil {
		return rc.ServingIdentity{}, err
	}
	call, err := f.NewID[mc.Call]()
	if err != nil {
		return rc.ServingIdentity{}, err
	}
	process, err := f.NewID[oc.Process]()
	if err != nil {
		return rc.ServingIdentity{}, err
	}
	serving := rc.ServingIdentity{GenerationID: generation.String(), SnapshotID: snapshot, Dimension: dimension}
	cause, err := readCause("retrieval-fixture-projection")
	if err != nil {
		return serving, err
	}
	err = s.transaction(ctx, entry, true, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		n := entry.namespace
		_, err := x.Exec(ctx, `INSERT INTO agenteam_memory.model_calls(call_id,operation_id,project_id,agent_id,execution_id,purpose,input_digest,snapshot_id,retired,created_at,process_id) VALUES($1,$2,$3,$4,$5,'memory_embedding',$6,$7,true,$8,$9)`, call.String(), entry.operation.String(), n.ProjectID.String(), n.AgentID.String(), entry.actor.Details().ExecutionID, string(digest([]byte("fixture"))), snapshot.String(), now().Time(), process.String())
		if err != nil {
			return portError(err)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_memory.generations SET status='retired',owner_version=2,retention_state='released' WHERE project_id=$1 AND agent_id=$2 AND status='ready'`, n.ProjectID.String(), n.AgentID.String()); err != nil {
			return portError(err)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_memory.generations(generation_id,project_id,agent_id,snapshot_id,dimension,schema_version,namespace_version,status,created_at,owner_version,retention_state,source_call_id,process_id) VALUES($1,$2,$3,$4,$5,1,$6,'ready',$7,1,'retained',$8,$9)`, serving.GenerationID, n.ProjectID.String(), n.AgentID.String(), snapshot.String(), dimension, int64(source.Version), now().Time(), call.String(), process.String())
		if err != nil {
			return portError(err)
		}
		for index, record := range source.Records {
			vector, err := vectorText(vectors[index].Vector, dimension)
			if err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_memory.projections(generation_id,project_id,agent_id,memory_id,revision,embedding,index_text) VALUES($1,$2,$3,$4,$5,$6::vector,$7)`, serving.GenerationID, n.ProjectID.String(), n.AgentID.String(), record.Current.MemoryID.String(), int64(record.Current.Revision), vector, record.Current.Text); err != nil {
				return portError(err)
			}
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_memory.namespaces SET serving_generation_id=$3 WHERE project_id=$1 AND agent_id=$2`, n.ProjectID.String(), n.AgentID.String(), serving.GenerationID)
		return portError(err)
	})
	return serving, err
}
