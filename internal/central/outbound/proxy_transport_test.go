package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

// The bridge only ever dials the exact listener created by this test. The
// CONNECT request cannot select any other endpoint or existing infrastructure.
func proxyTestBridge(t *testing.T, conn net.Conn, reader io.Reader, target *proxyTestGateway) {
	t.Helper()
	upstream, err := net.DialTimeout("tcp", target.listener.Addr().String(), time.Second)
	if err != nil {
		t.Error("owned target dial failed")
		return
	}
	defer upstream.Close()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, reader); _ = upstream.Close(); close(done) }()
	_, _ = io.Copy(conn, upstream)
	_ = conn.Close()
	<-done
}

func proxyTestForwardHTTP(t *testing.T, target *proxyTestGateway, auth string, seen *atomic.Int32, certificates ...tls.Certificate) *proxyTestGateway {
	t.Helper()
	return proxyTestStartGateway(t, func(conn net.Conn) {
		if len(certificates) != 0 {
			conn = tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: certificates})
			defer conn.Close()
		}
		reader := bufio.NewReader(conn)
		request, err := http.ReadRequest(reader)
		if err != nil {
			t.Error("CONNECT missing")
			return
		}
		defer request.Body.Close()
		if request.Method != "CONNECT" || request.RequestURI != target.listener.Addr().String() || request.Host != target.listener.Addr().String() {
			t.Error("gateway received unpinned target")
			return
		}
		if request.Header.Get("Proxy-Authorization") != auth || request.Header.Get("Authorization") != "" {
			t.Error("gateway credential binding failed")
			return
		}
		seen.Add(1)
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		proxyTestBridge(t, conn, reader, target)
	})
}

func proxyTestRuleFor(t *testing.T, state *policyState, target *proxyTestGateway, allowHTTP bool) {
	t.Helper()
	addr := target.listener.Addr().(*net.TCPAddr)
	ports, err := SelectedPorts(uint16(addr.Port))
	if err != nil {
		t.Fatal(err)
	}
	rule, err := NewRule(addr.IP.String()+"/32", ports, allowHTTP)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := NewRules(rule)
	if err != nil {
		t.Fatal(err)
	}
	state.mirror = policy(1, rules)
}

func proxyTestAuth(state *policyState, user, password string) *atomic.Int32 {
	reads := &atomic.Int32{}
	state.proxy.gateway.Username = user
	state.proxy.gateway.CredentialPresent = true
	state.proxy.credential = func(ctx context.Context, use func([]byte) error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		reads.Add(1)
		raw := []byte(password)
		defer clear(raw)
		return use(raw)
	}
	return reads
}

func TestProxyOwnedTargetHTTPAndTLS(t *testing.T) {
	for _, protocol := range []string{"http", "https"} {
		for _, scheme := range []string{"http", "https"} {
			t.Run(protocol+"/"+scheme, func(t *testing.T) {
				cert, trust := proxyTestCertificate(t, "owned.example", proxyTestPrivateIP(t))
				var targetRequests, connects atomic.Int32
				target := proxyTestStartGateway(t, func(raw net.Conn) {
					conn := raw
					if scheme == "https" {
						conn = tls.Server(raw, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
					}
					defer conn.Close()
					request, err := http.ReadRequest(bufio.NewReader(conn))
					if err != nil {
						t.Error("owned target request missing")
						return
					}
					defer request.Body.Close()
					targetRequests.Add(1)
					if !strings.HasPrefix(request.Host, "owned.example:") || request.Header.Get("Proxy-Authorization") != "" || request.URL.IsAbs() {
						t.Error("target lost original Host or received proxy metadata")
					}
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
				})
				password := "gateway-private-canary"
				var gatewayCerts []tls.Certificate
				if protocol == "https" {
					gatewayCerts = []tls.Certificate{cert}
				}
				gateway := proxyTestForwardHTTP(t, target, "Basic "+base64.StdEncoding.EncodeToString([]byte("test:"+password)), &connects, gatewayCerts...)
				resolver := &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr(proxyTestPrivateIP(t))}}
				client, state := proxyTestClient(t, trust, resolver, gateway)
				state.proxy.gateway.Protocol = protocol
				proxyTestRuleFor(t, state, target, true)
				reads := proxyTestAuth(state, "test", password)
				profile, err := NewProfile(ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, AllowHTTP: true})
				if err != nil {
					t.Fatal(err)
				}
				url := scheme + "://owned.example:" + strconv.Itoa(target.listener.Addr().(*net.TCPAddr).Port) + "/"
				request, _ := http.NewRequest("GET", url, nil)
				response, err := client.Do(context.Background(), request, profile)
				if err != nil {
					t.Fatal("owned target transfer:", err)
				}
				body, err := io.ReadAll(response.Body())
				response.Close()
				if err != nil || string(body) != "ok" || response.Decision().AddressClass != Private || !response.Decision().Sent || targetRequests.Load() != 1 || connects.Load() != 1 || reads.Load() != 1 {
					t.Fatal("owned target transfer evidence incomplete")
				}
			})
		}
	}
}

