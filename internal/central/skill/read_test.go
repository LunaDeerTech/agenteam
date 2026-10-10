package skill

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type skillReaderProjects struct {
	ProjectPorts
	store   *initializationReadStore
	owner   id.Actor
	project pc.ProjectRef
	err     error
	calls   int
}

func (p *skillReaderProjects) RequireOwnerInTx(_ context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (pc.ProjectAccess, error) {
	p.calls++
	if !p.store.live || p.store.tx != tx || intent != id.Read || project != p.project.ID {
		return pc.ProjectAccess{}, invalid()
	}
	if p.err != nil {
		return pc.ProjectAccess{}, p.err
	}
	if !actor.Equal(p.owner) {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	return pc.NewProjectAccess(actor, p.project, p.project.UpdatedAt)
}
func readerFixture(t *testing.T) (*Service, *initializationReadStore, *skillReaderProjects, id.Actor, initializationRow) {
	t.Helper()
	s, store, _, _, _ := readFixture(t)
	row := stateRow(t)
	actor, e := id.NewHuman(stateID[id.User](111), stateID[id.Session](112))
	if e != nil {
		t.Fatal(e)
	}
	projects := &skillReaderProjects{store: store, owner: actor, project: pc.ProjectRef{ID: row.request.ProjectID, OwnerUserID: stateID[id.User](111), Name: "Reader-Project", NormalizedName: "reader-project", Lifecycle: pc.Active, Version: 1, CreatedAt: row.created, UpdatedAt: row.updated}}
	if e = projects.project.Validate(); e != nil {
		t.Fatal(e)
	}
	s.state().authority.state().projects = projects
	return s, store, projects, actor, row
}
func TestSkillOwnerDirectoryUsesCurrentGrantAndCommittedPublication(t *testing.T) {
	for _, name := range []string{"list", "get", "archived", "wrong_owner", "revoked_session", "wrong_skill", "uninitialized", "tombstone", "corrupt", "unknown_commit"} {
		t.Run(name, func(t *testing.T) {
			s, store, projects, actor, row := readerFixture(t)
			skill := row.skill
			sentinel := fault(f.Forbidden)
			switch name {
			case "archived":
				projects.project.Lifecycle = pc.Archived
				at := projects.project.UpdatedAt
				projects.project.ArchivedAt = &at
			case "wrong_owner":
				actor, _ = id.NewHuman(stateID[id.User](113), stateID[id.Session](114))
			case "revoked_session":
				projects.err = sentinel
			case "wrong_skill":
				skill = stateID[pc.Skill](115)
			case "uninitialized":
				store.row.values = stateValues(t)
			case "tombstone":
				store.published.values[9] = false
			case "corrupt":
				store.published.values[13] = "invalid stored time"
			case "unknown_commit":
				store.unknown = true
			}
			// A malformed row is represented by a driver scan error, not a panic in
			// the controlled positional scanner used by these offline tests.
			if name == "corrupt" {
				store.published.err = fault(f.DependencyUnavailable)
			}
			var got []sc.Metadata
			var e error
			if name == "list" {
				got, e = s.ListSkills(context.Background(), actor, row.request.ProjectID)
			} else {
				var m sc.Metadata
				m, e = s.GetSkill(context.Background(), actor, row.request.ProjectID, skill)
				if e == nil {
					got = []sc.Metadata{m}
				}
			}
			if projects.calls != 1 {
				t.Fatal("current owner not checked exactly once")
			}
			switch name {
			case "list", "get", "archived":
				if e != nil || len(got) != 1 || got[0].ID != row.skill || !got[0].Protected {
					t.Fatal("current immutable directory", e)
				}
			default:
				if e == nil || len(got) != 0 {
					t.Fatal("private/uncommitted row escaped")
				}
				if name == "revoked_session" && (e != sentinel || store.facts != 0) {
					t.Fatal("current gate was substituted or too late")
				}
				if name == "unknown_commit" {
					if _, ok := UnknownAttempt(e); !ok {
						t.Fatal("unknown lost")
					}
				}
			}
		})
	}
}
