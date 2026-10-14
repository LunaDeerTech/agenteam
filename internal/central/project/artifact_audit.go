package project

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (a *Authority) checkArtifactAuditInTx(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) error {
	e, k := entry.Fields(), key.Details()
	if e.Scope.Details().Kind != identity.ProjectScope || (e.Actor.Details().Kind != identity.Human && e.Actor.Details().Kind != identity.AgentRun) ||
		e.Outcome != au.Success ||
		(e.Actor.Details().Kind == identity.Human && e.Associations != (au.Associations{})) || k.Producer != au.ArtifactProducer || k.Ordinal != 0 {
		return fault(f.Forbidden)
	}
	intent := identity.Read
	resource := e.Resource.Details()
	if e.Action == au.ArtifactList {
		if resource.Kind != au.ArtifactCollectionResource || resource.ID != e.Scope.Details().ProjectID {
			return fault(f.Forbidden)
		}
	} else if resource.Kind != au.ArtifactResource {
		return fault(f.Forbidden)
	}
	switch e.Action {
	case au.ArtifactCreate:
		intent = identity.Mutate
	case au.ArtifactRead:
	case au.ArtifactList:
	case au.ArtifactDownload:
		var meta struct {
			Phase au.ContentPhase `json:"phase"`
			Sent  string          `json:"sent_bytes"`
		}
		if json.Unmarshal(e.Metadata.JSON(), &meta) != nil || meta.Phase != au.IssuedPhase || meta.Sent != "0" {
			return fault(f.Forbidden)
		}
	default:
		return fault(f.Forbidden)
	}
	project, err := parseID[identity.Project](e.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	if e.Actor.Details().Kind == identity.AgentRun {
		if err = artifact.CheckAgentAuditAuthorizationInTx(ctx, tx, a.state().store, entry, key); err != nil {
			return portError(err)
		}
	} else {
		if _, err = a.RequireOwnerInTx(ctx, tx, e.Actor, project, intent); err != nil {
			return err
		}
	}
	provider := a.state().auditFacts[au.ArtifactProducer]
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}
