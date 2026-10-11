package execution

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"
	"sync/atomic"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type roundInputPlan struct {
	owner     *RoundInputDeterminer
	execution i.ExecutionID
	next      c.RoundID
	locks     []f.LockRequest
	source    *roundInputSource
	cutoff    f.Version
	pending   []sc.RuntimeAssignmentChange
	initial   sc.InitialRoundBindingsPlan
	replay    c.RoundInput
}

func (p *roundInputPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*roundInputPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_input_plan")
}
func (*roundInputPlan) LogValue() slog.Value { return slog.StringValue("execution_round_input_plan") }
func (*roundInputPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_input_plan"`), nil
}

func roundInputLocks(p i.ProjectID, a i.AgentID, e i.ExecutionID) []f.LockRequest {
	schedule, _ := f.ProjectScheduleLock(p.String())
	return []f.LockRequest{projectLock(p), {Key: schedule, Mode: f.Exclusive}, agentLock(a, f.Exclusive), executionLock(e)}
}
func (d *RoundInputDeterminer) roundReadTx(ctx context.Context, kind string, e i.ExecutionID, next c.RoundID, locks []f.LockRequest, fn func(context.Context, f.Tx) error) error {
	cause, err := f.NewRecoveryCause("execution.round-input."+kind, e.String(), next.String())
	if err != nil {
		return err
	}
	calls, completed := 0, false
	result := d.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fault(f.InvalidState)
		}
		if err := d.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := d.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if !completed || calls != 1 {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}
