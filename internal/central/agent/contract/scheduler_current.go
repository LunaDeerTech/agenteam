package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// SchedulerCurrentAuthority validates the original Scheduler owner's private
// caller-Tx intent before Agent existence is read. A preclaim can precede the
// pending row: its owner must prove its own active issuer, original context/Tx,
// complete held lock union, Actor/cause and request. A persisted Launch uses its
// own bound current handoff; neither a Service name nor an exported DTO grants
// access. This callback must not recurse into the Agent reader or coordinator.
type SchedulerCurrentAuthority interface {
	RequireSchedulerCurrentIntentInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID) (pc.SchedulerIntent, error)
}

// SchedulerProjectGate is implemented by the same Project authority already
// owned by Agent. It supplies current initialized/active Project facts and the
// persisted Scheduler configuration in the original caller transaction.
type SchedulerProjectGate interface {
	RequireSchedulerProjectInTx(context.Context, f.Tx, i.ProjectID) (pc.SchedulerProject, error)
}

// SchedulerCurrentReferences is distinct from the Human WorkReferences port.
// It requires same-Store caller Tx, Project SH, Schedule EX and Agent SH, then
// the real private Scheduler intent and current Project gate before reading the
// canonical active Agent plus completed creation receipt. No Human Session is
// borrowed, no transaction/lock is acquired, and no Activity or domain fact is
// written. Returned AgentRef proves neither an idle slot nor Launch authority.
type SchedulerCurrentReferences interface {
	RequireSchedulerCurrentInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID) (AgentRef, error)
}
