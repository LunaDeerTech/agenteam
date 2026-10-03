package outbound

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSDKStandardURLWrapperAndSafeMapping(t *testing.T) {
	raw := "https://example.com/path-private-canary?token=credential-private-canary"
	err := &url.Error{Op: "Get", URL: raw, Err: networkError(Decision{Consumer: ac.Model, Origin: "https://example.com:443", Sent: true}, ac.TLSFailed, errors.New("tls-raw-private-canary"))}
	if !strings.Contains(err.Error(), "credential-private-canary") {
		t.Fatal("test must observe the standard unsafe wrapper")
	}
	for _, input := range []error{err, errors.New("fallback-private-canary")} {
		safe := SafeNetworkError(input)
		if !errors.Is(safe, input) {
			t.Fatal("explicit cause lost")
		}
		for _, v := range []any{safe, struct{ failure error }{safe}} {
			var b strings.Builder
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
				fmt.Fprintf(&b, verb, v)
			}
			raw, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			b.Write(raw)
			slog.New(slog.NewTextHandler(&b, nil)).Info("probe", "v", v)
			slog.New(slog.NewJSONHandler(&b, nil)).Info("probe", "v", v)
			if strings.Contains(b.String(), "private-canary") {
				t.Fatal("mapped error leaked raw input")
			}
		}
	}
}
func TestCredentialBindingOriginAndLocationBoundary(t *testing.T) {
	origin, _ := ParseOrigin("https://EXAMPLE.com./")
	material, _ := sc.NewSecretMaterial([]byte("credential-private-canary"))
	defer material.Destroy()
	header, _ := HeaderCredential("Authorization", "Bearer ", material)
	query, _ := QueryCredential("key", material)
	binding, e := NewCredentialBinding(origin, header, query)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", "https://example.com:443/path", nil)
	values, e := applyCredentials(req, origin, binding, true)
	if e != nil || req.Header.Get("Authorization") != "Bearer credential-private-canary" || req.URL.Query().Get("key") != "credential-private-canary" {
		t.Fatal("typed binding failed")
	}
	other, _ := ParseOrigin("https://sub.example.com")
	if _, e = applyCredentials(req, other, binding, false); e == nil {
		t.Fatal("subdomain inherited binding")
	}
	for _, location := range []string{"https://other.example/?x=credential-private-canary", "https://other.example/?x=credential%2Dprivate%2Dcanary", "https://other.example/?x=credential%252Dprivate%252Dcanary"} {
		if !locationCarriesMaterial(location, values) {
			t.Fatal("redirect retained bound material")
		}
	}
	for _, v := range []any{binding, header, struct{ binding CredentialBinding }{binding}} {
		out := fmt.Sprintf("%+v %#v", v, v)
		raw, _ := json.Marshal(v)
		if strings.Contains(out+string(raw), "private-canary") {
			t.Fatal("binding implicit leak")
		}
	}
}
func TestRequestValidationAndResponseHeaderBudget(t *testing.T) {
	target, _ := ParseTarget("https://example.com/path")
	limits, _ := normalizeLimits(Limits{}, false)
	p := ProfileOptions{Limits: limits}
	for _, name := range []string{"Connection", "HOST", "Transfer-Encoding", "Proxy-Authorization", "Expect", "Authorization", "Cookie"} {
		r, _ := http.NewRequest("GET", "https://example.com/path", nil)
		r.Header[name] = []string{"private-canary"}
		if prepareRequest(r, target, p) == nil {
			t.Errorf("unsafe header accepted %s", name)
		}
	}
	for _, wire := range []string{"HTTP/1.1 200 OK\r\nX-Long: " + strings.Repeat("a", 65<<10) + "\r\n\r\n", "HTTP/1.1 200 OK\r\n" + strings.Repeat("X: y\r\n", 12000) + "\r\n"} {
		if _, e := readHeaderBlock(bufio.NewReader(strings.NewReader(wire)), 64<<10); !errors.Is(e, errHeaderLimit) {
			t.Fatal("header limit not enforced")
		}
	}
	reader := bufio.NewReader(strings.NewReader("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nBODY"))
	header, e := readHeaderBlock(reader, 64<<10)
	if e != nil || strings.Contains(string(header), "BODY") {
		t.Fatal("header consumed body")
	}
	body, _ := io.ReadAll(reader)
	if string(body) != "BODY" {
		t.Fatal("prefetched body lost")
	}
}

