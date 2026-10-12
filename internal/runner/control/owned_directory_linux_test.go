//go:build linux

package control

import (
	"context"
	"crypto/x509"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/operations"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestOwnedDirectoryStoredIdentityReads(t *testing.T) {
	for _, selection := range []string{
		"list-directory", "find-files", "find-files,list-directory",
		"read-file,list-directory", "find-files,read-file", "find-files,read-file,list-directory",
	} {
		t.Run(selection, func(t *testing.T) {
			selected, err := operations.Parse(selection)
			if err != nil {
				t.Fatal(err)
			}
			path, configuration := ownedReadFileIdentity(t)
			options := Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: selection}
			c, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			options.Operations = "changed-after-construction"
			var factories atomic.Int32
			c.ownOperations = func(got identity.Configuration, gotSelection operations.Selection) (operationExecutor, ownedReadFileManager, error) {
				factories.Add(1)
				if got != configuration || !slices.Equal(gotSelection.Names(), selected.Names()) {
					t.Error("directory root did not come from locked identity")
				}
				other, lockErr := identity.Open(path)
				if other != nil {
					_ = other.Close()
				}
				if lockErr != identity.ErrLocked {
					t.Error("directory factory ran outside the original identity lock")
				}
				return newOwnedOperations(got, gotSelection)
			}
			socket := clientSocket()
			ownedReadFileDevice(t, c, configuration, socket)
			run := startOwnedReadFileRun(t, c, nil)
			hello, ok := nextMessage(t, socket).Payload().(p.Hello)
			if !ok || factories.Load() != 1 || !slices.Equal(hello.Capabilities, selected.Names()) || len(hello.FeatureFlags) != 0 {
				t.Fatal("operation selection did not construct and announce its exact capabilities")
			}
			socket.in <- helloAck(t)
			read := ownedReadFileRequest(t, configuration.RootPath)
			root := filepath.Join(configuration.RootPath, string(read.ProjectID), string(read.AgentID), "source")
			if err = os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "nested", "a.go"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
				r := read
				r.RequestID, r.OperationID = sessionID(t), sessionID(t)
				r.OperationName, r.OperationRevision = operation, p.DirectoryRevision
				args := p.DirectoryArguments{Path: ".", Limit: 8}
				if operation == p.FindFilesOperation {
					args.Pattern, args.Match = "*.go", "name"
				}
				r.Payload, err = p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: "source", Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				socket.in <- correlated(t, r, r.RequestID, r.OperationID)
				result, ok := nextMessage(t, socket).Payload().(p.Response)
				if !ok || result.RequestID != r.RequestID || result.OperationID != r.OperationID {
					t.Fatal("directory lost original terminal identity")
				}
				if !selected.Contains(operation) {
					if result.Outcome != p.Failure || result.Code == nil || *result.Code != p.UnsupportedOperation {
						t.Fatal("unselected directory operation reached its Handler")
					}
					continue
				}
				if result.Outcome != p.Success {
					t.Fatal("selected directory operation failed")
				}
				if operation == p.ListDirectoryOperation {
					value, err := p.DecodeListDirectoryResult(result.Payload)
					if err != nil || value.Path != "." || value.Truncated || !slices.Equal(value.Entries, []p.DirectoryEntry{{Name: "file", Kind: "file"}, {Name: "nested", Kind: "directory"}}) {
						t.Fatal("real directory Handler list differs", err)
					}
				} else {
					value, err := p.DecodeFindFilesResult(result.Payload)
					if err != nil || value.Path != "." || value.Truncated || !slices.Equal(value.Paths, []string{"nested/a.go"}) {
						t.Fatal("real directory Handler find differs", err)
					}
				}
			}
			// The same Manager may serve selected read-file, but omission remains binding.
			socket.in <- correlated(t, read, read.RequestID, read.OperationID)
			response, ok := nextMessage(t, socket).Payload().(p.Response)
			if !ok || response.RequestID != read.RequestID || response.OperationID != read.OperationID {
				t.Fatal("read-file lost original terminal identity")
			}
			if selected.Contains(p.ReadFileOperation) {
				value, err := p.DecodeReadFileResult(response.Payload)
				if response.Outcome != p.Success || err != nil || value.Data != "ell" || value.BytesRead != 3 || value.EOF {
					t.Fatal("combined Handler did not read the original file range", err)
				}
			} else if response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation {
				t.Fatal("directory selection accepted undeclared read-file")
			}
			c.Stop()
			if err = run.join(t); err != context.Canceled {
				t.Fatal(err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}

func TestOwnedDirectoryRejectsBeforeDevice(t *testing.T) {
	for _, mode := range []string{"bad-root", "configuration-conflict", "external-executor"} {
		t.Run(mode, func(t *testing.T) {
			path, configuration := ownedReadFileIdentity(t)
			options := Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: "list-directory,find-files"}
			if mode == "bad-root" {
				if err := os.Remove(configuration.RootPath); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "configuration-conflict" {
				wrong := configuration
				wrong.RootPath = filepath.Join(configuration.RootPath, "different")
				options.Configuration = &wrong
			}
			var c *Client
			var err error
			if mode == "external-executor" {
				handler, _ := directoryFixture(t)
				c, err = NewDirectoryReads(options, handler)
			} else {
				c, err = New(options)
			}
			if err != nil {
				t.Fatal(err)
			}
			var opens, devices atomic.Int32
			c.open = func(path string) (ownedIdentity, error) { opens.Add(1); return identity.Open(path) }
			c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) {
				devices.Add(1)
				return nil, ErrTransport
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = c.Run(ctx); err == nil || devices.Load() != 0 || mode == "external-executor" && opens.Load() != 0 {
				t.Fatal("invalid directory owner reached the device or external identity", err)
			}
			if err = c.Drain(context.Background()); err != nil {
				t.Fatal(err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}

func TestOwnedDirectoryOriginalReturnKeepsIdentity(t *testing.T) {
	for _, closeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "joined", true: "cleanup-error"}[closeError], func(t *testing.T) {
			path, configuration := ownedReadFileIdentity(t)
			c, err := New(Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: "list-directory,find-files"})
			if err != nil {
				t.Fatal(err)
			}
			barrier := newOwnedReadFileDrainBarrier()
			if closeError {
				barrier.fault = workspace.ErrUnavailable
			}
			c.ownOperations = func(cfg identity.Configuration, selected operations.Selection) (operationExecutor, ownedReadFileManager, error) {
				executor, manager, err := newOwnedOperations(cfg, selected)
				if err != nil {
					return executor, manager, err
				}
				barrier.manager = manager
				return executor, barrier, nil
			}
			socket := &ownedReadFileHeldReader{sessionSocket: clientSocket(), started: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{})}
			ownedReadFileDevice(t, c, configuration, socket)
			run := startOwnedReadFileRun(t, c, func() { socket.unblock(); barrier.release() })
			if nextMessage(t, socket.sessionSocket).Type() != p.HelloType {
				t.Fatal("directory session did not enter wire")
			}
			await(t, socket.started)
			c.Stop()
			await(t, socket.entered)
			run.requireHeld(t, path)
			select {
			case <-barrier.stopped:
				t.Fatal("Manager stopped before original session reader returned")
			default:
			}
			socket.unblock()
			await(t, barrier.stopped)
			cleanup := receiveOwnedReadFile(t, barrier.entered)
			if cleanup == nil || cleanup.Err() != nil || cleanup.Done() != nil {
				t.Fatal("directory cleanup borrowed a cancelled context")
			}
			barrier.releasePhysical()
			if err = receiveOwnedReadFile(t, barrier.physical); err != nil {
				t.Fatal(err)
			}
			// Hold the composition return after REAL Manager.Drain, not kernel IO.
			run.requireHeld(t, path)
			barrier.releaseReturn()
			err = run.join(t)
			if closeError && err != workspace.ErrUnavailable || !closeError && err != context.Canceled {
				t.Fatal("directory cleanup outcome changed", err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}
