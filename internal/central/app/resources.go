package app

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"net"
	"net/http"
	"sync"
)

// The startup worker may return after a force deadline. Late acquisitions are
// still owned and immediately disposed using the original (possibly expired)
// cleanup context, never a newly reset timeout.
type resources struct {
	mu                sync.Mutex
	db                database
	listening         net.Listener
	server            *http.Server
	forced            context.Context
	auditService      *audit.Service
	secretService     maintenance
	maintenanceDone   chan struct{}
	maintenanceCancel context.CancelFunc
	maintenanceErr    error
	stopping          bool
	outboundService   egress
	objectService     objectStorage
	outboxService     outboxStorage
}

func (o *resources) addObjects(ctx context.Context, service objectStorage) bool {
	o.mu.Lock()
	forced, stopping := o.forced, o.stopping
	if forced == nil {
		// Even an unsuccessful/cancelled constructor may own a ProcessGuard
		// and joined probe checkpoints. Reject startup admission but retain
		// cleanup ownership for the original drain/force group. The startup
		// context is already cancelled and is not that group's budget.
		o.objectService = service
		o.mu.Unlock()
		if !stopping && ctx.Err() == nil {
			return true
		}
		service.StopAdmission()
		return false
	}
	o.mu.Unlock()
	service.StopAdmission()
	_ = service.Force(forced)
	return false
}
func (o *resources) objects() objectStorage { o.mu.Lock(); defer o.mu.Unlock(); return o.objectService }
func (o *resources) stopObjects() {
	if s := o.objects(); s != nil {
		s.StopAdmission()
	}
}

func (o *resources) addOutbound(ctx context.Context, service egress) bool {
	o.mu.Lock()
	forced, stopping := o.forced, o.stopping
	if forced == nil && !stopping && ctx.Err() == nil {
		o.outboundService = service
		o.mu.Unlock()
		return true
	}
	o.mu.Unlock()
	if forced != nil {
		ctx = forced
	}
	service.StopAdmission()
	_ = service.ForceClose(ctx)
	return false
}
func (o *resources) outbound() egress { o.mu.Lock(); defer o.mu.Unlock(); return o.outboundService }
func (o *resources) stopOutbound() {
	if service := o.outbound(); service != nil {
		service.StopAdmission()
	}
}

func (o *resources) startMaintenance(ctx context.Context, service maintenance) bool {
	if service == nil {
		return true // Only package-local unit assembly can omit the worker.
	}
	o.mu.Lock()
	if o.forced != nil || o.stopping {
		o.mu.Unlock()
		service.StopMaintenance()
		return false
	}
	worker, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	o.secretService, o.maintenanceCancel, o.maintenanceDone = service, cancel, done
	o.mu.Unlock()
	go func() {
		defer cancel()
		err := service.RunMaintenance(worker)
		o.mu.Lock()
		o.maintenanceErr = err
		o.mu.Unlock()
		close(done)
	}()
	return true
}
func (o *resources) stopMaintenance() {
	o.mu.Lock()
	o.stopping = true
	service := o.secretService
	o.mu.Unlock()
	if service != nil {
		service.StopMaintenance()
	}
}
func (o *resources) workerDone() <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.maintenanceDone
}
func (o *resources) workerError() error  { o.mu.Lock(); defer o.mu.Unlock(); return o.maintenanceErr }
func (o *resources) secret() maintenance { o.mu.Lock(); defer o.mu.Unlock(); return o.secretService }

func (o *resources) setAudit(service *audit.Service) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.forced == nil {
		o.auditService = service
	}
}

func (o *resources) addStore(store database) bool {
	o.mu.Lock()
	forced := o.forced
	if forced == nil {
		o.db = store
		o.mu.Unlock()
		return true
	}
	o.mu.Unlock()
	_ = store.ForceClose(forced)
	return false
}
func (o *resources) addListener(listener net.Listener) {
	o.mu.Lock()
	if o.forced == nil {
		o.listening = listener
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()
	_ = listener.Close()
}
func (o *resources) store() database               { o.mu.Lock(); defer o.mu.Unlock(); return o.db }
func (o *resources) listener() net.Listener        { o.mu.Lock(); defer o.mu.Unlock(); return o.listening }
func (o *resources) setServer(server *http.Server) { o.mu.Lock(); o.server = server; o.mu.Unlock() }
func (o *resources) closeHTTP() {
	o.mu.Lock()
	server, listener := o.server, o.listening
	o.mu.Unlock()
	if server != nil {
		_ = server.Close()
	} else if listener != nil {
		_ = listener.Close()
	}
}
func cleanupResources(ctx context.Context, o *resources, cancelServing context.CancelFunc) {
	o.stopMaintenance()
	o.stopOutbound()
	o.stopObjects()
	o.stopOutbox()
	o.mu.Lock()
	o.forced = ctx
	store := o.db
	cancelMaintenance := o.maintenanceCancel
	outboundService := o.outboundService
	objectService := o.objectService
	outboxService := o.outboxService
	o.mu.Unlock()
	if cancelMaintenance != nil {
		cancelMaintenance()
	}
	if cancelServing != nil {
		cancelServing()
	}
	o.closeHTTP()
	outboundDone := make(chan struct{})
	go func() {
		defer close(outboundDone)
		if outboundService != nil {
			_ = outboundService.ForceClose(ctx)
		}
	}()
	// Both real ports respect this already shared deadline, including an
	// expired one. Initiation is synchronous so DB cannot overtake cleanup.
	forceObjectAfterOutbox(ctx, objectService, outboxService)
	for _, done := range []<-chan struct{}{outboundDone, o.workerDone()} {
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
			}
		}
	}
	// DB admission and sockets stay available for already admitted object/HTTP
	// cleanup until their shared force budget is consumed. Never renew it here.
	// D03 ForceClose closes owned sockets before its bounded pool join, and
	// uses this parent deadline even if it has already elapsed. Calling it
	// synchronously guarantees initiation before reporting process shutdown.
	if store != nil {
		_ = store.ForceClose(ctx)
	}
}

func joinWorkers(ctx context.Context, serve, http, db <-chan error, health <-chan struct{}) {
	for serve != nil || http != nil || db != nil || health != nil {
		select {
		case <-serve:
			serve = nil
		case <-http:
			http = nil
		case <-db:
			db = nil
		case <-health:
			health = nil
		case <-ctx.Done():
			return
		}
	}
}
