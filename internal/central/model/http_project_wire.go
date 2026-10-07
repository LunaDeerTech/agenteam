package model

import (
	"bytes"
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
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const projectHTTPMaxRepresentation = 8 << 20
const projectHTTPMaxQuery = 32768
const projectHTTPMaxCursor = 8192

func projectHTTPQuery(r *http.Request, list bool) (ProjectQuery, error) {
	invalid := func() (ProjectQuery, error) { return ProjectQuery{}, fault(f.InvalidArgument) }
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > projectHTTPMaxQuery ||
		r.URL.ForceQuery && r.URL.RawQuery == "" || !list && r.URL.RawQuery != "" {
		return invalid()
	}
	query := ProjectQuery{Limit: 50}
	if r.URL.RawQuery == "" {
		return query, nil
	}
	if strings.Contains(r.URL.RawQuery, ";") {
		return invalid()
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(r.URL.RawQuery, "&") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return invalid()
		}
		key, err := url.QueryUnescape(key)
		if err != nil {
			return invalid()
		}
		value, err = url.QueryUnescape(value)
		if err != nil || key == "" || value == "" || !utf8.ValidString(key) || !utf8.ValidString(value) ||
			strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0) || seen[key] {
			return invalid()
		}
		seen[key] = true
		switch key {
		case "cursor":
			if len(value) > projectHTTPMaxCursor {
				return ProjectQuery{}, fault(f.CursorInvalid)
			}
			query.Cursor = value
		case "limit":
			if len(value) > 3 || value[0] < '1' || value[0] > '9' {
				return invalid()
			}
			for _, ch := range value {
				if ch < '0' || ch > '9' {
					return invalid()
				}
			}
			query.Limit, err = strconv.Atoi(value)
			if err != nil || query.Limit < 1 || query.Limit > 100 {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return query, nil
}

type projectHTTPScope struct {
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id,omitempty"`
}
type projectHTTPCapabilities struct {
	ToolCalls             bool           `json:"tool_calls"`
	ParallelToolCalls     bool           `json:"parallel_tool_calls"`
	Streaming             bool           `json:"streaming"`
	Reasoning             bool           `json:"reasoning"`
	InputModalities       []string       `json:"input_modalities"`
	OutputModalities      []string       `json:"output_modalities"`
	ReasoningEfforts      []string       `json:"reasoning_efforts"`
	StructuredOutputModes []string       `json:"structured_output_modes"`
	ContextLength         *mc.TokenCount `json:"context_length"`
	MaxOutput             *mc.TokenCount `json:"max_output"`
}
type projectHTTPProviderInput struct {
	Name          string           `json:"name"`
	Protocol      mc.Protocol      `json:"protocol"`
	BaseURL       string           `json:"base_url"`
	Enabled       bool             `json:"enabled"`
	CredentialRef *sc.CredentialID `json:"credential_ref"`
	Options       json.RawMessage  `json:"options"`
}
type projectHTTPProviderDTO struct {
	ID        mc.ProviderID            `json:"id"`
	Scope     projectHTTPScope         `json:"scope"`
	Input     projectHTTPProviderInput `json:"input"`
	Version   f.Version                `json:"version"`
	CreatedAt f.Instant                `json:"created_at"`
	UpdatedAt f.Instant                `json:"updated_at"`
}
type projectHTTPModelInput struct {
	Name             string                  `json:"name"`
	ProviderModelID  string                  `json:"provider_model_id"`
	Type             mc.ModelType            `json:"type"`
	Enabled          bool                    `json:"enabled"`
	Parameters       json.RawMessage         `json:"parameters"`
	RequestOverwrite json.RawMessage         `json:"request_overwrite"`
	HeaderOverwrite  map[string]string       `json:"header_overwrite"`
	Capabilities     projectHTTPCapabilities `json:"capabilities"`
}
type projectHTTPModelDTO struct {
	ID         mc.ModelID            `json:"id"`
	ProviderID mc.ProviderID         `json:"provider_id"`
	Scope      projectHTTPScope      `json:"scope"`
	Input      projectHTTPModelInput `json:"input"`
	Version    f.Version             `json:"version"`
	CreatedAt  f.Instant             `json:"created_at"`
	UpdatedAt  f.Instant             `json:"updated_at"`
}
type projectHTTPAvailableDTO struct {
	ID           mc.ModelID              `json:"id"`
	ProviderID   mc.ProviderID           `json:"provider_id"`
	Scope        projectHTTPScope        `json:"scope"`
	Name         string                  `json:"name"`
	ProviderName string                  `json:"provider_name"`
	Version      f.Version               `json:"version"`
	Capabilities projectHTTPCapabilities `json:"capabilities"`
}
type projectHTTPPage[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// This accounting precedes domain Validate (which allocates maps), any new
// DTO clone, and Marshal. It bounds only this HTTP representation work, not the
// already completed database/library reads, allocations or whole-process RSS.
type projectHTTPBound struct {
	ctx   context.Context
	used  int
	ticks uint32
	err   error
}

func projectHTTPContext(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if meetingSummaryHTTPExpired(ctx) {
		return context.DeadlineExceeded
	}
	return nil
}
func (b *projectHTTPBound) fail() bool {
	if b.err == nil {
		b.err = unavailable(nil)
	}
	return false
}
func (b *projectHTTPBound) live() bool {
	if b.err != nil {
		return false
	}
	if err := projectHTTPContext(b.ctx); err != nil {
		b.err = err
		return false
	}
	return true
}
func (b *projectHTTPBound) add(n int) bool {
	if b.err != nil {
		return false
	}
	b.ticks++
	if b.ticks&1023 == 0 && !b.live() {
		return false
	}
	if n < 0 || b.used < 0 || b.used > projectHTTPMaxRepresentation || n > projectHTTPMaxRepresentation-b.used {
		return b.fail()
	}
	b.used += n
	return true
}

// room is a safe lower-bound rejection, not a reservation. No multiplication
// occurs until the division establishes that it cannot overflow the budget.
func (b *projectHTTPBound) room(count, minimum int) bool {
	if !b.live() {
		return false
	}
	if count < 0 || minimum < 1 || b.used < 0 || b.used > projectHTTPMaxRepresentation || count > (projectHTTPMaxRepresentation-b.used)/minimum {
		return b.fail()
	}
	return true
}
func (b *projectHTTPBound) text(value string, maxBytes int) bool {
	if !b.live() {
		return false
	}
	if len(value) > maxBytes || !b.room(len(value), 1) || !b.add(2) {
		return b.fail()
	}
	// Match encoding/json's default string escaping, including HTML and the
	// JavaScript line separators. Invalid UTF-8 never becomes a replacement DTO.
	for i := 0; i < len(value); {
		ch := value[i]
		if ch < utf8.RuneSelf {
			size := 1
			switch ch {
			case '\\', '"', '\b', '\f', '\n', '\r', '\t':
				size = 2
			case '<', '>', '&':
				size = 6
			default:
				if ch < 0x20 {
					size = 6
				}
			}
			if !b.add(size) {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			return b.fail()
		}
		encoded := size
		if r == '\u2028' || r == '\u2029' {
			encoded = 6
		}
		if !b.add(encoded) {
			return false
		}
		i += size
	}
	return true
}
func (b *projectHTTPBound) strings(values []string, maxTokenBytes int) bool {
	if !b.room(len(values), 3) {
		return false
	}
	if !b.add(2 + len(values)) {
		return false
	} // brackets and a conservative comma per item
	for _, value := range values {
		if !b.text(value, maxTokenBytes) {
			return false
		}
	}
	return true
}
func (b *projectHTTPBound) raw(value json.RawMessage) bool {
	if len(value) > 64<<10 || !b.room(len(value), 6) {
		return b.fail()
	}
	// RawMessage Marshal compacts then HTML-escapes. Six bytes per input byte
	// safely includes <>& and U+2028/U+2029, even before JSON validation.
	if !b.add(len(value)*6) || !utf8.Valid(value) {
		return b.fail()
	}
	return true
}
func (b *projectHTTPBound) capabilities(c mc.Capabilities) bool {
	// All ten field names, punctuation, four booleans, and two nullable quoted
	// int64s fit below 384 bytes. Array elements are counted separately.
	return b.add(384) && b.strings(c.InputModalities, 6) && b.strings(c.OutputModalities, 6) &&
		b.strings(c.StructuredOutputModes, 11) && b.strings(c.ReasoningEfforts, 32)
}
func (b *projectHTTPBound) provider(v mc.ProviderView) bool {
	// 512 covers fixed keys, ID/scope/ref IDs, version, both instants, bool and
	// all punctuation. The untrusted strings and RawMessage remain separate.
	return b.add(512) && b.text(v.Input.Name, 512) && b.text(string(v.Input.Protocol), 32) &&
		b.text(v.Input.BaseURL, 8192) && b.raw(v.Input.Options)
}
func (b *projectHTTPBound) model(v mc.ModelView) bool {
	p := v.Input
	if !b.add(512) || !b.text(p.Name, 512) || !b.text(p.ProviderModelID, 256) || !b.text(string(p.Type), 32) ||
		!b.raw(p.Parameters) || !b.raw(p.RequestOverwrite) || !b.room(len(p.HeaderOverwrite), 6) || !b.add(2+2*len(p.HeaderOverwrite)) {
		return false
	}
	headerBytes := 0
	for key, value := range p.HeaderOverwrite {
		if len(key) > (16<<10)-headerBytes {
			return b.fail()
		}
		headerBytes += len(key)
		if len(value) > (16<<10)-headerBytes {
			return b.fail()
		}
		headerBytes += len(value)
		if !b.text(key, 16<<10) || !b.text(value, 16<<10) {
			return false
		}
	}
	return b.capabilities(p.Capabilities)
}
func (b *projectHTTPBound) available(v AvailableChatModel) bool {
	return b.add(384) && b.text(v.Name, 512) && b.text(v.ProviderName, 512) && b.capabilities(v.Capabilities)
}
func (b *projectHTTPBound) page(count int, next string, q ProjectQuery) bool {
	if q.Limit < 1 || q.Limit > 100 || count > q.Limit || count < 0 || count == 0 && next != "" ||
		next != "" && count != q.Limit || len(next) > projectHTTPMaxCursor || strings.ContainsRune(next, 0) {
		return b.fail()
	}
	return b.add(128+count) && b.text(next, projectHTTPMaxCursor)
}

func projectHTTPReadScope(scope id.Scope, project id.ProjectID, system bool) (projectHTTPScope, bool) {
	if scope.Validate() != nil || project.Validate() != nil {
		return projectHTTPScope{}, false
	}
	d := scope.Details()
	if d.Kind == id.ProjectScope && d.ProjectID == project.String() && d.AgentID == "" {
		return projectHTTPScope{Kind: "project", ProjectID: d.ProjectID}, true
	}
	if system && d.Kind == id.System && d.ProjectID == "" && d.AgentID == "" {
		return projectHTTPScope{Kind: "system"}, true
	}
	return projectHTTPScope{}, false
}
func projectHTTPCaps(c mc.Capabilities) projectHTTPCapabilities {
	copy := c.Clone()
	if copy.InputModalities == nil {
		copy.InputModalities = []string{}
	}
	if copy.OutputModalities == nil {
		copy.OutputModalities = []string{}
	}
	if copy.ReasoningEfforts == nil {
		copy.ReasoningEfforts = []string{}
	}
	if copy.StructuredOutputModes == nil {
		copy.StructuredOutputModes = []string{}
	}
	return projectHTTPCapabilities{copy.ToolCalls, copy.ParallelToolCalls, copy.Streaming, copy.Reasoning,
		copy.InputModalities, copy.OutputModalities, copy.ReasoningEfforts, copy.StructuredOutputModes, copy.ContextLength, copy.MaxOutput}
}
func projectHTTPProviderValue(v mc.ProviderView, project id.ProjectID) (projectHTTPProviderDTO, error) {
	scope, ok := projectHTTPReadScope(v.Scope, project, false)
	if !ok || v.Validate() != nil {
		return projectHTTPProviderDTO{}, unavailable(nil)
	}
	p := v.Input
	var credential *sc.CredentialID
	if p.CredentialRef != nil {
		value := p.CredentialRef.Details().ID
		credential = &value
	}
	return projectHTTPProviderDTO{v.ID, scope, projectHTTPProviderInput{p.Name, p.Protocol, p.BaseURL, p.Enabled, credential, bytes.Clone(p.Options)}, v.Version, v.CreatedAt, v.UpdatedAt}, nil
}
func projectHTTPModelValue(v mc.ModelView, project id.ProjectID) (projectHTTPModelDTO, error) {
	scope, ok := projectHTTPReadScope(v.Scope, project, false)
	if !ok || v.Validate() != nil {
		return projectHTTPModelDTO{}, unavailable(nil)
	}
	p := v.Input
	headers := make(map[string]string, len(p.HeaderOverwrite))
	for key, value := range p.HeaderOverwrite {
		headers[key] = value
	}
	return projectHTTPModelDTO{v.ID, v.ProviderID, scope,
		projectHTTPModelInput{p.Name, p.ProviderModelID, p.Type, p.Enabled, bytes.Clone(p.Parameters), bytes.Clone(p.RequestOverwrite), headers, projectHTTPCaps(p.Capabilities)},
		v.Version, v.CreatedAt, v.UpdatedAt}, nil
}
func projectHTTPName(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= 128
}
func projectHTTPAvailableValue(v AvailableChatModel, project id.ProjectID) (projectHTTPAvailableDTO, error) {
	scope, ok := projectHTTPReadScope(v.Scope, project, true)
	if !ok || v.ID.Validate() != nil || v.ProviderID.Validate() != nil || v.Version.Validate() != nil ||
		!projectHTTPName(v.Name) || !projectHTTPName(v.ProviderName) || v.Capabilities.Validate() != nil {
		return projectHTTPAvailableDTO{}, unavailable(nil)
	}
	return projectHTTPAvailableDTO{v.ID, v.ProviderID, scope, v.Name, v.ProviderName, v.Version, projectHTTPCaps(v.Capabilities)}, nil
}
func projectHTTPMarshal(ctx context.Context, value any) ([]byte, error) {
	if err := projectHTTPContext(ctx); err != nil {
		return nil, err
	}
	body, err := json.Marshal(value)
	if err != nil || len(body) > projectHTTPMaxRepresentation {
		return nil, unavailable(nil)
	}
	if err := projectHTTPContext(ctx); err != nil {
		return nil, err
	}
	return body, nil
}
func projectHTTPNext(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func projectHTTPEncodeProvider(ctx context.Context, project id.ProjectID, target mc.ProviderID, value mc.ProviderView) ([]byte, error) {
	b := projectHTTPBound{ctx: ctx}
	if !b.live() || !b.provider(value) {
		return nil, b.err
	}
	if value.ID != target {
		return nil, unavailable(nil)
	}
	dto, err := projectHTTPProviderValue(value, project)
	if err != nil {
		return nil, err
	}
	return projectHTTPMarshal(ctx, dto)
}
func projectHTTPEncodeModel(ctx context.Context, project id.ProjectID, target mc.ModelID, value mc.ModelView) ([]byte, error) {
	b := projectHTTPBound{ctx: ctx}
	if !b.live() || !b.model(value) {
		return nil, b.err
	}
	if value.ID != target {
		return nil, unavailable(nil)
	}
	dto, err := projectHTTPModelValue(value, project)
	if err != nil {
		return nil, err
	}
	return projectHTTPMarshal(ctx, dto)
}
func projectHTTPBefore(at f.Instant, key string, previousAt f.Instant, previousKey string) bool {
	return at.Time().Before(previousAt.Time()) || at == previousAt && key < previousKey
}
func projectHTTPEncodeProviders(ctx context.Context, project id.ProjectID, q ProjectQuery, value ProviderPage) ([]byte, error) {
	b := projectHTTPBound{ctx: ctx}
	if !b.live() || !b.page(len(value.Items), value.NextCursor, q) {
		return nil, b.err
	}
	for _, v := range value.Items {
		if !b.provider(v) {
			return nil, b.err
		}
	}
	// The complete page has passed admission before any row is validated/copied.
	dto := projectHTTPPage[projectHTTPProviderDTO]{Items: make([]projectHTTPProviderDTO, 0, len(value.Items)), NextCursor: projectHTTPNext(value.NextCursor)}
	seen := make(map[mc.ProviderID]bool, len(value.Items))
	for i, v := range value.Items {
		if err := projectHTTPContext(ctx); err != nil {
			return nil, err
		}
		if seen[v.ID] || i > 0 && !projectHTTPBefore(v.CreatedAt, v.ID.String(), value.Items[i-1].CreatedAt, value.Items[i-1].ID.String()) {
			return nil, unavailable(nil)
		}
		seen[v.ID] = true
		row, err := projectHTTPProviderValue(v, project)
		if err != nil {
			return nil, err
		}
		dto.Items = append(dto.Items, row)
	}
	return projectHTTPMarshal(ctx, dto)
}
func projectHTTPEncodeModels(ctx context.Context, project id.ProjectID, q ProjectQuery, value ModelPage) ([]byte, error) {
	b := projectHTTPBound{ctx: ctx}
	if !b.live() || !b.page(len(value.Items), value.NextCursor, q) {
		return nil, b.err
	}
	for _, v := range value.Items {
		if !b.model(v) {
			return nil, b.err
		}
	}
	dto := projectHTTPPage[projectHTTPModelDTO]{Items: make([]projectHTTPModelDTO, 0, len(value.Items)), NextCursor: projectHTTPNext(value.NextCursor)}
	seen := make(map[mc.ModelID]bool, len(value.Items))
	for i, v := range value.Items {
		if err := projectHTTPContext(ctx); err != nil {
			return nil, err
		}
		if seen[v.ID] || i > 0 && !projectHTTPBefore(v.CreatedAt, v.ID.String(), value.Items[i-1].CreatedAt, value.Items[i-1].ID.String()) {
			return nil, unavailable(nil)
		}
		seen[v.ID] = true
		row, err := projectHTTPModelValue(v, project)
		if err != nil {
			return nil, err
		}
		dto.Items = append(dto.Items, row)
	}
	return projectHTTPMarshal(ctx, dto)
}
func projectHTTPEncodeAvailable(ctx context.Context, project id.ProjectID, q ProjectQuery, value AvailableChatModelPage) ([]byte, error) {
	b := projectHTTPBound{ctx: ctx}
	if !b.live() || !b.page(len(value.Items), value.NextCursor, q) {
		return nil, b.err
	}
	for _, v := range value.Items {
		if !b.available(v) {
			return nil, b.err
		}
	}
	dto := projectHTTPPage[projectHTTPAvailableDTO]{Items: make([]projectHTTPAvailableDTO, 0, len(value.Items)), NextCursor: projectHTTPNext(value.NextCursor)}
	seen := make(map[mc.ModelID]bool, len(value.Items))
	for _, v := range value.Items {
		if err := projectHTTPContext(ctx); err != nil {
			return nil, err
		}
		if seen[v.ID] {
			return nil, unavailable(nil)
		}
		seen[v.ID] = true
		row, err := projectHTTPAvailableValue(v, project)
		if err != nil {
			return nil, err
		}
		dto.Items = append(dto.Items, row)
	}
	return projectHTTPMarshal(ctx, dto)
}
