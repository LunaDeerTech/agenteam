//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Faults occur only AFTER the named production port has returned success.
// Every test also reads the uncommitted facts through that same live Tx before
// injecting its exact fault; an unrelated earlier failure cannot pass.
type secretAuditTail struct {
	ac.Appender
	after func(context.Context, f.Tx) error
}

func (a *secretAuditTail) AppendInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	r, err := a.Appender.AppendInTx(ctx, tx, e, k)
	if err != nil {
		return ac.AppendReceipt{}, err
	}
	if err = a.after(ctx, tx); err != nil {
		return ac.AppendReceipt{}, err
	}
	return r, nil
}

type secretEventTail struct {
	oc.Appender
	after func(context.Context, f.Tx) error
}

func (a *secretEventTail) AppendEventInTx(ctx context.Context, tx f.Tx, actor i.Actor, e event.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	r, err := a.Appender.AppendEventInTx(ctx, tx, actor, e, p)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	if err = a.after(ctx, tx); err != nil {
		return oc.AppendReceipt{}, err
	}
	return r, nil
}

type secretActivityTail struct {
	pv.ActivityAuthority
	after func(context.Context, f.Tx) error
}

func (a *secretActivityTail) TouchActivityInTx(ctx context.Context, tx f.Tx, actor i.Actor) error {
	if err := a.ActivityAuthority.TouchActivityInTx(ctx, tx, actor); err != nil {
		return err
	}
	return a.after(ctx, tx)
}

func TestSecretVariableOwnerAtomicFacts(t *testing.T) {
	v := newSecretOwnerFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	in := secretCreateInput(t, "ROLLBACK_SECRET", []byte("original-protected-value"))
	created, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "atomic-create", nil), p, in)
	if err != nil {
		t.Fatal(err)
	}
	target := created.Fields().Variable.Fields().ID
	material, err := sc.NewSecretMaterial([]byte("replacement-must-rollback"))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	patch := secretUpdateInput(t, vc.SecretVariableUpdateFields{Value: &material})
	for _, stage := range []string{"after-d10-audit", "after-outbox", "after-activity", "owner-tail"} {
		t.Run(stage, func(t *testing.T) {
			activityDue := stage == "after-activity" || stage == "owner-tail"
			if activityDue {
				// Owned timing fact makes the real Account 60s throttle due,
				// preserving last_activity_at >= issued_at and the live Session.
				changed, err := v.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET issued_at=LEAST(issued_at,clock_timestamp()-interval '3 minutes'),last_activity_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`, a.Details().SessionID)
				if err != nil || changed.RowsAffected() != 1 {
					t.Fatal("Activity timing fixture did not update the original Session", err)
				}
			}
			baseline := v.secretSnapshot(t)
			var activityBefore, activityAfter string
			if err := v.raw.QueryRow(ctxFor(t), `SELECT last_activity_at::text FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&activityBefore); err != nil {
				t.Fatal(err)
			}
			version := f.Version(1)
			meta := meta(t, "atomic-"+stage, &version)
			identity, err := vc.SecretVariableCommandIdentity(p, vc.SecretUpdateCommand, meta.IdempotencyKey)
			if err != nil {
				t.Fatal(err)
			}
			targetCause := errors.New("owned exact post-success fault")
			failure := f.NewFault(f.ResourceBusy, f.NotCommitted).WithCause(targetCause)
			reached, failedResults := 0, 0
			inspect := func(ctx context.Context, tx f.Tx) error {
				x, err := v.tracked.InTx(tx)
				if err != nil {
					return err
				}
				var vv, cv, history, d04, audits, native, completed, events int64
				err = x.QueryRow(ctx, `SELECT version,credential_version,
 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1::uuid AND variable_id=$2::uuid),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1::uuid AND variable_id=$2::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND producer='projectvariable' AND resource_id=$2::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND producer='secret' AND action IN ('secret.create','secret.update')),
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id=$1::uuid AND command_name='project.secret_variable.update' AND idempotency_key=$3),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::uuid AND event_type='project.secret_variable_changed')
 FROM agenteam_projectvariable.variables WHERE project_id=$1::uuid AND id=$2::uuid`, p.String(), target.String(), string(meta.IdempotencyKey)).Scan(&vv, &cv, &history, &d04, &audits, &native, &completed, &events)
				if err != nil {
					return err
				}
				wantCompleted, wantEvents := int64(1), int64(2)
				if stage == "after-d10-audit" {
					wantCompleted, wantEvents = 0, 1
				}
				if vv != 2 || cv != 2 || history != 2 || d04 != 2 || audits != 2 || native != 2 || completed != wantCompleted || events != wantEvents {
					return fmt.Errorf("target stage lacks actual same-Tx facts: %d/%d/%d/%d/%d/%d/%d/%d", vv, cv, history, d04, audits, native, completed, events)
				}
				if activityDue {
					var touched bool
					if err = x.QueryRow(ctx, `SELECT last_activity_at > $2::timestamptz FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID, activityBefore).Scan(&touched); err != nil {
						return err
					}
					if !touched {
						return errors.New("target stage lacks an actual same-Tx Activity update")
					}
				}
				reached++
				return failure
			}
			deps := v.deps
			switch stage {
			case "after-d10-audit":
				v.deps.Audit = &secretAuditTail{v.deps.Audit, inspect}
			case "after-outbox":
				v.deps.Events = &secretEventTail{v.deps.Events, inspect}
			case "after-activity":
				v.deps.Activity = &secretActivityTail{v.deps.Activity, inspect}
			case "owner-tail":
				v.tracked.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
					if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() {
						return nil
					}
					x, err := v.tracked.InTx(tx)
					if err != nil {
						return err
					}
					var complete bool
					if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3)`, p.String(), string(vc.SecretUpdateCommand), string(meta.IdempotencyKey)).Scan(&complete); err != nil {
						return err
					}
					if !complete {
						return nil
					}
					return inspect(ctx, tx)
				})
			}
			svc := v.newOwner(t)
			v.deps = deps
			v.tracked.mu.Lock()
			v.tracked.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
				if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == identity.Canonical() && result.State() == f.NotCommitted && errors.Is(result.Fault(), targetCause) {
					failedResults++
				}
			}
			v.tracked.mu.Unlock()
			defer func() {
				v.tracked.setAfter(nil)
				v.tracked.mu.Lock()
				v.tracked.afterResult = nil
				v.tracked.mu.Unlock()
			}()
			got, err := svc.UpdateSecretVariable(ctxFor(t), a, meta, p, target, patch)
			requireCode(t, err, f.ResourceBusy)
			if !errors.Is(err, targetCause) || reached != 1 || failedResults != 1 || got.Validate() == nil {
				t.Fatal("target fault/actual callback/NotCommitted not proven", err, reached, failedResults)
			}
			if baseline != v.secretSnapshot(t) {
				t.Fatal("same-Tx Secret facts did not fully roll back")
			}
			if err = v.raw.QueryRow(ctxFor(t), `SELECT last_activity_at::text FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&activityAfter); err != nil || activityAfter != activityBefore {
				t.Fatal("Activity escaped rollback", err)
			}
			absent, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, secretLookup(t, p, target, vc.SecretUpdateCommand, meta))
			if err != nil || absent.Status() != vc.SecretLookupNotObserved {
				t.Fatal("rolled back command became completed", err)
			}
		})
	}
}
