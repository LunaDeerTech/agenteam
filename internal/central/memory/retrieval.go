package memory

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
	"github.com/jackc/pgx/v5"
)

// RetrievalProvider owns authorization and canonical eligibility. Rank fusion,
// per-leg deadlines and budgets belong to the shared D13 Runtime.
type RetrievalProvider struct {
	domain  *Service
	backend lexical.Backend
}

func NewRetrievalProvider(domain *Service, backend lexical.Backend) (*RetrievalProvider, error) {
	if domain == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if _, err := lexical.Expressions(backend, lexical.MemoryProjection, 4); err != nil {
		return nil, err
	}
	return &RetrievalProvider{domain: domain, backend: backend}, nil
}

func (p *RetrievalProvider) Lexical(ctx context.Context, request rc.Request) ([]rc.Candidate, error) {
	entry, err := p.entry(ctx, request)
	if err != nil {
		return nil, err
	}
	expressions, err := lexical.Expressions(p.backend, lexical.MemoryProjection, 4)
	if err != nil {
		return nil, err
	}
	cause, err := readCause("lexical-recall")
	if err != nil {
		return nil, err
	}
	out := []rc.Candidate{}
	err = p.domain.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := requireServing(ctx, x, entry.namespace, request.Serving); err != nil {
			return err
		}
		rows, err := x.Query(ctx, `SELECT p.memory_id::text,p.revision,r.body,`+expressions.Score+` FROM agenteam_memory.projections p JOIN agenteam_memory.memories m ON (m.project_id,m.agent_id,m.memory_id,m.current_revision)=(p.project_id,p.agent_id,p.memory_id,p.revision) JOIN agenteam_memory.revisions r ON (r.project_id,r.agent_id,r.memory_id,r.revision)=(p.project_id,p.agent_id,p.memory_id,p.revision) WHERE p.project_id=$1 AND p.agent_id=$2 AND p.generation_id=$3 AND m.status='active' AND r.status='active' AND (`+expressions.Match+`) AND (cardinality($5::text[])=0 OR r.type=ANY($5)) AND r.tags @> $6::text[] ORDER BY `+expressions.Score+` DESC,p.memory_id LIMIT $7`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), request.Serving.GenerationID, request.Query, append([]string{}, request.Filters.MemoryTypes...), append([]string{}, request.Filters.Tags...), request.Limits.LexicalK)
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var candidate rc.Candidate
			var revision int64
			var score float64
			if err := rows.Scan(&candidate.Source.ID, &revision, &candidate.RawText, &score); err != nil {
				return portError(err)
			}
			candidate.Source.Revision = f.Version(revision)
			candidate.NativeScore = &score
			if candidate.Validate() != nil {
				return fault(f.DependencyUnavailable)
			}
			out = append(out, candidate)
		}
		return portError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

var _ rc.LegProvider = (*RetrievalProvider)(nil)

