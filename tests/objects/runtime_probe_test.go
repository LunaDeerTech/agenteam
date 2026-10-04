//go:build integration

package objects_test

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func TestObjectRuntimeProbeBothOriginsAndUnknownRecovery(t *testing.T) {
	for _, commit := range []int64{0, 3, 4, 5} {
		t.Run(strconv.FormatInt(commit, 10), func(t *testing.T) {
			f := newFixture(t, false)
			main, transfer := newTransferProxy(t, f), newTransferProxy(t, f)
			copy := *f
			copy.config = main.config(t, f)
			var proxy *commitProxy
			if commit != 0 {
				copy.store, proxy = proxyStore(t, f, true)
				copy.authority = &authority{copy.store}
			} else {
				main.mode.Store(transferLoseProbePut)
			}
			path := filepath.Join(t.TempDir(), "spool")
			old := id[oc.Process](t)
			runtime, _, _ := runtimeAt(t, &copy, path, old, nil, transfer.server.URL)
			ctx, cancel := context.WithCancel(contextFor(t))
			defer cancel()
			done := make(chan error, 1)
			if proxy != nil {
				proxy.armed.Store(commit)
			}
			go func() { done <- runtime.Initialize(ctx) }()
			if proxy != nil {
				select {
				case <-proxy.reached:
				case <-time.After(5 * time.Second):
					t.Fatal("startup COMMIT not intercepted")
				}
				cancel()
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("failed startup did not return")
			}
			if err == nil {
				t.Fatal("unknown startup reported success")
			}
			if proxy != nil {
				requireCode(t, err, foundation.CommitUnknown)
				close(proxy.release)
			}
			if commit == 3 || commit == 4 {
				if main.probePUT.Load() != 0 {
					t.Fatal("unconfirmed claim/probe admitted PUT")
				}
			}
			var keys []string
			rows, e := f.store.Query(contextFor(t), `SELECT storage_key FROM agenteam_object.startup_probes WHERE process_id=$1`, old.String())
			if e != nil {
				t.Fatal(e)
			}
			for rows.Next() {
				var key string
				if e = rows.Scan(&key); e != nil {
					t.Fatal(e)
				}
				keys = append(keys, key)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				t.Fatal(e)
			}
			if commit == 3 && len(keys) != 0 || commit != 3 && len(keys) != 1 {
				t.Fatal("wrong exact startup checkpoint count")
			}
			runtime.StopAdmission()
			if e = runtime.Drain(contextFor(t)); e != nil {
				t.Fatal("actual failed instance join", e)
			}
			main.mode.Store(transferPass)
			fresh := *f
			fresh.config = main.config(t, f)
			next, _, _ := runtimeAt(t, &fresh, path, id[oc.Process](t), nil, transfer.server.URL)
			if e = next.Initialize(contextFor(t)); e != nil {
				t.Fatal("exact old checkpoint recovery", e)
			}
			for _, key := range keys {
				var phase, mode string
				if e = f.store.QueryRow(contextFor(t), `SELECT phase,cleanup_mode FROM agenteam_object.startup_probes WHERE storage_key=$1`, key).Scan(&phase, &mode); e != nil || phase != "complete" {
					t.Fatal("old probe incomplete", e)
				}
				if commit == 5 {
					_, e = f.s3.StatObject(contextFor(t), f.bucket, key, minio.StatObjectOptions{})
					if minio.ToErrorResponse(e).Code != "NoSuchKey" || mode != "delete" {
						t.Fatal("confirmed write terminal not deleted")
					}
				} else {
					body, e := f.s3.GetObject(contextFor(t), f.bucket, key, minio.GetObjectOptions{})
					if e != nil {
						t.Fatal(e)
					}
					data, e := io.ReadAll(io.LimitReader(body, 1))
					_ = body.Close()
					if e != nil || len(data) != 0 || mode != "zero_marker" {
						t.Fatal("unknown probe not permanently fenced", e)
					}
				}
			}
			if main.probePUT.Load() == 0 || main.probeGET.Load() == 0 || main.probeDELETE.Load() == 0 || transfer.probeGET.Load() == 0 {
				t.Fatal("no actual two-origin put/get/delete probe")
			}
		})
	}
}

func TestObjectRuntimeProbeFailuresPreserveUnrelatedKey(t *testing.T) {
	f := newFixture(t, false)
	proxy := newTransferProxy(t, f)
	_, err := f.s3.PutObject(contextFor(t), f.bucket, "unrelated-technical-key", strings.NewReader("keep"), 4, minio.PutObjectOptions{DisableMultipart: true})
	if err != nil {
		t.Fatal(err)
	}
	copy := *f
	copy.config = proxy.config(t, f)
	runtime, _, _ := runtimeAt(t, &copy, filepath.Join(t.TempDir(), "spool"), id[oc.Process](t), nil)
	proxy.mode.Store(transferFailProbePut)
	if err = runtime.Initialize(contextFor(t)); err == nil {
		t.Fatal("no PUT permission passed startup")
	}
	if _, err = f.s3.StatObject(contextFor(t), f.bucket, "unrelated-technical-key", minio.StatObjectOptions{}); err != nil {
		t.Fatal("failed probe touched unrelated key")
	}
}

func TestObjectRuntimeTransferOriginRejectsDifferentControlOrProbe(t *testing.T) {
	for _, mode := range []int32{transferCorruptControlRead, transferCorruptProbeRead} {
		t.Run(map[int32]string{transferCorruptControlRead: "identity", transferCorruptProbeRead: "probe"}[mode], func(t *testing.T) {
			f := newFixture(t, false)
			main, alias := newTransferProxy(t, f), newTransferProxy(t, f)
			copy := *f
			copy.config = main.config(t, f)
			alias.mode.Store(mode)
			runtime, service, _ := runtimeAt(t, &copy, filepath.Join(t.TempDir(), "spool"), id[oc.Process](t), f.authority, alias.server.URL)
			if err := runtime.Initialize(contextFor(t)); err == nil {
				t.Fatal("different alias content admitted runtime")
			}
			requireCode(t, runtime.StartMaintenance(contextFor(t)), foundation.DependencyUnavailable)
			var ready int
			if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.startup_probes WHERE phase='complete'`).Scan(&ready); err != nil || ready != 0 {
				t.Fatal("alias failure claimed completed probe", err)
			}
			request, _ := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PrepareAccess, Actor: f.actor, Owner: f.owner, Intent: identity.Mutate})
			_, err := service.DiscoverAccess(contextFor(t), request)
			requireCode(t, err, foundation.DependencyUnavailable)
		})
	}
}
