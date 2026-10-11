package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type skillCaptureSource struct {
	Result                sc.InitialSkillBindings  `json:"result"`
	CreationCommand       string                   `json:"creation_command"`
	PlanRevision          f.Version                `json:"plan_revision"`
	RequestBinding        f.Digest                 `json:"request_binding"`
	AddSkillsEnabled      bool                     `json:"add_skills_enabled"`
	InitialSkillID        sc.SkillID               `json:"initial_skill_id"`
	ObservedRevision      f.Revision               `json:"observed_revision"`
	HeadCreatedAt         f.Instant                `json:"head_created_at"`
	CreationID            pc.CreationID            `json:"creation_id"`
	InitializationKey     f.IdempotencyKey         `json:"initialization_key"`
	InitializationVersion f.Version                `json:"initialization_version"`
	InitializationDigest  f.Digest                 `json:"initialization_digest"`
	Metadata              sc.Metadata              `json:"metadata"`
	RevisionID            sc.RevisionID            `json:"revision_id"`
	ObjectID              oc.ObjectID              `json:"object_id"`
	ObjectVersion         f.Version                `json:"object_version"`
	ObjectCreatedAt       f.Instant                `json:"object_created_at"`
	PublishedAt           f.Instant                `json:"published_at"`
	PackageDigest         f.Digest                 `json:"package_digest"`
	ManifestDigest        f.Digest                 `json:"manifest_digest"`
	Assignments           *skillCaptureAssignments `json:"assignments,omitempty"`
}

// The immutable initialized source remains the format-1 byte contract.
// Current changed sets extend it explicitly; history never substitutes them.
func loadSkillCaptureSource(ctx context.Context, x postgres.SQLExecutor, r sc.SkillCaptureRequest, expected sc.SkillID) (skillCaptureSource, error) {
	var out skillCaptureSource
	head, err := loadAgentInitialization(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return out, err
	}
	if head == nil {
		return out, fault(f.DependencyUnbound)
	}
	if head.skill != expected {
		return out, fault(f.ConfirmationStale)
	}
	publication, err := loadInitialization(ctx, x, r.ProjectID)
	if err != nil {
		return out, err
	}
	if publication == nil || publication.phase != initializationPublished {
		return out, fault(f.InvalidState)
	}
	if publication.skill != head.skill || publication.bundle.revision != head.revision {
		return out, unavailable(nil)
	}
	meta, revision, err := loadPublished(ctx, x, *publication)
	if err != nil {
		return out, err
	}
	manifest, err := revision.Manifest.Digest()
	if err != nil {
		return out, unavailable(err)
	}
	result := sc.InitialSkillBindings{Request: r, AssignmentSequence: head.sequence, Bindings: []sc.SkillBinding{}}
	if head.enabled {
		if head.assignment == nil {
			return out, unavailable(nil)
		}
		result.Bindings = append(result.Bindings, sc.SkillBinding{SkillID: meta.ID, RevisionID: revision.ID, Revision: revision.Revision, AssignmentID: *head.assignment, AssignmentSequence: head.sequence, Name: revision.Name, Description: revision.Description, PackageSHA256: revision.PackageSHA256, EntryPath: revision.EntryPath})
	}
	if result.Validate() != nil {
		return out, unavailable(nil)
	}
	return skillCaptureSource{Result: result, CreationCommand: head.command, PlanRevision: head.planRevision, RequestBinding: head.binding, AddSkillsEnabled: head.enabled, InitialSkillID: head.skill, ObservedRevision: head.revision, HeadCreatedAt: head.created,
		CreationID: publication.request.CreationID, InitializationKey: publication.request.InitializationKey, InitializationVersion: publication.version, InitializationDigest: publication.semantic, Metadata: meta,
		RevisionID: revision.ID, ObjectID: revision.Object.ID, ObjectVersion: revision.Object.Version, ObjectCreatedAt: revision.Object.CreatedAt, PublishedAt: revision.PublishedAt, PackageDigest: revision.PackageSHA256, ManifestDigest: manifest}, ctx.Err()
}

type skillCaptureRecord struct {
	Format       int                `json:"format"`
	Attempt      f.Digest           `json:"attempt_binding"`
	SourceDigest f.Digest           `json:"source_digest"`
	Source       skillCaptureSource `json:"source"`
}

// Only changed sets carry this extension. Nil preserves every format-1 byte.
type skillCaptureAssignments struct {
	State     assignmentSet        `json:"state"`
	Revisions []assignmentRevision `json:"revisions"`
}

func (s skillCaptureSource) format() int {
	if s.Assignments != nil {
		return 2
	}
	return 1
}

func (s skillCaptureSource) assignmentSet() assignmentSet {
	if s.Assignments != nil {
		return s.Assignments.State
	}
	out := assignmentSet{Project: s.Result.Request.ProjectID, Agent: s.Result.Request.AgentID, Sequence: 1, Items: []sc.AgentSkillAssignment{}}
	for _, b := range s.Result.Bindings {
		out.Items = append(out.Items, sc.AgentSkillAssignment{ID: b.AssignmentID, ProjectID: out.Project, AgentID: out.Agent, SkillID: b.SkillID, Enabled: true, Sequence: b.AssignmentSequence, CreatedAt: s.HeadCreatedAt})
	}
	return out
}

