package projectvariable

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// ExecutionEnvironmentProvider captures real Project data, not a runtime
// process environment. It owns no goroutine, decryption, external I/O or Tx.
// Secret values never enter its result. Its caller joins both discovery and
// final resolution and decides the outcome of the one final transaction.
type ExecutionEnvironmentProvider struct {
	data func() *executionEnvironmentState
}
type executionEnvironmentState struct {
	authority *EnvironmentAuthority
	leases    sc.ProjectVariableLeases
}

func NewExecutionEnvironment(authority *EnvironmentAuthority, leases sc.ProjectVariableLeases) (*ExecutionEnvironmentProvider, error) {
	if authority.state() == nil || nilPort(leases) {
		return nil, fault(f.DependencyUnbound)
	}
	s := &executionEnvironmentState{authority, leases}
	return &ExecutionEnvironmentProvider{func() *executionEnvironmentState { return s }}, nil
}
func (p *ExecutionEnvironmentProvider) state() *executionEnvironmentState {
	if p == nil || p.data == nil {
		return nil
	}
	return p.data()
}

type environmentCandidate struct {
	issuer     *executionEnvironmentState
	request    c.EnvironmentCaptureRequest
	facts      c.EnvironmentDiscoveryFacts
	rows       environmentRows
	requests   []sc.ProjectVariableLeaseRequest
	leasePlans []sc.ProjectVariableLeasePlan
	locks      []f.LockRequest
}
type environmentPlan struct{ data func() *environmentCandidate }

func (p environmentPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (environmentPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_environment_plan")
}
func (environmentPlan) LogValue() slog.Value { return slog.StringValue("execution_environment_plan") }
func (environmentPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_environment_plan"`), nil
}

func (p *ExecutionEnvironmentProvider) DiscoverExecutionEnvironment(ctx context.Context, r c.EnvironmentCaptureRequest) (c.EnvironmentCapturePlan, error) {
	if err := environmentContext(ctx); err != nil {
		return nil, err
	}
	if r.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	s := p.state()
	if s == nil {
		return nil, fault(f.DependencyUnbound)
	}
	a := s.authority.state()
	locks, err := oc.NormalizeLocks(r.RequiredLocks())
	if err != nil {
		return nil, environmentError(err)
	}
	cause, err := readCause("environment_capture")
	if err != nil {
		return nil, err
	}
	d := &environmentCandidate{issuer: s, request: r, locks: locks}
	var callbackErr error
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = a.store.AcquireAll(ctx, tx, locks); err != nil {
			return environmentError(err)
		}
		x, err := a.store.InTx(tx)
		if err != nil || nilPort(x) {
			return internal(nil)
		}
		if err = a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return environmentError(err)
		}
		facts, err := a.owner.RequireEnvironmentDiscoveryInTx(ctx, tx, r)
		if err != nil {
			return environmentError(err)
		}
		if err = facts.Validate(r); err != nil {
			return err
		}
		d.facts = facts.Clone()
		d.rows, err = readEnvironment(ctx, x, r)
		if err != nil {
			return err
		}
		return environmentContext(ctx)
	})
	if err = txError(result); err != nil {
		if result.State() != f.Unknown && callbackErr != nil {
			return nil, environmentError(callbackErr)
		}
		return nil, err
	}
	if err = environmentContext(ctx); err != nil {
		return nil, err
	}
	d.requests = environmentRequests(r, d.facts, d.rows)
	d.leasePlans = make([]sc.ProjectVariableLeasePlan, len(d.requests))
	w := &environmentLeaseWitness{issuer: a, candidate: d}
	w.live.Store(true)
	planCtx := context.WithValue(ctx, environmentLeaseWitnessKey{}, w)
	// The witness is live only across the original synchronous discovery calls;
	// retaining this context cannot authorize a subsequent lease acquisition.
	defer w.live.Store(false)
	for n, request := range d.requests {
		plan, err := s.leases.DiscoverProjectVariableLease(planCtx, request)
		if err != nil {
			return nil, environmentError(err)
		}
		if nilPort(plan) {
			return nil, fault(f.DependencyUnbound)
		}
		d.leasePlans[n] = plan
		locks = append(locks, request.RequiredLocks()...)
		locks = append(locks, plan.RequiredLocks()...)
	}
	d.locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return nil, environmentError(err)
	}
	if err = environmentContext(ctx); err != nil {
		return nil, err
	}
	return environmentPlan{func() *environmentCandidate { return d }}, nil
}

