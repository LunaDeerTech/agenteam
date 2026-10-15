package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Done is evaluated only after Join captured the original lookup's done
// channel. The test controls its actual return independently of cancellation.
type installColdJoinWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *installColdJoinWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestInstallColdJoinAfterLookupReturn(t *testing.T) {
	for _, mode := range []string{"join-call", "drain", "retained-write-unknown"} {
		t.Run(mode, func(t *testing.T) {
			_, r, _ := installColdAuthorityFixture(t)
			d, b := r.call.dispatcher, r.call.binding
			d.cold = nil
			c, run, end, err := d.beginColdInstall(context.Background(), b)
			if err != nil || c == nil {
				t.Fatal("original lookup admission", err)
			}
			cause, err := inputCause(b)
			if err != nil {
				t.Fatal(err)
			}
			store := &installRecoveryReadFailureStore{Store: r.issuer.store, t: t, mode: "read-failed", cause: cause}
			core := r.executor.core.data()
			core.store = store
			r.executor.core = &Service{data: func() serviceState { return core }}
			r.issuer.store = store
			retain := mode == "retained-write-unknown"
			var original error
			if retain {
				c.proved = true
				original = commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), cause))
				c.writeErr = original
			}
			base, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ctx := &installColdJoinWaitContext{Context: base, waiting: make(chan struct{})}
			returned := make(chan error, 1)
			go func() {
				if mode == "drain" {
					returned <- d.Drain(ctx)
				} else {
					returned <- d.JoinCall(ctx, b)
				}
			}()
			select {
			case <-ctx.waiting:
			case <-base.Done():
				t.Fatal("Join did not capture original done")
			}
			d.Stop()
			if !errors.Is(run.Err(), context.Canceled) {
				t.Fatal("Stop did not cancel original lookup")
			}
			select {
			case <-c.done:
				t.Fatal("Stop fabricated original lookup return")
			default:
			}
			end(retain)
			select {
			case err = <-returned:
			case <-base.Done():
				t.Fatal("Join did not return after actual lookup return")
			}
			if retain {
				if !errors.Is(err, original) || installUnknown(err).Attempt != installUnknown(original).Attempt || c.writeErr != original || d.cold[b.Identity()] != c || store.calls != 1 || d.Joined() {
					t.Fatal("Join lost retained write Unknown or original owner", err, store.calls)
				}
			} else if err != nil || store.calls != 0 || len(d.cold) != 0 || d.Drain(base) != nil || !d.Joined() {
				t.Fatal("completed lookup created recovery or failed Drain", err, store.calls)
			}
		})
	}
}

func TestInstallColdOwnerWaitsForItsActualLookup(t *testing.T) {
	_, r, _ := installColdAuthorityFixture(t)
	d, b := r.call.dispatcher, r.call.binding
	d.cold = nil
	c, ctx, end, err := d.beginColdInstall(context.Background(), b)
	if err != nil || c == nil {
		t.Fatal(err)
	}
	if _, _, _, err = d.beginColdInstall(context.Background(), b); err == nil {
		t.Fatal("concurrent lookup admitted")
	}
	c.cancel()
	if ctx.Err() == nil {
		t.Fatal("lookup not cancelled")
	}
	select {
	case <-c.done:
		t.Fatal("cancellation fabricated completion")
	default:
	}
	c.proved = true
	end(true)
	select {
	case <-c.done:
	default:
		t.Fatal("actual lookup return not joined")
	}
	d.stopped = true
	if d.Joined() {
		t.Fatal("pending cold publication reported joined")
	}
	// Existing cleanup may borrow a fresh context after Stop; this is not a
	// restarted backend or a replacement for the old process's death proof.
	again, _, endAgain, err := d.beginColdInstall(context.Background(), b)
	if err != nil || again != c {
		t.Fatal("retained cleanup owner replaced", err)
	}
	endAgain(false)
	if len(d.cold) != 0 {
		t.Fatal("completed owner retained")
	}
	if _, _, _, err = d.beginColdInstall(context.Background(), b); err == nil {
		t.Fatal("Stop admitted a new cold lookup")
	}
}

func TestInstallColdTerminalRequiresExactOriginalTuple(t *testing.T) {
	_, r, _ := installColdAuthorityFixture(t)
	c := r.call
	c.finalRaw = []byte(`original-final-bytes`)
	o := &installObserved{record: c.record, attempt: c.origin.attempt, process: c.origin.process.String(), request: p.ID(c.origin.request.String()), raw: append([]byte(nil), c.finalRaw...), terminal: installTerminal{Known: true}}
	o.record.Version = 3
	if !coldInstallTerminalMatches(c, o) {
		t.Fatal("same terminal refused")
	}
	for _, mode := range []string{"process", "attempt", "request", "fingerprint", "bytes", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			changed := *o
			switch mode {
			case "process":
				changed.process = newOperationID[oc.Process](t).String()
			case "attempt":
				changed.attempt = newOperationID[tc.Attempt](t)
			case "request":
				changed.request = p.ID(newOperationID[f.Request](t).String())
			case "fingerprint":
				changed.record.Fingerprint = digest([]byte("changed"))
			case "bytes":
				changed.raw = []byte("other")
			case "unknown":
				changed.terminal.Known = false
			}
			if coldInstallTerminalMatches(c, &changed) {
				t.Fatal("changed result settled old owner")
			}
		})
	}
}

func TestInstallColdReadFailurePreservesFinalWriteUnknown(t *testing.T) {
	for _, mode := range []string{"read-failed", "cancelled", "read-unknown", "already-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			_, r, _ := installColdAuthorityFixture(t)
			d, c := r.call.dispatcher, r.call
			c.busy = false
			cause, err := inputCause(c.binding)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &installRecoveryReadFailureStore{Store: r.issuer.store, t: t, mode: mode, cancel: cancel, cause: cause}
			core := r.executor.core.data()
			core.store = store
			r.executor.core = &Service{data: func() serviceState { return core }}
			r.issuer.store = store
			original := commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), cause))
			c.writeErr = original
			if mode == "already-cancelled" {
				cancel()
			}
			got := d.recoverColdInstall(ctx, c.binding)
			if !errors.Is(got, original) || installUnknown(got).Attempt != installUnknown(original).Attempt || c.writeErr != original || d.cold[c.binding.Identity()] != c {
				t.Fatal("read replaced actual write Unknown", got)
			}
			want := 1
			if mode == "already-cancelled" {
				want = 0
			}
			if store.calls != want {
				t.Fatal("observer retried or dispatched", store.calls)
			}
		})
	}
}
