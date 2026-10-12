package project

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	artifact "github.com/LunaDeerTech/agenteam/internal/central/artifact"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewArtifactLifecycleParticipant binds the original Artifact cleanup entry,
// including its original Object owner. Stop remains explicitly unbound; this
// is not a complete registry or default Project Delete assembly.
func NewArtifactLifecycleParticipant(authority *LifecycleAuthority, cleaner *artifact.Service) (c.ProjectLifecycleParticipant, error) {
	if nilPort(cleaner) || !authority.bound() {
		return nil, fault(f.DependencyUnbound)
	}
	if err := cleaner.RequireProjectCleanupBinding(); err != nil {
		return nil, portError(err)
	}
	return &artifactCleanupParticipant{authority: authority, cleaner: cleaner}, nil
}

type artifactCleanupProvider interface {
	CleanupProject(context.Context, identity.Actor, objc.ProjectCleanupCause) (objc.ProjectCleanupResult, error)
}

type artifactCleanupParticipant struct {
	authority *LifecycleAuthority
	cleaner   artifactCleanupProvider
}

func (*artifactCleanupParticipant) Name() c.ParticipantName { return c.ArtifactObjectParticipant }

// Artifact's Project Stop implementation is not supplied by this slice.
func (*artifactCleanupParticipant) RequestStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	return c.StopReport{}, fault(f.DependencyUnbound)
}
func (*artifactCleanupParticipant) InspectStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	return c.StopReport{}, fault(f.DependencyUnbound)
}

type artifactCleanupCheckpoint struct {
	ProjectID   identity.ProjectID `json:"project_id"`
	OperationID objc.CleanupID     `json:"operation_id"`
}

func (p *artifactCleanupParticipant) Cleanup(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, checkpoint *c.CleanupCheckpoint) (c.CleanupReport, error) {
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
	_, err = decodeArtifactCleanupCheckpoint(checkpoint, cause, scope)
	if err != nil {
		return c.CleanupReport{}, err
	}
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	txCause, err := readCause("artifact-cleanup-preflight")
	if err != nil {
		return c.CleanupReport{}, err
	}
	validated := false
	result := p.authority.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := p.authority.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := p.authority.validateArtifactCleanupInTx(ctx, tx, project, cause); err != nil {
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
	operation, err := f.ParseID[objc.CleanupOperation](cause.OperationID.String())
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	downstream, err := objc.NewProjectCleanupCause(objc.ProjectCleanupDetails{ProjectID: project, OperationID: operation, Version: cause.ProjectVersion})
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	// No preflight lock survives this call. The original cleaner obtains EX
	// and reauthorizes every batch/Unknown confirmation in its own original Tx.
	observed, err := callArtifactCleanup(p.cleaner, ctx, actor, downstream)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	return artifactCleanupReport(cause, scope, observed)
}

func decodeArtifactCleanupCheckpoint(checkpoint *c.CleanupCheckpoint, cause c.LifecycleCause, scope c.ScopeRef) (*artifactCleanupCheckpoint, error) {
	if cause.Validate() != nil || cause.Action != c.Delete || scope.Validate() != nil || scope.Kind != c.ProjectScope {
		return nil, invalid()
	}
	if checkpoint == nil {
		return nil, nil
	}
	if !checkpoint.Matches(c.ArtifactObjectParticipant, cause, scope) || checkpoint.Schema() != 1 {
		return nil, invalid()
	}
	raw := checkpoint.Bytes()
	if len(raw) == 0 || len(raw) > 1024 {
		return nil, invalid()
	}
	var data artifactCleanupCheckpoint
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

func artifactCleanupReport(cause c.LifecycleCause, scope c.ScopeRef, observed objc.ProjectCleanupResult) (c.CleanupReport, error) {
	if observed.State != objc.CleanupPending && observed.State != objc.CleanupCompleted || observed.Remaining.Validate() != nil || observed.State == objc.CleanupCompleted && observed.Remaining != 0 {
		return c.CleanupReport{}, unavailable(nil)
	}
	raw, err := json.Marshal(artifactCleanupCheckpoint{ProjectID: observed.ProjectID, OperationID: observed.OperationID})
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	checkpoint, err := c.NewCleanupCheckpoint(c.ArtifactObjectParticipant, cause, scope, 1, raw)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	if _, err = decodeArtifactCleanupCheckpoint(&checkpoint, cause, scope); err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	details := c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending, Checkpoint: &checkpoint}
	if observed.State == objc.CleanupCompleted {
		details.State, details.SafeReason = c.CleanupCompleted, ""
	}
	// The provider always scans remaining rows; this checkpoint binds only the
	// original Project and operation, never a client-controlled skip cursor.
	return c.NewCleanupReport(c.ArtifactObjectParticipant, cause, scope, details)
}

func callArtifactCleanup(cleaner artifactCleanupProvider, ctx context.Context, actor identity.Actor, cause objc.ProjectCleanupCause) (report objc.ProjectCleanupResult, err error) {
	defer func() {
		if recover() != nil {
			report, err = objc.ProjectCleanupResult{}, unavailable(nil)
		}
	}()
	return cleaner.CleanupProject(ctx, actor, cause)
}

var _ c.ProjectLifecycleParticipant = (*artifactCleanupParticipant)(nil)
