package model

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// RuntimeDependencies must be composed against the same live Store. The
// original Usage/Secret callbacks validate that identity in their original Tx.
// Adapter owns its shared Budget; Runtime never stops that shared dependency.
type RuntimeDependencies struct {
	Usage        uc.Writer
	SecretReader sc.CredentialUsageReader
	SecretUsage  sc.UsageOperations
	Adapter      *wire.OpenAIChat
	AllowHTTP    bool
}

type Runtime struct{ data func() *runtimeState }
type runtimeState struct {
	store                        Store
	authority                    *RuntimeAuthority
	deps                         RuntimeDependencies
	agentTiming                  *mc.AgentRetryTiming
	mu                           sync.Mutex
	initializing, ready, stopped bool
	process                      oc.ProcessID
	calls                        map[mc.CallID]*runtimeCall
	admissions                   map[*runtimeAdmission]struct{}
}

type runtimeAdmission struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRuntime(store Store, authority *RuntimeAuthority, deps RuntimeDependencies) (*Runtime, error) {
	return newRuntime(store, authority, deps, nil)
}

// NewRuntimeWithAgentRetry opts into the explicit Agent text retry profile.
// It does not supply the consumer authority or bind an Execution runtime.
func NewRuntimeWithAgentRetry(store Store, authority *RuntimeAuthority, deps RuntimeDependencies, timing mc.AgentRetryTiming) (*Runtime, error) {
	if timing.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	// The existing text adapter has an overall 120s bound and a 60s idle bound.
	// Reject an unsupported configured profile before registering this Runtime.
	if timing.Fields().MaxRequestTimeout > 120*time.Second {
		return nil, fault(f.CapabilityUnsupported)
	}
	return newRuntime(store, authority, deps, &timing)
}

func newRuntime(store Store, authority *RuntimeAuthority, deps RuntimeDependencies, timing *mc.AgentRetryTiming) (*Runtime, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) || nilPort(deps.Usage) || nilPort(deps.SecretReader) || nilPort(deps.SecretUsage) || deps.Adapter == nil {
		return nil, fault(f.DependencyUnbound)
	}
	a := authority.state()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtime != nil {
		return nil, fault(f.ResourceBusy)
	}
	s := &runtimeState{store: store, authority: authority, deps: deps, agentTiming: timing, calls: make(map[mc.CallID]*runtimeCall), admissions: make(map[*runtimeAdmission]struct{})}
	a.runtime = s
	return &Runtime{data: func() *runtimeState { return s }}, nil
}

func (r *Runtime) state() *runtimeState {
	if r == nil || r.data == nil {
		return nil
	}
	return r.data()
}

func (r *Runtime) Initialize(ctx context.Context) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	s := r.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return fault(f.ShuttingDown)
	}
	if s.ready {
		s.mu.Unlock()
		return nil
	}
	if s.initializing {
		s.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	s.initializing = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.initializing = false; s.mu.Unlock() }()
	process, err := s.authority.state().auth.Process.CurrentProcess()
	if err != nil {
		return portError(err)
	}
	cause, err := readCause("runtime-initialize")
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var pending bool
		// No restart takeover in this version. An unknown/unretired prior call
		// blocks admission; a TTL or a missing local map is not a death proof.
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.calls WHERE NOT retired), (SELECT count(*) FROM agenteam_model.runtime_attempts WHERE false)`).Scan(&pending, new(int64)); err != nil {
			return unavailable(err)
		}
		if pending {
			return fault(f.InvalidState)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if err = runtimeContextError(ctx); err != nil {
		return err
	}
	current, err := s.authority.state().auth.Process.CurrentProcess()
	if err != nil || current != process {
		return unavailable(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return fault(f.ShuttingDown)
	}
	s.process, s.ready = process, true
	return nil
}

func (r *Runtime) StopAdmission() {
	s := r.state()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	calls := make([]*runtimeCall, 0, len(s.calls))
	for _, call := range s.calls {
		calls = append(calls, call)
	}
	admissions := make([]*runtimeAdmission, 0, len(s.admissions))
	for admission := range s.admissions {
		admissions = append(admissions, admission)
	}
	s.mu.Unlock()
	for _, admission := range admissions {
		admission.cancel()
	}
	for _, call := range calls {
		call.cancel()
	}
}

func (r *Runtime) Drain(ctx context.Context) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	s := r.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	for {
		s.mu.Lock()
		if s.initializing {
			s.mu.Unlock()
			return fault(f.ResourceBusy)
		}
		calls := make([]*runtimeCall, 0, len(s.calls))
		for _, call := range s.calls {
			calls = append(calls, call)
		}
		admissions := make([]*runtimeAdmission, 0, len(s.admissions))
		for admission := range s.admissions {
			admissions = append(admissions, admission)
		}
		s.mu.Unlock()
		if len(calls) == 0 && len(admissions) == 0 {
			return nil
		}
		for _, admission := range admissions {
			admission.cancel()
			select {
			case <-admission.done:
			case <-ctx.Done():
				return unavailable(ctx.Err())
			}
		}
		for _, call := range calls {
			if err := call.close(ctx); err != nil {
				return err
			}
		}
	}
}

func (r *Runtime) Force(ctx context.Context) error {
	r.StopAdmission()
	return r.Drain(ctx)
}

func (r *Runtime) Joined() bool {
	s := r.state()
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && !s.initializing && len(s.calls) == 0 && len(s.admissions) == 0
}

func (*Runtime) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "model_runtime") }
func (*Runtime) MarshalJSON() ([]byte, error) { return []byte(`"model_runtime"`), nil }
func (*Runtime) LogValue() slog.Value         { return slog.StringValue("model_runtime") }
