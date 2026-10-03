package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type pooledConn struct {
	conn                        net.Conn
	raw                         net.Conn
	reader                      *bufio.Reader
	origin                      Origin
	peer                        netip.Addr
	idleAt                      time.Time
	once                        sync.Once
	owner                       *clientState
	deadlineMu                  sync.Mutex
	transportWrite, callerWrite time.Time
}

func earlierDeadline(a, b time.Time) time.Time {
	if a.IsZero() || !b.IsZero() && b.Before(a) {
		return b
	}
	return a
}

// Transport caps and caller deadlines are separate facts. net.Conn permits
// concurrent setters, so resetting a caller deadline must not remove the
// first-write cap while the shared policy gate is held.
func (c *pooledConn) setTransportWriteDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	c.transportWrite = t
	return c.conn.SetWriteDeadline(earlierDeadline(c.transportWrite, c.callerWrite))
}
func (c *pooledConn) setCallerDeadline(t time.Time, read, write bool) error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	var readErr error
	if read {
		readErr = c.conn.SetReadDeadline(t)
	}
	if write {
		c.callerWrite = t
		err := c.conn.SetWriteDeadline(earlierDeadline(c.transportWrite, c.callerWrite))
		if readErr == nil {
			return err
		}
	}
	return readErr
}

func (c *pooledConn) close() {
	c.once.Do(func() {
		_ = c.raw.Close()
		s := c.owner
		s.mu.Lock()
		delete(s.connections, c)
		s.notify()
		s.mu.Unlock()
	})
}

type clientState struct {
	policy          *PolicyService
	trust           TrustStore
	resolver        Resolver
	mu              sync.Mutex
	stopped, forced bool
	idle            map[string][]*pooledConn
	connections     map[*pooledConn]bool
	active          map[*operation]bool
	writers         int
	changed         chan struct{}
}

func (s *clientState) notify() { close(s.changed); s.changed = make(chan struct{}) }

type Client struct{ data func() *clientState }

func NewClient(policy *PolicyService, trust TrustStore, resolver Resolver) (*Client, error) {
	if policy == nil || policy.data == nil || trust.Validate() != nil {
		return nil, invalid()
	}
	if nilPort(resolver) {
		var err error
		resolver, err = SystemResolver()
		if err != nil {
			return nil, err
		}
	}
	s := &clientState{policy: policy, trust: trust, resolver: resolver, idle: map[string][]*pooledConn{}, connections: map[*pooledConn]bool{}, active: map[*operation]bool{}, changed: make(chan struct{})}
	return &Client{data: func() *clientState { return s }}, nil
}
func (c *Client) state() *clientState { return c.data() }

type operation struct {
	ctx    context.Context
	cancel context.CancelFunc
	owner  *clientState
	once   sync.Once
}

func (o *operation) finish() {
	o.once.Do(func() {
		o.cancel()
		o.owner.mu.Lock()
		delete(o.owner.active, o)
		o.owner.notify()
		o.owner.mu.Unlock()
	})
}
func (c *Client) begin(ctx context.Context, overall time.Duration) (*operation, error) {
	ctx, cancel := context.WithTimeout(ctx, overall)
	o := &operation{ctx: ctx, cancel: cancel, owner: c.state()}
	s := c.state()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || ctx.Err() != nil {
		cancel()
		return nil, unavailable(ctx.Err())
	}
	s.active[o] = true
	context.AfterFunc(ctx, o.finish)
	return o, nil
}
func (c *Client) StopAdmission() {
	s := c.state()
	s.mu.Lock()
	s.stopped = true
	var idle []*pooledConn
	for _, list := range s.idle {
		idle = append(idle, list...)
	}
	s.idle = map[string][]*pooledConn{}
	s.notify()
	s.mu.Unlock()
	for _, conn := range idle {
		conn.close()
	}
}
func (c *Client) Drain(ctx context.Context) error {
	s := c.state()
	for {
		s.mu.Lock()
		if len(s.active) == 0 && s.writers == 0 && (!s.stopped || len(s.connections) == 0) {
			s.mu.Unlock()
			return nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return unavailable(ctx.Err())
		}
	}
}
func (c *Client) ForceClose(ctx context.Context) error {
	c.StopAdmission()
	s := c.state()
	s.mu.Lock()
	s.forced = true
	var active []*operation
	for o := range s.active {
		active = append(active, o)
	}
	var conns []*pooledConn
	for conn := range s.connections {
		conns = append(conns, conn)
	}
	s.mu.Unlock()
	for _, o := range active {
		o.cancel()
	}
	for _, conn := range conns {
		conn.close()
	}
	return c.Drain(ctx)
}
func (c *Client) takeIdle(origin Origin) *pooledConn {
	s := c.state()
	for {
		s.mu.Lock()
		list := s.idle[origin.String()]
		if len(list) == 0 {
			s.mu.Unlock()
			return nil
		}
		pc := list[len(list)-1]
		s.idle[origin.String()] = list[:len(list)-1]
		s.mu.Unlock()
		if time.Since(pc.idleAt) > 90*time.Second {
			pc.close()
			continue
		}
		return pc
	}
}
func (c *Client) putIdle(pc *pooledConn) {
	s := c.state()
	s.mu.Lock()
	key := pc.origin.String()
	if s.stopped || s.forced || len(s.connections) > 64 || len(s.idle[key]) >= 2 {
		s.mu.Unlock()
		pc.close()
		return
	}
	_ = pc.conn.SetDeadline(time.Time{})
	pc.idleAt = time.Now()
	s.idle[key] = append(s.idle[key], pc)
	s.mu.Unlock()
}

