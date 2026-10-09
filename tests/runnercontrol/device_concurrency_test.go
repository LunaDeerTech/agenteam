//go:build integration

package runnercontrol_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sort"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type deviceCompetitionResult[T any] struct {
	index int
	value T
	err   error
}

// Both calls must reach actual advisory-lock waits before the holder releases.
// The recursive observation includes a contender queued behind the other
// contender; merely starting two goroutines does not prove this competition.
func competeRunnerDevice[T any](t *testing.T, fixture *runnerServiceFixture, target rc.RunnerID, calls [2]func(context.Context) (T, error)) [2]deviceCompetitionResult[T] {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	global, e := f.SystemConfigLock("runner-management")
	requireServiceOK(t, e, "competition management key")
	connection, e := f.SystemConfigLock("runner-control-" + target.String())
	requireServiceOK(t, e, "competition connection key")
	locks := []f.LockRequest{{Key: global, Mode: f.Exclusive}, {Key: connection, Mode: f.Exclusive}}
	sort.Slice(locks, func(i, j int) bool { return f.CompareLockKeys(locks[i].Key, locks[j].Key) < 0 })
	cause, e := f.NewRecoveryCause("runner-competition", serviceID[struct{}](t).String(), "")
	requireServiceOK(t, e, "competition transaction cause")
	release, holderDone := make(chan struct{}), make(chan struct{})
	ready := make(chan int, 1)
	var releaseOnce sync.Once
	var held f.CommitResult
	results := make(chan deviceCompetitionResult[T], 2)
	callDone := make(chan struct{})
	started := false
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		tail, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case <-holderDone:
		case <-tail.Done():
			t.Error("competition holder did not actually return")
		}
		if started {
			select {
			case <-callDone:
			case <-tail.Done():
				t.Error("competition service calls did not actually return")
			}
		}
	})
	go func() {
		defer close(holderDone)
		held = fixture.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if e := fixture.store.AcquireAll(ctx, tx, locks); e != nil {
				return e
			}
			x, e := fixture.store.InTx(tx)
			if e != nil {
				return e
			}
			var pid int
			if e = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
				return e
			}
			ready <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var holderPID int
	select {
	case holderPID = <-ready:
	case <-holderDone:
		t.Fatalf("competition holder returned before admission: state=%s", held.State())
	case <-ctx.Done():
		t.Fatal("competition holder did not acquire real locks")
	}
	started = true
	var workers sync.WaitGroup
	for index, call := range calls {
		workers.Add(1)
		go func() {
			defer workers.Done()
			value, e := call(ctx)
			results <- deviceCompetitionResult[T]{index, value, e}
		}()
	}
	go func() { workers.Wait(); close(callDone) }()
	observe, stopObserve := context.WithTimeout(ctx, 5*time.Second)
	defer stopObserve()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case early := <-results:
			t.Fatalf("contender returned before real competition: index=%d error_present=%t", early.index, early.err != nil)
		case <-observe.Done():
			t.Fatal("both contender lock waits were not observed")
		case <-tick.C:
			var blocked int
			e = fixture.store.QueryRow(observe, `WITH RECURSIVE waiting(pid) AS (
 SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid))
 UNION
 SELECT a.pid FROM pg_stat_activity a JOIN waiting w ON w.pid=ANY(pg_blocking_pids(a.pid)) WHERE a.datname=current_database()
) SELECT count(*) FROM waiting w WHERE EXISTS (
 SELECT 1 FROM pg_locks l WHERE l.pid=w.pid AND l.locktype='advisory' AND l.mode='ExclusiveLock' AND NOT l.granted
)`, holderPID).Scan(&blocked)
			requireServiceOK(t, e, "actual contender lock observation")
			if blocked != 2 {
				continue
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case <-holderDone:
				if held.State() != f.Committed {
					t.Fatalf("competition holder not committed: state=%s", held.State())
				}
			case <-ctx.Done():
				t.Fatal("competition holder release did not actually return")
			}
			var out [2]deviceCompetitionResult[T]
			for range out {
				select {
				case result := <-results:
					out[result.index] = result
				case <-ctx.Done():
					t.Fatal("competition calls did not reach terminal results")
				}
			}
			select {
			case <-callDone:
			case <-ctx.Done():
				t.Fatal("competition result preceded actual worker join")
			}
			return out
		}
	}
}

