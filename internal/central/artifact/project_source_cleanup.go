package artifact

import (
	"context"
	"encoding/hex"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These are Artifact's original source command facts, read together with its
// work row under Project EX. Object lease state is owned by Object alone.
type artifactCleanupSourceCommand struct {
	Key, Project, Path, State, Lease string
	Source                           json.RawMessage
}

func (work artifactCleanupWork) closedSource(project i.ProjectID) bool {
	c := work.SourceCommand
	if work.Project != project.String() || work.Source != project.String() || work.Kind != "creation" ||
		!work.Joined || !work.SourceJoined || !work.SourceUse || !artifactTechnicalID(work.Lease) || c == nil ||
		c.Project != work.Project || c.Path != "source" || c.Lease != work.Lease {
		return false
	}
	command, err := commandIdentity(project, f.IdempotencyKey(c.Key), "create")
	if err != nil || hex.EncodeToString(digestRaw(digestBytes([]byte(command.Canonical())))) != work.Command {
		return false
	}
	switch c.State {
	case "pending", "completed", "cancel_requested", "cancelled", "deleted":
	default:
		return false
	}
	source, err := decodeSource(c.Source)
	if err != nil {
		return false
	}
	d := source.Details()
	return d.Reference.Details().Kind == oc.ArtifactFile && d.Reference.Details().ProjectID == project &&
		d.Owner.Details().Kind == oc.Artifact && d.Owner.Details().ProjectID == project.String()
}

// Only the final physical-completed cleanup transaction consumes this batch.
// The caller has rechecked every original instance/return and foreign death
// proof. Work and its deleted source command disappear atomically; no cleared
// lease pointer or missing row is used as evidence of a returned source call.
func retireCleanupSourceCalls(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, work []artifactCleanupWork) error {
	if len(work) == 0 {
		return nil
	}
	type selectedSource struct {
		ID      string `json:"id"`
		Command string `json:"command"`
		Lease   string `json:"lease"`
	}
	selected := make([]selectedSource, 0, len(work))
	for _, row := range work {
		if !row.closedSource(project) || row.SourceCommand.State != "deleted" {
			return failure(f.VersionConflict, nil)
		}
		selected = append(selected, selectedSource{row.ID, row.Command, row.Lease})
	}
	raw, err := json.Marshal(selected)
	if err != nil {
		return unavailable(err)
	}
	for _, query := range []string{
		`DELETE FROM agenteam_artifact.project_work w USING jsonb_to_recordset($2::jsonb) AS selected(id uuid,command text,lease uuid) WHERE w.project_id=$1 AND w.id=selected.id AND w.command_hash=decode(selected.command,'hex') AND w.source_lease_id=selected.lease AND w.source_project_id=$1 AND w.kind='creation' AND w.joined_at IS NOT NULL AND w.source_joined_at IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_artifact.commands c WHERE c.command_hash=w.command_hash AND c.project_id=w.project_id AND c.path='source' AND c.state='deleted' AND c.source_lease_id=w.source_lease_id AND c.source->>'project'=w.source_project_id::text)`,
		`DELETE FROM agenteam_artifact.commands c USING jsonb_to_recordset($2::jsonb) AS selected(id uuid,command text,lease uuid) WHERE c.project_id=$1::text::uuid AND c.command_hash=decode(selected.command,'hex') AND c.source_lease_id=selected.lease AND c.path='source' AND c.state='deleted' AND c.source->>'project'=$1::text`,
	} {
		tag, err := x.Exec(ctx, query, project.String(), raw)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != int64(len(selected)) {
			return failure(f.VersionConflict, nil)
		}
	}
	return ctx.Err()
}
