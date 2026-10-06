// Package contract defines Model data and trusted planning ports. Validation
// establishes structure, never current authorization, a commit, or joined I/O.
package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type Provider struct{}
type Model struct{}
type Snapshot struct{}
type Call struct{}
type Invocation struct{}
type ProviderID = f.ID[Provider]
type ModelID = f.ID[Model]
type SnapshotID = f.ID[Snapshot]
type CallID = f.ID[Call]
type InvocationID = f.ID[Invocation]
type ModelType string

const (
	ChatModel      ModelType = "chat"
	EmbeddingModel ModelType = "embedding"
	RerankerModel  ModelType = "reranker"
	ImageModel     ModelType = "image_generation"
)

type Protocol string

const (
	OpenAIChat        Protocol = "openai-chat-completions"
	AnthropicMessages Protocol = "anthropic-messages"
	OpenAIEmbeddings  Protocol = "openai-embeddings"
	JinaRerank        Protocol = "jina-rerank"
	OpenAIImages      Protocol = "openai-images-generations"
)

type ProfileID string

const (
	OpenAIChatV1        ProfileID = "openai-chat-completions-v1"
	AnthropicMessagesV1 ProfileID = "anthropic-messages-2023-06-01"
	OpenAIEmbeddingsV1  ProfileID = "openai-embeddings-v1"
	JinaRerankV1        ProfileID = "jina-rerank-v1"
	OpenAIImagesV1      ProfileID = "openai-images-generations-v1"
)

func (m ModelType) Valid() bool {
	return one(string(m), "chat", "embedding", "reranker", "image_generation")
}
func (p Protocol) Profile() ProfileID {
	switch p {
	case OpenAIChat:
		return OpenAIChatV1
	case AnthropicMessages:
		return AnthropicMessagesV1
	case OpenAIEmbeddings:
		return OpenAIEmbeddingsV1
	case JinaRerank:
		return JinaRerankV1
	case OpenAIImages:
		return OpenAIImagesV1
	}
	return ""
}
func (p Protocol) Valid() bool { return p.Profile() != "" }
func (p Protocol) Supports(t ModelType) bool {
	switch p {
	case OpenAIChat, AnthropicMessages:
		return t == ChatModel
	case OpenAIEmbeddings:
		return t == EmbeddingModel
	case JinaRerank:
		return t == RerankerModel
	case OpenAIImages:
		return t == ImageModel
	}
	return false
}

type Purpose string

const (
	AgentGeneration       Purpose = "agent_generation"
	AgentCompaction       Purpose = "agent_compaction"
	ApprovalAuto          Purpose = "approval_auto"
	MeetingSummaryInitial Purpose = "meeting_summary_initial"
	MeetingSummaryUpdate  Purpose = "meeting_summary_update"
	KnowledgeEmbedding    Purpose = "knowledge_embedding"
	MemoryEmbedding       Purpose = "memory_embedding"
	MemoryExtraction      Purpose = "memory_extraction"
	MemoryConsolidation   Purpose = "memory_consolidation"
	MemoryReflection      Purpose = "memory_reflection"
	Rerank                Purpose = "rerank"
	ImageGeneration       Purpose = "image_generation"
)

func (p Purpose) Valid() bool {
	return one(string(p), "agent_generation", "agent_compaction", "approval_auto", "meeting_summary_initial", "meeting_summary_update", "knowledge_embedding", "memory_embedding", "memory_extraction", "memory_consolidation", "memory_reflection", "rerank", "image_generation")
}
func (p Purpose) ModelType() ModelType {
	switch p {
	case KnowledgeEmbedding, MemoryEmbedding:
		return EmbeddingModel
	case Rerank:
		return RerankerModel
	case ImageGeneration:
		return ImageModel
	default:
		if p.Valid() {
			return ChatModel
		}
	}
	return ""
}

type ConsumerKind string

const (
	AgentConsumer     ConsumerKind = "agent"
	MeetingConsumer   ConsumerKind = "meeting"
	KnowledgeConsumer ConsumerKind = "knowledge"
	MemoryConsumer    ConsumerKind = "memory"
	ToolConsumer      ConsumerKind = "tool"
)

