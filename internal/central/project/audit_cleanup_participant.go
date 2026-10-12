package project

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewAuditLifecycleParticipant adds the original Audit cleaner to the existing
// gate-based Stop adapter. Cleanup still needs the live driver's Audit claim;
// this constructor does not bind a registry or complete Project deletion.
func NewAuditLifecycleParticipant(authority *LifecycleAuthority, cleaner audit.Cleaner) (c.ProjectLifecycleParticipant, error) {
	if nilPort(cleaner) {
		return nil, fault(f.DependencyUnbound)
	}
	stop, err := NewAuditStopParticipant(authority)
	if err != nil {
		return nil, err
	}
	return &auditCleanupParticipant{ProjectLifecycleParticipant: stop, authority: authority, cleaner: cleaner}, nil
}

type auditCleanupParticipant struct {
	c.ProjectLifecycleParticipant
	authority *LifecycleAuthority
	cleaner   audit.Cleaner
}

func (p *auditCleanupParticipant) Cleanup(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, checkpoint *c.CleanupCheckpoint) (c.CleanupReport, error) {
	if ctx == nil || cause.Validate() != nil || scope.Validate() != nil {
		return c.CleanupReport{}, invalid()
	}
	if p == nil || !p.authority.bound() || nilPort(p.cleaner) {
		return c.CleanupReport{}, fault(f.DependencyUnbound)
	}
	project, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if project != scope.ProjectID || cause.Action != c.Delete {
		return c.CleanupReport{}, fault(f.Forbidden)
	}
	if scope.Kind != c.ProjectScope {
		return c.CleanupReport{}, fault(f.DependencyUnbound)
	}
	prior, err := decodeAuditCleanupCheckpoint(checkpoint, cause, scope)
	if err != nil {
		return c.CleanupReport{}, err
	}
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	txCause, err := readCause("audit-cleanup-preflight")
	if err != nil {
		return c.CleanupReport{}, err
	}
	validated := false
	result := p.authority.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := p.authority.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := p.authority.validateAuditCleanupInTx(ctx, tx, project, cause); err != nil {
			return err
		}
		validated = true
		return nil
	})
	if err = commitError(result); err != nil {
		return c.CleanupReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	if !validated {
		return c.CleanupReport{}, unavailable(nil)
	}
	operation, err := f.ParseID[audit.LifecycleOperation](cause.OperationID.String())
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	downstream, err := audit.NewLifecycleCause(operation, audit.Delete, cause.ProjectVersion)
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	// No preflight lock survives this call. The original cleaner obtains EX
	// and reauthorizes every batch/Unknown confirmation in its own original Tx.
	observed, err := callAuditCleanup(p.cleaner, ctx, actor, downstream, project, prior)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	return auditCleanupReport(cause, scope, observed)
}

func decodeAuditCleanupCheckpoint(checkpoint *c.CleanupCheckpoint, cause c.LifecycleCause, scope c.ScopeRef) (*audit.CleanupCheckpoint, error) {
	if cause.Validate() != nil || cause.Action != c.Delete || scope.Validate() != nil || scope.Kind != c.ProjectScope {
		return nil, invalid()
	}
	if checkpoint == nil {
		return nil, nil
	}
	if !checkpoint.Matches(c.AuditParticipant, cause, scope) || checkpoint.Schema() != 1 {
		return nil, invalid()
	}
	raw := checkpoint.Bytes()
	if len(raw) == 0 || len(raw) > 1024 {
		return nil, invalid()
	}
	var data audit.CleanupCheckpoint
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, invalid()
	}
	canonical, err := json.Marshal(data)
	// These are this adapter's own canonical bytes. Exact encoding rejects
	// unknown/duplicate/case-folded keys, explicit null and trailing data.
	if err != nil || !bytes.Equal(raw, canonical) || data.ProjectID != scope.ProjectID || data.OperationID.String() != cause.OperationID.String() || data.ProjectID.Validate() != nil || data.OperationID.Validate() != nil || data.LastID != nil && data.LastID.Validate() != nil {
		return nil, invalid()
	}
	return &data, nil
}

func auditCleanupReport(cause c.LifecycleCause, scope c.ScopeRef, observed audit.CleanupReport) (c.CleanupReport, error) {
	if observed.State != audit.CleanupPending && observed.State != audit.CleanupCompleted {
		return c.CleanupReport{}, unavailable(nil)
	}
	raw, err := json.Marshal(observed.Checkpoint)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	checkpoint, err := c.NewCleanupCheckpoint(c.AuditParticipant, cause, scope, 1, raw)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	if _, err = decodeAuditCleanupCheckpoint(&checkpoint, cause, scope); err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	details := c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending, Checkpoint: &checkpoint}
	if observed.State == audit.CleanupCompleted {
		details.State, details.SafeReason = c.CleanupCompleted, ""
	}
	// LastID is diagnostic. Neither this adapter nor the original cleaner uses
	// it to skip records; every round starts from the remaining Project rows.
	return c.NewCleanupReport(c.AuditParticipant, cause, scope, details)
}

func callAuditCleanup(cleaner audit.Cleaner, ctx context.Context, actor identity.Actor, cause audit.LifecycleCause, project identity.ProjectID, checkpoint *audit.CleanupCheckpoint) (report audit.CleanupReport, err error) {
	defer func() {
		if recover() != nil {
			report, err = audit.CleanupReport{}, unavailable(nil)
		}
	}()
	return cleaner.CleanupProject(ctx, actor, cause, project, checkpoint)
}

var _ c.ProjectLifecycleParticipant = (*auditCleanupParticipant)(nil)
