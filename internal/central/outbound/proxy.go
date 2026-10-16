package outbound

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

var errProxyChanged = errors.New("OUTBOUND_PROXY_CHANGED")

func proxyGatewayAddressAllowed(ip netip.Addr) bool {
	return ip.IsValid() && ip.Zone() == ""
}

func (a *attempt) resolveProxy() error {
	ctx, cancel := context.WithTimeout(a.ctx, a.profile.Limits.DNS)
	defer cancel()
	host := a.proxy.Host
	if a.proxy.Port < 1 || a.proxy.Port > 65535 {
		return a.fail(ac.AddressForbidden, nil)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		a.proxyAddresses = []netip.Addr{ip.Unmap()}
	} else {
		var ips []netip.Addr
		var err error
		if strings.Contains(host, ".") {
			// Qualified gateway names receive the same complete A/AAAA resolution as
			// business targets, independently of the target's address approval.
			ips, err = a.client.state().resolver.Lookup(ctx, host)
		} else {
			// An explicitly configured single-label deployment gateway may use the
			// deployment resolver, including hosts and search domains. Approval still
			// uses one complete hosts snapshot or both DNS families for one candidate.
			ips, err = resolveDeploymentGateway(ctx, host, a.client.state().resolver)
		}
		if err != nil {
			return a.fail(a.reason(err, ac.DNSFailed), err)
		}
		a.proxyAddresses = uniqueIPs(slices.Clone(ips))
	}
	if len(a.proxyAddresses) == 0 || len(a.proxyAddresses) > 64 {
		return a.fail(ac.DNSFailed, nil)
	}
	for _, ip := range a.proxyAddresses {
		if !proxyGatewayAddressAllowed(ip) {
			return a.fail(ac.AddressForbidden, nil)
		}
	}
	return nil
}

// Deployment discovery may use hosts/search, but never supplies approved IPs
// from a best-effort lookup. Hosts is read as one complete snapshot; otherwise
// discovery chooses one canonical candidate and the strict resolver approves
// both A and AAAA for that exact candidate without search or family fallback.
func resolveDeploymentGateway(ctx context.Context, host string, resolver Resolver) ([]netip.Addr, error) {
	f, err := os.Open("/etc/hosts")
	if err != nil && !os.IsNotExist(err) {
		return nil, errDNS
	}
	if err == nil {
		raw, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		_ = f.Close()
		if readErr != nil || len(raw) > 1<<20 {
			return nil, errDNS
		}
		ips, err := proxyDeploymentHosts(raw, host)
		if err != nil || len(ips) != 0 {
			return ips, err
		}
	}
	deployment := &net.Resolver{PreferGo: true, StrictErrors: true}
	return proxyCanonicalGateway(ctx, host, deployment.LookupCNAME, resolver)
}

func proxyDeploymentHosts(raw []byte, host string) ([]netip.Addr, error) {
	var ips []netip.Addr
	for _, line := range strings.Split(string(raw), "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, alias := range fields[1:] {
			if strings.ToLower(strings.TrimSuffix(alias, ".")) != host {
				continue
			}
			ip, err := netip.ParseAddr(fields[0])
			if err != nil || ip.Zone() != "" {
				return nil, errDNS
			}
			ips = append(ips, ip.Unmap())
			break
		}
	}
	ips = uniqueIPs(ips)
	if len(ips) > 64 {
		return nil, errDNS
	}
	return ips, nil
}

func proxyCanonicalGateway(ctx context.Context, host string, discover func(context.Context, string) (string, error), resolver Resolver) ([]netip.Addr, error) {
	canonical, err := discover(ctx, host)
	if err != nil {
		return nil, errDNS
	}
	canonical, err = normalizeHost(canonical)
	if err != nil {
		return nil, errDNS
	}
	if _, err := netip.ParseAddr(canonical); err == nil {
		return nil, errDNS
	}
	return resolver.Lookup(ctx, canonical)
}

func (a *attempt) admitProxy(peer netip.Addr) error {
	if len(a.proxyAddresses) == 0 {
		return a.fail(ac.AddressForbidden, nil)
	}
	for _, ip := range a.proxyAddresses {
		if !proxyGatewayAddressAllowed(ip) {
			return a.fail(ac.AddressForbidden, nil)
		}
	}
	if peer.IsValid() && !slices.Contains(a.proxyAddresses, peer.Unmap()) {
		return a.fail(ac.AddressForbidden, nil)
	}
	return nil
}

func (a *attempt) matchesConnection(pc *pooledConn) bool {
	if pc.routeVersion != a.route.version || pc.proxied != (a.proxy != nil) {
		return false
	}
	if a.proxy != nil {
		return containsAddress(a.proxyAddresses, pc.peer) && containsAddress(a.addresses, pc.targetPeer)
	}
	return containsAddress(a.addresses, pc.peer)
}

