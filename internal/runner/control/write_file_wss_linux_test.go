//go:build linux

package control

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	"github.com/LunaDeerTech/agenteam/internal/runner/writefile"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

// This is a trusted Runner-side test peer, not Central authentication, a Mount
// grant, an Operation producer or production Client/default-root integration.
func TestWriteFileWSS(t *testing.T) {
	t.Run("committed-write-and-overwrite-conflict", func(t *testing.T) {
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
		handler, err := writefile.New(manager)
		if err != nil {
			t.Fatal("actual write-file handler construction failed")
		}
		identityPath := filepath.Join(rootPath, "unopened-identity.json")
		client, err := NewWriteFile(Options{IdentityFile: identityPath, RunnerVersion: "write-file-wss-fixture"}, handler)
		if err != nil {
			t.Fatal("explicit write-file client construction failed")
		}
		peer := &writeWSSPeer{ctx: ctx, runner: sessionID(t), ready: make(chan struct{}), done: make(chan struct{}), messages: make(chan p.Message, 4), result: make(chan error, 1)}
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
					t.Error("write/conflict did not use one connection and exactly two original terminals")
					ok = false
				}
				if ok && !t.Failed() {
					t.Log("WRITE_FILE_WSS owned_resources_joined=true")
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
		observed := &writeWSSExecutor{operationExecutor: client.executor, returned: make(chan p.ID, 2)}
		original, err = newSession(w, client.connectionHello(peer.runner, "write-file-wss-fixture"), observed)
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

		data := make([]byte, p.MaxWriteFileBytes)
		for n := range data {
			data[n] = byte(n*31 + n/257)
		}
		defer clear(data)
		request := request(t)
		request.OperationName, request.OperationRevision = p.WriteFileOperation, p.WriteFileRevision
		request.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "write-proof", Arguments: p.WriteFileArguments{Path: "complete.bin", Data: data, Overwrite: false}})
		if err != nil {
			t.Fatal("bounded write payload encoding failed")
		}
		var encoded struct {
			Arguments struct {
				Data string `json:"data_base64"`
			} `json:"arguments"`
		}
		if json.Unmarshal(request.Payload, &encoded) != nil || len(encoded.Arguments.Data) != p.MaxWriteFileEncodedBytes || encoded.Arguments.Data != base64.StdEncoding.EncodeToString(data) {
			t.Fatal("wire payload lost canonical padded base64 or the exact raw cap")
		}
		if err := peer.send(request, request.RequestID, request.OperationID); err != nil {
			t.Fatal("bounded complete write request frame failed")
		}
		terminal := writeWSSResponse(t, ctx, peer, observed, request)
		if terminal.Outcome != p.Success || terminal.Code != nil || terminal.SafeMessage != nil {
			t.Fatal("actual complete write did not return correlated known success")
		}
		result, err := p.DecodeWriteFileResult(terminal.Payload)
		if err != nil || result.Path != "complete.bin" || result.State != "committed" || result.BytesWritten != len(data) || !result.Durable || result.CompletionCode != nil {
			t.Fatal("terminal lost actual publication, byte count or durability")
		}
		local := workspace.Identity{ProjectID: request.ProjectID, AgentID: request.AgentID, Name: "write-proof"}
		writeWSSReadAll(t, ctx, manager, local, data)
		request.RequestID, request.OperationID = sessionID(t), sessionID(t)
		request.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: local.Name, Arguments: p.WriteFileArguments{Path: "complete.bin", Data: []byte("must-not-replace"), Overwrite: false}})
		if err != nil || peer.send(request, request.RequestID, request.OperationID) != nil {
			t.Fatal("same-connection conflict request failed")
		}
		terminal = writeWSSResponse(t, ctx, peer, observed, request)
		if terminal.Outcome != p.Failure || terminal.Code == nil || *terminal.Code != p.Conflict || terminal.Payload != nil {
			t.Fatal("overwrite=false did not return an exact known conflict")
		}
		writeWSSReadAll(t, ctx, manager, local, data)
		finish()
	})
}