func TestProxyTargetAndGatewayDenialsBeforeDial(t *testing.T) {
	for _, tc := range []struct {
		name            string
		target, gateway []netip.Addr
		reason          ac.Reason
	}{
		{"mixed_target_loopback", []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("::1")}, nil, ac.AddressForbidden},
		{"target_private_without_rule", []netip.Addr{netip.MustParseAddr("10.22.33.44")}, nil, ac.PrivateNotAllowed},
		{"mixed_gateway_loopback", nil, []netip.Addr{netip.MustParseAddr("10.22.33.44"), netip.MustParseAddr("127.0.0.1")}, ac.AddressForbidden},
		{"gateway_metadata", nil, []netip.Addr{netip.MustParseAddr("fd00:ec2::254")}, ac.AddressForbidden},
		{"gateway_linklocal", nil, []netip.Addr{netip.MustParseAddr("fe80::1")}, ac.AddressForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, trust := proxyTestCertificate(t, "api.example")
			gateway := proxyTestStartGateway(t, func(net.Conn) { t.Error("denied request dialed gateway") })
			resolver := proxyMappedResolver{target: tc.target, gateway: tc.gateway}
			client, state := proxyTestClient(t, trust, resolver, gateway)
			if tc.gateway != nil {
				state.proxy.gateway.Host = "gateway.example"
			}
			_, err := client.Do(context.Background(), proxyTestRequest(t), proxyTestProfile(t, false, Limits{}))
			if err == nil || SafeNetworkError(err).Decision().Reason != tc.reason || SafeNetworkError(err).Decision().Sent || gateway.accepts.Load() != 0 {
				t.Fatal("complete target/gateway set was not denied before dial", err)
			}
		})
	}
}

type proxyMappedResolver struct{ target, gateway []netip.Addr }

func (r proxyMappedResolver) Lookup(_ context.Context, host string) ([]netip.Addr, error) {
	if host == "gateway.example" {
		return r.gateway, nil
	}
	if r.target == nil {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	return r.target, nil
}

func TestProxyOwnedTargetSOCKS5(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(strconv.FormatBool(authenticated), func(t *testing.T) {
			_, trust := proxyTestCertificate(t, "owned.example")
			var requests atomic.Int32
			target := proxyTestStartGateway(t, func(conn net.Conn) {
				request, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					t.Error("SOCKS target missing")
					return
				}
				defer request.Body.Close()
				if !strings.HasPrefix(request.Host, "owned.example:") || request.Header.Get("Proxy-Authorization") != "" {
					t.Error("SOCKS target headers changed")
				}
				requests.Add(1)
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			})
			gateway := proxyTestStartGateway(t, func(conn net.Conn) {
				var hello [3]byte
				if _, err := io.ReadFull(conn, hello[:]); err != nil {
					return
				}
				method := byte(0)
				if authenticated {
					method = 2
				}
				if hello != [3]byte{5, 1, method} {
					t.Error("SOCKS method mismatch")
					return
				}
				_, _ = conn.Write([]byte{5, method})
				if authenticated {
					var header [2]byte
					if _, err := io.ReadFull(conn, header[:]); err != nil {
						return
					}
					user := make([]byte, int(header[1]))
					if _, err := io.ReadFull(conn, user); err != nil {
						return
					}
					var size [1]byte
					if _, err := io.ReadFull(conn, size[:]); err != nil {
						return
					}
					password := make([]byte, int(size[0]))
					if _, err := io.ReadFull(conn, password); err != nil {
						return
					}
					if header[0] != 1 || string(user) != "test" || string(password) != "socks-private-canary" {
						t.Error("SOCKS authentication mismatch")
						return
					}
					clear(password)
					_, _ = conn.Write([]byte{1, 0})
				}
				var command [10]byte
				if _, err := io.ReadFull(conn, command[:]); err != nil {
					return
				}
				addr := target.listener.Addr().(*net.TCPAddr)
				if command[0] != 5 || command[1] != 1 || command[2] != 0 || command[3] != 1 || !net.IP(command[4:8]).Equal(addr.IP) || int(binary.BigEndian.Uint16(command[8:])) != addr.Port {
					t.Error("SOCKS did not pin numeric target")
					return
				}
				_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				proxyTestBridge(t, conn, conn, target)
			})
			client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr(proxyTestPrivateIP(t))}}, gateway)
			state.proxy.gateway.Protocol = "socks5"
			proxyTestRuleFor(t, state, target, true)
			if authenticated {
				proxyTestAuth(state, "test", "socks-private-canary")
			}
			profile, _ := NewProfile(ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, AllowHTTP: true})
			request, _ := http.NewRequest("GET", "http://owned.example:"+strconv.Itoa(target.listener.Addr().(*net.TCPAddr).Port)+"/", nil)
			response, err := client.Do(context.Background(), request, profile)
			if err != nil {
				t.Fatal("SOCKS owned transfer:", err)
			}
			_, err = io.ReadAll(response.Body())
			response.Close()
			if err != nil || requests.Load() != 1 {
				t.Fatal("SOCKS owned transfer failed")
			}
		})
	}
}

