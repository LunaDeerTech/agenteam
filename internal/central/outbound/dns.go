package outbound

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"
)

// Resolver is a composition-root dependency, never a per-request option. A
// replacement may supply DNS facts; the client still classifies every address,
// pins numeric dials, and rechecks the complete set at each send boundary.
type Resolver interface {
	Lookup(context.Context, string) ([]netip.Addr, error)
}

// DNSResolver performs bounded A AND AAAA queries. It deliberately does not
// delegate completeness to net.Resolver: Go's StrictErrors discards only
// temporary family failures and can return a subset after other DNS errors.
type DNSResolver struct{ servers func() []netip.AddrPort }

func NewDNSResolver(servers []netip.AddrPort) (*DNSResolver, error) {
	if len(servers) == 0 || len(servers) > 8 {
		return nil, invalid()
	}
	for _, s := range servers {
		if !s.IsValid() || s.Port() == 0 || s.Addr().Zone() != "" {
			return nil, invalid()
		}
	}
	owned := slices.Clone(servers)
	return &DNSResolver{servers: func() []netip.AddrPort { return owned }}, nil
}

// SystemResolver reads only fixed deployment nameserver configuration. Search
// suffixes, NSS, /etc/hosts and environment overrides are not used. DNS service
// addresses are trusted infrastructure, not runtime outbound target approvals.
func SystemResolver() (*DNSResolver, error) {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil, unavailable(err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, unavailable(err)
	}
	var servers []netip.AddrPort
	for _, line := range strings.Split(string(raw), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line, _, _ = strings.Cut(line, ";")
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "nameserver" {
			continue
		}
		if len(fields) != 2 {
			return nil, invalid()
		}
		ip, e := netip.ParseAddr(fields[1])
		if e != nil {
			return nil, invalid()
		}
		servers = append(servers, netip.AddrPortFrom(ip, 53))
	}
	return NewDNSResolver(servers)
}

var errDNS = errors.New("OUTBOUND_DNS_FAILED")

type dnsRecord struct {
	owner   string
	kind    uint16
	address netip.Addr
	alias   string
}
type dnsAnswer struct {
	records   []dnsRecord
	truncated bool
	negative  bool
}

