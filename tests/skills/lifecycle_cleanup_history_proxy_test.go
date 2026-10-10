//go:build integration

package skill_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
)

// This is a real task-owned wire proxy, not an Object/authority replacement.
// Only candidate verification GET may fail. PUT and all later cleanup calls
// use the original signed request and actual owned MinIO. No SQL fact changes.
type skillAttemptProxy struct {
	server    *httptest.Server
	transport *http.Transport
	target    *url.URL
	reject    atomic.Bool
	rejected  atomic.Int64
	puts      atomic.Int64
	active    atomic.Int64
	wg        sync.WaitGroup
}

func newSkillAttemptProxy(t *testing.T) *skillAttemptProxy {
	t.Helper()
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal("owned Object proxy configuration", err)
	}
	_, transport, err := remote.Client()
	if err != nil {
		t.Fatal("owned Object proxy transport", err)
	}
	target, err := url.Parse(remote.Endpoint())
	if err != nil {
		transport.CloseIdleConnections()
		t.Fatal(err)
	}
	p := &skillAttemptProxy{transport: transport, target: target}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(func() {
		p.server.CloseClientConnections()
		p.server.Close()
		p.wg.Wait()
		p.transport.CloseIdleConnections()
		if p.active.Load() != 0 {
			t.Error("original candidate proxy handler did not return")
		}
	})
	return p
}

func (p *skillAttemptProxy) configure(values map[string]string) {
	values["ENDPOINT"] = p.server.URL
	values["TLS_MODE"] = "disable"
	delete(values, "CA_FILE")
}

func (p *skillAttemptProxy) serve(w http.ResponseWriter, r *http.Request) {
	p.wg.Add(1)
	p.active.Add(1)
	defer p.wg.Done()
	defer p.active.Add(-1)
	candidate := strings.Contains(r.URL.Path, "/candidate/")
	if candidate && r.Method == http.MethodGet && p.reject.Load() {
		p.rejected.Add(1)
		http.Error(w, "fixture verification unavailable", http.StatusServiceUnavailable)
		return
	}
	request := r.Clone(r.Context())
	request.URL.Scheme, request.URL.Host = p.target.Scheme, p.target.Host
	request.RequestURI, request.Host, request.GetBody = "", r.Host, nil
	if candidate && r.Method == http.MethodPut {
		// Preserve the actual signed wire body, including aws-chunked framing.
		// A bounded package cannot fill this limit; malformed input is rejected.
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		closeErr := r.Body.Close()
		if readErr != nil || closeErr != nil || len(raw) >= 2<<20 || r.ContentLength >= 0 && int64(len(raw)) != r.ContentLength || r.ContentLength < 0 && (len(r.TransferEncoding) != 1 || r.TransferEncoding[0] != "chunked") {
			http.Error(w, "fixture wire body unavailable", http.StatusServiceUnavailable)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		request.Trailer = r.Trailer.Clone()
		p.puts.Add(1)
	}
	response, err := p.transport.RoundTrip(request)
	if err != nil {
		http.Error(w, "fixture upstream unavailable", http.StatusServiceUnavailable)
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}
