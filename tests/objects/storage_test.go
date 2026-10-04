//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectExistingUploadReadReplayAndCurrentPermission(t *testing.T) {
	f := newFixture(t, false)
	first := f.put(t, "first", "hello storage")
	if first.Meta.State != oc.Available || first.Receipt.Validate() == nil || first.Meta.SHA256 != digest([]byte("hello storage")) {
		t.Fatal("published facts incorrect")
	}
	replay := f.put(t, "first", "hello storage")
	if replay.Meta.ID != first.Meta.ID {
		t.Fatal("replay changed object")
	}
	_, err := f.service.PutObject(contextFor(t), f.actor, f.owner, command(t, "first"), "text/plain", 13, nil, io.NopCloser(strings.NewReader("other payload")))
	requireCode(t, err, foundation.IdempotencyKeyReused)
	r, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, first.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r)
	if err != nil || string(body) != "hello storage" {
		t.Fatalf("read %q %v", body, err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	var audit, active int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='object.upload.complete'`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if audit != 1 {
		t.Fatal("replay duplicated Audit")
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE state='active'`).Scan(&active); err != nil || active != 0 {
		t.Fatal("joined read leaked lease", err)
	}
	f.sql(t, `UPDATE object_fixture.sessions SET active=false`)
	_, err = f.service.LookupPut(contextFor(t), f.actor, f.owner, "first")
	requireCode(t, err, foundation.SessionRevoked)
}
func TestObjectProspectiveReceiptConsumeAndCancel(t *testing.T) {
	f := newFixture(t, true)
	first := f.put(t, "reserved", "reserved bytes")
	if first.Receipt.Validate() != nil {
		t.Fatal("prospective receipt missing")
	}
	_, err := f.service.StatObject(contextFor(t), f.actor, f.owner, first.Meta.ID)
	requireCode(t, err, foundation.Forbidden)
	source, err := f.service.OpenUploadSource(contextFor(t), f.actor, f.owner, first.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(source)
	if err != nil || string(raw) != "reserved bytes" {
		t.Fatal("source copy failed", err)
	}
	_ = source.Close()
	f.sql(t, `UPDATE object_fixture.owners SET existence='existing'`)
	var reference oc.ObjectReference
	accessPlan1 := ownerPlan(t, f.service, f.actor, f.owner, oc.ConsumeAccess, oc.AccessRequestDetails{Receipt: first.Receipt})
	result := plannedTx(f.store, f.service, contextFor(t), cause(t), accessPlan1, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		var e error
		reference, e = f.service.ConsumeUploadInTx(ctx, tx, f.actor, f.owner, first.Receipt, plan, locked)
		return e
	})
	if result.State() != foundation.Committed || reference.Kind != oc.CanonicalReference {
		t.Fatal(result.Fault())
	}
	_, err = f.service.CancelUpload(contextFor(t), f.actor, f.owner, "reserved")
	requireCode(t, err, foundation.InvalidState)
	other := newFixture(t, true)
	upload := other.put(t, "cancel", "remove me")
	cancelled, err := other.service.CancelUpload(contextFor(t), other.actor, other.owner, "cancel")
	if err != nil || cancelled.State != oc.UploadRevoked || cancelled.Cleanup != oc.CleanupCompleted {
		t.Fatal("cancel incomplete", err, cancelled.Cleanup)
	}
	lookup, err := other.service.LookupPut(contextFor(t), other.actor, other.owner, "cancel")
	if err != nil || lookup.State != oc.UploadRevoked || lookup.Meta != nil || lookup.Receipt.Validate() == nil {
		t.Fatal("revoked receipt exposed", err)
	}
	_, err = other.service.OpenUploadSource(contextFor(t), other.actor, other.owner, upload.Receipt)
	if err == nil {
		t.Fatal("revoked source opened")
	}
	_, err = other.service.PutObject(contextFor(t), other.actor, other.owner, command(t, "cancel"), "text/plain", 9, nil, io.NopCloser(strings.NewReader("remove me")))
	requireCode(t, err, foundation.ResourceDeleted)
}
