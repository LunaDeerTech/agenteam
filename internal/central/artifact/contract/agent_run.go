package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// AgentRunAuthority belongs to the actual Tool Runtime/Execution composition.
// A public Actor or Invocation is not execution authority. The implementation
// must consume its original live invocation witness, verify the durable exact
// Operation/ToolCall and current Execution/Agent/tool policy, and include every
// parent lock in discovery. Object owner calls carry only Actor/ExecutionID;
// their remaining provenance must come from that same private witness.
//
// InTx methods use the caller's original transaction and already-held complete
// lock union. They never acquire locks, open a transaction or reuse a grant.
type AgentRunAuthority interface {
	DiscoverArtifactRun(context.Context, AccessSubject) (oc.AccessDependencies, error)
	DiscoverArtifactRunInTx(context.Context, f.Tx, AccessSubject) (oc.AccessDependencies, error)
	RequireArtifactRunInTx(context.Context, f.Tx, AccessSubject, i.AccessIntent) error
}
