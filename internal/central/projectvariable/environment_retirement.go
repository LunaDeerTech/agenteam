package projectvariable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// Retirement preserves all immutable environment history. Only the original
// Secret leases' permission to be used by this Execution is retired.
type EnvironmentRetirementProvider struct {
	data func() *environmentRetirementState
}
type environmentRetirementState struct {
	authority *EnvironmentRetirementAuthority
	leases    sc.ProjectVariableLeaseRetirement
}

func NewEnvironmentRetirement(authority *EnvironmentRetirementAuthority, leases sc.ProjectVariableLeaseRetirement) (*EnvironmentRetirementProvider, error) {
	if authority.state() == nil || nilPort(leases) {
		return nil, fault(f.DependencyUnbound)
	}
	s := &environmentRetirementState{authority, leases}
	return &EnvironmentRetirementProvider{func() *environmentRetirementState { return s }}, nil
}
func (p *EnvironmentRetirementProvider) state() *environmentRetirementState {
	if p == nil || p.data == nil {
		return nil
	}
	return p.data()
}

type environmentRetirementCandidate struct {
	issuer       *environmentRetirementState
	capture      c.EnvironmentCapture
	binding      f.Digest
	storedDigest f.Digest
	requests     []sc.ProjectVariableLeaseRetirementRequest
	leasePlans   []sc.ProjectVariableLeaseRetirementPlan
	locks        []f.LockRequest
}
type environmentRetirementPlan struct {
	data func() *environmentRetirementCandidate
}

func (p environmentRetirementPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (environmentRetirementPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "environment_retirement_plan")
}
func (environmentRetirementPlan) LogValue() slog.Value {
	return slog.StringValue("environment_retirement_plan")
}
func (environmentRetirementPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"environment_retirement_plan"`), nil
}

func environmentRetirementRequests(v c.EnvironmentCapture) ([]sc.ProjectVariableLeaseRetirementRequest, error) {
	fields := v.Fields()
	r := fields.Request
	requests := make([]sc.ProjectVariableLeaseRetirementRequest, len(fields.Secrets))
	leases, credentials := map[string]bool{}, map[string]bool{}
	for n, secret := range fields.Secrets {
		vf := secret.Variable.Fields()
		request := sc.ProjectVariableLeaseRetirementRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, ExecutionID: r.ExecutionID, VariableID: vf.ID, VariableVersion: vf.Version, AgentVersion: fields.AgentVersion, Ref: secret.CredentialRef, LeaseID: secret.LeaseID, AttemptBinding: fields.AttemptBinding}
		if request.Validate() != nil || leases[request.LeaseID.String()] || credentials[request.Ref.Details().ID.String()] {
			return nil, fault(f.InvalidArgument)
		}
		leases[request.LeaseID.String()], credentials[request.Ref.Details().ID.String()] = true, true
		requests[n] = request
	}
	slices.SortFunc(requests, func(a, b sc.ProjectVariableLeaseRetirementRequest) int {
		return strings.Compare(a.VariableID.String(), b.VariableID.String())
	})
	return requests, nil
}

// This reads only immutable PV facts. Current values/allowlists may legitimately
// have changed since capture and cannot prevent retirement of the original use.
func readEnvironmentRetirement(ctx context.Context, x postgres.SQLExecutor, d *environmentRetirementCandidate) (f.Digest, error) {
	v := d.capture.Fields()
	var project, agent, attempt, stored string
	var version int64
	var ordinary, secrets int
	err := x.QueryRow(ctx, `SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,ordinary_count,secret_count FROM agenteam_projectvariable.execution_environments WHERE execution_id=$1`, v.Request.ExecutionID.String()).Scan(&project, &agent, &version, &attempt, &stored, &ordinary, &secrets)
	if canceled := environmentContext(ctx); canceled != nil {
		return "", canceled
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fault(f.NotFound)
	}
	if err != nil {
		return "", unavailable(err)
	}
	if project != v.Request.ProjectID.String() || agent != v.Request.AgentID.String() || version != int64(v.AgentVersion) || attempt != string(v.AttemptBinding) || ordinary != len(v.Variables) || secrets != len(v.Secrets) || f.Digest(stored).Validate() != nil || d.storedDigest != "" && stored != string(d.storedDigest) {
		return "", fault(f.VersionConflict)
	}
	var variables, leases []string
	if err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(variable_id::text ORDER BY variable_id),'{}'::text[]),COALESCE(array_agg(lease_id::text ORDER BY variable_id),'{}'::text[]) FROM agenteam_projectvariable.execution_secret_references WHERE execution_id=$1`, v.Request.ExecutionID.String()).Scan(&variables, &leases); err != nil {
		return "", environmentError(err)
	}
	wantVariables, wantLeases := make([]string, len(d.requests)), make([]string, len(d.requests))
	for n, r := range d.requests {
		wantVariables[n], wantLeases[n] = r.VariableID.String(), r.LeaseID.String()
	}
	if !slices.Equal(variables, wantVariables) || !slices.Equal(leases, wantLeases) {
		return "", fault(f.VersionConflict)
	}
	return f.Digest(stored), environmentContext(ctx)
}

