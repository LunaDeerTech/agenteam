// Independent offline review bridge. Overlay only; no database connection.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func ReviewB02Rows(raw pgx.Rows) *Rows {
	return &Rows{raw: raw, ctx: context.Background(), cancel: func() {}, release: func() {}}
}
