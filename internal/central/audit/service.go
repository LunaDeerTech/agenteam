// Package audit implements append-only events, scoped authorized reads and the
// explicit Project lifecycle cleanup participant. No HTTP routes are installed.
package audit

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	contract "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type Store interface {
	postgres.SQLExecutor
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	Acquire(context.Context, foundation.Tx, foundation.LockKey, foundation.LockMode) error
}
type AccountAuthority = contract.AccountAuthority

type Authorizations struct {
	Runners  contract.RunnerAuthority
	Models   contract.ModelAuthority
	Accounts contract.AccountAuthority
	Sessions identity.SessionAuthority
	System   identity.SystemAuthority
	Projects contract.ProjectAuthority
}
type Service struct {
	store Store
	keys  cursor.Keyring
	auth  Authorizations
}

func New(store Store, keys cursor.Keyring, auth Authorizations) (*Service, error) {
	if nilPort(store) || keys.Validate() != nil {
		return nil, invalid("audit_configuration")
	}
	return &Service{store, keys, auth}, nil
}

// CheckStorage performs the bounded startup read without exposing records or
// implying that any of the still-unbound identity authorities is available.
func (s *Service) CheckStorage(ctx context.Context) error {
	var exists bool
	if err := s.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_audit.audit_records LIMIT 1)`).Scan(&exists); err != nil {
		return unavailable(err)
	}
	return nil
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	return (r.Kind() == reflect.Pointer || r.Kind() == reflect.Interface || r.Kind() == reflect.Func || r.Kind() == reflect.Map || r.Kind() == reflect.Slice) && r.IsNil()
}
func scopeValid(s identity.Scope) bool {
	return s.Validate() == nil && (s.Details().Kind == identity.System || s.Details().Kind == identity.ProjectScope)
}
func projectID(scope identity.Scope) identity.ProjectID {
	p, _ := foundation.ParseID[identity.Project](scope.Details().ProjectID)
	return p
}
func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func scopeKey(s identity.Scope) string {
	if s.Details().Kind == identity.System {
		return "system"
	}
	return s.Details().ProjectID
}

func actorSummary(actor identity.Actor) contract.ActorSummary {
	a := actor.Details()
	s := contract.ActorSummary{Kind: a.Kind}

	switch a.Kind {
	case identity.Human:
		s.ID = a.UserID
	case identity.AgentRun:
		s.ID = a.AgentID
		s.ProjectID = a.ProjectID
		s.ExecutionID = a.ExecutionID
	case identity.Service:
		s.Service = a.ServiceName
		s.CauseRef = a.CauseRef
		s.ProjectID = a.ProjectID
	}
	return s
}

// SemanticDigest excludes session/HTTP trace, while actual Attempt request IDs
// remain semantic. Input Entry cannot contain server-generated receipt fields.
func SemanticDigest(entry contract.Entry) (foundation.Digest, error) {
	if entry.Validate() != nil {
		return "", invalid("entry")
	}
	f := entry.Fields()
	associations := f.Associations
	associations.HTTPTraceID = ""
	b, err := json.Marshal(struct {
		Format       int                   `json:"format"`
		Scope        identity.Scope        `json:"scope"`
		Actor        contract.ActorSummary `json:"actor"`
		Action       contract.Action       `json:"action"`
		Outcome      contract.Outcome      `json:"outcome"`
		Resource     contract.Resource     `json:"resource"`
		Metadata     contract.Metadata     `json:"metadata"`
		Associations contract.Associations `json:"associations"`
	}{1, f.Scope, actorSummary(f.Actor), f.Action, f.Outcome, f.Resource, f.Metadata, associations})
	if err != nil {
		return "", invalid("entry")
	}
	d, err := cursor.Digest(b)
	if err != nil {
		return "", invalid("entry")
	}
	return d, nil
}
func CommandAppendKey(producer contract.Producer, identity foundation.CommandIdentity, ordinal int64) (contract.AppendKey, error) {
	if identity.Validate() != nil {
		return contract.AppendKey{}, invalid("append_key")
	}
	d, e := cursor.Digest([]byte(identity.Canonical()))
	if e != nil {
		return contract.AppendKey{}, invalid("append_key")
	}
	return contract.NewAppendKey(producer, string(d), ordinal)
}

func (s *Service) authorizeHuman(ctx context.Context, tx foundation.Tx, actor identity.Actor, scope identity.Scope, intent identity.AccessIntent) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || !scopeValid(scope) {
		return failure(foundation.Forbidden, "actor_rejected", nil)
	}
	if nilPort(s.auth.Sessions) {
		return failure(foundation.DependencyUnbound, "session_unbound", nil)
	}
	if err := s.auth.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
		return portError(err)
	}
	var grant identity.AccessGrant
	var err error
	if scope.Details().Kind == identity.System {
		if nilPort(s.auth.System) {
			return failure(foundation.DependencyUnbound, "system_authority_unbound", nil)
		}
		grant, err = s.auth.System.AuthorizeSystem(ctx, tx, actor, intent)
	} else {
		if nilPort(s.auth.Projects) {
			return failure(foundation.DependencyUnbound, "project_authority_unbound", nil)
		}
		grant, err = s.auth.Projects.AuthorizeProject(ctx, tx, actor, projectID(scope), intent)
	}
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, scope, intent) {
		return failure(foundation.DependencyUnavailable, "authorization_grant_invalid", nil)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}
func serviceOwns(actor identity.Actor, scope identity.Scope, key contract.AppendKey) bool {
	if actor.Validate() != nil || key.Validate() != nil || !scopeValid(scope) {
		return false
	}
	a, k := actor.Details(), key.Details()
	if a.Kind != identity.Service || a.ProjectID != scope.Details().ProjectID || a.CauseRef != k.CauseRef {
		return false
	}
	switch a.ServiceName {
	case identity.SecretService:
		return k.Producer == contract.SecretProducer
	case identity.SecretMaintenance:
		return scope.Details().Kind == identity.System && k.Producer == contract.MasterProducer
	case identity.OutboundService:
		return k.Producer == contract.PolicyProducer && scope.Details().Kind == identity.System || k.Producer == contract.AccessProducer
	case identity.ObjectService, identity.ObjectMaintenance:
		return k.Producer == contract.ObjectProducer
	}
	return false
}
func (s *Service) authorizeAppend(ctx context.Context, tx foundation.Tx, entry contract.Entry, key contract.AppendKey) error {
	f := entry.Fields()
	a := f.Actor.Details()
	if key.Details().Producer != contract.ProducerFor(f.Action) {
		return invalid("producer_action_mismatch")
	}
	if contract.AccountAction(f.Action) || (a.Kind == identity.Service && (a.ServiceName == identity.AccountAuth || a.ServiceName == identity.AccountMaintenance) && (f.Action == contract.SecretCreate || f.Action == contract.SecretDelete)) {
		if nilPort(s.auth.Accounts) {
			return failure(foundation.DependencyUnbound, "account_authority_unbound", nil)
		}
		return portError(s.auth.Accounts.CheckAppendInTx(ctx, tx, entry, key))
	}
	if contract.ProjectAction(f.Action) {
		if nilPort(s.auth.Projects) {
			return failure(foundation.DependencyUnbound, "project_gate_unbound", nil)
		}
		return portError(s.auth.Projects.CheckAppendInTx(ctx, tx, entry, key))
	}
	if contract.RunnerAction(f.Action) {
		if nilPort(s.auth.Runners) {
			return failure(foundation.DependencyUnbound, "runner_authority_unbound", nil)
		}
		return portError(s.auth.Runners.CheckAppendInTx(ctx, tx, entry, key))
	}
	if contract.ModelAction(f.Action) {
		if nilPort(s.auth.Models) {
			return failure(foundation.DependencyUnbound, "model_authority_unbound", nil)
		}
		return portError(s.auth.Models.CheckAppendInTx(ctx, tx, entry, key))
	}
	switch a.Kind {
	case identity.Human:
		intent := identity.Mutate
		if f.Action == contract.ArtifactList || f.Action == contract.ArtifactRead || f.Action == contract.ArtifactDownload {
			intent = identity.Read
		}
		if err := s.authorizeHuman(ctx, tx, f.Actor, f.Scope, intent); err != nil {
			return err
		}
	case identity.AgentRun:
		if f.Action != contract.SecretResolve && f.Action != contract.AccessDeny && f.Action != contract.ArtifactCreate && f.Action != contract.ArtifactList && f.Action != contract.ArtifactRead && f.Action != contract.ArtifactDownload {
			return failure(foundation.Forbidden, "agent_action_rejected", nil)
		}
	case identity.Service:
		if !serviceOwns(f.Actor, f.Scope, key) {
			return failure(foundation.Forbidden, "service_cause_rejected", nil)
		}
	default:
		return failure(foundation.Forbidden, "actor_rejected", nil)
	}
	if f.Scope.Details().Kind == identity.ProjectScope {
		if nilPort(s.auth.Projects) {
			return failure(foundation.DependencyUnbound, "project_gate_unbound", nil)
		}
		if err := s.auth.Projects.CheckAppendInTx(ctx, tx, entry, key); err != nil {
			return portError(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *Service) AppendInTx(ctx context.Context, tx foundation.Tx, entry contract.Entry, key contract.AppendKey) (contract.AppendReceipt, error) {
	if entry.Validate() != nil || key.Validate() != nil {
		return contract.AppendReceipt{}, invalid("append_input")
	}
	e, err := s.store.InTx(tx)
	if err != nil {
		return contract.AppendReceipt{}, unavailable(err)
	}
	if err = s.authorizeAppend(ctx, tx, entry, key); err != nil {
		return contract.AppendReceipt{}, err
	}
	digest, err := SemanticDigest(entry)
	if err != nil {
		return contract.AppendReceipt{}, err
	}
	id, err := foundation.NewID[contract.Record]()
	if err != nil {
		return contract.AppendReceipt{}, unavailable(err)
	}
	f, k := entry.Fields(), key.Details()
	a, r, assoc := f.Actor.Details(), f.Resource.Details(), f.Associations
	var rawID string
	var created time.Time
	err = e.QueryRow(ctx, `INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,user_id,session_id,actor_project_id,agent_id,actor_execution_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal,tool_id,execution_id,tool_call_id,operation_id,request_id,approval_id,runner_id,correlation_id,http_trace_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)
 ON CONFLICT(scope,scope_key,producer,cause_ref,ordinal) DO NOTHING RETURNING id::text,created_at`, id.String(), string(f.Scope.Details().Kind), null(f.Scope.Details().ProjectID), string(a.Kind), null(a.UserID), null(a.SessionID), null(a.ProjectID), null(a.AgentID), null(a.ExecutionID), null(string(a.ServiceName)), null(a.CauseRef), string(f.Action), string(f.Outcome), string(r.Kind), null(r.ID), string(f.Metadata.JSON()), string(digest), string(k.Producer), k.CauseRef, k.Ordinal, null(assoc.ToolID), null(assoc.ExecutionID), null(assoc.ToolCallID), null(assoc.OperationID), null(assoc.RequestID), null(assoc.ApprovalID), null(assoc.RunnerID), null(assoc.CorrelationID), null(assoc.HTTPTraceID)).Scan(&rawID, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		var saved string
		err = e.QueryRow(ctx, `SELECT id::text,created_at,semantic_digest FROM agenteam_audit.audit_records WHERE scope=$1 AND scope_key=$2 AND producer=$3 AND cause_ref=$4 AND ordinal=$5`, string(f.Scope.Details().Kind), scopeKey(f.Scope), string(k.Producer), k.CauseRef, k.Ordinal).Scan(&rawID, &created, &saved)
		if err == nil && subtle.ConstantTimeCompare([]byte(saved), []byte(digest)) != 1 {
			return contract.AppendReceipt{}, failure(foundation.IdempotencyKeyReused, "append_key_reused", nil)
		}
	}
	if err != nil {
		return contract.AppendReceipt{}, unavailable(err)
	}
	return receipt(rawID, created)
}
func receipt(id string, created time.Time) (contract.AppendReceipt, error) {
	parsed, e := foundation.ParseID[contract.Record](id)
	at, te := foundation.NewInstant(created)
	if e != nil || te != nil {
		return contract.AppendReceipt{}, unavailable(nil)
	}
	return contract.AppendReceipt{AuditID: parsed, CreatedAt: at}, nil
}
func (s *Service) LookupAppend(ctx context.Context, actor identity.Actor, scope identity.Scope, key contract.AppendKey, digest foundation.Digest) (contract.AppendLookup, error) {
	if !scopeValid(scope) || actor.Validate() != nil || key.Validate() != nil || digest.Validate() != nil {
		return contract.AppendLookup{}, invalid("lookup_input")
	}
	if actor.Details().Kind == identity.Service {
		account := actor.Details().ServiceName == identity.AccountBootstrap || actor.Details().ServiceName == identity.AccountAuth || actor.Details().ServiceName == identity.AccountMaintenance || actor.Details().ServiceName == identity.AccountMail
		if account {
			if scope.Details().Kind != identity.System || nilPort(s.auth.Accounts) {
				return contract.AppendLookup{}, failure(foundation.DependencyUnbound, "account_authority_unbound", nil)
			}
			if err := s.auth.Accounts.CheckServiceLookup(ctx, actor, key); err != nil {
				return contract.AppendLookup{}, portError(err)
			}
		} else if !serviceOwns(actor, scope, key) {
			return contract.AppendLookup{}, failure(foundation.Forbidden, "service_cause_rejected", nil)
		}
		if scope.Details().Kind == identity.ProjectScope {
			if nilPort(s.auth.Projects) {
				return contract.AppendLookup{}, failure(foundation.DependencyUnbound, "project_gate_unbound", nil)
			}
			if err := s.auth.Projects.CheckServiceLookup(ctx, actor, scope, key); err != nil {
				return contract.AppendLookup{}, portError(err)
			}
		}
	} else if err := s.authorizeHuman(ctx, foundation.Tx{}, actor, scope, identity.Read); err != nil {
		return contract.AppendLookup{}, err
	}
	k := key.Details()
	var id, saved string
	var at time.Time
	err := s.store.QueryRow(ctx, `SELECT id::text,created_at,semantic_digest FROM agenteam_audit.audit_records WHERE scope=$1 AND scope_key=$2 AND producer=$3 AND cause_ref=$4 AND ordinal=$5`, string(scope.Details().Kind), scopeKey(scope), string(k.Producer), k.CauseRef, k.Ordinal).Scan(&id, &at, &saved)
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.AppendLookup{State: contract.NotObserved}, nil
	}
	if err != nil {
		return contract.AppendLookup{}, unavailable(err)
	}
	if subtle.ConstantTimeCompare([]byte(saved), []byte(digest)) != 1 {
		return contract.AppendLookup{}, failure(foundation.IdempotencyKeyReused, "append_key_reused", nil)
	}
	r, err := receipt(id, at)
	if err != nil {
		return contract.AppendLookup{}, err
	}
	return contract.AppendLookup{State: contract.Committed, Receipt: &r}, nil
}
