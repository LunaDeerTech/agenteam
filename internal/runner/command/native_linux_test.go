//go:build linux

package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const helperMarker = "command-owner-test-helper"

func commandTestRequest(t *testing.T, mode string, extra ...string) Request {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Request{Program: program, Args: append([]string{"-test.run=^TestCommandProcessHelper$", "--", helperMarker, mode}, extra...)}
}

func commandTestOwner(t *testing.T, directory string) *Owner {
	t.Helper()
	file, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := New(file)
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	t.Cleanup(func() {
		owner.Stop()
		if err := owner.Drain(context.Background()); err != nil {
			t.Error(err)
		}
		if !owner.Joined() {
			t.Error("owner did not actually join")
		}
	})
	return owner
}

func TestCommandDirectExecKeepsArgvCwdAndMinimalEnvironment(t *testing.T) {
	base := t.TempDir()
	original := filepath.Join(base, "before")
	renamed := filepath.Join(base, "after")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "marker"), []byte("pinned-cwd"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := commandTestOwner(t, original)
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNNER_COMMAND_SECRET_CANARY", "never-inherit-this-value")
	args := []string{"literal ; $(not-shell)", "two words", ""}
	var stdout, stderr bytes.Buffer
	result, err := owner.Run(context.Background(), commandTestRequest(t, "inspect", args...), func(_ context.Context, stream Stream, chunk []byte) error {
		if stream == Stdout {
			_, _ = stdout.Write(chunk)
		} else {
			_, _ = stderr.Write(chunk)
		}
		return nil
	})
	if err != nil || !result.Started || !result.Waited || result.ExitCode != 0 || !result.OutputComplete || !result.ProcessGroupTerminationConfirmed {
		t.Fatalf("actual command result: %+v, %v", result, err)
	}
	var got struct {
		Args   []string
		Marker string
		Env    []string
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal("invalid helper response")
	}
	wantEnv := []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	slices.Sort(wantEnv)
	slices.Sort(got.Env)
	if !slices.Equal(got.Args, args) || got.Marker != "pinned-cwd" || !slices.Equal(got.Env, wantEnv) || stderr.String() != "separate-stderr" {
		t.Fatal("argv/cwd/environment/stream boundary mismatch")
	}
	if result.StdoutBytes != int64(stdout.Len()) || result.StderrBytes != int64(stderr.Len()) {
		t.Fatal("delivered byte counts mismatch")
	}
}

func TestCommandOutputBoundsAndActualNonzeroExit(t *testing.T) {
	owner := commandTestOwner(t, t.TempDir())
	var counts [2]int
	var inCallback atomic.Int32
	result, err := owner.Run(context.Background(), commandTestRequest(t, "output"), func(_ context.Context, stream Stream, chunk []byte) error {
		if inCallback.Add(1) != 1 {
			t.Error("concurrent Sink calls")
		}
		defer inCallback.Add(-1)
		if len(chunk) == 0 || len(chunk) > MaxChunkBytes {
			t.Error("chunk outside bound")
		}
		index := 0
		value := byte('o')
		if stream == Stderr {
			index = 1
			value = 'e'
		}
		if !bytes.Equal(chunk, bytes.Repeat([]byte{value}, len(chunk))) {
			t.Error("stream contents changed")
		}
		counts[index] += len(chunk)
		return nil
	})
	if err != nil || !result.Started || !result.Waited || result.ExitCode != 23 || result.Signal != 0 || !result.OutputComplete || !result.StdoutTruncated || !result.StderrTruncated || counts != [2]int{MaxStreamBytes, MaxStreamBytes} {
		t.Fatalf("actual output/exit mismatch: %+v, %v", result, err)
	}
}

func TestCommandStartFailureAndSinkErrorKeepSafeFacts(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		owner := commandTestOwner(t, t.TempDir())
		result, err := owner.Run(context.Background(), Request{Program: filepath.Join(t.TempDir(), "secret-not-executable")}, nil)
		if !errors.Is(err, ErrStart) || strings.Contains(err.Error(), "secret") || result.Started || result.Waited || result.ExitCode != -1 {
			t.Fatal("start failure invented exit facts")
		}
	})
	t.Run("sink", func(t *testing.T) {
		owner := commandTestOwner(t, t.TempDir())
		result, err := owner.Run(context.Background(), commandTestRequest(t, "output"), func(context.Context, Stream, []byte) error { return errors.New("secret-output-detail") })
		if !errors.Is(err, ErrIO) || strings.Contains(err.Error(), "secret") || !result.Started || !result.Waited || result.OutputComplete {
			t.Fatal("sink failure did not join safely")
		}
	})
}

