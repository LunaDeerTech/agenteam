package projectvariablehttp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Native TCP ownership with controlled domain/authorization ports. The PG
// boundary test separately proves real current-Session authorization; neither
// this test nor that fixture claims default production root acceptance.
func TestSecretHTTPNativeTransport(t *testing.T) {
	if os.Getenv("AGENTEAM_SECRET_VARIABLE_HTTP_NATIVE") != "1" {
		t.Skip("requires an explicitly granted Secret HTTP native window")
	}
	t.Run("deadlines-and-keepalive", func(t *testing.T) {
		h, _, ports := secretTestHandler(t)
		var closes, eof atomic.Int32
		address, results, count := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Test-Early") == "1" {
				ctx, cancel := context.WithTimeout(r.Context(), 150*time.Millisecond)
				defer cancel()
				r = r.WithContext(ctx)
			}
			r.Body = nativeBody{ReadCloser: r.Body, closes: &closes, eof: &eof}
			h.ServeHTTP(w, r)
		}), false)
		for _, entry := range []struct {
			method, path string
			budget       time.Duration
			early        bool
		}{
			{"GET", "/secret-variables", readBudget, false},
			{"POST", "/secret-variables/commands/lookup", readBudget, false},
			{"POST", "/secret-variables", mutationBudget, false},
			{"POST", "/secret-variables", 150 * time.Millisecond, true},
		} {
			conn := nativeDial(t, address)
			before := closes.Load()
			extra := ""
			if entry.early {
				extra = "X-Test-Early: 1\r\n"
			}
			started := time.Now()
			_, err := io.WriteString(conn, entry.method+" "+testPath(entry.path)+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: native-original\r\n"+extra+"Content-Length: 100\r\n\r\n{")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(conn)
			terminal := nativeTerminal(t, results)
			elapsed := time.Since(started)
			if err != nil || !terminal.aborted || len(raw) != 0 || elapsed < entry.budget*3/4 || elapsed > entry.budget+2*time.Second || closes.Load() != before+1 || ports.calls != 0 {
				t.Fatal("actual Secret deadline/EOF/Body ownership", entry.method, entry.path, entry.early, err, elapsed, len(raw), closes.Load()-before, ports.calls)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
		}
		beforeConns, beforeCloses := count.Load(), closes.Load()
		conn := nativeDial(t, address)
		reader := bufio.NewReader(conn)
		read := func() {
			t.Helper()
			response, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(response.Body)
			closed := response.Body.Close()
			if err != nil || closed != nil || response.StatusCode != 200 || len(raw) == 0 || nativeTerminal(t, results).aborted {
				t.Fatal("complete safe response on exact native connection", err, closed)
			}
		}
		nativeSend(t, conn, address, "GET", "/secret-variables", "", false)
		read()
		// The second request crosses the first request's installed deadline.
		timer := time.NewTimer(readBudget + 150*time.Millisecond)
		defer timer.Stop()
		<-timer.C
		nativeSend(t, conn, address, "GET", "/secret-variables", "", true)
		read()
		if count.Load() != beforeConns+1 || closes.Load() != beforeCloses+2 || ports.calls != 2 {
			t.Fatal("native connection was replaced or body owner did not close")
		}
	})
	t.Run("backpressure-and-disconnect-join", func(t *testing.T) {
		h, _, ports := secretTestHandler(t)
		items := make([]c.SecretVariable, 100)
		for n := range items {
			fields := secretTestValue(t, 1).Fields()
			fields.ID, fields.Name = testID[i.ProjectVariable](100+n), fmt.Sprintf("N%03d", n)
			fields.Description = strings.Repeat("\t", 4096)
			var err error
			items[n], err = c.NewSecretVariable(fields)
			if err != nil {
				t.Fatal(err)
			}
		}
		ports.page = f.Page[c.SecretVariable]{Items: items}
		writeEntered := make(chan struct{})
		address, results, _ := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				w = &nativeWriteObserver{ResponseWriter: w, entered: writeEntered}
			}
			h.ServeHTTP(w, r)
		}), true)
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unpark := func() { once.Do(func() { close(release) }) }
		// Registered after listener cleanup: release the domain call first on
		// every failure path, then let nativeListener join real handlers.
		t.Cleanup(unpark)
		conn := nativeDial(t, address)
		if err := conn.SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		nativeSend(t, conn, address, "GET", "/secret-variables?limit=100", "", true)
		joined(t, writeEntered)
		terminal := nativeTerminal(t, results)
		elapsed := time.Since(started)
		if !terminal.aborted || elapsed < readBudget*3/4 || elapsed > readBudget+2*time.Second || ports.calls != 1 {
			t.Fatal("blocked Secret response Write did not actually expire", elapsed)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		ports.result = secretTestReceipt(t, c.SecretCreateCommand, true, 1)
		ports.before = func(ctx context.Context) { close(entered); <-ctx.Done(); close(cancelled); <-release }
		conn = nativeDial(t, address)
		nativeSend(t, conn, address, "POST", "/secret-variables", secretCreateBody("native-owned-secret-canary"), false)
		joined(t, entered)
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		joined(t, cancelled)
		select {
		case <-results:
			t.Fatal("HTTP returned before the actual Secret call")
		default:
		}
		if ports.create == nil || ports.create.UseValue(func(value []byte) error {
			if string(value) != "native-owned-secret-canary" {
				return fmt.Errorf("owned material changed")
			}
			return nil
		}) != nil {
			t.Fatal("caller destroyed material while domain call still owned it")
		}
		unpark()
		if !nativeTerminal(t, results).aborted || ports.calls != 2 || ports.create.UseValue(func([]byte) error { return nil }) == nil {
			t.Fatal("disconnect did not join and destroy material before terminal")
		}
	})
}
