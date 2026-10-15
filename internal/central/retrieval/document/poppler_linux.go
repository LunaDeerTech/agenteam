//go:build linux

package document

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type popplerOutput struct {
	buffer   bytes.Buffer
	limit    int
	overflow *atomic.Bool
	cancel   context.CancelFunc
}

func (w *popplerOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.buffer.Len() {
		w.overflow.Store(true)
		w.cancel()
		return 0, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	return w.buffer.Write(p)
}

// The original synchronous call owns Start, cancellation and the real Wait.
// No shell, detached child, path search, output file or successful kill-as-Join.
func runPopplerProcess(ctx context.Context, c PopplerConfig, path string, args []string, input []byte, limit int, wall time.Duration) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	run, cancel := context.WithTimeout(ctx, wall)
	defer cancel()
	argv := []string{"--as=536870912", "--cpu=10", "--fsize=" + strconv.Itoa(limit), "--nofile=32", "--core=0", "--", path}
	argv = append(argv, args...)
	cmd := exec.CommandContext(run, c.PrlimitPath, argv...)
	cmd.Env = []string{"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "HOME=/nonexistent"}
	cmd.Stdin = bytes.NewReader(input)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	var overflow atomic.Bool
	out := &popplerOutput{limit: limit, overflow: &overflow, cancel: cancel}
	stderr := &popplerOutput{limit: 64 << 10, overflow: &overflow, cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	err := cmd.Wait()
	if overflow.Load() {
		clear(out.buffer.Bytes())
		clear(stderr.buffer.Bytes())
		return nil, nil, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if run.Err() != nil {
		clear(out.buffer.Bytes())
		clear(stderr.buffer.Bytes())
		return nil, nil, run.Err()
	}
	if err != nil {
		clear(out.buffer.Bytes())
		clear(stderr.buffer.Bytes())
		return nil, nil, invalid()
	}
	return out.buffer.Bytes(), stderr.buffer.Bytes(), nil
}
