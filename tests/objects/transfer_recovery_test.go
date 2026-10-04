//go:build integration

package objects_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestTransferStoppedPUTKeepsOutputIdentityForNewAttempt(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_payload", true: "unverified_candidate"}[candidate], func(t *testing.T) {
			f := newTransferFixture(t)
			proxy := newTransferProxy(t, f.fixture)
			through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
			body := []byte("stopped attempt keeps original output")
			if !candidate {
				body = nil
			}
			spec := f.spec(t, body)
			grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "stopped-original"), spec)
			if err != nil {
				t.Fatal(err)
			}
			var complete oc.TransferEvidenceRef
			if candidate {
				m, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
				if status, _ := transferRequest(t, through, m, body); status != 200 {
					t.Fatal(status)
				}
				complete = f.evidence(t, grant, oc.TransferCompletedEvidence, false)
				proxy.mode.Store(transferRejectCandidateRead)
				_, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, complete)
				requireCode(t, err, foundation.DependencyUnavailable)
				proxy.mode.Store(transferPass)
			}
			partial := f.evidence(t, grant, oc.TransferStoppedEvidence, false)
			if _, err = through.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, partial); err != nil {
				t.Fatal(err)
			}
			var gated bool
			if err = f.store.QueryRow(contextFor(t), `SELECT cleanup_gate FROM agenteam_object.object_transfers WHERE id=$1`, grant.Status.ID.String()).Scan(&gated); err != nil || gated {
				t.Fatal("partial stopped reference erased source", err)
			}
			f.sql(t, `UPDATE object_fixture.transfer_evidence SET joined=true,closed=true WHERE id=$1`, partial.ID.String())
			if _, err = through.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, partial); err != nil {
				t.Fatal(err)
			}
			if err = through.transfers.Recover(contextFor(t)); err != nil {
				t.Fatal(err)
			}
			state, err := through.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
			if err != nil || state.State != oc.TransferFailed || state.LeaseActive {
				t.Fatal("stopped PUT never retired", err)
			}
			if candidate {
				_, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, complete)
				requireCode(t, err, foundation.InvalidState)
			}
			retry, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "new-attempt"), spec)
			if err != nil || retry.Status.ObjectID != grant.Status.ObjectID || retry.Status.ID == grant.Status.ID {
				t.Fatal("new issue changed stable output", err)
			}
			// A joined old candidate is no longer current after the new staging
			// reservation. The existing recovery protocol safely cleans it first.
			if err = through.service.Recover(contextFor(t)); err != nil {
				t.Fatal(err)
			}
			m, _ := retry.Material.ForRunner(f.runner, retry.Status.ID)
			if status, _ := transferRequest(t, through, m, body); status != 200 {
				t.Fatal(status)
			}
			proof := f.evidence(t, retry, oc.TransferCompletedEvidence, false)
			state, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, retry.Status.ID, proof)
			if err != nil || state.State != oc.TransferComplete || state.ObjectID != grant.Status.ObjectID {
				t.Fatal("same output retry did not publish", err)
			}
		})
	}
}

func TestTransferStoppedRetirementWaitsForConcurrentLocalComplete(t *testing.T) {
	f := newTransferFixture(t)
	proxy := newTransferProxy(t, f.fixture)
	through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
	body := bytes.Repeat([]byte("x"), 256<<10)
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "local-complete"), f.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, through, m, body); status != 200 {
		t.Fatal(status)
	}
	proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	proxy.mode.Store(transferHoldStageRead)
	done := make(chan error, 1)
	go func() {
		_, err := through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
		done <- err
	}()
	proxy.wait(t)
	stopped := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
	state, err := through.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, stopped)
	if err != nil || !state.LeaseActive || state.State == oc.TransferFailed {
		t.Fatal("remote terminal erased active local source", err)
	}
	var gated bool
	if err = f.store.QueryRow(contextFor(t), `SELECT cleanup_gate FROM agenteam_object.upload_attempts WHERE id=(SELECT staging_id FROM agenteam_object.object_transfers WHERE id=$1)`, grant.Status.ID.String()).Scan(&gated); err != nil || gated {
		t.Fatal("source was gated before join", err)
	}
	proxy.unblock()
	proxy.mode.Store(transferPass)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal("in-flight Complete did not finish", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Complete did not join")
	}
	if err = through.transfers.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	state, err = through.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
	if err != nil || state.LeaseActive || state.State != oc.TransferComplete {
		t.Fatal("terminal recovery replaced completed history", err)
	}
}

