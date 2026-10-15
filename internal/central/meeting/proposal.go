package meeting

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type ProposalService struct {
	store  Store
	owner  *Service
	events *ProposalEvents
}

func NewProposalService(store Store, owner *Service, events *ProposalEvents) (*ProposalService, error) {
	if nilPort(store) || owner == nil || !sameStore(store, owner.store) || events == nil || !sameStore(store, events.authority.store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProposalService{store, owner, events}, nil
}
func loadProposal(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, m c.MeetingID) (c.ProposalView, error) {
	session, err := loadSession(ctx, x, p, m)
	if err != nil {
		return c.ProposalView{}, err
	}
	var execution, participant string
	var kind, id *string
	err = x.QueryRow(ctx, `SELECT execution_id,created_by_participant_id,origin_kind,origin_id FROM agenteam_meeting.proposal_sources WHERE meeting_id=$1`, m.String()).Scan(&execution, &participant, &kind, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.ProposalView{}, fault(f.NotFound)
	}
	if err != nil {
		return c.ProposalView{}, portError(err)
	}
	e, err := f.ParseID[i.Execution](execution)
	if err != nil {
		return c.ProposalView{}, fault(f.InternalError)
	}
	a, err := f.ParseID[c.Participant](participant)
	if err != nil {
		return c.ProposalView{}, fault(f.InternalError)
	}
	out := c.ProposalView{Session: session, SourceExecutionID: e, CreatedByParticipantID: a}
	if kind != nil && id != nil {
		out.Origin = &c.ProposalOrigin{Kind: *kind, ID: *id}
	}
	return out, nil
}
func (s *ProposalService) GetProposal(ctx context.Context, a i.Actor, p i.ProjectID, m c.MeetingID) (c.ProposalView, error) {
	if s == nil || ctx == nil || p.Validate() != nil || m.Validate() != nil {
		return c.ProposalView{}, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.ProposalView{}, err
	}
	locks, err := normalize([]f.LockRequest{userLock(a), projectLock(p, f.Shared), meetingLock(m, f.Shared)})
	if err != nil {
		return c.ProposalView{}, err
	}
	cause, err := readCause("meeting.proposal.read")
	if err != nil {
		return c.ProposalView{}, err
	}
	var out c.ProposalView
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner.owner(ctx, tx, a, p, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		out, err = loadProposal(ctx, x, p, m)
		return err
	})
	if err = commitError(result); err != nil {
		return c.ProposalView{}, err
	}
	return out, nil
}
func (s *ProposalService) LookupReview(ctx context.Context, a i.Actor, r c.ReviewProposalRequest) (c.Receipt, bool, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, false, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Receipt{}, false, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, false, err
	}
	d, err := commandDigest(a, "review-proposal", r)
	if err != nil {
		return c.Receipt{}, false, err
	}
	return s.owner.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d)
}
func (s *ProposalService) Review(ctx context.Context, a i.Actor, r c.ReviewProposalRequest) (c.Receipt, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Receipt{}, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, err
	}
	d, err := commandDigest(a, "review-proposal", r)
	if err != nil {
		return c.Receipt{}, err
	}
	command, err := commandIdentity(r.ProjectID, r.Meta.IdempotencyKey)
	if err != nil {
		return c.Receipt{}, err
	}
	if old, found, err := s.owner.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d); err != nil || found {
		return old, err
	}
	locks, err := normalize([]f.LockRequest{userLock(a), projectLock(r.ProjectID, f.Shared), meetingLock(r.MeetingID, f.Exclusive), commandLock(command)})
	if err != nil {
		return c.Receipt{}, err
	}
	status, name := "archive", c.ArchivedName
	if r.Approve {
		status, name = "active", c.ActivatedName
	}
	at := now()
	version := r.ExpectedVersion + 1
	if version.Validate() != nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	ev, err := s.events.prepare(ctx, a, r.ProjectID, r.MeetingID, version, name, status, at, locks)
	if err != nil {
		return c.Receipt{}, err
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return c.Receipt{}, err
	}
	var out c.Receipt
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, ev.locks); err != nil {
			return portError(err)
		}
		if err := s.owner.owner(ctx, tx, a, r.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if old, found, err := lookupCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, d); err != nil || found {
			out = old
			return err
		}
		if err = s.owner.owner(ctx, tx, a, r.ProjectID, i.Mutate); err != nil {
			return err
		}
		proposal, err := loadProposal(ctx, x, r.ProjectID, r.MeetingID)
		if err != nil {
			return err
		}
		if proposal.Session.Version != r.ExpectedVersion {
			return fault(f.ConfirmationStale)
		}
		if proposal.Session.Deleting || proposal.Session.Status != "proposed" {
			return fault(f.InvalidState)
		}
		// Either explicit review records the current Human participant, so an
		// archived proposal can later be restored and sent to normally. No Turn,
		// contribution, input or message is created by reviewing a proposal.
		{
			var users, owned int
			if err = x.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE source_id=$2) FROM agenteam_meeting.participants WHERE meeting_id=$1 AND kind='user' AND active`, r.MeetingID.String(), a.Details().UserID).Scan(&users, &owned); err != nil {
				return portError(err)
			}
			if users > 0 && (users != 1 || owned != 1) {
				return fault(f.Forbidden)
			}
			if users == 0 {
				id, err := f.NewID[c.Participant]()
				if err != nil {
					return portError(err)
				}
				if err = insertParticipant(ctx, x, c.ParticipantView{ID: id, MeetingID: r.MeetingID, Kind: "user", SourceID: a.Details().UserID, Name: "User", Order: 0, Active: true}, at); err != nil {
					return err
				}
			}
		}

		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET status=$2,version=$3,updated_at=$4 WHERE id=$1`, r.MeetingID.String(), status, int64(version), at); err != nil {
			return portError(err)
		}
		out = c.Receipt{MeetingID: r.MeetingID, Version: version}
		if err = saveCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, "review-proposal", d, out); err != nil {
			return err
		}
		return s.events.appendInTx(ctx, tx, ev)
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, err
	}
	s.owner.invalidateMeeting(r.ProjectID, out.MeetingID)
	return out, nil
}
