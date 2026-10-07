package projecthttp

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func requireUpdateNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_PROJECT_OWNER_UPDATE_NATIVE") != "1" {
		t.Skip("requires explicitly granted Project Owner update native window")
	}
}
func updateNativeSend(t *testing.T, w io.Writer, address, method, path, body string) {
	t.Helper()
	if _, err := io.WriteString(w, method+" "+path+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: original-key\r\nContent-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body); err != nil {
		t.Fatal(err)
	}
}
func TestProjectOwnerUpdateNativeKeepalive(t *testing.T) {
	requireUpdateNative(t)
	h, _, s := updateFixture()
	var eof, closes, requests atomic.Int32
	address, results, connections := nativeReadListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := time.Second
		if requests.Add(1) == 1 {
			limit = 120 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(r.Context(), limit)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = nativeReadBody{ReadCloser: r.Body, eof: &eof, closes: &closes}
		h.ServeHTTP(w, r)
	}), false)
	conn := nativeReadDial(t, address, false)
	reader := bufio.NewReader(conn)
	for i, tc := range []struct{ method, path, body string }{{"PATCH", httpDetailPath, updateTestBody}, {"POST", httpDetailPath + updateLookupSuffix, `{"command":"update"}`}, {"PATCH", httpDetailPath, updateTestBody}} {
		if i == 1 {
			<-time.After(200 * time.Millisecond)
		}
		updateNativeSend(t, conn, address, tc.method, tc.path, tc.body)
		response, err := http.ReadResponse(reader, &http.Request{Method: tc.method})
		if err != nil {
			t.Fatal(err)
		}
		raw, e := io.ReadAll(response.Body)
		ce := response.Body.Close()
		if e != nil || ce != nil || response.StatusCode != 200 || int64(len(raw)) != response.ContentLength || response.Header.Get("X-Request-ID") == "" || nativeReadTerminal(t, results).aborted {
			t.Fatal("incomplete native result", e, ce)
		}
	}
	if connections.Load() != 1 || closes.Load() != 3 || eof.Load() != 3 || s.updates.Load() != 2 || s.lookups.Load() != 1 {
		t.Fatal("keepalive/actual input completion")
	}
}
func TestProjectOwnerUpdateNativeBodyDeadline(t *testing.T) {
	requireUpdateNative(t)
	for _, tc := range []struct {
		method, path string
		limit        time.Duration
	}{{"PATCH", httpDetailPath, updateBudget}, {"POST", httpDetailPath + updateLookupSuffix, readBudget}, {"PATCH", httpDetailPath, 120 * time.Millisecond}} {
		t.Run(tc.method+tc.limit.String(), func(t *testing.T) {
			h, _, s := updateFixture()
			var eof, closes atomic.Int32
			address, results, _ := nativeReadListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.limit < readBudget {
					ctx, cancel := context.WithTimeout(r.Context(), tc.limit)
					defer cancel()
					r = r.WithContext(ctx)
				}
				r.Body = nativeReadBody{ReadCloser: r.Body, eof: &eof, closes: &closes}
				h.ServeHTTP(w, r)
			}), false)
			conn := nativeReadDial(t, address, false)
			if err := conn.SetDeadline(time.Now().Add(38 * time.Second)); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if _, err := io.WriteString(conn, tc.method+" "+tc.path+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: original-key\r\nContent-Length: 100\r\n\r\n{"); err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(io.LimitReader(conn, 8192))
			if err != nil {
				t.Fatal("native deadline EOF", err)
			}
			terminal := nativeReadTerminal(t, results)
			elapsed := time.Since(started)
			if !terminal.aborted || len(raw) != 0 || elapsed < tc.limit*3/4 || elapsed > tc.limit+time.Second || closes.Load() != 1 || s.updates.Load()+s.lookups.Load() != 0 {
				t.Fatal("slow body deadline/ownership", elapsed, len(raw), closes.Load())
			}
		})
	}
}
func TestProjectOwnerUpdateNativeWriteAndClose(t *testing.T) {
	requireUpdateNative(t)
	for _, mode := range []string{"short_write", "write_error", "flush_error", "close_error"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _ := updateFixture()
			var eof, closes atomic.Int32
			address, results, _ := nativeReadListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), time.Second)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = nativeReadBody{ReadCloser: r.Body, eof: &eof, closes: &closes, closeError: mode == "close_error"}
				h.ServeHTTP(&nativeReadFailureWriter{ResponseWriter: w, mode: mode}, r)
			}), false)
			conn := nativeReadDial(t, address, false)
			updateNativeSend(t, conn, address, "PATCH", httpDetailPath, updateTestBody)
			if !nativeReadTerminal(t, results).aborted || closes.Load() != 1 {
				t.Fatal("actual abort/close")
			}
			raw, err := io.ReadAll(io.LimitReader(conn, updateBodyLimit+4096))
			if err != nil || bytes.Contains(raw, []byte("application/problem+json")) || mode == "close_error" && len(raw) != 0 {
				t.Fatal("native unsafe terminal", err)
			}
			if strings.Contains(string(raw), "private") {
				t.Fatal("panic leaked")
			}
		})
	}
}
