//go:build integration

package work_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestWorkStructureMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("populated=%t", upgrade), func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			if upgrade {
				migrate(t, db, migrationPrefix(t, "00020"))
			} else {
				migrate(t, db, migrationPrefix(t, "00021"))
			}
			raw := openStore(t, db.Config(t, nil))
			fixture := assemble(t, db, raw, raw, true)
			actor := fixture.human(t, "migration-owner", "user")
			project, _, _ := fixture.create(t, actor, "migration-project")
			conn := db.Connect(t)
			snapshot := func() string {
				var v string
				if err := conn.QueryRow(ctxFor(t), `SELECT (SELECT row_to_json(t)::text FROM agenteam_project.projects t WHERE id=$1)||(SELECT row_to_json(t)::text FROM agenteam_account.users t WHERE id=$2)`, project.ID.String(), actor.Details().UserID).Scan(&v); err != nil {
					t.Fatal(err)
				}
				return v
			}
			before := snapshot()
			migrate(t, db, migrationPrefix(t, "00021"))
			migrate(t, db, migrationPrefix(t, "00021"))
			if before != snapshot() {
				t.Fatal("00021 changed existing Project/Account")
			}
			var tables, foreign int
			if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM information_schema.tables WHERE table_schema='agenteam_work' AND table_type='BASE TABLE'`).Scan(&tables); err != nil || tables != 5 {
				t.Fatal("Work table manifest", tables, err)
			}
			if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_constraint c JOIN pg_class a ON a.oid=c.conrelid JOIN pg_namespace an ON an.oid=a.relnamespace JOIN pg_class b ON b.oid=c.confrelid JOIN pg_namespace bn ON bn.oid=b.relnamespace WHERE c.contype='f' AND an.nspname='agenteam_work' AND bn.nspname<>'agenteam_work'`).Scan(&foreign); err != nil || foreign != 0 {
				t.Fatal("cross-domain FK", err)
			}
			m := fixture.milestone(t, actor, project.ID, "m")
			s := fixture.sprint(t, actor, project.ID, m.ID, "s")
			other := fixture.milestone(t, actor, project.ID, "n")
			for _, test := range []struct{ name, sql, state string }{
				{"foreign-parent", `UPDATE agenteam_work.sprints SET project_id='01900000-0000-7000-8000-000000000001' WHERE id=$1`, "23503"},
				{"pair", `UPDATE agenteam_work.sprints SET started_at=clock_timestamp() WHERE id=$1`, "23514"},
				{"zero-version", `UPDATE agenteam_work.sprints SET version=0 WHERE id=$1`, "23514"},
				{"bad-rank", `UPDATE agenteam_work.sprints SET manual_rank=repeat('f',32) WHERE id=$1`, "23514"},
			} {
				t.Run(test.name, func(t *testing.T) {
					_, err := conn.Exec(ctxFor(t), test.sql, s.ID.String())
					requirePGState(t, err, test.state)
				})
			}
			_, err := conn.Exec(ctxFor(t), `UPDATE agenteam_work.milestones SET manual_rank=$2 WHERE id=$1`, other.ID.String(), m.ManualRank)
			requirePGState(t, err, "23505")
			_, err = conn.Exec(ctxFor(t), `DELETE FROM agenteam_work.milestones WHERE id=$1`, m.ID.String())
			requirePGState(t, err, "23503")
		})
	}
	t.Run("DDL-failure-rollback", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00020"))
		files := migrationFiles(t, "00020")
		raw, err := fs.ReadFile(migrations.SQL, "00021_work_structure.sql")
		if err != nil {
			t.Fatal(err)
		}
		files["00021_work_structure.sql"] = &fstest.MapFile{Data: append(append([]byte{}, raw...), []byte("\nSELECT 1/0;\n")...)}
		source, err := postgres.NewSource(files, nil)
		if err != nil {
			t.Fatal(err)
		}
		m, err := postgres.NewMigrator(db.Config(t, nil), source)
		if err != nil {
			t.Fatal(err)
		}
		result := m.Migrate(ctxFor(t))
		if result.Migrated || result.Fault == nil {
			t.Fatal("intentional migration failure succeeded")
		}
		conn := db.Connect(t)
		var absent bool
		if err = conn.QueryRow(ctxFor(t), `SELECT to_regnamespace('agenteam_work') IS NULL`).Scan(&absent); err != nil || !absent {
			t.Fatal("partial Work schema escaped rollback", err)
		}
		var applied int
		if err = conn.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=21 AND is_applied`).Scan(&applied); err != nil || applied != 0 {
			t.Fatal("failed migration accepted", err)
		}
	})
}

func TestWorkStructurePersistenceAndPaging(t *testing.T) {
	f := newFixture(t)
	a := f.human(t, "structure-owner", "user")
	p, _, _ := f.create(t, a, "one")
	other, _, _ := f.create(t, a, "two")
	first := f.milestone(t, a, p.ID, "duplicate")
	second := f.milestone(t, a, p.ID, "duplicate")
	outside := f.milestone(t, a, other.ID, "outside")
	empty, err := f.reader.ListSprints(ctxFor(t), a, p.ID, first.ID, foundation.DefaultPageRequest())
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal("valid empty group", err)
	}
	if _, err = work.NewReader(f.store, nil, f.keys); err == nil {
		t.Fatal("missing authority became an empty reader")
	}
	s1 := f.sprint(t, a, p.ID, first.ID, "same")
	s2 := f.sprint(t, a, p.ID, first.ID, "same")
	f.sprint(t, a, p.ID, second.ID, "other group")
	title := " changed title "
	description := ""
	mr, err := f.service.UpdateMilestone(ctxFor(t), a, meta(t, "update-m", &first.Version), p.ID, first.ID, wc.UpdateFields{Title: &title, Description: &description})
	if err != nil || mr.Milestone.Version != 2 {
		t.Fatal("update milestone", err)
	}
	sr, err := f.service.UpdateSprint(ctxFor(t), a, meta(t, "update-s", &s1.Version), p.ID, s1.ID, wc.UpdateFields{Title: &title})
	if err != nil || sr.Sprint.Version != 2 {
		t.Fatal("update sprint", err)
	}
	moved, err := f.service.ReorderMilestone(ctxFor(t), a, meta(t, "move-m", &second.Version), p.ID, second.ID, wc.ReorderMilestoneRequest{BeforeID: &first.ID})
	if err != nil || !moved.Changed {
		t.Fatal("reorder milestone", err)
	}
	movedS, err := f.service.ReorderSprint(ctxFor(t), a, meta(t, "move-s", &s2.Version), p.ID, s2.ID, wc.ReorderSprintRequest{MilestoneID: first.ID, BeforeID: &s1.ID})
	if err != nil || !movedS.Changed {
		t.Fatal("reorder sprint", err)
	}
	parent, err := f.reader.GetMilestone(ctxFor(t), a, p.ID, first.ID)
	if err != nil || parent.Version != mr.Milestone.Version || parent.UpdatedAt != mr.Milestone.UpdatedAt {
		t.Fatal("Sprint changed parent business version", err)
	}
	_, err = f.reader.GetMilestone(ctxFor(t), a, p.ID, outside.ID)
	requireCode(t, err, foundation.NotFound)
	_, err = f.reader.ListSprints(ctxFor(t), a, p.ID, outside.ID, foundation.DefaultPageRequest())
	requireCode(t, err, foundation.NotFound)
	_, err = f.service.ReorderSprint(ctxFor(t), a, meta(t, "wrong-parent", &movedS.Sprint.Version), p.ID, s2.ID, wc.ReorderSprintRequest{MilestoneID: second.ID})
	requireCode(t, err, foundation.NotFound)
	for n := 2; n < 205; n++ {
		f.milestone(t, a, p.ID, "duplicate")
	}
	seen := map[wc.MilestoneID]bool{}
	page := foundation.DefaultPageRequest()
	var firstToken string
	for round := 0; ; round++ {
		result, err := f.reader.ListMilestones(ctxFor(t), a, p.ID, page)
		if err != nil {
			t.Fatal(err)
		}
		if round == 0 {
			if len(result.Items) != 50 {
				t.Fatal("default page limit")
			}
			firstToken = result.NextCursor
		}
		for _, item := range result.Items {
			if seen[item.ID] {
				t.Fatal("duplicate pagination item")
			}
			seen[item.ID] = true
		}
		if result.NextCursor == "" {
			break
		}
		page = foundation.PageRequest{Cursor: result.NextCursor, Limit: 7}
		if round > 40 {
			t.Fatal("unbounded pagination")
		}
	}
	if len(seen) != 205 {
		t.Fatal("pagination omitted records", len(seen))
	}
	renewed := f.renew(t, a)
	if _, err = f.reader.ListMilestones(ctxFor(t), renewed, p.ID, foundation.PageRequest{Cursor: firstToken, Limit: 1}); err != nil {
		t.Fatal("Session entered cursor binding", err)
	}
	parts := strings.Split(firstToken, ".")
	if len(parts) != 3 {
		t.Fatal("invalid signed token premise")
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(mac) != 32 {
		t.Fatal("invalid HMAC premise", err)
	}
	mac[0] ^= 1
	tamperedMAC := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(mac)
	kid, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	badKid := strings.Replace(string(kid), `"kid":"c"`, `"kid":"unknown"`, 1)
	if badKid == string(kid) {
		t.Fatal("fixture kid absent")
	}
	tamperedKid := base64.RawURLEncoding.EncodeToString([]byte(badKid)) + "." + parts[1] + "." + parts[2]
	for _, test := range []struct {
		name    string
		project c.ProjectID
		token   string
	}{{"signature", p.ID, tamperedMAC}, {"kid", p.ID, tamperedKid}, {"project", other.ID, firstToken}} {
		t.Run(test.name, func(t *testing.T) {
			_, err := f.reader.ListMilestones(ctxFor(t), a, test.project, foundation.PageRequest{Cursor: test.token, Limit: 5})
			requireCode(t, err, foundation.CursorInvalid)
		})
	}
	t.Run("cursor-owner-and-content", func(t *testing.T) {
		changed := "content does not invalidate order"
		current, err := f.reader.GetMilestone(ctxFor(t), a, p.ID, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.service.UpdateMilestone(ctxFor(t), a, meta(t, "cursor-content", &current.Version), p.ID, first.ID, wc.UpdateFields{Title: &changed}); err != nil {
			t.Fatal(err)
		}
		if _, err = f.reader.ListMilestones(ctxFor(t), a, p.ID, foundation.PageRequest{Cursor: firstToken, Limit: 2}); err != nil {
			t.Fatal("content staled structural cursor", err)
		}
		newOwner := f.human(t, "cursor-next-owner", "user")
		f.transferOwner(t, a, newOwner, p.ID)
		_, err = f.reader.ListMilestones(ctxFor(t), newOwner, p.ID, foundation.PageRequest{Cursor: firstToken, Limit: 1})
		requireCode(t, err, foundation.CursorInvalid)
		f.transferOwner(t, newOwner, a, p.ID)
	})
	t.Run("valid-new-kid-retains-old-token", func(t *testing.T) {
		encode := func(b byte) string {
			return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string([]byte{b}), 32)))
		}
		rotated, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"next","keys":[{"kid":"c","key_b64":%q},{"kid":"next","key_b64":%q}]}`, encode(1), encode(9)))
		if err != nil {
			t.Fatal(err)
		}
		reader, err := work.NewReader(f.store, f.authority, rotated)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reader.ListMilestones(ctxFor(t), a, p.ID, foundation.PageRequest{Cursor: firstToken, Limit: 1}); err != nil {
			t.Fatal("retained signing key rejected", err)
		}
	})
	pageS, err := f.reader.ListSprints(ctxFor(t), a, p.ID, first.ID, foundation.PageRequest{Limit: 1})
	if err != nil || pageS.NextCursor == "" {
		t.Fatal("sprint next page", err)
	}
	_, err = f.reader.ListSprints(ctxFor(t), a, p.ID, second.ID, foundation.PageRequest{Cursor: pageS.NextCursor, Limit: 1})
	requireCode(t, err, foundation.CursorInvalid)
	f.milestone(t, a, p.ID, "new membership")
	_, err = f.reader.ListMilestones(ctxFor(t), a, p.ID, foundation.PageRequest{Cursor: firstToken, Limit: 5})
	requireCode(t, err, foundation.CursorStale)
	createID := id[wc.Milestone](t)
	createMeta := meta(t, "copy-receipt", nil)
	request := wc.CreateMilestoneRequest{MilestoneID: createID, Title: "immutable receipt"}
	original, err := f.service.CreateMilestone(ctxFor(t), a, createMeta, p.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	copy := original.Clone()
	original.Milestone.Title = "caller changed"
	replay, err := f.service.CreateMilestone(ctxFor(t), renewed, createMeta, p.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	equalResult(t, copy, replay)
	schedule, _ := foundation.ProjectScheduleLock(p.ID.String())
	target, _ := foundation.AggregateLock(foundation.SprintAggregate, s1.ID.String())
	f.tx(t, fixtureLocks(a, p.ID, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}, foundation.LockRequest{Key: target, Mode: foundation.Shared}), func(ctx context.Context, tx foundation.Tx, _ postgres.SQLExecutor) error {
		placement, err := f.reader.ReadPlacementInTx(ctx, tx, a, p.ID, s1.ID)
		if err != nil {
			return err
		}
		if placement.Milestone.ID != first.ID || placement.Sprint.ID != s1.ID {
			return errors.New("wrong real placement")
		}
		return nil
	})
	before := f.snapshot(t, a)
	failed := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, err := f.reader.ReadPlacementInTx(ctx, tx, a, p.ID, s1.ID)
		return err
	})
	if failed.State() != foundation.NotCommitted || f.snapshot(t, a) != before {
		t.Fatal("missing placement locks did not poison/rollback")
	}
	foreign := openStore(t, f.db.Config(t, nil))
	failed = foreign.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, err := f.reader.ReadPlacementInTx(ctx, tx, a, p.ID, s1.ID)
		return err
	})
	if failed.State() != foundation.NotCommitted {
		t.Fatal("foreign Store accepted")
	}
	var ended foundation.Tx
	f.tx(t, fixtureLocks(a, p.ID), func(_ context.Context, tx foundation.Tx, _ postgres.SQLExecutor) error { ended = tx; return nil })
	if _, err = f.reader.ReadPlacementInTx(ctxFor(t), ended, a, p.ID, s1.ID); err == nil {
		t.Fatal("ended Tx accepted")
	}
	t.Run("invalid-stored-facts-are-not-empty", func(t *testing.T) {
		foreignSprint := f.sprint(t, a, other.ID, outside.ID, "outside pointer")
		f.tx(t, fixtureLocks(a, p.ID, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p.ID, identity.Read); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET current_sprint_id=$2 WHERE id=$1`, p.ID.String(), foreignSprint.ID.String())
			return err
		})
		_, err := f.reader.GetMilestone(ctxFor(t), a, p.ID, first.ID)
		requireCode(t, err, foundation.InternalError)
		f.tx(t, fixtureLocks(a, p.ID, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}), func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET current_sprint_id=NULL WHERE id=$1`, p.ID.String())
			return err
		})
		current, err := f.reader.GetMilestone(ctxFor(t), a, p.ID, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.tx(t, fixtureLocks(a, p.ID), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p.ID, identity.Mutate); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `UPDATE agenteam_work.milestones SET title='   ' WHERE id=$1`, first.ID.String())
			return err
		})
		_, err = f.reader.GetMilestone(ctxFor(t), a, p.ID, first.ID)
		requireCode(t, err, foundation.InternalError)
		f.tx(t, fixtureLocks(a, p.ID), func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `UPDATE agenteam_work.milestones SET title=$2 WHERE id=$1`, first.ID.String(), current.Title)
			return err
		})
	})
}

