package outbound

import (
	"context"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

// The business guard must remain authoritative while the policy writer owns its
// gate. Expiry must join the blocked callback before the policy is released.
func TestModelSendGuardPolicyWaitHonorsAuthorizationDeadline(t *testing.T) {
	rule, _ := NewRule("10.20.0.0/16", AllPorts())
	rules, _ := NewRules(rule)
	ps := &policyState{mirror: policy(1, Allowlist, rules), available: true, proxy: proxySnapshot{version: 1, mode: "direct"}, proxyAvailable: true}
	service := &PolicyService{data: func() *policyState { return ps }}
	state := &clientState{policy: service, connections: map[*pooledConn]bool{}, changed: make(chan struct{})}
	client := &Client{data: func() *clientState { return state }}
	target, _ := ParseTarget("http://10.20.1.1:8080/")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int32
	ctx = WithModelSendGuard(ctx, func(guard context.Context, write func() (int, error)) (int, error) { calls.Add(1); return write() })
	a := &attempt{route: proxySnapshot{version: 1, mode: "direct"}, ctx: ctx, client: client, target: target, addresses: []netip.Addr{netip.MustParseAddr("10.20.1.1")}, profile: ProfileOptions{Consumer: ac.Model}}
	conn, peer := net.Pipe()
	defer peer.Close()
	pc := &pooledConn{conn: conn, raw: conn, routeVersion: 1, origin: target.Origin(), peer: a.addresses[0], owner: state}
	state.connections[pc] = true
	defer pc.close()
	release, err := ps.gate.acquire(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := (&firstWriter{a: a, pc: pc, first: true}).Write([]byte("GET / HTTP/1.1\r\n\r\n"))
		done <- err
	}()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("expired authorization wrote request")
		}
	case <-ctx.Done():
		t.Fatal("guard did not cancel policy wait and join")
	}
	if calls.Load() != 1 || a.sent.Load() || ctx.Err() != nil {
		t.Fatal("guard lifetime escaped its own deadline")
	}
	release()
	_ = peer.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	b := make([]byte, 1)
	n, _ := peer.Read(b)
	if n != 0 {
		t.Fatal("late request bytes after guard ended")
	}
}
