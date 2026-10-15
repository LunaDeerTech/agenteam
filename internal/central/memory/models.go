package memory

import (
	"context"
	"errors"
	"strings"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

type ModelResolver interface {
	ResolveModel(context.Context, mc.ResolveRequest) (mc.ResolvedModel, error)
	LookupResolution(context.Context, mc.ResolveRequest) (mc.ResolvedModel, bool, error)
}
type ModelDependencies struct {
	Resolver         ModelResolver
	Chat             mc.JSONCaller
	Embedding        mc.EmbeddingCaller
	Retirement       mc.ModelCallRetirement
	ServingRetention mc.ServingSnapshotRetention
	Profiles         mc.EmbeddingProfileReader
	Stopped          mc.StoppedCallObserver
}

// Models preserves the original Model owner through cancellation and Unknown.
// A failed Close is retained for Drain; no lookup dispatches another request.
type Models struct {
	domain         *Service
	authority      *ModelAuthority
	deps           ModelDependencies
	mu             sync.Mutex
	pending        map[*modelPending]struct{}
	finished       map[*call]struct{}
	builds         map[*retainedGeneration]struct{}
	initializing   bool
	initialized    bool
	initializeDone chan struct{}
}
type modelHandle interface {
	Close(context.Context) error
	Joined() bool
}
type modelPending struct {
	mu                sync.Mutex
	inv               *modelInvocation
	request           mc.ResolveRequest
	resolved          *mc.ResolvedModel
	resolutionUnknown bool
	handle            modelHandle
	retirementUnknown bool
	admitting         bool
	done              bool
}

func NewModels(domain *Service, authority *ModelAuthority, deps ModelDependencies) (*Models, error) {
	if domain == nil || authority == nil || authority.guard == nil || authority.process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	for _, p := range []any{deps.Resolver, deps.Chat, deps.Embedding, deps.Retirement, deps.ServingRetention, deps.Profiles, deps.Stopped} {
		if nilPort(p) {
			return nil, fault(f.DependencyUnbound)
		}
	}
	if err := authority.bind(domain); err != nil {
		return nil, err
	}
	return &Models{domain: domain, authority: authority, deps: deps, pending: map[*modelPending]struct{}{}, finished: map[*call]struct{}{}, builds: map[*retainedGeneration]struct{}{}}, nil
}
func resolveRequest(inv *modelInvocation) mc.ResolveRequest {
	r := mc.ResolveRequest{Actor: inv.actor, Consumer: inv.consumer, Purpose: inv.consumer.Purpose, LeaseOwner: inv.owner}
	if inv.serving != nil {
		snapshot := inv.serving.SnapshotID
		r.Source, r.ServingSnapshotID, r.ServingGenerationID = mc.ServingSnapshotSource, &snapshot, inv.serving.GenerationID
	} else {
		selector := mc.MemorySelector
		if inv.consumer.Purpose == mc.MemoryEmbedding {
			selector = mc.EmbeddingSelector
		}
		r.Source, r.Selection = mc.CurrentSelectionSource, &mc.SelectionRef{Kind: "platform", Selector: selector}
	}
	return r
}
func isUnknown(err error) bool {
	var known *f.Fault
	return errors.As(err, &known) && (known.CommitState == f.Unknown || known.Code == f.CommitUnknown)
}
func (m *Models) prepare(ctx context.Context, entry *call, purpose mc.Purpose, input f.Digest, serving *rc.ServingIdentity, profile *mc.EmbeddingProfile) (context.Context, *modelPending, error) {
	run, inv, err := m.authority.admit(ctx, entry, purpose, input, serving, profile)
	if err != nil {
		return nil, nil, err
	}
	p := &modelPending{inv: inv, request: inv.resolution.Clone(), admitting: true}
	m.mu.Lock()
	m.pending[p] = struct{}{}
	m.mu.Unlock()
	p.mu.Lock()
	resolved, err := m.deps.Resolver.ResolveModel(run, p.request)
	if err == nil {
		if resolved.Validate() != nil || !resolved.Consumer.Equal(inv.consumer) || !resolved.LeaseOwner.Equal(inv.owner) {
			err = fault(f.DependencyUnavailable)
		} else {
			copy := resolved.Clone()
			p.resolved = &copy
			m.authority.mu.Lock()
			invCopy := copy.Clone()
			p.inv.resolved = &invCopy
			m.authority.mu.Unlock()
		}
	}
	p.resolutionUnknown = err != nil
	if err != nil {
		p.admitting = false
	}
	p.mu.Unlock()
	if err != nil {
		return run, p, err
	}
	return run, p, nil
}

func (m *Models) chat(ctx context.Context, entry *call, purpose mc.Purpose, messages []mc.Message, format mc.ResponseFormat) (string, mc.CallID, error) {
	input, err := model.StructuredInputDigest(messages, format)
	if format.Kind == "text" {
		input, err = model.TextInputDigest(messages)
	}
	if err != nil {
		return "", mc.CallID{}, err
	}
	run, p, err := m.prepare(ctx, entry, purpose, input, nil, nil)
	if p == nil {
		return "", mc.CallID{}, err
	}
	if err != nil {
		closeErr := m.close(run, p)
		return "", p.inv.id, modelCallError(err, closeErr)
	}
	p.mu.Lock()
	handle, beginErr := m.deps.Chat.BeginChat(run, mc.ModelRequest{Actor: p.inv.actor, CallID: p.inv.id, Consumer: p.inv.consumer, Model: p.resolved.Clone(), Input: p.inv.input, Messages: messages, Tools: []mc.Tool{}, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: format, RetryClass: mc.BoundedRetry})
	p.handle = handle
	p.admitting = false
	p.mu.Unlock()
	var response mc.ModelResponse
	if beginErr == nil && handle != nil {
		response, err = handle.Result(run)
	} else {
		err = beginErr
		if err == nil {
			err = fault(f.DependencyUnavailable)
		}
	}
	closeErr := m.close(run, p)
	if err != nil || closeErr != nil {
		return "", p.inv.id, modelCallError(err, closeErr)
	}
	if response.Validate() != nil || response.CallID != p.inv.id || response.FinishReason != "stop" {
		return "", p.inv.id, fault(f.DependencyUnavailable)
	}
	var text strings.Builder
	for _, part := range response.Message.Parts {
		if part.Text == nil {
			return "", p.inv.id, fault(f.DependencyUnavailable)
		}
		text.WriteString(part.Text.Text)
	}
	if text.Len() == 0 || text.Len() > 512<<10 {
		return "", p.inv.id, fault(f.DependencyUnavailable)
	}
	return text.String(), p.inv.id, nil
}

func (m *Models) embed(ctx context.Context, entry *call, texts []string, dimension int, serving *rc.ServingIdentity) (mc.Embeddings, mc.SnapshotID, error) {
	return m.embedWithGeneration(ctx, entry, texts, dimension, serving, nil)
}
func (m *Models) embedWithGeneration(ctx context.Context, entry *call, texts []string, dimension int, serving *rc.ServingIdentity, g *retainedGeneration) (mc.Embeddings, mc.SnapshotID, error) {
	input, err := model.EmbeddingInputDigest(texts, dimension)
	if err != nil {
		return mc.Embeddings{}, mc.SnapshotID{}, err
	}
	var profile *mc.EmbeddingProfile
	if g != nil {
		profile = &g.profile
	}
	run, p, err := m.prepare(ctx, entry, mc.MemoryEmbedding, input, serving, profile)
	if p == nil {
		return mc.Embeddings{}, mc.SnapshotID{}, err
	}
	if err != nil {
		closeErr := m.close(run, p)
		return mc.Embeddings{}, mc.SnapshotID{}, modelCallError(err, closeErr)
	}
	if g != nil {
		current, profileErr := m.currentProfile(run, entry)
		if profileErr == nil && (current != g.profile || !profileMatchesSnapshot(g.profile, p.resolved.Snapshot)) {
			profileErr = fault(f.VersionConflict)
		}
		if profileErr == nil {
			profileErr = g.pin(run, p)
		}
		if profileErr != nil {
			p.mu.Lock()
			p.admitting = false
			p.mu.Unlock()
			return mc.Embeddings{}, mc.SnapshotID{}, modelCallError(profileErr, m.close(run, p))
		}
	}
	p.mu.Lock()
	snapshot := p.resolved.Snapshot.ID
	handle, beginErr := m.deps.Embedding.BeginEmbed(run, mc.EmbeddingRequest{NonchatCall: mc.NonchatCall{Actor: p.inv.actor, CallID: p.inv.id, Consumer: p.inv.consumer, Model: p.resolved.Clone(), Input: p.inv.input}, Texts: texts, ExpectedDimensions: dimension})
	p.handle = handle
	p.admitting = false
	p.mu.Unlock()
	var response mc.Embeddings
	if beginErr == nil && handle != nil {
		response, err = handle.Result(run)
	} else {
		err = beginErr
		if err == nil {
			err = fault(f.DependencyUnavailable)
		}
	}
	closeErr := m.close(run, p)
	if err != nil || closeErr != nil {
		return mc.Embeddings{}, mc.SnapshotID{}, modelCallError(err, closeErr)
	}
	if response.ValidateFor(len(texts), dimension) != nil {
		return mc.Embeddings{}, mc.SnapshotID{}, fault(f.DependencyUnavailable)
	}
	return response, snapshot, nil
}

// Only a confirmed original Close permits converting a Model failure to a
// known domain failure. An unjoined owner or Unknown keeps its original error
// and remains registered for Tool Join/Drain; no canonical mutation is inferred.
func modelCallError(resultErr, closeErr error) error {
	if closeErr != nil {
		if isUnknown(closeErr) {
			return errors.Join(closeErr, resultErr)
		}
		return errors.Join(resultErr, closeErr)
	}
	return portError(resultErr)
}

// Close is serialized with admission so Drain cannot retire a resolution that
// is about to be handed to Runtime. Each subsequent close uses a fresh caller
// budget and the same private invocation and original handle.
func (m *Models) close(ctx context.Context, p *modelPending) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return nil
	}
	if p.admitting {
		return fault(f.ResourceBusy)
	}
	run := context.WithValue(context.WithValue(ctx, callKey{}, p.inv.entry), modelInvocationKey{}, p.inv)
	if p.handle != nil {
		err := p.handle.Close(run)
		if !p.handle.Joined() {
			if err != nil {
				return err
			}
			return fault(f.ResourceBusy)
		}
		m.complete(p)
		return nil
	}
	if p.resolutionUnknown {
		// Only the private cleanup path may validate a completed original
		// resolution after the outer Tool lifetime or admission has ended.
		run = context.WithValue(run, modelRecoveryKey{}, p.inv)
		resolved, found, err := m.deps.Resolver.LookupResolution(run, p.request)
		if err != nil {
			return err
		}
		p.resolutionUnknown = false
		if found {
			copy := resolved.Clone()
			p.resolved = &copy
			m.authority.mu.Lock()
			invCopy := copy.Clone()
			p.inv.resolved = &invCopy
			m.authority.mu.Unlock()
		}
	}
	if p.resolved == nil || p.resolved.CredentialLease == nil {
		m.complete(p)
		return nil
	}
	m.authority.mu.Lock()
	p.inv.terminal = true
	m.authority.mu.Unlock()
	r := mc.ModelCallRetirementRequest{Actor: p.inv.actor, Model: p.resolved.Clone(), TerminalVersion: 1}
	plan, err := m.deps.Retirement.DiscoverModelCallRetirement(run, r)
	if err != nil {
		return err
	}
	cause, err := readCause("model-call-retire")
	if err != nil {
		return err
	}
	confirmed := false
	result := m.domain.store.WithinTx(run, cause, func(ctx context.Context, tx f.Tx) error {
		if err := m.domain.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return portError(err)
		}
		x, err := m.domain.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if p.retirementUnknown {
			confirmed, err = m.deps.Retirement.ModelCallRetiredInTx(ctx, tx, r, plan)
			if err != nil || confirmed {
				return err
			}
			// The original complete lock union has now established that the
			// prior terminal commit is absent. Only the same idempotent
			// release may proceed; Resolve and Begin are never replayed.
		}
		count, err := x.Exec(ctx, `UPDATE agenteam_memory.model_calls SET terminal_version=1 WHERE call_id=$1 AND invocation_id IS NULL AND input_digest=$2`, p.inv.id.String(), string(p.inv.input.Digest))
		if err != nil {
			return portError(err)
		}
		if count.RowsAffected() != 1 {
			return fault(f.Forbidden)
		}
		if err := m.deps.Retirement.ReleaseModelCallInTx(ctx, tx, r, plan); err != nil {
			return err
		}
		confirmed = true
		return nil
	})
	err = transactionError(result)
	if isUnknown(err) {
		p.retirementUnknown = true
	}
	if err != nil {
		return err
	}
	if !confirmed {
		return fault(f.ResourceBusy)
	}
	m.complete(p)
	return nil
}