func sameCaptureAssignments(a, b assignmentSet) bool {
	return a.Project == b.Project && a.Agent == b.Agent && a.Sequence == b.Sequence && slices.Equal(a.Items, b.Items)
}

// Seq 1 keeps its original representation, but only while both the immutable
// mutation history and current pointer prove that no change has occurred.
// Changed sets use the assignment owner's complete live-row reader.
func loadCaptureAssignments(ctx context.Context, x postgres.SQLExecutor, r sc.SkillCaptureRequest, head *agentInitializationRecord) (assignmentSet, error) {
	var zero assignmentSet
	var current, changed bool
	var live, initial int64
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.assignment_current WHERE project_id=$1 AND agent_id=$2),EXISTS(SELECT 1 FROM agenteam_skill.assignment_changes WHERE project_id=$1 AND agent_id=$2),(SELECT count(*) FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND removed_at IS NULL),(SELECT count(*) FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND assignment_sequence=1 AND removed_at IS NULL)`, r.ProjectID.String(), r.AgentID.String()).Scan(&current, &changed, &live, &initial)
	if err != nil {
		return zero, unavailable(err)
	}
	if current {
		return loadAssignmentSet(ctx, x, r.ProjectID, r.AgentID)
	}
	expected := int64(0)
	if head.enabled {
		expected = 1
	}
	if changed || live != expected || initial != expected {
		return zero, unavailable(nil)
	}
	out := assignmentSet{Project: r.ProjectID, Agent: r.AgentID, Sequence: 1, Items: []sc.AgentSkillAssignment{}}
	if head.assignment != nil {
		out.Items = append(out.Items, sc.AgentSkillAssignment{ID: *head.assignment, ProjectID: r.ProjectID, AgentID: r.AgentID, SkillID: head.skill, Enabled: true, Sequence: 1, CreatedAt: head.created})
	}
	return out, ctx.Err()
}

func loadCurrentCaptureSource(ctx context.Context, x postgres.SQLExecutor, r sc.SkillCaptureRequest, initial sc.SkillID, expected assignmentSet) (skillCaptureSource, error) {
	var zero skillCaptureSource
	head, err := loadAgentInitialization(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return zero, err
	}
	if head == nil {
		return zero, fault(f.DependencyUnbound)
	}
	current, err := loadCaptureAssignments(ctx, x, r, head)
	if err != nil {
		return zero, err
	}
	// Compare before reading any Skill: a concurrent grant must not cause an
	// unplanned resource read under the previous discovery's held-lock set.
	if head.skill != initial || !sameCaptureAssignments(current, expected) {
		return zero, fault(f.ConfirmationStale)
	}
	out, err := loadSkillCaptureSource(ctx, x, r, initial)
	if err != nil {
		return zero, err
	}
	if current.Sequence == 1 {
		return out, nil
	}
	current.Items = slices.Clone(current.Items)
	out.Assignments = &skillCaptureAssignments{State: current, Revisions: []assignmentRevision{}}
	out.Result.AssignmentSequence = current.Sequence
	out.Result.Bindings = []sc.SkillBinding{}
	for _, a := range current.Items {
		if !a.Enabled {
			continue
		}
		_, revision, e := loadAssignmentRevision(ctx, x, r.ProjectID, a.SkillID)
		if e != nil {
			return zero, e
		}
		fixed, e := assignmentRevisionFrom(revision)
		if e != nil {
			return zero, e
		}
		out.Assignments.Revisions = append(out.Assignments.Revisions, fixed)
		out.Result.Bindings = append(out.Result.Bindings, bindingForAssignment(a, fixed))
	}
	if err = out.validate(); err != nil {
		return zero, err
	}
	return out, ctx.Err()
}

func (s skillCaptureSource) validate() error {
	if s.Result.Validate() != nil || s.InitialSkillID.Validate() != nil || s.Metadata.Validate() != nil || s.ObjectID.Validate() != nil {
		return unavailable(nil)
	}
	if s.Assignments == nil {
		if s.Result.AssignmentSequence != 1 || len(s.Result.Bindings) > 1 || s.AddSkillsEnabled != (len(s.Result.Bindings) == 1) {
			return unavailable(nil)
		}
		for _, b := range s.Result.Bindings {
			if b.SkillID != s.InitialSkillID || b.RevisionID != s.RevisionID || b.Revision != s.ObservedRevision || b.Name != s.Metadata.Name || b.Description != s.Metadata.Description || b.PackageSHA256 != s.PackageDigest {
				return unavailable(nil)
			}
		}
		return nil
	}
	a := s.Assignments
	if a.State.validate() != nil || a.State.Sequence < 2 || a.State.Sequence != s.Result.AssignmentSequence || a.State.Project != s.Result.Request.ProjectID || a.State.Agent != s.Result.Request.AgentID || a.Revisions == nil {
		return unavailable(nil)
	}
	n := 0
	for _, grant := range a.State.Items {
		if !grant.Enabled {
			continue
		}
		if n >= len(a.Revisions) || n >= len(s.Result.Bindings) {
			return unavailable(nil)
		}
		r := a.Revisions[n]
		if r.validate() != nil || r.ProjectID != a.State.Project || r.SkillID != grant.SkillID || bindingForAssignment(grant, r) != s.Result.Bindings[n] {
			return unavailable(nil)
		}
		n++
	}
	if n != len(a.Revisions) || n != len(s.Result.Bindings) {
		return unavailable(nil)
	}
	return nil
}

func (s skillCaptureSource) bindingObject(b sc.SkillBinding) (oc.ObjectID, error) {
	if err := s.validate(); err != nil {
		return oc.ObjectID{}, err
	}
	for n, expected := range s.Result.Bindings {
		if expected == b {
			if s.Assignments == nil {
				return s.ObjectID, nil
			}
			return s.Assignments.Revisions[n].ObjectID, nil
		}
	}
	return oc.ObjectID{}, unavailable(nil)
}

func verifySkillCapture(ctx context.Context, x postgres.SQLExecutor, p *initialBindingsPlan) (bool, error) {
	var project, agent, attempt, source string
	var sequence, count int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,assignment_sequence,attempt_binding,source_digest,binding_count,record FROM agenteam_skill.execution_binding_heads WHERE execution_id=$1`, p.request.ExecutionID.String()).Scan(&project, &agent, &sequence, &attempt, &source, &count, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, unavailable(err)
	}
	if project != p.request.ProjectID.String() || agent != p.request.AgentID.String() || sequence != int64(p.source.Result.AssignmentSequence) || attempt != string(p.attempt) || source != string(p.digest) || count != int64(len(p.source.Result.Bindings)) {
		return false, fault(f.ConfirmationStale)
	}
	var stored skillCaptureRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 256<<10 || decoder.Decode(&stored) != nil || decoder.Decode(new(any)) != io.EOF {
		return false, unavailable(nil)
	}
	expected := skillCaptureRecord{p.source.format(), p.attempt, p.digest, p.source}
	a, _, e := captureDigest(stored)
	if e != nil {
		return false, e
	}
	b, _, e := captureDigest(expected)
	if e != nil {
		return false, e
	}
	if a != b {
		return false, unavailable(nil)
	}
	var total int64
	err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.execution_bindings WHERE execution_id=$1`, p.request.ExecutionID.String()).Scan(&total)
	if err != nil {
		return false, unavailable(err)
	}
	if total != count {
		return false, unavailable(nil)
	}
	for _, binding := range p.source.Result.Bindings {
		object, e := p.source.bindingObject(binding)
		if e != nil {
			return false, e
		}
		var matching int64
		err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.execution_bindings WHERE execution_id=$1 AND project_id=$2 AND agent_id=$3 AND assignment_id=$4 AND assignment_sequence=$5 AND skill_id=$6 AND revision_id=$7 AND revision=$8 AND object_id=$9`, p.request.ExecutionID.String(), project, agent, binding.AssignmentID.String(), int64(binding.AssignmentSequence), binding.SkillID.String(), binding.RevisionID.String(), int64(binding.Revision), object.String()).Scan(&matching)
		if err != nil {
			return false, unavailable(err)
		}
		if matching != 1 {
			return false, unavailable(nil)
		}
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}

