package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	objectcontract "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type outboxStorage interface {
	Initialize(context.Context) error
	Start(context.Context) error
	Check(context.Context) error
	StopClaims()
	Drain(context.Context) error
	Force(context.Context) error
	Joined() bool
}

// The adapter is private to the composition root. D06 never imports the object
// package and cannot close the shared guard or invent a second death rule.
type outboxProcessAuthority struct{ objects *objectAssembly }

func (a outboxProcessAuthority) CurrentProcess() oc.ProcessID {
	id, _ := foundation.ParseID[oc.Process](a.objects.process.String())
	return id
}
func (a outboxProcessAuthority) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	original, err := foundation.ParseID[objectcontract.Process](id.String())
	if err != nil {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return a.objects.guard.ConfirmStopped(ctx, original)
}

// Construction performs no I/O or callback. The caller registers the owner
// before Initialize so every failure, cancellation and late return is owned.
func createOutbox(cfg config.Config, db database, auditor *audit.Service, objects objectStorage) (outboxStorage, error) {
	assembly, ok := objects.(*objectAssembly)
	if !ok || assembly == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	store, ok := db.(outbox.Store)
	if !ok {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	catalog := event.NewCatalog()
	service, err := outbox.New(store, catalog, outbox.Authorizations{Processes: outboxProcessAuthority{assembly}, Audit: auditor, Cursors: cfg.CursorKeyring()})
	if err != nil {
		return nil, err
	}
	// No domain event/handler or authorization provider is fabricated. The real
	// empty catalog initializes only when durable facts are compatible with it.
	return outbox.NewRuntime(service, nil, outbox.Options{})
}
func (o *resources) addOutbox(ctx context.Context, service outboxStorage) bool {
	o.mu.Lock()
	forced, stopping := o.forced, o.stopping
	if forced == nil {
		o.outboxService = service
		o.mu.Unlock()
		if !stopping && ctx.Err() == nil {
			return true
		}
		service.StopClaims()
		return false
	}
	o.mu.Unlock()
	service.StopClaims()
	_ = service.Force(forced)
	return false
}
func (o *resources) outbox() outboxStorage { o.mu.Lock(); defer o.mu.Unlock(); return o.outboxService }
func (o *resources) stopOutbox() {
	if service := o.outbox(); service != nil {
		service.StopClaims()
	}
}

// A live callback can still own arbitrary domain resources. Closing transports
// is bounded technical cleanup; only Joined authorizes releasing ProcessGuard.
func forceObjectAfterOutbox(ctx context.Context, objects objectStorage, deliveries outboxStorage) {
	if deliveries != nil {
		_ = deliveries.Force(ctx)
	}
	if objects == nil {
		return
	}
	if deliveries != nil && !deliveries.Joined() {
		if assembly, ok := objects.(interface{ forceTransports(context.Context) error }); ok {
			_ = assembly.forceTransports(ctx)
		}
		return
	}
	_ = objects.Force(ctx)
}
