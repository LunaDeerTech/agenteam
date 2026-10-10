//go:build linux && amd64

package process_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// This test uses the unmodified default cmd built by TestMain. No enrollment
// material, server, socket, injected signal channel, or production hook is used.
// The source owner must freeze this file during an existing process-test window.
func TestRunnerControlDefaultRunnerSignals(t *testing.T) {
	for _, mode := range []string{"eof", "second_signal", "deadline"} {
		if !t.Run(mode, func(t *testing.T) { runnerSignalCase(t, mode) }) {
			return // Preserve the first failure; never continue with another child.
		}
	}
}

type runnerSignalRecord struct{ Event, Phase, Outcome, Code string }
type runnerSignalOutput struct {
	mu      sync.Mutex
	bytes   int
	bad     bool
	pending []byte
	records []runnerSignalRecord
	stdout  bool
}

func (v *runnerSignalOutput) Write(p []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.bytes += len(p)
	if v.stdout || v.bytes > 65536 {
		v.bad = true
		return len(p), nil
	}
	v.pending = append(v.pending, p...)
	for {
		index := bytes.IndexByte(v.pending, '\n')
		if index < 0 {
			break
		}
		var raw map[string]any
		if json.Unmarshal(v.pending[:index], &raw) != nil || raw == nil || len(v.records) >= 128 {
			v.bad = true
			v.pending = nil
			break
		}
		for _, key := range []string{"connected", "authenticated", "ready"} {
			if raw[key] == true {
				v.bad = true
			}
		}
		closed := func(key string, values ...string) string {
			value, _ := raw[key].(string)
			for _, allowed := range values {
				if value == allowed {
					return value
				}
			}
			return ""
		}
		v.records = append(v.records, runnerSignalRecord{
			closed("event", "lifecycle", "shutdown"), closed("phase", "starting", "stopping"),
			closed("outcome", "drained", "forced"), closed("code", "SHUTDOWN_TIMEOUT", "FORCED_SHUTDOWN"),
		})
		v.pending = append(v.pending[:0], v.pending[index+1:]...)
	}
	return len(p), nil
}
func (v *runnerSignalOutput) state(final bool) ([]runnerSignalRecord, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]runnerSignalRecord(nil), v.records...), v.bad || final && len(v.pending) != 0
}

type runnerSignalIdentity struct{ dev, inode uint64 }
type runnerSignalChild struct {
	command      *exec.Cmd
	writer       *os.File
	done         chan struct{}
	err          error
	stdout, logs *runnerSignalOutput
	pid          int
	start        uint64
	pipe         uint64
	binary       runnerSignalIdentity
	lock         runnerSignalIdentity
	lockKnown    bool
	directory    string
}

