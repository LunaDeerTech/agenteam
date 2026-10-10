package model

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

var _ mc.Chat = (*Runtime)(nil)

func (r *Runtime) Chat(ctx context.Context, request mc.ModelRequest) (mc.ModelResponse, error) {
	c, err := r.begin(ctx, request, wire.JSONResponse)
	if err != nil {
		return mc.ModelResponse{}, err
	}
	if err = c.enter(ctx, true); err != nil {
		c.cancel()
		return mc.ModelResponse{}, err
	}
	defer c.leave()
	if err = runtimeContextError(c.ctx); err != nil {
		return mc.ModelResponse{}, c.failStart(c.ctx, err)
	}
	result, callErr := c.exchange.Result(c.ctx)
	if err = c.finish(c.ctx, &result, callErr); err != nil {
		return mc.ModelResponse{}, err
	}
	if callErr != nil {
		return mc.ModelResponse{}, runtimePortError(callErr)
	}
	if c.result == nil {
		return mc.ModelResponse{}, fault(f.InvalidState)
	}
	return c.result.Clone(), nil
}

type runtimeStream struct{ data func() *runtimeCall }

func (r *Runtime) Stream(ctx context.Context, request mc.ModelRequest) (mc.ModelStream, error) {
	c, err := r.begin(ctx, request, wire.SSEResponse)
	if err != nil {
		return nil, err
	}
	if err = c.enter(ctx, true); err != nil {
		c.cancel()
		return nil, err
	}
	defer c.leave()
	if err = runtimeContextError(c.ctx); err != nil {
		return nil, c.failStart(c.ctx, err)
	}
	value := c.copyRecord().value
	c.frames = []mc.ModelFrame{
		{Kind: "attempt_started", Start: &mc.FrameStart{StartedAt: &value.StartedAt}},
		{Kind: "message_start", Start: &mc.FrameStart{MessageID: value.ID.String()}},
	}
	return &runtimeStream{data: func() *runtimeCall { return c }}, nil
}

func (s *runtimeStream) call() *runtimeCall {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *runtimeStream) Joined() bool {
	c := s.call()
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.joined
}
func (s *runtimeStream) Close(ctx context.Context) error {
	c := s.call()
	if c == nil {
		return fault(f.DependencyUnbound)
	}
	return c.close(ctx)
}
func (s *runtimeStream) Next(ctx context.Context) (mc.ModelFrame, error) {
	c := s.call()
	if c == nil {
		return mc.ModelFrame{}, fault(f.DependencyUnbound)
	}
	if err := c.enter(ctx, false); err != nil {
		return mc.ModelFrame{}, err
	}
	defer c.leave()
	if c.terminalPublished {
		return mc.ModelFrame{}, io.EOF
	}
	// Combine this wait with the original attempt. Neither Next nor its DB
	// observations can extend the original deadline or detach cancellation.
	wait, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	ctx = wait
	if err := runtimeContextError(c.ctx); err != nil {
		return c.failFrame(ctx, err)
	}
	if len(c.frames) > 0 {
		frame := c.frames[0]
		c.frames[0] = mc.ModelFrame{}
		c.frames = c.frames[1:]
		return c.frame(frame)
	}
	event, err := c.exchange.Next(ctx)
	if err != nil {
		return c.failFrame(ctx, err)
	}
	switch event.Kind {
	case wire.TextDelta:
		if !utf8.ValidString(event.Text) || len(event.Text) > (16<<20)-len(c.text) {
			return c.failFrame(ctx, fault(f.CapabilityUnsupported))
		}
		offset := len(c.text)
		c.text = append(c.text, event.Text...)
		return c.frame(mc.ModelFrame{Kind: "text_delta", Text: &mc.FrameText{PartIndex: 0, OffsetUTF8: mc.TokenCount(offset), Text: event.Text}})
	case wire.UsageUpdate:
		if event.Usage == nil || event.Usage.Validate() != nil {
			return c.failFrame(ctx, fault(f.InvalidState))
		}
		if err := c.observe(ctx, c.exchange.Observe(), event.Usage, "", false); err != nil {
			return c.failFrame(ctx, err)
		}
		u := c.copyRecord().value.Usage.Clone()
		return c.frame(mc.ModelFrame{Kind: "usage_update", Usage: &u})
	case wire.StreamEnd:
		if event.End == nil {
			return c.failFrame(ctx, fault(f.InvalidState))
		}
		result := wire.Result{Text: string(c.text), End: *event.End}
		if err := c.finish(ctx, &result, nil); err != nil {
			c.cancel()
			c.terminalPublished = true
			return mc.ModelFrame{}, runtimePortError(err)
		}
		if c.result == nil {
			c.terminalPublished = true
			return mc.ModelFrame{}, fault(f.InvalidState)
		}
		response := c.result.Clone()
		return c.frame(mc.ModelFrame{Kind: "message_end", Response: &response})
	default:
		return c.failFrame(ctx, fault(f.InvalidState))
	}
}

