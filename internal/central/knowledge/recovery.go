package knowledge

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/jackc/pgx/v5"
)

const cleanupBatchSize = 32

// This cursor is scheduling state, never a durable cleanup/join proof. A fixed
// upper ID lets a pass revisit old pending items despite newly arriving work.
// Restarting merely starts another pass over the same durable exact causes.
type cleanupCursor struct{ after, through oc.CleanupID }

// RecoverCleanup attempts at most 32 already-persisted exact causes. Ordinary
// pending work does not prevent other items in that batch from progressing.
// ResourceBusy also reports a remaining batch; callers retain their own budget.
// Unknown/hard failures return unchanged, leaving that item to retry next time.
func (s *Service) RecoverCleanup(ctx context.Context) error {
	if s.state() != nil && s.state().ownership != nil {
		return fault(f.DependencyUnbound)
	}
	return s.recoverCleanupBatch(ctx, nil)
}

// Owned recovery knows its Project before the first scheduling query. A global
// scan cannot discover ownership after SQL has already started.
func (s *Service) RecoverProjectCleanup(ctx context.Context, project id.ProjectID) (resultErr error) {
	if project.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	ctx, entry, err := s.beginProjectCall(ctx, id.Actor{}, project, nil, projectCallRecovery)
	defer s.projectCallDone(entry, &resultErr)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, cleanupProjectContextKey{}, project)
	return s.recoverCleanupBatch(ctx, &project)
}

type cleanupProjectContextKey struct{}

func (s *Service) recoverCleanupBatch(ctx context.Context, project *id.ProjectID) error {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	st := s.state()
	var cursor cleanupCursor
	if owned := s.knowledgeSourceState(); project != nil && owned != nil {
		owned.mu.Lock()
		if owned.cleanupScanning[*project] {
			owned.mu.Unlock()
			return fault(f.ResourceBusy)
		}
		if owned.cleanupScanning == nil {
			owned.cleanupScanning = map[id.ProjectID]bool{}
			owned.cleanupCursors = map[id.ProjectID]cleanupCursor{}
		}
		owned.cleanupScanning[*project] = true
		cursor = owned.cleanupCursors[*project]
		owned.mu.Unlock()
		defer func() {
			owned.mu.Lock()
			owned.cleanupCursors[*project] = cursor
			delete(owned.cleanupScanning, *project)
			owned.mu.Unlock()
		}()
	} else {
		st.mu.Lock()
		if st.cleanupScanning {
			st.mu.Unlock()
			return fault(f.ResourceBusy)
		}
		st.cleanupScanning = true
		cursor = st.cleanupCursor
		st.mu.Unlock()
		defer func() { st.mu.Lock(); st.cleanupCursor = cursor; st.cleanupScanning = false; st.mu.Unlock() }()
	}
	if cursor.through == (oc.CleanupID{}) {
		var raw string
		query := `SELECT id::text FROM agenteam_knowledge.object_cleanup WHERE phase<>'completed'`
		var args []any
		if project != nil {
			query += ` AND project_id=$1::uuid`
			args = append(args, project.String())
		}
		err = st.store.QueryRow(ctx, query+` ORDER BY id DESC LIMIT 1`, args...).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			cursor = cleanupCursor{}
			return nil
		}
		if err != nil {
			return unavailable(err)
		}
		if cursor.through, err = f.ParseID[oc.CleanupOperation](raw); err != nil {
			return internal(err)
		}
	}
	var after any
	if cursor.after != (oc.CleanupID{}) {
		after = cursor.after.String()
	}
	// The aggregate contains at most 33 IDs; QueryRow closes its SQL rows before
	// any external Cleaner call. The extra ID distinguishes a bounded next batch.
	var raw []string
	query := `SELECT coalesce(array_agg(id::text ORDER BY id), ARRAY[]::text[]) FROM (
 SELECT id FROM agenteam_knowledge.object_cleanup WHERE phase<>'completed'
 AND ($1::uuid IS NULL OR id>$1::uuid) AND id<=$2::uuid`
	args := []any{after, cursor.through.String()}
	if project != nil {
		query += ` AND project_id=$3::uuid`
		args = append(args, project.String())
	}
	err = st.store.QueryRow(ctx, query+` ORDER BY id LIMIT 33) AS batch`, args...).Scan(&raw)
	if err != nil {
		return unavailable(err)
	}
	if len(raw) > cleanupBatchSize+1 {
		return internal(nil)
	}
	keys := make([]oc.CleanupID, len(raw))
	previous := cursor.after.String()
	for n, value := range raw {
		key, err := f.ParseID[oc.CleanupOperation](value)
		if err != nil || value <= previous || value > cursor.through.String() {
			return internal(err)
		}
		keys[n], previous = key, value
	}
	var pending error
	for _, key := range keys[:min(len(keys), cleanupBatchSize)] {
		if err = ctx.Err(); err != nil {
			return unavailable(err)
		}
		if err = s.recoverCleanup(ctx, key); err != nil {
			var known *f.Fault
			if !errors.As(err, &known) || known.Code != f.ResourceBusy || known.CommitState == f.Unknown {
				return err
			}
			if pending == nil {
				pending = err
			}
		}
		cursor.after = key
	}
	if len(keys) > cleanupBatchSize {
		if pending != nil {
			return pending
		}
		return fault(f.ResourceBusy)
	}
	// End this finite pass even if its oldest items remain pending. A new pass
	// gets a fresh upper bound; no new arrival can extend the current pass forever.
	cursor = cleanupCursor{}
	if pending != nil {
		return pending
	}
	var remaining bool
	query = `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.object_cleanup WHERE phase<>'completed'`
	args = nil
	if project != nil {
		query += ` AND project_id=$1::uuid`
		args = append(args, project.String())
	}
	if err = st.store.QueryRow(ctx, query+`)`, args...).Scan(&remaining); err != nil {
		return unavailable(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return nil
}
