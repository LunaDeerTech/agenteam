//go:build integration

package objects_test

import (
	"io"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectProjectStopOptionalBindingKeepsOrdinaryObjects(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "optional-binding", "body")
	replay := f.put(t, "optional-binding", "body")
	if replay.Meta.ID != stored.Meta.ID {
		t.Fatal("ordinary replay changed identity")
	}
	reader, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil || string(got) != "body" {
		t.Fatal("ordinary reader", err)
	}
	if projectWorkCount(t, f, "preparation", true) < 3 || projectWorkCount(t, f, "reader", true) != 1 {
		t.Fatal("nil optional ports skipped durable work or local join")
	}
	operation := id[oc.ProjectStopOperation](t)
	scope, _ := identity.InProject(f.project)
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	actor, _ := registration.Actor(operation.String(), scope)
	cause, _ := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: f.project, OperationID: operation, Action: oc.ProjectStopArchive, ProjectVersion: 2})
	_, err = f.service.RequestProjectStop(contextFor(t), actor, cause)
	requireCode(t, err, foundation.DependencyUnbound)
}

func TestObjectProjectStopArchiveAndDeleteNormalObjectCompatibility(t *testing.T) {
	f := newFixture(t, false)
	a := newObjectStopAuthority(t, f, f.store)
	s := newObjectStopService(t, f, a, stopServiceOptions{})
	f.service = s
	stored := f.put(t, "preserved-reference", "published")
	actor, archive := activateObjectStop(t, f, oc.ProjectStopArchive)
	stopUntilSettled(t, s, actor, archive)
	r, err := s.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(data) != "published" {
		t.Fatal("archive changed published read", err)
	}
	unknown, _ := oc.NewObjectOwner(oc.Knowledge, id[struct{}](t).String(), f.project.String())
	if _, err = s.PreparePayload(contextFor(t), f.actor, unknown, "text/plain", 1, nil, io.NopCloser(strings.NewReader("x"))); err == nil {
		t.Fatal("unknown owner accepted")
	}
	actor, deleted := activateObjectStop(t, f, oc.ProjectStopDelete)
	stopUntilSettled(t, s, actor, deleted)
	if _, err = s.ReadObject(contextFor(t), f.actor, f.owner, stored.Meta.ID, nil); err == nil {
		t.Fatal("delete opened payload")
	}
	var references int
	var state string
	if err = f.store.QueryRow(contextFor(t), `SELECT o.state,(SELECT count(*) FROM agenteam_object.object_references WHERE object_id=o.id) FROM agenteam_object.objects o WHERE o.id=$1`, stored.Meta.ID.String()).Scan(&state, &references); err != nil || state != "available" || references != 1 {
		t.Fatal("technical stop removed canonical content", err, state, references)
	}
	avatar, _ := oc.NewObjectOwner(oc.Avatar, f.actor.Details().UserID, "")
	f.sql(t, `INSERT INTO object_fixture.owners(id,kind,user_id,existence) VALUES($1,'avatar',$1,'existing')`, f.actor.Details().UserID)
	if _, err = s.PutObject(contextFor(t), f.actor, avatar, command(t, "system-avatar"), "text/plain", 1, nil, io.NopCloser(strings.NewReader("x"))); err != nil {
		t.Fatal("project stop crossed into System", err)
	}
	var misplaced int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.project_work WHERE object_id IN (SELECT id FROM agenteam_object.objects WHERE scope='system')`).Scan(&misplaced); err != nil || misplaced != 0 {
		t.Fatal("System work acquired a Project", err)
	}
}
