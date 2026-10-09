package runnerhttp

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const (
	enrollPath    = "/api/v1/runner/enroll"
	challengePath = "/api/v1/runner/challenge"
	controlPath   = "/api/v1/runner/control"
)

type deviceService interface {
	Enroll(context.Context, p.EnrollmentRequest) (p.EnrollmentResponse, error)
	Challenge(context.Context, p.ChallengeRequest) (p.ChallengeResponse, error)
}

// DeviceHandler never derives a Human actor from cookies, headers or a body.
// Its bounded slots cover body decoding and original service return, including
// cancellation tails. The same slot gate is used by the control upgrade owner.
type DeviceHandler struct {
	devices deviceService
	slots   chan struct{}
}

func NewDeviceHandler(s *service.Service) (*DeviceHandler, error) {
	if s == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &DeviceHandler{devices: s, slots: make(chan struct{}, 128)}, nil
}

func HandlesDevicePath(path string) bool {
	return path == enrollPath || path == challengePath
}

func deviceRequest(r *http.Request, control bool) error {
	if r == nil || r.URL == nil || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" || r.URL.User != nil || r.URL.Scheme != "" || r.URL.Host != "" {
		return invalidInput()
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Cookie") || strings.EqualFold(name, "Origin") || strings.EqualFold(name, "X-CSRF-Token") || !control && strings.EqualFold(name, "Authorization") {
			return f.NewFault(f.Unauthenticated, f.NotStarted)
		}
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return invalidInput()
	}
	return nil
}

func deviceProblem(w http.ResponseWriter, r *http.Request, e error) {
	copy := *r
	copy.URL = &url.URL{Path: "/api/v1/runner"}
	httpapi.WriteProblem(w, &copy, e)
}

// Read the original bounded bytes before using the protocol's closed decoder.
// Protocol DTOs deliberately hide their fields from default JSON reflection.
func deviceBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var contentTypes []string
	for name, values := range r.Header {
		if strings.EqualFold(name, "Content-Encoding") {
			return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
		if strings.EqualFold(name, "Content-Type") {
			contentTypes = append(contentTypes, values...)
		}
	}
	if len(contentTypes) != 1 {
		return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
	}
	media, params, e := mime.ParseMediaType(contentTypes[0])
	if e != nil || media != "application/json" {
		return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
	}
	for name, value := range params {
		if name != "charset" || !strings.EqualFold(value, "utf-8") {
			return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	if r.Body == nil {
		return nil, invalidInput()
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, p.MaxDeviceBodyBytes))
	if e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			return nil, f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		return nil, invalidInput()
	}
	return raw, nil
}

func (h *DeviceHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	budget := 2 * time.Second
	if request.URL != nil && request.URL.Path == enrollPath {
		budget = 30 * time.Second
	}
	if request.URL != nil && HandlesDevicePath(request.URL.Path) {
		request.Pattern = request.URL.Path
	} else {
		request.Pattern = ""
	}
	ctx, cancel := context.WithTimeout(request.Context(), budget)
	defer cancel()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	owned := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		e := owned.finish(normal)
		if panicked || e != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || owned.start() != nil {
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	body, e := h.execute(w, r)
	if expired(ctx) || owned.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if e != nil {
		deviceProblem(w, r, e)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
	if expired(ctx) || owned.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *DeviceHandler) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if h == nil || h.devices == nil || h.slots == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if e := deviceRequest(r, false); e != nil {
		return nil, e
	}
	if !HandlesDevicePath(r.URL.Path) {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		return nil, f.NewFault(f.RateLimited, f.NotStarted)
	}
	raw, e := deviceBody(w, r)
	if e != nil {
		return nil, e
	}
	if r.URL.Path == enrollPath {
		in, e := p.DecodeEnrollmentRequest(raw)
		if e != nil {
			return nil, invalidInput()
		}
		if expired(r.Context()) {
			panic(http.ErrAbortHandler)
		}
		out, e := h.devices.Enroll(r.Context(), in)
		if e != nil {
			return nil, e
		}
		body, e := p.EncodeEnrollmentResponse(out)
		if e != nil || out.RunnerID() != in.RunnerID() || out.PublicKeyFingerprint() != p.PublicKeyFingerprint(in.PublicKey()) {
			panic(http.ErrAbortHandler)
		}
		return body, nil
	}
	in, e := p.DecodeChallengeRequest(raw)
	if e != nil {
		return nil, invalidInput()
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	out, e := h.devices.Challenge(r.Context(), in)
	if e != nil {
		return nil, e
	}
	body, e := p.EncodeChallengeResponse(out)
	if e != nil {
		panic(http.ErrAbortHandler)
	}
	return body, nil
}
