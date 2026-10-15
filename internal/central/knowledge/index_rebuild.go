package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

// RebuildResult reports an atomically activated profile. A failed build leaves
// the previous serving profile intact; its durable jobs remain inspectable.
type RebuildResult struct {
	ProfileID string `json:"profile_id"`
	Documents int    `json:"documents"`
	Chunks    int    `json:"chunks"`
}
type indexJob struct {
	id       string
	document kc.DocumentID
	version  f.Version
	object   string
}

func (s *IndexService) ownerIndexTx(ctx context.Context, actor id.Actor, project id.ProjectID, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	if actor.Details().Kind == id.Service {
		w, workErr := currentIndexWork(ctx, actor, project)
		b, buildErr := currentIndexBuild(ctx, actor, project)
		if workErr != nil && buildErr != nil || workErr == nil && w.service != s || buildErr == nil && b.service != s {
			return fault(f.Forbidden)
		}
		return s.indexTx(ctx, project, true, nil, false, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			if err := s.documents.projectCallTx(ctx, tx, project); err != nil {
				return err
			}
			if workErr == nil {
				if err := w.authorizeInTx(ctx, tx, s.store); err != nil {
					return err
				}
			} else if err := b.authorizeInTx(ctx, tx); err != nil {
				return err
			}
			return fn(ctx, tx, x)
		})
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human || project.Validate() != nil {
		return fault(f.Forbidden)
	}
	locks, err := scopeLocks(actor, project, true)
	if err != nil {
		return err
	}
	cause, err := readCause()
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.authority.guard.RequireCurrentProcessInTx(ctx, tx, s.authority.process); err != nil {
			return portError(err)
		}
		if err := s.documents.projectCallTx(ctx, tx, project); err != nil {
			return err
		}
		grant, err := s.documents.state().deps.Projects.RequireOwnerInTx(ctx, tx, actor, project, id.Mutate)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	return txError(result)
}

