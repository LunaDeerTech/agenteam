//go:build linux

package filesystem

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
)

func TestFilesystemCommandCWDChild(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "filesystem-command-cwd-child" {
		t.Skip("subprocess-only helper")
	}
	data, err := os.ReadFile("command-marker")
	if err != nil {
		os.Exit(71)
	}
	if _, err = os.Stdout.Write(data); err != nil {
		os.Exit(72)
	}
	os.Exit(0)
}

func filesystemCommandRequest(t *testing.T) command.Request {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal("current test executable unavailable")
	}
	return command.Request{Program: program, Args: []string{"-test.run=^TestFilesystemCommandCWDChild$", "--", "filesystem-command-cwd-child"}}
}

func TestFilesystemCommandUsesContainedPinnedCWD(t *testing.T) {
	path, borrowed, root := fixture(t)
	if err := os.Mkdir(filepath.Join(path, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "sub", "command-marker"), []byte("original pinned directory"))
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	// Rename the pathname after Root has pinned it. Replacing the old name must
	// not redirect command cwd; neither Metadata nor a reconstructed path is used.
	moved := path + "-moved"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.MkdirAll(filepath.Join(path, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "sub", "command-marker"), []byte("wrong replacement"))
	var output bytes.Buffer
	got, err := root.RunCommand(context.Background(), "sub", filesystemCommandRequest(t), func(_ context.Context, stream command.Stream, data []byte) error {
		if stream != command.Stdout {
			return errors.New("unexpected stderr")
		}
		_, err := output.Write(data)
		return err
	})
	if err != nil || !got.Started || !got.Waited || got.ExitCode != 0 || output.String() != "original pinned directory" {
		t.Fatal("command did not use and join original contained cwd", err)
	}
}

func TestFilesystemCommandRejectsInvalidAndEscapingCWD(t *testing.T) {
	path, _, root := fixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(path, "outside")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "regular"), []byte("file"))
	for _, cwd := range []string{"", "/", "../outside", "sub/../other", "outside", "regular"} {
		t.Run(cwd, func(t *testing.T) {
			got, err := root.RunCommand(context.Background(), cwd, filesystemCommandRequest(t), func(context.Context, command.Stream, []byte) error {
				t.Error("rejected cwd reached stream callback")
				return nil
			})
			if err == nil || got.Started || got.Waited {
				t.Fatal("invalid or escaping cwd started a process")
			}
		})
	}
}

type controlledCommandNative struct {
	nativeRoot
	run func(context.Context, string, command.Request, command.Sink) (command.Result, error)
}

func (n controlledCommandNative) runCommand(ctx context.Context, cwd string, q command.Request, s command.Sink) (command.Result, error) {
	return n.run(ctx, cwd, q, s)
}

type controlledCommandOwner struct {
	run   func(context.Context, command.Request, command.Sink) (command.Result, error)
	stop  func()
	drain func(context.Context) error
}

func (o *controlledCommandOwner) Run(ctx context.Context, q command.Request, s command.Sink) (command.Result, error) {
	return o.run(ctx, q, s)
}
func (o *controlledCommandOwner) Stop()                           { o.stop() }
func (o *controlledCommandOwner) Drain(ctx context.Context) error { return o.drain(ctx) }

func TestFilesystemCommandRetainsAdmissionThroughOwnerAndLeaseClose(t *testing.T) {
	for _, phase := range []string{"owner-drain", "directory-close"} {
		t.Run(phase, func(t *testing.T) {
			_, _, root := fixture(t)
			actual := root.fs.(*linuxRoot)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unpark := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unpark)
			var stopped, drained, closed atomic.Int32
			calls := commandCalls{
				newOwner: func(file *os.File) (commandOwner, error) {
					if _, err := file.Stat(); err != nil {
						return nil, err
					}
					return &controlledCommandOwner{
						run: func(context.Context, command.Request, command.Sink) (command.Result, error) {
							return command.Result{Started: true, Waited: true, ExitCode: 7}, nil
						},
						stop: func() { stopped.Add(1) },
						drain: func(ctx context.Context) error {
							if ctx.Err() != nil {
								return errors.New("cleanup reused cancelled context")
							}
							if phase == "owner-drain" {
								close(entered)
								<-release
							}
							drained.Add(1)
							return nil
						},
					}, nil
				},
				closeDirectory: func(file *os.File) error {
					if drained.Load() != 1 {
						return errors.New("cwd closed before owner joined")
					}
					if phase == "directory-close" {
						close(entered)
						<-release
					}
					closed.Add(1)
					return file.Close()
				},
			}
			root.fs = controlledCommandNative{actual, func(ctx context.Context, cwd string, q command.Request, s command.Sink) (command.Result, error) {
				return actual.runCommandWith(ctx, cwd, q, s, calls)
			}}
			type response struct {
				result command.Result
				err    error
			}
			done := make(chan response, 1)
			q := filesystemCommandRequest(t)
			go func() {
				result, err := root.RunCommand(context.Background(), ".", q, nil)
				done <- response{result, err}
			}()
			waitSignal(t, entered)
			root.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			err := root.Drain(ctx)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("Drain did not wait original cleanup", err)
			}
			root.mu.Lock()
			active, isClosed := len(root.active), root.closed
			root.mu.Unlock()
			if active != 1 || isClosed || closed.Load() != 0 {
				t.Fatal("cwd lease retired early")
			}
			unpark()
			select {
			case out := <-done:
				if !errors.Is(out.err, context.Canceled) || !out.result.Started || !out.result.Waited || out.result.ExitCode != 7 {
					t.Fatal("actual result lost during Stop", out.err)
				}
			case <-time.After(time.Second):
				t.Fatal("original command did not return")
			}
			if err := root.Drain(context.Background()); err != nil {
				t.Fatal(err)
			}
			if stopped.Load() != 1 || drained.Load() != 1 || closed.Load() != 1 {
				t.Fatal("cleanup was missing or duplicated")
			}
		})
	}
}

func TestFilesystemCommandConstructorFailureJoinsBeforeLeaseClose(t *testing.T) {
	_, _, root := fixture(t)
	actual := root.fs.(*linuxRoot)
	var order []string
	var borrowed *os.File
	owner := &controlledCommandOwner{
		run: func(context.Context, command.Request, command.Sink) (command.Result, error) {
			t.Error("failed construction started command")
			return command.Result{}, nil
		},
		stop:  func() { order = append(order, "stop") },
		drain: func(context.Context) error { order = append(order, "drain"); return nil },
	}
	_, err := actual.runCommandWith(context.Background(), ".", filesystemCommandRequest(t), nil, commandCalls{
		newOwner:       func(file *os.File) (commandOwner, error) { borrowed = file; return owner, command.ErrUnavailable },
		closeDirectory: func(file *os.File) error { order = append(order, "close"); return file.Close() },
	})
	if !errors.Is(err, command.ErrUnavailable) || len(order) != 3 || order[0] != "stop" || order[1] != "drain" || order[2] != "close" {
		t.Fatal("failed constructor detached ownership", err)
	}
	if _, err := borrowed.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("original cwd lease still open")
	}
}
