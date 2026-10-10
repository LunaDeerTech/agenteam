//go:build integration

package objects_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

//go:embed testdata/metadata_cleanup_skill_cost.sql
var metadataSkillCostSQL string

// Exact query in the accepted Skills consumer 598bc02e,
// internal/central/skill/cleanup_repository.go:compressCleanupHistory.
// This is a cost measurement of that SQL, not invocation of Skills authority
// or a duplicate implementation of its 32-row deletion/transaction algorithm.
const metadataSkillJoinedCostQuery = `SELECT COALESCE(array_agg(id::text ORDER BY id),ARRAY[]::text[]) FROM (SELECT id FROM agenteam_skill.work WHERE project_id=$1 AND phase='joined' ORDER BY id LIMIT 33) history`

func TestObjectMetadataCleanupSkillsIndexPlans(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrator, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result := migrator.Migrate(contextFor(t)); !result.Migrated {
		t.Fatal(result.Fault)
	}
	store, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.ForceClose(ctx); err != nil {
			t.Error(err)
		}
	})
	conn := db.Connect(t)
	if _, err := conn.Exec(contextFor(t), metadataSkillCostSQL); err != nil {
		t.Fatal("Skills SQL-only cost shape failed its real CHECKs/FKs", err)
	}
	target, other, absent := metadataCostID(0x01b00000, 1), metadataCostID(0x01b00000, 2), metadataCostID(0x01b00000, 3)
	for _, project := range []string{target, absent} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		deadline, _ := ctx.Deadline()
		var got []string
		err := store.QueryRow(ctx, metadataSkillJoinedCostQuery, project).Scan(&got)
		returned := time.Now()
		cancel()
		want := 33
		if project == absent {
			want = 0
		}
		if err != nil || returned.After(deadline) || len(got) != want {
			t.Fatal("Skills original joined discovery result/deadline", len(got), err)
		}
		for i, id := range got {
			if id != metadataCostID(0x01b50000, i+1) {
				t.Fatal("Skills joined discovery included running work or changed order", i)
			}
		}
		// The aggregate plan returns one row even when its array is empty.
		metadataCostExplain(t, store, "skills-history-"+project, "skills-joined", metadataSkillJoinedCostQuery, []any{project}, 1)
	}
	var targetJoined, targetLive, otherJoined int
	err = conn.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase='joined'),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined'),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$2 AND phase='joined')`, target, other).Scan(&targetJoined, &targetLive, &otherJoined)
	if err != nil || targetJoined != 1001 || targetLive != 1 || otherJoined != 10001 {
		t.Fatal("Skills history cardinalities", targetJoined, targetLive, otherJoined, err)
	}

	// This is an explicit rollback-only SQL cost transaction, not the Skills
	// Service's cleanup. Setup removes only the target's history/core children
	// so its parent DELETE is legal; the 10001 foreign work rows remain.
	tx, err := conn.Begin(contextFor(t))
	if err != nil {
		t.Fatal(err)
	}
	rolledBack := false
	defer func() {
		if rolledBack {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := tx.Rollback(ctx); err != nil {
			t.Error("cost transaction actual rollback", err)
		}
	}()
	for _, table := range []string{"work", "cleanup", "revisions", "skills", "object_attempts"} {
		if _, err := tx.Exec(contextFor(t), "DELETE FROM agenteam_skill."+table+" WHERE project_id=$1", target); err != nil {
			t.Fatal("target-only cost setup", table, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	started := time.Now()
	var raw []byte
	err = tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) DELETE FROM agenteam_skill.initializations WHERE project_id=$1`, target).Scan(&raw)
	if err != nil || !json.Valid(raw) {
		t.Fatal("Skills parent DELETE plan", err)
	}
	// EXPLAIN DELETE alone does not execute deferred FK checks. Flush the
	// original queue in this transaction before claiming any measured tail.
	flush := time.Now()
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil || time.Now().After(deadline) {
		t.Fatal("Skills original deferred FK checks failed or returned late", err)
	}
	flushed := time.Now()
	var targetRows, foreignRows int
	err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1),(SELECT count(*) FROM agenteam_skill.work WHERE project_id=$2)`, target, other).Scan(&targetRows, &foreignRows)
	if err != nil || time.Now().After(deadline) || targetRows != 0 || foreignRows != 10001 {
		t.Fatal("parent plan lost its required empty-target/nonempty-foreign shape", targetRows, foreignRows, err)
	}
	// The queue timing is an upper bound for all deferred checks, not an
	// invented per-trigger time or an EXPLAIN of SET CONSTRAINTS itself.
	t.Logf("D05 SQL-cost Skills parent plan=%s delete_plus_fk=%s deferred_queue=%s", raw, flushed.Sub(started), flushed.Sub(flush))
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("Skills cost rollback actual return", err)
	}
	rolledBack = true
	if time.Now().After(deadline) {
		t.Fatal("Skills cost transaction tail exceeded its observation deadline")
	}
	var restoredWork, restoredCore int
	err = conn.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.cleanup WHERE project_id=$1)`, target).Scan(&restoredWork, &restoredCore)
	if err != nil || restoredWork != 1002 || restoredCore != 5 {
		t.Fatal("original Skills cost rows did not survive actual rollback", restoredWork, restoredCore, err)
	}
}