func proxyTestPublish(t *testing.T, state *policyState, update func(*proxySnapshot)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := state.gate.acquire(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.proxy.version++
	update(&state.proxy)
}

func TestProxyRouteChangeBeforeFirstWriteDoesNotReuseOldTunnel(t *testing.T) {
	cert, trust := proxyTestCertificate(t, "api.example")
	var oldBusiness, newBusiness atomic.Int32
	serve := func(counter *atomic.Int32) func(net.Conn) {
		return func(conn net.Conn) {
			if !proxyTestReadConnect(t, conn) {
				return
			}
			_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
			target := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
			defer target.Close()
			if request, err := http.ReadRequest(bufio.NewReader(target)); err == nil {
				defer request.Body.Close()
				counter.Add(1)
				_, _ = io.WriteString(target, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			}
		}
	}
	old := proxyTestStartGateway(t, serve(&oldBusiness))
	next := proxyTestStartGateway(t, serve(&newBusiness))
	client, state := proxyTestClient(t, trust, &proxyTestResolver{}, old)
	var once sync.Once
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) {
		once.Do(func() {
			proxyTestPublish(t, state, func(p *proxySnapshot) {
				copy := *p.gateway
				copy.Port = next.listener.Addr().(*net.TCPAddr).Port
				p.gateway = &copy
			})
		})
	}})
	response, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, false, Limits{}))
	if err != nil {
		t.Fatal("updated route retry:", err)
	}
	response.Close()
	if oldBusiness.Load() != 0 || newBusiness.Load() != 1 || old.accepts.Load() != 1 || next.accepts.Load() != 1 {
		t.Fatal("route update reused old tunnel or failed to refresh")
	}
}

func TestProxyOldPoolAndCredentialRotationUseNewGateway(t *testing.T) {
	_, trust := proxyTestCertificate(t, "owned.example")
	var requests, oldConnects, newConnects atomic.Int32
	target := proxyTestStartGateway(t, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		for {
			request, err := http.ReadRequest(reader)
			if err != nil {
				return
			}
			request.Body.Close()
			if request.Header.Get("Proxy-Authorization") != "" {
				t.Error("proxy authentication reached target")
			}
			requests.Add(1)
			if _, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"); err != nil {
				return
			}
		}
	})
	old := proxyTestForwardHTTP(t, target, "Basic "+base64.StdEncoding.EncodeToString([]byte("old:old-private-canary")), &oldConnects)
	next := proxyTestForwardHTTP(t, target, "Basic "+base64.StdEncoding.EncodeToString([]byte("new:new-private-canary")), &newConnects)
	client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr(proxyTestPrivateIP(t))}}, old)
	proxyTestRuleFor(t, state, target, true)
	oldReads := proxyTestAuth(state, "old", "old-private-canary")
	profile, _ := NewProfile(ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, AllowHTTP: true})
	request, _ := http.NewRequest("GET", "http://owned.example:"+strconv.Itoa(target.listener.Addr().(*net.TCPAddr).Port)+"/", nil)
	send := func() {
		response, err := client.Do(context.Background(), request, profile)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(response.Body())
		response.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	send()
	var newReads *atomic.Int32
	proxyTestPublish(t, state, func(p *proxySnapshot) {
		g := *p.gateway
		g.Port = next.listener.Addr().(*net.TCPAddr).Port
		p.gateway = &g
		newReads = proxyTestAuth(state, "new", "new-private-canary")
	})
	send()
	if requests.Load() != 2 || oldConnects.Load() != 1 || newConnects.Load() != 1 || oldReads.Load() != 1 || newReads.Load() != 1 {
		t.Fatal("pool or credentials survived route rotation")
	}
}

