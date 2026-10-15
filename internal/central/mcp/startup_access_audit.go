package mcp

import (
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func (a *Authority) RequireMCPAccessAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RequireMCPAccessAuditInTx(ctx, tx, entry, key)
}

func (s *Service) RequireMCPAccessAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	w, err := s.startupUse(ctx)
	if err != nil {
		return err
	}
	e, k := entry.Fields(), key.Details()
	actor := e.Actor.Details()
	var metadata struct {
		Consumer ac.Consumer `json:"consumer"`
	}
	if entry.Validate() != nil || key.Validate() != nil || e.Action != ac.AccessDeny || e.Outcome != ac.Denied || e.Resource.Details().Kind != ac.PolicyResource || e.Scope.Details().Kind != id.ProjectScope || e.Scope.Details().ProjectID != w.connection.ProjectID.String() || actor.Kind != id.Service || actor.ServiceName != id.OutboundService || actor.ProjectID != w.connection.ProjectID.String() || actor.CauseRef != w.request || k.Producer != ac.AccessProducer || k.CauseRef != w.request || k.Ordinal != 0 || e.Associations != (ac.Associations{RequestID: w.request}) || json.Unmarshal(e.Metadata.JSON(), &metadata) != nil || metadata.Consumer != ac.MCP {
		return fail(f.Forbidden)
	}
	// D04 already holds Project SH. A denial append cannot add catalog/Secret
	// locks or open a second transaction; the original immutable claim suffices.
	project, err := f.ProjectLock(w.connection.ProjectID.String())
	if err != nil {
		return err
	}
	if err = s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: project, Mode: f.Shared}}); err != nil {
		return safeError(err)
	}
	if err = s.owner.requireInTx(ctx, tx, w.call); err != nil {
		return err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	fact, err := loadStartupFact(ctx, x, w.request)
	if err != nil {
		return err
	}
	if !w.matches(fact) || fact.phase != "pending" || !fact.started || fact.retired || fact.released {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}

var _ pc.MCPAccessAuditProof = (*Authority)(nil)
var _ pc.MCPAccessAuditProof = (*Service)(nil)
