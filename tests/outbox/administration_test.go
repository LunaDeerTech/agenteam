//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type adminAuthority struct{ f *fixture }

func (a adminAuthority) sql(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if !tx.Valid() {
		return a.f.store, nil
	}
	return a.f.store.InTx(tx)
}
func (a adminAuthority) RequireCurrentSession(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return foundation.NewFault(foundation.Unauthenticated, foundation.NotStarted)
	}
	x, err := a.sql(tx)
	if err != nil {
		return err
	}
	var allowed bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM outbox_fixture.admin WHERE user_id=$1 AND session_id=$2 AND enabled)`, actor.Details().UserID, actor.Details().SessionID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return foundation.NewFault(foundation.SessionRevoked, foundation.NotStarted)
	}
	return nil
}
func (a adminAuthority) AuthorizeSystem(ctx context.Context, tx foundation.Tx, actor identity.Actor, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if err := a.RequireCurrentSession(ctx, tx, actor); err != nil {
		return identity.AccessGrant{}, err
	}
	x, _ := a.sql(tx)
	var allowed bool
	if err := x.QueryRow(ctx, `SELECT admin FROM outbox_fixture.admin WHERE user_id=$1`, actor.Details().UserID).Scan(&allowed); err != nil {
		return identity.AccessGrant{}, err
	}
	if !allowed {
		return identity.AccessGrant{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	at, _ := foundation.NewInstant(time.Now())
	return identity.NewAccessGrant(actor, identity.SystemScope(), intent, at, 1)
}
func (a adminAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, project identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if err := a.RequireCurrentSession(ctx, tx, actor); err != nil {
		return identity.AccessGrant{}, err
	}
	x, _ := a.sql(tx)
	var visible, active bool
	if err := x.QueryRow(ctx, `SELECT visible,active FROM outbox_fixture.authority WHERE id=$1`, project.String()).Scan(&visible, &active); err != nil {
		return identity.AccessGrant{}, err
	}
	if !visible {
		return identity.AccessGrant{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if intent == identity.Mutate && !active {
		return identity.AccessGrant{}, foundation.NewFault(foundation.ProjectNotActive, foundation.NotStarted)
	}
	at, _ := foundation.NewInstant(time.Now())
	scope, _ := identity.InProject(project)
	return identity.NewAccessGrant(actor, scope, intent, at, 1)
}
func (a adminAuthority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	d := entry.Fields()
	project, _ := foundation.ParseID[identity.Project](d.Scope.Details().ProjectID)
	k, _ := foundation.ProjectLock(project.String())
	if err := a.f.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: k, Mode: foundation.Shared}}); err != nil {
		return err
	}
	_, err := a.AuthorizeProject(ctx, tx, d.Actor, project, identity.Mutate)
	return err
}
func (adminAuthority) CheckServiceLookup(context.Context, identity.Actor, identity.Scope, ac.AppendKey) error {
	return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
}
func (adminAuthority) CheckCleanupInTx(context.Context, foundation.Tx, identity.Actor, ac.LifecycleCause, identity.ProjectID) error {
	return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
}
func administration(t *testing.T, f *fixture, store *postgres.Store, failAudit bool) (*outbox.Service, *audit.Service) {
	t.Helper()
	f.sql(t, `CREATE TABLE IF NOT EXISTS outbox_fixture.admin(user_id uuid PRIMARY KEY,session_id uuid NOT NULL,enabled boolean NOT NULL DEFAULT true,admin boolean NOT NULL DEFAULT true);`)
	f.sql(t, `INSERT INTO outbox_fixture.admin(user_id,session_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, f.actor.Details().UserID, f.actor.Details().SessionID)
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auth := adminAuthority{f}
	auditor, err := audit.New(store, keys, audit.Authorizations{Sessions: auth, System: auth, Projects: auth})
	if err != nil {
		t.Fatal(err)
	}
	var appendAudit ac.Appender = auditor
	if failAudit {
		appendAudit = rejectAudit{}
	}
	svc, err := outbox.New(store, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": f.auth}, Projects: f.auth, Processes: f.auth, Sessions: auth, System: auth, Audit: appendAudit, Cursors: keys})
	if err != nil {
		t.Fatal(err)
	}
	return svc, auditor
}

