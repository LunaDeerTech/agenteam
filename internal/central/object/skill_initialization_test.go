package object

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

type initializationSQL struct {
	postgres.SQLExecutor
	calls     int
	initiator string
}

var initializationSQLStop = errors.New("recorded reservation boundary")

func (s *initializationSQL) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	s.calls++
	if s.calls == 2 {
		s.initiator = args[11].(string)
		return pgconn.CommandTag{}, initializationSQLStop
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func TestSkillInitializationReservationKeepsExactInitiator(t *testing.T) {
	project := auditID[id.Project](t)
	creation := auditID[struct{}](t)
	revision := auditID[struct{}](t)
	scope, _ := id.InProject(project)
	role, _ := id.RegisterService(id.ProjectInitialization)
	actor, _ := role.Actor(creation.String(), scope)
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, revision.String(), project.String())
	grant, e := oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: id.Mutate, Existence: oc.ProspectiveOwner, CreationCause: creation.String(), Version: 1})
	if e != nil {
		t.Fatal(e)
	}
	meta := f.CommandMeta{RequestID: auditID[f.Request](t), IdempotencyKey: "original"}
	for _, tc := range []struct {
		name  string
		actor id.Actor
		grant oc.OwnerAuthorization
		want  string
	}{
		{"initialization", actor, grant, creation.String()},
		{"missing_grant", actor, oc.OwnerAuthorization{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql := &initializationSQL{}
			_, e := (&Service{}).reserveObjectCommand(context.Background(), sql, tc.actor, owner, meta, oc.PreparedDetails{}, auditID[oc.StoredObject](t), auditID[oc.Upload](t), auditID[oc.Receipt](t), nil, tc.grant)
			if tc.want == "" {
				if e == nil || sql.calls != 0 {
					t.Fatal("unbound reservation reached SQL")
				}
			} else if !errors.Is(e, initializationSQLStop) || sql.calls != 2 || sql.initiator != tc.want {
				t.Fatal("wrong original initiator")
			}
		})
	}
	for _, kind := range []id.ActorKind{id.Human, id.AgentRun} {
		user := auditID[id.User](t)
		agent := auditID[id.Agent](t)
		want := user.String()
		a, _ := id.NewHuman(user, auditID[id.Session](t))
		if kind == id.AgentRun {
			a, _ = id.NewAgentRun(project, agent, auditID[id.Execution](t))
			want = agent.String()
		}
		sql := &initializationSQL{}
		_, e := (&Service{}).reserveObjectCommand(context.Background(), sql, a, owner, meta, oc.PreparedDetails{}, auditID[oc.StoredObject](t), auditID[oc.Upload](t), auditID[oc.Receipt](t), nil, oc.OwnerAuthorization{})
		if !errors.Is(e, initializationSQLStop) || sql.initiator != want {
			t.Fatal("old initiator changed", kind)
		}
	}
}
