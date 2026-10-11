package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

// These are real ephemeral loopback sockets. The container's fixed port 53,
// private resolv.conf mount and actual App consumer are separate native checks.
func TestFixtureDNSExactAnswersAndOwnedClose(t *testing.T) {
	address := netip.MustParseAddr("127.0.0.1")
	dns, err := newFixtureDNS(address, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dns.Close(); err != nil || !dns.Joined() {
			t.Error("fixture DNS did not actually join", err)
		}
	})
	if dns.Joined() {
		t.Fatal("live DNS reported Joined")
	}
	endpoint := dns.tcp.Addr().String()
	if dns.udp.LocalAddr().String() != endpoint {
		t.Fatal("UDP and TCP do not share the same owned port")
	}
	for _, kind := range []uint16{1, 28} {
		query := fixtureDNSQuery("fixture.test", kind, 1)
		udp := fixtureDNSExchange(t, "udp4", endpoint, query)
		tcp := fixtureDNSExchange(t, "tcp4", endpoint, query)
		if !bytes.Equal(udp, tcp) {
			t.Fatalf("UDP/TCP disagree for question type %d", kind)
		}
		if len(udp) < len(query) || !bytes.Equal(udp[:2], query[:2]) ||
			binary.BigEndian.Uint16(udp[2:]) != 0x8500 ||
			binary.BigEndian.Uint16(udp[4:]) != 1 ||
			binary.BigEndian.Uint16(udp[8:]) != 0 || binary.BigEndian.Uint16(udp[10:]) != 0 ||
			!bytes.Equal(udp[12:len(query)], query[12:]) {
			t.Fatalf("question type %d lost identity, authoritative answer or no-recursion boundary", kind)
		}
		if kind == 1 {
			ip := address.As4()
			if binary.BigEndian.Uint16(udp[6:]) != 1 || len(udp) != len(query)+16 ||
				!bytes.Equal(udp[len(udp)-4:], ip[:]) {
				t.Fatal("A did not return exactly the configured IPv4 address")
			}
		} else if binary.BigEndian.Uint16(udp[6:]) != 0 || len(udp) != len(query) {
			t.Fatal("AAAA must be a complete NOERROR/NODATA response")
		}
	}
	// Consume the actual wire answers through the production complete-family
	// decoder, rather than accepting only a fixture's own response shape.
	resolver, err := outbound.NewDNSResolver([]netip.AddrPort{netip.MustParseAddrPort(endpoint)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	addresses, err := resolver.Lookup(ctx, "fixture.test")
	if err != nil || len(addresses) != 1 || addresses[0] != address {
		t.Fatal("production DNS decoder did not observe the exact complete address set", addresses, err)
	}
	if a, aaaa := dns.Counts(); a != 3 || aaaa != 3 {
		t.Fatalf("both transports and production lookup must complete both families: A=%d AAAA=%d", a, aaaa)
	}

	malformed := fixtureDNSQuery("fixture.test", 1, 1)
	binary.BigEndian.PutUint16(malformed[4:], 2)
	for _, rejected := range []struct {
		name  string
		query []byte
		code  uint16
	}{
		{"foreign name", fixtureDNSQuery("outside.test", 1, 1), 5},
		{"unsupported type", fixtureDNSQuery("fixture.test", 15, 1), 5},
		{"unsupported class", fixtureDNSQuery("fixture.test", 1, 3), 5},
		{"malformed question count", malformed, 1},
	} {
		response := fixtureDNSExchange(t, "udp4", endpoint, rejected.query)
		if len(response) < 12 || !bytes.Equal(response[:2], rejected.query[:2]) ||
			binary.BigEndian.Uint16(response[2:])&0x800f != 0x8000|rejected.code ||
			binary.BigEndian.Uint16(response[2:])&0x0080 != 0 ||
			binary.BigEndian.Uint16(response[6:]) != 0 ||
			binary.BigEndian.Uint16(response[8:]) != 0 || binary.BigEndian.Uint16(response[10:]) != 0 {
			t.Fatalf("%s was not refused without recursive or address facts", rejected.name)
		}
	}
	if a, aaaa := dns.Counts(); a != 3 || aaaa != 3 {
		t.Fatal("rejected questions were counted as owned family answers")
	}

	held, err := net.DialTimeout("tcp4", endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	if _, err := held.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	// Wait for this exact socket to be accepted before stopping, so Joined
	// proves retirement of a real partial-frame handler as well as listeners.
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for accepted := false; !accepted; {
		dns.mu.Lock()
		for conn := range dns.conns {
			accepted = accepted || conn.RemoteAddr().String() == held.LocalAddr().String()
		}
		dns.mu.Unlock()
		if accepted {
			break
		}
		select {
		case <-timer.C:
			t.Fatal("partial-frame socket was never accepted")
		case <-tick.C:
		}
	}
	if err := dns.Close(); err != nil || !dns.Joined() {
		t.Fatal("Close returned before actual DNS retirement", err)
	}
	dns.mu.Lock()
	remaining := len(dns.conns)
	dns.mu.Unlock()
	if remaining != 0 {
		t.Fatal("Joined retained accepted TCP handlers")
	}
	if err := held.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	if n, err := held.Read(one[:]); n != 0 || err == nil {
		t.Fatal("partial-frame peer remained open after Joined", n, err)
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("partial-frame peer reached a deadline instead of observing closure")
	}
	if _, err := dns.tcp.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatal("TCP listener remained live after Joined", err)
	}
	if _, _, err := dns.udp.ReadFromUDP(one[:]); !errors.Is(err, net.ErrClosed) {
		t.Fatal("UDP listener remained live after Joined", err)
	}
	if err := dns.Close(); err != nil || !dns.Joined() {
		t.Fatal("repeated Close lost the original completed retirement", err)
	}
}

func fixtureDNSQuery(name string, kind, class uint16) []byte {
	query := []byte{0x3c, 0x5d, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range strings.Split(name, ".") {
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	return append(query, 0, byte(kind>>8), byte(kind), byte(class>>8), byte(class))
}

func fixtureDNSExchange(t *testing.T, network, endpoint string, query []byte) []byte {
	t.Helper()
	conn, err := net.DialTimeout(network, endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	packet := query
	if network == "tcp4" {
		packet = append([]byte{byte(len(query) >> 8), byte(len(query))}, query...)
	}
	for len(packet) > 0 {
		n, err := conn.Write(packet)
		if err != nil || n == 0 {
			t.Fatal("DNS query did not finish writing", err)
		}
		packet = packet[n:]
	}
	if network == "udp4" {
		raw := make([]byte, 513)
		n, err := conn.Read(raw)
		if err != nil || n == len(raw) {
			t.Fatal("DNS UDP response did not complete", err)
		}
		return raw[:n]
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		t.Fatal("DNS TCP response header did not complete", err)
	}
	n := int(binary.BigEndian.Uint16(size[:]))
	if n < 12 || n > 512 {
		t.Fatal("DNS TCP response length is outside the fixture bound")
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(conn, raw); err != nil {
		t.Fatal("DNS TCP response did not complete", err)
	}
	return raw
}
