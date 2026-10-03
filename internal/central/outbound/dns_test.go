package outbound

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dnsFixture struct {
	udp                *net.UDPConn
	tcp                net.Listener
	wg                 sync.WaitGroup
	mu                 sync.Mutex
	sockets            map[net.Conn]bool
	udpCount, tcpCount atomic.Int64
}

func newDNSFixture(t *testing.T, reply func([]byte, string) []byte) *dnsFixture {
	t.Helper()
	tcp, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := tcp.Addr().(*net.TCPAddr)
	udp, e := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: addr.Port})
	if e != nil {
		tcp.Close()
		t.Fatal(e)
	}
	f := &dnsFixture{udp: udp, tcp: tcp, sockets: map[net.Conn]bool{}}
	f.wg.Add(2)
	go func() {
		defer f.wg.Done()
		buf := make([]byte, 65536)
		for {
			n, peer, e := udp.ReadFromUDP(buf)
			if e != nil {
				return
			}
			f.udpCount.Add(1)
			query := slices.Clone(buf[:n])
			response := reply(query, "udp")
			if response != nil {
				_, _ = udp.WriteToUDP(response, peer)
			}
		}
	}()
	go func() {
		defer f.wg.Done()
		for {
			conn, e := tcp.Accept()
			if e != nil {
				return
			}
			f.mu.Lock()
			f.sockets[conn] = true
			f.mu.Unlock()
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				defer func() { conn.Close(); f.mu.Lock(); delete(f.sockets, conn); f.mu.Unlock() }()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				var size [2]byte
				if _, e := io.ReadFull(conn, size[:]); e != nil {
					return
				}
				query := make([]byte, int(binary.BigEndian.Uint16(size[:])))
				if _, e := io.ReadFull(conn, query); e != nil {
					return
				}
				f.tcpCount.Add(1)
				response := reply(query, "tcp")
				if response == nil {
					return
				}
				binary.BigEndian.PutUint16(size[:], uint16(len(response)))
				_, _ = conn.Write(size[:])
				_, _ = conn.Write(response)
			}()
		}
	}()
	t.Cleanup(func() {
		udp.Close()
		tcp.Close()
		f.mu.Lock()
		for conn := range f.sockets {
			conn.Close()
		}
		f.mu.Unlock()
		f.wg.Wait()
	})
	return f
}
func (f *dnsFixture) resolver(t *testing.T) *DNSResolver {
	t.Helper()
	addr := f.tcp.Addr().(*net.TCPAddr)
	r, e := NewDNSResolver([]netip.AddrPort{netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(addr.Port))})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func dnsQueryFields(query []byte) (uint16, string, uint16) {
	name, next, _ := dnsName(query, 12)
	return binary.BigEndian.Uint16(query), name, binary.BigEndian.Uint16(query[next:])
}
func responseDNS(query []byte, rcode uint16, answers ...dnsRecord) []byte {
	_, pos, _ := dnsName(query, 12)
	b := slices.Clone(query[:pos+4])
	binary.BigEndian.PutUint16(b[2:], 0x8180|rcode)
	binary.BigEndian.PutUint16(b[6:], uint16(len(answers)))
	for _, rr := range answers {
		b = appendDNSName(b, rr.owner)
		b = append(b, byte(rr.kind>>8), byte(rr.kind), 0, 1, 0, 0, 0, 30)
		var data []byte
		if rr.kind == 5 {
			data = appendDNSName(nil, rr.alias)
		} else if rr.kind == 1 {
			a := rr.address.As4()
			data = a[:]
		} else {
			a := rr.address.As16()
			data = a[:]
		}
		b = append(b, byte(len(data)>>8), byte(len(data)))
		b = append(b, data...)
	}
	return b
}
func appendDNSName(b []byte, name string) []byte {
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			b = append(b, byte(i-start))
			b = append(b, name[start:i]...)
			start = i + 1
		}
	}
	return append(b, 0)
}
func dnsA(owner, ip string) dnsRecord {
	addr := netip.MustParseAddr(ip)
	kind := uint16(28)
	if addr.Is4() {
		kind = 1
	}
	return dnsRecord{owner: owner, kind: kind, address: addr}
}
func TestDNSRealDualStackNODATAAndTCPFallback(t *testing.T) {
	for _, mode := range []string{"dual", "nodata4", "nodata6", "tcp"} {
		t.Run(mode, func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, network string) []byte {
				_, name, kind := dnsQueryFields(q)
				if mode == "tcp" && network == "udp" {
					b := responseDNS(q, 0)
					binary.BigEndian.PutUint16(b[2:], 0x8380)
					return b
				}
				if kind == 1 && mode != "nodata4" {
					return responseDNS(q, 0, dnsA(name, "8.8.8.8"))
				}
				if kind == 28 && mode != "nodata6" {
					return responseDNS(q, 0, dnsA(name, "2001:4860::8888"))
				}
				return responseDNS(q, 0)
			})
			ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example")
			want := 2
			if mode == "nodata4" || mode == "nodata6" {
				want = 1
			}
			if e != nil || len(ips) != want {
				t.Fatal(ips, e)
			}
			if f.udpCount.Load() != 2 || mode == "tcp" && f.tcpCount.Load() != 2 {
				t.Fatal("families/fallback not queried")
			}
		})
	}
}
func TestDNSAnyFamilyErrorRejectsCompleteLookup(t *testing.T) {
	for _, rcode := range []uint16{1, 2, 3, 4, 5} {
		t.Run(string(rune('0'+rcode)), func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, _ string) []byte {
				_, name, kind := dnsQueryFields(q)
				if kind == 1 {
					return responseDNS(q, 0, dnsA(name, "8.8.8.8"))
				}
				return responseDNS(q, rcode)
			})
			if ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example"); e == nil || len(ips) != 0 {
				t.Fatal("partial family accepted", ips)
			}
		})
	}
	for _, damage := range []string{"id", "question", "kind", "class", "short", "count", "pointer", "rcode", "tc_tcp", "owner", "cname_conflict"} {
		t.Run(damage, func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, network string) []byte {
				_, name, kind := dnsQueryFields(q)
				if kind == 1 {
					return responseDNS(q, 0, dnsA(name, "8.8.8.8"))
				}
				rr := dnsA(name, "2001:4860::8888")
				b := responseDNS(q, 0, rr)
				_, end, _ := dnsName(b, 12)
				switch damage {
				case "id":
					b[0] ^= 1
				case "question":
					b[13] = 'X'
				case "kind":
					b[end+1] = 1
				case "class":
					b[end+3] = 3
				case "short":
					b = b[:len(b)-1]
				case "count":
					b[6] = 8
				case "pointer":
					b[12] = 0xc0
					b[13] = 12
				case "rcode":
					b[3] |= 1
				case "tc_tcp":
					binary.BigEndian.PutUint16(b[2:], 0x8380)
				case "owner":
					b = responseDNS(q, 0, dnsA("unrelated.example", "2001:4860::8888"))
				case "cname_conflict":
					b = responseDNS(q, 0, rr, dnsRecord{owner: name, kind: 5, alias: "alias.example"})
				}
				return b
			})
			if ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example"); e == nil || len(ips) != 0 {
				t.Fatal("bad DNS response accepted", ips)
			}
		})
	}
}
func TestDNSCNAMEChainCycleAndAddressLimit(t *testing.T) {
	for _, mode := range []string{"chain", "inline", "cycle", "conflict", "64", "65", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, _ string) []byte {
				_, name, kind := dnsQueryFields(q)
				if kind == 28 {
					return responseDNS(q, 0)
				}
				switch mode {
				case "chain":
					if name == "fixture.example" {
						return responseDNS(q, 0, dnsRecord{owner: name, kind: 5, alias: "alias.example"})
					}
					return responseDNS(q, 0, dnsA(name, "8.8.8.8"))
				case "inline":
					return responseDNS(q, 0, dnsRecord{owner: name, kind: 5, alias: "alias.example"}, dnsA("alias.example", "8.8.8.8"))
				case "cycle":
					return responseDNS(q, 0, dnsRecord{owner: name, kind: 5, alias: "alias.example"}, dnsRecord{owner: "alias.example", kind: 5, alias: name})
				case "conflict":
					return responseDNS(q, 0, dnsRecord{owner: name, kind: 5, alias: "one.example"}, dnsRecord{owner: name, kind: 5, alias: "two.example"})
				default:
					count := 65
					if mode == "64" {
						count = 64
					}
					var answers []dnsRecord
					for i := 0; i < count; i++ {
						n := i + 1
						if mode == "duplicate" {
							n = 1
						}
						answers = append(answers, dnsRecord{owner: name, kind: 1, address: netip.AddrFrom4([4]byte{8, 8, 8, byte(n)})})
					}
					return responseDNS(q, 0, answers...)
				}
			})
			ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example")
			bad := mode == "cycle" || mode == "conflict" || mode == "65"
			if bad && e == nil || !bad && e != nil {
				t.Fatal("chain/limit result", e)
			}
			if mode == "64" && len(ips) != 64 || mode == "duplicate" && len(ips) != 1 {
				t.Fatal("wrong dedup/limit", len(ips))
			}
		})
	}
}
func TestDNSCancellationClosesActualQuerySockets(t *testing.T) {
	reached := make(chan struct{}, 2)
	f := newDNSFixture(t, func(q []byte, _ string) []byte { reached <- struct{}{}; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := f.resolver(t).Lookup(ctx, "fixture.example"); done <- e }()
	<-reached
	<-reached
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancel success")
		}
	case <-time.After(time.Second):
		t.Fatal("DNS cancel hung")
	}
}

