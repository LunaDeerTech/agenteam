package memory

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestMemoryRecoveryRejectsBareOwnerBeforeProcessOrModelAccess(t *testing.T) {
	s := &Service{calls: map[*call]struct{}{}}
	reg, _ := i.RegisterService(i.MemoryRuntime)
	a, _ := NewModelAuthority(s, reg)
	a.process = testID[oc.Process](t)
	m := &Models{domain: s, authority: a}
	ns := c.Namespace{ProjectID: testID[i.Project](t), AgentID: testID[i.Agent](t)}
	actor, _ := i.NewAgentRun(ns.ProjectID, ns.AgentID, testID[i.Execution](t))
	entry := &call{service: s, namespace: ns, actor: actor, operation: testID[tc.Operation](t), intent: "recall"}
	candidate := abandonedGeneration{version: 1, target: generationTarget{serving: rc.ServingIdentity{GenerationID: testID[struct{}](t).String(), SnapshotID: testID[mc.Snapshot](t), Dimension: 2}, call: testID[mc.Call](t), process: testID[oc.Process](t)}}
	// The deliberately absent guard/Model ports would fail if either access
	// happened before rejecting the missing original Tool owner.
	requireFault(t, m.recoverGeneration(context.Background(), entry, candidate), f.Forbidden)
	ctx := context.WithValue(context.Background(), callKey{}, entry)
	requireFault(t, m.recoverGeneration(ctx, entry, candidate), f.Forbidden)
	s.calls[entry] = struct{}{}
	other := *entry
	requireFault(t, m.recoverGeneration(context.WithValue(ctx, callKey{}, &other), entry, candidate), f.Forbidden)
	candidate.target.process = a.process
	requireFault(t, m.recoverGeneration(ctx, entry, candidate), f.Forbidden)
	if len(m.builds) != 0 || len(s.calls) != 1 {
		t.Fatal("rejected recovery created or consumed a private owner")
	}
}
