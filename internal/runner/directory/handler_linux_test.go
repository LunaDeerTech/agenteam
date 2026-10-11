//go:build linux

package directory

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestDirectoryHandlerRealWorkspaceTraversalAndIsolation(t *testing.T) {
	deviceRoot := t.TempDir()
	root, err := os.Open(deviceRoot)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := workspace.New(root)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal("real directory workspace construction failed")
	}
	t.Cleanup(func() {
		if manager.Drain(context.Background()) != nil {
			t.Error("workspace manager did not actually join")
		}
	})
	handler, err := New(manager)
	if err != nil {
		t.Fatal(err)
	}
	r := request(t, p.ListDirectoryOperation)
	identity := workspace.Identity{ProjectID: r.ProjectID, AgentID: r.AgentID, Name: "source"}
	handle, err := manager.Ensure(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if handle.Drain(context.Background()) != nil {
		t.Fatal("setup handle did not join")
	}
	workspaceRoot := filepath.Join(deviceRoot, string(r.ProjectID), string(r.AgentID), "source")
	for _, dir := range []string{"src/nested", "src/empty"} {
		if os.MkdirAll(filepath.Join(workspaceRoot, dir), 0700) != nil {
			t.Fatal("directory fixture setup failed")
		}
	}
	for _, name := range []string{"src/a.go", "src/b.txt", "src/nested/c.go", "src/nested/文.go", ".hidden.go"} {
		if os.WriteFile(filepath.Join(workspaceRoot, name), []byte("fixture"), 0600) != nil {
			t.Fatal("file fixture setup failed")
		}
	}
	if os.Symlink("nested", filepath.Join(workspaceRoot, "src/linked")) != nil {
		t.Fatal("contained symlink fixture failed")
	}
	if os.Symlink("../../../../outside", filepath.Join(workspaceRoot, "escape")) != nil {
		t.Fatal("outside symlink fixture failed")
	}
	execute := func(t *testing.T, operation string, args p.DirectoryArguments) p.Response {
		t.Helper()
		r.OperationName = operation
		r.Payload, err = p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: "source", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		response, callErr := handler.Execute(context.Background(), r)
		if callErr != nil || response.Outcome != p.Success || response.Code != nil || response.SafeMessage != nil || response.RequestID != r.RequestID || response.OperationID != r.OperationID {
			t.Fatal("directory operation did not return correlated success")
		}
		return response
	}
	t.Run("list-kinds", func(t *testing.T) {
		response := execute(t, p.ListDirectoryOperation, p.DirectoryArguments{Path: "src", Limit: 8})
		got, err := p.DecodeListDirectoryResult(response.Payload)
		want := p.ListDirectoryResult{Path: "src", Entries: []p.DirectoryEntry{{Name: "a.go", Kind: "file"}, {Name: "b.txt", Kind: "file"}, {Name: "empty", Kind: "directory"}, {Name: "linked", Kind: "symlink"}, {Name: "nested", Kind: "directory"}}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("list changed sorted names or local kinds")
		}
	})
	for _, tc := range []struct {
		name  string
		args  p.DirectoryArguments
		paths []string
	}{
		{"find-name", p.DirectoryArguments{Path: "src", Limit: 8, Pattern: "*.go", Match: "name"}, []string{"src/a.go", "src/nested/c.go", "src/nested/文.go"}},
		{"find-path", p.DirectoryArguments{Path: "src", Limit: 8, Pattern: "src/*.go", Match: "path"}, []string{"src/a.go"}},
		{"find-root-hidden", p.DirectoryArguments{Path: ".", Limit: 8, Pattern: ".*.go", Match: "name"}, []string{".hidden.go"}},
		{"find-exact-limit", p.DirectoryArguments{Path: "src", Limit: 3, Pattern: "*.go", Match: "name"}, []string{"src/a.go", "src/nested/c.go", "src/nested/文.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := execute(t, p.FindFilesOperation, tc.args)
			got, err := p.DecodeFindFilesResult(response.Payload)
			if err != nil || got.Path != tc.args.Path || got.Truncated || !reflect.DeepEqual(got.Paths, tc.paths) {
				t.Fatal("find changed its glob, traversal, or discovered symlink boundary")
			}
		})
	}
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		t.Run(operation+"-empty", func(t *testing.T) {
			args := p.DirectoryArguments{Path: "src/empty", Limit: 1}
			if operation == p.FindFilesOperation {
				args.Pattern, args.Match = "*", "name"
			}
			response := execute(t, operation, args)
			if operation == p.ListDirectoryOperation {
				got, err := p.DecodeListDirectoryResult(response.Payload)
				if err != nil || got.Path != args.Path || got.Entries == nil || len(got.Entries) != 0 || got.Truncated {
					t.Fatal("empty list was not explicit empty")
				}
			} else {
				got, err := p.DecodeFindFilesResult(response.Payload)
				if err != nil || got.Path != args.Path || got.Paths == nil || len(got.Paths) != 0 || got.Truncated {
					t.Fatal("empty find was not explicit empty")
				}
			}
		})
		t.Run(operation+"-observed-truncation", func(t *testing.T) {
			args := p.DirectoryArguments{Path: "src", Limit: 1}
			if operation == p.FindFilesOperation {
				args.Pattern, args.Match = "*.go", "name"
			}
			response := execute(t, operation, args)
			if operation == p.ListDirectoryOperation {
				got, err := p.DecodeListDirectoryResult(response.Payload)
				if err != nil || len(got.Entries) != 1 || !got.Truncated {
					t.Fatal("observed extra list entry did not truncate")
				}
			} else {
				got, err := p.DecodeFindFilesResult(response.Payload)
				if err != nil || len(got.Paths) != 1 || !got.Truncated {
					t.Fatal("observed extra matching file did not truncate")
				}
			}
		})
		t.Run(operation+"-outside", func(t *testing.T) {
			args := p.DirectoryArguments{Path: "escape", Limit: 1}
			if operation == p.FindFilesOperation {
				args.Pattern, args.Match = "*", "name"
			}
			r := request(t, operation)
			r.Payload, err = p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: "source", Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			response, err := handler.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, p.PathOutsideWorkspace)
		})
	}
	for _, scope := range []string{"project", "agent", "workspace"} {
		t.Run(scope+"-isolation", func(t *testing.T) {
			r := request(t, p.ListDirectoryOperation)
			switch scope {
			case "project":
				r.ProjectID = "01900000-0000-7000-8000-000000000008"
			case "agent":
				r.AgentID = "01900000-0000-7000-8000-000000000009"
			case "workspace":
				r.Payload, err = p.EncodeDirectoryPayload(p.ListDirectoryOperation, p.DirectoryPayload{WorkspaceName: "different", Arguments: p.DirectoryArguments{Path: "src", Limit: 2}})
				if err != nil {
					t.Fatal(err)
				}
			}
			response, err := handler.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, p.NotFound)
		})
	}
	// Individual executions borrow the Manager and drain only their own Handle.
	handle, err = manager.Ensure(context.Background(), identity)
	if err != nil {
		t.Fatal("directory handler stopped its borrowed Manager")
	}
	if handle.Drain(context.Background()) != nil {
		t.Fatal("final handle did not actually join")
	}
	manager.Stop()
	r = request(t, p.FindFilesOperation)
	response, err := handler.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Failure, p.CapabilityUnavailable)
}
