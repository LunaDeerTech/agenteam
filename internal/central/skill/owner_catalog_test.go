package skill

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func catalogTestKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"catalog-test","keys":[{"kid":"catalog-test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}
func TestSkillOwnerCatalogCursorScopeAndConstructor(t *testing.T) {
	keys := catalogTestKeys(t)
	if _, err := NewOwnerCatalog(nil, keys); err == nil {
		t.Fatal("nil service accepted")
	}
	if _, err := NewOwnerCatalog(&Service{}, keys); err == nil {
		t.Fatal("unbound service accepted")
	}
	s, _, _, actor, row := readerFixture(t)
	if _, err := NewOwnerCatalog(s, cursor.Keyring{}); err == nil {
		t.Fatal("unsigned catalogue accepted")
	}
	c, err := NewOwnerCatalog(s, keys)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := catalogBinding(actor, row.request.ProjectID, 1)
	if err != nil {
		t.Fatal(err)
	}
	position, _ := cursor.UUID(row.skill.String())
	token, err := keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{position}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"project", "user", "limit", "view", "order"} {
		changed := binding
		switch name {
		case "project":
			changed.Scope, _ = id.InProject(stateID[id.Project](990))
		case "user":
			other, _ := id.NewHuman(stateID[id.User](991), stateID[id.Session](992))
			changed, _ = catalogBinding(other, row.request.ProjectID, 1)
		case "limit":
			changed, _ = catalogBinding(actor, row.request.ProjectID, 2)
		case "view":
			changed.QueryDigest, _ = cursor.Digest([]byte(`{"view":"other"}`))
		case "order":
			changed.Order = "id:desc"
		}
		if _, err := keys.Verify(token, changed); err == nil {
			t.Fatal("cursor crossed binding", name)
		}
	}
	for _, position := range []cursor.Position{{Scalars: []cursor.Scalar{cursor.Integer(1)}}, {Scalars: []cursor.Scalar{position, position}}, {Scalars: []cursor.Scalar{position}, OrderGeneration: new(int64(1))}} {
		token, err := keys.Sign(binding, position)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.List(context.Background(), actor, row.request.ProjectID, OwnerCatalogQuery{1, token}); err == nil {
			t.Fatal("foreign position shape accepted")
		}
	}
}

type catalogQueryStore struct {
	*initializationReadStore
	calls int
	query string
	args  []any
	err   error
}

func (s *catalogQueryStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.initializationReadStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *catalogQueryStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	if err := s.RequireHeldLocks(ctx, s.tx, []f.LockRequest{projectLock(stateRowProject(args), f.Shared)}); err != nil {
		return nil, err
	}
	s.calls++
	s.query = query
	s.args = append([]any(nil), args...)
	return nil, s.err
}
func stateRowProject(args []any) id.ProjectID {
	p, _ := f.ParseID[id.Project](args[0].(string))
	return p
}

func TestSkillOwnerCatalogCurrentGrantBeforeBoundedSQL(t *testing.T) {
	for _, name := range []string{"authorized-query", "revoked", "wrong-owner", "unpublished", "stopped"} {
		t.Run(name, func(t *testing.T) {
			s, base, projects, actor, row := readerFixture(t)
			sentinel := errors.New("catalog-private-sql-canary")
			store := &catalogQueryStore{initializationReadStore: base, err: sentinel}
			s.state().authority.state().store = store
			c, err := NewOwnerCatalog(s, catalogTestKeys(t))
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "revoked":
				projects.err = fault(f.SessionRevoked)
			case "wrong-owner":
				actor, _ = id.NewHuman(stateID[id.User](990), stateID[id.Session](991))
			case "unpublished":
				base.row.values[14] = "planned"
			case "stopped":
				s.Stop()
			}
			page, err := c.List(context.Background(), actor, row.request.ProjectID, OwnerCatalogQuery{Limit: 25})
			if err == nil || page.Items != nil || page.NextCursor != "" {
				t.Fatal("failed read published candidate")
			}
			if name == "authorized-query" {
				if store.calls != 1 || len(store.args) != 3 || store.args[0] != row.request.ProjectID.String() || store.args[1] != nil || store.args[2] != 26 || !strings.Contains(store.query, "ORDER BY id ASC LIMIT $3") || strings.Contains(store.query, "OFFSET") {
					t.Fatal("query was unbounded or outside original gate")
				}
				if !errors.Is(err, sentinel) || strings.Contains(err.Error(), "private-sql-canary") {
					t.Fatal("SQL error safety/cause changed")
				}
			} else if store.calls != 0 {
				t.Fatal("unauthorized/unpublished read queried catalogue")
			}
		})
	}
}
