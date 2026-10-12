package mount

import (
	"bytes"
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

type WriteFileResolver struct {
	store  Store
	agents ac.ExecutionConfiguration
	owner  mc.WriteFileAuthority
}

func NewWriteFileResolver(store Store, agents ac.ExecutionConfiguration, owner mc.WriteFileAuthority) (*WriteFileResolver, error) {
	if nilPort(store) || nilPort(agents) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &WriteFileResolver{store: store, agents: agents, owner: owner}, nil
}

// Tickets have no public constructor or mutable DTO fields. Copies share the
// single-use state. Resolve is not dispatch admission; Claim rechecks current
// facts in the original Store and only returns after a known commit.
type WriteFileTicket struct{ state *writeFileTicketState }
type writeFileTicketState struct {
	mu      sync.Mutex
	used    bool
	owner   *WriteFileResolver
	intent  mc.WriteFileIntent
	locks   []f.LockRequest
	current readFileCurrent
}

func (WriteFileTicket) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "write_file_ticket") }
func (WriteFileTicket) MarshalJSON() ([]byte, error) { return []byte(`"write_file_ticket"`), nil }
func (WriteFileTicket) LogValue() slog.Value         { return slog.StringValue("write_file_ticket") }
func (t WriteFileTicket) RunnerID() rc.RunnerID {
	if t.state == nil {
		return rc.RunnerID{}
	}
	return t.state.current.definition.Runner
}

// Target exposes the ticket's fixed safe metadata without consuming it or
// granting dispatch. The original Claim still rechecks the complete mapping.
func (t WriteFileTicket) Target() (mc.WriteFileTarget, error) {
	if t.state == nil {
		return mc.WriteFileTarget{}, fault(f.Forbidden)
	}
	return writeFileTarget(t.state.current)
}

func writeFileConfigurationRequest(in mc.WriteFileIntent) ac.ExecutionConfigurationRequest {
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}

func (r *WriteFileResolver) plan(in mc.WriteFileIntent) ([]f.LockRequest, error) {
	if r == nil || nilPort(r.store) || nilPort(r.agents) || nilPort(r.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if in.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	owned := in
	owned.Arguments.Data = bytes.Clone(in.Arguments.Data)
	defer clear(owned.Arguments.Data)
	more, err := r.owner.WriteFileLocks(owned)
	if err != nil {
		return nil, portError(err)
	}
	locks := append(writeFileConfigurationRequest(in).RequiredLocks(), more...)
	op, _ := f.AggregateLock(f.OperationAggregate, in.OperationID.String())
	locks = append(locks, f.LockRequest{Key: op, Mode: f.Exclusive})
	return oc.NormalizeLocks(locks)
}

func (r *WriteFileResolver) read(ctx context.Context, tx f.Tx, in mc.WriteFileIntent, requestID p.ID, locks []f.LockRequest) (readFileCurrent, error) {
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
	owned := in
	owned.Arguments.Data = bytes.Clone(in.Arguments.Data)
	defer clear(owned.Arguments.Data)
	err = r.owner.WithWriteFileInTx(ctx, tx, owned, requestID, func(authorized context.Context) (err error) {
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
		config, err := r.agents.ReadExecutionConfigurationInTx(authorized, tx, writeFileConfigurationRequest(in))
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

func (r *WriteFileResolver) transaction(ctx context.Context, in mc.WriteFileIntent, requestID p.ID, locks []f.LockRequest, work func(readFileCurrent) error) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	id, err := f.NewID[struct{}]()
	if err != nil {
		return fault(f.DependencyUnavailable)
	}
	cause, err := f.NewRecoveryCause("runner-write-file", id.String(), "")
	if err != nil {
		return fault(f.InvalidArgument)
	}
	completed := false
	result := r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, slices.Clone(locks)); err != nil {
			return portError(err)
		}
		current, err := r.read(ctx, tx, in, requestID, locks)
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

func (r *WriteFileResolver) Resolve(ctx context.Context, in mc.WriteFileIntent) (WriteFileTicket, error) {
	in.Arguments.Data = bytes.Clone(in.Arguments.Data)
	retained := false
	defer func() {
		if !retained {
			clear(in.Arguments.Data)
		}
	}()
	locks, err := r.plan(in)
	if err != nil {
		return WriteFileTicket{}, err
	}
	var current readFileCurrent
	if err = r.transaction(ctx, in, "", locks, func(v readFileCurrent) error { current = v; return nil }); err != nil {
		return WriteFileTicket{}, err
	}
	retained = true
	return WriteFileTicket{state: &writeFileTicketState{owner: r, intent: in, locks: slices.Clone(locks), current: current}}, nil
}

// Claim is the admission linearization point. No socket operation is performed
// under business locks. A later Agent update applies to future claims, not this
// already-admitted fixed mapping. Any failed/unknown commit consumes the ticket
// but produces no request and cannot authorize a retry.
func (r *WriteFileResolver) Claim(ctx context.Context, t WriteFileTicket, requestID p.ID) (p.Request, error) {
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
	defer clear(s.intent.Arguments.Data)
	if !requestID.Valid() {
		return p.Request{}, fault(f.InvalidArgument)
	}
	locks, err := r.plan(s.intent)
	if err != nil {
		return p.Request{}, err
	}
	if !sameReadFileLocks(locks, s.locks) {
		return p.Request{}, fault(f.VersionConflict)
	}
	err = r.transaction(ctx, s.intent, requestID, locks, func(v readFileCurrent) error {
		old := s.current
		if v.agentVersion != old.agentVersion || !slices.Equal(v.ids, old.ids) || !sameDefinition(v.definition, old.definition) || v.mapping != old.mapping {
			return fault(f.VersionConflict)
		}
		return nil
	})
	if err != nil {
		return p.Request{}, err
	}
	payload, err := p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: s.current.mapping.binding.Workspace, Arguments: s.intent.Arguments})
	if err != nil {
		return p.Request{}, fault(f.InvalidState)
	}
	return p.Request{RequestID: requestID, OperationID: p.ID(s.intent.OperationID.String()), ExecutionID: p.ID(s.intent.ExecutionID.String()), ProjectID: p.ID(s.intent.ProjectID.String()), AgentID: p.ID(s.intent.AgentID.String()), Mount: p.Mount{MountID: p.ID(s.intent.MountID.String()), WorkspaceID: p.ID(s.current.mapping.binding.WorkspaceID.String())}, OperationName: p.WriteFileOperation, OperationRevision: p.WriteFileRevision, Payload: payload}, nil
}
