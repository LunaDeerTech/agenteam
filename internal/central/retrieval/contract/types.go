// Package contract defines the domain-neutral hybrid retrieval boundary.
// A Scope describes a query; it is never an authorization grant. Each bound
// domain provider must authorize and filter before either candidate query.
package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type SourceKind string

const (
	Knowledge SourceKind = "knowledge"
	Memory    SourceKind = "memory"
)

type Scope struct {
	Kind      SourceKind
	ProjectID id.ProjectID
	AgentID   *id.AgentID
}

func (s Scope) Validate() error {
	if s.ProjectID.Validate() != nil {
		return invalid()
	}
	switch s.Kind {
	case Knowledge:
		if s.AgentID != nil {
			return invalid()
		}
	case Memory:
		if s.AgentID == nil || s.AgentID.Validate() != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

// ServingIdentity binds query embedding to the actual serving space. For
// Knowledge the generation identifies the project's serving profile; for
// Memory it identifies the namespace's serving index generation.
type ServingIdentity struct {
	GenerationID string
	SnapshotID   mc.SnapshotID
	Dimension    int
}

func (s ServingIdentity) Validate() error {
	if !uuid(s.GenerationID) || s.SnapshotID.Validate() != nil || s.Dimension < 1 || s.Dimension > 4096 {
		return invalid()
	}
	return nil
}

// Filters are closed domain filters, pushed into both retrieval legs. They
// are not an SQL expression, and the runtime never filters global candidates.
type Filters struct {
	DocumentID         string
	MediaType          string
	DocumentSourceKind string
	MemoryTypes        []string
	Tags               []string
}

func (q Filters) ValidateFor(kind SourceKind) error {
	switch kind {
	case Knowledge:
		if q.DocumentID != "" && !uuid(q.DocumentID) || len(q.MemoryTypes) != 0 || len(q.Tags) != 0 || !optionalText(q.MediaType, 255) || !optionalText(q.DocumentSourceKind, 64) {
			return invalid()
		}
	case Memory:
		if q.DocumentID != "" || q.MediaType != "" || q.DocumentSourceKind != "" || len(q.MemoryTypes) > 64 || len(q.Tags) > 64 {
			return invalid()
		}
		for _, values := range [][]string{q.MemoryTypes, q.Tags} {
			seen := make(map[string]bool, len(values))
			for _, value := range values {
				if !text(value, 128) || seen[value] {
					return invalid()
				}
				seen[value] = true
			}
		}
	default:
		return invalid()
	}
	return nil
}

// Limits are supplied by the active runtime configuration. No query can
// change leg weights: fusion always gives the two ranks equal weight.
type Limits struct {
	LexicalK, DenseK, FusedK, ResultK int
	RRFConstant, MaxResultBytes       int
}

func (l Limits) Validate() error {
	if l.LexicalK < 1 || l.LexicalK > 4096 || l.DenseK < 1 || l.DenseK > 4096 || l.FusedK < 1 || l.FusedK > l.LexicalK+l.DenseK || l.ResultK < 1 || l.ResultK > l.FusedK || l.RRFConstant < 1 || l.RRFConstant > 10000 || l.MaxResultBytes < 1 || l.MaxResultBytes > 1<<20 {
		return invalid()
	}
	return nil
}

type Request struct {
	Query   string
	Scope   Scope
	Serving ServingIdentity
	Limits  Limits
	Filters Filters
}

func (r Request) Validate() error {
	if !text(r.Query, 16<<10) || strings.TrimSpace(r.Query) == "" || r.Scope.Validate() != nil || r.Serving.Validate() != nil || r.Limits.Validate() != nil || r.Filters.ValidateFor(r.Scope.Kind) != nil {
		return invalid()
	}
	return nil
}

func (r Request) Clone() Request {
	if r.Scope.AgentID != nil {
		v := *r.Scope.AgentID
		r.Scope.AgentID = &v
	}
	r.Filters.MemoryTypes = append([]string(nil), r.Filters.MemoryTypes...)
	r.Filters.Tags = append([]string(nil), r.Filters.Tags...)
	return r
}

// Knowledge uses the chunk UUID and content version. Memory uses the Memory
// UUID and its active revision. The pair is the sole fusion merge identity.
type SourceKey struct {
	ID       string
	Revision f.Version
}

func (s SourceKey) Validate() error {
	if !uuid(s.ID) || s.Revision.Validate() != nil {
		return invalid()
	}
	return nil
}

// Candidate is an internal authorized projection. Tool/HTTP adapters project
// explicit fields instead of implicitly serializing the private source text.
type Candidate struct {
	Source      SourceKey
	RawText     string
	Locator     json.RawMessage
	NativeScore *float64
}

func (c Candidate) Validate() error {
	if c.Source.Validate() != nil || !text(c.RawText, 1<<20) || len(c.Locator) > 16<<10 || len(c.Locator) != 0 && !json.Valid(c.Locator) || c.NativeScore != nil && (math.IsNaN(*c.NativeScore) || math.IsInf(*c.NativeScore, 0)) {
		return invalid()
	}
	return nil
}

func (c Candidate) Clone() Candidate {
	c.Locator = append(json.RawMessage(nil), c.Locator...)
	if c.NativeScore != nil {
		v := *c.NativeScore
		c.NativeScore = &v
	}
	return c
}

type RankedHit struct {
	Candidate
	LexicalRank int
	DenseRank   int
	FusedScore  float64
}

type Mode string

const (
	Hybrid      Mode = "hybrid"
	LexicalOnly Mode = "lexical_only"
	DenseOnly   Mode = "dense_only"
)

type Result struct {
	Hits      []RankedHit
	Mode      Mode
	Degraded  bool
	Truncated bool
}

// LegProvider belongs to one domain. Every leg must verify its actual caller
// authority and push scope/current/active/ready predicates into its query.
// ValidateServing is called before retrieval and again before publication;
// it rejects concurrent generation changes and never substitutes new identity.
type LegProvider interface {
	Lexical(context.Context, Request) ([]Candidate, error)
	Dense(context.Context, Request, []float64) ([]Candidate, error)
	ValidateServing(context.Context, Request) error
}

type QueryEmbedder interface {
	EmbedQuery(context.Context, Request) ([]float64, error)
}

type Searcher interface {
	Search(context.Context, Request) (Result, error)
}

func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func uuid(v string) bool {
	_, err := f.ParseID[struct{}](v)
	return err == nil
}
func text(v string, limit int) bool {
	return len(v) > 0 && optionalText(v, limit)
}
func optionalText(v string, limit int) bool {
	return len(v) <= limit && utf8.ValidString(v) && !strings.ContainsRune(v, 0)
}

func (Request) Format(w fmt.State, _ rune)     { _, _ = io.WriteString(w, "retrieval_request") }
func (Request) LogValue() slog.Value           { return slog.StringValue("retrieval_request") }
func (Request) MarshalJSON() ([]byte, error)   { return []byte(`"retrieval_request"`), nil }
func (Candidate) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "retrieval_candidate") }
func (Candidate) LogValue() slog.Value         { return slog.StringValue("retrieval_candidate") }
func (Candidate) MarshalJSON() ([]byte, error) { return []byte(`"retrieval_candidate"`), nil }
func (Result) Format(w fmt.State, _ rune)      { _, _ = io.WriteString(w, "retrieval_result") }
func (Result) LogValue() slog.Value            { return slog.StringValue("retrieval_result") }
func (Result) MarshalJSON() ([]byte, error)    { return []byte(`"retrieval_result"`), nil }
