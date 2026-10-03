package postgres

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSourceManifestAndRecoveryRegistration(t *testing.T) {
	valid := "-- agenteam:transaction tx\n-- +goose Up\nSELECT 1;\n"
	for _, files := range []fstest.MapFS{
		{},
		{"1_bad.sql": {Data: []byte(valid)}},
		{"00002_gap.sql": {Data: []byte(valid)}},
		{"00001_a.sql": {Data: []byte(valid)}, "00001_b.sql": {Data: []byte(valid)}},
		{"00001_a.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}},
		{"00001_a.sql": {Data: []byte(strings.ReplaceAll(valid, "transaction tx", "transaction non_tx"))}},
		{"00001_a.sql": {Data: []byte("-- agenteam:transaction non_tx\n-- +goose NO TRANSACTION\n-- +goose Up\nSELECT 1;\n")}},
	} {
		if _, err := NewSource(files, nil); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	files := fstest.MapFS{"00001_a.sql": {Data: []byte(valid)}}
	source, err := NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	checksum := source.Manifest()[0].Checksum
	files["00001_a.sql"].Data = []byte("changed")
	manifest := source.Manifest()
	manifest[0].Filename = "changed"
	if source.Manifest()[0].Checksum != checksum || source.Manifest()[0].Filename != "00001_a.sql" {
		t.Fatal("source can be mutated")
	}
	nonTx := fstest.MapFS{"00001_a.sql": {Data: []byte("-- agenteam:transaction non_tx\n-- +goose NO TRANSACTION\n-- +goose Up\nSELECT 1;\n")}}
	plans := map[int64]RecoveryPlan{1: {RestoreSteps: []string{"DROP INDEX CONCURRENTLY IF EXISTS fixture_index"}, VerifyRestored: "SELECT to_regclass('fixture_index') IS NULL"}}
	if _, err := NewSource(nonTx, plans); err != nil {
		t.Fatal(err)
	}
}
func TestRawDriverFailureProjectionAndTransactionControl(t *testing.T) {
	raw := &pgconn.PgError{Code: "23505", Message: "password-sentinel", Detail: "sql-sentinel", Hint: "private-hint"}
	safe := failure(SQLFailed, raw)
	for _, value := range []any{safe, *safe, struct{ hidden *Error }{safe}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			text := fmt.Sprintf(format, value)
			if strings.Contains(text, "sentinel") || strings.Contains(text, "private-hint") {
				t.Fatal("driver error exposed")
			}
		}
		b, err := json.Marshal(value)
		if err != nil || strings.Contains(string(b), "sentinel") {
			t.Fatal("JSON exposed raw driver")
		}
	}
	if safe.SQLState() != "23505" || safe.Unwrap() != raw {
		t.Fatal("explicit diagnostic identity lost")
	}
	for _, query := range []string{"COMMIT", " /* comment */ ROLLBACK", "SELECT 1; -- comment\n BEGIN", "SAVEPOINT x", "RELEASE SAVEPOINT x", "START TRANSACTION", "PREPARE TRANSACTION 'x'"} {
		if !transactionControl(query) {
			t.Fatal("transaction control allowed")
		}
	}
	for _, query := range []string{"SELECT 'COMMIT;'", "SELECT $$BEGIN; ROLLBACK$$", `SELECT "COMMIT" FROM fixture`, `SELECT CASE WHEN true THEN 1 ELSE 2 END`, `/* nested /* BEGIN */ comment */ SELECT 1`} {
		if transactionControl(query) {
			t.Fatal("ordinary SQL rejected")
		}
	}
}
