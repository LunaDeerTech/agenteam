package mcp

import (
	"context"
	"slices"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// ExecutionRetirement participates in the original Execution terminal
// transaction. It never creates a terminal fact or drives a network request.
type ExecutionRetirement struct {
	service   *Service
	authority tc.MCPExecutionRetirementAuthority
	observer  sc.MCPLeaseRetirementObservation
}

func NewExecutionRetirement(service *Service, authority tc.MCPExecutionRetirementAuthority) (*ExecutionRetirement, error) {
	if service == nil || nilPort(authority) || nilPort(service.options.Secret) {
		return nil, fail(f.DependencyUnbound)
	}
	observer, ok := service.options.Secret.(sc.MCPLeaseRetirementObservation)
	if !ok || nilPort(observer) {
		return nil, fail(f.DependencyUnbound)
	}
	return &ExecutionRetirement{service: service, authority: authority, observer: observer}, nil
}

type executionRetirementPlan struct {
	owner   *ExecutionRetirement
	request tc.MCPExecutionRetirementRequest
	rows    []executionRuntimeLease
	entries []*executionRetirementEntry
	locks   []f.LockRequest
}

func (p *executionRetirementPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type executionRetirementEntry struct {
	usage   sc.UsageRequest
	secret  sc.UsageDependencies
	mapping f.Digest
}

type executionRetirementKey struct{}
type executionRetirementUse struct {
	plan      *executionRetirementPlan
	entry     *executionRetirementEntry
	mu        sync.Mutex
	active    bool
	validated f.Tx
}

func (w *executionRetirementUse) close() { w.mu.Lock(); w.active = false; w.mu.Unlock() }

func retirementLocks(r tc.MCPExecutionRetirementRequest) ([]f.LockRequest, error) {
	if r.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	project, _ := f.ProjectLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	execution, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	return mergeLocks([]f.LockRequest{{Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Shared}, {Key: execution, Mode: f.Exclusive}, registryLock(f.Shared)})
}

func (p *ExecutionRetirement) DiscoverMCPExecutionRetirement(ctx context.Context, request tc.MCPExecutionRetirementRequest) (tc.MCPExecutionRetirementPlan, error) {
	if p == nil || p.service == nil || nilPort(p.authority) {
		return nil, fail(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.authority.CheckMCPRetirementPlan(ctx, request); err != nil {
		return nil, safeError(err)
	}
	locks, err := retirementLocks(request)
	if err != nil {
		return nil, err
	}
	plan := &executionRetirementPlan{owner: p, request: request, locks: locks}
	cause, _ := f.NewRecoveryCause("mcp.execution.retire.discover", request.ExecutionID.String(), "")
	err = commit(p.service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := p.service.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := p.service.store.InTx(tx)
		if err != nil {
			return err
		}
		plan.rows, err = loadExecutionRuntimeLeases(ctx, x, request.ProjectID, request.ExecutionID)
		return err
	}))
	if err != nil {
		return nil, err
	}
	seen := map[sc.LeaseID]bool{}
	for _, row := range plan.rows {
		if row.CredentialID == "" || seen[row.LeaseID] {
			continue
		}
		seen[row.LeaseID] = true
		entry, err := newExecutionRetirementEntry(request, plan.rows, row)
		if err != nil {
			return nil, err
		}
		plan.entries = append(plan.entries, entry)
		w := &executionRetirementUse{plan: plan, entry: entry, active: true}
		entry.secret, err = p.service.options.Secret.DiscoverUsage(context.WithValue(ctx, executionRetirementKey{}, w), entry.usage)
		w.close()
		if err != nil {
			return nil, safeError(err)
		}
		binding, _ := sc.UsageBinding(entry.usage)
		if entry.secret.Validate() != nil || !entry.secret.ProviderPlan().Matches(p.service.secretIssuer, binding, entry.mapping) {
			return nil, fail(f.Forbidden)
		}
		plan.locks, err = mergeLocks(plan.locks, entry.secret.RequiredLocks())
		if err != nil {
			return nil, err
		}
	}
	return plan, ctx.Err()
}

func newExecutionRetirementEntry(request tc.MCPExecutionRetirementRequest, rows []executionRuntimeLease, row executionRuntimeLease) (*executionRetirementEntry, error) {
	credential, err := f.ParseID[sc.Credential](row.CredentialID)
	if err != nil || row.LeaseID.Validate() != nil {
		return nil, fail(f.InvalidState)
	}
	scope, _ := id.InProject(request.ProjectID)
	ref, _ := sc.NewCredentialRef(credential, scope)
	owner, _ := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, request.ExecutionID.String())
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(request.ExecutionID.String(), scope)
	if err != nil {
		return nil, err
	}
	usage := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: row.LeaseID, Action: sc.ReleaseLeaseUsage}
	binding, err := sc.UsageBinding(usage)
	if err != nil {
		return nil, err
	}
	// Retired is mutable confirmation state and cannot alter the immutable plan
	// identity when the original transaction's outcome needs to be observed.
	identities := slices.Clone(rows)
	for n := range identities {
		identities[n].Retired = false
	}
	raw, err := encode(struct {
		Project, Agent, Execution string
		Version                   f.Version
		Rows                      []executionRuntimeLease
		Usage                     f.Digest
	}{request.ProjectID.String(), request.AgentID.String(), request.ExecutionID.String(), request.TerminalVersion, identities, binding})
	if err != nil {
		return nil, err
	}
	return &executionRetirementEntry{usage: usage, mapping: f.Digest(digest(raw))}, nil
}