func TestProxyDirectWithSavedGatewayDoesNotUseCredentials(t *testing.T) {
	_, trust := proxyTestCertificate(t, "owned.example")
	var requests atomic.Int32
	target := proxyTestStartGateway(t, func(conn net.Conn) {
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		request.Body.Close()
		requests.Add(1)
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	})
	gateway := proxyTestStartGateway(t, func(net.Conn) { t.Error("direct mode dialed saved proxy") })
	client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr(proxyTestPrivateIP(t))}}, gateway)
	proxyTestRuleFor(t, state, target, true)
	reads := proxyTestAuth(state, "test", "unused-private-canary")
	state.proxy.mode = "direct"
	profile, _ := NewProfile(ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, AllowHTTP: true})
	request, _ := http.NewRequest("GET", "http://owned.example:"+strconv.Itoa(target.listener.Addr().(*net.TCPAddr).Port)+"/", nil)
	response, err := client.Do(context.Background(), request, profile)
	if err != nil {
		t.Fatal(err)
	}
	response.Close()
	if requests.Load() != 1 || gateway.accepts.Load() != 0 || reads.Load() != 0 {
		t.Fatal("direct mode consumed saved proxy")
	}
}

func TestProxyDeploymentDiscoveryRequiresCompleteCandidate(t *testing.T) {
	ips, err := proxyDeploymentHosts([]byte("10.2.3.4 gateway alias\nfd00:123::1 GATEWAY. # ipv6\n127.0.0.1 other\n"), "gateway")
	if err != nil || len(ips) != 2 || ips[0] != netip.MustParseAddr("10.2.3.4") || ips[1] != netip.MustParseAddr("fd00:123::1") {
		t.Fatal("deployment hosts did not collect both families")
	}
	ips, err = proxyDeploymentHosts([]byte("10.2.3.4 gateway\n::1 gateway\n"), "gateway")
	if err != nil || len(ips) != 2 || proxyGatewayAddressAllowed(ips[1]) {
		t.Fatal("deployment hosts omitted forbidden family")
	}
	if _, err = proxyDeploymentHosts([]byte("broken gateway\n10.2.3.4 gateway\n"), "gateway"); err == nil {
		t.Fatal("malformed matched hosts entry accepted")
	}
	for _, fail := range []bool{false, true} {
		resolver := &proxyCandidateResolver{t: t, fail: fail}
		ips, err = proxyCanonicalGateway(context.Background(), "gateway", func(context.Context, string) (string, error) { return "Gateway.Corporate.Example.", nil }, resolver)
		if resolver.calls != 1 || (err != nil) != fail || !fail && len(ips) != 1 {
			t.Fatal("canonical gateway skipped strict complete DNS")
		}
	}
}

type proxyCandidateResolver struct {
	t     *testing.T
	fail  bool
	calls int
}

func (r *proxyCandidateResolver) Lookup(_ context.Context, host string) ([]netip.Addr, error) {
	r.calls++
	if host != "gateway.corporate.example" {
		r.t.Error("strict gateway lookup changed search candidate")
	}
	if r.fail {
		return nil, errDNS
	}
	return []netip.Addr{netip.MustParseAddr("10.2.3.4")}, nil
}

