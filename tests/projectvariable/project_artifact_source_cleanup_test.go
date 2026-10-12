//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The original source fixture publishes a same-Project copy, then joins a
// second interrupted source call through real Artifact/Object Stop. The
// remaining lifecycle acceptance/prefix boundaries retain their existing
// controlled scope; this test does not bind the default Registry/finalizer.
func TestProjectArtifactSourceCleanup(t *testing.T) {
	s := newArtifactSourceStopPG(t, false)
	v := s.base
	if !s.settled {
		t.Fatal("source cleanup started before both original Stop components returned")
	}
	s.facts(t, "released", true, true, true)
	receipt := s.stopReceipt(t)
	var foreignIDs []string
	artifactSourceRequire(t, "cleanup-original-foreign-work", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(array_agg(id::text ORDER BY id),'{}'::text[]) FROM agenteam_artifact.project_work WHERE project_id=$1`, v.p2.base.project.ID.String()).Scan(&foreignIDs))
	if len(foreignIDs) == 0 {
		t.Fatal("independent Project lacked real returned owned reads")
	}
	foreign := sourceCleanupForeignWork(t, s, foreignIDs)
	driver := v.driver(t)
	// The original ready helper deliberately checks its two-Artifact fixture.
	// Keep that helper unchanged and consume the same actual predecessor calls.
	v.agentVariablesTerminalPG.ready(t, driver)
	for _, stage := range []string{"combined-terminal", "secret"} {
		call := v.run(t, driver)
		outboxCleanupWait(t, call.done, "original source cleanup "+stage+" predecessor")
		artifactSourceRequire(t, "cleanup-prefix-"+stage, call.err)
	}
	v.state(t, "required", "domains", "terminal")
	var sourceCommands, sourceWork, objects int
	artifactSourceRequire(t, "cleanup-original-source-history", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1 AND path='source'),(SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1 AND source_lease_id IS NOT NULL),(SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1)`, s.scope.ProjectID.String()).Scan(&sourceCommands, &sourceWork, &objects))
	if sourceCommands != 2 || sourceWork != 2 || objects != 4 || len(v.objects) != 4 || v.counts(t) != [5]int{4, 5, 1, 2, 1} || v.artifacts.calls.Load() != 0 || v.outbox.calls.Load() != 0 || v.secrets.calls.Load() != 1 {
		t.Fatal("source cleanup prefix lost its real nonempty command/work/Object facts", sourceCommands, sourceWork, objects, v.counts(t))
	}
	v.native(t, true)
	store := v.p2.base.tracked
	held := s.barrier()
	confirmation := s.barrier()
	var selected, masked, confirmed atomic.Bool
	var original f.CommitResult
	attempt := id[f.TransactionAttempt](t)
	// Release every held original call before any pair is globally stopped.
	// An assertion failure after Unknown must still confirm that same owner.
	t.Cleanup(func() {
		held.open()
		confirmation.open()
		for _, call := range v.calls {
			outboxCleanupWait(t, call.done, "source cleanup original return before shutdown")
		}
		s.clearHooks()
		if masked.Load() && !confirmed.Load() {
			call := v.run(t, driver)
			outboxCleanupWait(t, call.done, "original source history Unknown confirmation before shutdown")
			if call.err != nil {
				t.Error("source history cleanup retained its original owner", call.err)
			} else {
				confirmed.Store(true)
			}
		}
	})
	type beforeFinal struct {
		objectTechnical string
		commands, work  int
	}
	var originals sync.Map
	store.mu.Lock()
	store.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.providerCause(cause, "artifact-cleanup") {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var before beforeFinal
		if err = x.QueryRow(ctx, sourceCleanupObjectTechnicalSQL, s.scope.ProjectID.String()).Scan(&before.objectTechnical); err != nil {
			return err
		}
		if err = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1 AND path='source'),(SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1 AND source_lease_id IS NOT NULL)`, s.scope.ProjectID.String()).Scan(&before.commands, &before.work); err != nil {
			return err
		}
		originals.Store(tx, before)
		return nil
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if selected.Load() && !masked.Load() && v.providerCause(cause, "artifact-cleanup") && result.State() == f.Committed {
			held.hold(result)
		}
	}
	store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if selected.Load() && v.providerCause(cause, "artifact-cleanup") && result.State() == f.Committed && masked.CompareAndSwap(false, true) {
			original = f.UnknownResult(attempt, cause) // Local acknowledgement mask, not a network fault.
			return original
		}
		return result
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.providerCause(cause, "artifact-cleanup") {
			return nil
		}
		value, ok := originals.LoadAndDelete(tx)
		if !ok {
			return errors.New("source cleanup lost its original transaction observation")
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var completed bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.cleanup WHERE project_id=$1 AND operation_id=$2 AND state='completed')`, s.scope.ProjectID.String(), s.cause.OperationID.String()).Scan(&completed); err != nil || !completed {
			return err
		}
		var work, commands int
		var objectTechnical string
		if err = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1),(SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1)`, s.scope.ProjectID.String()).Scan(&work, &commands); err != nil {
			return err
		}
		if err = x.QueryRow(ctx, sourceCleanupObjectTechnicalSQL, s.scope.ProjectID.String()).Scan(&objectTechnical); err != nil {
			return err
		}
		before := value.(beforeFinal)
		if before.commands != 2 || before.work != 2 || commands != 0 || work != 0 || before.objectTechnical != objectTechnical {
			return errors.New("source history final transaction did not retire exact Artifact pairs or changed Object technical ownership")
		}
		selected.Store(true)
		return nil
	})
	var mutation *skillsCleanupPGCall
	previous := "required"
	// Four objects fit the same fixed batch as the original two. Each further
	// Run still requires its preceding actual Pending report and return.
rounds:
	for round := 0; round < 4; round++ {
		mutation = v.run(t, driver)
		select {
		case result := <-held.entered:
			if result.State() != f.Committed {
				t.Fatal("source history barrier lacked the original committed Store result")
			}
			break rounds
		case <-mutation.done:
			artifactSourceRequire(t, "cleanup-before-final-original-return", mutation.err)
			v.artifacts.mu.Lock()
			report := v.artifacts.report
			v.artifacts.mu.Unlock()
			if !report.Matches(pc.ArtifactObjectParticipant, v.cause, s.scope) || report.Details().State != pc.CleanupPending {
				t.Fatal("source cleanup advanced without its selected final transaction or original Pending")
			}
			previous = "pending"
			v.state(t, previous, "domains", "terminal")
		case <-time.After(5 * time.Second):
			t.Fatal("source history cleanup original final return was not reached")
		}
	}
	if !selected.Load() || mutation == nil {
		t.Fatal("source history did not reach its original bounded final transaction")
	}
	v.state(t, previous, "domains", "running")
	v.empty(t)
	sourceCleanupRetired(t, s, receipt, foreignIDs, foreign)
	v.secondDriverBusy(t, mutation)
	v.otherReadable(t)
	held.open()
	outboxCleanupWait(t, mutation.done, "source history original mutation acknowledgement")
	var actual, provider *f.Fault
	v.artifacts.mu.Lock()
	providerErr, report := v.artifacts.err, v.artifacts.report
	v.artifacts.mu.Unlock()
	if !masked.Load() || original.State() != f.Unknown || original.AttemptID() != attempt || !v.providerCause(original.Cause(), "artifact-cleanup") || !errors.As(mutation.err, &actual) || !errors.As(providerErr, &provider) || actual != provider || actual.Code != f.CommitUnknown || actual.CommitState != f.Unknown || actual.CauseID != attempt.String() || report.Details().State != "" {
		agentRetirementDiagnostic(t, "source-history-original-unknown", mutation.err)
		t.Fatal("source history Unknown lost its original cause/attempt/Fault or published progress")
	}
	v.state(t, previous, "domains", "running")
	s.clearHooks()
	before := sourceCleanupSnapshot(t, s)
	var baseline sync.Map
	var observations, writes atomic.Int64
	readWrites := func(ctx context.Context, tx f.Tx) (int64, error) {
		x, err := store.InTx(tx)
		if err != nil {
			return 0, err
		}
		var value int64
		err = x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname IN ('agenteam_artifact','agenteam_object','agenteam_download')`).Scan(&value)
		return value, err
	}
	store.mu.Lock()
	store.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.providerCause(cause, "artifact-cleanup") {
			return nil
		}
		n, err := readWrites(ctx, tx)
		if err == nil {
			baseline.Store(tx, n)
		}
		return err
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.providerCause(cause, "artifact-cleanup") && result.State() == f.Committed {
			confirmation.hold(result)
		}
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.providerCause(cause, "artifact-cleanup") {
			return nil
		}
		n, err := readWrites(ctx, tx)
		if err != nil {
			return err
		}
		start, ok := baseline.LoadAndDelete(tx)
		if !ok || n < start.(int64) {
			return errors.New("source history confirmation lost its same-original-Tx baseline")
		}
		writes.Add(n - start.(int64))
		observations.Add(1)
		return nil
	})
	call := v.run(t, driver)
	s.awaitHeld(t, call, confirmation)
	if observations.Load() != 1 || writes.Load() != 0 || sourceCleanupSnapshot(t, s) != before {
		t.Fatal("source history confirmation repeated a mutation or changed original facts", observations.Load(), writes.Load())
	}
	v.state(t, previous, "domains", "running")
	v.secondDriverBusy(t, call)
	v.otherReadable(t)
	confirmation.open()
	outboxCleanupWait(t, call.done, "source history confirmation original Store return")
	artifactSourceRequire(t, "cleanup-original-confirmation-return", call.err)
	confirmed.Store(true)
	s.clearHooks()
	v.state(t, "completed", "outbox", "terminal")
	if v.outbox.calls.Load() != 0 || sourceCleanupSnapshot(t, s) != before {
		t.Fatal("source history confirmation changed metadata or ran the next participant")
	}
	v.empty(t)
	sourceCleanupRetired(t, s, receipt, foreignIDs, foreign)
	v.otherReadable(t)
	s.closeSource(t)
	v.close(t)
	t.Log("PROJECT_ARTIFACT_SOURCE_CLEANUP real_same_project_source_publication=true two_original_source_commands_and_work_retired=true actual_artifact_object_stop=true physical_minio_removed=true final_provider_original_return_held=true local_ack_unknown_original_attempt=true confirmation_same_tx_zero_writes=true artifact_completed_after_return=true current_delete_receipt_retained=true object_technical_owner_unchanged_by_artifact_transaction=true other_project_live=true no_full_delete=true")
}