func (r *DNSResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	name, err := normalizeHost(host)
	if err != nil {
		return nil, errDNS
	}
	if _, e := netip.ParseAddr(name); e == nil {
		return nil, errDNS
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	type result struct {
		ips []netip.Addr
		err error
	}
	done := make(chan result, 2)
	for _, kind := range []uint16{1, 28} {
		go func() { ips, e := r.family(ctx, name, kind); done <- result{ips, e} }()
	}
	var ips []netip.Addr
	var first error
	for range 2 {
		got := <-done
		if got.err != nil {
			if first == nil {
				first = got.err
			}
			cancel()
		}
		ips = append(ips, got.ips...)
	}
	if first != nil {
		return nil, errDNS
	}
	ips = uniqueIPs(ips)
	if len(ips) == 0 || len(ips) > 64 {
		return nil, errDNS
	}
	return ips, nil
}
func uniqueIPs(ips []netip.Addr) []netip.Addr {
	for i, ip := range ips {
		ips[i] = ip.Unmap()
	}
	slices.SortFunc(ips, func(a, b netip.Addr) int { return a.Compare(b) })
	return slices.Compact(ips)
}
func (r *DNSResolver) family(ctx context.Context, name string, kind uint16) ([]netip.Addr, error) {
	seen := map[string]bool{}
	for depth := 0; depth < 16; depth++ {
		if seen[name] || metadataHost(name) {
			return nil, errDNS
		}
		seen[name] = true
		answer, err := r.query(ctx, name, kind)
		if err != nil {
			return nil, err
		}
		if _, valid := dnsTerminal(answer.records, name, kind); !valid {
			return nil, errDNS
		}
		current := name
		for hop := 0; hop < 16; hop++ {
			var ips []netip.Addr
			alias := ""
			for _, rr := range answer.records {
				if rr.owner != current {
					continue
				}
				if rr.kind == 5 {
					if alias != "" && alias != rr.alias {
						return nil, errDNS
					}
					alias = rr.alias
				}
				if rr.kind == kind {
					ips = append(ips, rr.address)
				}
			}
			if alias != "" && len(ips) > 0 {
				return nil, errDNS
			}
			ips = uniqueIPs(ips)
			if len(ips) > 64 {
				return nil, errDNS
			}
			if len(ips) > 0 {
				return uniqueIPs(ips), nil
			}
			if alias == "" {
				if current == name || answer.negative {
					return nil, nil
				} // NOERROR/NODATA is a complete empty family.
				name = current
				break // CNAME without terminal answer needs another query.
			}
			if seen[alias] || metadataHost(alias) {
				return nil, errDNS
			}
			if current != name {
				seen[current] = true
			}
			current = alias
			if hop == 15 {
				return nil, errDNS
			}
		}
	}
	return nil, errDNS
}

func dnsTerminal(records []dnsRecord, name string, kind uint16) (string, bool) {
	aliases := map[string]string{}
	addresses := map[string]bool{}
	for _, rr := range records {
		if rr.kind == 5 {
			if prior := aliases[rr.owner]; prior != "" && prior != rr.alias {
				return "", false
			}
			aliases[rr.owner] = rr.alias
		} else {
			if rr.kind != kind {
				return "", false
			}
			addresses[rr.owner] = true
		}
	}
	for owner := range aliases {
		if addresses[owner] {
			return "", false
		}
	}
	path := map[string]bool{}
	for hops := 0; ; hops++ {
		if hops >= 16 || path[name] {
			return "", false
		}
		path[name] = true
		next := aliases[name]
		if next == "" {
			break
		}
		name = next
	}
	for _, rr := range records {
		if !path[rr.owner] {
			return "", false
		}
	}
	return name, true
}
func dnsQuestion(name string, kind uint16) ([]byte, uint16, error) {
	var entropy [2]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, 0, errDNS
	}
	id := binary.BigEndian.Uint16(entropy[:])
	b := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(b, id)
	binary.BigEndian.PutUint16(b[2:], 0x0100)
	binary.BigEndian.PutUint16(b[4:], 1)
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, 0, errDNS
		}
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0, byte(kind>>8), byte(kind), 0, 1)
	return b, id, nil
}
func (r *DNSResolver) query(ctx context.Context, name string, kind uint16) (dnsAnswer, error) {
	query, id, err := dnsQuestion(name, kind)
	if err != nil {
		return dnsAnswer{}, err
	}
	for _, server := range r.servers() {
		raw, err := dnsExchange(ctx, server, "udp", query)
		if err != nil {
			if ctx.Err() != nil {
				return dnsAnswer{}, errDNS
			}
			continue
		}
		answer, err := parseDNS(raw, id, name, kind, true)
		if err != nil {
			return dnsAnswer{}, errDNS
		}
		if answer.truncated {
			raw, err = dnsExchange(ctx, server, "tcp", query)
			if err != nil {
				return dnsAnswer{}, errDNS
			}
			answer, err = parseDNS(raw, id, name, kind, false)
			if err != nil {
				return dnsAnswer{}, errDNS
			}
		}
		return answer, nil
	}
	return dnsAnswer{}, errDNS
}
func dnsExchange(ctx context.Context, server netip.AddrPort, network string, query []byte) ([]byte, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, network, server.String())
	if err != nil {
		return nil, errDNS
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	if conn.SetDeadline(deadline) != nil {
		return nil, errDNS
	}
	if network == "udp" {
		if n, e := conn.Write(query); e != nil || n != len(query) {
			return nil, errDNS
		}
		buf := make([]byte, 65536)
		n, e := conn.Read(buf)
		if e != nil || n < 12 || n == len(buf) {
			return nil, errDNS
		}
		return buf[:n], nil
	}
	packet := make([]byte, 2, len(query)+2)
	binary.BigEndian.PutUint16(packet, uint16(len(query)))
	packet = append(packet, query...)
	for len(packet) > 0 {
		n, e := conn.Write(packet)
		if e != nil || n == 0 {
			return nil, errDNS
		}
		packet = packet[n:]
	}
	var length [2]byte
	if _, e := io.ReadFull(conn, length[:]); e != nil {
		return nil, errDNS
	}
	n := int(binary.BigEndian.Uint16(length[:]))
	if n < 12 {
		return nil, errDNS
	}
	raw := make([]byte, n)
	if _, e := io.ReadFull(conn, raw); e != nil {
		return nil, errDNS
	}
	return raw, nil
}

