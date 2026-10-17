package knowledge

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
	"github.com/jackc/pgx/v5"
)

type indexAdmissionKey struct{}
type indexWorkKey struct{}
type indexJobFailure struct{ cause error }

func (indexJobFailure) Error() string   { return "knowledge_index_job_failed" }
func (e indexJobFailure) Unwrap() error { return e.cause }

type indexWork struct {
	service   *IndexService
	actor     id.Actor
	profile   indexProfile
	job       indexJob
	admission *indexAdmission
	build     *indexBuild
}

func currentIndexWork(ctx context.Context, actor id.Actor, project id.ProjectID) (*indexWork, error) {
	if ctx == nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(indexWorkKey{}).(*indexWork)
	if w == nil || w.service == nil || w.admission == nil || !w.actor.Equal(actor) || w.profile.Project != project || actor.Details().Kind != id.Service || actor.Details().ServiceName != id.KnowledgeRuntime || actor.Details().CauseRef != w.job.id {
		return nil, fault(f.Forbidden)
	}
	w.service.mu.Lock()
	_, active := w.service.calls[w.admission]
	w.service.mu.Unlock()
	if !active {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (w *indexWork) authorizeInTx(ctx context.Context, tx f.Tx, store Store) error {
	if w == nil || !sameStore(store, w.service.store) {
		return fault(f.Forbidden)
	}
	if current, err := currentIndexWork(ctx, w.actor, w.profile.Project); err != nil || current != w {
		return fault(f.Forbidden)
	}
	locks, err := indexLocks(w.profile.Project, false)
	if err != nil {
		return err
	}
	if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err = w.service.authority.guard.RequireCurrentProcessInTx(ctx, tx, w.service.authority.process); err != nil {
		return portError(err)
	}
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = w.service.requireIndexProject(ctx, x, w.profile.Project); err != nil {
		return err
	}
	if w.build != nil {
		if w.build.service != w.service || w.build.id != w.profile.ID || w.build.admission != w.admission {
			return fault(f.Forbidden)
		}
		if err = w.build.authorizeInTx(ctx, tx); err != nil {
			return err
		}
	}
	var eligible bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_jobs j
 JOIN agenteam_knowledge.documents d ON d.project_id=j.project_id AND d.id=j.document_id
 JOIN agenteam_knowledge.index_profiles p ON p.project_id=j.project_id AND p.id=j.profile_id
 JOIN agenteam_knowledge.index_state st ON st.project_id=p.project_id
 WHERE j.project_id=$1 AND j.id=$2 AND j.document_id=$3 AND j.document_version=$4 AND j.object_id=$5 AND j.profile_id=$6
 AND (j.state='pending' AND j.process_id IS NULL OR j.state='running' AND j.process_id=$7)
 AND d.status='active' AND d.content_version=j.document_version AND d.current_object_id=j.object_id
 AND ((p.state='active' AND st.serving_profile_id=p.id AND NOT $11::boolean) OR (p.state='building' AND st.target_profile_id=p.id AND $11::boolean)) AND p.retention_state='retained' AND p.snapshot_id=$8 AND p.dimension=$9 AND p.version=$10)`, w.profile.Project.String(), w.job.id, w.job.document.String(), int64(w.job.version), w.job.object, w.profile.ID, w.service.authority.process.String(), w.profile.Snapshot.String(), w.profile.Dimension, int64(w.profile.Version), w.build != nil).Scan(&eligible)
	if err != nil {
		return unavailable(err)
	}
	if !eligible {
		return fault(f.VersionConflict)
	}
	return nil
}

// Run observes configuration revisions, initializes complete retained profiles,
// and consumes newly published document versions under original owners.
func (s *IndexService) Run(ctx context.Context) error {
	run, done, err := s.admit(ctx)
	if err != nil {
		return err
	}
	defer done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		processed, err := s.RunPending(run)
		if run.Err() != nil {
			return nil
		}
		if err != nil {
			var failed indexJobFailure
			if processed && errors.As(err, &failed) {
				continue
			}
			return err
		}
		if processed {
			continue
		}
		select {
		case <-run.Done():
			return nil
		case <-tick.C:
		}
	}
}

// RunPending takes one durable current-version intent. A failed or uncertain
// original is never selected again as pending work.
func (s *IndexService) RunPending(ctx context.Context) (processed bool, resultErr error) {
	ctx, done, err := s.admit(ctx)
	if err != nil {
		return false, err
	}
	defer done()
	built, err := s.ensureAutomaticProfile(ctx)
	if err != nil || built {
		return built, err
	}
	var j indexJob
	var p indexProfile
	var project, document, snapshot, backend string
	var version, profileVersion int64
	cfg := s.deps.Settings
	err = s.store.QueryRow(ctx, `SELECT j.id::text,j.project_id::text,j.document_id::text,j.document_version,j.object_id::text,p.id::text,p.version,p.snapshot_id::text,p.dimension,p.lexical_backend
 FROM agenteam_knowledge.index_jobs j JOIN agenteam_knowledge.index_profiles p ON p.project_id=j.project_id AND p.id=j.profile_id
 JOIN agenteam_knowledge.index_state st ON st.project_id=p.project_id AND st.serving_profile_id=p.id
 JOIN agenteam_project.projects pr ON pr.id=j.project_id
 WHERE j.state='pending' AND p.state='active' AND p.retention_state='retained' AND pr.lifecycle='active' AND pr.initialized_at IS NOT NULL
 AND p.parser_profile=$1 AND p.chunker_profile=$2 AND p.soft_chunk_bytes=$3 AND p.hard_chunk_bytes=$4 AND p.forced_overlap_bytes=$5 AND p.lexical_backend=$6
 ORDER BY j.created_at,j.id LIMIT 1`, cfg.ParserProfile, chunker.Profile, cfg.Chunker.SoftBytes, cfg.Chunker.HardBytes, cfg.Chunker.ForcedOverlapBytes, string(cfg.Backend)).Scan(&j.id, &project, &document, &version, &j.object, &p.ID, &profileVersion, &snapshot, &p.Dimension, &backend)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, unavailable(err)
	}
	p.Project, err = f.ParseID[id.Project](project)
	if err != nil {
		return false, internal(err)
	}
	p.Snapshot, err = f.ParseID[mc.Snapshot](snapshot)
	if err != nil {
		return false, internal(err)
	}
	j.document, err = f.ParseID[kc.Document](document)
	if err != nil {
		return false, internal(err)
	}
	j.version, p.Version, p.Backend = f.Version(version), f.Version(profileVersion), lexical.Backend(backend)
	// Obsolete versions are retired under the same project/tree lock before
	// constructing a read authority, so they cannot block newer pending work.
	obsolete := false
	err = s.indexTx(ctx, p.Project, true, nil, false, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_jobs j SET state='obsolete',finished_at=clock_timestamp() WHERE j.project_id=$1 AND j.id=$2 AND j.state='pending' AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.documents d WHERE d.project_id=j.project_id AND d.id=j.document_id AND d.content_version=j.document_version AND d.current_object_id=j.object_id AND d.status='active')`, project, j.id)
		if err != nil {
			return portError(err)
		}
		obsolete = tag.RowsAffected() == 1
		return nil
	})
	if err != nil || obsolete {
		return obsolete, err
	}
	scope, _ := id.InProject(p.Project)
	actor, err := s.authority.registration.Actor(j.id, scope)
	if err != nil {
		return false, err
	}
	admission, _ := ctx.Value(indexAdmissionKey{}).(*indexAdmission)
	w := &indexWork{service: s, actor: actor, profile: p, job: j, admission: admission}
	ctx = context.WithValue(ctx, indexWorkKey{}, w)
	ctx, entry, err := s.documents.beginProjectCall(ctx, actor, p.Project, nil, projectCallRead)
	defer s.documents.projectCallDone(entry, &resultErr)
	if err != nil {
		return false, err
	}
	_, err = s.runIndexJob(ctx, actor, p, j)
	if err != nil && !indexRetentionUnknown(err) {
		original := err
		joined := true
		s.mu.Lock()
		for op := range s.owners {
			if op.profile.Project == p.Project && op.job == j.id {
				joined = false
			}
		}
		s.mu.Unlock()
		if joined {
			failed := false
			observation, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.deps.Settings.ModelTimeout)
			observed := s.indexTx(observation, p.Project, false, nil, true, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
				return portError(x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_jobs WHERE project_id=$1 AND id=$2 AND process_id=$3 AND state='failed')`, project, j.id, s.authority.process.String()).Scan(&failed))
			})
			cancel()
			if observed == nil && failed {
				err = indexJobFailure{original}
			}
		}
	}
	return true, err
}

func enqueueIndexInTx(ctx context.Context, x postgres.SQLExecutor, document kc.DocumentRef) error {
	key, err := f.NewID[struct{}]()
	if err != nil {
		return unavailable(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.index_jobs(id,project_id,document_id,document_version,object_id,profile_id,state)
 SELECT $1::uuid,$2::uuid,$3::uuid,$4::bigint,$5::uuid,p.id,'pending' FROM agenteam_knowledge.index_profiles p
 JOIN agenteam_knowledge.index_state st ON st.project_id=p.project_id AND st.serving_profile_id=p.id
 WHERE p.project_id=$2::uuid AND p.state='active' AND p.retention_state='retained'
 ON CONFLICT(document_id,document_version,profile_id) DO NOTHING`, key.String(), document.ProjectID.String(), document.ID.String(), int64(document.ContentVersion), document.ObjectID.String())
	return portError(err)
}
