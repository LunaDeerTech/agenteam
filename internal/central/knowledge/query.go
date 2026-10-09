package knowledge

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type queryInput struct {
	kind    string
	project id.ProjectID
	parent  *kc.DocumentID
	filter  kc.ListFilter
}

func (q queryInput) binding(actor id.Actor) (cursor.Binding, error) {
	raw, err := json.Marshal(struct {
		Format  int            `json:"format"`
		Kind    string         `json:"kind"`
		Project id.ProjectID   `json:"project_id"`
		User    string         `json:"owner_user_id"`
		Parent  *kc.DocumentID `json:"parent_document_id"`
		Filter  kc.ListFilter  `json:"filter"`
	}{1, q.kind, q.project, actor.Details().UserID, q.parent, q.filter})
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	scope, err := id.InProject(q.project)
	if err != nil {
		return cursor.Binding{}, portError(err)
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: "title:C:asc,id:asc"}, nil
}

func titleAfter(keys cursor.Keyring, token string, binding cursor.Binding) (string, string, error) {
	if token == "" {
		return "", "", nil
	}
	position, err := keys.Verify(token, binding)
	if err != nil {
		return "", "", err
	}
	if position.OrderGeneration != nil || len(position.Scalars) != 2 || position.Scalars[0].Kind() != "text" || position.Scalars[1].Kind() != "uuid" {
		return "", "", fault(f.CursorInvalid)
	}
	title, key := position.Scalars[0].Value(), position.Scalars[1].Value()
	if kc.ValidateTitle(title) != nil {
		return "", "", fault(f.CursorInvalid)
	}
	if _, err = f.ParseID[kc.Document](key); err != nil {
		return "", "", fault(f.CursorInvalid)
	}
	return title, key, nil
}

func titleCursor(keys cursor.Keyring, binding cursor.Binding, document kc.DocumentRef) (string, error) {
	title, err := cursor.Text(document.Title)
	if err != nil {
		return "", internal(err)
	}
	key, err := cursor.UUID(document.ID.String())
	if err != nil {
		return "", internal(err)
	}
	return keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{title, key}})
}

func literalTitle(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}
func optionalText[T ~string](s *T) any {
	if s == nil {
		return nil
	}
	return string(*s)
}

const listDocumentsSQL = `SELECT ` + documentColumns + ` FROM agenteam_knowledge.documents
 WHERE project_id=$1 AND status='active'
 AND (NOT $2::boolean OR parent_document_id IS NOT DISTINCT FROM $3::uuid)
 AND ($4::text='' OR title LIKE '%' || $4::text || '%' ESCAPE '!')
 AND ($5::text IS NULL OR source_kind=$5)
 AND ($6::text IS NULL OR media_type=$6)
 AND ($7::text IS NULL OR indexing_status=$7)
 AND (NOT $8::boolean OR (title COLLATE "C",id) > ($9::text COLLATE "C",$10::uuid))
 ORDER BY title COLLATE "C" ASC,id ASC LIMIT $11`

