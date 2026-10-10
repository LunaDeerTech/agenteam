//go:build integration

package runnercontrol_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type nativeClientRun struct {
	client *control.Client
	done   chan struct{}
	err    error // Read only after done, which follows the original Run return.
}

func startNativeClient(t *testing.T, options control.Options, unblock func()) *nativeClientRun {
	t.Helper()
	client, e := control.New(options)
	requireServiceOK(t, e, "real Runner client construction")
	v := &nativeClientRun{client: client, done: make(chan struct{})}
	go func() { v.err = client.Run(context.Background()); close(v.done) }()
	t.Cleanup(func() {
		if unblock != nil {
			unblock()
		}
		client.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := client.Drain(ctx); e != nil {
			t.Error("real Runner client cleanup did not join")
		}
		select {
		case <-v.done:
		case <-ctx.Done():
			t.Error("original Runner Run return was not observed")
		}
	})
	return v
}

func (v *nativeClientRun) connected(t *testing.T, states <-chan control.ConnectionState) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	for {
		select {
		case state := <-states:
			if state == control.Connected {
				return
			}
		case <-v.done:
			t.Fatalf("real Runner ended before hello: auth=%t unsafe_file=%t protocol=%t transport=%t", errors.Is(v.err, control.ErrAuthentication), errors.Is(v.err, identity.ErrUnsafeFile), errors.Is(v.err, control.ErrProtocol), errors.Is(v.err, control.ErrTransport))
		case <-ctx.Done():
			t.Fatal("real Runner did not complete hello within the test bound")
		}
	}
}

func (v *nativeClientRun) stop(t *testing.T) {
	t.Helper()
	v.client.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	requireServiceOK(t, v.client.Drain(ctx), "real client Drain")
	select {
	case <-v.done:
		if !errors.Is(v.err, context.Canceled) {
			t.Fatal("real Runner cancellation did not preserve the original Run outcome")
		}
	case <-ctx.Done():
		t.Fatal("Drain success preceded the original Run return")
	}
}

func nativeIdentityPath(t *testing.T) string {
	t.Helper()
	// /tmp is intentionally rejected by the production ancestor policy. Keep
	// this private directory below the owned checkout, without relaxing modes.
	directory, e := os.MkdirTemp(".", ".runner-native-identity-")
	requireServiceOK(t, e, "owned identity directory")
	directory, e = filepath.Abs(directory)
	requireServiceOK(t, e, "owned identity path")
	t.Cleanup(func() {
		if e := os.RemoveAll(directory); e != nil {
			t.Error("owned identity directory removal failed")
		}
		if _, e := os.Stat(directory); !errors.Is(e, os.ErrNotExist) {
			t.Error("owned identity directory remains after cleanup")
		}
	})
	requireServiceOK(t, os.Chmod(directory, 0700), "private identity mode")
	return filepath.Join(directory, "identity.json")
}

func requireNativeIdentityLocked(t *testing.T, path string) {
	t.Helper()
	other, e := identity.Open(path)
	if other != nil {
		_ = other.Close()
	}
	if !errors.Is(e, identity.ErrLocked) {
		t.Fatal("real Runner identity was not held by the current owner")
	}
}

func nativeStoredIdentity(t *testing.T, path string, expected identity.Configuration) identity.Identity {
	t.Helper()
	stored, e := identity.ReadOnly(path)
	requireServiceOK(t, e, "strict actual identity snapshot")
	if stored.State() != identity.Active || stored.Configuration() != expected {
		t.Fatal("real enrollment did not persist the exact active identity")
	}
	info, e := os.Stat(path)
	requireServiceOK(t, e, "actual identity mode")
	if info.Mode().Perm() != 0600 {
		t.Fatal("real private identity did not retain mode 0600")
	}
	return stored
}

func (v *runnerServiceFixture) nativeIdentityFacts(t *testing.T, target rc.RunnerID) {
	t.Helper()
	var tokens, consumed, identityEvents, audits int
	e := v.store.QueryRow(migrationContext(t), `SELECT
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid)`, target.String()).Scan(&tokens, &consumed, &identityEvents, &audits)
	requireServiceOK(t, e, "native client durable identity facts")
	if tokens != 1 || consumed != 1 || identityEvents != 2 || audits != 2 {
		t.Fatalf("native client repeated identity facts: tokens=%d consumed=%d events=%d audits=%d", tokens, consumed, identityEvents, audits)
	}
}

