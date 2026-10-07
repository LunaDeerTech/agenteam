//go:build integration

package model_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This observer never fabricates a CommitResult: only the actual final writer
// with its canonical/reference/receipt/Audit/Event facts arms the wire proxy.
type meetingSummaryCommitStore struct {
	*postgres.Store
	target           string
	arm              func()
	fired            atomic.Bool
	mu               sync.Mutex
	original         f.CommitResult
	reached, release chan struct{}
}

func (w *meetingSummaryCommitStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != w.target || w.fired.Load() {
			return nil
		}
		x, e := w.Store.InTx(tx)
		if e != nil {
			return e
		}
		var final bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.commands c JOIN agenteam_model.meeting_summary_selection s ON s.id=c.resource_id JOIN agenteam_model.references r ON r.owner_kind='platform_selector' AND r.owner_id=s.id AND r.role='meeting_summary' AND r.model_id=s.model_id AND r.owner_version=s.version WHERE c.command_identity=$1 AND c.phase='committed' AND c.safe_receipt IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records a WHERE a.producer='model' AND a.action='model.selection.update' AND a.resource_id=s.id) AND EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=c.event_id AND e.producer='model'))`, w.target).Scan(&final)
		if e != nil {
			return e
		}
		if final && w.fired.CompareAndSwap(false, true) {
			w.arm()
		}
		return nil
	})
	if result.State() == f.Unknown && w.fired.Load() {
		w.mu.Lock()
		w.original = result
		w.mu.Unlock()
		close(w.reached)
		select {
		case <-w.release:
		case <-ctx.Done():
		}
	}
	return result
}
func TestModelMeetingSummaryUnknown(t *testing.T) {
	for _, mode := range []string{"committed", "rollback", "pending", "different-after-rollback"} {
		t.Run(mode, func(t *testing.T) {
			base := newMeetingSummaryFixture(t, true)
			target, other := base.chat(t, false), base.chat(t, false)
			request := base.choice(t, target)
			identity, e := f.NewCommandIdentity("model.system", []string{base.admin.Details().UserID}, "model.selection.update", request.Key)
			if e != nil {
				t.Fatal(e)
			}
			upstream := net.JoinHostPort("127.0.0.1", base.db.Fixture.Port)
			var address string
			var arm func()
			var held *modelCommitProxy
			var trace *projectModelPGTrace
			if mode == "committed" {
				trace = newProjectModelPGTrace(t, upstream)
				address = trace.listener.Addr().String()
				arm = func() { trace.dropCommitACK.Store(true) }
			} else {
				held = newModelCommitProxy(t, upstream, false)
				address = held.listener.Addr().String()
				arm = func() { held.armed.Store(true) }
			}
			u, e := url.Parse(base.db.Fixture.URL(base.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = address
			raw := openStore(t, base.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			writer := &meetingSummaryCommitStore{Store: raw, target: identity.Canonical(), arm: arm, reached: make(chan struct{}), release: make(chan struct{})}
			candidate := assemble(t, base.db, raw, writer)
			candidate.admin = base.admin
			var writerOnce, confirmOnce sync.Once
			releaseWriter := func() {
				writerOnce.Do(func() {
					if held != nil {
						close(held.release)
					}
				})
			}
			releaseConfirmation := func() { confirmOnce.Do(func() { close(writer.release) }) }
			t.Cleanup(releaseWriter)
			t.Cleanup(releaseConfirmation)
			writerLock := func() error {
				key, e := f.CommandLock(identity)
				if e != nil {
					return e
				}
				result := base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
					return base.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}})
				})
				if result.State() != f.Committed {
					return result.Fault()
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			type response struct {
				receipt mc.CommandReceipt
				err     error
			}
			done := make(chan response, 1)
			go func() { r, e := candidate.service.UpdateMeetingSummarySelection(ctx, request); done <- response{r, e} }()
			if trace != nil {
				waitSignal(t, trace.ackDropped)
			} else {
				waitSignal(t, held.reached)
			}
			waitSignal(t, writer.reached)
			if mode == "pending" {
				if e := writerLock(); e == nil {
					t.Fatal("original writer no longer held EX")
				}
			}
			if mode == "rollback" || mode == "different-after-rollback" {
				releaseWriter()
				waitSignal(t, held.completed)
				if e := writerLock(); e != nil {
					t.Fatal("original rollback not terminal", e)
				}
			}
			var otherReceipt mc.CommandReceipt
			if mode == "different-after-rollback" {
				different := request
				different.Model = other
				otherReceipt, e = base.service.UpdateMeetingSummarySelection(testContext(t), different)
				if e != nil {
					t.Fatal("different request after confirmed rollback", e)
				}
			}
			releaseConfirmation()
			var got response
			select {
			case got = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("command did not actually return")
			}
			writer.mu.Lock()
			original := writer.original
			writer.mu.Unlock()
			if original.State() != f.Unknown || !writer.fired.Load() {
				t.Fatal("final real commit was not faulted")
			}
			switch mode {
			case "committed":
				if got.err != nil || got.receipt.ResourceID != request.SelectionID {
					t.Fatal("actual COMMIT not confirmed", got.err)
				}
				if !strings.Contains(strings.Join(trace.snapshot(), " "), "commit-idle-ack-dropped") {
					t.Fatal("missing real COMMIT/idle ACK loss")
				}
				// Advancing state must not turn replay into a fresh write or duplicate effects.
				base.selectModel(t, other)
				before := base.summaryFacts(t)
				again, e := base.service.UpdateMeetingSummarySelection(testContext(t), request)
				if e != nil || again != got.receipt {
					t.Fatal("original accepted replay changed", e)
				}
				httpSameFacts(t, before, base.summaryFacts(t))
			case "different-after-rollback":
				requireCode(t, got.err, f.IdempotencyKeyReused)
				if got.receipt != (mc.CommandReceipt{}) || otherReceipt.ResourceID != request.SelectionID {
					t.Fatal("adopted different semantic")
				}
			default:
				var unknown *model.UnknownCommandError
				if !errors.As(got.err, &unknown) || got.receipt != (mc.CommandReceipt{}) || unknown.AttemptID() != original.AttemptID() || unknown.Cause().Details().Primary.Canonical() != original.Cause().Details().Primary.Canonical() {
					t.Fatal("original cause/attempt or zero receipt lost", got.err)
				}
				if mode == "pending" {
					releaseWriter()
					waitSignal(t, held.completed)
					if e := writerLock(); e != nil {
						t.Fatal("held original writer not joined", e)
					}
				}
				lookup, e := base.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: request.CommandMeta, Command: "model.selection.update"})
				if e != nil || lookup.Found || lookup.Receipt != nil {
					t.Fatal("rollback became receipt", e)
				}
				if base.summary(t).Model != nil {
					t.Fatal("rollback left canonical")
				}
			}
			t.Logf("actual Summary final-COMMIT mode=%s original_state=%s, command goroutine joined", mode, original.State())
		})
	}
}
