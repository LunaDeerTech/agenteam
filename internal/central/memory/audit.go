package memory

import (
	"context"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type auditKey struct{}
type auditWitness struct {
	service *Service
	tx      f.Tx
	event   AuditEvent
	active  bool
}

func (s *Service) appendAudit(ctx context.Context, tx f.Tx, event AuditEvent) error {
	w := &auditWitness{s, tx, event, true}
	defer func() { w.active = false }()
	return portError(s.deps.Audit.AppendMemoryInTx(context.WithValue(ctx, auditKey{}, w), tx, event))
}

// CheckProjectAuditInTx proves the event came from the exact Memory mutation
// still running in this transaction. App metadata construction grants nothing.
func (s *Service) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if s == nil || entry.Validate() != nil || key.Validate() != nil {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(auditKey{}).(*auditWitness)
	if w == nil || !w.active || w.service != s || w.tx != tx {
		return fault(f.Forbidden)
	}
	if _, err := s.store.InTx(tx); err != nil {
		return portError(err)
	}
	e, k := entry.Fields(), key.Details()
	expected := w.event
	if !e.Actor.Equal(expected.Actor) || e.Scope.Details().Kind != i.ProjectScope || e.Scope.Details().ProjectID != expected.Namespace.ProjectID.String() || e.Outcome != ac.Success || string(k.Producer) != "memory" || k.Ordinal != 0 || k.CauseRef != expected.CauseRef {
		return fault(f.Forbidden)
	}
	resource := e.Resource.Details()
	switch expected.Action {
	case "retain":
		if string(e.Action) != "memory.retain" || string(resource.Kind) != "memory_namespace" || resource.ID != expected.Namespace.AgentID.String() || k.CauseRef != expected.OperationID {
			return fault(f.Forbidden)
		}
	case "delete":
		if string(e.Action) != "memory.delete" || string(resource.Kind) != "memory" || resource.ID != expected.MemoryID {
			return fault(f.Forbidden)
		}
	default:
		return fault(f.Forbidden)
	}
	metadata, err := e.Metadata.MemoryFields()
	if err != nil || metadata.AgentID != expected.Namespace.AgentID.String() || metadata.OperationID != expected.OperationID || metadata.MemoryID != expected.MemoryID || metadata.Gate != expected.Gate || metadata.Added != expected.Added || metadata.Updated != expected.Updated || metadata.Deleted != expected.Deleted || metadata.Noop != expected.Noop {
		return fault(f.Forbidden)
	}
	associations := ac.Associations{}
	if expected.Actor.Details().Kind == i.AgentRun {
		associations.ExecutionID = expected.Actor.Details().ExecutionID
		associations.OperationID = expected.OperationID
	}
	if !reflect.DeepEqual(e.Associations, associations) {
		return fault(f.Forbidden)
	}
	return nil
}

var _ ac.ProjectFactAuthority = (*Service)(nil)
