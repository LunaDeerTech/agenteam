// Package knowledge owns current Knowledge documents, their tree, and the
// durable work that publishes and retires their canonical objects.
package knowledge

import (
	"context"
	"sync"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ActivityAuthority interface {
	TouchActivityInTx(context.Context, f.Tx, id.Actor) error
}

type Dependencies struct {
	Projects         pc.ProjectAuthority
	Activity         ActivityAuthority
	Objects          oc.Objects
	Uploads          oc.Uploads
	Sources          oc.SourceResolver
	SourceReads      oc.SourceReads
	ReferenceCleanup oc.ReferenceCleanup
	ObjectCleanup    oc.Cleaner
	Audit            audit.Appender
	Outbox           ob.Appender
	Events           kc.KnowledgeEvents
	Processes        ob.ProcessAuthority
	Cursors          cursor.Keyring
	Confirmations    kc.ConfirmationKeys
}

type Service struct{ data func() *serviceState }
type serviceState struct {
	store   Store
	deps    Dependencies
	mu      sync.Mutex
	stopped bool
	calls   map[*call]struct{}
	changed chan struct{}
}
type call struct{ cancel context.CancelFunc }

func New(store Store, deps Dependencies) (*Service, error) {
	ports := []any{store, deps.Projects, deps.Activity, deps.Objects, deps.Uploads,
		deps.Sources, deps.SourceReads, deps.ReferenceCleanup, deps.ObjectCleanup,
		deps.Audit, deps.Outbox, deps.Processes}
	for _, port := range ports {
		if nilPort(port) {
			return nil, fault(f.DependencyUnbound)
		}
	}
	if !deps.Events.Valid() || deps.Processes.CurrentProcess().Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	if deps.Cursors.Validate() != nil || deps.Confirmations.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	st := &serviceState{store: store, deps: deps, calls: make(map[*call]struct{}), changed: make(chan struct{})}
	return &Service{data: func() *serviceState { return st }}, nil
}

func (s *Service) state() *serviceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
