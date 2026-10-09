//go:build integration

package runnercontrol_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

// The read must observe peer retirement, not manufacture it with the test's
// deadline. Its lower bound starts before the frame/upgrade that starts the
// production timer, so setup latency cannot make an early close look valid.
func nativeDeadlineRetirement(t *testing.T, socket *websocket.Conn, started time.Time, minimum, maximum time.Duration) {
	t.Helper()
	requireServiceOK(t, socket.SetReadDeadline(started.Add(maximum)), "native natural deadline observation bound")
	_, _, e := socket.ReadMessage()
	var timeout net.Error
	if e == nil || errors.As(e, &timeout) && timeout.Timeout() {
		t.Fatal("natural session deadline did not retire the original peer before the observer bound")
	}
	elapsed := time.Since(started)
	if elapsed < minimum || elapsed >= maximum {
		t.Fatalf("natural session retirement outside original interval: elapsed=%s minimum=%s maximum=%s", elapsed, minimum, maximum)
	}
}

// Author integration only: both original monotonic timers run naturally.
// No fake clock, shortened heartbeat, forced socket close or direct SQL state
// mutation supplies the timer outcome. Requires its own real resource window.
func TestRunnerControlNativeDeadlines(t *testing.T) {
	fixture := newRunnerServiceFixture(t)
	server := newRunnerNativeServer(t, fixture.runner)
	t.Run("missing hello closes after the original five seconds", func(t *testing.T) {
		_, _, created := fixture.create(t, "missing native hello")
		target := created.Receipt.Runner.ID
		key, _ := fixture.enroll(t, created)
		auth := server.authentication(t, target, key)
		started := time.Now()
		socket := server.dial(t, auth, false, http.StatusSwitchingProtocols)
		fixture.snapshot(t, target, rc.Offline)
		nativeDeadlineRetirement(t, socket, started, 5*time.Second, 8*time.Second)
		server.waitControls(t, 0)
		value := fixture.snapshot(t, target, rc.Offline)
		if value.LastHello != nil || value.Version != 2 || value.CredentialGeneration != 1 {
			t.Fatal("missing hello fabricated published capabilities or changed the credential")
		}
		var current int
		e := fixture.store.QueryRow(migrationContext(t), `SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1::uuid`, target.String()).Scan(&current)
		requireServiceOK(t, e, "missing hello durable retirement")
		if current != 0 {
			t.Fatal("expired hello retained the reserved connection")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("accepted status cannot renew the original heartbeat lease", func(t *testing.T) {
		_, _, created := fixture.create(t, "status without native heartbeat")
		target := created.Receipt.Runner.ID
		key, _ := fixture.enroll(t, created)
		socket := server.dial(t, server.authentication(t, target, key), false, http.StatusSwitchingProtocols)
		started := time.Now()
		nativeRunnerHello(t, socket, target)
		fixture.snapshot(t, target, rc.Online)
		var originalSeen, originalLease time.Time
		e := fixture.store.QueryRow(migrationContext(t), `SELECT last_seen_at,lease_expires_at FROM agenteam_runner.connections WHERE runner_id=$1::uuid`, target.String()).Scan(&originalSeen, &originalLease)
		requireServiceOK(t, e, "original heartbeat lease")
		if originalLease.Sub(originalSeen) != 30*time.Second {
			t.Fatal("native hello did not reserve the declared thirty-second lease")
		}
		// This delay is the stimulus for a natural timer test. Actual database
		// publication below, rather than the timer alone, proves status arrived.
		timer := time.NewTimer(time.Until(started.Add(15 * time.Second)))
		defer timer.Stop()
		<-timer.C
		nativeRunnerSend(t, socket, p.RunnerStatus{Headless: false, Capabilities: []string{}})
		observed, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			value, readErr := fixture.runner.Get(observed, fixture.actor, target)
			requireServiceOK(t, readErr, "status publication within original observation deadline")
			if value.Status != rc.Online {
				t.Fatal("native status session retired before its original heartbeat deadline")
			}
			if value.LastHello != nil && !value.LastHello.Headless {
				break
			}
			select {
			case <-ticker.C:
			case <-observed.Done():
				t.Fatal("the real status frame was not published before its observation deadline")
			}
		}
		var seen, lease time.Time
		var sequence string
		e = fixture.store.QueryRow(migrationContext(t), `SELECT last_seen_at,lease_expires_at,heartbeat_sequence::text FROM agenteam_runner.connections WHERE runner_id=$1::uuid`, target.String()).Scan(&seen, &lease, &sequence)
		requireServiceOK(t, e, "status preserved original heartbeat facts")
		if !seen.Equal(originalSeen) || !lease.Equal(originalLease) || sequence != "0" {
			t.Fatal("non-heartbeat status refreshed heartbeat sequence, last-seen or lease")
		}
		nativeDeadlineRetirement(t, socket, started, 30*time.Second, 34*time.Second)
		server.waitControls(t, 0)
		value := fixture.snapshot(t, target, rc.Offline)
		if value.LastHello == nil || value.LastHello.Headless || value.Version != 2 || value.CredentialGeneration != 1 {
			t.Fatal("heartbeat expiry changed the last legitimate status or credential")
		}
		var current int
		e = fixture.store.QueryRow(migrationContext(t), `SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1::uuid`, target.String()).Scan(&current)
		requireServiceOK(t, e, "heartbeat durable retirement")
		if current != 0 {
			t.Fatal("expired heartbeat retained the current connection")
		}
	})
}
