package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectcontract "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
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
type outboxProcessAuthority struct {
	process objectcontract.ProcessID
	guard   *object.ProcessGuard
}

func (a outboxProcessAuthority) CurrentProcess() oc.ProcessID {
	id, _ := foundation.ParseID[oc.Process](a.process.String())
	return id
}
func (a outboxProcessAuthority) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	original, err := foundation.ParseID[objectcontract.Process](id.String())
	if err != nil {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return a.guard.ConfirmStopped(ctx, original)
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
