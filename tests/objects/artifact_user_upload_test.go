//go:build integration

package objects_test

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	foundation "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type userUploadObservedBody struct {
	*bytes.Reader
	reads, closes int
}

type delayedUserUploadBody struct {
	*bytes.Reader
	reached, release chan struct{}
	once             sync.Once
}

func (b *delayedUserUploadBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.reached); <-b.release })
	return b.Reader.Read(p)
}
func (b *delayedUserUploadBody) Close() error { return nil }

// Both HTTP compositions initially observe no Object attempt. The late body's
// spool finishes only after the other writer's actual failed PUT/verification
// has returned and its persisted attempt is closed. The original Object API
// can recover that attempt, but this Human entry must not silently do so.
func TestArtifactUserUploadLateBodyCannotReplaceFailedAttempt(t *testing.T) {
	v := newArtifactFixture(t)
	ctx := contextFor(t)
	meta := command(t, "human-upload-late-body")
	payload := bytes.Repeat([]byte{0, 0xff, 1, 2}, 1024)
	intent := ac.UserUploadIntent{Display: ac.Display{Name: "original.bin"}, MediaType: "application/octet-stream", ByteSize: foundation.Progress(len(payload))}
	if _, err := v.artifact.BeginUserUpload(ctx, v.invocation(t), meta, intent); err != nil {
		t.Fatal("begin", err)
	}
	late := &delayedUserUploadBody{Reader: bytes.NewReader(payload), reached: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	open := func() { release.Do(func() { close(late.release) }) }
	done := make(chan error, 1)
	invocation := v.invocation(t)
	go func() {
		_, err := v.artifact.WriteUserUpload(ctx, invocation, meta, intent.MediaType, int64(len(payload)), late)
		done <- err
	}()
	joined := false
	t.Cleanup(func() {
		open()
		if !joined {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("late original upload did not return")
			}
		}
	})
	select {
	case <-late.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("late body did not enter original spool")
	}
	v.proxy.mode.Store(proxyRejectCandidateRead)
	_, err := v.artifact.WriteUserUpload(ctx, v.invocation(t), meta, intent.MediaType, int64(len(payload)), io.NopCloser(bytes.NewReader(payload)))
	if err == nil {
		t.Fatal("verification rejection did not reach original writer")
	}
	var closed bool
	var attempts int
	err = v.store.QueryRow(ctx, `SELECT a.io_closed,(SELECT count(*) FROM agenteam_object.upload_attempts WHERE upload_id=a.upload_id) FROM agenteam_artifact.upload_intents u JOIN agenteam_object.upload_attempts a ON a.id=u.target_attempt_id WHERE u.project_id=$1 AND u.command_key=$2 AND u.state='pending'`, v.project.String(), string(meta.IdempotencyKey)).Scan(&closed, &attempts)
	if err != nil || !closed || attempts != 1 {
		t.Fatal("original failed attempt not durably closed", err, closed, attempts)
	}
	puts := v.proxy.puts.Load()
	if puts != 1 {
		t.Fatal("expected one actual original candidate PUT", puts)
	}
	v.proxy.mode.Store(proxyPass)
	open()
	select {
	case err = <-done:
		joined = true
		requireCode(t, err, foundation.ResourceBusy)
	case <-time.After(5 * time.Second):
		t.Fatal("late original body did not return")
	}
	if v.proxy.puts.Load() != puts {
		t.Fatal("late body sent a replacement physical PUT")
	}
	var count int
	err = v.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.upload_attempts WHERE upload_id=(SELECT target_upload_id FROM agenteam_artifact.upload_intents WHERE project_id=$1 AND command_key=$2)`, v.project.String(), string(meta.IdempotencyKey)).Scan(&count)
	if err != nil || count != 1 {
		t.Fatal("late body reserved a replacement attempt", err, count)
	}
}

func (b *userUploadObservedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *userUploadObservedBody) Close() error               { b.closes++; return nil }

// Actual PG/Artifact/Object/storage with the existing private SQL authority
// fixture. Real Account/Owner wiring is separately exercised by the App test.
func TestArtifactUserUploadLiveAttemptCannotBeResent(t *testing.T) {
	v := newArtifactFixture(t)
	ctx := contextFor(t)
	meta := command(t, "human-upload-live-attempt")
	payload := bytes.Repeat([]byte{0, 0xff, 1, 2}, 4096)
	intent := ac.UserUploadIntent{Display: ac.Display{Name: "original.bin"}, MediaType: "application/octet-stream", ByteSize: foundation.Progress(len(payload))}
	out, err := v.artifact.BeginUserUpload(ctx, v.invocation(t), meta, intent)
	if err != nil || out.State != ac.UserUploadPending {
		t.Fatal("original begin", err, out.State)
	}
	v.proxy.mode.Store(proxyHoldPutBody)
	done := make(chan error, 1)
	go func() {
		result, err := v.artifact.WriteUserUpload(ctx, v.invocation(t), meta, intent.MediaType, int64(len(payload)), io.NopCloser(bytes.NewReader(payload)))
		if err == nil && result.State != ac.UserUploadUploaded {
			err = fault(foundation.InternalError)
		}
		done <- err
	}()
	joined := false
	t.Cleanup(func() {
		v.proxy.release()
		if !joined {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original upload did not return before fixture cleanup")
			}
		}
	})
	select {
	case <-v.proxy.began:
	case <-time.After(3 * time.Second):
		t.Fatal("actual candidate PUT never reached barrier")
	}
	puts := v.proxy.puts.Load()
	out, err = v.artifact.LookupUserUpload(ctx, v.invocation(t), meta, intent)
	if err != nil || out.State != ac.UserUploadPending || out.Artifact != nil {
		t.Fatal("live original upload observation", err, out.State)
	}
	body := &userUploadObservedBody{Reader: bytes.NewReader(payload)}
	_, err = v.artifact.WriteUserUpload(ctx, v.invocation(t), meta, intent.MediaType, int64(len(payload)), body)
	requireCode(t, err, foundation.ResourceBusy)
	if body.reads != 0 || body.closes != 1 || v.proxy.puts.Load() != puts {
		t.Fatal("pending observation resent or leaked upload body", body.reads, body.closes)
	}
	_, err = v.artifact.CompleteUserUpload(ctx, v.invocation(t), meta, intent)
	requireCode(t, err, foundation.ResourceBusy)
	v.proxy.release()
	select {
	case err = <-done:
		joined = true
		if err != nil {
			t.Fatal("original upload return", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original writer not joined")
	}
	created, err := v.artifact.CompleteUserUpload(ctx, v.invocation(t), meta, intent)
	if err != nil {
		t.Fatal("complete original upload", err)
	}
	recomposed := v.bindOn(t, v.store, nil, nil)
	observed, err := recomposed.artifact.LookupUserUpload(ctx, recomposed.invocation(t), meta, intent)
	if err != nil || observed.State != ac.UserUploadCommitted || observed.Artifact == nil || observed.Artifact.Details().Reference != created.Details().Reference || v.proxy.puts.Load() != puts {
		t.Fatal("durable original upload recovery failed", err)
	}
	changed := intent
	changed.Display.Name = "different.bin"
	_, err = recomposed.artifact.LookupUserUpload(ctx, recomposed.invocation(t), meta, changed)
	requireCode(t, err, foundation.IdempotencyKeyReused)
}
