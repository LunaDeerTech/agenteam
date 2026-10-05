//go:build integration

package security_test

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretProjectCleanupPendingReferencesBatchesAndGate(t *testing.T) {
	f := newSecretFixture(t)
	first := f.create(t, []byte("first retained"))
	for range 100 {
		f.create(t, []byte("project value"))
	}
	late, err := f.secret.PrepareWrite(auditContext(t), f.request(t, sc.Create, []byte("late")))
	if err != nil {
		t.Fatal(err)
	}
	system := f.request(t, sc.Create, []byte("system intact"))
	system.Scope = identity.SystemScope()
	system.Identity, err = foundation.NewCommandIdentity("secret", []string{f.actor.Details().UserID}, "create", foundation.IdempotencyKey(newID[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.secret.ExecuteWrite(auditContext(t), system); err != nil {
		t.Fatal(err)
	}
	owner, serviceActor := f.bind(t, first.Metadata.CredentialRef, sc.Model)
	lease := f.acquire(t, first.Metadata.CredentialRef, owner, serviceActor)
	result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		return f.secret.RetainReferenceInTx(ctx, tx, f.actor, first.Metadata.CredentialRef, sc.Model, owner.Details().ID)
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	operation := newID[sc.LifecycleOperation](t)
	cause, _ := sc.NewLifecycleCause(operation, 2, true)
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	actor, _ := registration.Actor(operation.String(), f.scope)
	_, err = f.secret.CleanupProject(auditContext(t), f.actor, cause, f.project, nil)
	requireCode(t, err, foundation.Forbidden)
	_, err = f.secret.CleanupProject(auditContext(t), actor, cause, f.project, nil)
	requireCode(t, err, foundation.InvalidState)
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='deleting',stopped=true,operation_id=$1,version=2`, operation.String()); err != nil {
		t.Fatal(err)
	}
	report, err := f.secret.CleanupProject(auditContext(t), actor, cause, f.project, nil)
	if err != nil || report.Completed {
		t.Fatal("live references reported cleared", err)
	}
	_, result = f.applyModelUsage(t, f.secret, f.modelLeaseRequest(serviceActor, lease, owner, sc.ReleaseLeaseUsage))
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	report, err = f.secret.CleanupProject(auditContext(t), actor, cause, f.project, &report.Checkpoint)
	if err != nil || report.Completed {
		t.Fatal("live config reference forgotten", err)
	}
	result = f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		return f.secret.ReleaseReferenceInTx(ctx, tx, serviceActor, first.Metadata.CredentialRef, sc.Model, owner.Details().ID)
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	report, err = f.secret.CleanupProject(auditContext(t), actor, cause, f.project, &report.Checkpoint)
	if err != nil || report.Completed {
		t.Fatal("first batch", err)
	}
	var remaining int
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_secret.secret_payloads WHERE project_id=$1`, f.project.String()).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("first payload batch %d %v", remaining, err)
	}
	bad := report.Checkpoint
	bad.OperationID = newID[sc.LifecycleOperation](t)
	_, err = f.secret.CleanupProject(auditContext(t), actor, cause, f.project, &bad)
	requireCode(t, err, foundation.InvalidArgument)
	report, err = f.secret.CleanupProject(auditContext(t), actor, cause, f.project, &report.Checkpoint)
	if err != nil || !report.Completed {
		t.Fatal("final cleanup", err)
	}
	result = f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, late.RequiredLocks()); err != nil {
			return err
		}
		_, err := f.secret.ApplyPreparedWriteInTx(ctx, tx, late)
		return err
	})
	requireCode(t, result.Fault(), foundation.NotFound)
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_secret.secret_payloads WHERE scope='system'`).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatal("System affected by Project deletion", err)
	}
	report, err = f.secret.CleanupProject(auditContext(t), actor, cause, f.project, &report.Checkpoint)
	if err != nil || !report.Completed {
		t.Fatal("cleanup retry", err)
	}
}
