package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAuthenticationHeaderBoundary(t *testing.T) {
	for _, name := range []string{"Host", "Authorization", "Cookie", "MCP-Session-Id", "Mcp-Protocol-Version", "Content-Length", "X-Key\r\nOther"} {
		if (Authentication{Scheme: "custom_header", HeaderName: name}).Validate() == nil {
			t.Errorf("reserved header accepted: %q", name)
		}
	}
	for _, scheme := range []string{"none", "bearer", "basic", "oauth"} {
		if (Authentication{Scheme: scheme}).Validate() != nil {
			t.Errorf("scheme rejected: %s", scheme)
		}
		if (Authentication{Scheme: scheme, HeaderName: "X-Key"}).Validate() == nil {
			t.Errorf("irrelevant header accepted: %s", scheme)
		}
	}
	if (Authentication{Scheme: "custom_header", HeaderName: "X-API-Key"}).Validate() != nil {
		t.Fatal("explicit credential header rejected")
	}
}

func TestFailedOpenDestroysCredential(t *testing.T) {
	material, _ := sc.NewSecretMaterial([]byte("owned-mcp-private-credential"))
	var r *Runtime
	if _, err := r.Open(context.Background(), Access{Authentication: Authentication{Scheme: "bearer"}, Material: material}); err == nil {
		t.Fatal("unbound runtime accepted")
	}
	if err := material.Use(func([]byte) error { return nil }); err == nil {
		t.Fatal("failed open retained credential")
	}
}

func TestSafeErrorsDoNotRetainSDKCause(t *testing.T) {
	raw := &url.Error{Op: "Post", URL: "https://server.invalid/private?token=credential-sentinel", Err: errors.New("server raw private response")}
	err := classify(raw, true)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if strings.Contains(fmt.Sprintf(verb, err), "sentinel") {
			t.Fatal("SDK error escaped")
		}
	}
	b, _ := json.Marshal(err)
	if strings.Contains(string(b), "private") || errors.Is(err, raw) {
		t.Fatal("unsafe cause retained")
	}
	var outcome *Error
	if !errors.As(err, &outcome) || !outcome.MayHaveExecuted {
		t.Fatal("unknown outcome misreported as not sent")
	}
	if classify(context.Canceled, true).(*Error).Code != Cancelled {
		t.Fatal("cancel classification lost")
	}
}

func TestCompletePaginationRejectsRepeatsAndOversize(t *testing.T) {
	var p pages
	if err := p.add("next", []string{"one"}, 1); err != nil {
		t.Fatal(err)
	}
	if err := p.add("next", []string{"two"}, 1); err == nil {
		t.Fatal("cursor cycle accepted")
	}
	p = pages{}
	if err := p.add("", nil, MaxCatalogItems+1); err == nil {
		t.Fatal("unbounded catalog accepted")
	}
}

func TestCredentialReflectionAndResourceMapping(t *testing.T) {
	m, _ := sc.NewSecretMaterial([]byte("private-mcp-reflection-token"))
	defer m.Destroy()
	s := &Session{material: m}
	if err := s.safe(map[string]string{"text": "echo private-mcp-reflection-token"}); err == nil {
		t.Fatal("credential reflection projected")
	}
	if err := s.safe(map[string]string{"text": "ordinary response"}); err != nil {
		t.Fatal(err)
	}
	v, err := mapResource(&sdk.ResourceContents{URI: "file:///not-a-local-read", Text: "payload"})
	if err != nil || v.Text == nil || *v.Text != "payload" {
		t.Fatal("resource URI was treated as an outbound HTTP target")
	}
	if _, err := mapResource(&sdk.ResourceContents{URI: "repo://a", Text: "text", Blob: []byte("blob")}); err == nil {
		t.Fatal("ambiguous content accepted")
	}
	if _, err := mapContent(&sdk.ToolUseContent{}); err == nil {
		t.Fatal("sampling-only content accepted")
	}
}
