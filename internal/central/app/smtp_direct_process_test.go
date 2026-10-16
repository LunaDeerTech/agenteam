//go:build integration

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

func TestSMTPRootDirectIgnoresOutboundPolicyAndProxy(t *testing.T) {
	if os.Getenv(netfixture.Env) == "" {
		t.Skip("requires nonce-owned PG, Object and network fixtures from scripts/test-security.sh")
	}
	network, err := netfixture.Load()
	previewCheck(t, "owned outbound fixture", err)
	trust, err := outbound.LoadTrustStore(network.CAFile)
	previewCheck(t, "owned HTTPS trust", err)
	setup, setupDone := context.WithTimeout(context.Background(), 40*time.Second)
	smtp, err := smtpfixture.Start(setup)
	setupDone()
	previewCheck(t, "owned SMTP fixture", err)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if err := smtp.Close(cleanup); err != nil {
			t.Error("owned SMTP fixture cleanup failed", err)
		}
	})
	setup, setupDone = context.WithTimeout(context.Background(), 5*time.Second)
	endpoint, err := smtp.Create(setup, smtpfixture.Scenario{Mode: "starttls"})
	setupDone()
	previewCheck(t, "owned STARTTLS endpoint", err)
	resolver := smtpDirectRootResolver{smtp: netip.MustParseAddr(smtp.PrivateIP), http: netip.MustParseAddr(network.PrivateIP)}
	gateway := startSMTPDirectRejectingGateway(t, net.JoinHostPort(network.PrivateIP, "8443"))
	var runtime *outboundRuntime
	a := newModelRootApp(t, "3s", func(a *modelRootApp, deps *dependencies) {
		a.cfg = guardConfiguration(t, append(guardEnvironment(t, a.db),
			"AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080",
			"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=3s",
			"AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+smtp.CAFile))
		// Both injected resolvers provide only owned DNS facts. SMTP retains
		// its real delivery port, Secret, send permit and direct transport.
		deps.smtpResolver = resolver
		deps.outboundConstruct = func(ctx context.Context, _ config.Config, created *outboundRuntime) error {
			runtime = created
			if err := created.client.ForceClose(ctx); err != nil {
				return err
			}
			created.client, err = outbound.NewClient(created.policy, trust, resolver)
			return err
		}
	})
	address := rootRouteAddress(t, a)
	if runtime == nil || a.owned.outbound() != runtime {
		t.Fatal("HTTP controls did not use the root outbound runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	login, err := fixtureAccountLoginResponse(ctx, a.cfg, a.core)
	previewCheck(t, "formal administrator login", err)
	var cookie sc.SecretMaterial
	previewCheck(t, "login cookie", login.UseCookie(func(raw []byte) error {
		var err error
		cookie, err = sc.NewSecretMaterial(raw)
		return err
	}))
	defer cookie.Destroy()
	previewCheck(t, "login response join", login.Close(ctx))
	actor, err := a.core.Authenticate(ctx, cookie)
	previewCheck(t, "current administrator session", err)
	view, err := a.core.GetSession(ctx, cookie)
	previewCheck(t, "current session CSRF", err)
	defer view.CSRF.Destroy()
	var cookieValue, csrf string
	previewCheck(t, "read owned cookie", cookie.Use(func(raw []byte) error { cookieValue = string(raw); return nil }))
	previewCheck(t, "read owned CSRF", view.CSRF.Use(func(raw []byte) error { csrf = string(raw); return nil }))
	management := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	defer management.CloseIdleConnections()
	callAPI := func(method, path string, input, output any, want int) {
		t.Helper()
		var body []byte
		if input != nil {
			var err error
			body, err = json.Marshal(input)
			previewCheck(t, "encode management request", err)
			defer clear(body)
		}
		request, err := http.NewRequestWithContext(ctx, method, address+path, bytes.NewReader(body))
		previewCheck(t, "create management request", err)
		request.Host = "localhost:8080"
		request.Header.Set("Origin", "http://localhost:8080")
		request.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookieValue})
		if method != http.MethodGet {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", csrf)
			request.Header.Set("Idempotency-Key", guardID[struct{}](t).String())
		}
		response, err := management.Do(request)
		previewCheck(t, "management HTTP response", err)
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		defer clear(raw)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(raw) > 1<<20 || response.StatusCode != want {
			t.Fatal("management request failed", method, path, response.StatusCode, want)
		}
		if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("management response bypassed shared HTTP middleware")
		}
		previewCheck(t, "decode management response", json.Unmarshal(raw, output))
	}
	const policyPath = "/api/v1/system/outbound-policy"
	var saved struct{ Version string }
	callAPI(http.MethodPut, policyPath, map[string]any{
		"expected_version": "1", "rule_mode": "allowlist",
		"rules": []any{map[string]any{"target": network.PrivateIP + "/32", "ports": []int{8443}}},
	}, &saved, http.StatusOK)
	if saved.Version != "2" {
		t.Fatal("global access rule did not commit")
	}
	gatewayAddress := gateway.listener.Addr().(*net.TCPAddr)
	callAPI(http.MethodPut, policyPath+"/proxy", map[string]any{
		"expected_version": "1", "mode": "proxy", "credential_action": "remove",
		"gateway": map[string]any{"protocol": "http", "host": gatewayAddress.IP.String(), "port": gatewayAddress.Port, "username": ""},
	}, &saved, http.StatusOK)
	if saved.Version != "2" {
		t.Fatal("rejecting global proxy did not commit")
	}
	scenario, err := network.Create(ctx, netfixture.ScenarioConfig{Body: "owned SMTP direct HTTP control"})
	previewCheck(t, "owned HTTPS control scenario", err)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		if err := network.Release(cleanup, scenario); err != nil {
			t.Error("owned HTTPS scenario release failed")
		}
	})
	requestBlocked := func(target string) outbound.Decision {
		t.Helper()
		key, err := ac.NewAppendKey(ac.AccessProducer, guardID[struct{}](t).String(), 0)
		previewCheck(t, "HTTP control audit key", err)
		call, err := outbound.NewCallContext(actor, identity.SystemScope(), key, ac.Associations{})
		previewCheck(t, "HTTP control context", err)
		profile, err := outbound.NewProfile(outbound.ProfileOptions{Consumer: ac.Model, Context: call, Limits: outbound.Limits{Overall: 5 * time.Second}})
		previewCheck(t, "HTTP control profile", err)
		request, err := http.NewRequest(http.MethodGet, target, nil)
		previewCheck(t, "HTTP control request", err)
		response, err := runtime.client.Do(ctx, request, profile)
		if err == nil {
			_ = response.Close()
			t.Fatal("root HTTP escaped its global access rules or proxy")
		}
		decision := outbound.SafeNetworkError(err).Decision()
		if decision.Sent {
			t.Fatal("blocked HTTP control sent application bytes")
		}
		return decision
	}
	blockedSMTP := requestBlocked("https://" + net.JoinHostPort(smtpfixture.Host, strconv.Itoa(endpoint.Port)) + "/")
	before, err := smtp.State(ctx, endpoint.ID)
	previewCheck(t, "SMTP control observation", err)
	if blockedSMTP.Reason != ac.RuleNotAllowed || before.Connections != 0 || gateway.connects.Load() != 0 {
		t.Fatal("global access rule did not reject the SMTP destination before dialing")
	}
	httpTarget := "https://fixture.test:8443/case/" + scenario
	requestBlocked(httpTarget)
	if gateway.connects.Load() != 1 {
		t.Fatal("allowed HTTP destination did not actually reach the rejecting global proxy")
	}
	assertHTTPTargetUntouched := func() {
		t.Helper()
		state, err := network.State(ctx, scenario)
		previewCheck(t, "owned HTTPS control observation", err)
		if len(state.Requests) != 0 {
			t.Fatal("HTTP fell back to direct delivery around the rejecting proxy")
		}
	}
	assertHTTPTargetUntouched()
	const smtpPath = "/api/v1/system/smtp"
	var settings struct{ Version string }
	callAPI(http.MethodGet, smtpPath, nil, &settings, http.StatusOK)
	password := guardID[struct{}](t).String()
	var configured struct {
		Settings struct {
			Configured        bool `json:"configured"`
			CredentialPresent bool `json:"credential_present"`
		}
	}
	callAPI(http.MethodPut, smtpPath, map[string]any{
		"version": settings.Version, "host": smtpfixture.Host, "port": endpoint.Port,
		"encryption": "starttls", "username": "fixture", "password": password, "credential_action": "keep",
		"sender_email": "sender@example.test", "sender_name": "Owned SMTP direct",
		"auto_retry_count": "0", "retry_interval_seconds": "10",
	}, &configured, http.StatusOK)
	if !configured.Settings.Configured || !configured.Settings.CredentialPresent {
		t.Fatal("formal SMTP configuration did not persist its credential")
	}
	var accepted struct {
		JobID string `json:"job_id"`
	}
	callAPI(http.MethodPost, smtpPath+"/test", map[string]string{"recipient": "smtp-direct@example.test"}, &accepted, http.StatusAccepted)
	if accepted.JobID == "" {
		t.Fatal("formal SMTP test did not return a job")
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status struct{ Phase, Attempts string }
		callAPI(http.MethodGet, "/api/v1/system/mail-jobs/"+accepted.JobID, nil, &status, http.StatusOK)
		if status.Phase == "sent" {
			if status.Attempts != "1" {
				t.Fatal("SMTP direct delivery required more than one attempt")
			}
			break
		}
		if status.Phase == "failed" || status.Phase == "cancelled" || status.Phase == "delivery_unknown" {
			t.Fatal("formal SMTP job did not send", status.Phase)
		}
		select {
		case <-ctx.Done():
			t.Fatal("SMTP root scenario exceeded its budget")
		case <-deadline.C:
			t.Fatal("SMTP root runtime did not settle its job")
		case <-ticker.C:
		}
	}
	state, err := smtp.State(ctx, endpoint.ID)
	previewCheck(t, "actual SMTP delivery observation", err)
	if state.Connections != 1 || state.TLS != 1 || state.Auth != 1 || state.Mail != 1 || state.RCPT != 1 || state.Data != 1 || state.Messages != 1 || gateway.connects.Load() != 1 {
		t.Fatal("SMTP did not deliver directly with STARTTLS and Secret authentication", state)
	}
	var terminal, joined bool
	var leases, active int
	err = a.store.QueryRow(ctx, `SELECT a.terminal,a.io_joined,
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id AND NOT released)
 FROM agenteam_account.mail_attempts a WHERE a.job_id=$1`, accepted.JobID).Scan(&terminal, &joined, &leases, &active)
	previewCheck(t, "SMTP actual join and Secret release", err)
	if !terminal || !joined || leases != 1 || active != 0 {
		t.Fatal("SMTP direct delivery did not settle its attempt and real Secret lease")
	}
	requestBlocked(httpTarget)
	assertHTTPTargetUntouched()
	if gateway.connects.Load() != 2 {
		t.Fatal("SMTP delivery changed the ordinary HTTP proxy route")
	}
	for _, sensitive := range []string{password, cookieValue, csrf} {
		if strings.Contains(a.logs.String(), sensitive) {
			t.Fatal("root logs exposed owned authentication material")
		}
	}
	a.signals <- syscall.SIGTERM
	await(t, a.done)
	if a.err != nil || !a.owned.accounts().Joined() {
		t.Fatal("SMTP root runtime did not actually join", a.err)
	}
	gateway.stop(t)
	closed, closedDone := context.WithTimeout(context.Background(), 2*time.Second)
	defer closedDone()
	for {
		state, err = smtp.State(closed, endpoint.ID)
		previewCheck(t, "owned SMTP socket closure", err)
		if state.Closed == state.Connections {
			break
		}
		select {
		case <-closed.Done():
			t.Fatal("SMTP socket outlived the root runtime join")
		case <-ticker.C:
		}
	}
}

