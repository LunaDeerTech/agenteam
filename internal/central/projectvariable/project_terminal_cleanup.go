package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectTerminalCleanup owns only this pair's closed Project call history.
// The Project caller owns the transaction containing both domain tails and the
// atomic replacement of its recovery anchor. No D04 state is retired here.
type ProjectTerminalCleanup struct {
	pair      *projectCallOwnershipState
	authority pc.ProjectTerminalCleanupAuthority
	owners    *projectTerminalOwnership
}
type projectTerminalOwnership struct {
	mu    sync.Mutex
	calls map[i.ProjectID]*projectTerminalCall
}
type projectTerminalCall struct {
	request  pc.ProjectTerminalCleanupRequest
	active   bool
	unknown  f.CommitResult
	returned func()
}
type variablesTerminalScopeKey struct{}
type variablesTerminalScope struct {
	owner           *ProjectTerminalCleanup
	request         pc.ProjectTerminalCleanupRequest
	proofs          map[pc.ProjectTerminalOwnerIdentity]bool
	mu              sync.Mutex
	open, validated bool
	tx              f.Tx
	calls           int
	changed         chan struct{}
}

func NewProjectTerminalCleanup(pair *ProjectCallOwnership, authority pc.ProjectTerminalCleanupAuthority) (*ProjectTerminalCleanup, error) {
	if pair == nil || pair.state == nil || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	s := pair.state
	a, b := s.service.state(), s.secrets.state()
	if a == nil || b == nil || a.ownership != s || b.ownership != s || !sameStore(s.store, a.store) || !sameStore(s.store, b.store) {
		return nil, fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	if s.terminal == nil {
		s.terminal = &projectTerminalOwnership{calls: make(map[i.ProjectID]*projectTerminalCall)}
	}
	owners := s.terminal
	s.mu.Unlock()
	return &ProjectTerminalCleanup{pair: s, authority: authority, owners: owners}, nil
}
func (s *ProjectTerminalCleanup) ProjectTerminalCleanupIdentity() pc.ProjectTerminalOwnerIdentity {
	if s == nil || s.pair == nil {
		return pc.ProjectTerminalOwnerIdentity{}
	}
	return pc.ProjectTerminalOwnerIdentity{Kind: pc.ProjectTerminalVariablesOwner, ProcessID: s.pair.process.String(), InstanceID: s.pair.instance.String()}
}
func sameTerminalRequest(a, b pc.ProjectTerminalCleanupRequest) bool {
	return a.Actor.Equal(b.Actor) && a.Cause == b.Cause && a.ProjectID == b.ProjectID && a.Owner == b.Owner
}
func (s *ProjectTerminalCleanup) borrow(r pc.ProjectTerminalCleanupRequest) (*projectTerminalCall, error) {
	s.owners.mu.Lock()
	defer s.owners.mu.Unlock()
	if c := s.owners.calls[r.ProjectID]; c != nil {
		if c.active {
			return nil, fault(f.ResourceBusy)
		}
		if !sameTerminalRequest(c.request, r) {
			return nil, fault(f.InvalidState)
		}
		if !r.Confirmation {
			return nil, txError(c.unknown)
		}
		c.active = true
		return c, nil
	}
	if len(s.owners.calls) >= maxProjectCallOwners {
		return nil, fault(f.ResourceBusy)
	}
	c := &projectTerminalCall{request: r, active: true}
	s.owners.calls[r.ProjectID] = c
	return c, nil
}
func (s *ProjectTerminalCleanup) release(r pc.ProjectTerminalCleanupRequest, c *projectTerminalCall) {
	s.owners.mu.Lock()
	defer s.owners.mu.Unlock()
	c.active = false
	if c.returned == nil {
		delete(s.owners.calls, r.ProjectID)
	}
}

func (s *ProjectTerminalCleanup) WithProjectTerminalCleanup(ctx context.Context, r pc.ProjectTerminalCleanupRequest, fn pc.ProjectTerminalCleanupCall) (f.CommitResult, error) {
	zero := f.NotCommittedResult(nil)
	if ctx == nil || r.Validate() != nil || r.Owner.Kind != pc.ProjectTerminalVariablesOwner || fn == nil {
		return zero, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return zero, canceled(err)
	}
	if s == nil || s.pair == nil || s.owners == nil || nilPort(s.authority) {
		return zero, fault(f.DependencyUnbound)
	}
	if ctx.Value(variablesTerminalScopeKey{}) != nil {
		return zero, fault(f.Forbidden)
	}
	if !r.Confirmation && r.Owner != s.ProjectTerminalCleanupIdentity() {
		return zero, fault(f.Forbidden)
	}
	call, err := s.borrow(r)
	if err != nil {
		return zero, err
	}
	defer s.release(r, call)
	run, _, ordinaryDone, err := s.pair.service.begin(ctx)
	if err != nil {
		return zero, err
	}
	run, _, secretDone, err := s.pair.secrets.begin(run)
	if err != nil {
		ordinaryDone()
		return zero, err
	}
	returned := func() { secretDone(); ordinaryDone() }
	retained := false
	defer func() {
		if !retained {
			returned()
		}
	}()
	proofs, result, err := s.preflight(run, r)
	if err != nil {
		if call.returned != nil {
			return call.unknown, errors.Join(txError(call.unknown), err)
		}
		return result, err
	}
	loan := &variablesTerminalScope{owner: s, request: r, proofs: proofs, open: true, changed: make(chan struct{})}
	defer loan.close()
	result, err = fn(context.WithValue(run, variablesTerminalScopeKey{}, loan))
	loan.close()
	loan.mu.Lock()
	validated := loan.validated
	loan.mu.Unlock()
	if call.returned != nil {
		if result.State() != f.Committed || !validated || err != nil || run.Err() != nil {
			return call.unknown, errors.Join(txError(call.unknown), err, txError(result))
		}
		call.returned()
		call.returned = nil
	} else if result.State() == f.Unknown {
		call.unknown, call.returned = result, returned
		retained = true
		return result, errors.Join(err, txError(result))
	}
	if err != nil {
		return result, err
	}
	if err = txError(result); err != nil {
		return result, err
	}
	if !validated {
		return result, fault(f.DependencyUnavailable)
	}
	if err = run.Err(); err != nil {
		return result, canceled(err)
	}
	return result, nil
}

func (s *variablesTerminalScope) close() {
	s.mu.Lock()
	s.open = false
	for s.calls != 0 {
		changed := s.changed
		s.mu.Unlock()
		<-changed
		s.mu.Lock()
	}
	s.mu.Unlock()
}
func (s *variablesTerminalScope) enter(owner *ProjectTerminalCleanup, r pc.ProjectTerminalCleanupRequest, tx f.Tx) (func(bool), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open || s.owner != owner || !sameTerminalRequest(s.request, r) || s.request.Confirmation != r.Confirmation || s.request.NotApplied != r.NotApplied || s.tx.Valid() && s.tx != tx {
		return nil, fault(f.Forbidden)
	}
	s.tx = tx
	s.calls++
	return func(valid bool) {
		s.mu.Lock()
		s.validated = s.validated || valid
		s.calls--
		close(s.changed)
		s.changed = make(chan struct{})
		s.mu.Unlock()
	}, nil
}

// Every local business return and retained cleanup Unknown still counts. The
// current terminal control is separately owned and does not wait for itself.
func (s *ProjectTerminalCleanup) localPending(project i.ProjectID) bool {
	if s.pair.cleanupPending(project) {
		return true
	}
	s.pair.mu.Lock()
	cleanup := s.pair.cleanup
	s.pair.mu.Unlock()
	if cleanup == nil {
		return false
	}
	cleanup.mu.Lock()
	defer cleanup.mu.Unlock()
	return cleanup.calls[project] != nil
}
func (s *ProjectTerminalCleanup) gate(ctx context.Context, tx f.Tx, r pc.ProjectTerminalCleanupRequest) (postgres.SQLExecutor, error) {
	if _, err := s.pair.store.InTx(tx); err != nil {
		return nil, portError(err)
	}
	if err := s.pair.store.RequireHeldLocks(ctx, tx, r.RequiredLocks()); err != nil {
		return nil, portError(err)
	}
	if err := s.authority.RequireProjectTerminalCleanupInTx(ctx, tx, r); err != nil {
		return nil, portError(err)
	}
	if err := s.pair.proof.RequireCurrentProcessInTx(ctx, tx, s.pair.process); err != nil {
		return nil, portError(err)
	}
	if s.localPending(r.ProjectID) {
		return nil, fault(f.ResourceBusy)
	}
	x, err := s.pair.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	return x, ctx.Err()
}

type terminalLedgerOwner struct {
	Process  string `json:"process_id"`
	Instance string `json:"instance_id"`
}
type terminalLedger struct {
	count  int64
	owners []pc.ProjectTerminalOwnerIdentity
}

func readTerminalLedger(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) (terminalLedger, error) {
	var value terminalLedger
	var running bool
	var raw []byte
	err := x.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_projectvariable.project_calls WHERE project_id=$1),
 EXISTS(SELECT 1 FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND phase<>'business_closed'),
 (SELECT COALESCE(jsonb_agg(jsonb_build_object('process_id',process_id,'instance_id',instance_id) ORDER BY process_id,instance_id),'[]'::jsonb)
 FROM (SELECT DISTINCT process_id,instance_id FROM agenteam_projectvariable.project_calls WHERE project_id=$1 ORDER BY process_id,instance_id) AS owners)`, p.String()).Scan(&value.count, &running, &raw)
	if err != nil {
		return value, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return value, canceled(err)
	}
	if running {
		return value, fault(f.ResourceBusy)
	}
	var owners []terminalLedgerOwner
	if value.count < 0 || json.Unmarshal(raw, &owners) != nil || owners == nil {
		return value, unavailable(nil)
	}
	seen := make(map[pc.ProjectTerminalOwnerIdentity]bool)
	for _, row := range owners {
		id := pc.ProjectTerminalOwnerIdentity{Kind: pc.ProjectTerminalVariablesOwner, ProcessID: row.Process, InstanceID: row.Instance}
		if id.Validate() != nil || seen[id] {
			return value, unavailable(nil)
		}
		seen[id] = true
		value.owners = append(value.owners, id)
	}
	if (value.count == 0) != (len(owners) == 0) {
		return value, unavailable(nil)
	}
	return value, nil
}
func (s *ProjectTerminalCleanup) preflight(ctx context.Context, r pc.ProjectTerminalCleanupRequest) (map[pc.ProjectTerminalOwnerIdentity]bool, f.CommitResult, error) {
	var ledger terminalLedger
	validated := false
	cause, err := f.NewRecoveryCause("projectvariable-terminal-preflight", r.Cause.OperationID.String(), r.ProjectID.String())
	if err != nil {
		return nil, f.NotCommittedResult(nil), portError(err)
	}
	result := s.pair.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.pair.store.AcquireAll(ctx, tx, r.RequiredLocks()); err != nil {
			return portError(err)
		}
		x, err := s.gate(ctx, tx, r)
		if err != nil {
			return err
		}
		ledger, err = readTerminalLedger(ctx, x, r.ProjectID)
		if err != nil {
			return err
		}
		validated = true
		return nil
	})
	if err = txError(result); err != nil {
		return nil, result, err
	}
	if !validated {
		return nil, result, unavailable(nil)
	}
	if err = ctx.Err(); err != nil {
		return nil, result, canceled(err)
	}
	proofs := make(map[pc.ProjectTerminalOwnerIdentity]bool)
	for _, identity := range append(ledger.owners, r.Owner) {
		if proofs[identity] {
			continue
		}
		if identity.ProcessID == s.pair.process.String() {
			if identity != s.ProjectTerminalCleanupIdentity() {
				return nil, result, fault(f.ResourceBusy)
			}
		} else {
			process, e := f.ParseID[oc.Process](identity.ProcessID)
			if e != nil {
				return nil, result, unavailable(e)
			}
			// The preflight Store has actually returned; no Project Tx is held.
			if e = s.pair.proof.ConfirmStopped(ctx, process); e != nil {
				return nil, result, portError(e)
			}
		}
		proofs[identity] = true
	}
	return proofs, result, ctx.Err()
}

func (s *ProjectTerminalCleanup) RetireProjectTerminalInTx(ctx context.Context, tx f.Tx, r pc.ProjectTerminalCleanupRequest) error {
	if r.Confirmation {
		return fault(f.Forbidden)
	}
	return s.inTx(ctx, tx, r, true)
}
func (s *ProjectTerminalCleanup) RequireProjectTerminalRetiredInTx(ctx context.Context, tx f.Tx, r pc.ProjectTerminalCleanupRequest) error {
	if !r.Confirmation || r.NotApplied {
		return fault(f.Forbidden)
	}
	return s.inTx(ctx, tx, r, false)
}
func (s *ProjectTerminalCleanup) RequireProjectTerminalUnappliedInTx(ctx context.Context, tx f.Tx, r pc.ProjectTerminalCleanupRequest) error {
	if !r.Confirmation || !r.NotApplied {
		return fault(f.Forbidden)
	}
	return s.inTx(ctx, tx, r, false)
}
func (s *ProjectTerminalCleanup) inTx(ctx context.Context, tx f.Tx, r pc.ProjectTerminalCleanupRequest, remove bool) (err error) {
	if ctx == nil || !tx.Valid() || r.Validate() != nil || r.Owner.Kind != pc.ProjectTerminalVariablesOwner {
		return fault(f.InvalidArgument)
	}
	if s == nil || s.pair == nil {
		return fault(f.DependencyUnbound)
	}
	loan, _ := ctx.Value(variablesTerminalScopeKey{}).(*variablesTerminalScope)
	if loan == nil {
		return fault(f.Forbidden)
	}
	done, err := loan.enter(s, r, tx)
	if err != nil {
		return err
	}
	valid := false
	defer func() { done(valid) }()
	x, err := s.gate(ctx, tx, r)
	if err != nil {
		return err
	}
	if err = requireProjectCleanupUnreferenced(ctx, x, r.ProjectID); err != nil {
		return err
	}
	empty, err := projectCleanupEmpty(ctx, x, r.ProjectID)
	if err != nil {
		return err
	}
	if !empty {
		return fault(f.ResourceBusy)
	}
	ledger, err := readTerminalLedger(ctx, x, r.ProjectID)
	if err != nil {
		return err
	}
	for _, identity := range append(ledger.owners, r.Owner) {
		if !loan.proofs[identity] {
			return fault(f.ResourceBusy)
		}
	}
	if remove {
		tag, e := x.Exec(ctx, `DELETE FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND phase='business_closed'`, r.ProjectID.String())
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != ledger.count {
			return fault(f.VersionConflict)
		}
	} else if !r.NotApplied && ledger.count != 0 {
		return fault(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return canceled(err)
	}
	valid = true
	return nil
}

var _ pc.ProjectTerminalCleanupDomain = (*ProjectTerminalCleanup)(nil)
