//go:build linux

package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/runcommand"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

// This is a trusted Runner-side test peer, not Central authentication, a Mount
// grant, an Operation producer or production Client/default-root integration.
func TestRunCommandWSS(t *testing.T) {
	t.Run("exit-and-cancel", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rootPath := t.TempDir()
		root, err := os.Open(rootPath)
		if err != nil {
			t.Fatal("owned workspace root open failed")
		}
		manager, err := workspace.New(root)
		closeErr := root.Close()
		if err != nil {
			t.Fatal("actual workspace manager construction failed")
		}
		managerDrainAttempted := false
		t.Cleanup(func() {
			if managerDrainAttempted {
				return
			}
			wait, end := context.WithTimeout(context.Background(), 5*time.Second)
			defer end()
			if manager.Drain(wait) != nil {
				t.Error("early-construction Manager cleanup did not actually return")
			}
		})
		if closeErr != nil {
			t.Fatal("borrowed workspace descriptor close failed")
		}
		handler, err := runcommand.New(manager)
		if err != nil {
			t.Fatal("actual run-command handler construction failed")
		}
		identityPath := filepath.Join(rootPath, "unopened-identity.json")
		client, err := NewRunCommand(Options{IdentityFile: identityPath, RunnerVersion: "run-command-wss-fixture"}, handler)
		if err != nil {
			t.Fatal("explicit run-command client construction failed")
		}
		peer := &runCommandWSSPeer{ctx: ctx, runner: sessionID(t), ready: make(chan struct{}), done: make(chan struct{}), messages: make(chan p.Message, 4), result: make(chan error, 1)}
		server := httptest.NewUnstartedServer(http.HandlerFunc(peer.serve))
		server.Config.ErrorLog = log.New(io.Discard, "", 0)
		var w *wire
		var runDone chan error
		var original *session
		var stopOnce sync.Once
		finish := func() {
			stopOnce.Do(func() {
				cancel()
				ok := true
				if w != nil && runDone == nil {
					w.stop()
				}
				if runDone != nil {
					select {
					case err := <-runDone:
						if !errors.Is(err, context.Canceled) {
							t.Error("original Control session did not return its cancellation")
							ok = false
						}
					case <-time.After(5 * time.Second):
						t.Error("original Control session/worker wait did not return")
						ok = false
						w.stop()
					}
				}
				peer.close()
				server.Close()
				if peer.connections.Load() != 0 {
					select {
					case <-peer.done:
						if err := <-peer.result; err != nil {
							t.Error("original WSS peer failed before cancellation")
							ok = false
						}
						if peer.closeErr != nil {
							t.Error("original WSS peer physical Close failed")
							ok = false
						}
					case <-time.After(5 * time.Second):
						t.Error("original WSS HTTP handler did not return")
						ok = false
					}
				}
				wait, end := context.WithTimeout(context.Background(), 5*time.Second)
				defer end()
				if runDone != nil && !w.joined(wait) {
					t.Error("original native reader/writer did not join")
					ok = false
				}
				if original != nil {
					original.mu.Lock()
					pending := len(original.requests)
					original.mu.Unlock()
					if pending != 0 {
						t.Error("Control retained an original request worker")
						ok = false
					}
				}
				client.Stop()
				if client.Drain(wait) != nil {
					t.Error("offline explicit Client did not drain")
					ok = false
				}
				manager.Stop()
				managerDrainAttempted = true
				if manager.Drain(wait) != nil {
					t.Error("original Manager/Handle/Root did not actually drain")
					ok = false
				}
				if _, err := os.Lstat(identityPath); !errors.Is(err, os.ErrNotExist) {
					t.Error("Runner-side Session fixture unexpectedly opened an identity file")
					ok = false
				}
				if peer.connections.Load() != 1 || peer.terminals.Load() != 2 {
					t.Error("exit/cancel did not use one connection and exactly two original terminals")
					ok = false
				}
				if ok && !t.Failed() {
					t.Log("RUN_COMMAND_WSS owned_resources_joined=true")
				}
			})
		}
		t.Cleanup(finish)
		server.StartTLS()
		roots := x509.NewCertPool()
		roots.AddCert(server.Certificate())
		dialer := websocket.Dialer{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, Subprotocols: []string{p.Subprotocol}, HandshakeTimeout: 5 * time.Second, EnableCompression: false}
		connection, response, err := dialer.DialContext(ctx, "wss"+strings.TrimPrefix(server.URL, "https"), nil)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			t.Fatal("owned verified TLS/WSS connection failed")
		}
		if response == nil || response.StatusCode != http.StatusSwitchingProtocols || response.Header.Get("Sec-WebSocket-Extensions") != "" {
			_ = connection.Close()
			t.Fatal("unexpected WSS handshake or compression")
		}
		w, err = newWire(connection)
		if err != nil {
			t.Fatal("original native Control wire construction failed")
		}
		actual, ok := client.executor.(runCommandExecutor)
		if !ok {
			t.Fatal("explicit command executor was not retained")
		}
		// Embedding the actual executor retains its private cancellation-tail
		// admission and env-free masking policy without creating a test grant.
		observed := &runCommandWSSExecutor{runCommandExecutor: actual, returned: make(chan p.ID, 2)}
		original, err = newSession(w, client.connectionHello(peer.runner, "run-command-wss-fixture"), observed)
		if err != nil {
			t.Fatal("original Control session construction failed")
		}
		runDone = make(chan error, 1)
		go func() { runDone <- original.run(ctx) }()
		select {
		case <-peer.ready:
		case <-peer.done:
			t.Fatal("actual peer ended before the exact Hello")
		case <-ctx.Done():
			t.Fatal("actual Control Hello did not complete")
		}

		request := runCommandWSSRequest(t, "exit")
		if peer.send(request, request.RequestID, request.OperationID) != nil {
			t.Fatal("original nonzero argv request frame failed")
		}
		first := &runCommandWSSOutput{}
		result := runCommandWSSResponse(t, ctx, peer, observed, request, first)
		if result.ExitCode != 23 || result.Signal != 0 || result.Cancelled || result.TimedOut || result.CompletionCode != nil ||
			!result.OutputComplete || !result.ProcessGroupTerminationConfirmed || result.StdoutTruncated || result.StderrTruncated ||
			first.stdout != runCommandWSSStdout || first.stderr != runCommandWSSStderr {
			t.Fatal("nonzero argv report lost the actual status, both streams or joins")
		}

		// The second child really starts on this original connection. Observe
		// both readiness streams before sending its exact original Cancel.
		request.RequestID, request.OperationID = sessionID(t), sessionID(t)
		request.Payload = runCommandWSSRequest(t, "cancel").Payload
		if peer.send(request, request.RequestID, request.OperationID) != nil {
			t.Fatal("original cancellable argv request frame failed")
		}
		second := &runCommandWSSOutput{}
		for second.stdout != runCommandWSSReadyOut || second.stderr != runCommandWSSReadyErr {
			message := runCommandWSSMessage(t, ctx, peer)
			if !second.accept(t, message, request) || !strings.HasPrefix(runCommandWSSReadyOut, second.stdout) || !strings.HasPrefix(runCommandWSSReadyErr, second.stderr) {
				t.Fatal("child did not provide both actual readiness streams before Cancel")
			}
		}
		select {
		case <-observed.returned:
			t.Fatal("cancellable child returned before its original Cancel")
		default:
		}
		if peer.send(p.Cancel{RequestID: request.RequestID, OperationID: request.OperationID, Reason: "caller_cancelled"}, request.RequestID, request.OperationID) != nil {
			t.Fatal("original correlated Cancel frame failed")
		}
		result = runCommandWSSResponse(t, ctx, peer, observed, request, second)
		if result.ExitCode != -1 || (result.Signal != int(syscall.SIGTERM) && result.Signal != int(syscall.SIGKILL)) ||
			!result.Cancelled || result.TimedOut || result.CompletionCode == nil || *result.CompletionCode != p.CancelledCode ||
			!result.OutputComplete || !result.ProcessGroupTerminationConfirmed || result.StdoutTruncated || result.StderrTruncated ||
			second.stdout != runCommandWSSReadyOut || second.stderr != runCommandWSSReadyErr {
			t.Fatal("original Cancel lost its actual Wait, pipe completion, status or Handle drain")
		}
		finish()
	})
}

