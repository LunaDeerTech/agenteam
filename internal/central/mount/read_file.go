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
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type ReadFileResolver struct {
	store  Store
	agents ac.ExecutionConfiguration
	owner  mc.ReadFileAuthority
}

func NewReadFileResolver(store Store, agents ac.ExecutionConfiguration, owner mc.ReadFileAuthority) (*ReadFileResolver, error) {
	if nilPort(store) || nilPort(agents) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ReadFileResolver{store: store, agents: agents, owner: owner}, nil
}

// Tickets have no public constructor or mutable DTO fields. Copies share the
// single-use state. Resolve is not dispatch admission; Claim rechecks current
// facts in the original Store and only returns after a known commit.
type ReadFileTicket struct{ state *readFileTicketState }
type readFileTicketState struct {
	mu      sync.Mutex
	used    bool
	owner   *ReadFileResolver
	intent  mc.ReadFileIntent
	locks   []f.LockRequest
	current readFileCurrent
}
type readFileCurrent struct {
	agentVersion f.Version
	ids          []i.MountID
	definition   definition
	mapping      workspaceMapping
}

func (ReadFileTicket) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "read_file_ticket") }
func (ReadFileTicket) MarshalJSON() ([]byte, error) { return []byte(`"read_file_ticket"`), nil }
func (ReadFileTicket) LogValue() slog.Value         { return slog.StringValue("read_file_ticket") }
func (t ReadFileTicket) RunnerID() rc.RunnerID {
	if t.state == nil {
		return rc.RunnerID{}
	}
	return t.state.current.definition.Runner
}

func readFileConfigurationRequest(in mc.ReadFileIntent) ac.ExecutionConfigurationRequest {
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}

func (r *ReadFileResolver) plan(in mc.ReadFileIntent) ([]f.LockRequest, error) {
	if r == nil || nilPort(r.store) || nilPort(r.agents) || nilPort(r.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if in.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	more, err := r.owner.ReadFileLocks(in)
	if err != nil {
		return nil, portError(err)
	}
	locks := append(readFileConfigurationRequest(in).RequiredLocks(), more...)
	op, _ := f.AggregateLock(f.OperationAggregate, in.OperationID.String())
	locks = append(locks, f.LockRequest{Key: op, Mode: f.Exclusive})
	return oc.NormalizeLocks(locks)
}

func (r *ReadFileResolver) read(ctx context.Context, tx f.Tx, in mc.ReadFileIntent, locks []f.LockRequest) (readFileCurrent, error) {
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
	err = r.owner.WithReadFileInTx(ctx, tx, in, func(authorized context.Context) (err error) {
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
		config, err := r.agents.ReadExecutionConfigurationInTx(authorized, tx, readFileConfigurationRequest(in))
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

func (r *ReadFileResolver) transaction(ctx context.Context, in mc.ReadFileIntent, locks []f.LockRequest, work func(readFileCurrent) error) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	id, err := f.NewID[struct{}]()
	if err != nil {
		return fault(f.DependencyUnavailable)
	}
	cause, err := f.NewRecoveryCause("runner-read-file", id.String(), "")
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

func (r *ReadFileResolver) Resolve(ctx context.Context, in mc.ReadFileIntent) (ReadFileTicket, error) {
	locks, err := r.plan(in)
	if err != nil {
		return ReadFileTicket{}, err
	}
	var current readFileCurrent
	if err = r.transaction(ctx, in, locks, func(v readFileCurrent) error { current = v; return nil }); err != nil {
		return ReadFileTicket{}, err
	}
	return ReadFileTicket{state: &readFileTicketState{owner: r, intent: in, locks: slices.Clone(locks), current: current}}, nil
}

// Claim is the admission linearization point. No socket operation is performed
// under business locks. A later Agent update applies to future claims, not this
// already-admitted fixed mapping. Any failed/unknown commit consumes the ticket
// but produces no request and cannot authorize a retry.
func (r *ReadFileResolver) Claim(ctx context.Context, t ReadFileTicket) (p.Request, error) {
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
	err = r.transaction(ctx, s.intent, locks, func(v readFileCurrent) error {
		old := s.current
		if v.agentVersion != old.agentVersion || !slices.Equal(v.ids, old.ids) || !sameDefinition(v.definition, old.definition) || v.mapping != old.mapping {
			return fault(f.VersionConflict)
		}
		return nil
	})
	if err != nil {
		return p.Request{}, err
	}
	payload, err := p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: s.current.mapping.binding.Workspace, Arguments: s.intent.Arguments})
	if err != nil {
		return p.Request{}, fault(f.InvalidState)
	}
	return p.Request{OperationID: p.ID(s.intent.OperationID.String()), ExecutionID: p.ID(s.intent.ExecutionID.String()), ProjectID: p.ID(s.intent.ProjectID.String()), AgentID: p.ID(s.intent.AgentID.String()), Mount: p.Mount{MountID: p.ID(s.intent.MountID.String()), WorkspaceID: p.ID(s.current.mapping.binding.WorkspaceID.String())}, OperationName: p.ReadFileOperation, OperationRevision: p.ReadFileRevision, Payload: payload}, nil
}

func sameReadFileLocks(a, b []f.LockRequest) bool {
	if len(a) != len(b) {
		return false
	}
	for n := range a {
		if a[n].Key.Canonical() != b[n].Key.Canonical() || a[n].Mode != b[n].Mode {
			return false
		}
	}
	return true
}
