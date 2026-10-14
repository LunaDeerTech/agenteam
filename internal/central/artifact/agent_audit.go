package artifact

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func artifactAuditActor(e au.EntryFields) bool {
	if e.Actor.Details().Kind == i.Human {
		return e.Associations == (au.Associations{})
	}
	a := e.Associations
	if e.Actor.Details().Kind != i.AgentRun || e.Action == au.ArtifactDownload || a.ExecutionID == "" || a.OperationID == "" || a.ToolID == "" || a.ToolCallID == "" || e.Actor.Details().ExecutionID != a.ExecutionID {
		return false
	}
	return a == (au.Associations{ExecutionID: a.ExecutionID, OperationID: a.OperationID, ToolID: a.ToolID, ToolCallID: a.ToolCallID})
}

// CheckAgentAuditAuthorizationInTx consumes the original Artifact publication,
// read or list witness and repeats current Runtime/Project authorization before
// Project admits its audit. An entry, actor, public invocation, copied context
// after return or a different Store/Tx cannot manufacture that live witness.
func CheckAgentAuditAuthorizationInTx(ctx context.Context, tx f.Tx, store Store, entry au.Entry, key au.AppendKey) error {
	if ctx == nil || !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	e := entry.Fields()
	if e.Actor.Details().Kind != i.AgentRun || !artifactAuditActor(e) {
		return failure(f.Forbidden, nil)
	}
	w, _ := ctx.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
	if w == nil || !w.active.Load() || w.tx != tx || !sameProjectAuditStore(w.store, store) || !sameProjectAuditEntry(w.entry, entry) || w.key.Details() != key.Details() || nilPort(w.authority) {
		return failure(f.Forbidden, nil)
	}
	project, err := f.ParseID[i.Project](e.Scope.Details().ProjectID)
	if err != nil {
		return invalid()
	}
	subject := ac.AccessSubject{Actor: e.Actor, ProjectID: project, ExecutionID: e.Associations.ExecutionID, OperationID: e.Associations.OperationID, ToolID: e.Associations.ToolID, ToolCallID: e.Associations.ToolCallID}
	intent := i.Read
	if e.Action == au.ArtifactCreate {
		intent = i.Mutate
	}
	grant, err := w.authority.AuthorizeInTx(ctx, tx, subject, intent)
	if err != nil {
		return portError(err)
	}
	scope, _ := i.InProject(project)
	if !grant.Matches(e.Actor, scope, intent) || !w.active.Load() {
		return failure(f.Forbidden, nil)
	}
	return portOrNil(ctx.Err())
}
