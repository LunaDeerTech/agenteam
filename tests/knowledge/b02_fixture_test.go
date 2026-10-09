//go:build integration

package knowledge_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func knowledgeContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func knowledgeID(t *testing.T) string {
	t.Helper()
	id, err := f.NewID[struct{}]()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
func knowledgeMigrationFiles(t *testing.T, through string) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	out := fstest.MapFS{}
	for _, name := range names {
		if name[:5] > through {
			continue
		}
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = &fstest.MapFile{Data: raw}
	}
	return out
}
func knowledgeMigrationSource(t *testing.T, files fstest.MapFS) postgres.Source {
	t.Helper()
	source, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
func knowledgeMigrate(t *testing.T, db *pgfixture.Database, source postgres.Source) {
	t.Helper()
	m, err := postgres.NewMigrator(db.Config(t, nil), source)
	if err != nil {
		t.Fatal(err)
	}
	result := m.Migrate(knowledgeContext(t))
	if !result.Migrated {
		t.Fatal("Knowledge migration failed", result.Fault)
	}
}
