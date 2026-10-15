package app

import (
	"context"
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
)

type retrievalReadinessDB struct {
	database
	ready   bool
	err     error
	queries int
	ctx     context.Context
}
type retrievalReadinessRow struct {
	ready bool
	err   error
}

func (r retrievalReadinessRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.ready
	return nil
}
func (d *retrievalReadinessDB) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	d.queries++
	d.ctx = ctx
	if query != lexical.PGSearchReadinessSQL || len(args) != 0 {
		return retrievalReadinessRow{err: errors.New("unexpected readiness query")}
	}
	return retrievalReadinessRow{d.ready, d.err}
}
func TestM3RetrievalProviderReadinessUsesOriginalDatabase(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "original")
	for _, ready := range []bool{false, true} {
		db := &retrievalReadinessDB{ready: ready}
		err := requirePGSearchProvider(ctx, db)
		if (err == nil) != ready || db.queries != 1 || db.ctx != ctx {
			t.Fatal("readiness did not use original database/context or admitted missing provider")
		}
	}
	db := &retrievalReadinessDB{ready: true, err: context.Canceled}
	err := requirePGSearchProvider(ctx, db)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnavailable || !errors.Is(err, context.Canceled) {
		t.Fatal("SQL failure reported provider ready or lost original cancellation")
	}
	if err = requireRetrievalProvider(ctx, config.Config{}, nil); err != nil {
		t.Fatal("disabled retrieval required an extension")
	}
}
