//go:build linux

package control

import (
	"bytes"
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

func TestOwnedWriteFileStoredIdentity(t *testing.T) {
	for _, selection := range []string{"", "write-file", "write-file,read-file,list-directory,find-files", "list-directory"} {
		t.Run(selection, func(t *testing.T) {
			selected, err := operations.Parse(selection)
			if err != nil {
				t.Fatal(err)
			}
			path, configuration := ownedReadFileIdentity(t)
			c, err := New(Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: selection})
			if err != nil {
				t.Fatal(err)
			}
			var factories atomic.Int32
			c.ownOperations = func(got identity.Configuration, gotSelection operations.Selection) (operationExecutor, ownedReadFileManager, error) {
				factories.Add(1)
				if got != configuration || !slices.Equal(gotSelection.Names(), selected.Names()) {
					t.Error("write configuration escaped the original locked identity")
				}
				other, lockErr := identity.Open(path)
				if other != nil {
					_ = other.Close()
				}
				if lockErr != identity.ErrLocked {
					t.Error("write factory ran outside the original identity lock")
				}
				return newOwnedOperations(got, gotSelection)
			}
			socket := clientSocket()
			ownedReadFileDevice(t, c, configuration, socket)
			run := startOwnedReadFileRun(t, c, nil)
			hello, ok := nextMessage(t, socket).Payload().(p.Hello)
			wantFactories := int32(0)
			if selection != "" {
				wantFactories = 1
			}
			if !ok || factories.Load() != wantFactories || !slices.Equal(hello.Capabilities, selected.Names()) || len(hello.FeatureFlags) != 0 {
				t.Fatal("selected capabilities were not built once before Hello")
			}
			socket.in <- helloAck(t)
			read := ownedReadFileRequest(t, configuration.RootPath)
			write := read
			write.RequestID, write.OperationID = sessionID(t), sessionID(t)
			write.OperationName, write.OperationRevision = p.WriteFileOperation, p.WriteFileRevision
			data := []byte{0, 0xff, '\n', 'A'}
			write.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "written", Data: data}})
			if err != nil {
				t.Fatal(err)
			}
			socket.in <- correlated(t, write, write.RequestID, write.OperationID)
			response, ok := nextMessage(t, socket).Payload().(p.Response)
			if !ok || response.RequestID != write.RequestID || response.OperationID != write.OperationID {
				t.Fatal("write terminal lost original identity")
			}
			file := filepath.Join(configuration.RootPath, string(write.ProjectID), string(write.AgentID), "source", "written")
			if selected.Contains(p.WriteFileOperation) {
				result, err := p.DecodeWriteFileResult(response.Payload)
				if response.Outcome != p.Success || response.Code != nil || response.SafeMessage != nil || err != nil || result.Path != "written" || result.State != "committed" || result.BytesWritten != len(data) || !result.Durable || result.CompletionCode != nil {
					t.Fatal("configured Handler changed the original publication result", err)
				}
				actual, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(actual, data) {
					t.Fatal("configured write did not publish all bytes", err)
				}
				// This second write is explicit. The original no-overwrite result
				// must remain a conflict, without replaying or reading it as success.
				write.RequestID, write.OperationID = sessionID(t), sessionID(t)
				socket.in <- correlated(t, write, write.RequestID, write.OperationID)
				response, ok = nextMessage(t, socket).Payload().(p.Response)
				if !ok || response.RequestID != write.RequestID || response.OperationID != write.OperationID || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.Conflict || response.Payload != nil {
					t.Fatal("configured write lost no-overwrite conflict")
				}
				actual, err = os.ReadFile(file)
				if err != nil || !bytes.Equal(actual, data) {
					t.Fatal("conflicting write changed published bytes", err)
				}
			} else {
				if response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation || response.Payload != nil {
					t.Fatal("unselected write was admitted")
				}
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatal("unselected write touched its target", err)
				}
			}
			socket.in <- correlated(t, read, read.RequestID, read.OperationID)
			response, ok = nextMessage(t, socket).Payload().(p.Response)
			if !ok || response.RequestID != read.RequestID || response.OperationID != read.OperationID {
				t.Fatal("read terminal lost original identity")
			}
			if selected.Contains(p.ReadFileOperation) {
				value, err := p.DecodeReadFileResult(response.Payload)
				if response.Outcome != p.Success || err != nil || value.Data != "ell" || value.BytesRead != 3 || value.EOF {
					t.Fatal("combined read/write owner changed read result", err)
				}
			} else if response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation {
				t.Fatal("write selection enabled an undeclared read")
			}
			c.Stop()
			if err = run.join(t); err != context.Canceled {
				t.Fatal(err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}

func TestOwnedWriteFileRejectsBeforeDevice(t *testing.T) {
	for _, mode := range []string{"bad-root", "configuration-conflict", "external-executor"} {
		t.Run(mode, func(t *testing.T) {
			path, configuration := ownedReadFileIdentity(t)
			options := Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: p.WriteFileOperation}
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
				handler, _ := writeFileFixture(t)
				c, err = NewWriteFile(options, handler)
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
				t.Fatal("invalid write owner reached the device or external identity", err)
			}
			if err = c.Drain(context.Background()); err != nil {
				t.Fatal(err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}

func TestOwnedWriteFileOriginalReturnKeepsIdentity(t *testing.T) {
	path, configuration := ownedReadFileIdentity(t)
	c, err := New(Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: "read-file,write-file"})
	if err != nil {
		t.Fatal(err)
	}
	barrier := newOwnedReadFileDrainBarrier()
	barrier.fault = workspace.ErrUnavailable
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
		t.Fatal("write session did not enter wire")
	}
	await(t, socket.started)
	c.Stop()
	await(t, socket.entered)
	run.requireHeld(t, path)
	select {
	case <-barrier.stopped:
		t.Fatal("write Manager stopped before original session reader returned")
	default:
	}
	socket.unblock()
	await(t, barrier.stopped)
	cleanup := receiveOwnedReadFile(t, barrier.entered)
	if cleanup == nil || cleanup.Err() != nil || cleanup.Done() != nil {
		t.Fatal("write cleanup borrowed a cancelled context")
	}
	barrier.releasePhysical()
	if err = receiveOwnedReadFile(t, barrier.physical); err != nil {
		t.Fatal(err)
	}
	// Hold the original composition return after the real Manager has drained.
	run.requireHeld(t, path)
	barrier.releaseReturn()
	if err = run.join(t); err != workspace.ErrUnavailable {
		t.Fatal("write cleanup error was hidden by cancellation", err)
	}
	requireOwnedReadFileIdentityReleased(t, path)
}
