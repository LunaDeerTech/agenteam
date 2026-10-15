package skill

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// Constructible before the ingress/outbox/audit services to break composition
// cycles. This instance never installs a production default or starts work.
type AssignmentAuthority struct {
	owner  *Authority
	events sc.AssignmentEvents
	issuer ob.PlanIssuer
}

func NewAssignmentAuthority(owner *Authority, events sc.AssignmentEvents) (*AssignmentAuthority, error) {
	if owner.state() == nil || !events.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	return &AssignmentAuthority{owner, events, ob.NewPlanIssuer()}, nil
}
func assignmentChangeLocks(c sc.RuntimeAssignmentChange) []f.LockRequest {
	schedule, _ := f.ProjectScheduleLock(c.ProjectID.String())
	agent, _ := f.AgentLock(c.AgentID.String())
	return []f.LockRequest{projectLock(c.ProjectID, f.Shared), {Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Exclusive}, skillLock(c.Binding.SkillID, f.Shared)}
}
func (a *AssignmentAuthority) changeInTx(ctx context.Context, tx f.Tx, c sc.RuntimeAssignmentChange) (postgres.SQLExecutor, *assignmentCommand, error) {
	if a == nil || a.owner.state() == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || c.Validate() != nil {
		return nil, nil, invalid()
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	store := a.owner.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return nil, nil, captureError(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, assignmentChangeLocks(c)); err != nil {
		return nil, nil, captureError(err)
	}
	var raw []byte
	err = x.QueryRow(ctx, `SELECT runtime_data FROM agenteam_skill.assignment_changes WHERE id=$1 AND project_id=$2 AND agent_id=$3 AND assignment_sequence=$4 AND agent_version=$5 AND created_at=$6`, c.ID.String(), c.ProjectID.String(), c.AgentID.String(), int64(c.AssignmentSequence), int64(c.AgentVersion), c.RecordedAt.Time()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fault(f.Forbidden)
	}
	if err != nil {
		return nil, nil, unavailable(err)
	}
	_, expected, err := captureDigest(c)
	if err != nil || !bytes.Equal(raw, expected) {
		return nil, nil, fault(f.Forbidden)
	}
	r, err := loadAssignmentByID(ctx, x, c.ProjectID, c.AgentID, c.ID)
	if err != nil {
		return nil, nil, err
	}
	if r.Candidate.Runtime == nil || *r.Candidate.Runtime != c || r.Candidate.Revision == nil {
		return nil, nil, fault(f.Forbidden)
	}
	return x, r, ctx.Err()
}
func (a *AssignmentAuthority) RequireAssignmentChangeInTx(ctx context.Context, tx f.Tx, c sc.RuntimeAssignmentChange) error {
	_, _, err := a.changeInTx(ctx, tx, c)
	return err
}
func (a *AssignmentAuthority) ProtectAssignmentChangeInTx(ctx context.Context, tx f.Tx, c sc.RuntimeAssignmentChange, execution i.ExecutionID) error {
	if execution.Validate() != nil {
		return invalid()
	}
	x, r, err := a.changeInTx(ctx, tx, c)
	if err != nil {
		return err
	}
	var e, p, agent, assignment, skill, revision, object string
	var sequence, number int64
	err = x.QueryRow(ctx, `SELECT execution_id::text,project_id::text,agent_id::text,assignment_id::text,assignment_sequence,skill_id::text,revision_id::text,revision,object_id::text FROM agenteam_skill.assignment_pending_refs WHERE change_id=$1`, c.ID.String()).Scan(&e, &p, &agent, &assignment, &sequence, &skill, &revision, &number, &object)
	if err == nil {
		if e != execution.String() || p != c.ProjectID.String() || agent != c.AgentID.String() || assignment != c.Binding.AssignmentID.String() || sequence != int64(c.AssignmentSequence) || skill != c.Binding.SkillID.String() || revision != c.Binding.RevisionID.String() || number != int64(c.Binding.Revision) || object != r.Candidate.Revision.ObjectID.String() {
			return fault(f.Forbidden)
		}
		return ctx.Err()
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return unavailable(err)
	}
	// New protection is possible only while the original Skill final writer is
	// active. Historical replay checks the existing reference and cannot rebind.
	if _, err = a.requireWitness(ctx, tx, r); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.assignment_pending_refs(change_id,project_id,agent_id,execution_id,assignment_id,assignment_sequence,skill_id,revision_id,revision,object_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.ID.String(), c.ProjectID.String(), c.AgentID.String(), execution.String(), c.Binding.AssignmentID.String(), int64(c.AssignmentSequence), c.Binding.SkillID.String(), c.Binding.RevisionID.String(), int64(c.Binding.Revision), r.Candidate.Revision.ObjectID.String())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}

type assignmentWitnessKey struct{}
type assignmentWriter struct {
	owner  *AssignmentAuthority
	tx     f.Tx
	id     sc.AssignmentChangeID
	digest f.Digest
	active atomic.Bool
	ctx    context.Context
}

func (a *AssignmentAuthority) witness(ctx context.Context, tx f.Tx, r *assignmentCommand) *assignmentWriter {
	w := &assignmentWriter{owner: a, tx: tx, id: r.ID, digest: r.Digest}
	w.active.Store(true)
	w.ctx = context.WithValue(ctx, assignmentWitnessKey{}, w)
	return w
}
func (w *assignmentWriter) expire() { w.active.Store(false) }
func (a *AssignmentAuthority) requireWitness(ctx context.Context, tx f.Tx, r *assignmentCommand) (postgres.SQLExecutor, error) {
	w, ok := ctx.Value(assignmentWitnessKey{}).(*assignmentWriter)
	if !ok || w == nil || w.owner != a || w.tx != tx || !w.active.Load() || w.id != r.ID || w.digest != r.Digest {
		return nil, fault(f.Forbidden)
	}
	if r.Input.Run != nil {
		if err := requireAssignmentRun(ctx, tx, r.Input, i.Mutate); err != nil {
			return nil, err
		}
	}
	store := a.owner.state().store
	if err := store.RequireHeldLocks(ctx, tx, append(r.Input.agentRequest(ctx).RequiredLocks(), skillLock(r.Candidate.Skill, f.Shared))); err != nil {
		return nil, captureError(err)
	}
	x, err := store.InTx(tx)
	if err != nil {
		return nil, captureError(err)
	}
	actual, err := loadAssignmentCommand(ctx, x, r.Input)
	if err != nil {
		return nil, err
	}
	if actual == nil || actual.State != "planned" || !actual.Candidate.Changed || !sameAssignmentCandidate(actual.Candidate, r.Candidate) {
		return nil, fault(f.Forbidden)
	}
	state, err := loadAssignmentSet(ctx, x, r.Input.Request.ProjectID, r.Input.Request.AgentID)
	if err != nil {
		return nil, err
	}
	d, _, err := captureDigest(state)
	if err != nil {
		return nil, err
	}
	wanted, _, err := captureDigest(r.Candidate.After)
	if err != nil || d != wanted {
		return nil, fault(f.Forbidden)
	}
	return x, ctx.Err()
}

var _ sc.AssignmentChangeAuthority = (*AssignmentAuthority)(nil)
