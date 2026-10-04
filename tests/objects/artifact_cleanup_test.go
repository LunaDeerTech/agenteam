//go:build integration

package objects_test

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestArtifactProspectiveCancelAndProjectCleanupDoNotRevive(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := contextFor(t)
	target, err := f.artifact.BeginUpload(ctx, f.invocation(t), command(t, "cancel-upload"), art.Display{Name: "cancel.txt"}, "text/plain", 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	put, err := f.artifact.UploadPayload(ctx, f.invocation(t), target, io.NopCloser(strings.NewReader("cancel")))
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.artifact.CancelUpload(ctx, f.invocation(t), target)
	if err != nil || state != oc.CleanupCompleted {
		t.Fatal("cancel prospective", state, err)
	}
	_, err = f.artifact.CreateFromUpload(ctx, f.invocation(t), command(t, "revive-upload"), art.Display{Name: "cancel.txt"}, put.Receipt)
	if err == nil {
		t.Fatal("cancelled receipt consumed")
	}
	before := f.proxy.puts.Load()
	_, err = f.artifact.UploadPayload(ctx, f.invocation(t), target, io.NopCloser(strings.NewReader("cancel")))
	if err == nil || f.proxy.puts.Load() != before {
		t.Fatal("cancelled upload revived")
	}
	original := f.create(t, "clean-source", "text/plain", strings.Repeat("s", 256<<10))
	ref, _ := original.Details().Reference.BusinessFile()
	copyCommand := command(t, "clean-copy")
	display := art.Display{Name: "copy.txt"}
	copied, err := f.artifact.CreateFromSource(ctx, f.invocation(t), copyCommand, display, ref)
	if err != nil {
		t.Fatal(err)
	}
	// Existing reader protects real payload even after canonical business rows
	// have been removed by a valid Project lifecycle operation.
	owner, _ := oc.NewObjectOwner(oc.Artifact, copied.Details().Reference.ArtifactID.String(), f.project.String())
	reader, err := f.objects.ReadObject(ctx, f.actor, owner, copied.Details().Object.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var active int
	if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active' AND owner_kind='reader'`, copied.Details().Object.ID.String()).Scan(&active); err != nil || active != 1 {
		t.Fatal("fixture failed to retain exact actual reader", active, err)
	}
	operation := id[oc.CleanupOperation](t)
	f.sql(t, `UPDATE object_fixture.projects SET state='deleting',operation_id=$1,version=2`, operation.String())
	cause, _ := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: f.project, OperationID: operation, Version: 2})
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(f.project)
	actor, _ := registration.Actor(operation.String(), scope)
	out, err := f.artifact.CleanupProject(ctx, actor, cause)
	if err != nil || out.State != oc.CleanupPending {
		t.Fatal("reader protection lost", out, err)
	}
	var visible int
	f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_artifact.artifacts`).Scan(&visible)
	if visible != 0 {
		t.Fatal("business canonical still visible")
	}
	var objectState string
	f.store.QueryRow(ctx, `SELECT state FROM agenteam_object.objects WHERE id=$1`, copied.Details().Object.ID.String()).Scan(&objectState)
	if objectState == "deleted" {
		t.Fatal("active reader payload deleted")
	}
	if _, err = io.ReadAll(reader); err != nil {
		t.Fatal("admitted reader cannot finish", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4 && out.State != oc.CleanupCompleted; i++ {
		out, err = f.artifact.CleanupProject(ctx, actor, cause)
		if err != nil {
			t.Fatal("cleanup convergence", err)
		}
	}
	if out.State != oc.CleanupCompleted {
		t.Fatal("cleanup never completed", out)
	}
	count := 0
	err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1`, f.project.String()).Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("metadata removal claimed without payload cleanup")
	}
	gets, puts, resolves := f.proxy.gets.Load(), f.proxy.puts.Load(), f.resolver.resolves.Load()
	_, err = f.artifact.CreateFromSource(ctx, f.invocation(t), copyCommand, display, ref)
	if err == nil || f.proxy.gets.Load() != gets || f.proxy.puts.Load() != puts || f.resolver.resolves.Load() != resolves {
		t.Fatal("deleted target replay revived/touched source")
	}
}