func runnerSignalStat(info os.FileInfo) (syscall.Stat_t, error) {
	value, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return syscall.Stat_t{}, errors.New("stat_identity_unavailable")
	}
	return *value, nil
}
func runnerSignalStart(raw []byte) (uint64, string, error) {
	end := bytes.LastIndexByte(raw, ')')
	if end < 1 {
		return 0, "", errors.New("proc_stat_shape")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return 0, "", errors.New("proc_stat_shape")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || ticks == 0 {
		return 0, "", errors.New("proc_start_invalid")
	}
	return ticks, fields[0], nil
}
func (v *runnerSignalChild) exited() bool {
	select {
	case <-v.done:
		return true
	default:
		return false
	}
}
func (v *runnerSignalChild) live() error {
	if v.exited() {
		return errors.New("child_exited_before_witness")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", v.pid))
	if err != nil {
		return errors.New("proc_identity_unavailable")
	}
	ticks, state, err := runnerSignalStart(raw)
	if err != nil || ticks != v.start || state == "Z" || state == "X" {
		return errors.New("child_identity_changed")
	}
	info, err := os.Stat(fmt.Sprintf("/proc/%d/exe", v.pid))
	if err != nil {
		return errors.New("child_executable_unavailable")
	}
	actual, err := runnerSignalStat(info)
	if err != nil || (runnerSignalIdentity{uint64(actual.Dev), actual.Ino}) != v.binary {
		return errors.New("child_executable_changed")
	}
	return nil
}
func runnerSignalRawRead(before, after, wchan string) bool {
	a, b := strings.Fields(before), strings.Fields(after)
	if len(a) < 7 || len(a) != len(b) || (wchan != "pipe_read" && wchan != "anon_pipe_read") {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	fd, fdErr := strconv.ParseUint(a[1], 0, 64)
	count, countErr := strconv.ParseUint(a[3], 0, 64)
	return a[0] == "0" && fdErr == nil && fd == 0 && countErr == nil && count > 0
}
func (v *runnerSignalChild) blocked() (bool, error) {
	if err := v.live(); err != nil {
		return false, err
	}
	base := fmt.Sprintf("/proc/%d", v.pid)
	pipe, err := os.Readlink(base + "/fd/0")
	if err != nil || pipe != fmt.Sprintf("pipe:[%d]", v.pipe) {
		return false, errors.New("stdin_pipe_changed")
	}
	fdinfo, err := os.ReadFile(base + "/fdinfo/0")
	if err != nil {
		return false, errors.New("stdin_flags_unavailable")
	}
	flagsFound := false
	for _, line := range strings.Split(string(fdinfo), "\n") {
		if strings.HasPrefix(line, "flags:") {
			flags, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "flags:")), 8, 64)
			if err != nil || flags&syscall.O_ACCMODE != syscall.O_RDONLY || flags&syscall.O_NONBLOCK != 0 {
				return false, errors.New("stdin_not_blocking_reader")
			}
			flagsFound = true
		}
	}
	if !flagsFound {
		return false, errors.New("stdin_flags_unavailable")
	}
	tasks, err := os.ReadDir(base + "/task")
	if err != nil || len(tasks) > 128 {
		return false, errors.New("task_scan_unavailable_or_limit")
	}
	for _, task := range tasks {
		path := filepath.Join(base, "task", task.Name())
		before, firstErr := os.ReadFile(path + "/syscall")
		wchan, wchanErr := os.ReadFile(path + "/wchan")
		after, lastErr := os.ReadFile(path + "/syscall")
		if errors.Is(firstErr, os.ErrNotExist) || errors.Is(wchanErr, os.ErrNotExist) || errors.Is(lastErr, os.ErrNotExist) {
			continue // The original Go thread may retire during the bounded scan.
		}
		if firstErr != nil || wchanErr != nil || lastErr != nil {
			return false, errors.New("task_observation_unavailable")
		}
		if runnerSignalRawRead(string(before), string(after), strings.TrimSpace(string(wchan))) {
			if err := v.live(); err != nil {
				return false, err
			}
			pipe, err := os.Readlink(base + "/fd/0")
			if err != nil || pipe != fmt.Sprintf("pipe:[%d]", v.pipe) {
				return false, errors.New("stdin_pipe_changed_after_snapshot")
			}
			return true, nil
		}
	}
	return false, nil
}
func (v *runnerSignalChild) until(deadline time.Time, predicate func() (bool, error)) error {
	for time.Now().Before(deadline) {
		ok, err := predicate()
		if err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return errors.New("original_phase_deadline")
		}
		if ok {
			return nil
		}
		if v.exited() {
			return errors.New("child_exited_before_phase")
		}
		delay := min(10*time.Millisecond, time.Until(deadline))
		if delay > 0 {
			time.Sleep(delay) // Poll pacing only; never used as an I/O witness.
		}
	}
	return errors.New("original_phase_deadline")
}
func (v *runnerSignalChild) signal(sig syscall.Signal) error {
	if err := v.live(); err != nil {
		return err
	}
	return v.command.Process.Signal(sig)
}
func (v *runnerSignalChild) wait(deadline time.Time) (int, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return -1, errors.New("original_wait_deadline")
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-v.done: // Cmd.Wait also joined the two actual stdio copy/EOF owners.
		if !time.Now().Before(deadline) {
			return -1, errors.New("original_wait_deadline")
		}
		if v.command.ProcessState == nil {
			return -1, errors.New("actual_wait_missing")
		}
		code := v.command.ProcessState.ExitCode()
		if v.err != nil {
			var exit *exec.ExitError
			if !errors.As(v.err, &exit) {
				return -1, errors.New("actual_wait_failed")
			}
		}
		return code, nil
	case <-timer.C:
		return -1, errors.New("original_wait_deadline")
	}
}
func (v *runnerSignalChild) writerEOF() error {
	if v.writer == nil {
		return nil
	}
	writer := v.writer
	v.writer = nil
	return writer.Close()
}
func (v *runnerSignalChild) cleanup(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	if !v.exited() {
		_ = v.command.Process.Kill() // Only the original direct child, never a guessed PID.
	}
	if v.writerEOF() != nil {
		t.Error("owned writer did not close")
	}
	if _, err := v.wait(deadline); err != nil {
		t.Error("owned child cleanup actual Wait missing")
	}
}
func (v *runnerSignalChild) lockState(owned bool) error {
	fd, err := syscall.Open(filepath.Join(v.directory, "identity.json.lock"), syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("identity_lock_unavailable")
	}
	defer syscall.Close(fd)
	var info syscall.Stat_t
	if syscall.Fstat(fd, &info) != nil || info.Mode&syscall.S_IFMT != syscall.S_IFREG || info.Mode&0777 != 0600 || int(info.Uid) != os.Geteuid() || info.Nlink != 1 {
		return errors.New("identity_lock_shape")
	}
	identity := runnerSignalIdentity{uint64(info.Dev), info.Ino}
	if v.lockKnown && identity != v.lock {
		return errors.New("identity_lock_replaced")
	}
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		if syscall.Flock(fd, syscall.LOCK_UN) != nil || owned {
			return errors.New("identity_lock_owner_mismatch")
		}
	} else if !owned || !(errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EACCES)) {
		return errors.New("identity_lock_owner_mismatch")
	}
	v.lock, v.lockKnown = identity, true
	return nil
}