func (k ConsumerKind) Valid() bool {
	return one(string(k), "agent", "meeting", "knowledge", "memory", "tool")
}

type TokenCount int64

func (n TokenCount) Validate() error {
	if n < 0 {
		return bad()
	}
	return nil
}
func (n TokenCount) MarshalJSON() ([]byte, error) {
	if n.Validate() != nil {
		return nil, bad()
	}
	return json.Marshal(strconv.FormatInt(int64(n), 10))
}
func (n *TokenCount) UnmarshalJSON(b []byte) error {
	if n == nil {
		return bad()
	}
	var s string
	if json.Unmarshal(b, &s) != nil || s == "" || len(s) > 19 || len(s) > 1 && s[0] == '0' {
		return bad()
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return bad()
		}
	}
	v, e := strconv.ParseInt(s, 10, 64)
	if e != nil {
		return bad()
	}
	*n = TokenCount(v)
	return nil
}

type UsageSource string

const (
	ProviderUsage UsageSource = "provider"
	UnknownUsage  UsageSource = "unknown"
)

type Usage struct {
	InputTokens       *TokenCount `json:"input_tokens"`
	OutputTokens      *TokenCount `json:"output_tokens"`
	TotalTokens       *TokenCount `json:"total_tokens"`
	CachedInputTokens *TokenCount `json:"cached_input_tokens"`
	CacheWriteTokens  *TokenCount `json:"cache_write_tokens"`
	ReasoningTokens   *TokenCount `json:"reasoning_tokens"`
	Source            UsageSource `json:"source"`
}

func (u Usage) Validate() error {
	known := 0
	for _, p := range []*TokenCount{u.InputTokens, u.OutputTokens, u.TotalTokens, u.CachedInputTokens, u.CacheWriteTokens, u.ReasoningTokens} {
		if p != nil {
			if p.Validate() != nil {
				return bad()
			}
			known++
		}
	}
	if u.Source == ProviderUsage && known > 0 || u.Source == UnknownUsage && known == 0 {
		return nil
	}
	return bad()
}
func (u Usage) Clone() Usage {
	u.InputTokens = copyPtr(u.InputTokens)
	u.OutputTokens = copyPtr(u.OutputTokens)
	u.TotalTokens = copyPtr(u.TotalTokens)
	u.CachedInputTokens = copyPtr(u.CachedInputTokens)
	u.CacheWriteTokens = copyPtr(u.CacheWriteTokens)
	u.ReasoningTokens = copyPtr(u.ReasoningTokens)
	return u
}

type Consumer struct {
	Kind        ConsumerKind    `json:"kind"`
	ProjectID   id.ProjectID    `json:"project_id"`
	Purpose     Purpose         `json:"purpose"`
	AgentID     *id.AgentID     `json:"agent_id,omitempty"`
	ExecutionID *id.ExecutionID `json:"execution_id,omitempty"`
	MeetingID   string          `json:"meeting_id,omitempty"`
	OperationID string          `json:"operation_id,omitempty"`
}