func TestProxyRulesApplyBeforeTunnel(t *testing.T) {
	for _, kind := range []string{"port", "http"} {
		t.Run(kind, func(t *testing.T) {
			_, trust := proxyTestCertificate(t, "owned.example")
			target := proxyTestStartGateway(t, func(net.Conn) { t.Error("denied target accepted a connection") })
			gateway := proxyTestStartGateway(t, func(net.Conn) { t.Error("denied policy opened a tunnel") })
			client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr(proxyTestPrivateIP(t))}}, gateway)
			port := uint16(target.listener.Addr().(*net.TCPAddr).Port)
			allowed := port
			if kind == "port" {
				allowed = 1
			}
			ports, _ := SelectedPorts(allowed)
			rule, _ := NewRule(proxyTestPrivateIP(t)+"/32", ports, kind != "http")
			rules, _ := NewRules(rule)
			state.mirror = policy(1, rules)
			profile, _ := NewProfile(ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, AllowHTTP: true})
			request, _ := http.NewRequest("GET", "http://owned.example:"+strconv.Itoa(int(port))+"/", nil)
			_, err := client.Do(context.Background(), request, profile)
			want := ac.HTTPDenied
			if kind == "port" {
				want = ac.PortDenied
			}
			if err == nil || SafeNetworkError(err).Decision().Reason != want || target.accepts.Load() != 0 || gateway.accepts.Load() != 0 {
				t.Fatal("proxy bypassed target policy", err)
			}
		})
	}
}

func TestProxyCanonicalGatewayRejectsPartialDNSFamily(t *testing.T) {
	for _, rcode := range []uint16{0, 3, 5} {
		t.Run(strconv.Itoa(int(rcode)), func(t *testing.T) {
			fixture := newDNSFixture(t, func(query []byte, _ string) []byte {
				_, name, kind := dnsQueryFields(query)
				if name != "gateway.corporate.example" {
					t.Error("DNS family changed canonical candidate")
				}
				if kind == 1 {
					return responseDNS(query, 0, dnsA(name, "10.2.3.4"))
				}
				return responseDNS(query, rcode)
			})
			ips, err := proxyCanonicalGateway(context.Background(), "gateway", func(context.Context, string) (string, error) { return "gateway.corporate.example.", nil }, fixture.resolver(t))
			if rcode == 0 {
				if err != nil || len(ips) != 1 {
					t.Fatal("complete IPv4 plus AAAA NODATA rejected")
				}
			} else if err == nil || len(ips) != 0 {
				t.Fatal("partial A plus failing AAAA accepted")
			}
		})
	}
}

func TestProxySOCKSIPv6UsesNumericTarget(t *testing.T) {
	_, trust := proxyTestCertificate(t, "api.example")
	ip := netip.MustParseAddr("2001:4860:4860::8888")
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		var greeting [3]byte
		if _, err := io.ReadFull(conn, greeting[:]); err != nil {
			return
		}
		_, _ = conn.Write([]byte{5, 0})
		var command [22]byte
		if _, err := io.ReadFull(conn, command[:]); err != nil {
			return
		}
		expected := ip.As16()
		if command[0] != 5 || command[1] != 1 || command[2] != 0 || command[3] != 4 || string(command[4:20]) != string(expected[:]) || binary.BigEndian.Uint16(command[20:]) != 80 {
			t.Error("SOCKS IPv6 target was not numeric")
			return
		}
		_, _ = conn.Write([]byte{5, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		defer request.Body.Close()
		if request.Host != "api.example:80" {
			t.Error("IPv6 route lost Host")
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	})
	client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{ip}}, gateway)
	state.proxy.gateway.Protocol = "socks5"
	profile, _ := NewProfile(ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, AllowHTTP: true})
	request, _ := http.NewRequest("GET", "http://api.example/", nil)
	response, err := client.Do(context.Background(), request, profile)
	if err != nil {
		t.Fatal(err)
	}
	response.Close()
}

func TestProxySOCKSRejectsAuthenticationDowngrade(t *testing.T) {
	_, trust := proxyTestCertificate(t, "api.example")
	trailing := make(chan int64, 1)
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		var greeting [3]byte
		if _, err := io.ReadFull(conn, greeting[:]); err != nil {
			trailing <- -1
			return
		}
		if greeting != [3]byte{5, 1, 2} {
			t.Error("authenticated route offered an unauthenticated method")
		}
		_, _ = conn.Write([]byte{5, 0})
		n, _ := io.Copy(io.Discard, conn)
		trailing <- n
	})
	client, state := proxyTestClient(t, trust, &proxyTestResolver{}, gateway)
	state.proxy.gateway.Protocol = "socks5"
	reads := proxyTestAuth(state, "test", "never-read-private-canary")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, true, Limits{}))
	if err == nil || SafeNetworkError(err).Decision().Reason != ac.CredentialDenied || SafeNetworkError(err).Decision().Sent || reads.Load() != 0 {
		t.Fatal("SOCKS authentication downgrade accepted")
	}
	select {
	case n := <-trailing:
		if n != 0 {
			t.Fatal("SOCKS downgrade received credentials or target")
		}
	case <-ctx.Done():
		t.Fatal("rejected SOCKS socket did not close")
	}
}

