package project

import (
	"bytes"
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// NewLifecycleCleanupDriverForUsage supplies only the fixed Usage
// cleanup step. Its concrete domain provider must be explicitly supplied by the
// trusted composition root. It cannot bind a partial default Registry, clean
// other domains, or advance domains to Outbox.
func NewLifecycleCleanupDriverForUsage(store Store, a *LifecycleAuthority, processes oc.ProcessAuthority, participant c.ProjectLifecycleParticipant) (*LifecycleCleanupDriver, error) {
	if nilPort(store) || !a.bound() || !sameStore(store, a.store) || nilPort(processes) || nilPort(participant) {
		return nil, fault(f.DependencyUnbound)
	}
	process := processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, invalid()
	}
	if name, err := cleanupParticipantName(participant); err != nil || name != c.UsageParticipant {
		return nil, fault(f.DependencyUnbound)
	}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		return nil, portError(err)
	}
	return &LifecycleCleanupDriver{state: &lifecycleCleanupState{store: store, authority: a, processes: processes, process: process, usage: participant, registration: registration, calls: map[c.ProjectID]context.CancelFunc{}, owners: map[c.OperationID]lifecycleCleanupOwner{}, changed: make(chan struct{})}}, nil
}
func usageDriverProgress(r *lifecycleRecord) (c.ParticipantName, bool, error) {
	if r == nil {
		return "", false, unavailable(nil)
	}
	for index, row := range r.participants {
		if row.name != c.UsageParticipant || row.cleanup != "completed" {
			continue
		}
		copyRecord := *r
		copyRecord.participants = append([]lifecycleParticipantRecord(nil), r.participants...)
		copyRecord.participants[index].cleanup = "pending"
		return c.UsageParticipant, true, usageCleanupProgress(&copyRecord)
	}
	return c.UsageParticipant, false, usageCleanupProgress(r)
}

// A provider Unknown retains this exact Project round. The next explicit Run
// first revalidates it in a read-only transaction; it must not replace its
// claim before the same provider's original Unknown confirmation returns.
func (st *lifecycleCleanupState) resumeUsage(ctx context.Context, round lifecycleCleanupRound) error {
	if st == nil || nilPort(st.usage) || !round.usageUnknown || round.participant != c.UsageParticipant {
		return fault(f.Forbidden)
	}
	validated := false
	result := st.store.WithinTx(ctx, round.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(round.claim.project, f.Exclusive)}); err != nil {
			return portError(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, round.claim.project)
		if err != nil {
			return err
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		actual, err := loadLifecycleStopClaim(ctx, x, round.claim.operation)
		if err != nil {
			return err
		}
		if actual == nil || *actual != round.claim || actual.phase != "running" {
			return fault(f.ResourceBusy)
		}
		cause, r, name, replay, err := st.current(ctx, x, round.claim.project, round.claim.operation)
		if err != nil {
			return err
		}
		if replay || cause != round.cause || name != c.UsageParticipant || r.operation.Version != round.operationVersion {
			return fault(f.InvalidState)
		}
		version, attempts, cp, err := cleanupParticipantRow(ctx, x, round)
		if err != nil {
			return err
		}
		if version != round.rowVersion || attempts < 1 || (cp == nil) != (round.checkpoint == nil) {
			return fault(f.ResourceBusy)
		}
		if cp != nil && (cp.Schema() != round.checkpoint.Schema() || !bytes.Equal(cp.Bytes(), round.checkpoint.Bytes())) {
			return fault(f.ResourceBusy)
		}
		validated = true
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return err
	}
	if !validated {
		return unavailable(nil)
	}
	return portError(ctx.Err())
}
func usageCleanupReport(round lifecycleCleanupRound, report c.CleanupReport) error {
	d := report.Details()
	if d.Checkpoint == nil {
		return unavailable(nil)
	}
	p, err := uc.DecodeProjectCleanupCheckpoint(d.Checkpoint, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project})
	if err != nil {
		return portError(err)
	}
	if (d.State == c.CleanupCompleted) != (p.Stage == "completed") {
		return unavailable(nil)
	}
	if d.State == c.CleanupCompleted && d.SafeReason != "" {
		return unavailable(nil)
	}
	return nil
}
