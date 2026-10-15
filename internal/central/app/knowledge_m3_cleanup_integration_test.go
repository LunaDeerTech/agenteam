//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The original fixed Knowledge cleanup driver is the only active participant.
// Other domains' Stop prerequisites remain controlled by the existing fixture;
// the default nil Registry and whole-Project deletion are not claimed here.
func TestKnowledgeM3AppProjectCleanup(t *testing.T) {
	if !t.Run("populated-schema-upgrade", knowledgeM3CleanupUpgrade) {
		return
	}
	network, err := netfixture.Load()
	workAppCheck(t, "knowledge-cleanup-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "knowledge-cleanup-address", err)
	var transport *outboundRuntime
	trace := &knowledgeM3CaptureTrace{}
	defer func() {
		if t.Failed() {
			trace.diagnose(t)
		}
	}()
	v := newKnowledgeLifecycleApp(t, func(d *dependencies, v *executionSchedulerApp) {
		v.root.cfg = knowledgeM3RetentionConfiguration(t, guardEnvironment(t, v.root.db), network.CAFile)
		originalObserve := m1CommitObserver(t)
		v.store.observeCommit = func(cause f.TransactionCause, result f.CommitResult) {
			originalObserve(cause, result)
			if strings.Contains(cause.Details().Owner, "cleanup") {
				trace.observe(cause, result)
			}
		}
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver, transport = appFixtureResolver{address}, actual
			return nil
		}
	})
	v.tracked.mu.Lock()
	v.tracked.writes = knowledgeM3CleanupWrites
	v.tracked.mu.Unlock()
	if transport == nil || v.knowledgeCalls.index == nil {
		t.Fatal("original outbound/index owners missing")
	}
	m1AllowOwnedPeer(t, v.executionSchedulerApp, transport, network)
	wire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "knowledge-retention", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &wire})
	workAppCheck(t, "knowledge-cleanup-peer", err)
	_, credential := knowledgeM3RetentionModels(t, v.executionSchedulerApp, scenario)
	profiles := make([]string, 0, 2)
	for _, project := range []id.ProjectID{v.a, v.b} {
		const body = "Mercury completes its orbit in 88 Earth days."
		source, err := kc.NewTextSource(kc.PlainText, body)
		workAppCheck(t, "knowledge-cleanup-source", err)
		doc, err := v.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: project, DocumentID: guardID[kc.Document](t), Title: "Cleanup retained source"}, source)
		workAppCheck(t, "knowledge-cleanup-create", err)
		// Borrow only the test helper's project view; the original pair, Store,
		// ProcessGuard and App are shared, with no new runtime or authority.
		view := *v.executionSchedulerApp
		base := *v.workAppFixture
		base.a = project
		view.workAppFixture = &base
		profile := knowledgeM3AwaitAutomatic(t, &view, doc, "", body)
		knowledgeM3RetentionFacts(t, &view, profile, "active", "retained", credential)
		profiles = append(profiles, profile)
	}
	knowledgeM3CleanupConstraint(t, v.ctx, v.admin, true)
	knowledgeM3CleanupRejectOrphan(t, v, profiles[0])
	otherCalls := knowledgeM3CleanupClosedCalls(t, v, v.b)
	other := knowledgeM3CleanupImage(t, v, v.b, otherCalls)
	modelFacts := knowledgeM3CleanupModelImage(t, v)
	v.cleanupEntry(t)
	// The original reference release commits, but the caller receives Unknown.
	// Its next original round may only observe that exact plan and domain pair.
	knowledgeM3CleanupUnknown(t, v, func(ctx context.Context, x postgres.SQLExecutor) (bool, error) {
		var released bool
		err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2 AND retention_state='released')`, v.a.String(), profiles[0]).Scan(&released)
		return released, err
	})
	knowledgeM3RetentionFacts(t, v.executionSchedulerApp, profiles[0], "retired", "released", credential)
	// Keep the profile and source-operation preimages until both were actually
	// removed. An Unknown observation cannot delete the next canonical row.
	knowledgeM3CleanupUnknown(t, v, func(ctx context.Context, x postgres.SQLExecutor) (bool, error) {
		var absent bool
		err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2)`, v.a.String(), profiles[0]).Scan(&absent)
		return absent, err
	})
	complete := false
	for n := 0; n < 40 && !complete; n++ {
		err = v.cleaner.Run(v.ctx, v.a, v.cause.OperationID)
		v.cleanupFailure(t, n+1, err)
		workAppCheck(t, "knowledge-m3-later-cleanup", err)
		workAppCheck(t, "knowledge-m3-cleanup-completed", v.admin.QueryRow(v.ctx, `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='knowledge'`, v.cause.OperationID.String()).Scan(&complete))
	}
	if !complete {
		t.Fatal("bounded original Knowledge cleanup did not complete")
	}
	for _, table := range knowledgeM3CleanupTables {
		var count int
		workAppCheck(t, "knowledge-m3-empty-"+table, v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_knowledge.`+table+` WHERE project_id=$1`, v.a.String()).Scan(&count))
		if count != 0 {
			t.Fatal("Knowledge cleanup retained its own facts", table, count)
		}
	}
	if !bytes.Equal(other, knowledgeM3CleanupImage(t, v, v.b, otherCalls)) || !bytes.Equal(modelFacts, knowledgeM3CleanupModelImage(t, v)) {
		t.Fatal("cleanup changed another project or physically removed Model facts")
	}
	var preserved, released bool
	workAppCheck(t, "knowledge-m3-scope-preserved", v.admin.QueryRow(v.ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$1)
 AND NOT EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants WHERE operation_id=$2 AND participant_name<>'knowledge' AND cleanup_state='completed'),
 EXISTS(SELECT 1 FROM agenteam_model.serving_snapshot_retentions r WHERE r.project_id=$1 AND r.generation_id=$3 AND r.state='released' AND r.released_version=r.owner_version AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_references s WHERE s.owner_id=r.reference_id AND s.consumer='model'))`, v.a.String(), v.cause.OperationID.String(), profiles[0]).Scan(&preserved, &released))
	if !preserved || !released {
		t.Fatal("cleanup omitted formal pin release or crossed its participant boundary")
	}
	v.cleaner.Stop()
	workAppCheck(t, "knowledge-m3-cleanup-driver-join", v.cleaner.Drain(v.ctx))
	v.stop.Stop()
	workAppCheck(t, "knowledge-m3-stop-driver-join", v.stop.Drain(v.ctx))
	v.acceptor.Stop()
	workAppCheck(t, "knowledge-m3-acceptor-join", v.acceptor.Drain(v.ctx))
	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "knowledge-m3-cleanup-original-app")
	workAppCheck(t, "knowledge-m3-cleanup-app-return", v.root.err)
	if !v.knowledgeCalls.index.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("cleanup App returned before original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	peer, err := network.State(context.Background(), scenario)
	workAppCheck(t, "knowledge-m3-cleanup-peer-state", err)
	if len(peer.Requests) != 2 || peer.CompletedHandlers != 2 || peer.ActiveHandlers != 0 {
		t.Fatal("cleanup dispatched another Model request or did not join original calls")
	}
	t.Log("M3_KNOWLEDGE_CLEANUP independent_populated_schema_upgrade=true formal_app_schema=139 original_formal_pin_release=true original_release_unknown_readonly=true bounded_pair_unknown_readonly=true model_facts_preserved=true other_project_preserved=true original_app_join=true whole_project_delete=false")
}

