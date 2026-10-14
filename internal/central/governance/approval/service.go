// Package approval owns durable one-operation Human approval decisions.
package approval

import (
	"context"
	"encoding/json"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"reflect"
	"time"
)

type Authority struct {
	store    Store
	projects pc.ProjectAuthority
	issuer   oc.PlanIssuer
}

func NewAuthority(store Store, projects pc.ProjectAuthority) (*Authority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	if !reflect.TypeOf(store).Comparable() {
		return nil, fault(f.InvalidArgument)
	}
	return &Authority{store, projects, oc.NewPlanIssuer()}, nil
}

type Dependencies struct {
	Authority  *Authority
	Executions c.ExecutionAuthority
	Operations tc.ApprovalOperations
	Events     oc.Appender
	EventTypes c.Events
}
type Service struct {
	store Store
	deps  Dependencies
}

func NewService(store Store, deps Dependencies) (*Service, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || deps.Authority == nil || deps.Authority.store != store || nilPort(deps.Executions) || nilPort(deps.Operations) || nilPort(deps.Events) || !deps.EventTypes.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	return &Service{store, deps}, nil
}
func executionLock(b tc.ToolCallBinding) f.LockRequest {
	k, _ := f.AggregateLock(f.ExecutionAggregate, b.ExecutionID.String())
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func humanLocks(a i.Actor, p i.ProjectID) []f.LockRequest {
	u, _ := f.UserLock(a.Details().UserID)
	k, _ := f.ProjectLock(p.String())
	return []f.LockRequest{{Key: u, Mode: f.Shared}, {Key: k, Mode: f.Shared}}
}
func (s *Service) owner(ctx context.Context, tx f.Tx, a i.Actor, p i.ProjectID, intent i.AccessIntent) error {
	if a.Validate() != nil || a.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	grant, err := s.deps.Authority.projects.RequireOwnerInTx(ctx, tx, a, p, intent)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(a, p) {
		return fault(f.Forbidden)
	}
	return nil
}
func (s *Service) operationLocks(ctx context.Context, op tc.ApprovalOperation) ([]f.LockRequest, error) {
	locks, err := s.deps.Executions.ApprovalLocks(ctx, op.Binding)
	if err != nil {
		return nil, portError(err)
	}
	return oc.NormalizeLocks(append(locks, op.RequiredLocks()...))
}
func (s *Service) Ensure(ctx context.Context, b tc.ToolCallBinding) (c.Waiting, error) {
	if s == nil || ctx == nil || b.Validate() != nil {
		return c.Waiting{}, fault(f.InvalidArgument)
	}
	op, err := s.deps.Operations.ReserveApprovalOperation(ctx, b)
	if err != nil {
		return c.Waiting{}, portError(err)
	}
	if op.Validate() != nil || !sameBinding(op.Binding, b) {
		return c.Waiting{}, fault(f.Forbidden)
	}
	locks, err := s.operationLocks(ctx, op)
	if err != nil {
		return c.Waiting{}, err
	}
	id, err := f.NewID[c.RequestIdentity]()
	if err != nil {
		return c.Waiting{}, portError(err)
	}
	at, _ := f.NewInstant(time.Now())
	r := &record{value: c.Request{ToolName: b.RunnerBinding.OperationName, ID: id, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, ToolID: b.Spec.ToolID, ToolCallID: b.CallID, OperationID: op.OperationID, Fingerprint: op.Fingerprint, Status: c.Pending, Version: 1, CreatedAt: at}, operation: op}
	plan, err := s.prepareEvent(ctx, b.Actor, r, locks)
	if err != nil {
		return c.Waiting{}, err
	}
	cause, _ := f.NewRecoveryCause("governance.approval.ensure", op.OperationID.String(), "")
	var out c.Waiting
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = s.deps.Executions.RequireApprovalExecutionInTx(ctx, tx, b, ""); err != nil {
			return portError(err)
		}
		if err = s.deps.Operations.RequireUnattemptedApprovalOperationInTx(ctx, tx, op); err != nil {
			return portError(err)
		}
		old, err := loadByBinding(ctx, x, b)
		if err != nil {
			return err
		}
		if old != nil {
			if old.operation.OperationID != op.OperationID || old.operation.Fingerprint != op.Fingerprint {
				return fault(f.ConfirmationStale)
			}
			out = old.value.Waiting()
			return nil
		}
		if err = insertRecord(ctx, x, r); err != nil {
			return err
		}
		if err = s.appendEvent(ctx, tx, plan); err != nil {
			return err
		}
		out = r.value.Waiting()
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return c.Waiting{}, err
	}
	return out, nil
}
func (s *Service) Resolve(ctx context.Context, b tc.ToolCallBinding, reference string) (c.Waiting, error) {
	id, err := f.ParseID[c.RequestIdentity](reference)
	if s == nil || ctx == nil || b.Validate() != nil || err != nil {
		return c.Waiting{}, fault(f.InvalidArgument)
	}
	r, err := loadByID(ctx, s.store, id)
	if err != nil {
		return c.Waiting{}, err
	}
	if r == nil || !sameBinding(r.operation.Binding, b) {
		return c.Waiting{}, fault(f.NotFound)
	}
	locks, err := s.operationLocks(ctx, r.operation)
	if err != nil {
		return c.Waiting{}, err
	}
	cause, _ := f.NewRecoveryCause("governance.approval.resolve", b.ExecutionID.String(), reference)
	var out c.Waiting
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.deps.Executions.RequireApprovalExecutionInTx(ctx, tx, b, reference); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadByID(ctx, x, id)
		if err != nil {
			return err
		}
		if current == nil || !sameBinding(current.operation.Binding, b) {
			return fault(f.ConfirmationStale)
		}
		if current.value.Status == c.Rejected {
			err = s.deps.Operations.RequireRejectedOperationInTx(ctx, tx, current.operation, reference)
		} else {
			err = s.deps.Operations.RequireUnattemptedApprovalOperationInTx(ctx, tx, current.operation)
		}
		if err != nil {
			return portError(err)
		}
		out = current.value.Waiting()
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return c.Waiting{}, err
	}
	return out, nil
}

