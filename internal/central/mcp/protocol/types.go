// Package protocol isolates the official MCP SDK and its untrusted wire data
// behind the platform's controlled outbound HTTP client. It grants no Project,
// Execution or Tool authority; the connection and operation owners supply it.
package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const (
	ModernRevision  = "2026-07-28"
	LegacyRevision  = "2025-11-25"
	MaxCatalogItems = 512
	MaxCatalogBytes = 8 << 20
	MaxContentBytes = 8 << 20
	MaxPages        = 64
)

type Authentication struct {
	Scheme     string `json:"scheme"`
	HeaderName string `json:"header_name,omitempty"`
}

func (a Authentication) Validate() error {
	switch a.Scheme {
	case "none", "bearer", "basic", "oauth":
		if a.HeaderName != "" {
			return failure(InvalidRequest, false)
		}
	case "custom_header":
		if len(a.HeaderName) == 0 || len(a.HeaderName) > 128 {
			return failure(InvalidRequest, false)
		}
		for _, c := range a.HeaderName {
			if c <= 32 || c >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}\t", c) {
				return failure(InvalidRequest, false)
			}
		}
		switch strings.ToLower(a.HeaderName) {
		case "authorization", "cookie", "host", "connection", "proxy-authorization", "proxy-connection", "keep-alive", "upgrade", "transfer-encoding", "content-length", "content-type", "accept", "accept-encoding", "user-agent", "trailer", "te", "expect", "mcp-session-id", "mcp-protocol-version", "last-event-id", "origin":
			return failure(InvalidRequest, false)
		}
	default:
		return failure(InvalidRequest, false)
	}
	return nil
}

// Access is supplied only by the trusted connection/operation owner after
// current authorization. Material ownership transfers to Open: every return
// path destroys it, or Close destroys it after the original SDK session joins.
type Access struct {
	Endpoint       string
	Authentication Authentication
	Material       sc.SecretMaterial
	Context        outbound.CallContext
}

type Capabilities struct{ Tools, Resources bool }
type Tool struct {
	Name                 string
	Description          string
	InputSchema          json.RawMessage
	OutputSchema         json.RawMessage
	ReadOnly, Idempotent bool
	Destructive          *bool
}
type Resource struct {
	URI, Name, Title, Description, MIMEType string
	Size                                    *int64
}
type Template struct{ URITemplate, Name, Title, Description, MIMEType string }
type ResourceContent struct {
	URI, MIMEType string
	Text          *string
	Blob          []byte
}
type Content struct {
	Kind, Text, MIMEType, URI, Name string
	Data                            []byte
	Resource                        *ResourceContent
}
type ToolResult struct {
	Content           []Content
	StructuredContent json.RawMessage
	IsError           bool
}

type Code string

const (
	InvalidRequest         Code = "mcp_invalid_request"
	InvalidResponse        Code = "mcp_invalid_response"
	TransportFailure       Code = "mcp_transport_error"
	AuthenticationRequired Code = "mcp_authentication_required"
	ProtocolUnsupported    Code = "mcp_protocol_version_unsupported"
	ProtocolFailure        Code = "mcp_protocol_error"
	CapabilityUnsupported  Code = "mcp_capability_unsupported"
	TooLarge               Code = "mcp_content_too_large"
	Timeout                Code = "mcp_timeout"
	Cancelled              Code = "mcp_cancelled"
	SensitiveResponse      Code = "mcp_sensitive_response"
)

// Error never wraps SDK errors: those can contain endpoint query strings,
// headers, server-controlled messages and reflected credentials. Delivery is a
// conservative observation, not a retry decision or proof of remote effects.
type Error struct {
	Code            Code
	MayHaveExecuted bool
}

func (e *Error) Error() string {
	if e == nil {
		return string(ProtocolFailure)
	}
	return string(e.Code)
}
func failure(c Code, sent bool) *Error      { return &Error{c, sent} }
func (e Error) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, string(e.Code)) }
func (e Error) LogValue() slog.Value        { return slog.StringValue(string(e.Code)) }
func (Access) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mcp_access") }
func (Access) MarshalJSON() ([]byte, error) { return []byte(`"mcp_access"`), nil }
func (Access) LogValue() slog.Value         { return slog.StringValue("mcp_access") }
func textValid(v string, n int) bool {
	return len(v) <= n && utf8.ValidString(v) && !strings.ContainsRune(v, 0)
}

func ValidRemoteName(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func ValidResourceURI(v string) bool { return len(v) > 0 && textValid(v, 8192) }
