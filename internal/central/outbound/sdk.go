package outbound

import (
	"errors"
	"net/http"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

// SDKClient is for a trusted composition-root adapter whose SDK requires the
// concrete standard-library client. Its exported fields must not be replaced;
// Go cannot make *http.Client immutable. Consumers get no dial/proxy/TLS options.
// net/http wraps failures in url.Error containing the SDK's original URL. The
// adapter MUST map errors with SafeNetworkError and map responses before they
// cross a logging/business boundary. Client.Do itself has safe native outputs.
func (c *Client) SDKClient(profile Profile) (*http.Client, error) {
	if profile.data == nil {
		return nil, invalid()
	}
	return &http.Client{Transport: &sdkTransport{client: c, profile: profile}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

type sdkTransport struct {
	client  *Client
	profile Profile
}

func (t *sdkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.client.Do(req.Context(), req, t.profile)
	if err != nil {
		return nil, err
	}
	d := response.data()
	out := *d.raw
	out.Body = d.body
	out.Header = out.Header.Clone()
	// The SDK receives its explicit body/headers. Do not additionally retain the
	// outbound URL, query, credentials or request body in Response.Request.
	out.Request, _ = http.NewRequest(req.Method, d.decision.Origin, nil)
	return &out, nil
}
func SafeNetworkError(err error) *NetworkError {
	var network *NetworkError
	if errors.As(err, &network) {
		d := network.Decision()
		return networkError(d, d.Reason, err)
	}
	return networkError(Decision{Consumer: ac.SystemConsumer}, ac.InternalError, err)
}
