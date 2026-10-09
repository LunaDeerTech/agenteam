package skill

// Independent tests execute the production Service. Existing helpers provide
// explicit Store/Project/Object substitutes, not real PostgreSQL or D05 proof.
import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestIndependentSkillConfirmationCurrentRecheck(t *testing.T) {
	for _, name := range []string{"current-denial", "current-unknown", "weak-object-lock", "foreign-issuer", "changed-request"} {
		t.Run(name, func(t *testing.T) {
			s, store, projects, actor, request := readFixture(t)
			plan, err := s.DiscoverConfirmation(context.Background(), actor, request)
			if err != nil {
				t.Fatal(err)
			}
			store.live = true
			store.tx = f.NewTx()
			store.held = map[string]f.LockMode{}
			for _, lock := range plan.RequiredLocks() {
				store.held[lock.Key.Canonical()] = lock.Mode
			}
			// Establish this exact original plan's valid path before changing one fact.
			receipt, err := s.ConfirmInitializedInTx(context.Background(), store.tx, actor, request, plan)
			if err != nil || receipt != plan.ProposedReceipt() {
				t.Fatal("positive original plan", err)
			}
			txs, acquires, facts := store.txs, store.acquires, store.facts
			var sentinel error
			switch name {
			case "current-denial":
				sentinel = f.NewFault(f.ProjectNotActive, f.NotCommitted)
				projects.gate = sentinel
			case "current-unknown":
				command, e := f.NewCommandIdentity("skills", []string{request.ProjectID.String()}, "initialize", request.InitializationKey)
				if e != nil {
					t.Fatal(e)
				}
				cause, e := f.NewCommandsCause(command)
				if e != nil {
					t.Fatal(e)
				}
				sentinel = f.UnknownResult(stateID[f.TransactionAttempt](91), cause).Fault()
				projects.gate = sentinel
			case "weak-object-lock":
				key, e := f.AggregateLock(f.ObjectAggregate, stateID[oc.StoredObject](7).String())
				if e != nil {
					t.Fatal(e)
				}
				store.held[key.Canonical()] = f.Shared
			case "foreign-issuer":
				plan, err = pc.NewInitializationPlanIssuer().Plan(actor, request, plan.ProposedReceipt(), plan.RequiredLocks())
				if err != nil {
					t.Fatal(err)
				}
			case "changed-request":
				request.InitializationKey = "different-original-key"
			}
			got, err := s.ConfirmInitializedInTx(context.Background(), store.tx, actor, request, plan)
			if err == nil || got != (pc.InitializationReceipt{}) {
				t.Fatal("changed current authority escaped original plan")
			}
			if sentinel != nil && err != sentinel {
				t.Fatal("original current Fault/Unknown identity changed")
			}
			if store.txs != txs || store.acquires != acquires {
				t.Fatal("confirmation started nested Tx or supplemented locks")
			}
			if (name == "weak-object-lock" || name == "foreign-issuer" || name == "changed-request") && store.facts != facts {
				t.Fatal("private identity/lock rejection read facts")
			}
			// constructorObjects has no successful Object methods: any accidental body
			// operation would panic, rather than silently serve a test success.
		})
	}
}

type independentHeldReader struct {
	body                        *oc.ObjectReader
	readReturned, closeReturned chan struct{}
	releaseRead, releaseClose   chan struct{}
	readOnce, closeOnce         sync.Once
}

func (b *independentHeldReader) Read(p []byte) (int, error) {
	n, e := b.body.Read(p)
	b.readOnce.Do(func() { close(b.readReturned) })
	<-b.releaseRead
	return n, e
}
func (b *independentHeldReader) Close() error {
	e := b.body.Close()
	b.closeOnce.Do(func() { close(b.closeReturned) })
	<-b.releaseClose
	return e
}

type independentPackageForwarder struct {
	ObjectPorts
	original *packageObjects
	held     *independentHeldReader
}

func (o *independentPackageForwarder) ReadObject(ctx context.Context, a id.Actor, owner oc.ObjectOwner, object oc.ObjectID, span *oc.ByteRange) (*oc.ObjectReader, error) {
	body, e := o.original.ReadObject(ctx, a, owner, object, span)
	if e != nil {
		return body, e
	}
	o.held.body = body
	return oc.NewObjectReader(body.Meta(), body.Range(), o.held)
}
func independentAwait(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(time.Second):
		t.Fatal("controlled boundary not reached")
	}
}
func independentClose(c chan struct{}) {
	select {
	case <-c:
	default:
		close(c)
	}
}

func TestIndependentSkillPackageActualReturnBoundaries(t *testing.T) {
	for _, first := range []string{"read-first", "close-first"} {
		t.Run(first, func(t *testing.T) {
			s, store, _, objects := packageFixture(t)
			originalError := errors.New("independent original Close result")
			objects.body.err = originalError
			held := &independentHeldReader{readReturned: make(chan struct{}), closeReturned: make(chan struct{}), releaseRead: make(chan struct{}), releaseClose: make(chan struct{})}
			defer independentClose(held.releaseRead)
			defer independentClose(held.releaseClose)
			s.state().objects = &independentPackageForwarder{ObjectPorts: objects, original: objects, held: held}
			body, e := s.OpenPackage(context.Background(), objects.actor, objects.row.request.ProjectID, objects.row.skill, 1)
			if e != nil {
				t.Fatal(e)
			}
			type answer struct {
				n int
				e error
			}
			readDone := make(chan answer, 1)
			go func() { n, e := body.Read(make([]byte, 1)); readDone <- answer{n, e} }()
			independentAwait(t, held.readReturned)
			s.Stop()
			independentAwait(t, held.closeReturned)
			assertHeld := func() {
				t.Helper()
				if body.Joined() || s.Joined() {
					t.Fatal("signal/native return joined an admitted outer call")
				}
				for _, row := range store.work {
					if row[5] != "running" {
						t.Fatal("work prematurely joined")
					}
				}
			}
			assertHeld()
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if !errors.Is(s.Drain(cancelled), context.Canceled) {
				t.Fatal("cancelled drain became successful")
			}
			if first == "read-first" {
				close(held.releaseRead)
				select {
				case a := <-readDone:
					if a.n != 1 || a.e != nil {
						t.Fatal("original Read result changed", a)
					}
				case <-time.After(time.Second):
					t.Fatal("Read return missing")
				}
				assertHeld()
				close(held.releaseClose)
			} else {
				close(held.releaseClose)
				if e = body.Close(); e != originalError {
					t.Fatal("original Close result replaced", e)
				}
				assertHeld()
				close(held.releaseRead)
				select {
				case a := <-readDone:
					if a.n != 1 || a.e != nil {
						t.Fatal("original Read result changed", a)
					}
				case <-time.After(time.Second):
					t.Fatal("Read return missing")
				}
			}
			waitPackageCalls(t, s)
			ctx, finish := context.WithTimeout(context.Background(), time.Second)
			defer finish()
			if e = s.Drain(ctx); e != nil || !s.Joined() {
				t.Fatal("released calls/accounting did not retire", e)
			}
			if e = body.Close(); e != originalError || !body.Joined() || objects.body.closes.Load() != 1 {
				t.Fatal("actual Close identity/single call/local join", e)
			}
			for _, row := range store.work {
				if row[5] != "joined" {
					t.Fatal("work was lost before terminal checkpoint")
				}
			}
		})
	}
}
