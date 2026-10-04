//go:build integration

package objects_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type storageProxy struct {
	t                                 *testing.T
	server                            *httptest.Server
	transport                         *http.Transport
	target                            *url.URL
	mode                              atomic.Int32
	puts                              atomic.Int64
	gets                              atomic.Int64
	deletes                           atomic.Int64
	markers                           atomic.Int64
	began, marker, released, finished chan struct{}
	upstreamDone                      chan struct{}
	upstreamOnce                      sync.Once
	once, markerOnce, releaseOnce     sync.Once
	ctx                               context.Context
	cancel                            context.CancelFunc
	wg                                sync.WaitGroup
}

const (
	proxyPass int32 = iota
	proxyLosePutResponse
	proxyHoldPutBody
	proxyLoseMarkerResponse
	proxyRejectCandidateRead
	proxyHoldReadBody
	proxyHoldMarkerResponse
)

func newStorageProxy(t *testing.T, f *fixture) *storageProxy {
	t.Helper()
	_, transport, e := f.remote.Client()
	if e != nil {
		t.Fatal(e)
	}
	target, _ := url.Parse(f.remote.Endpoint())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	p := &storageProxy{t: t, transport: transport, target: target, began: make(chan struct{}), marker: make(chan struct{}), released: make(chan struct{}), finished: make(chan struct{}), upstreamDone: make(chan struct{}), ctx: ctx, cancel: cancel}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(func() {
		p.release()
		p.cancel()
		p.server.CloseClientConnections()
		p.server.Close()
		p.wg.Wait()
		p.transport.CloseIdleConnections()
	})
	return p
}
func (p *storageProxy) release() { p.releaseOnce.Do(func() { close(p.released) }) }

type heldBody struct {
	body  *bytes.Reader
	left  int
	proxy *storageProxy
}

