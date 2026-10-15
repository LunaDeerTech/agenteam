package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"strconv"
	"strings"
	"sync/atomic"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

// ContentReadAuthority is supplied by the actual Execution/Model source owner.
// IDs describe the source; only its current private source loan can authorize
// an AgentRun read after the original Tool dispatch has returned.
type ContentReadAuthority = ec.MCPContentReadAuthority

type ContentAuthority struct {
	direct      *Service
	binding     *Authority
	invocations tc.MCPInvocationAuthority
	reads       ContentReadAuthority
	resources   ResourceInvocationAuthority
	maintenance ContentMaintenanceOwnerResolver
	recovery    oc.MCPUploadResolver
}

// This static authority is the same one bound by Service.New exactly once.
// Construction breaks the Object/Registry cycle without issuing any grant.
func NewContentAuthorityWithAuthority(binding *Authority, invocations tc.MCPInvocationAuthority, reads ContentReadAuthority, resources ...ResourceInvocationAuthority) (*ContentAuthority, error) {
	if binding == nil || nilPort(binding.store) || nilPort(invocations) || len(resources) > 1 {
		return nil, fail(f.DependencyUnbound)
	}
	a := &ContentAuthority{binding: binding, invocations: invocations, reads: reads}
	if len(resources) == 1 {
		if nilPort(resources[0]) {
			return nil, fail(f.DependencyUnbound)
		}
		a.resources = resources[0]
	}
	return a, nil
}

// Maintenance mapping is supplied by the same Object store at composition.
// It describes an original upload; the domain still proves the live tail.
func NewContentAuthorityWithMaintenance(binding *Authority, invocations tc.MCPInvocationAuthority, reads ContentReadAuthority, maintenance ContentMaintenanceOwnerResolver, resources ...ResourceInvocationAuthority) (*ContentAuthority, error) {
	if nilPort(maintenance) {
		return nil, fail(f.DependencyUnbound)
	}
	a, err := NewContentAuthorityWithAuthority(binding, invocations, reads, resources...)
	if err != nil {
		return nil, err
	}
	a.maintenance = maintenance
	return a, nil
}
func (a *ContentAuthority) service() *Service {
	if a == nil {
		return nil
	}
	if a.binding != nil {
		s, _ := a.binding.service()
		return s
	}
	return a.direct
}

func NewContentAuthority(service *Service, invocations tc.MCPInvocationAuthority, reads ContentReadAuthority, resources ...ResourceInvocationAuthority) (*ContentAuthority, error) {
	if service == nil || nilPort(invocations) || len(resources) > 1 {
		return nil, fail(f.DependencyUnbound)
	}
	a := &ContentAuthority{direct: service, invocations: invocations, reads: reads}
	if len(resources) == 1 {
		if nilPort(resources[0]) {
			return nil, fail(f.DependencyUnbound)
		}
		a.resources = resources[0]
	}
	return a, nil
}

type ContentStore struct {
	authority *ContentAuthority
	objects   oc.Objects
	recovery  ContentRecoveryObjects
}

func NewContentStore(authority *ContentAuthority, objects oc.Objects) (*ContentStore, error) {
	if authority == nil || authority.direct == nil && authority.binding == nil || nilPort(objects) {
		return nil, fail(f.DependencyUnbound)
	}
	return &ContentStore{authority: authority, objects: objects}, nil
}

type contentUseKey struct{}
type contentUse struct {
	owner           *ContentAuthority
	call            tc.ToolCallBinding
	operation       tc.OperationID
	attempt         tc.AttemptID
	request         string
	connection      RuntimeConnectionBinding
	locks           []f.LockRequest
	require         func(context.Context, f.Tx) error
	materialization string
	active          atomic.Bool
}
type contentFact struct {
	id, project, connection, runtime, execution, operation, attempt, request, uri, state string
	version                                                                              f.Version
}

