package meeting

import (
	"context"
	"math"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) LookupUpdateParticipants(ctx context.Context, actor i.Actor, r c.UpdateParticipantsRequest) (c.Receipt, bool, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, false, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.Receipt{}, false, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, false, err
	}
	d, err := commandDigest(actor, "participants", r)
	if err != nil {
		return c.Receipt{}, false, err
	}
	return s.lookup(ctx, actor, r.ProjectID, r.Meta.IdempotencyKey, d)
}

func (s *Service) UpdateParticipants(ctx context.Context, actor i.Actor, r c.UpdateParticipantsRequest) (c.Receipt, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.Receipt{}, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, err
	}
	d, err := commandDigest(actor, "participants", r)
	if err != nil {
		return c.Receipt{}, err
	}
	if old, found, err := s.lookup(ctx, actor, r.ProjectID, r.Meta.IdempotencyKey, d); err != nil || found {
		return old, err
	}
	if _, err = s.GetSession(ctx, actor, r.ProjectID, r.MeetingID); err != nil {
		return c.Receipt{}, err
	}
	entries := make([]ac.DirectoryEntry, len(r.Agents))
	locks := make([]f.LockRequest, 0, len(r.Agents)+1)
	schedule, _ := f.ProjectScheduleLock(r.ProjectID.String())
	locks = append(locks, f.LockRequest{Key: schedule, Mode: f.Exclusive})
	for n, agent := range r.Agents {
		entries[n], err = s.deps.Directory.GetAgent(ctx, actor, r.ProjectID, agent)
		if err != nil {
			return c.Receipt{}, portError(err)
		}
		locks = append(locks, agentLock(agent))
	}
	return s.editMeeting(ctx, actor, r.ProjectID, r.MeetingID, r.ExpectedVersion, r.Meta.IdempotencyKey, "participants", d, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		for n, agent := range r.Agents {
			current, err := s.deps.Agents.RequireCurrentInTx(ctx, tx, actor, r.ProjectID, agent)
			if err != nil {
				return portError(err)
			}
			if current.ConfigVersion != entries[n].Version {
				return fault(f.VersionConflict)
			}
		}
		previous, err := loadParticipants(ctx, x, r.MeetingID)
		if err != nil {
			return err
		}
		active := map[string]c.ParticipantView{}
		wanted := map[string]bool{}
		for _, agent := range r.Agents {
			wanted[agent.String()] = true
		}
		for _, p := range previous {
			if p.Kind != "agent" || !p.Active {
				continue
			}
			active[p.SourceID] = p
			if !wanted[p.SourceID] {
				if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.participants SET active=false WHERE id=$1 AND meeting_id=$2 AND active`, p.ID.String(), r.MeetingID.String()); err != nil {
					return portError(err)
				}
			}
		}
		for n, entry := range entries {
			if p, exists := active[entry.ID.String()]; exists {
				// Snapshot identity remains historical; reorder never changes an
				// already-created Turn or its contribution order.
				if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.participants SET sort_order=$2 WHERE id=$1 AND meeting_id=$3 AND active`, p.ID.String(), n+1, r.MeetingID.String()); err != nil {
					return portError(err)
				}
				continue
			}
			id, err := f.NewID[c.Participant]()
			if err != nil {
				return portError(err)
			}
			name := entry.Name
			if entry.DisplayName != nil {
				name = *entry.DisplayName
			}
			if err = insertParticipant(ctx, x, c.ParticipantView{ID: id, MeetingID: r.MeetingID, Kind: "agent", SourceID: entry.ID.String(), Name: name, Description: entry.Description, Order: n + 1, Active: true}, now()); err != nil {
				return err
			}
		}
		return nil
	})
}

// editMeeting joins metadata changes to the existing immutable receipt.
// The complete resource lock union is discovered before this transaction.
func (s *Service) editMeeting(ctx context.Context, actor i.Actor, project i.ProjectID, meeting c.MeetingID, version f.Version, key f.IdempotencyKey, kind string, digest f.Digest, more []f.LockRequest, edit func(context.Context, f.Tx, postgres.SQLExecutor) error) (c.Receipt, error) {
	command, err := commandIdentity(project, key)
	if err != nil {
		return c.Receipt{}, err
	}
	locks, err := normalize(append([]f.LockRequest{userLock(actor), projectLock(project, f.Shared), meetingLock(meeting, f.Exclusive), commandLock(command)}, more...))
	if err != nil {
		return c.Receipt{}, err
	}
	cause, _ := f.NewCommandsCause(command)
	var out c.Receipt
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, actor, project, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if old, found, err := lookupCommand(ctx, x, project, actor, key, digest); err != nil || found {
			out = old
			return err
		}
		if err = s.owner(ctx, tx, actor, project, i.Mutate); err != nil {
			return err
		}
		session, err := loadSession(ctx, x, project, meeting)
		if err != nil {
			return err
		}
		if session.Version != version {
			return fault(f.VersionConflict)
		}
		if session.Status != "active" || session.Deleting || version == math.MaxInt64 {
			return fault(f.InvalidState)
		}
		if err = edit(ctx, tx, x); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET version=version+1,updated_at=$2 WHERE id=$1`, meeting.String(), now()); err != nil {
			return portError(err)
		}
		out = c.Receipt{MeetingID: meeting, Version: version + 1}
		return saveCommand(ctx, x, project, actor, key, kind, digest, out)
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, err
	}
	s.invalidateMeeting(project, meeting)
	return out, nil
}