func (p *ExecutionEnvironmentProvider) ResolveExecutionEnvironmentInTx(ctx context.Context, tx f.Tx, r c.EnvironmentCaptureRequest, plan c.EnvironmentCapturePlan) (c.EnvironmentCapture, error) {
	empty := c.EnvironmentCapture{}
	if err := environmentContext(ctx); err != nil {
		return empty, err
	}
	if r.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	s := p.state()
	if s == nil {
		return empty, fault(f.DependencyUnbound)
	}
	a := s.authority.state()
	pp, ok := plan.(environmentPlan)
	if !ok || pp.data == nil {
		return empty, fault(f.InvalidArgument)
	}
	d := pp.data()
	if d == nil || d.issuer != s || d.request != r {
		return empty, fault(f.InvalidArgument)
	}
	x, err := a.store.InTx(tx)
	if err != nil || nilPort(x) {
		return empty, fault(f.InvalidArgument)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, d.locks); err != nil {
		return empty, environmentError(err)
	}
	facts, err := a.owner.RequireEnvironmentCaptureInTx(ctx, tx, r)
	if err != nil {
		return empty, environmentError(err)
	}
	if err = facts.Validate(r); err != nil {
		return empty, err
	}
	if facts.AttemptBinding != d.facts.AttemptBinding || !reflect.DeepEqual(facts.Project, d.facts.Project) || !slices.Equal(facts.AllowedSecretVariableIDs, environmentIDs(d.rows)) {
		return empty, fault(f.VersionConflict)
	}
	for _, entry := range d.rows.index {
		if entry.Version != facts.AgentVersion {
			return empty, fault(f.VersionConflict)
		}
	}
	rows, err := readEnvironment(ctx, x, r)
	if err != nil {
		return empty, err
	}
	if rows.mapping != d.rows.mapping {
		return empty, fault(f.VersionConflict)
	}
	if err = environmentContext(ctx); err != nil {
		return empty, err
	}
	w := &environmentLeaseWitness{issuer: a, candidate: d, tx: tx, final: true}
	w.live.Store(true)
	defer w.live.Store(false)
	leaseCtx := context.WithValue(ctx, environmentLeaseWitnessKey{}, w)
	fields := c.EnvironmentCaptureFields{Request: r, AttemptBinding: facts.AttemptBinding, AgentVersion: facts.AgentVersion, Variables: slices.Clone(rows.variables), Secrets: make([]c.ExecutionSecretVariable, len(rows.secrets))}
	for n, request := range d.requests {
		lease, err := s.leases.AcquireProjectVariableLeaseInTx(leaseCtx, tx, request, d.leasePlans[n])
		if err != nil {
			return empty, environmentError(err)
		}
		if lease.LeaseID.Validate() != nil || !lease.CredentialRef.Equal(request.Ref) {
			return empty, internal(nil)
		}
		fields.Secrets[n] = c.ExecutionSecretVariable{Variable: rows.secrets[n].Variable.Clone(), CredentialRef: lease.CredentialRef, LeaseID: lease.LeaseID}
	}
	value, err := c.NewEnvironmentCapture(fields)
	if err != nil {
		return empty, err
	}
	if err = persistEnvironment(ctx, x, value, rows); err != nil {
		return empty, err
	}
	if err = environmentContext(ctx); err != nil {
		return empty, err
	}
	return value, nil
}

var _ c.ExecutionEnvironment = (*ExecutionEnvironmentProvider)(nil)