func runnerSignalCase(t *testing.T, mode string) {
	t.Helper()
	binary := binaries["agenteam-runner"]
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("default TestMain Runner binary unavailable")
	}
	bin, err := runnerSignalStat(info)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp("", "agenteam-runner-signal-")
	if err != nil {
		t.Fatal("private signal directory unavailable")
	}
	// Failed evidence remains in this exact owned directory, never RemoveAll
	// while an original child might still own the identity lock.
	fds := []int{0, 0}
	if syscall.Pipe2(fds, syscall.O_CLOEXEC) != nil {
		t.Fatal("owned blocking pipe unavailable")
	}
	read, write := os.NewFile(uintptr(fds[0]), "runner-signal-read"), os.NewFile(uintptr(fds[1]), "runner-signal-write")
	defer read.Close()
	var pipe syscall.Stat_t
	if syscall.Fstat(fds[0], &pipe) != nil {
		write.Close()
		t.Fatal("owned pipe identity unavailable")
	}
	v := &runnerSignalChild{command: exec.Command(binary, "--enroll"), writer: write, done: make(chan struct{}),
		stdout: &runnerSignalOutput{stdout: true}, logs: &runnerSignalOutput{},
		pipe: pipe.Ino, binary: runnerSignalIdentity{uint64(bin.Dev), bin.Ino}, directory: directory}
	v.command.Dir = directory
	v.command.Env = []string{"PATH=" + os.Getenv("PATH"), "GOMAXPROCS=2", "AGENTEAM_RUNNER_IDENTITY_FILE=" + filepath.Join(directory, "identity.json"),
		"AGENTEAM_RUNNER_CENTRAL_URL=https://runner-probe.invalid", "AGENTEAM_RUNNER_ID=01900000-0000-7000-8000-000000000001", "AGENTEAM_RUNNER_ROOT_PATH=/runner-probe"}
	if mode == "deadline" {
		v.command.Env = append(v.command.Env, "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s")
	}
	v.command.Stdin, v.command.Stdout, v.command.Stderr = read, v.stdout, v.logs
	v.command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if v.command.Start() != nil {
		write.Close()
		t.Fatal("default Runner start failed")
	}
	v.pid = v.command.Process.Pid
	go func() { v.err = v.command.Wait(); close(v.done) }()
	defer v.cleanup(t)
	if read.Close() != nil {
		t.Fatal("parent reader close failed")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", v.pid))
	if err != nil {
		t.Fatal("original child stat unavailable")
	}
	v.start, _, err = runnerSignalStart(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("owned Runner pid=%d start_ticks=%d fd0_pipe=%d", v.pid, v.start, v.pipe)
	if err := v.until(time.Now().Add(5*time.Second), v.blocked); err != nil {
		t.Fatal("initial blocked read", err)
	}
	if v.lockState(true) != nil {
		t.Fatal("identity lock not owned before signal")
	}
	stoppedAt := time.Now()
	if v.signal(syscall.SIGTERM) != nil {
		t.Fatal("original TERM failed")
	}
	if err := v.until(stoppedAt.Add(time.Second), func() (bool, error) {
		records, bad := v.logs.state(false)
		if bad {
			return false, errors.New("unsafe_or_invalid_log")
		}
		for _, record := range records {
			if record.Event == "lifecycle" && record.Phase == "stopping" {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatal("original stopping", err)
	}
	if err := v.until(stoppedAt.Add(2*time.Second), v.blocked); err != nil {
		t.Fatal("cancelled read witness", err)
	}
	if v.lockState(true) != nil {
		t.Fatal("same identity lock released before read retirement")
	}
	forcedAt := stoppedAt
	want := 1
	if mode == "eof" {
		want = 0
		if v.writerEOF() != nil {
			t.Fatal("original writer EOF failed")
		}
	} else if mode == "second_signal" {
		forcedAt = time.Now()
		if v.signal(syscall.SIGINT) != nil {
			t.Fatal("original INT failed")
		}
	}
	code, err := v.wait(stoppedAt.Add(5 * time.Second))
	if err != nil || code != want {
		t.Fatalf("original Wait status=%d expected=%d valid=%t", code, want, err == nil)
	}
	elapsed := time.Since(stoppedAt)
	records, invalid := v.logs.state(true)
	_, stdoutBad := v.stdout.state(true)
	drained, forced, timeout := false, false, false
	for _, r := range records {
		drained = drained || r.Outcome == "drained"
		forced = forced || r.Outcome == "forced"
		timeout = timeout || r.Code == "SHUTDOWN_TIMEOUT"
	}
	if invalid || stdoutBad || (mode == "eof" && (!drained || forced)) || (mode != "eof" && (drained || !forced || !timeout)) {
		t.Fatal("original terminal log mismatch")
	}
	if mode == "second_signal" && (time.Since(forcedAt) < 900*time.Millisecond || elapsed >= 5*time.Second) || mode == "deadline" && (elapsed < 3900*time.Millisecond || elapsed >= 5*time.Second) {
		t.Fatal("original force or deadline budget mismatch")
	}
	if v.lockState(false) != nil {
		t.Fatal("same identity lock not released after Wait")
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", v.pid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("waited original PID still present or unknown")
	}
	if v.writerEOF() != nil {
		t.Fatal("owned parent writer not closed")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "identity.json.lock" {
		t.Fatal("unexpected private file or identity publication")
	}
	if os.Remove(filepath.Join(directory, "identity.json.lock")) != nil || os.Remove(directory) != nil {
		t.Fatal("owned private directory did not retire")
	}
	t.Logf("actual Wait=%d stdio_eof=true elapsed=%s read_joined=%t same_lock_released=true private_absent=true", code, elapsed, mode == "eof")
}

var _ io.Writer = (*runnerSignalOutput)(nil)