func TestArtifactProjectCleanupBoundedBatchAndForeignReader(t *testing.T) {
	f := newArtifactFixtureWithProxyBudget(t, 2*time.Minute)
	setup, stopSetup := context.WithTimeout(context.Background(), time.Minute)
	defer stopSetup()
	body := strings.Repeat("foreign-reader", 30000)
	protected := f.create(t, "batch-protected", "text/plain", body)
	for i := 0; i < 100; i++ {
		_, err := f.artifact.CreateFromContent(setup, f.invocation(t), command(t, "batch-"+strconv.Itoa(i)), art.Display{Name: "batch.txt"}, "text/plain", "x")
		if err != nil {
			t.Fatal("bounded batch setup", err)
		}
	}
	stopSetup()
	foreign := f.bindOn(t, f.store, nil, nil)
	owner, _ := oc.NewObjectOwner(oc.Artifact, protected.Details().Reference.ArtifactID.String(), f.project.String())
	f.proxy.mode.Store(proxyHoldReadBody)
	readCtx, stopRead := context.WithTimeout(context.Background(), time.Minute)
	defer stopRead()
	reader, err := foreign.objects.ReadObject(readCtx, f.actor, owner, protected.Details().Object.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	select {
	case <-f.proxy.began:
	case <-time.After(3 * time.Second):
		t.Fatal("foreign reader did not reach actual storage barrier")
	}
	var active int
	ctx := contextFor(t)
	if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, protected.Details().Object.ID.String()).Scan(&active); err != nil || active != 1 {
		t.Fatal("foreign actual lease absent", active, err)
	}
	operation := id[oc.CleanupOperation](t)
	f.sql(t, `UPDATE object_fixture.projects SET state='deleting',operation_id=$1,version=2`, operation.String())
	cause, _ := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: f.project, OperationID: operation, Version: 2})
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(f.project)
	actor, _ := registration.Actor(operation.String(), scope)
	out, err := f.artifact.CleanupProject(ctx, actor, cause)
	if err != nil || out.State != oc.CleanupPending {
		t.Fatal("first cleanup batch", out, err)
	}
	var visible int
	if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_artifact.artifacts`).Scan(&visible); err != nil || visible != 1 {
		t.Fatal("cleanup did not bound business batch at 100", visible, err)
	}
	for i := 0; i < 4; i++ {
		// Each explicitly bounded cleanup call is a separate maintenance turn,
		// after the independently bounded setup. No operation extends its budget.
		out, err = f.artifact.CleanupProject(contextFor(t), actor, cause)
		if err != nil || out.State != oc.CleanupPending {
			t.Fatal("foreign live lease bypassed", out, err)
		}
	}
	var objects int
	if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.objects`).Scan(&objects); err != nil || objects != 1 {
		t.Fatal("protected object blocked unrelated physical cleanup", objects, err)
	}
	if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active'`, protected.Details().Object.ID.String()).Scan(&active); err != nil || active != 1 {
		t.Fatal("foreign lease incorrectly released", active, err)
	}
	f.proxy.release()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != body {
		t.Fatal("foreign admitted read lost payload", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	ctx = contextFor(t)
	for i := 0; i < 4 && out.State != oc.CleanupCompleted; i++ {
		out, err = f.artifact.CleanupProject(ctx, actor, cause)
		if err != nil {
			t.Fatal(err)
		}
	}
	if out.State != oc.CleanupCompleted {
		t.Fatal("joined foreign lease did not permit completion", out)
	}
	if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.objects`).Scan(&objects); err != nil || objects != 0 {
		t.Fatal("payload cleanup still incomplete", objects, err)
	}
}
func TestArtifactPendingCreationCancelPreventsLaterPublication(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := contextFor(t)
	cmd := command(t, "cancel-create")
	display := art.Display{Name: "pending.txt"}
	// The candidate is durably reserved/uploaded but the business+reference+Audit
	// publication rolls back as one transaction.
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=false`)
	_, err := f.artifact.CreateFromContent(ctx, f.invocation(t), cmd, display, "text/plain", "unpublished")
	if err == nil {
		t.Fatal("publish without Audit")
	}
	var artifacts, canonical int
	f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts),(SELECT count(*) FROM agenteam_object.object_references WHERE kind='canonical')`).Scan(&artifacts, &canonical)
	if artifacts != 0 || canonical != 0 {
		t.Fatal("failed Audit leaked business binding")
	}
	f.sql(t, `UPDATE object_fixture.projects SET audit_allowed=true`)
	state, err := f.artifact.CancelCreation(ctx, f.invocation(t), cmd.IdempotencyKey)
	if err != nil || state != oc.CleanupCompleted {
		t.Fatal("cancel pending", state, err)
	}
	puts := f.proxy.puts.Load()
	_, err = f.artifact.CreateFromContent(ctx, f.invocation(t), cmd, display, "text/plain", "unpublished")
	requireCode(t, err, foundation.ResourceDeleted)
	if f.proxy.puts.Load() != puts {
		t.Fatal("cancelled command emitted PUT")
	}
}