func TestProxyUpdateBetweenSOCKSNegotiationAndCredentialUse(t *testing.T) {
	_, trust := proxyTestCertificate(t, "owned.example")
	var requests, connects atomic.Int32
	target := proxyTestStartGateway(t, func(conn net.Conn) {
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		request.Body.Close()
		requests.Add(1)
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	})
	next := proxyTestForwardHTTP(t, target, "Basic "+base64.StdEncoding.EncodeToString([]byte("new:new-private-canary")), &connects)
	ready, proceed := make(chan struct{}), make(chan struct{})
	var proceedOnce sync.Once
	releaseNegotiation := func() { proceedOnce.Do(func() { close(proceed) }) }
	defer releaseNegotiation()
	trailing := make(chan int64, 1)
	old := proxyTestStartGateway(t, func(conn net.Conn) {
		var hello [3]byte
		if _, err := io.ReadFull(conn, hello[:]); err != nil {
			trailing <- -1
			return
		}
		close(ready)
		<-proceed
		_, _ = conn.Write([]byte{5, 2})
		n, _ := io.Copy(io.Discard, conn)
		trailing <- n
	})
	client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr(proxyTestPrivateIP(t))}}, old)
	state.proxy.gateway.Protocol = "socks5"
	proxyTestRuleFor(t, state, target, true)
	oldReads := proxyTestAuth(state, "old", "old-private-canary")
	profile, _ := NewProfile(ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, AllowHTTP: true})
	request, _ := http.NewRequest("GET", "http://owned.example:"+strconv.Itoa(target.listener.Addr().(*net.TCPAddr).Port)+"/", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		response, err := client.Do(ctx, request, profile)
		if err == nil {
			response.Close()
		}
		result <- err
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		releaseNegotiation()
		t.Fatal("SOCKS negotiation barrier not reached")
	}
	var newReads *atomic.Int32
	proxyTestPublish(t, state, func(p *proxySnapshot) {
		g := *p.gateway
		g.Protocol = "http"
		g.Port = next.listener.Addr().(*net.TCPAddr).Port
		p.gateway = &g
		newReads = proxyTestAuth(state, "new", "new-private-canary")
	})
	releaseNegotiation()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("route refresh after negotiation:", err)
		}
	case <-ctx.Done():
		t.Fatal("route refresh did not complete")
	}
	select {
	case n := <-trailing:
		if n != 0 {
			t.Fatal("old gateway received authentication after rotation")
		}
	case <-ctx.Done():
		t.Fatal("old negotiation did not close")
	}
	if oldReads.Load() != 0 || newReads.Load() != 1 || requests.Load() != 1 || connects.Load() != 1 {
		t.Fatal("credential rotation did not isolate gateways")
	}
}