type modelRecoveryKey struct{}

func (m *Models) complete(p *modelPending) {
	p.done = true
	m.authority.forget(p.inv)
	m.mu.Lock()
	delete(m.pending, p)
	entry := p.inv.entry
	finished := m.releaseFinishedLocked(entry)
	m.mu.Unlock()
	if finished {
		m.domain.end(entry)
	}
}
func (m *Models) entryPendingLocked(entry *call) bool {
	for p := range m.pending {
		if p.inv.entry == entry {
			return true
		}
	}
	for g := range m.builds {
		if g.entry == entry {
			return true
		}
	}
	return false
}
func (m *Models) releaseFinishedLocked(entry *call) bool {
	if _, ok := m.finished[entry]; !ok || m.entryPendingLocked(entry) {
		return false
	}
	delete(m.finished, entry)
	return true
}
func (m *Models) finish(entry *call) {
	m.mu.Lock()
	if m.entryPendingLocked(entry) {
		m.finished[entry] = struct{}{}
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.domain.end(entry)
}
func (m *Models) Drain(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	initializing, initialized := m.initializing, m.initializeDone
	m.mu.Unlock()
	if initializing {
		select {
		case <-initialized:
		case <-ctx.Done():
			return portError(ctx.Err())
		}
	}
	m.mu.Lock()
	pending := make([]*modelPending, 0, len(m.pending))
	for p := range m.pending {
		pending = append(pending, p)
	}
	m.mu.Unlock()
	var result error
	for _, p := range pending {
		result = errors.Join(result, m.close(ctx, p))
	}
	m.mu.Lock()
	builds := make([]*retainedGeneration, 0, len(m.builds))
	for g := range m.builds {
		builds = append(builds, g)
	}
	m.mu.Unlock()
	for _, g := range builds {
		result = errors.Join(result, g.cleanup(ctx))
	}
	if result != nil {
		return result
	}
	return m.domain.Drain(ctx)
}
func (m *Models) Joined() bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.initializing && len(m.pending) == 0 && len(m.builds) == 0 && len(m.finished) == 0 && m.domain.Joined()
}

// EmbedQuery shares Model's original serving snapshot and Memory's private
// Tool lifetime; a caller cannot choose a foreign namespace or model profile.
func (m *Models) EmbedQuery(ctx context.Context, request rc.Request) ([]float64, error) {
	p := &RetrievalProvider{domain: m.domain}
	entry, err := p.entry(ctx, request)
	if err != nil {
		return nil, err
	}
	response, _, err := m.embed(ctx, entry, []string{request.Query}, request.Serving.Dimension, &request.Serving)
	if err != nil {
		return nil, err
	}
	return append([]float64(nil), response.Items[0].Values...), nil
}

var _ rc.QueryEmbedder = (*Models)(nil)
