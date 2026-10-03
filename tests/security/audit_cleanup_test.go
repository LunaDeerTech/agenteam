//go:build integration

package security_test

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestAuditCleanupBatchesGateAndNoLateResurrection(t *testing.T) {
	f := newAuditFixture(t)
	entry := f.entry(t, f.actor, f.scope)
	result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		lock, _ := foundation.ProjectLock(f.project.String())
		if err := f.store.Acquire(ctx, tx, lock, foundation.Shared); err != nil {
			return err
		}
		for range 501 {
			if _, err := f.service.AppendInTx(ctx, tx, entry, appendKey(t, ac.SecretProducer)); err != nil {
				return err
			}
		}
		return nil
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	system := f.entry(t, f.actor, identity.SystemScope())
	if _, r := appendAudit(t, f, system, appendKey(t, ac.SecretProducer)); r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	operation := newID[ac.LifecycleOperation](t)
	cause, _ := ac.NewLifecycleCause(operation, ac.Delete, 2)
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	service, _ := registration.Actor(operation.String(), f.scope)
	_, err := f.service.CleanupProject(auditContext(t), f.actor, cause, f.project, nil)
	requireCode(t, err, foundation.Forbidden)
	_, err = f.service.CleanupProject(auditContext(t), service, cause, f.project, nil)
	requireCode(t, err, foundation.InvalidState)
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='archived',stopped=true,operation_id=$1,version=2`, operation.String()); err != nil {
		t.Fatal(err)
	}
	archive, _ := ac.NewLifecycleCause(operation, ac.Archive, 2)
	_, err = f.service.CleanupProject(auditContext(t), service, archive, f.project, nil)
	requireCode(t, err, foundation.Forbidden)
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='deleting'`); err != nil {
		t.Fatal(err)
	}
	report, err := f.service.CleanupProject(auditContext(t), service, cause, f.project, nil)
	if err != nil || report.State != ac.CleanupPending || report.Checkpoint.LastID == nil {
		t.Fatalf("first cleanup: %v", err)
	}
	var projectCount, systemCount int
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FILTER(WHERE scope='project'),count(*) FILTER(WHERE scope='system') FROM agenteam_audit.audit_records`).Scan(&projectCount, &systemCount); err != nil || projectCount != 1 || systemCount != 1 {
		t.Fatal("cleanup batch or system scope wrong")
	}
	bad := report.Checkpoint
	bad.ProjectID = newID[identity.Project](t)
	_, err = f.service.CleanupProject(auditContext(t), service, cause, f.project, &bad)
	requireCode(t, err, foundation.InvalidArgument)
	report, err = f.service.CleanupProject(auditContext(t), service, cause, f.project, &report.Checkpoint)
	if err != nil || report.State != ac.CleanupCompleted {
		t.Fatalf("final cleanup: %v", err)
	}
	// A delayed Agent event still goes through the persisted gate after deletion.
	agent, _ := identity.NewAgentRun(f.project, newID[identity.Agent](t), newID[identity.Execution](t))
	fields := entry.Fields()
	fields.Actor = agent
	fields.Action = ac.SecretResolve
	fields.Metadata, _ = ac.SecretResolveMetadata(newID[struct{}](t).String(), ac.Model, "")
	late, _ := ac.NewEntry(fields)
	_, result = appendAudit(t, f, late, appendKey(t, ac.SecretProducer))
	if result.State() != foundation.NotCommitted || result.Fault().Code != foundation.ProjectNotActive {
		t.Fatal("late append resurrected deleted audit")
	}
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='project'`).Scan(&projectCount); err != nil || projectCount != 0 {
		t.Fatal("project rows remain")
	}
}

// Regression from the independent real PostgreSQL COMMIT-response-loss probe.
func TestAuditCleanupUnresolvedCommitState(t *testing.T) {
	f := newAuditFixture(t)
	if _, r := appendAudit(t, f, f.entry(t, f.actor, f.scope), appendKey(t, ac.SecretProducer)); r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	operation := newID[ac.LifecycleOperation](t)
	cause, _ := ac.NewLifecycleCause(operation, ac.Delete, 2)
	reg, _ := identity.RegisterService(identity.ProjectLifecycle)
	actor, _ := reg.Actor(operation.String(), f.scope)
	if _, err := f.store.Exec(auditContext(t), "UPDATE audit_fixture.projects SET state='deleting',stopped=true,operation_id=$1,version=2 WHERE id=$2", operation.String(), f.project.String()); err != nil {
		t.Fatal(err)
	}
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), true)
	u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
	u.Host = proxy.listener.Addr().String()
	store := openAuditStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	service, err := audit.New(store, auditKeys(t), audit.Authorizations{Projects: &auditAuthority{store}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := service.CleanupProject(auditContext(t), actor, cause, f.project, nil); done <- e }()
	select {
	case <-proxy.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("real committed cleanup barrier missing")
	}
	var count int
	if err := f.store.QueryRow(auditContext(t), "SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1", f.project.String()).Scan(&count); err != nil || count != 0 {
		t.Fatal("fixture did not prove deletion actually committed")
	}
	// Refuse only the follow-up verification transaction, while the already
	// admitted first transaction is still waiting on its real COMMIT reply.
	store.StopAdmission()
	close(proxy.release)
	select {
	case got := <-done:
		var fault *foundation.Fault
		if !errors.As(got, &fault) || fault.Code != foundation.CommitUnknown {
			t.Fatalf("missing unresolved-commit classification: %v", got)
		}
		if fault.CommitState != foundation.Unknown {
			t.Fatalf("actually committed deletion with unavailable verification: code=%s commit_state=%s, want unknown", fault.Code, fault.CommitState)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup recovery did not terminate")
	}
}