func (p *EnvironmentRetirementProvider) DiscoverEnvironmentRetirement(ctx context.Context, v c.EnvironmentCapture) (c.EnvironmentRetirementPlan, error) {
	if err := environmentContext(ctx); err != nil {
		return nil, err
	}
	binding, err := c.EnvironmentRetirementBinding(v)
	if err != nil {
		return nil, err
	}
	s := p.state()
	if s == nil {
		return nil, fault(f.DependencyUnbound)
	}
	a := s.authority.state()
	if err = a.owner.CheckEnvironmentRetirementPlan(ctx, v); err != nil {
		return nil, environmentError(err)
	}
	if err = environmentContext(ctx); err != nil {
		return nil, err
	}
	requests, err := environmentRetirementRequests(v)
	if err != nil {
		return nil, err
	}
	locks, err := oc.NormalizeLocks(v.Fields().Request.RequiredLocks())
	if err != nil {
		return nil, environmentError(err)
	}
	for _, r := range requests {
		locks, err = mergeEnvironmentLocks(locks, r.RequiredLocks())
		if err != nil {
			return nil, environmentError(err)
		}
	}
	d := &environmentRetirementCandidate{issuer: s, capture: v.Clone(), binding: binding, requests: requests, locks: locks, leasePlans: make([]sc.ProjectVariableLeaseRetirementPlan, len(requests))}
	cause, err := readCause("environment_retirement")
	if err != nil {
		return nil, err
	}
	var callbackErr error
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = a.store.AcquireAll(ctx, tx, locks); err != nil {
			return environmentError(err)
		}
		x, err := a.store.InTx(tx)
		if err != nil || nilPort(x) {
			return fault(f.InvalidArgument)
		}
		if err = a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return environmentError(err)
		}
		d.storedDigest, err = readEnvironmentRetirement(ctx, x, d)
		return err
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
	w := &environmentRetirementWitness{issuer: a, candidate: d, stage: environmentRetirementPlanning}
	w.live.Store(true)
	defer w.live.Store(false)
	private := context.WithValue(ctx, environmentRetirementWitnessKey{}, w)
	for n, r := range requests {
		plan, err := s.leases.DiscoverProjectVariableLeaseRetirement(private, r)
		if err != nil {
			return nil, environmentError(err)
		}
		if nilPort(plan) {
			return nil, fault(f.DependencyUnbound)
		}
		d.leasePlans[n] = plan
		locks, err = mergeEnvironmentLocks(locks, plan.RequiredLocks())
		if err != nil {
			return nil, environmentError(err)
		}
		if err = environmentContext(ctx); err != nil {
			return nil, err
		}
	}
	d.locks = locks
	return environmentRetirementPlan{func() *environmentRetirementCandidate { return d }}, nil
}

func (p *EnvironmentRetirementProvider) retirementInTx(ctx context.Context, tx f.Tx, v c.EnvironmentCapture, plan c.EnvironmentRetirementPlan, observe bool) (bool, error) {
	if err := environmentContext(ctx); err != nil {
		return false, err
	}
	binding, err := c.EnvironmentRetirementBinding(v)
	if err != nil {
		return false, err
	}
	s := p.state()
	if s == nil {
		return false, fault(f.DependencyUnbound)
	}
	pp, ok := plan.(environmentRetirementPlan)
	if !ok || pp.data == nil {
		return false, fault(f.InvalidArgument)
	}
	d := pp.data()
	if d == nil || d.issuer != s || d.binding != binding {
		return false, fault(f.InvalidArgument)
	}
	a := s.authority.state()
	x, err := a.store.InTx(tx)
	if err != nil || nilPort(x) {
		return false, fault(f.InvalidArgument)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, d.locks); err != nil {
		return false, environmentError(err)
	}
	if err = a.owner.CheckEnvironmentRetirementInTx(ctx, tx, v); err != nil {
		return false, environmentError(err)
	}
	if err = environmentContext(ctx); err != nil {
		return false, err
	}
	if _, err = readEnvironmentRetirement(ctx, x, d); err != nil {
		return false, err
	}
	stage := environmentRetirementWriting
	if observe {
		stage = environmentRetirementObserving
	}
	w := &environmentRetirementWitness{issuer: a, candidate: d, tx: tx, stage: stage}
	w.live.Store(true)
	defer w.live.Store(false)
	private := context.WithValue(ctx, environmentRetirementWitnessKey{}, w)
	all := true
	for n, r := range d.requests {
		if observe {
			var retired bool
			retired, err = s.leases.ProjectVariableLeaseRetiredInTx(private, tx, r, d.leasePlans[n])
			all = all && retired
		} else {
			err = s.leases.RetireProjectVariableLeaseInTx(private, tx, r, d.leasePlans[n])
		}
		if canceled := environmentContext(ctx); canceled != nil {
			return false, canceled
		}
		if err != nil {
			return false, environmentError(err)
		}
	}
	return all, environmentContext(ctx)
}
func (p *EnvironmentRetirementProvider) RetireEnvironmentInTx(ctx context.Context, tx f.Tx, v c.EnvironmentCapture, plan c.EnvironmentRetirementPlan) error {
	_, err := p.retirementInTx(ctx, tx, v, plan, false)
	return err
}
func (p *EnvironmentRetirementProvider) EnvironmentRetiredInTx(ctx context.Context, tx f.Tx, v c.EnvironmentCapture, plan c.EnvironmentRetirementPlan) (bool, error) {
	return p.retirementInTx(ctx, tx, v, plan, true)
}

var _ c.EnvironmentRetirement = (*EnvironmentRetirementProvider)(nil)
