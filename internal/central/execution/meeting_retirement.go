package execution

import (
	"context"
	"encoding/json"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// MeetingExecutionRetirement borrows the original four-owner graph. It neither
// retires an Execution nor stops a shared owner, and never creates a new one.
type MeetingExecutionRetirement struct {
	original  *projectCallState
	authority ec.MeetingExecutionRetirementAuthority
}

func NewMeetingExecutionRetirement(original *ProjectCallOwnership, authority ec.MeetingExecutionRetirementAuthority) (*MeetingExecutionRetirement, error) {
	if original == nil || original.state == nil || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	s := original.state
	if nilPort(s.store) || nilPort(s.proof) || s.process.Validate() != nil || s.instance.Validate() != nil || s.preparation == nil || s.direct == nil || s.associated == nil || s.preparation.state == nil || s.direct.state == nil || s.associated.state == nil || s.preparation.state.projectCalls != s || s.direct.state.projectCalls != s || s.associated.state.projectCalls != s {
		return nil, fault(f.DependencyUnbound)
	}
	return &MeetingExecutionRetirement{original: s, authority: authority}, nil
}

func (r *MeetingExecutionRetirement) RequireMeetingExecutionRetiredInTx(ctx context.Context, tx f.Tx, project i.ProjectID, m ec.AssociatedMeeting) error {
	if r == nil || r.original == nil || nilPort(r.authority) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return invalid()
	}
	locks, err := ec.MeetingExecutionRetirementLocks(project, m)
	if err != nil {
		return err
	}
	s := r.original
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err = r.authority.RequireBindingInTx(ctx, tx, s.store); err != nil {
		return portError(err)
	}
	if err = r.authority.RequireMeetingDeletionInTx(ctx, tx, project, m); err != nil {
		return portError(err)
	}
	if err = s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
		return portError(err)
	}
	key := ec.LaunchLookupKey{ProjectID: project, AgentID: m.AgentID, IdempotencyKey: m.Key}
	row, err := loadLaunch(ctx, x, key)
	if err != nil {
		return err
	}
	if row == nil {
		return fault(f.NotFound)
	}
	identity := associatedIdentity{AssociatedDispatch: ec.AssociatedDispatch{ExecutionID: m.ExecutionID, AgentID: m.AgentID, Key: m.Key, Digest: m.Digest}, meeting: m}
	if row.digest != m.Digest || row.summary.ID != m.ExecutionID || row.summary.ProjectID != project || row.summary.AgentID != m.AgentID || !identity.matchesLaunch(row.launch) {
		return fault(f.ConfirmationStale)
	}
	if !row.summary.Status.Terminal() || row.summary.CompletedAt == nil {
		return fault(f.ResourceBusy)
	}
	if !r.localReturned(project, m) {
		return fault(f.ResourceBusy)
	}
	claim, err := loadPreparationClaim(ctx, x, m.ExecutionID)
	if err != nil {
		return err
	}
	input, err := loadPreparationInput(ctx, x, m.ExecutionID)
	if err != nil {
		return err
	}
	stored, err := loadDirectText(ctx, x, m.ExecutionID)
	if err != nil {
		return err
	}
	if err = meetingRetirementTerminal(row, claim, input, stored); err != nil {
		return err
	}
	owners, err := meetingRetirementOwners(ctx, x, project, m)
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		// A legacy/missing call ledger cannot prove a returned physical owner.
		return fault(f.ResourceBusy)
	}
	if err = r.requireOwnersReturned(ctx, owners, claim, stored); err != nil {
		return err
	}
	// Database closure precedes the worker's returned hook. Recheck the actual
	// registries after every provider observation; a closed row alone is not it.
	if !r.localReturned(project, m) {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

func meetingRetirementTerminal(row *executionRecord, claim *preparationClaim, input *preparationInputRecord, stored *directTextRecord) error {
	if row == nil || !row.summary.Status.Terminal() || row.summary.CompletedAt == nil || row.summary.Version < 2 {
		return fault(f.ResourceBusy)
	}
	request := ec.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}
	if claim != nil && (claim.execution != row.summary.ID || claim.project != row.summary.ProjectID || claim.agent != row.summary.AgentID || claim.phase != "terminal") {
		return fault(f.ResourceBusy)
	}
	if input != nil {
		if claim == nil || !input.input.Fields().Request.Equal(request) {
			return fault(f.ConfirmationStale)
		}
		expected := *claim
		expected.phase = input.claim.phase
		if expected != input.claim {
			return fault(f.ConfirmationStale)
		}
	}
	if row.summary.SnapshotID == nil {
		// Only commitPreparationCancellation can close this shape. When there
		// is captured input, it releases Model/Environment/MCP in that same Tx;
		// without input, the atomic capture never acquired those resources.
		if row.summary.Status != ec.Cancelled || row.summary.CancelRequestedAt == nil || row.summary.StartedAt != nil || stored != nil {
			return fault(f.ResourceBusy)
		}
		return nil
	}
	if claim == nil || input == nil || stored == nil || stored.terminal == nil || row.summary.StartedAt == nil {
		return fault(f.ResourceBusy)
	}
	snapshot, terminal := stored.snapshot.Fields(), stored.terminal
	if !snapshot.Context.Input().Fields().Request.Equal(request) || snapshot.Context.InputDigest() != input.input.Digest() || snapshot.ID != *row.summary.SnapshotID || snapshot.CreatedAt != *row.summary.StartedAt || terminal.status != row.summary.Status || terminal.version != row.summary.Version || terminal.at != *row.summary.CompletedAt {
		return fault(f.ConfirmationStale)
	}
	// loadDirectText has validated the complete canonical snapshot/round and
	// terminal. commitTerminal persists it only after allJoined and the exact
	// Loop/Model owner returned, atomically with Model/Environment/MCP/Mount
	// retirement. Meeting receives no permission to read those private tables.
	return nil
}

