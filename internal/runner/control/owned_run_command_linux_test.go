//go:build linux

package control

import (
	"context"
	"crypto/x509"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/operations"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func ownedCommandRequest(t *testing.T) p.Request {
	t.Helper()
	r := request(t)
	r.OperationName, r.OperationRevision = p.RunCommandOperation, p.RunCommandRevision
	var err error
	r.Payload, err = p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "source", Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/echo", Args: []string{"literal $(ignored); 雪"}, CWD: "."}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOwnedRunCommandStoredIdentityAndSelection(t *testing.T) {
	for _, selection := range []string{"", "read-file,write-file", "run-command", "run-command,write-file,find-files,list-directory,read-file"} {
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
			makeOwned := func(got identity.Configuration, gotSelection operations.Selection) (operationExecutor, ownedReadFileManager, error) {
				factories.Add(1)
				if got != configuration || !slices.Equal(gotSelection.Names(), selected.Names()) {
					t.Error("command resources did not use the original locked configuration")
				}
				requireOwnedReadFileIdentityLocked(t, path)
				executor, manager, err := newOwnedOperations(got, gotSelection)
				if err == nil {
					_, policy := executor.(commandTailAdmission)
					if policy != selected.Contains(p.RunCommandOperation) || executor.maskingReady() != policy {
						t.Error("command stream policy escaped its explicit selection")
					}
				}
				return executor, manager, err
			}
			c.ownOperations = makeOwned
			socket := clientSocket()
			ownedReadFileDevice(t, c, configuration, socket)
			run := startOwnedReadFileRun(t, c, nil)
			hello, ok := nextMessage(t, socket).Payload().(p.Hello)
			wantFactories := int32(0)
			if selection != "" {
				wantFactories = 1
			}
			if !ok || factories.Load() != wantFactories || !slices.Equal(hello.Capabilities, selected.Names()) || len(hello.FeatureFlags) != 0 {
				t.Fatal("Hello did not advertise precisely the successfully constructed handlers")
			}
			socket.in <- helloAck(t)
			r := ownedCommandRequest(t)
			socket.in <- correlated(t, r, r.RequestID, r.OperationID)
			var output strings.Builder
			var response p.Response
			for {
				payload := nextMessage(t, socket).Payload()
				if stream, ok := payload.(p.Stream); ok {
					if stream.RequestID != r.RequestID || stream.OperationID != r.OperationID || stream.Stream != "stdout" {
						t.Fatal("configured command stream lost its original correlation")
					}
					output.WriteString(stream.Data)
					continue
				}
				response, ok = payload.(p.Response)
				if !ok || response.RequestID != r.RequestID || response.OperationID != r.OperationID {
					t.Fatal("configured command terminal lost its original correlation")
				}
				break
			}
			if selected.Contains(p.RunCommandOperation) {
				result, err := p.DecodeRunCommandResult(response.Payload)
				if response.Outcome != p.Success || err != nil || result.ExitCode != 0 || result.Signal != 0 || result.Cancelled || !result.OutputComplete || result.CompletionCode != nil || output.String() != "literal $(ignored); 雪\n" {
					t.Fatal("selected command did not preserve direct argv and complete output", err)
				}
			} else if response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation || output.Len() != 0 {
				t.Fatal("unselected command executed or streamed")
			}
			read := ownedReadFileRequest(t, configuration.RootPath)
			socket.in <- correlated(t, read, read.RequestID, read.OperationID)
			response, ok = nextMessage(t, socket).Payload().(p.Response)
			if !ok || response.RequestID != read.RequestID || response.OperationID != read.OperationID {
				t.Fatal("combined read terminal changed identity")
			}
			if selected.Contains(p.ReadFileOperation) {
				value, err := p.DecodeReadFileResult(response.Payload)
				if response.Outcome != p.Success || err != nil || value.Data != "ell" {
					t.Fatal("command selection changed the selected read behavior", err)
				}
			} else if response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation {
				t.Fatal("command selection enabled an unselected read")
			}
			c.Stop()
			if err = run.join(t); err != context.Canceled {
				t.Fatal(err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}

func TestOwnedRunCommandPreservesProfileGatesAndCancellationTail(t *testing.T) {
	_, configuration := ownedReadFileIdentity(t)
	selected, _ := operations.Parse("read-file,run-command")
	executor, manager, err := newOwnedOperations(configuration, selected)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		manager.Stop()
		if err := manager.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if data, err := executor.mask("雪 �"); err != nil || data != "雪 �" {
		t.Fatal("empty-injection mask changed valid text")
	}
	if _, err := executor.mask(string([]byte{0xff})); err == nil {
		t.Fatal("command mask accepted invalid UTF-8")
	}
	for _, mode := range []string{"empty-environment", "environment", "idempotency", "revision"} {
		t.Run(mode, func(t *testing.T) {
			r := ownedCommandRequest(t)
			want := p.InvalidRequest
			switch mode {
			case "empty-environment":
				r.Environment = map[string]string{}
			case "environment":
				r.Environment = map[string]string{"PRIVATE_CANARY": "not-injected"}
			case "idempotency":
				key := "not-replayable"
				r.IdempotencyKey = &key
			case "revision":
				r.OperationRevision = "2"
				want = p.UnsupportedOperation
				if executor.accepts(r) {
					t.Fatal("revision 2 became an advertised capability")
				}
			}
			response, err := executor.execute(context.Background(), r, func(string, string) error {
				t.Error("rejected command emitted output")
				return ErrProtocol
			})
			if err != nil || response.Outcome != p.Failure || response.Code == nil || *response.Code != want || response.Payload != nil {
				t.Fatal("command profile rejection changed", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	emitted := false
	response, err := executor.execute(ctx, ownedCommandRequest(t), func(string, string) error {
		emitted = true
		cancel()
		return errCommandTailCancelled
	})
	result, decodeErr := p.DecodeRunCommandResult(response.Payload)
	if !emitted || err != nil || decodeErr != nil || response.Outcome != p.Success || !result.Cancelled || result.OutputComplete || result.CompletionCode == nil || *result.CompletionCode != p.CancelledCode {
		t.Fatal("original command cancellation sentinel became transport Unknown", err, decodeErr)
	}
}

func TestOwnedRunCommandPartialOwnerKeepsIdentityUntilOriginalReturn(t *testing.T) {
	path, _ := ownedReadFileIdentity(t)
	c, err := New(Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: "read-file,run-command"})
	if err != nil {
		t.Fatal(err)
	}
	barrier := newOwnedReadFileDrainBarrier()
	failure := errors.New("controlled command composition failure")
	c.ownOperations = func(cfg identity.Configuration, selected operations.Selection) (operationExecutor, ownedReadFileManager, error) {
		_, manager, err := newOwnedOperations(cfg, selected)
		if err != nil {
			return nil, manager, err
		}
		barrier.manager = manager
		return nil, barrier, failure
	}
	var devices atomic.Int32
	c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) {
		devices.Add(1)
		return nil, ErrTransport
	}
	run := startOwnedReadFileRun(t, c, barrier.release)
	await(t, barrier.stopped)
	cleanup := receiveOwnedReadFile(t, barrier.entered)
	if cleanup == nil || cleanup.Err() != nil || cleanup.Done() != nil || devices.Load() != 0 {
		t.Fatal("partial command construction reached a device or skipped original cleanup")
	}
	run.requireHeld(t, path)
	barrier.releasePhysical()
	if err := receiveOwnedReadFile(t, barrier.physical); err != nil {
		t.Fatal(err)
	}
	run.requireHeld(t, path)
	barrier.releaseReturn()
	if err = run.join(t); err != failure {
		t.Fatal("partial command construction outcome changed", err)
	}
	requireOwnedReadFileIdentityReleased(t, path)
}
