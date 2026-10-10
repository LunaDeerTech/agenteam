package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/config"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
)

// Only the stdin tail is controlled. executeInput, the default app, native
// Client, private file and lock all remain real. Returning from Close does not
// settle this Read; cancellation cannot be mistaken for owner completion.
type cliHeldEnrollment struct {
	entered, closed, release, returned chan struct{}
	readOnce, closeOnce, releaseOnce   sync.Once
}

func (r *cliHeldEnrollment) Read([]byte) (int, error) {
	r.readOnce.Do(func() { close(r.entered) })
	<-r.release
	close(r.returned)
	return 0, io.EOF
}
func (r *cliHeldEnrollment) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}
func (r *cliHeldEnrollment) finish() { r.releaseOnce.Do(func() { close(r.release) }) }

type cliLifecycleLog struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	stopping chan struct{}
	once     sync.Once
}

func (w *cliLifecycleLog) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.buffer.Write(b)
	var event map[string]any
	if json.Unmarshal(bytes.TrimSpace(b), &event) == nil && event["event"] == "lifecycle" && event["phase"] == "stopping" {
		w.once.Do(func() { close(w.stopping) })
	}
	return len(b), nil
}
func (w *cliLifecycleLog) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func cliOwnerAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("original CLI owner phase was not observed")
	}
}

func cliOwnerLock(t *testing.T, path string, locked bool) {
	t.Helper()
	file, err := identity.Open(path)
	if file != nil && file.Close() != nil {
		t.Fatal("CLI ownership witness failed to close")
	}
	if locked && !errors.Is(err, identity.ErrLocked) || !locked && err != nil {
		t.Fatal("CLI private lock disagrees with original owner lifetime")
	}
}

// The signal channel is the default CLI's existing input, not an OS signal
// delivery claim. The held cases must report failure before their real read
// returns; only the released case can report drained and release its lock.
func TestRunnerCLIOriginalOwnerLifecycle(t *testing.T) {
	for _, mode := range []string{"released_before_deadline", "second_signal_with_held_read", "deadline_with_held_read"} {
		t.Run(mode, func(t *testing.T) {
			base, env, path := cliEnvironment(t)
			timeout := "5s"
			if mode == "deadline_with_held_read" {
				timeout = "100ms"
			}
			lookup := func(key string) (string, bool) {
				if key == config.Prefix+"SHUTDOWN_TIMEOUT" {
					return timeout, true
				}
				return base(key)
			}
			env = append(env, config.Prefix+"SHUTDOWN_TIMEOUT="+timeout)
			input := &cliHeldEnrollment{entered: make(chan struct{}), closed: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
			logs := &cliLifecycleLog{stopping: make(chan struct{})}
			var stdout bytes.Buffer
			signals := make(chan os.Signal, 2)
			done := make(chan int, 1)
			go func() { done <- executeInput([]string{"--enroll"}, lookup, env, input, &stdout, logs, signals) }()
			// Every failure path first releases the explicit input double and
			// then waits for the actual CLI call; no background owner is ignored.
			joined := false
			t.Cleanup(func() {
				input.finish()
				if !joined {
					select {
					case signals <- syscall.SIGTERM:
					default:
					}
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("original CLI call did not join during test cleanup")
					}
				}
			})
			cliOwnerAwait(t, input.entered)
			cliOwnerLock(t, path, true)
			started := time.Now()
			signals <- syscall.SIGTERM
			cliOwnerAwait(t, input.closed)
			cliOwnerAwait(t, logs.stopping)
			select {
			case <-done:
				t.Fatal("Close returned while the original read remained held, but CLI claimed completion")
			default:
			}
			cliOwnerLock(t, path, true)
			switch mode {
			case "released_before_deadline":
				input.finish()
			case "second_signal_with_held_read":
				signals <- syscall.SIGINT
			}
			var code int
			select {
			case code = <-done:
				joined = true
			case <-time.After(3 * time.Second):
				t.Fatal("second signal/original deadline failed to bound the default CLI call")
			}
			text := logs.text()
			if stdout.Len() != 0 || strings.Contains(text, path) || strings.Contains(text, `"connected":true`) || strings.Contains(text, `"ready":true`) {
				t.Fatal("CLI shutdown leaked private configuration or fabricated readiness")
			}
			if mode == "released_before_deadline" {
				cliOwnerAwait(t, input.returned)
				if code != 0 || !strings.Contains(text, `"outcome":"drained"`) || strings.Contains(text, `"outcome":"forced"`) {
					t.Fatal("actual read/Client return did not produce a clean default CLI result")
				}
				cliOwnerLock(t, path, false)
				return
			}
			if code != 1 || !strings.Contains(text, `"code":"SHUTDOWN_TIMEOUT"`) || !strings.Contains(text, `"outcome":"forced"`) || strings.Contains(text, `"outcome":"drained"`) {
				t.Fatal("unjoined enrollment owner was not reported as failed shutdown")
			}
			// The 5s configured drain cannot explain an exit within this bound:
			// the second signal must have selected the original 1s force tail.
			if elapsed := time.Since(started); elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
				t.Fatal("held shutdown skipped or refreshed the existing force allowance")
			}
			select {
			case <-input.returned:
				t.Fatal("held original read was silently retired")
			default:
			}
			cliOwnerLock(t, path, true)
			input.finish()
			cliOwnerAwait(t, input.returned)
			// A failed root is not retroactively called joined. Release only
			// permits the original Client's file owner to complete its tail.
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for {
				file, err := identity.Open(path)
				if err == nil {
					if file.Close() != nil {
						t.Fatal("released CLI lock witness did not close")
					}
					break
				}
				if !errors.Is(err, identity.ErrLocked) {
					t.Fatal("original CLI identity became unreadable after release")
				}
				select {
				case <-deadline.C:
					t.Fatal("original Client did not release its identity after the actual read")
				case <-tick.C:
				}
			}
		})
	}
}
