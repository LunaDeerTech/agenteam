package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

type eventLog struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	pending []byte
	events  chan map[string]any
}

func newEventLog() *eventLog { return &eventLog{events: make(chan map[string]any, 128)} }
func (l *eventLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.buffer.Write(b)
	l.pending = append(l.pending, b...)
	for {
		at := bytes.IndexByte(l.pending, '\n')
		if at < 0 {
			break
		}
		var event map[string]any
		if json.Unmarshal(l.pending[:at], &event) == nil {
			select {
			case l.events <- event:
			default:
			}
		}
		l.pending = l.pending[at+1:]
	}
	return len(b), nil
}
func (l *eventLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }
func (l *eventLog) wait(t *testing.T, predicate func(map[string]any) bool) map[string]any {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-l.events:
			if predicate(event) {
				return event
			}
		case <-timer.C:
			t.Fatalf("log event timed out: %s", l.String())
			return nil
		}
	}
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("app barrier timed out")
	}
}

func testConfig(t *testing.T, timeout string) config.Config {
	t.Helper()
	values := map[string]string{config.Prefix + "SECRET_KEYRING": `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, config.Prefix + "CURSOR_KEYRING": `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, config.Prefix + "HTTP_ADDR": "127.0.0.1:0", config.Prefix + "SHUTDOWN_TIMEOUT": timeout, config.Prefix + "DATABASE_URL": "postgresql://unit:unit@127.0.0.1:1/unit", config.Prefix + "DATABASE_TLS_MODE": "disable"}
	for key, value := range objectfixture.ConfigOnlyValues() {
		values[key] = value
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := values[k]; return v, ok }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

type appRun struct {
	signals chan os.Signal
	done    chan struct{}
	err     error
	cancel  context.CancelFunc
	log     *eventLog
	url     string
}
type observedLogger struct {
	*logging.Logger
	hook func(map[string]any)
}

func (l observedLogger) Transition(p logging.Phase) {
	l.Logger.Transition(p)
	l.hook(map[string]any{"phase": string(p)})
}
func startApp(t *testing.T, timeout string, deps dependencies, hooks ...func(map[string]any)) *appRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := &appRun{signals: make(chan os.Signal, 4), done: make(chan struct{}), cancel: cancel, log: newEventLog()}
	logger, err := logging.New(logging.Central, slog.LevelInfo, result.log)
	if err != nil {
		t.Fatal(err)
	}
	var observer processLogger = logger
	if len(hooks) > 0 {
		observer = observedLogger{logger, hooks[0]}
	}
	cfg := testConfig(t, timeout)
	deps = unitDependencies(deps)
	go func() { result.err = run(ctx, cfg, observer, result.signals, deps); close(result.done) }()
	t.Cleanup(func() { cancel(); await(t, result.done) })
	event := result.log.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })
	result.url = "http://" + event["listen_address"].(string)
	return result
}

func TestDiagnosticRoutes(t *testing.T) {
	app := startApp(t, "1s", dependencies{})
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{
		{"GET", "/livez", 200, ""}, {"GET", "/readyz", 503, "DEPENDENCY_UNAVAILABLE"}, {"GET", "/diagnostics", 200, ""},
		{"HEAD", "/livez", 200, ""}, {"HEAD", "/readyz", 503, "DEPENDENCY_UNAVAILABLE"}, {"HEAD", "/diagnostics", 200, ""},
		{"POST", "/livez", 405, "METHOD_NOT_ALLOWED"}, {"DELETE", "/readyz", 405, "METHOD_NOT_ALLOWED"}, {"PATCH", "/diagnostics", 405, "METHOD_NOT_ALLOWED"},
		{"GET", "/api/v1/session", 404, "NOT_FOUND"}, {"GET", "/page", 404, "NOT_FOUND"}, {"GET", "/assets/missing", 404, "NOT_FOUND"},
	} {
		r, _ := http.NewRequest(tc.method, app.url+tc.path, nil)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != tc.status || response.Header.Get("X-Request-ID") == "" {
			t.Fatalf("%s %s status=%d err=%v", tc.method, tc.path, response.StatusCode, err)
		}
		if tc.method == "HEAD" {
			if len(body) != 0 {
				t.Fatal("HEAD has a body")
			}
			continue
		}
		if tc.code != "" && !bytes.Contains(body, []byte(`"code":"`+tc.code+`"`)) {
			t.Fatalf("wrong Problem: %s", body)
		}
		if tc.path == "/livez" && tc.method == "GET" && string(body) != `{"status":"alive"}` {
			t.Fatalf("liveness: %s", body)
		}
		if tc.path == "/diagnostics" && tc.method == "GET" {
			var d diagnostics
			if json.Unmarshal(body, &d) != nil || d.Ready || len(d.Capabilities) < 5 {
				t.Fatal("diagnostics invalid")
			}
			for _, c := range d.Capabilities {
				want := "unbound"
				if c.Name == "secret" || c.Name == "outbound" || c.Name == "object_storage" {
					want = "unavailable"
				}
				if c.Name == "postgresql" || c.Name == "pgvector" || c.Name == "migrations" || c.Name == "read_write" || c.Name == "cursor" || c.Name == "audit_storage" {
					want = "available"
				}
				if c.Status != want {
					t.Fatal("unbound dependency claimed usable")
				}
			}
		}
	}
	app.signals <- syscall.SIGTERM
	await(t, app.done)
	if app.err != nil {
		t.Fatal(app.err)
	}
}