var knowledgeM3CleanupTables = []string{"index_chunks", "index_generations", "model_operations", "index_jobs", "index_state", "index_profiles", "documents", "command_events", "object_cleanup", "publications", "work_claims", "commands", "project_object_retirements", "project_calls"}

func TestKnowledgeM3CleanupMigrationSource(t *testing.T) {
	_ = knowledgeM3CleanupOldSource(t)
	current, err := postgres.EmbeddedSource()
	workAppCheck(t, "knowledge-cleanup-current-source", err)
	if current.Target() != 139 {
		t.Fatal("cleanup candidate requires fixed schema 139")
	}
}

func knowledgeM3CleanupOldSource(t *testing.T) postgres.Source {
	t.Helper()
	files, err := fs.ReadDir(migrations.SQL, ".")
	workAppCheck(t, "knowledge-cleanup-compiled-migrations", err)
	old := fstest.MapFS{}
	for _, file := range files {
		prefix, _, ok := strings.Cut(file.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if !ok || err != nil {
			t.Fatal("unexpected compiled migration name")
		}
		if version <= 138 {
			raw, err := fs.ReadFile(migrations.SQL, file.Name())
			workAppCheck(t, "knowledge-cleanup-compiled-migration-read", err)
			old[file.Name()] = &fstest.MapFile{Data: raw, Mode: 0444}
		}
	}
	source, err := postgres.NewSource(old, nil)
	workAppCheck(t, "knowledge-cleanup-formal-old-source", err)
	if source.Target() != 138 {
		t.Fatal("cleanup upgrade requires exact original 138 schema")
	}
	return source
}

func knowledgeM3CleanupConstraint(t *testing.T, ctx context.Context, x *pgx.Conn, upgraded bool) {
	t.Helper()
	var deferrable, initiallyDeferred bool
	var action, target string
	workAppCheck(t, "knowledge-cleanup-exact-fk-catalog", x.QueryRow(ctx, `SELECT condeferrable,condeferred,confdeltype::text,confrelid::regclass::text FROM pg_constraint WHERE conrelid='agenteam_knowledge.index_profiles'::regclass AND conname='index_profiles_source_operation_id_fkey'`).Scan(&deferrable, &initiallyDeferred, &action, &target))
	wantAction := "r"
	if upgraded {
		wantAction = "a"
	}
	if deferrable != upgraded || initiallyDeferred || action != wantAction || target != "agenteam_knowledge.model_operations" {
		t.Fatal("profile source FK changed the wrong table or default enforcement")
	}
}

// This independent database contains persistence fixtures, not authorization
// grants or invented Model completion. It tests the migration with the actual
// cyclic FK populated. The subsequent App uses its unchanged embedded 139
// source and health check, and creates its own facts through real services.
func knowledgeM3CleanupUpgrade(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	old, err := postgres.NewMigrator(db.Config(t, nil), knowledgeM3CleanupOldSource(t))
	workAppCheck(t, "knowledge-schema-old-migrator", err)
	if state := old.Migrate(ctx); !state.Migrated || state.Version != 138 || state.Fault != nil {
		t.Fatal("formal original 138 migration failed", state.Version, state.Fault)
	}
	x := db.Connect(t)
	knowledgeM3CleanupConstraint(t, ctx, x, false)
	_, err = x.Exec(ctx, `BEGIN;
INSERT INTO agenteam_project.projects(id,owner_user_id,name,normalized_name,description,lifecycle,version,created_at,updated_at,creation_id)
VALUES('01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000002','Migration.Project','migration.project','retained','active',1,'2026-01-01 00:00:00+00','2026-01-01 00:00:00+00','01900000-0000-7000-8000-000000000003');
INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,request_name,request_description,state,initialization_key,version,created_at,updated_at,event_id)
VALUES('01900000-0000-7000-8000-000000000003','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000002','migration-create','sha256:'||repeat('1',64),'Migration.Project','retained','accepted','migration-initialize',1,'2026-01-01 00:00:00+00','2026-01-01 00:00:00+00','01900000-0000-7000-8000-000000000004');
INSERT INTO agenteam_object.store_identity(instance_id,phase)
VALUES('01900000-0000-7000-8000-000000000005','reserved');
INSERT INTO agenteam_object.process_claims(process_id,store_id,deployment_id,spool_identity,spool_device,spool_inode,claim_device,claim_inode,host_identity,boot_id,claim_nonce,state)
VALUES('01900000-0000-7000-8000-000000000006','01900000-0000-7000-8000-000000000005','01900000-0000-7000-8000-000000000007',decode(repeat('1',64),'hex'),1,2,3,4,decode(repeat('2',64),'hex'),'schema-fixture',decode(repeat('3',64),'hex'),'claimed');
INSERT INTO agenteam_model.snapshots(id,project_id,snapshot_data,snapshot_digest,provider_version,model_version,created_at)
VALUES('01900000-0000-7000-8000-000000000008','01900000-0000-7000-8000-000000000001','{"format_version":1}','sha256:'||repeat('4',64),1,1,clock_timestamp());
INSERT INTO agenteam_knowledge.index_profiles(id,project_id,version,state,parser_profile,chunker_profile,soft_chunk_bytes,hard_chunk_bytes,forced_overlap_bytes,lexical_backend,lexical_config,dimension)
VALUES('01900000-0000-7000-8000-000000000009','01900000-0000-7000-8000-000000000001',1,'building','schema-fixture','schema-fixture',128,256,0,'native_pg_fts','{}',2);
INSERT INTO agenteam_knowledge.model_operations(id,project_id,kind,profile_id,generation_id,snapshot_id,call_id,process_id,input_digest,state,deadline_at)
VALUES('01900000-0000-7000-8000-000000000010','01900000-0000-7000-8000-000000000001','profile','01900000-0000-7000-8000-000000000009','01900000-0000-7000-8000-000000000009','01900000-0000-7000-8000-000000000008','01900000-0000-7000-8000-000000000011','01900000-0000-7000-8000-000000000006','sha256:'||repeat('5',64),'active',clock_timestamp()+interval '1 minute');
UPDATE agenteam_knowledge.index_profiles SET snapshot_id='01900000-0000-7000-8000-000000000008',source_operation_id='01900000-0000-7000-8000-000000000010',retention_state='retained';
COMMIT;`)
	workAppCheck(t, "knowledge-schema-populated-cycle", err)
	image := func() []byte {
		var parts []string
		for _, table := range []string{"agenteam_project.projects", "agenteam_project.creations", "agenteam_object.store_identity", "agenteam_object.process_claims", "agenteam_model.snapshots", "agenteam_knowledge.index_profiles", "agenteam_knowledge.model_operations"} {
			parts = append(parts, `(SELECT jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text) FROM `+table+` t)`)
		}
		var raw []byte
		workAppCheck(t, "knowledge-schema-full-preimages", x.QueryRow(ctx, `SELECT jsonb_build_array(`+strings.Join(parts, ",")+`)`).Scan(&raw))
		return raw
	}
	checks := func() string {
		var raw string
		workAppCheck(t, "knowledge-schema-original-checks", x.QueryRow(ctx, `SELECT string_agg(conrelid::regclass::text||conname||pg_get_constraintdef(oid),E'\n' ORDER BY conrelid::regclass::text,conname) FROM pg_constraint WHERE conrelid IN ('agenteam_knowledge.index_profiles'::regclass,'agenteam_knowledge.model_operations'::regclass) AND conname<>'index_profiles_source_operation_id_fkey'`).Scan(&raw))
		return raw
	}
	checkRetainedSource := func() {
		_, failure := x.Exec(ctx, `UPDATE agenteam_knowledge.index_profiles SET source_operation_id=NULL`)
		var rejected *pgconn.PgError
		if !errors.As(failure, &rejected) || rejected.Code != "23514" || rejected.ConstraintName != "knowledge_profile_retention_source" {
			t.Fatal("retained profile without its source was not rejected by the original CHECK")
		}
	}
	checkRetainedSource()
	before, oldChecks := image(), checks()
	current, err := postgres.NewMigrator(db.Config(t, nil))
	workAppCheck(t, "knowledge-schema-current-migrator", err)
	if state := current.Migrate(ctx); !state.Migrated || state.Version != 139 || state.Fault != nil {
		t.Fatal("formal populated 138 to 139 migration failed", state.Version, state.Fault)
	}
	knowledgeM3CleanupConstraint(t, ctx, x, true)
	if !bytes.Equal(before, image()) || oldChecks != checks() {
		t.Fatal("constraint upgrade changed stored rows or another constraint")
	}
	checkRetainedSource()
	t.Log("M3_KNOWLEDGE_SCHEMA populated_138_to_139=true cyclic_pair_preserved=true other_constraints_preserved=true runtime_authority=false")
}

func knowledgeM3CleanupRejectOrphan(t *testing.T, v *knowledgeLifecycleApp, profile string) {
	t.Helper()
	tx, err := v.admin.Begin(v.ctx)
	workAppCheck(t, "knowledge-cleanup-negative-tx", err)
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(v.ctx, `SET CONSTRAINTS agenteam_knowledge.index_profiles_source_operation_id_fkey DEFERRED`)
	workAppCheck(t, "knowledge-cleanup-defer-one-fk", err)
	tag, err := tx.Exec(v.ctx, `DELETE FROM agenteam_knowledge.model_operations WHERE id=(SELECT source_operation_id FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2)`, v.a.String(), profile)
	workAppCheck(t, "knowledge-cleanup-negative-orphan", err)
	if tag.RowsAffected() != 1 {
		t.Fatal("negative orphan did not target original source")
	}
	err = tx.Commit(v.ctx)
	var failure *pgconn.PgError
	if !errors.As(err, &failure) || failure.Code != "23503" || failure.ConstraintName != "index_profiles_source_operation_id_fkey" {
		t.Fatal("COMMIT allowed a source orphan or failed at another gate")
	}
}

func knowledgeM3CleanupClosedCalls(t *testing.T, v *knowledgeLifecycleApp, project id.ProjectID) []string {
	t.Helper()
	var ids []string
	workAppCheck(t, "knowledge-m3-original-closed-call-ids", v.admin.QueryRow(v.ctx, `SELECT ARRAY(SELECT id::text FROM agenteam_knowledge.project_calls WHERE project_id=$1 AND phase='business_closed' ORDER BY id)`, project.String()).Scan(&ids))
	if len(ids) == 0 {
		t.Fatal("original returned Knowledge calls missing")
	}
	return ids
}

func knowledgeM3CleanupImage(t *testing.T, v *knowledgeLifecycleApp, project id.ProjectID, closedCalls ...[]string) []byte {
	t.Helper()
	if len(closedCalls) > 1 {
		t.Fatal("one exact original call set required")
	}
	args := []any{project.String()}
	if len(closedCalls) == 1 {
		args = append(args, closedCalls[0])
	}
	var parts []string
	for _, table := range knowledgeM3CleanupTables {
		predicate := "project_id=$1"
		if table == "project_calls" && len(closedCalls) == 1 {
			// Current-profile observations still run for active projects. Compare
			// every originally returned call, while permitting their new calls.
			predicate += " AND id=ANY($2::uuid[])"
		}
		parts = append(parts, `(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb) FROM agenteam_knowledge.`+table+` t WHERE `+predicate+`)`)
	}
	var raw []byte
	workAppCheck(t, "knowledge-m3-exact-cleanup-image", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(`+strings.Join(parts, ",")+`)`, args...).Scan(&raw))
	return raw
}

func knowledgeM3CleanupModelImage(t *testing.T, v *knowledgeLifecycleApp) []byte {
	t.Helper()
	var raw []byte
	workAppCheck(t, "knowledge-m3-original-model-image", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.snapshots t WHERE project_id=$1),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.calls t WHERE project_id=$1))`, v.a.String()).Scan(&raw))
	return raw
}