func loadContentFact(ctx context.Context, x postgres.SQLExecutor, materialization string) (contentFact, error) {
	var r contentFact
	err := x.QueryRow(ctx, `SELECT id,project_id,connection_id,runtime_binding_id,execution_id,operation_id,COALESCE(attempt_id,''),COALESCE(request_id,''),uri,state,version FROM agenteam_mcp.materializations WHERE id=$1`, materialization).Scan(&r.id, &r.project, &r.connection, &r.runtime, &r.execution, &r.operation, &r.attempt, &r.request, &r.uri, &r.state, &r.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, fail(f.NotFound)
	}
	if err != nil {
		return r, safeError(err)
	}
	return r, nil
}
func contentOwner(owner oc.ObjectOwner) (id.ProjectID, string, error) {
	if owner.Validate() != nil || owner.Details().Kind != oc.MCPContent {
		return id.ProjectID{}, "", fail(f.InvalidArgument)
	}
	p, err := f.ParseID[id.Project](owner.Details().ProjectID)
	if err != nil {
		return p, "", fail(f.InvalidArgument)
	}
	return p, owner.Details().ID, nil
}
func (a *ContentAuthority) originalUse(ctx context.Context, actor id.Actor, materialization string) (*contentUse, bool) {
	w, ok := ctx.Value(contentUseKey{}).(*contentUse)
	return w, ok && w != nil && w.owner == a && w.active.Load() && w.materialization == materialization && w.call.Actor.Equal(actor)
}
func (a *ContentAuthority) factsMatch(w *contentUse, r contentFact) bool {
	return r.id == w.materialization && r.project == w.call.ProjectID.String() && r.execution == w.call.ExecutionID.String() && r.operation == w.operation.String() && r.attempt == w.attempt.String() && r.request == w.request && r.connection == w.connection.ConnectionID && r.runtime == w.connection.RuntimeBindingID
}
func (a *ContentAuthority) accessDependencies(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.AccessDependencies, error) {
	if a == nil || a.service() == nil || ctx == nil || actor.Validate() != nil {
		return oc.AccessDependencies{}, fail(f.InvalidArgument)
	}
	project, materialization, err := contentOwner(owner)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if intent != id.Read && intent != id.Mutate {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	var locks []f.LockRequest
	mapping := []string{"mcp.content.v1", project.String(), materialization}
	if w, ok := a.originalUse(ctx, actor, materialization); ok {
		if w.call.ProjectID != project {
			return oc.AccessDependencies{}, fail(f.Forbidden)
		}
		locks = w.locks
		mapping = append(mapping, w.request)
	} else if actor.Details().Kind == id.Human && intent == id.Read {
		scope, _ := id.InProject(project)
		locks, err = a.service().locks(actor, scope, nil, false)
		if err != nil {
			return oc.AccessDependencies{}, err
		}
	} else if actor.Details().Kind == id.AgentRun && intent == id.Read {
		if nilPort(a.reads) {
			return oc.AccessDependencies{}, fail(f.DependencyUnbound)
		}
		request, err := loadContentReadRequest(ctx, a.service().store, actor, owner)
		if err != nil {
			return oc.AccessDependencies{}, err
		}
		d, err := a.reads.DiscoverMCPContentRead(ctx, request)
		if err != nil {
			return oc.AccessDependencies{}, safeError(err)
		}
		if d.Validate() != nil {
			return oc.AccessDependencies{}, fail(f.DependencyUnbound)
		}
		// This opaque dependency is rederived and compared by the actual source
		// owner under the same complete plan before issuing any read grant.
		return d, nil
	} else {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	raw, _ := json.Marshal(mapping)
	return oc.NewAccessDependencies(f.Digest(digest(raw)), locks)
}

func (a *ContentAuthority) authorizeInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (contentFact, error) {
	if a == nil || a.service() == nil || ctx == nil || actor.Validate() != nil {
		return contentFact{}, fail(f.InvalidArgument)
	}
	project, materialization, err := contentOwner(owner)
	if err != nil {
		return contentFact{}, err
	}
	x, err := a.service().store.InTx(tx)
	if err != nil {
		return contentFact{}, safeError(err)
	}
	r, err := loadContentFact(ctx, x, materialization)
	if err != nil {
		return r, err
	}
	if r.project != project.String() {
		return r, fail(f.Forbidden)
	}
	if w, ok := a.originalUse(ctx, actor, materialization); ok {
		if intent != id.Read && intent != id.Mutate || !a.factsMatch(w, r) || r.state != "pending" && r.state != "ready" {
			return r, fail(f.Forbidden)
		}
		if err = a.service().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
			return r, safeError(err)
		}
		return r, w.require(ctx, tx)
	}
	if intent != id.Read || r.state != "ready" {
		return r, fail(f.Forbidden)
	}
	if actor.Details().Kind == id.Human {
		scope, _ := id.InProject(project)
		locks, err := a.service().locks(actor, scope, nil, false)
		if err != nil {
			return r, err
		}
		if err = a.service().store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return r, safeError(err)
		}
		return r, a.service().authorize(ctx, tx, actor, scope, id.Read)
	}
	if actor.Details().Kind != id.AgentRun || nilPort(a.reads) {
		return r, fail(f.Forbidden)
	}
	request, err := loadContentReadRequest(ctx, x, actor, owner)
	if err != nil {
		return r, err
	}
	return r, a.reads.RequireMCPContentReadInTx(ctx, tx, request)
}

