//go:build linux

package control

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	"github.com/LunaDeerTech/agenteam/internal/runner/writefile"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func writeFileFixture(t *testing.T) (*writefile.Handler, string) {
	t.Helper()
	directory := t.TempDir()
	root, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := workspace.New(root)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal("workspace construction failed")
	}
	t.Cleanup(func() {
		if err := manager.Drain(context.Background()); err != nil {
			t.Error("workspace manager did not join", err)
		}
	})
	handler, err := writefile.New(manager)
	if err != nil {
		t.Fatal(err)
	}
	return handler, directory
}

func TestWriteFileExplicitClientKeepsDefaultUnbound(t *testing.T) {
	handler, _ := writeFileFixture(t)
	options := Options{IdentityFile: "/PRIVATE_WRITE_IDENTITY/identity.json", RunnerVersion: "fixture-1"}
	plain, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := NewWriteFile(options, handler)
	if err != nil {
		t.Fatal(err)
	}
	id := sessionID(t)
	if plain.executor != nil || len(plain.connectionHello(id, "fixture-1").Capabilities) != 0 {
		t.Fatal("default constructor became bound")
	}
	hello := bound.connectionHello(id, "fixture-1")
	if bound.executor == nil || len(hello.Capabilities) != 1 || hello.Capabilities[0] != p.WriteFileOperation || len(hello.FeatureFlags) != 0 {
		t.Fatal("explicit write-file capability changed")
	}
	if bound.executor.maskingReady() {
		t.Fatal("write-file declared a masking provider")
	}
	if _, err := bound.executor.mask("PRIVATE"); err == nil {
		t.Fatal("unbound masking accepted raw input")
	}
	for _, handler := range []*writefile.Handler{nil, {}} {
		if _, err := NewWriteFile(options, handler); err == nil {
			t.Fatal("unbound handler accepted")
		}
	}
	plain.Stop()
	bound.Stop()
	if plain.Drain(context.Background()) != nil || bound.Drain(context.Background()) != nil {
		t.Fatal("offline clients did not drain")
	}
}

func TestWriteFileControlSessionUsesRealHandler(t *testing.T) {
	handler, root := writeFileFixture(t)
	r := request(t)
	r.OperationName, r.OperationRevision = p.WriteFileOperation, p.WriteFileRevision
	data := []byte{0, 0xff, '\n', 'A'}
	var err error
	r.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "file", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	socket, _, cancel, done := sessionFixture(t, writeFileExecutor{handler: handler})
	defer finishSession(t, cancel, done)
	socket.in <- helloAck(t)
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	response, ok := nextMessage(t, socket).Payload().(p.Response)
	if !ok || response.RequestID != r.RequestID || response.OperationID != r.OperationID || response.Outcome != p.Success || response.Code != nil || response.SafeMessage != nil {
		t.Fatal("session lost write publication")
	}
	result, err := p.DecodeWriteFileResult(response.Payload)
	if err != nil || result.State != "committed" || result.BytesWritten != len(data) || !result.Durable || result.CompletionCode != nil {
		t.Fatal("session altered the committed payload")
	}
	actual, err := os.ReadFile(filepath.Join(root, string(r.ProjectID), string(r.AgentID), "source", "file"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("real Handler did not write the complete bytes")
	}
	// A fresh request is an explicit second attempt, never an automatic retry.
	r.RequestID = sessionID(t)
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	response, ok = nextMessage(t, socket).Payload().(p.Response)
	if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.Conflict || response.Payload != nil {
		t.Fatal("no-overwrite conflict was misreported")
	}
	for _, operation := range []string{p.ReadFileOperation, p.WriteFileOperation} {
		r.RequestID = sessionID(t)
		r.OperationName, r.OperationRevision = operation, "2"
		socket.in <- correlated(t, r, r.RequestID, r.OperationID)
		response, ok := nextMessage(t, socket).Payload().(p.Response)
		if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation || response.Payload != nil {
			t.Fatal("unsupported request reached write handler")
		}
	}
	r.RequestID = sessionID(t)
	r.OperationRevision = p.WriteFileRevision
	r.Environment = map[string]string{"PRIVATE": "ENVIRONMENT_CANARY"}
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	response, ok = nextMessage(t, socket).Payload().(p.Response)
	if !ok || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.CapabilityUnavailable || response.Payload != nil {
		t.Fatal("unmasked environment admitted")
	}
	actual, err = os.ReadFile(filepath.Join(root, string(r.ProjectID), string(r.AgentID), "source", "file"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("rejected requests changed file content")
	}
}
