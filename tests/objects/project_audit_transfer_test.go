//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func auditGETSpec(t *testing.T, f *transferFixture, oid oc.ObjectID) oc.TransferSpec {
	t.Helper()
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2) ON CONFLICT DO NOTHING`, f.operation.String(), oid.String())
	s, err := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Direction: oc.TransferGET, Owner: f.owner, ObjectID: oid})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestObjectProjectAuditTransferPUTNestedRollbackAndGETCompleteCancel(t *testing.T) {
	t.Run("put-two-audits-one-tx", func(t *testing.T) {
		base := newTransferFixture(t)
		proxy := newTransferProxy(t, base.fixture)
		cfg, payloadPUTs := auditPayloadProxy(t, base.fixture, proxy.server.URL)
		f := objectAuditOn(t, base.fixture, objectAuditOptions{config: &cfg, transfer: base})
		tf := f.transfer
		body := []byte("audit private candidate")
		spec := tf.spec(t, body)
		grant, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "audit-put"), spec)
		if err != nil {
			t.Fatal(err)
		}
		material, err := grant.Material.ForRunner(tf.runner, grant.Status.ID)
		if err != nil {
			t.Fatal(err)
		}
		if code, _ := transferRequest(t, tf, material, body); code != 200 {
			t.Fatal(code)
		}
		evidence := tf.evidence(t, grant, oc.TransferCompletedEvidence, false)
		var inner foundation.Tx
		outer := false
		f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
			switch e.Fields().Action {
			case ac.ObjectUploadComplete:
				inner = tx
			case ac.ObjectTransferComplete:
				if tx != inner {
					t.Error("nested publication did not share transfer Tx")
					return fault(foundation.InvalidState)
				}
				x, er := f.dbstore.InTx(tx)
				if er != nil {
					return er
				}
				var published bool
				er = x.QueryRow(ctx, `SELECT t.phase='complete' AND u.state='committed' AND o.state='available' AND a.phase='published' FROM agenteam_object.object_transfers t JOIN agenteam_object.uploads u ON u.id=t.upload_id JOIN agenteam_object.objects o ON o.id=t.object_id JOIN agenteam_object.upload_attempts a ON a.id=t.candidate_id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&published)
				if er != nil {
					return er
				}
				if !published {
					return fault(foundation.InvalidState)
				}
				if er = f.checker.CheckProjectAuditInTx(ctx, tx, e, k); er != nil {
					return er
				}
				outer = true
				return fault(foundation.Forbidden)
			}
			return nil
		})
		_, err = tf.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, evidence)
		requireCode(t, err, foundation.Forbidden)
		if !outer || objectAuditCount(t, f.fixture, ac.ObjectUploadComplete) != 0 || objectAuditCount(t, f.fixture, ac.ObjectTransferComplete) != 0 {
			t.Fatal("two Audit/publication transaction escaped rollback")
		}
		var verified bool
		if err = f.store.QueryRow(contextFor(t), `SELECT a.phase='verified' AND o.state<>'available' AND t.phase<>'complete' FROM agenteam_object.object_transfers t JOIN agenteam_object.upload_attempts a ON a.id=t.candidate_id JOIN agenteam_object.objects o ON o.id=t.object_id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&verified); err != nil || !verified {
			t.Fatal("independent verified candidate lost", err)
		}
		gets, puts := payloadPUTs.getBytes.Load(), payloadPUTs.puts.Load()
		rawGets, rawPuts := proxy.stageGET.Load(), proxy.candidatePUT.Load()
		f.tap.set(nil)
		state, err := tf.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, evidence)
		if err != nil || state.State != oc.TransferComplete {
			t.Fatal("real candidate retry", err)
		}
		t.Logf("verified retry wire: raw stage GET %d->%d, raw candidate PUT %d->%d; payload GET bytes %d->%d, nonempty PUT %d->%d", rawGets, proxy.stageGET.Load(), rawPuts, proxy.candidatePUT.Load(), gets, payloadPUTs.getBytes.Load(), puts, payloadPUTs.puts.Load())
		if payloadPUTs.getBytes.Load() != gets || payloadPUTs.puts.Load() != puts {
			t.Fatal("verified retry repeated actual bytes")
		}
		if objectAuditCount(t, f.fixture, ac.ObjectUploadComplete) != 1 || objectAuditCount(t, f.fixture, ac.ObjectTransferComplete) != 1 {
			t.Fatal("exact nested Audits absent")
		}
		if _, err = tf.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, evidence); err != nil {
			t.Fatal(err)
		}
		if objectAuditCount(t, f.fixture, ac.ObjectTransferComplete) != 1 {
			t.Fatal("complete replay duplicated Audit")
		}
	})
	t.Run("get-complete-then-cancel", func(t *testing.T) {
		base := newTransferFixture(t)
		f := objectAuditOn(t, base.fixture, objectAuditOptions{transfer: base})
		tf := f.transfer
		put := f.put(t, "audit-get-source", "body")
		spec := auditGETSpec(t, tf, put.Meta.ID)
		grant, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "audit-get"), spec)
		if err != nil {
			t.Fatal(err)
		}
		material, _ := grant.Material.ForRunner(tf.runner, grant.Status.ID)
		if code, b := transferRequest(t, tf, material, nil); code != 200 || string(b) != "body" {
			t.Fatal("real GET failed", code)
		}
		ev := tf.evidence(t, grant, oc.TransferCompletedEvidence, false)
		if _, err = tf.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, ev); err != nil {
			t.Fatal(err)
		}
		f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
			if e.Fields().Action != ac.ObjectTransferRevoke {
				return nil
			}
			x, er := f.dbstore.InTx(tx)
			if er != nil {
				return er
			}
			var valid bool
			er = x.QueryRow(ctx, `SELECT phase='complete' AND revoked_at IS NOT NULL FROM agenteam_object.object_transfers WHERE id=$1`, grant.Status.ID.String()).Scan(&valid)
			if er != nil {
				return er
			}
			if !valid {
				return fault(foundation.InvalidState)
			}
			return nil
		})
		if _, err = tf.transfers.CancelTransfer(contextFor(t), f.actor, grant.Status.ID, command(t, "audit-cancel")); err != nil {
			t.Fatal(err)
		}
		if objectAuditCount(t, f.fixture, ac.ObjectTransferRevoke) != 1 {
			t.Fatal("complete-phase revoke rejected")
		}
		f.sql(t, `UPDATE object_fixture.runners SET generation=generation+1`)
		_, err = tf.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
		requireCode(t, err, foundation.Forbidden)
	})
}

func TestObjectProjectAuditCleanupBatchRevokeRollsBackSecondAudit(t *testing.T) {
	base := newTransferFixture(t)
	f := objectAuditOn(t, base.fixture, objectAuditOptions{transfer: base})
	tf := f.transfer
	put := f.put(t, "batch-source", "body")
	spec := auditGETSpec(t, tf, put.Meta.ID)
	for _, key := range []string{"batch-one", "batch-two"} {
		if _, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, key), spec); err != nil {
			t.Fatal(err)
		}
	}
	op := id[oc.CleanupOperation](t)
	f.sql(t, `UPDATE object_fixture.projects SET state='deleting',operation_id=$1`, op.String())
	c, _ := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: f.project, OperationID: op, Version: 1})
	calls := 0
	f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
		if e.Fields().Action != ac.ObjectTransferRevoke {
			return nil
		}
		calls++
		x, er := f.dbstore.InTx(tx)
		if er != nil {
			return er
		}
		var n int
		if er = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_transfers WHERE revoked_at IS NOT NULL`).Scan(&n); er != nil {
			return er
		}
		if n != 2 {
			t.Error("Audit preceded full original UPDATE", n)
			return fault(foundation.InvalidState)
		}
		if calls == 2 {
			return fault(foundation.Forbidden)
		}
		return nil
	})
	_, err := f.service.CleanupProject(contextFor(t), f.actor, c)
	requireCode(t, err, foundation.Forbidden)
	var revoked, refs int
	var cleaning bool
	if err = f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.object_transfers WHERE revoked_at IS NOT NULL),(SELECT count(*) FROM agenteam_object.object_references),cleaning FROM agenteam_object.objects WHERE id=$1`, put.Meta.ID.String()).Scan(&revoked, &refs, &cleaning); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || revoked != 0 || refs == 0 || cleaning || objectAuditCount(t, f.fixture, ac.ObjectTransferRevoke) != 0 {
		t.Fatal("batch revoke/Audit/refs/gate failed to roll back", calls, revoked, refs, cleaning)
	}
	f.tap.set(nil)
	report, err := f.service.CleanupProject(contextFor(t), f.actor, c)
	if err != nil || report.State != oc.CleanupPending {
		t.Fatal("remote leases not retained", err)
	}
	if objectAuditCount(t, f.fixture, ac.ObjectTransferRevoke) != 2 {
		t.Fatal("batch retry lost original Audit keys")
	}
}

func TestObjectProjectAuditStopRevokeRollbackAndTerminalInspect(t *testing.T) {
	for _, action := range []oc.ProjectStopAction{oc.ProjectStopArchive, oc.ProjectStopDelete} {
		t.Run(string(action), func(t *testing.T) {
			base := newTransferFixture(t)
			stop := newObjectStopAuthority(t, base.fixture, base.store)
			f := objectAuditOn(t, base.fixture, objectAuditOptions{transfer: base, stop: stop})
			tf := f.transfer
			body := []byte("remote stop audit")
			grant, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "stop-put"), tf.spec(t, body))
			if err != nil {
				t.Fatal(err)
			}
			// A real admitted preparation remains alive until confirmed gate commit.
			prepared, err := f.service.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
			if err != nil {
				t.Fatal(err)
			}
			defer f.service.DiscardPrepared(prepared)
			actor, c := activateObjectStop(t, f.fixture, action)
			f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
				if e.Fields().Action != ac.ObjectTransferRevoke {
					return nil
				}
				x, er := f.dbstore.InTx(tx)
				if er != nil {
					return er
				}
				var valid bool
				er = x.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND phase='issued' AND NOT cleanup_gate FROM agenteam_object.object_transfers WHERE id=$1`, grant.Status.ID.String()).Scan(&valid)
				if er != nil {
					return er
				}
				if !valid {
					return fault(foundation.InvalidState)
				}
				if er = f.checker.CheckProjectAuditInTx(ctx, tx, e, k); er != nil {
					return er
				}
				return fault(foundation.Forbidden)
			})
			_, err = f.service.RequestProjectStop(contextFor(t), actor, c)
			requireCode(t, err, foundation.Forbidden)
			var stops, revoked, joined int
			if err = f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.project_stops),(SELECT count(*) FROM agenteam_object.object_transfers WHERE revoked_at IS NOT NULL),(SELECT count(*) FROM agenteam_object.project_work WHERE kind='preparation' AND joined_at IS NOT NULL)`).Scan(&stops, &revoked, &joined); err != nil {
				t.Fatal(err)
			}
			if stops != 0 || revoked != 0 || joined != 0 || objectAuditCount(t, f.fixture, ac.ObjectTransferRevoke) != 0 {
				t.Fatal("stop gate failed to roll back or cancelled preparation", stops, revoked, joined)
			}
			f.tap.set(nil)
			report, err := f.service.RequestProjectStop(contextFor(t), actor, c)
			if err != nil && codeOf(err) != foundation.ResourceBusy || report.Details().State != oc.ProjectStopPending {
				t.Fatal("remote lease inferred stopped", err)
			}
			if err = f.service.DiscardPrepared(prepared); err != nil {
				t.Fatal(err)
			}
			auditRetireTransfer(t, tf, grant)
			stopUntilSettled(t, f.service, actor, c)
			f.sql(t, `UPDATE object_fixture.stop_causes SET phase='complete'`)
			before := objectAuditCount(t, f.fixture, ac.ObjectTransferRevoke)
			report, err = f.service.InspectProjectStop(contextFor(t), actor, c)
			if err != nil || report.Details().State != oc.ProjectStopped {
				t.Fatal("terminal Inspect", err)
			}
			if before != 1 || objectAuditCount(t, f.fixture, ac.ObjectTransferRevoke) != before {
				t.Fatal("terminal Inspect appended new Audit")
			}
		})
	}
}

func TestObjectProjectAuditStopGateActualCommitUnknownNeverCancels(t *testing.T) {
	for _, mode := range []string{"committed", "rollback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			base := newTransferFixture(t)
			raw, proxy := proxyStore(t, base.fixture, mode == "committed")
			proxy.late.Store(mode == "pending")
			stop := newObjectStopAuthority(t, base.fixture, raw)
			wrapped := &objectAuditCommitStore{Store: raw, proxy: proxy}
			f := objectAuditOn(t, base.fixture, objectAuditOptions{pg: raw, store: wrapped, stop: stop, transfer: base})
			tf := f.transfer
			body := []byte("real stop unknown")
			grant, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "unknown-stop-put"), tf.spec(t, body))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := f.service.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
			if err != nil {
				t.Fatal(err)
			}
			defer f.service.DiscardPrepared(prepared)
			actor, c := activateObjectStop(t, base.fixture, oc.ProjectStopArchive)
			wrapped.arm(ac.ObjectTransferRevoke, grant.Status.ID.String(), true)
			done := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
				defer cancel()
				_, er := f.service.RequestProjectStop(ctx, actor, c)
				done <- er
			}()
			select {
			case <-proxy.reached:
			case <-time.After(3 * time.Second):
				t.Fatal("exact Audit stop gate COMMIT not intercepted")
			}
			wrapped.assertHit(t, base.fixture, mode == "pending")
			if mode != "pending" {
				close(proxy.release)
			}
			select {
			case err = <-done:
				requireCode(t, err, foundation.CommitUnknown)
			case <-time.After(3 * time.Second):
				t.Fatal("Unknown did not return")
			}
			// A completed API response must not cancel the live preparation even when
			// an independent observer knows that the original DB transaction committed.
			var joined, revoked int
			if err = base.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.project_work WHERE kind='preparation' AND joined_at IS NOT NULL),(SELECT count(*) FROM agenteam_object.object_transfers WHERE revoked_at IS NOT NULL)`).Scan(&joined, &revoked); err != nil {
				t.Fatal(err)
			}
			if joined != 0 || mode == "committed" && revoked != 1 || mode != "committed" && revoked != 0 {
				t.Fatal("Unknown changed cancellation/visible outcome", mode, joined, revoked)
			}
			if mode == "pending" {
				close(proxy.release)
				select {
				case <-proxy.committed:
				case <-time.After(3 * time.Second):
					t.Fatal("original pending writer never committed")
				}
			}
			wrapped.waitWriter(t, base.fixture)
			// Current permission is still required after the original writer ends.
			f.sql(t, `UPDATE object_fixture.stop_causes SET manifest=false`)
			if _, err = f.service.RequestProjectStop(contextFor(t), actor, c); err == nil {
				t.Fatal("stale stop authorization accepted")
			}
			f.sql(t, `UPDATE object_fixture.stop_causes SET manifest=true`)
			report, err := f.service.RequestProjectStop(contextFor(t), actor, c)
			if err != nil && codeOf(err) != foundation.ResourceBusy || report.Details().State != oc.ProjectStopPending {
				t.Fatal("confirmed retry", err)
			}
			if err = f.service.DiscardPrepared(prepared); err != nil {
				t.Fatal(err)
			}
			auditRetireTransfer(t, tf, grant)
			stopUntilSettled(t, f.service, actor, c)
			if objectAuditCount(t, f.fixture, ac.ObjectTransferRevoke) != 1 {
				t.Fatal("Unknown retry duplicated revoke Audit")
			}
		})
	}
}