func (p *RetrievalProvider) entry(ctx context.Context, request rc.Request) (*call, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if p == nil || p.domain == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if request.Validate() != nil || request.Scope.Kind != rc.Memory {
		return nil, fault(f.InvalidArgument)
	}
	entry, _ := ctx.Value(callKey{}).(*call)
	if entry == nil || entry.service != p.domain || entry.namespace.ProjectID != request.Scope.ProjectID || entry.namespace.AgentID != *request.Scope.AgentID || entry.intent != "retain" && entry.intent != "recall" && entry.intent != "reflect" {
		return nil, fault(f.Forbidden)
	}
	filters := c.Filters{Tags: request.Filters.Tags}
	for _, t := range request.Filters.MemoryTypes {
		filters.Types = append(filters.Types, c.Type(t))
	}
	if filters.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	return entry, nil
}
func requireServing(ctx context.Context, x postgres.SQLExecutor, n c.Namespace, serving rc.ServingIdentity) error {
	var matches bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_memory.namespaces n JOIN agenteam_memory.generations g ON g.generation_id=n.serving_generation_id WHERE n.project_id=$1 AND n.agent_id=$2 AND g.project_id=n.project_id AND g.agent_id=n.agent_id AND g.generation_id=$3 AND g.snapshot_id=$4 AND g.dimension=$5 AND g.status='ready' AND g.owner_version=1 AND g.retention_state='retained')`, n.ProjectID.String(), n.AgentID.String(), serving.GenerationID, serving.SnapshotID.String(), serving.Dimension).Scan(&matches)
	if err != nil {
		return portError(err)
	}
	if !matches {
		return fault(f.InvalidState)
	}
	return nil
}
func (p *RetrievalProvider) ValidateServing(ctx context.Context, request rc.Request) error {
	entry, err := p.entry(ctx, request)
	if err != nil {
		return err
	}
	cause, err := readCause("validate-serving")
	if err != nil {
		return err
	}
	return p.domain.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return requireServing(ctx, x, entry.namespace, request.Serving)
	})
}
func vectorText(vector []float64, dimension int) (string, error) {
	if len(vector) != dimension || dimension < 1 || dimension > 4096 {
		return "", fault(f.InvalidArgument)
	}
	var sum float64
	var b strings.Builder
	b.WriteByte('[')
	for n, v := range vector {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
			return "", fault(f.InvalidArgument)
		}
		sum += v * v
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
	}
	b.WriteByte(']')
	if sum == 0 || math.IsInf(sum, 0) {
		return "", fault(f.InvalidArgument)
	}
	return b.String(), nil
}
func (p *RetrievalProvider) Dense(ctx context.Context, request rc.Request, vector []float64) ([]rc.Candidate, error) {
	entry, err := p.entry(ctx, request)
	if err != nil {
		return nil, err
	}
	encoded, err := vectorText(vector, request.Serving.Dimension)
	if err != nil {
		return nil, err
	}
	cause, err := readCause("dense-recall")
	if err != nil {
		return nil, err
	}
	out := []rc.Candidate{}
	err = p.domain.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := requireServing(ctx, x, entry.namespace, request.Serving); err != nil {
			return err
		}
		rows, err := x.Query(ctx, `SELECT p.memory_id::text,p.revision,r.body,1-(p.embedding <=> $4::vector) FROM agenteam_memory.projections p JOIN agenteam_memory.memories m ON (m.project_id,m.agent_id,m.memory_id,m.current_revision)=(p.project_id,p.agent_id,p.memory_id,p.revision) JOIN agenteam_memory.revisions r ON (r.project_id,r.agent_id,r.memory_id,r.revision)=(p.project_id,p.agent_id,p.memory_id,p.revision) WHERE p.project_id=$1 AND p.agent_id=$2 AND p.generation_id=$3 AND m.status='active' AND r.status='active' AND (cardinality($5::text[])=0 OR r.type=ANY($5)) AND r.tags @> $6::text[] ORDER BY p.embedding <=> $4::vector,p.memory_id LIMIT $7`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), request.Serving.GenerationID, encoded, append([]string{}, request.Filters.MemoryTypes...), append([]string{}, request.Filters.Tags...), request.Limits.DenseK)
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var candidate rc.Candidate
			var revision int64
			var score float64
			if err := rows.Scan(&candidate.Source.ID, &revision, &candidate.RawText, &score); err != nil {
				return portError(err)
			}
			candidate.Source.Revision = f.Version(revision)
			candidate.NativeScore = &score
			if candidate.Validate() != nil {
				return fault(f.DependencyUnavailable)
			}
			out = append(out, candidate)
		}
		return portError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) serving(ctx context.Context, entry *call) (rc.ServingIdentity, bool, error) {
	var out rc.ServingIdentity
	var exists bool
	cause, err := readCause("serving-generation")
	if err != nil {
		return out, false, err
	}
	err = s.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var snapshot string
		err := x.QueryRow(ctx, `SELECT g.generation_id::text,g.snapshot_id::text,g.dimension FROM agenteam_memory.namespaces n JOIN agenteam_memory.generations g ON (g.project_id,g.agent_id,g.generation_id)=(n.project_id,n.agent_id,n.serving_generation_id) WHERE n.project_id=$1 AND n.agent_id=$2 AND g.status='ready' AND g.owner_version=1 AND g.retention_state='retained'`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String()).Scan(&out.GenerationID, &snapshot, &out.Dimension)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return portError(err)
		}
		if err = out.SnapshotID.UnmarshalText([]byte(snapshot)); err != nil || out.Validate() != nil {
			return fault(f.DependencyUnavailable)
		}
		exists = true
		return nil
	})
	return out, exists, err
}
func (s *Service) hasActive(ctx context.Context, entry *call) (bool, error) {
	var active bool
	cause, err := readCause("has-active")
	if err != nil {
		return false, err
	}
	err = s.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return portError(x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_memory.memories WHERE project_id=$1 AND agent_id=$2 AND status='active')`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String()).Scan(&active))
	})
	return active, err
}
func (s *Service) projectRecall(ctx context.Context, entry *call, serving rc.ServingIdentity, result rc.Result) (c.RecallResult, error) {
	out := c.RecallResult{Memories: []c.Revision{}, Mode: string(result.Mode), Degraded: result.Degraded}
	cause, err := readCause("recall-projection")
	if err != nil {
		return out, err
	}
	err = s.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := requireServing(ctx, x, entry.namespace, serving); err != nil {
			return err
		}
		for _, hit := range result.Hits {
			if hit.Candidate.Validate() != nil {
				return fault(f.DependencyUnavailable)
			}
			id, err := f.ParseID[c.Memory](hit.Source.ID)
			if err != nil {
				return fault(f.DependencyUnavailable)
			}
			record, err := readRecord(ctx, x, entry.namespace, id)
			if err != nil {
				return err
			}
			if record == nil || record.Current.Status != c.Active || record.Current.Revision != hit.Source.Revision {
				continue
			}
			if record.Current.Text != hit.RawText {
				return fault(f.DependencyUnavailable)
			}
			out.Memories = append(out.Memories, record.Current)
		}
		return nil
	})
	if err != nil {
		return c.RecallResult{}, err
	}
	return out, nil
}
