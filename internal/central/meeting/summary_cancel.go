package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type summaryCancellationOwner interface {
	cancelSummaryCalls(context.Context, SummaryInput) error
}

func summaryCancellationRequested(ctx context.Context, x postgres.SQLExecutor, input SummaryInput) (bool, error) {
	var requested bool
	err := x.QueryRow(ctx, `SELECT t.cancel_requested OR s.deleting FROM agenteam_meeting.turns t JOIN agenteam_meeting.sessions s ON s.id=t.meeting_id WHERE s.project_id=$1 AND s.id=$2 AND t.id=$3`, input.ProjectID.String(), input.MeetingID.String(), input.TurnID.String()).Scan(&requested)
	return requested, portError(err)
}

// A runtime visit owns GenerateSummary until it returns. Observe cancellation
// separately so the same visit can cancel its original Model handle promptly.
// Every observation is a short transaction; the watcher joins before return.
func (m *SummaryModel) watchSummaryCancellation(ctx context.Context, call *summaryModelCall) func() {
	watch, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watch.Done():
				return
			case <-ticker.C:
			}
			var requested bool
			err := m.tx(watch, call.owner.input, "observe-cancel", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
				var err error
				requested, err = summaryCancellationRequested(ctx, x, call.owner.input)
				return err
			})
			if err == nil && requested {
				call.cancel()
				return
			}
		}
	}()
	return func() { stop(); <-done }
}

// No cancellation fact authorizes a replacement Model call. Warm calls use
// their original owner; cold calls require the formal stopped/retired proof.
func (m *SummaryModel) cancelSummaryCalls(ctx context.Context, input SummaryInput) error {
	var calls []mc.CallID
	err := m.tx(ctx, input, "cancel-calls", nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := m.deps.Authority.guard.RequireCurrentProcessInTx(ctx, tx, m.deps.Authority.process); err != nil {
			return portError(err)
		}
		requested, err := summaryCancellationRequested(ctx, x, input)
		if err != nil {
			return err
		}
		if !requested {
			return fault(f.ConfirmationStale)
		}
		rows, err := x.Query(ctx, `SELECT call_id FROM agenteam_meeting.summary_model_calls WHERE generation_id=$1 AND project_id=$2 AND meeting_id=$3 AND retired_at IS NULL ORDER BY attempt`, input.GenerationID.String(), input.ProjectID.String(), input.MeetingID.String())
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return portError(err)
			}
			id, err := f.ParseID[mc.Call](raw)
			if err != nil {
				return fault(f.InternalError)
			}
			calls = append(calls, id)
		}
		return portError(rows.Err())
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range calls {
		m.mu.Lock()
		call := m.calls[id]
		m.mu.Unlock()
		if call != nil {
			call.cancel()
			_, err = m.recoverCall(ctx, call)
		} else {
			var row summaryCallRecord
			err = m.tx(ctx, input, "read-cancel-call", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
				var e error
				row, e = loadSummaryCall(ctx, x, id)
				return e
			})
			if err == nil && row.retired == nil {
				scope, e := i.InProject(input.ProjectID)
				if e != nil {
					return e
				}
				actor, e := m.deps.Actor.Actor(input.GenerationID.String(), scope)
				if e != nil {
					return e
				}
				owner := &summaryOwner{authority: m.deps.Authority, input: input, call: id, active: true}
				cleanup := &summaryModelCall{owner: owner, actor: actor, dispatchUnknown: true}
				_, err = m.recoverStoppedCall(ctx, cleanup, row)
				owner.active = false
			}
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	// A cancelled provider reports an error after successful retirement. Only
	// the durable original-call facts decide whether cancellation may finish.
	err = m.tx(ctx, input, "confirm-cancel-calls", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var active bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.summary_model_calls WHERE generation_id=$1 AND retired_at IS NULL)`, input.GenerationID.String()).Scan(&active); err != nil {
			return portError(err)
		}
		if active {
			return errors.Join(unknownSummary(), errors.Join(failures...))
		}
		return nil
	})
	return err
}

func (r *Runtime) cancelSummary(ctx context.Context, input SummaryInput) error {
	if owner, ok := r.deps.Summaries.(summaryCancellationOwner); ok {
		if err := owner.cancelSummaryCalls(ctx, input); err != nil {
			return err
		}
	}
	return r.summaryTransaction(ctx, input, "cancelled", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		requested, err := summaryCancellationRequested(ctx, x, input)
		if err != nil {
			return err
		}
		if !requested {
			return fault(f.ConfirmationStale)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations g SET status='cancelled',error_code='' WHERE g.id=$1 AND g.meeting_id=$2 AND g.turn_id=$3 AND g.input_digest=$4 AND g.status IN('prepared','generating','failed','cancelled') AND NOT EXISTS(SELECT 1 FROM agenteam_meeting.summary_model_calls mc WHERE mc.generation_id=g.id AND mc.retired_at IS NULL)`, input.GenerationID.String(), input.MeetingID.String(), input.TurnID.String(), input.InputDigest.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return nil
	})
}

func (r *Runtime) cancelTurnSummary(ctx context.Context, p i.ProjectID, m c.MeetingID, t c.TurnID) error {
	input := SummaryInput{ProjectID: p, MeetingID: m, TurnID: t}
	found := false
	err := r.transaction(ctx, p, m, "read-cancel-summary", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var id, rawDigest string
		var data []byte
		err := x.QueryRow(ctx, `SELECT id,input_digest,input_data,initial,expected_summary_version FROM agenteam_meeting.summary_generations WHERE meeting_id=$1 AND turn_id=$2 AND status IN('prepared','generating','failed') ORDER BY created_at,id LIMIT 1`, m.String(), t.String()).Scan(&id, &rawDigest, &data, &input.Initial, &input.PreviousVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return portError(err)
		}
		input.GenerationID, err = f.ParseID[c.SummaryGeneration](id)
		if err != nil || json.Unmarshal(data, &input.Messages) != nil {
			return fault(f.InternalError)
		}
		input.InputDigest = f.Digest(rawDigest)
		found = true
		return nil
	})
	if err != nil || !found {
		return err
	}
	return r.cancelSummary(ctx, input)
}

var _ summaryCancellationOwner = (*SummaryModel)(nil)