// Rebuild runs the first complete indexing path synchronously under the actual
// Owner call. Every job and source version is durable before Object/Model I/O.
// Caller cancellation stops new work; it never publishes a partial profile.
func (s *IndexService) Rebuild(ctx context.Context, actor id.Actor, project id.ProjectID) (out RebuildResult, resultErr error) {
	if s == nil {
		return out, fault(f.DependencyUnbound)
	}
	if ctx == nil || actor.Validate() != nil || actor.Details().Kind != id.Human || project.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	ctx, done, err := s.admit(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	ctx, b, err := s.newBuild(ctx, actor, project)
	if err != nil {
		return out, err
	}
	return s.rebuild(ctx, b)
}

func (s *IndexService) rebuild(ctx context.Context, b *indexBuild) (out RebuildResult, resultErr error) {
	actor, project := b.owner, b.project
	lane := projectCallCommand
	if actor.Details().Kind == id.Service {
		lane = projectCallRead
	}
	ctx, entry, err := s.documents.beginProjectCall(ctx, actor, project, nil, lane)
	defer s.documents.projectCallDone(entry, &resultErr)
	if err != nil {
		return out, err
	}
	if b.profile.Validate() != nil {
		b.profile, err = b.readProfile(ctx)
		if err != nil {
			return out, err
		}
	}
	profileRaw, err := json.Marshal(b.profile)
	if err != nil {
		return out, internal(err)
	}
	profileDigest, err := b.profile.Digest()
	if err != nil {
		return out, err
	}
	profile := indexProfile{ID: b.id, Project: project, Dimension: s.deps.Settings.Dimension, Backend: s.deps.Settings.Backend}
	g, err := s.newRetention(actor, profile)
	if err != nil {
		return out, err
	}
	g.build = b
	_, g.projectCall, err = s.documents.beginProjectCall(ctx, actor, project, nil, projectCallRead)
	if err != nil {
		g.mu.Lock()
		g.complete()
		g.mu.Unlock()
		return out, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.deps.Settings.ModelTimeout)
		defer cancel()
		resultErr = errors.Join(resultErr, g.finish(cleanup))
	}()
	var jobs []indexJob
	err = s.ownerIndexTx(ctx, actor, project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var building bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.target_profile_id WHERE st.project_id=$1 AND p.state='building')`, project.String()).Scan(&building); err != nil {
			return unavailable(err)
		}
		if building {
			return fault(f.ResourceBusy)
		}
		if err := g.captureOld(ctx, x); err != nil {
			return err
		}
		sourceDigest, err := indexSourceDigest(ctx, x, project)
		if err != nil {
			return err
		}
		cfg := s.deps.Settings
		if err := x.QueryRow(ctx, `INSERT INTO agenteam_knowledge.index_profiles(id,project_id,version,state,parser_profile,chunker_profile,soft_chunk_bytes,hard_chunk_bytes,forced_overlap_bytes,lexical_backend,lexical_config,dimension,profile_data,profile_digest,source_digest) SELECT $1,$2::uuid,COALESCE(MAX(version),0)+1,'building',$3,$4,$5,$6,$7,$8,'{}'::jsonb,$9,$10::jsonb,$11,$12 FROM agenteam_knowledge.index_profiles WHERE project_id=$2::uuid RETURNING version`, profile.ID, project.String(), cfg.ParserProfile, chunker.Profile, cfg.Chunker.SoftBytes, cfg.Chunker.HardBytes, cfg.Chunker.ForcedOverlapBytes, string(profile.Backend), profile.Dimension, string(profileRaw), string(profileDigest), string(sourceDigest)).Scan(&profile.Version); err != nil {
			return portError(err)
		}
		g.target.Profile.Version = profile.Version
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_knowledge.index_state(project_id,target_profile_id,version) VALUES($1,$2,1) ON CONFLICT(project_id) DO UPDATE SET target_profile_id=EXCLUDED.target_profile_id,version=agenteam_knowledge.index_state.version+1`, project.String(), profile.ID); err != nil {
			return portError(err)
		}
		rows, err := x.Query(ctx, `SELECT id::text,content_version,current_object_id::text FROM agenteam_knowledge.documents WHERE project_id=$1 AND status='active' ORDER BY id`, project.String())
		if err != nil {
			return unavailable(err)
		}
		for rows.Next() {
			var key string
			var j indexJob
			var version int64
			if err := rows.Scan(&key, &version, &j.object); err != nil {
				rows.Close()
				return unavailable(err)
			}
			j.document, err = f.ParseID[kc.Document](key)
			if err != nil {
				rows.Close()
				return internal(err)
			}
			j.version = f.Version(version)
			jobs = append(jobs, j)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable(err)
		}
		for n := range jobs {
			key, err := f.NewID[struct{}]()
			if err != nil {
				return unavailable(err)
			}
			jobs[n].id = key.String()
			j := jobs[n]
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.index_jobs(id,project_id,document_id,document_version,object_id,profile_id,state) VALUES($1,$2,$3,$4,$5,$6,'pending')`, j.id, project.String(), j.document.String(), int64(j.version), j.object, profile.ID); err != nil {
				return portError(err)
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	// Capture the current selector once. Serving queries and every document use
	// this exact immutable snapshot, even if the administrator changes selectors.
	run, op, err := s.beginModelOperation(ctx, profile, "profile", "", []string{"knowledge profile snapshot"}, nil)
	if err == nil {
		resolved, resolveErr := s.resolveOperation(run, op)
		err = resolveErr
		if err == nil {
			current, profileErr := b.readProfile(ctx)
			if profileErr == nil && (current != b.profile || !embeddingProfileMatchesSnapshot(b.profile, resolved.Snapshot)) {
				profileErr = fault(f.VersionConflict)
			}
			err = profileErr
			if err == nil {
				err = g.pin(ctx, op, resolved)
			}
			if err == nil {
				profile.Snapshot = resolved.Snapshot.ID
			}
		}
		terminal := "completed"
		if err != nil {
			terminal = "failed"
		}
		s.markOperationTerminal(op, terminal)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(run), s.deps.Settings.ModelTimeout)
		err = errors.Join(err, s.retireOperation(cleanup, op))
		cancel()
	}
	if err != nil {
		return out, err
	}
	for _, job := range jobs {
		jobCtx, jobActor := ctx, actor
		if actor.Details().Kind == id.Service {
			scope, _ := id.InProject(project)
			jobActor, err = s.authority.registration.Actor(job.id, scope)
			if err != nil {
				return out, err
			}
			w := &indexWork{service: s, actor: jobActor, profile: profile, job: job, admission: b.admission, build: b}
			jobCtx = context.WithValue(ctx, indexWorkKey{}, w)
		}
		count, err := s.runIndexJob(jobCtx, jobActor, profile, job)
		if err != nil {
			return out, err
		}
		out.Documents++
		out.Chunks += count
	}
	current, err := b.readProfile(ctx)
	if err != nil {
		return out, err
	}
	if current != b.profile {
		return out, fault(f.VersionConflict)
	}
	err = g.publish(ctx)
	if err != nil {
		return out, err
	}
	out.ProfileID = profile.ID
	return out, nil
}

func (s *IndexService) runIndexJob(ctx context.Context, actor id.Actor, p indexProfile, j indexJob) (count int, resultErr error) {
	err := s.ownerIndexTx(ctx, actor, p.Project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_jobs j SET state='running',attempt_count=attempt_count+1,process_id=$3,started_at=clock_timestamp() FROM agenteam_knowledge.documents d WHERE j.project_id=$1 AND j.id=$2 AND j.state='pending' AND d.project_id=j.project_id AND d.id=j.document_id AND d.content_version=j.document_version AND d.current_object_id=j.object_id AND d.status='active'`, p.Project.String(), j.id, s.authority.process.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET indexing_status='processing' WHERE project_id=$1 AND id=$2 AND status='active' AND content_version=$3 AND EXISTS(SELECT 1 FROM agenteam_knowledge.index_state WHERE project_id=$1 AND serving_profile_id=$4)`, p.Project.String(), j.document.String(), int64(j.version), p.ID)
		return portError(err)
	})
	if err != nil {
		return 0, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, s.failJob(ctx, p, j, resultErr))
		}
	}()
	reader, err := s.documents.OpenCanonical(ctx, actor, p.Project, j.document, nil)
	if err != nil {
		return 0, err
	}
	head := reader.Document()
	raw, readErr := io.ReadAll(io.LimitReader(reader, parser.MaxSourceBytes+1))
	closeErr := reader.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return 0, err
	}
	if len(raw) > parser.MaxSourceBytes {
		return 0, fault(f.PayloadTooLarge)
	}
	if head.ContentVersion != j.version || head.ObjectID.String() != j.object {
		return 0, fault(f.VersionConflict)
	}
	// The same original job admission owns the synchronous parser call. An
	// external parser returns only after its actual process group has joined.
	parsed, err := s.deps.Parser.ParseBytes(ctx, parser.SourceIdentity{ProjectID: p.Project, DocumentID: j.document, ContentVersion: j.version, ObjectID: head.ObjectID}, head.Title, head.MediaType, s.deps.Settings.ParserProfile, raw)
	clear(raw)
	if err != nil {
		return 0, err
	}
	chunks, err := chunker.Split(ctx, parsed, s.deps.Settings.Chunker)
	if err != nil {
		return 0, err
	}
	vectors := make([][]float64, 0, len(chunks))
	batch := min(64, 262144/p.Dimension)
	for start := 0; start < len(chunks); start += batch {
		end := min(start+batch, len(chunks))
		texts := make([]string, end-start)
		for n := start; n < end; n++ {
			texts[n-start] = chunks[n].IndexText
		}
		got, err := s.embedTexts(ctx, p, "index", j.id, texts, nil)
		if err != nil {
			return 0, err
		}
		vectors = append(vectors, got...)
	}
	generation, err := f.NewID[struct{}]()
	if err != nil {
		return 0, unavailable(err)
	}
	err = s.ownerIndexTx(ctx, actor, p.Project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var eligible bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_jobs j JOIN agenteam_knowledge.documents d ON d.project_id=j.project_id AND d.id=j.document_id JOIN agenteam_knowledge.index_profiles p ON p.project_id=j.project_id AND p.id=j.profile_id WHERE j.project_id=$1 AND j.id=$2 AND j.state='running' AND j.process_id=$3 AND d.status='active' AND d.content_version=j.document_version AND d.current_object_id=j.object_id AND p.state IN ('building','active') AND p.retention_state='retained' AND p.snapshot_id=$4)`, p.Project.String(), j.id, s.authority.process.String(), p.Snapshot.String()).Scan(&eligible); err != nil {
			return unavailable(err)
		}
		if !eligible {
			return fault(f.VersionConflict)
		}
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_knowledge.index_generations(id,project_id,job_id,document_id,document_version,profile_id,snapshot_id,dimension,state,expected_chunks,parsed_text,parsed_profile) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'building',$9,$10,$11)`, generation.String(), p.Project.String(), j.id, j.document.String(), int64(j.version), p.ID, p.Snapshot.String(), p.Dimension, len(chunks), parsed.Text, parsed.ParserProfile); err != nil {
			return portError(err)
		}
		for n, c := range chunks {
			key, err := f.NewID[struct{}]()
			if err != nil {
				return unavailable(err)
			}
			locator, err := json.Marshal(c.Locator)
			if err != nil {
				return internal(err)
			}
			vector, err := vectorText(vectors[n])
			if err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.index_chunks(id,project_id,generation_id,ordinal,raw_text,index_text,locator,embedding,dimension) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8::vector,$9)`, key.String(), p.Project.String(), generation.String(), n+1, c.RawText, c.IndexText, string(locator), vector, p.Dimension); err != nil {
				return portError(err)
			}
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_generations SET state='ready',lexical_complete=true,dense_complete=true,ready_at=clock_timestamp() WHERE project_id=$1 AND id=$2`, p.Project.String(), generation.String()); err != nil {
			return portError(err)
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_jobs SET state='succeeded',finished_at=clock_timestamp() WHERE project_id=$1 AND id=$2 AND state='running'`, p.Project.String(), j.id); err != nil {
			return portError(err)
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET indexing_status='ready' WHERE project_id=$1 AND id=$2 AND status='active' AND content_version=$3 AND EXISTS(SELECT 1 FROM agenteam_knowledge.index_state WHERE project_id=$1 AND serving_profile_id=$4)`, p.Project.String(), j.document.String(), int64(j.version), p.ID)
		return portError(err)
	})
	if err != nil {
		return 0, err
	}
	return len(chunks), nil
}
func (s *IndexService) failJob(ctx context.Context, p indexProfile, j indexJob, original error) error {
	code := "DEPENDENCY_UNAVAILABLE"
	var known *f.Fault
	if errors.As(original, &known) {
		code = string(known.Code)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.deps.Settings.ModelTimeout)
	defer cancel()
	return s.indexTx(cleanup, p.Project, true, nil, true, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_jobs SET state='failed',error_code=$3,retryable=false,finished_at=clock_timestamp() WHERE project_id=$1 AND id=$2 AND state='running' AND process_id=$4`, p.Project.String(), j.id, code, s.authority.process.String()); err != nil {
			return portError(err)
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET indexing_status='failed' WHERE project_id=$1 AND id=$2 AND status='active' AND content_version=$3 AND EXISTS(SELECT 1 FROM agenteam_knowledge.index_state WHERE project_id=$1 AND serving_profile_id=$4)`, p.Project.String(), j.document.String(), int64(j.version), p.ID)
		return portError(err)
	})
}

// Stable canonical versions make a failed automatic build eligible again only
// after its real inputs change. No Model retry is inferred from elapsed time.
func indexSourceDigest(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) (f.Digest, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(id,content_version,current_object_id) ORDER BY id),'[]'::jsonb) FROM agenteam_knowledge.documents WHERE project_id=$1 AND status='active'`, project.String()).Scan(&raw)
	if err != nil {
		return "", unavailable(err)
	}
	return ob.DigestBytes(raw), nil
}
