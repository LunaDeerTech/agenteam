package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type ProviderInput struct {
	Name          string            `json:"name"`
	Protocol      Protocol          `json:"protocol"`
	BaseURL       string            `json:"base_url"`
	Enabled       bool              `json:"enabled"`
	CredentialRef *sc.CredentialRef `json:"credential_ref,omitempty"`
	Options       json.RawMessage   `json:"options"`
}

func (p ProviderInput) Validate() error {
	if !name(p.Name, 128) || !p.Protocol.Valid() || !endpoint(p.BaseURL) || p.CredentialRef != nil && p.CredentialRef.Validate() != nil || objectJSON(p.Options, 64<<10) != nil {
		return bad()
	}
	return nil
}
func (p ProviderInput) Clone() ProviderInput {
	p.CredentialRef = copyPtr(p.CredentialRef)
	p.Options = bytes.Clone(p.Options)
	return p
}

type ProviderView struct {
	ID        ProviderID    `json:"id"`
	Scope     id.Scope      `json:"scope"`
	Input     ProviderInput `json:"input"`
	Version   f.Version     `json:"version"`
	CreatedAt f.Instant     `json:"created_at"`
	UpdatedAt f.Instant     `json:"updated_at"`
}

func (p ProviderView) Validate() error {
	if p.ID.Validate() != nil || !configScope(p.Scope) || p.Input.Validate() != nil || p.Version.Validate() != nil || p.CreatedAt.Validate() != nil || p.UpdatedAt.Validate() != nil || p.UpdatedAt.Time().Before(p.CreatedAt.Time()) {
		return bad()
	}
	if p.Scope.Details().Kind == id.ProjectScope && !p.Input.Protocol.Supports(ChatModel) {
		return bad()
	}
	if p.Input.CredentialRef != nil && !p.Input.CredentialRef.Details().Scope.Equal(p.Scope) {
		return bad()
	}
	return nil
}
func (p ProviderView) Clone() ProviderView { p.Input = p.Input.Clone(); return p }

type ModelInput struct {
	Name             string            `json:"name"`
	ProviderModelID  string            `json:"provider_model_id"`
	Type             ModelType         `json:"type"`
	Enabled          bool              `json:"enabled"`
	Parameters       json.RawMessage   `json:"parameters"`
	RequestOverwrite json.RawMessage   `json:"request_overwrite"`
	HeaderOverwrite  map[string]string `json:"header_overwrite"`
	Capabilities     Capabilities      `json:"capabilities"`
}

func (m ModelInput) Validate() error {
	if !name(m.Name, 128) || !text(m.ProviderModelID, 256) || !m.Type.Valid() || objectJSON(m.Parameters, 64<<10) != nil || overwrite(m.RequestOverwrite, m.HeaderOverwrite) != nil || m.Capabilities.Validate() != nil {
		return bad()
	}
	return nil
}
func (m ModelInput) Clone() ModelInput {
	m.Parameters = bytes.Clone(m.Parameters)
	m.RequestOverwrite = bytes.Clone(m.RequestOverwrite)
	m.HeaderOverwrite = copyMap(m.HeaderOverwrite)
	m.Capabilities = m.Capabilities.Clone()
	return m
}

type ModelView struct {
	ID         ModelID    `json:"id"`
	ProviderID ProviderID `json:"provider_id"`
	Scope      id.Scope   `json:"scope"`
	Input      ModelInput `json:"input"`
	Version    f.Version  `json:"version"`
	CreatedAt  f.Instant  `json:"created_at"`
	UpdatedAt  f.Instant  `json:"updated_at"`
}

func (m ModelView) Validate() error {
	if m.ID.Validate() != nil || m.ProviderID.Validate() != nil || !configScope(m.Scope) || m.Input.Validate() != nil || m.Version.Validate() != nil || m.CreatedAt.Validate() != nil || m.UpdatedAt.Validate() != nil || m.UpdatedAt.Time().Before(m.CreatedAt.Time()) || m.Scope.Details().Kind == id.ProjectScope && m.Input.Type != ChatModel {
		return bad()
	}
	return nil
}
func (m ModelView) Clone() ModelView { m.Input = m.Input.Clone(); return m }

type SelectorKind string

const (
	EmbeddingSelector      SelectorKind = "embedding"
	MemorySelector         SelectorKind = "memory"
	RerankerSelector       SelectorKind = "reranker"
	ImageSelector          SelectorKind = "image"
	MeetingSummarySelector SelectorKind = "meeting_summary"
)

func (k SelectorKind) Valid() bool {
	return one(string(k), "embedding", "memory", "reranker", "image", "meeting_summary")
}