func TestProxyCredentialBorrowIsBoundedAndJoinedBeforeGateRelease(t *testing.T) {
	_, trust := proxyTestCertificate(t, "api.example")
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		n, _ := io.Copy(io.Discard, conn)
		if n != 0 {
			t.Error("cancelled credential borrow emitted proxy bytes")
		}
	})
	client, state := proxyTestClient(t, trust, &proxyTestResolver{}, gateway)
	state.proxy.gateway.Username = "test"
	state.proxy.gateway.CredentialPresent = true
	started := make(chan time.Time, 1)
	cancelled := make(chan struct{})
	finishCleanup := make(chan struct{})
	var cleanupOnce sync.Once
	finish := func() { cleanupOnce.Do(func() { close(finishCleanup) }) }
	defer finish()
	state.proxy.credential = func(ctx context.Context, _ func([]byte) error) error {
		deadline, _ := ctx.Deadline()
		started <- deadline
		<-ctx.Done()
		close(cancelled)
		<-finishCleanup
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, false, Limits{}))
		result <- err
	}()
	select {
	case deadline := <-started:
		if deadline.IsZero() || time.Until(deadline) > 2*time.Second {
			t.Fatal("credential callback received overall operation budget")
		}
	case <-time.After(time.Second):
		t.Fatal("credential callback not reached")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("credential callback did not observe cancellation")
	}
	state.gate.mu.Lock()
	readers := state.gate.readers
	writer := state.gate.writer
	state.gate.mu.Unlock()
	if readers != 1 || writer {
		t.Fatal("shared gate released before original credential callback joined")
	}
	client.StopAdmission()
	drain, cancelDrain := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := client.Drain(drain)
	cancelDrain()
	if err == nil {
		t.Fatal("client Drain ignored active credential callback")
	}
	finish()
	select {
	case err := <-result:
		if err == nil || SafeNetworkError(err).Decision().Sent || SafeNetworkError(err).Decision().Reason != ac.Cancelled {
			t.Fatal("credential cancellation lost send outcome")
		}
	case <-time.After(time.Second):
		t.Fatal("credential callback did not join request")
	}
	drain, cancelDrain = context.WithTimeout(context.Background(), time.Second)
	defer cancelDrain()
	if err := client.Drain(drain); err != nil {
		t.Fatal("joined credential callback prevented Drain", err)
	}
	state.gate.mu.Lock()
	readers = state.gate.readers
	state.gate.mu.Unlock()
	if readers != 0 {
		t.Fatal("credential callback left shared gate held")
	}
}

type proxyDeadlineCancelConn struct {
	net.Conn
	cancel context.CancelFunc
	read   bool
}

func (c *proxyDeadlineCancelConn) SetReadDeadline(deadline time.Time) error {
	if c.read {
		c.cancel()
		return net.ErrClosed
	}
	return c.Conn.SetReadDeadline(deadline)
}
func (c *proxyDeadlineCancelConn) SetWriteDeadline(deadline time.Time) error {
	if !c.read {
		c.cancel()
		return net.ErrClosed
	}
	return c.Conn.SetWriteDeadline(deadline)
}

// Force the cancellation/socket-close ordering at the exact deadline setter;
// the protocol never reaches a business write and must preserve cancellation.
func TestProxyDeadlineSetterCancellationReason(t *testing.T) {
	for _, protocol := range []string{"http", "socks5"} {
		for _, phase := range []string{"read", "write"} {
			t.Run(protocol+"/"+phase, func(t *testing.T) {
				_, trust := proxyTestCertificate(t, "api.example")
				client, state := proxyTestClient(t, trust, &proxyTestResolver{}, nil)
				state.proxy.mode = "proxy"
				state.proxy.gateway = &ProxyGateway{Protocol: protocol, Host: "10.2.3.4", Port: 8080}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				profile := proxyTestProfile(t, false, Limits{})
				target, _ := ParseTarget("https://api.example/")
				a := newAttempt(&operation{ctx: ctx}, client, profile.data(), target, 0)
				if err := a.resolve(); err != nil {
					t.Fatal(err)
				}
				raw, peer := net.Pipe()
				defer raw.Close()
				defer peer.Close()
				pc := &pooledConn{conn: &proxyDeadlineCancelConn{Conn: raw, cancel: cancel, read: phase == "read"}, peer: netip.MustParseAddr("10.2.3.4"), targetPeer: netip.MustParseAddr("8.8.8.8")}
				joined := make(chan error, 1)
				if phase == "read" {
					go func() {
						_ = peer.SetReadDeadline(time.Now().Add(time.Second))
						var err error
						if protocol == "http" {
							var request *http.Request
							request, err = http.ReadRequest(bufio.NewReader(peer))
							if err == nil {
								request.Body.Close()
							}
						} else {
							var hello [3]byte
							_, err = io.ReadFull(peer, hello[:])
						}
						joined <- err
					}()
				}
				var err error
				if protocol == "http" {
					err = a.proxyConnect(pc)
				} else {
					err = a.proxySOCKS(pc)
				}
				if err == nil || SafeNetworkError(err).Decision().Reason != ac.Cancelled || SafeNetworkError(err).Decision().Sent {
					t.Fatal("deadline setter hid cancellation", err)
				}
				if phase == "read" {
					select {
					case err := <-joined:
						if err != nil {
							t.Fatal("owned protocol reader failed")
						}
					case <-time.After(time.Second):
						t.Fatal("owned protocol reader did not join")
					}
				}
			})
		}
	}
}
