// Package usagehttp defines the current-Owner Project Usage HTTP boundary.
package usagehttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

const (
	maxQueryBytes          = 32768
	maxCursorBytes         = 8192
	maxRepresentationBytes = 1 << 20
)

func invalidQuery() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

// Bound the original bytes before splitting or decoding. QueryUnescape is used
// exactly once for each key/value; no attacker-controlled text enters a Fault.
func strictQuery(r *http.Request, resolve, aggregate bool) (map[string]string, error) {
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > maxQueryBytes || r.URL.RawQuery == "" && r.URL.ForceQuery {
		return nil, invalidQuery()
	}
	values := make(map[string]string)
	if r.URL.RawQuery == "" {
		return values, nil
	}
	if strings.Contains(r.URL.RawQuery, ";") {
		return nil, invalidQuery()
	}
	for _, pair := range strings.Split(r.URL.RawQuery, "&") {
		key, raw, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, invalidQuery()
		}
		key, err := url.QueryUnescape(key)
		if err != nil {
			return nil, invalidQuery()
		}
		value, err := url.QueryUnescape(raw)
		if err != nil || key == "" || value == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0) {
			return nil, invalidQuery()
		}
		if _, duplicate := values[key]; duplicate {
			return nil, invalidQuery()
		}
		allowed := false
		if resolve {
			allowed = key == "username" || key == "project_name"
		} else {
			switch key {
			case "consumer_kind", "agent_id", "execution_id", "meeting_id", "purpose", "provider_id", "model_id", "status", "from", "to", "limit", "cursor":
				allowed = true
			case "group_by":
				allowed = aggregate
			}
		}
		if !allowed {
			return nil, invalidQuery()
		}
		if key == "cursor" && len(value) > maxCursorBytes {
			return nil, f.NewFault(f.CursorInvalid, f.NotStarted)
		}
		values[key] = value
	}
	return values, nil
}

func resolveQuery(r *http.Request) (username, projectName string, err error) {
	values, err := strictQuery(r, true, false)
	if err != nil {
		return "", "", err
	}
	username, projectName = values["username"], values["project_name"]
	if _, err = pc.NormalizeProjectPath(username, projectName); err != nil {
		return "", "", invalidQuery()
	}
	return username, projectName, nil
}

func usageQuery(r *http.Request, project id.ProjectID, aggregate bool) (uc.Query, uc.GroupBy, error) {
	values, err := strictQuery(r, false, aggregate)
	if err != nil {
		return uc.Query{}, "", err
	}
	q := uc.Query{Filter: uc.Filter{ProjectID: project}, Limit: 50, Cursor: values["cursor"]}
	if value, ok := values["limit"]; ok {
		if len(value) > 3 || value[0] < '1' || value[0] > '9' {
			return uc.Query{}, "", invalidQuery()
		}
		for _, c := range value {
			if c < '0' || c > '9' {
				return uc.Query{}, "", invalidQuery()
			}
		}
		q.Limit, err = strconv.Atoi(value)
		if err != nil {
			return uc.Query{}, "", invalidQuery()
		}
	}
	if value, ok := values["consumer_kind"]; ok {
		v := mc.ConsumerKind(value)
		q.Filter.ConsumerKind = &v
	}
	if value, ok := values["purpose"]; ok {
		v := mc.Purpose(value)
		q.Filter.Purpose = &v
	}
	if value, ok := values["status"]; ok {
		v := uc.TerminalStatus(value)
		q.Filter.Status = &v
	}
	if value, ok := values["agent_id"]; ok {
		v, e := f.ParseID[id.Agent](value)
		if e != nil {
			return uc.Query{}, "", invalidQuery()
		}
		q.Filter.AgentID = &v
	}
	if value, ok := values["execution_id"]; ok {
		v, e := f.ParseID[id.Execution](value)
		if e != nil {
			return uc.Query{}, "", invalidQuery()
		}
		q.Filter.ExecutionID = &v
	}
	if value, ok := values["provider_id"]; ok {
		v, e := f.ParseID[mc.Provider](value)
		if e != nil {
			return uc.Query{}, "", invalidQuery()
		}
		q.Filter.ProviderID = &v
	}
	if value, ok := values["model_id"]; ok {
		v, e := f.ParseID[mc.Model](value)
		if e != nil {
			return uc.Query{}, "", invalidQuery()
		}
		q.Filter.ModelID = &v
	}
	q.Filter.MeetingID = values["meeting_id"]
	if value, ok := values["from"]; ok {
		v, e := f.ParseInstant(value)
		if e != nil {
			return uc.Query{}, "", invalidQuery()
		}
		q.Filter.From = &v
	}
	if value, ok := values["to"]; ok {
		v, e := f.ParseInstant(value)
		if e != nil {
			return uc.Query{}, "", invalidQuery()
		}
		q.Filter.To = &v
	}
	group := uc.GroupBy(values["group_by"])
	if q.Validate() != nil || aggregate && !group.Valid() {
		return uc.Query{}, "", invalidQuery()
	}
	return q, group, nil
}

