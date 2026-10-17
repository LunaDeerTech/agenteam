package model

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/managementwire"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type ManagementProcessAuthority interface {
	CurrentManagementProcess() (string, error)
	ConfirmManagementProcessStopped(context.Context, string) error
}
type ManagementSecrets interface {
	sc.UsageOperations
	Metadata(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error)
	ValidateManagementCredentialInTx(context.Context, f.Tx, sc.UsageRequest, sc.UsageDependencies, f.Version) error
	ReadCredentialForRequest(context.Context, id.Actor, sc.LeaseID) (sc.SecretMaterial, error)
}
type Management struct {
	service   *Service
	authority *ManagementSecretAuthority
	secrets   ManagementSecrets
	process   ManagementProcessAuthority
	transport managementwire.Transport
	slots     chan struct{}
	mu        sync.Mutex
	active    map[string]bool
}

func NewManagement(s *Service, a *ManagementSecretAuthority, secrets ManagementSecrets, p ManagementProcessAuthority, t managementwire.Transport) (*Management, error) {
	if s.state() == nil || a == nil || a.authority != s.state().authority || nilPort(secrets) || nilPort(p) || t.Policy == nil || t.Trust.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &Management{service: s, authority: a, secrets: secrets, process: p, transport: t, slots: make(chan struct{}, 4), active: map[string]bool{}}, nil
}

type ManagementItem struct {
	ProviderModelID string `json:"provider_model_id"`
}
type ManagementResult struct {
	OperationID     string            `json:"operation_id"`
	ProviderID      string            `json:"provider_id"`
	ProviderVersion f.Version         `json:"provider_version"`
	ModelID         string            `json:"model_id,omitempty"`
	ModelVersion    f.Version         `json:"model_version,omitempty"`
	Outcome         string            `json:"outcome"`
	Reason          *string           `json:"reason"`
	Items           *[]ManagementItem `json:"items,omitempty"`
	AnswerText      *string           `json:"answer_text"`
	AnswerTruncated bool              `json:"answer_truncated"`
	DurationMS      int64             `json:"duration_ms"`
	CreatedAt       f.Instant         `json:"created_at"`
}
type managementOperation struct {
	ID, Command, Semantic, User, Session, Process, Action, Provider, Model, Credential, Lease string
	ProviderVersion, ModelVersion, CredentialVersion                                          f.Version
	Result                                                                                    json.RawMessage
	CreatedAt                                                                                 f.Instant
	// Only the live operation needs its immutable model type to bound timing.
	// Recovery emits duration zero; successful replays use the persisted receipt.
	modelType      mc.ModelType
	hasAttachments bool
}

func managementLock(key string, mode f.LockMode) f.LockRequest {
	k, _ := f.RecordLock(f.ReferenceRecordLock, "model-management:"+key)
	return f.LockRequest{Key: k, Mode: mode}
}
func readManagement(ctx context.Context, x postgres.SQLExecutor, column, key string) (*managementOperation, error) {
	// column is exclusively selected by this package, never supplied by HTTP.
	if column != "id" && column != "command_digest" {
		return nil, fault(f.InvalidArgument)
	}
	var o managementOperation
	var pv, mv, cv int64
	var at time.Time
	e := x.QueryRow(ctx, `SELECT id::text,command_digest,semantic_digest,user_id::text,session_id::text,process_id::text,action,provider_id::text,provider_version,coalesce(model_id::text,''),coalesce(model_version,0),coalesce(credential_id::text,''),coalesce(credential_version,0),coalesce(lease_id::text,''),result,created_at FROM agenteam_model.management_operations WHERE `+column+`=$1`, key).Scan(&o.ID, &o.Command, &o.Semantic, &o.User, &o.Session, &o.Process, &o.Action, &o.Provider, &pv, &o.Model, &mv, &o.Credential, &cv, &o.Lease, &o.Result, &at)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	o.ProviderVersion = f.Version(pv)
	o.ModelVersion = f.Version(mv)
	o.CredentialVersion = f.Version(cv)
	o.CreatedAt, _ = f.NewInstant(at)
	return &o, nil
}
func (o *managementOperation) result(reason string, duration time.Duration) ManagementResult {
	budget := managementwire.Timeout(managementwire.Request{Action: o.Action, Protocol: string(mc.OpenAICompatible.EffectiveProtocol(o.modelType))})
	if o.hasAttachments {
		budget = 120 * time.Second
	}
	r := ManagementResult{OperationID: o.ID, ProviderID: o.Provider, ProviderVersion: o.ProviderVersion, ModelID: o.Model, ModelVersion: o.ModelVersion, Outcome: "failed", Reason: &reason, DurationMS: min(max(duration.Milliseconds(), 0), budget.Milliseconds()), CreatedAt: o.CreatedAt}
	if reason == "" {
		r.Outcome = "succeeded"
		r.Reason = nil
	}
	if reason == "interrupted" {
		r.Outcome = "unknown"
	}
	if o.Action == "discovery" {
		items := []ManagementItem{}
		r.Items = &items
	}
	return r
}

func managementRequestContext(parent context.Context, started time.Time, modelType mc.ModelType, attachments ...bool) (context.Context, context.CancelFunc) {
	budget := managementwire.Timeout(managementwire.Request{Action: "test", Protocol: string(mc.OpenAICompatible.EffectiveProtocol(modelType))})
	if len(attachments) > 0 && attachments[0] {
		budget = 120 * time.Second
	}
	return context.WithDeadline(parent, started.Add(budget))
}
func managementUnknown() error {
	v := f.NewFault(f.CommitUnknown, f.Unknown)
	v.RetryHint = "replay"
	return v
}

func managementCommitError(r f.CommitResult) error {
	if r.State() == f.Unknown {
		return managementUnknown()
	}
	return commitError(r)
}

// Discovery has no model answer; only the model-test DTO carries these keys.
func (r ManagementResult) MarshalJSON() ([]byte, error) {
	type plain ManagementResult
	raw, err := json.Marshal(plain(r))
	if err != nil || r.Items == nil {
		return raw, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "answer_text")
	delete(fields, "answer_truncated")
	return json.Marshal(fields)
}
