//go:build integration && linux

package projectvariable_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Setup uses the original controlled Model JSONCaller once and real Command
// WSS once, then formally revokes dispatch and commits 66, 68 and 72. This
// fixture adds only reservation calls against the same Store and actual guard.
type roundModelCallReservationFixture struct {
	request *nextRoundRequestFixture
	value   ec.NextRoundModelRequest
	row     nextRoundRequestRow
	model   [3]string
}

func newRoundModelCallReservationFixture(t *testing.T) *roundModelCallReservationFixture {
	t.Helper()
	v := newNextRoundRequestFixture(t)
	binder := v.binder(t)
	q := v.input.Fields()
	value, err := binder.Prepare(ctxFor(t), q.ExecutionID, q.RoundID)
	readFileAdapterOK(t, err, "real original 72 before reservation")
	row := v.durable(t)
	v.requireRequest(t, value, row)
	stopNextRoundRequestBinder(t, binder)
	x := &roundModelCallReservationFixture{request: v, value: value, row: row}
	x.model = x.modelFacts(t)
	x.requireUnchanged(t)
	if x.durable(t).count != 0 {
		t.Fatal("reservation fixture already contained a claim")
	}
	return x
}

func (v *roundModelCallReservationFixture) owner(t *testing.T) *execution.RoundModelCallOwner {
	t.Helper()
	c := v.request.command.operation.facts.capture
	owner, err := execution.NewRoundModelCallOwner(c.v.base.tracked, c.authority, c.projectAuthority, c.v.agent.guard)
	readFileAdapterOK(t, err, "original live Store and ProcessGuard reservation owner")
	// Registered after the original fixture cleanup: every reservation borrower
	// must finish before that fixture's Object Runtime releases this guard.
	t.Cleanup(func() { stopRoundModelCallReservationOwner(t, owner) })
	return owner
}

func stopRoundModelCallReservationOwner(t *testing.T, owner *execution.RoundModelCallOwner) {
	t.Helper()
	owner.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := owner.Drain(ctx); err != nil || !owner.Joined() {
		t.Error("original reservation owner did not actually join", err)
	}
}

func (v *roundModelCallReservationFixture) matches(cause f.TransactionCause) bool {
	d, q := cause.Details(), v.value.Fields()
	return d.Kind == f.JobCause && d.JobType == "execution.round-model-call.claim" && d.JobID == q.ExecutionID.String() && d.JobAttemptID == q.RoundInputID.String()
}

// Read all reservation columns and xmin independently, including total claims
// for E even when the selected input has no row. No product decoder supplies
// the expected SQL state or a receipt for the test.
type roundModelCallReservationRow struct {
	count, schema                                                                   int64
	claim, project, agent, execution, round, snapshot, call, process                string
	roundDigest, snapshotDigest, projectionDigest, inputDigest, requestDigest, xmin string
	created                                                                         time.Time
}

