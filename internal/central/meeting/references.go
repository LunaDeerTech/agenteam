package meeting

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) LookupUpdateReferences(ctx context.Context, actor i.Actor, request c.UpdateReferencesRequest) (c.Receipt, bool, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, false, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.Receipt{}, false, err
	}
	r, err := request.Canonical()
	if err != nil {
		return c.Receipt{}, false, err
	}
	d, err := commandDigest(actor, "references", r)
	if err != nil {
		return c.Receipt{}, false, err
	}
	return s.lookup(ctx, actor, r.ProjectID, r.Meta.IdempotencyKey, d)
}

func (s *Service) UpdateReferences(ctx context.Context, actor i.Actor, request c.UpdateReferencesRequest) (c.Receipt, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.Receipt{}, err
	}
	r, err := request.Canonical()
	if err != nil {
		return c.Receipt{}, err
	}
	d, err := commandDigest(actor, "references", r)
	if err != nil {
		return c.Receipt{}, err
	}
	if old, found, err := s.lookup(ctx, actor, r.ProjectID, r.Meta.IdempotencyKey, d); err != nil || found {
		return old, err
	}
	// This observation only discovers locks. The command version and previous
	// identities are checked again under the complete transaction lock union.
	session, err := s.GetSession(ctx, actor, r.ProjectID, r.MeetingID)
	if err != nil {
		return c.Receipt{}, err
	}
	if session.Version != r.ExpectedVersion {
		return c.Receipt{}, fault(f.VersionConflict)
	}
	previous, err := loadPinnedReferences(ctx, s.store, r.MeetingID)
	if err != nil {
		return c.Receipt{}, err
	}
	added := newResourceReferences(previous, r.References)
	var locks []f.LockRequest
	if len(added) > 0 {
		if nilPort(s.deps.References) {
			return c.Receipt{}, fault(f.DependencyUnbound)
		}
		locks, err = s.deps.References.ReferenceLocks(ctx, actor, r.ProjectID, added)
		if err != nil {
			return c.Receipt{}, portError(err)
		}
	}
	return s.editMeeting(ctx, actor, r.ProjectID, r.MeetingID, r.ExpectedVersion, r.Meta.IdempotencyKey, "references", d, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		current, err := loadPinnedReferences(ctx, x, r.MeetingID)
		if err != nil {
			return err
		}
		actual := newResourceReferences(current, r.References)
		if !sameReferenceIdentities(actual, added) {
			return fault(f.ConfirmationStale)
		}
		if len(actual) > 0 {
			if err = s.deps.References.RequireReferencesInTx(ctx, tx, actor, r.ProjectID, actual); err != nil {
				return portError(err)
			}
		}
		// Existing unavailable identities can still be unpinned or reordered;
		// retaining one does not grant permission to read its external content.
		if _, err = x.Exec(ctx, `DELETE FROM agenteam_meeting.meeting_references WHERE meeting_id=$1`, r.MeetingID.String()); err != nil {
			return portError(err)
		}
		for n, ref := range r.References {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.meeting_references(meeting_id,kind,stable_id,label,sort_order) VALUES($1,$2,$3,$4,$5)`, r.MeetingID.String(), ref.Kind, ref.ID, ref.Label, n); err != nil {
				return portError(err)
			}
		}
		return nil
	})
}

func newResourceReferences(previous, wanted []c.Reference) []c.Reference {
	known := map[string]bool{}
	for _, ref := range previous {
		known[ref.Kind+":"+ref.ID] = true
	}
	out := []c.Reference{}
	for _, ref := range wanted {
		if ref.Kind != "link" && !known[ref.Kind+":"+ref.ID] {
			out = append(out, ref)
		}
	}
	return out
}
func sameReferenceIdentities(a, b []c.Reference) bool {
	if len(a) != len(b) {
		return false
	}
	for n := range a {
		if a[n].Kind != b[n].Kind || a[n].ID != b[n].ID {
			return false
		}
	}
	return true
}
func loadPinnedReferences(ctx context.Context, x postgres.SQLExecutor, meeting c.MeetingID) ([]c.Reference, error) {
	rows, err := x.Query(ctx, `SELECT kind,stable_id,label FROM agenteam_meeting.meeting_references WHERE meeting_id=$1 ORDER BY sort_order,kind,stable_id`, meeting.String())
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	out := []c.Reference{}
	for rows.Next() {
		var ref c.Reference
		if err = rows.Scan(&ref.Kind, &ref.ID, &ref.Label); err != nil {
			return nil, portError(err)
		}
		out = append(out, ref)
		if len(out) > c.MaxReferences {
			return nil, fault(f.PayloadTooLarge)
		}
	}
	return out, portError(rows.Err())
}