func TestCommandCancellationWaitsForActualChildAndGroup(t *testing.T) {
	for _, mode := range []string{"group", "ignore-term", "moved-group"} {
		t.Run(mode, func(t *testing.T) {
			owner := commandTestOwner(t, t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var output bytes.Buffer
			ready := make(chan struct{}, 1)
			done := make(chan struct{})
			var result Result
			var runErr error
			go func() {
				defer close(done)
				result, runErr = owner.Run(ctx, commandTestRequest(t, mode, strconv.Itoa(syscall.Getpgrp())), func(_ context.Context, _ Stream, b []byte) error {
					_, _ = output.Write(b)
					if bytes.Contains(output.Bytes(), []byte("ready\n")) {
						select {
						case ready <- struct{}{}:
						default:
						}
					}
					return nil
				})
			}()
			select {
			case <-ready:
			case <-time.After(5 * time.Second):
				cancel()
				<-done
				t.Fatal("original child did not become ready")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("actual child cancellation did not return")
			}
			if !errors.Is(runErr, context.Canceled) || !result.Started || !result.Waited || !result.Cancelled || !result.ProcessGroupTerminationConfirmed {
				t.Fatalf("cancel actual result mismatch: %+v, %v", result, runErr)
			}
			if mode != "group" && result.Signal != int(syscall.SIGKILL) {
				t.Fatal("TERM-resistant child not actually KILLed")
			}
			if mode == "group" && !bytes.Contains(output.Bytes(), []byte("child-waited\n")) {
				t.Fatal("group child original Wait not observed")
			}
		})
	}
}

func TestCommandHeldSinkKeepsOwnerUntilActualReturn(t *testing.T) {
	owner := commandTestOwner(t, t.TempDir())
	entered, release := make(chan struct{}), make(chan struct{})
	var called atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = owner.Run(context.Background(), commandTestRequest(t, "output"), func(context.Context, Stream, []byte) error {
			if called.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Sink not called")
	}
	owner.Stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(owner.Drain(cancelled), context.Canceled) || owner.Joined() {
		close(release)
		t.Fatal("held Sink prematurely joined")
	}
	select {
	case <-done:
		close(release)
		t.Fatal("Run returned before Sink")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("original Run did not return")
	}
	if err := owner.Drain(context.Background()); err != nil || !owner.Joined() {
		t.Fatal("owner final drain failed")
	}
}

// Only spawned explicitly by the above tests; ordinary selection skips this.
func TestCommandProcessHelper(t *testing.T) {
	index := slices.Index(os.Args, helperMarker)
	if index < 0 {
		t.Skip("owned subprocess helper")
	}
	args := os.Args[index+1:]
	if len(args) == 0 {
		os.Exit(90)
	}
	switch args[0] {
	case "inspect":
		marker, err := os.ReadFile("marker")
		if err != nil {
			os.Exit(91)
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Args   []string
			Marker string
			Env    []string
		}{args[1:], string(marker), os.Environ()}); err != nil {
			os.Exit(92)
		}
		_, _ = os.Stderr.WriteString("separate-stderr")
		os.Exit(0)
	case "output":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte{'o'}, MaxStreamBytes+1000))
		_, _ = os.Stderr.Write(bytes.Repeat([]byte{'e'}, MaxStreamBytes+1000))
		os.Exit(23)
	case "group":
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		program, err := os.Executable()
		if err != nil {
			os.Exit(93)
		}
		child := exec.Command(program, "-test.run=^TestCommandProcessHelper$", "--", helperMarker, "sleep")
		child.Env = os.Environ()
		if err := child.Start(); err != nil {
			os.Exit(94)
		}
		_, _ = fmt.Fprintln(os.Stdout, strconv.Itoa(child.Process.Pid))
		_, _ = os.Stdout.WriteString("ready\n")
		<-sig
		if err := child.Wait(); err == nil {
			os.Exit(95)
		}
		_, _ = os.Stdout.WriteString("child-waited\n")
		os.Exit(0)
	case "ignore-term", "moved-group":
		if args[0] == "moved-group" {
			if len(args) != 2 {
				os.Exit(97)
			}
			group, err := strconv.Atoi(args[1])
			if err != nil || syscall.Setpgid(0, group) != nil {
				os.Exit(98)
			}
		}
		signal.Ignore(syscall.SIGTERM)
		_, _ = os.Stdout.WriteString("ready\n")
		for {
			time.Sleep(time.Hour)
		}
	case "sleep":
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(96)
	}
}