func (v *roundModelCallReservationFixture) read(ctx context.Context, x postgres.SQLExecutor) (roundModelCallReservationRow, error) {
	var got roundModelCallReservationRow
	q := v.value.Fields()
	err := x.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.round_model_claims z WHERE z.execution_id=e.id),
 COALESCE(r.id,''),COALESCE(r.project_id,''),COALESCE(r.agent_id,''),COALESCE(r.execution_id,''),
 COALESCE(r.round_input_id,''),COALESCE(r.snapshot_id,''),COALESCE(r.call_id,''),COALESCE(r.process_id,''),COALESCE(r.schema_version,0),
 COALESCE(r.round_input_digest,''),COALESCE(r.snapshot_digest,''),COALESCE(r.projection_digest,''),COALESCE(r.input_digest,''),COALESCE(r.request_digest,''),
 COALESCE(r.created_at,TIMESTAMPTZ 'epoch'),COALESCE(r.xmin::text,'')
 FROM agenteam_execution.executions e
 LEFT JOIN agenteam_execution.round_model_claims r ON r.execution_id=e.id AND r.round_input_id=$2
 WHERE e.id=$1`, q.ExecutionID.String(), q.RoundInputID.String()).Scan(&got.count,
		&got.claim, &got.project, &got.agent, &got.execution, &got.round, &got.snapshot, &got.call, &got.process, &got.schema,
		&got.roundDigest, &got.snapshotDigest, &got.projectionDigest, &got.inputDigest, &got.requestDigest, &got.created, &got.xmin)
	return got, err
}

func (v *roundModelCallReservationFixture) durable(t *testing.T) roundModelCallReservationRow {
	t.Helper()
	got, err := v.read(ctxFor(t), v.request.command.operation.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "independent original reservation row")
	return got
}

func (v *roundModelCallReservationFixture) modelFacts(t *testing.T) [3]string {
	t.Helper()
	var got [3]string
	err := v.request.command.operation.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT
 COALESCE((SELECT json_agg(z ORDER BY z.id)::text FROM agenteam_model.invocations z WHERE z.execution_id=$1),'[]'),
 COALESCE((SELECT json_agg(z ORDER BY z.invocation_id,z.sequence)::text FROM agenteam_model.invocation_observations z JOIN agenteam_model.invocations m ON m.id=z.invocation_id WHERE m.execution_id=$1),'[]'),
 COALESCE((SELECT json_agg(z ORDER BY z.project_id)::text FROM agenteam_model.execution_usage_summaries z WHERE z.execution_id=$1),'[]')`, v.value.Fields().ExecutionID.String()).Scan(&got[0], &got[1], &got[2])
	readFileAdapterOK(t, err, "independent original Invocation and Usage facts")
	return got
}

func (v *roundModelCallReservationFixture) requireUnchanged(t *testing.T) {
	t.Helper()
	v.request.requireUnchanged(t)
	if !reflect.DeepEqual(v.request.durable(t), v.row) || v.modelFacts(t) != v.model {
		t.Fatal("reservation changed original 72 bytes/digests or Invocation/Usage facts")
	}
	v.request.requireRequest(t, v.value, v.row)
}

func (v *roundModelCallReservationFixture) requireReceipt(t *testing.T, receipt ec.RoundModelClaimReceipt, row roundModelCallReservationRow) {
	t.Helper()
	if receipt == nil || receipt.Validate() != nil {
		t.Fatal("reservation did not return a valid original receipt")
	}
	q, got := v.value.Fields(), receipt.Fields()
	process, err := v.request.command.operation.facts.capture.v.agent.guard.CurrentProcess()
	readFileAdapterOK(t, err, "original guard remains held through receipt observation")
	if got.SchemaVersion != 1 || got.ClaimID.Validate() != nil || got.ProjectID != q.ProjectID || got.AgentID != q.AgentID || got.ExecutionID != q.ExecutionID || got.RoundInputID != q.RoundInputID || got.SnapshotID != q.SnapshotID || got.CallID != q.CallID || got.ProcessID != process || got.RoundInputDigest != q.RoundInputDigest || got.SnapshotDigest != q.SnapshotDigest || got.ProjectionDigest != q.ProjectionDigest || got.InputDigest != v.value.InputDigest() || got.RequestDigest != v.value.Digest() || got.CreatedAt.Time().Before(v.row.created) {
		t.Fatal("reservation receipt changed original request identity, process provenance or digest")
	}
	if row.count != 1 || row.schema != 1 || row.claim != got.ClaimID.String() || row.project != got.ProjectID.String() || row.agent != got.AgentID.String() || row.execution != got.ExecutionID.String() || row.round != got.RoundInputID.String() || row.snapshot != got.SnapshotID.String() || row.call != got.CallID.String() || row.process != got.ProcessID.String() || row.roundDigest != string(got.RoundInputDigest) || row.snapshotDigest != string(got.SnapshotDigest) || row.projectionDigest != string(got.ProjectionDigest) || row.inputDigest != string(got.InputDigest) || row.requestDigest != string(got.RequestDigest) || !row.created.Equal(got.CreatedAt.Time()) || row.xmin == "" {
		t.Fatal("independent committed reservation differs from the entire returned receipt")
	}
}

func requireSameRoundModelClaim(t *testing.T, a, b ec.RoundModelClaimReceipt) {
	t.Helper()
	if a == nil || b == nil || a.Validate() != nil || b.Validate() != nil || a.Fields() != b.Fields() {
		t.Fatal("reservation observation changed original ClaimID, CallID or complete candidate")
	}
}

