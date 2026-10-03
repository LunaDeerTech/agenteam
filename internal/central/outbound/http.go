package outbound

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

var errBodyLimit = errors.New("OUTBOUND_BODY_LIMIT")
var errHeaderLimit = errors.New("OUTBOUND_HEADER_LIMIT")

type responseData struct {
	raw      *http.Response
	decision Decision
	body     *responseBody
}

// Response deliberately has no raw Request/URL or string representation of
// headers/body. Explicit readers and copied headers are for the trusted adapter.
type Response struct{ data func() responseData }

func (r Response) StatusCode() int {
	if r.data == nil {
		return 0
	}
	return r.data().raw.StatusCode
}
func (r Response) Headers() http.Header {
	if r.data == nil {
		return nil
	}
	return r.data().raw.Header.Clone()
}
func (r Response) Trailers() http.Header {
	if r.data == nil {
		return nil
	}
	b := r.data().body
	b.readMu.Lock()
	defer b.readMu.Unlock()
	return r.data().raw.Trailer.Clone()
}
func (r Response) Body() io.ReadCloser {
	if r.data == nil {
		return http.NoBody
	}
	return r.data().body
}
func (r Response) Close() error { return r.Body().Close() }
func (r Response) Decision() Decision {
	if r.data == nil {
		return Decision{Reason: ac.InternalError}
	}
	d := r.data().decision
	if d.PolicyVersion != nil {
		v := *d.PolicyVersion
		d.PolicyVersion = &v
	}
	return d
}
func (r Response) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_response") }
func (r Response) MarshalJSON() ([]byte, error) { return []byte(`"outbound_response"`), nil }
func (r Response) LogValue() slog.Value         { return slog.StringValue("outbound_response") }

