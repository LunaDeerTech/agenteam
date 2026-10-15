package knowledge

import (
	"context"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func retentionFixture(t *testing.T) (*IndexService, id.Actor, indexProfile) {
	t.Helper()
	_, owner, q := queryFixture(t)
	registration, err := id.RegisterService(id.KnowledgeRuntime)
	if err != nil {
		t.Fatal(err)
	}
	a := &IndexAuthority{registration: registration, issuer: mc.NewPlanIssuer(), process: newID[oc.Process](t)}
	s := &IndexService{authority: a, retentions: map[*indexRetention]struct{}{}, changed: make(chan struct{})}
	a.service = s
	return s, owner, indexProfile{Project: q.project, ID: newID[struct{}](t).String(), Version: 4, Dimension: 4096}
}

func TestIndexRetentionKeepsOriginalProjectOwnerUntilConfirmed(t *testing.T) {
	s, owner, p := retentionFixture(t)
	const callers = 12
	var wg sync.WaitGroup
	winners := make(chan *indexRetention, callers)
	for range callers {
		wg.Go(func() {
			g, err := s.newRetention(owner, p)
			if err == nil {
				winners <- g
			}
		})
	}
	wg.Wait()
	close(winners)
	if len(winners) != 1 {
		t.Fatal("concurrent rebuild acquired more than one project owner")
	}
	g := <-winners
	s.StopAdmission()
	if s.Joined() {
		t.Fatal("empty model calls incorrectly released the generation owner")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Drain(ctx); err == nil || s.Joined() {
		t.Fatal("cancellation incorrectly confirmed the original generation")
	}
	g.mu.Lock()
	g.complete()
	g.mu.Unlock()
	if !s.Joined() {
		t.Fatal("confirmed owner did not release join")
	}
	if _, err := s.newRetention(owner, p); err == nil {
		t.Fatal("stopped service readmitted a retention owner")
	}
}

func TestIndexRetentionAuthorityRequiresOriginalPrivateOwnerAndTarget(t *testing.T) {
	s, owner, p := retentionFixture(t)
	g, err := s.newRetention(owner, p)
	if err != nil {
		t.Fatal(err)
	}
	g.target.Operation = newID[struct{}](t).String()
	g.target.Profile.Snapshot = newID[mc.Snapshot](t)
	r := g.request(g.target, mc.RetainServingSnapshot)
	ctx := g.context(context.Background())
	plan, err := s.authority.DiscoverServingSnapshot(ctx, r)
	if err != nil || plan.Validate() != nil || plan.Details().RetryPolicy != nil {
		t.Fatal("original retention owner rejected", err)
	}
	for name, mutate := range map[string]func(*mc.ServingSnapshotRetentionRequest){
		"project":    func(r *mc.ServingSnapshotRetentionRequest) { r.Owner.ProjectID = newID[id.Project](t) },
		"generation": func(r *mc.ServingSnapshotRetentionRequest) { r.Owner.GenerationID = newID[struct{}](t).String() },
		"snapshot":   func(r *mc.ServingSnapshotRetentionRequest) { r.SnapshotID = newID[mc.Snapshot](t) },
		"version":    func(r *mc.ServingSnapshotRetentionRequest) { r.OwnerVersion++ },
		"actor":      func(r *mc.ServingSnapshotRetentionRequest) { r.Actor = owner },
	} {
		t.Run(name, func(t *testing.T) {
			forged := r.Clone()
			mutate(&forged)
			if _, err := s.authority.DiscoverServingSnapshot(ctx, forged); err == nil {
				t.Fatal("request changed the original owner binding")
			}
		})
	}
	if _, err := s.authority.DiscoverServingSnapshot(context.Background(), r); err == nil {
		t.Fatal("DTO alone conferred retention authority")
	}
	old := g.target
	old.Profile.ID, old.Profile.Version = newID[struct{}](t).String(), f.Version(2)
	old.Operation = newID[struct{}](t).String()
	g.old = &old
	if _, err := s.authority.DiscoverServingSnapshot(ctx, g.request(old, mc.RetainServingSnapshot)); err == nil {
		t.Fatal("new transition acquired authority to retain old target")
	}
	if _, err := s.authority.DiscoverServingSnapshot(ctx, g.request(old, mc.ReleaseServingSnapshot)); err != nil {
		t.Fatal("original old release mapping rejected", err)
	}
	// CommitUnknown state must not change the immutable plan mapping, but the
	// original registered owner remains mandatory for both observation and apply.
	g.pinned, g.publishUnknown = true, true
	after, err := s.authority.DiscoverServingSnapshot(ctx, r)
	if err != nil || after.Details().Mapping != plan.Details().Mapping {
		t.Fatal("lifecycle changed the original plan mapping", err)
	}
	g.mu.Lock()
	g.complete()
	g.mu.Unlock()
	if _, err := s.authority.DiscoverServingSnapshot(ctx, r); err == nil {
		t.Fatal("retired private owner was reusable")
	}
}

func TestIndexRetentionHoldsOriginalProjectStopBarrier(t *testing.T) {
	x := newKnowledgeProjectPureFixture(t)
	documents := x.pair.Service()
	run, parent, err := documents.beginProjectCall(context.Background(), x.actor, x.a, nil, projectCallCommand)
	if err != nil {
		t.Fatal(err)
	}
	_, child, err := documents.beginProjectCall(run, x.actor, x.a, nil, projectCallRead)
	if err != nil {
		t.Fatal(err)
	}
	s := &IndexService{documents: documents, retentions: map[*indexRetention]struct{}{}, changed: make(chan struct{})}
	g := &indexRetention{service: s, projectCall: child, published: true}
	s.retentions[g] = struct{}{}
	// The real Rebuild has returned; its independently retained generation has
	// not. The Project pair must observe that original child reference.
	documents.projectCallDone(parent, nil)
	actor, cause, scope := x.stopIdentity(t, x.a)
	report, err := x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
	if err != nil || report.Details().State == pc.Stopped {
		t.Fatal("generation was omitted from Project stop", err)
	}
	if err := g.cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	x.convergeStop(t, actor, cause, scope)
}
