package knowledge

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type indexBuildKey struct{}

// The original admitted caller owns this private build. The technical actor
// alone never authorizes either a configuration read or a document read.
type indexBuild struct {
	service      *IndexService
	admission    *indexAdmission
	owner, actor id.Actor
	project      id.ProjectID
	id           string
	profile      mc.EmbeddingProfile
}

func (s *IndexService) newBuild(ctx context.Context, owner id.Actor, project id.ProjectID) (context.Context, *indexBuild, error) {
	admission, _ := ctx.Value(indexAdmissionKey{}).(*indexAdmission)
	if admission == nil || project.Validate() != nil {
		return nil, nil, fault(f.Forbidden)
	}
	key, err := f.NewID[struct{}]()
	if err != nil {
		return nil, nil, unavailable(err)
	}
	scope, _ := id.InProject(project)
	actor, err := s.authority.registration.Actor(key.String(), scope)
	if err != nil {
		return nil, nil, err
	}
	if owner.Validate() != nil {
		owner = actor
	}
	if owner.Details().Kind != id.Human && !owner.Equal(actor) {
		return nil, nil, fault(f.Forbidden)
	}
	b := &indexBuild{service: s, admission: admission, owner: owner, actor: actor, project: project, id: key.String()}
	return context.WithValue(ctx, indexBuildKey{}, b), b, nil
}
func currentIndexBuild(ctx context.Context, actor id.Actor, project id.ProjectID) (*indexBuild, error) {
	if ctx == nil {
		return nil, fault(f.Forbidden)
	}
	b, _ := ctx.Value(indexBuildKey{}).(*indexBuild)
	if b == nil || b.service == nil || b.admission == nil || !b.owner.Equal(actor) || b.project != project {
		return nil, fault(f.Forbidden)
	}
	b.service.mu.Lock()
	_, active := b.service.calls[b.admission]
	b.service.mu.Unlock()
	if !active {
		return nil, fault(f.Forbidden)
	}
	return b, nil
}
func (b *indexBuild) locks(write bool) ([]f.LockRequest, error) {
	if b.owner.Details().Kind == id.Human {
		return scopeLocks(b.owner, b.project, write)
	}
	return indexLocks(b.project, write)
}
func (b *indexBuild) authorizeInTx(ctx context.Context, tx f.Tx) error {
	if current, err := currentIndexBuild(ctx, b.owner, b.project); err != nil || current != b {
		return fault(f.Forbidden)
	}
	s := b.service
	locks, err := b.locks(false)
	if err != nil {
		return err
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err = s.authority.guard.RequireCurrentProcessInTx(ctx, tx, s.authority.process); err != nil {
		return portError(err)
	}
	if b.owner.Details().Kind == id.Human {
		grant, err := s.documents.state().deps.Projects.RequireOwnerInTx(ctx, tx, b.owner, b.project, id.Mutate)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(b.owner, b.project) {
			return fault(f.Forbidden)
		}
		return nil
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var eligible bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.projects p WHERE p.id=$1 AND p.lifecycle='active' AND p.initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_knowledge.documents d WHERE d.project_id=p.id AND d.status='active'))`, b.project.String()).Scan(&eligible)
	if err != nil {
		return unavailable(err)
	}
	if !eligible {
		return fault(f.InvalidState)
	}
	return nil
}
func (b *indexBuild) request() mc.EmbeddingProfileRequest {
	return mc.EmbeddingProfileRequest{Actor: b.actor, Consumer: mc.Consumer{Kind: mc.KnowledgeConsumer, ProjectID: b.project, OperationID: b.id, Purpose: mc.KnowledgeEmbedding}}
}
func (b *indexBuild) readProfile(ctx context.Context) (mc.EmbeddingProfile, error) {
	p, err := b.service.deps.EmbeddingProfiles.ReadEmbeddingProfile(ctx, b.request())
	if err != nil {
		return p, portError(err)
	}
	if p.Validate() != nil {
		return p, fault(f.DependencyUnavailable)
	}
	return p, nil
}
func (a *IndexAuthority) embeddingProfileWitness(ctx context.Context, r mc.EmbeddingProfileRequest) (*indexBuild, error) {
	if a == nil || ctx == nil || r.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	b, _ := ctx.Value(indexBuildKey{}).(*indexBuild)
	if b == nil || b.service == nil || b.service.authority != a {
		return nil, fault(f.Forbidden)
	}
	if _, err := currentIndexBuild(ctx, b.owner, b.project); err != nil {
		return nil, err
	}
	want := b.request()
	if !r.Actor.Equal(want.Actor) || !r.Consumer.Equal(want.Consumer) {
		return nil, fault(f.Forbidden)
	}
	return b, nil
}
func (a *IndexAuthority) DiscoverEmbeddingProfile(ctx context.Context, r mc.EmbeddingProfileRequest) (mc.ConsumerDependencies, error) {
	b, err := a.embeddingProfileWitness(ctx, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.EmbeddingProfileBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	raw, err := json.Marshal(struct {
		Owner   id.ActorDetails
		Process string
	}{b.owner.Details(), a.process.String()})
	if err != nil {
		return mc.ConsumerDependencies{}, internal(err)
	}
	locks, err := b.locks(false)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: ob.DigestBytes(raw), Locks: locks})
}
func (a *IndexAuthority) RequireEmbeddingProfileInTx(ctx context.Context, tx f.Tx, r mc.EmbeddingProfileRequest, plan mc.ConsumerDependencies) error {
	b, err := a.embeddingProfileWitness(ctx, r)
	if err != nil {
		return err
	}
	current, err := a.DiscoverEmbeddingProfile(ctx, r)
	if err != nil {
		return err
	}
	if plan.Validate() != nil || plan.Details().RetryPolicy != nil || !plan.Matches(a.issuer, current.Details().Binding, current.Details().Mapping) {
		return fault(f.Forbidden)
	}
	if _, err = a.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := b.service.documents.projectCallTx(ctx, tx, b.project); err != nil {
		return err
	}
	return b.authorizeInTx(ctx, tx)
}
func embeddingProfileMatchesSnapshot(p mc.EmbeddingProfile, s mc.ConfigSnapshot) bool {
	return p.Validate() == nil && s.SelectionVersion != nil && *s.SelectionVersion == p.SelectionVersion && s.Identity.ModelID == p.ModelID && s.Identity.ProviderID == p.ProviderID && s.Identity.AdapterRevision == p.AdapterRevision
}

var _ mc.EmbeddingProfileAuthority = (*IndexAuthority)(nil)