const (
	runCommandWSSStdout   = "literal ; $(not-shell)\nstdout α\n"
	runCommandWSSStderr   = "stderr β\n"
	runCommandWSSReadyOut = "command-ready-stdout\n"
	runCommandWSSReadyErr = "command-ready-stderr\n"
)

// Only the actual command.Owner starts this helper, via absolute argv and its
// fixed child environment. Ordinary selection skips it; it owns no descendants.
func TestRunCommandWSSChild(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "run-command-wss-child" {
		t.Skip("subprocess-only helper")
	}
	if os.Args[len(os.Args)-1] != "literal ; $(not-shell)" {
		os.Exit(70)
	}
	mode := os.Args[len(os.Args)-2]
	out, errout := runCommandWSSStdout, runCommandWSSStderr
	if mode == "cancel" {
		out, errout = runCommandWSSReadyOut, runCommandWSSReadyErr
	} else if mode != "exit" {
		os.Exit(71)
	}
	if _, err := io.WriteString(os.Stdout, out); err != nil {
		os.Exit(72)
	}
	if _, err := io.WriteString(os.Stderr, errout); err != nil {
		os.Exit(73)
	}
	if mode == "exit" {
		os.Exit(23)
	}
	// Readiness, not this sleep, synchronizes the parent's original Cancel.
	for {
		time.Sleep(time.Hour)
	}
}

