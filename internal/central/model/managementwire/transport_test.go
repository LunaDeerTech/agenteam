package managementwire

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Only the policy storage projection is controlled here. HTTP, DNS fact
// approval, rule evaluation, credential binding and socket lifecycle are real.
type wireStore struct {
	outbound.Store
	t     *testing.T
	rules []byte
}

func (s *wireStore) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if err := fn(ctx, f.NewTx()); err != nil {
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}
func (s *wireStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *wireStore) InTx(f.Tx) (postgres.SQLExecutor, error)                 { return s, nil }

type wireRow func(...any) error

func (r wireRow) Scan(values ...any) error { return r(values...) }
func (s *wireStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	return wireRow(func(out ...any) error {
		if query != "SELECT version,rule_mode,rules FROM agenteam_outbound.outbound_policy WHERE singleton=true" {
			s.t.Fatal("unexpected policy fixture query")
		}
		*out[0].(*int64) = 1
		*out[1].(*string) = "allowlist"
		*out[2].(*[]byte) = append([]byte(nil), s.rules...)
		return nil
	})
}

type wireAudit struct{}

func (wireAudit) AppendInTx(context.Context, f.Tx, ac.Entry, ac.AppendKey) (ac.AppendReceipt, error) {
	return ac.AppendReceipt{}, nil
}

type wireResolver struct{}

func (wireResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if host != "fixture.test" {
		return nil, &net.DNSError{Err: "not owned", IsNotFound: true}
	}
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func wireFixture(t *testing.T, server *httptest.Server, allowed bool) (Transport, outbound.CallContext, string) {
	t.Helper()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	raw := []byte(`[]`)
	if allowed {
		raw, _ = json.Marshal([]any{map[string]any{"target": "127.0.0.1/32", "ports": []int{number}}})
	}
	policy, err := outbound.NewPolicyService(&wireStore{t: t, rules: raw}, wireAudit{}, outbound.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	trust, err := outbound.LoadTrustStore("")
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.NewID[id.User]()
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.NewID[id.Session]()
	if err != nil {
		t.Fatal(err)
	}
	actor, err := id.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, user.String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	call, err := outbound.NewCallContext(actor, id.SystemScope(), key, ac.Associations{})
	if err != nil {
		t.Fatal(err)
	}
	scheme := "http"
	if server.TLS != nil {
		scheme = "https"
	}
	return Transport{Policy: policy, Trust: trust, Resolver: wireResolver{}}, call, scheme + "://fixture.test:" + port + "/v1%2Fowned"
}

func TestRunUsesRealControlledHTTPAndBoundCredentials(t *testing.T) {
	secret := "owned-wire-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	material, err := sc.NewSecretMaterial([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("exact-origin credential missing or proxy credential leaked")
		}
		switch r.URL.EscapedPath() {
		case "/v1%2Fowned/models":
			if r.Method != "GET" {
				t.Error("discovery method changed")
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"unknown-capability-model"}]}`)
		case "/v1%2Fowned/chat/completions":
			if r.Method != "POST" {
				t.Error("chat method changed")
			}
			_, _ = io.WriteString(w, validChat)
		case "/v1%2Fowned/embeddings":
			if r.Method != "POST" {
				t.Error("embedding method changed")
			}
			_, _ = io.WriteString(w, validEmbedding)
		default:
			t.Error("saved escaped prefix changed")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	transport, call, endpoint := wireFixture(t, server, true)
	for _, request := range []Request{{Action: "discovery", Protocol: chatProtocol, Endpoint: endpoint}, {Action: "test", Protocol: chatProtocol, Endpoint: endpoint, Model: "chat"}, {Action: "test", Protocol: embedProtocol, Endpoint: endpoint, Model: "embedding"}} {
		result, err := transport.Run(context.Background(), request, &material, call)
		if err != nil || result.Reason != "" {
			t.Fatal("controlled operation failed", result.Reason, err)
		}
		if request.Action == "discovery" && (len(result.Items) != 1 || result.Items[0] != "unknown-capability-model") {
			t.Fatal("discovery lost opaque model identifier")
		}
		if request.Action == "test" && len(result.Items) != 0 {
			t.Fatal("test exposed vendor response")
		}
	}
	if requests.Load() != 3 {
		t.Fatal("management operation repeated an upstream request")
	}
}

func TestRunControlledFailuresNeverReturnVendorMaterial(t *testing.T) {
	secret := "owned-material-response-marker"
	material, err := sc.NewSecretMaterial([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer other.Close()
	for _, test := range []struct {
		name, reason string
		allowed      bool
		respond      func(http.ResponseWriter, *http.Request)
	}{
		{"denied", "policy_rejected", false, func(w http.ResponseWriter, r *http.Request) { t.Error("denied target was contacted") }},
		{"redirect", "upstream_rejected", true, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/?token="+secret, 302) }},
		{"upstream", "upstream_rejected", true, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); _, _ = io.WriteString(w, secret) }},
		{"echo", "invalid_response", true, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"data":[{"id":"model-`+secret+`"}]}`)
		}},
		{"body-limit", "response_limit", true, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat(" ", discoveryLimit+1))
		}},
		{"header-limit", "response_limit", true, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Owned", strings.Repeat("x", 33<<10))
			_, _ = io.WriteString(w, `{"data":[]}`)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(test.respond))
			defer server.Close()
			transport, call, endpoint := wireFixture(t, server, test.allowed)
			result, err := transport.Run(context.Background(), Request{Action: "discovery", Protocol: chatProtocol, Endpoint: endpoint}, &material, call)
			if err != nil || result.Reason != test.reason || len(result.Items) != 0 {
				t.Fatal("safe failure mismatch", result.Reason, err)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), secret) {
				t.Fatal("vendor material escaped result")
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("redirect followed to a second owned origin")
	}
}

func TestRunCancellationWaitsForControlledSocketClosure(t *testing.T) {
	entered, closed := make(chan struct{}), make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			close(closed)
		}
	}
	server.Start()
	defer server.Close()
	transport, call, endpoint := wireFixture(t, server, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	var result Result
	var err error
	go func() {
		result, err = transport.Run(ctx, Request{Action: "discovery", Protocol: chatProtocol, Endpoint: endpoint}, nil, call)
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not reach owned response")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled wire call did not join")
	}
	if err != nil || result.Reason != "cancelled" {
		t.Fatal("cancel reason changed", result.Reason, err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("wire call left its socket open")
	}
}

func TestRunUntrustedTLSAndUnavailableCredentialFailClosed(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer server.Close()
	transport, call, endpoint := wireFixture(t, server, true)
	request := Request{Action: "discovery", Protocol: chatProtocol, Endpoint: endpoint}
	result, err := transport.Run(context.Background(), request, nil, call)
	if err != nil || result.Reason != "tls_failed" || requests.Load() != 0 {
		t.Fatal("untrusted fixture certificate accepted", result.Reason, err)
	}
	material, _ := sc.NewSecretMaterial([]byte("owned-destroyed"))
	material.Destroy()
	if _, err = transport.Run(context.Background(), request, &material, call); err == nil {
		t.Fatal("destroyed credential downgraded to anonymous")
	}
}
