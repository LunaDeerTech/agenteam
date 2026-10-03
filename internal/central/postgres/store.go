package postgres

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store never exposes its pool or driver connection. Every borrower is tracked,
// including rows that keep a connection until Close/EOF.
type Store struct{ state func() *storeState }
type storeState struct {
	config     Config
	pool       *pgxpool.Pool
	sockets    socketSet
	mu         sync.Mutex
	stopped    bool
	operations map[*operation]struct{}
	txs        map[foundation.Tx]*transaction
	changed    chan struct{}
	closeOnce  sync.Once
	closed     chan struct{}
}
type operation struct {
	owner  *storeState
	ctx    context.Context
	cancel context.CancelFunc
	conn   *pgxpool.Conn
	once   sync.Once
}
type socketSet struct {
	mu      sync.Mutex
	closed  bool
	sockets map[*ownedSocket]struct{}
}
type ownedSocket struct {
	net.Conn
	owner *socketSet
	once  sync.Once
}

func (s *ownedSocket) Close() error {
	err := s.Conn.Close()
	s.once.Do(func() { s.owner.mu.Lock(); delete(s.owner.sockets, s); s.owner.mu.Unlock() })
	return err
}
func (s *socketSet) dial(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	if s.sockets == nil {
		s.sockets = make(map[*ownedSocket]struct{})
	}
	owned := &ownedSocket{Conn: conn, owner: s}
	s.sockets[owned] = struct{}{}
	return owned, nil
}
func (s *socketSet) close() {
	s.mu.Lock()
	s.closed = true
	all := make([]*ownedSocket, 0, len(s.sockets))
	for socket := range s.sockets {
		all = append(all, socket)
	}
	s.mu.Unlock()
	for _, socket := range all {
		_ = socket.Close()
	}
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	parsed, err := cfg.driverConfig()
	if err != nil {
		return nil, err
	}
	poolConfig, err := pgxpool.ParseConfig(parsed.ConnString())
	if err != nil {
		return nil, failure(InvalidConfiguration, err)
	}
	state := &storeState{config: cfg, operations: make(map[*operation]struct{}), txs: make(map[foundation.Tx]*transaction), changed: make(chan struct{}), closed: make(chan struct{})}
	parsed.DialFunc = state.sockets.dial
	poolConfig.ConnConfig = parsed
	poolConfig.MaxConns = cfg.MaxConns()
	poolConfig.MinConns = 0
	poolConfig.MinIdleConns = 0
	poolConfig.HealthCheckPeriod = time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		state.sockets.close()
		return nil, failure(ConnectionFailed, err)
	}
	state.pool = pool
	store := &Store{state: func() *storeState { return state }}
	connect, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout())
	defer cancel()
	op, err := store.borrow(connect)
	if err == nil {
		err = checkCompatibility(connect, op.conn.Conn())
		op.release()
	}
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = store.ForceClose(cleanup)
		return nil, err
	}
	return store, nil
}
func (s *Store) borrow(ctx context.Context) (*operation, error) {
	state := s.state()
	opCtx, cancel := context.WithCancel(ctx)
	op := &operation{owner: state, ctx: opCtx, cancel: cancel}
	state.mu.Lock()
	if state.stopped {
		state.mu.Unlock()
		cancel()
		return nil, failure(AdmissionStopped, nil)
	}
	state.operations[op] = struct{}{}
	state.mu.Unlock()
	conn, err := state.pool.Acquire(opCtx)
	if err != nil {
		op.release()
		return nil, failure(ConnectionFailed, err)
	}
	state.mu.Lock()
	op.conn = conn
	state.mu.Unlock()
	return op, nil
}
func (o *operation) release() {
	o.once.Do(func() {
		o.cancel()
		o.owner.mu.Lock()
		delete(o.owner.operations, o)
		close(o.owner.changed)
		o.owner.changed = make(chan struct{})
		o.owner.mu.Unlock()
		if o.conn != nil {
			o.conn.Release()
		}
	})
}
func (s *Store) StopAdmission() {
	state := s.state()
	state.mu.Lock()
	state.stopped = true
	state.mu.Unlock()
}
func (s *Store) Drain(ctx context.Context) error {
	s.StopAdmission()
	state := s.state()
	for {
		state.mu.Lock()
		count, changed := len(state.operations), state.changed
		state.mu.Unlock()
		if count == 0 {
			state.startClose()
			return state.waitClose(ctx)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return failure(DrainTimeout, ctx.Err())
		}
	}
}
func (s *storeState) startClose() {
	s.closeOnce.Do(func() { go func() { s.pool.Close(); s.sockets.close(); close(s.closed) }() })
}
func (s *storeState) waitClose(ctx context.Context) error {
	select {
	case <-s.closed:
		return nil
	case <-ctx.Done():
		return failure(DrainTimeout, ctx.Err())
	}
}

// ForceClose cancels all borrowers and closes owned sockets before waiting at
// most one second. A callback ignoring cancellation cannot delay this return.
func (s *Store) ForceClose(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	s.StopAdmission()
	state := s.state()
	state.mu.Lock()
	connections := make([]*pgconn.PgConn, 0, len(state.operations))
	for op := range state.operations {
		op.cancel()
		if op.conn != nil {
			connections = append(connections, op.conn.Conn().PgConn())
		}
	}
	state.mu.Unlock()
	// EOF alone is not observed while PostgreSQL is inside a long statement.
	// Best-effort cancellation uses each owned connection's backend secret, not
	// a PID enumeration. All requests share a small part of the same total bound.
	requestCtx, requestCancel := context.WithTimeout(bounded, 100*time.Millisecond)
	var requests sync.WaitGroup
	for _, conn := range connections {
		requests.Add(1)
		go func() { defer requests.Done(); _ = conn.CancelRequest(requestCtx) }()
	}
	requestsDone := make(chan struct{})
	go func() { requests.Wait(); close(requestsDone) }()
	select {
	case <-requestsDone:
	case <-requestCtx.Done():
	}
	requestCancel()
	state.sockets.close()
	state.startClose()
	return state.waitClose(bounded)
}

type compatibilityQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func checkCompatibility(ctx context.Context, q compatibilityQuerier) error {
	var version int
	if err := q.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		return failure(ConnectionFailed, err)
	}
	if version/10000 != 17 || version%10000 < 8 {
		return failure(VersionUnsupported, nil)
	}
	var available bool
	if err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_available_extension_versions WHERE name='vector' AND version='0.8.1')").Scan(&available); err != nil {
		return failure(ExtensionUnsupported, err)
	}
	if !available {
		return failure(ExtensionUnsupported, nil)
	}
	var installed string
	if err := q.QueryRow(ctx, "SELECT coalesce((SELECT extversion FROM pg_extension WHERE extname='vector'),'')").Scan(&installed); err != nil {
		return failure(ExtensionUnsupported, err)
	}
	if installed != "" && installed != "0.8.1" {
		return failure(ExtensionUnsupported, nil)
	}
	return nil
}