func TestWorkStructureAuthorityAndReplay(t *testing.T) {
	f := newFixture(t)
	a := f.human(t, "authority-owner", "user")
	foreign := f.human(t, "foreign-owner", "user")
	admin := f.human(t, "admin-owner", "admin")
	p, _, _ := f.create(t, a, "authority")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	title := "new"
	request := wc.UpdateFields{Title: &title}
	originalMeta := meta(t, "original", &s.Version)
	original, err := f.service.UpdateSprint(ctxFor(t), a, originalMeta, p.ID, s.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []identity.Actor{foreign, admin} {
		_, err = f.service.UpdateSprint(ctxFor(t), actor, originalMeta, p.ID, s.ID, request)
		requireCode(t, err, foundation.NotFound)
	}
	agent, err := identity.NewAgentRun(p.ID, id[identity.Agent](t), id[identity.Execution](t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.UpdateSprint(ctxFor(t), agent, originalMeta, p.ID, s.ID, request)
	requireCode(t, err, foundation.DependencyUnbound)
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := identity.InProject(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.Actor(id[struct{}](t).String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.UpdateSprint(ctxFor(t), service, originalMeta, p.ID, s.ID, request)
	requireCode(t, err, foundation.Forbidden)
	changed := "different"
	_, err = f.service.UpdateSprint(ctxFor(t), a, originalMeta, p.ID, s.ID, wc.UpdateFields{Title: &changed})
	requireCode(t, err, foundation.IdempotencyKeyReused)
	other := f.sprint(t, a, p.ID, m.ID, "other")
	_, err = f.service.UpdateSprint(ctxFor(t), a, originalMeta, p.ID, other.ID, request)
	requireCode(t, err, foundation.IdempotencyKeyReused)
	t.Run("identity-owner-project-command-key", func(t *testing.T) {
		f.transferOwner(t, a, foreign, p.ID)
		_, err := f.service.UpdateSprint(ctxFor(t), foreign, originalMeta, p.ID, s.ID, request)
		requireCode(t, err, foundation.NotFound)
		f.transferOwner(t, foreign, a, p.ID)
		if _, err = f.service.CreateMilestone(ctxFor(t), a, meta(t, string(originalMeta.IdempotencyKey), nil), p.ID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "different command"}); err != nil {
			t.Fatal("command namespace collided", err)
		}
		newKey, err := f.service.UpdateSprint(ctxFor(t), a, meta(t, "distinct-key", &original.Sprint.Version), p.ID, s.ID, request)
		if err != nil || newKey.Changed {
			t.Fatal("distinct key did not persist its own no-op", err)
		}
		otherProject, _, _ := f.create(t, a, "key-other-project")
		parent := f.milestone(t, a, otherProject.ID, "parent")
		target := f.sprint(t, a, otherProject.ID, parent.ID, "before")
		if _, err = f.service.UpdateSprint(ctxFor(t), a, meta(t, string(originalMeta.IdempotencyKey), &target.Version), otherProject.ID, target.ID, request); err != nil {
			t.Fatal("Project namespace collided", err)
		}
	})
	f.lifecycle(t, a, p.ID, *original.Sprint, false)
	currentState, err := f.reader.GetSprint(ctxFor(t), a, p.ID, s.ID)
	if err != nil || currentState.State != wc.Current {
		t.Fatal("same-Tx current lifecycle input", err)
	}
	currentTitle := "current can update"
	if _, err = f.service.UpdateSprint(ctxFor(t), a, meta(t, "current-update", &currentState.Version), p.ID, s.ID, wc.UpdateFields{Title: &currentTitle}); err != nil {
		t.Fatal("current Sprint non-lifecycle update denied", err)
	}
	f.lifecycle(t, a, p.ID, *original.Sprint, true)
	replay, err := f.service.UpdateSprint(ctxFor(t), a, originalMeta, p.ID, s.ID, request)
	if err != nil {
		t.Fatal("completed history replay", err)
	}
	equalResult(t, original, replay)
	current, err := f.reader.GetSprint(ctxFor(t), a, p.ID, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.UpdateSprint(ctxFor(t), a, meta(t, "new-completed", &current.Version), p.ID, s.ID, request)
	requireCode(t, err, foundation.InvalidState)
	newSession := f.renew(t, a)
	userLock, _ := foundation.UserLock(a.Details().UserID)
	f.tx(t, []foundation.LockRequest{{Key: userLock, Mode: foundation.Exclusive}}, func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, a.Details().SessionID)
		return err
	})
	_, err = f.service.UpdateSprint(ctxFor(t), a, originalMeta, p.ID, s.ID, request)
	requireCode(t, err, foundation.SessionRevoked)
	replay, err = f.service.UpdateSprint(ctxFor(t), newSession, originalMeta, p.ID, s.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	equalResult(t, original, replay)
	a = newSession
	expired := f.renew(t, a)
	f.tx(t, []foundation.LockRequest{{Key: userLock, Mode: foundation.Exclusive}}, func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET absolute_expires_at=clock_timestamp()-interval '1 second',issued_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, expired.Details().SessionID)
		return err
	})
	if _, err = f.service.UpdateSprint(ctxFor(t), expired, originalMeta, p.ID, s.ID, request); err == nil {
		t.Fatal("expired Session replayed")
	}
	_, err = f.reader.ListMilestones(ctxFor(t), expired, p.ID, foundation.PageRequest{Limit: 1, Cursor: "invalid-token"})
	if err == nil {
		t.Fatal("expired Session received a cursor result")
	}
	var expiredFault *foundation.Fault
	if !errors.As(err, &expiredFault) || expiredFault.Code == foundation.CursorInvalid {
		t.Fatal("bad cursor displaced current Session gate")
	}
	f.seedLifecycle(t, a, p.ID, c.Archived)
	replay, err = f.service.UpdateSprint(ctxFor(t), a, originalMeta, p.ID, s.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	equalResult(t, original, replay)
	_, err = f.service.CreateMilestone(ctxFor(t), a, meta(t, "archive-write", nil), p.ID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "x"})
	requireCode(t, err, foundation.ProjectNotActive)
	f.seedLifecycle(t, a, p.ID, c.Active)
	for _, state := range []c.Lifecycle{c.Archiving, c.Deleting} {
		t.Run(string(state), func(t *testing.T) {
			project, _, _ := f.create(t, a, "transition-"+string(state))
			req := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "historical"}
			m := meta(t, "transition-original", nil)
			original, err := f.service.CreateMilestone(ctxFor(t), a, m, project.ID, req)
			if err != nil {
				t.Fatal(err)
			}
			f.seedProjectTransition(t, a, project.ID, state)
			replay, err := f.service.CreateMilestone(ctxFor(t), a, m, project.ID, req)
			if state == c.Archiving {
				if err != nil {
					t.Fatal("archiving Read historical replay", err)
				}
				equalResult(t, original, replay)
			} else {
				requireCode(t, err, foundation.ProjectNotActive)
			}
			_, err = f.service.CreateMilestone(ctxFor(t), a, meta(t, "transition-new", nil), project.ID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "new"})
			requireCode(t, err, foundation.ProjectNotActive)
		})
	}
	t.Run("planned-is-not-history", func(t *testing.T) {
		capture := &capturingAppender{Appender: f.events, before: func(context.Context, identity.Actor, event.Event) error {
			return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
		}}
		writer := f.newService(t, capture, f.accounts)
		req := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "pending"}
		m := meta(t, "planned", nil)
		_, err := writer.CreateMilestone(ctxFor(t), a, m, p.ID, req)
		requireCode(t, err, foundation.DependencyUnavailable)
		d, err := wc.CreateMilestoneDigest(a, m, p.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		q := wc.CommandLookupRequest{ProjectID: p.ID, Command: wc.MilestoneCreate, Key: m.IdempotencyKey, Semantic: d}
		lookup, err := f.service.LookupCommand(ctxFor(t), a, q)
		if err != nil || lookup.State != wc.LookupInProgress {
			t.Fatal("planned lookup", err)
		}
		f.seedLifecycle(t, a, p.ID, c.Archived)
		_, err = f.service.CreateMilestone(ctxFor(t), a, m, p.ID, req)
		requireCode(t, err, foundation.ProjectNotActive)
		f.seedLifecycle(t, a, p.ID, c.Active)
		if _, err = f.service.CreateMilestone(ctxFor(t), a, m, p.ID, req); err != nil {
			t.Fatal("explicit same request resume", err)
		}
	})
	t.Run("uninitialized", func(t *testing.T) {
		f.skills.setMode("pending")
		req := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "pending-project"}
		out, err := f.projects.CreateProject(ctxFor(t), a, meta(t, "pending-project", nil), req)
		if err != nil || out.State == c.CreationReady {
			t.Fatal("pending fixture", err)
		}
		_, err = f.service.CreateMilestone(ctxFor(t), a, meta(t, "pending-write", nil), req.ProjectID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "x"})
		requireCode(t, err, foundation.ProjectNotActive)
		f.skills.setMode("")
	})
	t.Run("capacity-replay-precedes-new-check", func(t *testing.T) {
		p2, _, _ := f.create(t, a, "capacity")
		req := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "first"}
		m := meta(t, "capacity-original", nil)
		original, err := f.service.CreateMilestone(ctxFor(t), a, m, p2.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		f.tx(t, fixtureLocks(a, p2.ID), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p2.ID, identity.Mutate); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `INSERT INTO agenteam_work.milestones(id,project_id,title,description,manual_rank,version,created_at,updated_at) SELECT ('01900000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,$1,'capacity','',lpad(to_hex(n),32,'0'),1,clock_timestamp(),clock_timestamp() FROM generate_series(1,4095) n`, p2.ID.String())
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_work.sprint_order_groups(project_id,milestone_id,order_generation) SELECT project_id,id,1 FROM agenteam_work.milestones WHERE project_id=$1 ON CONFLICT DO NOTHING`, p2.ID.String())
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_work.milestone_order_groups SET order_generation=order_generation+1 WHERE project_id=$1`, p2.ID.String())
			return err
		})
		replay, err := f.service.CreateMilestone(ctxFor(t), a, m, p2.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		equalResult(t, original, replay)
		_, err = f.service.CreateMilestone(ctxFor(t), a, meta(t, "over-capacity", nil), p2.ID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "overflow"})
		requireCode(t, err, foundation.ResourceBusy)
	})
}

