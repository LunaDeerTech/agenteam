package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectTerminalCleanup owns only the original Agent pair's final tail. Its
// callback includes the caller's actual transaction return, not only its body.
type ProjectTerminalCleanup struct {
	pair       *projectCallOwnershipState
	retirement *ProjectRetirement
	authority  pc.ProjectTerminalCleanupAuthority
	owners     *agentTerminalOwners
}
type agentTerminalOwners struct {
	mu    sync.Mutex
	calls map[i.ProjectID]*agentTerminalCall
}
type agentTerminalCall struct {
	req      pc.ProjectTerminalCleanupRequest
	active   bool
	unknown  f.CommitResult
	returned func()
}
type agentTerminalScopeKey struct{}
type agentTerminalScope struct {
	mu            sync.Mutex
	open, checked bool
	owner         *ProjectTerminalCleanup
	req           pc.ProjectTerminalCleanupRequest
	proofs        map[pc.ProjectTerminalOwnerIdentity]bool
}

func NewProjectTerminalCleanup(pair *ProjectCallOwnership, retirement *ProjectRetirement) (*ProjectTerminalCleanup, error) {
	if pair == nil || pair.state == nil || !retirement.bound() {
		return nil, fault(f.DependencyUnbound)
	}
	s := pair.state
	if s.service == nil || s.service.state == nil || s.directory == nil || s.directory.state == nil || s.service.state.ownership != s || s.directory.state.ownership != s || s.service.state.deps.Authority == nil || s.service.state.deps.Authority.state != retirement.agents.state || reflect.ValueOf(s.store) != reflect.ValueOf(retirement.agents.state.store) {
		return nil, fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	if s.terminal == nil {
		s.terminal = &agentTerminalOwners{calls: map[i.ProjectID]*agentTerminalCall{}}
	}
	owners := s.terminal
	s.mu.Unlock()
	return &ProjectTerminalCleanup{pair: s, retirement: retirement, authority: retirement.projects, owners: owners}, nil
}
func (s *ProjectTerminalCleanup) ProjectTerminalCleanupIdentity() pc.ProjectTerminalOwnerIdentity {
	if s == nil || s.pair == nil {
		return pc.ProjectTerminalOwnerIdentity{}
	}
	return pc.ProjectTerminalOwnerIdentity{Kind: pc.ProjectTerminalAgentOwner, ProcessID: s.pair.process.String(), InstanceID: s.pair.instance.String()}
}
func sameTerminalRequest(a, b pc.ProjectTerminalCleanupRequest) bool {
	return a.Actor.Equal(b.Actor) && a.Cause == b.Cause && a.ProjectID == b.ProjectID && a.Owner == b.Owner
}
func (s *ProjectTerminalCleanup) borrow(req pc.ProjectTerminalCleanupRequest) (*agentTerminalCall, error) {
	s.owners.mu.Lock()
	defer s.owners.mu.Unlock()
	if prior := s.owners.calls[req.ProjectID]; prior != nil {
		if prior.active {
			return nil, fault(f.ResourceBusy)
		}
		if !sameTerminalRequest(prior.req, req) || !req.Confirmation {
			return nil, fault(f.InvalidState)
		}
		prior.active = true
		return prior, nil
	}
	if len(s.owners.calls) >= maxProjectCallOwners {
		return nil, fault(f.ResourceBusy)
	}
	call := &agentTerminalCall{req: req, active: true}
	s.owners.calls[req.ProjectID] = call
	return call, nil
}
func (s *ProjectTerminalCleanup) WithProjectTerminalCleanup(ctx context.Context, req pc.ProjectTerminalCleanupRequest, fn pc.ProjectTerminalCleanupCall) (result f.CommitResult, err error) {
	result = f.NotCommittedResult(nil)
	if ctx == nil || fn == nil || req.Validate() != nil || req.Owner.Kind != pc.ProjectTerminalAgentOwner {
		return result, invalid()
	}
	if s == nil || s.pair == nil || s.owners == nil {
		return result, fault(f.DependencyUnbound)
	}
	if !req.Confirmation && req.Owner != s.ProjectTerminalCleanupIdentity() {
		return result, fault(f.Forbidden)
	}
	call, err := s.borrow(req)
	if err != nil {
		return result, err
	}
	defer func() {
		s.owners.mu.Lock()
		call.active = false
		if call.returned == nil {
			delete(s.owners.calls, req.ProjectID)
		}
		s.owners.mu.Unlock()
	}()
	run, doneA, err := s.pair.service.begin(ctx)
	if err != nil {
		return result, err
	}
	run, doneB, err := s.pair.directory.state.calls.begin(run)
	if err != nil {
		doneA()
		return result, err
	}
	done := func() { doneB(); doneA() }
	retain := false
	defer func() {
		if !retain {
			done()
		}
	}()
	if s.pair.terminalPending(req.ProjectID) {
		return result, fault(f.ResourceBusy)
	}
	w := &agentTerminalScope{open: true, owner: s, req: req, proofs: map[pc.ProjectTerminalOwnerIdentity]bool{}}
	defer func() { w.mu.Lock(); w.open = false; w.mu.Unlock() }()
	if err = s.proveOwner(run, req.Owner); err != nil {
		return result, err
	}
	var identities []pc.ProjectTerminalOwnerIdentity
	txCause, _ := f.NewRecoveryCause("agent-terminal-observe", req.Cause.OperationID.String(), req.ProjectID.String())
	validated := false
	result = s.pair.store.WithinTx(run, txCause, func(ctx context.Context, tx f.Tx) error {
		if e := s.pair.store.AcquireAll(ctx, tx, req.RequiredLocks()); e != nil {
			return portError(e)
		}
		if e := s.pair.proof.RequireCurrentProcessInTx(ctx, tx, s.pair.process); e != nil {
			return portError(e)
		}
		if e := s.authority.RequireProjectTerminalCleanupInTx(ctx, tx, req); e != nil {
			return portError(e)
		}
		x, e := s.pair.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		identities, e = agentTerminalIdentities(ctx, x, req.ProjectID)
		if e != nil {
			return e
		}
		validated = true
		return portError(ctx.Err())
	})
	if err = commitError(result); err != nil {
		return result, err
	}
	if !validated {
		return result, unavailable(nil)
	}
	for _, id := range identities {
		if err = s.proveOwner(run, id); err != nil {
			return result, err
		}
		w.proofs[id] = true
	}
	if err = run.Err(); err != nil {
		return result, portError(err)
	}
	result, err = fn(context.WithValue(run, agentTerminalScopeKey{}, w))
	w.mu.Lock()
	checked := w.checked
	w.mu.Unlock()
	if result.State() == f.Unknown {
		if call.returned == nil {
			call.unknown = result
			call.returned = done
			retain = true
		}
		return result, errors.Join(err, commitError(call.unknown))
	}
	if result.State() != f.Committed || err != nil || !checked || run.Err() != nil {
		if call.returned != nil {
			return call.unknown, errors.Join(err, commitError(call.unknown), commitError(result), portError(run.Err()))
		}
		if err == nil {
			err = commitError(result)
		}
		if err == nil {
			err = portError(run.Err())
		}
		if err == nil {
			err = unavailable(nil)
		}
		return result, err
	}
	if call.returned != nil {
		call.returned()
		call.returned = nil
	}
	return result, nil
}
func (s *ProjectTerminalCleanup) proveOwner(ctx context.Context, id pc.ProjectTerminalOwnerIdentity) error {
	if id.Validate() != nil || id.Kind != pc.ProjectTerminalAgentOwner {
		return unavailable(nil)
	}
	if id.ProcessID == s.pair.process.String() {
		if id.InstanceID != s.pair.instance.String() {
			return fault(f.ResourceBusy)
		}
		return nil
	}
	process, err := f.ParseID[oc.Process](id.ProcessID)
	if err != nil {
		return unavailable(err)
	}
	return portError(s.pair.proof.ConfirmStopped(ctx, process))
}
func agentTerminalIdentities(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) ([]pc.ProjectTerminalOwnerIdentity, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('kind','agent','process_id',process_id,'instance_id',instance_id) ORDER BY process_id,instance_id),'[]'::jsonb) FROM (SELECT DISTINCT process_id,instance_id FROM agenteam_agent.project_calls WHERE project_id=$1) AS owners`, p.String()).Scan(&raw)
	if err != nil {
		return nil, unavailable(err)
	}
	var ids []pc.ProjectTerminalOwnerIdentity
	if json.Unmarshal(raw, &ids) != nil || ids == nil {
		return nil, unavailable(nil)
	}
	for n, id := range ids {
		if id.Validate() != nil || id.Kind != pc.ProjectTerminalAgentOwner || n > 0 && (ids[n-1].ProcessID > id.ProcessID || ids[n-1].ProcessID == id.ProcessID && ids[n-1].InstanceID >= id.InstanceID) {
			return nil, unavailable(nil)
		}
	}
	return ids, portError(ctx.Err())
}
func (s *projectCallOwnershipState) terminalPending(p i.ProjectID) bool {
	a, b := s.service.state.calls, s.directory.state.calls
	a.mu.Lock()
	b.mu.Lock()
	s.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	defer s.mu.Unlock()
	for _, set := range []*callSet{a, b} {
		for call := range set.active {
			if call.project == p {
				return true
			}
		}
	}
	for _, owner := range s.owners {
		if owner.record.Project == p {
			return true
		}
	}
	return false
}
func (s *ProjectTerminalCleanup) terminalInTx(ctx context.Context, tx f.Tx, req pc.ProjectTerminalCleanupRequest, confirmation bool) error {
	if ctx == nil || req.Validate() != nil || req.Confirmation != confirmation {
		return invalid()
	}
	w, _ := ctx.Value(agentTerminalScopeKey{}).(*agentTerminalScope)
	if w == nil || w.owner != s || !sameTerminalRequest(w.req, req) || w.req.Confirmation != confirmation || w.req.NotApplied != req.NotApplied {
		return fault(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open || w.checked {
		return fault(f.Forbidden)
	}
	if err := s.pair.store.RequireHeldLocks(ctx, tx, req.RequiredLocks()); err != nil {
		return portError(err)
	}
	if err := s.pair.proof.RequireCurrentProcessInTx(ctx, tx, s.pair.process); err != nil {
		return portError(err)
	}
	if err := s.authority.RequireProjectTerminalCleanupInTx(ctx, tx, req); err != nil {
		return portError(err)
	}
	x, err := s.pair.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if s.pair.terminalPending(req.ProjectID) {
		return fault(f.ResourceBusy)
	}
	ids, err := agentTerminalIdentities(ctx, x, req.ProjectID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !w.proofs[id] {
			return fault(f.ResourceBusy)
		}
	}
	var blocked bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_agent.agents WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_agent.tool_allowlist WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_agent.mount_allowlist WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_agent.secret_allowlist WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_agent.commands WHERE project_id=$1 AND state<>'completed') OR EXISTS(SELECT 1 FROM agenteam_agent.project_calls WHERE project_id=$1 AND phase<>'joined')`, req.ProjectID.String()).Scan(&blocked)
	if err != nil {
		return unavailable(err)
	}
	if blocked {
		return fault(f.ResourceBusy)
	}
	for _, table := range []string{"commands", "project_calls"} {
		var count int64
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_agent.`+table+` WHERE project_id=$1`, req.ProjectID.String()).Scan(&count); err != nil {
			return unavailable(err)
		}
		if count < 0 {
			return unavailable(nil)
		}
		if confirmation {
			if !req.NotApplied && count != 0 {
				return fault(f.ResourceBusy)
			}
			continue
		}
		if err = ctx.Err(); err != nil {
			return portError(err)
		}
		tag, e := x.Exec(ctx, `DELETE FROM agenteam_agent.`+table+` WHERE project_id=$1`, req.ProjectID.String())
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != count {
			return fault(f.VersionConflict)
		}
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	w.checked = true
	return nil
}
func (s *ProjectTerminalCleanup) RetireProjectTerminalInTx(ctx context.Context, tx f.Tx, req pc.ProjectTerminalCleanupRequest) error {
	if req.NotApplied {
		return invalid()
	}
	return s.terminalInTx(ctx, tx, req, false)
}
func (s *ProjectTerminalCleanup) RequireProjectTerminalRetiredInTx(ctx context.Context, tx f.Tx, req pc.ProjectTerminalCleanupRequest) error {
	if req.NotApplied {
		return invalid()
	}
	return s.terminalInTx(ctx, tx, req, true)
}

var _ c.ProjectTerminalCleanupDomain = (*ProjectTerminalCleanup)(nil)

func (s *ProjectTerminalCleanup) RequireProjectTerminalUnappliedInTx(ctx context.Context, tx f.Tx, req pc.ProjectTerminalCleanupRequest) error {
	if !req.Confirmation || !req.NotApplied {
		return invalid()
	}
	return s.terminalInTx(ctx, tx, req, true)
}
