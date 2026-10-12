package execution

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// These fixed wrappers encompass the original methods' complete returns,
// including synchronous Model/Loop drains and preparation checkpoints. Legacy
// constructors retain their original admission and recovery paths.
func finishProjectCall(state *projectCallState, entry *projectCallEntry, err *error) {
	if failure := recover(); failure != nil {
		_ = state.finish(entry, f.NewFault(f.CommitUnknown, f.Unknown))
		state.returned(entry)
		panic(failure)
	}
	*err = state.finish(entry, *err)
	state.returned(entry)
}

func (s *Service) Launch(ctx context.Context, actor i.Actor, request c.LaunchRequest) (out c.LaunchResult, err error) {
	if s == nil || s.projectCalls == nil {
		return s.launch(ctx, actor, request)
	}
	if ctx == nil || request.Validate() != nil {
		return out, invalid()
	}
	if err = launchActor(actor); err != nil {
		return out, err
	}
	request = request.Clone()
	digest, err := request.Digest()
	if err != nil {
		return out, err
	}
	key := c.LaunchLookupKey{ProjectID: request.ProjectID, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}
	ctx, entry, err := s.projectCalls.admitService(ctx, actor, key, digest, "launch")
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = c.LaunchResult{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return out, err
	}
	out, err = s.launch(ctx, actor, request)
	bodyErr = err
	return out, err
}

func (s *Service) LookupLaunch(ctx context.Context, actor i.Actor, key c.LaunchLookupKey, expected f.Digest) (out c.LaunchLookup, err error) {
	if s == nil || s.projectCalls == nil {
		return s.lookupLaunch(ctx, actor, key, expected)
	}
	if ctx == nil || key.Validate() != nil || expected.Validate() != nil {
		return out, invalid()
	}
	if err = launchActor(actor); err != nil {
		return out, err
	}
	ctx, entry, err := s.projectCalls.admitService(ctx, actor, key, expected, "lookup_launch")
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = c.LaunchLookup{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return out, err
	}
	// The owned admission allows only exact retained recovery after Stop.
	// Its original technical owner counts this read until actual Store return;
	// the legacy callSet cannot admit a new call after global shutdown.
	out, err = s.lookup(ctx, actor, key, expected)
	bodyErr = err
	if err == nil && out.Found && out.RequestDigest == expected {
		s.projectCalls.recoveredProjectCall(entry, nil)
	}
	return out, err
}

func (d *PreparationDriver) Run(ctx context.Context, execution i.ExecutionID) (err error) {
	if d == nil || d.state == nil || d.state.projectCalls == nil {
		return d.run(ctx, execution)
	}
	if ctx == nil || execution.Validate() != nil {
		return invalid()
	}
	state := d.state.projectCalls
	ctx, entry, err := state.admitDriver(ctx, execution, projectLanePreparation, "prepare")
	if entry != nil {
		defer finishProjectCall(state, entry, &err)
	}
	if err != nil {
		return err
	}
	return d.run(ctx, execution)
}

func (d *PreparationDriver) ResolveUnknown(ctx context.Context, execution i.ExecutionID) (err error) {
	if d == nil || d.state == nil || d.state.projectCalls == nil {
		return d.resolveUnknown(ctx, execution)
	}
	if ctx == nil || execution.Validate() != nil {
		return invalid()
	}
	state := d.state.projectCalls
	ctx, entry, err := state.admitDriver(ctx, execution, projectLanePreparation, "resolve_preparation")
	if entry != nil {
		defer finishProjectCall(state, entry, &err)
	}
	if err != nil {
		return err
	}
	err = d.resolveUnknown(ctx, execution)
	if err == nil {
		state.recoveredProjectCall(entry, nil)
	}
	return err
}

func (d *DirectTextDriver) Start(ctx context.Context, execution i.ExecutionID) (out c.DirectTextReceipt, err error) {
	if d == nil || d.state == nil || d.state.projectCalls == nil {
		return d.start(ctx, execution)
	}
	if ctx == nil || execution.Validate() != nil {
		return out, invalid()
	}
	state := d.state.projectCalls
	ctx, entry, err := state.admitDriver(ctx, execution, projectLaneDirect, "start")
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = c.DirectTextReceipt{}
			}
		}()
		defer finishProjectCall(state, entry, &err)
	}
	if err != nil {
		return out, err
	}
	out, err = d.start(ctx, execution)
	bodyErr = err
	return out, err
}

func (d *DirectTextDriver) ResolveUnknown(ctx context.Context, execution i.ExecutionID) (out c.DirectTextReceipt, err error) {
	if d == nil || d.state == nil || d.state.projectCalls == nil {
		return d.resolveUnknown(ctx, execution)
	}
	if ctx == nil || execution.Validate() != nil {
		return out, invalid()
	}
	state := d.state.projectCalls
	ctx, entry, err := state.admitDriver(ctx, execution, projectLaneDirect, "resolve_direct")
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = c.DirectTextReceipt{}
			}
		}()
		defer finishProjectCall(state, entry, &err)
	}
	if err != nil {
		return out, err
	}
	d.state.mu.Lock()
	original := d.state.calls[execution]
	d.state.mu.Unlock()
	out, err = d.resolveUnknown(ctx, execution)
	bodyErr = err
	d.state.mu.Lock()
	retired := original != nil && d.state.calls[execution] != original && original.returned && original.unresolved == nil
	d.state.mu.Unlock()
	if retired {
		state.recoveredProjectCall(entry, nil)
	}
	return out, err
}
