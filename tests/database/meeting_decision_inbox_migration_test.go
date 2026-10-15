//go:build integration

package database_test

import (
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are deployed SQL shape and upgrade checks. Direct fixture inserts do
// not establish domain authority, canonical Outbox delivery, or Owner access.
func TestMeetingDecisionInboxMigrationPreservesOldRowsAndClosesSourceShape(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade-existing-proposals"
		}
		t.Run(name, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			id := func() string {
				t.Helper()
				value, err := f.NewID[struct{}]()
				if err != nil {
					t.Fatal(err)
				}
				return value.String()
			}
			project, meeting := id(), id()
			var oldIDs, oldImages []string
			if upgrade {
				previous, err := postgres.NewSource(outboxMigrationFiles(t, "00140"), nil)
				if err != nil {
					t.Fatal(err)
				}
				migrate(t, cfg, previous)
			}
			conn := db.Connect(t)
			read := func(table, record string) string {
				t.Helper()
				var image string
				// table is one of the two fixed test literals below, never input.
				if err := conn.QueryRow(testContext(t), `SELECT (to_jsonb(r)-'source_context')::text||r.xmin::text FROM `+table+` r WHERE id=$1`, record).Scan(&image); err != nil {
					t.Fatal(err)
				}
				return image
			}
			if upgrade {
				for _, status := range []string{"open", "resolved"} {
					record := id()
					_, err := conn.Exec(testContext(t), `INSERT INTO agenteam_humaninbox.items
 (id,project_id,source_type,source_id,action_type,status,title_snapshot,summary_snapshot,source_version,
 source_occurred_at,created_at,updated_at,resolved_at)
 VALUES($1,$2,'meeting',$3,'meeting.review_proposal',$4,'original title','original summary',2,
 clock_timestamp(),clock_timestamp(),clock_timestamp(),CASE WHEN $4::text='resolved' THEN clock_timestamp() END)`, record, project, id(), status)
					if err != nil {
						t.Fatal("predecessor proposal row rejected", err)
					}
					oldIDs = append(oldIDs, record)
					oldImages = append(oldImages, read("agenteam_humaninbox.items", record))
				}
			}
			target, err := postgres.NewSource(outboxMigrationFiles(t, "00141"), nil)
			if err != nil {
				t.Fatal(err)
			}
			migration := migrate(t, cfg, target)
			if repeated := migration.Migrate(testContext(t)); !repeated.Migrated || repeated.Version != 141 {
				t.Fatal("repeat migration failed", repeated.Fault)
			}
			var events int
			if err = conn.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_meeting.decision_events`).Scan(&events); err != nil || events != 0 {
				t.Fatal("migration fabricated historical Decision events", events, err)
			}
			if !upgrade {
				return // SQL behavior below is exercised once, after a real upgrade.
			}
			for i, record := range oldIDs {
				var empty bool
				if err = conn.QueryRow(testContext(t), `SELECT source_context='{}'::jsonb FROM agenteam_humaninbox.items WHERE id=$1`, record).Scan(&empty); err != nil || !empty || read("agenteam_humaninbox.items", record) != oldImages[i] {
					t.Fatal("migration changed predecessor fields/xmin or default routing", err)
				}
			}
			reject := func(code, query string, args ...any) {
				t.Helper()
				_, err := conn.Exec(testContext(t), query, args...)
				var fault *pgconn.PgError
				if !errors.As(err, &fault) || fault.Code != code {
					t.Fatalf("expected SQLSTATE %s, got %v", code, err)
				}
			}
			decision, item := id(), id()
			_, err = conn.Exec(testContext(t), `INSERT INTO agenteam_humaninbox.items
 (id,project_id,source_type,source_id,action_type,source_context,status,blocking,priority,
 title_snapshot,summary_snapshot,source_version,source_occurred_at,created_at,updated_at)
 VALUES($1,$2,'decision_request',$3,'decision.answer',jsonb_build_object('meeting_id',$4::text),'open',true,100,
 'decision','answer required',1,clock_timestamp(),clock_timestamp(),clock_timestamp())`, item, project, decision, meeting)
			if err != nil {
				t.Fatal("canonical Decision routing rejected", err)
			}
			before := read("agenteam_humaninbox.items", item)
			for _, assignment := range []string{
				`source_type='meeting'`, `action_type='meeting.review_proposal'`,
				`source_type='other'`, `action_type='approval.answer'`,
				`source_context='{}'::jsonb`, `source_context='[]'::jsonb`, `source_context='null'::jsonb`,
				`source_context=jsonb_build_object('meeting_id',NULL)`,
				`source_context=jsonb_build_object('meeting_id',1)`,
				`source_context=jsonb_build_object('meeting_id','not-an-id')`,
				`source_context=source_context||'{"execution_id":"extra"}'::jsonb`,
			} {
				reject("23514", `UPDATE agenteam_humaninbox.items SET `+assignment+` WHERE id=$1`, item)
			}
			reject("23502", `UPDATE agenteam_humaninbox.items SET source_context=NULL WHERE id=$1`, item)
			reject("23514", `UPDATE agenteam_humaninbox.items SET source_context=jsonb_build_object('meeting_id',$2::text) WHERE id=$1`, oldIDs[0], meeting)
			if read("agenteam_humaninbox.items", item) != before {
				t.Fatal("rejected routing mutation changed the Decision item")
			}
			const insertEvent = `INSERT INTO agenteam_meeting.decision_events
 (event_id,project_id,meeting_id,turn_id,contribution_id,participant_id,execution_id,decision_id,version,event_type,status,occurred_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,clock_timestamp())`
			turn, contribution, participant, execution := id(), id(), id(), id()
			var firstEvent, firstDecision string
			for _, state := range []string{"pending", "answered", "skipped", "cancelled"} {
				eventType, version := "meeting.decision."+state, int64(2)
				if state == "pending" {
					eventType, version = "meeting.decision.requested", 1
				}
				event, source := id(), id()
				if firstEvent == "" {
					firstEvent, firstDecision = event, source
				}
				if _, err = conn.Exec(testContext(t), insertEvent, event, project, meeting, turn, contribution, participant, execution, source, version, eventType, state); err != nil {
					t.Fatal("canonical event shape rejected", state, err)
				}
			}
			for _, invalid := range []struct {
				version int64
				kind    string
				state   string
			}{
				{0, "meeting.decision.requested", "pending"},
				{1, "meeting.decision.requested", "answered"},
				{2, "meeting.decision.answered", "skipped"},
				{2, "meeting.decision.skipped", "pending"},
				{2, "meeting.decision.cancelled", "answered"},
				{1, "meeting.proposed", "pending"},
			} {
				reject("23514", insertEvent, id(), project, meeting, turn, contribution, participant, execution, id(), invalid.version, invalid.kind, invalid.state)
			}
			reject("23505", insertEvent, id(), project, meeting, turn, contribution, participant, execution, firstDecision, int64(1), "meeting.decision.requested", "pending")
			reject("23514", `UPDATE agenteam_meeting.decision_events SET version=version+1 WHERE event_id=$1`, firstEvent)
			reject("23514", `DELETE FROM agenteam_meeting.decision_events WHERE event_id=$1`, firstEvent)
			if err = conn.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_meeting.decision_events`).Scan(&events); err != nil || events != 4 {
				t.Fatal("rejected event writes changed the canonical set", events, err)
			}
			for i, record := range oldIDs {
				if read("agenteam_humaninbox.items", record) != oldImages[i] {
					t.Fatal("new source vocabulary changed predecessor rows")
				}
			}
		})
	}
}