type observingListener struct {
	net.Listener
	read chan struct{}
}

func (l observingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &observingConn{Conn: conn, read: l.read}, nil
}

type observingConn struct {
	net.Conn
	read  chan struct{}
	input []byte
	once  sync.Once
}

func (c *observingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if len(c.input) < 4096 {
		c.input = append(c.input, b[:n]...)
		if bytes.Contains(c.input, []byte("X-Wait-For-Stop: 1\r\n")) {
			c.once.Do(func() { close(c.read) })
		}
	}
	return n, err
}

type responseResult struct {
	status int
	body   []byte
	err    error
}

func asyncGet(client *http.Client, url string) <-chan responseResult {
	done := make(chan responseResult, 1)
	go func() {
		r, err := client.Get(url)
		if err != nil {
			done <- responseResult{err: err}
			return
		}
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		done <- responseResult{status: r.StatusCode, body: body, err: err}
	}()
	return done
}
func receiveResponse(t *testing.T, ch <-chan responseResult) responseResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP response timed out")
		return responseResult{}
	}
}

func TestGracefulDrainPreservesActiveContextAndRejectsLateRequest(t *testing.T) {
	started, release, partialRead := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	contexts := make(chan context.Context, 1)
	shutdownRelease := make(chan struct{})
	var shutdownOnce sync.Once
	handoff := func() { shutdownOnce.Do(func() { close(shutdownRelease) }) }
	defer handoff()
	diagnostics := diagnosticRouter(newHealthMonitor(unitHealth(), healthTiming{}), true, nil, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slow" {
			diagnostics.ServeHTTP(w, r)
			return
		}
		contexts <- r.Context()
		close(started)
		<-release
		if r.Context().Err() != nil {
			t.Error("first signal cancelled the active request")
		}
		_ = httpapi.WriteJSON(w, r, 200, struct {
			Finished bool `json:"finished"`
		}{true})
	})
	app := startApp(t, "5s", dependencies{handler: handler, listen: func(ctx context.Context, network, address string) (net.Listener, error) {
		l, err := (&net.ListenConfig{}).Listen(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return observingListener{l, partialRead}, nil
	}}, func(event map[string]any) {
		if event["phase"] == "stopping" {
			<-shutdownRelease
		}
	})
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	response := asyncGet(client, app.url+"/slow")
	await(t, started)
	requestContext := <-contexts
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(app.url, "http://"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(conn, "GET /livez HTTP/1.1\r\nHost: localhost\r\nX-Wait-For-Stop: 1\r\n")
	await(t, partialRead)
	app.signals <- syscall.SIGTERM
	app.log.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	if requestContext.Err() != nil {
		t.Fatal("serving context shares first-stop cancellation")
	}
	_, _ = io.WriteString(conn, "\r\n")
	late, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(late.Body)
	_ = late.Body.Close()
	if err != nil || late.StatusCode != 503 || !bytes.Contains(body, []byte(`"code":"SHUTTING_DOWN"`)) {
		t.Fatalf("late admitted request: %d %s %v", late.StatusCode, body, err)
	}
	handoff()
	finish()
	result := receiveResponse(t, response)
	if result.err != nil || result.status != 200 || string(result.body) != `{"finished":true}` {
		t.Fatalf("active request did not drain: %+v", result)
	}
	await(t, app.done)
	if app.err != nil {
		t.Fatal(app.err)
	}
	if requestContext.Err() == nil {
		t.Fatal("serving context not released after drain")
	}
	_, _ = io.WriteString(conn, "GET /livez HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if late, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet}); err == nil {
		_ = late.Body.Close()
		t.Fatal("Shutdown retained an idle connection for new requests")
	}
	if !strings.Contains(app.log.String(), `"outcome":"drained"`) {
		t.Fatal("clean stop not reported")
	}
}

func TestForcedStopBoundsNonCooperativeHandler(t *testing.T) {
	for _, mode := range []string{"deadline", "second_signal"} {
		t.Run(mode, func(t *testing.T) {
			started, release, handlerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			contexts := make(chan context.Context, 1)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				contexts <- r.Context()
				close(started)
				<-release
			})
			timeout := "100ms"
			want := lifecycle.ShutdownTimeout
			if mode == "second_signal" {
				timeout = "5s"
				want = lifecycle.ForcedShutdown
			}
			app := startApp(t, timeout, dependencies{handler: handler})
			t.Cleanup(func() { close(release); await(t, handlerDone) })
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			response := asyncGet(client, app.url+"/slow")
			await(t, started)
			requestContext := <-contexts
			app.signals <- syscall.SIGTERM
			app.log.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
			if mode == "second_signal" {
				app.signals <- syscall.SIGINT
			}
			select {
			case <-app.done:
			case <-time.After(3 * time.Second):
				t.Fatal("forced stop waited for noncooperative handler")
			}
			if lifecycle.CodeOf(app.err) != want || requestContext.Err() == nil {
				t.Fatalf("wrong forced outcome: %v context=%v", app.err, requestContext.Err())
			}
			if result := receiveResponse(t, response); result.err == nil {
				t.Fatal("forced connection remained successful")
			}
			if !strings.Contains(app.log.String(), `"outcome":"forced"`) || strings.Contains(app.log.String(), `"outcome":"drained"`) {
				t.Fatal("forced stop claimed graceful completion")
			}
		})
	}
}

