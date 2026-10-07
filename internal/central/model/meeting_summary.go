package model

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// InitializeMeetingSummarySelection creates only an independent nullable
// technical selection. Startup binding is deliberately left to the caller.
func (s *Service) InitializeMeetingSummarySelection(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	cause, e := readCause("meeting-summary-initialize")
	if e != nil {
		return e
	}
	next, e := newID()
	if e != nil {
		return e
	}
	locks := []f.LockRequest{systemLock("model-platform-selection", f.Shared), systemLock("model-meeting-summary-selection", f.Exclusive), systemLock("model-references", f.Shared)}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		old, e := loadSelection(ctx, x)
		if e != nil {
			var ff *f.Fault
			if errors.As(e, &ff) && ff.Code == f.InvalidState {
				return fault(f.DependencyUnbound)
			}
			return e
		}
		if _, e = selectionView(old); e != nil {
			return e
		}
		r, e := loadMeetingSummaryState(ctx, x)
		if e != nil {
			return e
		}
		if r != nil {
			return nil
		}
		if next == old.ID {
			return unavailable(nil)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_model.meeting_summary_selection(id,singleton,version,model_id,updated_at) VALUES($1,true,1,NULL,clock_timestamp())`, next); e != nil {
			return unavailable(e)
		}
		r, e = loadMeetingSummaryState(ctx, x)
		if e != nil {
			return e
		}
		if r == nil {
			return unavailable(nil)
		}
		return nil
	})
	return commitError(result)
}

func (s *Service) GetMeetingSummarySelection(ctx context.Context, actor id.Actor) (mc.MeetingSummarySelection, error) {
	zero := mc.MeetingSummarySelection{}
	if ctx == nil {
		return zero, fault(f.InvalidArgument)
	}
	if e := human(actor); e != nil {
		return zero, e
	}
	state := s.state()
	if state == nil {
		return zero, fault(f.DependencyUnbound)
	}
	ctx, cancel := context.WithTimeout(ctx, managementReadBudget)
	defer cancel()
	cause, e := readCause("meeting-summary-query")
	if e != nil {
		return zero, e
	}
	locks := []f.LockRequest{userLock(actor.Details().UserID), systemLock("model-meeting-summary-selection", f.Shared), systemLock("model-references", f.Shared)}
	var out mc.MeetingSummarySelection
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := state.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		if e := state.store.RequireHeldLocks(ctx, tx, locks); e != nil {
			return portError(e)
		}
		if e := state.authority.currentScope(ctx, tx, actor, id.SystemScope(), id.Read); e != nil {
			return e
		}
		x, e := state.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		r, e := loadMeetingSummaryState(ctx, x)
		if e != nil {
			return e
		}
		out, e = r.view()
		return e
	})
	if e = commitError(result); e != nil {
		return zero, e
	}
	if e = ctx.Err(); e != nil {
		return zero, f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(e)
	}
	return out, nil
}
func (s *Service) UpdateMeetingSummarySelection(ctx context.Context, r mc.UpdateMeetingSummarySelectionRequest) (mc.CommandReceipt, error) {
	if ctx == nil {
		return mc.CommandReceipt{}, fault(f.InvalidArgument)
	}
	if e := r.Validate(); e != nil {
		return mc.CommandReceipt{}, e
	}
	model := r.Model
	return s.runCommand(ctx, commandRequest{Meta: r.CommandMeta, Kind: "model.selection.update", Resource: r.SelectionID, Expected: r.ExpectedVersion, MeetingSummary: &model})
}
