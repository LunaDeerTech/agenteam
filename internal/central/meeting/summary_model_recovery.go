package meeting

import (
	"context"
	"encoding/json"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"strings"
	"time"
)

func unknownSummary() error { return f.NewFault(f.CommitUnknown, f.Unknown) }
func isUnknown(err error) bool {
	var v *f.Fault
	return errors.As(err, &v) && v.CommitState == f.Unknown
}
func responseCandidate(response mc.ModelResponse, initial bool) (SummaryCandidate, error) {
	if response.FinishReason != "stop" {
		return SummaryCandidate{}, fault(f.InvalidState)
	}
	var text strings.Builder
	for _, part := range response.Message.Parts {
		if part.Text == nil {
			return SummaryCandidate{}, fault(f.InvalidState)
		}
		text.WriteString(part.Text.Text)
	}
	return ParseSummary([]byte(text.String()), initial)
}
func (m *SummaryModel) completedCandidate(ctx context.Context, input SummaryInput, row summaryCallRecord) (SummaryCandidate, error) {
	if row.status != "succeeded" {
		return SummaryCandidate{}, fault(f.DependencyUnavailable)
	}
	var raw []byte
	err := m.tx(ctx, input, "read-candidate", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		return portError(x.QueryRow(ctx, `SELECT candidate FROM agenteam_meeting.summary_generations WHERE id=$1 AND input_digest=$2`, input.GenerationID.String(), input.InputDigest.String()).Scan(&raw))
	})
	if err != nil {
		return SummaryCandidate{}, err
	}
	return ParseSummary(raw, input.Initial)
}
func (m *SummaryModel) recoverCall(ctx context.Context, call *summaryModelCall) (SummaryCandidate, error) {
	if !call.mu.TryLock() {
		return SummaryCandidate{}, fault(f.ResourceBusy)
	}
	defer call.mu.Unlock()
	if call.done {
		if call.candidate != nil {
			return *call.candidate, nil
		}
		return SummaryCandidate{}, call.failure
	}
	cleanup, cancel := context.WithTimeout(context.WithValue(ctx, summaryOwnerKey{}, call.owner), 5*time.Second)
	defer cancel()
	// A lost process's admitted provider call cannot be replaced with BeginChat.
	// Without the original handle or formal terminal proof it remains Unknown.
	if call.dispatchUnknown {
		var row summaryCallRecord
		err := m.tx(cleanup, call.owner.input, "read-stopped-call", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			var e error
			row, e = loadSummaryCall(ctx, x, call.owner.call)
			return e
		})
		if err != nil {
			return SummaryCandidate{}, err
		}
		candidate, err := m.recoverStoppedCall(cleanup, call, row)
		if call.done {
			m.mu.Lock()
			delete(m.calls, call.owner.call)
			m.mu.Unlock()
		}
		return candidate, err
	}
	if call.resolutionUnknown {
		if call.resolve == nil {
			return SummaryCandidate{}, unknownSummary()
		}
		resolved, found, err := m.deps.Resolver.LookupResolution(context.WithValue(cleanup, summaryResolutionLookupKey{}, call), call.resolve.Clone())
		if err != nil {
			return SummaryCandidate{}, err
		}
		if !found {
			return SummaryCandidate{}, unknownSummary()
		}
		call.resolved = &resolved
		if err = call.owner.bindResolved(resolved); err != nil {
			return SummaryCandidate{}, err
		}
		call.resolutionUnknown = false
		if err = m.tx(cleanup, call.owner.input, "recover-resolution", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET snapshot_id=$2,resolution_unknown=false WHERE call_id=$1 AND snapshot_id IS NULL`, call.owner.call.String(), resolved.Snapshot.ID.String())
			return portError(e)
		}); err != nil {
			return SummaryCandidate{}, err
		}
	}
	if call.resolved != nil && !call.stoppedSnapshotSettled && (call.pin == nil || call.pin.unknown) {
		if err := m.pinSummarySnapshot(cleanup, call); err != nil {
			if isUnknown(err) {
				return SummaryCandidate{}, err
			}
			call.failure = err
		}
	}
	if call.handle != nil {
		closeErr := call.handle.Close(cleanup)
		if !call.handle.Joined() {
			return SummaryCandidate{}, errors.Join(closeErr, unknownSummary())
		}
		if response, err := call.handle.Result(cleanup); err == nil {
			candidate, e := responseCandidate(response, call.owner.input.Initial)
			call.failure = e
			if e == nil {
				call.candidate = &candidate
			}
		} else {
			call.failure = err
		}
	}
	if call.failure == nil && call.candidate == nil {
		call.failure = fault(f.DependencyUnavailable)
	}
	if err := m.finishCall(cleanup, call, call.candidate != nil && call.failure == nil); err != nil {
		return SummaryCandidate{}, err
	}
	m.mu.Lock()
	if m.calls[call.owner.call] == call {
		delete(m.calls, call.owner.call)
	}
	m.mu.Unlock()
	if call.candidate != nil && call.failure == nil {
		return *call.candidate, nil
	}
	return SummaryCandidate{}, call.failure
}
func (m *SummaryModel) finishCall(ctx context.Context, call *summaryModelCall, success bool) error {
	if call.resolutionUnknown || call.dispatchUnknown || call.pin != nil && call.pin.unknown || call.handle != nil && !call.handle.Joined() {
		return unknownSummary()
	}
	status := "failed"
	if success {
		status = "succeeded"
	}
	if call.returnedUnknown {
		var row summaryCallRecord
		err := m.tx(ctx, call.owner.input, "confirm-returned", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			var e error
			row, e = loadSummaryCall(ctx, x, call.owner.call)
			return e
		})
		if err != nil {
			return err
		}
		if row.returned == nil {
			return unknownSummary()
		}
		if row.status != status {
			return fault(f.ConfirmationStale)
		}
		call.returnedUnknown = false
	} else {
		err := m.tx(ctx, call.owner.input, "returned", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET status=$2,returned_at=COALESCE(returned_at,$3),terminal_version=1 WHERE call_id=$1 AND returned_at IS NULL`, call.owner.call.String(), status, now())
			if err != nil {
				return portError(err)
			}
			if success {
				raw, err := json.Marshal(call.candidate)
				if err != nil {
					return portError(err)
				}
				_, err = x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations SET candidate=$2 WHERE id=$1 AND status='generating'`, call.owner.input.GenerationID.String(), raw)
				return portError(err)
			}
			return nil
		})
		if err != nil {
			call.returnedUnknown = isUnknown(err)
			return err
		}
	}
	if call.resolved != nil && call.resolved.CredentialLease != nil {
		if call.retirementPlan == nil {
			request := mc.ModelCallRetirementRequest{Actor: call.actor, Model: call.resolved.Clone(), TerminalVersion: 1}
			plan, err := m.deps.Retirement.DiscoverModelCallRetirement(ctx, request)
			if err != nil {
				return err
			}
			call.retirementRequest = &request
			call.retirementPlan = plan
		}
		request, plan := *call.retirementRequest, call.retirementPlan
		if call.retirementUnknown {
			confirmed := false
			err := m.tx(ctx, call.owner.input, "confirm-retirement", plan.RequiredLocks(), func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
				retired, e := m.deps.Retirement.ModelCallRetiredInTx(ctx, tx, request, plan)
				if e != nil {
					return e
				}
				var at *time.Time
				if e = x.QueryRow(ctx, `SELECT retired_at FROM agenteam_meeting.summary_model_calls WHERE call_id=$1`, call.owner.call.String()).Scan(&at); e != nil {
					return portError(e)
				}
				confirmed = retired && at != nil
				return nil
			})
			if err != nil {
				return err
			}
			if !confirmed {
				return unknownSummary()
			}
		} else {
			err := m.tx(ctx, call.owner.input, "retire", plan.RequiredLocks(), func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
				if err := m.deps.Retirement.ReleaseModelCallInTx(ctx, tx, request, plan); err != nil {
					return err
				}
				_, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET retired_at=COALESCE(retired_at,$2) WHERE call_id=$1 AND returned_at IS NOT NULL`, call.owner.call.String(), now())
				return portError(err)
			})
			if err != nil {
				call.retirementUnknown = isUnknown(err)
				return err
			}
		}
	} else {
		if err := m.tx(ctx, call.owner.input, "retire-no-lease", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET retired_at=COALESCE(retired_at,$2) WHERE call_id=$1 AND returned_at IS NOT NULL`, call.owner.call.String(), now())
			return portError(err)
		}); err != nil {
			return err
		}
	}
	call.done = true
	if call.pin != nil {
		call.pin.witness.active = false
	}
	call.owner.active = false
	return nil
}
