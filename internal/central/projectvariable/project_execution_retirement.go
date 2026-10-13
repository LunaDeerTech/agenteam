package projectvariable

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type ProjectExecutionRetirementReferences struct {
	authority *ProjectExecutionRetirementAuthority
	leases    sc.ProjectExecutionLeaseRetirement
}

func NewProjectExecutionRetirementReferences(authority *ProjectExecutionRetirementAuthority, leases sc.ProjectExecutionLeaseRetirement) (*ProjectExecutionRetirementReferences, error) {
	if authority.state() == nil || nilPort(leases) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectExecutionRetirementReferences{authority, leases}, nil
}
func (*ProjectExecutionRetirementReferences) ProjectExecutionRetirementReferenceKind() ec.ProjectExecutionRetirementReferenceKind {
	return ec.ProjectExecutionVariablesReferences
}

type projectExecutionVariablePlan struct {
	issuer      *ProjectExecutionRetirementReferences
	request     ec.ProjectExecutionRetirementRequest
	environment sc.ProjectExecutionLeaseRetirementRequest
	leasePlan   sc.ProjectExecutionLeaseRetirementPlan
	locks       []f.LockRequest
	binding     f.Digest
}

func (p *projectExecutionVariablePlan) ProjectExecutionRetirementReferenceDigest() f.Digest {
	if p == nil {
		return ""
	}
	return p.binding
}
func (p *projectExecutionVariablePlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*projectExecutionVariablePlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_execution_variables_plan"))
}
func (*projectExecutionVariablePlan) LogValue() slog.Value {
	return slog.StringValue("project_execution_variables_plan")
}
func (*projectExecutionVariablePlan) MarshalJSON() ([]byte, error) {
	return []byte(`"project_execution_variables_plan"`), nil
}

