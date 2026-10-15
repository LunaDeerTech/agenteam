package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func prepareRuntimeEmbedding(ctx context.Context, r mc.EmbeddingRequest) (*mc.EmbeddingRequest, error) {
	digest, err := runtimeEmbeddingDigest(ctx, r.Texts, r.ExpectedDimensions)
	if err != nil {
		return nil, err
	}
	if r.Validate() != nil || r.Input.Digest != digest {
		return nil, fault(f.InvalidArgument)
	}
	if r.Input.SchemaVersion != 1 {
		return nil, fault(f.SchemaUnsupported)
	}
	s, c := r.Model.Snapshot, r.Model.Snapshot.Capabilities
	if r.Model.LeaseOwner.Details().Kind != sc.ModelCallOwner || s.Identity.Protocol != mc.OpenAIEmbeddings || s.Identity.Profile != mc.OpenAIEmbeddingsV1 || s.Identity.ModelType != mc.EmbeddingModel || s.Identity.AdapterRevision != wire.OpenAIEmbeddingsFloatRevision && s.Identity.AdapterRevision != wire.OpenAIEmbeddingsFloatUsageRevision || !runtimeOnlyText(c.InputModalities) || len(c.OutputModalities) != 1 || c.OutputModalities[0] != "vector" || c.Streaming || c.ToolCalls || c.ParallelToolCalls || c.Reasoning || len(c.ReasoningEfforts) != 0 || len(c.StructuredOutputModes) != 0 || c.MaxOutput != nil || !runtimeEmptyObject(s.Parameters) || !runtimeEmptyObject(s.RequestOverwrite) || len(s.HeaderOverwrite) != 0 || s.CredentialRef == nil || r.Model.CredentialLease == nil {
		return nil, fault(f.CapabilityUnsupported)
	}
	copy := r.Clone()
	return &copy, nil
}

// The shared state machine consumes only the common call association here;
// this projection is never validated or sent as a chat request.
func runtimeEmbeddingCall(r mc.EmbeddingRequest) mc.ModelRequest {
	return mc.ModelRequest{Actor: r.Actor, CallID: r.CallID, Consumer: r.Consumer, Model: r.Model, Input: r.Input, RetryClass: mc.BoundedRetry}
}

type runtimeExchangeLifecycle interface {
	Close(context.Context) error
	Joined() bool
	Observe() wire.Observation
}

func (c *runtimeCall) lifecycleExchange() runtimeExchangeLifecycle {
	if c.embeddingExchange != nil {
		return c.embeddingExchange
	}
	if c.exchange != nil {
		return c.exchange
	}
	return nil
}

// Embed shares Chat's current consumer checks, accepted call ledger, Secret
// usage, controlled outbound transport, bounded budget and actual join gate.
func (r *Runtime) Embed(ctx context.Context, request mc.EmbeddingRequest) (result mc.Embeddings, err error) {
	ctx, usage := r.usageOuter(ctx)
	defer func() { err = usage.leave(err) }()
	c, err := r.beginCall(ctx, mc.ModelRequest{}, wire.JSONResponse, &request)
	if err != nil {
		return mc.Embeddings{}, err
	}
	return c.resultEmbedding(ctx)
}

func (c *runtimeCall) resultEmbedding(ctx context.Context) (mc.Embeddings, error) {
	var err error
	if err = c.enter(ctx, true); err != nil {
		c.cancel()
		return mc.Embeddings{}, err
	}
	defer c.leave()
	if err = runtimeContextError(c.ctx); err != nil {
		return mc.Embeddings{}, c.failStart(c.ctx, err)
	}
	value, callErr := c.embeddingExchange.Result(c.wireContext())
	c.completedEmbedding, c.completedJSONError = &value, callErr
	c.wireFailure = callErr != nil
	if err = c.finish(c.ctx, nil, callErr); err != nil {
		return mc.Embeddings{}, err
	}
	if callErr != nil {
		return mc.Embeddings{}, runtimePortError(callErr)
	}
	if c.embeddingResult == nil {
		return mc.Embeddings{}, fault(f.InvalidState)
	}
	return c.embeddingResult.Clone(), nil
}
