package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle barrier timed out")
	}
}
func TestStopBudgetSignalsAndConcurrency(t *testing.T) {
	signals := make(chan os.Signal, 2)
	c := New(context.Background(), signals, 10*time.Second)
	defer c.Close()
	signals <- syscall.SIGTERM
	await(t, c.StopContext().Done())
	drain, cancel := c.DrainContext()
	defer cancel()
	deadline, _ := drain.Deadline()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(c.Stop)
	}
	wg.Wait()
	drain2, cancel2 := c.DrainContext()
	defer cancel2()
	deadline2, _ := drain2.Deadline()
	if deadline != deadline2 || drain.Err() != nil || c.Forced() {
		t.Fatal("stop reset budget, cancelled drain, or escalated")
	}
	signals <- syscall.SIGINT
	await(t, c.ForceDone())
	if !c.Forced() || drain.Err() != nil {
		t.Fatal("force signal state is not distinct from drain context")
	}
}
func TestParentCancellationAndClosedSignals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal)
	close(signals)
	c := New(ctx, signals, time.Second)
	cancel()
	await(t, c.StopContext().Done())
	if c.Forced() {
		t.Fatal("closed channel or context cancellation forced stop")
	}
	c.Close()
	c.Close()
	c2 := New(ctx, nil, time.Second)
	if !c2.Stopping() {
		t.Fatal("already cancelled startup was allowed")
	}
	c2.Close()
}

type countedCloser struct{ count atomic.Int32 }

func (c *countedCloser) Close() error { c.count.Add(1); return nil }
func TestResourceRegistrationRacesWithClose(t *testing.T) {
	var resources Closers
	items := make([]countedCloser, 100)
	var wg sync.WaitGroup
	for i := range items {
		wg.Go(func() { resources.Add(&items[i]) })
	}
	for range 20 {
		wg.Go(resources.Close)
	}
	wg.Wait()
	resources.Close()
	for i := range items {
		if items[i].count.Load() != 1 {
			t.Fatalf("resource %d closed %d times", i, items[i].count.Load())
		}
	}
}

type rawCause string

func (e rawCause) Error() string { return string(e) }
func TestFailureNeverFormatsItsCause(t *testing.T) {
	const secret = "credential-SENTINEL"
	cause := rawCause(secret)
	failure := NewFailure(ListenFailed, cause)
	if !errors.Is(failure, cause) || CodeOf(fmt.Errorf("safe: %w", failure)) != ListenFailed {
		t.Fatal("cause identity lost")
	}
	for _, value := range []any{failure, *failure, struct{ err error }{failure}, struct{ failure Failure }{*failure}} {
		for _, format := range []string{"%s", "%q", "%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, value), secret) {
				t.Fatal("raw cause formatted")
			}
		}
		b, err := json.Marshal(value)
		if err != nil || bytes.Contains(b, []byte(secret)) {
			t.Fatal("raw cause serialized")
		}
		var log bytes.Buffer
		slog.New(slog.NewTextHandler(&log, nil)).Error("test", "failure", value)
		if strings.Contains(log.String(), secret) {
			t.Fatal("raw cause logged")
		}
	}
}
