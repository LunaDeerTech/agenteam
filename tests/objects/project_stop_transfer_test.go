//go:build integration

package objects_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func stopTransferFixture(t *testing.T) (*transferFixture, *objectStopAuthority) {
	t.Helper()
	base := newTransferFixture(t)
	authority := newObjectStopAuthority(t, base.fixture, base.store)
	f := transferOn(t, base, base.store, base.config, func(a *object.Authorizations, _ *audit.Authorizations) { a.ProjectStop = authority })
	return f, authority
}

func TestObjectProjectStopTransferRequiresRemoteRetirement(t *testing.T) {
	f, _ := stopTransferFixture(t)
	stored := f.fixture.put(t, "remote-get-source", "body")
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), stored.Meta.ID.String())
	spec, err := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Owner: f.owner, Direction: oc.TransferGET, ObjectID: stored.Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "remote-get"), spec)
	if err != nil {
		t.Fatal(err)
	}
	actor, archive := activateObjectStop(t, f.fixture, oc.ProjectStopArchive)
	stopUntilSettled(t, f.service, actor, archive)
	material, err := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if err != nil {
		t.Fatal(err)
	}
	status, payload := transferRequest(t, f, material, nil)
	if status != 200 || string(payload) != "body" {
		t.Fatal("archive revoked legal remote GET", status)
	}
	actor, deleted := activateObjectStop(t, f.fixture, oc.ProjectStopDelete)
	for range 6 {
		report, err := f.service.RequestProjectStop(contextFor(t), actor, deleted)
		if err != nil || report.Details().State != oc.ProjectStopPending {
			t.Fatal("local Issue return substituted for remote retirement", err)
		}
	}
	partial := f.evidence(t, grant, oc.TransferStoppedEvidence, false)
	if _, err = f.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, partial); err != nil {
		t.Fatal(err)
	}
	report, err := f.service.InspectProjectStop(contextFor(t), actor, deleted)
	if err != nil || report.Details().State != oc.ProjectStopPending {
		t.Fatal("partial remote evidence retired grant", err)
	}
	f.sql(t, `UPDATE object_fixture.transfer_evidence SET joined=true,closed=true WHERE id=$1`, partial.ID.String())
	if _, err = f.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, partial); err != nil {
		t.Fatal(err)
	}
	stopUntilSettled(t, f.service, actor, deleted)
	var active bool
	if err = f.store.QueryRow(contextFor(t), `SELECT l.state='active' FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&active); err != nil || active {
		t.Fatal("stopped while actual remote lease remained", err)
	}
}

func TestObjectProjectStopTransferLatePUTCannotPublish(t *testing.T) {
	base := newTransferFixture(t)
	authority := newObjectStopAuthority(t, base.fixture, base.store)
	proxy := newTransferProxy(t, base.fixture)
	f := transferOn(t, base, base.store, proxy.config(t, base.fixture), func(a *object.Authorizations, _ *audit.Authorizations) { a.ProjectStop = authority })
	body := []byte(strings.Repeat("late remote payload", 12000))
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "old-put-grant"), f.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	material, err := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if err != nil {
		t.Fatal(err)
	}
	proxy.mode.Store(transferHoldStagePut)
	request, err := http.NewRequestWithContext(contextFor(t), material.Method, material.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header = material.Headers.Clone()
	request.ContentLength = int64(len(body))
	type putOutcome struct {
		status int
		err    error
	}
	arrived := make(chan putOutcome, 1)
	go func() {
		response, err := (&http.Client{Transport: f.transport}).Do(request)
		if err != nil {
			arrived <- putOutcome{err: err}
			return
		}
		_, err = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		arrived <- putOutcome{response.StatusCode, err}
	}()
	proxy.wait(t)
	if proxy.payloadWritten.Load() != int64(len(body)/2) {
		t.Fatal("actual remote half-body barrier not reached")
	}
	actor, cause := activateObjectStop(t, f.fixture, oc.ProjectStopArchive)
	for range 6 {
		report, err := f.service.RequestProjectStop(contextFor(t), actor, cause)
		if err != nil || report.Details().State != oc.ProjectStopPending {
			t.Fatal("remote PUT was assumed joined", err)
		}
	}
	// The already signed original grant can still arrive after local revocation.
	// Its real storage success must not revive any local publish capability.
	select {
	case <-arrived:
		t.Fatal("stop incorrectly ended remote in-flight PUT")
	default:
	}
	proxy.unblock()
	select {
	case outcome := <-arrived:
		if outcome.err != nil || outcome.status != 200 {
			t.Fatal("actual late signed PUT did not finish", outcome.status, outcome.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("remote PUT failed to join")
	}
	f.sql(t, `UPDATE object_fixture.projects SET state='active',operation_id=NULL,version=version+1`)
	completed := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	if _, err = f.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, completed); err == nil {
		t.Fatal("Restore revived late original PUT grant")
	}
	var published bool
	if err = f.store.QueryRow(contextFor(t), `SELECT state='available' FROM agenteam_object.objects WHERE id=$1`, grant.Status.ObjectID.String()).Scan(&published); err != nil || published {
		t.Fatal("late revoked PUT published canonical content", err)
	}
	// The old stop cause is also stale even though its technical pending facts
	// must remain available to the next accepted lifecycle operation.
	if _, err = f.service.RequestProjectStop(contextFor(t), actor, cause); err == nil {
		t.Fatal("Restore allowed old stop authority")
	}
}
