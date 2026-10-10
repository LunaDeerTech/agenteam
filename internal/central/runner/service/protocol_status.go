package service

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// RejectIncompatible records a protocol-major rejection only for this Service's
// still-current authenticated reservation. Ordinary transport/authentication
// failures never call it. A subsequent accepted hello clears the marker.
func (s *Service) RejectIncompatible(ctx context.Context, connection Connection) error {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	return s.withConnection(ctx, connection, true, false, true, func(ctx context.Context, x postgres.SQLExecutor, r connectionReservation) error {
		tag, e := x.Exec(ctx, `UPDATE agenteam_runner.runners SET incompatible=true WHERE id=$1`, r.runner.String())
		return affected(tag, e)
	})
}
