package skill

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type skillWork struct{}
type skillWorkID = f.ID[skillWork]
type workKind string

const (
	initializationWork         workKind = "initialization"
	packageReaderWork          workKind = "package_reader"
	installationWork           workKind = "installation"
	installedPackageReaderWork workKind = "installed_package_reader"
)

func installedWork(kind workKind) bool {
	return kind == installationWork || kind == installedPackageReaderWork
}

type workPhase string

const (
	workRunning workPhase = "running"
	workJoined  workPhase = "joined"
	workUnknown workPhase = "unknown"
)

type workFact struct {
	id      skillWorkID
	project id.ProjectID
	skill   sc.SkillID
	process oc.ProcessID
	kind    workKind
	phase   workPhase
	fence   f.Version
	created f.Instant
	joined  *f.Instant
}

func (w workFact) validate() error {
	if w.id.Validate() != nil || w.project.Validate() != nil || w.skill.Validate() != nil || w.process.Validate() != nil || w.fence.Validate() != nil || w.created.Validate() != nil || (w.kind != initializationWork && w.kind != packageReaderWork && !installedWork(w.kind)) {
		return unavailable(nil)
	}
	switch w.phase {
	case workRunning, workUnknown:
		if w.joined != nil {
			return unavailable(nil)
		}
	case workJoined:
		if w.joined == nil || w.joined.Validate() != nil || w.joined.Time().Before(w.created.Time()) {
			return unavailable(nil)
		}
	default:
		return unavailable(nil)
	}
	return nil
}
func loadWork(ctx context.Context, x postgres.SQLExecutor, work skillWorkID) (*workFact, error) {
	var idText, project, skill, process, kind, phase string
	var fence int64
	var created time.Time
	var joined *time.Time
	e := x.QueryRow(ctx, `SELECT id::text,project_id::text,skill_id::text,process_id::text,kind,phase,fence,created_at,joined_at FROM agenteam_skill.work WHERE id=$1`, work.String()).Scan(&idText, &project, &skill, &process, &kind, &phase, &fence, &created, &joined)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	w := workFact{kind: workKind(kind), phase: workPhase(phase), fence: f.Version(fence)}
	if w.id, e = f.ParseID[skillWork](idText); e != nil {
		return nil, unavailable(e)
	}
	if w.project, e = f.ParseID[id.Project](project); e != nil {
		return nil, unavailable(e)
	}
	if w.skill, e = f.ParseID[pc.Skill](skill); e != nil {
		return nil, unavailable(e)
	}
	if w.process, e = f.ParseID[oc.Process](process); e != nil {
		return nil, unavailable(e)
	}
	if w.created, e = f.NewInstant(created); e != nil {
		return nil, unavailable(e)
	}
	if joined != nil {
		at, e := f.NewInstant(*joined)
		if e != nil {
			return nil, unavailable(e)
		}
		w.joined = &at
	}
	if e = w.validate(); e != nil {
		return nil, e
	}
	if w.id != work {
		return nil, unavailable(nil)
	}
	return &w, nil
}
func insertWork(ctx context.Context, x postgres.SQLExecutor, w workFact) error {
	if e := w.validate(); e != nil {
		return e
	}
	if w.phase != workRunning || w.fence != 1 {
		return invalid()
	}
	_, e := x.Exec(ctx, `INSERT INTO agenteam_skill.work(id,project_id,skill_id,process_id,kind,phase,fence,created_at) VALUES($1,$2,$3,$4,$5,'running',1,$6)`, w.id.String(), w.project.String(), w.skill.String(), w.process.String(), string(w.kind), w.created.Time())
	if e != nil {
		return unavailable(e)
	}
	return nil
}

// The caller must already prove actual local return, or exact foreign process
// death plus the original transaction's terminal lock boundary. This SQL is
// not a TTL/heartbeat inference and cannot transfer ownership to a new process.
func joinWork(ctx context.Context, x postgres.SQLExecutor, expected workFact) error {
	if e := expected.validate(); e != nil {
		return e
	}
	current, e := loadWork(ctx, x, expected.id)
	if e != nil {
		return e
	}
	if current == nil {
		return fault(f.NotFound)
	}
	if current.project != expected.project || current.skill != expected.skill || current.process != expected.process || current.kind != expected.kind || current.fence != expected.fence {
		return fault(f.ResourceBusy)
	}
	if current.phase == workJoined {
		return nil
	}
	tag, e := x.Exec(ctx, `UPDATE agenteam_skill.work SET phase='joined',joined_at=clock_timestamp() WHERE id=$1 AND process_id=$2 AND fence=$3 AND phase IN ('running','unknown')`, expected.id.String(), expected.process.String(), int64(expected.fence))
	if e != nil {
		return unavailable(e)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
