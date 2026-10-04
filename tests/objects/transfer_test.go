//go:build integration

package objects_test

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func transferRequest(t *testing.T, f *transferFixture, material oc.RunnerTransferMaterial, body []byte) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if material.Method == http.MethodPut {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(contextFor(t), material.Method, material.URL, reader)
	if err != nil {
		t.Fatal("material URL invalid")
	}
	request.Header = material.Headers.Clone()
	if v := request.Header.Get("Content-Length"); v != "" {
		request.ContentLength, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatal("invalid declared length")
		}
	}
	response, err := (&http.Client{Transport: f.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		t.Fatal("owned transfer request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		t.Fatal("owned transfer body failed")
	}
	return response.StatusCode, raw
}
func TestTransferPUTCompleteAndIndependentRetirement(t *testing.T) {
	f := newTransferFixture(t)
	body := []byte("runner output must become a different immutable object key")
	spec := f.spec(t, body)
	issue := command(t, "issue-output")
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	material, err := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(material.URL)
	if material.Method != http.MethodPut || parsed.Query().Get("X-Amz-SignedHeaders") != "content-length;content-type;host;if-none-match;x-amz-checksum-sha256" {
		t.Fatal("incomplete wire headers")
	}
	if material.WireExpiresAt.Time().After(grant.Status.ExpiresAt.Time()) {
		t.Fatal("wire deadline exceeded durable deadline")
	}
	if status, _ := transferRequest(t, f, material, body); status != http.StatusOK {
		t.Fatalf("PUT status=%d", status)
	}
	if status, _ := transferRequest(t, f, material, body); status != http.StatusPreconditionFailed {
		t.Fatalf("conditional replay status=%d", status)
	}
	// Both RequestIDs and the authenticated Session may change without changing
	// the stable output semantic or extending its original deadline.
	session := id[identity.Session](t)
	user, _ := foundation.ParseID[identity.User](f.actor.Details().UserID)
	f.sql(t, `INSERT INTO object_fixture.sessions(id,user_id) VALUES($1,$2)`, session.String(), user.String())
	actor, _ := identity.NewHuman(user, session)
	details := spec.Details()
	upload := *details.UploadCommand
	upload.RequestID = id[foundation.Request](t)
	details.UploadCommand = &upload
	spec2, _ := oc.NewTransferSpec(details)
	replay, err := f.transfers.IssueTransfer(contextFor(t), actor, command(t, "issue-output"), spec2)
	if err != nil || replay.Status.ID != grant.Status.ID || replay.Status.ExpiresAt != grant.Status.ExpiresAt {
		t.Fatalf("stable issue replay: %v", err)
	}
	completion := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	result, err := f.transfers.CompleteTransfer(contextFor(t), actor, grant.Status.ID, completion)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if result.State != oc.TransferComplete || !result.LeaseActive {
		t.Fatalf("completion must not retire external lease: %+v", result)
	}
	var key string
	var phases string
	if err = f.store.QueryRow(contextFor(t), `SELECT o.candidate_key,string_agg(a.kind||':'||a.phase,',' ORDER BY a.ordinal) FROM agenteam_object.objects o JOIN agenteam_object.upload_attempts a ON a.object_id=o.id WHERE o.id=$1 GROUP BY o.candidate_key`, grant.Status.ObjectID.String()).Scan(&key, &phases); err != nil {
		t.Fatal(err)
	}
	if parsed.Path == "/"+f.bucket+"/"+key || key == "" {
		t.Fatal("staging became available locator")
	}
	t.Logf("persisted attempts %s", phases)
	// If completed replay were to read staging, its permanent zero marker would
	// fail the original nonempty digest. The original result must still replay.
	result, err = f.transfers.CompleteTransfer(contextFor(t), actor, grant.Status.ID, completion)
	if err != nil || result.State != oc.TransferComplete {
		t.Fatalf("completed replay: %v", err)
	}
	partial := f.evidence(t, grant, oc.TransferStoppedEvidence, false)
	result, err = f.transfers.ConfirmStopped(contextFor(t), actor, grant.Status.ID, partial)
	if err != nil || !result.LeaseActive {
		t.Fatalf("incomplete terminal facts retired lease: %v", err)
	}
	terminal := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
	result, err = f.transfers.ConfirmStopped(contextFor(t), actor, grant.Status.ID, terminal)
	if err != nil || result.LeaseActive {
		t.Fatalf("retirement: %v state=%+v", err, result)
	}
	if status, _ := transferRequest(t, f, material, body); status != http.StatusPreconditionFailed {
		t.Fatalf("marker did not block old grant: %d", status)
	}
	var audits int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE resource_id=$1`, grant.Status.ID.String()).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("issue+complete Audit count %d", audits)
	}
}
func TestTransferGETCompletionAndCurrentAuthority(t *testing.T) {
	f := newTransferFixture(t)
	body := "protected get object"
	put := f.put(t, "get-object", body)
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	spec, err := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Owner: f.owner, Direction: oc.TransferGET, ObjectID: put.Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "issue-get"), spec)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	status, raw := transferRequest(t, f, material, nil)
	if status != 200 || string(raw) != body {
		t.Fatalf("GET status=%d len=%d", status, len(raw))
	}
	ev := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	state, err := f.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, ev)
	if err != nil || state.State != oc.TransferComplete || !state.LeaseActive {
		t.Fatalf("complete: %v", err)
	}
	f.sql(t, `UPDATE object_fixture.runners SET generation=generation+1 WHERE id=$1`, f.runner.String())
	_, err = f.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
	requireCode(t, err, foundation.Forbidden)
}

func TestTransferRetirementCheckpointWaitsForReaderAndCurrentAuthority(t *testing.T) {
	f := newTransferFixture(t)
	body := bytes.Repeat([]byte("x"), 256<<10)
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "reader-proof"), f.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, f, material, body); status != 200 {
		t.Fatal("fixture PUT failed")
	}
	completed := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	if _, err = f.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, completed); err != nil {
		t.Fatal(err)
	}
	reader, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, grant.Status.ObjectID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var active int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active' AND owner_kind='reader'`, grant.Status.ObjectID.String()).Scan(&active); err != nil || active != 1 {
		t.Fatal("exact reader lease not active", err)
	}
	retirement := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
	state, err := f.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, retirement)
	if err != nil || !state.LeaseActive || state.State != oc.TransferComplete {
		t.Fatalf("reader retirement fence: %v", err)
	}
	var completionID, retirementID string
	if err = f.store.QueryRow(contextFor(t), `SELECT completed_evidence::text,retirement_evidence::text FROM agenteam_object.object_transfers WHERE id=$1`, grant.Status.ID.String()).Scan(&completionID, &retirementID); err != nil || completionID != completed.ID.String() || retirementID != retirement.ID.String() {
		t.Fatal("independent proof checkpoint missing", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	f.sql(t, `UPDATE object_fixture.runners SET generation=2 WHERE id=$1`, f.runner.String())
	requireCode(t, f.transfers.Recover(contextFor(t)), foundation.Forbidden)
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_object.object_transfers t ON t.lease_id=l.id WHERE t.id=$1 AND l.state='active'`, grant.Status.ID.String()).Scan(&active); err != nil || active != 1 {
		t.Fatal("wrong current authority retired lease", err)
	}
	f.sql(t, `UPDATE object_fixture.runners SET generation=1 WHERE id=$1`, f.runner.String())
	f.sql(t, `UPDATE object_fixture.transfer_evidence SET closed=false WHERE id=$1`, retirement.ID.String())
	if err = f.transfers.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_object.object_transfers t ON t.lease_id=l.id WHERE t.id=$1 AND l.state='active'`, grant.Status.ID.String()).Scan(&active); err != nil || active != 1 {
		t.Fatal("withdrawn current proof retired the external lease", err)
	}
	f.sql(t, `UPDATE object_fixture.transfer_evidence SET closed=true WHERE id=$1`, retirement.ID.String())
	if err = f.transfers.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	state, err = f.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
	if err != nil || state.LeaseActive || state.State != oc.TransferComplete || state.Cleanup != oc.CleanupCompleted {
		t.Fatalf("joined worker retirement: %+v %v", state, err)
	}
	// This assertion distinguishes the permanent marker from an original payload
	// which would coincidentally also reject a second conditional PUT.
	parsed, _ := url.Parse(material.URL)
	key := parsed.Path[len("/"+f.bucket+"/"):]
	object, err := f.s3.GetObject(contextFor(t), f.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal("marker GET failed")
	}
	raw, err := io.ReadAll(io.LimitReader(object, 1))
	_ = object.Close()
	if err != nil || len(raw) != 0 {
		t.Fatal("stage was not actually cleared")
	}
}

