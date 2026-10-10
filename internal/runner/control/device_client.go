package control

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

var ErrAuthentication = errors.New("runner identity was not accepted")

type deviceClient struct {
	origin    string
	http      *http.Client
	transport *http.Transport
	dialer    websocket.Dialer
}

func newDeviceClient(config identity.Configuration, roots *x509.CertPool) (*deviceClient, error) {
	if config.Validate() != nil {
		return nil, identity.ErrInvalid
	}
	origin, err := url.Parse(config.CentralURL)
	if err != nil || origin.Hostname() == "" {
		return nil, identity.ErrInvalid
	}
	var clonedRoots *x509.CertPool
	if roots != nil {
		clonedRoots = roots.Clone()
	}
	security := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: clonedRoots}
	nativeDialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: nil, DialContext: nativeDialer.DialContext, TLSClientConfig: security.Clone(), TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: p.MaxDeviceBodyBytes, DisableCompression: true, MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second}
	return &deviceClient{origin: config.CentralURL, transport: transport,
		http:   &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		dialer: websocket.Dialer{NetDialContext: nativeDialer.DialContext, TLSClientConfig: security.Clone(), HandshakeTimeout: 5 * time.Second, Subprotocols: []string{p.Subprotocol}, EnableCompression: false}}, nil
}
func (c *deviceClient) close() { c.transport.CloseIdleConnections() }

// exchange returns only a complete, bounded HTTP success body. It never exposes
// a net/url error (which can contain credentials or private connection details),
// follows a redirect, or turns a partial/Unknown enrollment into known failure.
func (c *deviceClient) exchange(ctx context.Context, endpoint string, raw []byte) ([]byte, error) {
	if endpoint != "/api/v1/runner/challenge" && endpoint != "/api/v1/runner/enroll" || len(raw) > p.MaxDeviceBodyBytes {
		return nil, ErrProtocol
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, http.MethodPost, c.origin+endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, ErrTransport
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, ErrTransport
	}
	closed := false
	defer func() {
		if !closed {
			_ = response.Body.Close()
		}
	}()
	if response.StatusCode == http.StatusUnauthorized {
		return nil, ErrAuthentication
	}
	if response.StatusCode != http.StatusOK {
		return nil, ErrTransport
	}
	if response.ContentLength > p.MaxDeviceBodyBytes || hasHTTPHeader(response.Header, "Content-Encoding") || len(response.Header.Values("Content-Type")) != 1 {
		return nil, ErrProtocol
	}
	media, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		return nil, ErrProtocol
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, p.MaxDeviceBodyBytes+1))
	closeErr := response.Body.Close()
	closed = true
	if err != nil || closeErr != nil || bounded.Err() != nil {
		clear(body)
		return nil, ErrTransport
	}
	if len(body) > p.MaxDeviceBodyBytes {
		clear(body)
		return nil, ErrProtocol
	}
	if response.ContentLength >= 0 && response.ContentLength != int64(len(body)) {
		clear(body)
		return nil, ErrTransport
	}
	return body, nil
}
func hasHTTPHeader(header http.Header, name string) bool {
	for key := range header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}
func (c *deviceClient) challenge(ctx context.Context, runner p.ID) (p.ChallengeResponse, error) {
	request, err := p.NewChallengeRequest(runner)
	if err != nil {
		return p.ChallengeResponse{}, ErrProtocol
	}
	raw, err := p.EncodeChallengeRequest(request)
	if err != nil {
		return p.ChallengeResponse{}, ErrProtocol
	}
	defer clear(raw)
	body, err := c.exchange(ctx, "/api/v1/runner/challenge", raw)
	if err != nil {
		return p.ChallengeResponse{}, err
	}
	defer clear(body)
	response, err := p.DecodeChallengeResponse(body)
	if err != nil {
		return p.ChallengeResponse{}, ErrProtocol
	}
	return response, nil
}
func (c *deviceClient) enroll(ctx context.Context, id identity.Identity, token p.EnrollmentToken, os, arch string) (p.EnrollmentResponse, error) {
	public, err := id.PublicKey()
	if err != nil {
		return p.EnrollmentResponse{}, identity.ErrInvalid
	}
	config := id.Configuration()
	if config.CentralURL != c.origin {
		return p.EnrollmentResponse{}, identity.ErrInvalid
	}
	request, err := p.NewEnrollmentRequest(config.RunnerID, token, public, config.RootPath, os, arch)
	if err != nil {
		return p.EnrollmentResponse{}, ErrProtocol
	}
	raw, err := p.EncodeEnrollmentRequest(request)
	if err != nil {
		return p.EnrollmentResponse{}, ErrProtocol
	}
	defer clear(raw)
	body, err := c.exchange(ctx, "/api/v1/runner/enroll", raw)
	if err != nil {
		return p.EnrollmentResponse{}, err
	}
	defer clear(body)
	response, err := p.DecodeEnrollmentResponse(body)
	if err != nil || response.RunnerID() != config.RunnerID || response.PublicKeyFingerprint() != p.PublicKeyFingerprint(public) {
		return p.EnrollmentResponse{}, ErrProtocol
	}
	return response, nil
}
func (c *deviceClient) connect(ctx context.Context, id identity.Identity, nonce p.Nonce) (*wire, error) {
	if id.Validate() != nil || id.Configuration().CentralURL != c.origin {
		return nil, identity.ErrInvalid
	}
	auth, err := id.SignAuthentication(nonce, p.NewUnixSeconds(time.Now().Unix()))
	if err != nil {
		return nil, ErrAuthentication
	}
	header, err := p.EncodeAuthorization(auth)
	if err != nil {
		return nil, ErrProtocol
	}
	u, err := url.Parse(c.origin)
	if err != nil {
		return nil, identity.ErrInvalid
	}
	u.Scheme, u.Path = "wss", "/api/v1/runner/control"
	conn, response, err := c.dialer.DialContext(ctx, u.String(), http.Header{"Authorization": {header}})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return nil, ErrAuthentication
		}
		return nil, ErrTransport
	}
	if response == nil || hasHTTPHeader(response.Header, "Sec-WebSocket-Extensions") {
		_ = (nativeSocket{conn}).close()
		return nil, ErrProtocol
	}
	return newWire(conn)
}
