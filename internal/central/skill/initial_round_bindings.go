package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// InitialRoundBindings reads Skill-owned facts. It neither grants Execution
// authority nor writes capture, assignment, Round or Model facts.
type InitialRoundBindings struct{ store Store }

func NewInitialRoundBindings(authority *Authority) (*InitialRoundBindings, error) {
	if authority.state() == nil || nilPort(authority.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &InitialRoundBindings{store: authority.state().store}, nil
}

type initialRoundBindingsPlan struct {
	owner   *InitialRoundBindings
	request sc.InitialRoundBindingsRequest
	source  skillCaptureSource
	digest  f.Digest
	locks   []f.LockRequest
}

func (p *initialRoundBindingsPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*initialRoundBindingsPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_initial_round_bindings_plan")
}
func (*initialRoundBindingsPlan) LogValue() slog.Value {
	return slog.StringValue("skill_initial_round_bindings_plan")
}
func (*initialRoundBindingsPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_initial_round_bindings_plan"`), nil
}
func (InitialRoundBindings) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_initial_round_bindings")
}
func (InitialRoundBindings) LogValue() slog.Value {
	return slog.StringValue("skill_initial_round_bindings")
}

func sameInitialRoundRequest(a, b sc.InitialRoundBindingsRequest) bool {
	return a.CaptureAttemptBinding == b.CaptureAttemptBinding &&
		a.Initial.Request == b.Initial.Request && a.Initial.AssignmentSequence == b.Initial.AssignmentSequence &&
		slices.Equal(a.Initial.Bindings, b.Initial.Bindings)
}

func (p *InitialRoundBindings) valid(ctx context.Context, r sc.InitialRoundBindingsRequest) error {
	if ctx == nil || r.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || nilPort(p.store) {
		return fault(f.DependencyUnbound)
	}
	return nil
}