const sourceCleanupObjectTechnicalSQL = `SELECT jsonb_build_array((SELECT COALESCE(jsonb_agg(to_jsonb(w) ORDER BY id),'[]'::jsonb) FROM agenteam_object.project_work w WHERE project_id=$1),(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY operation_id),'[]'::jsonb) FROM agenteam_object.project_stops s WHERE project_id=$1))::text`

func sourceCleanupForeignWork(t *testing.T, s *artifactSourceStopPG, ids []string) string {
	t.Helper()
	var out string
	artifactSourceRequire(t, "cleanup-preserved-foreign-work", s.base.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(jsonb_agg(to_jsonb(w) ORDER BY id),'[]')::text FROM agenteam_artifact.project_work w WHERE project_id=$1 AND id=ANY($2::uuid[])`, s.base.p2.base.project.ID.String(), ids).Scan(&out))
	return out
}

func sourceCleanupSnapshot(t *testing.T, s *artifactSourceStopPG) [4]string {
	t.Helper()
	v := s.base
	var value [4]string
	value[0] = v.snapshot(t)
	artifactSourceRequire(t, "cleanup-object-technical-snapshot", v.p2.base.raw.QueryRow(ctxFor(t), sourceCleanupObjectTechnicalSQL, s.scope.ProjectID.String()).Scan(&value[1]))
	artifactSourceRequire(t, "cleanup-artifact-technical-snapshot", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT COALESCE(jsonb_agg(to_jsonb(w) ORDER BY id),'[]'::jsonb) FROM agenteam_artifact.project_work w WHERE project_id=$1),(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY operation_id),'[]'::jsonb) FROM agenteam_artifact.project_stops s WHERE project_id=$1))::text`, s.scope.ProjectID.String()).Scan(&value[2]))
	ids := make([]string, 0, len(v.objects))
	for _, object := range v.objects {
		ids = append(ids, object.id)
	}
	artifactSourceRequire(t, "cleanup-all-object-associated-snapshot", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.uploads x WHERE object_id::text=ANY($1::text[])),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.upload_attempts x WHERE object_id::text=ANY($1::text[])),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.object_leases x WHERE object_id::text=ANY($1::text[])),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_object.cleanup_operations x WHERE object_id::text=ANY($1::text[])))::text`, ids).Scan(&value[3]))
	return value
}

func sourceCleanupRetired(t *testing.T, s *artifactSourceStopPG, receipt string, foreignIDs []string, foreign string) {
	t.Helper()
	var commands, work, stops int
	var objectStopped bool
	artifactSourceRequire(t, "cleanup-final-owner-facts", s.base.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1),(SELECT count(*) FROM agenteam_artifact.project_stops WHERE project_id=$1),EXISTS(SELECT 1 FROM agenteam_object.project_stops WHERE project_id=$1 AND operation_id=$2 AND state='stopped')`, s.scope.ProjectID.String(), s.cause.OperationID.String()).Scan(&commands, &work, &stops, &objectStopped))
	if commands != 0 || work != 0 || stops != 1 || receipt == "" || s.stopReceipt(t) != receipt || !objectStopped || sourceCleanupForeignWork(t, s, foreignIDs) != foreign {
		t.Fatal("source history retirement lost its domain/receipt/other-Project boundary", commands, work, stops, objectStopped)
	}
}