func (s *Service) listInTx(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, q queryInput, page f.PageRequest) (f.Page[kc.DocumentRef], error) {
	if q.kind == "children" && q.parent != nil {
		row, err := loadDocument(ctx, x, q.project, *q.parent)
		if err != nil {
			return f.Page[kc.DocumentRef]{}, err
		}
		if row.head.Active == nil {
			return f.Page[kc.DocumentRef]{}, fault(f.NotFound)
		}
	}
	binding, err := q.binding(actor)
	if err != nil {
		return f.Page[kc.DocumentRef]{}, err
	}
	keys := s.state().deps.Cursors
	title, key, err := titleAfter(keys, page.Cursor, binding)
	if err != nil {
		return f.Page[kc.DocumentRef]{}, err
	}
	var parent, afterKey any
	if q.parent != nil {
		parent = q.parent.String()
	}
	if page.Cursor != "" {
		afterKey = key
	}
	rows, err := x.Query(ctx, listDocumentsSQL, q.project.String(), q.kind == "children", parent, literalTitle(q.filter.TitleQuery), optionalText(q.filter.SourceKind), optionalText(q.filter.MediaType), optionalText(q.filter.IndexingStatus), page.Cursor != "", title, afterKey, page.Limit+1)
	if err != nil {
		return f.Page[kc.DocumentRef]{}, unavailable(err)
	}
	defer rows.Close()
	out := f.Page[kc.DocumentRef]{Items: make([]kc.DocumentRef, 0, page.Limit+1)}
	for rows.Next() {
		row, err := scanDocument(rows)
		if err != nil {
			return f.Page[kc.DocumentRef]{}, err
		}
		if row.head.Active == nil || row.head.Active.ProjectID != q.project {
			return f.Page[kc.DocumentRef]{}, internal(nil)
		}
		out.Items = append(out.Items, *row.head.Active)
	}
	if err = rows.Err(); err != nil {
		return f.Page[kc.DocumentRef]{}, unavailable(err)
	}
	if len(out.Items) > page.Limit {
		out.Items = out.Items[:page.Limit]
		out.NextCursor, err = titleCursor(keys, binding, out.Items[len(out.Items)-1])
		if err != nil {
			return f.Page[kc.DocumentRef]{}, err
		}
	}
	return out, nil
}

func (s *Service) list(ctx context.Context, actor id.Actor, q queryInput, page f.PageRequest) (f.Page[kc.DocumentRef], error) {
	if err := readInput(ctx, actor, q.project); err != nil {
		return f.Page[kc.DocumentRef]{}, err
	}
	if q.filter.Validate() != nil || page.Validate() != nil || q.parent != nil && q.parent.Validate() != nil {
		return f.Page[kc.DocumentRef]{}, fault(f.InvalidArgument)
	}
	var out f.Page[kc.DocumentRef]
	err := s.read(ctx, actor, q.project, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		out, err = s.listInTx(ctx, x, actor, q, page)
		return err
	})
	if err != nil {
		return f.Page[kc.DocumentRef]{}, err
	}
	return out, nil
}

func (s *Service) ListDocuments(ctx context.Context, actor id.Actor, project id.ProjectID, filter kc.ListFilter, page f.PageRequest) (f.Page[kc.DocumentRef], error) {
	return s.list(ctx, actor, queryInput{kind: "documents", project: project, filter: filter}, page)
}
func (s *Service) ListChildren(ctx context.Context, actor id.Actor, project id.ProjectID, parent *kc.DocumentID, filter kc.ListFilter, page f.PageRequest) (f.Page[kc.DocumentRef], error) {
	return s.list(ctx, actor, queryInput{kind: "children", project: project, parent: parent, filter: filter}, page)
}

func (s *Service) SearchTitles(ctx context.Context, actor id.Actor, project id.ProjectID, title string, page f.PageRequest) (f.Page[kc.TitleHit], error) {
	if err := readInput(ctx, actor, project); err != nil {
		return f.Page[kc.TitleHit]{}, err
	}
	q := queryInput{kind: "search", project: project, filter: kc.ListFilter{TitleQuery: title}}
	if q.filter.Validate() != nil || page.Validate() != nil {
		return f.Page[kc.TitleHit]{}, fault(f.InvalidArgument)
	}
	var out f.Page[kc.TitleHit]
	err := s.read(ctx, actor, project, func(ctx context.Context, x postgres.SQLExecutor) error {
		documents, err := s.listInTx(ctx, x, actor, q, page)
		if err != nil {
			return err
		}
		out = f.Page[kc.TitleHit]{Items: make([]kc.TitleHit, 0, len(documents.Items)), NextCursor: documents.NextCursor}
		for _, document := range documents.Items {
			ancestors, err := readAncestors(ctx, x, project, document.ID)
			if err != nil {
				return err
			}
			hit := kc.TitleHit{Document: document, Ancestors: ancestors}
			if err = hit.Validate(); err != nil {
				return internal(err)
			}
			out.Items = append(out.Items, hit)
		}
		return nil
	})
	if err != nil {
		return f.Page[kc.TitleHit]{}, err
	}
	return out, nil
}
