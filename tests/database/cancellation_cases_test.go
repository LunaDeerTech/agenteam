//go:build integration

package database_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func cancellationPID(t *testing.T, s postgres.SQLExecutor) int32 {
	t.Helper()
	var pid int32
	if err := s.QueryRow(testContext(t), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestPoolCancellationNormalCleanupKeepsHealthyConnection(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newCancellationProxy(t, db, false)
	s := openStore(t, proxy.config(t, db, "2"))
	pid := cancellationPID(t, s)
	check := func() {
		t.Helper()
		if next := cancellationPID(t, s); next != pid {
			t.Fatalf("normal cleanup replaced healthy backend %d with %d", pid, next)
		}
		if n := proxy.count.Load(); n != 0 {
			t.Fatalf("normal cleanup emitted %d cancellation requests", n)
		}
	}
	for range 6 { // more than MaxConns; no lost capacity from internal cancel
		if _, err := s.Exec(testContext(t), "SELECT 1"); err != nil {
			t.Fatal(err)
		}
		check()
		for _, early := range []bool{false, true} {
			rows, err := s.Query(testContext(t), "SELECT generate_series(1,16)")
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var value int
				if err := rows.Scan(&value); err != nil {
					t.Fatal(err)
				}
				if early {
					break
				}
			}
			rows.Close()
			if rows.Err() != nil {
				t.Fatal(rows.Err())
			}
			check()
		}
		result := s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			e := executor(t, s, tx)
			if actual := cancellationPID(t, e); actual != pid {
				t.Fatal("transaction switched physical connection")
			}
			if _, err := e.Exec(ctx, "SELECT 1"); err != nil {
				return err
			}
			for _, early := range []bool{false, true} {
				rows, err := e.Query(ctx, "SELECT generate_series(1,16)")
				if err != nil {
					return err
				}
				for rows.Next() {
					var value int
					if err := rows.Scan(&value); err != nil {
						return err
					}
					if early {
						break
					}
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					return err
				}
			}
			var value int
			return e.QueryRow(ctx, "SELECT 42").Scan(&value)
		})
		requireState(t, result, foundation.Committed)
		check()
	}
	t.Logf("all ordinary and transaction Rows/Scan/Exec cleanup reused exact backend=%d; cancellation packets=0", pid)
}

func TestPoolCancellationReachableExecTransactionAndRows(t *testing.T) {
	for _, transport := range []string{"plaintext_proxy", "verify_full_tls"} {
		t.Run(transport, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			config := db.Config(t, map[string]string{"MAX_CONNS": "2"})
			var proxy *cancellationProxy
			if transport == "plaintext_proxy" {
				proxy = newCancellationProxy(t, db, false)
				config = proxy.config(t, db, "2")
			}
			s := openStore(t, config)
			admin := db.Connect(t)
			var otherPID int32
			if err := admin.QueryRow(testContext(t), "SELECT pg_backend_pid()").Scan(&otherPID); err != nil {
				t.Fatal(err)
			}
			seen := map[int32]bool{}
			for _, mode := range []string{"exec", "transaction", "rows", "exec", "transaction", "rows"} {
				pid := cancellationPID(t, s)
				if seen[pid] {
					t.Fatal("cancelled backend reused for new SQL")
				}
				seen[pid] = true
				ctx, cancel := context.WithCancel(testContext(t))
				done := make(chan error, 1)
				go func() {
					switch mode {
					case "transaction":
						result := s.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
							_, err := executor(t, s, tx).Exec(ctx, "SELECT pg_sleep(60)")
							return err
						})
						if result.State() != foundation.NotCommitted {
							t.Error("cancelled transaction changed commit classification")
						}
						done <- result.Fault()
					case "rows":
						rows, err := s.Query(ctx, "SELECT pg_sleep(60)")
						if err == nil {
							for rows.Next() {
							}
							err = rows.Err()
							rows.Close()
						}
						done <- err
					default:
						_, err := s.Exec(ctx, "SELECT pg_sleep(60)")
						done <- err
					}
				}()
				waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid)
				cancel()
				if err := receive(t, done); err == nil {
					t.Fatal("cancelled SQL succeeded")
				}
				waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
				var stillOther int32
				if err := admin.QueryRow(testContext(t), "SELECT pg_backend_pid()").Scan(&stillOther); err != nil || stillOther != otherPID {
					t.Fatal("cancellation affected another connection")
				}
				t.Logf("%s exact backend=%d exited; independent backend=%d remains", mode, pid, otherPID)
			}
			if next := cancellationPID(t, s); seen[next] {
				t.Fatal("connection reused after cancellation")
			}
			if proxy != nil && proxy.count.Load() < 6 {
				t.Fatal("normal cancellation control channel was not exercised")
			}
		})
	}
}