type smtpDirectRootResolver struct{ smtp, http netip.Addr }

func (r smtpDirectRootResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch host {
	case smtpfixture.Host:
		return []netip.Addr{r.smtp}, nil
	case "fixture.test":
		return []netip.Addr{r.http}, nil
	default:
		return nil, &net.DNSError{Err: "not an owned fixture", Name: host, IsNotFound: true}
	}
}

func (r smtpDirectRootResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if host != smtpfixture.Host {
		return nil, &net.DNSError{Err: "not the owned SMTP fixture", Name: host, IsNotFound: true}
	}
	return []net.IPAddr{{IP: net.IP(r.smtp.AsSlice())}}, nil
}

func startSMTPDirectRejectingGateway(t *testing.T, target string) *rootRouteGateway {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	previewCheck(t, "owned rejecting gateway interface", err)
	var host string
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is4() && outbound.Classify(prefix.Addr()) == outbound.Private {
			host = prefix.Addr().String()
			break
		}
	}
	if host == "" {
		t.Fatal("owned private interface required for the rejecting gateway")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	previewCheck(t, "owned rejecting CONNECT listener", err)
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
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				request, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					t.Error("owned rejecting gateway did not receive a complete CONNECT")
					return
				}
				defer request.Body.Close()
				if request.Method != http.MethodConnect || request.Host != target || request.RequestURI != target || request.Header.Get("Authorization") != "" || request.Header.Get("Proxy-Authorization") != "" {
					t.Error("global gateway received an unexpected target or credential")
					return
				}
				gateway.connects.Add(1)
				if _, err := io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"); err != nil {
					t.Error("owned gateway rejection write failed")
				}
			}()
		}
	}()
	t.Cleanup(func() { gateway.stop(t) })
	return gateway
}
