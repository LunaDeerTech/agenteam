package model

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// Only the original Resolver can attach this source to its actual Unknown.
// No public DTO, transaction cause, or lookalike row can mint the observation.
type resolutionDiscoveryUnknown struct {
	original  error
	candidate *resolutionCandidate
	readOnly  bool
}

func (e *resolutionDiscoveryUnknown) Error() string { return string(f.CommitUnknown) }
func (e *resolutionDiscoveryUnknown) Unwrap() error { return e.original }
func resolutionDiscoveryOutcome(result f.CommitResult, c *resolutionCandidate, readOnly bool) error {
	err := commitError(result)
	if err == nil || result.State() != f.Unknown {
		return err
	}
	return &resolutionDiscoveryUnknown{err, c, readOnly}
}

type executionCaptureUnknown struct {
	owner     *ExecutionCapture
	request   mc.ExecutionModelCaptureRequest
	attempt   f.Digest
	source    executionModelSource
	original  error
	discovery *resolutionDiscoveryUnknown
	locks     []f.LockRequest
}

func (e *executionCaptureUnknown) Error() string { return string(f.CommitUnknown) }
func (e *executionCaptureUnknown) Unwrap() error { return e.original }
func (e *executionCaptureUnknown) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte(string(f.CommitUnknown)))
}
func (e *executionCaptureUnknown) LogValue() slog.Value {
	return slog.StringValue(string(f.CommitUnknown))
}

func (p *ExecutionCapture) ObserveExecutionModelDiscovery(ctx context.Context, r mc.ExecutionModelCaptureRequest, original error) (mc.ModelCaptureObservation, error) {
	pending := mc.ModelCapturePending
	if err := p.valid(ctx, r); err != nil {
		return pending, err
	}
	var observation *executionCaptureUnknown
	if !errors.As(original, &observation) || observation == nil || observation.owner != p || observation.request != r || observation.attempt.Validate() != nil {
		return pending, fault(f.Forbidden)
	}
	var physical *UnknownCommandError
	if !errors.As(observation.original, &physical) || physical.AttemptID().Validate() != nil || physical.Cause().Validate() != nil {
		return pending, fault(f.Forbidden)
	}
	locks := slices.Clone(observation.locks)
	if observation.discovery != nil {
		c := observation.discovery.candidate
		if c == nil || c.authority != p.models.state().authority {
			return pending, fault(f.Forbidden)
		}
		expected, err := executionModelResolveRequest(r, observation.source)
		if err != nil {
			return pending, err
		}
		a, _ := mc.ResolveBinding(expected)
		b, err := mc.ResolveBinding(c.request)
		if err != nil || a != b || physical.Cause().Details().Kind != f.CommandsCause || physical.Cause().Details().Primary.Canonical() != c.identity.Canonical() {
			return pending, fault(f.Forbidden)
		}
		locks, err = resolutionUnion(locks, c.locks)
		if err != nil {
			return pending, err
		}
	}
	cause, err := readCause("execution-capture-observe")
	if err != nil {
		return pending, err
	}
	state := pending
	store := p.models.state().store
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return executionCaptureError(err)
		}
		x, err := p.executor(ctx, tx, locks)
		if err != nil {
			return err
		}
		scope, err := p.owner.RequireModelCaptureDiscoveryInTx(ctx, tx, r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return executionCaptureError(err)
		}
		if err = modelCaptureScope(r, scope); err != nil {
			return err
		}
		if scope.AttemptBinding != observation.attempt {
			return fault(f.ConfirmationStale)
		}
		if observation.discovery == nil || observation.discovery.readOnly {
			state = mc.ModelCaptureReadOnly
			return ctx.Err()
		}
		c := observation.discovery.candidate
		row, err := loadResolutionPreparation(ctx, x, c.identity.Canonical())
		if err != nil {
			return err
		}
		// Absence or a later/unrelated plan cannot settle this original intention.
		if row == nil || c.row == nil || row.ID != c.row.ID || row.SnapshotID != c.draft.Snapshot.ID || row.Semantic != resolutionSemantic(c.request) || row.Version != c.version || !resolutionEqual(row.Draft, c.draft) {
			return ctx.Err()
		}
		if row.Phase == "committed" {
			if err = loadCommittedResolution(ctx, x, row); err != nil {
				return err
			}
		}
		state = mc.ModelCapturePrepared
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return pending, err
	}
	if err = ctx.Err(); err != nil {
		return pending, err
	}
	return state, nil
}

var _ mc.ExecutionModelCaptureObserver = (*ExecutionCapture)(nil)
