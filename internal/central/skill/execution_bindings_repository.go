package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type skillCaptureSource struct {
	Result                sc.InitialSkillBindings `json:"result"`
	CreationCommand       string                  `json:"creation_command"`
	PlanRevision          f.Version               `json:"plan_revision"`
	RequestBinding        f.Digest                `json:"request_binding"`
	AddSkillsEnabled      bool                    `json:"add_skills_enabled"`
	InitialSkillID        sc.SkillID              `json:"initial_skill_id"`
	ObservedRevision      f.Revision              `json:"observed_revision"`
	HeadCreatedAt         f.Instant               `json:"head_created_at"`
	CreationID            pc.CreationID           `json:"creation_id"`
	InitializationKey     f.IdempotencyKey        `json:"initialization_key"`
	InitializationVersion f.Version               `json:"initialization_version"`
	InitializationDigest  f.Digest                `json:"initialization_digest"`
	Metadata              sc.Metadata             `json:"metadata"`
	RevisionID            sc.RevisionID           `json:"revision_id"`
	ObjectID              oc.ObjectID             `json:"object_id"`
	ObjectVersion         f.Version               `json:"object_version"`
	ObjectCreatedAt       f.Instant               `json:"object_created_at"`
	PublishedAt           f.Instant               `json:"published_at"`
	PackageDigest         f.Digest                `json:"package_digest"`
	ManifestDigest        f.Digest                `json:"manifest_digest"`
}

// Current production assignments are the actual initialized set. This strict
// reader never treats an absent head, an extra/unrecognized assignment, or a
// disabled default as an uninitialized empty catalogue. Future assignment
// writers must extend this reader's canonical source contract with them.
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
	expected := skillCaptureRecord{1, p.attempt, p.digest, p.source}
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
		var matching int64
		err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.execution_bindings WHERE execution_id=$1 AND project_id=$2 AND agent_id=$3 AND assignment_id=$4 AND assignment_sequence=$5 AND skill_id=$6 AND revision_id=$7 AND revision=$8 AND object_id=$9`, p.request.ExecutionID.String(), project, agent, binding.AssignmentID.String(), int64(binding.AssignmentSequence), binding.SkillID.String(), binding.RevisionID.String(), int64(binding.Revision), p.source.ObjectID.String()).Scan(&matching)
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
	found, err := verifySkillCapture(ctx, x, p)
	if err != nil || found {
		return err
	}
	record := skillCaptureRecord{1, p.attempt, p.digest, p.source}
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
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_skill.execution_bindings(execution_id,project_id,agent_id,skill_id,revision_id,revision,assignment_id,assignment_sequence,object_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.ExecutionID.String(), r.ProjectID.String(), r.AgentID.String(), b.SkillID.String(), b.RevisionID.String(), int64(b.Revision), b.AssignmentID.String(), int64(b.AssignmentSequence), p.source.ObjectID.String())
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