func runCommandWSSRequest(t *testing.T, mode string) p.Request {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal("actual child executable unavailable")
	}
	r := request(t)
	r.OperationName, r.OperationRevision = p.RunCommandOperation, p.RunCommandRevision
	r.Payload, err = p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "command-proof", Arguments: p.RunCommandArguments{
		Mode: "argv", Program: program, CWD: ".",
		Args: []string{"-test.run=^TestRunCommandWSSChild$", "--", "run-command-wss-child", mode, "literal ; $(not-shell)"},
	}})
	if err != nil {
		t.Fatal("original closed argv payload encoding failed")
	}
	return r
}

// This observer delegates the original executor unchanged and records only its
// actual synchronous return after command Wait, pipes and Handle Drain.
type runCommandWSSExecutor struct {
	runCommandExecutor
	returned chan p.ID
}

func (e *runCommandWSSExecutor) execute(ctx context.Context, request p.Request, stream func(string, string) error) (p.Response, error) {
	response, err := e.runCommandExecutor.execute(ctx, request, stream)
	e.returned <- request.RequestID
	return response, err
}

type runCommandWSSPeer struct {
	ctx                    context.Context
	runner                 p.ID
	mu, writeMu            sync.Mutex
	conn                   *websocket.Conn
	closeOnce              sync.Once
	closeErr               error
	ready, done            chan struct{}
	messages               chan p.Message
	result                 chan error
	connections, terminals atomic.Int32
}

func (ppeer *runCommandWSSPeer) serve(writer http.ResponseWriter, request *http.Request) {
	if ppeer.connections.Add(1) != 1 {
		http.Error(writer, "single owned connection", http.StatusConflict)
		return
	}
	defer close(ppeer.done)
	err := func() error {
		if request.TLS == nil || !request.TLS.HandshakeComplete {
			return ErrProtocol
		}
		upgrade := websocket.Upgrader{Subprotocols: []string{p.Subprotocol}, EnableCompression: false}
		connection, err := upgrade.Upgrade(writer, request, nil)
		if err != nil {
			return ErrTransport
		}
		ppeer.mu.Lock()
		ppeer.conn = connection
		ppeer.mu.Unlock()
		defer ppeer.close()
		connection.SetReadLimit(p.MaxMessageBytes)
		if connection.Subprotocol() != p.Subprotocol {
			return ErrProtocol
		}
		seenHello := false
		seen := map[p.ID]bool{}
		for {
			kind, raw, err := connection.ReadMessage()
			if err != nil {
				if ppeer.ctx.Err() != nil {
					return nil
				}
				return ErrTransport
			}
			message, err := p.Decode(raw)
			clear(raw)
			if err != nil || kind != websocket.TextMessage {
				return ErrProtocol
			}
			switch payload := message.Payload().(type) {
			case p.Hello:
				if seenHello || payload.RunnerID != ppeer.runner || !reflect.DeepEqual(payload.Capabilities, []string{p.RunCommandOperation}) || len(payload.FeatureFlags) != 0 {
					return ErrProtocol
				}
				seenHello = true
				if err := ppeer.send(p.HelloAck{Accepted: true, NegotiatedProtocolVersion: p.CurrentVersion(), HeartbeatIntervalMS: 1000, HeartbeatTimeoutMS: 3000, EnabledFeatures: []string{}}, "", ""); err != nil {
					return err
				}
				close(ppeer.ready)
			case p.Heartbeat:
				if !seenHello {
					return ErrProtocol
				}
				if err := ppeer.send(p.HeartbeatAck{Sequence: payload.Sequence}, "", ""); err != nil {
					return err
				}
			case p.Stream:
				if !seenHello || seen[payload.RequestID] {
					return ErrProtocol
				}
				select {
				case ppeer.messages <- message:
				case <-ppeer.ctx.Done():
					return nil
				}
			case p.Response:
				if !seenHello || seen[payload.RequestID] {
					return ErrProtocol
				}
				seen[payload.RequestID] = true
				ppeer.terminals.Add(1)
				select {
				case ppeer.messages <- message:
				case <-ppeer.ctx.Done():
					return nil
				}
			default:
				return ErrProtocol
			}
		}
	}()
	ppeer.result <- err
}

