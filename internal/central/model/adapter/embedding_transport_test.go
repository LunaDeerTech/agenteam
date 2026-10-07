package adapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

type embeddingTestGate struct {
	entered, release chan struct{}
	enter, leave     sync.Once
}

func embeddingGate() *embeddingTestGate {
	return &embeddingTestGate{entered: make(chan struct{}), release: make(chan struct{})}
}
func (g *embeddingTestGate) block() {
	g.enter.Do(func() { close(g.entered) })
	<-g.release
}
func (g *embeddingTestGate) open() { g.leave.Do(func() { close(g.release) }) }
func embeddingWait(t *testing.T, c <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(2 * time.Second):
		t.Fatal("controlled boundary did not complete:", label)
	}
}

type embeddingTestBody struct {
	reader   io.Reader
	eofGate  *embeddingTestGate
	readErr  error
	closeErr error
	closes   atomic.Int32
}

func (b *embeddingTestBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if err == io.EOF {
		if b.eofGate != nil {
			b.eofGate.block()
		}
		if b.readErr != nil {
			return n, b.readErr
		}
	}
	return n, err
}
func (b *embeddingTestBody) Close() error { b.closes.Add(1); return b.closeErr }

type embeddingTestResponse struct {
	status    int
	headers   http.Header
	body      *embeddingTestBody
	decision  outbound.Decision
	closeGate *embeddingTestGate
	closeErr  error
	closes    atomic.Int32
}

func (r *embeddingTestResponse) StatusCode() int             { return r.status }
func (r *embeddingTestResponse) Headers() http.Header        { return r.headers.Clone() }
func (r *embeddingTestResponse) Body() io.ReadCloser         { return r.body }
func (r *embeddingTestResponse) Decision() outbound.Decision { return r.decision }
func (r *embeddingTestResponse) Close() error {
	r.closes.Add(1)
	if r.closeGate != nil {
		r.closeGate.block()
	}
	if err := r.body.Close(); err != nil {
		return err
	}
	return r.closeErr
}

type embeddingTestClient struct {
	response  embeddingResponse
	doErr     error
	doGate    *embeddingTestGate
	onDo      func(context.Context, *http.Request, outbound.Profile)
	drainGate *embeddingTestGate
	drainErr  error
	onDrain   func(context.Context) error
	creates   atomic.Int32
	doCalls   atomic.Int32
	stops     atomic.Int32
	drains    atomic.Int32
	gates     []*embeddingTestGate
}