type attempt struct {
	ctx        context.Context
	client     *Client
	profile    ProfileOptions
	target     Target
	addresses  []netip.Addr
	sent       atomic.Bool
	decisionMu sync.Mutex
	decision   Decision
}

func newAttempt(op *operation, c *Client, p ProfileOptions, target Target, redirect int) *attempt {
	d := Decision{Consumer: p.Consumer, Origin: target.Origin().String(), RedirectCount: redirect, HTTPTraceID: p.Context.data().associations.HTTPTraceID}
	return &attempt{ctx: op.ctx, client: c, profile: p, target: target, decision: d}
}
func (a *attempt) safeDecision() Decision {
	a.decisionMu.Lock()
	defer a.decisionMu.Unlock()
	d := a.decision
	if d.PolicyVersion != nil {
		v := *d.PolicyVersion
		d.PolicyVersion = &v
	}
	d.Sent = a.sent.Load()
	return d
}
func (a *attempt) fail(reason ac.Reason, cause error) *NetworkError {
	return networkError(a.safeDecision(), reason, cause)
}
func (a *attempt) reason(err error, fallback ac.Reason) ac.Reason {
	if errors.Is(a.ctx.Err(), context.Canceled) {
		return ac.Cancelled
	}
	if errors.Is(a.ctx.Err(), context.DeadlineExceeded) {
		return ac.Timeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ac.Timeout
	}
	return fallback
}
func (a *attempt) resolve() error {
	origin := a.target.Origin()
	if metadataHost(origin.Host()) {
		return a.fail(ac.AddressForbidden, nil)
	}
	if ip, err := netip.ParseAddr(origin.Host()); err == nil {
		a.addresses = []netip.Addr{ip.Unmap()}
	} else {
		ctx, cancel := context.WithTimeout(a.ctx, a.profile.Limits.DNS)
		defer cancel()
		ips, err := a.client.state().resolver.Lookup(ctx, origin.Host())
		if err != nil {
			return a.fail(a.reason(err, ac.DNSFailed), err)
		}
		a.addresses = uniqueIPs(slices.Clone(ips))
	}
	if len(a.addresses) == 0 || len(a.addresses) > 64 {
		return a.fail(ac.DNSFailed, nil)
	}
	for _, ip := range a.addresses {
		if !ip.IsValid() || ip.Zone() != "" {
			return a.fail(ac.AddressForbidden, nil)
		}
	}
	return nil
}

// admit requires a shared gate held by the caller through the real dial/write.
func (a *attempt) admit(peer netip.Addr) error {
	if err := a.ctx.Err(); err != nil {
		return a.fail(a.reason(err, ac.Cancelled), err)
	}
	p, err := a.client.state().policy.snapshot()
	if err != nil {
		return a.fail(ac.PolicyUnavailable, err)
	}
	a.decisionMu.Lock()
	v := p.Version()
	a.decision.PolicyVersion = &v
	a.decision.AddressClass = Public
	a.decisionMu.Unlock()
	for _, ip := range a.addresses {
		class := Classify(ip)
		if class != Public {
			a.decisionMu.Lock()
			a.decision.AddressClass = class
			a.decisionMu.Unlock()
		}
		if reason := p.Rules().check(ip, a.target.Origin().Port(), a.target.Origin().Scheme() == "http"); reason != "" {
			return a.fail(reason, nil)
		}
	}
	if peer.IsValid() && !slices.Contains(a.addresses, peer.Unmap()) {
		return a.fail(ac.AddressForbidden, nil)
	}
	return nil
}
func (a *attempt) connect() (*pooledConn, error) {
	ctx, cancel := context.WithTimeout(a.ctx, a.profile.Limits.Connect)
	defer cancel()
	release, err := a.client.state().policy.state().gate.acquire(ctx, false)
	if err != nil {
		return nil, a.fail(a.reason(err, ac.Timeout), err)
	}
	if err = a.admit(netip.Addr{}); err != nil {
		release()
		return nil, err
	}
	var conn net.Conn
	var dialErr error
	var peer netip.Addr
	for _, ip := range a.addresses {
		d := net.Dialer{}
		conn, dialErr = d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(int(a.target.Origin().Port()))))
		if dialErr == nil {
			peer = ip
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	release() // TLS is bounded separately; it is not an HTTP business send.
	if dialErr != nil || conn == nil {
		return nil, a.fail(a.reason(dialErr, ac.InternalError), dialErr)
	}
	remote, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil || remote.Port() != a.target.Origin().Port() || remote.Addr().Unmap() != peer.Unmap() {
		_ = conn.Close()
		return nil, a.fail(ac.AddressForbidden, err)
	}
	peer = remote.Addr().Unmap()
	pc := &pooledConn{conn: conn, raw: conn, origin: a.target.Origin(), peer: peer, owner: a.client.state()}
	s := a.client.state()
	s.mu.Lock()
	if s.forced {
		s.mu.Unlock()
		_ = conn.Close()
		return nil, a.fail(ac.Cancelled, nil)
	}
	s.connections[pc] = true
	s.mu.Unlock()
	if a.target.Origin().Scheme() == "https" {
		tlsConn := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: s.trust.roots(), ServerName: a.target.Origin().Host(), NextProtos: []string{"http/1.1"}})
		tlsCtx, tlsCancel := context.WithTimeout(a.ctx, a.profile.Limits.TLS)
		err = tlsConn.HandshakeContext(tlsCtx)
		tlsCancel()
		if err != nil {
			pc.close()
			return nil, a.fail(a.reason(err, ac.TLSFailed), err)
		}
		// The underlying socket remains the close owner; application reads and
		// writes use TLS and verify the original hostname, never the pinned IP.
		pc.conn = tlsConn
	}
	pc.reader = bufio.NewReader(pc.conn)
	return pc, nil
}