func (c Consumer) Validate() error {
	if c.ProjectID.Validate() != nil || !c.Purpose.Valid() || c.AgentID != nil && c.AgentID.Validate() != nil || c.ExecutionID != nil && c.ExecutionID.Validate() != nil || c.MeetingID != "" && !uuid(c.MeetingID) || c.OperationID != "" && !uuid(c.OperationID) {
		return bad()
	}
	switch c.Kind {
	case AgentConsumer:
		if c.AgentID == nil || c.ExecutionID == nil || c.OperationID != "" || c.Purpose != AgentGeneration && c.Purpose != AgentCompaction {
			return bad()
		}
	case MeetingConsumer:
		if c.AgentID != nil || c.ExecutionID != nil || c.MeetingID == "" || c.OperationID == "" || c.Purpose != MeetingSummaryInitial && c.Purpose != MeetingSummaryUpdate {
			return bad()
		}
	case KnowledgeConsumer:
		if c.AgentID != nil || c.ExecutionID != nil || c.MeetingID != "" || c.OperationID == "" || c.Purpose != KnowledgeEmbedding && c.Purpose != Rerank {
			return bad()
		}
	case MemoryConsumer:
		if c.AgentID == nil || c.ExecutionID != nil || c.MeetingID != "" || c.OperationID == "" || !one(string(c.Purpose), "memory_embedding", "memory_extraction", "memory_consolidation", "memory_reflection", "rerank") {
			return bad()
		}
	case ToolConsumer:
		if c.AgentID != nil || c.MeetingID != "" || c.OperationID == "" || c.Purpose != ApprovalAuto && c.Purpose != ImageGeneration {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (c Consumer) Clone() Consumer {
	c.AgentID = copyPtr(c.AgentID)
	c.ExecutionID = copyPtr(c.ExecutionID)
	return c
}
func (c Consumer) Equal(b Consumer) bool {
	x, e := json.Marshal(c)
	y, e2 := json.Marshal(b)
	return c.Validate() == nil && b.Validate() == nil && e == nil && e2 == nil && bytes.Equal(x, y)
}

type Capabilities struct {
	ToolCalls             bool        `json:"tool_calls"`
	ParallelToolCalls     bool        `json:"parallel_tool_calls"`
	Streaming             bool        `json:"streaming"`
	Reasoning             bool        `json:"reasoning"`
	InputModalities       []string    `json:"input_modalities"`
	OutputModalities      []string    `json:"output_modalities"`
	ReasoningEfforts      []string    `json:"reasoning_efforts"`
	StructuredOutputModes []string    `json:"structured_output_modes"`
	ContextLength         *TokenCount `json:"context_length,omitempty"`
	MaxOutput             *TokenCount `json:"max_output,omitempty"`
}

func (c Capabilities) Validate() error {
	if c.ParallelToolCalls && !c.ToolCalls || !c.Reasoning && len(c.ReasoningEfforts) > 0 || !unique(c.InputModalities, "text", "image", "file", "vector") || !unique(c.OutputModalities, "text", "image", "file", "vector") || !unique(c.StructuredOutputModes, "text", "json_schema") || !uniqueTokens(c.ReasoningEfforts, 32) {
		return bad()
	}
	for _, n := range []*TokenCount{c.ContextLength, c.MaxOutput} {
		if n != nil && *n <= 0 {
			return bad()
		}
	}
	if c.ContextLength != nil && c.MaxOutput != nil && *c.MaxOutput > *c.ContextLength {
		return bad()
	}
	return nil
}
func (c Capabilities) Clone() Capabilities {
	c.InputModalities = append([]string(nil), c.InputModalities...)
	c.OutputModalities = append([]string(nil), c.OutputModalities...)
	c.ReasoningEfforts = append([]string(nil), c.ReasoningEfforts...)
	c.StructuredOutputModes = append([]string(nil), c.StructuredOutputModes...)
	c.ContextLength = copyPtr(c.ContextLength)
	c.MaxOutput = copyPtr(c.MaxOutput)
	return c
}

type ModelIdentity struct {
	ProviderID      ProviderID `json:"provider_id"`
	ModelID         ModelID    `json:"model_id"`
	ProviderName    string     `json:"provider_name"`
	ModelName       string     `json:"model_name"`
	ProviderModelID string     `json:"provider_model_id"`
	Protocol        Protocol   `json:"protocol"`
	Profile         ProfileID  `json:"profile"`
	ModelType       ModelType  `json:"model_type"`
	AdapterRevision string     `json:"adapter_revision"`
}

func (m ModelIdentity) Validate() error {
	if m.ProviderID.Validate() != nil || m.ModelID.Validate() != nil || !name(m.ProviderName, 128) || !name(m.ModelName, 128) || !text(m.ProviderModelID, 256) || !m.Protocol.Supports(m.ModelType) || m.Profile != m.Protocol.Profile() || !safeToken(m.AdapterRevision, 128) {
		return bad()
	}
	return nil
}

type ConfigSnapshot struct {
	ID               SnapshotID        `json:"id"`
	Identity         ModelIdentity     `json:"identity"`
	Endpoint         string            `json:"endpoint"`
	Parameters       json.RawMessage   `json:"parameters"`
	RequestOverwrite json.RawMessage   `json:"request_overwrite"`
	HeaderOverwrite  map[string]string `json:"header_overwrite"`
	Capabilities     Capabilities      `json:"capabilities"`
	CredentialRef    *sc.CredentialRef `json:"credential_ref,omitempty"`
	SelectionVersion *f.Version        `json:"selection_version,omitempty"`
}

func (s ConfigSnapshot) Validate() error {
	if s.ID.Validate() != nil || s.Identity.Validate() != nil || !endpoint(s.Endpoint) || objectJSON(s.Parameters, 64<<10) != nil || overwrite(s.RequestOverwrite, s.HeaderOverwrite) != nil || s.Capabilities.Validate() != nil || s.CredentialRef != nil && s.CredentialRef.Validate() != nil || s.SelectionVersion != nil && s.SelectionVersion.Validate() != nil {
		return bad()
	}
	return nil
}
func (s ConfigSnapshot) Clone() ConfigSnapshot {
	s.Parameters = bytes.Clone(s.Parameters)
	s.RequestOverwrite = bytes.Clone(s.RequestOverwrite)
	s.HeaderOverwrite = copyMap(s.HeaderOverwrite)
	s.Capabilities = s.Capabilities.Clone()
	s.CredentialRef = copyPtr(s.CredentialRef)
	s.SelectionVersion = copyPtr(s.SelectionVersion)
	return s
}

type ResolvedModel struct {
	Snapshot        ConfigSnapshot          `json:"snapshot"`
	Consumer        Consumer                `json:"consumer"`
	LeaseOwner      sc.CredentialLeaseOwner `json:"lease_owner"`
	CredentialLease *sc.CredentialLease     `json:"credential_lease,omitempty"`
}

func (r ResolvedModel) Validate() error {
	if r.Snapshot.Validate() != nil || r.Consumer.Validate() != nil || !ownerMatches(r.LeaseOwner, r.Consumer, nil) || !r.Snapshot.Identity.Protocol.Supports(r.Consumer.Purpose.ModelType()) {
		return bad()
	}
	if (r.Snapshot.CredentialRef == nil) != (r.CredentialLease == nil) {
		return bad()
	}
	if r.CredentialLease != nil && (r.CredentialLease.LeaseID.Validate() != nil || !r.CredentialLease.CredentialRef.Equal(*r.Snapshot.CredentialRef)) {
		return bad()
	}
	return nil
}
func (r ResolvedModel) Clone() ResolvedModel {
	r.Snapshot = r.Snapshot.Clone()
	r.Consumer = r.Consumer.Clone()
	r.CredentialLease = copyPtr(r.CredentialLease)
	return r
}

type ErrorCategory string

func (c ErrorCategory) Valid() bool {
	return one(string(c), "invalid_request", "authentication", "permission", "model_not_found", "rate_limited", "context_too_large", "provider_unavailable", "timeout", "network", "cancelled", "content_filter", "unsupported_feature", "provider_error", "unknown")
}

type FinishReason string

func (c FinishReason) Valid() bool {
	return one(string(c), "stop", "tool_calls", "length", "content_filter", "cancelled", "error", "unknown")
}

type ModelError struct {
	Category          ErrorCategory `json:"category"`
	Code              string        `json:"code,omitempty"`
	ProviderRequestID string        `json:"provider_request_id,omitempty"`
	Retryable         bool          `json:"retryable"`
	Dispatched        bool          `json:"dispatched"`
	PartialOutput     bool          `json:"partial_output"`
}

func (e ModelError) Validate() error {
	if !e.Category.Valid() || e.Code != "" && !safeToken(e.Code, 128) || e.ProviderRequestID != "" && !safeToken(e.ProviderRequestID, 256) || e.PartialOutput && !e.Dispatched {
		return bad()
	}
	if e.Retryable && !one(string(e.Category), "rate_limited", "provider_unavailable", "timeout", "network", "provider_error") {
		return bad()
	}
	return nil
}
func (e ModelError) Error() string {
	if e.Validate() != nil {
		return "model_error"
	}
	return "model_" + string(e.Category)
}
func (e ModelError) SafeMessage() string { return e.Error() }
func bad() error                         { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func one(s string, values ...string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	r := make(map[string]string, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}
func uuid(s string) bool { x, e := f.ParseID[struct{}](s); return e == nil && x.String() == s }
func text(s string, n int) bool {
	return s != "" && len(s) <= n && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func name(s string, n int) bool { return text(s, n*4) && utf8.RuneCountInString(s) <= n }
func safeToken(s string, n int) bool {
	if s == "" || len(s) > n {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || one(string(c), "-", "_", ".", ":")) {
			return false
		}
	}
	return true
}
func unique(v []string, allowed ...string) bool {
	seen := map[string]bool{}
	for _, s := range v {
		if seen[s] || !one(s, allowed...) {
			return false
		}
		seen[s] = true
	}
	return true
}
func uniqueTokens(v []string, n int) bool {
	seen := map[string]bool{}
	for _, s := range v {
		if !safeToken(s, n) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func ownerMatches(o sc.CredentialLeaseOwner, c Consumer, call *CallID) bool {
	if o.Validate() != nil {
		return false
	}
	d := o.Details()
	switch d.Kind {
	case sc.ExecutionOwner:
		return c.ExecutionID != nil && d.ID == c.ExecutionID.String()
	case sc.ModelCallOwner:
		return call == nil || call.Validate() == nil && d.ID == call.String()
	}
	return false
}
func actorMatches(a id.Actor, c Consumer) bool {
	if a.Validate() != nil {
		return false
	}
	d := a.Details()
	if d.Kind == id.AgentRun {
		return c.AgentID != nil && c.ExecutionID != nil && d.ProjectID == c.ProjectID.String() && d.AgentID == c.AgentID.String() && d.ExecutionID == c.ExecutionID.String()
	}
	return d.Kind != id.Service || d.ProjectID == "" || d.ProjectID == c.ProjectID.String()
}
func binding(v any) (f.Digest, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", bad()
	}
	h := sha256.Sum256(b)
	return f.Digest("sha256:" + hex.EncodeToString(h[:])), nil
}

// Strict structural JSON checking does not assert a provider's schema support.
func objectJSON(raw []byte, limit int) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return bad()
	}
	if jsonBody(d, t, 1) != nil {
		return bad()
	}
	if _, e = d.Token(); e != io.EOF {
		return bad()
	}
	return nil
}
func jsonBody(d *json.Decoder, t json.Token, depth int) error {
	if depth > 32 {
		return bad()
	}
	switch x := t.(type) {
	case json.Delim:
		if x == '{' {
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return bad()
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return bad()
				}
				seen[s] = true
				v, e := d.Token()
				if e != nil || jsonBody(d, v, depth+1) != nil {
					return bad()
				}
			}
			v, e := d.Token()
			if e != nil || v != json.Delim('}') {
				return bad()
			}
		} else if x == '[' {
			for d.More() {
				v, e := d.Token()
				if e != nil || jsonBody(d, v, depth+1) != nil {
					return bad()
				}
			}
			v, e := d.Token()
			if e != nil || v != json.Delim(']') {
				return bad()
			}
		} else {
			return bad()
		}
	case json.Number:
		v, e := strconv.ParseFloat(string(x), 64)
		if e != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return bad()
		}
	}
	return nil
}
func label(w fmt.State, s string) { _, _ = io.WriteString(w, s) }

var _ slog.LogValuer = ModelError{}

func (Usage) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_usage")) }
func (Usage) LogValue() slog.Value       { return slog.StringValue("model_usage") }

func (Consumer) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_consumer")) }
func (Consumer) LogValue() slog.Value       { return slog.StringValue("model_consumer") }

func (Capabilities) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_capabilities")) }
func (Capabilities) LogValue() slog.Value       { return slog.StringValue("model_capabilities") }

func (ModelIdentity) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_model_identity")) }
func (ModelIdentity) LogValue() slog.Value       { return slog.StringValue("model_model_identity") }

func (ConfigSnapshot) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_config_snapshot")) }
func (ConfigSnapshot) LogValue() slog.Value       { return slog.StringValue("model_config_snapshot") }

func (ResolvedModel) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_resolved_model")) }
func (ResolvedModel) LogValue() slog.Value       { return slog.StringValue("model_resolved_model") }

func (ModelError) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_model_error")) }
func (ModelError) LogValue() slog.Value       { return slog.StringValue("model_model_error") }
