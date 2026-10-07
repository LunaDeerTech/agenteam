package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// These private boundaries mirror only the public D04 operations this wire
// needs. The production implementation below forwards every operation to D04.
type embeddingResponse interface {
	StatusCode() int
	Headers() http.Header
	Body() io.ReadCloser
	Close() error
	Decision() outbound.Decision
}
type embeddingClient interface {
	Do(context.Context, *http.Request, outbound.Profile) (embeddingResponse, error)
	StopAdmission()
	Drain(context.Context) error
}
type embeddingD04Client struct{ *outbound.Client }

func (c embeddingD04Client) Do(ctx context.Context, req *http.Request, p outbound.Profile) (embeddingResponse, error) {
	response, err := c.Client.Do(ctx, req, p)
	if err != nil {
		return nil, err
	}
	return response, nil
}

type EmbeddingExchange struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	budget      *Budget
	client      embeddingClient
	material    sc.SecretMaterial // borrowed, never destroyed by the adapter
	ioDone      chan struct{}
	workerDone  chan struct{}
	joinPermit  chan struct{}
	joined      bool
	consumed    bool
	result      EmbeddingResult
	err         error
	joinErr     error
	observation Observation
	requestID   string
}

func newEmbeddingExchange(ctx context.Context, overall time.Duration, b *Budget, material sc.SecretMaterial) *EmbeddingExchange {
	ctx, cancel := context.WithTimeout(ctx, overall)
	x := &EmbeddingExchange{
		ctx: ctx, cancel: cancel, budget: b, material: material,
		ioDone: make(chan struct{}), workerDone: make(chan struct{}), joinPermit: make(chan struct{}, 1),
		observation: Observation{Usage: mc.Usage{Source: mc.UnknownUsage}},
	}
	x.joinPermit <- struct{}{}
	return x
}
func (x *EmbeddingExchange) cancelWork() { x.cancel() }

func (x *EmbeddingExchange) run(create func() (embeddingClient, error), p embeddingPrepared) {
	defer close(x.workerDone)
	result, err := x.perform(create, p)
	x.mu.Lock()
	client := x.client
	x.mu.Unlock()
	if client != nil {
		client.StopAdmission()
	}
	x.mu.Lock()
	if e := x.ctx.Err(); e != nil {
		result = EmbeddingResult{}
		if err == nil {
			err = contextFailure(e)
		}
	}
	x.result, x.err = result, err
	// Do, parser and both public Close boundaries have actually returned.
	close(x.ioDone)
	x.mu.Unlock()
	// Original attempt budget only. A late D04 writer remains owned until a
	// caller supplies its own remaining Close/Drain budget and joins it.
	_ = x.waitJoined(x.ctx)
}

func (x *EmbeddingExchange) perform(create func() (embeddingClient, error), p embeddingPrepared) (result EmbeddingResult, err error) {
	defer func() {
		if e := p.request.Body.Close(); e != nil {
			result = EmbeddingResult{}
			if err == nil {
				err = transportFailure(e)
			}
		}
	}()
	client, err := create()
	if err != nil || nilPort(client) {
		return EmbeddingResult{}, failure("unknown", "wire_transport_error")
	}
	x.mu.Lock()
	x.client, x.observation.DoStarted = client, true
	x.mu.Unlock()
	response, err := client.Do(x.ctx, p.request, p.profile)
	if !nilPort(response) {
		defer func() {
			if e := response.Close(); e != nil {
				x.observeNetworkError(e)
				result = EmbeddingResult{}
				if err == nil {
					err = transportFailure(e)
				}
			}
		}()
	}
	if err != nil {
		x.observeNetworkError(err)
		return EmbeddingResult{}, transportFailure(err)
	}
	if nilPort(response) {
		return EmbeddingResult{}, protocolFailure()
	}
	d := response.Decision()
	x.mu.Lock()
	x.observation.Decision = &d
	x.requestID = requestID(response.Headers().Get("X-Request-Id"))
	x.mu.Unlock()
	body := response.Body()
	if nilPort(body) {
		return EmbeddingResult{}, protocolFailure()
	}
	reader := embeddingReadFunc(func(p []byte) (int, error) {
		n, err := body.Read(p)
		x.observeNetworkError(err)
		return n, err
	})
	if response.StatusCode() != http.StatusOK {
		// Discard only a bounded diagnostic body; never expose its bytes.
		if _, err := io.Copy(io.Discard, io.LimitReader(reader, 64<<10)); err != nil {
			return EmbeddingResult{}, transportFailure(err)
		}
		return EmbeddingResult{}, statusFailure(response.StatusCode())
	}
	media, _, err := mime.ParseMediaType(response.Headers().Get("Content-Type"))
	if err != nil || media != "application/json" {
		return EmbeddingResult{}, protocolFailure()
	}
	raw, err := readEmbeddingBody(x.ctx, reader, p.responseBytes)
	if err != nil {
		return EmbeddingResult{}, err
	}
	result, usage, err := parseEmbeddingJSON(x.ctx, raw, p.count, p.dimensions)
	x.mu.Lock()
	x.observation.Usage = usage.Clone()
	result.ProviderRequestID = x.requestID
	x.mu.Unlock()
	if err != nil {
		return EmbeddingResult{}, err
	}
	return result, nil
}

type embeddingReadFunc func([]byte) (int, error)

func (r embeddingReadFunc) Read(p []byte) (int, error) { return r(p) }

