package registry

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type mcpDirectoryStore struct {
	*metadataStore
	query func(context.Context, string, ...any) (*postgres.Rows, error)
}

func (s *mcpDirectoryStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.metadataStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *mcpDirectoryStore) Query(ctx context.Context, sql string, args ...any) (*postgres.Rows, error) {
	s.queries++
	return s.query(ctx, sql, args...)
}

type mcpDirectorySessions struct{ err error }

func (s mcpDirectorySessions) RequireCurrentSession(context.Context, f.Tx, id.Actor) error {
	return s.err
}

type mcpDirectoryProjects struct {
	configurationAuth
	foreign bool
	err     error
}

func (p mcpDirectoryProjects) AuthorizeProject(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
	if p.err != nil {
		return id.AccessGrant{}, p.err
	}
	if p.foreign {
		project, _ = f.ParseID[id.Project]("018f0000-0000-7000-8000-000000000099")
	}
	return p.configurationAuth.AuthorizeProject(ctx, tx, actor, project, intent)
}

func TestMCPDirectoryRequiresCurrentOwnerBeforeMetadata(t *testing.T) {
	_, base, _, request := configurationFixture(t)
	store := &mcpDirectoryStore{metadataStore: base}
	connection := "018f0000-0000-7000-8000-000000000088"
	reached := errors.New("authorized-query-boundary")
	store.query = func(ctx context.Context, sql string, args ...any) (*postgres.Rows, error) {
		if len(args) != 2 || args[0] != request.ProjectID.String() || args[1] != connection || !strings.Contains(sql, "r.project_id=$1 AND r.connection_id=$2") || !strings.Contains(sql, "LIMIT 10001") {
			t.Fatal("directory query lost Project/connection scope or bound")
		}
		if err := store.RequireHeldLocks(ctx, store.tx, []f.LockRequest{RegistryLock(f.Shared)}); err != nil {
			t.Fatal(err)
		}
		return nil, reached
	}
	registry, err := New(store, Options{Authorizations: Authorizations{Sessions: mcpDirectorySessions{}, Projects: mcpDirectoryProjects{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		auth Authorizations
	}{
		{"unbound", Authorizations{}},
		{"revoked_session", Authorizations{Sessions: mcpDirectorySessions{err: f.NewFault(f.Unauthenticated, f.NotStarted)}, Projects: mcpDirectoryProjects{}}},
		{"project_denied", Authorizations{Sessions: mcpDirectorySessions{}, Projects: mcpDirectoryProjects{err: f.NewFault(f.Forbidden, f.NotStarted)}}},
		{"foreign_grant", Authorizations{Sessions: mcpDirectorySessions{}, Projects: mcpDirectoryProjects{foreign: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry.data().auth = test.auth
			if rows, err := registry.ListMCPTools(context.Background(), request.Actor, request.ProjectID, connection); err == nil || rows != nil || store.queries != 0 || len(store.writes) != 0 {
				t.Fatalf("unproven directory read reached metadata: queries=%d err=%v", store.queries, err)
			}
		})
	}
	registry.data().auth = Authorizations{Sessions: mcpDirectorySessions{}, Projects: mcpDirectoryProjects{}}
	if rows, err := registry.ListMCPTools(context.Background(), request.Actor, request.ProjectID, connection); !errors.Is(err, reached) || rows != nil || store.queries != 1 || len(store.writes) != 0 {
		t.Fatalf("original authorized metadata boundary not used: queries=%d err=%v", store.queries, err)
	}
	service, _ := id.RegisterService(id.ModelRuntime)
	scope, _ := id.InProject(request.ProjectID)
	actor, _ := service.Actor(connection, scope)
	if _, err = registry.ListMCPTools(context.Background(), actor, request.ProjectID, connection); err == nil || store.queries != 1 {
		t.Fatal("service DTO substituted for current Owner")
	}
}
