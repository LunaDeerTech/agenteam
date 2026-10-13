package artifact

import (
	"bytes"
	"context"
	"fmt"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type projectAuditList struct {
	invocation ac.Invocation
	filter     ac.ListFilter
	page       f.PageRequest
	query      f.Digest
	afterTime  *time.Time
	afterID    string
	rows       []ac.Metadata // Includes the original limit+1 continuation row.
}

func artifactListQuery(project i.ProjectID, filter ac.ListFilter) (f.Digest, error) {
	return jsonDigest(struct{ Resource, Project, Execution, Kind, Media, Name, Order string }{"artifact", project.String(), filter.ExecutionID, string(filter.Kind), filter.MediaType, filter.NameQuery, cursor.AuditOrder})
}

func artifactListAuditKey(v ac.Invocation, query f.Digest, page f.PageRequest) (f.Digest, error) {
	d := v.Details()
	return jsonDigest([]string{"list", d.AttemptID.String(), query.String(), page.Cursor, fmt.Sprint(page.Limit), stableActor(d.Actor)})
}

func artifactListRows(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, filter ac.ListFilter, after *time.Time, afterID string, limit int) ([]ac.Metadata, error) {
	query := `SELECT ` + artifactColumns + ` FROM agenteam_artifact.artifacts WHERE project_id=$1 AND ($2='' OR execution_id::text=$2) AND ($3='' OR kind=$3) AND ($4='' OR media_type=$4) AND ($5='' OR name LIKE '%' || $5 || '%' ESCAPE '\') AND ($6::timestamptz IS NULL OR (created_at,id::text)<($6,$7)) ORDER BY created_at DESC,id DESC LIMIT $8`
	rows, err := x.Query(ctx, query, project.String(), filter.ExecutionID, string(filter.Kind), filter.MediaType, escapeLike(filter.NameQuery), after, afterID, limit+1)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	items := []ac.Metadata{}
	for rows.Next() {
		row, _, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		if row.meta.Details().Reference.ProjectID != project || len(items) >= limit+1 {
			return nil, unavailable(nil)
		}
		items = append(items, row.meta)
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	return items, nil
}

func (s *Service) appendProjectListAudit(ctx context.Context, tx f.Tx, list projectAuditList) error {
	d := list.invocation.Details()
	count := min(len(list.rows), list.page.Limit)
	metadata, err := au.ArtifactMetadata(au.ArtifactList, au.ArtifactMetadataFields{Count: f.Progress(count), Phase: au.ListedPhase})
	if err != nil {
		return err
	}
	scope, _ := i.InProject(d.ProjectID)
	resource, _ := au.NewResource(au.ArtifactCollectionResource, d.ProjectID.String())
	entry, err := au.NewEntry(au.EntryFields{Scope: scope, Actor: d.Actor, Action: au.ArtifactList, Outcome: au.Success, Resource: resource, Metadata: metadata, Associations: au.Associations{ExecutionID: d.ExecutionID, OperationID: d.OperationID, ToolID: d.ToolID, ToolCallID: d.ToolCallID}})
	if err != nil {
		return err
	}
	digest, err := artifactListAuditKey(list.invocation, list.query, list.page)
	if err != nil {
		return err
	}
	key, err := au.NewAppendKey(au.ArtifactProducer, digest.String(), 0)
	if err != nil {
		return err
	}
	list.rows = append([]ac.Metadata{}, list.rows...)
	if list.afterTime != nil {
		at := *list.afterTime
		list.afterTime = &at
	}
	w := &projectAuditWitness{store: s.state().store, tx: tx, entry: entry, key: key, list: &list}
	w.active.Store(true)
	defer w.active.Store(false)
	_, err = s.state().audit.AppendInTx(context.WithValue(ctx, projectAuditWitnessKey{}, w), tx, entry, key)
	return portOrNil(err)
}

func (a *ProjectAuditAuthority) checkListInTx(ctx context.Context, tx f.Tx, w *projectAuditWitness) error {
	list := w.list
	if list == nil || w.meta.Validate() == nil || w.command != nil || w.download != nil || list.invocation.Validate() != nil || list.filter.Validate() != nil || list.page.Validate() != nil {
		return failure(f.Forbidden, nil)
	}
	d, e := list.invocation.Details(), w.entry.Fields()
	if !d.Actor.Equal(e.Actor) || d.ProjectID.String() != e.Scope.Details().ProjectID || d.ProjectID.String() != e.Resource.Details().ID ||
		d.ExecutionID != "" || d.OperationID != "" || d.ToolID != "" || d.ToolCallID != "" || len(list.rows) > list.page.Limit+1 {
		return failure(f.Forbidden, nil)
	}
	query, err := artifactListQuery(d.ProjectID, list.filter)
	if err != nil || query != list.query {
		return failure(f.Forbidden, nil)
	}
	key, err := artifactListAuditKey(list.invocation, query, list.page)
	if err != nil || w.key.Details().CauseRef != key.String() {
		return failure(f.Forbidden, nil)
	}
	metadata, err := au.ArtifactMetadata(au.ArtifactList, au.ArtifactMetadataFields{Count: f.Progress(min(len(list.rows), list.page.Limit)), Phase: au.ListedPhase})
	if err != nil || !bytes.Equal(metadata.JSON(), e.Metadata.JSON()) {
		return failure(f.Forbidden, nil)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return failure(f.DependencyUnbound, nil)
	}
	current, err := artifactListRows(ctx, x, d.ProjectID, list.filter, list.afterTime, list.afterID, list.page.Limit)
	if err != nil {
		return err
	}
	if len(current) != len(list.rows) {
		return failure(f.Forbidden, nil)
	}
	for index, item := range current {
		actual, aerr := jsonDigest(item.Details())
		expected, eerr := jsonDigest(list.rows[index].Details())
		if aerr != nil || eerr != nil || actual != expected {
			return failure(f.Forbidden, nil)
		}
	}
	if !w.active.Load() {
		return failure(f.Forbidden, nil)
	}
	return portOrNil(ctx.Err())
}
