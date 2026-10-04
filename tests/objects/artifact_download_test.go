//go:build integration

package objects_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

func (f *artifactFixture) downloads(t *testing.T, provider oc.DownloadProvider) *object.Downloads {
	t.Helper()
	material := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	secrets, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, material(21)), f.keys)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"new","keys":[{"kid":"old","key_b64":%q},{"kid":"new","key_b64":%q}]}`, material(22), material(23)), f.keys, secrets)
	if err != nil {
		t.Fatal(err)
	}
	d, err := object.NewDownloads(f.objects, provider, keys)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func (f *artifactFixture) create(t *testing.T, key, media, body string) art.Metadata {
	t.Helper()
	m, e := f.artifact.CreateFromContent(contextFor(t), f.invocation(t), command(t, key), art.Display{Name: "下载-canary.txt"}, media, body)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func (f *artifactFixture) downloadToken(t *testing.T, d *object.Downloads, m art.Metadata, mode oc.DownloadMode) string {
	t.Helper()
	ref, _ := m.Details().Reference.BusinessFile()
	value, e := d.IssueDownload(contextFor(t), f.actor, ref, mode, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	response, e := value.ForHuman(f.actor)
	if e != nil {
		t.Fatal(e)
	}
	return strings.TrimPrefix(response.URL, "/api/object-downloads/")
}

type downloaded struct {
	result   object.DownloadResult
	err      error
	accepted int64
}
type downloadWriter struct {
	http.ResponseWriter
	accepted  int64
	headers   bool
	failAfter int64
	first     func()
	once      sync.Once
}

func (w *downloadWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *downloadWriter) WriteHeader(status int) {
	w.headers = true
	w.ResponseWriter.WriteHeader(status)
}
func (w *downloadWriter) Write(b []byte) (int, error) {
	w.once.Do(func() {
		if w.first != nil {
			w.first()
		}
	})
	if w.failAfter > 0 && int64(len(b)) > w.failAfter-w.accepted {
		b = b[:max(int64(0), w.failAfter-w.accepted)]
		n, e := w.ResponseWriter.Write(b)
		w.accepted += int64(n)
		if e != nil {
			return n, e
		}
		return n, io.ErrClosedPipe
	}
	n, e := w.ResponseWriter.Write(b)
	w.accepted += int64(n)
	return n, e
}
func downloadHTTP(t *testing.T, d *object.Downloads, actor identity.Actor, token, rangeHeader string, failAfter int64, first func()) (*http.Response, []byte, error, downloaded) {
	t.Helper()
	done := make(chan downloaded, 1)
	// This authenticated test adapter never logs or reflects the bearer path.
	// D27 must supply the real Session adapter; no route is installed by D05.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := d.OpenDownload(r.Context(), actor, token, r.Header.Get("Range"))
		if err != nil {
			status := http.StatusForbidden
			var f *foundation.Fault
			if errors.As(err, &f) && f.Code == foundation.RangeNotSatisfiable {
				status = 416
				if size, ok := object.DownloadUnsatisfiedSize(err); ok {
					w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(int64(size), 10))
				}
			}
			w.WriteHeader(status)
			done <- downloaded{err: err}
			return
		}
		defer stream.Close()
		writer := &downloadWriter{ResponseWriter: w, failAfter: failAfter, first: first}
		result, err := stream.StreamHTTP(writer)
		done <- downloaded{result, err, writer.accepted}
		if err != nil {
			if writer.headers {
				panic(http.ErrAbortHandler)
			}
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer server.Close()
	req, _ := http.NewRequestWithContext(contextFor(t), http.MethodGet, server.URL+"/download", nil)
	req.Header.Set("Range", rangeHeader)
	client := server.Client()
	resp, err := client.Do(req)
	var body []byte
	if err == nil {
		body, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	select {
	case result := <-done:
		return resp, body, err, result
	case <-time.After(3 * time.Second):
		t.Fatal("download handler not joined")
		return nil, nil, nil, downloaded{}
	}
}
func TestArtifactDownloadRealStreamRangePermissionsAndAudit(t *testing.T) {
	f := newArtifactFixture(t)
	body := strings.Repeat("read-only browser body\n", 8192)
	m := f.create(t, "download", "text/plain", body)
	d := f.downloads(t, f.sources)
	token := f.downloadToken(t, d, m, oc.DownloadPreview)
	for _, tc := range []struct {
		rangeHeader string
		status      int
		want        string
	}{{"", 200, body}, {"bytes=7-22", 206, body[7:23]}, {"bytes=-19", 206, body[len(body)-19:]}, {"bytes=0-", 206, body}} {
		resp, got, err, result := downloadHTTP(t, d, f.actor, token, tc.rangeHeader, 0, nil)
		if err != nil || resp.StatusCode != tc.status || string(got) != tc.want || result.err != nil || result.result.Phase != oc.DownloadSent || result.result.AuditPending || int64(result.result.SentBytes) != int64(len(tc.want)) || result.accepted != int64(len(tc.want)) {
			t.Fatalf("stream/range mismatch status=%v read_error=%t stream_error=%v accepted=%d", respStatus(resp), err != nil, result.err, result.accepted)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cache-Control") != "private, no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(resp.Header.Get("Content-Disposition"), "filename*=utf-8''") || resp.Header.Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
			t.Fatal("missing safe headers")
		}
	}
	gets := f.proxy.gets.Load()
	resp, _, _, bad := downloadHTTP(t, d, f.actor, token, "bytes=99999999-", 0, nil)
	if resp.StatusCode != 416 || resp.Header.Get("Content-Range") != "bytes */"+strconv.Itoa(len(body)) {
		t.Fatal("range status")
	}
	requireCode(t, bad.err, foundation.RangeNotSatisfiable)
	if f.proxy.gets.Load() != gets {
		t.Fatal("invalid range opened object")
	}
	f.sql(t, `UPDATE object_fixture.projects SET state='archived'`)
	_, _, err, archived := downloadHTTP(t, d, f.actor, token, "bytes=0-3", 0, nil)
	if err != nil || archived.err != nil {
		t.Fatal("archived read/Audit", archived.err)
	}
	f.sql(t, `UPDATE object_fixture.sessions SET active=false`)
	gets = f.proxy.gets.Load()
	stream, err := d.OpenDownload(contextFor(t), f.actor, token, "")
	if stream != nil || err == nil || f.proxy.gets.Load() != gets {
		t.Fatal("revoked session read")
	}
	f.sql(t, `UPDATE object_fixture.sessions SET active=true`)
	other, _ := identity.NewHuman(id[identity.User](t), id[identity.Session](t))
	_, err = d.OpenDownload(contextFor(t), other, token, "")
	requireCode(t, err, foundation.Forbidden)
	_, err = d.OpenDownload(contextFor(t), f.actor, token[:len(token)-1]+"!", "")
	requireCode(t, err, foundation.Forbidden)
	var issued, started, sent int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FILTER(WHERE metadata->>'phase'='issued'),count(*) FILTER(WHERE metadata->>'phase'='started'),count(*) FILTER(WHERE metadata->>'phase'='sent') FROM agenteam_audit.audit_records WHERE action='artifact.download'`).Scan(&issued, &started, &sent); err != nil || issued != 1 || started != 5 || sent != 5 {
		t.Fatal("actual Artifact phase Audit", err, issued, started, sent)
	}
}
func respStatus(r *http.Response) int {
	if r == nil {
		return 0
	}
	return r.StatusCode
}
func TestArtifactDownloadAuditFailureAndActualPartialWrite(t *testing.T) {
	f := newArtifactFixture(t)
	body := strings.Repeat("stream-body", 16384)
	m := f.create(t, "download-failure", "text/plain", body)
	d := f.downloads(t, f.sources)
	ref, _ := m.Details().Reference.BusinessFile()
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=false`)
	value, err := d.IssueDownload(contextFor(t), f.actor, ref, oc.DownloadAttachment, 0)
	if _, e := value.ForHuman(f.actor); err == nil || e == nil {
		t.Fatal("issued without Audit")
	}
	var count int
	f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_download.grants`).Scan(&count)
	if count != 0 {
		t.Fatal("grant survived Audit rollback")
	}
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=true`)
	token := f.downloadToken(t, d, m, oc.DownloadAttachment)
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=false`)
	gets := f.proxy.gets.Load()
	stream, err := d.OpenDownload(contextFor(t), f.actor, token, "")
	if err == nil || stream != nil || f.proxy.gets.Load() != gets {
		t.Fatal("GET before started Audit")
	}
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=true`)
	resp, bytes, readErr, failed := downloadHTTP(t, d, f.actor, token, "", 8192, nil)
	if respStatus(resp) != 200 || readErr == nil || len(bytes) >= len(body) || failed.err == nil || failed.result.Phase != oc.DownloadFailed || failed.result.Failure != oc.DownloadWriteFailed || failed.result.SentBytes != 8192 || failed.accepted != 8192 || failed.result.AuditPending {
		t.Fatal("partial write not accurately audited", failed.result, failed.err, readErr != nil, len(bytes))
	}
	gets = f.proxy.gets.Load()
	_, _, _, pending := downloadHTTP(t, d, f.actor, token, "", 0, func() { f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=false`) })
	if pending.err == nil || !pending.result.AuditPending || pending.result.Phase != oc.DownloadSent || int64(pending.result.SentBytes) != int64(len(body)) {
		t.Fatal("final Audit hid real sent bytes", pending.result, pending.err)
	}
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=true`)
	confirmed, err := d.ConfirmDownloadOutcome(contextFor(t), f.actor, pending.result.AttemptID)
	if err != nil || confirmed.AuditPending || confirmed.SentBytes != pending.result.SentBytes || f.proxy.gets.Load() != gets+1 {
		t.Fatal("confirmation retransmitted payload or lost terminal facts", err)
	}
}
func TestArtifactDownloadPreviewSniffAndUnboundProvider(t *testing.T) {
	f := newArtifactFixture(t)
	d := f.downloads(t, f.sources)
	for i, body := range []string{"<html><script>alert(1)</script></html>", "<svg xmlns=\"http://www.w3.org/2000/svg\"><script>x</script></svg>", "<?xml version=\"1.0\"?><x/>", "%PDF-1.7\nunsafe", "plain text"} {
		m := f.create(t, fmt.Sprint("sniff", i), "image/png", body)
		token := f.downloadToken(t, d, m, oc.DownloadPreview)
		resp, got, err, result := downloadHTTP(t, d, f.actor, token, "", 0, nil)
		if err != nil || result.err != nil || string(got) != body {
			t.Fatal("sniff read", result.err)
		}
		if i < 4 && !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
			t.Fatal("active media inline")
		}
		if i == 4 && (resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "inline")) {
			t.Fatal("safe actual text not recognized")
		}
	}
	before := f.proxy.gets.Load()
	missing := f.downloads(t, nil)
	ref, _ := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: f.project, DocumentID: id[struct{}](t).String(), Revision: 1})
	for _, svc := range []*object.Downloads{d, missing} {
		_, err := svc.IssueDownload(contextFor(t), f.actor, ref, oc.DownloadAttachment, 0)
		requireCode(t, err, foundation.DependencyUnbound)
	}
	if f.proxy.gets.Load() != before {
		t.Fatal("unbound provider touched payload")
	}
}
