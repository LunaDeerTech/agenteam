//go:build linux

package control

import (
	"context"
	"crypto/x509"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/directory"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func directoryFixture(t *testing.T) (*directory.Handler, string) {
	t.Helper()
	path := t.TempDir()
	root, err := os.Open(path)
	if err != nil {
		t.Fatal("directory fixture root open failed")
	}
	manager, err := workspace.New(root)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal("directory fixture manager construction failed")
	}
	t.Cleanup(func() {
		if manager.Drain(context.Background()) != nil {
			t.Error("directory fixture manager did not actually join")
		}
	})
	handler, err := directory.New(manager)
	if err != nil {
		t.Fatal("directory fixture handler construction failed")
	}
	return handler, path
}

func TestDirectoryControlExplicitClientsKeepExactCapabilities(t *testing.T) {
	handler, _ := directoryFixture(t)
	readHandler, _ := readFileFixture(t)
	options := Options{IdentityFile: "/PRIVATE_DIRECTORY_IDENTITY/identity.json", RunnerVersion: "fixture-1"}
	plain, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	read, err := NewReadFile(options, readHandler)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := NewDirectoryReads(options, handler)
	if err != nil {
		t.Fatal(err)
	}
	id := sessionID(t)
	if plain.executor != nil || len(plain.connectionHello(id, "fixture-1").Capabilities) != 0 {
		t.Fatal("default client registered an operation")
	}
	if !reflect.DeepEqual(read.connectionHello(id, "fixture-1").Capabilities, []string{p.ReadFileOperation}) {
		t.Fatal("read-file constructor advertised unrelated directory operations")
	}
	hello := bound.connectionHello(id, "fixture-1")
	if bound.executor == nil || !reflect.DeepEqual(hello.Capabilities, []string{p.ListDirectoryOperation, p.FindFilesOperation}) || len(hello.FeatureFlags) != 0 {
		t.Fatal("directory constructor did not bind its exact capabilities")
	}
	for _, handler := range []*directory.Handler{nil, {}} {
		if _, err := NewDirectoryReads(options, handler); err == nil {
			t.Fatal("unbound directory handler accepted")
		}
	}
	for _, client := range []*Client{plain, read, bound} {
		client.Stop()
		if client.Drain(context.Background()) != nil {
			t.Fatal("offline explicit client did not drain")
		}
	}
}

func TestDirectoryControlSessionUsesRealHandler(t *testing.T) {
	handler, root := directoryFixture(t)
	r := request(t)
	r.OperationRevision = p.DirectoryRevision
	dir := filepath.Join(root, string(r.ProjectID), string(r.AgentID), "source", "src")
	if os.MkdirAll(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "a.go"), []byte("fixture"), 0600) != nil {
		t.Fatal("directory session fixture setup failed")
	}
	socket, _, cancel, done := sessionFixture(t, directoryExecutor{handler: handler})
	defer finishSession(t, cancel, done)
	socket.in <- helloAck(t)
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		r.RequestID = sessionID(t)
		r.OperationName = operation
		a := p.DirectoryArguments{Path: "src", Limit: 2}
		if operation == p.FindFilesOperation {
			a.Pattern, a.Match = "*.go", "name"
		}
		var err error
		r.Payload, err = p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: "source", Arguments: a})
		if err != nil {
			t.Fatal(err)
		}
		socket.in <- correlated(t, r, r.RequestID, r.OperationID)
		response, ok := nextMessage(t, socket).Payload().(p.Response)
		if !ok || response.Outcome != p.Success || response.RequestID != r.RequestID || response.OperationID != r.OperationID {
			t.Fatal("directory session did not return correlated success")
		}
		if operation == p.ListDirectoryOperation {
			result, err := p.DecodeListDirectoryResult(response.Payload)
			if err != nil || result.Path != "src" || !reflect.DeepEqual(result.Entries, []p.DirectoryEntry{{Name: "a.go", Kind: "file"}}) || result.Truncated {
				t.Fatal("control list lost its real entry")
			}
		} else {
			result, err := p.DecodeFindFilesResult(response.Payload)
			if err != nil || result.Path != "src" || !reflect.DeepEqual(result.Paths, []string{"src/a.go"}) || result.Truncated {
				t.Fatal("control find lost its real matching path")
			}
		}
	}
	for _, tc := range []struct {
		operation string
		revision  p.Decimal
	}{{p.ReadFileOperation, p.ReadFileRevision}, {p.ListDirectoryOperation, "2"}, {p.FindFilesOperation, "2"}} {
		r.RequestID, r.OperationName, r.OperationRevision = sessionID(t), tc.operation, tc.revision
		socket.in <- correlated(t, r, r.RequestID, r.OperationID)
		response, ok := nextMessage(t, socket).Payload().(p.Response)
		if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation || response.Payload != nil {
			t.Fatal("directory client admitted another operation or revision")
		}
	}
	r.RequestID, r.OperationName, r.OperationRevision = sessionID(t), p.FindFilesOperation, p.DirectoryRevision
	r.Environment = map[string]string{"PRIVATE": "ENVIRONMENT_CANARY"}
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	response, ok := nextMessage(t, socket).Payload().(p.Response)
	if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.CapabilityUnavailable || response.Payload != nil {
		t.Fatal("directory client admitted unmasked environment")
	}
}