func (a *attempt) connectProxy() (*pooledConn, error) {
	s := a.client.state()
	s.mu.Lock()
	s.writers++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.writers--; s.notify(); s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(a.ctx, a.profile.Limits.Connect)
	defer cancel()
	release, err := s.policy.state().gate.acquire(ctx, false)
	if err != nil {
		return nil, a.fail(a.reason(err, ac.Timeout), err)
	}
	if err = a.admit(netip.Addr{}); err != nil {
		release()
		return nil, err
	}
	var conn net.Conn
	var peer netip.Addr
	var dialErr error
	for _, ip := range a.proxyAddresses {
		dialer := net.Dialer{}
		conn, dialErr = dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(a.proxy.Port)))
		if dialErr == nil {
			peer = ip
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	release()
	if dialErr != nil || conn == nil {
		return nil, a.fail(a.reason(dialErr, ac.InternalError), dialErr)
	}
	remote, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil || int(remote.Port()) != a.proxy.Port || remote.Addr().Unmap() != peer {
		_ = conn.Close()
		return nil, a.fail(ac.AddressForbidden, nil)
	}
	pc := &pooledConn{conn: conn, raw: conn, origin: a.target.Origin(), peer: peer, targetPeer: a.addresses[0], routeVersion: a.route.version, proxied: true, owner: s}
	s.mu.Lock()
	if s.forced {
		s.mu.Unlock()
		_ = conn.Close()
		return nil, a.fail(ac.Cancelled, nil)
	}
	s.connections[pc] = true
	s.mu.Unlock()
	stop := stopOnCancel(a.ctx, pc)
	defer stop()
	ok := false
	defer func() {
		if !ok {
			pc.close()
		}
	}()
	if a.proxy.Protocol == "https" {
		if err := a.proxyTLS(pc, a.proxy.Host); err != nil {
			return nil, err
		}
	}
	switch a.proxy.Protocol {
	case "http", "https":
		err = a.proxyConnect(pc)
	case "socks5":
		err = a.proxySOCKS(pc)
	default:
		err = a.fail(ac.PolicyUnavailable, nil)
	}
	if err != nil {
		return nil, err
	}
	if a.target.Origin().Scheme() == "https" {
		if err := a.proxyTLS(pc, a.target.Origin().Host()); err != nil {
			return nil, err
		}
	}
	pc.reader = bufio.NewReader(pc.conn)
	ok = true
	return pc, nil
}

func (a *attempt) proxyTLS(pc *pooledConn, hostname string) error {
	conn := tls.Client(pc.conn, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: a.client.state().trust.roots(), ServerName: hostname, NextProtos: []string{"http/1.1"}})
	ctx, cancel := context.WithTimeout(a.ctx, a.profile.Limits.TLS)
	defer cancel()
	if err := conn.HandshakeContext(ctx); err != nil {
		return a.fail(a.reason(err, ac.TLSFailed), err)
	}
	pc.conn = conn
	return nil
}

// Every proxy protocol write (including authentication) is serialized with a
// configuration update. Secret bytes are built, written and cleared inside
// the credential callback and never retained by a connection or HTTP request.
func (a *attempt) proxyWrite(pc *pooledConn, build func(func([]byte) error) error) error {
	release, err := a.client.state().policy.state().gate.acquire(a.ctx, false)
	if err != nil {
		return a.fail(a.reason(err, ac.Cancelled), err)
	}
	defer release()
	if err := a.admit(pc.peer); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := a.ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := pc.conn.SetWriteDeadline(deadline); err != nil {
		return a.fail(a.reason(err, ac.InternalError), err)
	}
	defer pc.conn.SetWriteDeadline(time.Time{})
	err = build(func(wire []byte) error {
		n, err := pc.conn.Write(wire)
		if err == nil && n != len(wire) {
			err = io.ErrShortWrite
		}
		return err
	})
	if err != nil {
		return a.fail(a.reason(err, ac.InternalError), err)
	}
	return nil
}

func (a *attempt) proxyReadDeadline(pc *pooledConn) error {
	deadline := time.Now().Add(min(a.profile.Limits.ResponseHeaders, defaultResponseHeadersTimeout))
	if d, ok := a.ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return pc.conn.SetReadDeadline(deadline)
}

