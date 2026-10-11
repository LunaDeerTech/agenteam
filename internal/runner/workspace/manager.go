package workspace

import (
	"context"
	"os"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
)

type readerRoot interface {
	Read(context.Context, filesystem.ReadRequest) (filesystem.ReadResult, error)
	List(context.Context, filesystem.ListRequest) (filesystem.ListResult, error)
	Find(context.Context, filesystem.FindRequest) (filesystem.FindResult, error)
	Stop()
	Drain(context.Context) error
}
type nativeRoot interface {
	ensure(context.Context, Identity) (readerRoot, error)
	close() error
}
type operation struct {
	ctx    context.Context
	cancel context.CancelFunc
	handle *Handle
}

// Manager owns its root duplicate and every returned Handle until its actual
// close completes. It must not be copied. Stop signals; Drain actually joins.
type Manager struct {
	mu         sync.Mutex
	native     nativeRoot
	active     map[*operation]struct{}
	handles    map[*Handle]struct{}
	stopped    bool
	quiet      chan struct{}
	closing    bool
	closedDone chan struct{}
	closeErr   error
}

// Handle never exposes the owned Root or its descriptor. It must not be copied.
// All mutable fields below are protected by manager.mu.
type Handle struct {
	manager    *Manager
	identity   Identity
	root       readerRoot
	active     int
	stopped    bool
	quiet      chan struct{}
	closing    bool
	closedDone chan struct{}
	closeErr   error
}

// New borrows directory only until return. It duplicates the descriptor; the
// caller still owns directory and must keep it open during this call.
func New(directory *os.File) (*Manager, error) {
	native, err := openNativeRoot(directory)
	if err != nil {
		return nil, err
	}
	return &Manager{native: native, active: make(map[*operation]struct{}), handles: make(map[*Handle]struct{}), quiet: make(chan struct{}), closedDone: make(chan struct{})}, nil
}

func (m *Manager) begin(ctx context.Context, h *Handle) (*operation, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.native == nil || m.stopped || (h != nil && (h.manager != m || h.root == nil || h.stopped)) {
		return nil, ErrClosed
	}
	owned, cancel := context.WithCancel(ctx)
	op := &operation{owned, cancel, h}
	m.active[op] = struct{}{}
	if h != nil {
		h.active++
	}
	return op, nil
}

func (m *Manager) finishLocked(op *operation, err error) error {
	if cancelled := contextError(op.ctx); cancelled != nil {
		err = cancelled
	}
	m.retireLocked(op)
	return err
}
func (m *Manager) retireLocked(op *operation) {
	delete(m.active, op)
	if op.handle != nil {
		h := op.handle
		h.active--
		if h.stopped && h.active == 0 {
			close(h.quiet)
		}
	}
	if m.stopped && len(m.active) == 0 {
		close(m.quiet)
	}
	op.cancel()
}
func (m *Manager) finish(op *operation, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finishLocked(op, err)
}

func (m *Manager) Ensure(ctx context.Context, identity Identity) (*Handle, error) {
	if !identity.valid() {
		return nil, ErrInvalid
	}
	op, err := m.begin(ctx, nil)
	if err != nil {
		return nil, err
	}
	root, err := m.native.ensure(op.ctx, identity)
	m.mu.Lock()
	if err == nil {
		err = contextError(op.ctx)
	}
	if err == nil && root == nil {
		err = ErrUnavailable
	}
	if err == nil {
		h := &Handle{manager: m, identity: identity, root: root, quiet: make(chan struct{}), closedDone: make(chan struct{})}
		m.handles[h] = struct{}{}
		// Publishing and Stop are ordered by this same lock. No untracked Root.
		m.retireLocked(op)
		m.mu.Unlock()
		return h, err
	}
	m.mu.Unlock()
	if root != nil {
		// The unpublished Root remains owned by this registered Ensure until its
		// actual cleanup returns; caller cancellation never detaches that owner.
		if closeErr := root.Drain(context.Background()); closeErr != nil {
			m.mu.Lock()
			if m.closeErr == nil {
				m.closeErr = closeErr
			}
			m.mu.Unlock()
		}
	}
	return nil, m.finish(op, err)
}

