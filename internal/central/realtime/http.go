package realtime

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	"github.com/gorilla/websocket"
)

const Path = "/api/v1/realtime"

type HTTPOptions struct {
	PublicOrigin         string
	AuthorizationTimeout time.Duration
	WriteTimeout         time.Duration
	ReadTimeout          time.Duration
	HeartbeatInterval    time.Duration
}

type gateway struct {
	hub       *Hub
	boundary  *account.HTTPBoundary
	authority c.Authority
	options   HTTPOptions
	upgrader  websocket.Upgrader
}

// NewHTTPHandler binds the original Account boundary, never a caller-provided
// actor or cached Session DTO. The composition root installs Path explicitly.
func NewHTTPHandler(hub *Hub, boundary *account.HTTPBoundary, authority c.Authority, options HTTPOptions) (http.Handler, error) {
	if hub == nil || hub.done == nil || hub.epoch.Validate() != nil || boundary == nil || authority == nil || nilAuthority(authority) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	origin, err := url.Parse(options.PublicOrigin)
	if err != nil || origin.Host == "" || (origin.Scheme != "https" && origin.Scheme != "http") || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.String() != options.PublicOrigin {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	if options.AuthorizationTimeout == 0 {
		options.AuthorizationTimeout = 10 * time.Second
	}
	if options.WriteTimeout == 0 {
		options.WriteTimeout = 5 * time.Second
	}
	if options.ReadTimeout == 0 {
		options.ReadTimeout = 75 * time.Second
	}
	if options.HeartbeatInterval == 0 {
		options.HeartbeatInterval = 25 * time.Second
	}
	if options.AuthorizationTimeout <= 0 || options.AuthorizationTimeout > time.Minute || options.WriteTimeout <= 0 || options.WriteTimeout > time.Minute || options.ReadTimeout <= 0 || options.ReadTimeout > 5*time.Minute || options.HeartbeatInterval <= 0 || options.HeartbeatInterval >= options.ReadTimeout {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	g := &gateway{hub: hub, boundary: boundary, authority: authority, options: options}
	g.upgrader = websocket.Upgrader{
		ReadBufferSize: 4096, WriteBufferSize: 4096, HandshakeTimeout: options.WriteTimeout,
		CheckOrigin: func(r *http.Request) bool { return g.exactOrigin(r) },
		Error: func(w http.ResponseWriter, r *http.Request, _ int, _ error) {
			boundary.WriteProblem(w, r, f.NewFault(f.InvalidArgument, f.NotStarted))
		},
	}
	return g, nil
}

func nilAuthority(authority c.Authority) bool {
	v := reflect.ValueOf(authority)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (g *gateway) exactOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	return len(values) == 1 && values[0] == g.options.PublicOrigin
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		g.boundary.WriteProblem(w, r, f.NewFault(f.MethodNotAllowed, f.NotStarted))
		return
	}
	if r.URL == nil || r.URL.Path != Path || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Sec-WebSocket-Protocol")) > 0 {
		g.boundary.WriteProblem(w, r, f.NewFault(f.InvalidArgument, f.NotStarted))
		return
	}
	// GET's ordinary Account rules permit a missing Origin. A browser WebSocket
	// must carry exactly the configured Origin, including in Account DevMode.
	if !g.exactOrigin(r) {
		g.boundary.WriteProblem(w, r, f.NewFault(f.Forbidden, f.NotStarted))
		return
	}
	conn, err := g.hub.admit(r.Context())
	if err != nil {
		g.boundary.WriteProblem(w, r, err)
		return
	}
	defer g.hub.release(conn)
	request := r.Clone(conn.ctx)
	if err := g.boundary.CheckRequest(w, request); err != nil {
		g.boundary.WriteProblem(w, request, err)
		return
	}
	authCtx, cancel := context.WithTimeout(conn.ctx, g.options.AuthorizationTimeout)
	actor, err := g.boundary.RequireHuman(request.WithContext(authCtx))
	cancel()
	if err != nil {
		g.boundary.WriteProblem(w, request, err)
		return
	}
	if !conn.current(nil) || conn.ctx.Err() != nil {
		g.boundary.WriteProblem(w, request, f.NewFault(f.ShuttingDown, f.NotStarted))
		return
	}
	socket, err := g.upgrader.Upgrade(w, request, nil)
	if err != nil {
		return
	}
	socket.SetReadLimit(c.MaxInboundBytes)
	_ = socket.SetReadDeadline(time.Now().Add(g.options.ReadTimeout))
	socket.SetPongHandler(func(string) error {
		return socket.SetReadDeadline(time.Now().Add(g.options.ReadTimeout))
	})
	socket.SetPingHandler(func(payload string) error {
		return socket.WriteControl(websocket.PongMessage, []byte(payload), time.Now().Add(g.options.WriteTimeout))
	})
	socket.SetCloseHandler(func(code int, text string) error { return &websocket.CloseError{Code: code} })
	// This owned watcher closes I/O even if an authority callback is still
	// unwinding. Its completion alone never releases the connection from Hub.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		select {
		case <-conn.stop:
		case <-conn.ctx.Done():
		}
		conn.cancel()
		_ = socket.Close()
	}()
	written := make(chan struct{})
	go func() {
		defer close(written)
		defer conn.stopConnection()
		g.write(conn, socket, request, actor)
	}()
	defer func() {
		conn.stopConnection()
		<-written
		<-closed
	}()
	g.read(conn, socket, request, actor)
}

