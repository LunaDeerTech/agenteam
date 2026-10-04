package object

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func testStorageConfig(t *testing.T, endpoint string, changes map[string]string) StorageConfig {
	t.Helper()
	values := map[string]string{"ENDPOINT": endpoint, "BUCKET": "owned-test", "ACCESS_KEY": "access-sensitive-canary", "SECRET_KEY": "secret-sensitive-canary", "TLS_MODE": "disable"}
	for k, v := range changes {
		values[k] = v
	}
	c, err := LoadStorageConfig(func(name string) (string, bool) {
		if !strings.HasPrefix(name, EnvironmentPrefix) {
			t.Fatal("unapproved configuration source")
		}
		v, ok := values[strings.TrimPrefix(name, EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestStorageConfigNamedInputsAndPrivateProjections(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "must-not-consume-default-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "must-not-consume-default-secret")
	t.Setenv("HTTP_PROXY", "http://invalid-proxy:1111")
	t.Setenv("HTTPS_PROXY", "http://invalid-proxy:1111")
	c := testStorageConfig(t, "http://127.0.0.1:12345", nil)
	b, err := NewBackend(c)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	// Nested unexported fields are deliberately tested: fmt can otherwise walk
	// through a private string even when the outer type implements Formatter.
	values := []any{c, &c, struct{ v StorageConfig }{c}, b, struct{ v Backend }{*b}, failure(foundation.DependencyUnavailable, errors.New("secret-sensitive-canary"))}
	for _, v := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			out := fmt.Sprintf(format, v)
			for _, secret := range []string{"access-sensitive-canary", "secret-sensitive-canary", "127.0.0.1"} {
				if strings.Contains(out, secret) {
					t.Fatal("private formatting leaked")
				}
			}
		}
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("sensitive-canary")) {
			t.Fatal("JSON leaked")
		}
		for _, jsonLog := range []bool{false, true} {
			var out bytes.Buffer
			var handler slog.Handler
			if jsonLog {
				handler = slog.NewJSONHandler(&out, nil)
			} else {
				handler = slog.NewTextHandler(&out, nil)
			}
			slog.New(handler).Info("safe", "object", v)
			if strings.Contains(out.String(), "sensitive-canary") {
				t.Fatal("log leaked")
			}
		}
	}
	if err = json.Unmarshal([]byte(`{}`), &c); err == nil || c.Validate() != nil {
		t.Fatal("configuration deserialization changed live value")
	}
	if err = json.Unmarshal([]byte(`{}`), b); err == nil || b.data == nil {
		t.Fatal("backend deserialization changed live value")
	}
	var s Service
	var spool Spool
	if json.Unmarshal([]byte(`{}`), &s) == nil || json.Unmarshal([]byte(`{}`), &spool) == nil {
		t.Fatal("opaque service accepted JSON")
	}
	for _, change := range []map[string]string{{"ACCESS_KEY": ""}, {"SECRET_KEY": ""}, {"ENDPOINT": "http://user:secret@host"}, {"ENDPOINT": "http://host/prefix"}, {"ENDPOINT": "http://host?token=x"}, {"ENDPOINT": "http://host#x"}, {"ENDPOINT": "http://host:00080"}, {"TLS_MODE": "verify-full"}, {"TLS_MODE": "insecure"}, {"BUCKET": "../bucket"}, {"BUCKET": "127.0.0.1"}, {"CA_FILE": "/nonexistent-fixture-file"}} {
		values := map[string]string{"ENDPOINT": "http://127.0.0.1:12345", "BUCKET": "owned-test", "ACCESS_KEY": "explicit", "SECRET_KEY": "explicit", "TLS_MODE": "disable"}
		for k, v := range change {
			values[k] = v
		}
		if _, err = LoadStorageConfig(func(name string) (string, bool) {
			v, ok := values[strings.TrimPrefix(name, EnvironmentPrefix)]
			return v, ok
		}); err == nil {
			t.Fatal("invalid deployment configuration accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "large-ca")
	if err = os.WriteFile(path, bytes.Repeat([]byte{'x'}, (1<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadStorageConfig(func(name string) (string, bool) {
		values := map[string]string{"ENDPOINT": "https://localhost", "BUCKET": "owned-test", "ACCESS_KEY": "explicit", "SECRET_KEY": "explicit", "CA_FILE": path}
		v, ok := values[strings.TrimPrefix(name, EnvironmentPrefix)]
		return v, ok
	}); err == nil {
		t.Fatal("oversized CA accepted")
	}
}
func TestStorageAdapterCloseCancelsRealHeadersAndRejectsRedirect(t *testing.T) {
	began, joined := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(began); <-r.Context().Done(); close(joined) }))
	defer server.Close()
	b, err := NewBackend(testStorageConfig(t, server.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- b.CheckBucket(context.Background()) }()
	select {
	case <-began:
	case <-time.After(time.Second):
		t.Fatal("real request did not reach socket")
	}
	started := time.Now()
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if err == nil {
			t.Fatal("closed request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("backend request outlived close")
	}
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("real peer did not observe close")
	}
	if time.Since(started) > time.Second {
		t.Fatal("backend close exceeded common force budget")
	}
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL+"/owned-test")
		w.WriteHeader(307)
	}))
	defer redirect.Close()
	b, err = NewBackend(testStorageConfig(t, redirect.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = b.CheckBucket(ctx); err == nil || targetCalls.Load() != 0 {
		t.Fatal("storage redirect followed")
	}
}
func TestStorageAdapterEagerRangeFraming(t *testing.T) {
	for _, scenario := range []struct {
		status                     int
		contentRange, length, body string
		partial                    bool
	}{{200, "", "4", "body", true}, {206, "bytes 0-3/4", "4", "body", false}, {206, "bytes 1-2/4", "2", "od", true}, {200, "", "5", "wrong", false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", scenario.length)
			if scenario.contentRange != "" {
				w.Header().Set("Content-Range", scenario.contentRange)
			}
			w.WriteHeader(scenario.status)
			_, _ = io.WriteString(w, scenario.body)
		}))
		b, err := NewBackend(testStorageConfig(t, server.URL, nil))
		if err != nil {
			t.Fatal(err)
		}
		var selected *oc.ResolvedRange
		if scenario.partial {
			selected = &oc.ResolvedRange{Offset: 0, Length: 2, Total: 4}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		r, err := b.get(ctx, "candidate/example", 4, selected)
		if err == nil {
			_ = r.Close()
			t.Fatal("incorrect upstream range/length accepted")
		}
		cancel()
		_ = b.Close()
		server.Close()
	}
}
