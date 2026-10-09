package runnerhttp

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

type controlBackend interface {
	controlAuthority
	Authenticate(context.Context, p.Authentication) (service.Connection, error)
	OwnConnection(context.Context, service.Connection, func(context.Context) error) error
	RejectIncompatible(context.Context, service.Connection) error
}

type controlRegistry struct {
	mu       sync.Mutex
	sessions map[p.ID]*controlSession
}

// Call only inside the committed reservation's current-generation gate. An old
// HTTP upgrade arriving late must never replace a successor in this registry.
func (r *controlRegistry) install(session *controlSession) {
	r.mu.Lock()
	previous := r.sessions[session.runner]
	r.sessions[session.runner] = session
	r.mu.Unlock()
	if previous != nil && previous != session {
		previous.wire.stop()
	}
}
func (r *controlRegistry) remove(session *controlSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions[session.runner] == session {
		delete(r.sessions, session.runner)
	}
}

func exactControlHeader(h http.Header, name string) (string, bool) {
	var all []string
	for key, values := range h {
		if strings.EqualFold(key, name) {
			all = append(all, values...)
		}
	}
	if len(all) != 1 {
		return "", false
	}
	return all[0], true
}

func controlAuthentication(r *http.Request) (p.Authentication, error) {
	if e := deviceRequest(r, true); e != nil {
		return p.Authentication{}, e
	}
	if r.URL.Path != controlPath || r.Method != http.MethodGet || r.ProtoMajor != 1 || r.ProtoMinor != 1 {
		return p.Authentication{}, invalidInput()
	}
	for name := range r.Header {
		if strings.EqualFold(name, "Sec-WebSocket-Extensions") || strings.EqualFold(name, "Content-Encoding") {
			return p.Authentication{}, invalidInput()
		}
	}
	for name, wanted := range map[string]string{"Sec-WebSocket-Protocol": p.Subprotocol, "Sec-WebSocket-Version": "13"} {
		value, ok := exactControlHeader(r.Header, name)
		if !ok || value != wanted {
			return p.Authentication{}, invalidInput()
		}
	}
	upgrade, ok := exactControlHeader(r.Header, "Upgrade")
	if !ok || !strings.EqualFold(upgrade, "websocket") {
		return p.Authentication{}, invalidInput()
	}
	connection, ok := exactControlHeader(r.Header, "Connection")
	if !ok {
		return p.Authentication{}, invalidInput()
	}
	upgradeToken := false
	for _, value := range strings.Split(connection, ",") {
		if strings.EqualFold(strings.TrimSpace(value), "upgrade") {
			if upgradeToken {
				return p.Authentication{}, invalidInput()
			}
			upgradeToken = true
		}
	}
	if !upgradeToken {
		return p.Authentication{}, invalidInput()
	}
	key, ok := exactControlHeader(r.Header, "Sec-WebSocket-Key")
	decoded, e := base64.StdEncoding.DecodeString(key)
	if !ok || e != nil || len(decoded) != 16 || base64.StdEncoding.EncodeToString(decoded) != key {
		return p.Authentication{}, invalidInput()
	}
	header, ok := exactControlHeader(r.Header, "Authorization")
	if !ok {
		return p.Authentication{}, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	auth, e := p.DecodeAuthorization(header)
	if e != nil {
		return p.Authentication{}, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if e := emptyBody(r); e != nil {
		return p.Authentication{}, e
	}
	return auth, nil
}

// gorilla clears HTTP deadlines during Upgrade. This wrapper retains the exact
// original handshake deadline until successful handoff, including cancellation
// racing that reset. Afterwards it is an ordinary net.Conn for the WSS owner.
type handshakeConn struct {
	net.Conn
	mu       sync.Mutex
	ctx      context.Context
	deadline time.Time
	handed   bool
}

func (c *handshakeConn) bounded(value time.Time) time.Time {
	if c.handed {
		return value
	}
	if c.ctx.Err() != nil {
		return time.Now()
	}
	if value.IsZero() || value.After(c.deadline) {
		return c.deadline
	}
	return value
}
func (c *handshakeConn) SetDeadline(value time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.SetDeadline(c.bounded(value))
}
func (c *handshakeConn) SetReadDeadline(value time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.SetReadDeadline(c.bounded(value))
}
func (c *handshakeConn) SetWriteDeadline(value time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.SetWriteDeadline(c.bounded(value))
}
func (c *handshakeConn) handoff() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	c.handed = true
	return c.Conn.SetDeadline(time.Time{})
}

type controlHandshake struct {
	http.ResponseWriter
	io       requestIO
	mu       sync.Mutex
	conn     *handshakeConn
	hijacked bool
}

func (w *controlHandshake) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if expired(w.io.ctx) {
		return nil, nil, context.DeadlineExceeded
	}
	conn, buffer, e := http.NewResponseController(w.ResponseWriter).Hijack()
	if e != nil {
		return nil, nil, e
	}
	w.hijacked = true
	deadline, _ := w.io.ctx.Deadline()
	w.conn = &handshakeConn{Conn: conn, ctx: w.io.ctx, deadline: deadline}
	return w.conn, buffer, nil
}

