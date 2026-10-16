//go:build integration

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type independentModeRule struct {
	Target string          `json:"target"`
	Ports  json.RawMessage `json:"ports"`
}

type independentModePolicy struct {
	Version string                `json:"version"`
	Mode    string                `json:"rule_mode"`
	Rules   []independentModeRule `json:"rules"`
}

// Only DNS answers are controlled here; all actual dials, policy publication,
// first writes, management authorization and persistence use the real root.
// This is not a claim about the production resolver's DNS wire protocol.
type independentModeResolver struct{}

func (independentModeResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ips := []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	if host == "mixed.allowed.test" {
		ips = append(ips, netip.MustParseAddr("127.0.0.2"))
	}
	return ips, nil
}

func TestIndependentOutboundRuleModesHTTPAndCurrentPolicy(t *testing.T) {
	var requests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/redirect" {
			_, port, err := net.SplitHostPort(r.Host)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Location", "http://blocked.test:"+port+"/target")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "owned-rule-mode-response")
	}))
	t.Cleanup(target.Close)
	_, port, err := net.SplitHostPort(target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	ports, err := json.Marshal([]uint16{uint16(portNumber)})
	if err != nil {
		t.Fatal(err)
	}
	rule := func(target string) independentModeRule { return independentModeRule{Target: target, Ports: ports} }
	var runtime *outboundRuntime
	a := newModelRootApp(t, "3s", func(_ *modelRootApp, d *dependencies) {
		d.outboundConstruct = func(ctx context.Context, cfg config.Config, created *outboundRuntime) error {
			runtime = created
			if err := created.client.ForceClose(ctx); err != nil {
				return err
			}
			client, err := outbound.NewClient(created.policy, cfg.OutboundTrust(), independentModeResolver{})
			if err != nil {
				return err
			}
			created.client = client
			return nil
		}
	})
	browser := independentProxyLogin(t, a)
	if runtime == nil || a.owned.outbound() != runtime {
		t.Fatal("independent root runtime missing")
	}
	material, err := sc.NewSecretMaterial([]byte(browser.cookie))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	loginCtx, loginCancel := context.WithTimeout(context.Background(), 10*time.Second)
	actor, err := a.core.Authenticate(loginCtx, material)
	loginCancel()
	if err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/system/outbound-policy"
	read := func() independentModePolicy {
		t.Helper()
		raw := browser.call(t, "GET", path, "", nil, nil, 200)
		var fields map[string]json.RawMessage
		var current independentModePolicy
		if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 || fields["version"] == nil || fields["rule_mode"] == nil || fields["rules"] == nil || json.Unmarshal(raw, &current) != nil || current.Rules == nil {
			t.Fatal("new rule projection was incomplete or retained legacy fields")
		}
		return current
	}
	current := read()
	if current.Version != "1" || current.Mode != "denylist" || len(current.Rules) != 32 {
		t.Fatal("explicit default denylist missing")
	}
	save := func(mode string, rules []independentModeRule) (string, map[string]any) {
		t.Helper()
		body := map[string]any{"expected_version": current.Version, "rule_mode": mode, "rules": rules}
		key := guardID[struct{}](t).String()
		raw := browser.call(t, "PUT", path, key, body, nil, 200)
		oldVersion, err := strconv.ParseUint(current.Version, 10, 63)
		if err != nil {
			t.Fatal(err)
		}
		current.Version = strconv.FormatUint(oldVersion+1, 10)
		independentProxyVersion(t, raw, current.Version)
		current.Mode, current.Rules = mode, rules
		return key, body
	}
	invoke := func(ctx context.Context, host, targetPath string) (outbound.Decision, error) {
		id, err := ac.NewAppendKey(ac.AccessProducer, guardID[struct{}](t).String(), 0)
		if err != nil {
			return outbound.Decision{}, err
		}
		call, err := outbound.NewCallContext(actor, identity.SystemScope(), id, ac.Associations{})
		if err != nil {
			return outbound.Decision{}, err
		}
		profile, err := outbound.NewProfile(outbound.ProfileOptions{Consumer: ac.Model, Context: call, Limits: outbound.Limits{Overall: 10 * time.Second}})
		if err != nil {
			return outbound.Decision{}, err
		}
		request, err := http.NewRequest(http.MethodGet, "http://"+net.JoinHostPort(host, port)+targetPath, nil)
		if err != nil {
			return outbound.Decision{}, err
		}
		response, err := runtime.client.Do(ctx, request, profile)
		if err != nil {
			return outbound.SafeNetworkError(err).Decision(), err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body(), 128))
		closeErr := response.Close()
		if readErr != nil || closeErr != nil || string(body) != "owned-rule-mode-response" {
			return response.Decision(), errors.New("owned target response mismatch")
		}
		return response.Decision(), nil
	}
	check := func(host, targetPath string, allowed bool, writes int64) {
		t.Helper()
		before := requests.Load()
		decision, err := invoke(context.Background(), host, targetPath)
		if allowed && err != nil {
			t.Fatal("expected owned target success", outbound.SafeNetworkError(err).Decision().Reason)
		}
		if !allowed && err == nil {
			t.Fatal("denied target returned a response")
		}
		if !allowed && writes == 0 && decision.Sent {
			t.Fatal("denied request sent business bytes")
		}
		if requests.Load() != before+writes {
			t.Fatal("unexpected owned target request count", requests.Load()-before, writes)
		}
	}
	// The default is visible configuration, and deleting its loopback rule takes
	// effect without an unrelated hidden classifier or HTTP permission flag.
	check("127.0.0.1", "/target", false, 0)
	withoutLoopback := make([]independentModeRule, 0, len(current.Rules))
	removed := 0
	for _, item := range current.Rules {
		if item.Target == "127.0.0.0/8" {
			removed++
			continue
		}
		withoutLoopback = append(withoutLoopback, item)
	}
	if removed != 1 {
		t.Fatal("editable loopback default not present exactly once")
	}
	save("denylist", withoutLoopback)
	check("127.0.0.1", "/target", true, 1)
	// Legacy bodies and fields must not silently select a mode or execute.
	versionBefore := current.Version
	for _, body := range []any{
		map[string]any{"expected_version": current.Version, "rules": []any{}},
		map[string]any{"expected_version": current.Version, "rule_mode": "denylist", "rules": []any{map[string]any{"cidr": "127.0.0.0/8", "ports": "all", "allow_http": true}}},
		map[string]any{"expected_version": current.Version, "rule_mode": nil, "rules": []any{}},
	} {
		browser.call(t, "PUT", path, guardID[struct{}](t).String(), body, nil, 400)
	}
	if read().Version != versionBefore {
		t.Fatal("rejected legacy rule request changed current state")
	}
	empty := []independentModeRule{}
	save("denylist", empty)
	check("metadata.google.internal", "/target", true, 1)
	wildcard := []independentModeRule{rule("*.allowed.test")}
	save("allowlist", wildcard)
	check("a.allowed.test", "/target", true, 1)
	check("a.b.allowed.test", "/target", true, 1)
	check("allowed.test", "/target", false, 0)
	check("notallowed.test", "/target", false, 0)
	check("a.allowed.test.evil", "/target", false, 0)
	check("mixed.allowed.test", "/target", true, 1)
	save("allowlist", []independentModeRule{rule("127.0.0.1")})
	check("other.test", "/target", true, 1)
	check("mixed.allowed.test", "/target", false, 0)
	save("denylist", []independentModeRule{rule("127.0.0.2")})
	check("other.test", "/target", true, 1)
	check("mixed.allowed.test", "/target", false, 0)
	save("denylist", wildcard)
	check("a.b.allowed.test", "/target", false, 0)
	check("allowed.test", "/target", true, 1)
	// A new redirect target must satisfy the same current rule set independently.
	save("allowlist", []independentModeRule{rule("start.allowed.test")})
	check("start.allowed.test", "/redirect", false, 1)
	// Establish a real connection, then change only mode before first Write.
	// The identical rules must neither conceal a digest conflict nor keep an old
	// allow decision alive on that established socket.
	allowed := []independentModeRule{rule("127.0.0.1")}
	originalKey, originalBody := save("allowlist", allowed)
	changed := map[string]any{"expected_version": originalBody["expected_version"], "rule_mode": "denylist", "rules": allowed}
	browser.call(t, "PUT", path, originalKey, changed, nil, 409)
	if read().Version != current.Version {
		t.Fatal("mode-only idempotency conflict mutated policy")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce, enteredOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	traceCtx, traceCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer traceCancel()
	traceCtx = httptrace.WithClientTrace(traceCtx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) {
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-release:
		case <-traceCtx.Done():
		}
	}})
	type invocation struct {
		decision outbound.Decision
		err      error
	}
	done := make(chan invocation, 1)
	joined := false
	t.Cleanup(func() {
		if joined {
			return
		}
		traceCancel()
		unblock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("independent pre-write invocation did not join during cleanup")
		}
	})
	before := requests.Load()
	go func() { decision, err := invoke(traceCtx, "127.0.0.1", "/target"); done <- invocation{decision, err} }()
	select {
	case <-entered:
	case <-traceCtx.Done():
		t.Fatal("owned connection did not reach pre-write barrier")
	}
	save("denylist", allowed)
	unblock()
	select {
	case result := <-done:
		joined = true
		if result.err == nil || result.decision.Sent || requests.Load() != before {
			t.Fatal("mode update was bypassed by an established connection")
		}
	case <-traceCtx.Done():
		t.Fatal("mode-revoked request did not actually return")
	}
	// A real PG read failure must close the published mirror. Restoring the table
	// alone cannot substitute for an explicit successful current-state reload.
	save("allowlist", allowed)
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dbCancel()
	if _, err := a.store.Exec(dbCtx, `ALTER TABLE agenteam_outbound.outbound_policy RENAME TO independent_unavailable_policy`); err != nil {
		t.Fatal(err)
	}
	unavailable := runtime.policy.Reload(dbCtx)
	if _, err := a.store.Exec(dbCtx, `ALTER TABLE agenteam_outbound.independent_unavailable_policy RENAME TO outbound_policy`); err != nil {
		t.Fatal(err)
	}
	if unavailable == nil || runtime.policy.Status().Available {
		t.Fatal("failed database read fell back to a permissive policy")
	}
	check("127.0.0.1", "/target", false, 0)
	if err := runtime.policy.Reload(dbCtx); err != nil {
		t.Fatal("restored policy did not reload", err)
	}
	check("127.0.0.1", "/target", true, 1)
	a.cancel()
	independentProxyAwait(t, a.done, "independent rule-mode root stop")
	t.Log("independent_rule_modes real_api=true editable_loopback_default=true plain_http=true original_host_wildcard=true mixed_dns=true mode_digest=true prewrite_mode_change=true redirect_recheck=true real_pg_read_failure_closed=true root_joined=true dns_wire=false")
}
