//go:build integration

package security_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

func (f *outboundNetwork) forbid(t *testing.T) {
	t.Helper()
	rules, _ := outbound.NewRules()
	if _, e := f.policy.UpdatePolicy(auditContext(t), f.f.actor, policyCommand(t, f.f.actor, *f.policy.Status().Version), rules); e != nil {
		t.Fatal(e)
	}
}
func awaitOutbound[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("outbound fixture barrier not reached")
		var zero T
		return zero
	}
}

func TestOutboundNetworkCommitBeforeNewOrReusedFirstWrite(t *testing.T) {
	for _, reused := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "reused"}[reused], func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8443)
			p := f.profile(t, outbound.ProfileOptions{})
			if reused {
				id := f.scenario(t, netfixture.ScenarioConfig{Body: "warm"})
				r, e := f.get(t, id, p)
				if e != nil {
					t.Fatal(e)
				}
				readOutbound(t, r)
			}
			id := f.scenario(t, netfixture.ScenarioConfig{Body: "never sent"})
			borrowed := make(chan httptrace.GotConnInfo, 1)
			release := make(chan struct{})
			ctx, cancel := context.WithCancel(auditContext(t))
			defer cancel()
			ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
				borrowed <- info
				select {
				case <-release:
				case <-ctx.Done():
				}
			}})
			done := make(chan error, 1)
			go func() {
				req, _ := http.NewRequest("GET", f.url(id), nil)
				r, e := f.client.Do(ctx, req, p)
				if e == nil {
					r.Close()
				}
				done <- e
			}()
			info := awaitOutbound(t, borrowed)
			if info.Reused != reused || !reused && info.IdleTime != 0 {
				t.Fatal("wrong connection barrier")
			}
			if _, e := info.Conn.Write([]byte("bypass")); !errors.Is(e, errors.ErrUnsupported) {
				t.Fatal("trace allowed uncontrolled bytes")
			}
			if e := info.Conn.SetDeadline(time.Now()); !errors.Is(e, errors.ErrUnsupported) {
				t.Fatal("trace allowed deadline override")
			}
			f.forbid(t)
			close(release)
			outboundReason(t, awaitOutbound(t, done), ac.PrivateNotAllowed, false)
			if len(f.state(t, id).Requests) != 0 {
				t.Fatal("HTTP request crossed committed policy")
			}
		})
	}
}

type barrierResolver struct {
	ip      netip.Addr
	entered chan struct{}
	release chan struct{}
}

func (r *barrierResolver) Lookup(ctx context.Context, _ string) ([]netip.Addr, error) {
	close(r.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return []netip.Addr{r.ip}, nil
	}
}
func TestOutboundNetworkCommitWhileDNSStopsDial(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	r := &barrierResolver{ip: f.resolver.ip, entered: make(chan struct{}), release: make(chan struct{})}
	trust, _ := outbound.LoadTrustStore(f.net.CAFile)
	client, e := outbound.NewClient(f.policy, trust, r)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := client.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	var borrowed atomic.Int64
	ctx := httptrace.WithClientTrace(auditContext(t), &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { borrowed.Add(1) }})
	id := f.scenario(t, netfixture.ScenarioConfig{})
	before := len(f.state(t, id).Connections)
	done := make(chan error, 1)
	p := f.profile(t, outbound.ProfileOptions{})
	go func() { req, _ := http.NewRequest("GET", f.url(id), nil); _, e := client.Do(ctx, req, p); done <- e }()
	awaitOutbound(t, r.entered)
	f.forbid(t)
	close(r.release)
	outboundReason(t, awaitOutbound(t, done), ac.PrivateNotAllowed, false)
	state := f.state(t, id)
	if borrowed.Load() != 0 || len(state.Requests) != 0 || len(state.Connections) != before {
		t.Fatal("DNS result dialled after tightening")
	}
}

func TestOutboundNetworkSentResponseSurvivesCommit(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	id := f.scenario(t, netfixture.ScenarioConfig{Mode: "stream", Body: "old", Size: 4})
	r, e := f.get(t, id, f.profile(t, outbound.ProfileOptions{Streaming: true}))
	if e != nil {
		t.Fatal(e)
	}
	first := make([]byte, 3)
	if _, e := io.ReadFull(r.Body(), first); e != nil || string(first) != "old" {
		t.Fatal(e)
	}
	if !r.Decision().Sent || len(f.state(t, id).Requests) != 1 {
		t.Fatal("no actual sent barrier")
	}
	f.forbid(t)
	if e := f.net.Release(auditContext(t), id); e != nil {
		t.Fatal(e)
	}
	if got := readOutbound(t, r); got != "xxxx" {
		t.Fatal("old stream recalled", got)
	}
	next := f.scenario(t, netfixture.ScenarioConfig{})
	_, e = f.get(t, next, f.profile(t, outbound.ProfileOptions{}))
	outboundReason(t, e, ac.PrivateNotAllowed, false)
	if len(f.state(t, next).Requests) != 0 {
		t.Fatal("new request retained old policy")
	}
}

func TestOutboundNetworkZeroWriteRetryBoundAndFreshAdmission(t *testing.T) {
	for _, tighten := range []bool{false, true} {
		t.Run(map[bool]string{false: "two_actual_sockets", true: "second_fresh_dns_denied"}[tighten], func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8443)
			id := f.scenario(t, netfixture.ScenarioConfig{})
			before := len(f.state(t, id).Connections)
			var count atomic.Int64
			var rounds atomic.Int64
			var failedWrites atomic.Int64
			ctx := httptrace.WithClientTrace(auditContext(t), &httptrace.ClientTrace{GetConn: func(string) {
				if rounds.Add(1) == 2 && tighten {
					f.forbid(t)
				}
			}, GotConn: func(info httptrace.GotConnInfo) {
				count.Add(1)
				// A half-close keeps deadline operations valid; the actual first
				// TCP Write now returns zero/EPIPE, instead of failing earlier at
				// SetWriteDeadline on a fully closed descriptor.
				if e := info.Conn.(interface{ CloseWrite() error }).CloseWrite(); e != nil {
					t.Error(e)
				}
			}, WroteRequest: func(info httptrace.WroteRequestInfo) {
				if info.Err == nil || outbound.SafeNetworkError(info.Err).Decision().Sent {
					t.Error("expected actual zero-byte write error")
				}
				failedWrites.Add(1)
			}})
			req, _ := http.NewRequest("GET", f.url(id), nil)
			_, e := f.client.Do(ctx, req, f.profile(t, outbound.ProfileOptions{}))
			expected := int64(2)
			reason := ac.InternalError
			if tighten {
				expected = 1
				reason = ac.PrivateNotAllowed
			}
			outboundReason(t, e, reason, false)
			state := f.state(t, id)
			if count.Load() != expected || failedWrites.Load() != expected || f.resolver.calls.Load() != 2 || int64(len(state.Connections)-before) != expected || len(state.Requests) != 0 {
				t.Fatalf("zero retry counts borrow=%d writes=%d DNS=%d connections=%d received=%d", count.Load(), failedWrites.Load(), f.resolver.calls.Load(), len(state.Connections)-before, len(state.Requests))
			}
		})
	}
}
