package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func transferFixture(t *testing.T) (identity.Actor, TransferSpec, TransferAccessDetails) {
	t.Helper()
	project, _ := foundation.NewID[identity.Project]()
	user, _ := foundation.NewID[identity.User]()
	session, _ := foundation.NewID[identity.Session]()
	actor, _ := identity.NewHuman(user, session)
	ownerID, _ := foundation.NewID[StoredObject]()
	owner, _ := NewObjectOwner(ExecutionPayload, ownerID.String(), project.String())
	runner, _ := foundation.NewID[Runner]()
	op, _ := foundation.NewID[Operation]()
	request, _ := foundation.NewID[foundation.Request]()
	version := foundation.Version(4)
	manifest, err := NewTransferManifest(TransferManifestDetails{MediaType: "text/plain", Length: 3, SHA256: foundation.Digest("sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewTransferSpec(TransferSpecDetails{RunnerID: runner, OperationID: op, Direction: TransferPUT, Owner: owner, Manifest: manifest, UploadCommand: &foundation.CommandMeta{RequestID: request, IdempotencyKey: "output.original", ExpectedVersion: &version}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := foundation.NewID[Transfer]()
	object, _ := foundation.NewID[StoredObject]()
	upload, _ := foundation.NewID[Upload]()
	stage, _ := foundation.NewID[Attempt]()
	lease, _ := foundation.NewID[Lease]()
	evidence, _ := foundation.NewID[TransferEvidence]()
	d := TransferAccessDetails{Operation: TransferCapture, Actor: actor, IssueCommand: foundation.CommandMeta{RequestID: request, IdempotencyKey: "issue.original"}, Spec: spec, ID: id, ObjectID: object, UploadID: upload, StagingID: stage, LeaseID: lease, Manifest: manifest, Version: 1, Evidence: &TransferEvidenceRef{ID: evidence, Kind: TransferCompletedEvidence}}
	return actor, spec, d
}
func TestTransferSpecVariantsBoundsAndCopies(t *testing.T) {
	_, spec, d := transferFixture(t)
	v := spec.Details()
	*v.UploadCommand.ExpectedVersion = 99
	v.UploadCommand.IdempotencyKey = "changed"
	if *spec.Details().UploadCommand.ExpectedVersion != 4 || spec.Details().UploadCommand.IdempotencyKey != "output.original" {
		t.Fatal("caller changed stable output")
	}
	for _, mutate := range []func(*TransferSpecDetails){func(d *TransferSpecDetails) { d.ExpiresInSeconds = -1 }, func(d *TransferSpecDetails) { d.ExpiresInSeconds = 301 }, func(d *TransferSpecDetails) {
		d.ObjectID = transferTestID[StoredObject]("019620a0-1234-7000-8000-000000000001")
	}, func(d *TransferSpecDetails) { d.UploadCommand = nil }, func(d *TransferSpecDetails) { d.Manifest = TransferManifest{} }, func(d *TransferSpecDetails) { d.Direction = "copy" }} {
		v := spec.Details()
		mutate(&v)
		if _, err := NewTransferSpec(v); err == nil {
			t.Fatal("accepted malformed PUT")
		}
	}
	get := spec.Details()
	get.Direction = TransferGET
	get.ObjectID = d.ObjectID
	get.UploadCommand = nil
	get.Manifest = TransferManifest{}
	if _, err := NewTransferSpec(get); err != nil {
		t.Fatal(err)
	}
	get.Manifest = spec.Details().Manifest
	if _, err := NewTransferSpec(get); err == nil {
		t.Fatal("GET accepted output manifest")
	}
	for _, length := range []int64{-1, MaxObjectSize + 1} {
		m := spec.Details().Manifest.Details()
		m.Length = length
		if _, err := NewTransferManifest(m); err == nil {
			t.Fatal("length bound")
		}
	}
}
func TestTransferRequestCompleteBindingAndIndependentTerminalEvidence(t *testing.T) {
	_, _, d := transferFixture(t)
	request, err := NewTransferAccessRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	evidence := *d.Evidence
	d.Evidence.Kind = TransferStoppedEvidence
	if request.Details().Evidence.Kind != TransferCompletedEvidence {
		t.Fatal("mutable evidence")
	}
	execution, _ := foundation.NewID[identity.Execution]()
	m := request.Details().Manifest.Details()
	proof := TransferCompletion{Evidence: evidence, Digest: m.SHA256, Length: m.Length, SHA256: m.SHA256}
	a, err := NewTransferAuthorization(TransferAuthorizationDetails{Request: request, RunnerGeneration: 1, OperationVersion: 2, ExecutionID: execution, Completed: &proof})
	if err != nil || !a.Matches(request) || a.Details().Retirement != nil {
		t.Fatalf("completion required unrelated retirement: %v", err)
	}
	proof.Length++
	if a.Details().Completed.Length != m.Length {
		t.Fatal("completion not copied")
	}
	copy := a.Details()
	copy.Completed.Length++
	if a.Details().Completed.Length != m.Length {
		t.Fatal("details not copied")
	}
	for _, mutate := range []func(*TransferAccessDetails){func(d *TransferAccessDetails) {
		d.Actor, _ = identity.NewHuman(transferTestID[identity.User]("019620a0-1234-7000-8000-000000000002"), transferTestID[identity.Session]("019620a0-1234-7000-8000-000000000003"))
	}, func(d *TransferAccessDetails) { d.Version++ }, func(d *TransferAccessDetails) {
		d.ObjectID = transferTestID[StoredObject]("019620a0-1234-7000-8000-000000000004")
	}, func(d *TransferAccessDetails) {
		d.StagingID = transferTestID[Attempt]("019620a0-1234-7000-8000-000000000005")
	}, func(d *TransferAccessDetails) {
		d.Evidence.ID = transferTestID[TransferEvidence]("019620a0-1234-7000-8000-000000000006")
	}} {
		next := request.Details()
		mutate(&next)
		r, err := NewTransferAccessRequest(next)
		if err != nil {
			t.Fatal(err)
		}
		if a.Matches(r) || request.Equal(r) {
			t.Fatal("authorization omitted exact field")
		}
	}
	bad := a.Details()
	bad.Completed.Length++
	if _, err := NewTransferAuthorization(bad); err == nil {
		t.Fatal("trusted completion mismatched payload")
	}
	terminal := request.Details()
	terminal.Operation = TransferConfirmTerminal
	terminal.Evidence = &TransferEvidenceRef{ID: evidence.ID, Kind: TransferStoppedEvidence}
	tr, err := NewTransferAccessRequest(terminal)
	if err != nil {
		t.Fatal(err)
	}
	for _, joined := range []bool{false, true} {
		for _, closed := range []bool{false, true} {
			_, err := NewTransferAuthorization(TransferAuthorizationDetails{Request: tr, RunnerGeneration: 1, OperationVersion: 2, ExecutionID: execution, Retirement: &TransferRetirement{Evidence: *terminal.Evidence, Digest: m.SHA256, AllRequestsJoined: joined, StorageAdmissionClosed: closed}})
			if (err == nil) != (joined && closed) {
				t.Fatal("partial proof retired lease")
			}
		}
	}
	outer, err := NewTransferAccess(request)
	if err != nil {
		t.Fatal(err)
	}
	badOuter := outer.Details()
	badOuter.Owner = request.Details().Spec.Details().Owner
	if _, err := newAccessRequest(TransferAccess, badOuter); err == nil {
		t.Fatal("mixed variant bypass")
	}
}
func TestTransferPrivateMaterialCannotLeakOrBeMutated(t *testing.T) {
	_, _, d := transferFixture(t)
	expires, _ := foundation.NewInstant(time.Now().Add(time.Minute))
	headers := http.Header{"X-Amz-Checksum-Sha256": []string{"secret-header-canary"}}
	m, err := NewTransferMaterial(d.Spec.Details().RunnerID, d.ID, http.MethodPut, "https://storage.invalid/private/canary?signature=secret-query-canary", headers, expires)
	if err != nil {
		t.Fatal(err)
	}
	headers.Set("X-Amz-Checksum-Sha256", "mutated")
	out, err := m.ForRunner(d.Spec.Details().RunnerID, d.ID)
	if err != nil || out.Headers.Get("X-Amz-Checksum-Sha256") != "secret-header-canary" {
		t.Fatal("input alias")
	}
	out.Headers.Set("X-Amz-Checksum-Sha256", "changed")
	again, _ := m.ForRunner(d.Spec.Details().RunnerID, d.ID)
	if again.Headers.Get("X-Amz-Checksum-Sha256") != "secret-header-canary" {
		t.Fatal("output alias")
	}
	other, _ := foundation.NewID[Runner]()
	if _, err = m.ForRunner(other, d.ID); err == nil {
		t.Fatal("wrong Runner projection")
	}
	for _, v := range []any{m, &m, struct{ value TransferMaterial }{m}} {
		raw, _ := json.Marshal(v)
		var log bytes.Buffer
		slog.New(slog.NewTextHandler(&log, nil)).Info("event", "value", v)
		text := fmt.Sprintf("%v %+v %#v %s %q", v, v, v, v, v) + string(raw) + log.String()
		for _, canary := range []string{"secret-header-canary", "secret-query-canary", "storage.invalid"} {
			if strings.Contains(text, canary) {
				t.Fatalf("private material leaked %s", canary)
			}
		}
	}
	for _, raw := range []string{`null`, `{}`, `"private_transfer_material"`} {
		if json.Unmarshal([]byte(raw), &m) == nil {
			t.Fatal("decoded private material")
		}
	}
}

func transferTestID[T any](raw string) foundation.ID[T] {
	id, err := foundation.ParseID[T](raw)
	if err != nil {
		panic(err)
	}
	return id
}
