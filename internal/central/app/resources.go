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
	o.mu.Lock()
	o.forced = ctx
	store := o.db
	cancelMaintenance := o.maintenanceCancel
	outboundService := o.outboundService
	o.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if store != nil {
			_ = store.ForceClose(ctx)
		}
	}()
	outboundDone := make(chan struct{})
	go func() {
		defer close(outboundDone)
		if outboundService != nil {
			_ = outboundService.ForceClose(ctx)
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	select {
	case <-outboundDone:
	case <-ctx.Done():
	}
	if cancelMaintenance != nil {
		cancelMaintenance()
	}
	if cancelServing != nil {
		cancelServing()
	}
	o.closeHTTP()
	if done := o.workerDone(); done != nil {
		select {
		case <-done:
		case <-ctx.Done():
		}
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
