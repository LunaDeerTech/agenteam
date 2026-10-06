package audithttp

import (
	"encoding/json"
	"net/url"
	"strconv"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const successBytes = 1 << 20

type humanActor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type serviceActor struct {
	Kind     string               `json:"kind"`
	Service  identity.ServiceName `json:"service"`
	CauseRef string               `json:"cause_ref"`
}

type resourceDTO struct {
	Kind c.ResourceKind `json:"kind"`
	ID   string         `json:"id,omitempty"`
}

type associationsDTO struct {
	ToolID        string `json:"tool_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	RunnerID      string `json:"runner_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	HTTPTraceID   string `json:"http_trace_id,omitempty"`
}

type recordDTO struct {
	AuditID      c.ID               `json:"audit_id"`
	CreatedAt    foundation.Instant `json:"created_at"`
	Scope        string             `json:"scope"`
	Actor        any                `json:"actor"`
	Action       c.Action           `json:"action"`
	Outcome      c.Outcome          `json:"outcome"`
	Resource     resourceDTO        `json:"resource"`
	Metadata     json.RawMessage    `json:"metadata"`
	Associations associationsDTO    `json:"associations"`
	Summary      string             `json:"summary"`
}

type pageDTO struct {
	Items      []recordDTO `json:"items"`
	NextCursor *string     `json:"next_cursor"`
}

func query(raw string, force bool) (c.Filter, foundation.PageRequest, error) {
	var filter c.Filter
	page := foundation.DefaultPageRequest()
	bad := func() (c.Filter, foundation.PageRequest, error) {
		return c.Filter{}, foundation.PageRequest{}, fault(foundation.InvalidArgument)
	}
	if force || len(raw) > 32<<10 {
		return bad()
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return bad()
	}
	for key, values := range values {
		if len(values) != 1 {
			return bad()
		}
		value := values[0]
		switch key {
		case "limit":
			limit, e := strconv.Atoi(value)
			if e != nil || strconv.Itoa(limit) != value || limit < 1 || limit > foundation.MaxPageLimit {
				return bad()
			}
			page.Limit = limit
		case "cursor":
			if len(value) > cursor.MaxTokenBytes {
				return c.Filter{}, foundation.PageRequest{}, fault(foundation.CursorInvalid)
			}
			page.Cursor = value
		case "from", "to":
			if value != "" {
				instant, e := foundation.ParseInstant(value)
				if e != nil {
					return bad()
				}
				if key == "from" {
					filter.From = &instant
				} else {
					filter.To = &instant
				}
			}
		case "actor_kind":
			filter.ActorKind = identity.ActorKind(value)
		case "actor_id":
			filter.ActorID = value
		case "action":
			filter.Action = c.Action(value)
		case "outcome":
			filter.Outcome = c.Outcome(value)
		case "resource_kind":
			filter.ResourceKind = c.ResourceKind(value)
		case "resource_id":
			filter.ResourceID = value
		case "tool_id":
			filter.ToolID = value
		case "execution_id":
			filter.ExecutionID = value
		case "operation_id":
			filter.OperationID = value
		case "approval_id":
			filter.ApprovalID = value
		case "runner_id":
			filter.RunnerID = value
		case "agent_id":
			filter.AgentID = value
		default:
			return bad()
		}
	}
	if filter.Validate() != nil {
		return bad()
	}
	return filter, page, nil
}

func validID(value string) bool {
	_, err := foundation.ParseID[struct{}](value)
	return err == nil
}

func systemResource(kind c.ResourceKind) bool {
	switch kind {
	case c.SecretResource, c.MasterResource, c.RotationResource, c.PolicyResource, c.ObjectResource, c.OutboxDeliveryResource,
		c.UserResource, c.SessionResource, c.AccountAttemptResource, c.InvitationResource, c.PasswordResetResource, c.AccountSettingsResource,
		c.SMTPSettingsResource, c.MailJobResource, c.ModelProviderResource, c.ModelConfigResource, c.ModelSelectionResource:
		return true
	}
	return false
}

func projectRecord(record c.SafeRecord) (recordDTO, error) {
	bad := func() (recordDTO, error) { return recordDTO{}, fault(foundation.DependencyUnavailable) }
	if !record.Scope.Equal(identity.SystemScope()) || record.AuditID.Validate() != nil || record.CreatedAt.Validate() != nil || !record.Outcome.Valid() || record.Resource.Validate() != nil || record.Metadata.Validate(record.Action) != nil || record.Associations.Validate() != nil {
		return bad()
	}
	resource := record.Resource.Details()
	if !systemResource(resource.Kind) {
		return bad()
	}
	if _, err := c.NewResource(resource.Kind, resource.ID); err != nil {
		return bad()
	}
	a := record.Actor
	if a.ProjectID != "" || a.ExecutionID != "" {
		return bad()
	}
	var actor any
	switch a.Kind {
	case identity.Human:
		if !validID(a.ID) || a.Service != "" || a.CauseRef != "" {
			return bad()
		}
		actor = humanActor{Kind: "human", ID: a.ID}
	case identity.Service:
		if a.ID != "" {
			return bad()
		}
		if _, err := identity.RegisterService(a.Service); err != nil {
			return bad()
		}
		if !validID(a.CauseRef) && foundation.Digest(a.CauseRef).Validate() != nil {
			return bad()
		}
		actor = serviceActor{Kind: "service", Service: a.Service, CauseRef: a.CauseRef}
	default:
		return bad()
	}
	associations := record.Associations
	if associations.ExecutionID != "" || associations.ToolCallID != "" || associations.OperationID != "" || associations.ApprovalID != "" {
		return bad()
	}
	// Decode uses the formal typed constructors, including default Marshal's
	// escaping and 4096-byte limit. Never project arbitrary database JSON.
	metadata, err := c.DecodeMetadata(record.Action, record.Metadata.JSON())
	if err != nil || metadata.Validate(record.Action) != nil || !recordRelations(record, metadata) {
		return bad()
	}
	summary, ok := actionSummary(record.Action)
	if !ok {
		return bad()
	}
	return recordDTO{AuditID: record.AuditID, CreatedAt: record.CreatedAt, Scope: "system", Actor: actor, Action: record.Action, Outcome: record.Outcome,
		Resource: resourceDTO{resource.Kind, resource.ID}, Metadata: metadata.JSON(), Summary: summary,
		Associations: associationsDTO{associations.ToolID, associations.RequestID, associations.RunnerID, associations.CorrelationID, associations.HTTPTraceID}}, nil
}

// The scanner already used the full original actor in NewEntry. The wire
// checks relationships without inventing a Session for the clipped human actor.
func recordRelations(record c.SafeRecord, metadata c.Metadata) bool {
	r := record.Resource.Details()
	if c.AccountAction(record.Action) {
		m, err := metadata.AccountFields()
		if err != nil || record.Action == c.SMTPDeliveryRetry && record.Actor.Kind != identity.Human {
			return false
		}
		kind, id := c.UserResource, m.UserID
		switch record.Action {
		case c.AccountLogin, c.AccountPasswordResetRequest:
			kind, id = c.AccountAttemptResource, m.AttemptID
		case c.AccountLogout:
			kind, id = c.SessionResource, m.SessionID
		case c.AccountInviteCreate, c.AccountInviteRevoke:
			kind, id = c.InvitationResource, m.InvitationID
		case c.AccountPasswordResetComplete:
			kind, id = c.PasswordResetResource, m.ResetID
		case c.AccountSettingsUpdate:
			kind, id = c.AccountSettingsResource, r.ID
		case c.SMTPSettingsUpdate:
			kind, id = c.SMTPSettingsResource, r.ID
		case c.SMTPTestRequest, c.SMTPDelivery, c.SMTPDeliveryRetry:
			kind, id = c.MailJobResource, m.JobID
		}
		outcome := c.Success
		switch m.Phase {
		case c.AccountRejected:
			outcome = c.Denied
		case c.AccountFailed:
			outcome = c.Failed
		case c.AccountUnknown:
			outcome = c.Unknown
		}
		return r.Kind == kind && r.ID == id && record.Outcome == outcome
	}
	if c.ModelAction(record.Action) {
		m, err := metadata.ModelFields()
		if err != nil || record.Actor.Kind != identity.Human || record.Outcome != c.Success || record.Associations.ToolID != "" || record.Associations.RequestID != "" || record.Associations.RunnerID != "" {
			return false
		}
		kind, id := c.ModelConfigResource, m.ModelID
		switch record.Action {
		case c.ProviderCreate, c.ProviderUpdate, c.ProviderDelete:
			kind, id = c.ModelProviderResource, m.ProviderID
		case c.ModelSelectionUpdate:
			kind, id = c.ModelSelectionResource, m.SelectionID
		}
		return r.Kind == kind && r.ID == id
	}
	var links struct {
		RotationID string `json:"rotation_id"`
		ObjectID   string `json:"object_id"`
		DeliveryID string `json:"delivery_id"`
	}
	if json.Unmarshal(metadata.JSON(), &links) != nil {
		return false
	}
	switch record.Action {
	case c.SecretCreate, c.SecretUpdate, c.SecretDelete, c.SecretResolve:
		return r.Kind == c.SecretResource
	case c.MasterRegister:
		return r.Kind == c.MasterResource && record.Outcome == c.Success
	case c.RotationStart, c.RotationComplete, c.RotationFailed:
		outcome := c.Success
		if record.Action == c.RotationFailed {
			outcome = c.Failed
		}
		return r.Kind == c.RotationResource && r.ID == links.RotationID && record.Outcome == outcome
	case c.PolicyUpdate:
		return r.Kind == c.PolicyResource
	case c.AccessDeny:
		return (r.Kind == c.PolicyResource || r.Kind == c.SecretResource) && record.Outcome == c.Denied
	case c.ObjectUploadComplete, c.ObjectUploadFailed, c.ObjectDelete:
		if r.Kind != c.ObjectResource || r.ID != links.ObjectID || record.Actor.Kind != identity.Service || record.Actor.Service != identity.ObjectService && record.Actor.Service != identity.ObjectMaintenance {
			return false
		}
		if record.Action == c.ObjectUploadFailed {
			return record.Outcome == c.Failed || record.Outcome == c.Unknown
		}
		return record.Outcome == c.Success
	case c.OutboxDeliveryRequeue:
		return r.Kind == c.OutboxDeliveryResource && r.ID == links.DeliveryID && record.Actor.Kind == identity.Human && record.Outcome == c.Success
	}
	return false
}

func actionSummary(action c.Action) (string, bool) {
	switch action {
	case c.SecretCreate:
		return "Secret created", true
	case c.SecretUpdate:
		return "Secret updated", true
	case c.SecretDelete:
		return "Secret deleted", true
	case c.SecretResolve:
		return "Secret use recorded", true
	case c.MasterRegister:
		return "Master key registered", true
	case c.RotationStart:
		return "Master key rotation started", true
	case c.RotationComplete:
		return "Master key rotation completed", true
	case c.RotationFailed:
		return "Master key rotation failed", true
	case c.PolicyUpdate:
		return "Outbound policy updated", true
	case c.AccessDeny:
		return "Outbound access denied", true
	case c.ObjectUploadComplete, c.ObjectUploadFailed, c.ObjectDelete, c.OutboxDeliveryRequeue:
		return "Audit event", true
	default:
		return "Audit event", c.AccountAction(action) || c.ModelAction(action)
	}
}

func encodeRecord(record c.SafeRecord) ([]byte, error) {
	value, err := projectRecord(record)
	if err != nil {
		return nil, err
	}
	return encode(value)
}

func encodePage(page foundation.Page[c.SafeRecord]) ([]byte, error) {
	if len(page.Items) > foundation.MaxPageLimit {
		return nil, fault(foundation.DependencyUnavailable)
	}
	value := pageDTO{Items: make([]recordDTO, 0, len(page.Items))}
	for _, record := range page.Items {
		item, err := projectRecord(record)
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
	return encode(value)
}

func encode(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > successBytes {
		return nil, fault(foundation.DependencyUnavailable)
	}
	return data, nil
}