func (d *RoundInputDeterminer) roundScope(ctx context.Context, e i.ExecutionID, next c.RoundID) (*executionRecord, error) {
	var row *executionRecord
	err := d.roundReadTx(ctx, "scope", e, next, []f.LockRequest{executionLock(e)}, func(ctx context.Context, tx f.Tx) error {
		x, err := d.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err = loadExecution(ctx, x, e)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		return nil
	})
	return row, err
}
func (d *RoundInputDeterminer) discoverRoundInput(ctx context.Context, e i.ExecutionID, next c.RoundID) (*roundInputPlan, error) {
	scope, err := d.roundScope(ctx, e, next)
	if err != nil {
		return nil, err
	}
	p := &roundInputPlan{owner: d, execution: e, next: next, locks: roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e)}
	err = d.roundReadTx(ctx, "discover", e, next, p.locks, func(ctx context.Context, tx f.Tx) error {
		x, err := d.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		previous, found, err := loadRoundInput(ctx, x, e)
		if err != nil {
			return err
		}
		if found {
			if previous.Fields().RoundID != next {
				return fault(f.ConfirmationStale)
			}
			p.replay = previous
			return nil
		}
		source, err := loadRoundInputSource(ctx, x, e)
		if err != nil {
			return err
		}
		if source.row.summary.ProjectID != scope.summary.ProjectID || source.row.summary.AgentID != scope.summary.AgentID || source.original.round.Fields().ID == next {
			return fault(f.ConfirmationStale)
		}
		if err = d.requireRoundCurrent(ctx, tx, source, p.locks); err != nil {
			return err
		}
		p.cutoff, err = d.deps.Assignments.ReadAssignmentSequenceInTx(ctx, tx, scope.summary.ProjectID, scope.summary.AgentID)
		if err != nil {
			return portError(err)
		}
		if p.cutoff < source.initial.Initial.AssignmentSequence {
			return fault(f.ConfirmationStale)
		}
		p.pending, err = loadRoundPending(ctx, x, e, scope.summary.ProjectID, scope.summary.AgentID)
		if err != nil {
			return err
		}
		for _, change := range p.pending {
			if change.AssignmentSequence > p.cutoff {
				return fault(f.ConfirmationStale)
			}
		}
		p.source = source
		return nil
	})
	if err != nil {
		return nil, err
	}
	if p.replay.Validate() == nil {
		return p, nil
	}
	p.initial, err = d.deps.InitialSkills.DiscoverInitialRoundBindings(ctx, p.source.initial.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(p.initial) {
		return nil, fault(f.DependencyUnbound)
	}
	p.locks = append(p.locks, p.initial.RequiredLocks()...)
	for _, change := range p.pending {
		key, _ := f.AggregateLock(f.SkillAggregate, change.Binding.SkillID.String())
		p.locks = append(p.locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	p.locks, err = oc.NormalizeLocks(p.locks)
	if err != nil {
		return nil, portError(err)
	}
	return p, ctx.Err()
}

type roundCurrentKey struct{}
type roundCurrentWitness struct {
	owner   *RoundInputDeterminer
	tx      f.Tx
	request ac.ExecutionConfigurationRequest
	source  *roundInputSource
	locks   []f.LockRequest
	active  atomic.Bool
}

func (d *RoundInputDeterminer) requireRoundCurrent(ctx context.Context, tx f.Tx, source *roundInputSource, locks []f.LockRequest) error {
	row := source.row
	if row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil || row.summary.Version == math.MaxInt64 || row.summary.Version < source.receipt.Version {
		return fault(f.ConfirmationStale)
	}
	slot, err := d.slots.AgentSlotInTx(ctx, tx, row.summary.ProjectID, row.summary.AgentID)
	if err != nil {
		return err
	}
	if !slot.Occupied || slot.ExecutionID == nil || *slot.ExecutionID != row.summary.ID || slot.Status != c.Running || slot.CancelRequestedAt != nil {
		return fault(f.ConfirmationStale)
	}
	if _, err = d.deps.Projects.RequirePreparingProjectInTx(ctx, tx, row.summary.ProjectID); err != nil {
		return portError(err)
	}
	actor, err := i.NewAgentRun(row.summary.ProjectID, row.summary.AgentID, row.summary.ID)
	if err != nil {
		return err
	}
	request := ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: row.summary.ProjectID, AgentID: row.summary.AgentID, ExecutionID: row.summary.ID, Stage: ac.ExecutionConfigurationCurrent}
	w := &roundCurrentWitness{owner: d, tx: tx, request: request, source: source, locks: slices.Clone(locks)}
	w.active.Store(true)
	defer w.active.Store(false)
	// Only active/publication/current authority is read. New configuration does
	// not replace any Tool/Model/Mount/Secret field in the original Snapshot.
	_, err = d.deps.Agents.ReadExecutionConfigurationInTx(context.WithValue(ctx, roundCurrentKey{}, w), tx, request)
	if err != nil {
		return portError(err)
	}
	return ctx.Err()
}
func (a *Authority) requireRoundCurrentConfiguration(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest) error {
	w, ok := ctx.Value(roundCurrentKey{}).(*roundCurrentWitness)
	if !ok || w == nil || !w.active.Load() || w.owner == nil || w.owner.authority != a || w.tx != tx || !sameToolCurrentRequest(w.request, request) {
		return fault(f.Forbidden)
	}
	x, err := w.owner.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = w.owner.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	source, err := loadRoundInputSource(ctx, x, request.ExecutionID)
	if err != nil {
		return err
	}
	if !sameRoundSource(source, w.source) || source.row.summary.Status != c.Running || source.row.summary.CancelRequestedAt != nil || !w.active.Load() {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}
func sameRoundSource(a, b *roundInputSource) bool {
	return a != nil && b != nil && a.row.summary.ID == b.row.summary.ID && a.row.summary.ProjectID == b.row.summary.ProjectID && a.row.summary.AgentID == b.row.summary.AgentID && a.row.summary.Version == b.row.summary.Version && a.original.snapshot.Digest() == b.original.snapshot.Digest() && a.original.round.Digest() == b.original.round.Digest() && a.receipt == b.receipt && a.prefix == b.prefix
}

func (d *RoundInputDeterminer) determineRoundInputInTx(ctx context.Context, tx f.Tx, e i.ExecutionID, next c.RoundID, plan c.RoundInputPlan) (c.RoundInput, error) {
	var zero c.RoundInput
	p, ok := plan.(*roundInputPlan)
	if !ok || p == nil || p.owner != d || p.execution != e || p.next != next {
		return zero, fault(f.Forbidden)
	}
	x, err := d.store.InTx(tx)
	if err != nil {
		return zero, portError(err)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return zero, portError(err)
	}
	previous, found, err := loadRoundInput(ctx, x, e)
	if err != nil {
		return zero, err
	}
	if found {
		if previous.Fields().RoundID != next {
			return zero, fault(f.ConfirmationStale)
		}
		return previous, ctx.Err()
	}
	if p.replay.Validate() == nil || p.source == nil || nilPort(p.initial) {
		return zero, fault(f.ConfirmationStale)
	}
	source, err := loadRoundInputSource(ctx, x, e)
	if err != nil {
		return zero, err
	}
	if !sameRoundSource(source, p.source) {
		return zero, fault(f.ConfirmationStale)
	}
	if err = d.requireRoundCurrent(ctx, tx, source, p.locks); err != nil {
		return zero, err
	}
	h, err := d.deps.Assignments.ReadAssignmentSequenceInTx(ctx, tx, source.row.summary.ProjectID, source.row.summary.AgentID)
	if err != nil {
		return zero, portError(err)
	}
	if h != p.cutoff {
		return zero, fault(f.ConfirmationStale)
	}
	pending, err := loadRoundPending(ctx, x, e, source.row.summary.ProjectID, source.row.summary.AgentID)
	if err != nil {
		return zero, err
	}
	if !slices.Equal(pending, p.pending) {
		return zero, fault(f.ConfirmationStale)
	}
	initial, err := d.deps.InitialSkills.ReadInitialRoundBindingsInTx(ctx, tx, source.initial.Clone(), p.initial)
	if err != nil {
		return zero, portError(err)
	}
	actual, err := directTextJSON(initial)
	if err != nil {
		return zero, err
	}
	expected, _ := directTextJSON(source.initial.Initial)
	if !bytes.Equal(actual, expected) {
		return zero, fault(f.ConfirmationStale)
	}
	validities := make([]sc.AssignmentValidity, len(pending))
	for n, change := range pending {
		if err = d.deps.Assignments.RequirePendingAssignmentInTx(ctx, tx, e, change); err != nil {
			return zero, portError(err)
		}
		if change.AssignmentSequence <= initial.AssignmentSequence {
			continue
		}
		validities[n], err = d.deps.Assignments.ValidateAssignmentInTx(ctx, tx, change.ProjectID, change.AgentID, change.Binding.AssignmentID, change.Binding.SkillID, change.Binding.RevisionID, change.Binding.Revision)
		if err != nil {
			return zero, portError(err)
		}
		if validities[n].Validate() != nil {
			return zero, fault(f.InvalidState)
		}
	}
	candidate, err := buildRoundInput(source, next, h, pending, validities)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	err = insertRoundInput(ctx, x, candidate, source.row.summary.Version)
	return candidate, err
}

func buildRoundInput(source *roundInputSource, next c.RoundID, h f.Version, pending []sc.RuntimeAssignmentChange, validities []sc.AssignmentValidity) (c.RoundInput, error) {
	if source == nil || source.row.summary.Version == math.MaxInt64 || len(pending) != len(validities) {
		return c.RoundInput{}, invalid()
	}
	binding, err := f.NewID[c.InputBinding]()
	if err != nil {
		return c.RoundInput{}, unavailable(err)
	}
	at, err := f.NewInstant(time.Now())
	if err != nil {
		return c.RoundInput{}, err
	}
	if at.Time().Before(source.receipt.CreatedAt.Time()) {
		at = source.receipt.CreatedAt
	}
	q := c.RoundInputFields{SchemaVersion: 1, RoundID: next, ExecutionID: source.row.summary.ID, SnapshotID: source.original.snapshot.Fields().ID, SnapshotDigest: source.original.snapshot.Digest(), PredecessorRoundID: source.original.round.Fields().ID, Source: source.receipt, Initial: source.initial.Initial.Clone(), SkillInputBindingID: binding, Bindings: []sc.SkillBinding{}, AppliedChangeIDs: []sc.AssignmentChangeID{}, Dispositions: []c.SkillChangeDisposition{}, AssignmentCutoff: h, TranscriptThrough: 3, TranscriptPrefixDigest: source.prefix, CheckpointVersion: source.row.summary.Version + 1, CreatedAt: at}
	selected := make(map[sc.SkillID]int)
	directory := make(map[sc.SkillID]sc.SkillBinding)
	for _, b := range q.Initial.Bindings {
		directory[b.SkillID] = b
	}
	for n, change := range pending {
		if change.AssignmentSequence > q.Initial.AssignmentSequence && validities[n] == sc.AssignmentValid {
			if before, ok := selected[change.Binding.SkillID]; !ok || pending[before].AssignmentSequence < change.AssignmentSequence {
				selected[change.Binding.SkillID] = n
			}
		}
	}
	additions := make([]c.SkillAddition, 0, len(selected))
	for n, change := range pending {
		disposition := c.SkillChangeDisposition{ChangeID: change.ID, Sequence: change.AssignmentSequence}
		switch {
		case change.AssignmentSequence <= q.Initial.AssignmentSequence:
			disposition.State = "covered"
		case validities[n] != sc.AssignmentValid:
			disposition.State = "skipped"
			disposition.Reason = string(validities[n])
		case selected[change.Binding.SkillID] != n:
			disposition.State = "skipped"
			disposition.Reason = "superseded"
		default:
			disposition.State = "applied"
			q.AppliedChangeIDs = append(q.AppliedChangeIDs, change.ID)
			addition := c.SkillAddition{ChangeID: change.ID, Binding: change.Binding}
			if old, ok := directory[change.Binding.SkillID]; ok {
				oldID := old.AssignmentID
				addition.ReplacedAssignmentID = &oldID
			}
			directory[change.Binding.SkillID] = change.Binding
			additions = append(additions, addition)
		}
		q.Dispositions = append(q.Dispositions, disposition)
	}
	for _, b := range directory {
		q.Bindings = append(q.Bindings, b)
	}
	slices.SortFunc(q.Bindings, func(a, b sc.SkillBinding) int {
		return bytes.Compare([]byte(a.SkillID.String()), []byte(b.SkillID.String()))
	})
	if len(additions) > 0 {
		slices.SortFunc(additions, func(a, b c.SkillAddition) int {
			return bytes.Compare([]byte(a.Binding.SkillID.String()), []byte(b.Binding.SkillID.String()))
		})
		id, err := f.NewID[c.ControlInput]()
		if err != nil {
			return c.RoundInput{}, unavailable(err)
		}
		q.Control = &c.SkillsAddedControl{ID: id, ExecutionID: q.ExecutionID, RoundID: next, Kind: "skills_added", SourceID: next, SchemaVersion: 1, Payload: additions, TranscriptPosition: 4}
		q.TranscriptThrough = 4
	}
	return c.NewRoundInput(q)
}
