package secret

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// ProjectVariableLeaseService owns only environment retention. It cannot read
// material, use the legacy Purpose route, release a lease or authorize a future
// process. Every call is synchronous and the final write uses the caller's Tx.
type ProjectVariableLeaseService struct {
	data func() *projectVariableLeaseState
}
type projectVariableLeaseState struct {
	service *Service
	owner   sc.ProjectVariableLeaseAuthority
}

func NewProjectVariableLeases(service *Service, owner sc.ProjectVariableLeaseAuthority) (*ProjectVariableLeaseService, error) {
	if service == nil || service.data == nil || service.state() == nil || nilPort(owner) {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	s := &projectVariableLeaseState{service, owner}
	return &ProjectVariableLeaseService{func() *projectVariableLeaseState { return s }}, nil
}
func (s *ProjectVariableLeaseService) state() *projectVariableLeaseState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}

type projectVariableLeasePlanData struct {
	issuer  *projectVariableLeaseState
	request sc.ProjectVariableLeaseRequest
	id      sc.LeaseID
	locks   []f.LockRequest
}
type projectVariableLeasePlan struct {
	data func() projectVariableLeasePlanData
}

func (p projectVariableLeasePlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (projectVariableLeasePlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_lease_plan")
}
func (projectVariableLeasePlan) LogValue() slog.Value {
	return slog.StringValue("project_variable_lease_plan")
}
func (projectVariableLeasePlan) MarshalJSON() ([]byte, error) {
	return []byte(`"project_variable_lease_plan"`), nil
}

func environmentLeaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return authorization(err)
}
func environmentLeaseContext(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	return environmentLeaseError(ctx.Err())
}
func sameEnvironmentLease(a, b sc.ProjectVariableLeaseRequest) bool {
	return a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.VariableID == b.VariableID && a.VariableVersion == b.VariableVersion && a.AgentVersion == b.AgentVersion && a.CredentialVersion == b.CredentialVersion && a.AttemptBinding == b.AttemptBinding && a.Ref.Equal(b.Ref)
}
func (s *ProjectVariableLeaseService) DiscoverProjectVariableLease(ctx context.Context, r sc.ProjectVariableLeaseRequest) (sc.ProjectVariableLeasePlan, error) {
	if err := environmentLeaseContext(ctx); err != nil {
		return nil, err
	}
	if r.Validate() != nil {
		return nil, invalid()
	}
	st := s.state()
	if st == nil {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	if err := st.owner.CheckProjectVariableLeasePlan(ctx, r); err != nil {
		return nil, environmentLeaseError(err)
	}
	if err := environmentLeaseContext(ctx); err != nil {
		return nil, err
	}
	locks, err := oc.NormalizeLocks(r.RequiredLocks())
	if err != nil {
		return nil, invalid()
	}
	id, err := f.NewID[sc.Lease]()
	if err != nil {
		return nil, unavailable(nil)
	}
	d := projectVariableLeasePlanData{st, r, id, locks}
	return projectVariableLeasePlan{func() projectVariableLeasePlanData { return d }}, nil
}
func (s *ProjectVariableLeaseService) AcquireProjectVariableLeaseInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRequest, plan sc.ProjectVariableLeasePlan) (sc.CredentialLease, error) {
	empty := sc.CredentialLease{}
	if err := environmentLeaseContext(ctx); err != nil {
		return empty, err
	}
	if r.Validate() != nil {
		return empty, invalid()
	}
	st := s.state()
	if st == nil {
		return empty, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	p, ok := plan.(projectVariableLeasePlan)
	if !ok || p.data == nil {
		return empty, invalid()
	}
	d := p.data()
	if d.issuer != st || !sameEnvironmentLease(d.request, r) {
		return empty, invalid()
	}
	store := st.service.state().store
	x, err := store.InTx(tx)
	if err != nil || nilPort(x) {
		return empty, unavailable(nil)
	}
	if err = store.RequireHeldLocks(ctx, tx, d.locks); err != nil {
		return empty, environmentLeaseError(err)
	}
	if err = st.owner.CheckProjectVariableLeaseInTx(ctx, tx, r); err != nil {
		return empty, environmentLeaseError(err)
	}
	if err = environmentLeaseContext(ctx); err != nil {
		return empty, err
	}
	metadata, _, err := loadProjectVariableMetadata(ctx, x, r.Ref)
	if err != nil {
		return empty, environmentLeaseError(err)
	}
	if metadata.Version != r.CredentialVersion {
		return empty, failure(Conflict, f.VersionConflict, nil)
	}
	if err = st.service.writable(); err != nil {
		return empty, err
	}
	var lease, project, agent, variable, binding string
	var variableVersion, credentialVersion, agentVersion int64
	err = x.QueryRow(ctx, `SELECT lease_id::text,project_id,agent_id,variable_id::text,variable_version,credential_version,agent_version,attempt_binding FROM agenteam_secret.project_variable_execution_leases WHERE execution_id=$1 AND credential_id=$2`, r.ExecutionID.String(), r.Ref.Details().ID.String()).Scan(&lease, &project, &agent, &variable, &variableVersion, &credentialVersion, &agentVersion, &binding)
	if canceled := environmentLeaseContext(ctx); canceled != nil {
		return empty, canceled
	}
	if err == nil {
		id, e := f.ParseID[sc.Lease](lease)
		if e != nil || project != r.ProjectID.String() || agent != r.AgentID.String() || variable != r.VariableID.String() || variableVersion != int64(r.VariableVersion) || credentialVersion != int64(r.CredentialVersion) || agentVersion != int64(r.AgentVersion) || binding != string(r.AttemptBinding) {
			return empty, failure(Conflict, f.VersionConflict, nil)
		}
		if err = environmentLeaseContext(ctx); err != nil {
			return empty, err
		}
		return sc.CredentialLease{LeaseID: id, CredentialRef: r.Ref}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, unavailable(nil)
	}
	if err = environmentLeaseContext(ctx); err != nil {
		return empty, err
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_secret.project_variable_execution_leases(lease_id,execution_id,project_id,agent_id,variable_id,variable_version,credential_id,credential_version,agent_version,attempt_binding) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, d.id.String(), r.ExecutionID.String(), r.ProjectID.String(), r.AgentID.String(), r.VariableID.String(), int64(r.VariableVersion), r.Ref.Details().ID.String(), int64(r.CredentialVersion), int64(r.AgentVersion), string(r.AttemptBinding))
	if canceled := environmentLeaseContext(ctx); canceled != nil {
		return empty, canceled
	}
	if err != nil || tag.RowsAffected() != 1 {
		return empty, unavailable(nil)
	}
	if err = environmentLeaseContext(ctx); err != nil {
		return empty, err
	}
	return sc.CredentialLease{LeaseID: d.id, CredentialRef: r.Ref}, nil
}

var _ sc.ProjectVariableLeases = (*ProjectVariableLeaseService)(nil)
