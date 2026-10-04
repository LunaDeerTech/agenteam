//go:build integration

package objects_test

import (
	"io"
	"strings"
	"testing"
	"time"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func TestObjectLostPUTResponseVerifiesOriginalWithoutSecondWrite(t *testing.T) {
	f := newFixture(t, true)
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	proxy.mode.Store(proxyLosePutResponse)
	result, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "lost-response"), "text/plain", 15, nil, io.NopCloser(strings.NewReader("committed bytes")))
	if err != nil {
		t.Fatal("original key verification failed", err)
	}
	if proxy.puts.Load() != 1 || result.Meta.SHA256 != digest([]byte("committed bytes")) {
		t.Fatal("SDK resent an ambiguous physical attempt")
	}
	cancelled, err := service.CancelUpload(contextFor(t), f.actor, f.owner, "lost-response")
	if err != nil || cancelled.Cleanup != oc.CleanupCompleted {
		t.Fatal("unknown key marker cleanup failed", err)
	}
	if proxy.markers.Load() != 1 || proxy.deletes.Load() != 0 {
		t.Fatal("unknown key was deleted instead of retained as marker")
	}
}
func TestObjectLateRealPUTCannotRestoreCancelledPayload(t *testing.T) {
	f := newFixture(t, true)
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	proxy.mode.Store(proxyHoldPutBody)
	body := strings.Repeat("late-private-payload-", 14000)
	uploadDone := make(chan error, 1)
	go func() {
		_, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "late"), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
		uploadDone <- err
	}()
	select {
	case <-proxy.began:
	case <-time.After(5 * time.Second):
		t.Fatal("real PUT did not send half its body")
	}
	type cancellation struct {
		value oc.LookupResult
		err   error
	}
	cancelDone := make(chan cancellation, 1)
	go func() {
		value, err := service.CancelUpload(contextFor(t), f.actor, f.owner, "late")
		cancelDone <- cancellation{value, err}
	}()
	select {
	case <-proxy.marker:
	case value := <-cancelDone:
		t.Fatalf("cancel returned before marker: %v", value.err)
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not reach unconditional marker")
	}
	var gated bool
	var key string
	if err := f.store.QueryRow(contextFor(t), `SELECT a.cleanup_gate AND o.cleaning,a.candidate_key FROM agenteam_object.upload_attempts a JOIN agenteam_object.objects o ON o.id=a.object_id JOIN agenteam_object.uploads u ON u.id=a.upload_id WHERE u.command_key='late'`).Scan(&gated, &key); err != nil || !gated {
		t.Fatal("external marker preceded durable cleanup gate", err)
	}
	select {
	case e := <-uploadDone:
		if e == nil {
			t.Fatal("cancelled PUT reported publication")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owned SDK writer did not close/join")
	}
	proxy.release()
	select {
	case r := <-cancelDone:
		if r.err != nil || r.value.Cleanup != oc.CleanupCompleted {
			t.Fatal("marker cleanup incomplete", r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("late PUT/marker did not converge")
	}
	select {
	case <-proxy.finished:
	case <-time.After(time.Second):
		t.Fatal("remote PUT still in flight")
	}
	object, err := f.s3.GetObject(contextFor(t), f.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal("marker GET failed")
	}
	raw, err := io.ReadAll(object)
	_ = object.Close()
	if err != nil || len(raw) != 0 || digest(raw) != digest(nil) {
		t.Fatal("marker retained payload", err)
	}
	options := minio.PutObjectOptions{DisableMultipart: true, ContentType: "text/plain"}
	options.SetMatchETagExcept("*")
	_, err = f.s3.PutObject(contextFor(t), f.bucket, key, strings.NewReader(body), int64(len(body)), options)
	if minio.ToErrorResponse(err).StatusCode != 412 {
		t.Fatal("old conditional payload restored after marker")
	}
	if proxy.deletes.Load() != 0 {
		t.Fatal("technical marker was automatically deleted")
	}
}
func TestObjectMarkerResponseUnknownResumesCheckpoint(t *testing.T) {
	f := newFixture(t, true)
	proxy := newStorageProxy(t, f)
	service, _ := f.on(t, f.store, proxy.server.URL, nil)
	proxy.mode.Store(proxyLosePutResponse)
	_, err := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "marker-unknown"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if err != nil {
		t.Fatal(err)
	}
	proxy.mode.Store(proxyLoseMarkerResponse)
	result, err := service.CancelUpload(contextFor(t), f.actor, f.owner, "marker-unknown")
	if err == nil || result.Cleanup != oc.CleanupPending {
		t.Fatal("lost marker response claimed completed")
	}
	proxy.mode.Store(proxyPass)
	if err = service.Recover(contextFor(t)); err != nil {
		t.Fatal("checkpoint failed to resume", err)
	}
	lookup, err := service.LookupPut(contextFor(t), f.actor, f.owner, "marker-unknown")
	if err != nil || lookup.Cleanup != oc.CleanupCompleted {
		t.Fatal("resume incomplete", err)
	}
	if proxy.puts.Load() != 1 || proxy.markers.Load() != 2 || proxy.deletes.Load() != 0 {
		t.Fatal("cleanup replay retried business bytes or deleted marker")
	}
}
