package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Test-only overlay: retains the production Rows methods. The raw pgx.Rows,
// release callback and Store are controlled; no physical PG lease is implied.
func TimelineReviewRows(ctx context.Context, raw pgx.Rows, release func()) *Rows {
	return &Rows{raw: raw, ctx: ctx, cancel: func() {}, release: release}
}
