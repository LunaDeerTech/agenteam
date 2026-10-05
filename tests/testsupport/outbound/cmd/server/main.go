// The isolated fixture server runs only in a nonce-labelled disposable
// container. It records synthetic canaries in memory, never operational logs.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type config struct {
	Mode, Redirect, Body string
	Size, HeaderBytes    int
	Wire                 *wireScenario
}
type wireScenario struct {
	Suffix          string
	Status          int
	Headers         map[string]string
	Chunks          [][]byte
	HoldAfter       *int
	DisconnectAfter *int
}
type request struct {
	Connection                      int64
	Method, Host, Path, Query, Body string
	Headers                         http.Header
	RequestURI                      string
}
type scenario struct {
	Config                            config
	Requests                          []request
	Released                          bool
	ActiveHandlers, CompletedHandlers int
	release                           chan struct{}
}
type serverState struct {
	mu          sync.Mutex
	cases       map[string]*scenario
	closed      map[int64]bool
	connections map[int64]string
	next        atomic.Int64
	nonce       string
}
type connectionKey struct{}

func main() {
	cert := flag.String("cert", "", "fixture certificate")
	key := flag.String("key", "", "fixture key")
	nonce := flag.String("nonce", "", "fixture nonce")
	flag.Parse()
	s := &serverState{cases: map[string]*scenario{}, closed: map[int64]bool{}, connections: map[int64]string{}, nonce: *nonce}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	makeServer := func(addr string, handler http.Handler) *http.Server {
		return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ErrorLog: log.New(io.Discard, "", 0), ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connectionKey{}, s.next.Add(1))
		}, ConnState: func(c net.Conn, state http.ConnState) {}}
	}
	business := http.HandlerFunc(s.serve)
	httpServer, tlsServer, control := makeServer(":8080", business), makeServer(":8443", business), makeServer(":9000", http.HandlerFunc(s.control))
	// ConnState needs the same identity as ConnContext. Separate maps avoid
	// relying on remote ports or incidental ordering of concurrently accepted sockets.
	for _, srv := range []*http.Server{httpServer, tlsServer} {
		var mu sync.Mutex
		ids := map[net.Conn]int64{}
		srv.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
			id := s.next.Add(1)
			mu.Lock()
			ids[c] = id
			mu.Unlock()
			s.mu.Lock()
			s.connections[id] = c.RemoteAddr().String()
			s.mu.Unlock()
			return context.WithValue(ctx, connectionKey{}, id)
		}
		srv.ConnState = func(c net.Conn, state http.ConnState) {
			if state == http.StateClosed || state == http.StateHijacked {
				mu.Lock()
				id := ids[c]
				delete(ids, c)
				mu.Unlock()
				s.mu.Lock()
				s.closed[id] = true
				s.mu.Unlock()
			}
		}
	}
	errors := make(chan error, 3)
	go func() { errors <- httpServer.ListenAndServe() }()
	go func() { errors <- tlsServer.ListenAndServeTLS(*cert, *key) }()
	go func() { errors <- control.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case e := <-errors:
		if e != http.ErrServerClosed {
			os.Exit(1)
		}
	}
	for _, srv := range []*http.Server{httpServer, tlsServer, control} {
		_ = srv.Close()
	}
}
func (s *serverState) control(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Fixture-Nonce") != s.nonce {
		w.WriteHeader(403)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 1 && parts[0] == "ready" {
		w.WriteHeader(204)
		return
	}
	if len(parts) != 2 || len(parts[1]) != 32 {
		w.WriteHeader(400)
		return
	}
	id := parts[1]
	s.mu.Lock()
	defer s.mu.Unlock()
	switch parts[0] {
	case "create":
		var cfg config
		if r.Method != "POST" || json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cfg) != nil || cfg.Size < 0 || cfg.Size > 300<<20 || cfg.HeaderBytes < 0 || cfg.HeaderBytes > 1<<20 || !validWire(&cfg, id) {
			w.WriteHeader(400)
			return
		}
		if s.cases[id] != nil {
			w.WriteHeader(409)
			return
		}
		s.cases[id] = &scenario{Config: cfg, release: make(chan struct{})}
		w.WriteHeader(201)
	case "release":
		v := s.cases[id]
		if v == nil {
			w.WriteHeader(404)
			return
		}
		if !v.Released {
			v.Released = true
			close(v.release)
		}
		w.WriteHeader(204)
	case "state":
		v := s.cases[id]
		if v == nil {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Requests                          []request
			Closed                            map[int64]bool
			Connections                       map[int64]string
			ActiveHandlers, CompletedHandlers int
		}{v.Requests, s.closed, s.connections, v.ActiveHandlers, v.CompletedHandlers})
	default:
		w.WriteHeader(404)
	}
}
func (s *serverState) serve(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/case/")
	if len(id) != 32 {
		// Only the new wire scenario admits a suffix. Legacy modes retain their
		// exact decoded /case/<id> route below, including existing defaults.
		raw := strings.TrimPrefix(r.URL.EscapedPath(), "/case/")
		if !strings.HasPrefix(r.URL.EscapedPath(), "/case/") || len(raw) < 33 {
			w.WriteHeader(404)
			return
		}
		id = raw[:32]
	}
	s.mu.Lock()
	v := s.cases[id]
	if v == nil {
		s.mu.Unlock()
		w.WriteHeader(404)
		return
	}
	cfg := v.Config
	if cfg.Mode == "openai_chat_wire" {
		if r.URL.EscapedPath() != "/case/"+id+cfg.Wire.Suffix || r.URL.RawQuery != "" || r.URL.ForceQuery {
			s.mu.Unlock()
			w.WriteHeader(404)
			return
		}
	} else if strings.TrimPrefix(r.URL.Path, "/case/") != id {
		s.mu.Unlock()
		w.WriteHeader(404)
		return
	}
	release := v.release
	v.ActiveHandlers++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); v.ActiveHandlers--; v.CompletedHandlers++; s.mu.Unlock() }()
	body, e := io.ReadAll(io.LimitReader(r.Body, 16<<20+1))
	if e != nil {
		w.WriteHeader(400)
		return
	}
	_ = r.Body.Close()
	if cfg.Mode == "openai_chat_wire" && len(body) > 16<<20 {
		w.WriteHeader(413)
		return
	}
	record := request{Connection: r.Context().Value(connectionKey{}).(int64), Method: r.Method, Host: r.Host, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body), Headers: r.Header.Clone(), RequestURI: r.RequestURI}
	s.mu.Lock()
	v.Requests = append(v.Requests, record)
	s.mu.Unlock()
	if cfg.Mode == "openai_chat_wire" {
		serveWire(w, r, cfg.Wire, release)
		return
	}
	switch cfg.Mode {
	case "drop":
		conn, _, e := w.(http.Hijacker).Hijack()
		if e == nil {
			conn.Close()
		}
		return
	case "redirect":
		w.Header().Set("Location", cfg.Redirect)
		w.WriteHeader(302)
		return
	case "hold_headers":
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	case "informational":
		w.Header().Set("Link", "</fixture>; rel=preload")
		w.WriteHeader(103)
		w.Header().Del("Link")
	case "encoding":
		w.Header().Set("Content-Encoding", "gzip")
	case "upgrade":
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Upgrade", "fixture")
		w.WriteHeader(101)
		return
	}
	if cfg.HeaderBytes > 0 {
		w.Header().Set("X-Large", strings.Repeat("a", cfg.HeaderBytes))
	}
	if cfg.Mode == "connection_close" {
		w.Header().Set("Connection", "close")
	}
	if cfg.Mode == "chunked" {
		w.Header().Set("Trailer", "X-Fixture-Trailer")
	}
	w.WriteHeader(200)
	if cfg.Mode == "stream" || cfg.Mode == "chunked" {
		w.(http.Flusher).Flush()
	}
	if cfg.Body != "" {
		if _, e := io.WriteString(w, cfg.Body); e != nil {
			return
		}
	}
	if cfg.Mode == "stream" {
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	}
	if cfg.Size > 0 {
		buf := strings.Repeat("x", 4096)
		remaining := cfg.Size
		for remaining > 0 {
			part := buf
			if remaining < len(part) {
				part = part[:remaining]
			}
			n, e := io.WriteString(w, part)
			remaining -= n
			if e != nil {
				return
			}
			if cfg.Mode == "stream" {
				w.(http.Flusher).Flush()
			}
		}
	}
	if cfg.Mode == "chunked" {
		w.Header().Set("X-Fixture-Trailer", "complete")
	}
}