func saveSkillCapture(ctx context.Context, x postgres.SQLExecutor, p *initialBindingsPlan) error {
	if err := p.source.validate(); err != nil {
		return err
	}
	found, err := verifySkillCapture(ctx, x, p)
	if err != nil || found {
		return err
	}
	record := skillCaptureRecord{p.source.format(), p.attempt, p.digest, p.source}
	_, raw, err := captureDigest(record)
	if err != nil {
		return err
	}
	if len(raw) > 256<<10 {
		return fault(f.PayloadTooLarge)
	}
	r := p.request
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.execution_binding_heads(execution_id,project_id,agent_id,assignment_sequence,attempt_binding,source_digest,binding_count,record,captured_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp())`, r.ExecutionID.String(), r.ProjectID.String(), r.AgentID.String(), int64(p.source.Result.AssignmentSequence), string(p.attempt), string(p.digest), len(p.source.Result.Bindings), raw)
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	for _, b := range p.source.Result.Bindings {
		object, e := p.source.bindingObject(b)
		if e != nil {
			return e
		}
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_skill.execution_bindings(execution_id,project_id,agent_id,skill_id,revision_id,revision,assignment_id,assignment_sequence,object_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.ExecutionID.String(), r.ProjectID.String(), r.AgentID.String(), b.SkillID.String(), b.RevisionID.String(), int64(b.Revision), b.AssignmentID.String(), int64(b.AssignmentSequence), object.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
	}
	found, err = verifySkillCapture(ctx, x, p)
	if err != nil {
		return err
	}
	if !found {
		return unavailable(nil)
	}
	return nil
}