func (b *heldBody) Read(out []byte) (int, error) {
	if b.left > 0 {
		out = out[:min(len(out), b.left)]
		n, e := b.body.Read(out)
		b.left -= n
		return n, e
	}
	b.proxy.once.Do(func() { close(b.proxy.began) })
	select {
	case <-b.proxy.released:
		return b.body.Read(out)
	case <-b.proxy.ctx.Done():
		return 0, b.proxy.ctx.Err()
	}
}
func (b *heldBody) Close() error { return nil }
func (p *storageProxy) serve(w http.ResponseWriter, r *http.Request) {
	p.wg.Add(1)
	defer p.wg.Done()
	candidate := strings.Contains(r.URL.Path, "/candidate/")
	if candidate && r.Method == http.MethodGet {
		p.gets.Add(1)
	}
	var raw []byte
	decoded := r.ContentLength
	if candidate && r.Method == http.MethodPut {
		var e error
		raw, e = io.ReadAll(io.LimitReader(r.Body, 2<<20))
		_ = r.Body.Close()
		if e != nil || len(raw) >= 2<<20 || r.ContentLength >= 0 && int64(len(raw)) != r.ContentLength || r.ContentLength < 0 && (len(r.TransferEncoding) != 1 || r.TransferEncoding[0] != "chunked") {
			p.t.Logf("candidate wire rejected: content_length=%d bytes=%d transfer_encoding=%v", r.ContentLength, len(raw), r.TransferEncoding)
			http.Error(w, "fixture body unavailable", 503)
			return
		}
		if value := r.Header.Get("X-Amz-Decoded-Content-Length"); value != "" {
			declared, err := strconv.ParseInt(value, 10, 64)
			if err != nil || declared < 0 || !strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
				p.t.Logf("candidate framing rejected: content_length=%d declared=%d", r.ContentLength, declared)
				http.Error(w, "fixture framing invalid", 503)
				return
			}
			decoded, e = io.Copy(io.Discard, httputil.NewChunkedReader(bytes.NewReader(raw)))
			if e != nil || decoded != declared {
				p.t.Logf("candidate decoded rejected: content_length=%d declared=%d decoded=%d decoder_error=%t", r.ContentLength, declared, decoded, e != nil)
				http.Error(w, "fixture decoded length invalid", 503)
				return
			}
		}
	}
	payload := candidate && r.Method == http.MethodPut && decoded > 0
	marker := candidate && r.Method == http.MethodPut && decoded == 0
	if payload {
		p.puts.Add(1)
	}
	if marker {
		p.markers.Add(1)
		p.markerOnce.Do(func() { close(p.marker) })
	}
	if candidate && r.Method == http.MethodDelete {
		p.deletes.Add(1)
	}
	request := r.Clone(r.Context())
	request.URL.Scheme = p.target.Scheme
	request.URL.Host = p.target.Host
	request.RequestURI = ""
	request.Host = r.Host
	request.GetBody = nil
	if raw != nil {
		request.Body = io.NopCloser(bytes.NewReader(raw))
		request.Trailer = r.Trailer.Clone()
	}
	p.t.Logf("wire classified: method=%s content_length=%d decoded=%d payload=%t marker=%t", r.Method, r.ContentLength, decoded, payload, marker)
	mode := p.mode.Load()
	if candidate && r.Method == http.MethodGet && mode == proxyRejectCandidateRead {
		http.Error(w, "fixture verification unavailable", http.StatusServiceUnavailable)
		return
	}
	if payload && mode == proxyHoldPutBody {
		// The task-owned proxy deliberately keeps the real remote connection after
		// the client disappears. This models an actual unknown in-flight PUT.
		request = request.Clone(p.ctx)
		request.Body = &heldBody{body: bytes.NewReader(raw), left: len(raw) / 2, proxy: p}
		defer close(p.finished)
	}
	response, e := p.transport.RoundTrip(request)
	if e != nil {
		http.Error(w, "fixture upstream unavailable", 503)
		return
	}
	defer response.Body.Close()
	if marker && mode == proxyHoldMarkerResponse {
		_, _ = io.Copy(io.Discard, response.Body)
		p.upstreamOnce.Do(func() { close(p.upstreamDone) })
		<-r.Context().Done()
		return
	}
	if payload && mode == proxyLosePutResponse || marker && mode == proxyLoseMarkerResponse {
		_, _ = io.Copy(io.Discard, response.Body)
		conn, _, e := w.(http.Hijacker).Hijack()
		if e == nil {
			_ = conn.Close()
		}
		return
	}
	for key, values := range response.Header {
		w.Header()[key] = values
	}
	w.WriteHeader(response.StatusCode)
	if candidate && r.Method == http.MethodGet && mode == proxyHoldReadBody {
		defer close(p.finished)
		_, _ = io.CopyN(w, response.Body, 64<<10)
		w.(http.Flusher).Flush()
		p.once.Do(func() { close(p.began) })
		select {
		case <-p.released:
		case <-r.Context().Done():
			return
		}
	}
	_, _ = io.Copy(w, response.Body)
}
func (f *fixture) on(t *testing.T, store *postgres.Store, endpoint string, processes oc.ProcessAuthority) (*object.Service, object.StorageConfig) {
	t.Helper()
	return f.reopen(t, store, endpoint, processes, id[oc.Process](t), filepath.Join(t.TempDir(), "spool"))
}
func (f *fixture) reopen(t *testing.T, store *postgres.Store, endpoint string, processes oc.ProcessAuthority, process oc.ProcessID, spoolPath string) (*object.Service, object.StorageConfig) {
	t.Helper()
	service, config := f.createService(t, store, endpoint, processes, process, spoolPath)
	if e := service.Initialize(contextFor(t)); e != nil {
		t.Fatal(e)
	}
	return service, config
}
func (f *fixture) createService(t *testing.T, store *postgres.Store, endpoint string, processes oc.ProcessAuthority, process oc.ProcessID, spoolPath string) (*object.Service, object.StorageConfig) {
	t.Helper()
	values := map[string]string{"ENDPOINT": endpoint, "BUCKET": f.bucket, "ACCESS_KEY": f.remote.AccessKey, "SECRET_KEY": f.remote.SecretKey, "TLS_MODE": "disable"}
	if strings.HasPrefix(endpoint, "https:") {
		values["TLS_MODE"] = "verify-full"
		values["CA_FILE"] = f.remote.CAFile
	}
	config, e := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if e != nil {
		t.Fatal(e)
	}
	backend, e := object.NewBackend(config)
	if e != nil {
		t.Fatal(e)
	}
	spool, e := object.OpenSpool(spoolPath, process)
	if e != nil {
		t.Fatal(e)
	}
	auth := &authority{store: store}
	keys, e := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	auditing, e := audit.New(store, keys, audit.Authorizations{Projects: auditAuthority{auth}})
	if e != nil {
		t.Fatal(e)
	}
	service, e := object.New(store, backend, spool, auditing, object.Authorizations{Planner: auth, Resources: auth, Read: auth, Gate: auth, Cleanup: auth, Leases: auth, Processes: processes})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := service.Drain(ctx); e != nil {
			_ = service.Force(ctx)
			t.Error(e)
		}
	})
	return service, config
}
