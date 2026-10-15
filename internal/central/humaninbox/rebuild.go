package humaninbox

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Rebuild isolates this project's source writes and projector transactions with
// Project EX. Each configured source resolves only its own pair; an absent
// provider cannot resolve another source. Late events reread canonical facts.
func (s *Service) Rebuild(ctx context.Context, a i.Actor, p i.ProjectID) error {
	if s == nil || ctx == nil || p.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if a.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	if a.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	locks, err := oc.NormalizeLocks(ownerLocks(a, p, f.Exclusive))
	if err != nil {
		return err
	}
	cause, err := readCause()
	if err != nil {
		return err
	}
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.authority.owner(ctx, tx, a, p); err != nil {
			return err
		}
		var sources []c.Source
		if err := s.authority.withSource(ctx, tx, p, "", true, func(ctx context.Context) error {
			var err error
			sources, err = s.sources.ScanSourcesInTx(ctx, tx, p)
			return err
		}); err != nil {
			return err
		}
		x, err := s.authority.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = rebuildProjection(ctx, x, p, sources, proposalProjection); err != nil {
			return err
		}
		if !nilPort(s.decisions) {
			if err = s.authority.withDecisionSource(ctx, tx, p, "", "", true, func(ctx context.Context) error {
				var err error
				sources, err = s.decisions.ScanDecisionSourcesInTx(ctx, tx, p)
				return err
			}); err != nil {
				return err
			}
			if err = rebuildProjection(ctx, x, p, sources, decisionProjection); err != nil {
				return err
			}
		}
		return nil
	})
	return commitError(result)
}

// All scans and reconciliation share the Owner's Project-exclusive transaction.
// The source pair is fixed by the registered provider, never inferred from IDs.
func rebuildProjection(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, sources []c.Source, kind projectionKind) error {
	ids := []string{}
	for _, source := range sources {
		if source.ProjectID != p || !kind.validContext(source.Context) {
			return fault(f.Forbidden)
		}
		if !source.Relevant {
			continue
		}
		if err := upsertProjection(ctx, x, source, kind); err != nil {
			return err
		}
		ids = append(ids, source.ID)
	}
	sourceType, actionType := kind.pair()
	if sourceType == "" {
		return fault(f.InvalidArgument)
	}
	_, err := x.Exec(ctx, `UPDATE agenteam_humaninbox.items SET status='resolved',resolved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE project_id=$1 AND source_type=$2 AND action_type=$3 AND status='open' AND NOT(source_id::text=ANY($4::text[]))`, p.String(), sourceType, actionType, ids)
	return portError(err)
}
