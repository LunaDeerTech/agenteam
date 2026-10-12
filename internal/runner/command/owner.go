package command

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
)

type nativeOwner interface {
	run(context.Context, Request, Sink) (Result, error)
	close() error
}

// Owner is single-use. New duplicates a trusted directory descriptor; its
// caller retains ownership of the original. No descriptor or child handle is
// exposed. The directory duplicate remains owned until Stop plus actual Drain.
type Owner struct {
	mu                                     sync.Mutex
	native                                 nativeOwner
	used, stopped, active, closing, closed bool
	cancel                                 context.CancelFunc
	quiet                                  chan struct{}
	closedDone                             chan struct{}
	closeErr                               error
}

func New(directory *os.File) (*Owner, error) {
	native, err := newNativeOwner(directory)
	if err != nil {
		return nil, err
	}
	return newOwner(native), nil
}

func newOwner(native nativeOwner) *Owner {
	return &Owner{native: native, quiet: make(chan struct{}), closedDone: make(chan struct{})}
}

func (o *Owner) Run(ctx context.Context, request Request, sink Sink) (Result, error) {
	result := Result{ExitCode: -1}
	if ctx == nil {
		return result, ErrInvalid
	}
	if err := request.Validate(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if o == nil {
		return result, ErrClosed
	}
	request.Args = slices.Clone(request.Args)
	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return result, ErrClosed
	}
	if o.used {
		o.mu.Unlock()
		return result, ErrBusy
	}
	runCtx, cancel := context.WithCancel(ctx)
	o.used, o.active, o.cancel = true, true, cancel
	o.mu.Unlock()
	result, err := o.native.run(runCtx, request, sink)
	// Serialize Stop with the original return; cancellation cannot erase actual
	// Started/Waited/exit/output facts and never releases an active native call.
	o.mu.Lock()
	if cancelled := runCtx.Err(); cancelled != nil {
		result.Cancelled = true
		result.TimedOut = errors.Is(cancelled, context.DeadlineExceeded)
		err = errors.Join(err, cancelled)
	}
	o.active, o.cancel = false, nil
	if o.stopped {
		close(o.quiet)
	}
	o.mu.Unlock()
	cancel()
	return result, err
}

func (o *Owner) Stop() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return
	}
	o.stopped = true
	if o.cancel != nil {
		o.cancel()
	}
	if !o.active {
		close(o.quiet)
	}
}

// Drain closes admission and waits for actual native return and descriptor
// close. A caller deadline only stops its wait; it does not detach or fabricate
// retirement. The caller must resume Drain to observe the original close.
func (o *Owner) Drain(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if o == nil {
		return nil
	}
	o.Stop()
	select {
	case <-o.quiet:
	case <-ctx.Done():
		return ctx.Err()
	}
	o.mu.Lock()
	if !o.closing {
		o.closing = true
		o.mu.Unlock()
		err := o.native.close()
		o.mu.Lock()
		if err != nil {
			o.closeErr = ErrIO
		}
		o.closed = true
		close(o.closedDone)
	}
	done := o.closedDone
	o.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	o.mu.Lock()
	err := o.closeErr
	o.mu.Unlock()
	return err
}

func (o *Owner) Joined() bool {
	if o == nil {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stopped && !o.active && o.closed
}
