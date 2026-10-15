// Package runtime runs the same rank-based hybrid pipeline for Knowledge and
// Memory. Domain providers retain authorization and canonical eligibility.
package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"slices"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

type Timeouts struct {
	Lexical, Embedding, Dense time.Duration
}

type Runtime struct {
	provider rc.LegProvider
	embedder rc.QueryEmbedder
	timeouts Timeouts
	observer rc.Observer
}

func New(provider rc.LegProvider, embedder rc.QueryEmbedder, timeouts Timeouts, observers ...rc.Observer) (*Runtime, error) {
	if nilPort(provider) || nilPort(embedder) {
		return nil, fault(f.DependencyUnbound)
	}
	for _, timeout := range []time.Duration{timeouts.Lexical, timeouts.Embedding, timeouts.Dense} {
		if timeout <= 0 || timeout > 2*time.Minute {
			return nil, fault(f.InvalidArgument)
		}
	}
	if len(observers) > 1 || len(observers) == 1 && nilPort(observers[0]) {
		return nil, fault(f.InvalidArgument)
	}
	r := &Runtime{provider: provider, embedder: embedder, timeouts: timeouts}
	if len(observers) == 1 {
		r.observer = observers[0]
	}
	return r, nil
}

type legResult struct {
	items   []rc.Candidate
	err     error
	elapsed time.Duration
}

func (r *Runtime) Search(ctx context.Context, request rc.Request) (out rc.Result, resultErr error) {
	if ctx == nil || request.Validate() != nil {
		return rc.Result{}, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return rc.Result{}, err
	}
	if r == nil || nilPort(r.provider) || nilPort(r.embedder) {
		return rc.Result{}, fault(f.DependencyUnbound)
	}
	request = request.Clone()
	started := time.Now()
	diagnostic := rc.Diagnostics{Domain: request.Scope.Kind, ProjectID: request.Scope.ProjectID.String(), QueryBytes: len(request.Query)}
	if request.Scope.AgentID != nil {
		diagnostic.AgentID = request.Scope.AgentID.String()
	}
	if !nilPort(r.observer) {
		// Failure to allocate a diagnostic ID must not change business behavior.
		if key, err := f.NewID[struct{}](); err == nil {
			diagnostic.RequestID = key.String()
		}
		digest := sha256.Sum256([]byte(request.Query))
		diagnostic.QuerySHA256 = hex.EncodeToString(digest[:])
		defer func() {
			diagnostic.Total = time.Since(started)
			diagnostic.Mode, diagnostic.ResultCount = out.Mode, len(out.Hits)
			if resultErr != nil {
				diagnostic.Mode, diagnostic.ResultCount = "", 0
				diagnostic.ErrorCode = diagnosticError(resultErr)
			}
			observe(r.observer, ctx, diagnostic)
		}()
	}
	if err := r.provider.ValidateServing(ctx, request.Clone()); err != nil {
		return rc.Result{}, err
	}
	if provider, ok := r.provider.(rc.DiagnosticProvider); ok {
		switch backend := provider.DiagnosticBackend(ctx, request.Clone()); backend {
		case "native_pg_fts", "pgroonga", "pg_search":
			diagnostic.Backend = backend
		}
	}
	var lexical, dense legResult
	var embeddingElapsed time.Duration
	var joined sync.WaitGroup
	joined.Add(2)
	go func() {
		defer joined.Done()
		started := time.Now()
		defer func() { lexical.elapsed = time.Since(started) }()
		stage, cancel := context.WithTimeout(ctx, r.timeouts.Lexical)
		defer cancel()
		lexical.items, lexical.err = r.provider.Lexical(stage, request.Clone())
		if lexical.err == nil {
			lexical.err = stage.Err()
		}
	}()
	go func() {
		defer joined.Done()
		started := time.Now()
		stage, cancel := context.WithTimeout(ctx, r.timeouts.Embedding)
		vector, err := r.embedder.EmbedQuery(stage, request.Clone())
		embeddingElapsed = time.Since(started)
		if err == nil {
			err = stage.Err()
		}
		cancel()
		if err != nil {
			dense.err = err
			return
		}
		if len(vector) != request.Serving.Dimension {
			dense.err = fault(f.InvalidState)
			return
		}
		for _, value := range vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				dense.err = fault(f.InvalidState)
				return
			}
		}
		stage, cancel = context.WithTimeout(ctx, r.timeouts.Dense)
		defer cancel()
		started = time.Now()
		dense.items, dense.err = r.provider.Dense(stage, request.Clone(), append([]float64(nil), vector...))
		dense.elapsed = time.Since(started)
		if dense.err == nil {
			dense.err = stage.Err()
		}
	}()
	// Cancellation is not an ownership transfer. Both actual callbacks must
	// return before the request can finish or expose any result.
	joined.Wait()
	diagnostic.Lexical, diagnostic.Embedding, diagnostic.Dense = lexical.elapsed, embeddingElapsed, dense.elapsed
	diagnostic.LexicalCandidates, diagnostic.DenseCandidates = len(lexical.items), len(dense.items)
	if err := ctx.Err(); err != nil {
		return rc.Result{}, err
	}
	for _, err := range []error{lexical.err, dense.err} {
		if err != nil && !transient(err) {
			return rc.Result{}, err
		}
	}
	if lexical.err != nil && dense.err != nil {
		return rc.Result{}, fault(f.DependencyUnavailable).WithCause(errors.Join(lexical.err, dense.err))
	}
	mode := rc.Hybrid
	if lexical.err != nil {
		lexical.items, mode = nil, rc.DenseOnly
		diagnostic.DegradationReason = "lexical_unavailable"
	}
	if dense.err != nil {
		dense.items, mode = nil, rc.LexicalOnly
		diagnostic.DegradationReason = "dense_unavailable"
	}
	fusionStarted := time.Now()
	hits, err := fuse(lexical.items, dense.items, request.Limits)
	diagnostic.Fusion = time.Since(fusionStarted)
	diagnostic.FusionCandidates = len(hits)
	if err != nil {
		return rc.Result{}, err
	}
	if err = r.provider.ValidateServing(ctx, request.Clone()); err != nil {
		return rc.Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return rc.Result{}, err
	}
	result := rc.Result{Hits: make([]rc.RankedHit, 0, min(request.Limits.ResultK, len(hits))), Mode: mode, Degraded: mode != rc.Hybrid}
	remaining := request.Limits.MaxResultBytes
	for _, hit := range hits[:min(len(hits), request.Limits.ResultK)] {
		// Full Memory entries remain intact. Knowledge's provider supplies a
		// compact raw-text snippet, so budgets never slice UTF-8 or citations.
		size := len(hit.RawText) + len(hit.Locator)
		if size > remaining {
			result.Truncated = true
			break
		}
		remaining -= size
		result.Hits = append(result.Hits, hit)
	}
	return result, nil
}

