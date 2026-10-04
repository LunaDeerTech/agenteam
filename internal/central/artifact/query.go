package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type Page struct{ data func() pageDetails }
type pageDetails struct {
	Items      []ac.Metadata `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (p Page) Items() []ac.Metadata {
	if p.data == nil {
		return nil
	}
	return append([]ac.Metadata{}, p.data().Items...)
}
func (p Page) NextCursor() string {
	if p.data == nil {
		return ""
	}
	return p.data().NextCursor
}
func (p Page) MarshalJSON() ([]byte, error) {
	if p.data == nil {
		return nil, invalid()
	}
	return json.Marshal(p.data())
}
func (*Page) UnmarshalJSON([]byte) error { return invalid() }
func (Page) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "artifact_page") }
func (Page) LogValue() slog.Value        { return slog.StringValue("artifact_page") }

func (s *Service) ListArtifacts(ctx context.Context, v ac.Invocation, filter ac.ListFilter, page foundation.PageRequest) (Page, error) {
	if v.Validate() != nil || filter.Validate() != nil {
		return Page{}, invalid()
	}
	if page.Limit == 0 {
		page.Limit = foundation.DefaultPageLimit
	}
	if page.Validate() != nil {
		return Page{}, invalid()
	}
	d := v.Details()
	scope, _ := identity.InProject(d.ProjectID)
	queryDigest, err := jsonDigest(struct{ Resource, Project, Execution, Kind, Media, Name, Order string }{"artifact", d.ProjectID.String(), filter.ExecutionID, string(filter.Kind), filter.MediaType, filter.NameQuery, cursor.AuditOrder})
	if err != nil {
		return Page{}, err
	}
	binding := cursor.Binding{Scope: scope, QueryDigest: queryDigest, Order: cursor.AuditOrder}
	cause, err := txCause()
	if err != nil {
		return Page{}, err
	}
	var out pageDetails
	result := s.within(ctx, invocationSubject(v), identity.Read, cause, nil, nil, func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		var afterTime *time.Time
		var afterID string
		if page.Cursor != "" {
			position, err := s.state().cursors.Verify(page.Cursor, binding)
			if err != nil {
				return err
			}
			if len(position.Scalars) != 2 || position.Scalars[0].Kind() != "instant" || position.Scalars[1].Kind() != "uuid" || position.OrderGeneration != nil {
				return failure(foundation.CursorInvalid, nil)
			}
			at, err := foundation.ParseInstant(position.Scalars[0].Value())
			if err != nil {
				return failure(foundation.CursorInvalid, nil)
			}
			value := at.Time()
			afterTime = &value
			afterID = position.Scalars[1].Value()
		}
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		query := `SELECT ` + artifactColumns + ` FROM agenteam_artifact.artifacts WHERE project_id=$1 AND ($2='' OR execution_id::text=$2) AND ($3='' OR kind=$3) AND ($4='' OR media_type=$4) AND ($5='' OR name LIKE '%' || $5 || '%' ESCAPE '\') AND ($6::timestamptz IS NULL OR (created_at,id::text)<($6,$7)) ORDER BY created_at DESC,id DESC LIMIT $8`
		rows, err := e.Query(ctx, query, d.ProjectID.String(), filter.ExecutionID, string(filter.Kind), filter.MediaType, escapeLike(filter.NameQuery), afterTime, afterID, page.Limit+1)
		if err != nil {
			return unavailable(err)
		}
		items := []ac.Metadata{}
		for rows.Next() {
			row, _, err := scanArtifact(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, row.meta)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable(err)
		}
		if len(items) > page.Limit {
			items = items[:page.Limit]
			last := items[len(items)-1].Details()
			at, _ := cursor.Instant(last.CreatedAt)
			id, _ := cursor.UUID(last.Reference.ArtifactID.String())
			out.NextCursor, err = s.state().cursors.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{at, id}})
			if err != nil {
				return err
			}
		}
		out.Items = items
		metadata, err := au.ArtifactMetadata(au.ArtifactList, au.ArtifactMetadataFields{Count: foundation.Progress(len(items)), Phase: au.ListedPhase})
		if err != nil {
			return err
		}
		resource, _ := au.NewResource(au.ArtifactCollectionResource, d.ProjectID.String())
		key, err := jsonDigest([]string{"list", d.AttemptID.String(), binding.QueryDigest.String(), page.Cursor, fmt.Sprint(page.Limit), stableActor(d.Actor)})
		if err != nil {
			return err
		}
		return s.appendReadAudit(ctx, tx, v, au.ArtifactList, resource, metadata, key)
	})
	if err = commitError(result); err != nil {
		return Page{}, err
	}
	copyItems := append([]ac.Metadata{}, out.Items...)
	out.Items = copyItems
	return Page{func() pageDetails { value := out; value.Items = append([]ac.Metadata{}, copyItems...); return value }}, nil
}
func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
func (s *Service) metadata(ctx context.Context, v ac.Invocation, ref ac.ArtifactRef) (ac.Metadata, error) {
	if v.Validate() != nil || ref.Validate() != nil || v.Details().ProjectID != ref.ProjectID {
		return ac.Metadata{}, invalid()
	}
	cause, err := txCause()
	if err != nil {
		return ac.Metadata{}, err
	}
	var out ac.Metadata
	result := s.within(ctx, invocationSubject(v), identity.Read, cause, nil, []foundation.LockRequest{artifactLock(ref.ArtifactID.String())}, func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, ok, err := loadArtifact(ctx, e, ref.ArtifactID.String())
		if err != nil {
			return err
		}
		if !ok || row.meta.Details().Reference != ref {
			return failure(foundation.Forbidden, nil)
		}
		if _, err = s.state().owners.AuthorizeOwnerInTx(ctx, tx, v.Details().Actor, artifactOwner(ref), identity.Read); err != nil {
			return err
		}
		out = row.meta
		return nil
	})
	if err = commitError(result); err != nil {
		return ac.Metadata{}, err
	}
	return out, nil
}
func (s *Service) appendReadAudit(ctx context.Context, tx foundation.Tx, v ac.Invocation, action au.Action, resource au.Resource, metadata au.Metadata, keyDigest foundation.Digest) error {
	d := v.Details()
	scope, _ := identity.InProject(d.ProjectID)
	entry, err := au.NewEntry(au.EntryFields{Scope: scope, Actor: d.Actor, Action: action, Outcome: au.Success, Resource: resource, Metadata: metadata, Associations: au.Associations{ExecutionID: d.ExecutionID, OperationID: d.OperationID, ToolID: d.ToolID, ToolCallID: d.ToolCallID}})
	if err != nil {
		return err
	}
	key, err := au.NewAppendKey(au.ArtifactProducer, keyDigest.String(), 0)
	if err != nil {
		return err
	}
	_, err = s.state().audit.AppendInTx(ctx, tx, entry, key)
	return portOrNil(err)
}
