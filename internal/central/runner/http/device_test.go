package runnerhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type pureDevice struct {
	enrolls, challenges int
	entered             chan context.Context
	release             chan struct{}
	wrong               bool
}

func (s *pureDevice) Enroll(ctx context.Context, in p.EnrollmentRequest) (p.EnrollmentResponse, error) {
	s.enrolls++
	if s.entered != nil {
		s.entered <- ctx
		<-s.release
	}
	at, _ := p.NewInstant(time.Now())
	target := in.RunnerID()
	if s.wrong {
		target, _ = p.NewID()
	}
	return p.NewEnrollmentResponse(target, "2", "1", at, p.PublicKeyFingerprint(in.PublicKey()))
}
func (s *pureDevice) Challenge(context.Context, p.ChallengeRequest) (p.ChallengeResponse, error) {
	s.challenges++
	nonce, _ := p.NewNonce()
	at, _ := p.NewInstant(time.Now().Add(30 * time.Second))
	return p.NewChallengeResponse(nonce, at)
}

func pureEnrollment(t *testing.T) (p.EnrollmentRequest, string) {
	t.Helper()
	target, _ := p.NewID()
	token, _ := p.NewEnrollmentToken()
	in, e := p.NewEnrollmentRequest(target, token, [32]byte{1}, "/srv", "linux", "amd64")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := p.EncodeEnrollmentRequest(in)
	if e != nil {
		t.Fatal(e)
	}
	return in, string(raw)
}

func pureDeviceCall(h *DeviceHandler, r *http.Request) ([]byte, error) {
	var body []byte
	var err error
	httpapi.WithRequestID(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { body, err = h.execute(w, r) })).ServeHTTP(httptest.NewRecorder(), r)
	return body, err
}

func TestRunnerDeviceHTTPPureStrictIdentityAndProjection(t *testing.T) {
	device := &pureDevice{}
	h := &DeviceHandler{devices: device, slots: make(chan struct{}, 128)}
	in, raw := pureEnrollment(t)
	r := httptest.NewRequest("POST", enrollPath, strings.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	body, e := pureDeviceCall(h, r)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := p.DecodeEnrollmentResponse(body)
	if e != nil || decoded.RunnerID() != in.RunnerID() || decoded.PublicKeyFingerprint() != p.PublicKeyFingerprint(in.PublicKey()) || device.enrolls != 1 {
		t.Fatal("wrong known postimage", e)
	}
	challenge, _ := p.NewChallengeRequest(in.RunnerID())
	b, _ := p.EncodeChallengeRequest(challenge)
	r = httptest.NewRequest("POST", challengePath, strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	body, e = pureDeviceCall(h, r)
	if out, decode := p.DecodeChallengeResponse(body); e != nil || decode != nil || !out.Nonce().Valid() || device.challenges != 1 {
		t.Fatal("invalid challenge wire", e, decode)
	}
	constructed, e := NewDeviceHandler(&service.Service{})
	if e != nil || cap(constructed.slots) != 128 {
		t.Fatal("authentication cap")
	}
}

func TestRunnerDeviceHTTPPureRejectsBrowserAndMalformedWire(t *testing.T) {
	_, raw := pureEnrollment(t)
	for _, mode := range []string{"Cookie", "Origin", "X-CSRF-Token", "Authorization", "Content-Encoding", "query", "force-query", "raw-path", "alias", "duplicate", "null", "actor", "too-large", "trailing", "surrogate", "utf8", "wrong-media", "wrong-charset", "duplicate-content-type"} {
		t.Run(mode, func(t *testing.T) {
			body := raw
			switch mode {
			case "alias":
				body = strings.Replace(raw, `"runner_id"`, `"Runner_ID"`, 1)
			case "duplicate":
				body = strings.TrimSuffix(raw, "}") + `,"o\u0073":"linux"}`
			case "null":
				body = strings.Replace(raw, `"linux"`, `null`, 1)
			case "actor":
				body = strings.TrimSuffix(raw, "}") + `,"actor":"admin"}`
			case "too-large":
				body += strings.Repeat(" ", p.MaxDeviceBodyBytes)
			case "trailing":
				body += `{}`
			case "surrogate":
				body = strings.Replace(raw, `"/srv"`, `"/\ud800"`, 1)
			case "utf8":
				body = strings.Replace(raw, "/srv", "/\xff", 1)
			}
			r := httptest.NewRequest("POST", enrollPath, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			switch mode {
			case "Cookie", "Origin", "X-CSRF-Token", "Authorization", "Content-Encoding":
				r.Header[strings.ToLower(mode)] = []string{""}
			case "query":
				r.URL.RawQuery = "token=private-canary"
			case "force-query":
				r.URL.ForceQuery = true
			case "raw-path":
				r.URL.RawPath = "/api/v1/runner/%65nroll"
			case "wrong-media":
				r.Header.Set("Content-Type", "text/plain")
			case "wrong-charset":
				r.Header.Set("Content-Type", "application/json; charset=latin1")
			case "duplicate-content-type":
				r.Header["content-type"] = []string{"application/json"}
			}
			device := &pureDevice{}
			h := &DeviceHandler{devices: device, slots: make(chan struct{}, 128)}
			if _, e := pureDeviceCall(h, r); e == nil || device.enrolls != 0 || len(h.slots) != 0 {
				t.Fatal("bad wire reached device service or leaked slot")
			}
		})
	}
}

func TestRunnerDeviceHTTPPureCancelRetainsSlotUntilActualReturn(t *testing.T) {
	device := &pureDevice{entered: make(chan context.Context, 1), release: make(chan struct{})}
	h := &DeviceHandler{devices: device, slots: make(chan struct{}, 1)}
	_, raw := pureEnrollment(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := func() *http.Request {
		r := httptest.NewRequest("POST", enrollPath, strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	returned := make(chan struct{})
	go func() { defer close(returned); _, _ = pureDeviceCall(h, request().WithContext(ctx)) }()
	select {
	case <-device.entered:
	case <-returned:
		t.Fatal("valid enrollment returned before service admission")
	}
	t.Cleanup(func() {
		select {
		case <-device.release:
		default:
			close(device.release)
		}
		<-returned
	})
	cancel()
	_, e := pureDeviceCall(h, request())
	var ff *f.Fault
	if !errors.As(e, &ff) || ff.Code != f.RateLimited || len(h.slots) != 1 {
		t.Fatal("cancellation released inflight ownership", e)
	}
	close(device.release)
	<-returned
	if len(h.slots) != 0 || device.enrolls != 1 {
		t.Fatal("original service not actually retired")
	}
}

func TestRunnerDeviceHTTPPureCommittedWrongProjectionAborts(t *testing.T) {
	device := &pureDevice{wrong: true}
	h := &DeviceHandler{devices: device, slots: make(chan struct{}, 128)}
	_, raw := pureEnrollment(t)
	r := httptest.NewRequest("POST", enrollPath, strings.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	defer func() {
		e, ok := recover().(error)
		if !ok || !errors.Is(e, http.ErrAbortHandler) || len(h.slots) != 0 {
			t.Fatal("bad committed projection became a rejection or leaked slot")
		}
	}()
	_, _ = pureDeviceCall(h, r)
}
