package outbound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

// SMTPProfile is only a controlled connection profile, not an SMTP client.
// Limits have the same platform maxima as ordinary outbound requests.
type SMTPProfile struct{ data func() ProfileOptions }

func NewSMTPProfile(call CallContext, limits Limits) (SMTPProfile, error) {
	if call.data == nil {
		return SMTPProfile{}, invalid()
	}
	l, err := normalizeLimits(limits, false)
	if err != nil {
		return SMTPProfile{}, err
	}
	p := ProfileOptions{Consumer: ac.SMTP, Context: call, Limits: l}
	return SMTPProfile{data: func() ProfileOptions { return p }}, nil
}
func (p SMTPProfile) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_smtp_profile") }
func (p SMTPProfile) MarshalJSON() ([]byte, error) { return []byte(`"outbound_smtp_profile"`), nil }
func (p SMTPProfile) LogValue() slog.Value         { return slog.StringValue("outbound_smtp_profile") }

type smtpState struct {
	op              *operation
	pc              *pooledConn
	client          *Client
	profile         ProfileOptions
	target          Target
	writeMu         sync.Mutex
	viewMu          sync.Mutex
	a               *attempt
	writer          *firstWriter
	started, active bool
	sendCancel      context.CancelFunc
	stopSend        func()
	stop            func()
	closed          atomic.Bool
	closeOnce       sync.Once
}

// Conn is owned by Client shutdown and cannot replace its pinned socket.
// A trusted D07 adapter may use initial writes ONLY for noncredential protocol
// and TLS setup. Initial negotiation is not reported as an AUTH/mail send.
// Before every AUTH and every mail, call BeginSend; finish the protocol result
// then EndSend. Calls must be serial: concurrent BeginSend/Write is rejected.
// TLS can wrap this Conn: its underlying encrypted writes remain gated here.
// D07 must not place credentials in initial negotiation or omit BeginSend.
type Conn struct{ data func() *smtpState }

func (c *Client) DialTarget(ctx context.Context, host string, port uint16, profile SMTPProfile) (*Conn, error) {
	if profile.data == nil || port == 0 {
		return nil, networkError(Decision{Consumer: ac.SMTP}, ac.InvalidTarget, nil)
	}
	canonical, p, err := normalizeAuthority(net.JoinHostPort(host, strconv.Itoa(int(port))), "smtp")
	if err != nil {
		return nil, networkError(Decision{Consumer: ac.SMTP}, ac.InvalidTarget, err)
	}
	od := originData{"smtp", canonical, p}
	origin := Origin{data: func() originData { return od }}
	td := targetData{origin: origin, url: url.URL{Scheme: "smtp", Host: net.JoinHostPort(canonical, strconv.Itoa(int(p)))}}
	target := Target{data: func() targetData { return td }}
	op, err := c.begin(ctx, profile.data().Limits.Overall)
	if err != nil {
		return nil, networkError(Decision{Consumer: ac.SMTP, Origin: origin.String()}, ac.PolicyUnavailable, err)
	}
	a := newAttempt(op, c, profile.data(), target, 0)
	if status := c.state().policy.Status(); status.Version != nil {
		a.decision.PolicyVersion = status.Version
	}
	fail := func(err error) (*Conn, error) {
		safe := SafeNetworkError(err)
		a.denyAudit(safe.Decision().Reason)
		op.finish()
		return nil, safe
	}
	if err = a.resolve(); err != nil {
		return fail(err)
	}
	pc, err := a.connect()
	if err != nil {
		return fail(err)
	}
	s := &smtpState{op: op, pc: pc, client: c, profile: profile.data(), target: target, a: a, writer: &firstWriter{a: a, pc: pc, first: true}}
	s.stop = stopOnCancel(op.ctx, pc)
	return &Conn{data: func() *smtpState { return s }}, nil
}

