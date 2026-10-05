//go:build integration

package project_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Unlike B02's update-only branch, this wrapper arms the unchanged real COMMIT
// proxy only after the exact archive/delete plan or completed acceptance exists
// inside the original transaction. It never manufactures a CommitResult.
type r2CommitStore struct {
	*postgres.Store
	proxy                                            *commitProxy
	enabled, fired                                   atomic.Bool
	holder                                           atomic.Int32
	confirmationTimeouts                             atomic.Int64
	intent                                           r2Intent
	phase                                            string
	originalOperation, originalEvent, originalDigest string
}

func (w *r2CommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := fn(ctx, tx); err != nil {
			var pg *pgconn.PgError
			if w.fired.Load() && errors.As(err, &pg) && pg.Code == "55P03" {
				w.confirmationTimeouts.Add(1)
			}
			return err
		}
		if !w.enabled.Load() || w.fired.Load() {
			return nil
		}
		x, err := w.InTx(tx)
		if err != nil {
			return err
		}
		var count int
		err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2 AND key=$3 AND state=$4`, w.intent.project.ID.String(), string(w.intent.command), string(w.intent.meta.IdempotencyKey), w.phase).Scan(&count)
		if err != nil || count == 0 {
			return err
		}
		if w.fired.CompareAndSwap(false, true) {
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid(),plan->>'operation_id',event_id::text,plan->>'manifest_digest' FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2 AND key=$3`, w.intent.project.ID.String(), string(w.intent.command), string(w.intent.meta.IdempotencyKey)).Scan(&pid, &w.originalOperation, &w.originalEvent, &w.originalDigest); err != nil {
				return err
			}
			w.holder.Store(pid)
			w.proxy.armed.Store(true)
		}
		return nil
	})
}
func newR2ProxyFixture(t *testing.T, commit bool) (*r2Fixture, *r2CommitStore, *commitProxy) {
	t.Helper()
	db := newDatabase(t)
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
	u, err := url.Parse(db.Fixture.URL(db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "LOCK_TIMEOUT": "1s"}))
	store := &r2CommitStore{Store: raw, proxy: proxy}
	f := &r2Fixture{b02Fixture: assemble(t, db, raw, store), registry: r2Registry(t, r2Entries(1, false))}
	f.service = f.lifecycleService(t, nil)
	return f, store, proxy
}
func r2CheckUnknown(t *testing.T, err error, intent r2Intent, want foundation.CommitState) {
	t.Helper()
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.CommitState != want {
		t.Fatalf("original outcome=%v want=%s", err, want)
	}
	original, ok := project.UnknownAttempt(err)
	identity, _ := c.CommandIdentity(intent.project.ID, intent.command, intent.meta.IdempotencyKey)
	if !ok || original.AttemptID().Validate() != nil || original.Cause().Validate() != nil || original.Cause().Details().Primary.Canonical() != identity.Canonical() || fault.CauseID != original.AttemptID().String() {
		t.Fatal("lost original physical attempt or command cause")
	}
}
func r2AwaitReply(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle call did not join its confirmation budget")
		return nil
	}
}