func (a *ContentAuthority) AuthorizeOwner(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	deps, err := a.accessDependencies(ctx, actor, owner, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	request, err := f.NewID[f.Request]()
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	cause, err := f.NewRecoveryCause("mcp.content.read", request.String(), "")
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var out oc.OwnerAuthorization
	err = commit(a.service().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.service().store.AcquireAll(ctx, tx, deps.Locks()); err != nil {
			return safeError(err)
		}
		var err error
		out, err = a.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
		return err
	}))
	return out, err
}
func (a *ContentAuthority) AuthorizeOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	r, err := a.authorizeInTx(ctx, tx, actor, owner, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	existence := oc.ExistingOwner
	if r.state == "pending" {
		existence = oc.ProspectiveOwner
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: existence, CreationCause: r.request, Version: 1})
}
func (a *ContentAuthority) AuthorizeObjectReadInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID) (oc.OwnerAuthorization, error) {
	if object.Validate() != nil {
		return oc.OwnerAuthorization{}, fail(f.InvalidArgument)
	}
	r, err := a.authorizeInTx(ctx, tx, actor, owner, id.Read)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	x, err := a.service().store.InTx(tx)
	if err != nil {
		return oc.OwnerAuthorization{}, safeError(err)
	}
	var found bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.materialized_contents WHERE materialization_id=$1 AND object_id=$2)`, r.id, object.String()).Scan(&found)
	if err != nil {
		return oc.OwnerAuthorization{}, safeError(err)
	}
	if !found || r.state != "ready" {
		return oc.OwnerAuthorization{}, fail(f.Forbidden)
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: id.Read, Existence: oc.ExistingOwner, CreationCause: r.request, Version: 1, ReadObjectID: object})
}
func (a *ContentAuthority) CheckInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) error {
	_, err := a.authorizeInTx(ctx, tx, actor, owner, intent)
	return err
}
func contentAccessOwner(r oc.AccessRequest) (oc.ObjectOwner, id.AccessIntent, error) {
	if r.Validate() != nil {
		return oc.ObjectOwner{}, "", fail(f.InvalidArgument)
	}
	d := r.Details()
	switch d.Kind {
	case oc.OwnerAccess, oc.ObjectReadAccess:
		return d.Owner, d.Intent, nil
	case oc.SourceAccess:
		return d.Source.Details().Owner, id.Read, nil
	}
	return oc.ObjectOwner{}, "", fail(f.DependencyUnbound)
}
func (a *ContentAuthority) Discover(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	if ctx != nil && ctx.Value(contentRecoveryKey{}) != nil {
		return a.contentRecoveryDependencies(ctx, f.Tx{}, r)
	}
	if r.Validate() == nil && r.Details().Kind == oc.MaintenanceAccess {
		return a.contentMaintenance(ctx, f.Tx{}, r)
	}
	owner, intent, err := contentAccessOwner(r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.accessDependencies(ctx, r.Details().Actor, owner, intent)
}
func (a *ContentAuthority) ValidateInTx(ctx context.Context, tx f.Tx, r oc.AccessRequest, expected oc.AccessDependencies) error {
	if a.service() == nil {
		return fail(f.DependencyUnbound)
	}
	if expected.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if ctx != nil && ctx.Value(contentRecoveryKey{}) != nil {
		actual, err := a.contentRecoveryDependencies(ctx, tx, r)
		if err != nil {
			return err
		}
		if !actual.Equal(expected) {
			return fail(f.ResourceBusy)
		}
		return nil
	}
	if r.Validate() == nil && r.Details().Kind == oc.MaintenanceAccess {
		if err := a.service().store.RequireHeldLocks(ctx, tx, expected.Locks()); err != nil {
			return safeError(err)
		}
		actual, err := a.contentMaintenance(ctx, tx, r, expected)
		if err != nil {
			return err
		}
		if !actual.Equal(expected) {
			return fail(f.ResourceBusy)
		}
		return nil
	}
	owner, intent, err := contentAccessOwner(r)
	if err != nil {
		return err
	}
	if err = a.service().store.RequireHeldLocks(ctx, tx, expected.Locks()); err != nil {
		return safeError(err)
	}
	actor := r.Details().Actor
	_, materialization, err := contentOwner(owner)
	if err != nil {
		return err
	}
	_, original := a.originalUse(ctx, actor, materialization)
	if actor.Details().Kind == id.AgentRun && !original {
		if nilPort(a.reads) {
			return fail(f.DependencyUnbound)
		}
		x, err := a.service().store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		request, err := loadContentReadRequest(ctx, x, actor, owner)
		if err != nil {
			return err
		}
		if err = a.reads.ValidateMCPContentReadInTx(ctx, tx, request, expected); err != nil {
			return safeError(err)
		}
	} else {
		current, err := a.accessDependencies(ctx, r.Details().Actor, owner, intent)
		if err != nil {
			return err
		}
		if !current.Equal(expected) {
			return fail(f.ResourceBusy)
		}
	}
	_, err = a.authorizeInTx(ctx, tx, r.Details().Actor, owner, intent)
	return err
}

func contentBytes(v protocol.Content) ([]byte, string, string, bool, error) {
	var body []byte
	media, uri, image := v.MIMEType, v.URI, false
	switch v.Kind {
	case "text":
		body, media = []byte(v.Text), "text/plain"
	case "image":
		body, image = bytes.Clone(v.Data), true
	case "audio":
		body = bytes.Clone(v.Data)
	case "resource":
		if v.Resource == nil || v.Resource.Text != nil && v.Resource.Blob != nil {
			return nil, "", "", false, fail(f.InvalidArgument)
		}
		media, uri = v.Resource.MIMEType, v.Resource.URI
		if v.Resource.Text != nil {
			body = []byte(*v.Resource.Text)
			if media == "" {
				media = "text/plain"
			}
		} else {
			body = bytes.Clone(v.Resource.Blob)
		}
	default:
		return nil, "", "", false, fail(f.InvalidArgument)
	}
	if media == "" {
		media = "application/octet-stream"
	}
	parsed, _, err := mime.ParseMediaType(media)
	if err != nil || parsed == "" || len(body) > 8<<20 || len(uri) > 8192 {
		clear(body)
		return nil, "", "", false, fail(f.InvalidArgument)
	}
	if image && !strings.HasPrefix(parsed, "image/") {
		clear(body)
		return nil, "", "", false, fail(f.InvalidArgument)
	}
	if v.Kind == "resource" && v.Resource.Text == nil && strings.HasPrefix(parsed, "image/") {
		image = true
	}
	return body, parsed, uri, image, nil
}

func (s *ContentStore) MaterializeMCPContent(ctx context.Context, invocation tc.MCPInvocation, ordinal int, content protocol.Content) (mc.ResultPart, error) {
	if s == nil || s.authority == nil || s.authority.service() == nil || ctx == nil || invocation.Validate() != nil {
		return mc.ResultPart{}, fail(f.InvalidArgument)
	}
	a := s.authority
	plan, err := a.invocations.DiscoverMCPInvocation(ctx, invocation)
	if err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	if nilPort(plan) {
		return mc.ResultPart{}, fail(f.DependencyUnbound)
	}
	w := &contentUse{owner: a, call: invocation.Call, operation: invocation.OperationID, attempt: invocation.AttemptID, request: invocation.RequestID, connection: resourceBinding(invocation.Call.MCPBinding), locks: plan.RequiredLocks(), require: func(ctx context.Context, tx f.Tx) error {
		return a.invocations.RequireMCPInvocationInTx(ctx, tx, invocation, plan)
	}}
	return s.materialize(ctx, w, ordinal, content)
}
func (s *ContentStore) MaterializeMCPResource(ctx context.Context, invocation ResourceInvocation, plan ResourceInvocationPlan, connection RuntimeConnectionBinding, ordinal int, content protocol.ResourceContent) (mc.ResultPart, error) {
	if s == nil || s.authority == nil || s.authority.service() == nil || ctx == nil || invocation.Validate() != nil || nilPort(plan) || connection.Validate() != nil || connection.ProjectID != invocation.Call.ProjectID {
		return mc.ResultPart{}, fail(f.InvalidArgument)
	}
	a := s.authority
	if nilPort(a.resources) {
		return mc.ResultPart{}, fail(f.DependencyUnbound)
	}
	found := false
	for _, b := range plan.RuntimeConnections() {
		if b == connection {
			found = true
		}
	}
	if !found {
		return mc.ResultPart{}, fail(f.Forbidden)
	}
	w := &contentUse{owner: a, call: invocation.Call, operation: invocation.OperationID, attempt: invocation.AttemptID, request: invocation.RequestID, connection: connection, locks: plan.RequiredLocks(), require: func(ctx context.Context, tx f.Tx) error {
		return a.resources.RequireMCPResourceInvocationInTx(ctx, tx, invocation, plan)
	}}
	return s.materialize(ctx, w, ordinal, protocol.Content{Kind: "resource", Resource: &content})
}
func (s *ContentStore) materialize(ctx context.Context, w *contentUse, ordinal int, content protocol.Content) (mc.ResultPart, error) {
	if ordinal < 0 || ordinal > 255 {
		return mc.ResultPart{}, fail(f.InvalidArgument)
	}
	body, media, uri, image, err := contentBytes(content)
	if err != nil {
		return mc.ResultPart{}, err
	}
	defer clear(body)
	a := s.authority
	materialization, err := f.NewID[struct{}]()
	if err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	contentID, err := f.NewID[struct{}]()
	if err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	if uri == "" {
		uri = "mcp-content://" + w.connection.ConnectionID + "/" + w.request + "/" + strconv.Itoa(ordinal)
	}
	w.materialization = materialization.String()
	w.active.Store(true)
	defer w.active.Store(false)
	ctx = context.WithValue(ctx, contentUseKey{}, w)
	cause, err := f.NewRecoveryCause("mcp.content", w.request, "")
	if err != nil {
		return mc.ResultPart{}, err
	}
	err = commit(a.service().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.service().store.AcquireAll(ctx, tx, w.locks); err != nil {
			return safeError(err)
		}
		if err := w.require(ctx, tx); err != nil {
			return safeError(err)
		}
		process, err := a.materializationProcessInTx(ctx, tx)
		if err != nil {
			return err
		}
		x, err := a.service().store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		b := w.connection
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.materializations(id,project_id,connection_id,runtime_binding_id,execution_id,operation_id,uri,state,attempt_id,request_id,process_id) VALUES($1,$2,$3,$4,$5,$6,$7,'pending',$8,$9,$10)`, materialization.String(), w.call.ProjectID.String(), b.ConnectionID, b.RuntimeBindingID, w.call.ExecutionID.String(), w.operation.String(), uri, w.attempt.String(), w.request, process.String())
		return safeError(err)
	}))
	if err != nil {
		return mc.ResultPart{}, err
	}
	owner, err := oc.NewObjectOwner(oc.MCPContent, materialization.String(), w.call.ProjectID.String())
	if err != nil {
		return mc.ResultPart{}, err
	}
	request, _ := f.ParseID[f.Request](w.request)
	meta := f.CommandMeta{RequestID: request, IdempotencyKey: f.IdempotencyKey(materialization.String())}
	sha := f.Digest(digest(body))
	put, err := s.objects.PutObject(ctx, w.call.Actor, owner, meta, media, int64(len(body)), &sha, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	if put.Meta.ID.Validate() != nil || put.Receipt.Validate() != nil || put.Meta.MediaType != media || int64(put.Meta.ByteSize) != int64(len(body)) || put.Meta.SHA256 != sha {
		return mc.ResultPart{}, fail(f.InvalidState)
	}
	consume, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.ConsumeAccess, Actor: w.call.Actor, Owner: owner, Intent: id.Mutate, Receipt: put.Receipt})
	if err != nil {
		return mc.ResultPart{}, err
	}
	objectPlan, err := s.objects.DiscoverAccess(ctx, consume)
	if err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	err = commit(a.service().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := s.objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{objectPlan}, w.locks)
		if err != nil {
			return safeError(err)
		}
		if err = w.require(ctx, tx); err != nil {
			return safeError(err)
		}
		x, err := a.service().store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		r, err := loadContentFact(ctx, x, materialization.String())
		if err != nil {
			return err
		}
		if !a.factsMatch(w, r) || r.state != "pending" {
			return fail(f.ConfirmationStale)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.materialized_contents(materialization_id,ordinal,object_id,uri,mime_type,byte_size,content_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, materialization.String(), ordinal, put.Meta.ID.String(), uri, media, int64(len(body)), contentID.String())
		if err != nil {
			return safeError(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.materializations SET state='ready' WHERE id=$1 AND state='pending'`, materialization.String())
		if err != nil {
			return safeError(err)
		}
		// Object consumes a prospective receipt only after its real business
		// owner exists. Both the content postimage and canonical reference are
		// published in this same transaction, retaining the original cause.
		_, err = s.objects.ConsumeUploadInTx(ctx, tx, w.call.Actor, owner, put.Receipt, objectPlan, locked)
		return safeError(err)
	}))
	if err != nil {
		return mc.ResultPart{}, err
	}
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: w.call.ProjectID, FileID: contentID.String()})
	if err != nil {
		return mc.ResultPart{}, err
	}
	if content.Kind == "resource" && content.Resource != nil && content.Resource.Text != nil {
		return s.projectCanonicalText(ctx, w, owner, put.Meta, ref, uri)
	}
	part := &mc.MediaPart{Ref: ref, MediaType: media}
	if image {
		return mc.ResultPart{Image: part}, nil
	}
	return mc.ResultPart{File: part}, nil
}

var _ MediaMaterializer = (*ContentStore)(nil)
var _ ResourceMaterializer = (*ContentStore)(nil)
var _ oc.AccessPlanner = (*ContentAuthority)(nil)
var _ oc.ResourceAuthority = (*ContentAuthority)(nil)
var _ oc.ObjectReadAuthority = (*ContentAuthority)(nil)
var _ oc.ProjectGate = (*ContentAuthority)(nil)