func TestPoolCancellationConcurrentForceUsesRemainingPhase(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newCancellationProxy(t, db, true)
	s, err := postgres.Open(testContext(t), proxy.config(t, db, "4"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ForceClose(testContext(t)) })
	t.Cleanup(proxy.releaseHeld)
	admin := db.Connect(t)
	ready := make(chan int32, 4)
	done := make(chan foundation.CommitResult, 4)
	for range 4 {
		go func() {
			done <- s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				e := executor(t, s, tx)
				ready <- cancellationPID(t, e)
				_, err := e.Exec(ctx, "SELECT pg_sleep(60)")
				return err
			})
		}()
	}
	var pids []int32
	for range 4 {
		pid := receive(t, ready)
		pids = append(pids, pid)
		waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid)
	}
	start := time.Now()
	forced := make(chan error, 2)
	go func() { forced <- s.ForceClose(testContext(t)) }()
	receive(t, proxy.reached)
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() { forced <- s.ForceClose(short) }()
	for range 2 {
		if err := receive(t, forced); postgres.CodeOf(err) != postgres.DrainTimeout {
			t.Fatalf("held cancellation was reported as confirmed success: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("shared phase renewed per caller/target: %s", elapsed)
	}
	if !proxy.controlClosed(t) {
		t.Fatal("shortened cancellation control socket did not close")
	}
	proxy.releaseHeld()
	for range 4 {
		requireState(t, receive(t, done), foundation.NotCommitted)
	}
	for _, pid := range pids {
		waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
	}
	t.Logf("four real targets, two Force callers: elapsed=%s; all exact backends exited after held packet release", time.Since(start))
}

// A server-side ReadyForQuery barrier holds a real pool constructor after
// authentication. The cancelled Acquire has returned, but the background
// constructor and its exact PG backend still exist until Force seals sockets.
type cancellationStartupProxy struct {
	*cancellationProxy
	started     atomic.Int32
	holdStartup atomic.Bool
	ready       chan int32
	allow       chan struct{}
	allowMu     sync.Once
	replyMu     sync.Mutex
	reply       *cancellationReplyBarrier
}

type cancellationReplyBarrier struct{ reached, release chan struct{} }

func (p *cancellationStartupProxy) holdReply() *cancellationReplyBarrier {
	p.replyMu.Lock()
	defer p.replyMu.Unlock()
	p.reply = &cancellationReplyBarrier{reached: make(chan struct{}), release: make(chan struct{})}
	return p.reply
}

func newCancellationStartupProxy(t *testing.T, db *pgfixture.Database) *cancellationStartupProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &cancellationStartupProxy{cancellationProxy: &cancellationProxy{listener: l, upstream: net.JoinHostPort("127.0.0.1", db.Fixture.Port), quit: make(chan struct{}), connections: map[net.Conn]bool{}}, ready: make(chan int32, 1), allow: make(chan struct{})}
	p.holdStartup.Store(true)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := l.Accept()
			if err != nil {
				return
			}
			if !p.add(client) {
				return
			}
			p.wg.Add(1)
			go func() { defer p.wg.Done(); p.serveStartup(client) }()
		}
	}()
	t.Cleanup(p.Close)
	return p
}
func (p *cancellationStartupProxy) serveStartup(client net.Conn) {
	defer p.remove(client)
	var length [4]byte
	if _, err := io.ReadFull(client, length[:]); err != nil {
		return
	}
	size := binary.BigEndian.Uint32(length[:])
	if size < 8 || size > 1<<20 {
		return
	}
	packet := make([]byte, size)
	copy(packet, length[:])
	defer clear(packet)
	if _, err := io.ReadFull(client, packet[4:]); err != nil {
		return
	}
	isCancel := binary.BigEndian.Uint32(packet[4:8]) == 80877102
	hold := !isCancel && p.started.Add(1) == 2 && p.holdStartup.Load()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	server, err := (&net.Dialer{}).DialContext(ctx, "tcp", p.upstream)
	if err != nil || !p.add(server) {
		return
	}
	defer p.remove(server)
	if _, err := server.Write(packet); err != nil {
		return
	}
	clientDone := make(chan struct{})
	serverDone := make(chan struct{})
	go func() { _, _ = io.Copy(server, client); close(clientDone); _ = server.Close() }()
	go func() {
		defer close(serverDone)
		defer client.Close()
		if isCancel {
			_, _ = io.Copy(client, server)
			return
		}
		var pid int32
		authenticated := false
		for {
			var header [5]byte
			if _, err := io.ReadFull(server, header[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(header[1:])
			if n < 4 || n > 1<<20 {
				return
			}
			body := make([]byte, int(n)-4)
			if _, err := io.ReadFull(server, body); err != nil {
				clear(body)
				return
			}
			if header[0] == 'K' && len(body) >= 4 {
				pid = int32(binary.BigEndian.Uint32(body[:4]))
			}
			if header[0] == 'Z' {
				var release <-chan struct{}
				if !authenticated {
					authenticated = true
					if hold {
						p.ready <- pid
						release = p.allow
					}
				} else {
					p.replyMu.Lock()
					gate := p.reply
					p.reply = nil
					p.replyMu.Unlock()
					if gate != nil {
						close(gate.reached)
						release = gate.release
					}
				}
				if release != nil {
					select {
					case <-release:
					case <-clientDone:
						clear(body)
						return
					case <-p.quit:
						clear(body)
						return
					}
				}
			}
			_, err := client.Write(append(header[:], body...))
			clear(body)
			if err != nil {
				return
			}
		}
	}()
	<-clientDone
	<-serverDone
}

func TestPoolCancellationCompletedServerReplyRacesCallerCancellation(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newCancellationStartupProxy(t, db)
	proxy.holdStartup.Store(false)
	u, _ := url.Parse(db.Fixture.URL(db.Name))
	u.Host = proxy.listener.Addr().String()
	s := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "MAX_CONNS": "2"}))
	admin := db.Connect(t)
	for round := range 6 {
		pid := cancellationPID(t, s)
		gate := proxy.holdReply()
		ctx, cancel := context.WithCancel(testContext(t))
		done := make(chan error, 1)
		go func() { _, err := s.Exec(ctx, "SELECT 1"); done <- err }()
		receive(t, gate.reached)
		// PostgreSQL already completed SELECT and emitted ReadyForQuery. The
		// owner is still reading. Callback/Unwatch now race the released reply.
		waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND state='idle' AND query='SELECT 1')", pid)
		select {
		case <-done:
			t.Fatal("fixture did not hold the SQL owner at completion")
		default:
		}
		cancel()
		close(gate.release)
		_ = receive(t, done) // completed SQL may report success or cancellation
		waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
		next := cancellationPID(t, s)
		if next == pid {
			t.Fatal("completion/cancel race returned the cancelled physical connection to pool")
		}
		if _, err := s.Exec(testContext(t), "SELECT 1"); err != nil || cancellationPID(t, s) != next {
			t.Fatal("late cancellation reached the replacement checkout")
		}
		t.Logf("round=%d server completed SELECT before cancel; retired=%d replacement=%d remains healthy", round, pid, next)
	}
}