func TestObjectProjectAuditLateGETCompleteRetainsConvergeCompatibility(t *testing.T) {
	for _, stage := range []string{"retired", "cleaning", "deleted"} {
		t.Run(stage, func(t *testing.T) {
			base := newTransferFixture(t)
			f := objectAuditOn(t, base.fixture, objectAuditOptions{transfer: base})
			tf := f.transfer
			put := f.put(t, "late-get-source", "body")
			spec := auditGETSpec(t, tf, put.Meta.ID)
			grant, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "late-get"), spec)
			if err != nil {
				t.Fatal(err)
			}
			material, _ := grant.Material.ForRunner(tf.runner, grant.Status.ID)
			if code, raw := transferRequest(t, tf, material, nil); code != 200 || string(raw) != "body" {
				t.Fatal("real GET failed", code)
			}
			completed := tf.evidence(t, grant, oc.TransferCompletedEvidence, false)
			if stage != "cleaning" {
				terminal := tf.evidence(t, grant, oc.TransferStoppedEvidence, true)
				state, er := tf.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, terminal)
				if er != nil || state.LeaseActive || state.State == oc.TransferComplete {
					t.Fatal("real retirement prerequisite", er)
				}
			}
			if stage != "retired" {
				p := ownerPlan(t, f.service, f.actor, f.owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: put.Meta.ID})
				r := plannedTx(f.dbstore, f.service, contextFor(t), cause(t), p, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
					return f.service.ReleaseObjectInTx(ctx, tx, f.actor, f.owner, put.Meta.ID, p, l)
				})
				if r.State() != foundation.Committed {
					t.Fatal(r.Fault())
				}
				op := id[oc.CleanupOperation](t)
				f.sql(t, `INSERT INTO object_fixture.cleanup VALUES($1,$2,$3)`, op.String(), put.Meta.ID.String(), f.owner.Details().ID)
				cleanup, er := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: op, Owner: f.owner, Reason: oc.OwnerDeleted})
				if er != nil {
					t.Fatal(er)
				}
				state, er := f.service.DeleteUnreferenced(contextFor(t), cleanup, put.Meta.ID)
				if er != nil {
					t.Fatal(er)
				}
				want := oc.CleanupPending
				if stage == "deleted" {
					want = oc.CleanupCompleted
				}
				if state.State != want {
					t.Fatal("real cleanup prerequisite", state.State)
				}
			}
			var phase, lease, objectState string
			var cleaning bool
			if err = f.store.QueryRow(contextFor(t), `SELECT t.phase,l.state,o.state,o.cleaning FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id JOIN agenteam_object.objects o ON o.id=t.object_id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&phase, &lease, &objectState, &cleaning); err != nil {
				t.Fatal(err)
			}
			t.Logf("late GET prerequisite: stage=%s phase=%s lease=%s object=%s cleaning=%t", stage, phase, lease, objectState, cleaning)
			state, err := tf.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, completed)
			if err != nil || state.State != oc.TransferComplete {
				t.Fatalf("legitimate late GET completion rejected at %s: %v", stage, err)
			}
			if objectAuditCount(t, f.fixture, ac.ObjectTransferComplete) != 1 {
				t.Fatal("late completion Audit missing")
			}
		})
	}
}

func TestObjectProjectAuditIssueActualCommitUnknownNeverLeaksMaterial(t *testing.T) {
	for _, mode := range []string{"committed", "rollback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			base := newTransferFixture(t)
			raw, proxy := proxyStore(t, base.fixture, mode == "committed")
			proxy.late.Store(mode == "pending")
			wrapped := &objectAuditCommitStore{Store: raw, proxy: proxy}
			f := objectAuditOn(t, base.fixture, objectAuditOptions{pg: raw, store: wrapped, transfer: base})
			tf := f.transfer
			spec := tf.spec(t, []byte("unknown issue"))
			var transfer oc.TransferID
			f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
				if e.Fields().Action == ac.ObjectTransferIssue {
					var err error
					transfer, err = foundation.ParseID[oc.Transfer](e.Fields().Resource.Details().ID)
					if err != nil {
						return err
					}
					wrapped.arm(ac.ObjectTransferIssue, transfer.String(), false)
				}
				return nil
			})
			type outcome struct {
				grant oc.TransferGrant
				err   error
			}
			done := make(chan outcome, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
				defer cancel()
				grant, err := tf.transfers.IssueTransfer(ctx, f.actor, command(t, "unknown-issue"), spec)
				done <- outcome{grant, err}
			}()
			select {
			case <-proxy.reached:
			case <-time.After(3 * time.Second):
				t.Fatal("Issue Audit COMMIT not intercepted")
			}
			wrapped.assertHit(t, base.fixture, mode == "pending")
			if mode != "pending" {
				close(proxy.release)
			}
			select {
			case out := <-done:
				requireCode(t, out.err, foundation.CommitUnknown)
				if _, err := out.grant.Material.ForRunner(tf.runner, transfer); err == nil {
					t.Fatal("Unknown returned usable material")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Issue Unknown hung")
			}
			if mode == "pending" {
				close(proxy.release)
				select {
				case <-proxy.committed:
				case <-time.After(3 * time.Second):
					t.Fatal("original Issue writer did not commit")
				}
			}
			wrapped.waitWriter(t, base.fixture)
			f.tap.set(nil)
			var count int
			if err := base.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_transfers WHERE id=$1`, transfer.String()).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "rollback" {
				want = 0
			}
			if count != want || objectAuditCount(t, f.fixture, ac.ObjectTransferIssue) != want {
				t.Fatal("original Issue outcome not atomic", count, want)
			}
			f.sql(t, `UPDATE object_fixture.sessions SET active=false`)
			if _, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "unknown-issue"), spec); err == nil {
				t.Fatal("current permission bypassed by Issue history")
			}
		})
	}
}

