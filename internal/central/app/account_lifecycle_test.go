package app

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
)

type b04Work struct {
	stopped atomic.Bool
	joined  atomic.Bool
	forced  atomic.Int32
	force   func(context.Context) error
	drain   func(context.Context) error
}

func (w *b04Work) Start(context.Context) error     { return nil }
func (w *b04Work) Check(ctx context.Context) error { return ctx.Err() }
func (w *b04Work) Handler() http.Handler           { return http.NotFoundHandler() }
func (w *b04Work) StopAdmission()                  { w.stopped.Store(true) }
func (w *b04Work) Drain(ctx context.Context) error {
	if w.drain != nil {
		return w.drain(ctx)
	}
	if w.joined.Load() {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestB04AppMailFinishesBeforeCoreRetirement(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[force], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			mailEntered, allowMail, coreEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
			mail, core, sink := &b04Work{}, &b04Work{}, &b04Work{}
			sink.joined.Store(true)
			mailCall := func(actual context.Context) error {
				if actual != ctx {
					t.Error("mail cleanup renewed the root budget")
				}
				close(mailEntered)
				select {
				case <-allowMail:
				case <-actual.Done():
					return actual.Err()
				}
				if core.joined.Load() {
					t.Error("core retired before the mail completion transaction")
				}
				mail.joined.Store(true)
				return nil
			}
			coreCall := func(actual context.Context) error {
				close(coreEntered)
				if actual != ctx {
					t.Error("core cleanup renewed the root budget")
				}
				if !mail.joined.Load() {
					t.Error("core cleanup ran before actual mail join")
				}
				core.joined.Store(true)
				return nil
			}
			mail.drain, mail.force = mailCall, mailCall
			core.drain, core.force = coreCall, coreCall
			group := &accountAssembly{runtime: core, mail: mail, sink: sink}
			done := make(chan error, 1)
			go func() {
				if force {
					done <- group.Force(ctx)
				} else {
					done <- group.Drain(ctx)
				}
			}()
			select {
			case <-mailEntered:
			case <-ctx.Done():
				t.Fatal("mail cleanup was not initiated")
			}
			select {
			case <-coreEntered:
				t.Error("root began retiring core while mail completion was paused")
			case <-time.After(20 * time.Millisecond):
			}
			close(allowMail)
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-ctx.Done():
				t.Fatal("account cleanup did not join")
			}
			if !group.Joined() {
				t.Error("complete mail and core join was not observed")
			}
		})
	}
}
func (w *b04Work) Force(ctx context.Context) error {
	w.StopAdmission()
	w.forced.Add(1)
	if w.force != nil {
		return w.force(ctx)
	}
	return ctx.Err()
}
func (w *b04Work) Joined() bool { return w.joined.Load() }

func TestB04AppJointGuardRequiresEveryActualJoin(t *testing.T) {
	// These collaborators isolate the root's ownership rule. Real hashing,
	// Avatar leases and Sink I/O are separately exercised in their domain and
	// process groups; a fake lifecycle state does not prove those operations.
	for _, blocked := range []string{"none", "http", "outbox", "account", "mail", "sink", "construction"} {
		t.Run(blocked, func(t *testing.T) {
			account, mail, sink := &b04Work{}, &b04Work{}, &b04Work{}
			account.joined.Store(blocked != "account")
			mail.joined.Store(blocked != "mail")
			sink.joined.Store(blocked != "sink")
			group := &accountAssembly{runtime: account, mail: mail, sink: sink, constructing: blocked == "construction"}
			deliveries := &unitOutbox{}
			deliveries.joined.Store(blocked != "outbox")
			deliveries.force = func(ctx context.Context) error { return ctx.Err() }
			objects := &guardedUnitObjects{outbox: deliveries}
			owned := &resources{accountService: group, outboxService: deliveries, objectService: objects}
			if blocked == "http" && !owned.admitHTTP() {
				t.Fatal("HTTP was not admitted")
			}
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			var databaseCalled bool
			owned.db = &unitDatabase{force: func(actual context.Context) error {
				databaseCalled = true
				if actual != ctx || !account.stopped.Load() || !mail.stopped.Load() || account.forced.Load() != 1 || mail.forced.Load() != 1 {
					t.Error("DB force overtook real Account/Mail cancellation or reset context")
				}
				if !objects.transport.Load() && !objects.released.Load() {
					t.Error("DB force overtook object transport close")
				}
				return nil
			}}
			cleanupResources(ctx, owned, nil)
			wantRelease := blocked == "none"
			if !databaseCalled || objects.released.Load() != wantRelease || objects.transport.Load() == wantRelease || objects.seen != ctx {
				t.Fatal("joint force lost actual join evidence or the original budget")
			}
			if blocked == "http" {
				owned.finishHTTP()
			}
			account.joined.Store(true)
			mail.joined.Store(true)
			sink.joined.Store(true)
			deliveries.joined.Store(true)
			group.constructionDone()
			if !owned.producersJoined() {
				t.Fatal("complete joint termination was not observed")
			}
		})
	}
}

