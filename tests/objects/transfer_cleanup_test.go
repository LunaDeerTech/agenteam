//go:build integration

package objects_test

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func TestTransferConcurrentGrantQuotaAndTerminalReuse(t *testing.T) {
	f := newTransferFixture(t)
	put := f.put(t, "quota-target", "quota body")
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Direction: oc.TransferGET, Owner: f.owner, ObjectID: put.Meta.ID, ExpiresInSeconds: 300})
	type issued struct {
		grant oc.TransferGrant
		err   error
	}
	results := make(chan issued, 36)
	var wg sync.WaitGroup
	ctx := contextFor(t)
	for i := range 36 {
		cmd := command(t, fmt.Sprintf("quota-%d", i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			grant, err := f.transfers.IssueTransfer(ctx, f.actor, cmd, spec)
			results <- issued{grant, err}
		}()
	}
	wg.Wait()
	close(results)
	var grants []oc.TransferGrant
	for result := range results {
		if result.err != nil {
			requireCode(t, result.err, foundation.ResourceBusy)
		} else {
			grants = append(grants, result.grant)
		}
	}
	if len(grants) != 32 {
		t.Fatalf("global grant limit admitted %d", len(grants))
	}
	var active int
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='transfer' AND state='active'`).Scan(&active); err != nil || active != 32 {
		t.Fatal("lease admission differed from grants", err)
	}
	stopped := f.evidence(t, grants[0], oc.TransferStoppedEvidence, true)
	state, err := f.transfers.ConfirmStopped(contextFor(t), f.actor, grants[0].Status.ID, stopped)
	if err != nil || state.LeaseActive || state.State == oc.TransferComplete {
		t.Fatal("terminal evidence must not invent completion", err)
	}
	if _, err = f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "after-terminal"), spec); err != nil {
		t.Fatal("released quota not reusable", err)
	}
}

func TestTransferProjectCleanupRetainsRemoteLeaseThenErasesFacts(t *testing.T) {
	f := newTransferFixture(t)
	proxy := newTransferProxy(t, f.fixture)
	through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
	body := []byte("project private manifest and output")
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "project-cleanup"), f.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, through, material, body); status != 200 {
		t.Fatal(status)
	}
	completed := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	if _, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, completed); err != nil {
		t.Fatal(err)
	}
	operation := id[oc.CleanupOperation](t)
	f.sql(t, `UPDATE object_fixture.projects SET state='deleting',operation_id=$1`, operation.String())
	cause, _ := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: f.project, OperationID: operation, Version: 1})
	state, err := through.service.CleanupProject(contextFor(t), f.actor, cause)
	if err != nil || state.State != oc.CleanupPending || state.Remaining != 1 {
		t.Fatal("external lease incorrectly inferred terminal", err)
	}
	var retained int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_transfers WHERE id=$1 AND revoked_at IS NOT NULL AND completed_evidence=$2`, grant.Status.ID.String(), completed.ID.String()).Scan(&retained); err != nil || retained != 1 {
		t.Fatal("recovery facts erased before lease terminal", err)
	}
	terminal := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
	if _, err = through.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, terminal); err != nil {
		t.Fatal("terminal converge on deleting project", err)
	}
	state, err = through.service.CleanupProject(contextFor(t), f.actor, cause)
	if err != nil || state.State != oc.CleanupCompleted {
		t.Fatal("final project cleanup", err)
	}
	var remaining int
	if err = f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.object_transfers)+(SELECT count(*) FROM agenteam_object.objects)+(SELECT count(*) FROM agenteam_object.upload_attempts)+(SELECT count(*) FROM agenteam_object.object_leases)`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("business storage facts retained", err)
	}
	get, put := proxy.stageGET.Load(), proxy.candidatePUT.Load()
	_, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, completed)
	requireCode(t, err, foundation.NotFound)
	_, err = through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "late-new-key"), f.spec(t, body))
	if err == nil {
		t.Fatal("deleting project recreated transfer")
	}
	if proxy.stageGET.Load() != get || proxy.candidatePUT.Load() != put {
		t.Fatal("late command resurrected storage I/O")
	}
	if status, _ := transferRequest(t, through, material, body); status != 412 {
		t.Fatal("permanent marker missing", status)
	}
	u, _ := url.Parse(material.URL)
	key := strings.TrimPrefix(u.Path, "/"+f.bucket+"/")
	remote, err := f.s3.GetObject(contextFor(t), f.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal("marker unavailable")
	}
	bytes, err := io.ReadAll(io.LimitReader(remote, 1))
	_ = remote.Close()
	if err != nil || len(bytes) != 0 {
		t.Fatal("final marker contains payload", err)
	}
	var revoked int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE resource_id=$1 AND action='object.transfer.revoke'`, grant.Status.ID.String()).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatal("revoke Audit not atomic/idempotent", err)
	}
}
