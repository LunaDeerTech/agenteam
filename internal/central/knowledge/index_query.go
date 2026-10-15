package knowledge

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
)

type indexProfile struct {
	Version   f.Version
	ID        string
	Project   id.ProjectID
	Snapshot  mc.SnapshotID
	Dimension int
	Backend   lexical.Backend
}

func (p indexProfile) serving() rc.ServingIdentity {
	return rc.ServingIdentity{GenerationID: p.ID, SnapshotID: p.Snapshot, Dimension: p.Dimension}
}

type indexQueryKey struct{}
type indexQuery struct {
	service    *IndexService
	invocation kc.Invocation
	request    kc.QueryRequest
	profile    indexProfile
}

func (s *IndexService) ReadForInvocation(ctx context.Context, invocation kc.Invocation, document kc.DocumentID, version f.Version, request kc.ReadRequest) (kc.DocumentContent, error) {
	if s == nil {
		return kc.DocumentContent{}, fault(f.DependencyUnbound)
	}
	ctx, done, err := s.admit(ctx)
	if err != nil {
		return kc.DocumentContent{}, err
	}
	defer done()
	out, err := s.documents.ReadForInvocation(ctx, invocation, document, version, request)
	if err != nil || out.Document.SourceKind != kc.File {
		return out, err
	}
	return s.readFileProjection(ctx, invocation, document, version, request)
}

