package knowledge

import (
	"context"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/document"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
	runtime "github.com/LunaDeerTech/agenteam/internal/central/retrieval/runtime"
)

type IndexSettings struct {
	ParserProfile string
	Chunker       chunker.Config
	Backend       lexical.Backend
	Retrieval     rc.Limits
	Timeouts      runtime.Timeouts
	ModelTimeout  time.Duration
}

func (s IndexSettings) Validate() error {
	if (s.ParserProfile != document.Profile && s.ParserProfile != document.RegistryProfile && s.ParserProfile != document.PopplerRegistryProfile) || s.Chunker.Validate() != nil || s.Retrieval.Validate() != nil || s.ModelTimeout <= 0 || s.ModelTimeout > 2*time.Minute {
		return fault(f.InvalidArgument)
	}
	if _, err := lexical.Expressions(s.Backend, lexical.KnowledgeChunks, 1); err != nil {
		return err
	}
	return nil
}

type IndexModelResolver interface {
	mc.Resolver
	mc.ModelCallRetirement
	LookupResolution(context.Context, mc.ResolveRequest) (mc.ResolvedModel, bool, error)
}

type IndexDependencies struct {
	Parser            *document.Registry
	EmbeddingProfiles mc.EmbeddingProfileReader
	ServingRetention  mc.ServingSnapshotRetention
	Models            IndexModelResolver
	Calls             mc.EmbeddingCaller
	Observer          rc.Observer
	Settings          IndexSettings
}

// IndexAuthority is constructed before Model's static consumer router. Binding
// its concrete IndexService is one-time and does not grant a caller authority.
type IndexAuthority struct {
	store        Store
	guard        *object.ProcessGuard
	process      oc.ProcessID
	registration id.ServiceRegistration
	issuer       mc.PlanIssuer
	mu           sync.Mutex
	service      *IndexService
}

func NewIndexAuthority(store Store, guard *object.ProcessGuard, registration id.ServiceRegistration, expected ...oc.ProcessID) (*IndexAuthority, error) {
	if nilPort(store) || guard == nil || len(expected) > 1 || len(expected) == 1 && expected[0].Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	process, err := guard.CurrentProcess()
	if len(expected) == 1 {
		if err == nil && process != expected[0] {
			return nil, fault(f.DependencyUnbound)
		}
		process = expected[0]
	} else if err != nil {
		return nil, portError(err)
	}
	project, err := f.NewID[id.Project]()
	if err != nil {
		return nil, unavailable(err)
	}
	scope, _ := id.InProject(project)
	actor, err := registration.Actor(project.String(), scope)
	if err != nil || actor.Details().ServiceName != id.KnowledgeRuntime {
		return nil, fault(f.DependencyUnbound)
	}
	return &IndexAuthority{store: store, guard: guard, process: process, registration: registration, issuer: mc.NewPlanIssuer()}, nil
}

type IndexService struct {
	store         Store
	documents     *Service
	authority     *IndexAuthority
	deps          IndexDependencies
	search        *runtime.Runtime
	mu            sync.Mutex
	stopped       bool
	owners        map[*indexOperation]struct{}
	retentions    map[*indexRetention]struct{}
	calls         map[*indexAdmission]struct{}
	changed       chan struct{}
	profileCursor string
}

func NewIndexService(store Store, documents *Service, authority *IndexAuthority, deps IndexDependencies) (*IndexService, error) {
	if nilPort(store) || documents == nil || documents.state() == nil || authority == nil || !sameStore(store, documents.state().store) || !sameStore(store, authority.store) || nilPort(deps.Models) || nilPort(deps.Calls) || nilPort(deps.ServingRetention) || nilPort(deps.EmbeddingProfiles) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := deps.Settings.Validate(); err != nil {
		return nil, err
	}
	if deps.Settings.ParserProfile == document.PopplerRegistryProfile && deps.Parser == nil {
		return nil, fault(f.DependencyUnbound)
	}
	s := &IndexService{store: store, documents: documents, authority: authority, deps: deps, owners: map[*indexOperation]struct{}{}, retentions: map[*indexRetention]struct{}{}, calls: map[*indexAdmission]struct{}{}, changed: make(chan struct{})}
	var observers []rc.Observer
	if deps.Observer != nil {
		observers = append(observers, deps.Observer)
	}
	r, err := runtime.New(s, s, deps.Settings.Timeouts, observers...)
	if err != nil {
		return nil, err
	}
	s.search = r
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.service != nil {
		return nil, fault(f.InvalidState)
	}
	if pair := documents.state().ownership; pair != nil {
		pair.mu.Lock()
		defer pair.mu.Unlock()
		if pair.index != nil || pair.service != documents || !sameStore(pair.store, store) || pair.process != authority.process || pair.proof != authority.guard {
			return nil, fault(f.DependencyUnbound)
		}
		pair.index = s
	}
	authority.service = s
	return s, nil
}

func (s *IndexService) RequireBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	other, ok := store.(Store)
	if s == nil || !ok || !sameStore(s.store, other) || s.documents.state().ownership == nil || nilPort(s.documents.state().deps.InvocationAuthority) {
		return fault(f.DependencyUnbound)
	}
	if input, ok := s.documents.state().deps.InvocationAuthority.(kc.KnowledgeInvocationInputAuthority); !ok || nilPort(input) {
		return fault(f.DependencyUnbound)
	}
	if _, err := s.store.InTx(tx); err != nil {
		return portError(err)
	}
	return s.authority.guard.RequireCurrentProcessInTx(ctx, tx, s.authority.process)
}

func (s *IndexService) StopAdmission() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for owner := range s.owners {
		owner.cancel()
	}
	for call := range s.calls {
		call.cancel()
	}
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *IndexService) Drain(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if s == nil {
		return nil
	}
	for {
		s.mu.Lock()
		empty := len(s.owners) == 0 && len(s.calls) == 0 && len(s.retentions) == 0
		owners := make([]*indexOperation, 0, len(s.owners))
		for owner := range s.owners {
			owners = append(owners, owner)
		}
		retentions := make([]*indexRetention, 0, len(s.retentions))
		for owner := range s.retentions {
			retentions = append(retentions, owner)
		}
		changed := s.changed
		s.mu.Unlock()
		if empty {
			return nil
		}
		for _, owner := range owners {
			owner.mu.Lock()
			ready := owner.terminal != "" && !owner.done
			owner.mu.Unlock()
			if ready {
				if err := s.retireOperation(context.WithValue(ctx, indexOperationKey{}, owner), owner); err != nil {
					return err
				}
			}
		}
		for _, owner := range retentions {
			owner.mu.Lock()
			ready := !owner.running && !owner.done
			owner.mu.Unlock()
			if ready {
				if err := owner.cleanup(ctx); err != nil {
					return err
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// Joined observes the original callers as well as their Model operations.
// Cancellation or an empty Model registry alone is not an index-call join.
func (s *IndexService) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.owners) == 0 && len(s.calls) == 0 && len(s.retentions) == 0
}

type indexAdmission struct{ cancel context.CancelFunc }

func (s *IndexService) admit(ctx context.Context) (context.Context, func(), error) {
	if s == nil || ctx == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	run, cancel := context.WithCancel(ctx)
	call := &indexAdmission{cancel: cancel}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		cancel()
		return nil, nil, fault(f.ShuttingDown)
	}
	if s.calls == nil {
		s.calls = map[*indexAdmission]struct{}{}
	}
	s.calls[call] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel()
			s.mu.Lock()
			delete(s.calls, call)
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
		})
	}
	return context.WithValue(run, indexAdmissionKey{}, call), done, nil
}
