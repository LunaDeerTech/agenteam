//go:build integration

package runnercontrol_test

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

func nativeProtocolFrame(t *testing.T, payload p.Payload) (p.ID, []byte) {
	t.Helper()
	id, e := p.NewID()
	requireServiceOK(t, e, "native protocol message identity")
	message, e := p.NewMessage(p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: id, Timestamp: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))}, payload)
	requireServiceOK(t, e, "native protocol message")
	raw, e := p.Encode(message)
	requireServiceOK(t, e, "native protocol message encoding")
	return id, raw
}

func nativeProtocolWrite(t *testing.T, socket *websocket.Conn, raw []byte) {
	t.Helper()
	requireServiceOK(t, socket.SetWriteDeadline(time.Now().Add(3*time.Second)), "native protocol write deadline")
	requireServiceOK(t, socket.WriteMessage(websocket.TextMessage, raw), "native protocol input write")
}

func nativeProtocolRejected(t *testing.T, socket *websocket.Conn, code p.ProtocolCode, offending p.ID) {
	t.Helper()
	value, ok := nativeRunnerReceive(t, socket).Payload().(p.ProtocolError)
	if !ok || !value.Fatal || value.Code != code || value.SafeMessage != code.SafeMessage() || value.OffendingMessageID != offending {
		t.Fatal("native protocol rejection lost its exact safe code or message identity")
	}
	nativeRunnerClosed(t, socket)
}

// Real PG and native TLS/WSS author coverage. Compile/list is offline; running
// this top requires a separate resource grant and the original complete tail.
func TestRunnerControlNativeProtocolRejection(t *testing.T) {
	fixture := newRunnerServiceFixture(t)
	server := newRunnerNativeServer(t, fixture.runner)
	for _, scenario := range []string{"major mismatch", "duplicate envelope with wrong major", "heartbeat before hello", "duplicate heartbeat sequence"} {
		t.Run(scenario, func(t *testing.T) {
			_, _, created := fixture.create(t, scenario)
			target := created.Receipt.Runner.ID
			key, _ := fixture.enroll(t, created)
			socket := server.dial(t, server.authentication(t, target, key), false, http.StatusSwitchingProtocols)
			fixture.snapshot(t, target, rc.Offline)
			hello := p.Hello{RunnerID: p.ID(target.String()), RunnerVersion: "native-protocol-test", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{}, FeatureFlags: []string{}}
			var offending p.ID
			var raw []byte
			code, status := p.InvalidEnvelope, rc.Offline
			switch scenario {
			case "major mismatch":
				_, raw = nativeProtocolFrame(t, hello)
				if bytes.Count(raw, []byte(`"major":1`)) != 2 {
					t.Fatal("major rejection input does not contain both declared versions")
				}
				raw = bytes.ReplaceAll(raw, []byte(`"major":1`), []byte(`"major":2`))
				code, status = p.IncompatibleVersion, rc.Incompatible
			case "duplicate envelope with wrong major":
				_, valid := nativeProtocolFrame(t, hello)
				// Duplicate JSON is invalid before version classification. A bad
				// envelope cannot manufacture the persistent incompatible marker.
				raw = append([]byte(`{"protocol_version":{"major":2,"minor":0},`), valid[1:]...)
			case "heartbeat before hello":
				offending, raw = nativeProtocolFrame(t, p.Heartbeat{Sequence: "1", RunnerTime: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))})
				code = p.HelloOrder
			case "duplicate heartbeat sequence":
				nativeRunnerHello(t, socket, target)
				fixture.snapshot(t, target, rc.Online)
				nativeRunnerSend(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))})
				if ack, ok := nativeRunnerReceive(t, socket).Payload().(p.HeartbeatAck); !ok || ack.Sequence != "1" {
					t.Fatal("native heartbeat positive control did not acknowledge sequence one")
				}
				// New message ID, repeated sequence: this must fail sequence
				// correlation rather than the independent message-ID deduper.
				offending, raw = nativeProtocolFrame(t, p.Heartbeat{Sequence: "1", RunnerTime: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))})
				code = p.CorrelationInvalid
			}
			nativeProtocolWrite(t, socket, raw)
			nativeProtocolRejected(t, socket, code, offending)
			server.waitControls(t, 0)
			value := fixture.snapshot(t, target, status)
			if value.Version != 2 || value.CredentialGeneration != 1 {
				t.Fatal("protocol rejection changed business identity generation")
			}
			var current, generation, events, audits int
			e := fixture.store.QueryRow(migrationContext(t), `SELECT connection_generation,
 (SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid)
 FROM agenteam_runner.runners WHERE id=$1::uuid`, target.String()).Scan(&generation, &current, &events, &audits)
			requireServiceOK(t, e, "rejected native connection durable retirement")
			if generation != 1 || current != 0 || events != 2 || audits != 2 {
				t.Fatalf("protocol rejection facts generation=%d current=%d events=%d audits=%d", generation, current, events, audits)
			}
			if scenario == "major mismatch" {
				// The old marker remains until the successor's legitimate hello.
				successor := server.dial(t, server.authentication(t, target, key), false, http.StatusSwitchingProtocols)
				fixture.snapshot(t, target, rc.Incompatible)
				nativeRunnerHello(t, successor, target)
				fixture.snapshot(t, target, rc.Online)
				requireServiceOK(t, successor.Close(), "native successor explicit close")
				server.waitControls(t, 0)
				fixture.snapshot(t, target, rc.Offline)
			}
		})
		if t.Failed() {
			return
		}
	}
}