func TestPoolCancellationLateBackgroundAcquireCannotPublish(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newCancellationStartupProxy(t, db)
	u, _ := url.Parse(db.Fixture.URL(db.Name))
	u.Host = proxy.listener.Addr().String()
	s, err := postgres.Open(testContext(t), db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "MAX_CONNS": "2"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ForceClose(testContext(t)) })
	rows, err := s.Query(testContext(t), "SELECT 1") // hold the first checkout
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Exec(ctx, "SELECT pg_sleep(60)"); done <- err }()
	latePID := receive(t, proxy.ready)
	if latePID <= 0 {
		t.Fatal("real late connection has no authenticated backend identity")
	}
	cancel()
	if err := receive(t, done); err == nil {
		t.Fatal("cancelled Acquire reached business SQL")
	}
	admin := db.Connect(t)
	var exists bool
	if err := admin.QueryRow(testContext(t), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND state='idle')", latePID).Scan(&exists); err != nil || !exists {
		t.Fatal("background constructor did not remain alive at ReadyForQuery barrier")
	}
	forceDone := make(chan error, 1)
	go func() { forceDone <- s.ForceClose(testContext(t)) }()
	rows.Close()
	if err := receive(t, forceDone); err != nil {
		t.Fatal(err)
	}
	proxy.allowMu.Do(func() { close(proxy.allow) })
	if _, err := s.Exec(testContext(t), "SELECT 1"); postgres.CodeOf(err) != postgres.AdmissionStopped {
		t.Fatal("post-force borrower admitted")
	}
	waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", latePID)
	proxy.Close()
	t.Logf("exact late authenticated backend=%d exited; cancelled borrower never received the connection; proxy joined", latePID)
}

