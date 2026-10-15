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

func TestGenerationAuthorityRequiresOriginalOwnerAndExactTarget(t *testing.T) {
	s := &Service{}
	reg, _ := i.RegisterService(i.MemoryRuntime)
	a, _ := NewModelAuthority(s, reg)
	ns := c.Namespace{ProjectID: testID[i.Project](t), AgentID: testID[i.Agent](t)}
	entry := &call{service: s, namespace: ns, operation: testID[tc.Operation](t), intent: "retain"}
	scope, _ := i.InProject(ns.ProjectID)
	actor, _ := reg.Actor(entry.operation.String(), scope)
	m := &Models{domain: s, authority: a, builds: map[*retainedGeneration]struct{}{}}
	g := &retainedGeneration{models: m, entry: entry, actor: actor, source: generationSource{Version: 1}, target: generationTarget{serving: rc.ServingIdentity{GenerationID: testID[struct{}](t).String(), SnapshotID: testID[mc.Snapshot](t), Dimension: 2}, call: testID[mc.Call](t), process: testID[oc.Process](t)}}
	m.builds[g] = struct{}{}
	ctx := g.context(context.Background())
	request := g.request(g.target, mc.RetainServingSnapshot)
	plan, err := a.DiscoverServingSnapshot(ctx, request)
	if err != nil || plan.Validate() != nil || plan.Details().RetryPolicy != nil {
		t.Fatal("valid private intent", err)
	}
	for name, change := range map[string]func(*mc.ServingSnapshotRetentionRequest){
		"generation": func(r *mc.ServingSnapshotRetentionRequest) { r.Owner.GenerationID = testID[struct{}](t).String() },
		"agent":      func(r *mc.ServingSnapshotRetentionRequest) { v := testID[i.Agent](t); r.Owner.AgentID = &v },
		"snapshot":   func(r *mc.ServingSnapshotRetentionRequest) { r.SnapshotID = testID[mc.Snapshot](t) },
		"version":    func(r *mc.ServingSnapshotRetentionRequest) { r.OwnerVersion = 2 },
		"actor": func(r *mc.ServingSnapshotRetentionRequest) {
			r.Actor, _ = reg.Actor(testID[struct{}](t).String(), scope)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := request.Clone()
			change(&r)
			if _, err := a.DiscoverServingSnapshot(ctx, r); err == nil {
				t.Fatal("foreign original target accepted")
			}
		})
	}
	if _, err := a.DiscoverServingSnapshot(context.Background(), request); err == nil {
		t.Fatal("bare DTO authorized")
	}
	release := g.request(g.target, mc.ReleaseServingSnapshot)
	if _, err := a.DiscoverServingSnapshot(ctx, release); err != nil {
		t.Fatal("same owner terminal v2 discovery", err)
	}
	delete(m.builds, g)
	if _, err := a.DiscoverServingSnapshot(ctx, release); err == nil {
		t.Fatal("completed owner reused")
	}
}
func TestMemoryGenerationPreventsDrainDuringEmbeddingAdmission(t *testing.T) {
	s := &Service{calls: map[*call]struct{}{}, changed: make(chan struct{})}
	_, cancel := context.WithCancel(context.Background())
	entry := &call{service: s, cancel: cancel}
	s.calls[entry] = struct{}{}
	m := &Models{domain: s, pending: map[*modelPending]struct{}{}, finished: map[*call]struct{}{}, builds: map[*retainedGeneration]struct{}{}}
	g := &retainedGeneration{models: m, entry: entry, building: true}
	m.builds[g] = struct{}{}
	m.finish(entry)
	if m.Joined() {
		t.Fatal("building reference owner prematurely joined")
	}
	requireFault(t, m.Drain(context.Background()), f.ResourceBusy)
	if g.done || len(s.calls) != 1 {
		t.Fatal("drain detached active build")
	}
	// A known uncommitted build has no reference to release. Once its original
	// builder exits, cleanup may end that owner without issuing a Model call.
	if err := g.finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !g.done || !m.Joined() {
		t.Fatal("finished absent build still owns operation")
	}
}