func (ppeer *runCommandWSSPeer) close() {
	ppeer.mu.Lock()
	connection := ppeer.conn
	ppeer.mu.Unlock()
	if connection != nil {
		// Concurrent cleanup waits for this original physical Close to return.
		// Retain its first result privately; never retry or format its raw error.
		ppeer.closeOnce.Do(func() {
			_ = connection.UnderlyingConn().SetDeadline(time.Now())
			ppeer.closeErr = connection.Close()
		})
	}
}

func (ppeer *runCommandWSSPeer) send(payload p.Payload, request, operation p.ID) error {
	message, err := newMessage(payload, request, operation)
	if err != nil {
		return ErrProtocol
	}
	raw, err := p.Encode(message)
	if err != nil || len(raw) > p.MaxMessageBytes {
		return ErrProtocol
	}
	defer clear(raw)
	ppeer.writeMu.Lock()
	defer ppeer.writeMu.Unlock()
	ppeer.mu.Lock()
	connection := ppeer.conn
	ppeer.mu.Unlock()
	if connection == nil || connection.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil || connection.WriteMessage(websocket.TextMessage, raw) != nil {
		return ErrTransport
	}
	return nil
}

func runCommandWSSMessage(t *testing.T, ctx context.Context, peer *runCommandWSSPeer) p.Message {
	t.Helper()
	select {
	case message := <-peer.messages:
		return message
	case <-peer.done:
		t.Fatal("original peer ended before the requested terminal")
	case <-ctx.Done():
		t.Fatal("original correlated command message did not arrive")
	}
	return p.Message{}
}

type runCommandWSSOutput struct {
	stdout, stderr string
	sequence       [2]uint64
}

func (output *runCommandWSSOutput) accept(t *testing.T, message p.Message, request p.Request) bool {
	t.Helper()
	if message.Header().RequestID != request.RequestID || message.Header().OperationID != request.OperationID {
		t.Fatal("wire command message changed the original correlation")
	}
	stream, ok := message.Payload().(p.Stream)
	if !ok {
		return false
	}
	if stream.RequestID != request.RequestID || stream.OperationID != request.OperationID {
		t.Fatal("wire stream changed original payload correlation")
	}
	index, value := 0, &output.stdout
	if stream.Stream == "stderr" {
		index, value = 1, &output.stderr
	} else if stream.Stream != "stdout" {
		t.Fatal("command emitted a non-output stream")
	}
	output.sequence[index]++
	if string(stream.Sequence) != strconv.FormatUint(output.sequence[index], 10) || len(*value)+len(stream.Data) > p.MaxRunCommandStreamBytes {
		t.Fatal("wire stream lost original sequence or bounded byte count")
	}
	*value += stream.Data
	return true
}

func runCommandWSSResponse(t *testing.T, ctx context.Context, peer *runCommandWSSPeer, execution *runCommandWSSExecutor, request p.Request, output *runCommandWSSOutput) p.RunCommandResult {
	t.Helper()
	for {
		message := runCommandWSSMessage(t, ctx, peer)
		if output.accept(t, message, request) {
			continue
		}
		response, ok := message.Payload().(p.Response)
		if !ok || response.RequestID != request.RequestID || response.OperationID != request.OperationID || response.Outcome != p.Success || response.Code != nil || response.SafeMessage != nil {
			t.Fatal("wire terminal lost its correlated complete command report")
		}
		select {
		case returned := <-execution.returned:
			if returned != request.RequestID {
				t.Fatal("terminal did not follow its original Handler return")
			}
		default:
			t.Fatal("terminal preceded original command Wait and Handle Drain")
		}
		result, err := p.DecodeRunCommandResult(response.Payload)
		if err != nil || result.State != "completed" || string(result.StdoutBytes) != strconv.Itoa(len(output.stdout)) || string(result.StderrBytes) != strconv.Itoa(len(output.stderr)) {
			t.Fatal("complete report lost actual FIFO-admitted UTF-8 byte counts")
		}
		return result
	}
}