func (c *runtimeCall) frame(frame mc.ModelFrame) (mc.ModelFrame, error) {
	if c.frameSequence == math.MaxInt64 {
		c.cancel()
		c.terminalPublished = true
		return mc.ModelFrame{}, fault(f.InvalidState)
	}
	c.frameSequence++
	value := c.copyRecord().value
	frame.CallID, frame.InvocationID, frame.AttemptIndex, frame.Sequence = value.CallID, value.ID, 1, c.frameSequence
	if frame.Validate() != nil {
		c.cancel()
		c.terminalPublished = true
		return mc.ModelFrame{}, fault(f.InvalidState)
	}
	if frame.Kind == "message_end" || frame.Kind == "call_failed" || frame.Kind == "call_cancelled" {
		c.terminalPublished = true
	}
	return frame, nil
}

func (c *runtimeCall) failFrame(ctx context.Context, original error) (mc.ModelFrame, error) {
	if err := c.finish(ctx, nil, original); err != nil {
		c.cancel()
		c.terminalPublished = true
		return mc.ModelFrame{}, runtimePortError(err)
	}
	value := c.copyRecord().value
	if value.Final == nil {
		c.terminalPublished = true
		return mc.ModelFrame{}, fault(f.InvalidState)
	}
	if value.Final.Status == uc.Cancelled {
		return c.frame(mc.ModelFrame{Kind: "call_cancelled", CancelReason: "cancelled"})
	}
	err := runtimeModelError(original, value.Dispatch == uc.Sent, len(c.text) > 0)
	if value.Final.Error != nil {
		copy := *value.Final.Error
		err = &copy
	}
	return c.frame(mc.ModelFrame{Kind: "call_failed", Error: err})
}

func (c *runtimeCall) failStart(ctx context.Context, original error) error {
	if err := c.finish(ctx, nil, original); err != nil {
		return err
	}
	return runtimePortError(original)
}

// finish runs under the call operation gate. It never gives a cancelled
// operation a fresh budget; a later explicit Close/Drain owns any remaining
// confirmation and retirement work with its caller's context.
func (c *runtimeCall) finish(ctx context.Context, result *wire.Result, original error) error {
	c.mu.Lock()
	if original != nil && c.err == nil {
		c.err = runtimePortError(original)
	}
	if c.err != nil {
		original = c.err
	}
	pending, accepted, joined := c.pending, c.accepted, c.joined
	x, handoff, returned := c.exchange, c.handoff, c.startReturned
	c.mu.Unlock()
	if joined {
		return nil
	}
	if pending != nil {
		if err := c.confirmMutation(ctx, pending); err != nil {
			return err
		}
		c.mu.Lock()
		accepted = c.accepted
		c.mu.Unlock()
	}
	if !accepted {
		c.releaseLocal()
		return nil
	}
	if x != nil {
		if err := x.Close(ctx); err != nil {
			return runtimePortError(err)
		}
		if !x.Joined() {
			return fault(f.ResourceBusy)
		}
	} else if handoff && !returned {
		return fault(f.ResourceBusy)
	}
	// Every borrower of the material has actually returned at this point.
	c.mu.Lock()
	material, owned := c.material, c.materialOwned
	c.mu.Unlock()
	if owned {
		material.Destroy()
	}
	c.mu.Lock()
	c.materialOwned, c.ioRetired = false, true
	c.mu.Unlock()
	current := c.copyRecord()
	if current.value.Final != nil {
		return c.retireLease(ctx)
	}
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	observation := wire.Observation{Usage: mc.Usage{Source: mc.UnknownUsage}}
	if x != nil {
		observation = x.Observe()
	}
	var usage *mc.Usage
	requestID := ""
	if original == nil && result != nil {
		usage = &result.End.Usage
		requestID = result.End.ProviderRequestID
	}
	if err := c.observe(ctx, observation, usage, requestID, true); err != nil {
		return err
	}
	current = c.copyRecord()
	var response *mc.ModelResponse
	if original == nil && result != nil {
		r := mc.ModelResponse{CallID: current.value.CallID, InvocationID: current.value.ID,
			Message:      mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: result.Text}}}},
			FinishReason: result.End.FinishReason, Usage: current.value.Usage.Clone(), ProviderRequestID: current.value.ProviderRequestID}
		if current.value.Dispatch != uc.Sent || r.Validate() != nil || r.FinishReason != "stop" && r.FinishReason != "length" {
			original = fault(f.InvalidState)
		} else {
			response = &r
		}
	}
	if response == nil && original == nil {
		original = fault(f.InvalidState)
	}
	status := uc.Succeeded
	var modelError *mc.ModelError
	if original != nil {
		modelError = runtimeModelError(original, current.value.Dispatch == uc.Sent, len(c.text) > 0)
		status = uc.Failed
		if modelError.Category == "cancelled" {
			status = uc.Cancelled
		}
	}
	if current.value.Dispatch == uc.DispatchUnknown {
		status = uc.Unknown
	}
	at, err := dbNow(ctx, c.runtime.store)
	if err != nil {
		return err
	}
	current.value.Final = &uc.Final{Status: status, Version: 1, FinishedAt: at, Error: modelError}
	current.phase = string(status)
	if err = runtimeAdvance(current); err != nil {
		return err
	}
	consumer, plan, err := c.finalizationPlan(ctx, current)
	if err != nil {
		return err
	}
	if err = c.persist(ctx, uc.FinalizeAction, current, consumer, plan); err != nil {
		return err
	}
	c.result = response
	if err = c.retireLease(ctx); err != nil {
		return err
	}
	if result != nil && original != nil && response == nil {
		return runtimePortError(original)
	}
	return nil
}

