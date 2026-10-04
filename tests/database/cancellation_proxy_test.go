//go:build integration

package database_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// The first real CancelRequest can be withheld without changing the data
// connection. The peer read establishes actual EOF, not a sleep or driver flag.
// Cancellation credentials stay in this fixture's memory and are never logged.
type cancellationProxy struct {
	listener          net.Listener
	upstream          string
	hold              bool
	reached           chan struct{}
	inspect           chan chan error
	release           chan struct{}
	quit              chan struct{}
	count             atomic.Int32
	once, releaseOnce sync.Once
	mu                sync.Mutex
	closed            bool
	connections       map[net.Conn]bool
	wg                sync.WaitGroup
}

func newCancellationProxy(t *testing.T, db *pgfixture.Database, hold bool) *cancellationProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &cancellationProxy{listener: l, upstream: net.JoinHostPort("127.0.0.1", db.Fixture.Port), hold: hold, reached: make(chan struct{}), inspect: make(chan chan error), release: make(chan struct{}), quit: make(chan struct{}), connections: map[net.Conn]bool{}}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
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
func (p *cancellationProxy) add(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = c.Close()
		return false
	}
	p.connections[c] = true
	return true
}
func (p *cancellationProxy) remove(c net.Conn) {
	_ = c.Close()
	p.mu.Lock()
	delete(p.connections, c)
	p.mu.Unlock()
}
func (p *cancellationProxy) releaseHeld() { p.releaseOnce.Do(func() { close(p.release) }) }
func (p *cancellationProxy) Close() {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		all := make([]net.Conn, 0, len(p.connections))
		for c := range p.connections {
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
func (p *cancellationProxy) serve(client net.Conn) {
	defer p.remove(client)
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
	defer clear(startup)
	if binary.BigEndian.Uint32(startup[4:8]) == 80877102 {
		if p.count.Add(1) == 1 {
			close(p.reached)
			if p.hold {
				select {
				case response := <-p.inspect:
					_ = client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
					var b [1]byte
					_, err := client.Read(b[:])
					response <- err
					_ = client.SetReadDeadline(time.Time{})
				case <-p.quit:
					return
				}
				select {
				case <-p.release:
				case <-p.quit:
					return
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	server, err := (&net.Dialer{}).DialContext(ctx, "tcp", p.upstream)
	if err != nil {
		return
	}
	if !p.add(server) {
		return
	}
	defer p.remove(server)
	if _, err = server.Write(startup); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(server, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, server); done <- struct{}{} }()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
func (p *cancellationProxy) config(t *testing.T, db *pgfixture.Database, max string) postgres.Config {
	t.Helper()
	u, err := url.Parse(db.Fixture.URL(db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = p.listener.Addr().String()
	return db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "MAX_CONNS": max})
}
func (p *cancellationProxy) controlClosed(t *testing.T) bool {
	t.Helper()
	response := make(chan error, 1)
	select {
	case p.inspect <- response:
	case <-time.After(time.Second):
		t.Fatal("control inspection not reached")
	}
	select {
	case err := <-response:
		return errors.Is(err, io.EOF)
	case <-time.After(time.Second):
		t.Fatal("control inspection did not join")
	}
	return false
}
