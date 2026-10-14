package outbound

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"golang.org/x/net/http/httpproxy"
)

// TrustedProxy describes a deployment gateway route, not an address class of
// the remote origin. The gateway owns the origin DNS lookup and connection.
const TrustedProxy AddressClass = "trusted_proxy"

// ProxyRoutes is an immutable composition-root grant for exact HTTPS origins.
// Its zero value preserves the original direct-only client. It exposes neither
// environment values nor a request-controlled proxy or TLS configuration.
type ProxyRoutes struct {
	selectRoute func(Origin) (*proxyRoute, error)
}

type proxyRoute struct{ origin Origin }

// NewEnvironmentProxyRoutes snapshots the standard deployment proxy selection
// and grants only the supplied origins. It performs no network I/O. Invalid
// configured proxies fail closed rather than becoming an implicit direct route.
// Proxy authentication is deliberately unsupported; userinfo is rejected.
func NewEnvironmentProxyRoutes(origins []Origin) (ProxyRoutes, error) {
	if len(origins) == 0 || len(origins) > 32 {
		return ProxyRoutes{}, invalid()
	}
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		if !proxyTargetAllowed(origin) || allowed[origin.String()] {
			return ProxyRoutes{}, invalid()
		}
		allowed[origin.String()] = true
	}
	environment := httpproxy.FromEnvironment()
	for _, raw := range []string{environment.HTTPProxy, environment.HTTPSProxy} {
		if raw != "" {
			if _, err := parseProxyRoute(raw); err != nil {
				return ProxyRoutes{}, invalid()
			}
		}
	}
	choose := environment.ProxyFunc()
	return ProxyRoutes{selectRoute: func(origin Origin) (*proxyRoute, error) {
		if !origin.Valid() {
			return nil, invalid()
		}
		if origin.Scheme() != "http" && origin.Scheme() != "https" {
			return nil, nil // SMTP retains its original direct transport.
		}
		proxy, err := choose(&url.URL{Scheme: origin.Scheme(), Host: net.JoinHostPort(origin.Host(), strconv.Itoa(int(origin.Port())))})
		if err != nil {
			return nil, invalid()
		}
		if proxy == nil {
			return nil, nil // NO_PROXY is selection, never failure fallback.
		}
		if !allowed[origin.String()] || !proxyTargetAllowed(origin) {
			return nil, invalid()
		}
		return parseProxyRoute(proxy.String())
	}}, nil
}

func proxyTargetAllowed(origin Origin) bool {
	if !origin.Valid() || origin.Scheme() != "https" || metadataHost(origin.Host()) || origin.Host() == "localhost" || strings.HasSuffix(origin.Host(), ".localhost") {
		return false
	}
	_, err := netip.ParseAddr(origin.Host())
	return err != nil
}

func parseProxyRoute(raw string) (*proxyRoute, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	target, err := ParseTarget(raw)
	if err != nil {
		return nil, invalid()
	}
	u := target.URL()
	if u.Path != "/" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || metadataHost(target.Origin().Host()) {
		return nil, invalid()
	}
	if ip, err := netip.ParseAddr(target.Origin().Host()); err == nil && !proxyGatewayAddressAllowed(ip) {
		return nil, invalid()
	}
	return &proxyRoute{origin: target.Origin()}, nil
}

func (r ProxyRoutes) route(origin Origin) (*proxyRoute, error) {
	if !origin.Valid() {
		return nil, invalid()
	}
	if r.selectRoute == nil {
		return nil, nil
	}
	return r.selectRoute(origin)
}

// UsesProxy validates the captured route without resolving or dialing either
// endpoint. A true result is a gateway authorization, not proof of reachability
// or of the origin's DNS/address class behind that gateway.
func (r ProxyRoutes) UsesProxy(origin Origin) (bool, error) {
	proxy, err := r.route(origin)
	return proxy != nil, err
}

func (r ProxyRoutes) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_proxy_routes") }
func (r ProxyRoutes) MarshalJSON() ([]byte, error) { return []byte(`"outbound_proxy_routes"`), nil }
func (r ProxyRoutes) LogValue() slog.Value         { return slog.StringValue("outbound_proxy_routes") }

// These addresses identify configured deployment infrastructure, not an
// arbitrary business target. In particular a loopback gateway never changes
// Classify(loopback) or grants direct loopback target access.
func proxyGatewayAddressAllowed(ip netip.Addr) bool {
	switch Classify(ip) {
	case Public, Private, Loopback:
		return true
	default:
		return false
	}
}

