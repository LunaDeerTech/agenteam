//go:build integration

package objects_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func runtimeFixture(t *testing.T, f *fixture) (*object.Runtime, *object.Service, oc.ProcessID) {
	t.Helper()
	process := id[oc.Process](t)
	runtime, service, _ := runtimeAt(t, f, filepath.Join(t.TempDir(), "spool"), process, nil)
	return runtime, service, process
}
func runtimeAt(t *testing.T, f *fixture, path string, process oc.ProcessID, planner oc.AccessPlanner, transferOrigins ...string) (*object.Runtime, *object.Service, *object.ProcessGuard) {
	t.Helper()
	backend, err := object.NewBackend(f.config)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(path, process)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auditing, err := audit.New(f.store, keys, audit.Authorizations{Projects: auditAuthority{f.authority}})
	if err != nil {
		t.Fatal(err)
	}
	authorizations := object.Authorizations{Processes: guard}
	if planner != nil {
		authorizations = object.Authorizations{Planner: planner, Processes: guard, Resources: f.authority, Read: f.authority, Gate: f.authority, Cleanup: f.authority, Leases: f.authority}
	}
	service, err := object.New(f.store, backend, spool, auditing, authorizations)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := object.LoadTransferEndpoint(func(name string) (string, bool) {
		if name == object.EnvironmentPrefix+"TRANSFER_ENDPOINT" && len(transferOrigins) != 0 {
			return transferOrigins[0], true
		}
		return "", false
	}, f.config)
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := object.NewTransferService(service, nil, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := object.NewRuntime(service, guard, transfers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtime.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtime.Drain(ctx); err != nil {
			_ = runtime.Force(ctx)
			t.Error(err)
		}
	})
	return runtime, service, guard
}
func TestObjectRuntimeProbeEmptyAndSingleWorker(t *testing.T) {
	f := newFixture(t, false)
	runtime, service, process := runtimeFixture(t, f)
	if err := runtime.Initialize(contextFor(t)); err != nil {
		t.Fatalf("runtime initialize: %v", err)
	}
	var phase, mode, claim string
	var closed bool
	if err := f.store.QueryRow(contextFor(t), `SELECT p.phase,p.cleanup_mode,p.io_closed,c.state FROM agenteam_object.startup_probes p JOIN agenteam_object.process_claims c ON c.process_id=p.process_id WHERE p.process_id=$1`, process.String()).Scan(&phase, &mode, &closed, &claim); err != nil {
		t.Fatal(err)
	}
	if phase != "complete" || mode != "delete" || !closed || claim != "claimed" {
		t.Fatalf("startup checkpoint %s/%s/%t/%s", phase, mode, closed, claim)
	}
	requireCode(t, service.Recover(contextFor(t)), foundation.DependencyUnbound)
	if err := runtime.StartMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireCode(t, service.StartMaintenance(context.Background()), foundation.InvalidState)
	requireCode(t, runtime.StartMaintenance(context.Background()), foundation.InvalidState)
	if err := runtime.Check(contextFor(t)); err != nil {
		t.Fatalf("runtime check: %v", err)
	}
	runtime.StopAdmission()
	if err := runtime.Drain(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	if err := f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&claim); err != nil {
		t.Fatal(err)
	}
	if claim != "stopped" {
		t.Fatal("drained process not confirmed stopped")
	}
}
func TestObjectRuntimeUnboundCannotAdoptBusinessFacts(t *testing.T) {
	f := newFixture(t, false)
	f.put(t, "requires-owner-authority", "body")
	runtime, _, _ := runtimeFixture(t, f)
	requireCode(t, runtime.Initialize(contextFor(t)), foundation.DependencyUnbound)
	requireCode(t, runtime.StartMaintenance(context.Background()), foundation.DependencyUnavailable)
}
