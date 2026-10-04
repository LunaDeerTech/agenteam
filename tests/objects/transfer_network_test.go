//go:build integration

package objects_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/object"
)

const (
	transferPass int32 = iota
	transferHoldStageRead
	transferCorruptStageRead
	transferLoseCandidatePut
	transferFailProbePut
	transferLoseProbePut
	transferHoldProbeGet
	transferHoldControlGet
	transferRejectCandidateRead
	transferHoldStagePut
	transferCorruptProbeRead
	transferCorruptControlRead
	transferHoldCleanup
)

// This proxy forwards actual signed storage requests to the nonce-owned TLS
// server. It does not replace an SDK, database result, authority or classifier.
type transferProxy struct {
	server                                                            *httptest.Server
	transport                                                         *http.Transport
	mode                                                              atomic.Int32
	stageGET, stagePUT, candidatePUT, probePUT, probeGET, probeDELETE atomic.Int64
	joinedReads                                                       atomic.Int64
	payloadWritten                                                    atomic.Int64
	reached                                                           chan struct{}
	release                                                           chan struct{}
	once                                                              sync.Once
}

func newTransferProxy(t *testing.T, f *fixture) *transferProxy {
	t.Helper()
	_, transport, err := f.remote.Client()
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := url.Parse(f.remote.Endpoint())
	if err != nil {
		t.Fatal("invalid owned endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	p := &transferProxy{transport: transport, reached: make(chan struct{}, 1), release: make(chan struct{})}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stage := strings.Contains(r.URL.Path, "/staging/")
		candidate := strings.Contains(r.URL.Path, "/candidate/")
		probe := strings.Contains(r.URL.Path, "/control/probe/")
		mode := p.mode.Load()
		if stage && r.Method == http.MethodGet {
			p.stageGET.Add(1)
		}
		if stage && r.Method == http.MethodPut {
			p.stagePUT.Add(1)
		}
		if candidate && r.Method == http.MethodPut {
			p.candidatePUT.Add(1)
		}
		if probe {
			switch r.Method {
			case http.MethodPut:
				p.probePUT.Add(1)
			case http.MethodGet:
				p.probeGET.Add(1)
			case http.MethodDelete:
				p.probeDELETE.Add(1)
			}
		}
		if probe && r.Method == http.MethodPut && mode == transferFailProbePut {
			http.Error(w, "owned injected failure", 503)
			return
		}
		if candidate && r.Method == http.MethodGet && mode == transferRejectCandidateRead {
			http.Error(w, "owned verification unavailable", 503)
			return
		}
		operation, done := context.WithCancel(r.Context())
		defer done()
		stop := context.AfterFunc(ctx, done)
		defer stop()
		request := r.Clone(operation)
		request.RequestURI = ""
		request.URL.Scheme, request.URL.Host = upstream.Scheme, upstream.Host
		actualTransport := transport
		if stage && r.Method == http.MethodPut && mode == transferHoldStagePut && r.URL.Query().Get("X-Amz-Signature") != "" {
			body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
			if err != nil || int64(len(body)) != r.ContentLength || len(body) < 64<<10 || len(body) >= 2<<20 {
				http.Error(w, "owned body invalid", 503)
				return
			}
			request.Body = &transferHeldBody{reader: bytes.NewReader(body), remaining: len(body) / 2, release: p.release, ctx: operation}
			actualTransport = transport.Clone()
			actualTransport.DisableKeepAlives = true
			actualTransport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: transport.TLSClientConfig}
				conn, err := dialer.DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				return &transferMeasuredWire{Conn: conn, proxy: p, half: int64(len(body) / 2)}, nil
			}
			defer actualTransport.CloseIdleConnections()
		}
		response, err := actualTransport.RoundTrip(request)
		if err != nil {
			http.Error(w, "owned upstream unavailable", 503)
			return
		}
		defer response.Body.Close()
		if mode == transferHoldCleanup && candidate && (r.Method == http.MethodDelete || r.Method == http.MethodPut) && response.StatusCode/100 == 2 {
			select {
			case p.reached <- struct{}{}:
			default:
			}
			<-operation.Done()
			return
		}
		if response.StatusCode/100 == 2 && ((candidate && r.Method == http.MethodPut && mode == transferLoseCandidatePut) || (probe && r.Method == http.MethodPut && mode == transferLoseProbePut)) {
			_, _ = io.Copy(io.Discard, response.Body)
			select {
			case p.reached <- struct{}{}:
			default:
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		for key, values := range response.Header {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.WriteHeader(response.StatusCode)
		if r.Method == http.MethodGet && response.StatusCode == 200 && ((probe && mode == transferHoldProbeGet) || (!probe && strings.Contains(r.URL.Path, "/control/") && mode == transferHoldControlGet)) {
			defer p.joinedReads.Add(1)
			w.(http.Flusher).Flush()
			select {
			case p.reached <- struct{}{}:
			default:
			}
			select {
			case <-p.release:
			case <-operation.Done():
				return
			}
		}
		if stage && r.Method == http.MethodGet && response.StatusCode == 200 && mode == transferHoldStageRead {
			_, _ = io.CopyN(w, response.Body, 32<<10)
			w.(http.Flusher).Flush()
			select {
			case p.reached <- struct{}{}:
			default:
			}
			select {
			case <-p.release:
			case <-operation.Done():
				return
			}
		}
		if r.Method == http.MethodGet && response.StatusCode == 200 && (stage && mode == transferCorruptStageRead || probe && mode == transferCorruptProbeRead || !probe && strings.Contains(r.URL.Path, "/control/") && mode == transferCorruptControlRead) {
			// Flip one actual byte without changing framing or the declared size.
			var first [1]byte
			if _, err := io.ReadFull(response.Body, first[:]); err == nil {
				first[0] ^= 1
				_, _ = w.Write(first[:])
			}
		}
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(func() {
		cancel()
		p.unblock()
		p.server.CloseClientConnections()
		p.server.Close()
		transport.CloseIdleConnections()
	})
	return p
}

// Signal after the real TLS socket accepts half the body, not on Reader
// prefetch. The old storage request remains live while a marker competes.
type transferHeldBody struct {
	reader    *bytes.Reader
	remaining int
	release   <-chan struct{}
	ctx       context.Context
}

func (b *transferHeldBody) Read(p []byte) (int, error) {
	if b.remaining > 0 {
		n, e := b.reader.Read(p[:min(len(p), b.remaining)])
		b.remaining -= n
		return n, e
	}
	select {
	case <-b.release:
		return b.reader.Read(p)
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	}
}
func (*transferHeldBody) Close() error { return nil }

type transferMeasuredWire struct {
	net.Conn
	proxy    *transferProxy
	header   []byte
	headDone bool
	half     int64
}

func (c *transferMeasuredWire) Write(p []byte) (int, error) {
	n, e := c.Conn.Write(p)
	actual := p[:n]
	if !c.headDone {
		c.header = append(c.header, actual...)
		at := bytes.Index(c.header, []byte("\r\n\r\n"))
		if at < 0 {
			return n, e
		}
		actual = c.header[at+4:]
		c.headDone = true
	}
	if c.proxy.payloadWritten.Add(int64(len(actual))) >= c.half {
		select {
		case c.proxy.reached <- struct{}{}:
		default:
		}
	}
	c.header = nil
	return n, e
}
func (p *transferProxy) unblock() { p.once.Do(func() { close(p.release) }) }
func (p *transferProxy) wait(t *testing.T) {
	t.Helper()
	select {
	case <-p.reached:
	case <-contextFor(t).Done():
		t.Fatal("actual storage barrier not reached")
	}
}
func (p *transferProxy) config(t *testing.T, f *fixture) object.StorageConfig {
	t.Helper()
	values := map[string]string{"ENDPOINT": p.server.URL, "BUCKET": f.bucket, "ACCESS_KEY": f.remote.AccessKey, "SECRET_KEY": f.remote.SecretKey, "TLS_MODE": "disable"}
	config, err := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return config
}