func runtimeAdvance(record *runtimeRecord) error {
	if record.sequence == math.MaxInt64 || record.version == math.MaxInt64 {
		return fault(f.InvalidState)
	}
	record.sequence++
	record.version++
	return nil
}

func (c *runtimeCall) finalizationPlan(ctx context.Context, record *runtimeRecord) (mc.ConsumerRequest, mc.ConsumerDependencies, error) {
	v := record.value
	attempt := mc.AttemptIdentity{CallID: v.CallID, InvocationID: v.ID, AttemptIndex: v.AttemptIndex, ProcessID: v.ProcessID, Fence: v.Fence}
	consumer := runtimeConsumerRequest(c.request, mc.FinalizeConsumer, &attempt, nil)
	actor, err := c.technicalActor()
	if err != nil {
		return mc.ConsumerRequest{}, mc.ConsumerDependencies{}, err
	}
	consumer.Actor = actor
	plan, err := c.runtime.authority.discoverConsumer(ctx, consumer)
	return consumer, plan, err
}

func (c *runtimeCall) observe(ctx context.Context, observation wire.Observation, usage *mc.Usage, requestID string, retired bool) error {
	current := c.copyRecord()
	if current.value.Final != nil {
		return nil
	}
	next := c.copyRecord()
	if observation.Decision != nil && observation.Decision.Sent {
		if next.value.Dispatch != uc.Sent {
			at, err := dbNow(ctx, c.runtime.store)
			if err != nil {
				return err
			}
			next.value.Dispatch, next.value.DispatchedAt = uc.Sent, &at
		}
	} else if next.value.Dispatch != uc.Sent {
		if !retired {
			return fault(f.InvalidState)
		}
		if !observation.DoStarted || observation.Decision != nil {
			next.value.Dispatch = uc.NotSent
		} else {
			next.value.Dispatch = uc.DispatchUnknown
		}
	}
	if next.value.Dispatch == uc.Sent {
		candidate := observation.Usage
		if usage != nil {
			candidate = *usage
		}
		if candidate.Validate() != nil {
			return fault(f.InvalidState)
		}
		if candidate.Source != mc.UnknownUsage {
			next.value.Usage = candidate.Clone()
		}
	}
	if requestID != "" {
		next.value.ProviderRequestID = requestID
	}
	if resolutionEqual(current.value, next.value) {
		return nil
	}
	if next.phase == "accepted" {
		next.phase = "running"
	}
	if err := runtimeAdvance(next); err != nil {
		return err
	}
	consumer, plan, err := c.finalizationPlan(ctx, next)
	if err != nil {
		return err
	}
	return c.persist(ctx, uc.ObserveAction, next, consumer, plan)
}

func runtimeModelError(err error, sent, partial bool) *mc.ModelError {
	result := mc.ModelError{Category: "unknown"}
	var known *mc.ModelError
	if errors.As(err, &known) && known.Validate() == nil {
		result = *known
	}
	if errors.Is(err, context.Canceled) {
		result.Category, result.Retryable = "cancelled", false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result.Category, result.Retryable = "timeout", false
	}
	result.Dispatched, result.PartialOutput = sent, sent && partial
	return &result
}

func (*runtimeStream) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "model_runtime_stream") }
func (*runtimeStream) MarshalJSON() ([]byte, error) { return []byte(`"model_runtime_stream"`), nil }
func (*runtimeStream) LogValue() slog.Value         { return slog.StringValue("model_runtime_stream") }
