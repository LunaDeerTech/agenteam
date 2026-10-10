package execution

import (
	"context"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type Dependencies struct {
	Authority *Authority
	Agents    ac.ExecutionConfiguration
	Task      c.TriggerProvider
	Meeting   c.TriggerProvider
}
type Service struct {
	store Store
	deps  Dependencies
	calls *callSet
}

// Construction performs no I/O. Each Trigger is explicitly bound; absence is
// retained and fails that launch before any created/slot fact is written.
func New(store Store, deps Dependencies) (*Service, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || deps.Authority == nil || deps.Authority.state == nil || deps.Authority.state.store != store || nilPort(deps.Agents) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Service{store, deps, newCalls()}, nil
}
func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	if s == nil || s.calls == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	return s.calls.begin(ctx)
}
func (s *Service) Stop() {
	if s != nil && s.calls != nil {
		s.calls.stop()
	}
}
func (s *Service) Drain(ctx context.Context) error {
	if s == nil || s.calls == nil {
		return fault(f.DependencyUnbound)
	}
	return s.calls.drain(ctx)
}
func (s *Service) Joined() bool { return s != nil && s.calls != nil && s.calls.joined() }

func (s *Service) Launch(ctx context.Context, actor i.Actor, request c.LaunchRequest) (c.LaunchResult, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.LaunchResult{}, err
	}
	defer done()
	if err = launchActor(actor); err != nil {
		return c.LaunchResult{}, err
	}
	if err = request.Validate(); err != nil {
		return c.LaunchResult{}, err
	}
	request = request.Clone()
	digest, _ := request.Digest()
	key := c.LaunchLookupKey{ProjectID: request.ProjectID, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}
	// Authorized original results precede current source launchability and busy
	// checks. This read is not a proof that an old unknown writer cannot commit.
	lookup, err := s.lookup(ctx, actor, key, digest)
	if err != nil {
		return c.LaunchResult{}, err
	}
	if lookup.Found {
		return c.LaunchResult{Execution: lookup.Execution.Clone(), Replayed: true}, nil
	}
	provider := s.deps.Task
	if request.Trigger.Kind == "meeting" {
		provider = s.deps.Meeting
	}
	if nilPort(provider) {
		return c.LaunchResult{}, fault(f.DependencyUnbound)
	}
	plan, err := provider.DiscoverLaunch(ctx, actor, request.Clone())
	if err != nil {
		return c.LaunchResult{}, portError(err)
	}
	if nilPort(plan) {
		return c.LaunchResult{}, fault(f.DependencyUnavailable)
	}
	execution, err := f.NewID[i.Execution]()
	if err != nil {
		return c.LaunchResult{}, unavailable(err)
	}
	command, _ := request.Command()
	locks := append(scopeLocks(actor, request.ProjectID, request.AgentID, f.Exclusive), commandLock(command), executionLock(execution))
	if request.Trigger.Kind == "task" {
		key, _ := f.ProjectScheduleLock(request.ProjectID.String())
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	}
	locks, err = oc.NormalizeLocks(append(locks, plan.RequiredLocks()...))
	if err != nil {
		return c.LaunchResult{}, portError(err)
	}
	cause, _ := f.NewCommandsCause(command)
	var output c.LaunchResult
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = s.deps.Authority.requireScope(ctx, tx, actor, request.ProjectID, request.AgentID, i.Read); err != nil {
			return err
		}
		old, err := loadLaunch(ctx, x, key)
		if err != nil {
			return err
		}
		if old != nil {
			if old.digest != digest {
				return fault(f.IdempotencyKeyReused)
			}
			output = c.LaunchResult{Execution: old.summary.Clone(), Replayed: true}
			return nil
		}
		if err = s.deps.Authority.requireScope(ctx, tx, actor, request.ProjectID, request.AgentID, i.Launch); err != nil {
			return err
		}
		permit, err := provider.ValidateLaunchInTx(ctx, tx, actor, request.Clone(), plan)
		if err != nil {
			return portError(err)
		}
		if !permit.Matches(request) {
			return fault(f.ConfirmationStale)
		}
		capture := ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: request.ProjectID, AgentID: request.AgentID, ExecutionID: execution, Stage: ac.ExecutionConfigurationLaunch}
		witness := s.deps.Authority.launchContext(ctx, tx, capture, request, permit, locks)
		config, err := s.deps.Agents.ReadExecutionConfigurationInTx(witness, tx, capture)
		if err != nil {
			return portError(err)
		}
		fields := config.Fields()
		if config.Validate() != nil || fields.Core.ID != request.AgentID || fields.Core.ProjectID != request.ProjectID || fields.Core.Lifecycle != ac.AgentActive {
			return fault(f.InvalidState)
		}
		busy, err := occupied(ctx, x, request.AgentID)
		if err != nil {
			return err
		}
		if busy {
			return fault(f.AgentBusy)
		}
		created, err := insertCreated(ctx, x, execution, actor, request, permit)
		if err != nil {
			return err
		}
		output = c.LaunchResult{Execution: created.summary.Clone()}
		return nil
	})
	if err = commitError(result); err != nil {
		return c.LaunchResult{}, err
	}
	return output, nil
}

func (s *Service) LookupLaunch(ctx context.Context, actor i.Actor, key c.LaunchLookupKey, expected f.Digest) (c.LaunchLookup, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.LaunchLookup{}, err
	}
	defer done()
	return s.lookup(ctx, actor, key, expected)
}
func (s *Service) lookup(ctx context.Context, actor i.Actor, key c.LaunchLookupKey, expected f.Digest) (c.LaunchLookup, error) {
	if err := launchActor(actor); err != nil {
		return c.LaunchLookup{}, err
	}
	if key.Validate() != nil || expected.Validate() != nil {
		return c.LaunchLookup{}, invalid()
	}
	command, _ := key.Command()
	locks, err := oc.NormalizeLocks(append(scopeLocks(actor, key.ProjectID, key.AgentID, f.Shared), commandLock(command)))
	if err != nil {
		return c.LaunchLookup{}, portError(err)
	}
	cause, _ := f.NewCommandsCause(command)
	var output c.LaunchLookup
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = s.deps.Authority.requireScope(ctx, tx, actor, key.ProjectID, key.AgentID, i.Read); err != nil {
			return err
		}
		row, err := loadLaunch(ctx, x, key)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		if row.digest != expected {
			return fault(f.IdempotencyKeyReused)
		}
		value := row.summary.Clone()
		output = c.LaunchLookup{Found: true, RequestDigest: row.digest, Execution: &value}
		return nil
	})
	if err = commitError(result); err != nil {
		return c.LaunchLookup{}, err
	}
	return output, nil
}
