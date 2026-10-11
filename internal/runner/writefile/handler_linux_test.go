//go:build linux

package writefile

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestHandlerRealWorkspaceWriteAndRead(t *testing.T) {
	directory := t.TempDir()
	root, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := workspace.New(root)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal("real workspace construction failed")
	}
	t.Cleanup(func() {
		if err := manager.Drain(context.Background()); err != nil {
			t.Error("manager failed to join", err)
		}
	})
	handler, err := New(manager)
	if err != nil {
		t.Fatal(err)
	}
	r := request(t)
	identity := workspace.Identity{ProjectID: r.ProjectID, AgentID: r.AgentID, Name: "source"}
	readAll := func(identity workspace.Identity) []byte {
		t.Helper()
		handle, err := manager.Ensure(context.Background(), identity)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := handle.Drain(context.Background()); err != nil {
				t.Error("read handle did not close", err)
			}
		}()
		var data []byte
		for {
			part, err := handle.Read(context.Background(), filesystem.ReadRequest{Path: "file", Offset: int64(len(data)), Length: 65536, Encoding: "base64"})
			if err != nil {
				t.Fatal("independent real read failed", err)
			}
			decoded, err := base64.StdEncoding.Strict().DecodeString(part.Data)
			if err != nil || part.BytesRead != len(decoded) || part.Offset != int64(len(data)) {
				t.Fatal("independent read facts changed")
			}
			data = append(data, decoded...)
			if part.EOF {
				return data
			}
			if part.BytesRead != 65536 || len(data) > p.MaxWriteFileBytes {
				t.Fatal("read did not remain bounded")
			}
		}
	}
	setPayload := func(data []byte, overwrite bool) {
		t.Helper()
		r.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "file", Data: data, Overwrite: overwrite}})
		if err != nil {
			t.Fatal(err)
		}
	}
	maxData := bytes.Repeat([]byte{0xff, 0x00, 0x0a, 0x42}, p.MaxWriteFileBytes/4)
	setPayload(maxData, false)
	response, err := handler.Execute(context.Background(), r)
	assertCommitted(t, r, response, err, len(maxData), true, "")
	if !bytes.Equal(readAll(identity), maxData) {
		t.Fatal("maximum complete binary write differs")
	}
	setPayload([]byte("must-not-replace"), false)
	response, err = handler.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Failure, p.Conflict)
	if !bytes.Equal(readAll(identity), maxData) {
		t.Fatal("no-overwrite conflict changed content")
	}
	setPayload(nil, true)
	response, err = handler.Execute(context.Background(), r)
	assertCommitted(t, r, response, err, 0, true, "")
	if len(readAll(identity)) != 0 {
		t.Fatal("empty replacement retained old bytes")
	}
	for _, scope := range []string{"project", "agent", "workspace"} {
		t.Run(scope, func(t *testing.T) {
			other, otherIdentity := r, identity
			switch scope {
			case "project":
				other.ProjectID = "01900000-0000-7000-8000-000000000008"
				otherIdentity.ProjectID = other.ProjectID
			case "agent":
				other.AgentID = "01900000-0000-7000-8000-000000000009"
				otherIdentity.AgentID = other.AgentID
			case "workspace":
				otherIdentity.Name = "different"
			}
			other.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: otherIdentity.Name, Arguments: p.WriteFileArguments{Path: "file", Data: []byte(scope)}})
			if err != nil {
				t.Fatal(err)
			}
			response, err := handler.Execute(context.Background(), other)
			assertCommitted(t, other, response, err, len(scope), true, "")
			if !bytes.Equal(readAll(otherIdentity), []byte(scope)) || len(readAll(identity)) != 0 {
				t.Fatal("write crossed a workspace scope")
			}
		})
	}
	workspacePath := filepath.Join(directory, string(identity.ProjectID), string(identity.AgentID), identity.Name)
	if err := os.Symlink("../../../../outside", filepath.Join(workspacePath, "escape")); err != nil {
		t.Fatal(err)
	}
	r.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "escape", Data: []byte("private"), Overwrite: true}})
	if err != nil {
		t.Fatal(err)
	}
	response, err = handler.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Failure, p.PathOutsideWorkspace)
	if len(readAll(identity)) != 0 {
		t.Fatal("rejected symlink changed another file")
	}
	manager.Stop()
	response, err = handler.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Failure, p.CapabilityUnavailable)
}
