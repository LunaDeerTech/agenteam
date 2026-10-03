package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/internal/runner/config"
)

type eventWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	started chan struct{}
}

func (w *eventWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.buffer.Write(b)
	var event map[string]any
	if json.Unmarshal(b, &event) == nil && event["phase"] == "unconnected" {
		close(w.started)
	}
	return len(b), nil
}
func TestUnconnectedRunnerStopsOnSignalsOrContext(t *testing.T) {
	for _, mode := range []string{"SIGINT", "SIGTERM", "context", "already_cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "already_cancelled" {
				cancel()
			}
			cfg, err := config.Load(func(string) (string, bool) { return "", false }, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := &eventWriter{started: make(chan struct{})}
			logger, _ := logging.New(logging.Runner, slog.LevelInfo, output)
			signals := make(chan os.Signal, 2)
			done := make(chan error, 1)
			go func() { done <- Run(ctx, cfg, logger, signals) }()
			if mode != "already_cancelled" {
				select {
				case <-output.started:
				case <-time.After(5 * time.Second):
					t.Fatal("Runner did not enter unconnected state")
				}
			}
			switch mode {
			case "SIGINT":
				signals <- syscall.SIGINT
			case "SIGTERM":
				signals <- syscall.SIGTERM
			default:
				cancel()
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Runner failed to stop")
			}
			logs := output.buffer.String()
			if strings.Contains(logs, `"ready":true`) || strings.Contains(logs, `"connected":true`) || strings.Contains(logs, `"authenticated":true`) || !strings.Contains(logs, `"outcome":"drained"`) {
				t.Fatal("Runner misrepresented its state")
			}
		})
	}
}
