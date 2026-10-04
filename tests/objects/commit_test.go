//go:build integration

package objects_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// commitProxy forwards the real PostgreSQL protocol on a test-only plaintext
// loopback connection. It withholds actual commit response frames; no driver
// Commit result is replaced or faked.
type commitProxy struct {
	listener    net.Listener
	upstream    string
	after       bool
	reached     chan struct{}
	release     chan struct{}
	quit        chan struct{}
	once        sync.Once
	wg          sync.WaitGroup
	mu          sync.Mutex
	connections map[net.Conn]bool
	armed       atomic.Int64
	late        atomic.Bool
	committed   chan struct{}
	commitOnce  sync.Once
}

func newCommitProxy(t *testing.T, upstream string, after bool) *commitProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &commitProxy{listener: listener, upstream: upstream, after: after, reached: make(chan struct{}, 1), release: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool), committed: make(chan struct{})}
	p.armed.Store(0)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.connections[client] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(p.Close)
	return p
}
func (p *commitProxy) Close() {
	p.once.Do(func() {
		close(p.quit)
		_ = p.listener.Close()
		p.mu.Lock()
		for conn := range p.connections {
			_ = conn.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
}
func (p *commitProxy) hold() {
	p.reached <- struct{}{}
	select {
	case <-p.release:
	case <-p.quit:
	}
}
func (p *commitProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	defer server.Close()
	var header [4]byte
	if _, err := io.ReadFull(client, header[:]); err != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, err := io.ReadFull(client, startup[4:]); err != nil {
		return
	}
	if _, err := server.Write(startup); err != nil {
		return
	}
	var committing atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readPGFrame(client)
			if err != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && p.targetCommit() {
				committing.Store(true)
				if !p.after {
					if p.late.Load() {
						_ = client.Close()
					}
					p.hold()
					if !p.late.Load() {
						return
					}
				}
			}
			if _, err := server.Write(frame); err != nil {
				return
			}
			if committing.Load() && p.late.Load() {
				// The original client is gone, but the owned upstream must stay
				// open until PostgreSQL actually answers the released COMMIT.
				select {
				case <-p.committed:
				case <-p.quit:
				case <-time.After(3 * time.Second):
				}
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readPGFrame(server)
			if err != nil {
				return
			}
			if committing.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				p.commitOnce.Do(func() { close(p.committed) })
			}
			if p.after && committing.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, err := readPGFrame(server)
				if err != nil || ready[0] != 'Z' || len(ready) != 6 || ready[5] != 'I' {
					return
				}
				p.hold()
				return
			}
			if _, err := client.Write(frame); err != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
func readPGFrame(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 1<<20 {
		return nil, errors.New("fixture protocol frame rejected")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, err := io.ReadFull(reader, frame[5:])
	return frame, err
}

func (p *commitProxy) targetCommit() bool {
	for {
		old := p.armed.Load()
		if old <= 0 {
			return false
		}
		if p.armed.CompareAndSwap(old, old-1) {
			return old == 1
		}
	}
}
func proxyStore(t *testing.T, f *fixture, after bool) (*postgres.Store, *commitProxy) {
	t.Helper()
	p := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), after)
	u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
	u.Host = p.listener.Addr().String()
	store, e := postgres.Open(contextFor(t), f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := store.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	return store, p
}
func TestObjectUnknownReservationWaitsForRealLateCommit(t *testing.T) {
	f := newFixture(t, true)
	httpProxy := newStorageProxy(t, f)
	store, p := proxyStore(t, f, false)
	p.late.Store(true)
	service, _ := f.on(t, store, httpProxy.server.URL, nil)
	p.armed.Store(2) // Prepare authorization commits first; reserve is second.
	errors := make(chan error, 1)
	ctx := contextFor(t)
	cmd := command(t, "held-reserve")
	go func() {
		_, e := service.PutObject(ctx, f.actor, f.owner, cmd, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
		errors <- e
	}()
	select {
	case <-p.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("reserve COMMIT not intercepted")
	}
	if httpProxy.puts.Load() != 0 {
		t.Fatal("physical PUT preceded confirmed reserve")
	}
	select {
	case e := <-errors:
		requireCode(t, e, foundation.CommitUnknown)
	case <-time.After(5 * time.Second):
		t.Fatal("unknown reserve did not return")
	}
	lookup, e := f.service.LookupPut(contextFor(t), f.actor, f.owner, "held-reserve")
	if e == nil {
		t.Fatal("held original transaction returned a result", lookup.State)
	}
	if httpProxy.puts.Load() != 0 {
		t.Fatal("lookup failure caused speculative PUT")
	}
	close(p.release)
	select {
	case <-p.committed:
	case <-time.After(3 * time.Second):
		t.Fatal("original transaction did not actually commit late")
	}
	lookup, e = f.service.LookupPut(contextFor(t), f.actor, f.owner, "held-reserve")
	if e != nil || lookup.State != oc.UploadPending {
		t.Fatal("late reservation not recovered", e)
	}
	var original string
	if e = f.store.QueryRow(contextFor(t), `SELECT object_id::text FROM agenteam_object.uploads WHERE command_key='held-reserve'`).Scan(&original); e != nil {
		t.Fatal(e)
	}
	result, e := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "held-reserve"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if e != nil || result.Meta.ID.String() != original || httpProxy.puts.Load() != 1 {
		t.Fatal("late command recovery changed identity or resent", e)
	}
}
func TestObjectLostReserveResponseResolvesBeforeOnlyPUT(t *testing.T) {
	f := newFixture(t, false)
	httpProxy := newStorageProxy(t, f)
	store, p := proxyStore(t, f, true)
	service, _ := f.on(t, store, httpProxy.server.URL, nil)
	p.armed.Store(2)
	done := make(chan error, 1)
	ctx := contextFor(t)
	cmd := command(t, "reserve-loss")
	go func() {
		_, e := service.PutObject(ctx, f.actor, f.owner, cmd, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
		done <- e
	}()
	select {
	case <-p.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("reserve response not intercepted")
	}
	if httpProxy.puts.Load() != 0 {
		t.Fatal("write ran before reserve confirmation")
	}
	close(p.release)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reserve recovery hung")
	}
	if httpProxy.puts.Load() != 1 {
		t.Fatal("physical write count", httpProxy.puts.Load())
	}
	var attempts int64
	if e := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.upload_attempts`).Scan(&attempts); e != nil || attempts != 1 {
		t.Fatal("unknown reserve created another attempt", e)
	}
}
func TestObjectPublishUnknownRetainsOriginalAuditAndPermissionFirst(t *testing.T) {
	f := newFixture(t, true)
	httpProxy := newStorageProxy(t, f)
	store, p := proxyStore(t, f, true)
	service, _ := f.on(t, store, httpProxy.server.URL, nil)
	prepared, e := service.PreparePayload(contextFor(t), f.actor, f.owner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if e != nil {
		t.Fatal(e)
	}
	defer service.DiscardPrepared(prepared)
	var attempt oc.UploadAttempt
	cmd := command(t, "publish-loss")
	accessPlan1 := ownerPlan(t, service, f.actor, f.owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &cmd, Prepared: prepared})
	result := plannedTx(store, service, contextFor(t), cause(t), accessPlan1, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		var e error
		attempt, e = service.ReserveUploadInTx(ctx, tx, f.actor, f.owner, cmd, prepared, plan, locked)
		return e
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	attempt, e = service.UploadPrepared(contextFor(t), f.actor, f.owner, prepared, attempt)
	if e != nil {
		t.Fatal(e)
	}
	p.armed.Store(1)
	done := make(chan foundation.CommitResult, 1)
	ctx := contextFor(t)
	txCause := cause(t)
	go func() {
		accessPlan2 := ownerPlan(t, service, f.actor, f.owner, oc.PublishAccess, oc.AccessRequestDetails{Attempt: attempt})
		done <- plannedTx(store, service, ctx, txCause, accessPlan2, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			_, e := service.PublishVerifiedInTx(ctx, tx, f.actor, f.owner, attempt, plan, locked)
			return e
		})
	}()
	select {
	case <-p.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("publication response not intercepted")
	}
	lookup, e := f.service.LookupPut(contextFor(t), f.actor, f.owner, "publish-loss")
	if e != nil || lookup.State != oc.UploadCommitted {
		t.Fatal("actual publication not visible", e)
	}
	object := lookup.Meta.ID
	var auditID string
	if e = f.store.QueryRow(contextFor(t), `SELECT audit_id::text FROM agenteam_object.uploads WHERE object_id=$1`, object.String()).Scan(&auditID); e != nil {
		t.Fatal(e)
	}
	close(p.release)
	select {
	case result := <-done:
		if result.State() != foundation.Unknown {
			t.Fatal("real response loss was not unknown")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unknown publication hung")
	}
	replay, e := service.PutObject(contextFor(t), f.actor, f.owner, command(t, "publish-loss"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if e != nil || replay.Meta.ID != object || httpProxy.puts.Load() != 1 {
		t.Fatal("publication replay resent payload", e)
	}
	var current string
	if e = f.store.QueryRow(contextFor(t), `SELECT audit_id::text FROM agenteam_object.uploads WHERE object_id=$1`, object.String()).Scan(&current); e != nil || current != auditID {
		t.Fatal("publication Audit identity changed")
	}
	f.sql(t, `UPDATE object_fixture.sessions SET active=false`)
	_, e = service.LookupPut(contextFor(t), f.actor, f.owner, "publish-loss")
	requireCode(t, e, foundation.SessionRevoked)
}
