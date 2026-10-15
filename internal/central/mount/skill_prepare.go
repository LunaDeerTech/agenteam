package mount

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type SkillPrepareResolver struct {
	store  Store
	agents ac.ExecutionConfiguration
	owner  mc.SkillPrepareAuthority
}

func NewSkillPrepareResolver(store Store, agents ac.ExecutionConfiguration, owner mc.SkillPrepareAuthority) (*SkillPrepareResolver, error) {
	if nilPort(store) || nilPort(agents) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SkillPrepareResolver{store: store, agents: agents, owner: owner}, nil
}

// Tickets have no public constructor or mutable DTO fields. Copies share the
// single-use state. Resolve is not dispatch admission; Claim rechecks current
// facts in the original Store and only returns after a known commit.
type SkillPrepareTicket struct{ state *skillPrepareTicketState }
type skillPrepareTicketState struct {
	mu      sync.Mutex
	used    bool
	owner   *SkillPrepareResolver
	intent  mc.SkillPrepareIntent
	locks   []f.LockRequest
	current readFileCurrent
}

func (SkillPrepareTicket) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_prepare_ticket")
}
func (SkillPrepareTicket) MarshalJSON() ([]byte, error) { return []byte(`"skill_prepare_ticket"`), nil }
func (SkillPrepareTicket) LogValue() slog.Value         { return slog.StringValue("skill_prepare_ticket") }
func (t SkillPrepareTicket) RunnerID() rc.RunnerID {
	if t.state == nil {
		return rc.RunnerID{}
	}
	return t.state.current.definition.Runner
}

// Target exposes the ticket's fixed safe metadata without consuming it or
// granting dispatch. The original Claim still rechecks the complete mapping.
func (t SkillPrepareTicket) Target() (mc.WriteFileTarget, error) {
	if t.state == nil {
		return mc.WriteFileTarget{}, fault(f.Forbidden)
	}
	return writeFileTarget(t.state.current)
}

func skillPrepareConfigurationRequest(in mc.SkillPrepareIntent) ac.ExecutionConfigurationRequest {
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}

func (r *SkillPrepareResolver) plan(ctx context.Context, in mc.SkillPrepareIntent) ([]f.LockRequest, error) {
	if r == nil || nilPort(r.store) || nilPort(r.agents) || nilPort(r.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if in.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	more, err := r.owner.SkillPrepareLocks(ctx, in)
	if err != nil {
		return nil, portError(err)
	}
	locks := append(skillPrepareConfigurationRequest(in).RequiredLocks(), more...)
	op, _ := f.AggregateLock(f.OperationAggregate, in.OperationID.String())
	locks = append(locks, f.LockRequest{Key: op, Mode: f.Exclusive})
	return oc.NormalizeLocks(locks)
}

func (r *SkillPrepareResolver) read(ctx context.Context, tx f.Tx, in mc.SkillPrepareIntent, payload *p.SkillPreparePayload, locks []f.LockRequest) (readFileCurrent, error) {
	var out readFileCurrent
	x, err := r.store.InTx(tx)
	if err != nil {
		return out, portError(err)
	}
	if err = r.store.RequireHeldLocks(ctx, tx, slices.Clone(locks)); err != nil {
		return out, portError(err)
	}
	calls := 0
	var callbackErr error
	err = r.owner.WithSkillPrepareInTx(ctx, tx, in, payload, func(authorized context.Context) (err error) {
		defer func() {
			if err != nil {
				callbackErr = err
			}
		}()
		calls++
		if calls != 1 || authorized == nil {
			return fault(f.Forbidden)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		config, err := r.agents.ReadExecutionConfigurationInTx(authorized, tx, skillPrepareConfigurationRequest(in))
		if err != nil {
			return portError(err)
		}
		out, err = readCurrentMount(authorized, x, config, in.ProjectID, in.AgentID, in.MountID)
		return err
	})
	if err != nil {
		return readFileCurrent{}, portError(err)
	}
	if calls != 1 {
		return readFileCurrent{}, fault(f.Forbidden)
	}
	if callbackErr != nil {
		return readFileCurrent{}, portError(callbackErr)
	}
	if err = ctx.Err(); err != nil {
		return readFileCurrent{}, err
	}
	return out, nil
}

func (r *SkillPrepareResolver) transaction(ctx context.Context, in mc.SkillPrepareIntent, payload *p.SkillPreparePayload, locks []f.LockRequest, work func(readFileCurrent) error) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	id, err := f.NewID[struct{}]()
	if err != nil {
		return fault(f.DependencyUnavailable)
	}
	cause, err := f.NewRecoveryCause("runner-skill-prepare", id.String(), "")
	if err != nil {
		return fault(f.InvalidArgument)
	}
	completed := false
	result := r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, slices.Clone(locks)); err != nil {
			return portError(err)
		}
		current, err := r.read(ctx, tx, in, payload, locks)
		if err != nil {
			return err
		}
		if err := work(current); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err := commitError(result); err != nil {
		return err
	}
	if !completed {
		return fault(f.DependencyUnavailable)
	}
	return nil
}