func TestRoundModelCallReservation(t *testing.T) {
	t.Run("committed-request-reservation-and-history", func(t *testing.T) {
		v := newRoundModelCallReservationFixture(t)
		owner := v.owner(t)
		q := v.value.Fields()
		receipt, err := owner.Claim(ctxFor(t), q.ExecutionID, q.RoundInputID)
		readFileAdapterOK(t, err, "original reservation committed return")
		committed := v.durable(t)
		v.requireReceipt(t, receipt, committed)
		again, err := owner.Claim(ctxFor(t), q.ExecutionID, q.RoundInputID)
		readFileAdapterOK(t, err, "same local owner original reservation replay")
		requireSameRoundModelClaim(t, receipt, again)
		observer := v.owner(t)
		for _, current := range []*execution.RoundModelCallOwner{owner, observer} {
			observed, found, err := current.Lookup(ctxFor(t), q.ExecutionID, q.RoundInputID)
			readFileAdapterOK(t, err, "historical reservation after dispatch revocation")
			if !found {
				t.Fatal("original reservation disappeared")
			}
			requireSameRoundModelClaim(t, receipt, observed)
		}
		// The new instance has the same real guard and has seen the real receipt.
		// Neither observation nor matching ProcessID confers the old local owner.
		foreign, err := observer.Claim(ctxFor(t), q.ExecutionID, q.RoundInputID)
		requireCode(t, err, f.ResourceBusy)
		if foreign != nil || v.durable(t) != committed {
			t.Fatal("new-instance Claim took over or rewrote the immutable reservation")
		}
		stopRoundModelCallReservationOwner(t, observer)
		stopRoundModelCallReservationOwner(t, owner)
		v.requireUnchanged(t)
		t.Log("round_model_call_reservation same_owner_replay=true new_instance_lookup_only=true immutable_reservation_rows=1 reservation_model_calls=0 reservation_wire_requests=0 invocation_usage_unchanged=true setup_model_calls=1 setup_wire_requests=1")
	})
	t.Run("reservation-rollback-and-commit-loss-lookup", func(t *testing.T) {
		v := newRoundModelCallReservationFixture(t)
		before := v.durable(t)
		owner := v.owner(t)
		q := v.value.Fields()
		store := v.request.command.operation.facts.capture.v.base.tracked
		var candidate roundModelCallReservationRow
		var physical f.CommitResult
		finalCalls := 0
		rollback := errors.New("owned reservation rollback")
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !v.matches(cause) {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			candidate, err = v.read(ctx, x)
			if err != nil {
				return err
			}
			if candidate.count != 1 || candidate.claim == "" || candidate.call != q.CallID.String() {
				return errors.New("reservation callback lacked its original complete insert")
			}
			return rollback
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if v.matches(cause) {
				finalCalls++
				physical = result
			}
		}
		store.mu.Unlock()
		t.Cleanup(func() {
			roundInputClearHooks(store)
			store.mu.Lock()
			store.transformResult = nil
			store.mu.Unlock()
		})
		absent, err := owner.Claim(ctxFor(t), q.ExecutionID, q.RoundInputID)
		if err == nil || absent != nil || finalCalls != 1 || physical.State() != f.NotCommitted || candidate.count != 1 || v.durable(t) != before {
			t.Fatal("actual original rollback did not leave zero reservation facts")
		}
		observed, found, err := owner.Lookup(ctxFor(t), q.ExecutionID, q.RoundInputID)
		readFileAdapterOK(t, err, "absent reservation after actual NotCommitted")
		if found || observed != nil {
			t.Fatal("rolled-back reservation was published")
		}
		v.requireUnchanged(t)
		// Only the actual NotCommitted result permits this next attempt. The
		// read above is an observation, never proof of a write's noncommit.
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !v.matches(cause) {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			candidate, err = v.read(ctx, x)
			return err
		})
		entered, release, returned := make(chan f.CommitResult, 1), make(chan struct{}), make(chan struct{})
		var once sync.Once
		resume := func() { once.Do(func() { close(release) }) }
		attempt := id[f.TransactionAttempt](t)
		store.mu.Lock()
		store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if v.matches(cause) && result.State() == f.Committed {
				entered <- result
				<-release
				return f.UnknownResult(attempt, cause)
			}
			return result
		}
		store.mu.Unlock()
		type completion struct {
			receipt ec.RoundModelClaimReceipt
			err     error
		}
		done := make(chan completion, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(func() {
			resume()
			cancel()
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("original held Claim did not actually return")
			}
		})
		go func() {
			defer close(returned)
			receipt, err := owner.Claim(ctx, q.ExecutionID, q.RoundInputID)
			done <- completion{receipt, err}
		}()
		select {
		case actual := <-entered:
			if actual.State() != f.Committed || finalCalls != 2 || physical.State() != f.Committed {
				t.Fatal("reservation receipt barrier did not follow actual original COMMIT")
			}
		case <-returned:
			t.Fatal("Claim returned before its original COMMIT return barrier")
		case <-ctx.Done():
			t.Fatal("original reservation COMMIT barrier was not reached")
		}
		committed := v.durable(t)
		if committed != candidate || committed.count != 1 {
			t.Fatal("physical COMMIT lost the complete original reservation candidate")
		}
		owner.Stop()
		short, finish := context.WithTimeout(context.Background(), 25*time.Millisecond)
		drainErr := owner.Drain(short)
		finish()
		if !errors.Is(drainErr, context.DeadlineExceeded) || owner.Joined() {
			t.Fatal("Stop or Drain retired the owner before original Store return")
		}
		select {
		case <-done:
			t.Fatal("Claim published before original Store return")
		default:
		}
		resume()
		var result completion
		select {
		case result = <-done:
		case <-ctx.Done():
			t.Fatal("original Claim did not return after barrier release")
		}
		<-returned
		requireCode(t, result.err, f.CommitUnknown)
		unknown, retained := execution.UnknownAttempt(result.err)
		if result.receipt != nil || !retained || unknown.State() != f.Unknown || unknown.AttemptID() != attempt || !v.matches(unknown.Cause()) || owner.Joined() {
			t.Fatal("Unknown lost original attempt/candidate or was silently joined")
		}
		observer := v.owner(t)
		foreign, found, err := observer.Lookup(ctxFor(t), q.ExecutionID, q.RoundInputID)
		readFileAdapterOK(t, err, "new instance observes actual committed reservation")
		if !found {
			t.Fatal("new historical observer lost the original committed reservation")
		}
		v.requireReceipt(t, foreign, committed)
		// An independent observation cannot settle the original private pending
		// registration. Stop retains only its exact original Lookup channel.
		drainErr = owner.Drain(ctxFor(t))
		pending, retained := execution.UnknownAttempt(drainErr)
		if !retained || pending.AttemptID() != attempt || !v.matches(pending.Cause()) || owner.Joined() {
			t.Fatal("foreign observation settled or changed the original Unknown")
		}
		wrong, found, err := owner.Lookup(ctxFor(t), q.ExecutionID, id[ec.Round](t))
		requireCode(t, err, f.ConfirmationStale)
		if found || wrong != nil || owner.Joined() {
			t.Fatal("another key settled the original pending reservation")
		}
		observed, found, err = owner.Lookup(ctxFor(t), q.ExecutionID, q.RoundInputID)
		readFileAdapterOK(t, err, "original complete candidate Lookup after Stop")
		if !found || finalCalls != 2 || v.durable(t) != committed {
			t.Fatal("original Lookup rewrote or repeated the reservation attempt")
		}
		v.requireReceipt(t, observed, committed)
		requireSameRoundModelClaim(t, foreign, observed)
		stopRoundModelCallReservationOwner(t, observer)
		stopRoundModelCallReservationOwner(t, owner)
		v.requireUnchanged(t)
		t.Log("round_model_call_reservation actual_rollback_zero_rows=true physical_commit_before_ack_loss=true stop_waits_original_return=true original_pending_lookup_only=true immutable_reservation_rows=1 reservation_model_calls=0 reservation_wire_requests=0 invocation_usage_unchanged=true setup_model_calls=1 setup_wire_requests=1")
	})
}