func TestWorkStructureAtomicityAndProducer(t *testing.T) {
	f := newFixture(t)
	a := f.human(t, "atomic-owner", "user")
	p, _, _ := f.create(t, a, "atomic")
	m := f.milestone(t, a, p.ID, "old")
	f.ageSession(t, a)
	before := f.snapshot(t, a)
	title := "new"
	request := wc.UpdateFields{Title: &title}
	metadata := meta(t, "atomic-command", &m.Version)
	for _, failure := range []string{"append", "activity"} {
		t.Run(failure, func(t *testing.T) {
			capture := &capturingAppender{Appender: f.events, failAppend: failure == "append"}
			var activity work.ActivityAuthority = f.accounts
			if failure == "activity" {
				activity = failActivity{f.accounts}
			}
			writer := f.newService(t, capture, activity)
			_, err := writer.UpdateMilestone(ctxFor(t), a, metadata, p.ID, m.ID, request)
			requireCode(t, err, foundation.DependencyUnavailable)
			if f.snapshot(t, a) != before {
				t.Fatal("canonical/event/receipt/generation/Activity escaped rollback")
			}
			saved := capture.last(t)
			attempt := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, saved.Plan.Locks()); err != nil {
					return err
				}
				return f.authority.ValidateAppendInTx(ctx, tx, a, saved.Event.Summary(), saved.Plan.Details().Producer, oc.NewFact)
			})
			if attempt.State() != foundation.NotCommitted {
				t.Fatal("planned without canonical accepted NewFact")
			}
		})
	}
	capture := &capturingAppender{Appender: f.events}
	writer := f.newService(t, capture, f.accounts)
	original, err := writer.UpdateMilestone(ctxFor(t), a, metadata, p.ID, m.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if f.snapshot(t, a) == before {
		t.Fatal("successful mutation has no facts")
	}
	saved := capture.last(t)
	count := f.eventCount(t)
	snapshot := f.snapshot(t, a)
	replay, err := writer.UpdateMilestone(ctxFor(t), a, metadata, p.ID, m.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	equalResult(t, original, replay)
	if f.eventCount(t) != count || f.snapshot(t, a) != snapshot {
		t.Fatal("replay touched durable facts")
	}
	noOpMeta := meta(t, "first-no-op", &original.Milestone.Version)
	noOp, err := writer.UpdateMilestone(ctxFor(t), a, noOpMeta, p.ID, m.ID, request)
	if err != nil || noOp.Changed || noOp.EventID != nil || noOp.Milestone.Version != original.Milestone.Version || f.eventCount(t) != count {
		t.Fatal("no-op changed version/event", err)
	}
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, saved.Plan.Locks()); err != nil {
				return err
			}
			return f.authority.ValidateAppendInTx(ctx, tx, a, saved.Event.Summary(), saved.Plan.Details().Producer, stage)
		})
		if stage == oc.CurrentAccess && result.State() != foundation.Committed || stage == oc.NewFact && result.State() != foundation.NotCommitted {
			t.Fatal("historical stage boundary", stage, result.State(), result.Fault())
		}
	}
	t.Run("same-catalog-no-command", func(t *testing.T) {
		header := saved.Event.Header()
		header.EventID = id[event.EventIdentity](t)
		payload, err := f.workEvents.DecodeMilestoneChanged(saved.Event)
		if err != nil {
			t.Fatal(err)
		}
		forged, err := f.workEvents.NewMilestoneChanged(header, payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.events.PrepareAppend(ctxFor(t), a, forged); err == nil {
			t.Fatal("typed event without durable command accepted")
		}
	})
	t.Run("forged-header-payload-and-catalog", func(t *testing.T) {
		payload, err := f.workEvents.DecodeMilestoneChanged(saved.Event)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"header", "payload", "catalog"} {
			header := saved.Event.Header()
			body := payload
			typed := f.workEvents
			switch kind {
			case "header":
				version := *header.AggregateVersion + 1
				header.AggregateVersion = &version
			case "payload":
				body.CommandID = id[wc.StructureCommand](t)
			case "catalog":
				typed, err = wc.RegisterWorkEvents(event.NewCatalog())
				if err != nil {
					t.Fatal(err)
				}
			}
			forged, err := typed.NewMilestoneChanged(header, body)
			if err != nil {
				t.Fatal("validly typed forgery premise", err)
			}
			if _, err = f.events.PrepareAppend(ctxFor(t), a, forged); err == nil {
				t.Fatal("unbound typed event accepted", kind)
			}
		}
	})
	t.Run("producer-needs-owned-tx-and-full-locks", func(t *testing.T) {
		for _, which := range []string{"missing-locks", "foreign-store", "ended-tx"} {
			var tx foundation.Tx
			store := f.raw
			if which == "foreign-store" {
				store = openStore(t, f.db.Config(t, nil))
			}
			result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, current foundation.Tx) error {
				tx = current
				if which == "ended-tx" {
					return nil
				}
				return f.authority.ValidateAppendInTx(ctx, current, a, saved.Event.Summary(), saved.Plan.Details().Producer, oc.CurrentAccess)
			})
			if which == "ended-tx" {
				if result.State() != foundation.Committed {
					t.Fatal(result.Fault())
				}
				if err := f.authority.ValidateAppendInTx(ctxFor(t), tx, a, saved.Event.Summary(), saved.Plan.Details().Producer, oc.CurrentAccess); err == nil {
					t.Fatal("ended producer Tx accepted")
				}
			} else if result.State() != foundation.NotCommitted {
				t.Fatal("producer Tx/lock denial absent", which)
			}
		}
	})
	t.Run("foreign-issuer-and-session", func(t *testing.T) {
		newAuthority, err := work.NewAuthority(f.store, f.projectAuthority)
		if err != nil {
			t.Fatal(err)
		}
		newSession := f.renew(t, a)
		for _, test := range []struct {
			authority *work.Authority
			actor     identity.Actor
		}{{newAuthority, a}, {f.authority, newSession}} {
			result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, saved.Plan.Locks()); err != nil {
					return err
				}
				return test.authority.ValidateAppendInTx(ctx, tx, test.actor, saved.Event.Summary(), saved.Plan.Details().Producer, oc.CurrentAccess)
			})
			if result.State() != foundation.NotCommitted {
				t.Fatal("foreign issuer/Session accepted")
			}
		}
		fresh, err := f.events.PrepareAppend(ctxFor(t), newSession, saved.Event)
		if err != nil {
			t.Fatal(err)
		}
		f.tx(t, fresh.Locks(), func(ctx context.Context, tx foundation.Tx, _ postgres.SQLExecutor) error {
			_, err := f.events.AppendEventInTx(ctx, tx, newSession, saved.Event, fresh)
			return err
		})
		if f.eventCount(t) != count {
			t.Fatal("new Session historic append duplicated")
		}
		// This plan really belongs to the renewed Session and current issuer;
		// revoke only after discovery, then require the real current gate again.
		userLock, _ := foundation.UserLock(newSession.Details().UserID)
		f.tx(t, []foundation.LockRequest{{Key: userLock, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			if err := f.accounts.RequireCurrentSession(ctx, tx, newSession); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, newSession.Details().SessionID)
			return err
		})
		denied := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, fresh.Locks()); err != nil {
				return err
			}
			return f.authority.ValidateAppendInTx(ctx, tx, newSession, saved.Event.Summary(), fresh.Details().Producer, oc.CurrentAccess)
		})
		if denied.State() != foundation.NotCommitted {
			t.Fatal("revoked current producer Session accepted")
		}
		requireCode(t, denied.Fault(), foundation.SessionRevoked)
	})
	t.Run("actual-event-missing-cannot-recreate", func(t *testing.T) {
		f.tx(t, saved.Plan.Locks(), func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `DELETE FROM agenteam_outbox.events WHERE id=$1`, saved.Event.Header().EventID.String())
			return err
		})
		plan, err := f.events.PrepareAppend(ctxFor(t), a, saved.Event)
		if err != nil {
			t.Fatal(err)
		}
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
				return err
			}
			_, err := f.events.AppendEventInTx(ctx, tx, a, saved.Event, plan)
			return err
		})
		if result.State() != foundation.NotCommitted {
			t.Fatal("completed event resurrected")
		}
	})
	t.Run("known-PK-conflict-safe", func(t *testing.T) {
		other, _, _ := f.create(t, a, "foreign-target")
		_, err := f.service.CreateMilestone(ctxFor(t), a, meta(t, "occupied", nil), other.ID, wc.CreateMilestoneRequest{MilestoneID: m.ID, Title: "not rebound"})
		requireCode(t, err, foundation.ResourceBusy)
		var fault *foundation.Fault
		if !errors.As(err, &fault) || len(fault.FieldErrors) != 1 || fault.FieldErrors[0].Code != "TARGET_OCCUPIED" {
			t.Fatal("wrong occupied projection")
		}
		if strings.Contains(fmt.Sprintf("%+v", err), "SELECT") {
			t.Fatal("SQL leaked")
		}
	})
	t.Run("open-rows-rolls-back-and-joins", func(t *testing.T) {
		raw := openStore(t, f.db.Config(t, nil))
		store := &hookStore{fixtureStore: raw}
		owned := assemble(t, f.db, raw, store, false)
		current, err := f.reader.GetMilestone(ctxFor(t), a, p.ID, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		text := "rows must not commit"
		metadata := meta(t, "open-rows", &current.Version)
		identity, err := wc.Identity(p.ID, wc.MilestoneUpdate, metadata.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		var rows *postgres.Rows
		store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT state='completed' FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneUpdate), string(metadata.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if completed {
				rows, err = x.Query(ctx, `SELECT generate_series(1,4)`)
				return err
			}
			return nil
		})
		before := f.snapshot(t, a)
		_, err = owned.service.UpdateMilestone(ctxFor(t), a, metadata, p.ID, m.ID, wc.UpdateFields{Title: &text})
		var fault *foundation.Fault
		if err == nil || !errors.As(err, &fault) || fault.CommitState != foundation.NotCommitted || rows == nil || rows.Next() {
			t.Fatal("open Rows escaped actual transaction terminal", err)
		}
		rows.Close()
		if f.snapshot(t, a) != before {
			t.Fatal("Rows rollback leaked business facts")
		}
		owned.service.Stop()
		if err = owned.service.Drain(ctxFor(t)); err != nil {
			t.Fatal(err)
		}
		var alive int
		if err = raw.QueryRow(ctxFor(t), `SELECT 1`).Scan(&alive); err != nil || alive != 1 {
			t.Fatal("Rows checkout not returned", err)
		}
	})
	t.Run("counterfeit-row-receipt-rejected", func(t *testing.T) {
		var raw []byte
		if err := f.raw.QueryRow(ctxFor(t), `SELECT receipt FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneUpdate), string(metadata.IdempotencyKey)).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var result wc.StructureMutation
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		result.Milestone.Title = "forged historical content"
		f.tx(t, saved.Plan.Locks(), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p.ID, identity.Read); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `UPDATE agenteam_work.structure_commands SET receipt=$4 WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneUpdate), string(metadata.IdempotencyKey), jsonBytes(t, result))
			return err
		})
		_, err := f.service.UpdateMilestone(ctxFor(t), a, metadata, p.ID, m.ID, request)
		requireCode(t, err, foundation.InternalError)
	})
	var forbiddenAudit int
	if err = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='work'`).Scan(&forbiddenAudit); err != nil || forbiddenAudit != 0 {
		t.Fatal("ordinary Work event mislabeled Audit", err)
	}
}

// The SQLSTATE helper only observes real PG rejection; no driver result is replaced.
func requirePGState(t *testing.T, err error, state string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != state {
		t.Fatalf("expected SQLSTATE %s got %v", state, err)
	}
}
