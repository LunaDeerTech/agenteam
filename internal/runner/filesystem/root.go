package filesystem

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

type nativeRoot interface {
	read(context.Context, ReadRequest) (ReadResult, error)
	list(context.Context, ListRequest) (ListResult, error)
	close() error
}

type operation struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Root owns a duplicate of its borrowed directory. It must not be copied.
// Stop requests cancellation; Drain alone waits for all actual calls and closes
// the duplicate. Neither bounds the duration of an already-entered syscall.
type Root struct {
	mu              sync.Mutex
	fs              nativeRoot
	active          map[*operation]struct{}
	stopped         bool
	quiet           chan struct{}
	closing, closed bool
	closedDone      chan struct{}
	closeErr        error
}

func (*Root) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "filesystem_root") }
func (*Root) MarshalJSON() ([]byte, error) { return []byte(`"filesystem_root"`), nil }
func (*Root) LogValue() slog.Value         { return slog.StringValue("filesystem_root") }

// NewRoot borrows directory only during construction; the caller must keep that
// *os.File alive until this function returns. No path is obtained or accepted.
func NewRoot(directory *os.File) (*Root, error) {
	fs, err := openNativeRoot(directory)
	if err != nil {
		return nil, err
	}
	return &Root{fs: fs, active: make(map[*operation]struct{}), quiet: make(chan struct{}), closedDone: make(chan struct{})}, nil
}

func (r *Root) begin(ctx context.Context) (*operation, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrClosed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fs == nil || r.stopped {
		return nil, ErrClosed
	}
	owned, cancel := context.WithCancel(ctx)
	op := &operation{ctx: owned, cancel: cancel}
	r.active[op] = struct{}{}
	return op, nil
}

// finish runs only after the native method has returned and closed its target.
// The shared admission lock is the linearization point for success vs Stop.
func (r *Root) finish(op *operation, err error) error {
	r.mu.Lock()
	if cancelled := contextError(op.ctx); cancelled != nil {
		err = cancelled
	}
	delete(r.active, op)
	if r.stopped && len(r.active) == 0 {
		close(r.quiet)
	}
	r.mu.Unlock()
	op.cancel()
	return err
}

func (r *Root) Read(ctx context.Context, request ReadRequest) (ReadResult, error) {
	if !request.valid() {
		return ReadResult{}, ErrInvalid
	}
	op, err := r.begin(ctx)
	if err != nil {
		return ReadResult{}, err
	}
	result, err := r.fs.read(op.ctx, request)
	if err == nil {
		err = readBudget(result)
	}
	if err = r.finish(op, err); err != nil {
		return ReadResult{}, err
	}
	return result, nil
}

func (r *Root) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if !request.valid() {
		return ListResult{}, ErrInvalid
	}
	op, err := r.begin(ctx)
	if err != nil {
		return ListResult{}, err
	}
	result, err := r.fs.list(op.ctx, request)
	if err == nil {
		err = listBudget(result)
	}
	if err = r.finish(op, err); err != nil {
		return ListResult{}, err
	}
	return result, nil
}

func (r *Root) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fs == nil || r.stopped {
		return
	}
	r.stopped = true
	for op := range r.active {
		op.cancel()
	}
	if len(r.active) == 0 {
		close(r.quiet)
	}
}

func (r *Root) Drain(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if r == nil {
		return ErrClosed
	}
	r.Stop()
	if err := contextError(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	if r.fs == nil {
		r.mu.Unlock()
		return ErrClosed
	}
	quiet := r.quiet
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return contextError(ctx)
	case <-quiet:
	}
	r.mu.Lock()
	if err := contextError(ctx); err != nil {
		r.mu.Unlock()
		return err
	}
	if r.closing {
		done := r.closedDone
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return contextError(ctx)
		case <-done:
		}
		r.mu.Lock()
		err := r.closeErr
		r.mu.Unlock()
		if cancelled := contextError(ctx); cancelled != nil {
			return cancelled
		}
		return err
	}
	r.closing = true
	r.mu.Unlock()
	// This caller is the only close owner. No timeout goroutine or close retry.
	err := r.fs.close()
	r.mu.Lock()
	r.closeErr = err
	r.closed = true
	close(r.closedDone)
	r.mu.Unlock()
	if cancelled := contextError(ctx); cancelled != nil {
		return cancelled
	}
	return err
}
