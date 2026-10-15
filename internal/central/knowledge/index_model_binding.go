package knowledge

import (
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// This binding belongs to the original in-process operation. It is separate
// from op.mu: Close holds that mutex while Model calls back to finalize itself.
// Model additionally verifies its private runtimeCall and durable attempt.
type indexModelBinding struct {
	mu      sync.Mutex
	model   mc.ResolvedModel
	attempt *mc.AttemptIdentity
}

func (b *indexModelBinding) remember(model mc.ResolvedModel) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.model = model
}

// commit binds the attempt only after the caller validated this operation's
// durable facts and ProcessGuard in the current transaction. Finalize may be
// the first callback when startup fails before credential reading.
func (b *indexModelBinding) authorize(op *indexOperation, r mc.ConsumerRequest, commit bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	technical := r.Action == mc.FinalizeConsumer || r.Action == mc.RetireConsumer && r.TerminalVersion == nil
	if technical {
		d := r.Actor.Details()
		if op.kind == "profile" || d.Kind != id.Service || d.ServiceName != id.ModelRuntime || d.ProjectID != op.profile.Project.String() {
			return fault(f.Forbidden)
		}
		attempt := b.attempt
		if r.Attempt != nil {
			attempt = r.Attempt
		}
		if attempt == nil || d.CauseRef != attempt.InvocationID.String() {
			return fault(f.Forbidden)
		}
	} else if !r.Actor.Equal(op.actor) {
		return fault(f.Forbidden)
	}
	if r.Action != mc.ResolveConsumer {
		m := b.model
		if m.Validate() != nil || r.SnapshotID != m.Snapshot.ID || !r.LeaseOwner.Equal(m.LeaseOwner) || (r.LeaseID == nil) != (m.CredentialLease == nil) || r.LeaseID != nil && *r.LeaseID != m.CredentialLease.LeaseID {
			return fault(f.Forbidden)
		}
	}
	if r.Attempt != nil {
		a := *r.Attempt
		if a.Validate() != nil || a.CallID != op.call || a.ProcessID != op.service.authority.process || a.AttemptIndex != 1 || a.Fence != 1 || b.attempt != nil && *b.attempt != a {
			return fault(f.Forbidden)
		}
		if commit && b.attempt == nil {
			b.attempt = &a
		}
	}
	return nil
}
