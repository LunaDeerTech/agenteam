package object

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestStorageInitializeBucket(t *testing.T) {
	for _, item := range []struct {
		name, headError, createError, version string
		exists, reserved, success             bool
		puts                                  int32
	}{
		{name: "existing", exists: true, reserved: true, success: true},
		{name: "missing-new-store", reserved: true, success: true, puts: 1},
		{name: "missing-confirmed-store"},
		{name: "access-denied", headError: "AccessDenied", reserved: true},
		{name: "server-error", headError: "InternalError", reserved: true},
		{name: "creation-denied", createError: "AccessDenied", reserved: true, puts: 1},
		{name: "concurrent-owner", createError: "BucketAlreadyOwnedByYou", reserved: true, success: true, puts: 1},
		{name: "another-owner", createError: "BucketAlreadyExists", reserved: true, puts: 1},
		{name: "region-change-rejected", createError: "AuthorizationHeaderMalformed", reserved: true, puts: 1},
		{name: "unsafe-existing", exists: true, reserved: true, version: "Enabled"},
		{name: "unsafe-concurrent", createError: "BucketAlreadyOwnedByYou", reserved: true, version: "Suspended", puts: 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			var exists atomic.Bool
			exists.Store(item.exists)
			var puts, requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/owned-test/" && r.URL.Path != "/owned-test" {
					t.Error("request escaped the configured bucket")
					w.WriteHeader(400)
					return
				}
				reject := func(status int, code string) {
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(status)
					if r.Method != http.MethodHead {
						_, _ = fmt.Fprintf(w, `<Error><Code>%s</Code><Region>us-west-2</Region></Error>`, code)
					}
				}
				switch r.Method {
				case http.MethodHead:
					if item.headError == "AccessDenied" {
						reject(403, item.headError)
					} else if item.headError != "" {
						reject(500, item.headError)
					} else if !exists.Load() {
						reject(404, "NoSuchBucket")
					} else {
						w.WriteHeader(200)
					}
				case http.MethodPut:
					puts.Add(1)
					if r.URL.RawQuery != "" || r.ContentLength > 0 || r.Header.Get("X-Amz-Bucket-Object-Lock-Enabled") != "" || !strings.Contains(r.Header.Get("Authorization"), "/us-east-1/s3/aws4_request") {
						t.Error("creation changed region or bucket settings")
					}
					if item.createError == "" || item.createError == "BucketAlreadyOwnedByYou" {
						exists.Store(true)
					}
					if item.createError != "" {
						status := 409
						if item.createError == "AccessDenied" {
							status = 403
						} else if item.createError == "AuthorizationHeaderMalformed" {
							status = 400
						}
						reject(status, item.createError)
					} else {
						w.WriteHeader(200)
					}
				case http.MethodGet:
					w.Header().Set("Content-Type", "application/xml")
					switch {
					case r.URL.Query().Has("versioning"):
						_, _ = fmt.Fprintf(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>%s</Status></VersioningConfiguration>`, item.version)
					case r.URL.Query().Has("object-lock"):
						reject(404, "ObjectLockConfigurationNotFoundError")
					case r.URL.Query().Has("lifecycle"):
						reject(404, "NoSuchLifecycleConfiguration")
					case r.URL.Query().Has("policy"):
						reject(404, "NoSuchBucketPolicy")
					default:
						t.Error("unexpected bucket operation")
						w.WriteHeader(400)
					}
				default:
					t.Error("unexpected storage mutation")
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			backend, err := NewBackend(testStorageConfig(t, server.URL, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err = backend.initializeBucket(ctx, item.reserved)
			if (err == nil) != item.success || puts.Load() != item.puts || requests.Load() == 0 {
				t.Fatalf("unexpected initialization result: success=%t puts=%d", err == nil, puts.Load())
			}
			// A later health check must never create even a now-missing bucket.
			exists.Store(false)
			if err = backend.CheckBucket(ctx); err == nil || puts.Load() != item.puts {
				t.Fatal("read-only health check repaired missing storage")
			}
		})
	}
}

type bucketIdentityStore struct {
	Store
	entered, returned chan struct{}
	unknown           bool
	attempt           f.ID[f.TransactionAttempt]
}

func (s *bucketIdentityStore) WithinTx(_ context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	close(s.entered)
	<-s.returned
	if s.unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestInitializeBucketWaitsForIdentityTransaction(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown-%t", unknown), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(404) }))
			defer server.Close()
			backend, err := NewBackend(testStorageConfig(t, server.URL, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			store := &bucketIdentityStore{entered: make(chan struct{}), returned: make(chan struct{}), unknown: unknown, attempt: auditID[f.TransactionAttempt](t)}
			state := &serviceState{store: store, backend: backend, operations: map[*operation]bool{}, changed: make(chan struct{})}
			service := &Service{data: func() *serviceState { return state }}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			var release sync.Once
			joined := false
			defer func() {
				release.Do(func() { close(store.returned) })
				cancel()
				if !joined {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("original initialization has not returned")
					}
				}
			}()
			go func() { done <- service.Initialize(ctx) }()
			select {
			case <-store.entered:
			case <-ctx.Done():
				t.Fatal("original identity transaction did not begin")
			}
			state.mu.Lock()
			owned := len(state.operations) == 1
			state.mu.Unlock()
			if !owned || requests.Load() != 0 {
				t.Error("startup crossed the original transaction return")
			}
			release.Do(func() { close(store.returned) })
			select {
			case err = <-done:
				joined = true
			case <-ctx.Done():
				t.Fatal("original startup did not return")
			}
			if err == nil || requests.Load() != 0 || state.initialized || len(state.operations) != 0 || unknown && !hasCode(err, f.CommitUnknown) {
				t.Fatal("unconfirmed identity authorized bucket initialization")
			}
		})
	}
}