func TestDirectoryControlClientRunUsesBoundExecutor(t *testing.T) {
	handler, _ := directoryFixture(t)
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
	client, err := NewDirectoryReads(Options{IdentityFile: "/owned/private/identity.json", RunnerVersion: "fixture-1"}, handler)
	if err != nil {
		t.Fatal(err)
	}
	client.open = func(string) (ownedIdentity, error) { return file, nil }
	client.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) { return device, nil }
	done := make(chan error, 1)
	go func() { done <- client.Run(context.Background()) }()
	joined := false
	t.Cleanup(func() {
		client.Stop()
		if !joined {
			_ = awaitClient(t, done)
		}
	})
	hello, ok := nextMessage(t, socket).Payload().(p.Hello)
	if !ok || !reflect.DeepEqual(hello.Capabilities, []string{p.ListDirectoryOperation, p.FindFilesOperation}) {
		t.Fatal("Run ignored explicit directory binding")
	}
	socket.in <- helloAck(t)
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		r := request(t)
		r.OperationName, r.OperationRevision = operation, p.DirectoryRevision
		a := p.DirectoryArguments{Path: ".", Limit: 1}
		if operation == p.FindFilesOperation {
			a.Pattern, a.Match = "*", "name"
		}
		r.Payload, err = p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: "source", Arguments: a})
		if err != nil {
			t.Fatal(err)
		}
		socket.in <- correlated(t, r, r.RequestID, r.OperationID)
		response, ok := nextMessage(t, socket).Payload().(p.Response)
		if !ok || response.Outcome != p.Success || response.RequestID != r.RequestID || response.OperationID != r.OperationID {
			t.Fatal("Run did not use its bound directory executor")
		}
		if operation == p.ListDirectoryOperation {
			result, err := p.DecodeListDirectoryResult(response.Payload)
			if err != nil || result.Path != "." || result.Entries == nil || len(result.Entries) != 0 || result.Truncated {
				t.Fatal("Run lost explicit empty list")
			}
		} else {
			result, err := p.DecodeFindFilesResult(response.Payload)
			if err != nil || result.Path != "." || result.Paths == nil || len(result.Paths) != 0 || result.Truncated {
				t.Fatal("Run lost explicit empty find")
			}
		}
	}
	client.Stop()
	_ = awaitClient(t, done)
	joined = true
	if !file.closed.Load() || !device.closed.Load() {
		t.Fatal("directory Run did not join identity and transport owners")
	}
}
