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
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectRuntimeSharesWorkerSlotWhenServiceStartsFirst(t *testing.T) {
	f := newFixture(t, true)
	proxy := newTransferProxy(t, f)
	copy := *f
	copy.config = proxy.config(t, f)
	process := id[oc.Process](t)
	runtime, service, _ := runtimeAt(t, &copy, filepath.Join(t.TempDir(), "spool"), process, f.authority)
	if err := runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("w", 256<<10)
	put, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "shared-worker"), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := service.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := service.CancelUpload(contextFor(t), f.actor, f.owner, "shared-worker"); err != nil || out.Cleanup != oc.CleanupPending {
		t.Fatal("reader did not protect pending setup", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	proxy.mode.Store(transferHoldCleanup)
	worker, cancel := context.WithCancel(contextFor(t))
	defer cancel()
	if err = service.StartMaintenance(worker); err != nil {
		t.Fatal(err)
	}
	proxy.wait(t)
	requireCode(t, runtime.StartMaintenance(worker), foundation.InvalidState)
	service.StopAdmission()
	drain, endDrain := context.WithTimeout(contextFor(t), 100*time.Millisecond)
	err = service.Drain(drain)
	endDrain()
	if err == nil {
		t.Fatal("original Service.Drain ignored real maintenance I/O")
	}
	var claim string
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&claim); err != nil || claim != "claimed" {
		t.Fatal("unjoined worker released process claim", err)
	}
	force, endForce := context.WithTimeout(contextFor(t), time.Second)
	defer endForce()
	if err = runtime.Force(force); err != nil {
		t.Fatal("shared worker failed bounded force/join", err)
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&claim); err != nil || claim != "stopped" || runtime.MaintenanceStatus().Running {
		t.Fatal("joined shared worker was not finalized", err)
	}
}

func TestObjectRuntimeRecoveryCleanupKeepsStartupAndWorkerBudget(t *testing.T) {
	for _, worker := range []bool{false, true} {
		t.Run(map[bool]string{false: "startup", true: "worker"}[worker], func(t *testing.T) {
			f := newFixture(t, true)
			proxy := newTransferProxy(t, f)
			copy := *f
			copy.config = proxy.config(t, f)
			runtime, service, _ := runtimeAt(t, &copy, filepath.Join(t.TempDir(), "spool"), id[oc.Process](t), f.authority)
			writer := f.service
			if worker {
				if err := runtime.Initialize(contextFor(t)); err != nil {
					t.Fatal(err)
				}
				writer = service
			}
			body := strings.Repeat("x", 256<<10)
			put, err := writer.PutObject(contextFor(t), f.actor, f.owner, command(t, "recovery-budget"), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
			if err != nil {
				t.Fatal(err)
			}
			reader, err := writer.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			if out, err := writer.CancelUpload(contextFor(t), f.actor, f.owner, "recovery-budget"); err != nil || out.Cleanup != oc.CleanupPending {
				t.Fatal("pending setup", err)
			}
			if err = reader.Close(); err != nil {
				t.Fatal(err)
			}
			proxy.mode.Store(transferHoldCleanup)
			ctx, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			done := make(chan error, 1)
			if worker {
				if err = runtime.StartMaintenance(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				go func() { done <- runtime.Initialize(ctx) }()
			}
			select {
			case <-proxy.reached:
			case <-time.After(13 * time.Second):
				t.Fatal("actual recovery cleanup not reached")
			}
			cancel()
			join, cancelJoin := context.WithTimeout(contextFor(t), time.Second)
			defer cancelJoin()
			if worker {
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for runtime.MaintenanceStatus().Running {
					select {
					case <-tick.C:
					case <-join.Done():
						t.Fatal("worker cleanup renewed cancelled budget")
					}
				}
			} else {
				select {
				case err = <-done:
					if err == nil {
						t.Fatal("cancelled startup became ready")
					}
				case <-join.Done():
					t.Fatal("startup cleanup renewed cancelled budget")
				}
			}
			var pending bool
			if err = f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND phase='applying')`, put.Meta.ID.String()).Scan(&pending); err != nil || !pending {
				t.Fatal("unconfirmed cleanup checkpoint discarded", err)
			}
		})
	}
}

func TestObjectRuntimeMetadataGateUntilActualProbeAndRecovery(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "confirmed", false: "cancelled"}[success], func(t *testing.T) {
			f := newFixture(t, false)
			put := f.put(t, "before-runtime", "existing body")
			proxy := newTransferProxy(t, f)
			copy := *f
			copy.config = proxy.config(t, f)
			runtime, service, _ := runtimeAt(t, &copy, filepath.Join(t.TempDir(), "spool"), id[oc.Process](t), f.authority)
			proxy.mode.Store(transferHoldProbeGet)
			ctx, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runtime.Initialize(ctx) }()
			proxy.wait(t)
			_, err := service.StatObject(contextFor(t), f.actor, f.owner, put.Meta.ID)
			requireCode(t, err, foundation.DependencyUnavailable)
			request, _ := oc.NewObjectReadAccess(oc.AccessRequestDetails{Operation: oc.StatAccess, Actor: f.actor, Owner: f.owner, ObjectID: put.Meta.ID, Intent: identity.Read})
			_, err = service.DiscoverAccess(contextFor(t), request)
			requireCode(t, err, foundation.DependencyUnavailable)
			var leases int
			if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE state='active'`).Scan(&leases); err != nil || leases != 0 {
				t.Fatal("unconfirmed runtime admitted business lease", err)
			}
			if !success {
				cancel()
			}
			proxy.unblock()
			select {
			case err = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("startup did not join")
			}
			if success {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = service.StatObject(contextFor(t), f.actor, f.owner, put.Meta.ID); err != nil {
					t.Fatal("confirmed metadata admission", err)
				}
			} else {
				if err == nil {
					t.Fatal("cancelled runtime initialized")
				}
				_, err = service.StatObject(contextFor(t), f.actor, f.owner, put.Meta.ID)
				requireCode(t, err, foundation.DependencyUnavailable)
			}
		})
	}
}

