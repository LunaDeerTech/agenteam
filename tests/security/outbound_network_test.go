//go:build integration

package security_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

type fixtureResolver struct {
	ip     netip.Addr
	calls  atomic.Int64
	rebind atomic.Bool
}

func (r *fixtureResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	r.calls.Add(1)
	if r.rebind.Load() {
		return []netip.Addr{r.ip, netip.MustParseAddr("127.0.0.1")}, nil
	}
	return []netip.Addr{r.ip}, ctx.Err()
}

type outboundNetwork struct {
	f        *auditFixture
	net      *netfixture.Descriptor
	policy   *outbound.PolicyService
	client   *outbound.Client
	resolver *fixtureResolver
}

func newOutboundNetwork(t *testing.T) *outboundNetwork {
	t.Helper()
	if os.Getenv(netfixture.Env) == "" {
		t.Skip("requires owned network fixture from scripts/test-security.sh")
	}
	descriptor, e := netfixture.Load()
	if e != nil {
		t.Fatal(e)
	}
	f := newAuditFixture(t)
	policy := outboundPolicy(t, f, nil)
	resolver := &fixtureResolver{ip: netip.MustParseAddr(descriptor.PrivateIP)}
	trust, e := outbound.LoadTrustStore(descriptor.CAFile)
	if e != nil {
		t.Fatal(e)
	}
	client, e := outbound.NewClient(policy, trust, resolver)
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
	return &outboundNetwork{f, descriptor, policy, client, resolver}
}
func (f *outboundNetwork) allow(t *testing.T, allowHTTP bool, ports ...uint16) {
	t.Helper()
	p, e := outbound.SelectedPorts(ports...)
	if e != nil {
		t.Fatal(e)
	}
	rule, e := outbound.NewRule(f.net.PrivateIP+"/32", p, allowHTTP)
	if e != nil {
		t.Fatal(e)
	}
	rules, _ := outbound.NewRules(rule)
	status := f.policy.Status()
	if _, e = f.policy.UpdatePolicy(auditContext(t), f.f.actor, policyCommand(t, f.f.actor, *status.Version), rules); e != nil {
		t.Fatal(e)
	}
}
func (f *outboundNetwork) profile(t *testing.T, options outbound.ProfileOptions) outbound.Profile {
	t.Helper()
	call, e := outbound.NewCallContext(f.f.actor, identity.SystemScope(), appendKey(t, ac.AccessProducer), ac.Associations{HTTPTraceID: newID[struct{}](t).String()})
	if e != nil {
		t.Fatal(e)
	}
	options.Context = call
	options.Consumer = ac.Model
	p, e := outbound.NewProfile(options)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func (f *outboundNetwork) scenario(t *testing.T, cfg netfixture.ScenarioConfig) string {
	t.Helper()
	id, e := f.net.Create(auditContext(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *outboundNetwork) url(id string) string { return "https://fixture.test:8443/case/" + id }
func (f *outboundNetwork) state(t *testing.T, id string) netfixture.State {
	t.Helper()
	state, e := f.net.State(auditContext(t), id)
	if e != nil {
		t.Fatal(e)
	}
	return state
}
func outboundReason(t *testing.T, e error, reason ac.Reason, sent bool) {
	t.Helper()
	var n *outbound.NetworkError
	if !errors.As(e, &n) || n.Decision().Reason != reason || n.Decision().Sent != sent {
		t.Fatalf("network reason/sent got %v wanted %s/%v", e, reason, sent)
	}
}
func readOutbound(t *testing.T, r outbound.Response) string {
	t.Helper()
	defer r.Close()
	raw, e := io.ReadAll(r.Body())
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func (f *outboundNetwork) get(t *testing.T, id string, p outbound.Profile) (outbound.Response, error) {
	t.Helper()
	req, _ := http.NewRequest("GET", f.url(id), nil)
	return f.client.Do(auditContext(t), req, p)
}

func TestOutboundNetworkRealPrivateRulesTLSAndKeepAlive(t *testing.T) {
	f := newOutboundNetwork(t)
	p := f.profile(t, outbound.ProfileOptions{})
	id := f.scenario(t, netfixture.ScenarioConfig{Body: "ok"})
	_, e := f.get(t, id, p)
	outboundReason(t, e, ac.PrivateNotAllowed, false)
	if len(f.state(t, id).Requests) != 0 {
		t.Fatal("default private policy sent request")
	}
	f.allow(t, false, 8443, 8080)
	r, e := f.get(t, id, p)
	if e != nil || readOutbound(t, r) != "ok" {
		t.Fatal("trusted private TLS failed", e)
	}
	first := f.state(t, id).Requests[0]
	second := f.scenario(t, netfixture.ScenarioConfig{Body: "next"})
	r, e = f.get(t, second, p)
	if e != nil || readOutbound(t, r) != "next" {
		t.Fatal(e)
	}
	if got := f.state(t, second).Requests[0].Connection; got != first.Connection {
		t.Fatal("H1 keepalive not reused")
	}
	f.resolver.rebind.Store(true)
	third := f.scenario(t, netfixture.ScenarioConfig{Body: "forbidden"})
	_, e = f.get(t, third, p)
	outboundReason(t, e, ac.AddressForbidden, false)
	if len(f.state(t, third).Requests) != 0 {
		t.Fatal("reused socket skipped fresh complete DNS check")
	}
	f.resolver.rebind.Store(false)
	for _, mode := range []string{"untrusted", "wrong_hostname"} {
		trust, _ := outbound.LoadTrustStore(f.net.CAFile)
		if mode == "untrusted" {
			trust, _ = outbound.LoadTrustStore("")
		}
		client, e := outbound.NewClient(f.policy, trust, f.resolver)
		if e != nil {
			t.Fatal(e)
		}
		address := f.url(id)
		if mode == "wrong_hostname" {
			address = strings.Replace(address, "fixture.test", "wrong.fixture.test", 1)
		}
		req, _ := http.NewRequest("GET", address, nil)
		_, e = client.Do(auditContext(t), req, p)
		outboundReason(t, e, ac.TLSFailed, false)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if e := client.ForceClose(ctx); e != nil {
			t.Fatal(e)
		}
		cancel()
	}
	httpID := f.scenario(t, netfixture.ScenarioConfig{Body: "http"})
	req, _ := http.NewRequest("GET", "http://fixture.test:8080/case/"+httpID, nil)
	_, e = f.client.Do(auditContext(t), req, f.profile(t, outbound.ProfileOptions{AllowHTTP: true}))
	outboundReason(t, e, ac.HTTPDenied, false)
	f.allow(t, true, 8443, 8080)
	r, e = f.client.Do(auditContext(t), req, f.profile(t, outbound.ProfileOptions{AllowHTTP: true}))
	if e != nil || readOutbound(t, r) != "http" {
		t.Fatal("explicit HTTP rule failed", e)
	}
}
func TestOutboundNetworkSentRequestsNeverRetry(t *testing.T) {
	for _, idempotency := range []bool{false, true} {
		t.Run(map[bool]string{false: "get", true: "idempotency_key"}[idempotency], func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8443)
			p := f.profile(t, outbound.ProfileOptions{})
			warm := f.scenario(t, netfixture.ScenarioConfig{Body: "warm"})
			r, e := f.get(t, warm, p)
			if e != nil {
				t.Fatal(e)
			}
			readOutbound(t, r)
			conn := f.state(t, warm).Requests[0].Connection
			drop := f.scenario(t, netfixture.ScenarioConfig{Mode: "drop"})
			req, _ := http.NewRequest("GET", f.url(drop), nil)
			if idempotency {
				req.Header.Set("Idempotency-Key", "fixture-only")
			}
			_, e = f.client.Do(auditContext(t), req, p)
			outboundReason(t, e, ac.InternalError, true)
			state := f.state(t, drop)
			if len(state.Requests) != 1 || state.Requests[0].Connection != conn {
				t.Fatal("sent reused request was hidden-retried or not reused")
			}
		})
	}
}
func TestOutboundNetworkHTTPFramingAndLimits(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	for _, mode := range []string{"informational", "chunked", "connection_close"} {
		id := f.scenario(t, netfixture.ScenarioConfig{Mode: mode, Body: "framed"})
		r, e := f.get(t, id, f.profile(t, outbound.ProfileOptions{}))
		if e != nil || readOutbound(t, r) != "framed" {
			t.Fatal(mode, e)
		}
		if mode == "chunked" && r.Trailers().Get("X-Fixture-Trailer") != "complete" {
			t.Fatal("trailers lost")
		}
	}
	for _, mode := range []string{"encoding", "upgrade", "large_header"} {
		cfg := netfixture.ScenarioConfig{Mode: mode}
		if mode == "large_header" {
			cfg.HeaderBytes = 65 << 10
		}
		id := f.scenario(t, cfg)
		_, e := f.get(t, id, f.profile(t, outbound.ProfileOptions{}))
		reason := ac.ResponseLimit
		if mode == "upgrade" {
			reason = ac.InvalidTarget
		}
		outboundReason(t, e, reason, true)
	}
	id := f.scenario(t, netfixture.ScenarioConfig{Mode: "chunked", Body: "12345"})
	r, e := f.get(t, id, f.profile(t, outbound.ProfileOptions{Limits: outbound.Limits{ResponseBodyBytes: 4}}))
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(r.Body())
	outboundReason(t, e, ac.ResponseLimit, true)
	if string(body) != "1234" {
		t.Fatal("body limit delivered excess bytes")
	}
	r.Close()
	stream := f.scenario(t, netfixture.ScenarioConfig{Mode: "stream", Body: "start"})
	r, e = f.get(t, stream, f.profile(t, outbound.ProfileOptions{Streaming: true, Limits: outbound.Limits{ReadIdle: 50 * time.Millisecond}}))
	if e != nil {
		t.Fatal(e)
	}
	first := make([]byte, 5)
	if _, e = io.ReadFull(r.Body(), first); e != nil {
		t.Fatal(e)
	}
	_, e = r.Body().Read(make([]byte, 1))
	outboundReason(t, e, ac.Timeout, true)
	r.Close()
}
func TestOutboundNetworkRedirectStripsAllCallerAuthentication(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	dest := f.scenario(t, netfixture.ScenarioConfig{Body: "destination"})
	origin, _ := outbound.ParseOrigin("https://fixture.test:8443")
	material, _ := sc.NewSecretMaterial([]byte("redirect-credential-private-canary"))
	defer material.Destroy()
	header, _ := outbound.HeaderCredential("Authorization", "Bearer ", material)
	query, _ := outbound.QueryCredential("token", material)
	binding, _ := outbound.NewCredentialBinding(origin, header, query)
	for _, leak := range []bool{false, true} {
		address := "https://other.fixture.test:8443/case/" + dest + "?fresh=1"
		if leak {
			address += "&copy=redirect-credential-private-canary"
		}
		start := f.scenario(t, netfixture.ScenarioConfig{Mode: "redirect", Redirect: address})
		req, _ := http.NewRequest("GET", f.url(start), nil)
		req.Header.Set("X-Custom-Authentication", "caller-private-canary")
		p := f.profile(t, outbound.ProfileOptions{Credentials: binding})
		r, e := f.client.Do(auditContext(t), req, p)
		if leak {
			outboundReason(t, e, ac.CredentialDenied, true)
		} else {
			if e != nil || readOutbound(t, r) != "destination" {
				t.Fatal(e)
			}
			request := f.state(t, dest).Requests[0]
			if request.Headers.Get("Authorization") != "" || request.Headers.Get("X-Custom-Authentication") != "" || request.Query != "fresh=1" {
				t.Fatal("cross origin retained caller auth/query")
			}
		}
	}
	if count := len(f.state(t, dest).Requests); count != 1 {
		t.Fatal("credential leak redirect reached destination", count)
	}
}
