package knowledge

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// RecoverCleanup advances only already-persisted exact Knowledge cleanup
// causes. It does not create a user command, authorize a whole Project stop,
// or claim that pending Object readers have joined. The caller owns the budget.
func (s *Service) RecoverCleanup(ctx context.Context) error {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	st := s.state()
	var after any
	for {
		rows, err := st.store.Query(ctx, `SELECT id::text FROM agenteam_knowledge.object_cleanup
 WHERE phase<>'completed' AND ($1::uuid IS NULL OR id>$1::uuid) ORDER BY id LIMIT 32`, after)
		if err != nil {
			return unavailable(err)
		}
		keys := make([]oc.CleanupID, 0, 32)
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return unavailable(err)
			}
			key, err := f.ParseID[oc.CleanupOperation](raw)
			if err != nil {
				rows.Close()
				return internal(err)
			}
			keys = append(keys, key)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable(err)
		}
		if len(keys) == 0 {
			return nil
		}
		for _, key := range keys {
			if err = ctx.Err(); err != nil {
				return unavailable(err)
			}
			if err = s.recoverCleanup(ctx, key); err != nil {
				return err
			}
			after = key.String()
		}
	}
}
