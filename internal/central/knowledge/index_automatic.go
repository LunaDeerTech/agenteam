package knowledge

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/jackc/pgx/v5"
)

// A round-robin project observation cannot starve a later project's first
// document behind an already current profile. Observations never create leases.
func (s *IndexService) ensureAutomaticProfile(ctx context.Context) (processed bool, resultErr error) {
	s.mu.Lock()
	cursor := s.profileCursor
	s.mu.Unlock()
	var key string
	err := s.store.QueryRow(ctx, `SELECT p.id::text FROM agenteam_project.projects p
 WHERE p.lifecycle='active' AND p.initialized_at IS NOT NULL
 AND EXISTS(SELECT 1 FROM agenteam_knowledge.documents d WHERE d.project_id=p.id AND d.status='active')
 ORDER BY (p.id::text <= $1),p.id LIMIT 1`, cursor).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, unavailable(err)
	}
	project, err := f.ParseID[id.Project](key)
	if err != nil {
		return false, internal(err)
	}
	s.mu.Lock()
	s.profileCursor = key
	s.mu.Unlock()
	ctx, b, err := s.newBuild(ctx, id.Actor{}, project)
	if err != nil {
		return false, err
	}
	ctx, entry, err := s.documents.beginProjectCall(ctx, b.owner, project, nil, projectCallRead)
	defer s.documents.projectCallDone(entry, &resultErr)
	if err != nil {
		return false, err
	}
	b.profile, err = b.readProfile(ctx)
	if err != nil {
		// A project may receive its first document before platform models are
		// configured. A later observation reads the actual new configuration.
		var known *f.Fault
		if errors.As(err, &known) && known.CommitState != f.Unknown && (known.Code == f.InvalidState || known.Code == f.NotFound || known.Code == f.ResourceBusy) {
			return false, nil
		}
		return false, err
	}
	needed := false
	err = s.ownerIndexTx(ctx, b.owner, project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var building bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.target_profile_id WHERE st.project_id=$1 AND p.state='building')`, key).Scan(&building); err != nil {
			return unavailable(err)
		}
		if building {
			return nil
		}
		digest, err := b.profile.Digest()
		if err != nil {
			return err
		}
		source, err := indexSourceDigest(ctx, x, project)
		if err != nil {
			return err
		}
		cfg := s.deps.Settings
		var currentOrFailed bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles p
 LEFT JOIN agenteam_knowledge.index_state st ON st.project_id=p.project_id
 WHERE p.project_id=$1 AND p.profile_digest=$2 AND p.parser_profile=$3 AND p.chunker_profile=$4
 AND p.soft_chunk_bytes=$5 AND p.hard_chunk_bytes=$6 AND p.forced_overlap_bytes=$7 AND p.dimension=$8 AND p.lexical_backend=$9
 AND ((p.state='active' AND p.retention_state='retained' AND st.serving_profile_id=p.id)
 OR (p.state='failed' AND p.source_digest=$10)))`, key, string(digest), cfg.ParserProfile, chunker.Profile, cfg.Chunker.SoftBytes, cfg.Chunker.HardBytes, cfg.Chunker.ForcedOverlapBytes, b.profile.Dimensions, string(cfg.Backend), string(source)).Scan(&currentOrFailed)
		if err != nil {
			return unavailable(err)
		}
		needed = !currentOrFailed
		return nil
	})
	if err != nil || !needed {
		return false, err
	}
	_, err = s.rebuild(ctx, b)
	if err == nil {
		return true, nil
	}
	if indexRetentionUnknown(err) {
		return true, err
	}
	// Only confirmed terminal state and returned original owners allow this
	// failed build to remain an inspectable business failure while Run continues.
	s.mu.Lock()
	joined := true
	for g := range s.retentions {
		if g.target.Profile.ID == b.id {
			joined = false
		}
	}
	for op := range s.owners {
		if op.profile.ID == b.id {
			joined = false
		}
	}
	s.mu.Unlock()
	if !joined {
		return true, err
	}
	failed := false
	observation, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.deps.Settings.ModelTimeout)
	defer cancel()
	observed := s.indexTx(observation, project, false, nil, true, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		return portError(x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2 AND state='failed' AND retention_state IN ('unretained','released'))`, key, b.id).Scan(&failed))
	})
	if observed == nil && failed {
		return true, indexJobFailure{err}
	}
	return true, err
}
