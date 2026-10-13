package skill

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type assignmentDirectoryReader interface {
	ListAssignments(context.Context, i.Actor, i.ProjectID, i.AgentID) (AssignmentDirectory, error)
}

// OwnerAssignments pages one fully validated, currently authorized assignment
// set. It stores no second configuration and does not interpret grants as
// execution bindings. Every page observes its own current Agent version.
type OwnerAssignments struct {
	commands assignmentDirectoryReader
	keys     cursor.Keyring
}

type OwnerAssignmentQuery struct {
	Limit  int
	Cursor string
}

type OwnerAssignmentPage struct {
	AgentVersion f.Version
	Sequence     f.Version
	Items        []sc.AgentSkillAssignment
	NextCursor   string
}

func NewOwnerAssignments(commands *AssignmentCommands, keys cursor.Keyring) (*OwnerAssignments, error) {
	if commands == nil || commands.deps.Service.state() == nil || keys.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &OwnerAssignments{commands: commands, keys: keys}, nil
}

func assignmentDirectoryBinding(actor i.Actor, project i.ProjectID, agent i.AgentID, limit int) (cursor.Binding, error) {
	if actor.Validate() != nil || actor.Details().Kind != i.Human || project.Validate() != nil || agent.Validate() != nil || limit < 1 || limit > 100 {
		return cursor.Binding{}, invalid()
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, err
	}
	raw, err := json.Marshal(struct {
		View, User, Agent string
		Limit             int
	}{"skill-owner-assignments-v1", actor.Details().UserID, agent.String(), limit})
	if err != nil {
		return cursor.Binding{}, unavailable(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, err
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: "skill_id:asc"}, nil
}

func (r *OwnerAssignments) List(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID, query OwnerAssignmentQuery) (OwnerAssignmentPage, error) {
	var zero OwnerAssignmentPage
	if r == nil || nilPort(r.commands) || r.keys.Validate() != nil {
		return zero, fault(f.DependencyUnbound)
	}
	if ctx == nil || len(query.Cursor) > cursor.MaxTokenBytes {
		return zero, invalid()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	binding, err := assignmentDirectoryBinding(actor, project, agent, query.Limit)
	if err != nil {
		return zero, err
	}
	var position cursor.Position
	var after string
	if query.Cursor != "" {
		position, err = r.keys.Verify(query.Cursor, binding)
		if err != nil {
			return zero, err
		}
		if position.OrderGeneration == nil || len(position.Scalars) != 1 || position.Scalars[0].Kind() != "uuid" {
			return zero, fault(f.CursorInvalid)
		}
		after = position.Scalars[0].Value()
		parsed, err := f.ParseID[pc.Skill](after)
		if err != nil || parsed.String() != after {
			return zero, fault(f.CursorInvalid)
		}
	}
	current, err := r.commands.ListAssignments(ctx, actor, project, agent)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	state := assignmentSet{Project: project, Agent: agent, Sequence: current.Sequence, Items: current.Items}
	if current.AgentVersion.Validate() != nil || state.validate() != nil {
		return zero, unavailable(nil)
	}
	if position.OrderGeneration != nil && *position.OrderGeneration != int64(current.Sequence) {
		return zero, fault(f.CursorStale)
	}
	start := 0
	if after != "" {
		found := false
		for n, item := range current.Items {
			if item.SkillID.String() == after {
				start, found = n+1, true
				break
			}
		}
		if !found {
			return zero, fault(f.CursorInvalid)
		}
	}
	end := min(start+query.Limit, len(current.Items))
	items := slices.Clone(current.Items[start:end])
	if items == nil {
		items = []sc.AgentSkillAssignment{}
	}
	out := OwnerAssignmentPage{AgentVersion: current.AgentVersion, Sequence: current.Sequence, Items: items}
	if end < len(current.Items) {
		last, err := cursor.UUID(items[len(items)-1].SkillID.String())
		if err != nil {
			return zero, unavailable(err)
		}
		generation := int64(current.Sequence)
		out.NextCursor, err = r.keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{last}, OrderGeneration: &generation})
		if err != nil {
			return zero, err
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return out, nil
}