func TestB04AppPartialSinkAndLateAcquisitionUseOneOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	group := &accountAssembly{constructing: true}
	deliveries := &unitOutbox{}
	objects := &guardedUnitObjects{outbox: deliveries}
	owned := &resources{accountService: group, objectService: objects, outboxService: deliveries}
	cleanupResources(ctx, owned, nil)
	if objects.released.Load() || !objects.transport.Load() {
		t.Fatal("construction was treated as joined")
	}
	sink := &b04Work{force: func(actual context.Context) error {
		if actual != ctx {
			t.Error("late Sink renewed cleanup context")
		}
		return actual.Err()
	}}
	if group.install(context.Background(), func() { group.sink = sink }) || !sink.stopped.Load() || sink.forced.Load() != 1 {
		t.Fatal("late Sink escaped its original partial owner")
	}
	group.constructionDone()
	if group.Joined() {
		t.Fatal("late unjoined Sink was hidden by constructor completion")
	}
	late := &guardedUnitObjects{outbox: deliveries}
	if owned.addObjects(context.Background(), late) || late.released.Load() || !late.transport.Load() || owned.objects() != objects {
		t.Fatal("late object return retired guard while the original Sink remained live")
	}
	sink.joined.Store(true)
	if !group.Joined() {
		t.Fatal("actual late Sink closure was not observed")
	}
}

func TestB04AppPartialRealSinkActuallyCloses(t *testing.T) {
	inputs := accountenv.New(t).Values()
	sink, err := recoverylog.Open(inputs["AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG"])
	if err != nil {
		t.Fatal(err)
	}
	group := &accountAssembly{sink: sink}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := group.Drain(ctx); err != nil || !sink.Joined() || !group.Joined() {
		t.Fatal("failure before core construction lost actual Sink closure")
	}
}

func TestB04AppExhaustedSinkBudgetStillForcesDatabaseLast(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	sink := &b04Work{force: func(actual context.Context) error {
		if actual != ctx {
			t.Error("Sink got a renewed force deadline")
		}
		<-actual.Done()
		return actual.Err()
	}}
	group := &accountAssembly{sink: sink}
	deliveries := &unitOutbox{}
	objects := &guardedUnitObjects{outbox: deliveries}
	called := false
	owned := &resources{accountService: group, outboxService: deliveries, objectService: objects, db: &unitDatabase{force: func(actual context.Context) error {
		called = true
		if actual != ctx || !errors.Is(actual.Err(), context.DeadlineExceeded) || sink.forced.Load() != 1 || !objects.transport.Load() {
			t.Error("exhausted Sink force skipped or reordered mandatory cleanup")
		}
		return actual.Err()
	}}}
	cleanupResources(ctx, owned, nil)
	if !called || objects.released.Load() {
		t.Fatal("expired force skipped DB or retired live Sink's guard")
	}
}

func TestB04AppAccountHealthHasIndependentFreshness(t *testing.T) {
	now := time.Now()
	monitor := newHealthMonitor(unitHealth(), healthTiming{now: func() time.Time { return now }})
	monitor.accountAvailable = true
	monitor.accountReceived = now
	now = now.Add(21 * time.Second)
	monitor.objectReceived, monitor.outboxReceived, monitor.received = now, now, now
	if monitor.accountSnapshot() {
		t.Fatal("other health samples refreshed stale Account")
	}
	monitor.accountReceived = now
	if !monitor.accountSnapshot() {
		t.Fatal("fresh Account health unavailable")
	}
	monitor.accountAvailable = false
	if monitor.accountSnapshot() {
		t.Fatal("failed Account check remained available")
	}
}
