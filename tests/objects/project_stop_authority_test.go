//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectProjectStopAuthorityAndReadOnlyInspect(t *testing.T) {
	f := newFixture(t, false)
	a := newObjectStopAuthority(t, f, f.store)
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	actor, cause := activateObjectStop(t, f, oc.ProjectStopArchive)
	before := projectStopSnapshot(t, f)
	if _, err := s.InspectProjectStop(contextFor(t), actor, cause); err == nil {
		t.Fatal("Inspect created an absent gate")
	}
	if before != projectStopSnapshot(t, f) {
		t.Fatal("failed Inspect wrote facts")
	}
	for _, field := range []string{"manifest", "phase", "version", "action"} {
		t.Run(field, func(t *testing.T) {
			switch field {
			case "manifest":
				f.sql(t, `UPDATE object_fixture.stop_causes SET manifest=false`)
			case "phase":
				f.sql(t, `UPDATE object_fixture.stop_causes SET phase='planned'`)
			case "version":
				f.sql(t, `UPDATE object_fixture.stop_causes SET accepted_version=accepted_version+1`)
			case "action":
				f.sql(t, `UPDATE object_fixture.stop_causes SET action='delete'`)
			}
			_, err := s.RequestProjectStop(contextFor(t), actor, cause)
			requireCode(t, err, foundation.Forbidden)
			if before != projectStopSnapshot(t, f) {
				t.Fatal("unauthorized stop mutated technical state")
			}
			f.sql(t, `UPDATE object_fixture.stop_causes SET manifest=true,phase='stopping',accepted_version=$1,action='archive'`, int64(cause.Details().ProjectVersion))
		})
	}
	stopUntilSettled(t, s, actor, cause)
	f.sql(t, `UPDATE object_fixture.stop_causes SET phase='complete'`)
	before = projectStopSnapshot(t, f)
	for range 2 {
		report, err := s.InspectProjectStop(contextFor(t), actor, cause)
		if err != nil || report.Details().State != oc.ProjectStopped {
			t.Fatal("read-only receipt", err)
		}
	}
	if before != projectStopSnapshot(t, f) {
		t.Fatal("read-only Inspect performed a write")
	}
	_, err := s.RequestProjectStop(contextFor(t), actor, cause)
	requireCode(t, err, foundation.Forbidden)
}

func TestObjectProjectStopRestoreKeepsRevocation(t *testing.T) {
	f := newFixture(t, true)
	a := newObjectStopAuthority(t, f, f.store)
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	f.service = s
	prepared, err := s.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 3, nil, io.NopCloser(strings.NewReader("old")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DiscardPrepared(prepared)
	command := command(t, "old-prepared")
	plan := ownerPlan(t, s, f.actor, f.owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &command, Prepared: prepared})
	var attempt oc.UploadAttempt
	result := plannedTx(f.store, s, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		var err error
		attempt, err = s.ReserveUploadInTx(ctx, tx, f.actor, f.owner, command, prepared, plan, locked)
		return err
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	actor, stop := activateObjectStop(t, f, oc.ProjectStopArchive)
	stopUntilSettled(t, s, actor, stop)
	f.sql(t, `UPDATE object_fixture.projects SET state='active',version=version+1,operation_id=NULL`)
	if _, err = s.UploadPrepared(contextFor(t), f.actor, f.owner, prepared, attempt); err == nil {
		t.Fatal("Restore revived old prepared attempt")
	}
	newResult, err := s.PutObject(contextFor(t), f.actor, f.owner, commandMetaForStop(t, "fresh-after-restore"), "text/plain", 3, nil, io.NopCloser(strings.NewReader("new")))
	if err != nil {
		t.Fatal("new generation rejected", err)
	}
	if newResult.Meta.ID == attempt.Details().ObjectID {
		t.Fatal("new command reused revoked native identity")
	}
	var revoked int
	f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.project_work WHERE revoked_by=$1`, stop.Details().OperationID.String()).Scan(&revoked)
	if revoked < 2 {
		t.Fatal("preparation and attempt were not independently revoked")
	}
	_, err = s.RequestProjectStop(contextFor(t), actor, stop)
	requireCode(t, err, foundation.Forbidden)
}

func commandMetaForStop(t *testing.T, key string) foundation.CommandMeta { return command(t, key) }