// The callback follows ownership through Hijack; it is actually joined before
// a socket is handed to the long-lived session or an HTTP handler returns.
func (w *controlHandshake) start(ctx context.Context, body *http.Request) error {
	w.io = requestIO{ctx: ctx, controller: http.NewResponseController(w.ResponseWriter), body: body.Body}
	deadline, _ := ctx.Deadline()
	if e := w.io.controller.SetReadDeadline(deadline); e != nil {
		return e
	}
	if e := w.io.controller.SetWriteDeadline(deadline); e != nil {
		return e
	}
	w.io.callbackDone = make(chan struct{})
	w.io.stop = context.AfterFunc(ctx, func() {
		defer close(w.io.callbackDone)
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.conn != nil {
			w.io.callbackErr = w.conn.SetDeadline(time.Now())
		} else {
			w.io.callbackErr = errors.Join(w.io.controller.SetReadDeadline(time.Now()), w.io.controller.SetWriteDeadline(time.Now()))
		}
	})
	return nil
}

func (w *controlHandshake) stopCallback() {
	if w.io.stop != nil {
		if !w.io.stop() {
			<-w.io.callbackDone
		}
		w.io.stop = nil
	}
}

func (h *DeviceHandler) serveControl(w http.ResponseWriter, request *http.Request) {
	request.Pattern = controlPath
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	r := request.WithContext(ctx)
	handshake := &controlHandshake{ResponseWriter: abortWriter{w}}
	handed := false
	defer func() {
		panicked := recover() != nil
		handshake.stopCallback()
		closeErr := handshake.io.closeBody()
		if handshake.hijacked {
			if !handed && handshake.conn != nil {
				_ = handshake.conn.SetDeadline(time.Now())
				_ = handshake.conn.Close()
			}
		} else {
			closeErr = errors.Join(closeErr, handshake.io.finish(!panicked))
		}
		if panicked || closeErr != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if handshake.start(ctx, r) != nil {
		panic(http.ErrAbortHandler)
	}
	problem := func(e error) {
		if expired(ctx) || handshake.io.closeBody() != nil {
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		deviceProblem(handshake.ResponseWriter, r, e)
		if handshake.io.controller.Flush() != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if h == nil || h.control == nil || h.registry == nil || h.slots == nil {
		problem(f.NewFault(f.DependencyUnbound, f.NotStarted))
		return
	}
	select {
	case h.slots <- struct{}{}:
	default:
		problem(f.NewFault(f.RateLimited, f.NotStarted))
		return
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { <-h.slots }) }
	defer release()
	auth, e := controlAuthentication(r)
	if e != nil {
		problem(e)
		return
	}
	if handshake.io.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	reservation, e := h.control.Authenticate(ctx, auth)
	if e != nil {
		problem(e)
		return
	}
	// The service owns upgrade, native session and final conditional DB cleanup.
	// Its original HTTP parent survives the finite authentication budget.
	e = h.control.OwnConnection(request.Context(), reservation, func(owned context.Context) error {
		cancelReturned := make(chan struct{})
		stopCancel := context.AfterFunc(owned, func() { defer close(cancelReturned); cancel() })
		defer func() {
			if !stopCancel() {
				<-cancelReturned
			}
		}()
		var session *controlSession
		var socket *websocket.Conn
		defer func() {
			if socket != nil {
				_ = (controlNativeSocket{socket}).close()
			}
			if session != nil {
				h.registry.remove(session)
			}
		}()
		e := h.control.WithCurrentConnection(ctx, reservation, func(gated context.Context) error {
			if owned.Err() != nil || expired(gated) {
				return errControlClosed
			}
			deadline, _ := gated.Deadline()
			upgrader := websocket.Upgrader{HandshakeTimeout: time.Until(deadline), Subprotocols: []string{p.Subprotocol}, EnableCompression: false,
				CheckOrigin: func(req *http.Request) bool { return deviceRequest(req, true) == nil },
				Error:       func(http.ResponseWriter, *http.Request, int, error) {},
			}
			var e error
			socket, e = upgrader.Upgrade(handshake, r, http.Header{"Cache-Control": []string{"no-store"}})
			if e != nil {
				return errControlTransport
			}
			handshake.stopCallback()
			if e := errors.Join(handshake.io.callbackErr, handshake.conn.handoff(), owned.Err(), gated.Err()); e != nil {
				return errControlTransport
			}
			socket.EnableWriteCompression(false)
			socket.SetReadLimit(p.MaxMessageBytes)
			wire := newControlWire(controlNativeSocket{socket}, func(ctx context.Context, write func(context.Context) error) error {
				return h.control.WithCurrentConnection(ctx, reservation, write)
			})
			wire.incompatible = func(ctx context.Context) error { return h.control.RejectIncompatible(ctx, reservation) }
			session = newControlSession(wire, h.control, reservation, auth.RunnerID())
			h.registry.install(session)
			handed = true
			return nil
		})
		if e != nil {
			return e
		}
		cancel()
		release()
		return session.run(owned)
	})
	if !handshake.hijacked {
		if e == nil {
			e = f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		problem(e)
	}
}
