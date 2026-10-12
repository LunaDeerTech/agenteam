//go:build linux

package command

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// These bounds apply only to termination and pipe cleanup after cancellation
// or actual child exit, never to business execution duration.
const terminationGrace = 250 * time.Millisecond
const pipeCloseGrace = 250 * time.Millisecond

type linuxOwner struct{ directory *os.File }

func newNativeOwner(directory *os.File) (nativeOwner, error) {
	if directory == nil {
		return nil, ErrInvalid
	}
	fd, err := unix.FcntlInt(directory.Fd(), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), "command-owned-cwd")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = file.Close()
		return nil, ErrInvalid
	}
	return &linuxOwner{directory: file}, nil
}

func (o *linuxOwner) close() error { return o.directory.Close() }

type ownedPipe struct {
	reader, writer *os.File
	closeOnce      sync.Once
	closeErr       error
}

func newOwnedPipe() (*ownedPipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, ErrIO
	}
	return &ownedPipe{reader: r, writer: w}, nil
}

func (p *ownedPipe) closeReader() error {
	p.closeOnce.Do(func() { p.closeErr = p.reader.Close() })
	return p.closeErr
}

type outputCollector struct {
	mu        sync.Mutex
	ctx       context.Context
	sink      Sink
	seen      [2]int
	delivered [2]int64
	truncated [2]bool
	failed    bool
	failure   chan struct{}
}

func (c *outputCollector) fail() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failLocked()
}

func (c *outputCollector) failLocked() {
	if !c.failed {
		c.failed = true
		close(c.failure)
	}
}

func (c *outputCollector) accept(index int, stream Stream, chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := len(chunk)
	if remaining := MaxStreamBytes - c.seen[index]; count > remaining {
		count = remaining
		c.truncated[index] = true
	}
	c.seen[index] += count
	if count == 0 || c.failed || c.sink == nil {
		return
	}
	if err := c.sink(c.ctx, stream, chunk[:count]); err != nil {
		// Callback errors may contain command output or credentials. Keep the
		// failure category, never incorporate their arbitrary error strings.
		c.failLocked()
		return
	}
	c.delivered[index] += int64(count)
}

func (c *outputCollector) read(p *ownedPipe, index int, stream Stream, done chan<- bool) {
	buffer := make([]byte, MaxChunkBytes)
	complete := false
	for {
		n, err := p.reader.Read(buffer)
		if n > 0 {
			c.accept(index, stream, buffer[:n])
			clear(buffer[:n])
		}
		if err != nil {
			complete = errors.Is(err, io.EOF)
			if !complete {
				c.fail()
			}
			break
		}
	}
	clear(buffer)
	if p.closeReader() != nil {
		complete = false
		c.fail()
	}
	done <- complete
}