func TestObjectRuntimeForceJoinsMainAndTransferIOInOneBudget(t *testing.T) {
	f := newFixture(t, false)
	main, transfer := newTransferProxy(t, f), newTransferProxy(t, f)
	copy := *f
	copy.config = main.config(t, f)
	process := id[oc.Process](t)
	runtime, service, guard := runtimeAt(t, &copy, filepath.Join(t.TempDir(), "spool"), process, nil, transfer.server.URL)
	if err := runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.StartMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	main.mode.Store(transferHoldProbeGet)
	transfer.mode.Store(transferHoldControlGet)
	probe, check := make(chan error, 1), make(chan error, 1)
	go func() { probe <- service.ProbeStorage(contextFor(t), guard) }()
	main.wait(t)
	go func() { check <- runtime.Check(contextFor(t)) }()
	transfer.wait(t)
	runtime.StopAdmission()
	short, cancel := context.WithTimeout(contextFor(t), 40*time.Millisecond)
	if err := runtime.Drain(short); err == nil {
		t.Fatal("drain ignored two real live streams")
	}
	cancel()
	force, cancel := context.WithTimeout(contextFor(t), time.Second)
	defer cancel()
	start := time.Now()
	if err := runtime.Force(force); err != nil {
		t.Fatal("cooperative sockets did not join", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("force created an extra resource budget")
	}
	for _, done := range []chan error{probe, check} {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("forced I/O reported success")
			}
		case <-force.Done():
			t.Fatal("forced I/O not joined")
		}
	}
	// Request context cancellation on the owned server is evidence that both
	// distinct transports lost their live streams, not just their idle pool.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for main.joinedReads.Load() != 1 || transfer.joinedReads.Load() != 1 {
		select {
		case <-ticker.C:
		case <-force.Done():
			t.Fatal("owned server streams remain")
		}
	}
	var state string
	if err := f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&state); err != nil || state != "stopped" {
		t.Fatal("guard released before real joins", err)
	}
}
