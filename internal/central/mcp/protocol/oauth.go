package protocol

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// OAuthMetadata is the bounded, validated result of official SDK discovery.
// It carries no credentials and grants no connection or network authority.
type OAuthMetadata struct {
	Resource, Issuer, AuthorizationEndpoint, TokenEndpoint, RegistrationEndpoint string
	Scopes                                                                       []string
	TokenAuthMethod                                                              string
	RequireIssuerParameter                                                       bool
}

func oauthURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 16384 {
		return nil, failure(InvalidRequest, false)
	}
	target, err := outbound.ParseTarget(raw)
	if err != nil || target.Origin().Scheme() != "https" {
		return nil, failure(InvalidRequest, false)
	}
	// OAuth resource/issuer identifiers use exact string comparison. Validate
	// with outbound's parser but preserve the original default-port and path
	// spelling; its network client still canonicalizes the actual connection.
	u, err := url.Parse(raw)
	if err != nil {
		return nil, failure(InvalidRequest, false)
	}
	if u.User != nil || u.Fragment != "" {
		return nil, failure(InvalidRequest, false)
	}
	return u, nil
}

func (r *Runtime) oauthClient(call outbound.CallContext, credentials outbound.CredentialBinding) (*http.Client, error) {
	if r == nil || r.client == nil {
		return nil, failure(InvalidRequest, false)
	}
	profile, err := outbound.NewProfile(outbound.ProfileOptions{Consumer: ac.MCP, Context: call, Credentials: credentials, Limits: outbound.Limits{Overall: 60 * time.Second, ReadIdle: 15 * time.Second, RequestBodyBytes: 65536, ResponseBodyBytes: 1 << 20}})
	if err != nil {
		return nil, failure(InvalidRequest, false)
	}
	c, err := r.client.SDKClient(profile)
	if err != nil {
		return nil, failure(InvalidRequest, false)
	}
	return c, nil
}

// DiscoverOAuth always uses the original owner's controlled client, including
// all SDK well-known requests. No SDK default resolver, transport or TLS path
// is used. The connection owner must recheck its current revision after I/O.
func (r *Runtime) DiscoverOAuth(ctx context.Context, endpoint string, call outbound.CallContext) (OAuthMetadata, error) {
	u, err := oauthURL(endpoint)
	if err != nil {
		return OAuthMetadata{}, err
	}
	c, err := r.oauthClient(call, outbound.CredentialBinding{})
	if err != nil {
		return OAuthMetadata{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return OAuthMetadata{}, failure(InvalidRequest, false)
	}
	response, err := c.Do(request)
	if err != nil {
		return OAuthMetadata{}, classify(err, false)
	}
	headers := response.Header.Values("WWW-Authenticate")
	closeErr := response.Body.Close()
	if closeErr != nil {
		return OAuthMetadata{}, classify(closeErr, false)
	}
	var headerBytes int
	for _, header := range headers {
		headerBytes += len(header)
	}
	if headerBytes > 8192 {
		return OAuthMetadata{}, failure(TooLarge, false)
	}
	challenges, err := oauthex.ParseWWWAuthenticate(headers)
	if err != nil {
		return OAuthMetadata{}, failure(InvalidResponse, false)
	}
	type candidate struct{ metadata, resource string }
	var candidates []candidate
	for _, challenge := range challenges {
		if challenge.Scheme == "bearer" && challenge.Params["resource_metadata"] != "" {
			metadata := challenge.Params["resource_metadata"]
			if _, err := oauthURL(metadata); err != nil {
				return OAuthMetadata{}, err
			}
			candidates = append(candidates, candidate{metadata, u.String()})
			break
		}
	}
	origin := *u
	origin.Path, origin.RawPath, origin.RawQuery, origin.ForceQuery = "", "", "", false
	path := strings.TrimPrefix(u.EscapedPath(), "/")
	if path != "" {
		candidates = append(candidates, candidate{origin.String() + "/.well-known/oauth-protected-resource/" + path, u.String()})
	}
	candidates = append(candidates, candidate{origin.String() + "/.well-known/oauth-protected-resource", origin.String()})
	var resource *oauthex.ProtectedResourceMetadata
	for _, candidate := range candidates {
		resource, err = oauthex.GetProtectedResourceMetadata(ctx, candidate.metadata, candidate.resource, c)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return OAuthMetadata{}, classify(ctx.Err(), false)
		}
	}
	if err != nil || resource == nil || len(resource.AuthorizationServers) == 0 || len(resource.AuthorizationServers) > 8 || resource.DPOPBoundAccessTokensRequired || resource.TLSClientCertificateBoundAccessTokens || len(resource.BearerMethodsSupported) > 0 && !slices.Contains(resource.BearerMethodsSupported, "header") {
		return OAuthMetadata{}, failure(CapabilityUnsupported, false)
	}
	issuer := resource.AuthorizationServers[0]
	urls, err := oauthIssuerMetadataURLs(issuer)
	if err != nil {
		return OAuthMetadata{}, err
	}
	var server *oauthex.AuthServerMeta
	for _, metadata := range urls {
		server, err = oauthex.GetAuthServerMeta(ctx, metadata, issuer, c)
		if err != nil {
			return OAuthMetadata{}, failure(InvalidResponse, false)
		}
		if server != nil {
			break
		}
	}
	return normalizeOAuthMetadata(resource, server, issuer)
}

