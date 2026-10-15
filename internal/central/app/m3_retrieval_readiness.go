package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
)

// This uses the same database that will serve both domain backends. Selecting
// an explicitly installed provider never installs extensions or indexes during
// ordinary application startup, or silently changes the deployment's backend.
func requireRetrievalProvider(ctx context.Context, cfg config.Config, db database) error {
	options, enabled := cfg.RetrievalRuntime()
	if !enabled || options.Backend() != lexical.PGSearch {
		return nil
	}
	return requirePGSearchProvider(ctx, db)
}

func requirePGSearchProvider(ctx context.Context, db database) error {
	query, ok := db.(interface {
		QueryRow(context.Context, string, ...any) postgres.Row
	})
	if !ok {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var ready bool
	if err := query.QueryRow(ctx, lexical.PGSearchReadinessSQL).Scan(&ready); err != nil {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
	}
	if !ready {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return nil
}
