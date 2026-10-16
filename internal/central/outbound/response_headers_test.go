package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

func TestModelResponseHeaderBudgetEligibility(t *testing.T) {
	call := CallContext{data: func() callData { return callData{} }}
	consumers := []ac.Consumer{ac.Model, ac.MCP, ac.Runner, ac.ObjectStorage, ac.SystemConsumer}
	for _, consumer := range consumers {
		for _, streaming := range []bool{false, true} {
			profile, err := NewProfile(ProfileOptions{Consumer: consumer, Streaming: streaming, Context: call})
			if err != nil || profile.data().Limits.ResponseHeaders != 15*time.Second {
				t.Fatalf("default response-header budget changed for %s (streaming=%t)", consumer, streaming)
			}
		}
	}
	for _, tc := range []struct {
		name      string
		consumer  ac.Consumer
		streaming bool
		headers   time.Duration
		overall   time.Duration
		wantValid bool
	}{
		{"model_16_seconds", ac.Model, false, 16 * time.Second, 25 * time.Second, true},
		{"model_60_seconds", ac.Model, false, 60 * time.Second, 120 * time.Second, true},
		{"model_equal_overall", ac.Model, false, 60 * time.Second, 60 * time.Second, true},
		{"model_above_60", ac.Model, false, 60*time.Second + time.Nanosecond, 120 * time.Second, false},
		{"model_above_overall", ac.Model, false, 20 * time.Second, 19 * time.Second, false},
		{"model_implicit_overall", ac.Model, false, 20 * time.Second, 0, false},
		{"model_streaming", ac.Model, true, 20 * time.Second, 25 * time.Second, false},
		{"mcp", ac.MCP, false, 20 * time.Second, 25 * time.Second, false},
		{"runner", ac.Runner, false, 20 * time.Second, 25 * time.Second, false},
		{"object_storage", ac.ObjectStorage, false, 20 * time.Second, 25 * time.Second, false},
		{"system", ac.SystemConsumer, false, 20 * time.Second, 25 * time.Second, false},
		{"model_shorter", ac.Model, false, 2 * time.Second, 25 * time.Second, true},
		{"model_original_cap", ac.Model, false, 15 * time.Second, 0, true},
		{"streaming_shorter", ac.Model, true, 2 * time.Second, 25 * time.Second, true},
		{"other_shorter", ac.MCP, false, 2 * time.Second, 25 * time.Second, true},
		{"original_short_overall", ac.Model, false, time.Second, 100 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, err := NewProfile(ProfileOptions{Consumer: tc.consumer, Streaming: tc.streaming, Context: call, Limits: Limits{ResponseHeaders: tc.headers, Overall: tc.overall}})
			if (err == nil) != tc.wantValid {
				t.Fatalf("profile validity = %t; want %t", err == nil, tc.wantValid)
			}
			if err == nil && profile.data().Limits.ResponseHeaders != tc.headers {
				t.Fatal("explicit response-header budget was changed")
			}
		})
	}
	profile, err := NewSMTPProfile(call, Limits{})
	if err != nil || profile.data().Limits.ResponseHeaders != 15*time.Second {
		t.Fatal("SMTP default response-header limit changed")
	}
	if _, err := NewSMTPProfile(call, Limits{ResponseHeaders: 20 * time.Second, Overall: 25 * time.Second}); err == nil {
		t.Fatal("Model response-header extension escaped into SMTP limits")
	}
}

func TestResponseHeaderPhaseClosedSafeProjection(t *testing.T) {
	for _, phase := range []NetworkPhase{PhaseResponseHeaders, NetworkPhase("phase-private-canary")} {
		failure := networkError(Decision{Consumer: ac.Model, Origin: "https://api.example:443", Sent: true, Phase: phase}, ac.Timeout, errors.New("cause-private-canary"))
		want := NetworkPhase("")
		if phase == PhaseResponseHeaders {
			want = PhaseResponseHeaders
		}
		if failure.Decision().Phase != want {
			t.Fatal("network error did not constrain its phase to the closed safe set")
		}
		proxyTestSafeFormatting(t, failure)
		proxyTestSafeFormatting(t, struct{ failure error }{failure})
	}
}

