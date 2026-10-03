//go:build integration

package security_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

type smtpBarrierResolver struct {
	ip      netip.Addr
	calls   atomic.Int64
	entered chan struct{}
}

func (r *smtpBarrierResolver) Lookup(ctx context.Context, _ string) ([]netip.Addr, error) {
	if r.calls.Add(1) == 2 {
		close(r.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []netip.Addr{r.ip}, nil
}

func TestOutboundNetworkSMTPConcurrentBeginSendCannotReuseOldAttempt(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8080)
	resolver := &smtpBarrierResolver{ip: f.resolver.ip, entered: make(chan struct{})}
	trust, _ := outbound.LoadTrustStore(f.net.CAFile)
	client, e := outbound.NewClient(f.policy, trust, resolver)
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
	conn, e := client.DialTarget(auditContext(t), "fixture.test", 8080, f.smtpProfile(t))
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- conn.BeginSend(auditContext(t)) }()
	awaitOutbound(t, resolver.entered)
	if _, e := conn.Write([]byte("concurrent")); e == nil {
		t.Fatal("concurrent write accepted")
	}
	if e := conn.BeginSend(auditContext(t)); e == nil {
		t.Fatal("concurrent BeginSend accepted")
	}
	if e := awaitOutbound(t, done); e == nil {
		t.Fatal("cancelled BeginSend accepted")
	}
	if _, e := conn.Write([]byte("old")); e == nil {
		t.Fatal("failed BeginSend retained old attempt")
	}
}

func (f *outboundNetwork) smtpProfile(t *testing.T) outbound.SMTPProfile {
	t.Helper()
	call, e := outbound.NewCallContext(f.f.actor, identity.SystemScope(), appendKey(t, ac.AccessProducer), ac.Associations{})
	if e != nil {
		t.Fatal(e)
	}
	p, e := outbound.NewSMTPProfile(call, outbound.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	return p
}

// The fixture exchanges synthetic HTTP bytes through the SMTP dial port to
// observe encrypted underlying writes. This is not an SMTP/AUTH integration.
func TestOutboundNetworkSMTPDialTLSSetupAndPerSendAdmission(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, false, 8443)
	p := f.smtpProfile(t)
	conn, e := f.client.DialTarget(auditContext(t), "fixture.test", 8443, p)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	raw, e := os.ReadFile(f.net.CAFile)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		t.Fatal("fixture CA")
	}
	secured := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "fixture.test", NextProtos: []string{"http/1.1"}})
	if e := secured.HandshakeContext(auditContext(t)); e != nil {
		t.Fatal(e)
	}
	if conn.Decision().Sent {
		t.Fatal("TLS negotiation counted as mail send")
	}
	if e := conn.BeginSend(auditContext(t)); e != nil {
		t.Fatal(e)
	}
	id := f.scenario(t, netfixture.ScenarioConfig{Body: "synthetic protocol bytes"})
	if _, e := fmt.Fprintf(secured, "GET /case/%s HTTP/1.1\r\nHost: fixture.test:8443\r\n\r\n", id); e != nil {
		t.Fatal(e)
	}
	response, e := http.ReadResponse(bufio.NewReader(secured), nil)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil || string(body) != "synthetic protocol bytes" {
		t.Fatal(e)
	}
	if !conn.Decision().Sent || conn.Decision().Consumer != ac.SMTP {
		t.Fatal("underlying TLS write not counted")
	}
	if e := conn.EndSend(); e != nil {
		t.Fatal(e)
	}
	if _, e := secured.Write([]byte("without BeginSend")); e == nil {
		t.Fatal("write allowed between sends")
	}
	f.forbid(t)
	outboundReason(t, conn.BeginSend(auditContext(t)), ac.PrivateNotAllowed, false)
	if _, e := conn.Write([]byte("old attempt")); e == nil {
		t.Fatal("failed BeginSend allowed old attempt")
	}
	if count := len(f.state(t, id).Requests); count != 1 {
		t.Fatal("SMTP port resent bytes", count)
	}
}

func TestOutboundNetworkSMTPFirstWriteChecksCurrentPolicyAndCancel(t *testing.T) {
	for _, mode := range []string{"policy", "rebind", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8080)
			ctx, cancel := context.WithCancel(auditContext(t))
			defer cancel()
			conn, e := f.client.DialTarget(ctx, "fixture.test", 8080, f.smtpProfile(t))
			if e != nil {
				t.Fatal("allow_http must not apply to SMTP", e)
			}
			defer conn.Close()
			id := f.scenario(t, netfixture.ScenarioConfig{})
			switch mode {
			case "policy":
				if e := conn.BeginSend(ctx); e != nil {
					t.Fatal(e)
				}
				f.forbid(t)
				_, e = conn.Write([]byte("GET /case/" + id + " HTTP/1.1\r\nHost: fixture.test\r\n\r\n"))
				outboundReason(t, e, ac.PrivateNotAllowed, false)
			case "rebind":
				f.resolver.rebind.Store(true)
				outboundReason(t, conn.BeginSend(ctx), ac.AddressForbidden, false)
			case "cancel":
				if e := conn.BeginSend(ctx); e != nil {
					t.Fatal(e)
				}
				done := make(chan error, 1)
				go func() { _, e := conn.Read(make([]byte, 1)); done <- e }()
				cancel()
				outboundReason(t, awaitOutbound(t, done), ac.Cancelled, false)
			}
			conn.Close()
			if len(f.state(t, id).Requests) != 0 {
				t.Fatal("denied SMTP attempt sent bytes")
			}
			f.client.StopAdmission()
			drain, stop := context.WithTimeout(auditContext(t), time.Second)
			defer stop()
			if e := f.client.Drain(drain); e != nil {
				t.Fatal(e)
			}
		})
	}
}