// Wire-only encoders below deliberately do not use the production name parser
// or the normal test hostname encoder to reproduce the label/referral defects.
func rawDNSName(labels ...string) []byte {
	var b []byte
	for _, label := range labels {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0)
}
func rawDNSRecord(owner []byte, kind uint16, payload []byte) []byte {
	b := append([]byte(nil), owner...)
	b = append(b, byte(kind>>8), byte(kind), 0, 1, 0, 0, 0, 30, byte(len(payload)>>8), byte(len(payload)))
	return append(b, payload...)
}
func rawDNSResponse(q []byte, flags uint16, answers, authority [][]byte) []byte {
	b := append([]byte(nil), q...)
	binary.BigEndian.PutUint16(b[2:], flags)
	binary.BigEndian.PutUint16(b[6:], uint16(len(answers)))
	binary.BigEndian.PutUint16(b[8:], uint16(len(authority)))
	for _, r := range answers {
		b = append(b, r...)
	}
	for _, r := range authority {
		b = append(b, r...)
	}
	return b
}
func TestDNSRejectsCollapsedWireLabels(t *testing.T) {
	for _, where := range []string{"question", "owner", "cname"} {
		t.Run(where, func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, _ string) []byte {
				if binary.BigEndian.Uint16(q[len(q)-4:]) == 28 {
					return rawDNSResponse(q, 0x8180, nil, nil)
				}
				rr := rawDNSRecord([]byte{0xc0, 12}, 1, []byte{8, 8, 4, 4})
				switch where {
				case "question":
					changed := append([]byte(nil), q[:12]...)
					changed = append(changed, rawDNSName("fixture.example")...)
					changed = append(changed, 0, 1, 0, 1)
					return rawDNSResponse(changed, 0x8180, [][]byte{rr}, nil)
				case "owner":
					return rawDNSResponse(q, 0x8180, [][]byte{rawDNSRecord(rawDNSName("fixture.example"), 1, []byte{8, 8, 4, 4})}, nil)
				default:
					return rawDNSResponse(q, 0x8180, [][]byte{rawDNSRecord([]byte{0xc0, 12}, 5, rawDNSName("alias.example")), rawDNSRecord(rawDNSName("alias", "example"), 1, []byte{8, 8, 4, 4})}, nil)
				}
			})
			if ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example"); e == nil || len(ips) != 0 {
				t.Fatal("wire label boundaries collapsed")
			}
		})
	}
}
func TestDNSRejectsReferralButAllowsSOANODATA(t *testing.T) {
	for _, flags := range []uint16{0x8100, 0x8180} {
		f := newDNSFixture(t, func(q []byte, _ string) []byte {
			if binary.BigEndian.Uint16(q[len(q)-4:]) == 1 {
				return rawDNSResponse(q, 0x8180, [][]byte{rawDNSRecord([]byte{0xc0, 12}, 1, []byte{8, 8, 4, 4})}, nil)
			}
			ns := rawDNSRecord(rawDNSName("example"), 2, rawDNSName("ns", "example"))
			return rawDNSResponse(q, flags, nil, [][]byte{ns})
		})
		if ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example"); e == nil || len(ips) != 0 {
			t.Fatal("NS referral accepted as empty family")
		}
	}
	f := newDNSFixture(t, func(q []byte, _ string) []byte {
		if binary.BigEndian.Uint16(q[len(q)-4:]) == 1 {
			return rawDNSResponse(q, 0x8180, [][]byte{rawDNSRecord([]byte{0xc0, 12}, 1, []byte{8, 8, 4, 4})}, nil)
		}
		soa := append(rawDNSName("ns", "example"), rawDNSName("hostmaster", "example")...)
		soa = append(soa, make([]byte, 20)...)
		return rawDNSResponse(q, 0x8180, nil, [][]byte{rawDNSRecord(rawDNSName("example"), 6, soa)})
	})
	if ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example"); e != nil || len(ips) != 1 {
		t.Fatal("valid SOA NODATA rejected", e)
	}
}

