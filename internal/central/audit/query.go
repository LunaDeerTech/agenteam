package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Explicit fields define canonical-v1 filter semantics. Empty optional enums/IDs
// and nil time bounds mean no restriction; page size is deliberately absent.
func filterDigest(f contract.Filter) (foundation.Digest, error) {
	b, err := json.Marshal(struct {
		From         *foundation.Instant   `json:"from"`
		To           *foundation.Instant   `json:"to"`
		ActorKind    identity.ActorKind    `json:"actor_kind"`
		ActorID      string                `json:"actor_id"`
		Action       contract.Action       `json:"action"`
		Outcome      contract.Outcome      `json:"outcome"`
		ResourceKind contract.ResourceKind `json:"resource_kind"`
		ResourceID   string                `json:"resource_id"`
		ToolID       string                `json:"tool_id"`
		ExecutionID  string                `json:"execution_id"`
		OperationID  string                `json:"operation_id"`
		ApprovalID   string                `json:"approval_id"`
		RunnerID     string                `json:"runner_id"`
		AgentID      string                `json:"agent_id"`
	}{f.From, f.To, f.ActorKind, f.ActorID, f.Action, f.Outcome, f.ResourceKind, f.ResourceID, f.ToolID, f.ExecutionID, f.OperationID, f.ApprovalID, f.RunnerID, f.AgentID})
	if err != nil {
		return "", invalid("filter")
	}
	return cursor.Digest(b)
}

const recordColumns = `id::text,created_at,scope,coalesce(project_id::text,''),actor_kind,coalesce(user_id::text,''),coalesce(session_id::text,''),coalesce(actor_project_id::text,''),coalesce(agent_id::text,''),coalesce(actor_execution_id::text,''),coalesce(service_name,''),coalesce(service_cause,''),action,outcome,resource_kind,coalesce(resource_id::text,''),metadata::text,coalesce(tool_id::text,''),coalesce(execution_id::text,''),coalesce(tool_call_id::text,''),coalesce(operation_id::text,''),coalesce(request_id::text,''),coalesce(approval_id::text,''),coalesce(runner_id::text,''),coalesce(correlation_id::text,''),coalesce(http_trace_id::text,'')`

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (contract.SafeRecord, error) {
	var id, scope, project, kind, user, session, actorProject, agent, execution, service, cause, action, outcome, resourceKind, resourceID, metadata string
	var at time.Time
	var assoc contract.Associations
	if err := row.Scan(&id, &at, &scope, &project, &kind, &user, &session, &actorProject, &agent, &execution, &service, &cause, &action, &outcome, &resourceKind, &resourceID, &metadata, &assoc.ToolID, &assoc.ExecutionID, &assoc.ToolCallID, &assoc.OperationID, &assoc.RequestID, &assoc.ApprovalID, &assoc.RunnerID, &assoc.CorrelationID, &assoc.HTTPTraceID); err != nil {
		return contract.SafeRecord{}, err
	}
	s := identity.SystemScope()
	if scope == string(identity.ProjectScope) {
		p, e := foundation.ParseID[identity.Project](project)
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
		s, e = identity.InProject(p)
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
	} else if scope != string(identity.System) || project != "" {
		return contract.SafeRecord{}, unavailable(nil)
	}
	var actor identity.Actor
	var err error
	switch identity.ActorKind(kind) {
	case identity.Human:
		u, e := foundation.ParseID[identity.User](user)
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
		ss, e := foundation.ParseID[identity.Session](session)
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
		actor, err = identity.NewHuman(u, ss)
	case identity.AgentRun:
		p, e := foundation.ParseID[identity.Project](actorProject)
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
		a, e := foundation.ParseID[identity.Agent](agent)
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
		x, e := foundation.ParseID[identity.Execution](execution)
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
		actor, err = identity.NewAgentRun(p, a, x)
	case identity.Service:
		registration, e := identity.RegisterService(identity.ServiceName(service))
		if e != nil {
			return contract.SafeRecord{}, unavailable(e)
		}
		actor, err = registration.Actor(cause, s)
	default:
		return contract.SafeRecord{}, unavailable(nil)
	}
	if err != nil {
		return contract.SafeRecord{}, unavailable(err)
	}
	r, err := contract.NewResource(contract.ResourceKind(resourceKind), resourceID)
	if err != nil {
		return contract.SafeRecord{}, unavailable(err)
	}
	m, err := contract.DecodeMetadata(contract.Action(action), []byte(metadata))
	if err != nil {
		return contract.SafeRecord{}, unavailable(err)
	}
	e, err := contract.NewEntry(contract.EntryFields{Scope: s, Actor: actor, Action: contract.Action(action), Outcome: contract.Outcome(outcome), Resource: r, Metadata: m, Associations: assoc})
	if err != nil {
		return contract.SafeRecord{}, unavailable(err)
	}
	receipt, err := receipt(id, at)
	if err != nil {
		return contract.SafeRecord{}, err
	}
	f := e.Fields()
	return contract.SafeRecord{AppendReceipt: receipt, Scope: s, Actor: actorSummary(actor), Action: f.Action, Outcome: f.Outcome, Resource: r, Metadata: m, Associations: f.Associations, Summary: summary(f.Action)}, nil
}
func summary(action contract.Action) string {
	switch action {
	case contract.SecretCreate:
		return "Secret created"
	case contract.SecretUpdate:
		return "Secret updated"
	case contract.SecretDelete:
		return "Secret deleted"
	case contract.SecretResolve:
		return "Secret use recorded"
	case contract.MasterRegister:
		return "Master key registered"
	case contract.RotationStart:
		return "Master key rotation started"
	case contract.RotationComplete:
		return "Master key rotation completed"
	case contract.RotationFailed:
		return "Master key rotation failed"
	case contract.PolicyUpdate:
		return "Outbound policy updated"
	case contract.AccessDeny:
		return "Outbound access denied"
	}
	return "Audit event"
}

