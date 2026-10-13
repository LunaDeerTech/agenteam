package skillhttp

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const assignmentBodyLimit = 8 << 10
const assignmentRepresentationLimit = 128 << 10

type assignmentRequestDTO struct {
	SkillID                                         *sc.SkillID      `json:"skill_id,omitempty"`
	Enabled                                         *bool            `json:"enabled,omitempty"`
	AssignmentID                                    *sc.AssignmentID `json:"assignment_id,omitempty"`
	skillPresent, enabledPresent, assignmentPresent bool
}
type assignmentCommandDTO struct {
	Expected       *f.Version            `json:"expected_version"`
	Request        *assignmentRequestDTO `json:"request"`
	Command        *string               `json:"command,omitempty"`
	commandPresent bool
}

func (v *assignmentRequestDTO) UnmarshalJSON(raw []byte) error {
	type plain assignmentRequestDTO
	var next plain
	if err := json.Unmarshal(raw, &next); err != nil {
		return invalidInput()
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return invalidInput()
	}
	_, next.skillPresent = fields["skill_id"]
	_, next.enabledPresent = fields["enabled"]
	_, next.assignmentPresent = fields["assignment_id"]
	*v = assignmentRequestDTO(next)
	return nil
}

func (v *assignmentCommandDTO) UnmarshalJSON(raw []byte) error {
	type plain assignmentCommandDTO
	var next plain
	if err := json.Unmarshal(raw, &next); err != nil {
		return invalidInput()
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return invalidInput()
	}
	_, next.commandPresent = fields["command"]
	*v = assignmentCommandDTO(next)
	return nil
}

// Lookup retains the complete original command, including version and key.
// It never resubmits a mutation and absence is not proof of non-commit.
func decodeAssignment(w http.ResponseWriter, r *http.Request, route assignmentRoute, project i.ProjectID, agent i.AgentID) (f.CommandMeta, skill.AssignmentRequest, error) {
	var zero skill.AssignmentRequest
	if r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return f.CommandMeta{}, zero, invalidInput()
	}
	var keys, types []string
	for name, values := range r.Header {
		switch {
		case strings.EqualFold(name, "Idempotency-Key"):
			keys = append(keys, values...)
		case strings.EqualFold(name, "Content-Type"):
			types = append(types, values...)
		case strings.EqualFold(name, "Content-Encoding"):
			return f.CommandMeta{}, zero, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	if len(keys) != 1 || f.IdempotencyKey(keys[0]).Validate() != nil {
		return f.CommandMeta{}, zero, invalidInput()
	}
	if len(types) != 1 {
		return f.CommandMeta{}, zero, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
	}
	var wire assignmentCommandDTO
	if err := httpapi.DecodeJSON(w, r, &wire, assignmentBodyLimit); err != nil {
		return f.CommandMeta{}, zero, err
	}
	if wire.Expected == nil || wire.Expected.Validate() != nil || wire.Request == nil {
		return f.CommandMeta{}, zero, invalidInput()
	}
	kind := route.kind
	if kind == "lookup" {
		if wire.Command == nil || *wire.Command != "skill.assign" && *wire.Command != "skill.remove" {
			return f.CommandMeta{}, zero, invalidInput()
		}
		kind = strings.TrimPrefix(*wire.Command, "skill.")
	} else if wire.commandPresent {
		return f.CommandMeta{}, zero, invalidInput()
	}
	input := skill.AssignmentRequest{ProjectID: project, AgentID: agent, SkillID: wire.Request.SkillID, Enabled: wire.Request.Enabled, AssignmentID: wire.Request.AssignmentID}
	if kind == "assign" {
		if input.SkillID == nil || input.SkillID.Validate() != nil || input.Enabled == nil || wire.Request.assignmentPresent {
			return f.CommandMeta{}, zero, invalidInput()
		}
	} else if kind != "remove" || input.AssignmentID == nil || input.AssignmentID.Validate() != nil || wire.Request.skillPresent || wire.Request.enabledPresent {
		return f.CommandMeta{}, zero, invalidInput()
	}
	meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0]), ExpectedVersion: wire.Expected}
	if meta.Validate() != nil {
		return f.CommandMeta{}, zero, invalidInput()
	}
	return meta, input, nil
}

func validateAssignmentMutation(meta f.CommandMeta, input skill.AssignmentRequest, out sc.AssignmentMutation) error {
	bad := func() error { return f.NewFault(f.DependencyUnavailable, f.Unknown) }
	if out.Validate() != nil || meta.ExpectedVersion == nil || out.ProjectID != input.ProjectID || out.AgentID != input.AgentID {
		return bad()
	}
	expected := *meta.ExpectedVersion
	if out.Changed {
		if expected == math.MaxInt64 || out.AgentVersion != expected+1 {
			return bad()
		}
	} else if out.AgentVersion != expected || out.Runtime != nil {
		return bad()
	}
	if input.SkillID != nil {
		if input.Enabled == nil || out.Assignment == nil || out.Assignment.SkillID != *input.SkillID || out.Assignment.Enabled != *input.Enabled {
			return bad()
		}
		if out.Changed && out.Assignment.Sequence != out.AssignmentSequence {
			return bad()
		}
		if (out.Changed && *input.Enabled) != (out.Runtime != nil) {
			return bad()
		}
	} else if out.Assignment != nil || out.Runtime != nil {
		return bad()
	}
	return nil
}

func encodeAssignments(ctx context.Context, project i.ProjectID, agent i.AgentID, query skill.OwnerAssignmentQuery, page skill.OwnerAssignmentPage) ([]byte, error) {
	if project.Validate() != nil || agent.Validate() != nil || query.Limit < 1 || query.Limit > 100 || page.AgentVersion.Validate() != nil || page.Sequence.Validate() != nil || page.Items == nil || len(page.Items) > query.Limit || len(page.NextCursor) > cursor.MaxTokenBytes || page.NextCursor != "" && len(page.Items) != query.Limit {
		return nil, badProjection()
	}
	previous := ""
	seen := make(map[sc.AssignmentID]bool, len(page.Items))
	for _, item := range page.Items {
		if item.Validate() != nil || item.ProjectID != project || item.AgentID != agent || item.Sequence > page.Sequence || item.SkillID.String() <= previous || seen[item.ID] {
			return nil, badProjection()
		}
		previous, seen[item.ID] = item.SkillID.String(), true
	}
	return encodeManagement(ctx, struct {
		ProjectID    i.ProjectID               `json:"project_id"`
		AgentID      i.AgentID                 `json:"agent_id"`
		AgentVersion f.Version                 `json:"agent_version"`
		Sequence     f.Version                 `json:"assignment_sequence"`
		Items        []sc.AgentSkillAssignment `json:"items"`
		NextCursor   string                    `json:"next_cursor,omitempty"`
	}{project, agent, page.AgentVersion, page.Sequence, page.Items, page.NextCursor}, assignmentRepresentationLimit)
}
