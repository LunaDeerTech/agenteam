package skill

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These repository controls observe bounded deletions and retained anchors;
// they do not simulate a successful native Object purge or Agent retirement.
type installedCleanupStore struct {
	*installedReadStore
	queries, writes                   []string
	canonicalAbsent, assignmentsEmpty bool
	failAt                            int
	zeroAffected                      bool
}

func (s *installedCleanupStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	s.queries = append(s.queries, q)
	switch {
	case strings.HasPrefix(q, "SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.skills"):
		return skillRowValues{values: []any{s.canonicalAbsent}}
	case strings.Contains(q, "FROM agenteam_skill.agent_assignment_heads"):
		return skillRowValues{values: []any{s.assignmentsEmpty}}
	case strings.HasPrefix(q, "SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.work"):
		return skillRowValues{values: []any{true}}
	case strings.HasPrefix(q, "SELECT COALESCE(array_agg") && strings.Contains(q, "FROM agenteam_skill.work"):
		if len(args) != 2 || args[0] != s.installed.project.String() || args[1] != s.installed.skill.String() {
			return skillRowValues{err: errors.New("wrong bounded work owner")}
		}
		var ids []string
		for id := range s.work {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		ids = ids[:min(len(ids), 33)]
		return skillRowValues{values: []any{ids}}
	case strings.HasPrefix(q, "SELECT COALESCE(array_agg") && strings.Contains(q, "FROM agenteam_skill.installation_attempts"):
		return skillRowValues{values: []any{[]string{}}}
	}
	return s.installedReadStore.QueryRow(ctx, q, args...)
}

func (s *installedCleanupStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	s.writes = append(s.writes, q)
	if s.failAt == len(s.writes) {
		return pgconn.CommandTag{}, errors.New("controlled-install-cleanup-failure")
	}
	if s.zeroAffected {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}
	if strings.HasPrefix(q, "DELETE FROM agenteam_skill.work") {
		w, ok := s.work[args[0].(string)]
		if !ok || w[1] != args[1] || w[2] != args[2] || w[3] != args[3] || w[4] != args[4] || w[5] != "joined" || w[6] != args[5] {
			return pgconn.CommandTag{}, errors.New("wrong work deletion")
		}
		delete(s.work, args[0].(string))
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	if strings.HasPrefix(q, "DELETE FROM agenteam_skill.") {
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	return s.installedReadStore.Exec(ctx, q, args...)
}

func TestInstalledCleanupRetainsBoundedWorkAndCurrentAttempt(t *testing.T) {
	_, base, _, _, r := installedReadFixture(t)
	store := &installedCleanupStore{installedReadStore: base, assignmentsEmpty: true}
	store.work = map[string][]any{}
	for n := 1; n <= 35; n++ {
		id := stateID[skillWork](700 + n).String()
		at := r.created.Time()
		store.work[id] = []any{id, r.project.String(), r.skill.String(), stateID[oc.Process](50).String(), string(installedPackageReaderWork), "joined", int64(1), at, &at}
	}
	more, err := compressInstallationHistory(context.Background(), store, r, true)
	if err != nil || !more || len(store.work) != 3 || len(store.writes) != 32 {
		t.Fatal("one step must delete at most 32 joined work", err)
	}
	for _, q := range store.queries {
		if strings.Contains(q, "FROM agenteam_skill.installation_attempts") {
			t.Fatal("same step also consumed attempt history")
		}
	}
	for _, q := range store.writes {
		if strings.Contains(q, "installations") || strings.Contains(q, "installation_attempts") {
			t.Fatal("history step removed original anchor")
		}
	}
	more, err = compressInstallationHistory(context.Background(), store, r, true)
	if err != nil || !more || len(store.work) != 0 || len(store.writes) != 35 {
		t.Fatal("next step must make bounded durable progress", err)
	}
}

func TestInstalledCleanupUnpublishedAndReferenceGates(t *testing.T) {
	_, base, _, _, r := installedReadFixture(t)
	store := &installedCleanupStore{installedReadStore: base, canonicalAbsent: true, assignmentsEmpty: false}
	r.phase = installationReserved
	if err := installationCleanupCore(context.Background(), store, r, false); err != nil {
		t.Fatal("reserved original parent is not a fake published skill", err)
	}
	store.canonicalAbsent = false
	if err := installationCleanupCore(context.Background(), store, r, false); err == nil {
		t.Fatal("unpublished command accepted visible canonical rows")
	}
	joined, err := cleanupWorkJoined(context.Background(), store, r.project)
	if err != nil || joined || len(store.writes) != 0 {
		t.Fatal("retained Agent head/assignment did not block cleanup", err)
	}
	store.assignmentsEmpty = true
	joined, err = cleanupWorkJoined(context.Background(), store, r.project)
	if err != nil || !joined {
		t.Fatal("empty reference set blocked joined work", err)
	}
}

func TestInstalledCleanupFinalAnchorsRequireCompletedOriginalGate(t *testing.T) {
	for _, name := range []string{"pending", "foreign", "published", "reserved", "planned", "sql", "missing-row"} {
		t.Run(name, func(t *testing.T) {
			_, base, _, _, r := installedReadFixture(t)
			store := &installedCleanupStore{installedReadStore: base}
			c := &cleanupRow{id: stateID[oc.CleanupOperation](800), project: r.project, cause: pc.LifecycleCause{OperationID: stateID[pc.Operation](801), ProjectVersion: 2, Action: pc.Delete}, skill: r.skill, revision: r.revision, object: r.object, upload: r.upload, phase: cleanupCompleted, version: 3}
			switch name {
			case "pending":
				c.phase = cleanupPending
			case "foreign":
				c.skill = stateID[pc.Skill](802)
			case "reserved":
				r.phase = installationReserved
			case "planned":
				r.phase = installationPlanned
				r.object = oc.ObjectID{}
				r.upload = oc.UploadID{}
				r.attempt = oc.AttemptID{}
				c = nil
			case "sql":
				store.failAt = 2
			case "missing-row":
				store.zeroAffected = true
			}
			err := deleteInstallationCore(context.Background(), store, r, c)
			if name == "pending" || name == "foreign" {
				if err == nil || len(store.writes) != 0 {
					t.Fatal("wrong gate consumed original anchors")
				}
				return
			}
			if name == "sql" || name == "missing-row" {
				if err == nil {
					t.Fatal("failed deletion upgraded to completion")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 5
			if name == "reserved" {
				want = 3
			}
			if name == "planned" {
				want = 1
			}
			if len(store.writes) != want || !strings.HasPrefix(store.writes[want-1], "DELETE FROM agenteam_skill.installations ") {
				t.Fatal("source anchors removed in wrong bounded order")
			}
			if name == "reserved" || name == "planned" {
				for _, q := range store.writes {
					if strings.Contains(q, "agenteam_skill.skills ") || strings.Contains(q, "agenteam_skill.revisions ") {
						t.Fatal("unfinished installation treated as published Skill")
					}
				}
			}
		})
	}
}
