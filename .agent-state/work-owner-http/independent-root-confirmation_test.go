//go:build integration

package app

// This probe calls the real Structure service installed by default run/bind.
// A physical COMMIT cut, not a manufactured CommitResult, enters its real
// WithoutCancel confirmation. HTTP publication and the other Work domains have
// separate acceptance scenarios; only Project Skills are test-owned here.
import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type independentConfirmationWait struct {
	pid      int32
	ctx      context.Context
	deadline time.Time
}
type independentConfirmationMarker struct{}
type independentConfirmationBundle struct {
	*workPlanningAssembly
	draining chan struct{}
	once     sync.Once
}

func (b *independentConfirmationBundle) Drain(ctx context.Context) error {
	b.once.Do(func() { close(b.draining) })
	return b.workPlanningAssembly.Drain(ctx)
}

type independentConfirmationStore struct {
	*postgres.Store
	proxy                                          *independentConfirmationProxy
	mu                                             sync.Mutex
	target                                         f.CommandIdentity
	parentCancel                                   context.CancelFunc
	armed, unknown                                 atomic.Bool
	original                                       chan f.CommitResult
	entered                                        chan independentConfirmationWait
	sqlReturned                                    chan error
	release, returned, dbStopped                   chan struct{}
	releaseOnce, returnedOnce, stopOnce, forceOnce sync.Once
	forced                                         chan context.Context
}

func (s *independentConfirmationStore) unhold() { s.releaseOnce.Do(func() { close(s.release) }) }
func (s *independentConfirmationStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	target, cancel := s.target, s.parentCancel
	s.mu.Unlock()
	selected := target.Validate() == nil && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == target.Canonical()
	confirmation := selected && s.unknown.Load()
	if confirmation {
		defer s.returnedOnce.Do(func() { close(s.returned) })
	}
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if confirmation {
			return fn(context.WithValue(ctx, independentConfirmationMarker{}, true), tx)
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if !selected || s.armed.Load() {
			return nil
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var completed bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, target.OwnerIDs()[0], target.Command(), string(target.Key())).Scan(&completed); err != nil {
			return err
		}
		if completed && s.armed.CompareAndSwap(false, true) {
			var pid int32
			if err = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			s.proxy.target.Store(pid)
		}
		return nil
	})
	if selected && result.State() == f.Unknown && s.unknown.CompareAndSwap(false, true) {
		// Cancel only after the actual physical Store has returned Unknown.
		// The real service must create/own its separate confirmation context.
		cancel()
		s.original <- result
	}
	return result
}
func (s *independentConfirmationStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if ctx.Value(independentConfirmationMarker{}) != true {
		return s.Store.AcquireAll(ctx, tx, locks)
	}
	x, err := s.Store.InTx(tx)
	if err != nil {
		return err
	}
	var pid int32
	if err = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	s.entered <- independentConfirmationWait{pid, ctx, deadline}
	err = s.Store.AcquireAll(ctx, tx, locks)
	s.sqlReturned <- err
	// The actual cancelled SQL has returned, but its owning adapter/Tx has not.
	// This tests ownership rather than treating cancellation as a join proof.
	<-s.release
	return err
}
func (s *independentConfirmationStore) StopAdmission() {
	s.stopOnce.Do(func() { close(s.dbStopped) })
	s.Store.StopAdmission()
}
func (s *independentConfirmationStore) ForceClose(ctx context.Context) error {
	s.forceOnce.Do(func() { s.forced <- ctx })
	return s.Store.ForceClose(ctx)
}

