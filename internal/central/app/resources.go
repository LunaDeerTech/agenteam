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
	mu           sync.Mutex
	db           database
	listening    net.Listener
	server       *http.Server
	forced       context.Context
	auditService *audit.Service
}

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
	o.mu.Lock()
	o.forced = ctx
	store := o.db
	o.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if store != nil {
			_ = store.ForceClose(ctx)
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	if cancelServing != nil {
		cancelServing()
	}
	o.closeHTTP()
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
