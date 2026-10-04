//go:build integration

package objects_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func TestArtifactDownloadIntegrityNeverDeliversCorruptWholeBody(t *testing.T) {
	f := newArtifactFixture(t)
	d := f.downloads(t, f.sources)
	for _, size := range []int{0, 16, 256 << 10} {
		body := strings.Repeat("a", size)
		m := f.create(t, "integrity-"+foundation.Progress(size).String(), "text/plain", body)
		token := f.downloadToken(t, d, m, oc.DownloadAttachment)
		key := f.physical(t, m.Details().Object.ID)
		changed := strings.Repeat("b", size)
		if size == 0 {
			changed = "!"
		}
		if _, err := f.s3.PutObject(contextFor(t), f.bucket, key, strings.NewReader(changed), int64(len(changed)), minio.PutObjectOptions{DisableMultipart: true}); err != nil {
			t.Fatal("owned mutation failed")
		}
		resp, got, readErr, out := downloadHTTP(t, d, f.actor, token, "", 0, nil)
		if size <= 64<<10 {
			if respStatus(resp) == 200 || out.accepted != 0 || len(got) != 0 || out.err == nil {
				t.Fatal("small corrupt body delivered", size, respStatus(resp), out.err)
			}
			var confirmed int
			err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_download.attempts a JOIN agenteam_download.grants g ON g.id=a.grant_id WHERE g.object_id=$1 AND a.phase='failed' AND a.sent_bytes=0 AND a.failure='integrity_failed'`, m.Details().Object.ID.String()).Scan(&confirmed)
			if err != nil || confirmed != 1 {
				t.Fatal("small corrupt read lost integrity Audit/zero bytes", confirmed, err)
			}
		} else {
			if respStatus(resp) != 200 || readErr == nil || len(got) == 0 || len(got) > size-(64<<10) || out.result.Phase != oc.DownloadFailed || out.result.Failure != oc.DownloadIntegrityFailed || out.accepted != int64(out.result.SentBytes) || out.result.AuditPending {
				t.Fatal("tail integrity/actual bytes", out.result, out.err, readErr != nil, len(got))
			}
		}
	}
}
func TestArtifactDownloadDrainAndForceJoinActualStorage(t *testing.T) {
	for _, forced := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[forced], func(t *testing.T) {
			f := newArtifactFixture(t)
			m := f.create(t, "held-download", "text/plain", strings.Repeat("x", 512<<10))
			d := f.downloads(t, f.sources)
			token := f.downloadToken(t, d, m, oc.DownloadAttachment)
			f.proxy.mode.Store(proxyHoldReadBody)
			done := make(chan downloaded, 1)
			go func() { _, _, _, out := downloadHTTP(t, d, f.actor, token, "", 0, nil); done <- out }()
			select {
			case <-f.proxy.began:
			case <-time.After(3 * time.Second):
				t.Fatal("actual storage body not held")
			}
			var active int
			if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, m.Details().Object.ID.String()).Scan(&active); err != nil || active != 1 {
				t.Fatal("no actual download reader lease", err, active)
			}
			f.objects.StopAdmission()
			_, err := d.OpenDownload(contextFor(t), f.actor, token, "")
			requireCode(t, err, foundation.ShuttingDown)
			short, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			err = f.objects.Drain(short)
			cancel()
			if err == nil {
				t.Fatal("Drain returned with actual live stream")
			}
			finish, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			started := time.Now()
			if forced {
				err = f.objects.Force(finish)
			} else {
				f.proxy.release()
				err = f.objects.Drain(finish)
			}
			if err != nil || time.Since(started) > 1100*time.Millisecond {
				t.Fatal("shared stream shutdown", err)
			}
			select {
			case out := <-done:
				if forced {
					if out.err == nil || out.result.Phase != oc.DownloadFailed {
						t.Fatal("forced stream claimed sent", out.result)
					}
				} else if out.err != nil || out.result.Phase != oc.DownloadSent || out.result.AuditPending {
					t.Fatal("first stop rejected admitted completion/Audit", out.result, out.err)
				}
			case <-finish.Done():
				t.Fatal("HTTP operation did not join")
			}
			select {
			case <-f.proxy.finished:
			case <-finish.Done():
				t.Fatal("actual storage peer did not join")
			}
			if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active'`, m.Details().Object.ID.String()).Scan(&active); err != nil || active != 0 {
				t.Fatal("stream left active lease", err, active)
			}
		})
	}
}
func TestArtifactDownloadUnopenedCloseAuditsZeroAndExpiryBlocksNew(t *testing.T) {
	f := newArtifactFixture(t)
	m := f.create(t, "never-delivered", "text/plain", strings.Repeat("q", 128<<10))
	d := f.downloads(t, f.sources)
	ref, _ := m.Details().Reference.BusinessFile()
	signed, err := d.IssueDownload(contextFor(t), f.actor, ref, oc.DownloadAttachment, 400*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	response, _ := signed.ForHuman(f.actor)
	token := strings.TrimPrefix(response.URL, "/api/object-downloads/")
	stream, err := d.OpenDownload(contextFor(t), f.actor, token, "")
	if err != nil {
		t.Fatal(err)
	}
	f.objects.StopAdmission()
	if err = stream.Close(); err == nil {
		t.Fatal("undelivered stream claimed success")
	}
	var failed int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_download.attempts WHERE phase='failed' AND sent_bytes=0 AND failure='cancelled'`).Scan(&failed); err != nil || failed != 1 {
		t.Fatal("unopened reader outcome", failed, err)
	}
	// A newly composed service enforces the original expiry; no fresh lifetime is
	// granted by a signature or by reloading the same DB row/keyring.
	on := f.bindOn(t, f.store, nil, nil)
	fresh := on.downloads(t, on.sources)
	timer := time.NewTimer(time.Until(response.ExpiresAt.Time()) + time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-contextFor(t).Done():
		t.Fatal("expiry wait")
	}
	before := f.proxy.gets.Load()
	_, err = fresh.OpenDownload(contextFor(t), f.actor, token, "")
	requireCode(t, err, foundation.Forbidden)
	if f.proxy.gets.Load() != before {
		t.Fatal("expired grant opened storage")
	}
}
