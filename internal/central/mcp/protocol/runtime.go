package protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Runtime struct{ client *outbound.Client }

func New(client *outbound.Client) (*Runtime, error) {
	if client == nil {
		return nil, failure(InvalidRequest, false)
	}
	return &Runtime{client}, nil
}

type Session struct {
	mu           sync.Mutex
	active       sync.WaitGroup
	sdk          *sdk.ClientSession
	material     sc.SecretMaterial
	revision     string
	capabilities Capabilities
	closed       bool
}

// Open uses the official SDK's discovery/legacy negotiation; all actual HTTP
// requests, including negotiation and DELETE during Close, use the same
// controlled client. There is no alternate transport, TLS or dial hook.
func (r *Runtime) Open(ctx context.Context, a Access) (_ *Session, err error) {
	owned := true
	defer func() {
		if owned {
			a.Material.Destroy()
		}
	}()
	if r == nil || r.client == nil || ctx == nil || a.Authentication.Validate() != nil {
		return nil, failure(InvalidRequest, false)
	}
	if err := ctx.Err(); err != nil {
		return nil, classify(err, false)
	}
	target, err := outbound.ParseTarget(a.Endpoint)
	if err != nil {
		return nil, failure(InvalidRequest, false)
	}
	var credentials outbound.CredentialBinding
	if a.Authentication.Scheme != "none" {
		header, prefix := "Authorization", "Bearer "
		switch a.Authentication.Scheme {
		case "basic":
			prefix = "Basic "
		case "custom_header":
			header, prefix = a.Authentication.HeaderName, ""
		}
		if err := a.Material.Use(func(raw []byte) error {
			if len(raw) == 0 {
				return failure(InvalidRequest, false)
			}
			return nil
		}); err != nil {
			return nil, failure(AuthenticationRequired, false)
		}
		field, e := outbound.HeaderCredential(header, prefix, a.Material)
		if e != nil {
			return nil, failure(InvalidRequest, false)
		}
		credentials, e = outbound.NewCredentialBinding(target.Origin(), field)
		if e != nil {
			return nil, failure(InvalidRequest, false)
		}
	}
	profile, err := outbound.NewProfile(outbound.ProfileOptions{Consumer: ac.MCP, AllowHTTP: target.Origin().Scheme() == "http", Context: a.Context, Credentials: credentials, Limits: outbound.Limits{Overall: 60 * time.Second, ReadIdle: 30 * time.Second, RequestBodyBytes: 1 << 20, ResponseBodyBytes: MaxContentBytes}})
	if err != nil {
		return nil, failure(InvalidRequest, false)
	}
	httpClient, err := r.client.SDKClient(profile)
	if err != nil {
		return nil, failure(InvalidRequest, false)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "agenteam", Version: "1"}, &sdk.ClientOptions{Capabilities: &sdk.ClientCapabilities{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	s, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: target.URL().String(), HTTPClient: httpClient, MaxRetries: -1, DisableStandaloneSSE: true, MaxEventSize: MaxContentBytes}, &sdk.ClientSessionOptions{ProtocolVersion: ModernRevision})
	if err != nil {
		return nil, classify(err, false)
	}
	init := s.InitializeResult()
	if init == nil || init.ProtocolVersion != ModernRevision && init.ProtocolVersion != LegacyRevision {
		_ = s.Close()
		_ = s.Wait()
		return nil, failure(ProtocolUnsupported, false)
	}
	var caps Capabilities
	if init.Capabilities != nil {
		caps = Capabilities{Tools: init.Capabilities.Tools != nil, Resources: init.Capabilities.Resources != nil}
	}
	owned = false
	return &Session{sdk: s, material: a.Material, revision: init.ProtocolVersion, capabilities: caps}, nil
}
func (s *Session) Revision() string {
	if s == nil {
		return ""
	}
	return s.revision
}
func (s *Session) Capabilities() Capabilities {
	if s == nil {
		return Capabilities{}
	}
	return s.capabilities
}
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.sdk.Close()
	joined := s.sdk.Wait()
	s.active.Wait()
	s.material.Destroy()
	if err != nil {
		return classify(err, false)
	}
	if joined != nil {
		return classify(joined, false)
	}
	return nil
}

func (s *Session) check(ctx context.Context) error {
	if ctx == nil || s == nil || s.sdk == nil {
		return failure(InvalidRequest, false)
	}
	if err := ctx.Err(); err != nil {
		return classify(err, false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return failure(Cancelled, false)
	}
	s.active.Add(1)
	return nil
}

func classify(err error, call bool) error {
	if err == nil {
		return nil
	}
	sent := call
	var n *outbound.NetworkError
	if errors.As(err, &n) {
		sent = call && n.Decision().Sent
	}
	if errors.Is(err, context.Canceled) {
		return failure(Cancelled, sent)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failure(Timeout, sent)
	}
	if n != nil {
		if n.Decision().Reason == ac.Timeout {
			return failure(Timeout, sent)
		}
		return failure(TransportFailure, sent)
	}
	var rpc *jsonrpc.Error
	if errors.As(err, &rpc) {
		if rpc.Code == sdk.CodeUnsupportedProtocolVersion {
			return failure(ProtocolUnsupported, false)
		}
		// A received JSON-RPC error is a known protocol failure. Invalid method
		// and arguments did not execute a tool; other server errors can follow
		// effects and remain conservative.
		if rpc.Code == -32601 || rpc.Code == -32602 {
			sent = false
		}
		return failure(ProtocolFailure, sent)
	}
	return failure(ProtocolFailure, sent)
}

func (s *Session) safe(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return failure(InvalidResponse, false)
	}
	defer clear(raw)
	if len(raw) > MaxContentBytes {
		return failure(TooLarge, false)
	}
	if s.sensitive(raw) {
		return failure(SensitiveResponse, false)
	}
	return nil
}

func (s *Session) sensitive(raw []byte) bool {
	var reflected bool
	// An absent credential is normal for unauthenticated connections. Exact
	// plaintext and common HTTP/JSON encodings are rejected before projection.
	_ = s.material.Use(func(secret []byte) error {
		variants := [][]byte{secret, []byte(base64.StdEncoding.EncodeToString(secret)), []byte(url.QueryEscape(string(secret))), []byte(url.PathEscape(string(secret)))}
		encoded, _ := json.Marshal(string(secret))
		if len(encoded) > 2 {
			variants = append(variants, encoded[1:len(encoded)-1])
		}
		for _, v := range variants {
			if len(v) > 0 && bytes.Contains(raw, v) {
				reflected = true
			}
		}
		return nil
	})
	return reflected
}
