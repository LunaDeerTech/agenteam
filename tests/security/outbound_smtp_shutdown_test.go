//go:build integration

package security_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

type smtpStopResolver struct {
	ip               netip.Addr
	calls            atomic.Int64
	entered, release chan struct{}
}

func (r *smtpStopResolver) Lookup(ctx context.Context, _ string) ([]netip.Addr, error) {
	if r.calls.Add(1) == 2 {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []netip.Addr{r.ip}, ctx.Err()
}
func smtpConnectionID(t *testing.T, f *outboundNetwork, scenario string, conn *outbound.Conn) int64 {
	t.Helper()
	address := conn.LocalAddr().String()
	var found int64
	awaitNetworkState(t, f, scenario, func(state netfixture.State) bool {
		for id, peer := range state.Connections {
			if peer == address {
				found = id
				return true
			}
		}
		return false
	})
	return found
}
func drainSMTPClient(t *testing.T, c *outbound.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(auditContext(t), time.Second)
	defer cancel()
	if err := c.Drain(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestOutboundNetworkSMTPStopRejectsNewSendBeforeAndDuringDNS(t *testing.T) {
	for _, duringDNS := range []bool{false, true} {
		t.Run(fmt.Sprint(duringDNS), func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8080)
			r := &smtpStopResolver{ip: f.resolver.ip, entered: make(chan struct{}), release: make(chan struct{})}
			trust, _ := outbound.LoadTrustStore(f.net.CAFile)
			client, err := outbound.NewClient(f.policy, trust, r)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if e := client.ForceClose(ctx); e != nil {
					t.Error(e)
				}
			})
			conn, err := client.DialTarget(auditContext(t), "fixture.test", 8080, f.smtpProfile(t))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			id := f.scenario(t, netfixture.ScenarioConfig{})
			connection := smtpConnectionID(t, f, id, conn)
			if duringDNS {
				done := make(chan error, 1)
				go func() { done <- conn.BeginSend(auditContext(t)) }()
				awaitOutbound(t, r.entered)
				client.StopAdmission()
				close(r.release)
				err = awaitOutbound(t, done)
			} else {
				client.StopAdmission()
				err = conn.BeginSend(auditContext(t))
				if r.calls.Load() != 1 {
					t.Fatal("stopped client began new DNS")
				}
			}
			outboundReason(t, err, ac.PolicyUnavailable, false)
			if n, e := conn.Write([]byte("GET /case/" + id + " HTTP/1.1\r\nHost: fixture.test\r\n\r\n")); n != 0 || e == nil {
				t.Fatal("rejected send retained connection", n, e)
			}
			state := awaitNetworkState(t, f, id, func(state netfixture.State) bool { return state.Closed[connection] })
			if len(state.Requests) != 0 {
				t.Fatal("shutdown rejection sent request")
			}
			drainSMTPClient(t, client)
		})
	}
}

// Synthetic HTTP observes the dial port's in-flight bytes; D07 still owns real
// SMTP AUTH/mail protocol integration and must call BeginSend before each.
func TestOutboundNetworkSMTPAdmittedSendCompletesAfterStop(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprint(sent), func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8080)
			conn, e := f.client.DialTarget(auditContext(t), "fixture.test", 8080, f.smtpProfile(t))
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			id := f.scenario(t, netfixture.ScenarioConfig{Mode: "stream", Body: "open"})
			if e = conn.BeginSend(auditContext(t)); e != nil {
				t.Fatal(e)
			}
			if sent {
				if _, e = fmt.Fprintf(conn, "GET /case/%s HTTP/1.1\r\n", id); e != nil {
					t.Fatal(e)
				}
				if !conn.Decision().Sent {
					t.Fatal("real partial write not counted")
				}
			}
			f.client.StopAdmission()
			if !sent {
				if _, e = fmt.Fprintf(conn, "GET /case/%s HTTP/1.1\r\n", id); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = io.WriteString(conn, "Host: fixture.test:8080\r\n\r\n"); e != nil {
				t.Fatal(e)
			}
			response, e := http.ReadResponse(bufio.NewReader(conn), nil)
			if e != nil {
				t.Fatal(e)
			}
			first := make([]byte, 4)
			if _, e = io.ReadFull(response.Body, first); e != nil || string(first) != "open" {
				t.Fatal(e)
			}
			if e = f.net.Release(auditContext(t), id); e != nil {
				t.Fatal(e)
			}
			if _, e = io.Copy(io.Discard, response.Body); e != nil {
				t.Fatal(e)
			}
			response.Body.Close()
			if e = conn.EndSend(); e != nil {
				t.Fatal(e)
			}
			outboundReason(t, conn.BeginSend(auditContext(t)), ac.PolicyUnavailable, false)
			if state := f.state(t, id); len(state.Requests) != 1 {
				t.Fatal("old send was replayed", len(state.Requests))
			}
			drainSMTPClient(t, f.client)
		})
	}
}

func TestOutboundNetworkSMTPBeginSendDenialAuditPreservesRefusal(t *testing.T) {
	for _, mode := range []string{"success", "revoked", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			f := newOutboundNetwork(t)
			f.allow(t, false, 8080)
			conn, e := f.client.DialTarget(auditContext(t), "fixture.test", 8080, f.smtpProfile(t))
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			id := f.scenario(t, netfixture.ScenarioConfig{})
			connection := smtpConnectionID(t, f, id, conn)
			f.forbid(t)
			if mode == "revoked" {
				if _, e = f.f.store.Exec(auditContext(t), `UPDATE audit_fixture.sessions SET active=false`); e != nil {
					t.Fatal(e)
				}
			}
			var unlock func()
			if mode == "blocked" {
				guard := f.f.db.Connect(t)
				if _, e = guard.Exec(auditContext(t), `BEGIN; LOCK TABLE agenteam_audit.audit_records IN ACCESS EXCLUSIVE MODE`); e != nil {
					t.Fatal(e)
				}
				unlock = func() {
					if _, e := guard.Exec(auditContext(t), `ROLLBACK`); e != nil {
						t.Fatal(e)
					}
				}
			}
			done := make(chan error, 1)
			go func() { done <- conn.BeginSend(auditContext(t)) }()
			state := awaitNetworkState(t, f, id, func(state netfixture.State) bool { return state.Closed[connection] })
			if len(state.Requests) != 0 {
				t.Fatal("denied send reached server")
			}
			if unlock != nil {
				select {
				case e := <-done:
					t.Fatal("Audit did not wait for owned table lock", e)
				default:
				}
				unlock()
			}
			outboundReason(t, awaitOutbound(t, done), ac.PrivateNotAllowed, false)
			if n, e := conn.Write([]byte("forbidden")); n != 0 || e == nil {
				t.Fatal("denial retained old attempt")
			}
			var count int
			if e = f.f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='outbound.access.deny' AND scope='system'`).Scan(&count); e != nil {
				t.Fatal(e)
			}
			want := 1
			if mode == "revoked" {
				want = 0
			}
			if count != want {
				t.Fatal("denial Audit count", count, want)
			}
			f.client.StopAdmission()
			drainSMTPClient(t, f.client)
		})
	}
}