func listQuery(r *http.Request, project id.ProjectID) (uc.Query, error) {
	q, _, err := usageQuery(r, project, false)
	return q, err
}
func aggregateQuery(r *http.Request, project id.ProjectID) (uc.AggregateQuery, error) {
	q, group, err := usageQuery(r, project, true)
	if err != nil {
		return uc.AggregateQuery{}, err
	}
	return uc.AggregateQuery{Filter: q.Filter, GroupBy: group, Cursor: q.Cursor, Limit: q.Limit}, nil
}

type usageDTO struct {
	Input       *mc.TokenCount `json:"input_tokens"`
	Output      *mc.TokenCount `json:"output_tokens"`
	Total       *mc.TokenCount `json:"total_tokens"`
	CachedInput *mc.TokenCount `json:"cached_input_tokens"`
	CacheWrite  *mc.TokenCount `json:"cache_write_tokens"`
	Reasoning   *mc.TokenCount `json:"reasoning_tokens"`
	Source      mc.UsageSource `json:"source"`
}
type finalDTO struct {
	Status        uc.TerminalStatus `json:"status"`
	FinishedAt    f.Instant         `json:"finished_at"`
	ErrorCategory *mc.ErrorCategory `json:"error_category"`
}
type invocationDTO struct {
	ID                mc.InvocationID `json:"id"`
	CallID            mc.CallID       `json:"call_id"`
	AttemptIndex      f.Sequence      `json:"attempt_index"`
	ProjectID         id.ProjectID    `json:"project_id"`
	ConsumerKind      mc.ConsumerKind `json:"consumer_kind"`
	Purpose           mc.Purpose      `json:"purpose"`
	AgentID           *id.AgentID     `json:"agent_id"`
	ExecutionID       *id.ExecutionID `json:"execution_id"`
	MeetingID         *string         `json:"meeting_id"`
	ProviderID        mc.ProviderID   `json:"provider_id"`
	ModelID           mc.ModelID      `json:"model_id"`
	ProviderName      string          `json:"provider_name"`
	ModelName         string          `json:"model_name"`
	ProviderModelID   string          `json:"provider_model_id"`
	Protocol          mc.Protocol     `json:"protocol"`
	ModelType         mc.ModelType    `json:"model_type"`
	LiveProviderID    *mc.ProviderID  `json:"live_provider_id"`
	LiveModelID       *mc.ModelID     `json:"live_model_id"`
	Dispatch          uc.Dispatch     `json:"dispatch"`
	StartedAt         f.Instant       `json:"started_at"`
	DispatchedAt      *f.Instant      `json:"dispatched_at"`
	Final             *finalDTO       `json:"final"`
	Usage             usageDTO        `json:"usage"`
	ProviderRequestID *string         `json:"provider_request_id"`
}
type listDTO struct {
	Items      []invocationDTO `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}
type fieldSummaryDTO struct {
	Sum          *mc.TokenCount `json:"sum"`
	KnownCount   mc.TokenCount  `json:"known_count"`
	UnknownCount mc.TokenCount  `json:"unknown_count"`
}
type summaryDTO struct {
	ConfirmedInvocations mc.TokenCount   `json:"confirmed_invocations"`
	DispatchUnknown      mc.TokenCount   `json:"dispatch_unknown"`
	Succeeded            mc.TokenCount   `json:"succeeded"`
	Failed               mc.TokenCount   `json:"failed"`
	Cancelled            mc.TokenCount   `json:"cancelled"`
	Unknown              mc.TokenCount   `json:"unknown"`
	Input                fieldSummaryDTO `json:"input"`
	Output               fieldSummaryDTO `json:"output"`
	Total                fieldSummaryDTO `json:"total"`
	CachedInput          fieldSummaryDTO `json:"cached_input"`
	CacheWrite           fieldSummaryDTO `json:"cache_write"`
	Reasoning            fieldSummaryDTO `json:"reasoning"`
	AsOf                 f.Instant       `json:"as_of"`
}
type groupKeyDTO struct {
	By  uc.GroupBy `json:"by"`
	ID  *string    `json:"id"`
	Day *string    `json:"day"`
}
type groupDTO struct {
	Key     groupKeyDTO `json:"key"`
	Summary summaryDTO  `json:"summary"`
}
type aggregateDTO struct {
	Items      []groupDTO `json:"items"`
	NextCursor *string    `json:"next_cursor"`
	AsOf       f.Instant  `json:"as_of"`
}
type projectDTO struct {
	ID              id.ProjectID `json:"id"`
	OwnerUserID     id.UserID    `json:"owner_user_id"`
	Name            string       `json:"name"`
	NormalizedName  string       `json:"normalized_name"`
	Description     string       `json:"description"`
	Lifecycle       pc.Lifecycle `json:"lifecycle"`
	Version         f.Version    `json:"version"`
	CurrentSprintID *pc.SprintID `json:"current_sprint_id"`
	CreatedAt       f.Instant    `json:"created_at"`
	UpdatedAt       f.Instant    `json:"updated_at"`
	ArchivedAt      *f.Instant   `json:"archived_at"`
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func validResultCursor(value string) bool {
	return len(value) <= maxCursorBytes && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// Called only after the service returned a successful committed read. Projection
// failure cannot change that known transaction state into NotStarted/Unknown.
func projectionFailure() error { return f.NewFault(f.DependencyUnavailable, f.Committed) }
func projectionContext(ctx context.Context) error {
	if ctx == nil {
		return projectionFailure()
	}
	if err := ctx.Err(); err != nil {
		return f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(err)
	}
	return nil
}
func encodeRepresentation(ctx context.Context, value any) ([]byte, error) {
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	body, err := json.Marshal(value)
	if err != nil || len(body) > maxRepresentationBytes {
		return nil, projectionFailure()
	}
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	return body, nil
}
func encodeList(ctx context.Context, query uc.Query, page uc.Page) ([]byte, error) {
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	if query.Validate() != nil || page.Validate() != nil || len(page.Items) > query.Limit || !validResultCursor(page.NextCursor) {
		return nil, projectionFailure()
	}
	out := listDTO{Items: make([]invocationDTO, 0, len(page.Items)), NextCursor: nullableString(page.NextCursor)}
	for _, row := range page.Items {
		if err := projectionContext(ctx); err != nil {
			return nil, err
		}
		// Page.Validate above validates even the fields deliberately omitted here.
		if row.Consumer.ProjectID != query.Filter.ProjectID {
			return nil, projectionFailure()
		}
		var final *finalDTO
		if row.Final != nil {
			final = &finalDTO{Status: row.Final.Status, FinishedAt: row.Final.FinishedAt}
			if row.Final.Error != nil {
				category := row.Final.Error.Category
				final.ErrorCategory = &category
			}
		}
		out.Items = append(out.Items, invocationDTO{
			ID: row.ID, CallID: row.CallID, AttemptIndex: row.AttemptIndex,
			ProjectID: row.Consumer.ProjectID, ConsumerKind: row.Consumer.Kind, Purpose: row.Consumer.Purpose,
			AgentID: row.Consumer.AgentID, ExecutionID: row.Consumer.ExecutionID, MeetingID: nullableString(row.Consumer.MeetingID),
			ProviderID: row.Identity.ProviderID, ModelID: row.Identity.ModelID, ProviderName: row.Identity.ProviderName,
			ModelName: row.Identity.ModelName, ProviderModelID: row.Identity.ProviderModelID, Protocol: row.Identity.Protocol, ModelType: row.Identity.ModelType,
			LiveProviderID: row.LiveProviderID, LiveModelID: row.LiveModelID, Dispatch: row.Dispatch, StartedAt: row.StartedAt, DispatchedAt: row.DispatchedAt,
			Final: final, Usage: usageDTO{row.Usage.InputTokens, row.Usage.OutputTokens, row.Usage.TotalTokens, row.Usage.CachedInputTokens, row.Usage.CacheWriteTokens, row.Usage.ReasoningTokens, row.Usage.Source}, ProviderRequestID: nullableString(row.ProviderRequestID),
		})
	}
	return encodeRepresentation(ctx, out)
}
func projectFieldSummary(s uc.FieldSummary) fieldSummaryDTO {
	return fieldSummaryDTO{s.Sum, s.KnownCount, s.UnknownCount}
}
func encodeAggregate(ctx context.Context, query uc.AggregateQuery, page uc.AggregatePage) ([]byte, error) {
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	if query.Validate() != nil || page.Validate() != nil || len(page.Items) > query.Limit || !validResultCursor(page.NextCursor) {
		return nil, projectionFailure()
	}
	out := aggregateDTO{Items: make([]groupDTO, 0, len(page.Items)), NextCursor: nullableString(page.NextCursor), AsOf: page.AsOf}
	for _, row := range page.Items {
		if err := projectionContext(ctx); err != nil {
			return nil, err
		}
		if row.Key.By != query.GroupBy {
			return nil, projectionFailure()
		}
		s := row.Summary
		out.Items = append(out.Items, groupDTO{groupKeyDTO{row.Key.By, row.Key.ID, row.Key.Day}, summaryDTO{
			ConfirmedInvocations: s.ConfirmedInvocations, DispatchUnknown: s.DispatchUnknown, Succeeded: s.Succeeded, Failed: s.Failed, Cancelled: s.Cancelled, Unknown: s.Unknown,
			Input: projectFieldSummary(s.Input), Output: projectFieldSummary(s.Output), Total: projectFieldSummary(s.Total), CachedInput: projectFieldSummary(s.CachedInput), CacheWrite: projectFieldSummary(s.CacheWrite), Reasoning: projectFieldSummary(s.Reasoning), AsOf: s.AsOf,
		}})
	}
	return encodeRepresentation(ctx, out)
}
func encodeProject(ctx context.Context, actor id.Actor, project pc.ProjectRef) ([]byte, error) {
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human || project.Validate() != nil || project.OwnerUserID.String() != actor.Details().UserID {
		return nil, projectionFailure()
	}
	return encodeRepresentation(ctx, projectDTO{project.ID, project.OwnerUserID, project.Name, project.NormalizedName, project.Description, project.Lifecycle, project.Version, project.CurrentSprintID, project.CreatedAt, project.UpdatedAt, project.ArchivedAt})
}
