//go:build integration

package security_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Outbox cleanup is the last Project participant. Its minimal receipt must
// retain the committed Audit ID without retaining the earlier Audit row.
func TestOutboxReceiptSurvivesEarlierAuditProjectCleanup(t *testing.T) {
	f := newAuditFixture(t)
	delivery, attempt, process := newID[oc.Delivery](t), newID[oc.Attempt](t), newID[oc.Process](t)
	catalog := event.NewCatalog()
	type fixturePayload struct {
		ObjectID string `json:"object_id"`
	}
	et, err := event.DefineEvent(catalog, event.Definition[fixturePayload]{Schema: event.Schema{Producer: "verify", EventType: "verify.changed", AggregateType: "verify", Version: 1}, Codec: event.JSONCodec[fixturePayload]{}, Validate: func(v fixturePayload) error {
		_, err := foundation.ParseID[event.Aggregate](v.ObjectID)
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	project, err := foundation.ParseID[event.Project](f.project.String())
	if err != nil {
		t.Fatal(err)
	}
	aggregate := newID[event.Aggregate](t)
	at, _ := foundation.NewInstant(time.Now())
	version := foundation.Version(1)
	e, err := event.NewEvent(et, event.Header{EventID: newID[event.EventIdentity](t), EventType: "verify.changed", SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "verify", AggregateID: aggregate, AggregateVersion: &version}, fixturePayload{ObjectID: aggregate.String()})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := ac.OutboxRequeueMetadata(ac.OutboxRequeueFields{DeliveryID: delivery.String(), EventID: e.Header().EventID.String(), HandlerID: "verify.audit-cleanup", FromState: "failed", RedriveCycle: 1, Reason: ac.OperatorRetry})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := ac.NewResource(ac.OutboxDeliveryResource, delivery.String())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: f.scope, Actor: f.actor, Action: ac.OutboxDeliveryRequeue, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	header, err := e.HeaderJSON()
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := oc.SemanticDigest(f.actor, e)
	if err != nil {
		t.Fatal(err)
	}
	actorKey, err := oc.StableActor(f.actor)
	if err != nil {
		t.Fatal(err)
	}
	commandHash := oc.DigestBytes([]byte("verify-requeue-command"))
	var receipt ac.AppendReceipt
	result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		projectLock, _ := foundation.ProjectLock(f.project.String())
		if err := f.store.Acquire(ctx, tx, projectLock, foundation.Shared); err != nil {
			return err
		}
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		// Fixture-only durable states for the not-yet-implemented B02 requeue:
		// the production schema validates a prior failed/joined attempt and a
		// committed redrive receipt. Audit itself uses its real typed service.
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.events(id,producer,event_type,schema_version,scope,project_id,aggregate_type,aggregate_id,aggregate_version,occurred_at,header,payload,semantic_digest,stable_actor) VALUES($1,'verify','verify.changed',1,'project',$2,'verify',$3,1,$4,$5::jsonb,$6,$7,$8)`, e.Header().EventID.String(), f.project.String(), aggregate.String(), at.Time(), string(header), e.PayloadBytes(), semantic.String(), actorKey); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.handlers(id,effect,ordering_policy,declaration_digest) VALUES('verify.audit-cleanup','canonical_converge','version_guarded',$1)`, oc.DigestBytes([]byte("verify-handler")).String()); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.deliveries(id,event_id,handler_id,scope,project_id) VALUES($1,$2,'verify.audit-cleanup','project',$3)`, delivery.String(), e.Header().EventID.String(), f.project.String()); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.attempts(id,delivery_id,process_id,fence,redrive_cycle,cycle_number,lifetime_number,deadline,handler_returned_at,joined_at,finished_at,checkpoint) VALUES($1,$2,$3,1,0,1,1,clock_timestamp()+interval '20 seconds',clock_timestamp(),clock_timestamp(),clock_timestamp(),'failed')`, attempt.String(), delivery.String(), process.String()); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET phase='pending',version=3,redrive_cycle=1,cycle_attempts=0,lifetime_attempts=1,current_attempt_id=$2,fence=1 WHERE id=$1`, delivery.String(), attempt.String()); err != nil {
			return err
		}
		receipt, err = f.service.AppendInTx(ctx, tx, entry, appendKey(t, ac.OutboxProducer))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.requeue_commands(command_hash,semantic_digest,user_id,delivery_id,from_state,expected_version,redrive_cycle,result_version,reason_code,audit_id) VALUES($1,$2,$3,$4,'failed',2,1,3,'operator_retry',$5)`, commandHash.String(), semantic.String(), f.actor.Details().UserID, delivery.String(), receipt.AuditID.String())
		return err
	})
	if result.State() != foundation.Committed {
		t.Fatal("fixture did not establish committed Audit and receipt", result.Fault())
	}
	if _, result = appendAudit(t, f, f.entry(t, f.actor, identity.SystemScope()), appendKey(t, ac.SecretProducer)); result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	operation := newID[ac.LifecycleOperation](t)
	cause, err := ac.NewLifecycleCause(operation, ac.Delete, 2)
	if err != nil {
		t.Fatal(err)
	}
	role, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := role.Actor(operation.String(), f.scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='deleting',stopped=true,operation_id=$1,version=2 WHERE id=$2`, operation.String(), f.project.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Exec(auditContext(t), `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase) VALUES($1,$2,'delete',2,'stopped')`, f.project.String(), operation.String()); err != nil {
		t.Fatal(err)
	}
	report, err := f.service.CleanupProject(auditContext(t), actor, cause, f.project, nil)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			t.Logf("Audit cleanup database evidence: SQLSTATE=%s constraint=%s", pg.Code, pg.ConstraintName)
		}
		t.Errorf("Audit participant must complete before final Outbox cleanup: %v", err)
	} else if report.State != ac.CleanupCompleted {
		t.Errorf("single Audit row did not finish cleanup: %s", report.State)
	}
	var projectRows, systemRows int
	if err := f.store.QueryRow(auditContext(t), `SELECT count(*) FILTER(WHERE scope='project'),count(*) FILTER(WHERE scope='system') FROM agenteam_audit.audit_records`).Scan(&projectRows, &systemRows); err != nil {
		t.Fatal(err)
	}
	if projectRows != 0 || systemRows != 1 {
		t.Errorf("expected project Audit zero, system Audit one; got %d/%d", projectRows, systemRows)
	}
	var savedAudit string
	if err := f.store.QueryRow(auditContext(t), `SELECT audit_id::text FROM agenteam_outbox.requeue_commands WHERE command_hash=$1`, commandHash.String()).Scan(&savedAudit); err != nil || savedAudit != receipt.AuditID.String() {
		t.Fatal("Audit cleanup removed or rewrote Outbox completion fact", err)
	}
}
