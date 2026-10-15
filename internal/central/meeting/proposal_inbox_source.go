package meeting

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	hc "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"time"
)

type ProposalInboxSource struct {
	store     Store
	authority hc.SourceReadAuthority
}

func NewProposalInboxSource(store Store, a hc.SourceReadAuthority) (*ProposalInboxSource, error) {
	if nilPort(store) || nilPort(a) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProposalInboxSource{store, a}, nil
}
func proposalInboxSource(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, id string) (hc.Source, error) {
	out := hc.Source{ProjectID: p, ID: id, Title: "Meeting proposal"}
	var version int64
	var status string
	var deleting bool
	err := x.QueryRow(ctx, `SELECT s.version,s.status,s.deleting,COALESCE(s.title,'Meeting proposal'),COALESCE(s.proposal_content,''),s.updated_at FROM agenteam_meeting.sessions s JOIN agenteam_meeting.proposal_sources p ON p.meeting_id=s.id WHERE s.project_id=$1 AND s.id=$2`, p.String(), id).Scan(&version, &status, &deleting, &out.Title, &out.Summary, &out.OccurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deletion retains the canonical lifecycle fact. A late Proposed event can
		// only resolve; it cannot reconstruct a deleted source from old payload.
		err = x.QueryRow(ctx, `SELECT version,occurred_at,EXISTS(SELECT 1 FROM agenteam_meeting.lifecycle_events WHERE project_id=$1 AND meeting_id=$2 AND event_type='meeting.proposed') FROM agenteam_meeting.lifecycle_events WHERE project_id=$1 AND meeting_id=$2 ORDER BY version DESC LIMIT 1`, p.String(), id).Scan(&version, &out.OccurredAt, &out.Relevant)
		if errors.Is(err, pgx.ErrNoRows) {
			return hc.Source{}, fault(f.NotFound)
		}
		if err != nil {
			return hc.Source{}, portError(err)
		}
		out.Version = f.Version(version)
		return out, nil
	}
	if err != nil {
		return hc.Source{}, portError(err)
	}
	out.Version = f.Version(version)
	out.Relevant = true
	out.Open = status == "proposed" && !deleting
	return out, nil
}
func (s *ProposalInboxSource) ReadSourceInTx(ctx context.Context, tx f.Tx, p i.ProjectID, id string) (hc.Source, error) {
	if s == nil || ctx == nil {
		return hc.Source{}, fault(f.DependencyUnbound)
	}
	if err := s.authority.RequireSourceReadInTx(ctx, tx, s.store, p, id, false); err != nil {
		return hc.Source{}, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return hc.Source{}, portError(err)
	}
	return proposalInboxSource(ctx, x, p, id)
}
func (s *ProposalInboxSource) ScanSourcesInTx(ctx context.Context, tx f.Tx, p i.ProjectID) ([]hc.Source, error) {
	if s == nil || ctx == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if err := s.authority.RequireSourceReadInTx(ctx, tx, s.store, p, "", true); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	rows, err := x.Query(ctx, `SELECT s.id,s.version,s.status,s.deleting,COALESCE(s.title,'Meeting proposal'),COALESCE(s.proposal_content,''),s.updated_at FROM agenteam_meeting.sessions s JOIN agenteam_meeting.proposal_sources p ON p.meeting_id=s.id WHERE s.project_id=$1 ORDER BY s.id`, p.String())
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	out := []hc.Source{}
	for rows.Next() {
		var v hc.Source
		var version int64
		var status string
		var deleting bool
		v.ProjectID = p
		if err = rows.Scan(&v.ID, &version, &status, &deleting, &v.Title, &v.Summary, &v.OccurredAt); err != nil {
			return nil, portError(err)
		}
		v.Version = f.Version(version)
		v.Relevant = true
		v.Open = status == "proposed" && !deleting
		if v.OccurredAt.IsZero() || v.OccurredAt.After(time.Now().Add(time.Minute)) {
			return nil, fault(f.InvalidState)
		}
		out = append(out, v)
	}
	return out, portError(rows.Err())
}

var _ hc.SourceProvider = (*ProposalInboxSource)(nil)
