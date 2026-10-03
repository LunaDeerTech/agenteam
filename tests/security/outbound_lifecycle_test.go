//go:build integration

package security_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

func awaitNetworkState(t *testing.T, f *outboundNetwork, id string, predicate func(netfixture.State) bool) netfixture.State {
	t.Helper()
	ctx, cancel := context.WithTimeout(auditContext(t), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, e := f.net.State(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if predicate(state) {
			return state
		}
		select {
		case <-ctx.Done():
			t.Fatal("server state not reached")
			return state
		case <-ticker.C:
		}
	}
}

func TestOutboundNetworkCancellationDrainAndForce(t *testing.T) {
	for _, mode := range []string{"cancel", "drain", "force"} {
		t.Run(mode, func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8443)
			id := f.scenario(t, netfixture.ScenarioConfig{Mode: "stream", Body: "open"})
			ctx, cancel := context.WithCancel(auditContext(t))
			defer cancel()
			req, _ := http.NewRequest("GET", f.url(id), nil)
			r, e := f.client.Do(ctx, req, f.profile(t, outbound.ProfileOptions{Streaming: true}))
			if e != nil {
				t.Fatal(e)
			}
			first := make([]byte, 4)
			if _, e := io.ReadFull(r.Body(), first); e != nil {
				t.Fatal(e)
			}
			conn := f.state(t, id).Requests[0].Connection
			switch mode {
			case "cancel":
				cancel()
				_, e = r.Body().Read(make([]byte, 1))
				outboundReason(t, e, ac.Cancelled, true)
			case "drain":
				f.client.StopAdmission()
				_, e = f.get(t, id, f.profile(t, outbound.ProfileOptions{}))
				outboundReason(t, e, ac.PolicyUnavailable, false)
				short, stop := context.WithTimeout(auditContext(t), 30*time.Millisecond)
				if e := f.client.Drain(short); e == nil {
					t.Fatal("drain ignored active response")
				}
				stop()
				if e := f.net.Release(auditContext(t), id); e != nil {
					t.Fatal(e)
				}
				readOutbound(t, r)
			case "force":
				force, stop := context.WithTimeout(auditContext(t), time.Second)
				if e := f.client.ForceClose(force); e != nil {
					t.Fatal(e)
				}
				stop()
				_, e = r.Body().Read(make([]byte, 1))
				outboundReason(t, e, ac.Cancelled, true)
			}
			r.Close()
			awaitNetworkState(t, f, id, func(s netfixture.State) bool { return s.Closed[conn] })
			f.client.StopAdmission()
			if e := f.client.Drain(auditContext(t)); e != nil {
				t.Fatal(e)
			}
		})
	}
}

type observedPipe struct {
	*io.PipeReader
	entered chan struct{}
	once    sync.Once
}

func (p *observedPipe) Read(b []byte) (int, error) {
	p.once.Do(func() { close(p.entered) })
	return p.PipeReader.Read(b)
}

func TestOutboundNetworkBlockedRequestBodyCancelledAndJoined(t *testing.T) {
	for _, mode := range []string{"cancel", "overall", "force", "declared_length_extra"} {
		t.Run(mode, func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8443)
			id := f.scenario(t, netfixture.ScenarioConfig{})
			reader, writer := io.Pipe()
			defer writer.Close()
			body := &observedPipe{PipeReader: reader, entered: make(chan struct{})}
			ctx, cancel := context.WithCancel(auditContext(t))
			defer cancel()
			req, _ := http.NewRequest("POST", f.url(id), body)
			if mode == "declared_length_extra" {
				req.ContentLength = 1
				go func() { _, _ = writer.Write([]byte("a")) }()
			}
			limits := outbound.Limits{}
			if mode == "overall" {
				limits.Overall = 100 * time.Millisecond
			}
			profile := f.profile(t, outbound.ProfileOptions{Limits: limits})
			done := make(chan error, 1)
			go func() {
				r, e := f.client.Do(ctx, req, profile)
				if e == nil {
					r.Close()
				}
				done <- e
			}()
			awaitOutbound(t, body.entered)
			switch mode {
			case "cancel", "declared_length_extra":
				cancel()
			case "force":
				force, stop := context.WithTimeout(auditContext(t), time.Second)
				if e := f.client.ForceClose(force); e != nil {
					t.Fatal(e)
				}
				stop()
			}
			e := awaitOutbound(t, done)
			var expected = ac.Cancelled
			if mode == "overall" {
				expected = ac.Timeout
			}
			// Request.Write may read a body before flushing its buffered headers;
			// sent truth is therefore taken from the actual error, not assumed here.
			safe := outbound.SafeNetworkError(e)
			if safe.Decision().Reason != expected {
				t.Fatal(e)
			}
			f.client.StopAdmission()
			if e := f.client.Drain(auditContext(t)); e != nil {
				t.Fatal("body writer did not join", e)
			}
		})
	}
}

func TestOutboundNetworkOldContextCannotCloseReusedConnection(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	p := f.profile(t, outbound.ProfileOptions{})
	ctx, cancel := context.WithCancel(auditContext(t))
	defer cancel()
	one := f.scenario(t, netfixture.ScenarioConfig{Body: "one"})
	req, _ := http.NewRequest("GET", f.url(one), nil)
	r, e := f.client.Do(ctx, req, p)
	if e != nil {
		t.Fatal(e)
	}
	readOutbound(t, r)
	conn := f.state(t, one).Requests[0].Connection
	two := f.scenario(t, netfixture.ScenarioConfig{Mode: "stream", Body: "two"})
	r, e = f.get(t, two, p)
	if e != nil {
		t.Fatal(e)
	}
	first := make([]byte, 3)
	if _, e := io.ReadFull(r.Body(), first); e != nil {
		t.Fatal(e)
	}
	if f.state(t, two).Requests[0].Connection != conn {
		t.Fatal("expected reuse")
	}
	cancel()
	if e := f.net.Release(auditContext(t), two); e != nil {
		t.Fatal(e)
	}
	readOutbound(t, r)
}

func TestOutboundNetworkHeaderTimeoutAndNativeClientMapping(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	id := f.scenario(t, netfixture.ScenarioConfig{Mode: "hold_headers"})
	_, e := f.get(t, id, f.profile(t, outbound.ProfileOptions{Limits: outbound.Limits{ResponseHeaders: 50 * time.Millisecond}}))
	outboundReason(t, e, ac.Timeout, true)
	state := f.state(t, id)
	if len(state.Requests) != 1 {
		t.Fatal("header timeout request count")
	}
	awaitNetworkState(t, f, id, func(s netfixture.State) bool { return s.Closed[state.Requests[0].Connection] })

	id = f.scenario(t, netfixture.ScenarioConfig{Mode: "drop"})
	native, e := f.client.SDKClient(f.profile(t, outbound.ProfileOptions{}))
	if e != nil {
		t.Fatal(e)
	}
	if native.Jar != nil || native.CheckRedirect == nil {
		t.Fatal("unsafe native defaults")
	}
	req, _ := http.NewRequestWithContext(auditContext(t), "GET", f.url(id)+"?token=native-private-canary", nil)
	var borrowed bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { borrowed = true }}))
	_, e = native.Do(req)
	if !borrowed {
		t.Fatal("native did not use controlled transport")
	}
	outboundReason(t, outbound.SafeNetworkError(e), ac.InternalError, true)
	if len(f.state(t, id).Requests) != 1 {
		t.Fatal("native client retried sent request")
	}
}