func oauthIssuerMetadataURLs(issuer string) ([]string, error) {
	u, err := oauthURL(issuer)
	if err != nil || u.RawQuery != "" || u.ForceQuery {
		return nil, failure(InvalidResponse, false)
	}
	origin := *u
	origin.Path, origin.RawPath = "", ""
	path := strings.Trim(u.EscapedPath(), "/")
	if path == "" {
		return []string{origin.String() + "/.well-known/oauth-authorization-server", origin.String() + "/.well-known/openid-configuration"}, nil
	}
	return []string{origin.String() + "/.well-known/oauth-authorization-server/" + path, origin.String() + "/.well-known/openid-configuration/" + path, origin.String() + "/" + path + "/.well-known/openid-configuration"}, nil
}

func normalizeOAuthMetadata(resource *oauthex.ProtectedResourceMetadata, server *oauthex.AuthServerMeta, issuer string) (OAuthMetadata, error) {
	if resource == nil || server == nil || server.Issuer != issuer || !slices.Contains(server.CodeChallengeMethodsSupported, "S256") || !slices.Contains(server.ResponseTypesSupported, "code") || len(server.GrantTypesSupported) > 0 && !slices.Contains(server.GrantTypesSupported, "authorization_code") {
		return OAuthMetadata{}, failure(CapabilityUnsupported, false)
	}
	m := OAuthMetadata{Resource: resource.Resource, Issuer: issuer, AuthorizationEndpoint: server.AuthorizationEndpoint, TokenEndpoint: server.TokenEndpoint, RegistrationEndpoint: server.RegistrationEndpoint, Scopes: slices.Clone(resource.ScopesSupported), RequireIssuerParameter: server.AuthorizationResponseIssParameterSupported}
	for _, raw := range []string{m.Resource, m.Issuer, m.AuthorizationEndpoint, m.TokenEndpoint} {
		if _, err := oauthURL(raw); err != nil {
			return OAuthMetadata{}, failure(InvalidResponse, false)
		}
	}
	if m.RegistrationEndpoint != "" {
		if _, err := oauthURL(m.RegistrationEndpoint); err != nil {
			return OAuthMetadata{}, failure(InvalidResponse, false)
		}
	}
	if len(m.Scopes) > 64 {
		return OAuthMetadata{}, failure(TooLarge, false)
	}
	for _, scope := range m.Scopes {
		if len(scope) == 0 || len(scope) > 256 {
			return OAuthMetadata{}, failure(InvalidResponse, false)
		}
		for _, ch := range scope {
			if ch < 0x21 || ch > 0x7e || ch == '"' || ch == '\\' {
				return OAuthMetadata{}, failure(InvalidResponse, false)
			}
		}
	}
	methods := server.TokenEndpointAuthMethodsSupported
	if len(methods) == 0 {
		methods = []string{"client_secret_basic"}
	}
	for _, method := range []string{"none", "client_secret_post", "client_secret_basic"} {
		if slices.Contains(methods, method) {
			m.TokenAuthMethod = method
			break
		}
	}
	if m.TokenAuthMethod == "" {
		return OAuthMetadata{}, failure(CapabilityUnsupported, false)
	}
	return m, nil
}

// oauthReadBody joins the original controlled response before decoding or
// returning secret material; status bodies and SDK errors are never exposed.
func oauthReadBody(response *http.Response) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, failure(InvalidResponse, true)
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	closed := response.Body.Close()
	if err != nil || closed != nil || len(b) > 65536 {
		clear(b)
		return nil, failure(InvalidResponse, true)
	}
	return b, nil
}
