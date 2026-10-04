//go:build integration

package objects_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectProjectCleanupFairBatchesSurviveServiceRestart(t *testing.T) {
	f := newFixture(t, false)
	var objects []oc.ObjectID
	for i := range 101 {
		result := f.put(t, fmt.Sprintf("cleanup-prefix-%03d", i), "x")
		objects = append(objects, result.Meta.ID)
		if i == 100 {
			continue
		}
		use := id[struct{}](t)
		owner, _ := oc.NewLeaseOwner(oc.HistoryOwner, use.String())
		f.sql(t, `INSERT INTO object_fixture.uses(id,object_id,kind,owner_id) VALUES($1,$2,'history',$3)`, use.String(), result.Meta.ID.String(), f.owner.Details().ID)
		accessPlan1 := leasePlan(t, f.service, f.actor, result.Meta.ID, owner, oc.AcquireUseAccess)
		r := plannedTx(f.store, f.service, contextFor(t), cause(t), accessPlan1, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			_, err := f.service.AcquireLeaseInTx(ctx, tx, f.actor, result.Meta.ID, owner, plan, locked)
			return err
		})
		if r.State() != foundation.Committed {
			t.Fatal(r.Fault())
		}
	}
	var orderedFirst, orderedLast string
	if err := f.store.QueryRow(contextFor(t), `SELECT min(id::text),max(id::text) FROM agenteam_object.objects`).Scan(&orderedFirst, &orderedLast); err != nil || orderedFirst != objects[0].String() || orderedLast != objects[100].String() {
		t.Fatal("fixture order not established", err)
	}
	operation := id[oc.CleanupOperation](t)
	f.sql(t, `UPDATE object_fixture.projects SET state='deleting',operation_id=$1`, operation.String())
	request, _ := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: f.project, OperationID: operation, Version: 1})
	service := f.service
	for batch := range 3 {
		if batch == 1 {
			// A new service instance must retain the scheduling progress in SQL.
			service, _ = f.on(t, f.store, f.remote.Endpoint(), nil)
		}
		result, err := service.CleanupProject(contextFor(t), f.actor, request)
		if err != nil {
			t.Fatal("cleanup unexpectedly failed", err)
		}
		if result.State != oc.CleanupPending {
			t.Fatal("active history leases must still keep project pending")
		}
	}
	var exists, cleaning bool
	if err := f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.objects WHERE id=$1),coalesce((SELECT cleaning FROM agenteam_object.objects WHERE id=$1),false)`, objects[100].String()).Scan(&exists, &cleaning); err != nil {
		t.Fatal(err)
	}
	var protected int64
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='history' AND state='active'`).Scan(&protected); err != nil || protected != 100 {
		t.Fatal("cleanup ended a protected use", err, protected)
	}
	if exists {
		t.Fatalf("three cleanup batches revisited protected first 100 and never converged eligible object 101: cleaning=%t", cleaning)
	}
}

type stillRunningProcess struct {
	live  oc.ProcessID
	calls int
}

func (p *stillRunningProcess) ConfirmStopped(_ context.Context, id oc.ProcessID) error {
	p.calls++
	if id == p.live {
		return fault(foundation.ResourceBusy)
	}
	return fault(foundation.Forbidden)
}

func TestObjectRecoveryContinuesPastOtherLiveProcess(t *testing.T) {
	f := newFixture(t, true)
	remote := f.put(t, "foreign-live-reader", strings.Repeat("a", 256<<10))
	reader, err := f.service.OpenUploadSource(contextFor(t), f.actor, f.owner, remote.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var process string
	if err = f.store.QueryRow(contextFor(t), `SELECT process_id::text FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source' AND state='active'`, remote.Meta.ID.String()).Scan(&process); err != nil {
		t.Fatal(err)
	}
	live, _ := foundation.ParseID[oc.Process](process)
	proof := &stillRunningProcess{live: live}
	service, _ := f.on(t, f.store, f.remote.Endpoint(), proof)
	local, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "local-cleanup"), "text/plain", 256<<10, nil, io.NopCloser(strings.NewReader(strings.Repeat("b", 256<<10))))
	if err != nil {
		t.Fatal(err)
	}
	localReader, err := service.OpenUploadSource(contextFor(t), f.actor, f.owner, local.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	defer localReader.Close()
	result, err := service.CancelUpload(contextFor(t), f.actor, f.owner, "local-cleanup")
	if err != nil || result.Cleanup != oc.CleanupPending {
		t.Fatal("local cleanup gate not established", err)
	}
	if err = localReader.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_ = service.Recover(contextFor(t))
	}
	var state string
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.objects WHERE id=$1`, local.Meta.ID.String()).Scan(&state); err != nil {
		t.Fatal(err)
	}
	var active int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source' AND state='active'`, remote.Meta.ID.String()).Scan(&active); err != nil || active != 1 {
		t.Fatal("foreign live reader was released", err)
	}
	var byteRead [1]byte
	if n, err := reader.Read(byteRead[:]); err != nil || n != 1 || byteRead[0] != 'a' {
		t.Fatal("actual foreign reader is not live", err)
	}
	if proof.calls < 2 || state != string(oc.Deleted) {
		t.Fatalf("unrelated live process blocks authorized local cleanup: authority_calls=%d local_state=%s", proof.calls, state)
	}
}