func (c *Client) Do(ctx context.Context, request *http.Request, profile Profile) (Response, error) {
	if profile.data == nil {
		return Response{}, networkError(Decision{Consumer: ac.SystemConsumer}, ac.ConsumerDenied, nil)
	}
	p := profile.data()
	op, err := c.begin(ctx, p.Limits.Overall)
	if err != nil {
		return Response{}, networkError(Decision{Consumer: p.Consumer}, ac.PolicyUnavailable, err)
	}
	transferred := false
	defer func() {
		if !transferred {
			op.finish()
		}
	}()
	if request == nil || request.URL == nil {
		return Response{}, networkError(Decision{Consumer: p.Consumer}, ac.InvalidTarget, nil)
	}
	req := request.Clone(op.ctx)
	req.Body = ownedBody(request.Body)
	if req.Body != nil {
		defer func() {
			if !transferred {
				_ = req.Body.Close()
			}
		}()
	}
	target, err := ParseTarget(req.URL.String())
	a := newAttempt(op, c, p, target, 0)
	if status := c.state().policy.Status(); status.Version != nil {
		a.decision.PolicyVersion = status.Version
	}
	fail := func(e error) (Response, error) {
		var n *NetworkError
		if !errors.As(e, &n) {
			n = a.fail(a.reason(e, ac.InternalError), e)
		}
		a.denyAudit(n.Decision().Reason)
		return Response{}, n
	}
	if err != nil {
		return fail(a.fail(ac.InvalidTarget, err))
	}
	if err = prepareRequest(req, target, p); err != nil {
		return fail(a.fail(ac.InvalidTarget, err))
	}
	if _, err = c.state().policy.snapshot(); err != nil {
		return fail(a.fail(ac.PolicyUnavailable, err))
	}
	activeBinding := p.Credentials
	var materials []string
	if activeBinding.data != nil {
		materials, err = applyCredentials(req, target.Origin(), activeBinding, true)
		if err != nil {
			return fail(a.fail(ac.CredentialDenied, err))
		}
	}
	for redirects := 0; ; redirects++ {
		a = newAttempt(op, c, p, target, redirects)
		if status := c.state().policy.Status(); status.Version != nil {
			a.decision.PolicyVersion = status.Version
		}
		if target.Origin().Scheme() == "http" && !p.AllowHTTP {
			return fail(a.fail(ac.HTTPDenied, nil))
		}
		x, err := a.roundTrip(req)
		if err != nil {
			return fail(err)
		}
		if !redirectStatus(x.raw.StatusCode) || x.raw.Header.Get("Location") == "" {
			body := &responseBody{exchange: x, op: op, a: a, remaining: p.Limits.ResponseBodyBytes, reusable: !req.Close && !x.raw.Close}
			d := responseData{x.raw, a.safeDecision(), body}
			result := Response{data: func() responseData { return d }}
			transferred = true
			if x.raw.Body == http.NoBody {
				body.finish(true)
			}
			return result, nil
		}
		location := x.raw.Header.Get("Location")
		x.close()
		if redirects >= p.Limits.Redirects || req.Method != "GET" && req.Method != "HEAD" || req.Body != nil && req.Body != http.NoBody {
			return fail(a.fail(ac.RedirectDenied, nil))
		}
		relative, e := url.Parse(location)
		if e != nil {
			return fail(a.fail(ac.RedirectDenied, e))
		}
		next, e := ParseTarget(target.URL().ResolveReference(relative).String())
		if e != nil {
			return fail(a.fail(ac.RedirectDenied, e))
		}
		if target.Origin().Scheme() == "https" && next.Origin().Scheme() != "https" {
			return fail(a.fail(ac.RedirectDenied, nil))
		}
		nextReq := req.Clone(op.ctx)
		nextReq.URL = next.URL()
		nextReq.Host = nextReq.URL.Host
		nextReq.Body = nil
		nextReq.GetBody = nil
		nextReq.ContentLength = 0
		if !target.Origin().Equal(next.Origin()) {
			if locationCarriesMaterial(next.URL().String(), materials) {
				return fail(a.fail(ac.CredentialDenied, nil))
			}
			nextReq.Header = make(http.Header)
			nextReq.Header.Set("Accept-Encoding", "identity")
			activeBinding = CredentialBinding{}
		} else if activeBinding.data != nil {
			more, e := applyCredentials(nextReq, next.Origin(), activeBinding, false)
			if e != nil {
				return fail(a.fail(ac.CredentialDenied, e))
			}
			materials = append(materials, more...)
		}
		req = nextReq
		target = next
	}
}
func prepareRequest(req *http.Request, target Target, p ProfileOptions) error {
	if req.RequestURI != "" || req.Host != "" && req.Host != req.URL.Host || len(req.TransferEncoding) != 0 || len(req.Trailer) != 0 || req.ContentLength < -1 || req.ContentLength > p.Limits.RequestBodyBytes {
		return invalid()
	}
	if req.Body == nil && req.ContentLength != 0 {
		return invalid()
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	if !headerToken(req.Method) || req.Method == "CONNECT" {
		return invalid()
	}
	headers := make(http.Header)
	for name, values := range req.Header {
		if !headerToken(name) || connectionHeader(name) || strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Cookie") {
			return invalid()
		}
		for _, v := range values {
			if !headerValue(v) {
				return invalid()
			}
			headers.Add(name, v)
		}
	}
	for _, encoding := range headers.Values("Accept-Encoding") {
		if !strings.EqualFold(strings.TrimSpace(encoding), "identity") {
			return invalid()
		}
	}
	headers.Set("Accept-Encoding", "identity")
	req.Header = headers
	req.URL = target.URL()
	req.Host = req.URL.Host
	req.Proto = "HTTP/1.1"
	req.ProtoMajor = 1
	req.ProtoMinor = 1
	return nil
}
func applyCredentials(req *http.Request, origin Origin, binding CredentialBinding, first bool) ([]string, error) {
	d := binding.data()
	if !origin.Equal(d.origin) {
		return nil, invalid()
	}
	q, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return nil, invalid()
	}
	var materials []string
	for _, field := range d.fields {
		v := field.data()
		if first && (v.query && q.Has(v.name) || !v.query && len(req.Header.Values(v.name)) != 0) {
			return nil, invalid()
		}
		err := v.material.Use(func(secret []byte) error {
			if len(secret) == 0 {
				return invalid()
			}
			value := v.prefix + string(secret)
			if !v.query && !headerValue(value) {
				return invalid()
			}
			materials = append(materials, string(secret), value)
			if v.query {
				q.Set(v.name, value)
			} else {
				req.Header.Set(v.name, value)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	req.URL.RawQuery = q.Encode()
	return materials, nil
}
func locationCarriesMaterial(raw string, materials []string) bool {
	if len(materials) == 0 {
		return false
	}
	for range 16 {
		for _, value := range materials {
			if value != "" && strings.Contains(raw, value) {
				return true
			}
		}
		decoded, e := url.QueryUnescape(raw)
		if e != nil || decoded == raw {
			return false
		}
		raw = decoded
	}
	// With bound material, excessively nested escaping is ambiguous and is
	// conservatively refused rather than forwarded to another origin.
	return true
}
func redirectStatus(code int) bool {
	return code == 301 || code == 302 || code == 303 || code == 307 || code == 308
}

type boundedRequestBody struct {
	io.ReadCloser
	remaining int64
}

type onceBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

// requestHeaderWriter checks the one actual Request.Write serialization,
// including automatic headers and escaping. No original Body/GetBody is
// consulted for counting. It releases no bytes until CRLFCRLF fits the budget,
// and retains at most limit+1 bytes while locating that boundary.
type requestHeaderWriter struct {
	next     io.Writer
	limit    int
	header   []byte
	complete bool
}

func (w *requestHeaderWriter) Write(p []byte) (int, error) {
	if w.complete {
		return w.next.Write(p)
	}
	before := len(w.header)
	count := min(len(p), w.limit+1-before)
	w.header = append(w.header, p[:count]...)
	end := bytes.Index(w.header, []byte("\r\n\r\n"))
	if end < 0 {
		if len(w.header) > w.limit {
			return 0, errHeaderLimit
		}
		return len(p), nil
	}
	end += 4
	if end > w.limit {
		return 0, errHeaderLimit
	}
	n, err := w.next.Write(w.header[:end])
	if err == nil && n != end {
		err = io.ErrShortWrite
	}
	if err != nil {
		return max(0, min(len(p), n-before)), err
	}
	w.complete = true
	w.header = nil
	consumed := end - before
	if consumed < len(p) {
		n, err = w.next.Write(p[consumed:])
		return consumed + n, err
	}
	return len(p), nil
}

func ownedBody(body io.ReadCloser) io.ReadCloser {
	if body == nil || body == http.NoBody {
		return body
	}
	return &onceBody{ReadCloser: body}
}
func (b *onceBody) Close() error { b.once.Do(func() { b.err = b.ReadCloser.Close() }); return b.err }

func (r *boundedRequestBody) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if int64(len(b)) > r.remaining+1 {
		b = b[:int(r.remaining+1)]
	}
	n, err := r.ReadCloser.Read(b)
	if int64(n) > r.remaining {
		allowed := int(r.remaining)
		r.remaining = 0
		return allowed, errBodyLimit
	}
	r.remaining -= int64(n)
	return n, err
}

type exchangeResponse struct {
	raw         *http.Response
	pc          *pooledConn
	stop        func()
	headerBytes int
}

func (r *exchangeResponse) close() { r.pc.close(); r.stop(); _ = r.raw.Body.Close() }
func stopOnCancel(ctx context.Context, pc *pooledConn) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { pc.close(); close(done) })
	var once sync.Once
	return func() {
		once.Do(func() {
			if !stop() {
				<-done
			}
		})
	}
}
func (a *attempt) roundTrip(req *http.Request) (*exchangeResponse, error) {
	for tries := 0; tries < 2; tries++ {
		if a.sent.Load() {
			return nil, a.fail(ac.InternalError, nil)
		}
		trace := httptrace.ContextClientTrace(a.ctx)
		if trace != nil && trace.GetConn != nil {
			trace.GetConn(a.target.URL().Host)
		}
		if err := a.resolve(); err != nil {
			return nil, err
		}
		pc := a.client.takeIdle(a.target.Origin())
		if pc != nil && !containsAddress(a.addresses, pc.peer) {
			pc.close()
			pc = nil
		}
		reused := pc != nil
		if pc == nil {
			var e error
			pc, e = a.connect()
			if e != nil {
				return nil, e
			}
		}
		if trace != nil && trace.GotConn != nil {
			var idleTime time.Duration
			if reused {
				idleTime = time.Since(pc.idleAt)
			}
			trace.GotConn(httptrace.GotConnInfo{Conn: observedConnection(pc), Reused: reused, WasIdle: reused, IdleTime: idleTime})
		}
		result, err := a.exchange(req, pc)
		if err == nil {
			return result, nil
		}
		pc.close()
		var ne *NetworkError
		reason := ac.InternalError
		if errors.As(err, &ne) {
			reason = ne.Decision().Reason
		}
		if a.sent.Load() || tries == 1 || a.ctx.Err() != nil || reason != ac.InternalError && reason != ac.Timeout {
			return nil, err
		}
		if req.Body != nil && req.Body != http.NoBody {
			if req.GetBody == nil {
				return nil, err
			}
			body, e := req.GetBody()
			if e != nil {
				return nil, a.fail(ac.InternalError, e)
			}
			req.Body = ownedBody(body)
		}
	}
	return nil, a.fail(ac.InternalError, nil)
}
func containsAddress(ips []netip.Addr, peer netip.Addr) bool {
	for _, ip := range ips {
		if ip == peer.Unmap() {
			return true
		}
	}
	return false
}
func (a *attempt) exchange(req *http.Request, pc *pooledConn) (*exchangeResponse, error) {
	stop := stopOnCancel(a.ctx, pc)
	ok := false
	defer func() {
		if !ok {
			pc.close()
			stop()
		}
	}()
	if deadline, exists := a.ctx.Deadline(); exists {
		_ = pc.conn.SetDeadline(deadline)
	}
	request := req.Clone(a.ctx)
	request.Body = req.Body
	if request.Body != nil && request.Body != http.NoBody {
		request.Body = &boundedRequestBody{ReadCloser: request.Body, remaining: a.profile.Limits.RequestBodyBytes}
	}
	s := a.client.state()
	s.mu.Lock()
	s.writers++
	s.mu.Unlock()
	written := make(chan error, 1)
	go func() {
		defer func() {
			s.mu.Lock()
			s.writers--
			s.notify()
			s.mu.Unlock()
		}()
		// Request.Write closes its body and invokes standard WroteRequest once.
		written <- request.Write(&requestHeaderWriter{next: &firstWriter{a: a, pc: pc, first: true}, limit: a.profile.Limits.RequestHeaderBytes})
	}()
	var writeErr error
	select {
	case writeErr = <-written:
	case <-a.ctx.Done():
		pc.close()
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, a.fail(a.reason(a.ctx.Err(), ac.Cancelled), a.ctx.Err())
	}
	if writeErr != nil {
		var ne *NetworkError
		if errors.As(writeErr, &ne) {
			return nil, ne
		}
		if errors.Is(writeErr, errBodyLimit) || errors.Is(writeErr, errHeaderLimit) {
			return nil, a.fail(ac.ResponseLimit, writeErr)
		}
		return nil, a.fail(a.reason(writeErr, ac.InternalError), writeErr)
	}
	deadline := time.Now().Add(a.profile.Limits.ResponseHeaders)
	if d, exists := a.ctx.Deadline(); exists && d.Before(deadline) {
		deadline = d
	}
	_ = pc.conn.SetReadDeadline(deadline)
	remaining := a.profile.Limits.ResponseHeaderBytes
	for interim := 0; interim <= 16; interim++ {
		header, err := readHeaderBlock(pc.reader, remaining)
		if err != nil {
			reason := a.reason(err, ac.InternalError)
			if errors.Is(err, errHeaderLimit) {
				reason = ac.ResponseLimit
			}
			return nil, a.fail(reason, err)
		}
		remaining -= len(header)
		// Preserve prefetched body bytes without layering old readers forever.
		prefetched := make([]byte, pc.reader.Buffered())
		_, _ = io.ReadFull(pc.reader, prefetched)
		header = append(header, prefetched...)
		pc.reader = bufio.NewReader(io.MultiReader(bytes.NewReader(header), pc.conn))
		resp, err := http.ReadResponse(pc.reader, req)
		if err != nil {
			return nil, a.fail(a.reason(err, ac.InternalError), err)
		}
		if resp.ProtoMajor != 1 || resp.ProtoMinor > 1 || resp.StatusCode == 101 {
			pc.close()
			_ = resp.Body.Close()
			return nil, a.fail(ac.InvalidTarget, nil)
		}
		if resp.StatusCode >= 100 && resp.StatusCode < 200 {
			_ = resp.Body.Close()
			continue
		}
		for _, v := range resp.Header.Values("Content-Encoding") {
			if !strings.EqualFold(strings.TrimSpace(v), "identity") {
				pc.close()
				_ = resp.Body.Close()
				return nil, a.fail(ac.ResponseLimit, nil)
			}
		}
		if resp.ContentLength > a.profile.Limits.ResponseBodyBytes {
			pc.close()
			_ = resp.Body.Close()
			return nil, a.fail(ac.ResponseLimit, nil)
		}
		if conn, ok := pc.conn.(*tls.Conn); ok {
			state := conn.ConnectionState()
			resp.TLS = &state
		}
		ok = true
		return &exchangeResponse{resp, pc, stop, a.profile.Limits.ResponseHeaderBytes - remaining}, nil
	}
	return nil, a.fail(ac.ResponseLimit, nil)
}
func readHeaderBlock(reader *bufio.Reader, limit int) ([]byte, error) {
	var header []byte
	lineBytes := 0
	for {
		part, err := reader.ReadSlice('\n')
		if len(header)+len(part) > limit {
			return nil, errHeaderLimit
		}
		header = append(header, part...)
		lineBytes += len(part)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		if lineBytes == 1 && part[len(part)-1] == '\n' || lineBytes == 2 && len(part) >= 2 && part[len(part)-2] == '\r' {
			return header, nil
		}
		lineBytes = 0
	}
}