func (m *Manager) stopHandleLocked(h *Handle) {
	if h.stopped {
		return
	}
	h.stopped = true
	for op := range m.active {
		if op.handle == h {
			op.cancel()
		}
	}
	h.root.Stop()
	if h.active == 0 {
		close(h.quiet)
	}
}
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.native == nil || m.stopped {
		return
	}
	m.stopped = true
	for op := range m.active {
		op.cancel()
	}
	for h := range m.handles {
		m.stopHandleLocked(h)
	}
	if len(m.active) == 0 {
		close(m.quiet)
	}
}
func wait(ctx context.Context, done <-chan struct{}) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return contextError(ctx)
	case <-done:
		return contextError(ctx)
	}
}

func (m *Manager) Drain(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if m == nil {
		return ErrClosed
	}
	m.Stop()
	m.mu.Lock()
	if m.native == nil {
		m.mu.Unlock()
		return ErrClosed
	}
	quiet := m.quiet
	m.mu.Unlock()
	if err := wait(ctx, quiet); err != nil {
		return err
	}
	for {
		m.mu.Lock()
		var next *Handle
		for h := range m.handles {
			next = h
			break
		}
		m.mu.Unlock()
		if next == nil {
			break
		}
		_ = next.Drain(ctx) // Actual close errors are retained in m.closeErr.
		if err := contextError(ctx); err != nil {
			return err
		}
	}
	m.mu.Lock()
	if err := contextError(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.closing {
		done := m.closedDone
		m.mu.Unlock()
		if err := wait(ctx, done); err != nil {
			return err
		}
		m.mu.Lock()
		err := m.closeErr
		m.mu.Unlock()
		return err
	}
	m.closing = true
	m.mu.Unlock()
	err := m.native.close() // One synchronous owner, never a detached goroutine.
	m.mu.Lock()
	if m.closeErr == nil {
		m.closeErr = err
	}
	err = m.closeErr
	close(m.closedDone)
	m.mu.Unlock()
	if cancelled := contextError(ctx); cancelled != nil {
		return cancelled
	}
	return err
}

func (h *Handle) Metadata() Identity {
	if h == nil || h.manager == nil {
		return Identity{}
	}
	return h.identity
}
func (h *Handle) Stop() {
	if h == nil || h.manager == nil {
		return
	}
	m := h.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if h.root != nil {
		m.stopHandleLocked(h)
	}
}
func (h *Handle) Drain(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if h == nil || h.manager == nil || h.root == nil {
		return ErrClosed
	}
	h.Stop()
	m := h.manager
	if err := wait(ctx, h.quiet); err != nil {
		return err
	}
	m.mu.Lock()
	if err := contextError(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	if h.closing {
		done := h.closedDone
		m.mu.Unlock()
		if err := wait(ctx, done); err != nil {
			return err
		}
		m.mu.Lock()
		err := h.closeErr
		m.mu.Unlock()
		return err
	}
	h.closing = true
	m.mu.Unlock()
	// All wrapper I/O has actually returned. Root owns its single physical close;
	// once entered it must return before this Handle can retire, even if ctx expires.
	err := h.root.Drain(context.Background())
	m.mu.Lock()
	h.closeErr = err
	if m.closeErr == nil {
		m.closeErr = err
	}
	delete(m.handles, h)
	close(h.closedDone)
	m.mu.Unlock()
	if cancelled := contextError(ctx); cancelled != nil {
		return cancelled
	}
	return err
}
func (h *Handle) begin(ctx context.Context) (*operation, error) {
	if h == nil || h.manager == nil {
		return nil, ErrClosed
	}
	return h.manager.begin(ctx, h)
}
func (h *Handle) Read(ctx context.Context, r filesystem.ReadRequest) (filesystem.ReadResult, error) {
	op, err := h.begin(ctx)
	if err != nil {
		return filesystem.ReadResult{}, err
	}
	result, err := h.root.Read(op.ctx, r)
	if err = h.manager.finish(op, err); err != nil {
		return filesystem.ReadResult{}, err
	}
	return result, nil
}
func (h *Handle) List(ctx context.Context, r filesystem.ListRequest) (filesystem.ListResult, error) {
	op, err := h.begin(ctx)
	if err != nil {
		return filesystem.ListResult{}, err
	}
	result, err := h.root.List(op.ctx, r)
	if err = h.manager.finish(op, err); err != nil {
		return filesystem.ListResult{}, err
	}
	return result, nil
}
func (h *Handle) Find(ctx context.Context, r filesystem.FindRequest) (filesystem.FindResult, error) {
	op, err := h.begin(ctx)
	if err != nil {
		return filesystem.FindResult{}, err
	}
	result, err := h.root.Find(op.ctx, r)
	if err = h.manager.finish(op, err); err != nil {
		return filesystem.FindResult{}, err
	}
	return result, nil
}