// This port runs inside the original current-configuration witness and final
// Tool Claim. The caller already holds Execution and Operation locks. Approval
// can never bypass those current checks or authorize a different fingerprint.
func (s *Service) RequireApprovedInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding) error {
	if s == nil || ctx == nil || b.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{executionLock(b)}); err != nil {
		return portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	r, err := loadByBinding(ctx, x, b)
	if err != nil {
		return err
	}
	if r == nil || r.value.Status != c.Approved {
		return fault(f.Forbidden)
	}
	if err = s.deps.Operations.RequireApprovalOperationInTx(ctx, tx, r.operation); err != nil {
		return portError(err)
	}
	var ok bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_governance.one_time_approvals WHERE request_id=$1 AND operation_id=$2 AND fingerprint=$3)`, r.value.ID.String(), r.value.OperationID.String(), string(r.value.Fingerprint)).Scan(&ok)
	if err != nil {
		return portError(err)
	}
	if !ok {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}
func (s *Service) Get(ctx context.Context, a i.Actor, p i.ProjectID, id c.RequestID) (c.Request, error) {
	if s == nil || ctx == nil || p.Validate() != nil || id.Validate() != nil || a.Validate() != nil || a.Details().Kind != i.Human {
		return c.Request{}, fault(f.InvalidArgument)
	}
	cause, _ := f.NewRecoveryCause("governance.approval.read", id.String(), "")
	var out c.Request
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, humanLocks(a, p)); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, p, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		r, err := loadByID(ctx, x, id)
		if err != nil {
			return err
		}
		if r == nil || r.value.ProjectID != p {
			return fault(f.NotFound)
		}
		out = r.value
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return c.Request{}, err
	}
	return out, nil
}
func decisionIdentity(p i.ProjectID, key f.IdempotencyKey) (f.CommandIdentity, error) {
	return f.NewCommandIdentity("project", []string{p.String()}, "approval.decide", key)
}
func decisionDigest(a i.Actor, p i.ProjectID, id c.RequestID, in c.Decision) (f.Digest, error) {
	raw, err := json.Marshal(struct {
		Format, Command, User, Project, Request string
		Input                                   c.Decision
	}{"canonical-v1", "approval.decide", a.Details().UserID, p.String(), id.String(), in})
	if err != nil {
		return "", err
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", err
	}
	return oc.DigestBytes(raw), nil
}

var _ c.Gate = (*Service)(nil)