func TestPoolCancellationFailureDoesNotClaimServerStopped(t *testing.T) {
	for _, mode := range []string{"cancelled_parent", "expired_deadline", "control_connection_refused"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			proxy := newCancellationProxy(t, db, false)
			s, err := postgres.Open(testContext(t), proxy.config(t, db, "2"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.ForceClose(testContext(t)) })
			pid := cancellationPID(t, s)
			admin := db.Connect(t)
			done := make(chan error, 1)
			go func() { _, err := s.Exec(testContext(t), "SELECT pg_sleep(60)"); done <- err }()
			waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			want := postgres.ConnectionFailed
			if mode == "cancelled_parent" {
				cancel()
				want = postgres.DrainTimeout
			} else if mode == "expired_deadline" {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				want = postgres.DrainTimeout
			} else {
				// Existing data socket remains live. Only new control TCP dials
				// now receive an actual local connection-refused response.
				if err := proxy.listener.Close(); err != nil {
					t.Fatal(err)
				}
			}
			start := time.Now()
			if err := s.ForceClose(ctx); postgres.CodeOf(err) != want {
				t.Fatalf("force error=%v want=%s", err, want)
			}
			if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
				t.Fatalf("failed force added a fresh wait budget: %s", elapsed)
			}
			if err := receive(t, done); err == nil {
				t.Fatal("forced query succeeded")
			}
			if mode == "control_connection_refused" && proxy.count.Load() != 0 {
				t.Fatal("unavailable control channel unexpectedly forwarded cancellation")
			}
			repeated := time.Now()
			if err := s.ForceClose(context.Background()); postgres.CodeOf(err) != want {
				t.Fatalf("repeated Force erased its known cancellation outcome: %v", err)
			}
			if time.Since(repeated) > 100*time.Millisecond {
				t.Fatal("repeated failed Force renewed cancellation I/O or its wait budget")
			}
			var remains bool
			if err := admin.QueryRow(testContext(t), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid).Scan(&remains); err != nil {
				t.Fatal(err)
			}
			// Wire count can include pgx asyncClose's independent best-effort
			// cancellation. The package-local expired-work test proves zero
			// platform dials directly; this count must not impersonate it.
			t.Logf("force error=%s elapsed=%s exact_backend=%d still_pg_sleep=%t all_wire_cancel_packets=%d; local join is not a server-stop proof", want, time.Since(start), pid, remains, proxy.count.Load())
			if remains {
				// Explicit fixture cleanup, not a production fallback. This
				// helper re-verifies this database's nonce and this exact PID.
				if err := db.Terminate(testContext(t), pid); err != nil {
					t.Fatal(err)
				}
			}
			waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
			proxy.Close()
		})
	}
}

