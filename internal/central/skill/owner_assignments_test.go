package skill

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type ownerAssignmentReadControl struct {
	current AssignmentDirectory
	calls   int
	err     error
}

func (c *ownerAssignmentReadControl) ListAssignments(context.Context, i.Actor, i.ProjectID, i.AgentID) (AssignmentDirectory, error) {
	c.calls++
	return c.current, c.err
}

func TestOwnerAssignmentsCursorAndCurrentGeneration(t *testing.T) {
	actor, _ := i.NewHuman(stateID[i.User](801), stateID[i.Session](802))
	project, agent := stateID[i.Project](803), stateID[i.Agent](804)
	at, _ := f.NewInstant(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	control := &ownerAssignmentReadControl{current: AssignmentDirectory{AgentVersion: 5, Sequence: 3, Items: []sc.AgentSkillAssignment{
		{ID: stateID[sc.Assignment](810), ProjectID: project, AgentID: agent, SkillID: stateID[pc.Skill](820), Enabled: false, Sequence: 2, CreatedAt: at},
		{ID: stateID[sc.Assignment](811), ProjectID: project, AgentID: agent, SkillID: stateID[pc.Skill](821), Enabled: true, Sequence: 3, CreatedAt: at},
	}}}
	r := &OwnerAssignments{commands: control, keys: catalogTestKeys(t)}
	first, err := r.List(context.Background(), actor, project, agent, OwnerAssignmentQuery{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].Enabled || first.NextCursor == "" || first.AgentVersion != 5 {
		t.Fatal("first current page omitted disabled grant", err)
	}
	control.current.AgentVersion = 6 // An unrelated Agent configuration edit is not an assignment generation.
	last, err := r.List(context.Background(), actor, project, agent, OwnerAssignmentQuery{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(last.Items) != 1 || last.Items[0].SkillID != control.current.Items[1].SkillID || last.NextCursor != "" || last.AgentVersion != 6 {
		t.Fatal("signed continuation lost current version or final item", err)
	}
	for _, mode := range []string{"agent", "project", "user", "limit"} {
		t.Run(mode, func(t *testing.T) {
			a, p, g, q := actor, project, agent, OwnerAssignmentQuery{Limit: 1, Cursor: first.NextCursor}
			switch mode {
			case "agent":
				g = stateID[i.Agent](900)
			case "project":
				p = stateID[i.Project](900)
			case "user":
				a, _ = i.NewHuman(stateID[i.User](900), stateID[i.Session](901))
			case "limit":
				q.Limit = 2
			}
			before := control.calls
			if _, err := r.List(context.Background(), a, p, g, q); err == nil || control.calls != before {
				t.Fatal("cursor escaped its exact query binding")
			}
		})
	}
	control.current.Sequence = 4
	_, err = r.List(context.Background(), actor, project, agent, OwnerAssignmentQuery{Limit: 1, Cursor: first.NextCursor})
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != f.CursorStale {
		t.Fatal("changed assignment set reused an old page", err)
	}
	control.current.Items = []sc.AgentSkillAssignment{}
	empty, err := r.List(context.Background(), actor, project, agent, OwnerAssignmentQuery{Limit: 25})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("empty current set was not an honest complete page", err)
	}
}
