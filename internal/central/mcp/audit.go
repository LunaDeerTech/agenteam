package mcp

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
)

type AuditAdapter struct{ appender ac.Appender }

func NewAuditAdapter(appender ac.Appender) (*AuditAdapter, error) {
	if nilPort(appender) {
		return nil, fail(f.DependencyUnbound)
	}
	return &AuditAdapter{appender}, nil
}
func (a *AuditAdapter) AppendMCPInTx(ctx context.Context, tx f.Tx, event AuditEvent) error {
	if a == nil || nilPort(a.appender) {
		return fail(f.DependencyUnbound)
	}
	entry, key, err := auditEntry(event)
	if err != nil {
		return err
	}
	_, err = a.appender.AppendInTx(ctx, tx, entry, key)
	return safeError(err)
}
func auditEntry(event AuditEvent) (ac.Entry, ac.AppendKey, error) {
	action := ac.Action(event.Action)
	metadata, err := ac.MCPMetadata(action, ac.MCPMetadataFields{ConfigID: event.ConfigID, ConnectionID: event.ConnectionID, Version: event.Version, ToolCount: event.ToolCount, ResourceCount: event.ResourceCount, InvalidCount: event.InvalidCount, ErrorCode: event.ErrorCode})
	if err != nil {
		return ac.Entry{}, ac.AppendKey{}, safeError(err)
	}
	kind, resourceID := ac.MCPConfigResource, event.ConfigID
	if event.ConnectionID != "" {
		kind, resourceID = ac.MCPConnectionResource, event.ConnectionID
	}
	resource, err := ac.NewResource(kind, resourceID)
	if err != nil {
		return ac.Entry{}, ac.AppendKey{}, safeError(err)
	}
	outcome := ac.Success
	if event.ErrorCode != "" {
		outcome = ac.Failed
	}
	entry, err := ac.NewEntry(ac.EntryFields{Actor: event.Actor, Scope: event.Scope, Action: action, Resource: resource, Outcome: outcome, Metadata: metadata})
	if err != nil {
		return ac.Entry{}, ac.AppendKey{}, safeError(err)
	}
	key, err := ac.NewAppendKey(ac.MCPProducer, event.CauseID, 0)
	return entry, key, safeError(err)
}
func (s *Service) RequireMCPAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if ctx == nil || entry.Validate() != nil || key.Validate() != nil || !ac.MCPAction(entry.Fields().Action) {
		return fail(f.Forbidden)
	}
	w, ok := ctx.Value(auditWitnessKey{}).(*auditWitness)
	if !ok || w == nil {
		return fail(f.Forbidden)
	}
	event := w.event
	if err := s.requireAuditWitness(ctx, tx, event); err != nil {
		return err
	}
	expected, expectedKey, err := auditEntry(event)
	if err != nil {
		return err
	}
	a, b := entry.Fields(), expected.Fields()
	am, err := a.Metadata.MCPFields()
	if err != nil {
		return fail(f.Forbidden)
	}
	bm, err := b.Metadata.MCPFields()
	if err != nil {
		return fail(f.Forbidden)
	}
	if !a.Actor.Equal(b.Actor) || !a.Scope.Equal(b.Scope) || a.Action != b.Action || a.Outcome != b.Outcome || a.Resource.Details() != b.Resource.Details() || a.Associations != b.Associations || am != bm || key.Details() != expectedKey.Details() {
		return fail(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	var matches bool
	if event.ConnectionID != "" {
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections WHERE id=$1 AND config_id=$2 AND project_id=$3 AND version=$4)`, event.ConnectionID, event.ConfigID, event.Scope.Details().ProjectID, int64(event.Version)).Scan(&matches)
	} else if a.Action == ac.MCPConfigDelete {
		err = x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_mcp.configs WHERE id=$1)`, event.ConfigID).Scan(&matches)
	} else {
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.configs WHERE id=$1 AND scope_kind=$2 AND COALESCE(project_id,'')=$3 AND version=$4)`, event.ConfigID, string(event.Scope.Details().Kind), event.Scope.Details().ProjectID, int64(event.Version)).Scan(&matches)
	}
	if err != nil {
		return safeError(err)
	}
	if !matches {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}
func (s *Service) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if entry.Fields().Scope.Details().Kind != id.ProjectScope {
		return fail(f.Forbidden)
	}
	return s.RequireMCPAuditInTx(ctx, tx, entry, key)
}
func auditFailure(code string) string {
	switch protocol.Code(code) {
	case protocol.AuthenticationRequired, protocol.SensitiveResponse:
		return string(ac.CredentialDenied)
	case protocol.Cancelled:
		return string(ac.Cancelled)
	case protocol.Timeout:
		return string(ac.Timeout)
	case protocol.TooLarge:
		return string(ac.ResponseLimit)
	case protocol.InvalidRequest:
		return string(ac.InvalidTarget)
	case protocol.InvalidResponse:
		return string(ac.IntegrityMismatch)
	default:
		return string(ac.InternalError)
	}
}

var _ ac.MCPAuthority = (*Service)(nil)
var _ ac.ProjectFactAuthority = (*Service)(nil)