type responseBody struct {
	exchange  *exchangeResponse
	op        *operation
	a         *attempt
	readMu    sync.Mutex
	once      sync.Once
	closed    atomic.Bool
	remaining int64
	reusable  bool
}

func (b *responseBody) finish(eof bool) {
	b.once.Do(func() {
		b.closed.Store(true)
		if !eof {
			b.exchange.pc.close()
		}
		b.exchange.stop()
		_ = b.exchange.raw.Body.Close()
		if eof && b.reusable && b.a.ctx.Err() == nil && b.exchange.pc.reader.Buffered() == 0 {
			b.a.client.putIdle(b.exchange.pc)
		} else {
			b.exchange.pc.close()
		}
		b.op.finish()
	})
}
func (b *responseBody) Read(p []byte) (int, error) {
	b.readMu.Lock()
	defer b.readMu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if b.closed.Load() {
		return 0, io.EOF
	}
	if err := b.a.ctx.Err(); err != nil {
		reason := b.a.reason(err, ac.Cancelled)
		b.finish(false)
		return 0, b.a.fail(reason, err)
	}
	deadline := time.Now().Add(b.a.profile.Limits.ReadIdle)
	if d, exists := b.a.ctx.Deadline(); exists && d.Before(deadline) {
		deadline = d
	}
	_ = b.exchange.pc.conn.SetReadDeadline(deadline)
	if int64(len(p)) > b.remaining+1 {
		p = p[:int(b.remaining+1)]
	}
	n, err := b.exchange.raw.Body.Read(p)
	if int64(n) > b.remaining {
		n = int(b.remaining)
		b.remaining = 0
		b.exchange.pc.close()
		failure := b.a.fail(ac.ResponseLimit, nil)
		b.a.denyAudit(ac.ResponseLimit)
		b.finish(false)
		return n, failure
	}
	b.remaining -= int64(n)
	if err == io.EOF {
		size := b.exchange.headerBytes
		for k, values := range b.exchange.raw.Trailer {
			for _, v := range values {
				size += len(k) + len(v) + 4
			}
		}
		if size > b.a.profile.Limits.ResponseHeaderBytes {
			b.exchange.pc.close()
			b.a.denyAudit(ac.ResponseLimit)
			b.finish(false)
			return n, b.a.fail(ac.ResponseLimit, nil)
		}
		b.finish(true)
		return n, io.EOF
	}
	if err != nil {
		reason := b.a.reason(err, ac.InternalError)
		b.finish(false)
		return n, b.a.fail(reason, err)
	}
	return n, nil
}
func (b *responseBody) Close() error { b.finish(false); return nil }