func (p *InitialRoundBindings) inTx(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, captureError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, captureError(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return x, nil
}

// The head is immutable; its record also identifies the protected default
// Skill when the captured directory is empty. Never infer empty from no row.
func loadInitialRoundCapture(ctx context.Context, x postgres.SQLExecutor, r sc.InitialRoundBindingsRequest) (skillCaptureRecord, error) {
	var empty skillCaptureRecord
	var project, agent, attempt, source string
	var sequence, count int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,assignment_sequence,attempt_binding,source_digest,binding_count,record FROM agenteam_skill.execution_binding_heads WHERE execution_id=$1`, r.Initial.Request.ExecutionID.String()).Scan(&project, &agent, &sequence, &attempt, &source, &count, &raw)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, fault(f.DependencyUnbound)
	}
	if err != nil {
		return empty, captureError(err)
	}
	if project != r.Initial.Request.ProjectID.String() || agent != r.Initial.Request.AgentID.String() || sequence != int64(r.Initial.AssignmentSequence) || attempt != string(r.CaptureAttemptBinding) || count != int64(len(r.Initial.Bindings)) {
		return empty, fault(f.ConfirmationStale)
	}
	if len(raw) == 0 || len(raw) > 256<<10 || count < 0 {
		return empty, unavailable(nil)
	}
	var record skillCaptureRecord
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&record) != nil || d.Decode(new(any)) != io.EOF || record.Format != record.Source.format() ||
		record.Attempt != r.CaptureAttemptBinding || record.SourceDigest != f.Digest(source) ||
		record.SourceDigest.Validate() != nil || record.Source.InitialSkillID.Validate() != nil ||
		record.Source.validate() != nil || !sameInitialRoundRequest(r, sc.InitialRoundBindingsRequest{Initial: record.Source.Result, CaptureAttemptBinding: record.Attempt}) {
		return empty, unavailable(nil)
	}
	digest, _, err := captureDigest(record.Source)
	if err != nil {
		return empty, err
	}
	if digest != record.SourceDigest {
		return empty, unavailable(nil)
	}
	return record, nil
}

func validateInitialRoundCapture(ctx context.Context, x postgres.SQLExecutor, r sc.InitialRoundBindingsRequest, record skillCaptureRecord) error {
	err := verifyHistoricalCaptureSource(ctx, x, record.Source)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return captureError(err)
	}
	// Reuse the capture writer's complete canonical/row-count/assignment/
	// revision/Object verification without calling its INSERT path or issuer.
	expected := &initialBindingsPlan{request: r.Initial.Request, attempt: r.CaptureAttemptBinding, source: record.Source, digest: record.SourceDigest}
	found, err := verifySkillCapture(ctx, x, expected)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return captureError(err)
	}
	if !found {
		return fault(f.ConfirmationStale)
	}
	return nil
}

// Historical input is anchored to the immutable initialization/change and
// exact revision rows. Neither assignment_current nor skills.revision_id is
// consulted: later configuration cannot replace a captured Round catalogue.
func verifyHistoricalCaptureSource(ctx context.Context, x postgres.SQLExecutor, s skillCaptureSource) error {
	if err := s.validate(); err != nil {
		return err
	}
	r := s.Result.Request
	head, err := loadAgentInitialization(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if head == nil || head.command != s.CreationCommand || head.planRevision != s.PlanRevision || head.binding != s.RequestBinding || head.enabled != s.AddSkillsEnabled || head.skill != s.InitialSkillID || head.revision != s.ObservedRevision || head.created != s.HeadCreatedAt {
		return fault(f.ConfirmationStale)
	}
	if s.Assignments == nil && head.enabled && (head.assignment == nil || *head.assignment != s.Result.Bindings[0].AssignmentID) {
		return fault(f.ConfirmationStale)
	}
	publication, err := loadInitialization(ctx, x, r.ProjectID)
	if err != nil {
		return err
	}
	if publication == nil || publication.phase != initializationPublished || publication.request.CreationID != s.CreationID || publication.request.InitializationKey != s.InitializationKey || publication.version != s.InitializationVersion || publication.semantic != s.InitializationDigest || publication.skill != s.InitialSkillID || publication.revision != s.RevisionID || publication.bundle.revision != s.ObservedRevision || publication.object != s.ObjectID || publication.bundle.packageDigest != s.PackageDigest || publication.bundle.manifestDigest != s.ManifestDigest {
		return fault(f.ConfirmationStale)
	}
	meta := s.Metadata
	if meta.ID != s.InitialSkillID || meta.ProjectID != r.ProjectID || !meta.Protected || meta.Name != publication.bundle.name || meta.NormalizedName != AddSkillsNormalizedName || meta.Description != publication.bundle.description || meta.CurrentRevision != s.ObservedRevision {
		return unavailable(nil)
	}
	builtin := assignmentRevision{ID: s.RevisionID, SkillID: s.InitialSkillID, ProjectID: r.ProjectID, Revision: s.ObservedRevision, Name: meta.Name, Description: meta.Description, EntryPath: sc.EntryPath, ObjectID: s.ObjectID, ObjectVersion: s.ObjectVersion, ObjectCreatedAt: s.ObjectCreatedAt, PublishedAt: s.PublishedAt, PackageSHA256: s.PackageDigest, ManifestDigest: s.ManifestDigest}
	if err = verifyCapturedRevision(ctx, x, builtin, s.InitialSkillID, s.CreationID.String()); err != nil {
		return err
	}
	if s.Assignments == nil {
		return ctx.Err()
	}
	state := s.Assignments.State
	var raw []byte
	var digest string
	err = x.QueryRow(ctx, `SELECT state_data,state_digest FROM agenteam_skill.assignment_changes WHERE project_id=$1 AND agent_id=$2 AND assignment_sequence=$3`, state.Project.String(), state.Agent.String(), int64(state.Sequence)).Scan(&raw, &digest)
	if err != nil {
		return captureError(err)
	}
	var stored assignmentSet
	if assignmentDecode(raw, &stored, 1<<20) != nil || !sameCaptureAssignments(stored, state) {
		return unavailable(nil)
	}
	want, _, err := captureDigest(state)
	if err != nil || string(want) != digest {
		return unavailable(err)
	}
	// Removed rows remain immutable history. Do not require removed_at NULL.
	for _, a := range state.Items {
		var matching int64
		err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND id=$3 AND skill_id=$4 AND enabled=$5 AND assignment_sequence=$6 AND created_at=$7`, a.ProjectID.String(), a.AgentID.String(), a.ID.String(), a.SkillID.String(), a.Enabled, int64(a.Sequence), a.CreatedAt.Time()).Scan(&matching)
		if err != nil {
			return captureError(err)
		}
		if matching != 1 {
			return unavailable(nil)
		}
	}
	for _, revision := range s.Assignments.Revisions {
		if revision.SkillID == s.InitialSkillID {
			if revision != builtin {
				return unavailable(nil)
			}
			continue
		}
		if err = verifyCapturedRevision(ctx, x, revision, s.InitialSkillID, s.CreationID.String()); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func verifyCapturedRevision(ctx context.Context, x postgres.SQLExecutor, r assignmentRevision, initial sc.SkillID, creation string) error {
	if r.validate() != nil {
		return unavailable(nil)
	}
	installation, attempt, upload := "", "", ""
	if r.SkillID != initial {
		origin, err := loadInstallationRevision(ctx, x, r.ProjectID, r.ID)
		if err != nil {
			return err
		}
		if origin == nil || origin.phase != installationPublished || origin.skill != r.SkillID || origin.object != r.ObjectID || r.Revision != 1 || origin.pkg.name != r.Name || origin.pkg.description != r.Description || origin.pkg.packageDigest != r.PackageSHA256 || origin.pkg.manifestDigest != r.ManifestDigest {
			return unavailable(nil)
		}
		installation, attempt, upload = origin.id.String(), origin.attempt.String(), origin.upload.String()
	}
	var matching int64
	err := x.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.revisions r JOIN agenteam_skill.skills s ON s.project_id=r.project_id AND s.id=r.skill_id WHERE r.project_id=$1 AND r.skill_id=$2 AND r.id=$3 AND r.revision=$4 AND r.object_id=$5 AND r.object_version=$6 AND r.object_created_at=$7 AND r.published_at=$8 AND s.serving AND COALESCE(r.installation_id::text,'')=$9 AND (($9='' AND s.protected AND s.creation_id::text=$10 AND s.installation_id IS NULL) OR ($9<>'' AND NOT s.protected AND s.creation_id IS NULL AND s.installation_id::text=$9 AND EXISTS(SELECT 1 FROM agenteam_skill.installation_attempts a WHERE a.project_id=r.project_id AND a.installation_id=r.installation_id AND a.skill_id=r.skill_id AND a.revision_id=r.id AND a.object_id=r.object_id AND a.attempt_id::text=$11 AND a.upload_id::text=$12)))`, r.ProjectID.String(), r.SkillID.String(), r.ID.String(), int64(r.Revision), r.ObjectID.String(), int64(r.ObjectVersion), r.ObjectCreatedAt.Time(), r.PublishedAt.Time(), installation, creation, attempt, upload).Scan(&matching)
	if err != nil {
		return captureError(err)
	}
	if matching != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}

func (p *InitialRoundBindings) DiscoverInitialRoundBindings(ctx context.Context, r sc.InitialRoundBindingsRequest) (sc.InitialRoundBindingsPlan, error) {
	if err := p.valid(ctx, r); err != nil {
		return nil, err
	}
	r = r.Clone()
	locks, err := captureLocks(r.Initial.Request, nil)
	if err != nil {
		return nil, captureError(err)
	}
	var record skillCaptureRecord
	// The first short transaction discovers the real Skill key. The second
	// takes a new complete union; neither transaction adds a late lock.
	for pass := 0; pass < 2; pass++ {
		id, err := f.NewID[f.TransactionAttempt]()
		if err != nil {
			return nil, unavailable(err)
		}
		cause, err := f.NewJobCause("skill-initial-round", r.Initial.Request.ExecutionID.String(), id.String())
		if err != nil {
			return nil, err
		}
		result := p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := p.store.AcquireAll(ctx, tx, locks); err != nil {
				return captureError(err)
			}
			x, err := p.inTx(ctx, tx, locks)
			if err != nil {
				return err
			}
			stored, err := loadInitialRoundCapture(ctx, x, r)
			if err != nil {
				return err
			}
			if pass == 1 {
				if stored.SourceDigest != record.SourceDigest {
					return fault(f.ConfirmationStale)
				}
				if err = validateInitialRoundCapture(ctx, x, r, stored); err != nil {
					return err
				}
			}
			record = stored
			return ctx.Err()
		})
		if err = commitError(result); err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if pass == 0 {
			locks, err = captureSetLocks(r.Initial.Request, assignmentSkills(record.Source.assignmentSet(), record.Source.InitialSkillID))
			if err != nil {
				return nil, captureError(err)
			}
		}
	}
	return &initialRoundBindingsPlan{owner: p, request: r, source: record.Source, digest: record.SourceDigest, locks: slices.Clone(locks)}, nil
}

func (p *InitialRoundBindings) ReadInitialRoundBindingsInTx(ctx context.Context, tx f.Tx, r sc.InitialRoundBindingsRequest, plan sc.InitialRoundBindingsPlan) (sc.InitialSkillBindings, error) {
	var empty sc.InitialSkillBindings
	if err := p.valid(ctx, r); err != nil {
		return empty, err
	}
	selected, ok := plan.(*initialRoundBindingsPlan)
	if !ok || selected == nil || selected.owner != p || !sameInitialRoundRequest(selected.request, r) {
		return empty, fault(f.Forbidden)
	}
	x, err := p.inTx(ctx, tx, selected.locks)
	if err != nil {
		return empty, err
	}
	stored, err := loadInitialRoundCapture(ctx, x, r)
	if err != nil {
		return empty, err
	}
	if stored.SourceDigest != selected.digest {
		return empty, fault(f.ConfirmationStale)
	}
	if err = validateInitialRoundCapture(ctx, x, r, stored); err != nil {
		return empty, err
	}
	return stored.Source.Result.Clone(), nil
}

var _ sc.InitialRoundBindingsReader = (*InitialRoundBindings)(nil)
