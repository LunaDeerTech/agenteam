package meeting

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func loadSession(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, id c.MeetingID) (c.Session, error) {
	out := c.Session{ID: id, ProjectID: p}
	var version int64
	var summary []byte
	err := x.QueryRow(ctx, `SELECT status,version,title,proposal_content,summary,deleting,created_at,updated_at FROM agenteam_meeting.sessions WHERE project_id=$1 AND id=$2`, p.String(), id.String()).Scan(&out.Status, &version, &out.Title, &out.ProposalContent, &summary, &out.Deleting, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.Session{}, fault(f.NotFound)
	}
	if err != nil {
		return c.Session{}, portError(err)
	}
	out.Version = f.Version(version)
	if len(summary) > 0 {
		var value c.RollingSummary
		if json.Unmarshal(summary, &value) != nil {
			return c.Session{}, fault(f.InternalError)
		}
		out.Summary = &value
	}
	return out, nil
}
func loadParticipants(ctx context.Context, x postgres.SQLExecutor, meeting c.MeetingID) ([]c.ParticipantView, error) {
	rows, err := x.Query(ctx, `SELECT id,kind,source_id,name,description,sort_order,active FROM agenteam_meeting.participants WHERE meeting_id=$1 ORDER BY sort_order,id`, meeting.String())
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	out := []c.ParticipantView{}
	for rows.Next() {
		var raw string
		v := c.ParticipantView{MeetingID: meeting}
		if err = rows.Scan(&raw, &v.Kind, &v.SourceID, &v.Name, &v.Description, &v.Order, &v.Active); err != nil {
			return nil, portError(err)
		}
		v.ID, err = f.ParseID[c.Participant](raw)
		if err != nil {
			return nil, fault(f.InternalError)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, portError(err)
	}
	return out, nil
}

func (s *Service) GetSession(ctx context.Context, a i.Actor, p i.ProjectID, id c.MeetingID) (c.Session, error) {
	if s == nil || ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return c.Session{}, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Session{}, err
	}
	locks, err := normalize([]f.LockRequest{userLock(a), projectLock(p, f.Shared), meetingLock(id, f.Shared)})
	if err != nil {
		return c.Session{}, err
	}
	cause, err := readCause("meeting.read")
	if err != nil {
		return c.Session{}, err
	}
	var out c.Session
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
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
		out, err = loadSession(ctx, x, p, id)
		return err
	})
	if err = commitError(result); err != nil {
		return c.Session{}, err
	}
	return out, nil
}
