package audithttp

import (
	"context"
	"encoding/json"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const projectAuditSuccessBytes = 1 << 20

type projectAuditAgentActor struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	ExecutionID string `json:"execution_id"`
}
type projectAuditServiceActor struct {
	Kind      string               `json:"kind"`
	Service   identity.ServiceName `json:"service"`
	CauseRef  string               `json:"cause_ref"`
	ProjectID string               `json:"project_id"`
}
type projectAuditRecordDTO struct {
	AuditID      c.ID               `json:"audit_id"`
	CreatedAt    foundation.Instant `json:"created_at"`
	Scope        string             `json:"scope"`
	ProjectID    identity.ProjectID `json:"project_id"`
	Actor        any                `json:"actor"`
	Action       c.Action           `json:"action"`
	Outcome      c.Outcome          `json:"outcome"`
	Resource     resourceDTO        `json:"resource"`
	Metadata     json.RawMessage    `json:"metadata"`
	Associations c.Associations     `json:"associations"`
	Summary      string             `json:"summary"`
}
type projectAuditPageDTO struct {
	Items      []projectAuditRecordDTO `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}

func projectAuditSummary(action c.Action) (string, bool) {
	switch action {
	case c.SecretCreate:
		return "Secret created", true
	case c.SecretUpdate:
		return "Secret updated", true
	case c.SecretDelete:
		return "Secret deleted", true
	case c.SecretResolve:
		return "Secret use recorded", true
	case c.AccessDeny:
		return "Outbound access denied", true
	case c.ObjectUploadComplete, c.ObjectUploadFailed, c.ObjectDelete,
		c.ObjectTransferIssue, c.ObjectTransferComplete, c.ObjectTransferRevoke,
		c.ArtifactCreate, c.ArtifactRead, c.ArtifactDownload, c.ArtifactList,
		c.OutboxDeliveryRequeue,
		c.ProjectCreateAccepted, c.ProjectCreateCompleted, c.ProjectUpdate,
		c.ProjectArchiveAccepted, c.ProjectArchiveCompleted, c.ProjectRestore,
		c.ProjectDeleteAccepted, c.ProjectLifecycleRetry,
		c.ProviderCreate, c.ProviderUpdate, c.ProviderDelete,
		c.ModelCreate, c.ModelUpdate, c.ModelDelete, c.KnowledgeDeleteSubtree,
		c.ProjectVariableCreate, c.ProjectVariableUpdate, c.ProjectVariableDelete,
		c.ProjectSecretVariableCreate, c.ProjectSecretVariableUpdate, c.ProjectSecretVariableDelete:
		return "Audit event", true
	}
	return "", false
}

func projectAuditProjection(project identity.ProjectID, record c.SafeRecord) (projectAuditRecordDTO, error) {
	bad := func() (projectAuditRecordDTO, error) {
		return projectAuditRecordDTO{}, fault(foundation.DependencyUnavailable)
	}
	scope, err := identity.InProject(project)
	if err != nil || !record.Scope.Equal(scope) || record.AuditID.Validate() != nil || record.CreatedAt.Validate() != nil || !record.Outcome.Valid() || record.Resource.Validate() != nil || record.Associations.Validate() != nil || record.Metadata.Validate(record.Action) != nil {
		return bad()
	}
	summary, ok := projectAuditSummary(record.Action)
	if !ok || record.Summary != summary {
		return bad()
	}
	resource := record.Resource.Details()
	if _, err = c.NewResource(resource.Kind, resource.ID); err != nil {
		return bad()
	}
	var actor any
	a := record.Actor
	switch a.Kind {
	case identity.Human:
		if !validID(a.ID) || a.ProjectID != "" || a.ExecutionID != "" || a.Service != "" || a.CauseRef != "" {
			return bad()
		}
		actor = humanActor{Kind: "human", ID: a.ID}
	case identity.AgentRun:
		if !validID(a.ID) || a.ProjectID != project.String() || !validID(a.ExecutionID) || a.Service != "" || a.CauseRef != "" || record.Associations.ExecutionID != a.ExecutionID {
			return bad()
		}
		actor = projectAuditAgentActor{"agent_run", a.ID, a.ProjectID, a.ExecutionID}
	case identity.Service:
		if a.ID != "" || a.ExecutionID != "" || a.ProjectID != project.String() || !identity.ValidCauseRef(a.CauseRef) {
			return bad()
		}
		if _, err = identity.RegisterService(a.Service); err != nil {
			return bad()
		}
		actor = projectAuditServiceActor{"service", a.Service, a.CauseRef, a.ProjectID}
	default:
		return bad()
	}
	metadata, err := c.DecodeMetadata(record.Action, record.Metadata.JSON())
	if err != nil || metadata.Validate(record.Action) != nil || !projectAuditRelations(record, metadata, project) {
		return bad()
	}
	canonical := metadata.JSON()
	if len(canonical) > 4096 {
		return bad()
	}
	return projectAuditRecordDTO{record.AuditID, record.CreatedAt, "project", project, actor, record.Action, record.Outcome, resourceDTO{resource.Kind, resource.ID}, canonical, record.Associations, summary}, nil
}

// The SQL scanner already reconstructs NewEntry with the full original Actor.
// HTTP rechecks the clipped safe projection without fabricating a Session ID.
func projectAuditRelations(record c.SafeRecord, metadata c.Metadata, project identity.ProjectID) bool {
	r, a, links := record.Resource.Details(), record.Actor, record.Associations
	if c.ProjectSecretVariableAction(record.Action) {
		m, err := metadata.ProjectSecretVariableFields()
		return err == nil && a.Kind == identity.Human && record.Outcome == c.Success && r.Kind == c.ProjectVariableResource && r.ID == m.VariableID && links == (c.Associations{})
	}
	if c.ProjectVariableAction(record.Action) {
		m, err := metadata.ProjectVariableFields()
		return err == nil && a.Kind == identity.Human && record.Outcome == c.Success && r.Kind == c.ProjectVariableResource && r.ID == m.VariableID && links == (c.Associations{})
	}
	if c.ProjectAction(record.Action) {
		m, err := metadata.ProjectFields()
		if err != nil || m.ProjectID != project.String() || record.Outcome != c.Success || links.ToolID != "" || links.ExecutionID != "" || links.ToolCallID != "" || links.ApprovalID != "" || links.OperationID != "" && links.OperationID != m.OperationID {
			return false
		}
		kind, id := c.ProjectResource, m.ProjectID
		switch record.Action {
		case c.ProjectCreateAccepted, c.ProjectCreateCompleted:
			kind, id = c.ProjectCreationResource, m.CreationID
		case c.ProjectArchiveAccepted, c.ProjectArchiveCompleted, c.ProjectDeleteAccepted, c.ProjectLifecycleRetry:
			kind, id = c.ProjectOperationResource, m.OperationID
		}
		if r.Kind != kind || r.ID != id {
			return false
		}
		switch record.Action {
		case c.ProjectCreateCompleted:
			return a.Kind == identity.Service && a.Service == identity.ProjectInitialization && a.CauseRef == m.CreationID
		case c.ProjectArchiveCompleted:
			return a.Kind == identity.Service && a.Service == identity.ProjectLifecycle && a.CauseRef == m.OperationID
		default:
			return a.Kind == identity.Human && a.ID == m.InitiatorID
		}
	}
	if c.ModelAction(record.Action) {
		m, err := metadata.ModelFields()
		if err != nil || record.Action == c.ModelSelectionUpdate || a.Kind != identity.Human || record.Outcome != c.Success || links.ToolID != "" || links.ExecutionID != "" || links.ToolCallID != "" || links.OperationID != "" || links.RequestID != "" || links.ApprovalID != "" || links.RunnerID != "" {
			return false
		}
		kind, id := c.ModelProviderResource, m.ProviderID
		if m.ModelID != "" {
			kind, id = c.ModelConfigResource, m.ModelID
		}
		return r.Kind == kind && r.ID == id
	}
	if c.KnowledgeAction(record.Action) {
		m, err := metadata.KnowledgeFields()
		return err == nil && a.Kind == identity.Human && a.ID == m.InitiatorID && m.ProjectID == project.String() && record.Outcome == c.Success && r.Kind == c.KnowledgeDocumentResource && r.ID == m.RootID && links == (c.Associations{})
	}
	var m struct {
		ObjectID   string         `json:"object_id"`
		TransferID string         `json:"transfer_id"`
		ArtifactID string         `json:"artifact_id"`
		DeliveryID string         `json:"delivery_id"`
		Phase      c.ContentPhase `json:"phase"`
	}
	if json.Unmarshal(metadata.JSON(), &m) != nil {
		return false
	}
	switch record.Action {
	case c.SecretCreate, c.SecretUpdate, c.SecretDelete, c.SecretResolve:
		return r.Kind == c.SecretResource
	case c.AccessDeny:
		return record.Outcome == c.Denied && (r.Kind == c.PolicyResource || r.Kind == c.SecretResource || r.Kind == c.AgentResource)
	case c.ObjectUploadComplete, c.ObjectUploadFailed, c.ObjectDelete, c.ObjectTransferIssue, c.ObjectTransferComplete, c.ObjectTransferRevoke:
		if a.Kind != identity.Service || a.Service != identity.ObjectService && a.Service != identity.ObjectMaintenance {
			return false
		}
		kind, id := c.ObjectResource, m.ObjectID
		if m.TransferID != "" {
			kind, id = c.ObjectTransferResource, m.TransferID
		}
		if r.Kind != kind || r.ID != id {
			return false
		}
		if record.Action == c.ObjectUploadFailed {
			return record.Outcome == c.Failed || record.Outcome == c.Unknown
		}
		return record.Outcome == c.Success
	case c.ArtifactCreate, c.ArtifactRead, c.ArtifactDownload, c.ArtifactList:
		if a.Kind != identity.Human && a.Kind != identity.AgentRun {
			return false
		}
		kind, id := c.ArtifactResource, m.ArtifactID
		if record.Action == c.ArtifactList {
			kind, id = c.ArtifactCollectionResource, project.String()
		}
		if r.Kind != kind || r.ID != id {
			return false
		}
		if m.Phase == c.FailedPhase {
			return record.Action == c.ArtifactDownload && record.Outcome == c.Failed
		}
		return record.Outcome == c.Success
	case c.OutboxDeliveryRequeue:
		return a.Kind == identity.Human && record.Outcome == c.Success && r.Kind == c.OutboxDeliveryResource && r.ID == m.DeliveryID
	}
	return false
}

func projectAuditMatches(record c.SafeRecord, filter c.Filter) bool {
	if filter.From != nil && record.CreatedAt.Time().Before(filter.From.Time()) || filter.To != nil && !record.CreatedAt.Time().Before(filter.To.Time()) {
		return false
	}
	r, a := record.Resource.Details(), record.Associations
	for _, pair := range [][2]string{
		{string(filter.ActorKind), string(record.Actor.Kind)}, {filter.ActorID, record.Actor.ID},
		{string(filter.Action), string(record.Action)}, {string(filter.Outcome), string(record.Outcome)},
		{string(filter.ResourceKind), string(r.Kind)}, {filter.ResourceID, r.ID},
		{filter.ToolID, a.ToolID}, {filter.ExecutionID, a.ExecutionID}, {filter.OperationID, a.OperationID},
		{filter.ApprovalID, a.ApprovalID}, {filter.RunnerID, a.RunnerID},
	} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	return filter.AgentID == "" || record.Actor.Kind == identity.AgentRun && record.Actor.ID == filter.AgentID || r.Kind == c.AgentResource && r.ID == filter.AgentID
}

func projectAuditEncodeRecord(ctx context.Context, project identity.ProjectID, id c.ID, record c.SafeRecord) ([]byte, error) {
	if record.AuditID != id {
		return nil, fault(foundation.DependencyUnavailable)
	}
	value, err := projectAuditProjection(project, record)
	if err != nil {
		return nil, err
	}
	return projectAuditEncode(ctx, value)
}
func projectAuditEncodePage(ctx context.Context, project identity.ProjectID, filter c.Filter, request foundation.PageRequest, page foundation.Page[c.SafeRecord]) ([]byte, error) {
	if request.Validate() != nil || len(page.Items) > request.Limit || page.NextCursor != "" && len(page.Items) != request.Limit {
		return nil, fault(foundation.DependencyUnavailable)
	}
	value := projectAuditPageDTO{Items: make([]projectAuditRecordDTO, 0, len(page.Items))}
	for i, record := range page.Items {
		projectAuditCheckBudget(ctx)
		if !projectAuditMatches(record, filter) {
			return nil, fault(foundation.DependencyUnavailable)
		}
		if i > 0 {
			previous := page.Items[i-1]
			order := record.CreatedAt.Time().Compare(previous.CreatedAt.Time())
			if order > 0 || order == 0 && record.AuditID.String() >= previous.AuditID.String() {
				return nil, fault(foundation.DependencyUnavailable)
			}
		}
		item, err := projectAuditProjection(project, record)
		if err != nil {
			return nil, err
		}
		value.Items = append(value.Items, item)
	}
	if page.NextCursor != "" {
		if len(page.NextCursor) > cursor.MaxTokenBytes {
			return nil, fault(foundation.DependencyUnavailable)
		}
		for _, ch := range []byte(page.NextCursor) {
			if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.') {
				return nil, fault(foundation.DependencyUnavailable)
			}
		}
		value.NextCursor = &page.NextCursor
	}
	return projectAuditEncode(ctx, value)
}
func projectAuditEncode(ctx context.Context, value any) ([]byte, error) {
	projectAuditCheckBudget(ctx)
	data, err := json.Marshal(value)
	if err != nil || len(data) > projectAuditSuccessBytes {
		return nil, fault(foundation.DependencyUnavailable)
	}
	projectAuditCheckBudget(ctx)
	return data, nil
}