func TestTransferStagingAndCandidateShareTwoAttemptsAndResumeVerified(t *testing.T) {
	f := newTransferFixture(t)
	proxy := newTransferProxy(t, f.fixture)
	through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
	body := []byte("same original output after verification outage")
	spec := f.spec(t, body)
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "two-attempts"), spec)
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, through, material, body); status != 200 {
		t.Fatal(status)
	}
	proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	proxy.mode.Store(transferRejectCandidateRead)
	_, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
	requireCode(t, err, foundation.DependencyUnavailable)
	var attempts, stages, candidates int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*),count(*) FILTER(WHERE kind='runner_staging'),count(*) FILTER(WHERE kind='private_candidate') FROM agenteam_object.upload_attempts WHERE object_id=$1 AND phase NOT IN ('published','cleaned')`, grant.Status.ObjectID.String()).Scan(&attempts, &stages, &candidates); err != nil || attempts != 2 || stages != 1 || candidates != 1 {
		t.Fatal("stage/candidate admission", attempts, stages, candidates, err)
	}
	_, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
	requireCode(t, err, foundation.ResourceBusy)
	// The old formal upload wrapper shares the exact command and the same quota.
	_, err = through.service.PutObject(contextFor(t), f.actor, f.owner, *spec.Details().UploadCommand, "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(string(body))))
	requireCode(t, err, foundation.ResourceBusy)
	if proxy.candidatePUT.Load() != 1 || proxy.stageGET.Load() != 1 {
		t.Fatal("unresolved candidate produced a third attempt")
	}
	proxy.mode.Store(transferPass)
	if err = through.service.Recover(contextFor(t)); err != nil {
		t.Fatal("exact existing candidate verification", err)
	}
	state, err := through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
	if err != nil || state.State != oc.TransferComplete || state.ObjectID != grant.Status.ObjectID {
		t.Fatal("verified original resume", err)
	}
	// The additional staging GET verifies the empty marker. The payload source
	// was not reopened, and no second private candidate was sent.
	if proxy.candidatePUT.Load() != 1 || proxy.stageGET.Load() != 2 {
		t.Fatal("verified resume repeated source or payload PUT")
	}
}

func TestTransferRecoverySkipsClosedHistoryAndContinuesPastInvalidCurrent(t *testing.T) {
	f := newTransferFixture(t)
	put := f.put(t, "history-input", "same immutable input")
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Owner: f.owner, Direction: oc.TransferGET, ObjectID: put.Meta.ID, ExpiresInSeconds: 300})
	// More than one maintenance batch of real completed technical histories must
	// not starve live work after a Runner generation changes.
	for n := range 101 {
		g, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, fmt.Sprintf("history-%d", n)), spec)
		if err != nil {
			t.Fatal(err)
		}
		e := f.evidence(t, g, oc.TransferStoppedEvidence, true)
		if state, err := f.transfers.ConfirmStopped(contextFor(t), f.actor, g.Status.ID, e); err != nil || state.LeaseActive {
			t.Fatal("history did not retire", err)
		}
	}
	invalid, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "old-active"), spec)
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, `UPDATE object_fixture.runners SET generation=2 WHERE id=$1`, f.runner.String())
	current, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "new-active"), spec)
	if err != nil {
		t.Fatal(err)
	}
	partial := f.evidence(t, current, oc.TransferStoppedEvidence, false)
	if _, err = f.transfers.ConfirmStopped(contextFor(t), f.actor, current.Status.ID, partial); err != nil {
		t.Fatal(err)
	}
	f.sql(t, `UPDATE object_fixture.transfer_evidence SET joined=true,closed=true WHERE id=$1`, partial.ID.String())
	// Exact scheduling facts make the invalid live grant the first item.
	f.sql(t, `UPDATE agenteam_object.object_transfers SET recovery_pass=CASE WHEN id=$1 THEN 0 ELSE 1 END`, invalid.Status.ID.String())
	err = f.transfers.Recover(contextFor(t))
	requireCode(t, err, foundation.Forbidden)
	var oldActive, newActive bool
	if err = f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=(SELECT lease_id FROM agenteam_object.object_transfers WHERE id=$1) AND state='active'),EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=(SELECT lease_id FROM agenteam_object.object_transfers WHERE id=$2) AND state='active')`, invalid.Status.ID.String(), current.Status.ID.String()).Scan(&oldActive, &newActive); err != nil || !oldActive || newActive {
		t.Fatal("invalid item blocked unrelated current retirement or was released", oldActive, newActive, err)
	}
}

func TestTransferStagingConsumesGlobalAttemptQuota(t *testing.T) {
	f := newTransferFixture(t)
	for n := range 63 {
		if _, err := reserveOnly(t, f.fixture, f.service, f.owner, fmt.Sprintf("fill-%d", n)); err != nil {
			t.Fatal(err)
		}
	}
	spec := f.spec(t, []byte("last admission"))
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "last-stage"), spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reserveOnly(t, f.fixture, f.service, f.owner, "too-many-private"); err == nil {
		t.Fatal("staging did not consume common quota")
	} else {
		requireCode(t, err, foundation.ResourceBusy)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, f, material, []byte("last admission")); status != 200 {
		t.Fatal(status)
	}
	proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	_, err = f.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
	requireCode(t, err, foundation.ResourceBusy)
	var attempts, candidates int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*),count(*) FILTER(WHERE object_id=$1 AND kind='private_candidate') FROM agenteam_object.upload_attempts WHERE phase NOT IN ('published','cleaned')`, grant.Status.ObjectID.String()).Scan(&attempts, &candidates); err != nil || attempts != 64 || candidates != 0 {
		t.Fatal("global quota overflowed during Complete", attempts, candidates, err)
	}
}
