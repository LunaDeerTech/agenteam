//go:build linux

package control

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/runcommand"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestRunCommandExplicitClientKeepsDefaultAndEnvironmentClosed(t *testing.T) {
	root, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := workspace.New(root)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal("real workspace constructor failed")
	}
	t.Cleanup(func() {
		if err := manager.Drain(context.Background()); err != nil {
			t.Error("manager did not drain", err)
		}
	})
	handler, err := runcommand.New(manager)
	if err != nil {
		t.Fatal(err)
	}
	options := Options{IdentityFile: "/PRIVATE_COMMAND_IDENTITY/identity.json", RunnerVersion: "fixture-1"}
	plain, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := NewRunCommand(options, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		plain.Stop()
		bound.Stop()
		if plain.Drain(context.Background()) != nil || bound.Drain(context.Background()) != nil {
			t.Error("offline clients did not join")
		}
	})
	id := sessionID(t)
	if plain.executor != nil || len(plain.connectionHello(id, "fixture-1").Capabilities) != 0 {
		t.Fatal("default constructor acquired command capability")
	}
	hello := bound.connectionHello(id, "fixture-1")
	if len(hello.Capabilities) != 1 || hello.Capabilities[0] != p.RunCommandOperation || len(hello.FeatureFlags) != 0 {
		t.Fatal("explicit capability changed")
	}
	if _, ok := bound.executor.(commandTailAdmission); !ok {
		t.Fatal("bound command lacks private admission policy")
	}
	if !bound.executor.maskingReady() {
		t.Fatal("closed empty injection set cannot stream")
	}
	if data, err := bound.executor.mask("legitimate � text"); err != nil || data != "legitimate � text" {
		t.Fatal("empty-set masker altered text")
	}
	if _, err := bound.executor.mask(string([]byte{0xff})); err == nil {
		t.Fatal("masking gate accepted invalid UTF-8")
	}
	for _, h := range []*runcommand.Handler{nil, {}} {
		if _, err := NewRunCommand(options, h); err == nil {
			t.Fatal("unbound Handler accepted")
		}
	}
	for _, environment := range []map[string]string{{}, {"SECRET": "PRIVATE"}} {
		r := request(t)
		r.OperationName = p.RunCommandOperation
		r.OperationRevision = p.RunCommandRevision
		r.Environment = environment
		r.Payload, err = p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "source", Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/echo", Args: []string{}, CWD: "."}})
		if err != nil {
			t.Fatal(err)
		}
		response, err := bound.executor.execute(context.Background(), r, func(string, string) error { return errors.New("rejected request emitted") })
		if err != nil || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.InvalidRequest || response.Payload != nil {
			t.Fatal("environment injection reached command")
		}
	}
}

func commandGateSession(executor operationExecutor) *session {
	socket := &sessionSocket{in: make(chan p.Message, 1), out: make(chan p.Message, 1), closed: make(chan struct{})}
	return &session{executor: executor, wire: wireWithSocket(socket)}
}

func TestRunCommandCancellationTailKeepsOriginalWireGates(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		command, terminal, failed, closed bool
		sentinel                          bool
	}{
		{"bound-cancelled", true, false, false, false, true},
		{"same-name-unbound-policy", false, false, false, false, false},
		{"already-terminal", true, true, false, false, false},
		{"already-failed", true, false, true, false, false},
		{"wire-stopped", true, false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var executor operationExecutor = testExecutor{ready: true}
			if tc.command {
				executor = runCommandExecutor{}
			}
			s := commandGateSession(executor)
			defer s.wire.stop()
			if tc.closed {
				s.wire.stop()
			}
			active := &activeRequest{terminal: tc.terminal, streamFailed: tc.failed, sequences: map[string]uint64{"stdout": 3}}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := request(t)
			r.OperationName = p.RunCommandOperation
			r.OperationRevision = p.RunCommandRevision
			err := s.stream(ctx, active, r, "stdout", "cancelled tail")
			if (err == errCommandTailCancelled) != tc.sentinel {
				t.Fatal("cancel exception escaped its private bound policy")
			}
			if active.sequences["stdout"] != 3 || len(s.wire.queue.frames) != 0 {
				t.Fatal("cancelled tail changed sequence or queue")
			}
			if tc.sentinel {
				if active.streamFailed {
					t.Fatal("cancel tail fabricated a send failure")
				}
				select {
				case <-s.wire.stopCh:
					t.Fatal("cancel tail closed healthy connection")
				default:
				}
			} else {
				if err == nil || !active.streamFailed {
					t.Fatal("old failure gate was weakened")
				}
				select {
				case <-s.wire.stopCh:
				default:
					t.Fatal("old failure did not stop wire")
				}
			}
		})
	}
}

// An observing wrapper must deliberately retain the private bound policy. The
// operation DTO alone cannot acquire the cancellation exception.
type commandMaskObserver struct {
	runCommandExecutor
	beforeMask func()
}

func (e commandMaskObserver) mask(data string) (string, error) {
	e.beforeMask()
	return e.runCommandExecutor.mask(data)
}

func TestRunCommandStreamPreservesRealSendFailureAndFIFO(t *testing.T) {
	s := commandGateSession(runCommandExecutor{})
	defer s.wire.stop()
	r := request(t)
	r.OperationName = p.RunCommandOperation
	r.OperationRevision = p.RunCommandRevision
	active := &activeRequest{sequences: make(map[string]uint64)}
	if err := s.stream(context.Background(), active, r, "stdout", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.stream(context.Background(), active, r, "stderr", "second"); err != nil {
		t.Fatal(err)
	}
	if active.sequences["stdout"] != 1 || active.sequences["stderr"] != 1 || len(s.wire.queue.frames) != 2 {
		t.Fatal("stream sequence or queue order changed")
	}
	for i, want := range []string{"first", "second"} {
		m, err := p.Decode(s.wire.queue.frames[i].wire)
		if err != nil {
			t.Fatal(err)
		}
		v, ok := m.Payload().(p.Stream)
		if !ok || v.Data != want || v.RequestID != r.RequestID || v.OperationID != r.OperationID {
			t.Fatal("queued stream changed identity or bytes")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.executor = commandMaskObserver{beforeMask: func() { cancel(); s.wire.stop() }}
	if err := s.stream(ctx, active, r, "stdout", "failed"); err == nil || err == errCommandTailCancelled {
		t.Fatal("actual queue error was converted to a cancel sentinel")
	}
	if !active.streamFailed || active.sequences["stdout"] != 1 {
		t.Fatal("actual send failure was swallowed")
	}
}