// PostgreSQL 17.8 combines pending backend counters with current transaction
// counters. Compare both samples in this exact Tx; an absolute value can include
// earlier transactions on the reused connection. The observation still rejects
// every new Knowledge, Object, Model or Secret row write, including no-op updates.
func knowledgeM3CleanupWrites(ctx context.Context, x postgres.SQLExecutor) (int64, error) {
	var count int64
	err := x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname IN ('agenteam_knowledge','agenteam_object','agenteam_model','agenteam_secret')`).Scan(&count)
	return count, err
}

func knowledgeM3CleanupUnknown(t *testing.T, v *knowledgeLifecycleApp, matches func(context.Context, postgres.SQLExecutor) (bool, error)) {
	t.Helper()
	h := &usageAppHook{match: usageAppOwner("knowledge-project-cleanup", v.a), mask: true, selectRow: func(ctx context.Context, x postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		if after <= before {
			return false, nil
		}
		return matches(ctx, x)
	}}
	v.tracked.arm(h)
	var original usageAppObservation
	var held *usageAppCall
	for n := 0; n < 40 && held == nil; n++ {
		c := v.start(func() error { return v.cleaner.Run(v.ctx, v.a, v.cause.OperationID) })
		select {
		case original = <-h.observed:
			held = c
		case <-c.done:
			v.cleanupFailure(t, n+1, c.err)
			workAppCheck(t, "knowledge-m3-explicit-cleanup-round", c.err)
		case <-time.After(5 * time.Second):
			t.Fatal("original cleanup round did not return or reach selected physical COMMIT")
		}
	}
	if held == nil || original.result.State() != f.Committed {
		t.Fatal("target original cleanup commit was not observed")
	}
	h.open()
	usageAppWait(t, held.done, "knowledge-m3-original-cleanup-unknown")
	knowledgeAppRequireUnknown(t, held.err, original)
	before := knowledgeM3CleanupImage(t, v, v.a)
	confirm := &usageAppHook{match: func(c f.TransactionCause) bool { return usageAppSameCause(c, original.cause) }, selectRow: func(ctx context.Context, x postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		if after != before {
			t.Logf("M3_KNOWLEDGE_CLEANUP_CONFIRM unexpected_four_schema_writes=%d", after-before)
			return false, f.NewFault(f.InvalidState, f.NotCommitted)
		}
		return true, nil
	}}
	v.tracked.arm(confirm)
	read := v.start(func() error { return v.cleaner.Run(v.ctx, v.a, v.cause.OperationID) })
	select {
	case observed := <-confirm.observed:
		if observed.result.State() != f.Committed {
			t.Fatal("original cleanup observation did not commit")
		}
		t.Logf("M3_KNOWLEDGE_CLEANUP_CONFIRM committed=true four_schema_write_delta=%d", observed.after-observed.before)
	case <-read.done:
		v.cleanupFailure(t, 0, read.err)
		var fault *f.Fault
		code, outcome := "none", "none"
		if errors.As(read.err, &fault) {
			code, outcome = string(fault.Code.Safe()), string(fault.CommitState.Safe())
		}
		t.Fatalf("original cleanup confirmation returned before its commit barrier: code=%s outcome=%s", code, outcome)
	case <-time.After(5 * time.Second):
		t.Fatal("original cleanup confirmation did not return or reach its commit barrier")
	}
	if !bytes.Equal(before, knowledgeM3CleanupImage(t, v, v.a)) {
		t.Fatal("cleanup confirmation changed the original committed facts")
	}
	confirm.open()
	usageAppWait(t, read.done, "knowledge-m3-original-cleanup-observation")
	workAppCheck(t, "knowledge-m3-readonly-confirmation", read.err)
	if !bytes.Equal(before, knowledgeM3CleanupImage(t, v, v.a)) {
		t.Fatal("confirmation advanced another cleanup batch")
	}
}