func TestTransferPartialTerminalCheckpointRevalidatesBeforeRetirement(t *testing.T) {
	f := newTransferFixture(t)
	put := f.put(t, "partial-get", "trusted input")
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Direction: oc.TransferGET, Owner: f.owner, ObjectID: put.Meta.ID})
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "partial"), spec)
	if err != nil {
		t.Fatal(err)
	}
	partial := f.evidence(t, grant, oc.TransferStoppedEvidence, false)
	state, err := f.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, partial)
	if err != nil || !state.LeaseActive {
		t.Fatal("partial proof was treated as terminal", err)
	}
	var pending string
	if err = f.store.QueryRow(contextFor(t), `SELECT retirement_pending::text FROM agenteam_object.object_transfers WHERE id=$1 AND retirement_evidence IS NULL AND completed_evidence IS NULL`, grant.Status.ID.String()).Scan(&pending); err != nil || pending != partial.ID.String() {
		t.Fatal("partial checkpoint missing", err)
	}
	if err = f.transfers.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	state, err = f.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
	if err != nil || !state.LeaseActive || state.State != oc.TransferPending {
		t.Fatal("incomplete evidence invented outcome", err)
	}
	f.sql(t, `UPDATE object_fixture.transfer_evidence SET joined=true,closed=true WHERE id=$1`, partial.ID.String())
	if err = f.transfers.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	state, err = f.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
	if err != nil || state.LeaseActive || state.State != oc.TransferPending {
		t.Fatal("new current terminal evidence not applied independently", err)
	}
}