// This observer delegates the original executor unchanged and records only its
// actual synchronous return. It cannot manufacture a successful write or join.
type writeWSSExecutor struct {
	operationExecutor
	returned chan p.ID
}

func (e *writeWSSExecutor) execute(ctx context.Context, request p.Request, stream func(string, string) error) (p.Response, error) {
	response, err := e.operationExecutor.execute(ctx, request, stream)
	e.returned <- request.RequestID
	return response, err
}

type writeWSSPeer struct {
	ctx                    context.Context
	runner                 p.ID
	mu, writeMu            sync.Mutex
	conn                   *websocket.Conn
	ready, done            chan struct{}
	messages               chan p.Message
	result                 chan error
	connections, terminals atomic.Int32
}

func (ppeer *writeWSSPeer) serve(writer http.ResponseWriter, request *http.Request) {
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
				if seenHello || payload.RunnerID != ppeer.runner || !reflect.DeepEqual(payload.Capabilities, []string{p.WriteFileOperation}) || len(payload.FeatureFlags) != 0 {
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

func (ppeer *writeWSSPeer) close() {
	ppeer.mu.Lock()
	connection := ppeer.conn
	ppeer.mu.Unlock()
	if connection != nil {
		_ = connection.UnderlyingConn().SetDeadline(time.Now())
		_ = connection.Close()
	}
}

func (ppeer *writeWSSPeer) send(payload p.Payload, request, operation p.ID) error {
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

func writeWSSResponse(t *testing.T, ctx context.Context, peer *writeWSSPeer, execution *writeWSSExecutor, request p.Request) p.Response {
	t.Helper()
	var message p.Message
	select {
	case message = <-peer.messages:
	case <-peer.done:
		t.Fatal("original peer ended before the requested terminal")
	case <-ctx.Done():
		t.Fatal("original correlated terminal did not arrive")
	}
	response, ok := message.Payload().(p.Response)
	if !ok || message.Header().RequestID != request.RequestID || message.Header().OperationID != request.OperationID || response.RequestID != request.RequestID || response.OperationID != request.OperationID {
		t.Fatal("wire terminal changed the original RequestID/OperationID")
	}
	select {
	case returned := <-execution.returned:
		if returned != request.RequestID {
			t.Fatal("terminal did not follow its original Handler return")
		}
	default:
		t.Fatal("terminal preceded the original Handler return")
	}
	return response
}

func writeWSSReadAll(t *testing.T, ctx context.Context, manager *workspace.Manager, identity workspace.Identity, expected []byte) {
	t.Helper()
	handle, err := manager.Ensure(ctx, identity)
	if err != nil {
		t.Fatal("independent actual workspace handle failed")
	}
	defer func() {
		wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if handle.Drain(wait) != nil {
			t.Error("independent read Handle/Root did not actually drain")
		}
	}()
	actual := make([]byte, 0, len(expected))
	defer func() { clear(actual) }()
	for offset := 0; offset < len(expected); offset += filesystem.MaxReadBytes {
		result, err := handle.Read(ctx, filesystem.ReadRequest{Path: "complete.bin", Offset: int64(offset), Length: filesystem.MaxReadBytes, Encoding: "base64"})
		if err != nil || result.Path != "complete.bin" || result.Offset != int64(offset) || result.Encoding != "base64" || result.BytesRead != filesystem.MaxReadBytes || result.EOF != (offset+filesystem.MaxReadBytes == len(expected)) {
			t.Fatal("independent read lost complete file range or EOF")
		}
		chunk, err := base64.StdEncoding.Strict().DecodeString(result.Data)
		if err != nil || len(chunk) != result.BytesRead {
			t.Fatal("independent read returned invalid complete bytes")
		}
		actual = append(actual, chunk...)
		clear(chunk)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatal("independent full read differs from the original write bytes")
	}
}