func (p *ProjectExecutionRetirementReferences) authorize(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, locks []f.LockRequest, applied bool) (postgres.SQLExecutor, error) {
	if err := environmentContext(ctx); err != nil {
		return nil, err
	}
	if r.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if p == nil || p.authority.state() == nil || nilPort(p.leases) {
		return nil, fault(f.DependencyUnbound)
	}
	s := p.authority.state()
	x, err := s.store.InTx(tx)
	if err != nil || nilPort(x) {
		return nil, fault(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, environmentError(err)
	}
	if applied {
		err = s.owner.RequireProjectExecutionRetirementAppliedInTx(ctx, tx, r)
	} else {
		err = s.owner.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, r)
	}
	if err != nil {
		return nil, environmentError(err)
	}
	if err = environmentContext(ctx); err != nil {
		return nil, err
	}
	return x, nil
}
func (p *ProjectExecutionRetirementReferences) DiscoverProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementReferencePlan, error) {
	base := sc.ProjectExecutionLeaseRetirementRequest{ProjectID: r.Binding.ProjectID, AgentID: r.Binding.AgentID, ExecutionID: r.Binding.ExecutionID}
	x, err := p.authorize(ctx, tx, r, base.RequiredLocks(), false)
	if err != nil {
		return nil, err
	}
	environment, err := readProjectExecutionEnvironment(ctx, x, r)
	if err != nil {
		return nil, err
	}
	plan := &projectExecutionVariablePlan{issuer: p, request: r, environment: environment, locks: base.RequiredLocks()}
	err = p.withLeaseScope(ctx, tx, plan, projectExecutionDiscover, func(ctx context.Context) error {
		var e error
		plan.leasePlan, e = p.leases.DiscoverProjectExecutionLeasesInTx(ctx, tx, environment.Clone())
		return e
	})
	if err != nil {
		return nil, environmentError(err)
	}
	if nilPort(plan.leasePlan) {
		return nil, fault(f.DependencyUnbound)
	}
	plan.locks, err = oc.NormalizeLocks(append(slices.Clone(plan.locks), plan.leasePlan.RequiredLocks()...))
	if err != nil {
		return nil, environmentError(err)
	}
	plan.binding, err = projectExecutionVariableDigest(plan)
	if err != nil {
		return nil, err
	}
	return plan, environmentContext(ctx)
}
func (p *ProjectExecutionRetirementReferences) plan(r ec.ProjectExecutionRetirementRequest, v ec.ProjectExecutionRetirementReferencePlan) (*projectExecutionVariablePlan, error) {
	d, ok := v.(*projectExecutionVariablePlan)
	if !ok || d == nil || d.issuer != p || d.request.Binding != r.Binding || d.request.Cause != r.Cause || !d.request.Actor.Equal(r.Actor) {
		return nil, fault(f.Forbidden)
	}
	return d, nil
}
func (p *ProjectExecutionRetirementReferences) RetireProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, plan ec.ProjectExecutionRetirementReferencePlan) error {
	d, err := p.plan(r, plan)
	if err != nil {
		return err
	}
	x, err := p.authorize(ctx, tx, r, d.locks, false)
	if err != nil {
		return err
	}
	current, err := readProjectExecutionEnvironment(ctx, x, r)
	if err != nil {
		return err
	}
	if !current.Equal(d.environment) {
		return fault(f.VersionConflict)
	}
	if err = deleteProjectExecutionReferences(ctx, x, d.environment); err != nil {
		return err
	}
	err = p.withLeaseScope(ctx, tx, d, projectExecutionRetire, func(ctx context.Context) error {
		return p.leases.RetireProjectExecutionLeasesInTx(ctx, tx, d.environment.Clone(), d.leasePlan)
	})
	if err != nil {
		return environmentError(err)
	}
	if err = deleteProjectExecutionEnvironment(ctx, x, d.environment); err != nil {
		return err
	}
	return nil
}
func (p *ProjectExecutionRetirementReferences) RequireProjectExecutionReferencesRetiredInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, plan ec.ProjectExecutionRetirementReferencePlan) error {
	d, err := p.plan(r, plan)
	if err != nil {
		return err
	}
	x, err := p.authorize(ctx, tx, r, d.locks, true)
	if err != nil {
		return err
	}
	// Absence is observed only in the original Applied scope. Unknown commit
	// confirmation may reuse this exact immutable plan in a new read-only Tx.
	current, err := readProjectExecutionEnvironment(ctx, x, r)
	if err != nil {
		return err
	}
	if current.EnvironmentPresent || len(current.References) != 0 {
		return fault(f.ResourceBusy)
	}
	return environmentError(p.withLeaseScope(ctx, tx, d, projectExecutionObserve, func(ctx context.Context) error {
		return p.leases.RequireProjectExecutionLeasesRetiredInTx(ctx, tx, d.environment.Clone(), d.leasePlan)
	}))
}

var _ ec.ProjectExecutionRetirementReferences = (*ProjectExecutionRetirementReferences)(nil)

func projectExecutionVariableDigest(p *projectExecutionVariablePlan) (f.Digest, error) {
	// Optional absent-capture scalars intentionally use primitive wire types;
	// Version/Digest MarshalJSON correctly reject their zero domain values.
	type environment struct {
		Project, Agent, Execution string
		Present                   bool
		AgentVersion              int64
		Attempt, Capture          string
		Ordinary, Secrets         int
		References                []sc.ProjectExecutionLeaseReference
	}
	r0 := p.environment
	facts := environment{r0.ProjectID.String(), r0.AgentID.String(), r0.ExecutionID.String(), r0.EnvironmentPresent, int64(r0.AgentVersion), string(r0.AttemptBinding), string(r0.CaptureDigest), r0.OrdinaryCount, r0.SecretCount, r0.References}
	type lock struct {
		Key  string
		Mode f.LockMode
	}
	keys := make([]lock, len(p.locks))
	for n, l := range p.locks {
		keys[n] = lock{l.Key.Canonical(), l.Mode}
	}
	lease := p.leasePlan.ProjectExecutionLeaseRetirementDigest()
	if lease.Validate() != nil {
		return "", fault(f.DependencyUnbound)
	}
	raw, err := json.Marshal(struct {
		Domain      string
		Binding     ec.ProjectExecutionRetirementBinding
		Actor       any
		Cause       any
		Environment environment
		Lease       f.Digest
		Locks       []lock
	}{"project_execution_variables.v1", p.request.Binding, p.request.Actor.Details(), p.request.Cause, facts, lease, keys})
	if err != nil {
		return "", unavailable(err)
	}
	return digest(raw), nil
}