// BeginSend does not itself send. It refreshes the entire address set and
// verifies the fixed peer. First Write rechecks policy under the shared gate.
// A failed BeginSend closes the connection; the old attempt cannot continue.
func (c *Conn) BeginSend(ctx context.Context) error {
	s := c.data()
	if !s.writeMu.TryLock() {
		_ = c.Close()
		return networkError(c.Decision(), ac.BindingInvalid, nil)
	}
	defer s.writeMu.Unlock()
	combined, cancel := context.WithCancel(ctx)
	stopParent := context.AfterFunc(s.op.ctx, cancel)
	if s.op.ctx.Err() != nil {
		cancel()
	}
	a := newAttempt(&operation{ctx: combined}, s.client, s.profile, s.target, 0)
	if status := s.client.state().policy.Status(); status.Version != nil {
		a.decision.PolicyVersion = status.Version
	}
	fail := func(err error) error {
		s.pc.close()
		a.denyAudit(SafeNetworkError(err).Decision().Reason)
		stopParent()
		cancel()
		_ = c.Close()
		return err
	}
	owner := s.client.state()
	owner.mu.Lock()
	stopped := owner.stopped || owner.forced
	owner.mu.Unlock()
	if stopped {
		return fail(a.fail(ac.PolicyUnavailable, nil))
	}
	if s.closed.Load() || s.active {
		return fail(a.fail(ac.BindingInvalid, nil))
	}
	if err := a.resolve(); err != nil {
		return fail(err)
	}
	release, err := owner.policy.state().gate.acquire(combined, false)
	if err != nil {
		return fail(a.fail(a.reason(err, ac.Cancelled), err))
	}
	if err = a.admit(s.pc.peer); err != nil {
		release()
		return fail(err)
	}
	// DNS and gate acquisition are cancellable work outside the owner lock.
	// Publishing the new attempt and StopAdmission share this exact mutex.
	owner.mu.Lock()
	if owner.stopped || owner.forced {
		owner.mu.Unlock()
		release()
		return fail(a.fail(ac.PolicyUnavailable, nil))
	}
	if err := combined.Err(); err != nil {
		owner.mu.Unlock()
		release()
		return fail(a.fail(a.reason(err, ac.Cancelled), err))
	}
	s.active = true
	s.viewMu.Lock()
	s.started = true
	s.a = a
	s.viewMu.Unlock()
	s.writer = &firstWriter{a: a, pc: s.pc, first: true}
	s.sendCancel = cancel
	stopSocket := stopOnCancel(combined, s.pc)
	s.stopSend = func() { stopSocket(); stopParent() }
	owner.mu.Unlock()
	release()
	return nil
}

// EndSend is called after the previous AUTH/mail protocol result is complete.
// It disables writes until the next BeginSend; it never resets a sent attempt.
func (c *Conn) EndSend() error {
	s := c.data()
	if !s.writeMu.TryLock() {
		_ = c.Close()
		return networkError(c.Decision(), ac.BindingInvalid, nil)
	}
	defer s.writeMu.Unlock()
	if !s.active || s.closed.Load() {
		return networkError(c.Decision(), ac.BindingInvalid, nil)
	}
	s.stopSend()
	s.sendCancel()
	s.stopSend = nil
	s.sendCancel = nil
	s.active = false
	return nil
}
func (c *Conn) Decision() Decision {
	s := c.data()
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	d := s.a.safeDecision()
	// A firstWriter also bounds setup bytes, but setup is not a business send.
	if !s.started {
		d.Sent = false
	}
	return d
}
func (c *Conn) Write(b []byte) (int, error) {
	s := c.data()
	if !s.writeMu.TryLock() {
		return 0, networkError(c.Decision(), ac.BindingInvalid, nil)
	}
	defer s.writeMu.Unlock()
	if s.closed.Load() || s.started && !s.active {
		return 0, networkError(c.Decision(), ac.BindingInvalid, nil)
	}
	n, err := s.writer.Write(b)
	if err != nil {
		d := c.Decision()
		reason := s.writer.a.reason(err, ac.InternalError)
		var safe *NetworkError
		if errors.As(err, &safe) {
			reason = safe.Decision().Reason
		}
		s.pc.close()
		s.writer.a.denyAudit(reason)
		_ = c.Close()
		return n, networkError(d, reason, err)
	}
	return n, nil
}
func (c *Conn) Read(b []byte) (int, error) {
	s := c.data()
	n, err := s.pc.conn.Read(b)
	if err != nil && err != io.EOF {
		s.viewMu.Lock()
		a := s.a
		s.viewMu.Unlock()
		return n, networkError(c.Decision(), a.reason(err, ac.InternalError), err)
	}
	return n, err
}
func (c *Conn) Close() error {
	s := c.data()
	s.closeOnce.Do(func() { s.closed.Store(true); s.pc.close(); s.stop(); s.op.finish() })
	return nil
}
func (c *Conn) LocalAddr() net.Addr  { return c.data().pc.raw.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr { return c.data().pc.raw.RemoteAddr() }
func (c *Conn) boundedDeadline(t time.Time) time.Time {
	if d, ok := c.data().op.ctx.Deadline(); ok && (t.IsZero() || d.Before(t)) {
		return d
	}
	return t
}
func (c *Conn) SetDeadline(t time.Time) error {
	return c.data().pc.setCallerDeadline(c.boundedDeadline(t), true, true)
}
func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.data().pc.setCallerDeadline(c.boundedDeadline(t), true, false)
}
func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.data().pc.setCallerDeadline(c.boundedDeadline(t), false, true)
}
func (c Conn) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_connection") }
func (c Conn) MarshalJSON() ([]byte, error) { return []byte(`"outbound_connection"`), nil }
func (c Conn) LogValue() slog.Value         { return slog.StringValue("outbound_connection") }
