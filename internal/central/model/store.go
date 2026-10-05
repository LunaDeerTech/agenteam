package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func fault(code f.Code) *f.Fault  { return f.NewFault(code, f.NotStarted) }
func unavailable(err error) error { return fault(f.DependencyUnavailable).WithCause(err) }
func portError(err error) error {
	if err == nil {
		return nil
	}
	var ff *f.Fault
	if errors.As(err, &ff) {
		return ff
	}
	return unavailable(err)
}
func commitError(r f.CommitResult) error {
	switch r.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		return r.Fault()
	default:
		return &UnknownCommandError{cause: r.Cause(), attempt: r.AttemptID()}
	}
}

// UnknownCommandError preserves the original physical attempt/cause without
// exposing command keys in formatting, JSON or ordinary logs.
type UnknownCommandError struct {
	cause   f.TransactionCause
	attempt f.ID[f.TransactionAttempt]
}

func (e *UnknownCommandError) Error() string                         { return string(f.CommitUnknown) }
func (e *UnknownCommandError) Unwrap() error                         { return f.NewFault(f.CommitUnknown, f.Unknown) }
func (e *UnknownCommandError) Cause() f.TransactionCause             { return e.cause }
func (e *UnknownCommandError) AttemptID() f.ID[f.TransactionAttempt] { return e.attempt }
func (e *UnknownCommandError) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, string(f.CommitUnknown))
}
func (e *UnknownCommandError) MarshalJSON() ([]byte, error) {
	return []byte(`{"code":"COMMIT_UNKNOWN","commit_state":"unknown"}`), nil
}
func (e *UnknownCommandError) LogValue() slog.Value { return slog.StringValue(string(f.CommitUnknown)) }
func newID() (string, error) {
	v, e := f.NewID[struct{}]()
	if e != nil {
		return "", unavailable(e)
	}
	return v.String(), nil
}
func readCause(kind string) (f.TransactionCause, error) {
	v, e := newID()
	if e != nil {
		return f.TransactionCause{}, e
	}
	return f.NewRecoveryCause("model."+kind, v, "")
}
func hash(b []byte) f.Digest { return oc.DigestBytes(b) }
func encoded(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, unavailable(e)
	}
	return b, nil
}
func userLock(user string) f.LockRequest {
	k, _ := f.UserLock(user)
	return f.LockRequest{Key: k, Mode: f.Shared}
}
func systemLock(name string, mode f.LockMode) f.LockRequest {
	k, _ := f.SystemConfigLock(name)
	return f.LockRequest{Key: k, Mode: mode}
}
func aggregateLock(kind f.AggregateKind, id string, mode f.LockMode) f.LockRequest {
	k, _ := f.AggregateLock(kind, id)
	return f.LockRequest{Key: k, Mode: mode}
}
func commandLock(c f.CommandIdentity) f.LockRequest {
	k, _ := f.CommandLock(c)
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func recordLock(kind f.RecordKind, id string) f.LockRequest {
	k, _ := f.RecordLock(kind, id)
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func dbNow(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var t time.Time
	if e := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t); e != nil {
		return f.Instant{}, unavailable(e)
	}
	v, e := f.NewInstant(t)
	return v, portError(e)
}
func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type providerInput struct {
	Name         string
	Protocol     mc.Protocol
	BaseURL      string
	Enabled      bool
	CredentialID string
	Options      json.RawMessage
}
type providerRecord struct {
	Project              string `json:",omitempty"`
	ID                   string
	Input                providerInput
	Version              f.Version
	CreatedAt, UpdatedAt f.Instant
}

func providerFromInput(v mc.ProviderInput) providerInput {
	r := providerInput{Name: v.Name, Protocol: v.Protocol, BaseURL: v.BaseURL, Enabled: v.Enabled, Options: normalizedObject(v.Options)}
	if v.CredentialRef != nil {
		r.CredentialID = v.CredentialRef.Details().ID.String()
	}
	return r
}
func (r providerRecord) view() (mc.ProviderView, error) {
	i := mc.ProviderInput{Name: r.Input.Name, Protocol: r.Input.Protocol, BaseURL: r.Input.BaseURL, Enabled: r.Input.Enabled, Options: append(json.RawMessage(nil), r.Input.Options...)}
	if r.Input.CredentialID != "" {
		cid, e := f.ParseID[sc.Credential](r.Input.CredentialID)
		if e != nil {
			return mc.ProviderView{}, unavailable(e)
		}
		ref, e := sc.NewCredentialRef(cid, configurationScope(r.Project))
		if e != nil {
			return mc.ProviderView{}, unavailable(e)
		}
		i.CredentialRef = &ref
	}
	pid, e := f.ParseID[mc.Provider](r.ID)
	if e != nil {
		return mc.ProviderView{}, unavailable(e)
	}
	v := mc.ProviderView{ID: pid, Scope: configurationScope(r.Project), Input: i, Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if e = v.Validate(); e != nil {
		return mc.ProviderView{}, unavailable(e)
	}
	return v, nil
}

type scanner interface{ Scan(...any) error }

const providerColumns = `id::text,name,protocol,base_url,enabled,credential_id::text,provider_options,version,created_at,updated_at`

func scanProvider(row scanner) (*providerRecord, error) {
	return scanProviderScope(row, id.SystemScope())
}
func scanProviderScope(row scanner, scope id.Scope) (*providerRecord, error) {
	r := providerRecord{Project: scope.Details().ProjectID}
	var cid *string
	var created, updated time.Time
	err := row.Scan(&r.ID, &r.Input.Name, &r.Input.Protocol, &r.Input.BaseURL, &r.Input.Enabled, &cid, &r.Input.Options, &r.Version, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if cid != nil {
		r.Input.CredentialID = *cid
	}
	r.CreatedAt, _ = f.NewInstant(created)
	r.UpdatedAt, _ = f.NewInstant(updated)
	if _, err = r.view(); err != nil {
		return nil, err
	}
	return &r, nil
}
func loadProvider(ctx context.Context, x postgres.SQLExecutor, id string) (*providerRecord, error) {
	return scanProvider(x.QueryRow(ctx, `SELECT `+providerColumns+` FROM agenteam_model.providers WHERE scope='system' AND id=$1`, id))
}

type modelRecord struct {
	Project              string `json:",omitempty"`
	ID, ProviderID       string
	Input                mc.ModelInput
	Version              f.Version
	CreatedAt, UpdatedAt f.Instant
}

func (r modelRecord) view() (mc.ModelView, error) {
	mid, e := f.ParseID[mc.Model](r.ID)
	if e != nil {
		return mc.ModelView{}, unavailable(e)
	}
	pid, e := f.ParseID[mc.Provider](r.ProviderID)
	if e != nil {
		return mc.ModelView{}, unavailable(e)
	}
	v := mc.ModelView{ID: mid, ProviderID: pid, Scope: configurationScope(r.Project), Input: r.Input.Clone(), Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if e = v.Validate(); e != nil {
		return mc.ModelView{}, unavailable(e)
	}
	return v, nil
}

const modelColumns = `m.id::text,m.provider_id::text,m.name,m.provider_model_id,m.type,m.enabled,m.parameters,m.request_overwrite,m.header_overwrite,m.capabilities,m.version,m.created_at,m.updated_at`

func scanModel(row scanner) (*modelRecord, error) {
	return scanModelScope(row, id.SystemScope())
}
func scanModelScope(row scanner, scope id.Scope) (*modelRecord, error) {
	r := modelRecord{Project: scope.Details().ProjectID}
	var headers, caps []byte
	var created, updated time.Time
	e := row.Scan(&r.ID, &r.ProviderID, &r.Input.Name, &r.Input.ProviderModelID, &r.Input.Type, &r.Input.Enabled, &r.Input.Parameters, &r.Input.RequestOverwrite, &headers, &caps, &r.Version, &created, &updated)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	if json.Unmarshal(headers, &r.Input.HeaderOverwrite) != nil || json.Unmarshal(caps, &r.Input.Capabilities) != nil {
		return nil, unavailable(nil)
	}
	r.CreatedAt, _ = f.NewInstant(created)
	r.UpdatedAt, _ = f.NewInstant(updated)
	if _, e = r.view(); e != nil {
		return nil, e
	}
	return &r, nil
}
func loadModel(ctx context.Context, x postgres.SQLExecutor, id string) (*modelRecord, error) {
	return scanModel(x.QueryRow(ctx, `SELECT `+modelColumns+` FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='system' AND m.id=$1`, id))
}

type selectionRecord struct {
	ID                                 string
	Version                            f.Version
	Configured                         bool
	Embedding, Memory, Reranker, Image string
}

func loadSelection(ctx context.Context, x postgres.SQLExecutor) (*selectionRecord, error) {
	var r selectionRecord
	var ids [4]*string
	e := x.QueryRow(ctx, `SELECT id::text,version,configured,embedding_id::text,memory_id::text,reranker_id::text,image_id::text FROM agenteam_model.platform_selection WHERE singleton`).Scan(&r.ID, &r.Version, &r.Configured, &ids[0], &ids[1], &ids[2], &ids[3])
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, fault(f.InvalidState)
	}
	if e != nil {
		return nil, unavailable(e)
	}
	dest := []*string{&r.Embedding, &r.Memory, &r.Reranker, &r.Image}
	for i, v := range ids {
		if v != nil {
			*dest[i] = *v
		}
	}
	return &r, nil
}

// Scope is inherited from the exact Provider row; no cross-scope bare-ID load.
func configurationScope(project string) id.Scope {
	if project == "" {
		return id.SystemScope()
	}
	key, err := f.ParseID[id.Project](project)
	if err != nil {
		return id.Scope{}
	}
	scope, _ := id.InProject(key)
	return scope
}
func projectLock(project string) f.LockRequest {
	key, _ := f.ProjectLock(project)
	return f.LockRequest{Key: key, Mode: f.Shared}
}
func scopeLocks(actor id.Actor, scope id.Scope) []f.LockRequest {
	locks := []f.LockRequest{userLock(actor.Details().UserID)}
	if scope.Details().Kind == id.ProjectScope {
		locks = append(locks, projectLock(scope.Details().ProjectID))
	}
	return locks
}
func loadProviderScope(ctx context.Context, x postgres.SQLExecutor, key string, scope id.Scope) (*providerRecord, error) {
	if scope.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if scope.Details().Kind == id.System {
		return loadProvider(ctx, x, key)
	}
	return scanProviderScope(x.QueryRow(ctx, `SELECT `+providerColumns+` FROM agenteam_model.providers WHERE scope='project' AND project_id=$2 AND id=$1`, key, scope.Details().ProjectID), scope)
}
func loadModelScope(ctx context.Context, x postgres.SQLExecutor, key string, scope id.Scope) (*modelRecord, error) {
	if scope.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if scope.Details().Kind == id.System {
		return loadModel(ctx, x, key)
	}
	return scanModelScope(x.QueryRow(ctx, `SELECT `+modelColumns+` FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='project' AND p.project_id=$2 AND m.id=$1`, key, scope.Details().ProjectID), scope)
}
