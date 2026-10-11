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

type DirectoryResolver struct {
	store  Store
	agents ac.ExecutionConfiguration
	owner  mc.DirectoryAuthority
}

func NewDirectoryResolver(store Store, agents ac.ExecutionConfiguration, owner mc.DirectoryAuthority) (*DirectoryResolver, error) {
	if nilPort(store) || nilPort(agents) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &DirectoryResolver{store: store, agents: agents, owner: owner}, nil
}

// Tickets have no public constructor or mutable DTO fields. Copies share the
// single-use state. Resolve is not dispatch admission; Claim rechecks current
// facts in the original Store and only returns after a known commit.
type DirectoryTicket struct{ state *directoryTicketState }
type directoryTicketState struct {
	mu      sync.Mutex
	used    bool
	owner   *DirectoryResolver
	intent  mc.DirectoryIntent
	locks   []f.LockRequest
	current directoryCurrent
}
type directoryCurrent = readFileCurrent

func (DirectoryTicket) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "directory_ticket") }
func (DirectoryTicket) MarshalJSON() ([]byte, error) { return []byte(`"directory_ticket"`), nil }
func (DirectoryTicket) LogValue() slog.Value         { return slog.StringValue("directory_ticket") }
func (t DirectoryTicket) Operation() string {
	if t.state == nil {
		return ""
	}
	return t.state.intent.Operation
}

func (t DirectoryTicket) RunnerID() rc.RunnerID {
	if t.state == nil {
		return rc.RunnerID{}
	}
	return t.state.current.definition.Runner
}

func directoryConfigurationRequest(in mc.DirectoryIntent) ac.ExecutionConfigurationRequest {
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}

func (r *DirectoryResolver) plan(in mc.DirectoryIntent) ([]f.LockRequest, error) {
	if r == nil || nilPort(r.store) || nilPort(r.agents) || nilPort(r.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if in.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	more, err := r.owner.DirectoryLocks(in)
	if err != nil {
		return nil, portError(err)
	}
	locks := append(directoryConfigurationRequest(in).RequiredLocks(), more...)
	op, _ := f.AggregateLock(f.OperationAggregate, in.OperationID.String())
	locks = append(locks, f.LockRequest{Key: op, Mode: f.Exclusive})
	return oc.NormalizeLocks(locks)
}

func (r *DirectoryResolver) read(ctx context.Context, tx f.Tx, in mc.DirectoryIntent, locks []f.LockRequest) (directoryCurrent, error) {
	var out directoryCurrent
	x, err := r.store.InTx(tx)
	if err != nil {
		return out, portError(err)
	}
	if err = r.store.RequireHeldLocks(ctx, tx, slices.Clone(locks)); err != nil {
		return out, portError(err)
	}
	calls := 0
	var callbackErr error
	err = r.owner.WithDirectoryInTx(ctx, tx, in, func(authorized context.Context) (err error) {
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
		config, err := r.agents.ReadExecutionConfigurationInTx(authorized, tx, directoryConfigurationRequest(in))
		if err != nil {
			return portError(err)
		}
		out, err = readCurrentMount(authorized, x, config, in.ProjectID, in.AgentID, in.MountID)
		return err
	})
	if err != nil {
		return directoryCurrent{}, portError(err)
	}
	if calls != 1 {
		return directoryCurrent{}, fault(f.Forbidden)
	}
	if callbackErr != nil {
		return directoryCurrent{}, portError(callbackErr)
	}
	if err = ctx.Err(); err != nil {
		return directoryCurrent{}, err
	}
	return out, nil
}

func (r *DirectoryResolver) transaction(ctx context.Context, in mc.DirectoryIntent, locks []f.LockRequest, work func(directoryCurrent) error) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	id, err := f.NewID[struct{}]()
	if err != nil {
		return fault(f.DependencyUnavailable)
	}
	cause, err := f.NewRecoveryCause("runner-directory", id.String(), "")
	if err != nil {
		return fault(f.InvalidArgument)
	}
	result := r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, slices.Clone(locks)); err != nil {
			return portError(err)
		}
		current, err := r.read(ctx, tx, in, locks)
		if err != nil {
			return err
		}
		return work(current)
	})
	return commitError(result)
}

func (r *DirectoryResolver) Resolve(ctx context.Context, in mc.DirectoryIntent) (DirectoryTicket, error) {
	locks, err := r.plan(in)
	if err != nil {
		return DirectoryTicket{}, err
	}
	var current directoryCurrent
	if err = r.transaction(ctx, in, locks, func(v directoryCurrent) error { current = v; return nil }); err != nil {
		return DirectoryTicket{}, err
	}
	return DirectoryTicket{state: &directoryTicketState{owner: r, intent: in, locks: slices.Clone(locks), current: current}}, nil
}

// Claim is the admission linearization point. No socket operation is performed
// under business locks. A later Agent update applies to future claims, not this
// already-admitted fixed mapping. Any failed/unknown commit consumes the ticket
// but produces no request and cannot authorize a retry.
func (r *DirectoryResolver) Claim(ctx context.Context, t DirectoryTicket) (p.Request, error) {
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
	locks, err := r.plan(s.intent)
	if err != nil {
		return p.Request{}, err
	}
	if !sameReadFileLocks(locks, s.locks) {
		return p.Request{}, fault(f.VersionConflict)
	}
	err = r.transaction(ctx, s.intent, locks, func(v directoryCurrent) error {
		old := s.current
		if v.agentVersion != old.agentVersion || !slices.Equal(v.ids, old.ids) || !sameDefinition(v.definition, old.definition) || v.mapping != old.mapping {
			return fault(f.VersionConflict)
		}
		return nil
	})
	if err != nil {
		return p.Request{}, err
	}
	payload, err := p.EncodeDirectoryPayload(s.intent.Operation, p.DirectoryPayload{WorkspaceName: s.current.mapping.binding.Workspace, Arguments: s.intent.Arguments})
	if err != nil {
		return p.Request{}, fault(f.InvalidState)
	}
	return p.Request{OperationID: p.ID(s.intent.OperationID.String()), ExecutionID: p.ID(s.intent.ExecutionID.String()), ProjectID: p.ID(s.intent.ProjectID.String()), AgentID: p.ID(s.intent.AgentID.String()), Mount: p.Mount{MountID: p.ID(s.intent.MountID.String()), WorkspaceID: p.ID(s.current.mapping.binding.WorkspaceID.String())}, OperationName: s.intent.Operation, OperationRevision: p.DirectoryRevision, Payload: payload}, nil
}