type rejectAudit struct{}

func (rejectAudit) AppendInTx(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) (ac.AppendReceipt, error) {
	return ac.AppendReceipt{}, errors.New("private fixture audit failure")
}
func failedDelivery(t *testing.T, f *fixture, svc *outbox.Service, project bool, name string) (oc.DeliveryID, foundation.Version) {
	t.Helper()
	h := f.handler(name)
	h.mode = "reject"
	if _, err := svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
		t.Fatal(err)
	}
	e := f.event(t, project, 1, "private event payload")
	_, result := f.append(t, svc, f.store, f.actor, e)
	state(t, result, foundation.Committed)
	delivery := f.delivery(t, e, name)
	f.claim(t, delivery)
	plan, err := svc.PrepareDelivery(ctxFor(t), delivery)
	if err != nil {
		t.Fatal(err)
	}
	result, _ = svc.ApplyDelivery(ctxFor(t), plan)
	state(t, result, foundation.NotCommitted)
	// The test exercises the administration of a genuinely returned handler;
	// it records the same terminal checkpoint as the dispatcher's owned stage.
	f.sql(t, `UPDATE agenteam_outbox.attempts SET handler_returned_at=clock_timestamp(),joined_at=clock_timestamp(),finished_at=clock_timestamp(),checkpoint='dead_letter',safe_reason='source_terminal' WHERE delivery_id=$1`, delivery.String())
	f.sql(t, `UPDATE agenteam_outbox.deliveries SET phase='dead_letter',version=version+1,safe_reason='source_terminal',last_at=clock_timestamp() WHERE id=$1`, delivery.String())
	var version int64
	if err = f.store.QueryRow(ctxFor(t), `SELECT version FROM agenteam_outbox.deliveries WHERE id=$1`, delivery.String()).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return delivery, foundation.Version(version)
}
func commandMeta(t *testing.T) foundation.CommandMeta {
	t.Helper()
	return foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(id[struct{}](t).String())}
}
func TestOutboxRequeueCurrentAuthorizationReceiptAndAudit(t *testing.T) {
	f := newFixture(t)
	svc, _ := administration(t, f, f.store, false)
	delivery, version := failedDelivery(t, f, svc, true, "redrive")
	meta := commandMeta(t)
	receipt, err := svc.Requeue(ctxFor(t), f.actor, meta, delivery, version, oc.OperatorRetry)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Cycle != 1 || receipt.Version != version+1 || f.count(t, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='outbox.delivery.requeue'`) != 1 {
		t.Fatal("requeue and Audit did not commit exactly once")
	}
	user, _ := foundation.ParseID[identity.User](f.actor.Details().UserID)
	actor, _ := identity.NewHuman(user, id[identity.Session](t))
	f.sql(t, `UPDATE outbox_fixture.admin SET session_id=$1`, actor.Details().SessionID)
	f.sql(t, `UPDATE outbox_fixture.authority SET active=false WHERE id=$1`, f.project.String())
	meta.RequestID = id[foundation.Request](t)
	again, err := svc.Requeue(ctxFor(t), actor, meta, delivery, version, oc.OperatorRetry)
	if err != nil || again != receipt {
		t.Fatalf("visible replay changed with session/trace/archive: %v", err)
	}
	_, err = svc.Requeue(ctxFor(t), actor, meta, delivery, version, oc.SchemaAvailable)
	code(t, err, foundation.IdempotencyKeyReused)
	_, err = svc.Requeue(ctxFor(t), actor, commandMeta(t), delivery, receipt.Version, oc.OperatorRetry)
	code(t, err, foundation.ProjectNotActive)
	_, err = svc.Requeue(ctxFor(t), f.actor, meta, delivery, version, oc.OperatorRetry)
	code(t, err, foundation.SessionRevoked)
	f.sql(t, `UPDATE outbox_fixture.authority SET visible=false WHERE id=$1`, f.project.String())
	_, err = svc.Requeue(ctxFor(t), actor, meta, delivery, version, oc.OperatorRetry)
	code(t, err, foundation.Forbidden)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.requeue_commands`) != 1 || f.count(t, `SELECT count(*) FROM agenteam_audit.audit_records`) != 1 {
		t.Fatal("replay/denial added facts")
	}
}
func TestOutboxRequeueAuditFailureAndInvalidPhasesRollback(t *testing.T) {
	f := newFixture(t)
	svc, _ := administration(t, f, f.store, true)
	delivery, version := failedDelivery(t, f, svc, false, "audit-failure")
	_, err := svc.Requeue(ctxFor(t), f.actor, commandMeta(t), delivery, version, oc.OperatorRetry)
	code(t, err, foundation.DependencyUnavailable)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.requeue_commands`) != 0 || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND phase='dead_letter' AND version=$2`, delivery.String(), int64(version)) != 1 {
		t.Fatal("failed Audit changed delivery")
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatal("raw Audit failure exposed")
	}
	svc, _ = administration(t, f, f.store, false)
	h := f.handler("audit-failure")
	if _, err = svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Requeue(ctxFor(t), f.actor, commandMeta(t), delivery, version+1, oc.OperatorRetry)
	code(t, err, foundation.VersionConflict)
	receipt, err := svc.Requeue(ctxFor(t), f.actor, commandMeta(t), delivery, version, oc.OperatorRetry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Requeue(ctxFor(t), f.actor, commandMeta(t), delivery, receipt.Version, oc.OperatorRetry)
	code(t, err, foundation.InvalidState)
}
func TestOutboxDiagnosticsSnapshotFiltersCursorAndCurrentPermission(t *testing.T) {
	f := newFixture(t)
	svc, _ := administration(t, f, f.store, false)
	for i := 0; i < 3; i++ {
		failedDelivery(t, f, svc, true, "diagnostic")
	}
	// One retained event has no subscriptions. It belongs in produced throughput,
	// but a handler filter excludes it and must not duplicate produced events.
	systemEvent := f.event(t, false, 1, "system payload must remain private")
	_, result := f.append(t, svc, f.store, f.actor, systemEvent)
	state(t, result, foundation.Committed)
	project, _ := foundation.ParseID[identity.Project](f.project.String())
	scope, _ := identity.InProject(project)
	page, err := svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" || page.Summary.DeadLetterDeliveries != 3 || page.Summary.PendingEvents != 0 || len(page.Summary.RecentErrors) != 3 || page.Summary.HandlerLatency[0].Count != 3 || page.Summary.EventThroughput[0].ProducedEvents != 3 {
		t.Fatalf("wrong real aggregate %+v", page.Summary)
	}
	later, err := svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(later.Items) != 2 || later.NextCursor != "" || later.Items[0].ID == page.Items[0].ID || later.Summary.DeadLetterDeliveries != 3 {
		t.Fatal("cursor page or summary changed with limit")
	}
	none, err := svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{Phase: oc.Succeeded}, foundation.PageRequest{Limit: 1})
	if err != nil || len(none.Items) != 0 || none.Summary.DeadLetterDeliveries != 3 {
		t.Fatal("phase incorrectly trimmed Summary")
	}
	_, err = svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{HandlerID: "other"}, foundation.PageRequest{Limit: 1, Cursor: page.NextCursor})
	code(t, err, foundation.CursorInvalid)
	_, err = svc.QueryDiagnostics(ctxFor(t), f.actor, identity.SystemScope(), oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 1, Cursor: page.NextCursor})
	code(t, err, foundation.CursorInvalid)
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private event") || strings.Contains(string(raw), "system payload") {
		t.Fatal("diagnostics included payload")
	}
	f.sql(t, `UPDATE outbox_fixture.authority SET visible=false WHERE id=$1`, f.project.String())
	_, err = svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{}, foundation.PageRequest{Limit: 2, Cursor: page.NextCursor})
	code(t, err, foundation.Forbidden)
}
