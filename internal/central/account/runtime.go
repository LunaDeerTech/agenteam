package account

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

var accountRuntimes sync.Map // one runtime reservation per actual core instance

type Runtime struct{ data func() *runtimeState }
type runtimeState struct {
	core                       *Service
	profiles                   *ProfileService
	mu                         sync.Mutex
	started, starting, stopped bool
	startDone                  chan struct{}
	loopDone                   chan struct{}
	cancel                     context.CancelFunc
	startCancel                context.CancelFunc
	failure                    error
}

func NewRuntime(core *Service, profiles *ProfileService) (*Runtime, error) {
	if core == nil || core.data == nil || profiles == nil || profiles.data == nil || profiles.state().core != core {
		return nil, invalid()
	}
	st := &runtimeState{core: core, profiles: profiles}
	r := &Runtime{func() *runtimeState { return st }}
	if _, exists := accountRuntimes.LoadOrStore(core.state(), r); exists {
		return nil, fault(foundation.InvalidState, nil)
	}
	profiles.state().mu.Lock()
	profiles.state().runtime = r
	profiles.state().mu.Unlock()
	return r, nil
}
func (r *Runtime) Start(ctx context.Context) error {
	st := r.data()
	st.mu.Lock()
	if st.starting || st.started || st.stopped {
		st.mu.Unlock()
		return fault(foundation.InvalidState, nil)
	}
	st.starting = true
	st.startDone = make(chan struct{})
	work, cancel := context.WithCancel(ctx)
	st.startCancel = cancel
	st.mu.Unlock()
	op, e := st.core.begin(work, false)
	if e == nil {
		_, e = st.core.Bootstrap(op.ctx)
		if e == nil {
			_, e = st.core.Recover(op.ctx)
		}
		if e == nil {
			_, e = st.profiles.RecoverAvatars(op.ctx)
		}
		st.core.finish(op)
	}
	cancel()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.starting = false
	st.failure = e
	close(st.startDone)
	if e != nil {
		return e
	}
	if st.stopped {
		return fault(foundation.ShuttingDown, nil)
	}
	loop, stop := context.WithCancel(context.Background())
	st.cancel = stop
	st.loopDone = make(chan struct{})
	st.started = true
	go r.maintain(loop)
	return nil
}
func (r *Runtime) maintain(ctx context.Context) {
	st := r.data()
	defer close(st.loopDone)
	timer := time.NewTicker(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		work, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, first := st.core.Recover(work)
		if work.Err() == nil {
			if _, e := st.profiles.RecoverAvatars(work); first == nil {
				first = e
			}
		}
		cancel()
		st.mu.Lock()
		if !st.stopped {
			st.failure = first
		}
		st.mu.Unlock()
	}
}
func (r *Runtime) Check(ctx context.Context) error {
	st := r.data()
	st.mu.Lock()
	ready := st.started && !st.starting && !st.stopped
	err := st.failure
	st.mu.Unlock()
	if !ready {
		return unavailable(nil)
	}
	if err != nil {
		return portError(err)
	}
	return st.core.state().deps.Authority.CheckStorage(ctx)
}
func (r *Runtime) StopAdmission() {
	st := r.data()
	st.mu.Lock()
	st.stopped = true
	if st.cancel != nil {
		st.cancel()
	}
	if st.startCancel != nil {
		st.startCancel()
	}
	st.mu.Unlock()
	st.core.StopAdmission()
}
func waitRuntime(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return unavailable(ctx.Err())
	}
}
func (r *Runtime) Drain(ctx context.Context) error {
	r.StopAdmission()
	st := r.data()
	st.mu.Lock()
	start, loop := st.startDone, st.loopDone
	st.mu.Unlock()
	if e := waitRuntime(ctx, start); e != nil {
		return e
	}
	if e := waitRuntime(ctx, loop); e != nil {
		return e
	}
	return st.core.Drain(ctx)
}
func (r *Runtime) Force(ctx context.Context) error {
	r.StopAdmission()
	err := r.data().core.Force(ctx)
	if e := r.Drain(ctx); err == nil {
		err = e
	}
	return err
}
func (r *Runtime) Joined() bool {
	st := r.data()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.stopped || st.starting {
		return false
	}
	if st.loopDone != nil {
		select {
		case <-st.loopDone:
		default:
			return false
		}
	}
	joined := st.core.Joined()
	if joined {
		accountRuntimes.CompareAndDelete(st.core.state(), r)
	}
	return joined
}
func (r Runtime) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_runtime") }
func (r Runtime) MarshalJSON() ([]byte, error) { return []byte(`"account_runtime"`), nil }
func (*Runtime) UnmarshalJSON([]byte) error    { return invalid() }
func (r Runtime) LogValue() slog.Value         { return slog.StringValue("account_runtime") }
