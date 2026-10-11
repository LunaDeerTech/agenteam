package skill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// AssignmentValidation is an explicitly composed, SQL-only Skill owner. It
// returns facts, never a reusable current-authority or package-read witness.
type AssignmentValidation struct{ store Store }

func NewAssignmentValidation(owner *Authority) (*AssignmentValidation, error) {
	if owner.state() == nil || nilPort(owner.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &AssignmentValidation{store: owner.state().store}, nil
}
func (AssignmentValidation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_assignment_validation")
}
func (AssignmentValidation) LogValue() slog.Value {
	return slog.StringValue("skill_assignment_validation")
}
func (AssignmentValidation) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_assignment_validation"`), nil
}

func validationLocks(project i.ProjectID, agent i.AgentID, skill *sc.SkillID, execution *i.ExecutionID) ([]f.LockRequest, error) {
	if project.Validate() != nil || agent.Validate() != nil {
		return nil, invalid()
	}
	a, _ := f.AgentLock(agent.String())
	schedule, _ := f.ProjectScheduleLock(project.String())
	locks := []f.LockRequest{projectLock(project, f.Shared), {Key: schedule, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}}
	if skill != nil {
		if skill.Validate() != nil {
			return nil, invalid()
		}
		locks = append(locks, skillLock(*skill, f.Shared))
	}
	if execution != nil {
		if execution.Validate() != nil {
			return nil, invalid()
		}
		e, _ := f.AggregateLock(f.ExecutionAggregate, execution.String())
		locks = append(locks, f.LockRequest{Key: e, Mode: f.Exclusive})
	}
	return ob.NormalizeLocks(locks)
}
func (v *AssignmentValidation) inTx(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	if ctx == nil || !tx.Valid() {
		return nil, invalid()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if v == nil || nilPort(v.store) {
		return nil, fault(f.DependencyUnbound)
	}
	x, err := v.store.InTx(tx)
	if err != nil {
		return nil, captureError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = v.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, captureError(err)
	}
	return x, ctx.Err()
}
func (v *AssignmentValidation) ReadAssignmentSequenceInTx(ctx context.Context, tx f.Tx, project i.ProjectID, agent i.AgentID) (f.Version, error) {
	locks, err := validationLocks(project, agent, nil, nil)
	if err != nil {
		return 0, err
	}
	x, err := v.inTx(ctx, tx, locks)
	if err != nil {
		return 0, err
	}
	state, err := loadAssignmentSet(ctx, x, project, agent)
	if err != nil {
		return 0, captureError(err)
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return state.Sequence, nil
}

func (v *AssignmentValidation) RequirePendingAssignmentInTx(ctx context.Context, tx f.Tx, execution i.ExecutionID, c sc.RuntimeAssignmentChange) error {
	if c.Validate() != nil {
		return invalid()
	}
	locks, err := validationLocks(c.ProjectID, c.AgentID, &c.Binding.SkillID, &execution)
	if err != nil {
		return err
	}
	x, err := v.inTx(ctx, tx, locks)
	if err != nil {
		return err
	}
	var runtime, state []byte
	var digest string
	err = x.QueryRow(ctx, `SELECT runtime_data,state_data,state_digest FROM agenteam_skill.assignment_changes WHERE id=$1 AND project_id=$2 AND agent_id=$3 AND assignment_sequence=$4 AND agent_version=$5 AND created_at=$6`, c.ID.String(), c.ProjectID.String(), c.AgentID.String(), int64(c.AssignmentSequence), int64(c.AgentVersion), c.RecordedAt.Time()).Scan(&runtime, &state, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return fault(f.Forbidden)
	}
	if err != nil {
		return captureError(err)
	}
	_, expected, err := captureDigest(c)
	if err != nil {
		return err
	}
	if !bytes.Equal(runtime, expected) {
		return fault(f.Forbidden)
	}
	command, err := loadAssignmentByID(ctx, x, c.ProjectID, c.AgentID, c.ID)
	if err != nil {
		return err
	}
	if command.State != "completed" || command.Candidate.Runtime == nil || *command.Candidate.Runtime != c || command.Candidate.Revision == nil || command.Candidate.Added == nil {
		return fault(f.Forbidden)
	}
	receipt, err := command.receipt()
	if err != nil {
		return err
	}
	if receipt.Runtime == nil || !receipt.Runtime.Queued || receipt.Runtime.ExecutionID == nil || *receipt.Runtime.ExecutionID != execution {
		return fault(f.Forbidden)
	}
	want, raw, err := captureDigest(command.Candidate.After)
	if err != nil {
		return err
	}
	if digest != string(want) || !bytes.Equal(state, raw) {
		return unavailable(nil)
	}
	if err = validateAssignmentRouting(ctx, x, command); err != nil {
		return err
	}
	grant, err := readValidationGrant(ctx, x, c.Binding.AssignmentID)
	if err != nil {
		return err
	}
	if grant.assignment != *command.Candidate.Added {
		return unavailable(nil)
	}
	fixed, err := readValidationRevision(ctx, x, c.ProjectID, c.Binding.SkillID, c.Binding.RevisionID, c.Binding.Revision)
	if err != nil {
		return err
	}
	if fixed != *command.Candidate.Revision {
		return unavailable(nil)
	}
	return ctx.Err()
}

type validationGrant struct {
	assignment sc.AgentSkillAssignment
	removed    *f.Instant
}

func readValidationGrant(ctx context.Context, x postgres.SQLExecutor, assignment sc.AssignmentID) (validationGrant, error) {
	var out validationGrant
	var project, agent, skill string
	var enabled bool
	var sequence int64
	var created time.Time
	var removed *time.Time
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,skill_id::text,enabled,assignment_sequence,created_at,removed_at FROM agenteam_skill.agent_assignments WHERE id=$1`, assignment.String()).Scan(&project, &agent, &skill, &enabled, &sequence, &created, &removed)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fault(f.NotFound)
	}
	if err != nil {
		return out, captureError(err)
	}
	out.assignment.ID = assignment
	out.assignment.ProjectID, err = f.ParseID[i.Project](project)
	if err != nil {
		return validationGrant{}, unavailable(err)
	}
	out.assignment.AgentID, err = f.ParseID[i.Agent](agent)
	if err != nil {
		return validationGrant{}, unavailable(err)
	}
	out.assignment.SkillID, err = f.ParseID[pc.Skill](skill)
	if err != nil {
		return validationGrant{}, unavailable(err)
	}
	out.assignment.Enabled, out.assignment.Sequence = enabled, f.Version(sequence)
	out.assignment.CreatedAt, err = f.NewInstant(created)
	if err != nil {
		return validationGrant{}, unavailable(err)
	}
	if removed != nil {
		at, e := f.NewInstant(*removed)
		if e != nil || removed.Before(created) {
			return validationGrant{}, unavailable(e)
		}
		out.removed = &at
	}
	if out.assignment.Validate() != nil {
		return validationGrant{}, unavailable(nil)
	}
	return out, ctx.Err()
}

// Read the bound revision and its original same-domain provenance, never the
// current Skill revision pointer. Closed serving is classified separately;
// it cannot conceal a foreign/missing revision or fabricate package metadata.
func readValidationRevision(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, skill sc.SkillID, revision sc.RevisionID, number f.Revision) (assignmentRevision, error) {
	var zero assignmentRevision
	var p, s, object, installation string
	var n, version int64
	var created, published time.Time
	err := x.QueryRow(ctx, `SELECT project_id::text,skill_id::text,revision,object_id::text,object_version,object_created_at,published_at,COALESCE(installation_id::text,'') FROM agenteam_skill.revisions WHERE id=$1`, revision.String()).Scan(&p, &s, &n, &object, &version, &created, &published, &installation)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, fault(f.NotFound)
	}
	if err != nil {
		return zero, captureError(err)
	}
	if p != project.String() || s != skill.String() || n != int64(number) {
		return zero, fault(f.Forbidden)
	}
	out := assignmentRevision{ID: revision, SkillID: skill, ProjectID: project, Revision: number, EntryPath: sc.EntryPath, ObjectVersion: f.Version(version)}
	out.ObjectID, err = f.ParseID[oc.StoredObject](object)
	if err != nil {
		return zero, unavailable(err)
	}
	out.ObjectCreatedAt, err = f.NewInstant(created)
	if err != nil {
		return zero, unavailable(err)
	}
	out.PublishedAt, err = f.NewInstant(published)
	if err != nil || published.Before(created) {
		return zero, unavailable(err)
	}
	if installation == "" {
		origin, e := loadInitialization(ctx, x, project)
		if e != nil {
			return zero, e
		}
		if origin == nil || origin.phase != initializationPublished || origin.skill != skill || origin.revision != revision || origin.bundle.revision != number || origin.object != out.ObjectID {
			return zero, unavailable(nil)
		}
		out.Name, out.Description, out.PackageSHA256, out.ManifestDigest = origin.bundle.name, origin.bundle.description, origin.bundle.packageDigest, origin.bundle.manifestDigest
	} else {
		origin, e := loadInstallationRevision(ctx, x, project, revision)
		if e != nil {
			return zero, e
		}
		if origin == nil || origin.id.String() != installation || origin.phase != installationPublished || origin.skill != skill || number != 1 || origin.object != out.ObjectID {
			return zero, unavailable(nil)
		}
		out.Name, out.Description, out.PackageSHA256, out.ManifestDigest = origin.pkg.name, origin.pkg.description, origin.pkg.packageDigest, origin.pkg.manifestDigest
	}
	if out.validate() != nil {
		return zero, unavailable(nil)
	}
	return out, ctx.Err()
}

func (v *AssignmentValidation) ValidateAssignmentInTx(ctx context.Context, tx f.Tx, project i.ProjectID, agent i.AgentID, assignment sc.AssignmentID, skill sc.SkillID, revision sc.RevisionID, number f.Revision) (sc.AssignmentValidity, error) {
	if assignment.Validate() != nil || revision.Validate() != nil || number.Validate() != nil {
		return "", invalid()
	}
	locks, err := validationLocks(project, agent, &skill, nil)
	if err != nil {
		return "", err
	}
	x, err := v.inTx(ctx, tx, locks)
	if err != nil {
		return "", err
	}
	grant, err := readValidationGrant(ctx, x, assignment)
	if err != nil {
		return "", err
	}
	if grant.assignment.ProjectID != project || grant.assignment.AgentID != agent || grant.assignment.SkillID != skill {
		return "", fault(f.Forbidden)
	}
	if _, err = readValidationRevision(ctx, x, project, skill, revision, number); err != nil {
		return "", err
	}
	current, err := loadAssignmentSet(ctx, x, project, agent)
	if err != nil {
		return "", captureError(err)
	}
	var serving bool
	err = x.QueryRow(ctx, `SELECT serving FROM agenteam_skill.skills WHERE project_id=$1 AND id=$2`, project.String(), skill.String()).Scan(&serving)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fault(f.NotFound)
	}
	if err != nil {
		return "", captureError(err)
	}
	status, err := classifyValidationGrant(current, grant, serving)
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return status, nil
}
func classifyValidationGrant(current assignmentSet, grant validationGrant, serving bool) (sc.AssignmentValidity, error) {
	a := grant.assignment
	if current.validate() != nil || a.Validate() != nil || a.ProjectID != current.Project || a.AgentID != current.Agent || a.Sequence > current.Sequence {
		return "", unavailable(nil)
	}
	found := false
	for _, item := range current.Items {
		if item.ID == a.ID {
			if item != a {
				return "", unavailable(nil)
			}
			found = true
		}
	}
	if grant.removed != nil {
		if found {
			return "", unavailable(nil)
		}
		return sc.AssignmentRemoved, nil
	}
	if !found {
		return "", unavailable(nil)
	}
	if !a.Enabled {
		return sc.AssignmentDisabled, nil
	}
	if !serving {
		return sc.AssignmentDeleted, nil
	}
	return sc.AssignmentValid, nil
}

var _ sc.RoundAssignmentValidation = (*AssignmentValidation)(nil)
