package meeting

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type deletionCleanup struct {
	original  deletionRecord
	intents   []intentRecord
	summaries []SummaryInput
}

// Each visit cancels a bounded batch through the original Execution and Model
// owners. Completed Turns may still own a running replacement contribution.
func (r *Runtime) planDeletionCleanup(ctx context.Context, p i.ProjectID, m c.MeetingID) (deletionCleanup, error) {
	var out deletionCleanup
	err := r.transaction(ctx, p, m, "delete-cleanup", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		session, err := loadSession(ctx, x, p, m)
		if err != nil {
			return err
		}
		if !session.Deleting {
			return fault(f.ConfirmationStale)
		}
		out.original, err = loadDeletion(ctx, x, p, m)
		if err != nil {
			return err
		}
		if out.original.phase != c.DeletionCancelling {
			return fault(f.InvalidState)
		}
		rows, err := x.Query(ctx, `SELECT id,generation,attempt,status FROM agenteam_meeting.contributions WHERE meeting_id=$1 AND kind='agent' AND status IN('pending','waiting_for_agent','running') ORDER BY id LIMIT 32`, m.String())
		if err != nil {
			return portError(err)
		}
		var pending []contributionRecord
		for rows.Next() {
			var v contributionRecord
			var id string
			if err = rows.Scan(&id, &v.generation, &v.attempt, &v.state); err != nil {
				rows.Close()
				return portError(err)
			}
			v.id, err = f.ParseID[c.Contribution](id)
			if err != nil || v.generation.Validate() != nil || v.attempt.Validate() != nil {
				rows.Close()
				return fault(f.InternalError)
			}
			pending = append(pending, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return portError(err)
		}
		for _, v := range pending {
			intent, err := loadIntentByContribution(ctx, x, v.id, v.generation, v.attempt)
			var known *f.Fault
			if err != nil && (!errors.As(err, &known) || known.Code != f.NotFound) {
				return err
			}
			if err == nil && (intent.execution != nil || intent.unknown) {
				intent.cancelRequested = true
				out.intents = append(out.intents, intent)
				continue
			}
			// An unattempted launch has no external owner. An unknown launch is
			// deliberately kept above and can only be observed by its old key.
			// This is a local terminal result, not a request to cancel an
			// Execution: the cancellation flag requires a current Execution.
			tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET status='cancelled',cancel_requested=false,waiting_on_execution_id=NULL,completed_at=$4 WHERE meeting_id=$1 AND id=$2 AND generation=$3 AND attempt=$5 AND current_execution_id IS NULL AND status IN('pending','waiting_for_agent')`, m.String(), v.id.String(), int64(v.generation), now(), int64(v.attempt))
			if err != nil {
				return portError(err)
			}
			if tag.RowsAffected() != 1 {
				return fault(f.InvalidState)
			}
		}
		rows, err = x.Query(ctx, `SELECT id,turn_id,input_digest,input_data,initial,expected_summary_version FROM agenteam_meeting.summary_generations WHERE meeting_id=$1 AND status IN('prepared','generating','failed') ORDER BY created_at,id LIMIT 32`, m.String())
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			input := SummaryInput{ProjectID: p, MeetingID: m}
			var generation, turn, rawDigest string
			var data []byte
			if err = rows.Scan(&generation, &turn, &rawDigest, &data, &input.Initial, &input.PreviousVersion); err != nil {
				return portError(err)
			}
			input.GenerationID, err = f.ParseID[c.SummaryGeneration](generation)
			if err != nil {
				return fault(f.InternalError)
			}
			input.TurnID, err = f.ParseID[c.Turn](turn)
			input.InputDigest = f.Digest(rawDigest)
			if err != nil || input.InputDigest != digest(data) || input.PreviousVersion < 0 || json.Unmarshal(data, &input.Messages) != nil {
				return fault(f.InternalError)
			}
			out.summaries = append(out.summaries, input)
		}
		return portError(rows.Err())
	})
	return out, err
}

func (r *Runtime) cancelDeletionOwners(ctx context.Context, p i.ProjectID, m c.MeetingID, plan deletionCleanup) error {
	var failures []error
	for _, intent := range plan.intents {
		_, err := r.advanceIntent(ctx, p, m, intent, "cancelling")
		if err != nil {
			failures = append(failures, err)
		}
	}
	for _, input := range plan.summaries {
		if err := r.cancelSummary(ctx, input); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