func (p *ExecutionRetirement) requireInTx(ctx context.Context, tx f.Tx, request tc.MCPExecutionRetirementRequest, supplied tc.MCPExecutionRetirementPlan) (*executionRetirementPlan, []executionRuntimeLease, error) {
	if p == nil || p.service == nil || ctx == nil {
		return nil, nil, fail(f.DependencyUnbound)
	}
	plan, ok := supplied.(*executionRetirementPlan)
	if !ok || plan == nil || plan.owner != p || request.Validate() != nil || plan.request != request {
		return nil, nil, fail(f.Forbidden)
	}
	if err := p.service.store.RequireHeldLocks(ctx, tx, plan.locks); err != nil {
		return nil, nil, safeError(err)
	}
	if err := p.authority.CheckMCPRetirementInTx(ctx, tx, request); err != nil {
		return nil, nil, safeError(err)
	}
	x, err := p.service.store.InTx(tx)
	if err != nil {
		return nil, nil, safeError(err)
	}
	rows, err := loadExecutionRuntimeLeases(ctx, x, request.ProjectID, request.ExecutionID)
	if err != nil {
		return nil, nil, err
	}
	if !sameRetirementLeaseSet(plan.rows, rows) {
		return nil, nil, fail(f.ConfirmationStale)
	}
	return plan, rows, ctx.Err()
}

func (p *ExecutionRetirement) ReleaseMCPExecutionInTx(ctx context.Context, tx f.Tx, request tc.MCPExecutionRetirementRequest, supplied tc.MCPExecutionRetirementPlan) error {
	plan, _, err := p.requireInTx(ctx, tx, request, supplied)
	if err != nil {
		return err
	}
	for _, entry := range plan.entries {
		w := &executionRetirementUse{plan: plan, entry: entry, active: true}
		result, err := p.service.options.Secret.ApplyUsageInTx(context.WithValue(ctx, executionRetirementKey{}, w), tx, entry.usage, entry.secret)
		w.close()
		if err != nil {
			return safeError(err)
		}
		if result.Action() != sc.ReleaseLeaseUsage {
			return fail(f.Forbidden)
		}
	}
	x, err := p.service.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	_, err = x.Exec(ctx, `UPDATE agenteam_mcp.runtime_leases SET retired=true,retired_at=clock_timestamp() WHERE execution_id=$1 AND project_id=$2 AND NOT retired`, request.ExecutionID.String(), request.ProjectID.String())
	if err != nil {
		return safeError(err)
	}
	return ctx.Err()
}

func (p *ExecutionRetirement) MCPExecutionRetiredInTx(ctx context.Context, tx f.Tx, request tc.MCPExecutionRetirementRequest, supplied tc.MCPExecutionRetirementPlan) (bool, error) {
	plan, rows, err := p.requireInTx(ctx, tx, request, supplied)
	if err != nil {
		return false, err
	}
	released := true
	for _, row := range rows {
		released = released && row.Retired
	}
	for _, entry := range plan.entries {
		w := &executionRetirementUse{plan: plan, entry: entry, active: true}
		observed, err := p.observer.MCPExecutionLeaseReleasedInTx(context.WithValue(ctx, executionRetirementKey{}, w), tx, entry.usage, entry.secret)
		w.close()
		if err != nil {
			return false, safeError(err)
		}
		released = released && observed
	}
	return released, ctx.Err()
}

func (s *Service) executionRetirementUse(ctx context.Context, r sc.UsageRequest) (*executionRetirementUse, error) {
	if ctx == nil || s == nil || r.Validate() != nil || r.Purpose != sc.MCP || r.Action != sc.ReleaseLeaseUsage {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(executionRetirementKey{}).(*executionRetirementUse)
	if !ok || w == nil || w.plan == nil || w.plan.owner == nil || w.plan.owner.service != s || w.entry == nil || !slices.Contains(w.plan.entries, w.entry) {
		return nil, fail(f.Forbidden)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	got, err := sc.UsageBinding(r)
	want, e := sc.UsageBinding(w.entry.usage)
	if !active || err != nil || e != nil || got != want {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (s *Service) discoverExecutionRetirementSecret(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := s.executionRetirementUse(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	if err = w.plan.owner.authority.CheckMCPRetirementPlan(ctx, w.plan.request); err != nil {
		return sc.UsageDependencies{}, safeError(err)
	}
	binding, _ := sc.UsageBinding(r)
	locks, err := retirementLocks(w.plan.request)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.entry.mapping, locks)
}

func (s *Service) validateExecutionRetirementSecret(ctx context.Context, tx f.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	w, err := s.executionRetirementUse(ctx, r)
	if err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !d.Matches(s.secretIssuer, binding, w.entry.mapping) {
		return fail(f.Forbidden)
	}
	if _, _, err = w.plan.owner.requireInTx(ctx, tx, w.plan.request, w.plan); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return fail(f.Forbidden)
	}
	w.validated = tx
	return ctx.Err()
}

func (s *Service) authorizeExecutionRetirementSecret(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	w, ok := ctx.Value(executionRetirementKey{}).(*executionRetirementUse)
	if !ok || w == nil || w.entry == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.executionRetirementUse(ctx, w.entry.usage); err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := tx.Valid() && w.active && w.validated == tx
	w.mu.Unlock()
	if !valid || action != sc.ReleaseLease || !actor.Equal(w.entry.usage.Actor) || !ref.Equal(w.entry.usage.Ref) || !owner.Equal(w.entry.usage.LeaseOwner) {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.MCP}, nil
}

var _ tc.MCPExecutionRetirement = (*ExecutionRetirement)(nil)
