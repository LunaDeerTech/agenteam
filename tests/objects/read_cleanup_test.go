//go:build integration

package objects_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

type repeated byte

func (r repeated) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}
func TestObjectStreamingRangeMissingAndIntegrity(t *testing.T) {
	f := newFixture(t, false)
	const size = 64 << 20
	hash := sha256.New()
	if _, err := io.CopyBuffer(hash, io.LimitReader(repeated('q'), size), make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	expected := foundation.Digest("sha256:" + hex.EncodeToString(hash.Sum(nil)))
	result, err := f.service.PutObject(contextFor(t), f.actor, f.owner, command(t, "large"), "application/octet-stream", size, &expected, io.NopCloser(io.LimitReader(repeated('q'), size)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, result.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	outHash := sha256.New()
	n, err := io.CopyBuffer(outHash, r, make([]byte, 31<<10))
	if err != nil || n != size || hex.EncodeToString(outHash.Sum(nil)) != expected.String()[7:] {
		t.Fatal("full streaming digest mismatch", err)
	}
	_ = r.Close()
	r, err = f.service.ReadObject(contextFor(t), f.actor, f.owner, result.Meta.ID, &oc.ByteRange{Offset: size - 10, Length: 100})
	if err != nil {
		t.Fatal(err)
	}
	part, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(part, bytes.Repeat([]byte{'q'}, 10)) || r.Range().Length != 10 {
		t.Fatal("range facts invalid", err)
	}
	_ = r.Close()
	_, err = f.service.ReadObject(contextFor(t), f.actor, f.owner, result.Meta.ID, &oc.ByteRange{Offset: size, Length: 1})
	requireCode(t, err, foundation.RangeNotSatisfiable)
	small := f.put(t, "small", "small private body")
	key := f.physical(t, small.Meta.ID)
	if err = f.s3.RemoveObject(contextFor(t), f.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatal("fixture remove failed")
	}
	if _, err = f.service.StatObject(contextFor(t), f.actor, f.owner, small.Meta.ID); err != nil {
		t.Fatal("metadata stat accessed storage", err)
	}
	_, err = f.service.ReadObject(contextFor(t), f.actor, f.owner, small.Meta.ID, nil)
	requireCode(t, err, foundation.ObjectPayloadMissing)
	_, err = f.s3.PutObject(contextFor(t), f.bucket, key, strings.NewReader("wrong private body"), int64(len("wrong private body")), minio.PutObjectOptions{DisableMultipart: true})
	if err != nil {
		t.Fatal("owned corruption preparation failed")
	}
	_, err = f.service.ReadObject(contextFor(t), f.actor, f.owner, small.Meta.ID, nil)
	requireCode(t, err, foundation.ObjectIntegrityMismatch)
	medium := f.put(t, "medium", strings.Repeat("x", 256<<10))
	key = f.physical(t, medium.Meta.ID)
	_, err = f.s3.PutObject(contextFor(t), f.bucket, key, io.LimitReader(repeated('y'), 256<<10), 256<<10, minio.PutObjectOptions{DisableMultipart: true})
	if err != nil {
		t.Fatal("owned corruption preparation failed")
	}
	r, err = f.service.ReadObject(contextFor(t), f.actor, f.owner, medium.Meta.ID, nil)
	if err != nil {
		t.Fatal("large corrupt prefix was not streamed", err)
	}
	n, err = io.Copy(io.Discard, r)
	requireCode(t, err, foundation.ObjectIntegrityMismatch)
	if n == 0 || n > (256<<10)-(64<<10) {
		t.Fatal("integrity holdback did not retain the tail")
	}
	_ = r.Close()
}
func TestObjectCancelProtectsActualSourceUntilJoined(t *testing.T) {
	f := newFixture(t, true)
	put := f.put(t, "source", strings.Repeat("s", 256<<10))
	key := f.physical(t, put.Meta.ID)
	reader, err := f.service.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.CancelUpload(contextFor(t), f.actor, f.owner, "source")
	if err != nil || result.Cleanup != oc.CleanupPending {
		t.Fatal("source did not protect cleanup", err)
	}
	if _, err = f.s3.StatObject(contextFor(t), f.bucket, key, minio.StatObjectOptions{}); err != nil {
		t.Fatal("payload removed while reader active")
	}
	_, err = f.service.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt)
	if err == nil {
		t.Fatal("new source admitted after cancellation")
	}
	n, err := io.Copy(io.Discard, reader)
	if err != nil || n != 256<<10 {
		t.Fatal("existing source did not finish", err)
	}
	_ = reader.Close()
	if err = f.service.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	lookup, err := f.service.LookupPut(contextFor(t), f.actor, f.owner, "source")
	if err != nil || lookup.Cleanup != oc.CleanupCompleted {
		t.Fatal("cleanup did not resume after actual join", err)
	}
}
func TestObjectProtectedLeaseAndProjectCleanup(t *testing.T) {
	f := newFixture(t, false)
	put := f.put(t, "leased", "lease protected bytes")
	use := id[struct{}](t)
	owner, err := oc.NewLeaseOwner(oc.HistoryOwner, use.String())
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, `INSERT INTO object_fixture.uses(id,object_id,kind,owner_id) VALUES($1,$2,'history',$3)`, use.String(), put.Meta.ID.String(), f.owner.Details().ID)
	var lease oc.ObjectLease
	accessPlan1 := leasePlan(t, f.service, f.actor, put.Meta.ID, owner, oc.AcquireUseAccess)
	result := plannedTx(f.store, f.service, contextFor(t), cause(t), accessPlan1, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		var e error
		lease, e = f.service.AcquireLeaseInTx(ctx, tx, f.actor, put.Meta.ID, owner, plan, locked)
		return e
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	f.sql(t, `UPDATE object_fixture.uses SET lease_id=$1`, lease.ID.String())
	accessPlan2 := ownerPlan(t, f.service, f.actor, f.owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: put.Meta.ID})
	result = plannedTx(f.store, f.service, contextFor(t), cause(t), accessPlan2, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		return f.service.ReleaseObjectInTx(ctx, tx, f.actor, f.owner, put.Meta.ID, plan, locked)
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	if _, err = f.service.StatObject(contextFor(t), f.actor, f.owner, put.Meta.ID); err != nil {
		t.Fatal("exact protected fixed object rejected", err)
	}
	f.sql(t, `UPDATE object_fixture.uses SET active=false`)
	_, err = f.service.StatObject(contextFor(t), f.actor, f.owner, put.Meta.ID)
	requireCode(t, err, foundation.Forbidden)
	operation := id[oc.CleanupOperation](t)
	f.sql(t, `UPDATE object_fixture.projects SET state='deleting',operation_id=$1`, operation.String())
	cleanup, err := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: f.project, OperationID: operation, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.service.CleanupProject(contextFor(t), f.actor, cleanup)
	if err != nil || pending.State != oc.CleanupPending || pending.Remaining != 1 {
		t.Fatal("unknown history lease guessed terminal", err)
	}
	f.sql(t, `UPDATE object_fixture.uses SET terminal=true`)
	accessPlan3 := leasePlan(t, f.service, f.actor, put.Meta.ID, owner, oc.ReleaseUseAccess)
	result = plannedTx(f.store, f.service, contextFor(t), cause(t), accessPlan3, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		return f.service.ReleaseLeaseInTx(ctx, tx, f.actor, put.Meta.ID, owner, plan, locked)
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	done, err := f.service.CleanupProject(contextFor(t), f.actor, cleanup)
	if err != nil || done.State != oc.CleanupCompleted || done.Remaining != 0 {
		t.Fatal("Project cleanup incomplete", err)
	}
	var records int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1`, f.project.String()).Scan(&records); err != nil || records != 0 {
		t.Fatal("Project metadata retained", err)
	}
}