func observeChild(pid int) error {
	// Observe this child without reaping it. Holding the original leader until
	// group termination prevents a numeric PID/PGID reuse race with kill(-pid).
	// This neither adopts descendants nor changes SIGCHLD/global subreaper state.
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func signalGroup(pid int, signal unix.Signal) error {
	err := unix.Kill(-pid, signal)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func signalOwnedChild(process *os.Process, signal unix.Signal) error {
	groupErr := signalGroup(process.Pid, signal)
	// A trusted command can move out of its original process group. Always
	// signal the original process handle too; Go retains its pidfd where
	// supported, and this child is not reaped until the later original Wait.
	childErr := process.Signal(signal)
	if errors.Is(childErr, os.ErrProcessDone) || errors.Is(childErr, unix.ESRCH) {
		childErr = nil
	}
	return errors.Join(groupErr, childErr)
}

func (o *linuxOwner) run(ctx context.Context, request Request, sink Sink) (result Result, resultErr error) {
	result.ExitCode = -1
	if err := ctx.Err(); err != nil {
		return result, err
	}
	stdout, err := newOwnedPipe()
	if err != nil {
		return result, err
	}
	stderr, err := newOwnedPipe()
	if err != nil {
		_ = stdout.writer.Close()
		_ = stdout.closeReader()
		return result, err
	}
	cmd := exec.Command(request.Program, request.Args...)
	// Go's Linux fork path performs chdir before descriptor remapping/exec.
	// The owned CLOEXEC fd is present then, including after a directory rename;
	// no host path is recovered and the fd is not inherited by the new program.
	cmd.Dir = "/proc/self/fd/" + strconv.FormatUint(uint64(o.directory.Fd()), 10)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = stdout.writer, stderr.writer
	// Stdin is the documented os/exec null device, never the Runner's stdin.
	startErr := cmd.Start()
	closeOutErr, closeErrErr := stdout.writer.Close(), stderr.writer.Close()
	if startErr != nil {
		_ = stdout.closeReader()
		_ = stderr.closeReader()
		return result, ErrStart
	}
	result.Started = true
	collector := &outputCollector{ctx: ctx, sink: sink, failure: make(chan struct{})}
	readDone := make(chan bool, 2)
	go collector.read(stdout, 0, Stdout, readDone)
	go collector.read(stderr, 1, Stderr, readDone)
	if closeOutErr != nil || closeErrErr != nil {
		collector.fail()
	}
	observed := make(chan error, 1)
	go func() { observed <- observeChild(cmd.Process.Pid) }()
	var observationErr error
	var terminated bool
	select {
	case observationErr = <-observed:
		terminated = true
	case <-ctx.Done():
	case <-collector.failure:
	}
	if !terminated {
		if err := signalOwnedChild(cmd.Process, unix.SIGTERM); err != nil {
			resultErr = ErrUnavailable
		}
		timer := time.NewTimer(terminationGrace)
		select {
		case observationErr = <-observed:
			terminated = true
		case <-timer.C:
		}
		timer.Stop()
	}
	// Even on normal leader exit, do not leave same-group children alive. The
	// original unreaped child anchors this group until after this signal. ECHILD
	// would mean an external reaper broke ownership: do not signal a reused ID.
	if !errors.Is(observationErr, unix.ECHILD) {
		if err := signalOwnedChild(cmd.Process, unix.SIGKILL); err != nil {
			resultErr = errors.Join(resultErr, ErrUnavailable)
		}
	}
	if !terminated {
		observationErr = <-observed
	}
	if observationErr != nil {
		resultErr = errors.Join(resultErr, ErrUnavailable)
	}
	// Exactly this Cmd owns the actual child Wait; observation above was WNOWAIT.
	waitErr := cmd.Wait()
	if cmd.ProcessState != nil {
		result.Waited = true
		result.ExitCode = cmd.ProcessState.ExitCode()
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			result.Signal = int(status.Signal())
		}
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		resultErr = errors.Join(resultErr, ErrUnavailable)
	}
	// This is a read-only group absence check, not a child-tree proof. Escaped
	// descendants cannot be excluded by process-group signaling or direct Wait.
	result.ProcessGroupTerminationConfirmed = errors.Is(unix.Kill(-cmd.Process.Pid, 0), unix.ESRCH)
	complete := true
	timer := time.NewTimer(pipeCloseGrace)
	for remaining := 2; remaining > 0; {
		select {
		case eof := <-readDone:
			complete = complete && eof
			remaining--
		case <-timer.C:
			// A descendant may have escaped with a pipe writer. Close our read
			// ends, report incomplete output, and still join actual callbacks.
			complete = false
			_ = stdout.closeReader()
			_ = stderr.closeReader()
		}
	}
	timer.Stop()
	collector.mu.Lock()
	result.StdoutBytes, result.StderrBytes = collector.delivered[0], collector.delivered[1]
	result.StdoutTruncated, result.StderrTruncated = collector.truncated[0], collector.truncated[1]
	result.OutputComplete = complete && !collector.failed
	if collector.failed || !complete {
		resultErr = errors.Join(resultErr, ErrIO)
	}
	collector.mu.Unlock()
	return result, resultErr
}
