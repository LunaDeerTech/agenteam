package meeting

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func loadContributionHistory(ctx context.Context, x postgres.SQLExecutor, id c.ContributionID) ([]c.ContributionHistory, error) {
	rows, err := x.Query(ctx, `SELECT execution_id,generation,attempt,status,message_id FROM agenteam_meeting.contribution_executions WHERE contribution_id=$1 ORDER BY generation,attempt LIMIT 1001`, id.String())
	if err != nil {
		return nil, portError(err)
	}
	out := []c.ContributionHistory{}
	messages := []*string{}
	for rows.Next() {
		var raw string
		var generation, attempt int64
		var message *string
		v := c.ContributionHistory{}
		if err = rows.Scan(&raw, &generation, &attempt, &v.Status, &message); err != nil {
			rows.Close()
			return nil, portError(err)
		}
		v.ExecutionID, err = f.ParseID[i.Execution](raw)
		if err != nil {
			rows.Close()
			return nil, fault(f.InternalError)
		}
		v.Generation, v.Attempt = f.Version(generation), f.Version(attempt)
		out = append(out, v)
		messages = append(messages, message)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, portError(err)
	}
	if len(out) > 1000 {
		return nil, fault(f.PayloadTooLarge)
	}
	for n := range out {
		intent, err := loadIntentByContribution(ctx, x, id, out[n].Generation, out[n].Attempt)
		if err != nil {
			return nil, err
		}
		if intent.execution == nil || *intent.execution != out[n].ExecutionID {
			return nil, fault(f.InternalError)
		}
		out[n].RetryOf, out[n].RegenerateOf = intent.request.Lineage.RetryOf, intent.request.Lineage.RegenerateOf
		if messages[n] != nil {
			value, err := loadMessageView(ctx, x, *messages[n])
			if err != nil {
				return nil, err
			}
			out[n].Message = &value
		}
	}
	return out, nil
}

func loadMessageView(ctx context.Context, x postgres.SQLExecutor, raw string) (c.MessageView, error) {
	v := c.MessageView{}
	var id, participant string
	var content []byte
	var reply *string
	err := x.QueryRow(ctx, `SELECT id,participant_id,content,reply_to_id,created_at FROM agenteam_meeting.messages WHERE id=$1`, raw).Scan(&id, &participant, &content, &reply, &v.CreatedAt)
	if err != nil {
		return v, portError(err)
	}
	if v.ID, err = f.ParseID[c.Message](id); err != nil {
		return v, fault(f.InternalError)
	}
	if v.ParticipantID, err = f.ParseID[c.Participant](participant); err != nil {
		return v, fault(f.InternalError)
	}
	if json.Unmarshal(content, &v.Content) != nil {
		return v, fault(f.InternalError)
	}
	if reply != nil {
		r, e := f.ParseID[c.Message](*reply)
		if e != nil {
			return v, fault(f.InternalError)
		}
		v.ReplyTo = &r
	}
	return v, nil
}