// firstWriter owns exactly one attempt state across the single allowed
// zero-byte retry. A replacement socket cannot reset whether bytes were sent.
type firstWriter struct {
	a     *attempt
	pc    *pooledConn
	first bool
}

func (w *firstWriter) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if err := w.a.ctx.Err(); err != nil {
		return 0, w.a.fail(w.a.reason(err, ac.Cancelled), err)
	}
	if !w.first {
		return w.pc.conn.Write(b)
	}
	w.first = false
	if w.a.sent.Load() {
		return 0, w.a.fail(ac.InternalError, nil)
	}
	release, err := w.a.client.state().policy.state().gate.acquire(w.a.ctx, false)
	if err != nil {
		return 0, w.a.fail(w.a.reason(err, ac.Cancelled), err)
	}
	defer release()
	if !w.pc.origin.Equal(w.a.target.Origin()) {
		return 0, w.a.fail(ac.OriginDenied, nil)
	}
	if err = w.a.admit(w.pc.peer); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(2 * time.Second)
	if d, ok := w.a.ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = w.pc.setTransportWriteDeadline(deadline); err != nil {
		return 0, w.a.fail(ac.InternalError, err)
	}
	n, err := w.pc.conn.Write(b)
	if n > 0 {
		w.a.sent.Store(true)
	}
	var rest time.Time
	if d, ok := w.a.ctx.Deadline(); ok {
		rest = d
	}
	_ = w.pc.setTransportWriteDeadline(rest)
	if n == 0 && err == nil {
		err = io.ErrNoProgress
	}
	if err != nil {
		return n, w.a.fail(w.a.reason(err, ac.InternalError), err)
	}
	return n, nil
}

func (a *attempt) denyAudit(reason ac.Reason) {
	s := a.client.state().policy.state()
	d := a.safeDecision()
	if d.PolicyVersion == nil {
		return
	}
	// Ordinary timeout, cancellation, disconnect and TLS/DNS availability errors
	// are operational outcomes, not copies of all failed traffic into Audit.
	switch reason {
	case ac.InvalidTarget, ac.AddressForbidden, ac.PrivateNotAllowed, ac.PortDenied, ac.HTTPDenied, ac.RedirectDenied, ac.BindingInvalid, ac.CredentialDenied, ac.OriginDenied, ac.ResponseLimit:
	default:
		return
	}
	call := a.profile.Context.data()
	resource, _ := ac.NewResource(ac.PolicyResource, "")
	metadata, err := ac.DenialMetadata(a.profile.Consumer, reason, *d.PolicyVersion)
	if err != nil {
		return
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: call.scope, Actor: call.actor, Action: ac.AccessDeny, Outcome: ac.Denied, Resource: resource, Metadata: metadata, Associations: call.associations})
	if err != nil {
		return
	}
	id, err := foundation.NewID[struct{}]()
	if err != nil {
		return
	}
	cause, err := foundation.NewRecoveryCause("outbound-denial", id.String(), "")
	if err != nil {
		return
	}
	// It is deliberately not detached from the operation budget. Missing Project
	// authority or a failed Audit preserves the original network refusal.
	ctx, cancel := context.WithTimeout(a.ctx, time.Second)
	defer cancel()
	_ = s.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if call.scope.Details().ProjectID != "" {
			lock, _ := foundation.ProjectLock(call.scope.Details().ProjectID)
			if err := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Shared}}); err != nil {
				return err
			}
		}
		_, err := s.audit.AppendInTx(ctx, tx, entry, call.key)
		return err
	})
}
