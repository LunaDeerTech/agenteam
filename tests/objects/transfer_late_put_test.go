//go:build integration

package objects_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func TestTransferSlowOriginalPUTCannotRestoreMarker(t *testing.T) {
	f := newTransferFixture(t)
	proxy := newTransferProxy(t, f.fixture)
	through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
	body := bytes.Repeat([]byte("s"), 512<<10)
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "slow-put"), f.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	proxy.mode.Store(transferHoldStagePut)
	request, err := http.NewRequestWithContext(contextFor(t), material.Method, material.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal("invalid material")
	}
	request.Header = material.Headers.Clone()
	request.ContentLength = int64(len(body))
	type response struct {
		status int
		err    error
	}
	done := make(chan response, 1)
	go func() {
		r, e := (&http.Client{Transport: f.transport}).Do(request)
		if e != nil {
			done <- response{err: e}
			return
		}
		_, e = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		done <- response{r.StatusCode, e}
	}()
	proxy.wait(t)
	if proxy.payloadWritten.Load() != int64(len(body)/2) {
		t.Fatal("fixture did not stop after an actual half-body socket write", proxy.payloadWritten.Load())
	}
	type cancelled struct {
		state oc.TransferStatusView
		err   error
	}
	cancelledDone := make(chan cancelled, 1)
	cancelCommand := command(t, "cancel-slow")
	go func() {
		s, e := through.transfers.CancelTransfer(contextFor(t), f.actor, grant.Status.ID, cancelCommand)
		cancelledDone <- cancelled{s, e}
	}()
	wait, stop := context.WithTimeout(contextFor(t), 3*time.Second)
	defer stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for proxy.stagePUT.Load() < 2 {
		select {
		case <-tick.C:
		case <-wait.Done():
			t.Fatal("parallel marker not attempted")
		case c := <-cancelledDone:
			t.Fatal("cancel returned before marker", c.err)
		}
	}
	var gated, cleaned bool
	if err = f.store.QueryRow(contextFor(t), `SELECT a.cleanup_gate,a.phase='cleaned' FROM agenteam_object.upload_attempts a JOIN agenteam_object.object_transfers t ON t.staging_id=a.id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&gated, &cleaned); err != nil || !gated || cleaned {
		t.Fatal("exact cleanup gate must precede competing marker", err)
	}
	// On this real server the competing marker waits behind the old request.
	// Releasing the old body allows both requests to finish; the marker must then
	// eliminate the payload without treating this as remote lease retirement.
	select {
	case <-done:
		t.Fatal("slow original unexpectedly ended before release")
	default:
	}
	proxy.unblock()
	select {
	case got := <-done:
		if got.err != nil || got.status != http.StatusOK && got.status != http.StatusPreconditionFailed {
			t.Fatal("original conditional PUT did not terminate", got.status)
		}
		t.Logf("original conditional PUT final status=%d", got.status)
	case <-time.After(5 * time.Second):
		t.Fatal("original request not joined")
	}
	var state oc.TransferStatusView
	select {
	case c := <-cancelledDone:
		state, err = c.state, c.err
	case <-time.After(5 * time.Second):
		t.Fatal("competing marker did not converge")
	}
	if err != nil || !state.LeaseActive || state.Cleanup != oc.CleanupPending {
		t.Fatal("marker inferred external retirement", err)
	}
	u, _ := url.Parse(material.URL)
	key := strings.TrimPrefix(u.Path, "/"+f.bucket+"/")
	stored, err := f.s3.GetObject(contextFor(t), f.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal("marker unavailable")
	}
	raw, err := io.ReadAll(stored)
	_ = stored.Close()
	if err != nil || len(raw) != 0 || digest(raw) != digest(nil) {
		t.Fatal("late PUT restored payload", err)
	}
	proxy.mode.Store(transferPass)
	if status, _ := transferRequest(t, through, material, body); status != 412 {
		t.Fatal("old bearer restored payload after marker", status)
	}
	terminal := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
	state, err = through.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, terminal)
	if err != nil || state.LeaseActive || state.Cleanup != oc.CleanupCompleted {
		t.Fatal("actual terminal proof did not retire", err)
	}
}