func validWire(cfg *config, id string) bool {
	if cfg.Mode != "openai_chat_wire" {
		return cfg.Wire == nil
	}
	v := cfg.Wire
	if v == nil || cfg.Redirect != "" || cfg.Body != "" || cfg.Size != 0 || cfg.HeaderBytes != 0 {
		return false
	}
	if raw, err := hex.DecodeString(id); err != nil || len(raw) != 16 {
		return false
	}
	if v.Suffix == "" {
		v.Suffix = "/chat/completions"
	}
	if len(v.Suffix) > 2048 || !strings.HasPrefix(v.Suffix, "/") || !strings.HasSuffix(v.Suffix, "/chat/completions") || strings.ContainsAny(v.Suffix, "?#") {
		return false
	}
	u, err := url.ParseRequestURI(v.Suffix)
	if err != nil || u.EscapedPath() != v.Suffix || u.RawQuery != "" || u.ForceQuery {
		return false
	}
	switch v.Status {
	case 200, 302, 307, 308, 400, 401, 403, 404, 408, 409, 429, 500, 502, 503, 504:
	default:
		return false
	}
	seen := map[string]bool{}
	headerBytes := 0
	for key, value := range v.Headers {
		name := http.CanonicalHeaderKey(key)
		switch name {
		case "Content-Type", "X-Request-Id", "Retry-After", "Location":
		default:
			return false
		}
		if seen[name] {
			return false
		}
		seen[name] = true
		for _, c := range value {
			if c < 32 && c != '\t' || c == 127 {
				return false
			}
		}
		headerBytes += len(key) + len(value)
		if headerBytes > 16<<10 {
			return false
		}
	}
	if len(v.Chunks) > 512 {
		return false
	}
	total := 0
	for _, chunk := range v.Chunks {
		total += len(chunk)
		if total > 512<<10 {
			return false
		}
	}
	for _, n := range []*int{v.HoldAfter, v.DisconnectAfter} {
		if n != nil && (*n < 0 || *n > len(v.Chunks)) {
			return false
		}
	}
	return true
}
func serveWire(w http.ResponseWriter, r *http.Request, v *wireScenario, release <-chan struct{}) {
	for key, value := range v.Headers {
		w.Header().Set(key, value)
	}
	w.WriteHeader(v.Status)
	w.(http.Flusher).Flush()
	for sent := 0; sent <= len(v.Chunks); sent++ {
		if v.HoldAfter != nil && sent == *v.HoldAfter {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if v.DisconnectAfter != nil && sent == *v.DisconnectAfter {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		if sent == len(v.Chunks) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
		if _, err := w.Write(v.Chunks[sent]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
	}
}