func TestModelResponseHeaderPhaseExcludesConnectFailures(t *testing.T) {
	_, trust := proxyTestCertificate(t, "api.example")
	closed := make(chan struct{})
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		defer close(closed)
		if !proxyTestReadConnect(t, conn) {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
		_, _ = io.Copy(io.Discard, conn)
	})
	resolver := &proxyTestResolver{}
	client, _ := proxyTestClient(t, trust, resolver, gateway)
	profile := proxyTestProfile(t, false, Limits{ResponseHeaders: 20 * time.Second, Overall: 25 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := client.Do(ctx, proxyTestRequest(t), profile)
	if err == nil || SafeNetworkError(err).Decision().Phase != "" || SafeNetworkError(err).Decision().Sent {
		t.Fatal("CONNECT failure was projected as an origin response-header failure")
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("CONNECT failure did not close the original proxy socket")
	}
	if gateway.accepts.Load() != 1 || resolver.calls.Load() != 1 {
		t.Fatal("CONNECT failure retried or did not resolve the target exactly once")
	}
	responseHeadersTestDrain(t, client)
}

// Both requests use real task-owned sockets and target TLS. Their 16-second
// response delay runs concurrently so the old default and the Model extension
// are compared in one bounded round, rather than through a synthetic clock.
func TestModelResponseHeadersDefaultTimeoutAndExplicitSuccess(t *testing.T) {
	certificate, trust := proxyTestCertificate(t, "api.example")
	closed := make(chan string, 2)
	var requests atomic.Int32
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		// The shared gateway helper normally protects short protocol tests with
		// a 5s deadline. This scenario deliberately waits longer than 15s.
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		if !proxyTestReadConnect(t, conn) {
			return
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		target := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		request, err := http.ReadRequest(bufio.NewReader(target))
		if err != nil {
			_ = target.Close()
			t.Error("delayed-response target did not receive its request")
			return
		}
		request.Body.Close()
		requests.Add(1)
		if request.URL.Path != "/default" && request.URL.Path != "/extended" {
			t.Error("unexpected delayed-response path")
		}
		peerClosed := make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, target)
			close(peerClosed)
		}()
		defer func() {
			_ = target.Close()
			<-peerClosed
			closed <- request.URL.Path
		}()
		timer := time.NewTimer(16 * time.Second)
		defer timer.Stop()
		select {
		case <-peerClosed:
			return
		case <-timer.C:
			_, _ = io.WriteString(target, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		}
	})
	resolver := &proxyTestResolver{}
	client, _ := proxyTestClient(t, trust, resolver, gateway)
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
	defer cancel()
	type outcome struct {
		name     string
		response Response
		body     string
		err      error
		elapsed  time.Duration
	}
	results := make(chan outcome, 2)
	for _, name := range []string{"default", "extended"} {
		limits := Limits{Overall: 25 * time.Second}
		if name == "extended" {
			limits.ResponseHeaders = 20 * time.Second
		}
		profile := proxyTestProfile(t, false, limits)
		request, err := http.NewRequest("GET", "https://api.example/"+name, nil)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			started := time.Now()
			response, err := client.Do(ctx, request, profile)
			result := outcome{name: name, response: response, err: err}
			if err == nil {
				body, readErr := io.ReadAll(response.Body())
				response.Close()
				result.body, result.err = string(body), readErr
			}
			result.elapsed = time.Since(started)
			results <- result
		}()
	}
	for range 2 {
		select {
		case result := <-results:
			if result.name == "default" {
				if result.err == nil {
					t.Fatal("default response-header budget accepted a 16-second wait")
				}
				decision := SafeNetworkError(result.err).Decision()
				if decision.Reason != ac.Timeout || !decision.Sent || decision.Phase != PhaseResponseHeaders {
					t.Fatalf("wrong default header-timeout projection: %+v", decision)
				}
				if result.elapsed < 14*time.Second || result.elapsed > 19*time.Second {
					t.Fatalf("default header timeout did not retain the 15s budget: %v", result.elapsed)
				}
			} else {
				if result.err != nil || result.response.StatusCode() != http.StatusOK || result.body != "ok" || !result.response.Decision().Sent {
					t.Fatalf("explicit Model header budget did not deliver the delayed response: %v", result.err)
				}
				if result.elapsed < 15*time.Second || result.elapsed >= 23*time.Second {
					t.Fatalf("extended request did not complete within its original budget: %v", result.elapsed)
				}
			}
		case <-ctx.Done():
			t.Fatal("delayed-response callers did not finish within the test budget")
		}
	}
	seenClosed := map[string]bool{}
	for range 2 {
		select {
		case name := <-closed:
			seenClosed[name] = true
		case <-ctx.Done():
			t.Fatal("delayed-response gateway sockets did not join")
		}
	}
	if !seenClosed["/default"] || !seenClosed["/extended"] || requests.Load() != 2 || gateway.accepts.Load() != 2 || resolver.calls.Load() != 2 {
		t.Fatal("delayed response lost socket ownership or retried its sent request")
	}
	responseHeadersTestDrain(t, client)
}

