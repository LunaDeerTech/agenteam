package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync/atomic"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// SkillAssignmentAuthority composes the canonical Agent writer with the four
// existing reference owners. It has no default bindings, worker or private SQL
// access to Skill, Tool, Mount, Model or Secret tables.
type SkillAssignmentAuthority struct {
	agents     *Authority
	references [4]c.SkillAssignmentVersionReferences
}

func NewSkillAssignmentAuthority(agents *Authority, references []c.SkillAssignmentVersionReferences) (*SkillAssignmentAuthority, error) {
	if agents == nil || agents.state == nil || len(references) != 4 {
		return nil, fault(f.DependencyUnbound)
	}
	out := &SkillAssignmentAuthority{agents: agents}
	for _, ref := range references {
		if nilPort(ref) {
			return nil, fault(f.DependencyUnbound)
		}
		var n int
		switch ref.SkillAssignmentReferenceKind() {
		case c.SkillAssignmentToolReferences:
			n = 0
		case c.SkillAssignmentMountReferences:
			n = 1
		case c.SkillAssignmentModelReferences:
			n = 2
		case c.SkillAssignmentSecretReferences:
			n = 3
		default:
			return nil, fault(f.DependencyUnbound)
		}
		if out.references[n] != nil {
			return nil, fault(f.DependencyUnbound)
		}
		out.references[n] = ref
	}
	return out, nil
}

type skillAssignmentPlan struct {
	issuer  *SkillAssignmentAuthority
	request c.SkillAssignmentRequest
	before  c.AgentConfig
	locks   []f.LockRequest
}

func (p *skillAssignmentPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*skillAssignmentPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("agent_skill_assignment_plan"))
}
func (*skillAssignmentPlan) LogValue() slog.Value {
	return slog.StringValue("agent_skill_assignment_plan")
}
func (*skillAssignmentPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_skill_assignment_plan"`), nil
}

func sameSkillAssignmentRequest(a, b c.SkillAssignmentRequest) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.Command.Canonical() == b.Command.Canonical() && a.ExpectedVersion == b.ExpectedVersion
}