func (r *MeetingExecutionRetirement) localReturned(project i.ProjectID, m ec.AssociatedMeeting) bool {
	s := r.original
	s.mu.Lock()
	busy := false
	for _, entry := range s.owners {
		if entry.record.Project == project && (entry.execution == m.ExecutionID || entry.launchKey.ProjectID == project && entry.launchKey.AgentID == m.AgentID && entry.launchKey.IdempotencyKey == m.Key) {
			busy = true
			break
		}
	}
	s.mu.Unlock()
	if busy {
		return false
	}
	p, d, a := s.preparation.state, s.direct.state, s.associated.state
	p.mu.Lock()
	busy = p.calls[m.ExecutionID] != nil
	p.mu.Unlock()
	d.mu.Lock()
	busy = busy || d.calls[m.ExecutionID] != nil
	d.mu.Unlock()
	a.mu.Lock()
	busy = busy || a.jobs[m.ExecutionID] != nil || a.admissions[m.ExecutionID] != nil
	a.mu.Unlock()
	return !busy
}

type meetingRetirementOwner struct {
	Process     oc.ProcessID              `json:"process"`
	Instance    f.ID[projectCallInstance] `json:"instance"`
	Running     bool                      `json:"running"`
	Launch      bool                      `json:"launch"`
	Preparation bool                      `json:"preparation"`
	Direct      bool                      `json:"direct"`
}

func meetingRetirementOwners(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, m ec.AssociatedMeeting) ([]meetingRetirementOwner, error) {
	var raw []byte
	// Aggregate within this exact E/launch scope, not a Project-wide history
	// download. Extra matching history can only delay retirement, never grant it.
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('process',process_id,'instance',instance_id,'running',running,'launch',launch,'preparation',preparation,'direct',direct) ORDER BY process_id,instance_id),'[]'::jsonb) FROM (
 SELECT process_id,instance_id,bool_or(phase<>'business_closed') AS running,
 bool_or(lane='service' AND invocation_identity::jsonb->'invocation'->>'command'='launch') AS launch,
 bool_or(lane='preparation') AS preparation,bool_or(lane='direct') AS direct
 FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND (
 (invocation_identity::jsonb->'invocation'->'owner_ids') ? $2::text OR (
 lane='service' AND invocation_identity::jsonb->'invocation'->>'key'=$3::text
 AND invocation_identity::jsonb->>'request_digest'=$4::text
 AND invocation_identity::jsonb->'invocation'->'owner_ids'->>1=$5::text))
 GROUP BY process_id,instance_id ORDER BY process_id,instance_id LIMIT 129) owners`, project.String(), m.ExecutionID.String(), string(m.Key), string(m.Digest), m.AgentID.String()).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	var owners []meetingRetirementOwner
	if json.Unmarshal(raw, &owners) != nil || owners == nil {
		return nil, unavailable(nil)
	}
	if len(owners) > 128 {
		return nil, fault(f.ResourceBusy)
	}
	for _, owner := range owners {
		if owner.Process.Validate() != nil || owner.Instance.Validate() != nil {
			return nil, unavailable(nil)
		}
	}
	return owners, ctx.Err()
}

func (r *MeetingExecutionRetirement) requireOwnersReturned(ctx context.Context, owners []meetingRetirementOwner, claim *preparationClaim, stored *directTextRecord) error {
	s := r.original
	launch, preparation, direct := false, claim == nil, stored == nil
	confirmed := map[oc.ProcessID]bool{}
	for _, owner := range owners {
		launch = launch || owner.Launch
		preparation = preparation || claim != nil && owner.Process.String() == claim.process.String() && owner.Preparation
		direct = direct || stored != nil && owner.Process.String() == stored.snapshot.Fields().ProcessID.String() && owner.Direct
		if owner.Process == s.process {
			if owner.Instance != s.instance || owner.Running {
				return fault(f.ResourceBusy)
			}
		} else if !confirmed[owner.Process] {
			// Exact ProcessGuard death, not a stale database phase, proves that
			// an old process has no physical return tail left to join.
			if err := s.proof.ConfirmStopped(ctx, owner.Process); err != nil {
				return portError(err)
			}
			confirmed[owner.Process] = true
		}
	}
	if !launch || !preparation || !direct {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

var _ ec.MeetingExecutionRetirementReader = (*MeetingExecutionRetirement)(nil)
