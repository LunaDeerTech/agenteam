package control

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type deviceRoundTrip func(*http.Request) (*http.Response, error)

func (f deviceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type deviceBody struct {
	io.Reader
	closes   atomic.Int32
	closeErr error
}

func (b *deviceBody) Close() error { b.closes.Add(1); return b.closeErr }
func deviceIdentity(t *testing.T) identity.Identity {
	t.Helper()
	id, err := identity.NewPending(identity.Configuration{CentralURL: "https://runner.example", RunnerID: sessionID(t), RootPath: "/private/runner"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func deviceForTest(t *testing.T, id identity.Identity, round deviceRoundTrip) *deviceClient {
	t.Helper()
	c, err := newDeviceClient(id.Configuration(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)
	c.http.Transport = round
	return c
}
func responseWithBody(body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: body, ContentLength: -1}
}
func TestDeviceClientEnrollmentBindsOriginalIdentity(t *testing.T) {
	id := deviceIdentity(t)
	token, _ := p.NewEnrollmentToken()
	public, _ := id.PublicKey()
	at, _ := p.NewInstant(time.Now())
	receipt, _ := p.NewEnrollmentResponse(id.Configuration().RunnerID, "2", "1", at, p.PublicKeyFingerprint(public))
	encoded, _ := p.EncodeEnrollmentResponse(receipt)
	var requestBody []byte
	var calls int
	body := &deviceBody{Reader: bytes.NewReader(encoded)}
	c := deviceForTest(t, id, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://runner.example/api/v1/runner/enroll" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Origin") != "" {
			t.Error("device request crossed its fixed HTTP boundary")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Error("request has no bounded original deadline")
		}
		requestBody, _ = io.ReadAll(r.Body)
		return responseWithBody(body), nil
	})
	got, err := c.enroll(context.Background(), id, token, "linux", "amd64")
	if err != nil || got != receipt || calls != 1 || body.closes.Load() != 1 {
		t.Fatal("known complete enrollment failed or close repeated", err, calls, body.closes.Load())
	}
	decoded, err := p.DecodeEnrollmentRequest(requestBody)
	if err != nil || decoded.RunnerID() != id.Configuration().RunnerID || decoded.PublicKey() != public || decoded.Token() != token || decoded.RootPath() != id.Configuration().RootPath {
		t.Fatal("original enrollment identity changed", err)
	}
	for _, kind := range []string{"runner", "key", "extra", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			raw := append([]byte(nil), encoded...)
			switch kind {
			case "runner":
				raw = bytes.Replace(raw, []byte(id.Configuration().RunnerID), []byte(sessionID(t)), 1)
			case "key":
				raw = bytes.Replace(raw, []byte(p.PublicKeyFingerprint(public)), []byte("sha256:"+strings.Repeat("0", 64)), 1)
			case "extra":
				raw = append(raw[:len(raw)-1], []byte(`,"token":"CANARY"}`)...)
			case "truncated":
				raw = raw[:len(raw)-1]
			}
			b := &deviceBody{Reader: bytes.NewReader(raw)}
			d := deviceForTest(t, id, func(*http.Request) (*http.Response, error) { return responseWithBody(b), nil })
			if _, err := d.enroll(context.Background(), id, token, "linux", "amd64"); err != ErrProtocol || b.closes.Load() != 1 || id.State() != identity.Pending {
				t.Fatal("bad receipt activated or exposed original identity", err)
			}
		})
	}
}
func TestDeviceClientResponseBoundaries(t *testing.T) {
	id := deviceIdentity(t)
	for _, tc := range []struct {
		name   string
		mutate func(*http.Response, *deviceBody)
		want   error
	}{
		{"valid", func(*http.Response, *deviceBody) {}, nil},
		{"unauthorized", func(r *http.Response, _ *deviceBody) { r.StatusCode = 401 }, ErrAuthentication},
		{"unknown", func(r *http.Response, _ *deviceBody) { r.StatusCode = 503 }, ErrTransport},
		{"redirect", func(r *http.Response, _ *deviceBody) {
			r.StatusCode = 307
			r.Header.Set("Location", "https://foreign.example/private")
		}, ErrTransport},
		{"empty-encoding", func(r *http.Response, _ *deviceBody) { r.Header["content-encoding"] = []string{""} }, ErrProtocol},
		{"identity-encoding", func(r *http.Response, _ *deviceBody) { r.Header.Set("Content-Encoding", "identity") }, ErrProtocol},
		{"two-types", func(r *http.Response, _ *deviceBody) { r.Header.Add("Content-Type", "application/json") }, ErrProtocol},
		{"type-parameters", func(r *http.Response, _ *deviceBody) { r.Header.Set("Content-Type", "application/json; boundary=x") }, ErrProtocol},
		{"non-json", func(r *http.Response, _ *deviceBody) { r.Header.Set("Content-Type", "text/plain") }, ErrProtocol},
		{"declared-over", func(r *http.Response, _ *deviceBody) { r.ContentLength = p.MaxDeviceBodyBytes + 1 }, ErrProtocol},
		{"actual-over", func(_ *http.Response, b *deviceBody) {
			b.Reader = strings.NewReader(strings.Repeat("a", p.MaxDeviceBodyBytes+1))
		}, ErrProtocol},
		{"exact-limit", func(r *http.Response, b *deviceBody) {
			b.Reader = strings.NewReader(strings.Repeat("a", p.MaxDeviceBodyBytes))
			r.ContentLength = p.MaxDeviceBodyBytes
		}, nil},
		{"short-body", func(r *http.Response, _ *deviceBody) { r.ContentLength = 3 }, ErrTransport},
		{"unexpected-tail", func(r *http.Response, _ *deviceBody) { r.ContentLength = 1 }, ErrTransport},
		{"read-error", func(_ *http.Response, b *deviceBody) {
			b.Reader = io.MultiReader(strings.NewReader("{}"), errorReader{})
		}, ErrTransport},
		{"close-error", func(_ *http.Response, b *deviceBody) { b.closeErr = errors.New("PRIVATE_CLOSE_CANARY") }, ErrTransport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &deviceBody{Reader: strings.NewReader("{}")}
			response := responseWithBody(b)
			tc.mutate(response, b)
			calls := 0
			c := deviceForTest(t, id, func(*http.Request) (*http.Response, error) { calls++; return response, nil })
			raw, err := c.exchange(context.Background(), "/api/v1/runner/enroll", []byte("{}"))
			if err != tc.want || calls != 1 || b.closes.Load() != 1 {
				t.Fatal("HTTP boundary/once close failed", err, calls, b.closes.Load())
			}
			if err != nil && raw != nil {
				t.Fatal("failed device body escaped")
			}
		})
	}
	c := deviceForTest(t, id, func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "POST", URL: "PRIVATE_CANARY", Err: errors.New("SECRET_CANARY")}
	})
	if _, err := c.exchange(context.Background(), "/api/v1/runner/challenge", nil); err != ErrTransport {
		t.Fatal("raw transport error escaped", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("PRIVATE_READ_CANARY") }

type heldDeviceBody struct {
	read, finish, closed chan struct{}
	closes               atomic.Int32
}

func (b *heldDeviceBody) Read([]byte) (int, error) { close(b.read); <-b.finish; return 0, io.EOF }
func (b *heldDeviceBody) Close() error             { b.closes.Add(1); close(b.closed); return nil }
func TestDeviceClientCancellationWaitsForOriginalBody(t *testing.T) {
	id := deviceIdentity(t)
	b := &heldDeviceBody{read: make(chan struct{}), finish: make(chan struct{}), closed: make(chan struct{})}
	var original context.Context
	c := deviceForTest(t, id, func(r *http.Request) (*http.Response, error) { original = r.Context(); return responseWithBody(b), nil })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.exchange(ctx, "/api/v1/runner/enroll", nil); done <- err }()
	select {
	case <-b.read:
	case <-time.After(time.Second):
		t.Fatal("body was not entered")
	}
	expected, _ := ctx.Deadline()
	got, _ := original.Deadline()
	if got != expected {
		t.Fatal("earlier caller deadline was replaced")
	}
	cancel()
	select {
	case <-original.Done():
	case <-time.After(time.Second):
		t.Fatal("request did not inherit cancel")
	}
	select {
	case <-done:
		t.Fatal("cancellation fabricated original read return")
	default:
	}
	close(b.finish)
	select {
	case err := <-done:
		if err != ErrTransport {
			t.Fatal("late cancellation became success", err)
		}
	case <-time.After(time.Second):
		t.Fatal("original read did not join")
	}
	if b.closes.Load() != 1 {
		t.Fatal("body did not close exactly once")
	}
}
func TestDeviceClientNativeConfigurationAndChallenge(t *testing.T) {
	id := deviceIdentity(t)
	c, err := newDeviceClient(id.Configuration(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if c.transport.Proxy != nil || !c.transport.DisableCompression || c.transport.TLSClientConfig.InsecureSkipVerify || c.transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || c.dialer.EnableCompression || c.dialer.Proxy != nil || !reflect.DeepEqual(c.dialer.Subprotocols, []string{p.Subprotocol}) {
		t.Fatal("native security settings weakened")
	}
	if c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect permitted")
	}
	nonce, _ := p.NewNonce()
	expiry, _ := p.NewInstant(time.Now().Add(30 * time.Second))
	response, _ := p.NewChallengeResponse(nonce, expiry)
	wire, _ := p.EncodeChallengeResponse(response)
	c.http.Transport = deviceRoundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		request, err := p.DecodeChallengeRequest(raw)
		if err != nil || request.RunnerID() != id.Configuration().RunnerID || r.URL.Path != "/api/v1/runner/challenge" {
			t.Error("challenge intent changed")
		}
		return responseWithBody(&deviceBody{Reader: bytes.NewReader(wire)}), nil
	})
	if got, err := c.challenge(context.Background(), id.Configuration().RunnerID); err != nil || got != response {
		t.Fatal("challenge not bound to original response", err)
	}
}