func (a *attempt) resolveProxy() error {
	ctx, cancel := context.WithTimeout(a.ctx, a.profile.Limits.DNS)
	defer cancel()
	host := a.proxy.origin.Host()
	if ip, err := netip.ParseAddr(host); err == nil {
		a.proxyAddresses = []netip.Addr{ip.Unmap()}
	} else {
		// Gateway discovery follows the deployment's normal resolver, including
		// hosts. Origin DNS is performed by the explicitly trusted gateway.
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
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

func (a *attempt) admitProxy(peer netip.Addr) error {
	route, err := a.client.state().routes.route(a.target.Origin())
	if err != nil || route == nil || !route.origin.Equal(a.proxy.origin) || len(a.proxyAddresses) == 0 {
		return a.fail(ac.OriginDenied, nil)
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
	if a.proxy != nil {
		return pc.proxyKey == a.proxy.origin.String() && containsAddress(a.proxyAddresses, pc.peer)
	}
	return pc.proxyKey == "" && containsAddress(a.addresses, pc.peer)
}

func (a *attempt) connectProxy() (*pooledConn, error) {
	s := a.client.state()
	// Drain must also join a cancelled dial/CONNECT/TLS operation, even before
	// a socket has been registered and after the request context has fired.
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
	port := a.proxy.origin.Port()
	for _, ip := range a.proxyAddresses {
		dialer := net.Dialer{}
		conn, dialErr = dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(int(port))))
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
	if err != nil || remote.Port() != port || remote.Addr().Unmap() != peer {
		_ = conn.Close()
		return nil, a.fail(ac.AddressForbidden, nil)
	}
	pc := &pooledConn{conn: conn, raw: conn, origin: a.target.Origin(), peer: peer, proxyKey: a.proxy.origin.String(), owner: s}
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
	if a.proxy.origin.Scheme() == "https" {
		if err := a.proxyTLS(pc, a.proxy.origin.Host()); err != nil {
			return nil, err
		}
	}
	if err := a.proxyConnect(pc); err != nil {
		return nil, err
	}
	if err := a.proxyTLS(pc, a.target.Origin().Host()); err != nil {
		return nil, err
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

func (a *attempt) proxyConnect(pc *pooledConn) error {
	// CONNECT contains only the exact authorized authority. No origin request
	// header, query, body, credential or sent state participates in this step.
	authority := net.JoinHostPort(a.target.Origin().Host(), strconv.Itoa(int(a.target.Origin().Port())))
	wire := "CONNECT " + authority + " HTTP/1.1\r\nHost: " + authority + "\r\n\r\n"
	if len(wire) > a.profile.Limits.RequestHeaderBytes {
		return a.fail(ac.ResponseLimit, nil)
	}
	release, err := a.client.state().policy.state().gate.acquire(a.ctx, false)
	if err != nil {
		return a.fail(a.reason(err, ac.Cancelled), err)
	}
	if err = a.admit(pc.peer); err != nil {
		release()
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := a.ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	err = pc.conn.SetWriteDeadline(deadline)
	if err == nil {
		var n int
		n, err = io.WriteString(pc.conn, wire)
		if err == nil && n != len(wire) {
			err = io.ErrShortWrite
		}
	}
	release()
	if err != nil {
		return a.fail(a.reason(err, ac.InternalError), err)
	}
	_ = pc.conn.SetWriteDeadline(time.Time{})
	// Model generation may wait longer for its origin response. Establishing
	// a deployment tunnel retains the ordinary response-header ceiling.
	deadline = time.Now().Add(min(a.profile.Limits.ResponseHeaders, defaultResponseHeadersTimeout))
	if d, ok := a.ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := pc.conn.SetReadDeadline(deadline); err != nil {
		return a.fail(a.reason(err, ac.InternalError), err)
	}
	reader := bufio.NewReader(pc.conn)
	header, err := readHeaderBlock(reader, a.profile.Limits.ResponseHeaderBytes)
	if err != nil {
		if err == errHeaderLimit {
			return a.fail(ac.ResponseLimit, nil)
		}
		return a.fail(a.reason(err, ac.InternalError), err)
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(header)), &http.Request{Method: "CONNECT"})
	if err != nil || response.StatusCode != http.StatusOK || response.ProtoMajor != 1 || response.ProtoMinor > 1 || len(response.TransferEncoding) != 0 || response.ContentLength > 0 || reader.Buffered() != 0 {
		return a.fail(ac.InternalError, nil)
	}
	_ = response.Body.Close() // Header-only parsing owns no live socket reader.
	_ = pc.conn.SetReadDeadline(time.Time{})
	return nil
}
