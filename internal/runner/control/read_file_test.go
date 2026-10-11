//go:build linux

package control

import (
	"context"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/readfile"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func readFileFixture(t *testing.T) (*readfile.Handler, string) {
	t.Helper()
	directory := t.TempDir()
	root, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := workspace.New(root)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal("workspace construction failed", err, closeErr)
	}
	t.Cleanup(func() {
		if err := manager.Drain(context.Background()); err != nil {
			t.Error("workspace manager did not join", err)
		}
	})
	handler, err := readfile.New(manager)
	if err != nil {
		t.Fatal(err)
	}
	return handler, directory
}

func TestReadFileExplicitClientKeepsDefaultUnbound(t *testing.T) {
	handler, _ := readFileFixture(t)
	options := Options{IdentityFile: "/PRIVATE_READ_FILE_IDENTITY/identity.json", RunnerVersion: "fixture-1"}
	plain, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := NewReadFile(options, handler)
	if err != nil {
		t.Fatal(err)
	}
	id := sessionID(t)
	if plain.executor != nil || len(plain.connectionHello(id, "fixture-1").Capabilities) != 0 {
		t.Fatal("default constructor registered an operation")
	}
	hello := bound.connectionHello(id, "fixture-1")
	if bound.executor == nil || len(hello.Capabilities) != 1 || hello.Capabilities[0] != p.ReadFileOperation || len(hello.FeatureFlags) != 0 {
		t.Fatal("explicit constructor did not bind only read-file")
	}
	for _, handler := range []*readfile.Handler{nil, {}} {
		if _, err := NewReadFile(options, handler); err == nil {
			t.Fatal("unbound handler accepted")
		}
	}
	plain.Stop()
	bound.Stop()
	if plain.Drain(context.Background()) != nil || bound.Drain(context.Background()) != nil {
		t.Fatal("offline clients did not drain")
	}
}

func TestReadFileControlSessionUsesRealHandler(t *testing.T) {
	handler, root := readFileFixture(t)
	r := request(t)
	r.OperationName, r.OperationRevision = p.ReadFileOperation, p.ReadFileRevision
	directory := filepath.Join(root, string(r.ProjectID), string(r.AgentID), "source")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	r.Payload, err = p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: "file", Offset: 1, Length: 3, Encoding: "utf8"}})
	if err != nil {
		t.Fatal(err)
	}
	socket, _, cancel, done := sessionFixture(t, readFileExecutor{handler: handler})
	defer finishSession(t, cancel, done)
	socket.in <- helloAck(t)
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	response, ok := nextMessage(t, socket).Payload().(p.Response)
	if !ok || response.Outcome != p.Success || response.RequestID != r.RequestID || response.OperationID != r.OperationID {
		t.Fatal("control read-file did not return correlated success", response)
	}
	result, err := p.DecodeReadFileResult(response.Payload)
	if err != nil || result.Data != "ell" || result.BytesRead != 3 || result.EOF {
		t.Fatal("control result lost the actual range", err)
	}
	for _, operation := range []string{"write-file", "read-file"} {
		r.RequestID = sessionID(t)
		r.OperationName = operation
		r.OperationRevision = "2"
		socket.in <- correlated(t, r, r.RequestID, r.OperationID)
		response, ok := nextMessage(t, socket).Payload().(p.Response)
		if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation {
			t.Fatal("unsupported operation/revision reached execution", response)
		}
	}
	r.RequestID = sessionID(t)
	r.OperationRevision = p.ReadFileRevision
	r.Environment = map[string]string{"PRIVATE": "ENVIRONMENT_CANARY"}
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	response, ok = nextMessage(t, socket).Payload().(p.Response)
	if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.CapabilityUnavailable || response.Payload != nil {
		t.Fatal("read-file admitted an unmasked environment", response)
	}
}

func TestReadFileClientRunUsesBoundExecutor(t *testing.T) {
	handler, _ := readFileFixture(t)
	stored, err := deviceIdentity(t).AsActive()
	if err != nil {
		t.Fatal(err)
	}
	file := &clientIdentity{value: stored}
	socket := clientSocket()
	challenge := freshChallenge(t)
	device := &clientDevice{
		challengeFn: func(context.Context, p.ID) (p.ChallengeResponse, error) { return challenge, nil },
		connectFn:   func(context.Context, identity.Identity, p.Nonce) (*wire, error) { return wireWithSocket(socket), nil },
	}
	c, err := NewReadFile(Options{IdentityFile: "/owned/private/identity.json", RunnerVersion: "fixture-1"}, handler)
	if err != nil {
		t.Fatal(err)
	}
	c.open = func(string) (ownedIdentity, error) { return file, nil }
	c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) { return device, nil }
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background()) }()
	joined := false
	t.Cleanup(func() {
		c.Stop()
		if !joined {
			_ = awaitClient(t, done)
		}
	})
	hello, ok := nextMessage(t, socket).Payload().(p.Hello)
	if !ok || len(hello.Capabilities) != 1 || hello.Capabilities[0] != p.ReadFileOperation {
		t.Fatal("client Run ignored explicit read-file binding")
	}
	socket.in <- helloAck(t)
	r := request(t)
	r.OperationName, r.OperationRevision = p.ReadFileOperation, p.ReadFileRevision
	r.Payload, err = p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: "missing", Length: 1, Encoding: "utf8"}})
	if err != nil {
		t.Fatal(err)
	}
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	response, ok := nextMessage(t, socket).Payload().(p.Response)
	if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.NotFound {
		t.Fatal("client did not execute its bound read-file handler", response)
	}
	c.Stop()
	_ = awaitClient(t, done)
	joined = true
	if !file.closed.Load() || !device.closed.Load() {
		t.Fatal("bound client did not join identity/transport owners")
	}
}