func (x *EmbeddingExchange) observeNetworkError(err error) {
	var network *outbound.NetworkError
	if !errors.As(err, &network) {
		return
	}
	d := network.Decision()
	x.mu.Lock()
	x.observation.Decision = &d
	x.mu.Unlock()
}
func (x *EmbeddingExchange) Observe() Observation {
	if x == nil {
		return Observation{Usage: mc.Usage{Source: mc.UnknownUsage}}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	o := x.observation
	o.Usage = o.Usage.Clone()
	if o.Decision != nil {
		d := *o.Decision
		if d.PolicyVersion != nil {
			v := *d.PolicyVersion
			d.PolicyVersion = &v
		}
		o.Decision = &d
	}
	return o
}

func (x *EmbeddingExchange) waitJoined(ctx context.Context) error { return x.join(ctx, false) }
func (x *EmbeddingExchange) join(ctx context.Context, probe bool) error {
	if x == nil || ctx == nil {
		return invalid()
	}
	select {
	case <-x.ioDone:
	default:
		if probe {
			return contextFailure(context.Canceled)
		}
		select {
		case <-x.ioDone:
		case <-ctx.Done():
			return contextFailure(ctx.Err())
		}
	}
	// Only one actual Drain at a time, without an uninterruptible mutex wait.
	// An instantaneous Joined probe never waits behind another join caller.
	select {
	case <-x.joinPermit:
	default:
		if probe {
			return contextFailure(context.Canceled)
		}
		select {
		case <-x.joinPermit:
		case <-ctx.Done():
			return contextFailure(ctx.Err())
		}
	}
	defer func() { x.joinPermit <- struct{}{} }()
	x.mu.Lock()
	client, joined := x.client, x.joined
	x.mu.Unlock()
	if joined {
		return nil
	}
	if client != nil {
		if err := client.Drain(ctx); err != nil {
			x.observeNetworkError(err)
			if ctx.Err() != nil {
				return contextFailure(ctx.Err())
			}
			safe := transportFailure(err)
			// A real Drain failure stays a failed result even when a later
			// explicit cleanup succeeds. A cancelled no-wait probe is not a
			// business failure and cannot poison a still-running attempt.
			if !probe {
				x.mu.Lock()
				if x.joinErr == nil {
					x.joinErr = safe
				}
				x.result = EmbeddingResult{}
				x.mu.Unlock()
			}
			return safe
		}
	}
	x.mu.Lock()
	if !x.joined {
		x.joined = true
		x.material = sc.SecretMaterial{}
		x.budget.release(x)
	}
	x.mu.Unlock()
	return nil
}
func (x *EmbeddingExchange) Joined() bool {
	if x == nil {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return x.join(ctx, true) == nil
}
func (x *EmbeddingExchange) Close(ctx context.Context) error {
	if x == nil || ctx == nil {
		return invalid()
	}
	x.cancel()
	return x.waitJoined(ctx)
}

func (x *EmbeddingExchange) readError(err error) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	var model *mc.ModelError
	if !errors.As(err, &model) {
		model = failure("unknown", "wire_transport_error")
	}
	v := *model
	v.ProviderRequestID = x.requestID
	v.Dispatched = x.observation.Decision != nil && x.observation.Decision.Sent
	v.PartialOutput = false
	return &v
}

func (x *EmbeddingExchange) consumerJoin(ctx context.Context) error {
	wait, cancel := context.WithCancel(x.ctx)
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		cancel()
		close(callbackDone)
	})
	defer func() {
		if !stop() {
			<-callbackDone
		}
		cancel()
	}()
	if ctx.Err() != nil {
		cancel()
	}
	err := x.waitJoined(wait)
	if ctx.Err() != nil {
		return contextFailure(ctx.Err())
	}
	if x.ctx.Err() != nil {
		return contextFailure(x.ctx.Err())
	}
	return err
}
func (x *EmbeddingExchange) Result(ctx context.Context) (EmbeddingResult, error) {
	if x == nil || ctx == nil {
		return EmbeddingResult{}, invalid()
	}
	x.mu.Lock()
	if x.consumed {
		x.mu.Unlock()
		return EmbeddingResult{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	x.consumed = true
	x.mu.Unlock()
	defer func() {
		x.cancel()
		x.mu.Lock()
		x.result = EmbeddingResult{}
		x.mu.Unlock()
	}()
	select {
	case <-x.ioDone:
	case <-ctx.Done():
		return EmbeddingResult{}, x.readError(contextFailure(ctx.Err()))
	case <-x.ctx.Done():
		return EmbeddingResult{}, x.readError(contextFailure(x.ctx.Err()))
	}
	x.mu.Lock()
	err := x.err
	x.mu.Unlock()
	if err != nil {
		return EmbeddingResult{}, x.readError(err)
	}
	if err := x.consumerJoin(ctx); err != nil {
		return EmbeddingResult{}, x.readError(err)
	}
	x.mu.Lock()
	result, err := x.result, x.joinErr
	x.result = EmbeddingResult{}
	x.mu.Unlock()
	if err != nil {
		return EmbeddingResult{}, x.readError(err)
	}
	// Recheck cancellation after actual join and before any success escapes.
	if err := ctx.Err(); err != nil {
		return EmbeddingResult{}, x.readError(contextFailure(err))
	}
	if err := x.ctx.Err(); err != nil {
		return EmbeddingResult{}, x.readError(contextFailure(err))
	}
	// Move the sole vector backing to the once-only consumer. Usage still has
	// an independent observation copy; no vector remains in the handle.
	result.Usage = result.Usage.Clone()
	return result, nil
}

func (*EmbeddingExchange) Format(w fmt.State, _ rune) { safeFormat(w, "model_embedding_exchange") }
func (*EmbeddingExchange) MarshalJSON() ([]byte, error) {
	return []byte("\"model_embedding_exchange\""), nil
}
func (*EmbeddingExchange) LogValue() slog.Value { return slog.StringValue("model_embedding_exchange") }
