package execution

import (
	"context"
	"errors"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type skillRoundPlan struct {
	owner     *directTextState
	run       *directTextCall
	validator sc.RoundAssignmentValidation
	source    c.RoundID
	snapshot  c.SnapshotID
	previous  f.Version
	cutoff    f.Version
	bindings  []sc.SkillBinding
	pending   []sc.RuntimeAssignmentChange
	locks     []f.LockRequest
}

func (p *skillRoundPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type skillRoundTransition struct {
	plan         *skillRoundPlan
	bindings     []sc.SkillBinding
	dispositions []c.SkillChangeDisposition
	additions    []c.SkillAddition
}

func (t *skillRoundTransition) roundInput(round c.RoundID, at f.Instant, sequence f.Sequence) (c.SkillRoundInput, error) {
	if t == nil || t.plan == nil {
		return c.SkillRoundInput{}, invalid()
	}
	p := t.plan
	v := c.SkillRoundInput{SchemaVersion: 1, ExecutionID: p.run.request.ExecutionID, SnapshotID: p.snapshot, RoundID: round, SourceRoundID: p.source, PreviousCutoff: p.previous, AssignmentCutoff: p.cutoff, PreviousBindings: slices.Clone(p.bindings), Bindings: slices.Clone(t.bindings), Dispositions: slices.Clone(t.dispositions), Additions: slices.Clone(t.additions), CreatedAt: at}
	if len(t.additions) > 0 {
		v.ControlSequence = &sequence
	}
	return v, v.Validate()
}

// A pre-migration round retains only its old Snapshot bindings. Missing data
// for a newly created successor fails closed; pending ingress is never read as
// if it were already part of an earlier Model input.
func loadSkillBindingsForRound(ctx context.Context, x postgres.SQLExecutor, stored *directTextRecord) ([]sc.SkillBinding, f.Version, error) {
	if stored == nil || stored.snapshot.Validate() != nil || stored.round.Validate() != nil {
		return nil, 0, invalid()
	}
	initial := stored.snapshot.Fields().Context.Input().Fields().Skills
	if initial.Validate() != nil {
		return nil, 0, invalid()
	}
	var required bool
	if err := x.QueryRow(ctx, `SELECT skill_input_required FROM agenteam_execution.rounds WHERE execution_id=$1 AND id=$2`, stored.round.Fields().ExecutionID.String(), stored.round.Fields().ID.String()).Scan(&required); err != nil {
		return nil, 0, unavailable(err)
	}
	loop, ok := stored.round.(loopExecutionRound)
	if !ok || loop.LoopRound.Fields().Ordinal == 1 || !required {
		return slices.Clone(initial.Bindings), initial.AssignmentSequence, nil
	}
	var raw []byte
	var digest string
	var cutoff int64
	err := x.QueryRow(ctx, `SELECT content,digest,assignment_cutoff FROM agenteam_execution.loop_skill_inputs WHERE execution_id=$1 AND round_id=$2`, stored.round.Fields().ExecutionID.String(), stored.round.Fields().ID.String()).Scan(&raw, &digest, &cutoff)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, fault(f.InvalidState)
	}
	if err != nil {
		return nil, 0, unavailable(err)
	}
	v, err := c.DecodeSkillRoundInput(raw)
	r := stored.round.Fields()
	if err != nil || c.TriggerInputDigest(raw) != f.Digest(digest) || v.ExecutionID != r.ExecutionID || v.RoundID != r.ID || v.SnapshotID != r.SnapshotID || loop.LoopRound.Fields().Predecessor == nil || v.SourceRoundID != *loop.LoopRound.Fields().Predecessor || v.AssignmentCutoff != f.Version(cutoff) || !v.CreatedAt.Time().Equal(r.CreatedAt.Time()) {
		return nil, 0, fault(f.InvalidState)
	}
	return slices.Clone(v.Bindings), v.AssignmentCutoff, nil
}

// The Runtime can call this only with the original private ToolCallPlan in its
// current same-Store transaction. It returns immutable facts, not a grant.
func (d *ToolCallDriver) ReadSkillToolBindingsInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan) ([]sc.SkillBinding, error) {
	switch b.Binding.HandlerID {
	case "skill.list", "skill.read", "skill.assign", "skill.prepare":
	default:
		return nil, fault(f.Forbidden)
	}
	if bindings, present, err := d.readSkillToolInputLoan(ctx, tx, b, plan); present {
		return bindings, err
	}
	var out []sc.SkillBinding
	err := d.withToolCallInTx(ctx, tx, b, plan, func(current context.Context, actual tc.ToolCallBinding, _ []byte) error {
		x, err := d.driver.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		stored, err := loadDirectTextRound(current, x, actual.ExecutionID, actual.RoundID)
		if err != nil {
			return err
		}
		out, _, err = loadSkillBindingsForRound(current, x, stored)
		return err
	})
	return out, err
}

