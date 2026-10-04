//go:build integration

package account_test

import (
	"context"
	"errors"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
	"time"
)

func TestAccountCleanupDurablePassDoesNotStarveTailBehindHundredProtectedCommands(t *testing.T) {
	f := newB02Account(t)
	a := b02Admin(t, f)
	create := func(email string) c.InvitationReceipt {
		r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: a, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email})
		v, e := f.service.CreateInvitation(ctxFor(t), r)
		if e != nil {
			t.Fatal(e, safeFailure(e))
		}
		return v
	}
	first := create("protected-prefix@example.com")
	// Every prefix command is a genuine authorized resend of the same link.
	// Advancing last_delivery_at is fixture time preparation, not a forged row.
	for range 99 {
		if _, e := f.store.Exec(ctxFor(t), `UPDATE agenteam_account.invitations SET last_delivery_at=clock_timestamp()-interval '61 seconds' WHERE id=$1`, first.ID.String()); e != nil {
			t.Fatal(e)
		}
		v := create("protected-prefix@example.com")
		if v.ID != first.ID {
			t.Fatal("resend changed link")
		}
	}
	tail := create("independent-tail@example.com")
	// Empty unrelated local bookkeeping and canonical enqueue before the barrier.
	for range 2 {
		if _, e := f.service.Recover(ctxFor(t)); e != nil {
			t.Fatal(e, safeFailure(e))
		}
	}
	if _, e := f.store.Exec(ctxFor(t), `WITH n AS (SELECT clock_timestamp() t) UPDATE agenteam_account.invitations SET created_at=n.t-interval '25 hours',expires_at=n.t-interval '1 hour' FROM n`); e != nil {
		t.Fatal(e)
	}
	key, e := foundation.RecordLock(foundation.ReferenceRecordLock, first.ID.String())
	if e != nil {
		t.Fatal(e)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan foundation.CommitResult, 1)
	released := false
	defer func() {
		if !released {
			close(release)
		}
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("owned holder did not join")
		}
	}()
	go func() {
		finished <- f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); e != nil {
				return e
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	await(t, locked)
	invoke := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_, e := f.service.Recover(ctx)
		if e == nil || !errors.Is(e, context.DeadlineExceeded) {
			t.Fatal("protected budget must remain observable", e, safeFailure(e))
		}
	}
	invoke()
	var bumped int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.commands WHERE command_name='invite-create' AND cleanup_pass>0`).Scan(&bumped); e != nil || bumped < 100 {
		t.Fatal("batch not durably advanced", bumped, e)
	}
	invoke()
	var protected, independent int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE id=$1),count(*) FILTER(WHERE id=$2) FROM agenteam_account.invitations`, first.ID.String(), tail.ID.String()).Scan(&protected, &independent); e != nil || protected != 1 || independent != 0 {
		t.Fatal("tail starved or protected row removed", protected, independent, e)
	}
	close(release)
	released = true
	// Consume holder result here, replacing it for the single deferred join check.
	r := <-finished
	finished <- r
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	for range 2 {
		if _, e = f.service.Recover(ctxFor(t)); e != nil {
			t.Fatal("after release", e, safeFailure(e))
		}
	}
	var links, pending int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.invitations),(SELECT count(*) FROM agenteam_account.material_cleanup WHERE owner_kind='invitation' AND phase<>'completed')`).Scan(&links, &pending); e != nil || links != 0 || pending != 0 {
		t.Fatal("cleanup did not finish", links, pending, e)
	}
}
