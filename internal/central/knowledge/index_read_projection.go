package knowledge

import (
	"context"
	"errors"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// File read locators address extracted text. Every authorized read rechecks
// the current canonical document and exact complete generation in one Tx.
func (s *IndexService) readFileProjection(ctx context.Context, invocation kc.Invocation, document kc.DocumentID, version f.Version, request kc.ReadRequest) (kc.DocumentContent, error) {
	ctx, err := s.documents.invocationContext(ctx, invocation, document, version, request, nil)
	if err != nil {
		return kc.DocumentContent{}, err
	}
	d := invocation.Details()
	return s.documents.readParsedProjection(ctx, d.Actor, d.ProjectID, document, version, request)
}

// Human preview and original Tool reads share the same exact current-version
// projection. This read neither chooses old content nor creates index work.
func (s *Service) readParsedProjection(ctx context.Context, actor id.Actor, project id.ProjectID, document kc.DocumentID, version f.Version, request kc.ReadRequest) (kc.DocumentContent, error) {
	var out kc.DocumentContent
	err := s.read(ctx, actor, project, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadDocument(ctx, x, project, document)
		if err != nil {
			return err
		}
		if row.head.Active == nil || row.head.Active.ContentVersion != version || row.head.Active.SourceKind != kc.File {
			return fault(f.VersionConflict)
		}
		out.Document = *row.head.Active
		var text, profile string
		err = x.QueryRow(ctx, `SELECT g.parsed_text,g.parsed_profile FROM agenteam_knowledge.index_generations g JOIN agenteam_knowledge.index_profiles p ON p.project_id=g.project_id AND p.id=g.profile_id JOIN agenteam_knowledge.index_jobs j ON j.project_id=g.project_id AND j.id=g.job_id JOIN agenteam_knowledge.documents d ON d.project_id=g.project_id AND d.id=g.document_id WHERE g.project_id=$1 AND g.document_id=$2 AND g.document_version=$3 AND d.status='active' AND d.content_version=g.document_version AND d.current_object_id=j.object_id AND g.state='ready' AND g.lexical_complete AND g.dense_complete AND g.parsed_text IS NOT NULL AND g.parsed_profile IS NOT NULL AND p.state IN ('building','ready','active','retired') ORDER BY p.version DESC LIMIT 1`, project.String(), document.String(), int64(version)).Scan(&text, &profile)
		if errors.Is(err, pgx.ErrNoRows) {
			unavailable := kc.ReadableProcessing
			if out.Document.IndexingStatus == kc.IndexFailed {
				unavailable = kc.ReadableFailed
			}
			out.Unavailable = &unavailable
			return nil
		}
		if err != nil {
			return unavailable(err)
		}
		content, err := parsedTextPage(text, profile, request)
		if err != nil {
			return err
		}
		out.Text = &content
		return out.Validate()
	})
	if err != nil {
		return kc.DocumentContent{}, err
	}
	return out, nil
}
func parsedTextPage(text, profile string, request kc.ReadRequest) (kc.TextContent, error) {
	if request.Validate() != nil || !utf8.ValidString(text) || profile == "" || int64(request.ByteOffset) > int64(len(text)) {
		return kc.TextContent{}, fault(f.InvalidArgument)
	}
	start := int(request.ByteOffset)
	if start < len(text) && !utf8.RuneStart(text[start]) {
		return kc.TextContent{}, fault(f.InvalidArgument)
	}
	end := min(len(text), start+request.MaxBytes)
	for end > start && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	if end == start && start < len(text) {
		return kc.TextContent{}, fault(f.InvalidArgument)
	}
	out := kc.TextContent{ParserProfile: profile, Text: text[start:end], NextByteOffset: f.Progress(end), Truncated: end < len(text)}
	if err := out.Validate(); err != nil {
		return kc.TextContent{}, err
	}
	return out, nil
}