func (c *embeddingTestClient) Do(ctx context.Context, req *http.Request, profile outbound.Profile) (embeddingResponse, error) {
	c.doCalls.Add(1)
	if c.doGate != nil {
		c.doGate.block()
	}
	if c.onDo != nil {
		c.onDo(ctx, req, profile)
	}
	return c.response, c.doErr
}
func (c *embeddingTestClient) StopAdmission() { c.stops.Add(1) }
func (c *embeddingTestClient) Drain(ctx context.Context) error {
	c.drains.Add(1)
	if c.onDrain != nil {
		return c.onDrain(ctx)
	}
	if c.drainGate != nil {
		c.drainGate.enter.Do(func() { close(c.drainGate.entered) })
		select {
		case <-c.drainGate.release:
		default:
			select {
			case <-c.drainGate.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return c.drainErr
}
func (c *embeddingTestClient) release() {
	for _, g := range c.gates {
		g.open()
	}
}

type embeddingOutcome struct {
	result EmbeddingResult
	err    error
}
type embeddingHarness struct {
	x       *EmbeddingExchange
	client  *embeddingTestClient
	readers []<-chan struct{}
}

func embeddingResponseFor(raw string) *embeddingTestResponse {
	version := f.Version(7)
	return &embeddingTestResponse{
		status:   http.StatusOK,
		headers:  http.Header{"Content-Type": {"application/json; charset=utf-8"}, "X-Request-Id": {"embedding-request-7"}},
		body:     &embeddingTestBody{reader: strings.NewReader(raw)},
		decision: outbound.Decision{Sent: true, PolicyVersion: &version},
	}
}
func embeddingValidBody() string {
	return string(embeddingJSON("[{\"object\":\"embedding\",\"index\":0,\"embedding\":[1,2]}]", "{\"prompt_tokens\":0,\"total_tokens\":9007199254740993}"))
}
func startEmbeddingHarness(t *testing.T, parent context.Context, r EmbeddingRequest, o CallOptions, client *embeddingTestClient) *embeddingHarness {
	t.Helper()
	h := &embeddingHarness{client: client}
	t.Cleanup(func() {
		client.release()
		if h.x != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = h.x.Close(ctx) // failures are asserted at their original boundary
			embeddingWait(t, h.x.workerDone, "worker")
			// A controlled non-context Drain failure is allowed to persist;
			// each test that injects one supplies a later successful cleanup.
			if !h.x.Joined() {
				t.Error("actual controlled client remained unjoined")
			}
		}
		for _, done := range h.readers {
			embeddingWait(t, done, "Result caller")
		}
	})
	a := &OpenAIEmbeddings{budget: NewBudget()}
	x, err := a.start(parent, r, o, func() (embeddingClient, error) {
		client.creates.Add(1)
		return client, nil
	})
	if err != nil {
		t.Fatal("controlled Start", err)
	}
	h.x = x
	return h
}
func (h *embeddingHarness) result(ctx context.Context) <-chan embeddingOutcome {
	out := make(chan embeddingOutcome, 1)
	done := make(chan struct{})
	h.readers = append(h.readers, done)
	go func() {
		defer close(done)
		r, err := h.x.Result(ctx)
		out <- embeddingOutcome{r, err}
	}()
	return out
}
func embeddingReceive(t *testing.T, out <-chan embeddingOutcome) embeddingOutcome {
	t.Helper()
	select {
	case r := <-out:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("Result caller did not return")
		return embeddingOutcome{}
	}
}
func embeddingSlot(t *testing.T, x *EmbeddingExchange, want int) {
	t.Helper()
	active, _ := x.budget.snapshot()
	if len(active) != want {
		t.Fatal("actual budget slot count", len(active), want)
	}
}
func embeddingMaterial(t *testing.T, x *EmbeddingExchange, live bool) {
	t.Helper()
	x.mu.Lock()
	m := x.material
	x.mu.Unlock()
	err := m.Use(func([]byte) error { return nil })
	if (err == nil) != live {
		t.Fatal("borrowed material retention", live)
	}
}

func TestOpenAIEmbeddingsExchangeOwnership(t *testing.T) {
	r, o := embeddingUnitInput(t)
	gate := embeddingGate()
	response := embeddingResponseFor(embeddingValidBody())
	response.body.eofGate = gate
	client := &embeddingTestClient{response: response, gates: []*embeddingTestGate{gate}}
	h := startEmbeddingHarness(t, context.Background(), r, o, client)
	embeddingWait(t, gate.entered, "EOF")
	if value, err := h.x.Result(nil); err == nil || !reflect.DeepEqual(value, EmbeddingResult{}) {
		t.Fatal("nil Result context consumed or returned a candidate")
	}
	first := h.result(context.Background())
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		h.x.mu.Lock()
		consumed := h.x.consumed
		h.x.mu.Unlock()
		if consumed {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("Result ownership not acquired")
		}
	}
	_, err := h.x.Result(context.Background())
	requireFault(t, err, f.InvalidState)
	select {
	case <-first:
		t.Fatal("parseable bytes without EOF published success")
	default:
	}
	before := h.x.Observe()
	if before.Usage.Source != mc.UnknownUsage || before.Decision == nil || !before.Decision.Sent {
		t.Fatal("pre-EOF observation")
	}
	*before.Decision.PolicyVersion = 90
	if *h.x.Observe().Decision.PolicyVersion != 7 {
		t.Fatal("Decision alias")
	}
	gate.open()
	result := embeddingReceive(t, first)
	if result.err != nil || len(result.result.Items) != 1 || !reflect.DeepEqual(result.result.Items[0].Values, []float64{1, 2}) ||
		result.result.ProviderRequestID != "embedding-request-7" || !h.x.Joined() {
		t.Fatal("joined result shape", result.err)
	}
	result.result.Items[0].Values[0] = 88
	*result.result.Usage.TotalTokens = 1
	observed := h.x.Observe()
	if observed.Usage.InputTokens == nil || *observed.Usage.InputTokens != 0 ||
		observed.Usage.TotalTokens == nil || *observed.Usage.TotalTokens != 9007199254740993 {
		t.Fatal("result/observation usage alias or precision")
	}
	*observed.Usage.TotalTokens = 2
	if *h.x.Observe().Usage.TotalTokens != 9007199254740993 {
		t.Fatal("Observe retained alias")
	}
	h.x.mu.Lock()
	zero := reflect.DeepEqual(h.x.result, EmbeddingResult{})
	h.x.mu.Unlock()
	if !zero || client.creates.Load() != 1 || client.doCalls.Load() != 1 || client.stops.Load() != 1 ||
		response.closes.Load() != 1 || response.body.closes.Load() != 1 {
		t.Fatal("result transfer or exactly-once public operations")
	}
	embeddingSlot(t, h.x, 0)
	embeddingMaterial(t, h.x, false)
	if err := o.Credential.Use(func([]byte) error { return nil }); err != nil {
		t.Fatal("adapter destroyed caller-owned material")
	}
	_, err = h.x.Result(context.Background())
	requireFault(t, err, f.InvalidState)
}

func TestOpenAIEmbeddingsActualTerminalBarriers(t *testing.T) {
	for _, stage := range []string{"Do", "EOF", "ResponseClose", "Drain"} {
		t.Run(stage, func(t *testing.T) {
			r, o := embeddingUnitInput(t)
			o.Limits.Overall = 2 * time.Second
			gate := embeddingGate()
			response := embeddingResponseFor(embeddingValidBody())
			client := &embeddingTestClient{response: response, gates: []*embeddingTestGate{gate}}
			switch stage {
			case "Do":
				client.doGate = gate
			case "EOF":
				response.body.eofGate = gate
			case "ResponseClose":
				response.closeGate = gate
			case "Drain":
				client.drainGate = gate
			}
			h := startEmbeddingHarness(t, context.Background(), r, o, client)
			embeddingWait(t, gate.entered, stage)
			if h.x.Joined() {
				t.Fatal("held actual boundary claimed joined")
			}
			embeddingSlot(t, h.x, 1)
			embeddingMaterial(t, h.x, true)
			caller, cancel := context.WithCancel(context.Background())
			cancel()
			got, err := h.x.Result(caller)
			embeddingZero(t, got, err)
			requireModel(t, err, "cancelled", "wire_cancelled")
			wait, stop := context.WithTimeout(context.Background(), 40*time.Millisecond)
			started := time.Now()
			err = h.x.Close(wait)
			elapsed := time.Since(started)
			stop()
			if err == nil || elapsed < 25*time.Millisecond || elapsed > 500*time.Millisecond || h.x.Joined() {
				t.Fatal("Close forged completion or ignored caller budget", elapsed)
			}
			embeddingSlot(t, h.x, 1)
			embeddingMaterial(t, h.x, true)
			if stage == "ResponseClose" || stage == "Drain" {
				u := h.x.Observe().Usage
				if u.InputTokens == nil || *u.InputTokens != 0 || u.TotalTokens == nil {
					t.Fatal("reliable EOF usage lost at actual tail")
				}
			}
			gate.open()
			wait, stop = context.WithTimeout(context.Background(), time.Second)
			err = h.x.Close(wait)
			stop()
			if err != nil || !h.x.Joined() {
				t.Fatal("actual tail did not join", err)
			}
			embeddingSlot(t, h.x, 0)
			embeddingMaterial(t, h.x, false)
			_, err = h.x.Result(context.Background())
			requireFault(t, err, f.InvalidState)
		})
	}
}

func TestOpenAIEmbeddingsBoundaryFailures(t *testing.T) {
	for _, name := range []string{"factory", "Do", "read", "ResponseClose", "requestClose", "Drain", "mime", "401", "404", "429", "302", "dimension"} {
		t.Run(name, func(t *testing.T) {
			r, o := embeddingUnitInput(t)
			response := embeddingResponseFor(embeddingValidBody())
			client := &embeddingTestClient{response: response}
			privateErr := errors.New("embedding-private-error-canary")
			wantCategory, wantCode := mc.ErrorCategory("network"), "wire_transport_error"
			wantUsage := false
			switch name {
			case "factory":
				a := &OpenAIEmbeddings{budget: NewBudget()}
				x, err := a.start(context.Background(), r, o, func() (embeddingClient, error) { return nil, privateErr })
				if err != nil || x == nil {
					t.Fatal("accepted failure must retain exact handle")
				}
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_ = x.Close(ctx)
					embeddingWait(t, x.workerDone, "factory worker")
				})
				got, err := x.Result(context.Background())
				embeddingZero(t, got, err)
				requireModel(t, err, "unknown", "wire_transport_error")
				if x.Observe().DoStarted || x.Observe().Decision != nil {
					t.Fatal("factory failure fabricated Do/Decision")
				}
				return
			case "Do":
				client.response, client.doErr = nil, privateErr
			case "read":
				response.body.readErr = io.ErrUnexpectedEOF
			case "ResponseClose":
				response.closeErr, wantUsage = privateErr, true
			case "requestClose":
				wantUsage = true
				client.onDo = func(_ context.Context, req *http.Request, _ outbound.Profile) {
					req.Body = &embeddingTestBody{reader: req.Body, closeErr: privateErr}
				}
			case "Drain":
				wantUsage = true
				var calls atomic.Int32
				client.onDrain = func(context.Context) error {
					if calls.Add(1) == 1 {
						return privateErr
					}
					return nil
				}
			case "mime":
				response.headers.Set("Content-Type", "text/plain")
				wantCategory, wantCode = "provider_error", "wire_protocol_invalid"
			case "401":
				response.status = 401
				wantCategory, wantCode = "authentication", "wire_http_error"
			case "404", "302":
				response.status = 404
				if name == "302" {
					response.status = 302
				}
				wantCategory, wantCode = "provider_error", "wire_http_error"
			case "429":
				response.status = 429
				wantCategory, wantCode = "rate_limited", "wire_http_error"
			case "dimension":
				r.ExpectedDimensions = 3
				wantCategory, wantCode, wantUsage = "provider_error", "wire_protocol_invalid", true
			}
			h := startEmbeddingHarness(t, context.Background(), r, o, client)
			if name == "Drain" {
				// The first actual failed Drain must remain a failed Result
				// even though the subsequent cleanup Drain can succeed.
				embeddingWait(t, h.x.workerDone, "original failed Drain")
			}
			got, err := h.x.Result(context.Background())
			embeddingZero(t, got, err)
			m := requireModel(t, err, wantCategory, wantCode)
			if strings.Contains(err.Error(), "canary") || m.PartialOutput || m.Dispatched != (name != "Do") || m.Retryable != (name == "429") {
				t.Fatal("unsafe or fabricated error facts")
			}
			if (h.x.Observe().Usage.InputTokens != nil) != wantUsage {
				t.Fatal("EOF reliable usage boundary")
			}
			if client.creates.Load() != 1 || client.doCalls.Load() != 1 {
				t.Fatal("adapter retried")
			}
			if name != "Do" && response.closes.Load() != 1 {
				t.Fatal("public response Close was not observed")
			}
		})
	}
}

