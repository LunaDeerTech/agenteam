//go:build integration

package objects_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func TestObjectMetadataCleanupOldAttemptsAndStopHistory(t *testing.T) {
	base := newFixture(t, false)
	proxy := newStorageProxy(t, base)
	// This owns only the setup proxy's lifetime. Every Stop/cleanup call below
	// still receives the original 2s budget; the test binary keeps its 6m cap.
	proxy.cancel()
	proxy.ctx, proxy.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	cfg := auditStorageConfig(t, base, proxy.server.URL)
	plans := &metadataPlanStore{Store: base.store}
	f, _ := metadataCleanupFixtureOn(t, base, objectAuditOptions{config: &cfg, store: plans})
	body := strings.Repeat("x", 2*oc.StreamBufferSize+64)
	cmd := command(t, "actual-old-candidates")
	var object oc.ObjectID
	for range 65 {
		proxy.mode.Store(proxyRejectCandidateRead)
		_, err := f.service.PutObject(contextFor(t), f.actor, f.owner, cmd, "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
		if err == nil {
			t.Fatal("controlled real verification failure was accepted")
		}
		var raw, key string
		var closed bool
		err = f.store.QueryRow(contextFor(t), `SELECT u.object_id::text,a.candidate_key,a.io_closed FROM agenteam_object.uploads u JOIN agenteam_object.upload_attempts a ON a.id=u.current_attempt_id WHERE u.command_key=$1`, string(cmd.IdempotencyKey)).Scan(&raw, &key, &closed)
		if err != nil || !closed {
			t.Fatal("failed candidate did not actually close", err)
		}
		current, err := foundation.ParseID[oc.StoredObject](raw)
		if err != nil || object.Validate() == nil && object != current {
			t.Fatal("same command changed the original Object identity", err)
		}
		object = current
		// Remove the task-owned bytes, then let real verification establish
		// absence, original AbandonedAttempt cause, cleanup and joined work.
		// No native phase/io_closed/joined/checkpoint is fabricated with SQL.
		if err := f.s3.RemoveObject(contextFor(t), f.bucket, key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatal(err)
		}
		proxy.mode.Store(proxyPass)
		if err := f.service.Recover(contextFor(t)); err != nil {
			t.Fatal("real failed-candidate recovery", err)
		}
	}
	put, err := f.service.PutObject(contextFor(t), f.actor, f.owner, cmd, "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	if err != nil || put.Meta.ID != object {
		t.Fatal("same command did not publish its fresh current candidate", err)
	}
	var oldCount int
	var original string
	err = f.store.QueryRow(contextFor(t), `SELECT count(*),min(c.operation_id::text) FROM agenteam_object.cleanup_operations c JOIN agenteam_object.uploads u ON u.object_id=c.object_id WHERE c.object_id=$1 AND c.reason='abandoned_attempt' AND c.operation_id=u.id AND c.phase='completed'`, object.String()).Scan(&oldCount, &original)
	if err != nil || oldCount != 65 {
		t.Fatal("old completed causes were not produced by actual recovery", err, oldCount)
	}
	for range 1001 {
		r, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, object, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal("real terminal history reader", readErr, closeErr)
		}
	}
	// Keep one later native reader truly live. Its selected Object has >1000
	// historical rows, which the old fanout projection could never advance.
	proxy.mode.Store(proxyHoldReadBody)
	reader, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, object, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertStopBodyHeld(t, proxy)
	var live int
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, object.String()).Scan(&live); err != nil || live != 1 {
		t.Fatal("late reader was not actually live", err, live)
	}
	actor, stop := activateObjectStop(t, f.fixture, oc.ProjectStopDelete)
	ctx, cancel := context.WithTimeout(contextFor(t), 2*time.Second)
	deadline, _ := ctx.Deadline()
	_, err = f.service.RequestProjectStop(ctx, actor, stop)
	returned := time.Now()
	cancel()
	if err != nil || returned.After(deadline) {
		t.Fatal("bounded first Stop", err)
	}
	proxy.release()
	if err := reader.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal("late reader Close returned an unrelated failure", err)
	}
	// finished is an earlier response milestone: serve closes it before its
	// deferred upstream Body.Close and final wg.Done. Wait for the original
	// handler accounting before any subsequent Stop/Release/Delete call.
	joinDeadline := time.Now().Add(2 * time.Second)
	joinTimer := time.NewTimer(time.Until(joinDeadline))
	defer joinTimer.Stop()
	handlersJoined := make(chan struct{})
	go func() {
		proxy.wg.Wait()
		close(handlersJoined)
	}()
	select {
	case <-handlersJoined:
		if time.Now().After(joinDeadline) {
			t.Fatal("original GET handler joined after the existing tail deadline")
		}
	case <-joinTimer.C:
		t.Fatal("original held GET handler did not actually return")
	}
	metadataHistoryStopUntilSettled(t, f, actor, stop)
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active'`, object.String()).Scan(&live); err != nil || live != 0 {
		t.Fatal("Stop skipped an active native lease", err, live)
	}
	if projectWorkCount(t, f.fixture, "reader", true) != 1002 || projectWorkCount(t, f.fixture, "reader", false) != 0 {
		t.Fatal("1001 history readers plus the late reader did not actually retire")
	}
	// EXPLAIN observes genuine histories after Stop actually returned; it does
	// not supply stop/cleanup permission or extend the business call's budget.
	f.sql(t, `ANALYZE agenteam_object.upload_attempts; ANALYZE agenteam_object.cleanup_operations; ANALYZE agenteam_object.object_leases; ANALYZE agenteam_object.project_work`)
	plans.explain(t, "1001-history-stop-returned", "stop-work", "stop-full-pending")
	operation, err := foundation.ParseID[oc.CleanupOperation](stop.Details().OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: operation, Owner: f.owner, Reason: oc.ProjectDeleted})
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, `INSERT INTO object_fixture.metadata_cleanup(operation_id,object_id,upload_id,project_id,owner_id,skill_id,user_id,project_version,phase) SELECT $1,u.object_id,u.id,u.project_id,u.owner_id,r.parent_id,p.owner_id,p.version,'gated' FROM agenteam_object.uploads u JOIN object_fixture.owners r ON r.id=u.owner_id JOIN object_fixture.projects p ON p.id=u.project_id WHERE u.object_id=$2`, operation.String(), object.String())
	for range 2 {
		if r := metadataRelease(t, f, cleanup, object); r.State() != foundation.Committed {
			t.Fatal("canonical release/replay rejected old independent causes", r.Fault())
		}
	}
	ctx, cancel = context.WithTimeout(contextFor(t), 2*time.Second)
	deadline, _ = ctx.Deadline()
	cleaned, err := f.service.DeleteUnreferencedWithinBudget(ctx, cleanup, object)
	returned = time.Now()
	cancel()
	if err != nil || cleaned.State != oc.CleanupCompleted || returned.After(deadline) {
		t.Fatal("bounded physical cleanup after genuine history", err)
	}
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND reason='abandoned_attempt' AND operation_id=$2 AND phase='completed'`, object.String(), original).Scan(&oldCount); err != nil || oldCount != 65 {
		t.Fatal("cleanup rewrote old causes", err, oldCount)
	}
	if objectAuditCount(t, f.fixture, ac.ObjectDelete) != 1 {
		t.Fatal("canonical delete did not append exactly one native Audit")
	}
	plans.explain(t, "65-attempts-1001-readers-physical-returned", "gate-two-pending-sets", "physical-full-pending")
}

// Keep this stricter historical-budget oracle local: previously accepted
// metadata cases and their shared fixture retain their original input bytes.
func metadataHistoryStopUntilSettled(t *testing.T, f *objectAuditFixture, actor identity.Actor, cause oc.ProjectStopCause) {
	t.Helper()
	total, end := context.WithTimeout(contextFor(t), 3*time.Second)
	defer end()
	totalDeadline, _ := total.Deadline()
	for range 40 {
		ctx, cancel := context.WithTimeout(total, 2*time.Second)
		deadline, _ := ctx.Deadline()
		report, err := f.service.RequestProjectStop(ctx, actor, cause)
		returned := time.Now()
		cancel()
		if err == nil && (returned.After(deadline) || returned.After(totalDeadline)) {
			t.Fatal("Stop returned success after its original call or total deadline")
		}
		if err == nil && report.Details().State == oc.ProjectStopped {
			return
		}
		if err != nil && codeOf(err) != foundation.ResourceBusy {
			t.Fatal("bounded Stop failed", err)
		}
		select {
		case <-total.Done():
			t.Fatal("bounded Stop did not converge", report.Details().State)
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatal("bounded Stop exhausted the fixture's existing round limit")
}