func TestRunnerControlDeviceCompetition(t *testing.T) {
	fixture := newRunnerServiceFixture(t)
	services := [2]*service.Service{fixture.runner, fixture.newRunner(t)}
	intent, _, created := fixture.create(t, "competing devices")
	target := intent.Target()
	var private [2]ed25519.PrivateKey
	var requests [2]p.EnrollmentRequest
	for index := range private {
		public, key, e := ed25519.GenerateKey(rand.Reader)
		requireServiceOK(t, e, "competition device key")
		private[index] = key
		t.Cleanup(func() { clear(key) })
		requests[index], e = p.NewEnrollmentRequest(p.ID(target.String()), created.Material.Token, [32]byte(public), created.Receipt.Runner.RootPath, "linux", "amd64")
		requireServiceOK(t, e, "competition enrollment request")
	}
	var enrollWinner int
	t.Run("same token binds exactly one device key", func(t *testing.T) {
		results := competeRunnerDevice(t, fixture, target, [2]func(context.Context) (p.EnrollmentResponse, error){
			func(ctx context.Context) (p.EnrollmentResponse, error) { return services[0].Enroll(ctx, requests[0]) },
			func(ctx context.Context) (p.EnrollmentResponse, error) { return services[1].Enroll(ctx, requests[1]) },
		})
		wins := 0
		for _, result := range results {
			if result.err != nil {
				requireServiceCode(t, result.err, f.Unauthenticated)
				if _, e := p.EncodeEnrollmentResponse(result.value); e == nil {
					t.Fatal("losing enrollment published a valid receipt")
				}
				continue
			}
			wins++
			enrollWinner = result.index
			if result.value.RunnerID() != p.ID(target.String()) || result.value.Version() != "2" || result.value.CredentialGeneration() != "1" || result.value.PublicKeyFingerprint() != p.PublicKeyFingerprint(requests[result.index].PublicKey()) {
				t.Fatal("winning enrollment receipt differs from original device intent")
			}
		}
		if wins != 1 {
			t.Fatalf("same enrollment token yielded %d successful consumers", wins)
		}
		var stored []byte
		var version, generation, tokens, consumed, commands, events, enrolled, audits, enrollmentAudits int
		e := fixture.store.QueryRow(migrationContext(t), `SELECT device_public_key,version,credential_generation,
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid AND kind='enrolled'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid AND action='runner.enroll')
 FROM agenteam_runner.runners WHERE id=$1::uuid`, target.String()).Scan(&stored, &version, &generation, &tokens, &consumed, &commands, &events, &enrolled, &audits, &enrollmentAudits)
		requireServiceOK(t, e, "exact enrollment winner facts")
		public := requests[enrollWinner].PublicKey()
		if !bytes.Equal(stored, public[:]) || version != 2 || generation != 1 || tokens != 1 || consumed != 1 || commands != 1 || events != 2 || enrolled != 1 || audits != 2 || enrollmentAudits != 1 {
			t.Fatalf("enrollment facts mismatch: key_matches=%t version=%d generation=%d tokens=%d consumed=%d commands=%d events=%d enrolled=%d audits=%d enrollment_audits=%d", bytes.Equal(stored, public[:]), version, generation, tokens, consumed, commands, events, enrolled, audits, enrollmentAudits)
		}
	})
	if t.Failed() {
		return
	}
	t.Run("same nonce reserves exactly one owner generation", func(t *testing.T) {
		auth := fixture.authentication(t, target, private[enrollWinner])
		results := competeRunnerDevice(t, fixture, target, [2]func(context.Context) (service.Connection, error){
			func(ctx context.Context) (service.Connection, error) { return services[0].Authenticate(ctx, auth) },
			func(ctx context.Context) (service.Connection, error) { return services[1].Authenticate(ctx, auth) },
		})
		wins, winner := 0, 0
		for _, result := range results {
			if result.err != nil {
				requireServiceCode(t, result.err, f.Unauthenticated)
				if result.value.ID().Valid() || result.value.RunnerID().Validate() == nil {
					t.Fatal("losing authentication published a reservation")
				}
				continue
			}
			wins++
			winner = result.index
			if result.value.RunnerID() != target || !result.value.ID().Valid() {
				t.Fatal("winning reservation lost original Runner identity")
			}
		}
		if wins != 1 {
			t.Fatalf("same challenge yielded %d successful consumers", wins)
		}
		selected := results[winner].value
		var id string
		var businessVersion, credential, counter, connectionGeneration, challenges, consumed, events, audits int
		e := fixture.store.QueryRow(migrationContext(t), `SELECT r.version,r.credential_generation,r.connection_generation,c.id::text,c.generation,
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1::uuid AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid)
 FROM agenteam_runner.runners r JOIN agenteam_runner.connections c ON c.runner_id=r.id WHERE r.id=$1::uuid`, target.String()).Scan(&businessVersion, &credential, &counter, &id, &connectionGeneration, &challenges, &consumed, &events, &audits)
		requireServiceOK(t, e, "exact authentication winner facts")
		if businessVersion != 2 || credential != 1 || counter != 1 || id != string(selected.ID()) || connectionGeneration != 1 || challenges != 1 || consumed != 1 || events != 2 || audits != 2 {
			t.Fatalf("authentication facts mismatch: version=%d credential=%d counter=%d same_id=%t generation=%d challenges=%d consumed=%d events=%d audits=%d", businessVersion, credential, counter, id == string(selected.ID()), connectionGeneration, challenges, consumed, events, audits)
		}
		fixture.snapshot(t, target, rc.Offline)
		requireServiceCode(t, services[1-winner].CurrentConnection(migrationContext(t), selected), f.Unauthenticated)
		hello := p.Hello{RunnerID: p.ID(target.String()), RunnerVersion: "runner-test", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{}, FeatureFlags: []string{}}
		_, e = services[winner].AcceptHello(migrationContext(t), selected, hello)
		requireServiceOK(t, e, "winning owner hello")
		fixture.snapshot(t, target, rc.Online)
		for _, instance := range services {
			replay, e := instance.Authenticate(migrationContext(t), auth)
			requireServiceCode(t, e, f.Unauthenticated)
			if replay.ID().Valid() {
				t.Fatal("consumed nonce replay published another connection")
			}
		}
		requireServiceOK(t, services[winner].OwnConnection(migrationContext(t), selected, func(context.Context) error { return nil }), "winning owner actual retirement")
		fixture.snapshot(t, target, rc.Offline)
		var remaining, afterCounter int
		e = fixture.store.QueryRow(migrationContext(t), `SELECT connection_generation,(SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1::uuid) FROM agenteam_runner.runners WHERE id=$1::uuid`, target.String()).Scan(&afterCounter, &remaining)
		requireServiceOK(t, e, "post-retirement connection facts")
		if afterCounter != 1 || remaining != 0 {
			t.Fatalf("replay/retirement changed generation or left connection: counter=%d remaining=%d", afterCounter, remaining)
		}
	})
}
