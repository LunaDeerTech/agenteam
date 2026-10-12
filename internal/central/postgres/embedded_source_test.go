package postgres

import "testing"

// Use the real embedded SQL, so source rules also cover each newly added
// migration. Loading the source performs no database or network operation.
func TestEmbeddedSourceLoadsAllMigrations(t *testing.T) {
	if _, err := EmbeddedSource(); err != nil {
		t.Fatalf("embedded migration source: %v", err)
	}
}