func TestObjectProjectAuditPUTFinalCommitUnknownKeepsBothAudits(t *testing.T) {
	base := newTransferFixture(t)
	raw, commit := proxyStore(t, base.fixture, true)
	wire := newTransferProxy(t, base.fixture)
	cfg, counts := auditPayloadProxy(t, base.fixture, wire.server.URL)
	wrapped := &objectAuditCommitStore{Store: raw, proxy: commit}
	f := objectAuditOn(t, base.fixture, objectAuditOptions{pg: raw, store: wrapped, config: &cfg, transfer: base})
	tf := f.transfer
	body := []byte("two audits committed together")
	grant, err := tf.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "unknown-final-put"), tf.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(tf.runner, grant.Status.ID)
	if code, _ := transferRequest(t, tf, material, body); code != 200 {
		t.Fatal(code)
	}
	evidence := tf.evidence(t, grant, oc.TransferCompletedEvidence, false)
	wrapped.arm(ac.ObjectTransferComplete, grant.Status.ID.String(), false)
	done := make(chan error, 1)
	go func() {
		_, err := tf.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, evidence)
		done <- err
	}()
	select {
	case <-commit.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("final PUT two-Audit transaction not intercepted")
	}
	wrapped.assertHit(t, base.fixture, false)
	var exact bool
	if err = base.store.QueryRow(contextFor(t), `SELECT t.phase='complete' AND u.state='committed' AND o.state='available' AND t.xmin=u.xmin AND t.xmin=o.xmin AND (SELECT count(*) FROM agenteam_audit.audit_records a WHERE a.xmin=t.xmin AND ((a.action='object.transfer.complete' AND a.resource_id=t.id) OR (a.action='object.upload.complete' AND a.resource_id=o.id)))=2 FROM agenteam_object.object_transfers t JOIN agenteam_object.uploads u ON u.id=t.upload_id JOIN agenteam_object.objects o ON o.id=t.object_id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&exact); err != nil || !exact {
		t.Fatal("original final PUT commit identity mismatch", err)
	}
	close(commit.release)
	select {
	case err = <-done:
		requireCode(t, err, foundation.CommitUnknown)
	case <-time.After(3 * time.Second):
		t.Fatal("final PUT Unknown did not return")
	}
	wrapped.waitWriter(t, base.fixture)
	gets, puts := counts.getBytes.Load(), counts.puts.Load()
	state, err := tf.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, evidence)
	if err != nil || state.State != oc.TransferComplete {
		t.Fatal("final PUT replay", err)
	}
	if counts.getBytes.Load() != gets || counts.puts.Load() != puts || objectAuditCount(t, f.fixture, ac.ObjectTransferComplete) != 1 || objectAuditCount(t, f.fixture, ac.ObjectUploadComplete) != 1 {
		t.Fatal("Unknown final replay repeated bytes/Audit")
	}
}
