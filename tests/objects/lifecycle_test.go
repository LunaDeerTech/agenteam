//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestObjectGracefulDrainKeepsAdmittedReaderAlive(t *testing.T) {
	f := newFixture(t, false)
	body := strings.Repeat("old-stream", 30000)
	stored := f.put(t, "reader-drain", body)
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	proxy.mode.Store(proxyHoldReadBody)
	reader, err := service.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-proxy.began
	service.StopAdmission()
	_, err = service.StatObject(contextFor(t), f.actor, f.owner, stored.Meta.ID)
	requireCode(t, err, foundation.ShuttingDown)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.Drain(ctx) }()
	select {
	case err = <-done:
		t.Fatal("drain returned with live reader", err)
	default:
	}
	proxy.release()
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != body {
		t.Fatal("admitted reader cancelled by first stop", err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("drain did not join")
	}
	var leases int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE state='active'`).Scan(&leases); err != nil || leases != 0 {
		t.Fatal("drain left reader lease", err)
	}
}
func TestObjectForceClosesRealReadAndSharesCleanupDeadline(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "reader-force", strings.Repeat("force-stream", 30000))
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	proxy.mode.Store(proxyHoldReadBody)
	reader, err := service.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); readDone <- err }()
	<-proxy.began
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = service.Force(ctx); err != nil {
		t.Fatal("force failed to join within its shared context", err)
	}
	if time.Since(started) > 1100*time.Millisecond {
		t.Fatal("force added per-resource budget")
	}
	select {
	case err = <-readDone:
		if err == nil {
			t.Fatal("truncated reader succeeded")
		}
	case <-ctx.Done():
		t.Fatal("source read did not join")
	}
	_ = reader.Close()
	select {
	case <-proxy.finished:
	case <-ctx.Done():
		t.Fatal("owned real peer not closed")
	}
	var leases int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE state='active'`).Scan(&leases); err != nil || leases != 0 {
		t.Fatal("force released wrong lease state", err)
	}
}

type preparationPipe struct {
	*io.PipeReader
	began chan struct{}
	once  sync.Once
}

func (p *preparationPipe) Read(b []byte) (int, error) {
	p.once.Do(func() { close(p.began) })
	return p.PipeReader.Read(b)
}
func TestObjectForceJoinsActualPreparationPipe(t *testing.T) {
	f := newFixture(t, false)
	reader, writer := io.Pipe()
	defer writer.Close()
	source := &preparationPipe{PipeReader: reader, began: make(chan struct{})}
	result := make(chan error, 1)
	ctx := contextFor(t)
	cmd := command(t, "force-prepare")
	go func() {
		_, err := f.service.PutObject(ctx, f.actor, f.owner, cmd, "text/plain", 100, nil, source)
		result <- err
	}()
	select {
	case <-source.began:
	case <-ctx.Done():
		t.Fatal("prepare body never started")
	}
	force, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.service.Force(force); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("partial body published")
		}
	case <-force.Done():
		t.Fatal("preparation did not join")
	}
	var rows int64
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.objects`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("partial prepare reserved database object", err)
	}
}
func TestObjectForceCancelsAlreadyFreshMarkerContext(t *testing.T) {
	f := newFixture(t, true)
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	proxy.mode.Store(proxyLosePutResponse)
	if _, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "force-marker"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body"))); err != nil {
		t.Fatal(err)
	}
	proxy.mode.Store(proxyHoldMarkerResponse)
	result := make(chan error, 1)
	ctx := contextFor(t)
	go func() { _, err := service.CancelUpload(ctx, f.actor, f.owner, "force-marker"); result <- err }()
	select {
	case <-proxy.upstreamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("actual marker was not applied")
	}
	started := time.Now()
	force, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Force(force); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 1100*time.Millisecond {
		t.Fatal("fresh marker context escaped force budget")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("withheld marker response claimed completion")
		}
	case <-force.Done():
		t.Fatal("marker operation outlived force")
	}
	proxy.mode.Store(proxyPass)
	restarted, _ := f.on(t, f.store, proxy.server.URL, nil)
	if err := restarted.Recover(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	r, err := restarted.LookupPut(contextFor(t), f.actor, f.owner, "force-marker")
	if err != nil || r.Cleanup != "completed" || proxy.deletes.Load() != 0 {
		t.Fatal("forced marker checkpoint lost", err)
	}
}
