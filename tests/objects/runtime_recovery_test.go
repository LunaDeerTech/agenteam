//go:build integration

package objects_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectRuntimeBusyDoesNotMaskLaterRecoveryFailure(t *testing.T) {
	f := newFixture(t, true)
	var ids []oc.ObjectID
	for _, key := range []string{"busy", "hard", "independent"} {
		body := strings.Repeat(key, 50000)
		put, err := f.service.PutObject(contextFor(t), f.actor, f.owner, command(t, key), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		reader, err := f.service.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt)
		if err != nil {
			t.Fatal(err)
		}
		cancelled, err := f.service.CancelUpload(contextFor(t), f.actor, f.owner, foundation.IdempotencyKey(key))
		if err != nil || cancelled.Cleanup != oc.CleanupPending {
			t.Fatal("protected cleanup setup", err)
		}
		if err = reader.Close(); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, put.Meta.ID)
	}
	planner := &recoveryItemPlanner{base: f.authority}
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Kind != oc.MaintenanceAccess {
			return nil
		}
		if d.ObjectID == ids[0] {
			return fault(foundation.ResourceBusy)
		}
		if d.ObjectID == ids[1] {
			return fault(foundation.Forbidden)
		}
		return nil
	})
	runtime, _, _ := runtimeAt(t, f, filepath.Join(t.TempDir(), "spool"), id[oc.Process](t), planner)
	requireCode(t, runtime.Initialize(contextFor(t)), foundation.Forbidden)
	// Public Recover keeps the original first-error contract, while Runtime
	// also observes the second item's real permission failure.
	legacy := plannedService(t, f, f.store, planner)
	requireCode(t, legacy.Recover(contextFor(t)), foundation.ResourceBusy)
	var first, second, third string
	if err := f.store.QueryRow(contextFor(t), `SELECT (SELECT state FROM agenteam_object.objects WHERE id=$1),(SELECT state FROM agenteam_object.objects WHERE id=$2),(SELECT state FROM agenteam_object.objects WHERE id=$3)`, ids[0].String(), ids[1].String(), ids[2].String()).Scan(&first, &second, &third); err != nil {
		t.Fatal(err)
	}
	if first != "available" || second != "available" || third != "deleted" {
		t.Fatal("failed items lost protection or independent cleanup was starved")
	}
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Kind == oc.MaintenanceAccess && d.ObjectID == ids[0] {
			return fault(foundation.ResourceBusy)
		}
		return nil
	})
	if err := runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal("safe pending startup", err)
	}
	if err := runtime.StartMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	before := runtime.MaintenanceStatus().CheckedAt
	planner.set(func(d oc.AccessRequestDetails) error {
		if d.Kind == oc.MaintenanceAccess && d.ObjectID == ids[0] {
			return fault(foundation.Forbidden)
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(contextFor(t), 13*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for runtime.MaintenanceStatus().CheckedAt == before {
		select {
		case <-ctx.Done():
			t.Fatal("worker did not finish bounded round")
		case <-ticker.C:
		}
	}
	if runtime.MaintenanceStatus().Healthy {
		t.Fatal("worker permission failure reported healthy")
	}
	requireCode(t, runtime.Check(contextFor(t)), foundation.DependencyUnavailable)
}
