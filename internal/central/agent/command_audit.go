package agent

import (
	"context"
	"encoding/json"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func recordAudit(r *commandRecord, actor i.Actor) (ac.Entry, ac.AppendKey, error) {
	var empty ac.Entry
	var key ac.AppendKey
	if r == nil || r.validate() != nil || len(r.ChangedFields) == 0 || r.User.String() != actor.Details().UserID {
		return empty, key, invalid()
	}
	action, producer := ac.Action(r.Name), ac.Producer("agent")
	// The formal Audit registry must explicitly support Agent. A generic typed
	// Entry or a nonnil Appender does not supply an absent producer dispatch.
	if !action.Valid() || !producer.Valid() {
		return empty, key, fault(f.DependencyUnbound)
	}
	raw, err := json.Marshal(struct {
		AgentID       string    `json:"agent_id"`
		Version       f.Version `json:"version"`
		CommandID     string    `json:"command_id"`
		ChangedFields []string  `json:"changed_fields"`
	}{r.Target.String(), r.After.Fields().Core.Version, r.ID.String(), r.ChangedFields})
	if err != nil {
		return empty, key, unavailable(err)
	}
	metadata, err := ac.DecodeMetadata(action, raw)
	if err != nil {
		return empty, key, portError(err)
	}
	scope, err := i.InProject(r.Project)
	if err != nil {
		return empty, key, invalid()
	}
	resource, err := ac.NewResource(ac.AgentResource, r.Target.String())
	if err != nil {
		return empty, key, invalid()
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		return empty, key, portError(err)
	}
	digest, err := cursor.Digest([]byte(r.identity().Canonical()))
	if err != nil {
		return empty, key, unavailable(err)
	}
	key, err = ac.NewAppendKey(producer, digest.String(), 0)
	return entry, key, err
}

// Project invokes this only after its current Human Owner check. The Agent
// checker nevertheless requires the actual same-Tx canonical writer witness,
// verifies the complete command/postimage again, and compares every Entry field.
func (a *Authority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	d := entry.Fields()
	if d.Actor.Validate() != nil || d.Actor.Details().Kind != i.Human || d.Scope.Details().Kind != i.ProjectScope || d.Resource.Details().Kind != ac.AgentResource || d.Outcome != ac.Success || d.Action != ac.Action(c.CreateAgentCommand) && d.Action != ac.Action(c.UpdateAgentCommand) || key.Details().Producer != ac.Producer("agent") {
		return fault(f.Forbidden)
	}
	w, ok := ctx.Value(mutationWitnessKey{}).(mutationWitness)
	if !ok || w.authority != a.state || w.tx != tx || !sameValue(w.actor.Details(), d.Actor.Details()) {
		return fault(f.Forbidden)
	}
	project, err := f.ParseID[i.Project](d.Scope.Details().ProjectID)
	if err != nil {
		return invalid()
	}
	if w.after.Fields().Core.ProjectID != project || w.after.Fields().Core.ID.String() != d.Resource.Details().ID {
		return fault(f.Forbidden)
	}
	x, err := a.state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	// The witness's command UUID selects an Agent-owned row; user-supplied
	// metadata and append-key material cannot choose another command.
	var name, keyText string
	if err = x.QueryRow(ctx, `SELECT command_name,idempotency_key FROM agenteam_agent.commands WHERE project_id=$1 AND id=$2`, project.String(), w.command.String()).Scan(&name, &keyText); err != nil {
		return unavailable(err)
	}
	command := c.CommandName(name)
	commandKey := f.IdempotencyKey(keyText)
	identity, err := c.AgentCommandIdentity(project, command, commandKey)
	if err != nil {
		return unavailable(err)
	}
	r, err := a.checkApplied(ctx, tx, d.Actor, project, w.after.Fields().Core.ID, identity, w.mapping)
	if err != nil {
		return err
	}
	expected, expectedKey, err := recordAudit(r, d.Actor)
	if err != nil {
		return err
	}
	e := expected.Fields()
	if !sameValue(d.Scope.Details(), e.Scope.Details()) || !sameValue(d.Actor.Details(), e.Actor.Details()) || d.Action != e.Action || d.Outcome != e.Outcome || !sameValue(d.Resource.Details(), e.Resource.Details()) || !sameValue(json.RawMessage(d.Metadata.JSON()), json.RawMessage(e.Metadata.JSON())) || !sameValue(d.Associations, e.Associations) || key.Details() != expectedKey.Details() {
		return fault(f.Forbidden)
	}
	return nil
}

var _ ac.ProjectFactAuthority = (*Authority)(nil)