func (v *runnerServiceFixture) nativeHeartbeat(t *testing.T, target rc.RunnerID, run *nativeClientRun) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var seen bool
		e := v.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_runner.connections WHERE runner_id=$1::uuid AND heartbeat_sequence>=1 AND hello_at IS NOT NULL AND lease_expires_at>clock_timestamp())`, target.String()).Scan(&seen)
		requireServiceOK(t, e, "native natural heartbeat fact")
		if seen {
			return
		}
		select {
		case <-run.done:
			t.Fatal("real Runner exited before its natural heartbeat")
		case <-ctx.Done():
			t.Fatal("real Runner did not send its original 10s heartbeat")
		case <-ticker.C:
		}
	}
}

// This top runs both unmodified production endpoints. There is no device,
// enrollment, session or operation replacement and no direct positive SQL seed.
// It requires a separately granted native/PG window; compile/list is not a run.
func TestRunnerControlNativeClientLifecycle(t *testing.T) {
	v := newRunnerServiceFixture(t)
	server := newRunnerNativeServer(t, v.runner)
	_, _, created := v.create(t, "native-production-client")
	target := created.Receipt.Runner.ID
	configuration := identity.Configuration{CentralURL: server.origin, RunnerID: p.ID(target.String()), RootPath: created.Receipt.Runner.RootPath}
	path := nativeIdentityPath(t)
	states := make(chan control.ConnectionState, 32)
	observe := func(ctx context.Context, state control.ConnectionState) {
		select {
		case states <- state:
		case <-ctx.Done():
		}
	}
	first := startNativeClient(t, control.Options{IdentityFile: path, Configuration: &configuration, EnrollmentToken: created.Material.Token, RunnerVersion: "native-production", Roots: server.security.RootCAs, Observe: observe}, nil)
	first.connected(t, states)
	stored := nativeStoredIdentity(t, path, configuration)
	public, e := stored.PublicKey()
	requireServiceOK(t, e, "actual stored public key")
	snapshot := v.snapshot(t, target, rc.Online)
	if snapshot.PublicKeyFingerprint == nil || string(*snapshot.PublicKeyFingerprint) != p.PublicKeyFingerprint(public) || snapshot.LastHello == nil || len(snapshot.LastHello.Capabilities) != 0 || len(snapshot.LastHello.FeatureFlags) != 0 {
		t.Fatal("native Client hello/identity fabricated a production operation")
	}
	requireNativeIdentityLocked(t, path)
	v.nativeHeartbeat(t, target, first)
	v.nativeIdentityFacts(t, target)
	first.stop(t)
	server.waitControls(t, 0)
	v.snapshot(t, target, rc.Offline)
	file, e := identity.Open(path)
	requireServiceOK(t, e, "identity reacquisition after actual client return")
	requireServiceOK(t, file.Close(), "released identity observer")

	// A normal restart receives neither token nor Configuration. It must read
	// the original private file, use a fresh challenge, and preserve the key.
	restartedStates := make(chan control.ConnectionState, 32)
	second := startNativeClient(t, control.Options{IdentityFile: path, RunnerVersion: "native-production", Roots: server.security.RootCAs, Observe: func(ctx context.Context, state control.ConnectionState) {
		select {
		case restartedStates <- state:
		case <-ctx.Done():
		}
	}}, nil)
	second.connected(t, restartedStates)
	reloaded := nativeStoredIdentity(t, path, configuration)
	restartedPublic, e := reloaded.PublicKey()
	requireServiceOK(t, e, "restarted actual public key")
	if restartedPublic != public {
		t.Fatal("normal restart replaced the original device key")
	}
	v.nativeIdentityFacts(t, target)
	v.snapshot(t, target, rc.Online)
	second.stop(t)
	server.waitControls(t, 0)

	// The real Connected callback holds the original reader, which in turn
	// retains the original identity owner. Force signals cancel; its deadline
	// cannot manufacture Run return or permit another identity owner.
	held := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	third := startNativeClient(t, control.Options{IdentityFile: path, RunnerVersion: "native-production", Roots: server.security.RootCAs, Observe: func(_ context.Context, state control.ConnectionState) {
		if state == control.Connected {
			close(held)
			<-release
		}
	}}, unblock)
	select {
	case <-held:
	case <-third.done:
		t.Fatal("third real client returned before the actual held callback")
	case <-time.After(12 * time.Second):
		t.Fatal("third real client did not reach the actual held callback")
	}
	v.snapshot(t, target, rc.Online)
	force, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	e = third.client.Force(force)
	forceAtReturn := force.Err()
	cancel()
	if !errors.Is(e, context.DeadlineExceeded) || !errors.Is(forceAtReturn, context.DeadlineExceeded) {
		t.Fatal("Force did not honor its original expired context")
	}
	select {
	case <-third.done:
		t.Fatal("held callback was represented as an actual Run return")
	default:
	}
	requireNativeIdentityLocked(t, path)
	unblock()
	third.stop(t)
	server.waitControls(t, 0)
	v.snapshot(t, target, rc.Offline)
	file, e = identity.Open(path)
	requireServiceOK(t, e, "identity reacquisition after real forced tail")
	requireServiceOK(t, file.Close(), "final identity observer close")
	v.nativeIdentityFacts(t, target)
}
