//go:build integration

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

func TestSystemOutboundProxyHTTPRouteChanges(t *testing.T) {
	if os.Getenv(netfixture.Env) == "" {
		t.Skip("requires nonce-owned network fixture from scripts/test-security.sh")
	}
	network, err := netfixture.Load()
	previewCheck(t, "owned outbound fixture", err)
	trust, err := outbound.LoadTrustStore(network.CAFile)
	previewCheck(t, "owned outbound CA", err)
	var runtime *outboundRuntime
	a := newModelRootApp(t, "3s", func(_ *modelRootApp, deps *dependencies) {
		deps.outboundConstruct = func(ctx context.Context, _ config.Config, created *outboundRuntime) error {
			runtime = created
			// Only the unconsumed client's fixture DNS and CA change. Its policy,
			// proxy store, Secret authority, process owner and classifiers are real.
			if err := created.client.ForceClose(ctx); err != nil {
				return err
			}
			created.client, err = outbound.NewClient(created.policy, trust, appFixtureResolver{netip.MustParseAddr(network.PrivateIP)})
			return err
		}
	})
	address := rootRouteAddress(t, a)
	if runtime == nil || a.owned.outbound() != runtime {
		t.Fatal("management API and transport did not share the root runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	login, err := fixtureAccountLoginResponse(ctx, a.cfg, a.core)
	previewCheck(t, "formal account login", err)
	var cookie sc.SecretMaterial
	err = login.UseCookie(func(raw []byte) error {
		var err error
		cookie, err = sc.NewSecretMaterial(raw)
		return err
	})
	previewCheck(t, "login cookie", err)
	defer cookie.Destroy()
	previewCheck(t, "login response join", login.Close(ctx))
	actor, err := a.core.Authenticate(ctx, cookie)
	previewCheck(t, "current session authentication", err)
	view, err := a.core.GetSession(ctx, cookie)
	previewCheck(t, "current session CSRF", err)
	defer view.CSRF.Destroy()
	var cookieValue, csrf string
	previewCheck(t, "read owned cookie", cookie.Use(func(raw []byte) error { cookieValue = string(raw); return nil }))
	previewCheck(t, "read owned CSRF", view.CSRF.Use(func(raw []byte) error { csrf = string(raw); return nil }))
	management := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	defer management.CloseIdleConnections()
	callAPI := func(method, path string, input any, output any) {
		t.Helper()
		var body []byte
		if input != nil {
			var err error
			body, err = json.Marshal(input)
			previewCheck(t, "encode management request", err)
			defer clear(body)
		}
		r, err := http.NewRequestWithContext(ctx, method, address+path, bytes.NewReader(body))
		previewCheck(t, "create management request", err)
		r.Host = "localhost:8080"
		r.Header.Set("Origin", "http://localhost:8080")
		r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookieValue})
		if method == http.MethodPut {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
			r.Header.Set("Idempotency-Key", guardID[struct{}](t).String())
		}
		response, err := management.Do(r)
		previewCheck(t, "management HTTP response", err)
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || len(raw) > 1<<20 {
			t.Fatal("management request failed or returned an incomplete response", response.StatusCode)
		}
		if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("management response bypassed shared HTTP middleware")
		}
		previewCheck(t, "decode management response", json.Unmarshal(raw, output))
	}
	const policyPath = "/api/v1/system/outbound-policy"
	const proxyPath = policyPath + "/proxy"
	var saved struct{ Version string }
	callAPI(http.MethodPut, policyPath, map[string]any{
		"expected_version": "1",
		"rules":            []any{map[string]any{"cidr": network.PrivateIP + "/32", "ports": []int{8443}, "allow_http": false}},
	}, &saved)
	if saved.Version != "2" {
		t.Fatal("HTTP policy save did not advance the current version")
	}
	bodyMarker := "owned-proxy-route-" + guardID[struct{}](t).String()
	scenario, err := network.Create(ctx, netfixture.ScenarioConfig{Body: bodyMarker})
	previewCheck(t, "create owned HTTPS scenario", err)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		if err := network.Release(cleanup, scenario); err != nil {
			t.Error("owned scenario release failed")
		}
	})
	password := guardID[struct{}](t).String()
	username := "owned-proxy-user"
	proxyAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	gateway := startRootRouteGateway(t, net.JoinHostPort(network.PrivateIP, "8443"), proxyAuth)
	gatewayAddress := gateway.listener.Addr().(*net.TCPAddr)
	proxyInput := func(version, mode, action, user string, port int) map[string]any {
		return map[string]any{
			"expected_version": version, "mode": mode, "credential_action": action,
			"gateway": map[string]any{"protocol": "http", "host": gatewayAddress.IP.String(), "port": port, "username": user},
		}
	}
	authenticated := proxyInput("1", "proxy", "keep", username, gatewayAddress.Port)
	authenticated["password"] = password
	callAPI(http.MethodPut, proxyPath, authenticated, &saved)
	delete(authenticated, "password")
	if saved.Version != "2" {
		t.Fatal("HTTP proxy save did not advance the current version")
	}
	businessToken := "owned-target-" + guardID[struct{}](t).String()
	businessAuth := "Bearer " + businessToken
	businessMaterial, err := sc.NewSecretMaterial([]byte(businessToken))
	previewCheck(t, "owned business credential", err)
	defer businessMaterial.Destroy()
	businessField, err := outbound.HeaderCredential("Authorization", "Bearer ", businessMaterial)
	previewCheck(t, "business credential field", err)
	businessOrigin, err := outbound.ParseOrigin("https://fixture.test:8443")
	previewCheck(t, "business credential origin", err)
	businessCredentials, err := outbound.NewCredentialBinding(businessOrigin, businessField)
	previewCheck(t, "business credential binding", err)
	requestTarget := func(wantSuccess bool) {
		t.Helper()
		key, err := ac.NewAppendKey(ac.AccessProducer, guardID[struct{}](t).String(), 0)
		previewCheck(t, "target audit key", err)
		call, err := outbound.NewCallContext(actor, identity.SystemScope(), key, ac.Associations{})
		previewCheck(t, "target call context", err)
		profile, err := outbound.NewProfile(outbound.ProfileOptions{Consumer: ac.Model, Context: call, Credentials: businessCredentials, Limits: outbound.Limits{Overall: 5 * time.Second}})
		previewCheck(t, "target profile", err)
		request, err := http.NewRequest(http.MethodGet, "https://fixture.test:8443/case/"+scenario, nil)
		previewCheck(t, "owned target request", err)
		response, err := runtime.client.Do(ctx, request, profile)
		if !wantSuccess {
			if err == nil {
				_ = response.Close()
				t.Fatal("closed configured gateway unexpectedly produced a response")
			}
			if outbound.SafeNetworkError(err).Decision().Sent {
				t.Fatal("failed gateway was reported after sending business bytes")
			}
			return
		}
		previewCheck(t, "owned HTTPS request", err)
		body, readErr := io.ReadAll(io.LimitReader(response.Body(), 256))
		closeErr := response.Close()
		if readErr != nil || closeErr != nil || string(body) != bodyMarker {
			t.Fatal("actual owned HTTPS target response did not arrive")
		}
	}
	checkTarget := func(want int) netfixture.State {
		t.Helper()
		state, err := network.State(ctx, scenario)
		previewCheck(t, "owned target observation", err)
		if len(state.Requests) != want {
			t.Fatal("owned target request count changed across route transition", len(state.Requests), want)
		}
		for _, request := range state.Requests {
			if request.Headers.Get("Proxy-Authorization") != "" || request.Headers.Get("Authorization") != businessAuth || request.Host != "fixture.test:8443" || request.RequestURI != "/case/"+scenario {
				t.Fatal("target received proxy credentials or lost its original request identity")
			}
		}
		return state
	}
	requestTarget(true)
	checkTarget(1)
	if gateway.connects.Load() != 1 {
		t.Fatal("first request did not traverse exactly one authenticated CONNECT")
	}
	var operations, active, released int64
	err = a.store.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_outbound.proxy_credential_operations),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='outbound_proxy' AND NOT released),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='outbound_proxy' AND released AND consumer='system' AND scope='system')`).Scan(&operations, &active, &released)
	previewCheck(t, "actual credential lease cleanup", err)
	if operations != 0 || active != 0 || released < 1 {
		t.Fatal("real proxy credential borrow was absent or did not synchronously release")
	}
	// Reserve a port owned by this test, then close it before configuring it.
	// The following request must neither reuse the old tunnel nor dial direct.
	closed, err := net.Listen("tcp", net.JoinHostPort(gatewayAddress.IP.String(), "0"))
	previewCheck(t, "owned unavailable gateway", err)
	closedPort := closed.Addr().(*net.TCPAddr).Port
	previewCheck(t, "close owned unavailable gateway", closed.Close())
	callAPI(http.MethodPut, proxyPath, proxyInput("2", "proxy", "remove", "", closedPort), &saved)
	if saved.Version != "3" {
		t.Fatal("unavailable proxy route was not saved")
	}
	requestTarget(false)
	checkTarget(1)
	if gateway.connects.Load() != 1 {
		t.Fatal("new route reused the former proxy gateway")
	}
	callAPI(http.MethodPut, proxyPath, proxyInput("3", "direct", "keep", "", closedPort), &saved)
	if saved.Version != "4" {
		t.Fatal("direct route was not saved")
	}
	var current outbound.ProxySettings
	callAPI(http.MethodGet, proxyPath, nil, &current)
	if current.Mode != "direct" || current.Gateway == nil || current.Gateway.Host != gatewayAddress.IP.String() || current.Gateway.Port != closedPort || current.Gateway.CredentialPresent {
		t.Fatal("direct mode did not preserve the saved gateway without credentials")
	}
	requestTarget(true)
	state := checkTarget(2)
	if gateway.connects.Load() != 1 {
		t.Fatal("direct route contacted a gateway")
	}
	for _, sensitive := range []string{password, proxyAuth, cookieValue, csrf, businessAuth} {
		if strings.Contains(a.logs.String(), sensitive) {
			t.Fatal("ordinary root logs exposed owned authentication material")
		}
	}
	a.signals <- syscall.SIGTERM
	await(t, a.done)
	if a.err != nil || !a.owned.accounts().Joined() {
		t.Fatal("root route scenario did not actually join", a.err)
	}
	gateway.stop(t)
	closedCtx, closedCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closedCancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		currentState, err := network.State(closedCtx, scenario)
		previewCheck(t, "owned target closure observation", err)
		joined := currentState.ActiveHandlers == 0
		for _, request := range state.Requests {
			joined = joined && currentState.Closed[request.Connection]
		}
		if joined {
			break
		}
		select {
		case <-closedCtx.Done():
			t.Fatal("owned target connection outlived root and gateway join")
		case <-ticker.C:
		}
	}
}

func rootRouteAddress(t *testing.T, app *modelRootApp) string {
	t.Helper()
	// Observe the real startup event within its existing two phase budgets.
	// The generic five-second log helper can expire during owned DB migration.
	timer := time.NewTimer(app.cfg.Database().StartupTimeout() + SecurityStartupTimeout)
	defer timer.Stop()
	for {
		select {
		case event := <-app.logs.events:
			if event["event"] == "listening" {
				address, ok := event["listen_address"].(string)
				if !ok || address == "" {
					t.Fatal("root listening event lacked its owned address")
				}
				return "http://" + address
			}
		case <-app.done:
			t.Fatal("root exited before the route scenario could start", app.err)
		case <-timer.C:
			t.Fatal("root did not listen within its configured startup budgets")
		}
	}
}

type rootRouteGateway struct {
	listener net.Listener
	mu       sync.Mutex
	closing  bool
	conns    map[net.Conn]bool
	workers  sync.WaitGroup
	done     chan struct{}
	connects atomic.Int64
}

func startRootRouteGateway(t *testing.T, target, authorization string) *rootRouteGateway {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	previewCheck(t, "owned gateway interface", err)
	var host string
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is4() && outbound.Classify(prefix.Addr()) == outbound.Private {
			host = prefix.Addr().String()
			break
		}
	}
	if host == "" {
		t.Fatal("owned private interface required for the CONNECT gateway")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(0)))
	previewCheck(t, "owned CONNECT listener", err)
	gateway := &rootRouteGateway{listener: listener, conns: make(map[net.Conn]bool), done: make(chan struct{})}
	go func() {
		defer close(gateway.done)
		defer gateway.workers.Wait()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			if !gateway.track(conn) {
				return
			}
			gateway.workers.Add(1)
			go func() {
				defer gateway.workers.Done()
				defer gateway.closeConnection(conn)
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				reader := bufio.NewReader(conn)
				request, err := http.ReadRequest(reader)
				if err != nil {
					t.Error("owned gateway did not receive a complete CONNECT")
					return
				}
				defer request.Body.Close()
				if request.Method != http.MethodConnect || request.RequestURI != target || request.Host != target || request.Header.Get("Proxy-Authorization") != authorization || request.Header.Get("Authorization") != "" {
					t.Error("gateway lost numeric target pinning or credential separation")
					return
				}
				// The request cannot choose an endpoint: only this already-verified
				// fixture's exact private socket may ever be forwarded.
				upstream, err := net.DialTimeout("tcp", target, 2*time.Second)
				if err != nil {
					t.Error("owned gateway could not dial its exact fixture target")
					return
				}
				if !gateway.track(upstream) {
					return
				}
				defer gateway.closeConnection(upstream)
				_ = upstream.SetDeadline(time.Now().Add(30 * time.Second))
				gateway.connects.Add(1)
				if _, err = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
					t.Error("owned gateway CONNECT response failed")
					return
				}
				copied := make(chan struct{})
				go func() {
					_, _ = io.Copy(upstream, reader)
					_ = upstream.Close()
					close(copied)
				}()
				_, _ = io.Copy(conn, upstream)
				_ = conn.Close()
				_ = upstream.Close()
				<-copied
			}()
		}
	}()
	t.Cleanup(func() { gateway.stop(t) })
	return gateway
}

func (g *rootRouteGateway) track(conn net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		_ = conn.Close()
		return false
	}
	g.conns[conn] = true
	return true
}
func (g *rootRouteGateway) closeConnection(conn net.Conn) {
	_ = conn.Close()
	g.mu.Lock()
	delete(g.conns, conn)
	g.mu.Unlock()
}
func (g *rootRouteGateway) stop(t *testing.T) {
	t.Helper()
	_ = g.listener.Close()
	g.mu.Lock()
	g.closing = true
	for conn := range g.conns {
		_ = conn.Close()
	}
	g.mu.Unlock()
	select {
	case <-g.done:
	case <-time.After(2 * time.Second):
		t.Error("owned gateway accept loop or tunnel copies did not actually join")
	}
}