func TestProjectB03R2UnknownWriterHeldThenCommitOrRollback(t *testing.T) {
	for _, command := range []c.CommandName{c.ArchiveCommand, c.DeleteCommand} {
		for _, phase := range []string{"planned", "completed"} {
			for _, commit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/commit=%t", command, phase, commit), func(t *testing.T) {
					f, w, proxy := newR2ProxyFixture(t, commit)
					owner := f.human(t, "owner-alpha", "user")
					p, _, _ := f.create(t, owner, "UnknownHeld")
					intent := newR2Intent(t, p, command, "unknown-held")
					w.intent, w.phase = intent, phase
					w.enabled.Store(true)
					ctx, cancel := context.WithCancel(ctxFor(t))
					defer cancel()
					done := make(chan error, 1)
					go func() { _, err := intent.invoke(ctx, f.service, owner); done <- err }()
					await(t, proxy.reached)
					identity, _ := c.CommandIdentity(p.ID, command, intent.meta.IdempotencyKey)
					key, _ := foundation.CommandLock(identity)
					r2ObserveWaiter(t, f, w.holder.Load(), key, "ExclusiveLock")
					if command == c.DeleteCommand && phase == "completed" && !commit {
						cancel()
					} // HTTP cancellation cannot remove the held acceptance.
					r2CheckUnknown(t, r2AwaitReply(t, done), intent, foundation.Unknown)
					if w.confirmationTimeouts.Load() == 0 {
						t.Fatal("original confirmation did not observe actual 55P03")
					}
					_, err := f.service.LookupCommand(ctxFor(t), owner, intent.lookup())
					r2RequireSQLState(t, err, "55P03")
					close(proxy.release)
					await(t, proxy.completed)
					lookup, err := f.service.LookupCommand(ctxFor(t), owner, intent.lookup())
					if err != nil {
						t.Fatal("terminal writer Lookup", r2SafeError(err))
					}
					want := c.LookupNotObserved
					if phase == "completed" {
						want = c.LookupInProgress
					}
					if commit {
						want = c.LookupInProgress
						if phase == "completed" {
							want = c.LookupCommitted
						}
					}
					if lookup.State != want {
						t.Fatalf("lookup=%s want=%s", lookup.State, want)
					}
					accepted, err := intent.invoke(ctxFor(t), f.service, owner)
					if err != nil {
						t.Fatal("same identity recovery", r2SafeError(err))
					}
					manifest, _ := f.registry.Manifest()
					f.assertAccepted(t, intent, owner, accepted, manifest)
					if phase == "completed" || commit {
						var eventID, digest string
						err = f.raw.QueryRow(ctxFor(t), `SELECT event_id::text,plan->>'manifest_digest' FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2 AND key=$3`, p.ID.String(), string(command), string(intent.meta.IdempotencyKey)).Scan(&eventID, &digest)
						if err != nil || accepted.Operation.ID.String() != w.originalOperation || eventID != w.originalEvent || digest != w.originalDigest {
							t.Fatal("recovery replaced durable identity", err)
						}
					}
				})
			}
		}
	}
}

func TestProjectB03R2UnknownOriginalCallSerializesWriter(t *testing.T) {
	for _, command := range []c.CommandName{c.ArchiveCommand, c.DeleteCommand} {
		for _, phase := range []string{"planned", "completed"} {
			for _, commit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/commit=%t", command, phase, commit), func(t *testing.T) {
					f, w, proxy := newR2ProxyFixture(t, commit)
					owner := f.human(t, "owner-alpha", "user")
					p, _, _ := f.create(t, owner, "UnknownResolved")
					intent := newR2Intent(t, p, command, "unknown-resolved")
					w.intent, w.phase = intent, phase
					w.enabled.Store(true)
					done := make(chan error, 1)
					go func() { _, err := intent.invoke(ctxFor(t), f.service, owner); done <- err }()
					await(t, proxy.reached)
					identity, _ := c.CommandIdentity(p.ID, command, intent.meta.IdempotencyKey)
					key, _ := foundation.CommandLock(identity)
					r2ObserveWaiter(t, f, w.holder.Load(), key, "ExclusiveLock")
					close(proxy.release)
					await(t, proxy.completed)
					err := r2AwaitReply(t, done)
					if phase == "completed" && commit {
						if err != nil {
							t.Fatal("confirmed acceptance not returned", err)
						}
					} else {
						want := foundation.NotCommitted
						if phase == "planned" && commit {
							want = foundation.Unknown
						}
						r2CheckUnknown(t, err, intent, want)
					}
					result, err := intent.invoke(ctxFor(t), f.service, owner)
					if err != nil {
						t.Fatal("original intent retry", r2SafeError(err))
					}
					manifest, _ := f.registry.Manifest()
					f.assertAccepted(t, intent, owner, result, manifest)
				})
			}
		}
	}
}
