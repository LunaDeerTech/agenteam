package mcp

import (
	"context"
	"errors"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type contentSource struct {
	materialization string
	object          oc.ObjectID
	media           string
	size            f.Progress
	owner           oc.ObjectOwner
}

func loadContentSource(ctx context.Context, x postgres.SQLExecutor, reference oc.BusinessFileRef) (contentSource, error) {
	var out contentSource
	if reference.Validate() != nil || reference.Details().Kind != oc.MCPFile {
		return out, fail(f.InvalidArgument)
	}
	d := reference.Details()
	var object string
	err := x.QueryRow(ctx, `SELECT c.materialization_id,c.object_id,c.mime_type,c.byte_size FROM agenteam_mcp.materialized_contents c JOIN agenteam_mcp.materializations m ON m.id=c.materialization_id WHERE c.content_id=$1 AND m.project_id=$2 AND m.state='ready'`, d.FileID, d.ProjectID.String()).Scan(&out.materialization, &object, &out.media, &out.size)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fail(f.NotFound)
	}
	if err != nil {
		return out, safeError(err)
	}
	out.object, err = f.ParseID[oc.StoredObject](object)
	if err != nil {
		return out, fail(f.InvalidState)
	}
	out.owner, err = oc.NewObjectOwner(oc.MCPContent, out.materialization, d.ProjectID.String())
	return out, err
}

func (s *ContentStore) sourceSnapshot(ctx context.Context, actor id.Actor, reference oc.BusinessFileRef) (contentSource, error) {
	if s == nil || s.authority == nil || s.authority.service() == nil || ctx == nil || actor.Validate() != nil {
		return contentSource{}, fail(f.InvalidArgument)
	}
	a := s.authority
	before, err := loadContentSource(ctx, a.service().store, reference)
	if err != nil {
		return contentSource{}, err
	}
	deps, err := a.accessDependencies(ctx, actor, before.owner, id.Read)
	if err != nil {
		return contentSource{}, err
	}
	request, err := f.NewID[f.Request]()
	if err != nil {
		return contentSource{}, err
	}
	cause, err := f.NewRecoveryCause("mcp.source", request.String(), "")
	if err != nil {
		return contentSource{}, err
	}
	var current contentSource
	err = commit(a.service().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.service().store.AcquireAll(ctx, tx, deps.Locks()); err != nil {
			return safeError(err)
		}
		x, err := a.service().store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		current, err = loadContentSource(ctx, x, reference)
		if err != nil {
			return err
		}
		if current.materialization != before.materialization || current.object != before.object {
			return fail(f.ResourceBusy)
		}
		if actor.Details().Kind == id.AgentRun {
			if _, original := a.originalUse(ctx, actor, current.materialization); !original {
				if nilPort(a.reads) {
					return fail(f.DependencyUnbound)
				}
				request, err := loadContentReadRequest(ctx, x, actor, current.owner)
				if err != nil {
					return err
				}
				if !sameContentReference(request.Reference, reference) {
					return fail(f.Forbidden)
				}
				if err = a.reads.ValidateMCPContentReadInTx(ctx, tx, request, deps); err != nil {
					return safeError(err)
				}
			}
		}
		_, err = a.authorizeInTx(ctx, tx, actor, current.owner, id.Read)
		return err
	}))
	return current, err
}

func (s *ContentStore) Resolve(ctx context.Context, actor id.Actor, reference oc.BusinessFileRef) (oc.ResolvedSource, error) {
	content, err := s.sourceSnapshot(ctx, actor, reference)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	meta, err := s.objects.StatObject(ctx, actor, content.owner, content.object)
	if err != nil {
		return oc.ResolvedSource{}, safeError(err)
	}
	current, err := s.sourceSnapshot(ctx, actor, reference)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	if current.object != meta.ID || current.media != meta.MediaType || current.size != meta.ByteSize || !current.owner.Equal(content.owner) {
		return oc.ResolvedSource{}, fail(f.ResourceBusy)
	}
	return oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: reference, Owner: current.owner, Meta: meta, Revision: 1})
}

func (s *ContentStore) ValidateInTx(ctx context.Context, tx f.Tx, actor id.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if s == nil || s.authority == nil || s.authority.service() == nil || ctx == nil || actor.Validate() != nil || source.Validate() != nil || plan.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	d := source.Details()
	if d.Reference.Details().Kind != oc.MCPFile || d.Revision != 1 {
		return fail(f.InvalidArgument)
	}
	request, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: plan.Details().Request.Details().Operation, Actor: actor, Source: source})
	if err != nil {
		return err
	}
	if err = s.objects.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return safeError(err)
	}
	x, err := s.authority.service().store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	current, err := loadContentSource(ctx, x, d.Reference)
	if err != nil {
		return err
	}
	if !current.owner.Equal(d.Owner) || current.object != d.Meta.ID || current.media != d.Meta.MediaType || current.size != d.Meta.ByteSize {
		return fail(f.ResourceBusy)
	}
	_, err = s.authority.authorizeInTx(ctx, tx, actor, current.owner, id.Read)
	return err
}

var _ oc.SourceResolver = (*ContentStore)(nil)

// A materialization is one content part, not an arbitrary object directory.
// Resolve its full immutable provenance from this domain's own rows. Neither
// a caller's file ID nor a public Execution descriptor can select a new owner.
func loadContentReadRequest(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, owner oc.ObjectOwner) (ec.MCPContentReadRequest, error) {
	var r ec.MCPContentReadRequest
	project, materialization, err := contentOwner(owner)
	if err != nil {
		return r, err
	}
	r.Actor, r.ProjectID, r.MaterializationID = actor, project, materialization
	var execution, operation, attempt, content string
	var count int64
	err = x.QueryRow(ctx, `SELECT m.execution_id,m.operation_id,m.attempt_id,m.request_id,m.uri,m.connection_id,m.runtime_binding_id,b.config_id,b.config_revision,c.content_id,count(*) OVER() FROM agenteam_mcp.materializations m JOIN agenteam_mcp.runtime_bindings b ON (b.id,b.project_id,b.connection_id)=(m.runtime_binding_id,m.project_id,m.connection_id) JOIN agenteam_mcp.materialized_contents c ON c.materialization_id=m.id WHERE m.id=$1 AND m.project_id=$2 AND m.state='ready' LIMIT 1`, materialization, project.String()).Scan(&execution, &operation, &attempt, &r.RequestID, &r.URI, &r.ConnectionID, &r.RuntimeBindingID, &r.ConfigID, &r.ConfigRevision, &content, &count)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, fail(f.NotFound)
	}
	if err != nil {
		return r, safeError(err)
	}
	if count != 1 {
		return r, fail(f.InvalidState)
	}
	if r.ExecutionID, err = f.ParseID[id.Execution](execution); err != nil {
		return r, fail(f.InvalidState)
	}
	if r.OperationID, err = f.ParseID[tc.Operation](operation); err != nil {
		return r, fail(f.InvalidState)
	}
	if r.AttemptID, err = f.ParseID[tc.Attempt](attempt); err != nil {
		return r, fail(f.InvalidState)
	}
	r.Reference, err = oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: content})
	if err != nil || r.Validate() != nil {
		return r, fail(f.Forbidden)
	}
	return r, nil
}

func sameContentReference(a, b oc.BusinessFileRef) bool {
	return a.Validate() == nil && b.Validate() == nil && a.Details().Kind == oc.MCPFile && b.Details().Kind == oc.MCPFile && a.Details().ProjectID == b.Details().ProjectID && a.Details().FileID == b.Details().FileID
}
