//go:build integration

package database_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type attributionProxy struct {
	listener                                                  net.Listener
	upstream                                                  string
	drop                                                      bool
	afterExecute                                              bool
	query                                                     string
	ready, requested, delivered, ack, quit                    chan struct{}
	readyOnce, requestOnce, deliveredOnce, ackOnce, closeOnce sync.Once
	ackSeen                                                   atomic.Bool
	mu                                                        sync.Mutex
	conns                                                     map[net.Conn]struct{}
	closed                                                    bool
	wg                                                        sync.WaitGroup
}

func attributionFrame(r io.Reader) ([]byte, error) {
	var h [5]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return nil, e
	}
	n := binary.BigEndian.Uint32(h[1:])
	if n < 4 || n > 1<<20 {
		return nil, errors.New("attribution fixture frame size")
	}
	b := make([]byte, int(n)+1)
	copy(b, h[:])
	_, e := io.ReadFull(r, b[5:])
	return b, e
}
func attributionWrite(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		b = b[n:]
	}
	return nil
}
func (p *attributionProxy) add(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = c.Close()
		return false
	}
	p.conns[c] = struct{}{}
	return true
}
func (p *attributionProxy) remove(c net.Conn) {
	_ = c.Close()
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
}
func (p *attributionProxy) release() { p.ackOnce.Do(func() { close(p.ack) }) }
func (p *attributionProxy) Close() {
	p.closeOnce.Do(func() {
		p.release()
		p.mu.Lock()
		p.closed = true
		all := make([]net.Conn, 0, len(p.conns))
		for c := range p.conns {
			all = append(all, c)
		}
		p.mu.Unlock()
		close(p.quit)
		_ = p.listener.Close()
		for _, c := range all {
			_ = c.Close()
		}
		p.wg.Wait()
	})
}
func newAttributionProxy(t *testing.T, db *pgfixture.Database, query string, drop bool) *attributionProxy {
	return newAttributionProxyPhase(t, db, query, drop, false)
}
func newAttributionExecuteProxy(t *testing.T, db *pgfixture.Database, query string, drop bool) *attributionProxy {
	return newAttributionProxyPhase(t, db, query, drop, true)
}
func newAttributionProxyPhase(t *testing.T, db *pgfixture.Database, query string, drop, afterExecute bool) *attributionProxy {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &attributionProxy{listener: l, upstream: net.JoinHostPort("127.0.0.1", db.Fixture.Port), drop: drop, afterExecute: afterExecute, query: query, ready: make(chan struct{}), requested: make(chan struct{}), delivered: make(chan struct{}), ack: make(chan struct{}), quit: make(chan struct{}), conns: map[net.Conn]struct{}{}}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			if !p.add(c) {
				return
			}
			p.wg.Add(1)
			go func() { defer p.wg.Done(); p.serve(c) }()
		}
	}()
	t.Cleanup(p.Close)
	return p
}
func (p *attributionProxy) serve(client net.Conn) {
	defer p.remove(client)
	var h [4]byte
	if _, e := io.ReadFull(client, h[:]); e != nil {
		return
	}
	n := binary.BigEndian.Uint32(h[:])
	if n < 8 || n > 1<<20 {
		return
	}
	start := make([]byte, int(n))
	copy(start, h[:])
	defer clear(start)
	if _, e := io.ReadFull(client, start[4:]); e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	server, e := (&net.Dialer{}).DialContext(ctx, "tcp", p.upstream)
	if e != nil {
		return
	}
	if !p.add(server) {
		return
	}
	defer p.remove(server)
	if e = attributionWrite(server, start); e != nil {
		return
	}
	if binary.BigEndian.Uint32(start[4:8]) == 80877102 {
		p.requestOnce.Do(func() { close(p.requested) })
		var one [1]byte
		_ = server.SetReadDeadline(time.Now().Add(time.Second))
		_, e := server.Read(one[:])
		if errors.Is(e, io.EOF) {
			p.ackSeen.Store(true)
		}
		if !p.drop {
			select {
			case <-p.ack:
			case <-p.quit:
			}
		}
		return
	}
	var armed atomic.Bool
	var skipPrepare atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			b, e := attributionFrame(client)
			if e != nil {
				return
			}
			if b[0] == 'Q' && string(b[5:len(b)-1]) == p.query {
				armed.Store(true)
			}
			if b[0] == 'P' {
				parts := bytes.SplitN(b[5:], []byte{0}, 3)
				if len(parts) >= 2 && string(parts[1]) == p.query {
					armed.Store(true)
					if p.afterExecute {
						skipPrepare.Store(true)
					}
				}
			}
			e = attributionWrite(server, b)
			clear(b)
			if e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			b, e := attributionFrame(server)
			if e != nil {
				return
			}
			if b[0] == 'Z' && skipPrepare.CompareAndSwap(true, false) {
				// This phase lets the real Prepare finish. Its barrier is the
				// following Execute's ReadyForQuery, after command completion.
			} else if b[0] == 'Z' && armed.CompareAndSwap(true, false) {
				p.readyOnce.Do(func() { close(p.ready) })
				if p.drop {
					clear(b)
					return
				}
				select {
				case <-p.requested:
				case <-p.quit:
					return
				}
				if e = attributionWrite(client, b); e != nil {
					return
				}
				clear(b)
				p.deliveredOnce.Do(func() { close(p.delivered) })
				continue
			}
			e = attributionWrite(client, b)
			clear(b)
			if e != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
func (p *attributionProxy) config(t *testing.T, db *pgfixture.Database) postgres.Config {
	t.Helper()
	u, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal(e)
	}
	u.Host = p.listener.Addr().String()
	return db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "MAX_CONNS": "2"})
}

func attributionAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("cancellation attribution protocol boundary timeout")
	}
}
func attributionOwnerUnwatch() bool {
	b := make([]byte, 1<<20)
	n := runtime.Stack(b, true)
	for _, stack := range bytes.Split(b[:n], []byte("\n\n")) {
		if bytes.Contains(stack, []byte("ctxwatch.(*ContextWatcher).Unwatch")) && bytes.Contains(stack, []byte("TestPoolSQLCancellationAttribution")) {
			return true
		}
	}
	return false
}
