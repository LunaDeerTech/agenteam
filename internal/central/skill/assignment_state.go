package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// A current set is an exact sorted set, including disabled configuration. Old
// grants remain immutable history after removal and never silently reactivate.
type assignmentSet struct {
	Project  i.ProjectID               `json:"project_id"`
	Agent    i.AgentID                 `json:"agent_id"`
	Sequence f.Version                 `json:"sequence"`
	Items    []sc.AgentSkillAssignment `json:"items"`
}

func (s assignmentSet) validate() error {
	if s.Project.Validate() != nil || s.Agent.Validate() != nil || s.Sequence.Validate() != nil || s.Items == nil {
		return unavailable(nil)
	}
	ids := make(map[sc.AssignmentID]bool, len(s.Items))
	for n, a := range s.Items {
		if a.Validate() != nil || a.ProjectID != s.Project || a.AgentID != s.Agent || a.Sequence > s.Sequence || ids[a.ID] || n > 0 && s.Items[n-1].SkillID.String() >= a.SkillID.String() {
			return unavailable(nil)
		}
		ids[a.ID] = true
	}
	return nil
}
func assignmentDecode(raw []byte, out any, limit int) error {
	if len(raw) < 2 || len(raw) > limit {
		return unavailable(nil)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return unavailable(nil)
	}
	_, canonical, err := captureDigest(out)
	if err != nil || !bytes.Equal(raw, canonical) {
		return unavailable(err)
	}
	return nil
}

func loadAssignmentSet(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID) (assignmentSet, error) {
	var zero assignmentSet
	head, err := loadAgentInitialization(ctx, x, project, agent)
	if err != nil {
		return zero, err
	}
	if head == nil {
		return zero, fault(f.DependencyUnbound)
	}
	var seq int64
	var digest string
	var raw []byte
	err = x.QueryRow(ctx, `SELECT c.assignment_sequence,c.state_digest,h.state_data FROM agenteam_skill.assignment_current c JOIN agenteam_skill.assignment_changes h ON h.id=c.change_id AND h.project_id=c.project_id AND h.agent_id=c.agent_id AND h.assignment_sequence=c.assignment_sequence AND h.state_digest=c.state_digest WHERE c.project_id=$1 AND c.agent_id=$2`, project.String(), agent.String()).Scan(&seq, &digest, &raw)
	state := assignmentSet{Project: project, Agent: agent, Sequence: 1, Items: []sc.AgentSkillAssignment{}}
	if errors.Is(err, pgx.ErrNoRows) {
		if head.assignment != nil {
			state.Items = append(state.Items, sc.AgentSkillAssignment{ID: *head.assignment, ProjectID: project, AgentID: agent, SkillID: head.skill, Enabled: true, Sequence: 1, CreatedAt: head.created})
		}
	} else if err != nil {
		return zero, unavailable(err)
	} else {
		if err = assignmentDecode(raw, &state, 1<<20); err != nil {
			return zero, err
		}
		d, _, e := captureDigest(state)
		if e != nil || seq < 2 || state.Sequence != f.Version(seq) || state.Project != project || state.Agent != agent || d != f.Digest(digest) {
			return zero, unavailable(e)
		}
	}
	if err = state.validate(); err != nil {
		return zero, err
	}
	// No missing or extra live grant can be hidden behind a well-formed JSON
	// head. This also detects an absent current head after a real mutation.
	rows, err := x.Query(ctx, `SELECT id::text,skill_id::text,enabled,assignment_sequence,created_at FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND removed_at IS NULL ORDER BY skill_id`, project.String(), agent.String())
	if err != nil {
		return zero, unavailable(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, skill string
		var enabled bool
		var sequence int64
		var created time.Time
		if err = rows.Scan(&id, &skill, &enabled, &sequence, &created); err != nil {
			return zero, unavailable(err)
		}
		if n >= len(state.Items) {
			return zero, unavailable(nil)
		}
		a := state.Items[n]
		n++
		if id != a.ID.String() || skill != a.SkillID.String() || enabled != a.Enabled || sequence != int64(a.Sequence) || !created.Equal(a.CreatedAt.Time()) {
			return zero, unavailable(nil)
		}
	}
	if err = rows.Err(); err != nil {
		return zero, unavailable(err)
	}
	if n != len(state.Items) {
		return zero, unavailable(nil)
	}
	return state, ctx.Err()
}

func assignmentSkills(state assignmentSet, initial sc.SkillID) []sc.SkillID {
	out := []sc.SkillID{initial}
	for _, a := range state.Items {
		if a.Enabled {
			out = append(out, a.SkillID)
		}
	}
	slices.SortFunc(out, func(a, b sc.SkillID) int { return bytes.Compare([]byte(a.String()), []byte(b.String())) })
	return slices.Compact(out)
}

// Metadata and immutable revision are loaded together under the Skill gate.
// This path is shared by new grants and new Execution capture; it performs no
// Object I/O and does not upgrade old Execution bindings to the latest revision.
func loadAssignmentRevision(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, skill sc.SkillID) (sc.Metadata, sc.RevisionMetadata, error) {
	init, err := loadInitialization(ctx, x, project)
	if err != nil {
		return sc.Metadata{}, sc.RevisionMetadata{}, err
	}
	if init == nil || init.phase != initializationPublished {
		return sc.Metadata{}, sc.RevisionMetadata{}, fault(f.InvalidState)
	}
	if init.skill == skill {
		return loadPublished(ctx, x, *init)
	}
	r, err := loadInstallationSkill(ctx, x, project, skill)
	if err != nil {
		return sc.Metadata{}, sc.RevisionMetadata{}, err
	}
	if r == nil {
		return sc.Metadata{}, sc.RevisionMetadata{}, fault(f.NotFound)
	}
	return loadInstalled(ctx, x, *r)
}