type SelectionRef struct {
	Kind      string        `json:"kind"`
	ProjectID *id.ProjectID `json:"project_id,omitempty"`
	Selector  SelectorKind  `json:"selector,omitempty"`
	Version   *f.Version    `json:"version,omitempty"`
}

func (s SelectionRef) Validate() error {
	if s.Version != nil && s.Version.Validate() != nil {
		return bad()
	}
	switch s.Kind {
	case "direct":
		if s.ProjectID != nil || s.Selector != "" || s.Version != nil {
			return bad()
		}
	case "platform":
		if s.ProjectID != nil || !s.Selector.Valid() || s.Selector == MeetingSummarySelector {
			return bad()
		}
	case "project_summary":
		if s.ProjectID == nil || s.ProjectID.Validate() != nil || s.Selector != MeetingSummarySelector {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (s SelectionRef) Clone() SelectionRef {
	s.ProjectID = copyPtr(s.ProjectID)
	s.Version = copyPtr(s.Version)
	return s
}

type SelectionRequest struct {
	Consumer  Consumer     `json:"consumer"`
	ModelRef  *ModelID     `json:"model_ref,omitempty"`
	Selection SelectionRef `json:"selection"`
}

func (s SelectionRequest) Validate() error {
	if s.Consumer.Validate() != nil || s.Selection.Validate() != nil {
		return bad()
	}
	if s.Selection.Kind == "direct" {
		if s.ModelRef == nil || s.ModelRef.Validate() != nil || s.Consumer.Purpose.ModelType() != ChatModel || s.Consumer.Kind == MemoryConsumer || s.Consumer.Kind == MeetingConsumer {
			return bad()
		}
	} else {
		if s.ModelRef != nil {
			return bad()
		}
		if s.Selection.Kind == "project_summary" {
			if *s.Selection.ProjectID != s.Consumer.ProjectID || s.Consumer.Kind != MeetingConsumer {
				return bad()
			}
		} else {
			want := map[Purpose]SelectorKind{KnowledgeEmbedding: EmbeddingSelector, MemoryEmbedding: EmbeddingSelector, MemoryExtraction: MemorySelector, MemoryConsolidation: MemorySelector, MemoryReflection: MemorySelector, Rerank: RerankerSelector, ImageGeneration: ImageSelector}
			if want[s.Consumer.Purpose] != s.Selection.Selector {
				return bad()
			}
		}
	}
	return nil
}
func (s SelectionRequest) Clone() SelectionRequest {
	s.Consumer = s.Consumer.Clone()
	s.ModelRef = copyPtr(s.ModelRef)
	s.Selection = s.Selection.Clone()
	return s
}

type SelectionResult struct {
	Selected         *ModelID   `json:"selected"`
	ModelVersion     *f.Version `json:"model_version"`
	SelectionVersion *f.Version `json:"selection_version"`
}

func (s SelectionResult) Validate() error {
	if (s.Selected == nil) != (s.ModelVersion == nil) || s.Selected != nil && s.Selected.Validate() != nil || s.ModelVersion != nil && s.ModelVersion.Validate() != nil || s.SelectionVersion != nil && s.SelectionVersion.Validate() != nil {
		return bad()
	}
	return nil
}
func (s SelectionResult) ValidateFor(r SelectionRequest) error {
	if s.Validate() != nil || r.Validate() != nil {
		return bad()
	}
	if s.Selected == nil && (r.Selection.Kind != "platform" || r.Selection.Selector != RerankerSelector && r.Selection.Selector != ImageSelector) {
		return bad()
	}
	if r.Selection.Kind == "direct" {
		if s.SelectionVersion != nil || s.Selected == nil || *s.Selected != *r.ModelRef {
			return bad()
		}
	} else if s.SelectionVersion == nil {
		return bad()
	}
	return nil
}
func (s SelectionResult) Clone() SelectionResult {
	s.Selected = copyPtr(s.Selected)
	s.ModelVersion = copyPtr(s.ModelVersion)
	s.SelectionVersion = copyPtr(s.SelectionVersion)
	return s
}

type PlatformSelection struct {
	ID        string    `json:"id"`
	Version   f.Version `json:"version"`
	Embedding ModelID   `json:"embedding"`
	Memory    ModelID   `json:"memory"`
	Reranker  *ModelID  `json:"reranker"`
	Image     *ModelID  `json:"image"`
}

func (s PlatformSelection) Validate() error {
	if !uuid(s.ID) || s.Version.Validate() != nil || s.Embedding.Validate() != nil || s.Memory.Validate() != nil || s.Reranker != nil && s.Reranker.Validate() != nil || s.Image != nil && s.Image.Validate() != nil {
		return bad()
	}
	return nil
}
func (s PlatformSelection) Clone() PlatformSelection {
	s.Reranker = copyPtr(s.Reranker)
	s.Image = copyPtr(s.Image)
	return s
}

type ProjectModelSettings struct {
	ProjectID      id.ProjectID `json:"project_id"`
	Version        f.Version    `json:"version"`
	MeetingSummary ModelID      `json:"meeting_summary"`
}

func (s ProjectModelSettings) Validate() error {
	if s.ProjectID.Validate() != nil || s.Version.Validate() != nil || s.MeetingSummary.Validate() != nil {
		return bad()
	}
	return nil
}

type CommandMeta struct {
	Actor id.Actor         `json:"-"`
	Scope id.Scope         `json:"scope"`
	Key   f.IdempotencyKey `json:"-"`
}

func (m CommandMeta) Validate() error {
	if m.Actor.Validate() != nil || m.Actor.Details().Kind != id.Human || !configScope(m.Scope) || m.Key.Validate() != nil {
		return bad()
	}
	return nil
}

type CreateProviderRequest struct {
	CommandMeta
	Input ProviderInput `json:"input"`
}
type UpdateProviderRequest struct {
	CommandMeta
	ID              ProviderID    `json:"id"`
	ExpectedVersion f.Version     `json:"expected_version"`
	Input           ProviderInput `json:"input"`
}
type DeleteProviderRequest struct {
	CommandMeta
	ID              ProviderID `json:"id"`
	ExpectedVersion f.Version  `json:"expected_version"`
}
type CreateModelRequest struct {
	CommandMeta
	ProviderID ProviderID `json:"provider_id"`
	Input      ModelInput `json:"input"`
}
type UpdateModelRequest struct {
	CommandMeta
	ID              ModelID    `json:"id"`
	ExpectedVersion f.Version  `json:"expected_version"`
	Input           ModelInput `json:"input"`
}
type DeleteModelRequest struct {
	CommandMeta
	ID              ModelID   `json:"id"`
	ExpectedVersion f.Version `json:"expected_version"`
	Replacement     *ModelID  `json:"replacement"`
}
type UpdatePlatformSelectionRequest struct {
	CommandMeta
	ExpectedVersion f.Version         `json:"expected_version"`
	Selection       PlatformSelection `json:"selection"`
}
type UpdateProjectModelSettingsRequest struct {
	CommandMeta
	ExpectedVersion f.Version            `json:"expected_version"`
	Settings        ProjectModelSettings `json:"settings"`
}
type CommandReceipt struct {
	Kind               string     `json:"kind"`
	ResourceID         string     `json:"resource_id"`
	Version            f.Version  `json:"version"`
	AffectedReferences TokenCount `json:"affected_references"`
}

func (r CreateProviderRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.Input.Validate() != nil || r.Scope.Details().Kind == id.ProjectScope && !r.Input.Protocol.Supports(ChatModel) || r.Input.CredentialRef != nil && !r.Input.CredentialRef.Details().Scope.Equal(r.Scope) {
		return bad()
	}
	return nil
}
func (r UpdateProviderRequest) Validate() error {
	if r.ID.Validate() != nil || r.ExpectedVersion.Validate() != nil {
		return bad()
	}
	return (CreateProviderRequest{r.CommandMeta, r.Input}).Validate()
}
func (r DeleteProviderRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.ID.Validate() != nil || r.ExpectedVersion.Validate() != nil {
		return bad()
	}
	return nil
}
func (r CreateModelRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.ProviderID.Validate() != nil || r.Input.Validate() != nil || r.Scope.Details().Kind == id.ProjectScope && r.Input.Type != ChatModel {
		return bad()
	}
	return nil
}
func (r UpdateModelRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.ID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.Input.Validate() != nil || r.Scope.Details().Kind == id.ProjectScope && r.Input.Type != ChatModel {
		return bad()
	}
	return nil
}
func (r DeleteModelRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.ID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.Replacement != nil && (r.Replacement.Validate() != nil || *r.Replacement == r.ID) {
		return bad()
	}
	return nil
}
func (r UpdatePlatformSelectionRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.Scope.Details().Kind != id.System || r.ExpectedVersion.Validate() != nil || r.Selection.Validate() != nil || r.ExpectedVersion != r.Selection.Version {
		return bad()
	}
	return nil
}
func (r UpdateProjectModelSettingsRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.Scope.Details().Kind != id.ProjectScope || r.ExpectedVersion.Validate() != nil || r.Settings.Validate() != nil || r.Scope.Details().ProjectID != r.Settings.ProjectID.String() || r.ExpectedVersion != r.Settings.Version {
		return bad()
	}
	return nil
}
func (r CommandReceipt) Validate() error {
	if !one(r.Kind, "provider.create", "provider.update", "provider.delete", "model.create", "model.update", "model.delete", "model.selection.update") || !uuid(r.ResourceID) || r.Version.Validate() != nil || r.AffectedReferences.Validate() != nil {
		return bad()
	}
	return nil
}
func configScope(s id.Scope) bool {
	return s.Validate() == nil && (s.Details().Kind == id.System || s.Details().Kind == id.ProjectScope)
}
func endpoint(s string) bool {
	if !text(s, 8192) || strings.ContainsAny(s, "\r\n\t #") {
		return false
	}
	u, e := url.Parse(s)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return false
		}
	}
	return true
}
func overwrite(raw json.RawMessage, headers map[string]string) error {
	if objectJSON(raw, 64<<10) != nil {
		return bad()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return bad()
	}
	for k := range fields {
		if one(strings.ToLower(k), "model", "messages", "input", "prompt", "query", "documents", "tools", "tool_choice", "url", "base_url", "authorization", "api_key", "credential", "headers", "stream", "stream_options") {
			return bad()
		}
	}
	size := 0
	seen := map[string]bool{}
	for k, v := range headers {
		key := strings.ToLower(k)
		if seen[key] || !safeHeaderName(k) || strings.ContainsAny(v, "\r\n\x00") || !utf8String(v) || one(key, "authorization", "proxy-authorization", "x-api-key", "api-key", "host", "cookie", "set-cookie", "content-length", "transfer-encoding", "connection", "upgrade", "trailer", "te") {
			return bad()
		}
		seen[key] = true
		size += len(k) + len(v)
	}
	if size > 16<<10 || size+len(raw) > 64<<10 {
		return bad()
	}
	return nil
}
func safeHeaderName(k string) bool {
	if k == "" {
		return false
	}
	for _, c := range k {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
func utf8String(s string) bool { return strings.ToValidUTF8(s, "\x00") == s }

func (ProviderInput) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_provider_input")) }
func (ProviderInput) LogValue() slog.Value       { return slog.StringValue("model_provider_input") }

func (ProviderView) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_provider_view")) }
func (ProviderView) LogValue() slog.Value       { return slog.StringValue("model_provider_view") }

func (ModelInput) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_model_input")) }
func (ModelInput) LogValue() slog.Value       { return slog.StringValue("model_model_input") }

func (ModelView) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_model_view")) }
func (ModelView) LogValue() slog.Value       { return slog.StringValue("model_model_view") }

func (SelectionRef) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_selection_ref")) }
func (SelectionRef) LogValue() slog.Value       { return slog.StringValue("model_selection_ref") }

func (SelectionRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_selection_request"))
}
func (SelectionRequest) LogValue() slog.Value { return slog.StringValue("model_selection_request") }

func (SelectionResult) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_selection_result")) }
func (SelectionResult) LogValue() slog.Value       { return slog.StringValue("model_selection_result") }

func (PlatformSelection) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_platform_selection"))
}
func (PlatformSelection) LogValue() slog.Value { return slog.StringValue("model_platform_selection") }

func (ProjectModelSettings) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_project_model_settings"))
}
func (ProjectModelSettings) LogValue() slog.Value {
	return slog.StringValue("model_project_model_settings")
}

func (CommandMeta) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_command_meta")) }
func (CommandMeta) LogValue() slog.Value       { return slog.StringValue("model_command_meta") }

func (CreateProviderRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_create_provider_request"))
}
func (CreateProviderRequest) LogValue() slog.Value {
	return slog.StringValue("model_create_provider_request")
}

func (UpdateProviderRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_update_provider_request"))
}
func (UpdateProviderRequest) LogValue() slog.Value {
	return slog.StringValue("model_update_provider_request")
}

func (DeleteProviderRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_delete_provider_request"))
}
func (DeleteProviderRequest) LogValue() slog.Value {
	return slog.StringValue("model_delete_provider_request")
}

func (CreateModelRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_create_model_request"))
}
func (CreateModelRequest) LogValue() slog.Value {
	return slog.StringValue("model_create_model_request")
}

func (UpdateModelRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_update_model_request"))
}
func (UpdateModelRequest) LogValue() slog.Value {
	return slog.StringValue("model_update_model_request")
}

func (DeleteModelRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_delete_model_request"))
}
func (DeleteModelRequest) LogValue() slog.Value {
	return slog.StringValue("model_delete_model_request")
}

func (UpdatePlatformSelectionRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_update_platform_selection_request"))
}
func (UpdatePlatformSelectionRequest) LogValue() slog.Value {
	return slog.StringValue("model_update_platform_selection_request")
}

func (UpdateProjectModelSettingsRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_update_project_model_settings_request"))
}
func (UpdateProjectModelSettingsRequest) LogValue() slog.Value {
	return slog.StringValue("model_update_project_model_settings_request")
}

func (CommandReceipt) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_command_receipt")) }
func (CommandReceipt) LogValue() slog.Value       { return slog.StringValue("model_command_receipt") }