func (s *Service) Get(ctx context.Context, actor identity.Actor, scope identity.Scope, id contract.ID) (contract.SafeRecord, error) {
	if err := s.authorizeHuman(ctx, foundation.Tx{}, actor, scope, identity.Read); err != nil {
		return contract.SafeRecord{}, err
	}
	return getRecord(ctx, s.store, scope, id, nil)
}

// The optional check belongs to the new System facade; old readers retain
// their original authorization, validation and error projection.
func getRecord(ctx context.Context, sql postgres.SQLExecutor, scope identity.Scope, id contract.ID, check func(contract.SafeRecord) error) (contract.SafeRecord, error) {
	if id.Validate() != nil {
		return contract.SafeRecord{}, invalid("audit_id")
	}
	r, err := scanRecord(sql.QueryRow(ctx, `SELECT `+recordColumns+` FROM agenteam_audit.audit_records WHERE scope=$1 AND scope_key=$2 AND id=$3`, string(scope.Details().Kind), scopeKey(scope), id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.SafeRecord{}, failure(foundation.NotFound, "audit_not_found", nil)
	}
	if err != nil {
		return contract.SafeRecord{}, unavailable(err)
	}
	if check != nil {
		if err = check(r); err != nil {
			return contract.SafeRecord{}, err
		}
	}
	return r, nil
}
func (s *Service) List(ctx context.Context, actor identity.Actor, scope identity.Scope, f contract.Filter, page foundation.PageRequest) (foundation.Page[contract.SafeRecord], error) {
	empty := foundation.Page[contract.SafeRecord]{}
	if err := s.authorizeHuman(ctx, foundation.Tx{}, actor, scope, identity.Read); err != nil {
		return empty, err
	}
	return s.listRecords(ctx, s.store, scope, f, page, nil)
}

func (s *Service) listRecords(ctx context.Context, sql postgres.SQLExecutor, scope identity.Scope, f contract.Filter, page foundation.PageRequest, check func(contract.SafeRecord) error) (foundation.Page[contract.SafeRecord], error) {
	empty := foundation.Page[contract.SafeRecord]{}
	if f.Validate() != nil || page.Validate() != nil {
		return empty, invalid("page_filter")
	}
	digest, err := filterDigest(f)
	if err != nil {
		return empty, err
	}
	binding := cursor.Binding{Scope: scope, QueryDigest: digest, Order: cursor.AuditOrder}
	args := []any{string(scope.Details().Kind), scopeKey(scope)}
	where := []string{"scope=$1", "scope_key=$2"}
	if scope.Details().Kind == identity.ProjectScope {
		args = append(args, scope.Details().ProjectID)
		where = append(where, "project_id=$3")
	}
	add := func(expression string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(expression, len(args)))
	}
	if f.From != nil {
		add("created_at >= $%d", f.From.Time())
	}
	if f.To != nil {
		add("created_at < $%d", f.To.Time())
	}
	for _, v := range []struct{ column, value string }{{"actor_kind", string(f.ActorKind)}, {"actor_id", f.ActorID}, {"action", string(f.Action)}, {"outcome", string(f.Outcome)}, {"resource_kind", string(f.ResourceKind)}, {"resource_id", f.ResourceID}, {"tool_id", f.ToolID}, {"execution_id", f.ExecutionID}, {"operation_id", f.OperationID}, {"approval_id", f.ApprovalID}, {"runner_id", f.RunnerID}} {
		if v.value != "" {
			add(v.column+"=$%d", v.value)
		}
	}
	if f.AgentID != "" {
		args = append(args, f.AgentID)
		n := len(args)
		where = append(where, fmt.Sprintf("((actor_kind='agent_run' AND agent_id=$%d) OR (resource_kind='agent' AND resource_id=$%d))", n, n))
	}
	if page.Cursor != "" {
		p, e := s.keys.Verify(page.Cursor, binding)
		if e != nil {
			return empty, e
		}
		if len(p.Scalars) != 2 || p.Scalars[0].Kind() != "instant" || p.Scalars[1].Kind() != "uuid" || p.OrderGeneration != nil {
			return empty, failure(foundation.CursorInvalid, "cursor_invalid", nil)
		}
		at, _ := foundation.ParseInstant(p.Scalars[0].Value())
		args = append(args, at.Time(), p.Scalars[1].Value())
		where = append(where, fmt.Sprintf("(created_at,id)<($%d,$%d)", len(args)-1, len(args)))
	}
	args = append(args, page.Limit+1)
	rows, err := sql.Query(ctx, `SELECT `+recordColumns+` FROM agenteam_audit.audit_records WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return empty, unavailable(err)
	}
	return s.recordPage(rows, binding, page.Limit, check)
}

// recordRows is local to the bounded query scanner, not a second Store port.
type recordRows interface {
	scanner
	Next() bool
	Err() error
	Close()
}

func (s *Service) recordPage(rows recordRows, binding cursor.Binding, limit int, check func(contract.SafeRecord) error) (foundation.Page[contract.SafeRecord], error) {
	empty := foundation.Page[contract.SafeRecord]{}
	closed := false
	defer func() {
		if !closed {
			rows.Close()
		}
	}()
	items := make([]contract.SafeRecord, 0, limit+1)
	for rows.Next() {
		r, e := scanRecord(rows)
		if e != nil {
			return empty, unavailable(e)
		}
		if check != nil {
			if e = check(r); e != nil {
				return empty, e
			}
			if len(items) >= limit+1 {
				return empty, unavailable(nil)
			}
		}
		items = append(items, r)
	}
	if check != nil {
		// A strict read publishes nothing until Close and its final error have
		// been observed. This includes the extra pagination row.
		rows.Close()
		closed = true
	}
	if err := rows.Err(); err != nil {
		return empty, unavailable(err)
	}
	out := foundation.Page[contract.SafeRecord]{Items: items}
	if len(items) > limit {
		out.Items = items[:limit]
		last := out.Items[len(out.Items)-1]
		at, _ := cursor.Instant(last.CreatedAt)
		id, _ := cursor.UUID(last.AuditID.String())
		var err error
		out.NextCursor, err = s.keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{at, id}})
		if err != nil {
			return empty, err
		}
	}
	return out, nil
}