func TestIndependentWorkOwnerRootConfirmationJoin(t *testing.T) {
	for _, forced := range []bool{false, true} {
		t.Run(map[bool]string{false: "graceful-real-confirmation", true: "force-original-deadline"}[forced], func(t *testing.T) {
			budget := "5s"
			if forced {
				budget = "1s"
			}
			s := &independentConfirmationStore{original: make(chan f.CommitResult, 1), entered: make(chan independentConfirmationWait, 1), sqlReturned: make(chan error, 1), release: make(chan struct{}), returned: make(chan struct{}), dbStopped: make(chan struct{}), forced: make(chan context.Context, 1)}
			root := newModelRootApp(t, budget, func(root *modelRootApp, deps *dependencies) {
				s.proxy = newIndependentConfirmationProxy(t, net.JoinHostPort("127.0.0.1", root.db.Fixture.Port))
				deps.open = func(ctx context.Context, _ postgres.Config) (database, error) {
					u, err := url.Parse(root.db.Fixture.URL(root.db.Name))
					if err != nil {
						return nil, err
					}
					u.Host = s.proxy.listener.Addr().String()
					raw, err := postgres.Open(ctx, root.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "LOCK_TIMEOUT": "5s"}))
					if err != nil {
						return nil, err
					}
					s.Store, root.store.Store = raw, raw
					return s, nil
				}
			})
			// Release first on every failure, before the reused root cleanup joins.
			t.Cleanup(func() { s.unhold(); s.proxy.forward() })
			root.address(t)
			actor, err := fixtureAccountActor(databaseTestContext(t), root.cfg, root.core)
			if err != nil {
				t.Fatal("formal root Login actor")
			}
			projectID := independentConfirmationProject(t, root, actor)
			assembly := root.owned.accounts().(*accountAssembly)
			planning := assembly.planning.(*workPlanningAssembly)
			bundle := &independentConfirmationBundle{workPlanningAssembly: planning, draining: make(chan struct{})}
			activityDrain := make(chan struct{})
			assembly.mu.Lock()
			assembly.planning = bundle
			assembly.runtime = &workRootAccountDrain{accountRuntime: assembly.runtime, entered: activityDrain}
			assembly.mu.Unlock()
			admin := root.db.Connect(t)
			key := f.IdempotencyKey("independent-root-real-confirmation")
			identity, err := f.NewCommandIdentity("project", []string{projectID.String()}, string(wc.MilestoneCreate), key)
			if err != nil {
				t.Fatal("original identity")
			}
			caller, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			s.mu.Lock()
			s.target, s.parentCancel = identity, cancel
			s.mu.Unlock()
			milestone := guardID[wc.Milestone](t)
			requestID := guardID[f.Request](t)
			var response wc.StructureMutation
			var callErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				response, callErr = planning.structure.CreateMilestone(caller, actor, f.CommandMeta{RequestID: requestID, IdempotencyKey: key}, projectID, wc.CreateMilestoneRequest{MilestoneID: milestone, Title: "independent confirmation original"})
			}()
			t.Cleanup(func() {
				cancel()
				s.unhold()
				s.proxy.forward()
				independentConfirmationJoin(t, done, "original Work caller")
			})
			independentConfirmationJoin(t, s.proxy.reached, "actual original COMMIT frame")
			var original f.CommitResult
			select {
			case original = <-s.original:
			case <-done:
				t.Fatal("command returned without real Unknown")
			case <-time.After(3 * time.Second):
				t.Fatal("physical Store Unknown missing")
			}
			if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Details().Primary.Canonical() != identity.Canonical() || caller.Err() != context.Canceled {
				t.Fatal("original Unknown/cancel provenance")
			}
			var confirmation independentConfirmationWait
			select {
			case confirmation = <-s.entered:
			case <-done:
				t.Fatal("no owned WithoutCancel confirmation")
			case <-time.After(time.Second):
				t.Fatal("confirmation did not start")
			}
			remaining := time.Until(confirmation.deadline)
			if confirmation.ctx.Err() != nil || remaining <= 2*time.Second || remaining > 3*time.Second || confirmation.pid <= 0 || confirmation.pid == s.proxy.backend.Load() {
				t.Fatal("actual confirmation context was inherited/cancelled or unbounded")
			}
			commandLock, _ := f.CommandLock(identity)
			lock := uint64(commandLock.AdvisoryKey())
			waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
			for {
				var blocked bool
				err = admin.QueryRow(waitCtx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND classid::bigint=$2 AND objid::bigint=$3 AND objsubid=1 AND mode='ExclusiveLock' AND NOT granted AND $4::int=ANY(pg_blocking_pids(pid)))`, confirmation.pid, int64(lock>>32), int64(lock&0xffffffff), s.proxy.backend.Load()).Scan(&blocked)
				if err != nil {
					waitCancel()
					t.Fatal("real confirmation writer-lock observation")
				}
				if blocked {
					break
				}
				select {
				case <-waitCtx.Done():
					waitCancel()
					t.Fatal("confirmation never waited on the original physical writer")
				case <-time.After(2 * time.Millisecond):
				}
			}
			waitCancel()
			stopAt := time.Now()
			root.signals <- syscall.SIGTERM
			select {
			case <-confirmation.ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("Stop did not cancel the registered confirmation")
			}
			if confirmation.ctx.Err() != context.Canceled || !time.Now().Before(confirmation.deadline) {
				t.Fatal("Stop only waited out confirmation's natural deadline")
			}
			select {
			case e := <-s.sqlReturned:
				if e == nil {
					t.Fatal("blocked confirmation SQL unexpectedly succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled confirmation SQL did not return")
			}
			if planning.Joined() {
				t.Fatal("cancelled but held confirmation claimed Joined")
			}
			independentConfirmationJoin(t, bundle.draining, "root's real Work Drain admission")
			select {
			case <-done:
				t.Fatal("Work abandoned its held confirmation")
			default:
			}
			if forced {
				independentConfirmationJoin(t, root.done, "forced root terminal")
				if root.err == nil || planning.Joined() {
					t.Fatal("forced root invented successful join")
				}
				var forcedCtx context.Context
				select {
				case forcedCtx = <-s.forced:
				default:
					t.Fatal("unjoined confirmation prevented DB.ForceClose")
				}
				deadline, ok := forcedCtx.Deadline()
				root.owned.mu.Lock()
				originalForce := root.owned.forced
				root.owned.mu.Unlock()
				if !ok || forcedCtx != originalForce || forcedCtx.Err() == nil || deadline.After(stopAt.Add(1500*time.Millisecond)) {
					t.Fatal("Force renewed the original one-second budget")
				}
			} else {
				select {
				case <-activityDrain:
					t.Fatal("Activity retired before confirmation join")
				case <-s.dbStopped:
					t.Fatal("DB retired before confirmation join")
				case <-root.done:
					t.Fatal("root finished while confirmation held")
				default:
				}
			}
			// The old writer really commits on its own still-live backend. It is
			// not converted to a rollback because the caller/root was cancelled.
			s.proxy.forward()
			independentConfirmationJoin(t, s.proxy.committed, "physical COMMIT plus ReadyForQuery")
			s.unhold()
			independentConfirmationJoin(t, s.returned, "actual confirmation transaction return")
			independentConfirmationJoin(t, done, "actual Work caller return")
			var fault *f.Fault
			if !errors.As(callErr, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.RetryHint != "lookup" || fault.CauseID != original.AttemptID().String() || errors.Unwrap(callErr) == nil || errors.Unwrap(callErr).Error() != string(f.CommitUnknown) || response.Milestone != nil {
				t.Fatal("cancelled confirmation lost the original Unknown provenance")
			}
			independentConfirmationJoin(t, root.done, "root actual completion")
			if !forced && root.err != nil {
				t.Fatal("graceful root failed")
			}
			joinCtx, joinCancel := context.WithTimeout(context.Background(), time.Second)
			err = planning.Drain(joinCtx)
			joinCancel()
			if err != nil || !planning.Joined() {
				t.Fatal("released confirmation did not actually join")
			}
			if !forced {
				select {
				case <-activityDrain:
				default:
					t.Fatal("Activity never retired after Work join")
				}
			}
			var commands, objects, events int
			if err = admin.QueryRow(databaseTestContext(t), `SELECT (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed' AND receipt->'milestone'->>'id'=$4),(SELECT count(*) FROM agenteam_work.milestones WHERE project_id=$1 AND id=$4 AND version=1),(SELECT count(*) FROM agenteam_outbox.events WHERE producer='work' AND project_id=$1 AND aggregate_id=$4)`, projectID.String(), string(wc.MilestoneCreate), string(key), milestone.String()).Scan(&commands, &objects, &events); err != nil || commands != 1 || objects != 1 || events != 1 {
				t.Fatal("late original COMMIT did not produce exactly one durable fact")
			}
			s.proxy.close()
			retireCtx, retireCancel := context.WithTimeout(context.Background(), time.Second)
			for {
				var borrowers bool
				if err = admin.QueryRow(retireCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`).Scan(&borrowers); err != nil {
					retireCancel()
					t.Fatal("root/proxy database retirement observation")
				}
				if !borrowers {
					break
				}
				select {
				case <-retireCtx.Done():
					retireCancel()
					t.Fatal("root/proxy left a real database borrower")
				case <-time.After(5 * time.Millisecond):
				}
			}
			retireCancel()
			root.owned.mu.Lock()
			active := root.owned.httpActive
			root.owned.mu.Unlock()
			if active != 0 {
				t.Fatal("root retained HTTP work")
			}
		})
	}
}
func independentConfirmationJoin(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("missing actual join: " + what)
	}
}

// Only the complete-frame proxy seam is adapted from tests/work's accepted
// private helper. It forwards all setup traffic and arms one exact backend.
type independentConfirmationProxy struct {
	listener                          net.Listener
	upstream                          string
	target, backend                   atomic.Int32
	reached, release, committed, quit chan struct{}
	releaseOnce, closeOnce            sync.Once
	wg                                sync.WaitGroup
	mu                                sync.Mutex
	connections                       map[net.Conn]bool
}

func newIndependentConfirmationProxy(t *testing.T, upstream string) *independentConfirmationProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("owned confirmation proxy listener")
	}
	p := &independentConfirmationProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), committed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool)}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			if !p.track(client) {
				return
			}
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(p.close)
	return p
}
func (p *independentConfirmationProxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.quit:
		_ = c.Close()
		return false
	default:
		p.connections[c] = true
		return true
	}
}
func (p *independentConfirmationProxy) retire(c net.Conn) {
	_ = c.Close()
	p.mu.Lock()
	delete(p.connections, c)
	p.mu.Unlock()
}
func (p *independentConfirmationProxy) forward() { p.releaseOnce.Do(func() { close(p.release) }) }
func (p *independentConfirmationProxy) close() {
	p.closeOnce.Do(func() {
		close(p.quit)
		_ = p.listener.Close()
		p.mu.Lock()
		for c := range p.connections {
			_ = c.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
}
func (p *independentConfirmationProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer p.retire(client)
	server, e := net.DialTimeout("tcp", p.upstream, 3*time.Second)
	if e != nil {
		return
	}
	if !p.track(server) {
		return
	}
	defer p.retire(server)
	var header [4]byte
	if _, e = io.ReadFull(client, header[:]); e != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, e = io.ReadFull(client, startup[4:]); e != nil {
		return
	}
	if _, e = server.Write(startup); e != nil {
		return
	}
	var held atomic.Bool
	var backend atomic.Int32
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := independentConfirmationFrame(client)
			if e != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && backend.Load() != 0 && p.target.CompareAndSwap(backend.Load(), 0) {
				held.Store(true)
				p.backend.Store(backend.Load())
				_ = client.Close()
				close(p.reached)
				select {
				case <-p.release:
				case <-p.quit:
					return
				}
				if _, e = server.Write(frame); e != nil {
					return
				}
				select {
				case <-p.committed:
				case <-p.quit:
				}
				return
			}
			if _, e = server.Write(frame); e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := independentConfirmationFrame(server)
			if e != nil {
				return
			}
			if frame[0] == 'K' && len(frame) >= 9 {
				backend.Store(int32(binary.BigEndian.Uint32(frame[5:9])))
			}
			if held.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, e := independentConfirmationFrame(server)
				if e == nil && ready[0] == 'Z' && len(ready) == 6 && ready[5] == 'I' {
					close(p.committed)
				}
				return
			}
			if _, e = client.Write(frame); e != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
func independentConfirmationFrame(r io.Reader) ([]byte, error) {
	var header [5]byte
	if _, e := io.ReadFull(r, header[:]); e != nil {
		return nil, e
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 1<<20 {
		return nil, errors.New("invalid owned PG frame")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, e := io.ReadFull(r, frame[5:])
	return frame, e
}

// Project preparation is a real service/producer path, with only its absent
// Skills implementation replaced by a durable test-owned initialization fact.
func independentConfirmationProject(t *testing.T, root *modelRootApp, actor i.Actor) pc.ProjectID {
	t.Helper()
	store, err := postgres.Open(databaseTestContext(t), root.db.Config(t, nil))
	if err != nil {
		t.Fatal("Project preparation Store")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if store.ForceClose(ctx) != nil {
			t.Error("Project preparation Store not closed")
		}
	}()
	accounts, err := account.NewAuthority(store, root.cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	pa, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, root.cfg.CursorKeyring(), audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	events, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := independentConfirmationProcess{guardID[oc.Process](t)}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{pc.ProjectProducer: pa}, Projects: pa, Sessions: accounts, System: accounts, Processes: process, Audit: aud, Cursors: root.cfg.CursorKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Exec(databaseTestContext(t), `CREATE SCHEMA independent_work_confirmation; CREATE TABLE independent_work_confirmation.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	skills := &independentConfirmationSkills{store, pa, pc.NewInitializationPlanIssuer()}
	service, err := project.New(store, project.Dependencies{Authority: pa, Activity: accounts, Audit: aud, Events: box, ProjectEvents: events, Initializer: skills, Processes: process, Cursors: root.cfg.CursorKeyring()}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if service.Drain(ctx) != nil {
			t.Error("Project preparation service not joined")
		}
	}()
	p := guardID[i.Project](t)
	result, err := service.CreateProject(databaseTestContext(t), actor, f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: "independent-root-project"}, pc.CreateProjectRequest{ProjectID: p, Name: "independent-root-confirmation"})
	if err != nil || result.State != pc.CreationReady || result.Project == nil || result.Project.ID != p {
		t.Fatal("real initialized Project preparation")
	}
	return p
}

type independentConfirmationProcess struct{ id oc.ProcessID }

func (p independentConfirmationProcess) CurrentProcess() oc.ProcessID { return p.id }
func (independentConfirmationProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type independentConfirmationSkills struct {
	store     project.Store
	authority *project.Authority
	issuer    pc.InitializationPlanIssuer
}

func (s *independentConfirmationSkills) InspectProjectSkills(ctx context.Context, actor i.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	if actor.Details().ServiceName != i.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return pc.InitializationResult{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	var project, key, skill string
	var revision int64
	err := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM independent_work_confirmation.skills WHERE creation_id=$1`, r.CreationID.String()).Scan(&project, &key, &skill, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return pc.InitializationResult{State: pc.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: pc.ReasonWorkPending}, nil
	}
	if err != nil {
		return pc.InitializationResult{}, err
	}
	if project != r.ProjectID.String() || key != string(r.InitializationKey) {
		return pc.InitializationResult{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	id, err := f.ParseID[pc.Skill](skill)
	if err != nil {
		return pc.InitializationResult{}, err
	}
	rev := f.Revision(revision)
	return pc.InitializationResult{State: pc.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &id, Revision: &rev}, nil
}
func (s *independentConfirmationSkills) InitializeProjectSkills(ctx context.Context, actor i.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	key, _ := f.ProjectLock(r.ProjectID.String())
	cause, _ := f.NewRecoveryCause("project.independent-skills", r.CreationID.String(), "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		if err := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		skill, err := f.NewID[pc.Skill]()
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO independent_work_confirmation.skills VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return err
	})
	if result.State() != f.Committed {
		return pc.InitializationResult{}, f.NewFault(f.DependencyUnavailable, result.State())
	}
	return s.InspectProjectSkills(ctx, actor, r)
}
func (s *independentConfirmationSkills) DiscoverConfirmation(ctx context.Context, actor i.Actor, r pc.InitializationRequest) (pc.InitializationConfirmationPlan, error) {
	v, err := s.InspectProjectSkills(ctx, actor, r)
	if err != nil {
		return pc.InitializationConfirmationPlan{}, err
	}
	if v.State != pc.InitializationCompleted {
		return pc.InitializationConfirmationPlan{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	key, _ := f.ProjectLock(r.ProjectID.String())
	return s.issuer.Plan(actor, r, pc.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *v.AddSkillsID, Revision: *v.Revision}, []f.LockRequest{{Key: key, Mode: f.Exclusive}})
}
func (s *independentConfirmationSkills) ConfirmInitializedInTx(ctx context.Context, tx f.Tx, actor i.Actor, r pc.InitializationRequest, plan pc.InitializationConfirmationPlan) (pc.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return pc.InitializationReceipt{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return pc.InitializationReceipt{}, err
	}
	if err := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); err != nil {
		return pc.InitializationReceipt{}, err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return pc.InitializationReceipt{}, err
	}
	receipt := plan.ProposedReceipt()
	var valid bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM independent_work_confirmation.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if err != nil {
		return pc.InitializationReceipt{}, err
	}
	if !valid {
		return pc.InitializationReceipt{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	return receipt, nil
}
