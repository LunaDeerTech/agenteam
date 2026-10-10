package execution

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// Domain planning can own a durable intent before the caller's final capture
// transaction. Keep that original error and physical owner; a generic Fault
// must not turn a foreign Unknown into a known rolled-back capture.
func preparationUnknown(err error) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && (fault.CommitState == f.Unknown || fault.Code == f.CommitUnknown)
}

func (s *preparationState) observeModelDiscovery(ctx context.Context, run *preparationCall) (err error) {
	if run.modelUnknown == nil {
		return nil
	}
	observer, ok := s.models.(mc.ExecutionModelCaptureObserver)
	if !ok || nilPort(observer) {
		return run.unresolved
	}
	defer func() {
		if recover() != nil {
			err = run.unresolved
		}
	}()
	request := mc.ExecutionModelCaptureRequest{ProjectID: run.request.Launch.ProjectID, AgentID: run.request.Launch.AgentID, ExecutionID: run.request.ExecutionID}
	var observation mc.ModelCaptureObservation
	err = s.resourceCall(ctx, run.request, *run.claim, "model", true, func(recoveryCtx context.Context) error {
		var observeErr error
		observation, observeErr = observer.ObserveExecutionModelDiscovery(recoveryCtx, request, run.modelUnknown)
		return observeErr
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return run.unresolved
	}
	if observation != mc.ModelCaptureReadOnly && observation != mc.ModelCapturePrepared {
		return run.unresolved
	}
	return nil
}
