package object

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type MCPUploadResolver struct{ store Store }

func NewMCPUploadResolver(store Store) (*MCPUploadResolver, error) {
	if nilPort(store) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	return &MCPUploadResolver{store: store}, nil
}

func mcpUploadRequest(ctx context.Context, owner oc.ObjectOwner, key f.IdempotencyKey) error {
	if ctx == nil || owner.Validate() != nil || owner.Details().Kind != oc.MCPContent || owner.Details().ProjectID == "" || key.Validate() != nil || string(key) != owner.Details().ID {
		return invalid()
	}
	return unavailableIf(ctx.Err())
}

func (r *MCPUploadResolver) Discover(ctx context.Context, owner oc.ObjectOwner, key f.IdempotencyKey) (oc.MCPUploadMapping, bool, error) {
	if err := mcpUploadRequest(ctx, owner, key); err != nil {
		return oc.MCPUploadMapping{}, false, err
	}
	if r == nil || nilPort(r.store) {
		return oc.MCPUploadMapping{}, false, failure(f.DependencyUnbound, nil)
	}
	return mcpUploadMapping(ctx, r.store, owner, key)
}

func (r *MCPUploadResolver) ResolveInTx(ctx context.Context, tx f.Tx, owner oc.ObjectOwner, key f.IdempotencyKey) (oc.MCPUploadMapping, bool, error) {
	if err := mcpUploadRequest(ctx, owner, key); err != nil {
		return oc.MCPUploadMapping{}, false, err
	}
	if r == nil || nilPort(r.store) {
		return oc.MCPUploadMapping{}, false, failure(f.DependencyUnbound, nil)
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return oc.MCPUploadMapping{}, false, portError(err)
	}
	return mcpUploadMapping(ctx, x, owner, key)
}

func mcpUploadMapping(ctx context.Context, x postgres.SQLExecutor, owner oc.ObjectOwner, key f.IdempotencyKey) (oc.MCPUploadMapping, bool, error) {
	command, err := commandIdentity(owner, key)
	if err != nil {
		return oc.MCPUploadMapping{}, false, err
	}
	u, found, err := loadCommand(ctx, x, command)
	if err != nil || !found {
		return oc.MCPUploadMapping{}, false, err
	}
	if !u.owner.Equal(owner) || u.command != key || u.initiator != id.AgentRun || u.existence != oc.ProspectiveOwner {
		return oc.MCPUploadMapping{}, false, failure(f.Forbidden, nil)
	}
	m := oc.MCPUploadMapping{Owner: u.owner, Key: u.command, UploadID: u.id, ObjectID: u.object,
		AttemptID: u.attempt, CreationCause: u.creation, State: u.state, Disposition: u.disposition}
	m.AgentID, err = f.ParseID[id.Agent](u.initiatorID)
	if err != nil {
		return oc.MCPUploadMapping{}, false, unavailable(err)
	}
	m.ExecutionID, err = f.ParseID[id.Execution](u.executionID)
	if err != nil {
		return oc.MCPUploadMapping{}, false, unavailable(err)
	}
	if u.attempt != (oc.AttemptID{}) {
		a, found, err := loadAttempt(ctx, x, u.attempt)
		if err != nil {
			return oc.MCPUploadMapping{}, false, err
		}
		if !found || a.kind != "private_candidate" || a.upload != u.id || a.object != u.object || a.id != u.attempt {
			return oc.MCPUploadMapping{}, false, failure(f.Forbidden, nil)
		}
		m.ProcessID = a.process
	}
	if err := m.Validate(); err != nil {
		return oc.MCPUploadMapping{}, false, unavailable(err)
	}
	return m, true, nil
}

var _ oc.MCPUploadResolver = (*MCPUploadResolver)(nil)