// This unit test uses net.Pipe only to prove the gate/Write ordering. It does
// not claim an allowed loopback HTTP request; real private sockets are covered
// by the separately owned container integration suite.
func TestFirstWriteLinearizesAndSlowWriteReleasesGate(t *testing.T) {
	rule, _ := NewRule("10.20.0.0/16", AllPorts(), true)
	rules, _ := NewRules(rule)
	ps := &policyState{mirror: policy(1, rules), available: true}
	policyService := &PolicyService{data: func() *policyState { return ps }}
	state := &clientState{policy: policyService, connections: map[*pooledConn]bool{}, changed: make(chan struct{})}
	client := &Client{data: func() *clientState { return state }}
	target, _ := ParseTarget("http://10.20.1.1:8080/")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	a := &attempt{ctx: ctx, client: client, target: target, addresses: []netip.Addr{netip.MustParseAddr("10.20.1.1")}}
	conn, peer := net.Pipe()
	defer peer.Close()
	pc := &pooledConn{conn: conn, raw: conn, origin: target.Origin(), peer: a.addresses[0], owner: state}
	state.connections[pc] = true
	defer pc.close()
	writer := &firstWriter{a: a, pc: pc, first: true}
	writeDone := make(chan error, 1)
	go func() { _, e := writer.Write([]byte("GET / HTTP/1.1\r\n\r\n")); writeDone <- e }()
	// Observe the shared gate rather than sleeping to establish the barrier.
	for {
		ps.gate.mu.Lock()
		held := ps.gate.readers == 1
		ps.gate.mu.Unlock()
		if held {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("first write did not acquire gate")
		default:
		}
	}
	start := time.Now()
	release, e := ps.gate.acquire(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	elapsed := time.Since(start)
	release()
	if elapsed < time.Second || elapsed > 3*time.Second {
		t.Fatalf("first write exceeded bounded permit: %s", elapsed)
	}
	if e = <-writeDone; e == nil || a.sent.Load() {
		t.Fatal("blocked zero-byte write reported sent")
	}
}
func TestBoundedRequestBodyCountsActualBytes(t *testing.T) {
	r := &boundedRequestBody{ReadCloser: io.NopCloser(bytes.NewReader([]byte("12345"))), remaining: 4}
	got, e := io.ReadAll(r)
	if string(got) != "1234" || !errors.Is(e, errBodyLimit) {
		t.Fatal("body limit", string(got), e)
	}
}

type countedBody struct {
	io.Reader
	closes int
}

func (b *countedBody) Close() error { b.closes++; return nil }
func TestRequestBodyCloseOwnershipAndNestedLocation(t *testing.T) {
	raw := &countedBody{Reader: strings.NewReader("body")}
	body := ownedBody(raw)
	req, _ := http.NewRequest("POST", "https://example.com", body)
	if err := req.Write(io.Discard); err != nil {
		t.Fatal(err)
	}
	_ = body.Close()
	if raw.closes != 1 {
		t.Fatal("request body closed more than once")
	}
	encoded := "credential-private-canary"
	for range 20 {
		encoded = strings.ReplaceAll(encoded, "-", "%2D")
		encoded = strings.ReplaceAll(encoded, "%", "%25")
	}
	if !locationCarriesMaterial("https://other.example/?token="+encoded, []string{"credential-private-canary"}) {
		t.Fatal("deeply escaped binding forwarded")
	}
}

func TestSerializedHeaderBudgetAcrossEveryWriteBoundary(t *testing.T) {
	header := "POST / HTTP/1.1\r\nHost: example.com\r\nContent-Length: 4\r\n\r\n"
	wire := header + "BODY"
	for cut := 0; cut <= len(wire); cut++ {
		var target bytes.Buffer
		w := &requestHeaderWriter{next: &target, limit: len(header)}
		if _, err := w.Write([]byte(wire[:cut])); err != nil {
			t.Fatal(cut, err)
		}
		if cut < len(header) && target.Len() != 0 {
			t.Fatal("buffered bytes counted/sent before complete header")
		}
		if _, err := w.Write([]byte(wire[cut:])); err != nil || target.String() != wire {
			t.Fatal("split header/body lost", cut, err)
		}
	}
	for _, parts := range [][]string{{header}, {header[:len(header)-2], header[len(header)-2:]}, {header + strings.Repeat("body", 8192)}} {
		var target bytes.Buffer
		w := &requestHeaderWriter{next: &target, limit: len(header) - 1}
		var failure error
		for _, part := range parts {
			_, failure = w.Write([]byte(part))
			if failure != nil {
				break
			}
		}
		if !errors.Is(failure, errHeaderLimit) || target.Len() != 0 || len(w.header) > w.limit+1 {
			t.Fatal("oversized header escaped bounded pre-send buffer")
		}
	}
}

func TestExchangeRejectsGeneratedHeaderOverflowWithZeroPeerBytes(t *testing.T) {
	for _, connectionClose := range []bool{false, true} {
		client, _, target, ctx, cancel := boundaryState(t)
		limits, _ := normalizeLimits(Limits{}, false)
		measure, _ := http.NewRequest("POST", target.URL().String(), strings.NewReader("body"))
		measure.Close = connectionClose
		if err := prepareRequest(measure, target, ProfileOptions{Limits: limits}); err != nil {
			t.Fatal(err)
		}
		var wire bytes.Buffer
		if err := measure.Write(&wire); err != nil {
			t.Fatal(err)
		}
		limits.RequestHeaderBytes = bytes.Index(wire.Bytes(), []byte("\r\n\r\n")) + 3
		a := &attempt{ctx: ctx, client: client, profile: ProfileOptions{Limits: limits}, target: target, addresses: []netip.Addr{netip.MustParseAddr("10.20.1.1")}}
		raw, peer := net.Pipe()
		pc := &pooledConn{conn: raw, raw: raw, reader: bufio.NewReader(raw), origin: target.Origin(), peer: a.addresses[0], owner: client.state()}
		client.state().connections[pc] = true
		received := make(chan []byte, 1)
		go func() { b, _ := io.ReadAll(peer); peer.Close(); received <- b }()
		req, _ := http.NewRequest("POST", target.URL().String(), strings.NewReader("body"))
		req.Close = connectionClose
		if err := prepareRequest(req, target, ProfileOptions{Limits: limits}); err != nil {
			t.Fatal(err)
		}
		_, err := a.exchange(req, pc)
		if err == nil || SafeNetworkError(err).Decision().Reason != ac.ResponseLimit || a.sent.Load() {
			t.Fatal("generated header overflow escaped exchange", err)
		}
		select {
		case b := <-received:
			if len(b) != 0 {
				t.Fatal("peer received bytes before header approval")
			}
		case <-ctx.Done():
			t.Fatal("overflow did not close peer")
		}
		cancel()
	}
}

func TestSMTPCallerTighteningSurvivesTransportDeadlineReset(t *testing.T) {
	raw, peer := net.Pipe()
	defer raw.Close()
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pc := &pooledConn{conn: raw}
	state := &smtpState{pc: pc, op: &operation{ctx: ctx}}
	c := &Conn{data: func() *smtpState { return state }}
	if err := pc.setTransportWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetWriteDeadline(time.Now().Add(40 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := raw.Write([]byte("blocked")); err == nil || time.Since(start) > 250*time.Millisecond {
		t.Fatal("caller tightening did not unblock write")
	}
	if err := pc.setTransportWriteDeadline(time.Now().Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if _, err := raw.Write([]byte("still expired")); err == nil || time.Since(start) > 100*time.Millisecond {
		t.Fatal("transport reset widened shorter caller deadline")
	}
	start = time.Now()
	if _, err := raw.Read(make([]byte, 1)); err == nil || time.Since(start) > 100*time.Millisecond {
		t.Fatal("write cap changed caller read deadline")
	}
}

func boundaryState(t *testing.T) (*Client, *policyState, Target, context.Context, context.CancelFunc) {
	t.Helper()
	rule, err := NewRule("10.20.0.0/16", AllPorts(), true)
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := NewRules(rule)
	ps := &policyState{mirror: policy(1, rules), available: true}
	policyService := &PolicyService{data: func() *policyState { return ps }}
	state := &clientState{policy: policyService, idle: map[string][]*pooledConn{}, connections: map[*pooledConn]bool{}, active: map[*operation]bool{}, changed: make(chan struct{})}
	client := &Client{data: func() *clientState { return state }}
	target, err := ParseTarget("http://10.20.1.1:8080/")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	return client, ps, target, ctx, cancel
}

// Only in-memory net.Pipe peers are used. This probes shutdown ordering after
// stdlib has accepted an unsupported protocol header, not address admission.
func TestUnsupportedProtocolClosesBeforeBodyDrain(t *testing.T) {
	client, _, target, ctx, cancel := boundaryState(t)
	defer cancel()
	limits, _ := normalizeLimits(Limits{ResponseHeaders: 600 * time.Millisecond}, false)
	a := &attempt{ctx: ctx, client: client, profile: ProfileOptions{Limits: limits}, target: target, addresses: []netip.Addr{netip.MustParseAddr("10.20.1.1")}}
	conn, peer := net.Pipe()
	defer peer.Close()
	pc := &pooledConn{conn: conn, raw: conn, reader: bufio.NewReader(conn), origin: target.Origin(), peer: a.addresses[0], owner: client.state()}
	client.state().connections[pc] = true
	defer pc.close()
	ready := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		req, err := http.ReadRequest(bufio.NewReader(peer))
		if err != nil {
			return
		}
		req.Body.Close()
		_, err = io.WriteString(peer, "HTTP/1.2 200 OK\r\nContent-Length: 1\r\n\r\n")
		if err != nil {
			return
		}
		close(ready)
		_, _ = peer.Read(make([]byte, 1))
	}()
	result := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest("GET", target.URL().String(), nil)
		_, err := a.exchange(req, pc)
		result <- err
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("header not delivered")
	}
	start := time.Now()
	err := <-result
	elapsed := time.Since(start)
	if err == nil || SafeNetworkError(err).Decision().Reason != ac.InvalidTarget {
		t.Fatalf("wrong rejection: %v", err)
	}
	<-serverDone
	if elapsed > 200*time.Millisecond {
		t.Fatalf("unsupported response waited for body drain: %v (header budget 600ms)", elapsed)
	}
}

type boundaryWriteBarrier struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *boundaryWriteBarrier) Write(b []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(b)
}

// net.Conn allows concurrent deadline setters. A public SMTP deadline must not
// lengthen the already-held first-write permit beyond the platform's 2s cap.
func TestSMTPDeadlineCannotExtendFirstWrite(t *testing.T) {
	for _, setter := range []string{"SetDeadline", "SetWriteDeadline"} {
		t.Run(setter, func(t *testing.T) {
			client, ps, target, ctx, cancel := boundaryState(t)
			defer cancel()
			raw, peer := net.Pipe()
			defer peer.Close()
			observed := &boundaryWriteBarrier{Conn: raw, entered: make(chan struct{})}
			pc := &pooledConn{conn: observed, raw: observed, origin: target.Origin(), peer: netip.MustParseAddr("10.20.1.1"), owner: client.state()}
			client.state().connections[pc] = true
			op := &operation{ctx: ctx, cancel: cancel, owner: client.state()}
			client.state().active[op] = true
			a := &attempt{ctx: ctx, client: client, target: target, addresses: []netip.Addr{pc.peer}}
			state := &smtpState{op: op, pc: pc, client: client, a: a, writer: &firstWriter{a: a, pc: pc, first: true}, started: true, active: true, stop: func() {}}
			conn := &Conn{data: func() *smtpState { return state }}
			defer conn.Close()
			done := make(chan error, 1)
			go func() { _, err := conn.Write([]byte("controlled synthetic bytes")); done <- err }()
			select {
			case <-observed.entered:
			case <-ctx.Done():
				t.Fatal("write not entered")
			}
			var err error
			if setter == "SetDeadline" {
				err = conn.SetDeadline(time.Time{})
			} else {
				err = conn.SetWriteDeadline(time.Time{})
			}
			if err != nil {
				t.Fatal(err)
			}
			bounded, stop := context.WithTimeout(context.Background(), 2250*time.Millisecond)
			start := time.Now()
			release, gateErr := ps.gate.acquire(bounded, true)
			stop()
			if gateErr == nil {
				release()
			}
			conn.Close()
			<-done
			if gateErr != nil {
				t.Fatalf("deadline reset held first-write gate beyond 2s: %v (%v)", gateErr, time.Since(start))
			}
		})
	}
}
