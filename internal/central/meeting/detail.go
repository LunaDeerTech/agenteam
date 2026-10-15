package meeting

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) read(ctx context.Context, a i.Actor, p i.ProjectID, m *c.MeetingID, fn func(context.Context, postgres.SQLExecutor) error) error {
	if s == nil || ctx == nil || p.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return err
	}
	locks := []f.LockRequest{userLock(a), projectLock(p, f.Shared)}
	if m != nil {
		if m.Validate() != nil {
			return fault(f.InvalidArgument)
		}
		locks = append(locks, meetingLock(*m, f.Shared))
	}
	locks, err := normalize(locks)
	if err != nil {
		return err
	}
	cause, err := readCause("meeting.view")
	if err != nil {
		return err
	}
	return commitError(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, p, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, x)
	}))
}
func (s *Service) List(ctx context.Context, a i.Actor, p i.ProjectID) ([]c.Session, error) {
	out := []c.Session{}
	err := s.read(ctx, a, p, nil, func(ctx context.Context, x postgres.SQLExecutor) error {
		rows, err := x.Query(ctx, `SELECT id FROM agenteam_meeting.sessions WHERE project_id=$1 ORDER BY updated_at DESC,id DESC LIMIT 201`, p.String())
		if err != nil {
			return portError(err)
		}
		ids := []c.MeetingID{}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return portError(err)
			}
			id, e := f.ParseID[c.Meeting](raw)
			if e != nil {
				rows.Close()
				return fault(f.InternalError)
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return portError(err)
		}
		if len(ids) > 200 {
			return fault(f.PayloadTooLarge)
		}
		for _, id := range ids {
			session, err := loadSession(ctx, x, p, id)
			if err != nil {
				return err
			}
			out = append(out, session)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) GetDetail(ctx context.Context, a i.Actor, p i.ProjectID, m c.MeetingID) (c.Detail, error) {
	out := c.Detail{References: []c.Reference{}, Turns: []c.TurnView{}, Decisions: []c.DecisionView{}}
	err := s.read(ctx, a, p, &m, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		out.Session, err = loadSession(ctx, x, p, m)
		if err != nil {
			return err
		}
		out.Participants, err = loadParticipants(ctx, x, m)
		if err != nil {
			return err
		}
		rows, err := x.Query(ctx, `SELECT kind,stable_id,label FROM agenteam_meeting.meeting_references WHERE meeting_id=$1 ORDER BY sort_order,kind,stable_id`, m.String())
		if err != nil {
			return portError(err)
		}
		for rows.Next() {
			var ref c.Reference
			if err = rows.Scan(&ref.Kind, &ref.ID, &ref.Label); err != nil {
				rows.Close()
				return portError(err)
			}
			out.References = append(out.References, ref)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return portError(err)
		}
		rows, err = x.Query(ctx, `SELECT id,mode,status,COALESCE(result,''),version,created_at,summary_error FROM agenteam_meeting.turns WHERE meeting_id=$1 ORDER BY created_at,id LIMIT 1001`, m.String())
		if err != nil {
			return portError(err)
		}
		for rows.Next() {
			var turn c.TurnView
			var id string
			var version int64
			if err = rows.Scan(&id, &turn.Mode, &turn.Status, &turn.Result, &version, &turn.CreatedAt, &turn.SummaryError); err != nil {
				rows.Close()
				return portError(err)
			}
			turn.ID, err = f.ParseID[c.Turn](id)
			if err != nil {
				rows.Close()
				return fault(f.InternalError)
			}
			turn.Version = f.Version(version)
			out.Turns = append(out.Turns, turn)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return portError(err)
		}
		if len(out.Turns) > 1000 {
			return fault(f.PayloadTooLarge)
		}
		for n := range out.Turns {
			out.Turns[n].Contributions, err = loadContributions(ctx, x, out.Turns[n].ID)
			if err != nil {
				return err
			}
		}
		rows, err = x.Query(ctx, `SELECT id,contribution_id,execution_id,question,options,status,answer,version FROM agenteam_meeting.decision_requests WHERE meeting_id=$1 ORDER BY created_at,id`, m.String())
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var decision c.DecisionView
			var id, contribution, execution string
			var options []byte
			var version int64
			if err = rows.Scan(&id, &contribution, &execution, &decision.Question, &options, &decision.Status, &decision.Answer, &version); err != nil {
				return portError(err)
			}
			if decision.ID, err = f.ParseID[c.Decision](id); err != nil {
				return fault(f.InternalError)
			}
			if decision.ContributionID, err = f.ParseID[c.Contribution](contribution); err != nil {
				return fault(f.InternalError)
			}
			if decision.ExecutionID, err = f.ParseID[i.Execution](execution); err != nil {
				return fault(f.InternalError)
			}
			if json.Unmarshal(options, &decision.Options) != nil {
				return fault(f.InternalError)
			}
			decision.Version = f.Version(version)
			out.Decisions = append(out.Decisions, decision)
		}
		return portError(rows.Err())
	})
	if err != nil {
		return c.Detail{}, err
	}
	return out, nil
}
func loadContributions(ctx context.Context, x postgres.SQLExecutor, turn c.TurnID) ([]c.ContributionView, error) {
	rows, err := x.Query(ctx, `SELECT id,participant_id,kind,sort_order,status,generation,attempt,current_execution_id,waiting_on_execution_id,current_message_id,started_at,completed_at,cancel_requested FROM agenteam_meeting.contributions WHERE turn_id=$1 ORDER BY sort_order`, turn.String())
	if err != nil {
		return nil, portError(err)
	}
	out := []c.ContributionView{}
	messageIDs := []*string{}
	for rows.Next() {
		v := c.ContributionView{TurnID: turn, History: []c.ContributionHistory{}}
		var id, participant string
		var execution, waiting, message *string
		var generation, attempt int64
		if err = rows.Scan(&id, &participant, &v.Kind, &v.Order, &v.Status, &generation, &attempt, &execution, &waiting, &message, &v.StartedAt, &v.CompletedAt, &v.CancelRequested); err != nil {
			rows.Close()
			return nil, portError(err)
		}
		if v.ID, err = f.ParseID[c.Contribution](id); err != nil {
			rows.Close()
			return nil, fault(f.InternalError)
		}
		if v.ParticipantID, err = f.ParseID[c.Participant](participant); err != nil {
			rows.Close()
			return nil, fault(f.InternalError)
		}
		v.Generation, v.Attempt = f.Version(generation), f.Version(attempt)
		for _, pair := range []struct {
			raw    *string
			target **i.ExecutionID
		}{{execution, &v.ExecutionID}, {waiting, &v.WaitingOn}} {
			if pair.raw != nil {
				parsed, e := f.ParseID[i.Execution](*pair.raw)
				if e != nil {
					rows.Close()
					return nil, fault(f.InternalError)
				}
				*pair.target = &parsed
			}
		}
		out = append(out, v)
		messageIDs = append(messageIDs, message)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, portError(err)
	}
	for n, raw := range messageIDs {
		if raw != nil {
			value, err := loadMessageView(ctx, x, *raw)
			if err != nil {
				return nil, err
			}
			out[n].Message = &value
		}
		if out[n].Kind == "agent" {
			out[n].History, err = loadContributionHistory(ctx, x, out[n].ID)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