func (s *IndexService) QueryDocuments(ctx context.Context, invocation kc.Invocation, request kc.QueryRequest) (out kc.QueryResult, resultErr error) {
	if s == nil {
		return out, fault(f.DependencyUnbound)
	}
	if request.Validate() != nil || request.Limit > s.deps.Settings.Retrieval.ResultK {
		return out, fault(f.InvalidArgument)
	}
	ctx, done, err := s.admit(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	ctx, err = s.documents.invocationContext(ctx, invocation, kc.DocumentID{}, 0, kc.ReadRequest{}, &request)
	if err != nil {
		return out, err
	}
	d := invocation.Details()
	ctx, entry, err := s.documents.beginProjectCall(ctx, d.Actor, d.ProjectID, nil, projectCallRead)
	defer s.documents.projectCallDone(entry, &resultErr)
	if err != nil {
		return out, err
	}
	var profile indexProfile
	err = s.documents.read(ctx, d.Actor, d.ProjectID, func(ctx context.Context, x postgres.SQLExecutor) error {
		var snapshot, backend string
		profile.Project = d.ProjectID
		if err := x.QueryRow(ctx, `SELECT p.id::text,p.snapshot_id::text,p.dimension,p.lexical_backend,p.version
 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p
 ON p.project_id=st.project_id AND p.id=st.serving_profile_id
 WHERE st.project_id=$1 AND p.state='active'`, d.ProjectID.String()).Scan(&profile.ID, &snapshot, &profile.Dimension, &backend, &profile.Version); err != nil {
			return unavailable(err)
		}
		var err error
		profile.Snapshot, err = f.ParseID[mc.Snapshot](snapshot)
		profile.Backend = lexical.Backend(backend)
		if err != nil || profile.serving().Validate() != nil {
			return fault(f.InvalidState)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	q := &indexQuery{service: s, invocation: invocation, request: request, profile: profile}
	ctx = context.WithValue(ctx, indexQueryKey{}, q)
	limits := s.deps.Settings.Retrieval
	limits.ResultK = request.Limit
	result, err := s.search.Search(ctx, rc.Request{Query: request.Query, Scope: rc.Scope{Kind: rc.Knowledge, ProjectID: d.ProjectID}, Serving: profile.serving(), Limits: limits, Filters: request.Filters})
	if err != nil {
		return out, err
	}
	out = kc.QueryResult{Hits: make([]kc.QueryHit, 0, len(result.Hits)), Degraded: result.Degraded, Truncated: result.Truncated}
	err = s.documents.read(ctx, d.Actor, d.ProjectID, func(ctx context.Context, x postgres.SQLExecutor) error {
		for _, hit := range result.Hits {
			var citation knowledgeCandidateLocator
			if json.Unmarshal(hit.Locator, &citation) != nil || citation.Document.Validate() != nil || citation.Version.Validate() != nil || citation.Version != hit.Source.Revision || kc.ValidateTitle(citation.Title) != nil || !json.Valid(citation.Position) {
				return fault(f.InvalidState)
			}
			var current bool
			if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.documents d JOIN agenteam_knowledge.index_state st ON st.project_id=d.project_id WHERE d.project_id=$1 AND d.id=$2 AND d.status='active' AND d.content_version=$3 AND st.serving_profile_id=$4)`, d.ProjectID.String(), citation.Document.String(), int64(citation.Version), profile.ID).Scan(&current); err != nil {
				return unavailable(err)
			}
			if !current {
				return fault(f.VersionConflict)
			}
			out.Hits = append(out.Hits, kc.QueryHit{DocumentID: citation.Document, DocumentVersion: citation.Version, Title: citation.Title, MediaType: citation.MediaType, Snippet: hit.RawText, Locator: citation.Position})
		}
		return nil
	})
	if err != nil {
		return kc.QueryResult{}, err
	}
	return out, nil
}

type knowledgeCandidateLocator struct {
	Document  kc.DocumentID   `json:"document_id"`
	Version   f.Version       `json:"document_version"`
	Title     string          `json:"title"`
	MediaType string          `json:"media_type"`
	Position  json.RawMessage `json:"position"`
}

func (s *IndexService) queryOwner(ctx context.Context, request rc.Request) (*indexQuery, error) {
	if s == nil || ctx == nil || request.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	q, ok := ctx.Value(indexQueryKey{}).(*indexQuery)
	if !ok || q == nil || q.service != s || q.invocation.Validate() != nil || request.Scope.Kind != rc.Knowledge || request.Scope.ProjectID != q.profile.Project || request.Serving != q.profile.serving() || request.Query != q.request.Query || request.Filters.DocumentID != q.request.Filters.DocumentID || request.Filters.MediaType != q.request.Filters.MediaType || request.Filters.DocumentSourceKind != q.request.Filters.DocumentSourceKind {
		return nil, fault(f.Forbidden)
	}
	return q, nil
}

func (s *IndexService) ValidateServing(ctx context.Context, request rc.Request) error {
	q, err := s.queryOwner(ctx, request)
	if err != nil {
		return err
	}
	d := q.invocation.Details()
	return s.documents.read(ctx, d.Actor, d.ProjectID, func(ctx context.Context, x postgres.SQLExecutor) error {
		var matches bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.serving_profile_id WHERE st.project_id=$1 AND p.id=$2 AND p.state='active' AND p.snapshot_id=$3 AND p.dimension=$4)`, d.ProjectID.String(), request.Serving.GenerationID, request.Serving.SnapshotID.String(), request.Serving.Dimension).Scan(&matches); err != nil {
			return unavailable(err)
		}
		if !matches {
			return fault(f.VersionConflict)
		}
		return nil
	})
}

func (s *IndexService) DiagnosticBackend(ctx context.Context, request rc.Request) string {
	q, err := s.queryOwner(ctx, request)
	if err != nil {
		return ""
	}
	return string(q.profile.Backend)
}

const knowledgeCandidateProjection = `c.id::text,d.content_version,
 c.raw_text,jsonb_build_object('document_id',d.id,'document_version',d.content_version::text,
 'title',d.title,'media_type',d.media_type,'position',c.locator)`
const knowledgeCandidateFrom = ` FROM agenteam_knowledge.index_chunks c
 JOIN agenteam_knowledge.index_generations g ON g.project_id=c.project_id AND g.id=c.generation_id
 JOIN agenteam_knowledge.documents d ON d.project_id=g.project_id AND d.id=g.document_id
 JOIN agenteam_knowledge.index_state st ON st.project_id=g.project_id AND st.serving_profile_id=g.profile_id
 JOIN agenteam_knowledge.index_profiles ip ON ip.project_id=g.project_id AND ip.id=g.profile_id
 WHERE c.project_id=$1 AND g.profile_id=$2 AND g.state='ready' AND ip.state='active'
 AND g.snapshot_id=ip.snapshot_id AND g.dimension=ip.dimension AND c.dimension=ip.dimension
 AND d.status='active' AND d.content_version=g.document_version
 AND ($3::uuid IS NULL OR d.id=$3) AND ($4::text='' OR d.media_type=$4) AND ($5::text='' OR d.source_kind=$5)`

func (s *IndexService) candidates(ctx context.Context, request rc.Request, expression lexical.SQL, query any, limit int, dense bool) ([]rc.Candidate, error) {
	q, err := s.queryOwner(ctx, request)
	if err != nil {
		return nil, err
	}
	d := q.invocation.Details()
	var out []rc.Candidate
	err = s.documents.read(ctx, d.Actor, d.ProjectID, func(ctx context.Context, x postgres.SQLExecutor) error {
		var document any
		if request.Filters.DocumentID != "" {
			document = request.Filters.DocumentID
		}
		sql := `SELECT ` + knowledgeCandidateProjection + `,` + expression.Score + knowledgeCandidateFrom
		if expression.Match != "" {
			sql += ` AND (` + expression.Match + `)`
		}
		if dense {
			sql += ` ORDER BY c.embedding <=> $6::vector ASC,c.id ASC LIMIT $7`
		} else {
			sql += ` ORDER BY ` + expression.Score + ` DESC,c.id ASC LIMIT $7`
		}
		rows, err := x.Query(ctx, sql, d.ProjectID.String(), request.Serving.GenerationID, document, request.Filters.MediaType, request.Filters.DocumentSourceKind, query, limit)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		out = make([]rc.Candidate, 0, limit)
		for rows.Next() {
			var hit rc.Candidate
			var version int64
			var score float64
			if err = rows.Scan(&hit.Source.ID, &version, &hit.RawText, &hit.Locator, &score); err != nil {
				return unavailable(err)
			}
			hit.Source.Revision = f.Version(version)
			hit.NativeScore = &score
			if len(hit.RawText) > 1024 {
				at := 1024
				for !utf8.RuneStart(hit.RawText[at]) {
					at--
				}
				hit.RawText = hit.RawText[:at]
			}
			if hit.Validate() != nil {
				return fault(f.InvalidState)
			}
			out = append(out, hit)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *IndexService) Lexical(ctx context.Context, request rc.Request) ([]rc.Candidate, error) {
	q, err := s.queryOwner(ctx, request)
	if err != nil {
		return nil, err
	}
	expression, err := lexical.Expressions(q.profile.Backend, lexical.KnowledgeChunks, 6)
	if err != nil {
		return nil, err
	}
	return s.candidates(ctx, request, expression, request.Query, request.Limits.LexicalK, false)
}
func (s *IndexService) Dense(ctx context.Context, request rc.Request, vector []float64) ([]rc.Candidate, error) {
	if len(vector) != request.Serving.Dimension {
		return nil, fault(f.InvalidState)
	}
	value, err := vectorText(vector)
	if err != nil {
		return nil, err
	}
	return s.candidates(ctx, request, lexical.SQL{Score: `1-(c.embedding <=> $6::vector)`}, value, request.Limits.DenseK, true)
}
func vectorText(vector []float64) (string, error) {
	if len(vector) == 0 || len(vector) > 4096 {
		return "", fault(f.InvalidArgument)
	}
	var out strings.Builder
	out.WriteByte('[')
	nonzero := false
	for n, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", fault(f.InvalidState)
		}
		nonzero = nonzero || value != 0
		if n > 0 {
			out.WriteByte(',')
		}
		out.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
	}
	if !nonzero {
		return "", fault(f.InvalidState)
	}
	out.WriteByte(']')
	return out.String(), nil
}

func (s *IndexService) EmbedQuery(ctx context.Context, request rc.Request) ([]float64, error) {
	q, err := s.queryOwner(ctx, request)
	if err != nil {
		return nil, err
	}
	vectors, err := s.embedTexts(ctx, q.profile, "query", "", []string{request.Query}, q)
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fault(f.InvalidState)
	}
	return vectors[0], nil
}
