//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectSourceLeaseUnknownNeverSpeculatesGET(t *testing.T) {
	for _, phase := range []string{"origin_commit", "open_confirmation_commit"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t, false)
			stored := f.put(t, "source-unknown", strings.Repeat("u", 150000))
			httpProxy := newStorageProxy(t, f)
			store, pgProxy := proxyStore(t, f, true)
			service, _ := f.on(t, store, httpProxy.server.URL, nil)
			provider := newSourceAuthority(t, f, service, stored)
			provider.store = store
			reads, err := object.NewSourceReads(service, provider)
			if err != nil {
				t.Fatal(err)
			}
			var lease oc.SourceLease
			done := make(chan error, 1)
			if phase == "origin_commit" {
				pgProxy.armed.Store(1)
				go func() {
					var result foundation.CommitResult
					lease, result = acquireSourceOn(t, f, service, reads, provider.source, store, nil)
					if result.State() != foundation.Unknown {
						done <- fault(foundation.InvalidState)
						return
					}
					done <- foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
				}()
			} else {
				var result foundation.CommitResult
				lease, result = acquireSourceOn(t, f, service, reads, provider.source, store, nil)
				if result.State() != foundation.Committed {
					t.Fatal(result.Fault())
				}
				pgProxy.armed.Store(1)
				go func() {
					reader, err := reads.OpenLeasedSource(contextFor(t), f.actor, lease)
					if reader != nil {
						_ = reader.Close()
					}
					done <- err
				}()
			}
			select {
			case <-pgProxy.reached:
			case <-time.After(3 * time.Second):
				t.Fatal("actual COMMIT response not held")
			}
			if httpProxy.gets.Load() != 0 {
				t.Fatal("GET before confirmed lease transaction")
			}
			close(pgProxy.release)
			select {
			case err = <-done:
				requireCode(t, err, foundation.CommitUnknown)
			case <-time.After(3 * time.Second):
				t.Fatal("unknown did not return")
			}
			var active int
			if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases l JOIN object_fixture.copy_commands c ON c.source_lease=l.id WHERE l.id=$1 AND l.state='active'`, lease.ID().String()).Scan(&active); err != nil || active != 1 {
				t.Fatal("atomic durable checkpoint lost", err)
			}
			if httpProxy.gets.Load() != 0 {
				t.Fatal("unknown initiated GET")
			}
			// A fresh confirmed verification observes the durable source lease and
			// reauthorizes the fixed source before allowing exactly one actual GET.
			reader, err := reads.OpenLeasedSource(contextFor(t), f.actor, lease)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || len(got) != 150000 {
				t.Fatal("confirmed checkpoint recovery", err)
			}
			if httpProxy.gets.Load() != 1 {
				t.Fatal("source retry issued multiple GETs")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = reads.CancelSourceLease(ctx, lease); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestObjectSourceLeaseWaitsForOriginalWriterTerminal(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "source-held-commit", "source-body")
	httpProxy := newStorageProxy(t, f)
	store, pgProxy := proxyStore(t, f, false)
	pgProxy.late.Store(true)
	service, _ := f.on(t, store, httpProxy.server.URL, nil)
	provider := newSourceAuthority(t, f, service, stored)
	provider.store = store
	reads, err := object.NewSourceReads(service, provider)
	if err != nil {
		t.Fatal(err)
	}
	pgProxy.armed.Store(1)
	var lease oc.SourceLease
	done := make(chan foundation.CommitResult, 1)
	go func() {
		var result foundation.CommitResult
		lease, result = acquireSourceOn(t, f, service, reads, provider.source, store, nil)
		done <- result
	}()
	select {
	case <-pgProxy.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("COMMIT was not held before server")
	}
	select {
	case result := <-done:
		if result.State() != foundation.Unknown {
			t.Fatal("transport loss not unknown")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unknown did not return")
	}
	ctx, cancel := context.WithTimeout(contextFor(t), 150*time.Millisecond)
	reader, err := reads.OpenLeasedSource(ctx, f.actor, lease)
	cancel()
	if reader != nil || err == nil || httpProxy.gets.Load() != 0 {
		t.Fatal("uncommitted lease observation initiated GET")
	}
	close(pgProxy.release)
	select {
	case <-pgProxy.committed:
	case <-time.After(3 * time.Second):
		t.Fatal("held original writer did not reach terminal COMMIT")
	}
	reader, err = reads.OpenLeasedSource(contextFor(t), f.actor, lease)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != "source-body" || httpProxy.gets.Load() != 1 {
		t.Fatal("fixed original source was not recovered", err)
	}
}
