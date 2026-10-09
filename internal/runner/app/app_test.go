package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/internal/runner/config"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type eventWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (w *eventWriter) Write(raw []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Write(raw)
}
func (w *eventWriter) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.buffer.String() }
func processConfig(t *testing.T) config.Config {
	t.Helper()
	directory, err := os.MkdirTemp(".", ".app-identity-")
	if err != nil {
		t.Fatal(err)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(directory, "identity.json")
	id, _ := p.NewID()
	v, err := identity.NewPending(identity.Configuration{CentralURL: "https://runner.example", RunnerID: id, RootPath: "/runner"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Save(v); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{config.Prefix + "IDENTITY_FILE": path, config.Prefix + "SHUTDOWN_TIMEOUT": "100ms"}
	env := []string{config.Prefix + "IDENTITY_FILE=" + path, config.Prefix + "SHUTDOWN_TIMEOUT=100ms"}
	cfg, err := config.Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, env)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

type heldClient struct {
	entered, release, returned, stop chan struct{}
	once                             sync.Once
	forced                           chan context.Context
	drained                          chan context.Context
	options                          control.Options
}

func (c *heldClient) Run(ctx context.Context) error {
	close(c.entered)
	if c.options.Observe != nil {
		c.options.Observe(ctx, control.Connecting)
	}
	<-ctx.Done()
	if c.release != nil {
		<-c.release
	}
	close(c.returned)
	return ctx.Err()
}
func (c *heldClient) Stop() { c.once.Do(func() { close(c.stop) }) }
func (c *heldClient) Drain(ctx context.Context) error {
	c.drained <- ctx
	select {
	case <-c.returned:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *heldClient) Force(ctx context.Context) error {
	c.forced <- ctx
	select {
	case <-c.returned:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func appWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("root phase was not reached")
	}
}
func appResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("root did not return within original budget")
		return nil
	}
}
func TestRunnerRootStopsAndActuallyJoins(t *testing.T) {
	for _, mode := range []string{"SIGINT", "SIGTERM", "context", "already_cancelled"} {
		t.Run(mode, func(t *testing.T) {
			cfg := processConfig(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "already_cancelled" {
				cancel()
			}
			output := &eventWriter{}
			logger, _ := logging.New(logging.Runner, slog.LevelInfo, output)
			signals := make(chan os.Signal, 2)
			owned := &heldClient{entered: make(chan struct{}), returned: make(chan struct{}), stop: make(chan struct{}), forced: make(chan context.Context, 1), drained: make(chan context.Context, 1)}
			construct := func(options control.Options) (clientOwner, error) { owned.options = options; return owned, nil }
			done := make(chan error, 1)
			go func() { done <- run(ctx, cfg, logger, signals, nil, construct) }()
			if mode != "already_cancelled" {
				appWait(t, owned.entered)
			}
			switch mode {
			case "SIGINT":
				signals <- syscall.SIGINT
			case "SIGTERM":
				signals <- syscall.SIGTERM
			default:
				cancel()
			}
			if err := appResult(t, done); err != nil {
				t.Fatal(err)
			}
			text := output.String()
			if !strings.Contains(text, `"outcome":"drained"`) || strings.Contains(text, `"ready":true`) || strings.Contains(text, `"connected":true`) {
				t.Fatal("root fabricated connection/join")
			}
			if mode != "already_cancelled" {
				appWait(t, owned.returned)
				select {
				case actual := <-owned.drained:
					if _, ok := actual.Deadline(); !ok {
						t.Fatal("original drain lost deadline")
					}
				default:
					t.Fatal("actual client Drain was skipped")
				}
			} else {
				select {
				case <-owned.entered:
					t.Fatal("cancelled root admitted client")
				default:
				}
			}
		})
	}
}
func TestRunnerRootForcePreservesDeadlineAndUnjoinedFailure(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(map[bool]string{false: "held", true: "returned"}[release], func(t *testing.T) {
			cfg := processConfig(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			output := &eventWriter{}
			logger, _ := logging.New(logging.Runner, slog.LevelInfo, output)
			c := &heldClient{entered: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{}), stop: make(chan struct{}), forced: make(chan context.Context, 1), drained: make(chan context.Context, 1)}
			done := make(chan error, 1)
			go func() {
				done <- run(ctx, cfg, logger, nil, nil, func(o control.Options) (clientOwner, error) { c.options = o; return c, nil })
			}()
			appWait(t, c.entered)
			cancel()
			var force context.Context
			select {
			case force = <-c.forced:
			case <-time.After(time.Second):
				t.Fatal("force was not called after original drain")
			}
			deadline, ok := force.Deadline()
			if !ok || time.Until(deadline) > time.Second || force.Err() != nil {
				t.Fatal("Force received refreshed/missing/expired initial context")
			}
			if release {
				close(c.release)
			}
			err := appResult(t, done)
			want := lifecycle.ShutdownTimeout
			if release {
				want = lifecycle.ForcedShutdown
			}
			if lifecycle.CodeOf(err) != want {
				t.Fatal("wrong actual force outcome", err)
			}
			if !release {
				select {
				case <-c.returned:
					t.Fatal("held original callback magically returned")
				default:
				}
				if !strings.Contains(output.String(), `"outcome":"forced"`) || strings.Contains(output.String(), `"outcome":"drained"`) {
					t.Fatal("force timeout reported drained")
				}
				close(c.release)
				appWait(t, c.returned)
			}
		})
	}
}

type countedInput struct {
	io.Reader
	closed int
}

func (i *countedInput) Close() error { i.closed++; return nil }
func TestEnrollmentInputIsExactAndSafe(t *testing.T) {
	token, _ := p.NewEnrollmentToken()
	wire, _ := token.Wire()
	for _, text := range []string{wire, wire + "\n", wire + "\r\n", wire + "\n\n", " " + wire, wire + " ", strings.Repeat("x", 129), ""} {
		input := &countedInput{Reader: strings.NewReader(text)}
		got, err := readEnrollmentToken(context.Background(), input)
		valid := text == wire || text == wire+"\n"
		if (err == nil) != valid || valid && got != token || input.closed != 1 {
			t.Fatal("stdin token length/EOF/close boundary", err)
		}
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := readEnrollmentToken(ctx, reader); done <- err }()
	cancel()
	if err := appResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("stdin did not inherit native cancellation", err)
	}
}
