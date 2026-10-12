package project

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// NewSecretLifecycleParticipant adds the original Secret cleaner to the existing
// gate-based Stop adapter. Cleanup still needs the live driver's Secret claim;
// this constructor does not bind a registry or complete Project deletion.
func NewSecretLifecycleParticipant(authority *LifecycleAuthority, cleaner *secret.Service) (c.ProjectLifecycleParticipant, error) {
	if nilPort(cleaner) {
		return nil, fault(f.DependencyUnbound)
	}
	stop, err := NewSecretStopParticipant(authority)
	if err != nil {
		return nil, err
	}
	return &secretCleanupParticipant{ProjectLifecycleParticipant: stop, authority: authority, cleaner: cleaner}, nil
}

type secretCleanupProvider interface {
	CleanupProject(context.Context, identity.Actor, sc.LifecycleCause, identity.ProjectID, *sc.CleanupCheckpoint) (sc.CleanupReport, error)
}

type secretCleanupParticipant struct {
	c.ProjectLifecycleParticipant
	authority *LifecycleAuthority
	cleaner   secretCleanupProvider
}

func (p *secretCleanupParticipant) Cleanup(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, checkpoint *c.CleanupCheckpoint) (c.CleanupReport, error) {
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
	prior, err := decodeSecretCleanupCheckpoint(checkpoint, cause, scope)
	if err != nil {
		return c.CleanupReport{}, err
	}
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	txCause, err := readCause("secret-cleanup-preflight")
	if err != nil {
		return c.CleanupReport{}, err
	}
	validated := false
	result := p.authority.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := p.authority.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := p.authority.validateSecretCleanupInTx(ctx, tx, project, cause); err != nil {
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
	operation, err := f.ParseID[sc.LifecycleOperation](cause.OperationID.String())
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	downstream, err := sc.NewLifecycleCause(operation, cause.ProjectVersion, true)
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	// No preflight lock survives this call. The original cleaner obtains EX
	// and reauthorizes every batch/Unknown confirmation in its own original Tx.
	observed, err := callSecretCleanup(p.cleaner, ctx, actor, downstream, project, prior)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	return secretCleanupReport(cause, scope, observed)
}

func decodeSecretCleanupCheckpoint(checkpoint *c.CleanupCheckpoint, cause c.LifecycleCause, scope c.ScopeRef) (*sc.CleanupCheckpoint, error) {
	if cause.Validate() != nil || cause.Action != c.Delete || scope.Validate() != nil || scope.Kind != c.ProjectScope {
		return nil, invalid()
	}
	if checkpoint == nil {
		return nil, nil
	}
	if !checkpoint.Matches(c.SecretParticipant, cause, scope) || checkpoint.Schema() != 1 {
		return nil, invalid()
	}
	raw := checkpoint.Bytes()
	if len(raw) == 0 || len(raw) > 1024 {
		return nil, invalid()
	}
	var data sc.CleanupCheckpoint
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, invalid()
	}
	canonical, err := json.Marshal(data)
	// These are this adapter's own canonical bytes. Exact encoding rejects
	// unknown/duplicate/case-folded keys, explicit null and trailing data.
	if err != nil || !bytes.Equal(raw, canonical) || data.ProjectID != scope.ProjectID || data.OperationID.String() != cause.OperationID.String() || data.ProjectID.Validate() != nil || data.OperationID.Validate() != nil {
		return nil, invalid()
	}
	return &data, nil
}

func secretCleanupReport(cause c.LifecycleCause, scope c.ScopeRef, observed sc.CleanupReport) (c.CleanupReport, error) {
	raw, err := json.Marshal(observed.Checkpoint)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	checkpoint, err := c.NewCleanupCheckpoint(c.SecretParticipant, cause, scope, 1, raw)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	if _, err = decodeSecretCleanupCheckpoint(&checkpoint, cause, scope); err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	details := c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending, Checkpoint: &checkpoint}
	if observed.Completed {
		details.State, details.SafeReason = c.CleanupCompleted, ""
	}
	// The provider always scans remaining rows; this checkpoint binds only the
	// original Project and operation, never a client-controlled skip cursor.
	return c.NewCleanupReport(c.SecretParticipant, cause, scope, details)
}

func callSecretCleanup(cleaner secretCleanupProvider, ctx context.Context, actor identity.Actor, cause sc.LifecycleCause, project identity.ProjectID, checkpoint *sc.CleanupCheckpoint) (report sc.CleanupReport, err error) {
	defer func() {
		if recover() != nil {
			report, err = sc.CleanupReport{}, unavailable(nil)
		}
	}()
	return cleaner.CleanupProject(ctx, actor, cause, project, checkpoint)
}

var _ c.ProjectLifecycleParticipant = (*secretCleanupParticipant)(nil)