func TestOpenAIEmbeddingsCancellationAndJoin(t *testing.T) {
	for _, boundary := range []string{"parent", "result", "cancel-after-join"} {
		t.Run(boundary, func(t *testing.T) {
			r, o := embeddingUnitInput(t)
			o.Limits.Overall = time.Second
			parent := context.Background()
			var parentCancel context.CancelFunc
			if boundary == "parent" {
				parent, parentCancel = context.WithTimeout(parent, 50*time.Millisecond)
				defer parentCancel()
			}
			gate := embeddingGate()
			client := &embeddingTestClient{response: embeddingResponseFor(embeddingValidBody()), drainGate: gate, gates: []*embeddingTestGate{gate}}
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			if boundary == "cancel-after-join" {
				client.drainGate = nil
				client.onDrain = func(context.Context) error { cancel(); return nil }
			}
			h := startEmbeddingHarness(t, parent, r, o, client)
			if boundary != "cancel-after-join" {
				embeddingWait(t, gate.entered, "join")
			}
			if boundary == "parent" {
				pd, _ := parent.Deadline()
				xd, _ := h.x.ctx.Deadline()
				if !pd.Equal(xd) {
					t.Fatal("earlier parent deadline was replaced")
				}
			}
			out := h.result(caller)
			if boundary == "result" {
				cancel()
			}
			got := embeddingReceive(t, out)
			embeddingZero(t, got.result, got.err)
			if boundary == "parent" {
				requireModel(t, got.err, "timeout", "wire_transport_error")
			} else {
				requireModel(t, got.err, "cancelled", "wire_cancelled")
			}
			u := h.x.Observe().Usage
			if u.InputTokens == nil || *u.InputTokens != 0 {
				t.Fatal("cancelled tail lost reliable usage")
			}
			if boundary != "cancel-after-join" {
				embeddingSlot(t, h.x, 1)
				embeddingMaterial(t, h.x, true)
				if h.x.Joined() {
					t.Fatal("cancelled join released actual held client")
				}
				gate.open()
			}
		})
	}
}