func (a *attempt) proxyConnect(pc *pooledConn) error {
	authority := net.JoinHostPort(pc.targetPeer.String(), strconv.Itoa(int(a.target.Origin().Port())))
	prefix := []byte("CONNECT " + authority + " HTTP/1.1\r\nHost: " + authority + "\r\n")
	err := a.proxyWrite(pc, func(write func([]byte) error) error {
		send := func(password []byte) error {
			capacity := len(prefix) + 2
			if a.proxy.CredentialPresent {
				capacity += len("Proxy-Authorization: Basic ") + base64.StdEncoding.EncodedLen(len(a.proxy.Username)+1+len(password)) + 2
			}
			wire := make([]byte, 0, capacity)
			wire = append(wire, prefix...)
			defer func() { clear(wire) }()
			if a.proxy.CredentialPresent {
				plain := append([]byte(a.proxy.Username+":"), password...)
				defer clear(plain)
				wire = append(wire, "Proxy-Authorization: Basic "...)
				wire = base64.StdEncoding.AppendEncode(wire, plain)
				wire = append(wire, '\r', '\n')
			}
			wire = append(wire, '\r', '\n')
			if len(wire) > a.profile.Limits.RequestHeaderBytes {
				return errHeaderLimit
			}
			return write(wire)
		}
		if a.proxy.CredentialPresent {
			if a.route.credential == nil {
				return invalid()
			}
			ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
			defer cancel()
			return a.route.credential(ctx, send)
		}
		return send(nil)
	})
	if err != nil {
		return err
	}
	if err := a.proxyReadDeadline(pc); err != nil {
		return a.fail(a.reason(err, ac.InternalError), err)
	}
	reader := bufio.NewReader(pc.conn)
	header, err := readHeaderBlock(reader, a.profile.Limits.ResponseHeaderBytes)
	if err != nil {
		if errors.Is(err, errHeaderLimit) {
			return a.fail(ac.ResponseLimit, nil)
		}
		return a.fail(a.reason(err, ac.InternalError), err)
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(header)), &http.Request{Method: "CONNECT"})
	if err != nil {
		return a.fail(ac.InternalError, nil)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ProtoMajor != 1 || response.ProtoMinor > 1 || len(response.TransferEncoding) != 0 || response.ContentLength > 0 {
		return a.fail(ac.InternalError, nil)
	}
	_ = pc.conn.SetReadDeadline(time.Time{})
	// SMTP may send its greeting in the same packet as the CONNECT response.
	if reader.Buffered() != 0 {
		pc.conn = &proxyBufferedConn{Conn: pc.conn, reader: reader}
	}
	return nil
}

type proxyBufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *proxyBufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (a *attempt) proxySOCKS(pc *pooledConn) error {
	method := byte(0)
	if a.proxy.CredentialPresent {
		method = 2
	}
	write := func(wire []byte) error {
		return a.proxyWrite(pc, func(send func([]byte) error) error { return send(wire) })
	}
	read := func(wire []byte) error {
		if err := a.proxyReadDeadline(pc); err != nil {
			return a.fail(a.reason(err, ac.InternalError), err)
		}
		if _, err := io.ReadFull(pc.conn, wire); err != nil {
			return a.fail(a.reason(err, ac.InternalError), err)
		}
		return nil
	}
	if err := write([]byte{5, 1, method}); err != nil {
		return err
	}
	var answer [2]byte
	if err := read(answer[:]); err != nil {
		return err
	}
	if answer != [2]byte{5, method} {
		return a.fail(ac.CredentialDenied, nil)
	}
	if method == 2 {
		err := a.proxyWrite(pc, func(send func([]byte) error) error {
			if a.route.credential == nil {
				return invalid()
			}
			ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
			defer cancel()
			return a.route.credential(ctx, func(password []byte) error {
				if len(a.proxy.Username) == 0 || len(a.proxy.Username) > 255 || len(password) == 0 || len(password) > 255 {
					return invalid()
				}
				wire := make([]byte, 0, 3+len(a.proxy.Username)+len(password))
				wire = append(wire, 1, byte(len(a.proxy.Username)))
				wire = append(wire, a.proxy.Username...)
				wire = append(wire, byte(len(password)))
				wire = append(wire, password...)
				defer clear(wire)
				return send(wire)
			})
		})
		if err != nil {
			return err
		}
		if err := read(answer[:]); err != nil {
			return err
		}
		if answer != [2]byte{1, 0} {
			return a.fail(ac.CredentialDenied, nil)
		}
	}
	wire := []byte{5, 1, 0}
	if pc.targetPeer.Is4() {
		ip := pc.targetPeer.As4()
		wire = append(wire, 1)
		wire = append(wire, ip[:]...)
	} else {
		ip := pc.targetPeer.As16()
		wire = append(wire, 4)
		wire = append(wire, ip[:]...)
	}
	wire = binary.BigEndian.AppendUint16(wire, a.target.Origin().Port())
	if err := write(wire); err != nil {
		return err
	}
	var head [4]byte
	if err := read(head[:]); err != nil {
		return err
	}
	if head[0] != 5 || head[1] != 0 || head[2] != 0 {
		return a.fail(ac.InternalError, nil)
	}
	var length int
	switch head[3] {
	case 1:
		length = 4
	case 4:
		length = 16
	case 3:
		var n [1]byte
		if err := read(n[:]); err != nil {
			return err
		}
		if n[0] == 0 {
			return a.fail(ac.InternalError, nil)
		}
		length = int(n[0])
	default:
		return a.fail(ac.InternalError, nil)
	}
	if err := read(make([]byte, length+2)); err != nil {
		return err
	}
	_ = pc.conn.SetReadDeadline(time.Time{})
	return nil
}
