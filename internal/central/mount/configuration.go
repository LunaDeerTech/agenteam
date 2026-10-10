package mount

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type Configuration struct{ state *configurationState }
type configurationState struct {
	store    Store
	projects pc.ProjectAuthority
	owner    ac.MountReferenceOwnerAuthority
}

// NewConfiguration performs no I/O. Every collection, including [], requires
// the real current Project authority and Agent canonical writer witness.
// Mount creation/Runner selection and physical ensure are separate unbound
// capabilities; this provider cannot manufacture a definition for a supplied ID.
func NewConfiguration(store Store, projects pc.ProjectAuthority, owner ac.MountReferenceOwnerAuthority) (*Configuration, error) {
	if nilPort(store) || nilPort(projects) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Configuration{state: &configurationState{store, projects, owner}}, nil
}

type planData struct {
	issuer      *configurationState
	change      ac.MountConfigurationChange
	owner       ac.MountReferenceOwnerPlan
	locks       []f.LockRequest
	before      referenceState
	definitions []definition
}
type configurationPlan struct{ data func() planData }

func (p configurationPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (configurationPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "mount_configuration_plan")
}
func (configurationPlan) LogValue() slog.Value { return slog.StringValue("mount_configuration_plan") }
func (configurationPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"mount_configuration_plan"`), nil
}
func sameChange(a, b ac.MountConfigurationChange) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.Command.Canonical() == b.Command.Canonical() && a.PlanRevision == b.PlanRevision && a.ResultOwnerVersion == b.ResultOwnerVersion &&
		(a.ExpectedOwnerVersion == nil && b.ExpectedOwnerVersion == nil || a.ExpectedOwnerVersion != nil && b.ExpectedOwnerVersion != nil && *a.ExpectedOwnerVersion == *b.ExpectedOwnerVersion) && slices.Equal(a.Before, b.Before) && slices.Equal(a.After, b.After)
}
func requiredLocks(r ac.MountConfigurationChange, owner ac.MountReferenceOwnerPlan) ([]f.LockRequest, error) {
	command, _ := f.CommandLock(r.Command)
	user, _ := f.UserLock(r.Actor.Details().UserID)
	project, _ := f.ProjectLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	// Every Mount definition writer must hold this same Agent EX lock. There
	// is no later per-Mount acquisition or lock upgrade inside the final Tx.
	locks := []f.LockRequest{{Key: command, Mode: f.Exclusive}, {Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Exclusive}}
	locks = append(locks, owner.RequiredLocks()...)
	if len(locks) > 512 {
		return nil, fault(f.InvalidArgument)
	}
	return oc.NormalizeLocks(locks)
}
func (s *configurationState) executor(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	if ctx == nil || !tx.Valid() {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnavailable)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, slices.Clone(locks)); err != nil {
		return nil, portError(err)
	}
	return x, nil
}
func (s *configurationState) currentOwner(ctx context.Context, tx f.Tx, r ac.MountConfigurationChange, mutate bool) error {
	intents := []i.AccessIntent{i.Read}
	if mutate {
		intents = append(intents, i.Mutate)
	}
	for _, intent := range intents {
		grant, err := s.projects.RequireOwnerInTx(ctx, tx, r.Actor, r.ProjectID, intent)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(r.Actor, r.ProjectID) {
			return fault(f.Forbidden)
		}
	}
	return portError(ctx.Err())
}
func (c *Configuration) DiscoverMountConfiguration(ctx context.Context, change ac.MountConfigurationChange) (ac.MountConfigurationPlan, error) {
	if c == nil || c.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	if err := change.Validate(); err != nil {
		return nil, err
	}
	change = change.Clone()
	s := c.state
	// Discovery is outside our transaction. It is never invoked from the
	// caller's final transaction, and is not an applied creation witness.
	owner, err := s.owner.DiscoverMountReferenceOwner(ctx, change.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	locks, err := requiredLocks(change, owner)
	if err != nil {
		return nil, portError(err)
	}
	cause, err := f.NewCommandsCause(change.Command)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	d := planData{issuer: s, change: change, owner: owner, locks: locks}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, slices.Clone(locks)); err != nil {
			return portError(err)
		}
		x, err := s.executor(ctx, tx, locks)
		if err != nil {
			return err
		}
		if err = s.currentOwner(ctx, tx, change, false); err != nil {
			return err
		}
		d.before, err = loadReferences(ctx, x, change.ProjectID, change.AgentID)
		if err != nil {
			return err
		}
		if err = checkBefore(change, d.before); err != nil {
			return err
		}
		for _, id := range change.After {
			v, err := loadDefinition(ctx, x, change.ProjectID, change.AgentID, id)
			if err != nil {
				return err
			}
			d.definitions = append(d.definitions, v)
		}
		return portError(ctx.Err())
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	return configurationPlan{data: func() planData { return d }}, nil
}
func (c *Configuration) validatePlan(ctx context.Context, tx f.Tx, r ac.MountConfigurationChange, p ac.MountConfigurationPlan) (planData, postgres.SQLExecutor, error) {
	var zero planData
	if c == nil || c.state == nil {
		return zero, nil, fault(f.DependencyUnbound)
	}
	if err := r.Validate(); err != nil {
		return zero, nil, err
	}
	plan, ok := p.(configurationPlan)
	if !ok || plan.data == nil {
		return zero, nil, fault(f.InvalidArgument)
	}
	d := plan.data()
	if d.issuer != c.state || !sameChange(d.change, r) {
		return zero, nil, fault(f.InvalidArgument)
	}
	x, err := c.state.executor(ctx, tx, d.locks)
	if err != nil {
		return zero, nil, err
	}
	if err = c.state.currentOwner(ctx, tx, r, true); err != nil {
		return zero, nil, err
	}
	before, err := loadReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return zero, nil, err
	}
	if err = checkBefore(r, before); err != nil {
		return zero, nil, err
	}
	if before.exists != d.before.exists || before.version != d.before.version || !slices.Equal(before.ids, d.before.ids) {
		return zero, nil, fault(f.VersionConflict)
	}
	for _, original := range d.definitions {
		now, err := loadDefinition(ctx, x, r.ProjectID, r.AgentID, original.ID)
		if err != nil {
			return zero, nil, err
		}
		if !sameDefinition(original, now) {
			return zero, nil, fault(f.VersionConflict)
		}
	}
	return d, x, portError(ctx.Err())
}
func (c *Configuration) RequireMountConfigurationInTx(ctx context.Context, tx f.Tx, change ac.MountConfigurationChange, plan ac.MountConfigurationPlan) error {
	_, _, err := c.validatePlan(ctx, tx, change, plan)
	return err
}
func (c *Configuration) ApplyMountConfigurationInTx(ctx context.Context, tx f.Tx, change ac.MountConfigurationChange, plan ac.MountConfigurationPlan) error {
	d, x, err := c.validatePlan(ctx, tx, change, plan)
	if err != nil {
		return err
	}
	// Always consume the original caller context and handle after its real
	// canonical writer, including empty and unchanged allowlists. No public row
	// or DTO can stand in for the Agent provider's private same-Tx witness.
	if err = c.state.owner.CheckMountReferenceOwnerAppliedInTx(ctx, tx, change.Clone(), d.owner); err != nil {
		return portError(err)
	}
	return writeReferences(ctx, x, change, d.before)
}

var _ ac.MountConfiguration = (*Configuration)(nil)