func (g *gateway) requireCurrent(ctx context.Context, request *http.Request, original i.Actor) error {
	current, err := g.boundary.RequireHuman(request.WithContext(ctx))
	if err != nil {
		return err
	}
	if !original.Equal(current) {
		return f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	return nil
}

func (g *gateway) read(conn *connection, socket *websocket.Conn, request *http.Request, actor i.Actor) {
	for {
		kind, raw, err := socket.ReadMessage()
		if err != nil || kind != websocket.TextMessage {
			return
		}
		message, err := c.DecodeClientMessage(raw)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(conn.ctx, g.options.AuthorizationTimeout)
		ok := g.handle(ctx, conn, request, actor, message)
		cancel()
		if !ok {
			return
		}
	}
}

func (g *gateway) handle(ctx context.Context, conn *connection, request *http.Request, actor i.Actor, message c.ClientMessage) bool {
	if g.requireCurrent(ctx, request, actor) != nil {
		return false
	}
	switch message.Type {
	case c.Subscribe:
		return g.authority.RequireSubscription(ctx, actor, *message.Topic) == nil && conn.subscribe(message)
	case c.Unsubscribe:
		return conn.unsubscribe(message.SubscriptionID)
	case c.ReadSnapshot:
		// Capture BEFORE entering the reader; a concurrent commit remains a
		// later invalidation even when it arrives before this response does.
		sub, watermark, ok := conn.snapshotStart(message.SubscriptionID)
		if !ok || g.authority.RequireSubscription(ctx, actor, sub.topic) != nil {
			return false
		}
		snapshot, err := g.authority.ReadSnapshot(ctx, actor, sub.topic)
		message := c.ServerMessage{Type: c.SnapshotRead, SubscriptionID: sub.id, Topic: &sub.topic, Watermark: &watermark, Snapshot: &snapshot}
		if err != nil {
			// Authorization is rechecked by the writer before any resync frame.
			message.Type, message.Snapshot, message.Reason = c.Resync, nil, "unavailable"
		}
		frame, err := encodeFrame(message, sub)
		return err == nil && conn.enqueue(frame)
	default:
		return false
	}
}

func (g *gateway) authorizeBatch(ctx context.Context, conn *connection, request *http.Request, actor i.Actor, batch []queuedFrame) bool {
	if g.requireCurrent(ctx, request, actor) != nil {
		return false
	}
	checked := make(map[*subscription]bool)
	for _, frame := range batch {
		if frame.sub != nil && conn.current(frame.sub) && !checked[frame.sub] {
			if g.authority.RequireSubscription(ctx, actor, frame.sub.topic) != nil {
				return false
			}
			checked[frame.sub] = true
		}
	}
	return ctx.Err() == nil && conn.current(nil)
}

func (g *gateway) write(conn *connection, socket *websocket.Conn, request *http.Request, actor i.Actor) {
	ticker := time.NewTicker(g.options.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-conn.stop:
			return
		case <-conn.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(conn.ctx, g.options.AuthorizationTimeout)
			ok := g.requireCurrent(ctx, request, actor) == nil
			if ok {
				for _, topic := range conn.topics() {
					if g.authority.RequireSubscription(ctx, actor, topic) != nil {
						ok = false
						break
					}
				}
			}
			cancel()
			if !ok || !conn.current(nil) || socket.WriteControl(websocket.PingMessage, nil, time.Now().Add(g.options.WriteTimeout)) != nil {
				return
			}
		case <-conn.wake:
			batch, ok := conn.takeBatch()
			if !ok {
				return
			}
			if len(batch) == 0 {
				continue
			}
			ctx, cancel := context.WithTimeout(conn.ctx, g.options.AuthorizationTimeout)
			ok = g.authorizeBatch(ctx, conn, request, actor, batch)
			cancel()
			if !ok || socket.SetWriteDeadline(time.Now().Add(g.options.WriteTimeout)) != nil {
				return
			}
			for _, frame := range batch {
				if conn.current(frame.sub) && socket.WriteMessage(websocket.TextMessage, frame.raw) != nil {
					return
				}
			}
		}
	}
}