// DNS names are decoded with compression bounds/loop detection and canonical
// ASCII normalization. next advances only through the original encoded name.
func dnsName(raw []byte, start int) (name string, next int, err error) {
	return dnsNameMode(raw, start, false)
}
func dnsNameMode(raw []byte, start int, mailbox bool) (name string, next int, err error) {
	pos := start
	next = -1
	seen := map[int]bool{}
	labels := []string{}
	size := 0
	for steps := 0; steps < 128; steps++ {
		if pos < 0 || pos >= len(raw) || seen[pos] {
			return "", 0, errDNS
		}
		seen[pos] = true
		n := int(raw[pos])
		pos++
		if n&0xc0 == 0xc0 {
			if pos >= len(raw) {
				return "", 0, errDNS
			}
			pointer := (n&0x3f)<<8 | int(raw[pos])
			pos++
			if pointer < 12 || pointer >= pos-2 {
				return "", 0, errDNS
			}
			if next < 0 {
				next = pos
			}
			pos = pointer
			continue
		}
		if n&0xc0 != 0 {
			return "", 0, errDNS
		}
		if n == 0 {
			if next < 0 {
				next = pos
			}
			name = strings.Join(labels, ".")
			if name == "" {
				return "", next, nil
			}
			if mailbox {
				return name, next, nil
			}
			normalized, e := normalizeHost(name)
			if e != nil {
				return "", 0, errDNS
			}
			return normalized, next, nil
		}
		if n > 63 || pos+n > len(raw) {
			return "", 0, errDNS
		}
		// Validate each encoded label before joining. A literal dot inside one
		// wire label must not become indistinguishable from two DNS labels.
		label := raw[pos : pos+n]
		local := mailbox && len(labels) == 0
		if !local && (label[0] == '-' || label[len(label)-1] == '-') {
			return "", 0, errDNS
		}
		for _, c := range label {
			if local {
				if c < 33 || c > 126 {
					return "", 0, errDNS
				}
				continue
			}
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return "", 0, errDNS
			}
		}
		size += n + 1
		if size > 254 {
			return "", 0, errDNS
		}
		labels = append(labels, string(raw[pos:pos+n]))
		pos += n
	}
	return "", 0, errDNS
}
func parseDNS(raw []byte, id uint16, name string, kind uint16, allowTruncated bool) (dnsAnswer, error) {
	if len(raw) < 12 || len(raw) > 65535 || binary.BigEndian.Uint16(raw) != id {
		return dnsAnswer{}, errDNS
	}
	flags := binary.BigEndian.Uint16(raw[2:])
	if flags&0x8000 == 0 || flags&0x7800 != 0 || flags&0x0040 != 0 || flags&0x000f != 0 {
		return dnsAnswer{}, errDNS
	}
	if binary.BigEndian.Uint16(raw[4:]) != 1 {
		return dnsAnswer{}, errDNS
	}
	qn, pos, err := dnsName(raw, 12)
	if err != nil || qn != name || pos+4 > len(raw) || binary.BigEndian.Uint16(raw[pos:]) != kind || binary.BigEndian.Uint16(raw[pos+2:]) != 1 {
		return dnsAnswer{}, errDNS
	}
	pos += 4
	if flags&0x0200 != 0 {
		if !allowTruncated {
			return dnsAnswer{}, errDNS
		}
		return dnsAnswer{truncated: true}, nil
	}
	counts := []int{int(binary.BigEndian.Uint16(raw[6:])), int(binary.BigEndian.Uint16(raw[8:])), int(binary.BigEndian.Uint16(raw[10:]))}
	if counts[0]+counts[1]+counts[2] > 1024 {
		return dnsAnswer{}, errDNS
	}
	var records []dnsRecord
	optSeen := false
	referral, negative, terminal := false, false, false
	var authorityZones []string
	for section, count := range counts {
		for range count {
			owner, next, e := dnsName(raw, pos)
			if e != nil || next+10 > len(raw) {
				return dnsAnswer{}, errDNS
			}
			pos = next
			k := binary.BigEndian.Uint16(raw[pos:])
			class := binary.BigEndian.Uint16(raw[pos+2:])
			ttl := binary.BigEndian.Uint32(raw[pos+4:])
			length := int(binary.BigEndian.Uint16(raw[pos+8:]))
			pos += 10
			end := pos + length
			if end > len(raw) {
				return dnsAnswer{}, errDNS
			}
			if k == 41 {
				if section != 2 || owner != "" || optSeen || ttl>>16 != 0 {
					return dnsAnswer{}, errDNS
				}
				for option := pos; option < end; {
					if option+4 > end {
						return dnsAnswer{}, errDNS
					}
					size := int(binary.BigEndian.Uint16(raw[option+2:]))
					option += 4 + size
					if option > end {
						return dnsAnswer{}, errDNS
					}
				}
				optSeen = true
				pos = end
				continue
			}
			if class != 1 || owner == "" && !(section == 1 && (k == 2 || k == 6)) {
				return dnsAnswer{}, errDNS
			}
			rr := dnsRecord{owner: owner, kind: k}
			switch k {
			case 2:
				ns, last, e := dnsName(raw, pos)
				if e != nil || ns == "" || last != end {
					return dnsAnswer{}, errDNS
				}
				if section == 1 {
					authorityZones = append(authorityZones, owner)
				}
			case 6:
				master, last, e := dnsName(raw, pos)
				if e != nil || master == "" || last >= end {
					return dnsAnswer{}, errDNS
				}
				// SOA RNAME's first label is a mailbox local part. Its legal dot
				// and underscore bytes do not become a URL hostname or a DNS target.
				mail, last, e := dnsNameMode(raw, last, true)
				if e != nil || mail == "" || last+20 != end {
					return dnsAnswer{}, errDNS
				}
				if section == 1 {
					authorityZones = append(authorityZones, owner)
				}
			case 1:
				if length != 4 {
					return dnsAnswer{}, errDNS
				}
				rr.address = netip.AddrFrom4([4]byte(raw[pos:end]))
			case 28:
				if length != 16 {
					return dnsAnswer{}, errDNS
				}
				rr.address = netip.AddrFrom16([16]byte(raw[pos:end]))
			case 5:
				alias, last, e := dnsName(raw, pos)
				if e != nil || alias == "" || last != end {
					return dnsAnswer{}, errDNS
				}
				rr.alias = alias
			}
			if section == 0 && (k == 1 || k == 28 || k == 5) {
				records = append(records, rr)
			}
			if section == 0 && k == kind {
				terminal = true
			}
			if section == 1 && k == 2 {
				referral = true
			}
			if section == 1 && k == 6 {
				negative = true
			}
			pos = end
		}
	}
	if pos != len(raw) {
		return dnsAnswer{}, errDNS
	}
	terminalName, valid := dnsTerminal(records, name, kind)
	if !valid {
		return dnsAnswer{}, errDNS
	}
	for _, zone := range authorityZones {
		if !dnsZoneContains(zone, terminalName) {
			return dnsAnswer{}, errDNS
		}
	}
	// We use a recursive deployment resolver, not an iterative resolver. An
	// explicit NS referral is incomplete, regardless of the claimed RA flag.
	if referral && !negative && !terminal {
		return dnsAnswer{}, errDNS
	}
	return dnsAnswer{records: records, negative: negative}, nil
}
func dnsZoneContains(zone, name string) bool {
	return zone == "" || zone == name || strings.HasSuffix(name, "."+zone)
}