func TestDNSSOAMustBeValidNegativeEvidence(t *testing.T) {
	for _, mode := range []string{"empty_soa", "short_soa", "trailing_soa", "malformed_soa_name", "unrelated_soa", "empty_ns"} {
		t.Run(mode, func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, _ string) []byte {
				if binary.BigEndian.Uint16(q[len(q)-4:]) == 1 {
					return rawDNSResponse(q, 0x8180, [][]byte{rawDNSRecord([]byte{0xc0, 12}, 1, []byte{8, 8, 4, 4})}, nil)
				}
				nsPayload := rawDNSName("ns", "example")
				soaPayload := append(rawDNSName("ns", "example"), rawDNSName("hostmaster", "example")...)
				soaPayload = append(soaPayload, make([]byte, 20)...)
				owner := rawDNSName("example")
				switch mode {
				case "empty_soa":
					soaPayload = nil
				case "short_soa":
					soaPayload = soaPayload[:len(soaPayload)-1]
				case "trailing_soa":
					soaPayload = append(soaPayload, 0)
				case "malformed_soa_name":
					soaPayload[0] = 0xff
				case "unrelated_soa":
					owner = rawDNSName("unrelated", "invalid")
				case "empty_ns":
					nsPayload = nil
				}
				ns := rawDNSRecord(rawDNSName("example"), 2, nsPayload)
				soa := rawDNSRecord(owner, 6, soaPayload)
				return rawDNSResponse(q, 0x8180, nil, [][]byte{ns, soa})
			})
			ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example")
			if e == nil || len(ips) != 0 {
				t.Fatalf("invalid negative evidence accepted as completed family: mode=%s ips=%v error=%v", mode, ips, e)
			}
		})
	}
}

