//go:build linux

package readfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestHandlerRealWorkspaceReadAndIsolation(t *testing.T) {
	directory := t.TempDir()
	root, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	manager, err := workspace.New(root)
	if err != nil {
		t.Fatal("real Workspace construction failed", err)
	}
	t.Cleanup(func() {
		if err := manager.Drain(context.Background()); err != nil {
			t.Error("manager did not drain", err)
		}
	})
	handler, err := New(manager)
	if err != nil {
		t.Fatal(err)
	}
	r := request(t)
	identity := workspace.Identity{ProjectID: r.ProjectID, AgentID: r.AgentID, Name: "source"}
	handle, err := manager.Ensure(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, string(r.ProjectID), string(r.AgentID), "source", "file")
	if err := os.WriteFile(path, []byte("Aé\xffZ"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args p.ReadFileArguments
		want p.ReadFileResult
		code p.Code
	}{
		{"utf8", p.ReadFileArguments{Path: "file", Length: 3, Encoding: "utf8"}, p.ReadFileResult{Path: "file", BytesRead: 3, Encoding: "utf8", Data: "Aé"}, ""},
		{"base64", p.ReadFileArguments{Path: "file", Offset: 3, Length: 2, Encoding: "base64"}, p.ReadFileResult{Path: "file", Offset: 3, BytesRead: 2, EOF: true, Encoding: "base64", Data: "/1o="}, ""},
		{"past-eof", p.ReadFileArguments{Path: "file", Offset: 100, Length: 1, Encoding: "utf8"}, p.ReadFileResult{Path: "file", Offset: 100, EOF: true, Encoding: "utf8"}, ""},
		{"split-utf8", p.ReadFileArguments{Path: "file", Offset: 1, Length: 1, Encoding: "utf8"}, p.ReadFileResult{}, p.InvalidRequest},
		{"missing", p.ReadFileArguments{Path: "missing", Length: 1, Encoding: "utf8"}, p.ReadFileResult{}, p.NotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.Payload, err = p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "source", Arguments: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			response, err := handler.Execute(context.Background(), r)
			if tc.code != "" {
				assertFailure(t, r, response, err, p.Failure, tc.code)
				return
			}
			if err != nil || response.Outcome != p.Success || response.Code != nil || response.SafeMessage != nil || response.RequestID != r.RequestID || response.OperationID != r.OperationID {
				t.Fatal("real read did not produce a correlated success", response, err)
			}
			result, err := p.DecodeReadFileResult(response.Payload)
			if err != nil || result != tc.want {
				t.Fatal("real read bytes or EOF differ", err)
			}
		})
	}
	r = request(t)
	for _, scope := range []string{"project", "agent", "workspace"} {
		t.Run(scope, func(t *testing.T) {
			other := r
			switch scope {
			case "project":
				other.ProjectID = "01900000-0000-7000-8000-000000000008"
			case "agent":
				other.AgentID = "01900000-0000-7000-8000-000000000009"
			case "workspace":
				other.Payload, err = p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "different", Arguments: p.ReadFileArguments{Path: "file", Length: 3, Encoding: "utf8"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			response, err := handler.Execute(context.Background(), other)
			assertFailure(t, other, response, err, p.Failure, p.NotFound)
		})
	}
	if err := os.Symlink("../../../../outside", filepath.Join(filepath.Dir(path), "escape")); err != nil {
		t.Fatal(err)
	}
	r.Payload, err = p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: "escape", Length: 1, Encoding: "utf8"}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := handler.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Failure, p.PathOutsideWorkspace)
	// Handler does not stop the borrowed Manager when an individual read returns.
	handle, err = manager.Ensure(context.Background(), identity)
	if err != nil {
		t.Fatal("handler stopped its borrowed Manager", err)
	}
	if err := handle.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager.Stop()
	response, err = handler.Execute(context.Background(), request(t))
	assertFailure(t, request(t), response, err, p.Failure, p.CapabilityUnavailable)
}
