// Package account implements current account facts. It installs no HTTP route
// and obtains every cross-domain capability from its trusted composition root.
package account

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type Dependencies struct {
	Authority       *Authority
	Audit           ac.Appender
	Secrets         *secret.Service
	Events          oc.Appender
	SessionsRevoked event.EventType[c.SessionsRevoked]
	Processes       c.ProcessAuthority
	RecoveryLog     *recoverylog.Sink
	Challenges      c.ChallengeAuthority
}
type Service struct{ data func() *serviceState }
type serviceState struct {
	deps             Dependencies
	store            Store
	keys             Keyring
	process          c.ProcessID
	browserIssuer    c.BrowserIssuer
	hasher           *PasswordHasher
	mu               sync.Mutex
	stopped, drained bool
	force            context.Context
	operations       map[*operation]bool
	changed          chan struct{}
	responses        map[string]*responseState
	logins           map[string]*loginOperation
	bootstraps       map[string]*operation
	localCursor      string
	forceDone        chan struct{}
	forceErr         error
}
type loginOperation struct {
	op       *operation
	identity foundation.CommandIdentity
}
type operation struct {
	ctx       context.Context
	cancel    context.CancelFunc
	once      sync.Once
	done      chan struct{}
	stopForce func() bool
}

func New(d Dependencies) (*Service, error) {
	if d.Authority == nil || d.Authority.data == nil || nilPort(d.Audit) || d.Secrets == nil || nilPort(d.Processes) || d.Processes.CurrentProcess().Validate() != nil || d.RecoveryLog == nil {
		return nil, invalid()
	}
	a := d.Authority.state()
	s := &serviceState{deps: d, store: a.store, keys: a.keys, process: d.Processes.CurrentProcess(), browserIssuer: c.NewBrowserIssuer(), hasher: NewPasswordHasher(), operations: map[*operation]bool{}, changed: make(chan struct{}), responses: map[string]*responseState{}, logins: map[string]*loginOperation{}, bootstraps: map[string]*operation{}}
	return &Service{func() *serviceState { return s }}, nil
}
func (s *Service) state() *serviceState { return s.data() }
func (s *Service) begin(ctx context.Context, cleanup bool) (*operation, error) {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.drained || st.stopped && !cleanup {
		return nil, fault(foundation.ShuttingDown, nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	var stopForce func() bool
	if cleanup && st.force != nil {
		if st.force.Err() != nil {
			cancel()
		} else {
			stopForce = context.AfterFunc(st.force, cancel)
		}
	}
	if err := ctx.Err(); err != nil {
		cancel()
		return nil, unavailable(err)
	}
	op := &operation{ctx: ctx, cancel: cancel, done: make(chan struct{}), stopForce: stopForce}
	st.operations[op] = true
	return op, nil
}
func (s *Service) finish(op *operation) {
	op.once.Do(func() {
		op.cancel()
		if op.stopForce != nil {
			op.stopForce()
		}
		close(op.done)
		st := s.state()
		st.mu.Lock()
		delete(st.operations, op)
		close(st.changed)
		st.changed = make(chan struct{})
		st.mu.Unlock()
	})
}
func (s *Service) StopAdmission() {
	st := s.state()
	st.mu.Lock()
	st.stopped = true
	st.mu.Unlock()
	st.hasher.StopAdmission()
	st.deps.RecoveryLog.StopAdmission()
}
func (s *Service) Drain(ctx context.Context) error {
	st := s.state()
	for {
		st.mu.Lock()
		if len(st.operations) == 0 && st.hasher.Joined() && st.deps.RecoveryLog.Joined() {
			st.drained = true
			st.mu.Unlock()
			return nil
		}
		ch := st.changed
		st.mu.Unlock()
		// Hash/log work belongs to an account operation, except an explicitly
		// supplied sink used by a trusted adapter, which has its own join port.
		if err := st.hasher.Drain(ctx); err != nil {
			return err
		}
		if err := st.deps.RecoveryLog.Drain(ctx); err != nil {
			return portError(err)
		}
		st.mu.Lock()
		if len(st.operations) == 0 {
			st.drained = true
			st.mu.Unlock()
			return nil
		}
		ch = st.changed
		st.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return unavailable(ctx.Err())
		}
	}
}
func (s *Service) Force(ctx context.Context) error {
	s.StopAdmission()
	st := s.state()
	st.mu.Lock()
	if st.forceDone == nil {
		st.force = ctx
		st.forceDone = make(chan struct{})
		for op := range st.operations {
			op.cancel()
		}
		force := ctx
		go func() {
			done := make(chan error, 2)
			go func() { done <- st.hasher.Force(force) }()
			go func() { done <- st.deps.RecoveryLog.Force(force) }()
			var first error
			for range 2 {
				if e := <-done; e != nil && first == nil {
					first = e
				}
			}
			if e := s.Drain(force); first == nil {
				first = e
			}
			st.mu.Lock()
			st.forceErr = first
			close(st.forceDone)
			st.mu.Unlock()
		}()
	}
	done := st.forceDone
	st.mu.Unlock()
	select {
	case <-done:
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.forceErr
	case <-ctx.Done():
		return unavailable(ctx.Err())
	}
}
func operationJoined(op *operation) bool {
	if op == nil {
		return false
	}
	select {
	case <-op.done:
		return true
	default:
		return false
	}
}

func (s *Service) Joined() bool {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.operations) == 0 && st.hasher.Joined() && st.deps.RecoveryLog.Joined()
}
func (s Service) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_service") }
func (s Service) MarshalJSON() ([]byte, error) { return []byte(`"account_service"`), nil }
func (s Service) LogValue() slog.Value         { return slog.StringValue("account_service") }
