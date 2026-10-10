package agentloop

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// DirectTextController owns accepted, single-Turn sessions. Accept performs no
// Model I/O and grants no permission to run: the Execution owner must commit its
// Snapshot, running/Started and exact first RoundInput before calling Result.
// Start/ResolveStartUnknown and all durable receipts remain Execution-owned.
type DirectTextController struct{ state func() *controllerState }

type controllerState struct {
	mu       sync.Mutex
	models   mc.JSONCaller
	stopped  bool
	sessions map[i.ExecutionID]*sessionState
}

// DirectTextSession retains the exact JSON handle until its real call has
// joined. Its first Result retains the caller's original Execution owner
// context; subsequent recovery never replaces it or starts another Model call.
// A successful Result is only a stable response candidate. The Executor still
// has to commit Transcript/terminal facts before publishing completion.
type DirectTextSession struct{ state func() *sessionState }

type sessionState struct {
	owner     *controllerState
	execution i.ExecutionID
	request   DirectTextRequest
	mu        sync.Mutex
	changed   chan struct{}
	active    bool
	started   bool
	stopping  bool
	joined    bool
	parent    context.Context
	ctx       context.Context
	cancel    context.CancelFunc
	call      mc.JSONCall
	consumed  bool
	response  *mc.ModelResponse
	err       error
}

func NewDirectTextController(models mc.JSONCaller) (*DirectTextController, error) {
	if nilLoopPort(models) {
		return nil, loopFault(f.DependencyUnbound)
	}
	s := &controllerState{models: models, sessions: make(map[i.ExecutionID]*sessionState)}
	return &DirectTextController{state: func() *controllerState { return s }}, nil
}

// Accept retains the immutable request without invoking Model or claiming that
// the Round has committed. A still-owned Execution cannot acquire a second
// session, including while its original startup or Model result is Unknown.
func (c *DirectTextController) Accept(request DirectTextRequest) (*DirectTextSession, error) {
	if c == nil || c.state == nil {
		return nil, loopFault(f.DependencyUnbound)
	}
	if request.Validate() != nil {
		return nil, invalid()
	}
	r := request.Request()
	if r.Validate() != nil || r.Input.ExecutionID == nil {
		return nil, invalid()
	}
	s := c.state()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, loopFault(f.ShuttingDown)
	}
	execution := *r.Input.ExecutionID
	if s.sessions[execution] != nil {
		return nil, loopFault(f.ResourceBusy)
	}
	run := &sessionState{owner: s, execution: execution, request: request, changed: make(chan struct{})}
	s.sessions[execution] = run
	return &DirectTextSession{state: func() *sessionState { return run }}, nil
}

func (c *DirectTextController) Stop() {
	if c == nil || c.state == nil {
		return
	}
	s := c.state()
	s.mu.Lock()
	s.stopped = true
	runs := make([]*sessionState, 0, len(s.sessions))
	for _, run := range s.sessions {
		runs = append(runs, run)
	}
	s.mu.Unlock()
	for _, run := range runs {
		run.stop()
	}
}

// Drain cancels this Controller's sessions only; it never drains a shared
// Model Runtime. Timeout leaves every unfinished original owner retained.
func (c *DirectTextController) Drain(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if c == nil || c.state == nil {
		return loopFault(f.DependencyUnbound)
	}
	c.Stop()
	s := c.state()
	s.mu.Lock()
	runs := make([]*sessionState, 0, len(s.sessions))
	for _, run := range s.sessions {
		runs = append(runs, run)
	}
	s.mu.Unlock()
	for _, run := range runs {
		if err := run.drain(ctx); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (c *DirectTextController) Joined() bool {
	if c == nil || c.state == nil {
		return false
	}
	s := c.state()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.sessions) == 0
}

func (s *DirectTextSession) Stop() {
	if s != nil && s.state != nil {
		s.state().stop()
	}
}

func (s *DirectTextSession) Drain(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.state == nil {
		return loopFault(f.DependencyUnbound)
	}
	return s.state().drain(ctx)
}

func (s *DirectTextSession) Joined() bool {
	if s == nil || s.state == nil {
		return false
	}
	run := s.state()
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.joined && !run.active
}

func (s *sessionState) stop() {
	s.mu.Lock()
	if s.joined && !s.active {
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
		return
	}
	s.stopping = true
	if s.cancel != nil {
		s.cancel()
	}
	if !s.started && !s.active {
		s.joined = true
		s.err = context.Canceled
	}
	released := s.joined && !s.active
	s.mu.Unlock()
	if released {
		s.release()
	}
}

func (s *sessionState) enter(ctx context.Context, wait bool) error {
	for {
		if err := contextError(ctx); err != nil {
			return err
		}
		s.mu.Lock()
		if !s.active {
			s.active = true
			s.mu.Unlock()
			return nil
		}
		changed := s.changed
		s.mu.Unlock()
		if !wait {
			return loopFault(f.ResourceBusy)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (s *sessionState) leave() {
	s.mu.Lock()
	s.active = false
	close(s.changed)
	s.changed = make(chan struct{})
	released := s.joined
	cancel := s.cancel
	s.mu.Unlock()
	if released {
		if cancel != nil {
			cancel()
		}
		s.release()
	}
}

func (s *sessionState) release() {
	s.owner.mu.Lock()
	if s.owner.sessions[s.execution] == s {
		delete(s.owner.sessions, s.execution)
	}
	s.owner.mu.Unlock()
}

func nilLoopPort(v any) bool {
	if v == nil {
		return true
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(v).IsNil()
	}
	return false
}

func loopFault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }

func (DirectTextController) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "direct_text_controller")
}
func (DirectTextController) LogValue() slog.Value { return slog.StringValue("direct_text_controller") }
func (DirectTextController) MarshalJSON() ([]byte, error) {
	return []byte(`"direct_text_controller"`), nil
}
func (DirectTextSession) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "direct_text_session") }
func (DirectTextSession) LogValue() slog.Value       { return slog.StringValue("direct_text_session") }
func (DirectTextSession) MarshalJSON() ([]byte, error) {
	return []byte(`"direct_text_session"`), nil
}