func TestModelResponseHeadersRetainShorterOperationDeadline(t *testing.T) {
	for _, source := range []string{"parent_context", "overall"} {
		t.Run(source, func(t *testing.T) {
			certificate, trust := proxyTestCertificate(t, "api.example")
			ready, closed := make(chan struct{}), make(chan struct{})
			gateway := proxyTestStartGateway(t, func(conn net.Conn) {
				defer close(closed)
				if !proxyTestReadConnect(t, conn) {
					return
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				target := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
				defer target.Close()
				reader := bufio.NewReader(target)
				// Establish the owned tunnel and target TLS before measuring
				// the existing 100ms response-wait deadline on its next use.
				warmup, err := http.ReadRequest(reader)
				if err != nil {
					t.Error("warmup did not reach origin HTTP")
					return
				}
				warmup.Body.Close()
				if _, err = io.WriteString(target, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"); err != nil {
					return
				}
				request, err := http.ReadRequest(reader)
				if err != nil {
					t.Error("short-deadline request did not reach origin HTTP")
					return
				}
				request.Body.Close()
				close(ready)
				_, _ = io.Copy(io.Discard, target)
			})
			resolver := &proxyTestResolver{}
			client, _ := proxyTestClient(t, trust, resolver, gateway)
			warmup, err := client.Do(context.Background(), proxyTestRequest(t), proxyTestProfile(t, false, Limits{}))
			if err != nil {
				t.Fatal("warmup failed:", err)
			}
			if _, err = io.ReadAll(warmup.Body()); err != nil {
				warmup.Close()
				t.Fatal("warmup response failed:", err)
			}
			warmup.Close()
			limits := Limits{ResponseHeaders: 20 * time.Second, Overall: 25 * time.Second}
			parentLimit := 100 * time.Millisecond
			if source == "overall" {
				limits = Limits{Overall: 100 * time.Millisecond}
				parentLimit = 2 * time.Second
			}
			profile := proxyTestProfile(t, false, limits)
			ctx, cancel := context.WithTimeout(context.Background(), parentLimit)
			defer cancel()
			request := proxyTestRequest(t)
			result := make(chan error, 1)
			started := time.Now()
			go func() { _, err := client.Do(ctx, request, profile); result <- err }()
			select {
			case <-ready:
			case <-time.After(time.Second):
				t.Fatal("request did not reach the response-header barrier")
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("shorter operation deadline was ignored")
				}
				decision := SafeNetworkError(err).Decision()
				if decision.Reason != ac.Timeout || !decision.Sent || decision.Phase != PhaseResponseHeaders {
					t.Fatalf("wrong short-deadline decision: %+v", decision)
				}
				if elapsed := time.Since(started); elapsed > time.Second {
					t.Fatalf("header budget widened the 100ms operation deadline: %v", elapsed)
				}
			case <-time.After(time.Second):
				t.Fatal("short operation deadline did not release the caller")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("short operation deadline did not close the original socket")
			}
			if gateway.accepts.Load() != 1 || resolver.calls.Load() != 2 {
				t.Fatal("timed-out sent request retried or escaped its proxy")
			}
			responseHeadersTestDrain(t, client)
		})
	}
}

func responseHeadersTestDrain(t *testing.T, client *Client) {
	t.Helper()
	client.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Drain(ctx); err != nil {
		t.Fatal("response-header operation did not drain:", err)
	}
}
