//go:build linux

package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
)

func TestWorkspaceCommandChild(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "workspace-command-child" {
		t.Skip("subprocess-only helper")
	}
	data, err := os.ReadFile("marker")
	if err != nil {
		os.Exit(71)
	}
	if _, err = os.Stdout.Write(data); err != nil {
		os.Exit(72)
	}
	os.Exit(0)
}
func workspaceCommandRequest(t *testing.T) command.Request {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return command.Request{Program: program, Args: []string{"-test.run=^TestWorkspaceCommandChild$", "--", "workspace-command-child"}}
}

func TestHandleCommandUsesActualPinnedWorkspace(t *testing.T) {
	base, borrowed, m := fixture(t)
	h := ensure(t, m, identity())
	if err := os.Mkdir(filepath.Join(physical(base, identity()), "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Write(context.Background(), filesystem.WriteRequest{Path: "sub/marker", Data: []byte("managed cwd")}); err != nil {
		t.Fatal(err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	result, err := h.RunCommand(context.Background(), "sub", workspaceCommandRequest(t), func(_ context.Context, stream command.Stream, data []byte) error {
		if stream != command.Stdout {
			return errors.New("unexpected stderr")
		}
		_, err := output.Write(data)
		return err
	})
	if err != nil || !result.Started || !result.Waited || result.ExitCode != 0 || output.String() != "managed cwd" {
		t.Fatal("managed command lost actual workspace/exit", err)
	}
	h.Stop()
	if result, err := h.RunCommand(context.Background(), ".", workspaceCommandRequest(t), nil); !errors.Is(err, ErrClosed) || result.Started {
		t.Fatal("stopped handle admitted command")
	}
}

type heldCommandRoot struct {
	readerRoot
	run func(context.Context, string, command.Request, command.Sink) (command.Result, error)
}

func (r heldCommandRoot) RunCommand(ctx context.Context, cwd string, q command.Request, s command.Sink) (command.Result, error) {
	return r.run(ctx, cwd, q, s)
}

func TestHandleCommandStopJoinsOriginalReturnBeforeRetiring(t *testing.T) {
	_, _, m := fixture(t)
	h := ensure(t, m, identity())
	actual := h.root.(*filesystem.Root)
	if _, err := h.Write(context.Background(), filesystem.WriteRequest{Path: "marker", Data: []byte("done")}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unpark)
	h.root = heldCommandRoot{actual, func(ctx context.Context, cwd string, q command.Request, s command.Sink) (command.Result, error) {
		result, err := actual.RunCommand(ctx, cwd, q, s)
		close(entered)
		<-release
		return result, err
	}}
	done := make(chan error, 1)
	request := workspaceCommandRequest(t)
	go func() {
		result, err := h.RunCommand(context.Background(), ".", request, func(context.Context, command.Stream, []byte) error { return nil })
		if !result.Started || !result.Waited || result.ExitCode != 0 {
			err = errors.New("actual command exit erased")
		}
		done <- err
	}()
	await(t, entered)
	m.Stop()
	deadline(t, m.Drain)
	m.mu.Lock()
	active, registered := len(m.active), len(m.handles)
	m.mu.Unlock()
	if active != 1 || registered != 1 {
		t.Fatal("Manager forgot command before original return")
	}
	unpark()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("original command return", err)
	}
	if err := m.Drain(context.Background()); err != nil {
		t.Fatal("manager actual close", err)
	}
}