type failingListener struct {
	net.Listener
	failure error
}

func (l failingListener) Accept() (net.Conn, error) { return nil, l.failure }

func TestStartupStopAndUnexpectedServeFailure(t *testing.T) {
	for _, mode := range []string{"already_cancelled", "during_listen", "after_acquisition", "serve_failure", "unexpected_server_closed"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			logs := newEventLog()
			logger, _ := logging.New(logging.Central, slog.LevelInfo, logs)
			var acquired net.Listener
			cause := errors.New("credential-SENTINEL")
			deps := unitDependencies(dependencies{})
			switch mode {
			case "already_cancelled":
				cancel()
				deps.listen = func(context.Context, string, string) (net.Listener, error) {
					t.Error("listen called after startup cancellation")
					return nil, cause
				}
			case "during_listen":
				deps.listen = func(ctx context.Context, _, _ string) (net.Listener, error) {
					cancel()
					<-ctx.Done()
					return nil, ctx.Err()
				}
			case "after_acquisition":
				deps.listen = func(ctx context.Context, network, address string) (net.Listener, error) {
					l, err := (&net.ListenConfig{}).Listen(ctx, network, address)
					acquired = l
					cancel()
					<-ctx.Done()
					return l, err
				}
			default:
				deps.listen = func(ctx context.Context, network, address string) (net.Listener, error) {
					l, err := (&net.ListenConfig{}).Listen(ctx, network, address)
					if err != nil {
						return nil, err
					}
					acquired = l
					if mode == "unexpected_server_closed" {
						cause = http.ErrServerClosed
					}
					return failingListener{l, cause}, nil
				}
			}
			err := run(ctx, testConfig(t, "1s"), logger, nil, deps)
			if strings.HasPrefix(mode, "serve_") || mode == "unexpected_server_closed" {
				if lifecycle.CodeOf(err) != lifecycle.ServeFailed || !errors.Is(err, cause) {
					t.Fatalf("unexpected Serve return was ignored: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(logs.String(), `"event":"listening"`) {
					t.Fatal("cancelled startup logged listening")
				}
			}
			if acquired != nil {
				if conn, err := net.DialTimeout("tcp", acquired.Addr().String(), 100*time.Millisecond); err == nil {
					_ = conn.Close()
					t.Fatal("listener leaked")
				}
			}
			if strings.Contains(logs.String(), "SENTINEL") {
				t.Fatal("raw startup/Serve error logged")
			}
		})
	}
}