func (s *directTextState) discoverSkillRound(ctx context.Context, run *directTextCall, validation sc.RoundAssignmentValidation) (*skillRoundPlan, error) {
	if s == nil || run == nil || s.multi == nil || nilPort(validation) || run.round == nil {
		return nil, fault(f.DependencyUnbound)
	}
	p := &skillRoundPlan{owner: s, run: run, validator: validation, source: run.round.Fields().ID, snapshot: run.round.Fields().SnapshotID}
	p.locks = append(slices.Clone(run.locks), roundInputLocks(run.request.Launch.ProjectID, run.request.Launch.AgentID, run.request.ExecutionID)...)
	var err error
	p.locks, err = oc.NormalizeLocks(p.locks)
	if err != nil {
		return nil, err
	}
	cause, _ := f.NewRecoveryCause("execution.skill-round.discover", run.request.ExecutionID.String(), p.source.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, p.locks); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
			return err
		}
		if err := s.multi.guard.RequireCurrentProcessInTx(ctx, tx, s.multiProcess()); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		stored, err := loadDirectTextRound(ctx, x, run.request.ExecutionID, p.source.String())
		if err != nil {
			return err
		}
		if stored == nil || stored.round.Digest() != run.round.Digest() {
			return fault(f.ConfirmationStale)
		}
		p.bindings, p.previous, err = loadSkillBindingsForRound(ctx, x, stored)
		if err != nil {
			return err
		}
		p.cutoff, err = validation.ReadAssignmentSequenceInTx(ctx, tx, run.request.Launch.ProjectID, run.request.Launch.AgentID)
		if err != nil {
			return portError(err)
		}
		if p.cutoff < p.previous {
			return fault(f.ConfirmationStale)
		}
		p.pending, err = loadRoundPending(ctx, x, run.request.ExecutionID, run.request.Launch.ProjectID, run.request.Launch.AgentID)
		return err
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	for _, b := range p.bindings {
		key, _ := f.AggregateLock(f.SkillAggregate, b.SkillID.String())
		p.locks = append(p.locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	for _, change := range p.pending {
		key, _ := f.AggregateLock(f.SkillAggregate, change.Binding.SkillID.String())
		p.locks = append(p.locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	p.locks, err = oc.NormalizeLocks(p.locks)
	return p, err
}

func (s *directTextState) consumeSkillRoundInTx(ctx context.Context, tx f.Tx, run *directTextCall, p *skillRoundPlan) (*skillRoundTransition, error) {
	if p == nil || p.owner != s || p.run != run || run.round.Fields().ID != p.source || run.round.Fields().SnapshotID != p.snapshot {
		return nil, fault(f.Forbidden)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	actual, err := p.validator.ReadAssignmentSequenceInTx(ctx, tx, run.request.Launch.ProjectID, run.request.Launch.AgentID)
	if err != nil {
		return nil, portError(err)
	}
	if actual != p.cutoff {
		return nil, fault(f.ConfirmationStale)
	}
	pending, err := loadRoundPending(ctx, x, run.request.ExecutionID, run.request.Launch.ProjectID, run.request.Launch.AgentID)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(pending, p.pending) {
		return nil, fault(f.ConfirmationStale)
	}
	states := make([]sc.AssignmentValidity, len(pending))
	for n, change := range pending {
		if change.AssignmentSequence <= p.previous {
			continue
		}
		if change.AssignmentSequence > p.cutoff {
			return nil, fault(f.ConfirmationStale)
		}
		if err = p.validator.RequirePendingAssignmentInTx(ctx, tx, run.request.ExecutionID, change); err != nil {
			return nil, portError(err)
		}
		states[n], err = p.validator.ValidateAssignmentInTx(ctx, tx, change.ProjectID, change.AgentID, change.Binding.AssignmentID, change.Binding.SkillID, change.Binding.RevisionID, change.Binding.Revision)
		if err != nil {
			return nil, portError(err)
		}
		if states[n].Validate() != nil {
			return nil, fault(f.InvalidState)
		}
	}
	return buildSkillRoundTransition(p, states)
}
func buildSkillRoundTransition(p *skillRoundPlan, states []sc.AssignmentValidity) (*skillRoundTransition, error) {
	if p == nil || len(states) != len(p.pending) {
		return nil, invalid()
	}
	t := &skillRoundTransition{plan: p, bindings: []sc.SkillBinding{}, dispositions: []c.SkillChangeDisposition{}, additions: []c.SkillAddition{}}
	directory := map[sc.SkillID]sc.SkillBinding{}
	selected := map[sc.SkillID]int{}
	for _, b := range p.bindings {
		directory[b.SkillID] = b
	}
	for n, change := range p.pending {
		if change.AssignmentSequence > p.previous && states[n] == sc.AssignmentValid {
			selected[change.Binding.SkillID] = n
		}
	}
	for n, change := range p.pending {
		if change.AssignmentSequence <= p.previous {
			continue
		}
		d := c.SkillChangeDisposition{ChangeID: change.ID, Sequence: change.AssignmentSequence, State: "skipped"}
		switch {
		case states[n] != sc.AssignmentValid:
			d.Reason = string(states[n])
		case selected[change.Binding.SkillID] != n:
			d.Reason = "superseded"
		default:
			d.State = "applied"
			a := c.SkillAddition{ChangeID: change.ID, Binding: change.Binding}
			if old, ok := directory[change.Binding.SkillID]; ok {
				id := old.AssignmentID
				a.ReplacedAssignmentID = &id
			}
			directory[change.Binding.SkillID] = change.Binding
			t.additions = append(t.additions, a)
		}
		t.dispositions = append(t.dispositions, d)
	}
	for _, b := range directory {
		t.bindings = append(t.bindings, b)
	}
	slices.SortFunc(t.bindings, func(a, b sc.SkillBinding) int { return compareSkillIDs(a.SkillID.String(), b.SkillID.String()) })
	slices.SortFunc(t.additions, func(a, b c.SkillAddition) int {
		return compareSkillIDs(a.Binding.SkillID.String(), b.Binding.SkillID.String())
	})
	return t, nil
}
func compareSkillIDs(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func insertSkillRoundInput(ctx context.Context, x postgres.SQLExecutor, v c.SkillRoundInput) error {
	raw, err := v.CanonicalBytes()
	if err != nil {
		return err
	}
	var sequence any
	if v.ControlSequence != nil {
		sequence = int64(*v.ControlSequence)
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.loop_skill_inputs(execution_id,round_id,source_round_id,snapshot_id,assignment_cutoff,content,digest,control_sequence,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.ExecutionID.String(), v.RoundID.String(), v.SourceRoundID.String(), v.SnapshotID.String(), int64(v.AssignmentCutoff), raw, string(c.TriggerInputDigest(raw)), sequence, v.CreatedAt.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return nil
}

// Install retains its original separate Operation/Attempt ledger. This loan
// shares current Execution authorization without classifying install as one of
// the generic DomainBuiltin backends.
func (d *ToolCallDriver) WithInstallToolCallInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if b.Binding != (tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}) || b.RunnerBinding != (tc.RunnerBinding{}) || b.MCPBinding != (tc.MCPBinding{}) || consume == nil {
		return fault(f.Forbidden)
	}
	return d.withToolCallInTx(ctx, tx, b, plan, consume)
}