func TestDNSLegalSOAMailboxLabels(t *testing.T) {
	for _, local := range []string{"hostmaster", "host_master", "host.master"} {
		t.Run(local, func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, _ string) []byte {
				if binary.BigEndian.Uint16(q[len(q)-4:]) == 1 {
					return rawDNSResponse(q, 0x8180, [][]byte{rawDNSRecord([]byte{0xc0, 12}, 1, []byte{8, 8, 4, 4})}, nil)
				}
				soaPayload := append(rawDNSName("ns", "example"), rawDNSName(local, "example")...)
				soaPayload = append(soaPayload, make([]byte, 20)...)
				soa := rawDNSRecord(rawDNSName("example"), 6, soaPayload)
				ns := rawDNSRecord(rawDNSName("example"), 2, rawDNSName("ns", "example"))
				return rawDNSResponse(q, 0x8180, nil, [][]byte{ns, soa})
			})
			ips, e := f.resolver(t).Lookup(context.Background(), "fixture.example")
			if e != nil || len(ips) != 1 {
				t.Fatalf("valid SOA mailbox name rejected: local=%q ips=%v error=%v", local, ips, e)
			}
		})
	}
}

func TestDNSCrossZoneCNAMEWithTerminalNODATA(t *testing.T) {
	for _, withNS := range []bool{false, true} {
		name := "soa_only"
		if withNS {
			name = "soa_and_ns"
		}
		t.Run(name, func(t *testing.T) {
			f := newDNSFixture(t, func(q []byte, _ string) []byte {
				// Queries are ordinary names produced by the resolver itself.
				terminalQuery := int(q[12]) == 7 && string(q[13:20]) == "service"
				var answers [][]byte
				if !terminalQuery {
					answers = append(answers, rawDNSRecord([]byte{0xc0, 12}, 5, rawDNSName("service", "other")))
				}
				if binary.BigEndian.Uint16(q[len(q)-4:]) == 1 {
					answers = append(answers, rawDNSRecord(rawDNSName("service", "other"), 1, []byte{8, 8, 4, 4}))
					return rawDNSResponse(q, 0x8180, answers, nil)
				}
				soaPayload := append(rawDNSName("ns", "other"), rawDNSName("host_master", "other")...)
				soaPayload = append(soaPayload, make([]byte, 20)...)
				authority := [][]byte{rawDNSRecord(rawDNSName("other"), 6, soaPayload)}
				if withNS {
					authority = append(authority, rawDNSRecord(rawDNSName("other"), 2, rawDNSName("ns", "other")))
				}
				return rawDNSResponse(q, 0x8180, answers, authority)
			})
			ips, e := f.resolver(t).Lookup(context.Background(), "alias.example")
			if e != nil || len(ips) != 1 || ips[0] != netip.MustParseAddr("8.8.4.4") {
				t.Fatalf("valid cross-zone CNAME terminal NODATA rejected: ips=%v error=%v", ips, e)
			}
		})
	}
}
