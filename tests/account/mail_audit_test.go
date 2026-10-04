//go:build integration

package account_test

import (
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAccountMailAuditRequiresExactClosedCurrentAttempt(t *testing.T) {
	f := newB02Account(t)
	q := finishedMailForRetry(t, f)
	var attempt, initiator, channel string
	var fence int64
	e := f.store.QueryRow(ctxFor(t), `SELECT a.id::text,a.fence,i.initiator_id::text,a.channel FROM agenteam_account.mail_attempts a JOIN agenteam_account.mail_jobs j ON j.id=a.job_id JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id WHERE j.id=$1 AND j.current_attempt_id=a.id AND a.terminal AND a.io_joined`, q.JobID.String()).Scan(&attempt, &fence, &initiator, &channel)
	if e != nil {
		t.Fatal(e)
	}
	reg, e := identity.RegisterService(identity.AccountMail)
	if e != nil {
		t.Fatal(e)
	}
	actor, e := reg.Actor(attempt, identity.SystemScope())
	if e != nil {
		t.Fatal(e)
	}
	var locks []foundation.LockRequest
	for _, value := range []string{q.JobID.String(), attempt} {
		k, e := foundation.RecordLock(foundation.ReferenceRecordLock, value)
		if e != nil {
			t.Fatal(e)
		}
		locks = append(locks, foundation.LockRequest{Key: k, Mode: foundation.Exclusive})
	}
	rollback := errors.New("owned audit projection rollback")
	projections := []struct {
		result, reason string
		outcome        ac.Outcome
		phase          ac.AccountPhase
		projected      ac.AccountReason
	}{
		{"sent", "sent", ac.Success, ac.AccountSent, ""},
		{"unknown", "network_failed", ac.Unknown, ac.AccountUnknown, ac.DeliveryUnknown},
		{"failed", "smtp_rejected", ac.Failed, ac.AccountFailed, ac.DeliveryRejected},
		{"failed", "timeout", ac.Failed, ac.AccountFailed, ac.DeliveryTimeout},
		{"failed", "token_invalid", ac.Failed, ac.AccountFailed, ac.DeliveryCancelled},
		{"cancelled", "cancelled", ac.Failed, ac.AccountFailed, ac.DeliveryCancelled},
		{"cancelled", "token_invalid", ac.Failed, ac.AccountFailed, ac.DeliveryCancelled},
	}
	for _, p := range projections {
		t.Run(p.result+"_"+p.reason, func(t *testing.T) {
			for _, change := range []string{"exact", "false_outcome_phase", "outcome_only", "reason", "version", "initiator", "channel", "current_attempt", "job_fence", "terminal", "io_joined", "protocol_phase", "result_null", "producer", "cause", "ordinal", "missing_locks"} {
				t.Run(change, func(t *testing.T) {
					fields := ac.AccountMetadataFields{Version: foundation.Version(fence), JobID: q.JobID.String(), AttemptID: attempt, InitiatorID: initiator, Channel: ac.DeliveryChannel(channel), Phase: p.phase, Reason: p.projected}
					outcome := p.outcome
					producer, causeRef, ordinal := ac.AccountMailProducer, attempt, int64(0)
					switch change {
					case "false_outcome_phase":
						outcome, fields.Phase, fields.Reason = ac.Success, ac.AccountSent, ""
						if p.result == "sent" {
							outcome, fields.Phase, fields.Reason = ac.Failed, ac.AccountFailed, ac.DeliveryRejected
						}
					case "outcome_only":
						outcome = ac.Success
						if outcome == p.outcome {
							outcome = ac.Failed
						}
					case "reason":
						fields.Reason = ac.DeliveryTimeout
						if fields.Reason == p.projected {
							fields.Reason = ac.DeliveryRejected
						}
					case "version":
						fields.Version++
					case "initiator":
						fields.InitiatorID = id[identity.User](t).String()
					case "channel":
						fields.Channel = "log"
					case "producer":
						producer = ac.AccountProducer
					case "cause":
						causeRef = id[struct{}](t).String()
					case "ordinal":
						ordinal = 1
					}
					metadata, e := ac.AccountMetadata(ac.SMTPDelivery, fields)
					if e != nil {
						if change != "reason" || p.result != "sent" {
							t.Fatal("unexpected typed metadata rejection", e)
						}
						return
					}
					resource, _ := ac.NewResource(ac.MailJobResource, q.JobID.String())
					entry, e := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: ac.SMTPDelivery, Outcome: outcome, Resource: resource, Metadata: metadata})
					if e != nil {
						if change != "outcome_only" {
							t.Fatal("unexpected typed entry rejection", e)
						}
						return
					}
					key, e := ac.NewAppendKey(producer, causeRef, ordinal)
					if e != nil {
						t.Fatal(e)
					}
					called, accepted := false, false
					r := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
						if change != "missing_locks" {
							if e := f.store.AcquireAll(ctx, tx, locks); e != nil {
								return e
							}
						}
						x, e := f.store.InTx(tx)
						if e != nil {
							return e
						}
						// This test changes only rollback-owned durable facts to probe
						// the provider. It does not claim SMTP sent bytes or commit a
						// manufactured successful Audit record. Real worker delivery
						// outcomes are covered by the existing network suites.
						if _, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET result=$2 WHERE id=$1`, attempt, p.result); e != nil {
							return e
						}
						if _, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET reason=$2 WHERE id=$1`, q.JobID.String(), p.reason); e != nil {
							return e
						}
						switch change {
						case "current_attempt":
							_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET current_attempt_id=$2 WHERE id=$1`, q.JobID.String(), id[struct{}](t).String())
						case "job_fence":
							_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET fence=fence+1 WHERE id=$1`, q.JobID.String())
						case "terminal":
							_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET terminal=false WHERE id=$1`, attempt)
						case "io_joined":
							_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET io_joined=false WHERE id=$1`, attempt)
						case "protocol_phase":
							_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET phase='negotiation' WHERE id=$1`, attempt)
						case "result_null":
							_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET result=NULL WHERE id=$1`, attempt)
						}
						if e != nil {
							return e
						}
						called = true
						accepted = f.authority.CheckAppendInTx(ctx, tx, entry, key) == nil
						return rollback
					})
					if !called || r.State() != foundation.NotCommitted || accepted != (change == "exact") {
						t.Fatal("durable Audit fact binding", called, accepted, r.State(), safeFailure(r.Fault()))
					}
				})
			}
		})
	}
	var unchanged bool
	if e = f.store.QueryRow(ctxFor(t), `SELECT a.result='cancelled' AND a.terminal AND a.io_joined AND a.phase='closed' AND a.fence=j.fence AND j.current_attempt_id=a.id AND j.reason='cancelled' AND (SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery')=1 FROM agenteam_account.mail_attempts a JOIN agenteam_account.mail_jobs j ON j.id=a.job_id WHERE a.id=$1`, attempt).Scan(&unchanged); e != nil || !unchanged {
		t.Fatal("projection probes leaked state or Audit", unchanged, e)
	}
}
