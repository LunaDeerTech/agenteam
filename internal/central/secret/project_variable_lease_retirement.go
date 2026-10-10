package secret

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// This service retires dedicated environment leases, never legacy Model leases
// or historical references. It performs no value read, worker or nested Tx.
type ProjectVariableLeaseRetirementService struct {
	service *Service
	owner   sc.ProjectVariableLeaseRetirementAuthority
}

func NewProjectVariableLeaseRetirement(service *Service, owner sc.ProjectVariableLeaseRetirementAuthority) (*ProjectVariableLeaseRetirementService, error) {
	if service == nil || service.data == nil || service.state() == nil || nilPort(owner) {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	return &ProjectVariableLeaseRetirementService{service: service, owner: owner}, nil
}

type projectVariableLeaseRetirementPlan struct {
	issuer  *ProjectVariableLeaseRetirementService
	request sc.ProjectVariableLeaseRetirementRequest
	locks   []f.LockRequest
}

func (p *projectVariableLeaseRetirementPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*projectVariableLeaseRetirementPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_lease_retirement_plan")
}
func (*projectVariableLeaseRetirementPlan) LogValue() slog.Value {
	return slog.StringValue("project_variable_lease_retirement_plan")
}
func (*projectVariableLeaseRetirementPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"project_variable_lease_retirement_plan"`), nil
}

func sameEnvironmentRetirement(a, b sc.ProjectVariableLeaseRetirementRequest) bool {
	return a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.VariableID == b.VariableID && a.VariableVersion == b.VariableVersion && a.AgentVersion == b.AgentVersion && a.LeaseID == b.LeaseID && a.AttemptBinding == b.AttemptBinding && a.Ref.Equal(b.Ref)
}
func (s *ProjectVariableLeaseRetirementService) valid(ctx context.Context, r sc.ProjectVariableLeaseRetirementRequest) error {
	if err := environmentLeaseContext(ctx); err != nil {
		return err
	}
	if r.Validate() != nil {
		return invalid()
	}
	if s == nil || s.service == nil || s.service.data == nil || s.service.state() == nil || nilPort(s.owner) {
		return failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	return nil
}
func (s *ProjectVariableLeaseRetirementService) DiscoverProjectVariableLeaseRetirement(ctx context.Context, r sc.ProjectVariableLeaseRetirementRequest) (sc.ProjectVariableLeaseRetirementPlan, error) {
	if err := s.valid(ctx, r); err != nil {
		return nil, err
	}
	if err := s.owner.CheckProjectVariableLeaseRetirementPlan(ctx, r); err != nil {
		return nil, environmentLeaseError(err)
	}
	if err := environmentLeaseContext(ctx); err != nil {
		return nil, err
	}
	locks, err := oc.NormalizeLocks(r.RequiredLocks())
	if err != nil {
		return nil, invalid()
	}
	return &projectVariableLeaseRetirementPlan{issuer: s, request: r, locks: locks}, nil
}

func (s *ProjectVariableLeaseRetirementService) authorized(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest, plan sc.ProjectVariableLeaseRetirementPlan, observe bool) (postgres.SQLExecutor, error) {
	if err := s.valid(ctx, r); err != nil {
		return nil, err
	}
	p, ok := plan.(*projectVariableLeaseRetirementPlan)
	if !ok || p == nil || p.issuer != s || !sameEnvironmentRetirement(p.request, r) {
		return nil, invalid()
	}
	store := s.service.state().store
	x, err := store.InTx(tx)
	if err != nil || nilPort(x) {
		return nil, unavailable(nil)
	}
	if err = store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return nil, environmentLeaseError(err)
	}
	if observe {
		err = s.owner.CheckProjectVariableLeaseRetiredInTx(ctx, tx, r)
	} else {
		err = s.owner.CheckProjectVariableLeaseRetirementInTx(ctx, tx, r)
	}
	if canceled := environmentLeaseContext(ctx); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, environmentLeaseError(err)
	}
	return x, nil
}

func readEnvironmentLeaseRetirement(ctx context.Context, x postgres.SQLExecutor, r sc.ProjectVariableLeaseRetirementRequest) (bool, error) {
	var execution, project, agent, variable, credential, attempt string
	var variableVersion, credentialVersion, agentVersion int64
	var released bool
	var at *time.Time
	err := x.QueryRow(ctx, `SELECT execution_id,project_id,agent_id,variable_id::text,variable_version,credential_id::text,credential_version,agent_version,attempt_binding,released,released_at FROM agenteam_secret.project_variable_execution_leases WHERE lease_id=$1`, r.LeaseID.String()).Scan(&execution, &project, &agent, &variable, &variableVersion, &credential, &credentialVersion, &agentVersion, &attempt, &released, &at)
	if canceled := environmentLeaseContext(ctx); canceled != nil {
		return false, canceled
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, failure(NotFound, f.NotFound, nil)
	}
	if err != nil {
		return false, unavailable(err)
	}
	if execution != r.ExecutionID.String() || project != r.ProjectID.String() || agent != r.AgentID.String() || variable != r.VariableID.String() || variableVersion != int64(r.VariableVersion) || credential != r.Ref.Details().ID.String() || agentVersion != int64(r.AgentVersion) || attempt != string(r.AttemptBinding) || credentialVersion <= 0 {
		return false, failure(Conflict, f.VersionConflict, nil)
	}
	if released != (at != nil) || at != nil && (at.IsZero() || at.Nanosecond()%1000 != 0) {
		return false, unavailable(nil)
	}
	return released, nil
}

func (s *ProjectVariableLeaseRetirementService) ProjectVariableLeaseRetiredInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest, plan sc.ProjectVariableLeaseRetirementPlan) (bool, error) {
	x, err := s.authorized(ctx, tx, r, plan, true)
	if err != nil {
		return false, err
	}
	return readEnvironmentLeaseRetirement(ctx, x, r)
}

func (s *ProjectVariableLeaseRetirementService) RetireProjectVariableLeaseInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest, plan sc.ProjectVariableLeaseRetirementPlan) error {
	x, err := s.authorized(ctx, tx, r, plan, false)
	if err != nil {
		return err
	}
	released, err := readEnvironmentLeaseRetirement(ctx, x, r)
	if err != nil || released {
		return err
	}
	if err = s.service.writable(); err != nil {
		return err
	}
	if err = environmentLeaseContext(ctx); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_secret.project_variable_execution_leases SET released=true,released_at=clock_timestamp() WHERE lease_id=$1 AND execution_id=$2 AND project_id=$3 AND agent_id=$4 AND variable_id=$5 AND variable_version=$6 AND credential_id=$7 AND agent_version=$8 AND attempt_binding=$9 AND NOT released`, r.LeaseID.String(), r.ExecutionID.String(), r.ProjectID.String(), r.AgentID.String(), r.VariableID.String(), int64(r.VariableVersion), r.Ref.Details().ID.String(), int64(r.AgentVersion), string(r.AttemptBinding))
	if canceled := environmentLeaseContext(ctx); canceled != nil {
		return canceled
	}
	if err != nil || tag.RowsAffected() != 1 {
		return unavailable(err)
	}
	released, err = readEnvironmentLeaseRetirement(ctx, x, r)
	if err != nil {
		return err
	}
	if !released {
		return unavailable(nil)
	}
	return nil
}

var _ sc.ProjectVariableLeaseRetirement = (*ProjectVariableLeaseRetirementService)(nil)