func (r *SkillPrepareResolver) Resolve(ctx context.Context, in mc.SkillPrepareIntent) (SkillPrepareTicket, error) {
	locks, err := r.plan(ctx, in)
	if err != nil {
		return SkillPrepareTicket{}, err
	}
	var current readFileCurrent
	if err = r.transaction(ctx, in, nil, locks, func(v readFileCurrent) error { current = v; return nil }); err != nil {
		return SkillPrepareTicket{}, err
	}
	return SkillPrepareTicket{state: &skillPrepareTicketState{owner: r, intent: in, locks: slices.Clone(locks), current: current}}, nil
}

// Claim is the admission linearization point. No socket operation is performed
// under business locks. A later Agent update applies to future claims, not this
// already-admitted fixed mapping. Any failed/unknown commit consumes the ticket
// but produces no request and cannot authorize a retry.
func (r *SkillPrepareResolver) Claim(ctx context.Context, t SkillPrepareTicket, payload p.SkillPreparePayload) (p.Request, error) {
	if t.state == nil || t.state.owner != r {
		return p.Request{}, fault(f.Forbidden)
	}
	s := t.state
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return p.Request{}, fault(f.InvalidState)
	}
	s.used = true
	s.mu.Unlock()
	if payload.Validate() != nil || payload.SkillID != s.intent.SkillID || payload.RevisionID != s.intent.RevisionID || string(payload.Revision) != s.intent.Revision.String() || payload.ArchiveSHA256 != s.intent.PackageSHA256.String() || payload.WorkspaceName != s.current.mapping.binding.Workspace {
		return p.Request{}, fault(f.InvalidArgument)
	}
	locks, err := r.plan(ctx, s.intent)
	if err != nil {
		return p.Request{}, err
	}
	if !sameReadFileLocks(locks, s.locks) {
		return p.Request{}, fault(f.VersionConflict)
	}
	err = r.transaction(ctx, s.intent, &payload, locks, func(v readFileCurrent) error {
		old := s.current
		if v.agentVersion != old.agentVersion || !slices.Equal(v.ids, old.ids) || !sameDefinition(v.definition, old.definition) || v.mapping != old.mapping {
			return fault(f.VersionConflict)
		}
		return nil
	})
	if err != nil {
		return p.Request{}, err
	}
	wire, err := p.EncodeSkillPreparePayload(payload)
	if err != nil {
		return p.Request{}, fault(f.InvalidState)
	}
	return p.Request{RequestID: s.intent.RequestID, OperationID: p.ID(s.intent.OperationID.String()), ExecutionID: p.ID(s.intent.ExecutionID.String()), ProjectID: p.ID(s.intent.ProjectID.String()), AgentID: p.ID(s.intent.AgentID.String()), Mount: p.Mount{MountID: p.ID(s.intent.MountID.String()), WorkspaceID: p.ID(s.current.mapping.binding.WorkspaceID.String())}, OperationName: p.SkillPrepareOperation, OperationRevision: p.SkillPrepareRevision, Payload: wire}, nil
}

func (t SkillPrepareTicket) RequestID() p.ID {
	if t.state == nil {
		return ""
	}
	return t.state.intent.RequestID
}

// RequiredLocks exposes only the fixed precollected lock set, not authority.
func (t SkillPrepareTicket) RequiredLocks() []f.LockRequest {
	if t.state == nil {
		return nil
	}
	return slices.Clone(t.state.locks)
}

// RequireCurrentInTx validates the original mapping for Transfer issuance in
// that provider's existing transaction. It neither consumes nor dispatches.
func (r *SkillPrepareResolver) RequireCurrentInTx(ctx context.Context, tx f.Tx, t SkillPrepareTicket) error {
	if t.state == nil || t.state.owner != r {
		return fault(f.Forbidden)
	}
	s := t.state
	current, err := r.read(ctx, tx, s.intent, nil, s.locks)
	if err != nil {
		return err
	}
	old := s.current
	if current.agentVersion != old.agentVersion || !slices.Equal(current.ids, old.ids) || !sameDefinition(current.definition, old.definition) || current.mapping != old.mapping {
		return fault(f.VersionConflict)
	}
	return nil
}