func diagnosticError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "DEADLINE_EXCEEDED"
	}
	var known *f.Fault
	if errors.As(err, &known) {
		return string(known.Code.Safe())
	}
	return string(f.InternalError)
}

func observe(observer rc.Observer, ctx context.Context, diagnostic rc.Diagnostics) {
	// Observability is not a business owner. A faulty diagnostics adapter must
	// not replace the already-computed retrieval result or bypass its joins.
	defer func() { _ = recover() }()
	observer.ObserveRetrieval(ctx, diagnostic)
}

func fuse(lexical, dense []rc.Candidate, limits rc.Limits) ([]rc.RankedHit, error) {
	if len(lexical) > limits.LexicalK || len(dense) > limits.DenseK {
		return nil, fault(f.InvalidState)
	}
	merged := make(map[rc.SourceKey]*rc.RankedHit, len(lexical)+len(dense))
	for leg, candidates := range [][]rc.Candidate{lexical, dense} {
		seen := make(map[rc.SourceKey]bool, len(candidates))
		for index, candidate := range candidates {
			if candidate.Validate() != nil || seen[candidate.Source] {
				return nil, fault(f.InvalidState)
			}
			seen[candidate.Source] = true
			hit := merged[candidate.Source]
			if hit == nil {
				hit = &rc.RankedHit{Candidate: candidate.Clone()}
				merged[candidate.Source] = hit
			} else if hit.RawText != candidate.RawText || !bytes.Equal(hit.Locator, candidate.Locator) {
				// Equal source identities with different projections indicate
				// inconsistent generation reads, never a degradable backend error.
				return nil, fault(f.InvalidState)
			}
			rank := index + 1
			if leg == 0 {
				hit.LexicalRank = rank
			} else {
				hit.DenseRank = rank
			}
			hit.FusedScore += 1 / float64(limits.RRFConstant+rank)
		}
	}
	hits := make([]rc.RankedHit, 0, len(merged))
	for _, hit := range merged {
		hits = append(hits, *hit)
	}
	slices.SortFunc(hits, func(a, b rc.RankedHit) int {
		if a.FusedScore > b.FusedScore {
			return -1
		}
		if a.FusedScore < b.FusedScore {
			return 1
		}
		if a.Source.ID < b.Source.ID {
			return -1
		}
		if a.Source.ID > b.Source.ID {
			return 1
		}
		if a.Source.Revision < b.Source.Revision {
			return -1
		}
		if a.Source.Revision > b.Source.Revision {
			return 1
		}
		return 0
	})
	return hits[:min(limits.FusedK, len(hits))], nil
}

func transient(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var known *f.Fault
	return errors.As(err, &known) && known.Code == f.DependencyUnavailable
}
func fault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return x.IsNil()
	}
	return false
}