func TestPoolCancellationWaitingAcquireDoesNotCancelOwnedCheckouts(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newCancellationProxy(t, db, false)
	s := openStore(t, proxy.config(t, db, "2"))
	var held []*postgres.Rows
	var pids []int32
	for range 2 {
		rows, err := s.Query(testContext(t), "SELECT pg_backend_pid()")
		if err != nil || !rows.Next() {
			t.Fatal("fixture could not retain pool checkout")
		}
		t.Cleanup(rows.Close)
		var pid int32
		if err := rows.Scan(&pid); err != nil {
			t.Fatal(err)
		}
		held, pids = append(held, rows), append(pids, pid)
	}
	admin := db.Connect(t)
	var count int
	if err := admin.QueryRow(testContext(t), "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam'").Scan(&count); err != nil || count != 2 {
		t.Fatal("physical pool limit not observed")
	}
	var done = make(chan error, 12)
	ctx, cancel := context.WithCancel(testContext(t))
	for range 12 {
		go func() { _, err := s.Exec(ctx, "SELECT 1"); done <- err }()
	}
	cancel()
	for range 12 {
		if err := receive(t, done); err == nil {
			t.Fatal("waiting Acquire ran SQL on an owned checkout")
		}
	}
	if err := admin.QueryRow(testContext(t), "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam'").Scan(&count); err != nil || count != 2 {
		t.Fatal("cancelled waiters exceeded physical pool capacity")
	}
	for _, rows := range held {
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal("waiter cancellation affected an independent owner")
		}
	}
	if pid := cancellationPID(t, s); pid != pids[0] && pid != pids[1] {
		t.Fatal("waiting Acquire discarded a healthy checkout")
	}
	if proxy.count.Load() != 0 {
		t.Fatal("targetless waiting operation emitted cancellation")
	}
	t.Log("12 cancelled waiters: 2 physical backends, independent checkouts unaffected, 0 cancellation packets")
}

func TestPoolCancellationConcurrentSuccessfulForceAndRepeatedResult(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newCancellationProxy(t, db, false)
	s, err := postgres.Open(testContext(t), proxy.config(t, db, "2"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ForceClose(testContext(t)) })
	pid := cancellationPID(t, s)
	rows, err := s.Query(testContext(t), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	start := make(chan struct{})
	ready := make(chan struct{}, 32)
	done := make(chan error, 32)
	for range 32 {
		go func() {
			ready <- struct{}{}
			<-start
			done <- s.ForceClose(testContext(t))
		}()
	}
	for range 32 {
		receive(t, ready)
	}
	close(start)
	receive(t, proxy.reached)
	// Keep the actual pool checkout until the shared cancellation has begun;
	// all Force waiters must see the same completed result, not cleanup cancel.
	rows.Close()
	for range 32 {
		if err := receive(t, done); err != nil {
			t.Fatalf("successful shared force changed result for another caller: %v", err)
		}
	}
	for range 4 {
		if err := s.ForceClose(context.Background()); err != nil {
			t.Fatalf("already joined force acquired a new failure/budget: %v", err)
		}
	}
	admin := db.Connect(t)
	waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
	t.Log("32 concurrent Force callers and 4 completed repeats returned the same nil local result; exact backend exited")
}
