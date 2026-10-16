package outboundhttp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type proxySpy struct {
	policySpy
	putProxy func(context.Context, identity.Actor, outbound.CommandMeta, outbound.ProxyUpdate) (outbound.ProxyUpdateResult, error)
}

func (s proxySpy) GetProxy(context.Context, identity.Actor) (outbound.ProxySettings, error) {
	return outbound.ProxySettings{Version: 1, Mode: "direct"}, nil
}
func (s proxySpy) UpdateProxy(ctx context.Context, a identity.Actor, m outbound.CommandMeta, p outbound.ProxyUpdate) (outbound.ProxyUpdateResult, error) {
	return s.putProxy(ctx, a, m, p)
}
func TestProxyHTTPStrictIntentAndMaterialLifetime(t *testing.T) {
	actor := testActor(t)
	var borrowed *sc.SecretMaterial
	auditID, e := foundation.NewID[ac.Record]()
	if e != nil {
		t.Fatal(e)
	}
	at, e := foundation.NewInstant(time.Now())
	if e != nil {
		t.Fatal(e)
	}
	h := systemHTTP{policy: proxySpy{putProxy: func(_ context.Context, a identity.Actor, m outbound.CommandMeta, p outbound.ProxyUpdate) (outbound.ProxyUpdateResult, error) {
		if !a.Equal(actor) || m.Identity.Namespace() != "outbound-proxy" || m.ExpectedVersion != 1 || p.Password == nil || p.Gateway == nil || p.Gateway.Host != "proxy" {
			t.Fatal("proxy intent identity changed")
		}
		borrowed = p.Password
		if e := p.Password.Use(func(b []byte) error {
			if string(b) != "private-fixture-password" {
				t.Fatal("secret changed")
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		return outbound.ProxyUpdateResult{Version: 2, AuditID: auditID, CreatedAt: at}, nil
	}}}
	raw := `{"expected_version":"1","mode":"proxy","gateway":{"protocol":"https","host":"proxy","port":8443,"username":"fixture"},"credential_action":"keep","password":"private-fixture-password"}`
	body, e := h.proxyCall(httptest.NewRecorder(), updateRequest(raw), actor)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(body), "private-fixture") || strings.Contains(string(body), "credential") || strings.Contains(string(body), "gateway") {
		t.Fatal("receipt disclosed material/configuration")
	}
	var receipt map[string]json.RawMessage
	if json.Unmarshal(body, &receipt) != nil || len(receipt) != 3 {
		t.Fatal("receipt shape")
	}
	if borrowed == nil || borrowed.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("HTTP material was not destroyed after synchronous service return")
	}
}
func TestProxyHTTPRejectsMalformedAndOversizedBeforeService(t *testing.T) {
	actor := testActor(t)
	h := systemHTTP{policy: proxySpy{putProxy: func(context.Context, identity.Actor, outbound.CommandMeta, outbound.ProxyUpdate) (outbound.ProxyUpdateResult, error) {
		t.Fatal("invalid wire reached service")
		return outbound.ProxyUpdateResult{}, nil
	}}}
	for _, raw := range []string{
		`{}`, `{"expected_version":"1","mode":"direct","credential_action":"remove"}`,
		`{"expected_version":"1","mode":"direct","gateway":null,"credential_action":"remove","password":null}`,
		`{"expected_version":"1","mode":"direct","gateway":null,"credential_action":"remove","password":""}`,
		`{"expected_version":"1","mode":"direct","gateway":{"protocol":"http","host":"proxy","port":8080},"credential_action":"keep"}`,
		`{"expected_version":"1","mode":"direct","gateway":{"protocol":"http","host":"proxy","port":8080,"username":"","credential_present":true},"credential_action":"keep"}`,
		`{"expected_version":"1","mode":"direct","gateway":null,"credential_action":"remove","password":"x","password":"x"}`,
	} {
		_, e := h.proxyCall(httptest.NewRecorder(), updateRequest(raw), actor)
		requireCode(t, e, foundation.InvalidArgument)
	}
	_, e := h.proxyCall(httptest.NewRecorder(), updateRequest(`{"expected_version":"1","mode":"direct","gateway":null,"credential_action":"remove"}`+strings.Repeat(" ", 16<<10)), actor)
	requireCode(t, e, foundation.PayloadTooLarge)
}