func (a *Authority) skillAssignmentCurrent(ctx context.Context, tx f.Tx, request c.SkillAssignmentRequest, locks []f.LockRequest, intent i.AccessIntent) (postgres.SQLExecutor, c.AgentConfig, error) {
	if a == nil || a.state == nil {
		return nil, c.AgentConfig{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return nil, c.AgentConfig{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, c.AgentConfig{}, err
	}
	if err := currentActor(request.Actor); err != nil {
		return nil, c.AgentConfig{}, err
	}
	if request.Validate() != nil {
		return nil, c.AgentConfig{}, invalid()
	}
	x, err := a.state.store.InTx(tx)
	if err != nil {
		return nil, c.AgentConfig{}, executionConfigurationError(err)
	}
	if nilPort(x) {
		return nil, c.AgentConfig{}, fault(f.DependencyUnbound)
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, c.AgentConfig{}, executionConfigurationError(err)
	}
	grant, err := a.state.projects.RequireOwnerInTx(ctx, tx, request.Actor, request.ProjectID, intent)
	if canceled := ctx.Err(); canceled != nil {
		return nil, c.AgentConfig{}, canceled
	}
	if err != nil {
		return nil, c.AgentConfig{}, executionConfigurationError(err)
	}
	if !grant.Matches(request.Actor, request.ProjectID) {
		return nil, c.AgentConfig{}, fault(f.Forbidden)
	}
	current, err := initializedAgentState(ctx, x, request.ProjectID, request.AgentID, intent != i.Read)
	if canceled := ctx.Err(); canceled != nil {
		return nil, c.AgentConfig{}, canceled
	}
	if err != nil {
		return nil, c.AgentConfig{}, executionConfigurationError(err)
	}
	return x, current, nil
}

func (a *SkillAssignmentAuthority) ReadSkillAssignmentInTx(ctx context.Context, tx f.Tx, request c.SkillAssignmentRequest) (c.AgentRef, error) {
	if a == nil || a.agents == nil || a.agents.state == nil {
		return c.AgentRef{}, fault(f.DependencyUnbound)
	}
	_, current, err := a.agents.skillAssignmentCurrent(ctx, tx, request, request.RequiredLocks(), i.Read)
	if err != nil {
		return c.AgentRef{}, err
	}
	return c.AgentRef{ProjectID: request.ProjectID, AgentID: request.AgentID, ConfigVersion: current.Fields().Core.Version}, nil
}

func (a *SkillAssignmentAuthority) DiscoverSkillAssignment(ctx context.Context, request c.SkillAssignmentRequest) (c.SkillAssignmentPlan, error) {
	if a == nil || a.agents == nil || a.agents.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := currentActor(request.Actor); err != nil {
		return nil, err
	}
	if request.Validate() != nil {
		return nil, invalid()
	}
	base := request.RequiredLocks()
	cause, err := f.NewCommandsCause(request.Command)
	if err != nil {
		return nil, invalid()
	}
	var before c.AgentConfig
	state := a.agents.state
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, base); err != nil {
			return executionConfigurationError(err)
		}
		_, current, err := a.agents.skillAssignmentCurrent(ctx, tx, request, base, i.Read)
		before = current
		return err
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Discovery does not compare the caller's old expected version or inspect
	// any foreign reference index. Completed replay must remain observable.
	locks := slices.Clone(base)
	version := before.Fields().Core.Version
	if version != math.MaxInt64 && before.Fields().Core.Lifecycle == c.AgentActive {
		probe := request
		probe.ExpectedVersion = version
		change := c.SkillAssignmentVersionChange{Request: probe, Before: before, ResultVersion: version + 1}
		for _, references := range a.references {
			additional, err := references.SkillAssignmentVersionLocks(change.Clone())
			if err != nil {
				return nil, executionConfigurationError(err)
			}
			locks = append(locks, additional...)
		}
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil || len(locks) > 512 {
		return nil, fault(f.ResourceBusy)
	}
	return &skillAssignmentPlan{issuer: a, request: request, before: before, locks: locks}, nil
}

type skillAssignmentVersionKey struct{}
type skillAssignmentVersionWitness struct {
	issuer *authorityState
	tx     f.Tx
	ctx    context.Context
	change c.SkillAssignmentVersionChange
	after  c.AgentConfig
	locks  []f.LockRequest
	active atomic.Bool
}

func (a *SkillAssignmentAuthority) WithSkillAssignmentInTx(ctx context.Context, tx f.Tx, request c.SkillAssignmentRequest, plan c.SkillAssignmentPlan,
	prepare func(context.Context, c.AgentRef) (bool, error), finalize func(context.Context, c.AgentRef) error) error {
	if a == nil || a.agents == nil || a.agents.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || prepare == nil || finalize == nil || request.Validate() != nil {
		return invalid()
	}
	selected, ok := plan.(*skillAssignmentPlan)
	if !ok || selected == nil || selected.issuer != a || !sameSkillAssignmentRequest(selected.request, request) {
		return fault(f.Forbidden)
	}
	x, current, err := a.agents.skillAssignmentCurrent(ctx, tx, request, selected.RequiredLocks(), i.Read)
	if err != nil {
		return err
	}
	ref := c.AgentRef{ProjectID: request.ProjectID, AgentID: request.AgentID, ConfigVersion: current.Fields().Core.Version}
	changed, err := prepare(ctx, ref)
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if ref.ConfigVersion != request.ExpectedVersion || !sameValue(current, selected.before) {
		return fault(f.VersionConflict)
	}
	if ref.ConfigVersion == math.MaxInt64 {
		return counterExhausted()
	}
	// Recheck the live owner and complete canonical preimage after Prepare
	// actually returns. A callback must not change the Agent before this CAS.
	x, actual, err := a.agents.skillAssignmentCurrent(ctx, tx, request, selected.RequiredLocks(), i.Mutate)
	if err != nil {
		return err
	}
	if !sameValue(actual, current) {
		return fault(f.VersionConflict)
	}
	after, err := advanceSkillAssignmentAgent(ctx, x, current)
	if err != nil {
		return err
	}
	change := c.SkillAssignmentVersionChange{Request: request, Before: current, ResultVersion: after.Fields().Core.Version}
	w := &skillAssignmentVersionWitness{issuer: a.agents.state, tx: tx, change: change, after: after, locks: selected.RequiredLocks()}
	w.ctx = context.WithValue(ctx, skillAssignmentVersionKey{}, w)
	w.active.Store(true)
	defer w.active.Store(false)
	for _, references := range a.references {
		if err = references.AdvanceSkillAssignmentVersionInTx(w.ctx, tx, change.Clone()); err != nil {
			return executionConfigurationError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
	// Neither a future callback nor the Skill writer receives the private
	// context that the four reference owners borrowed above.
	w.active.Store(false)
	ref.ConfigVersion = change.ResultVersion
	if err = finalize(ctx, ref); err != nil {
		return err
	}
	return ctx.Err()
}

func advanceSkillAssignmentAgent(ctx context.Context, x postgres.SQLExecutor, before c.AgentConfig) (c.AgentConfig, error) {
	d := before.Fields()
	version := d.Core.Version + 1
	var updated time.Time
	err := x.QueryRow(ctx, `UPDATE agenteam_agent.agents SET version=$3,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE project_id=$1 AND id=$2 AND version=$4 AND lifecycle='active' RETURNING updated_at`, d.Core.ProjectID.String(), d.Core.ID.String(), int64(version), int64(d.Core.Version)).Scan(&updated)
	if canceled := ctx.Err(); canceled != nil {
		return c.AgentConfig{}, canceled
	}
	if err != nil {
		return c.AgentConfig{}, unavailable(err)
	}
	d.Core.UpdatedAt, err = f.NewInstant(updated)
	if err != nil || !updated.After(before.Fields().Core.UpdatedAt.Time()) {
		return c.AgentConfig{}, unavailable(err)
	}
	d.Core.Version = version
	after, err := c.NewAgentConfig(d)
	if err != nil {
		return c.AgentConfig{}, unavailable(err)
	}
	actual, err := loadAgent(ctx, x, d.Core.ProjectID, d.Core.ID)
	if canceled := ctx.Err(); canceled != nil {
		return c.AgentConfig{}, canceled
	}
	if err != nil || actual == nil || !sameValue(*actual, after) {
		return c.AgentConfig{}, unavailable(err)
	}
	return after, nil
}

// A plausible change DTO, a current version, another Authority, and a saved
// callback context are each insufficient. Only this canonical writer issues
// this witness, after the original CAS and complete unchanged postimage read.
func (a *Authority) RequireSkillAssignmentVersionAppliedInTx(ctx context.Context, tx f.Tx, change c.SkillAssignmentVersionChange) error {
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || change.Validate() != nil {
		return invalid()
	}
	w, ok := ctx.Value(skillAssignmentVersionKey{}).(*skillAssignmentVersionWitness)
	if !ok || w == nil || !w.active.Load() || w.issuer != a.state || w.tx != tx || w.ctx != ctx || !sameSkillAssignmentRequest(w.change.Request, change.Request) || w.change.ResultVersion != change.ResultVersion || !sameValue(w.change.Before, change.Before) {
		return fault(f.Forbidden)
	}
	_, current, err := a.skillAssignmentCurrent(ctx, tx, change.Request, w.locks, i.Mutate)
	if err != nil {
		return err
	}
	if !w.active.Load() || !sameValue(current, w.after) {
		return fault(f.Forbidden)
	}
	return nil
}

var _ c.SkillAssignmentOwner = (*SkillAssignmentAuthority)(nil)
var _ c.SkillAssignmentVersionOwner = (*Authority)(nil)
