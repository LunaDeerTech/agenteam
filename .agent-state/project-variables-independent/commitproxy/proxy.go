// Package commitproxy supplies an owned complete-frame PostgreSQL stimulus for
// independent tests. It never parses or emits credentials, SQL bodies or DSNs.
package commitproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Proxy struct {
	listener                                              net.Listener
	upstream                                              string
	mu                                                    sync.Mutex
	connections                                           map[net.Conn]struct{}
	closed                                                bool
	workers                                               sync.WaitGroup
	target, writer                                        atomic.Int32
	reached, release, committed, heldJoined, quit, joined chan struct{}
	closeOnce, releaseOnce                                sync.Once
}

func New(ctx context.Context, upstream string) (*Proxy, error) {
	l, e := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	p := &Proxy{listener: l, upstream: upstream, connections: map[net.Conn]struct{}{}, reached: make(chan struct{}), release: make(chan struct{}), committed: make(chan struct{}), heldJoined: make(chan struct{}), quit: make(chan struct{}), joined: make(chan struct{})}
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			if !p.own(c) {
				_ = c.Close()
				return
			}
			p.workers.Add(1)
			go p.serve(c)
		}
	}()
	return p, nil
}
func (p *Proxy) Address() string { return p.listener.Addr().String() }
func (p *Proxy) Arm(pid int32) error {
	if pid <= 0 || p.writer.Load() != 0 || !p.target.CompareAndSwap(0, pid) {
		return errors.New("invalid commit target")
	}
	return nil
}
func (p *Proxy) Reached() <-chan struct{}    { return p.reached }
func (p *Proxy) Committed() <-chan struct{}  { return p.committed }
func (p *Proxy) HeldJoined() <-chan struct{} { return p.heldJoined }
func (p *Proxy) WriterPID() int32            { return p.writer.Load() }
func (p *Proxy) Release()                    { p.releaseOnce.Do(func() { close(p.release) }) }
func (p *Proxy) Close(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.quit)
		_ = p.listener.Close()
		for c := range p.connections {
			_ = c.Close()
		}
		p.mu.Unlock()
		go func() { p.workers.Wait(); close(p.joined) }()
	})
	select {
	case <-p.joined:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *Proxy) own(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.connections[c] = struct{}{}
	return true
}
func (p *Proxy) retire(c net.Conn) {
	_ = c.Close()
	p.mu.Lock()
	delete(p.connections, c)
	p.mu.Unlock()
}
func (p *Proxy) serve(client net.Conn) {
	defer p.workers.Done()
	defer p.retire(client)
	server, e := net.DialTimeout("tcp", p.upstream, 3*time.Second)
	if e != nil {
		return
	}
	if !p.own(server) {
		_ = server.Close()
		return
	}
	defer p.retire(server)
	var header [4]byte
	if _, e = io.ReadFull(client, header[:]); e != nil {
		return
	}
	n := binary.BigEndian.Uint32(header[:])
	if n < 8 || n > 1<<20 {
		return
	}
	startup := make([]byte, int(n))
	copy(startup, header[:])
	if _, e = io.ReadFull(client, startup[4:]); e != nil {
		clear(startup)
		return
	}
	e = write(server, startup)
	clear(startup)
	if e != nil {
		return
	}
	var backend atomic.Int32
	var held atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := read(client)
			if e != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && backend.Load() > 0 && p.target.CompareAndSwap(backend.Load(), 0) {
				held.Store(true)
				p.writer.Store(backend.Load())
				_ = client.Close()
				close(p.reached)
				select {
				case <-p.release:
				case <-p.quit:
					clear(frame)
					return
				}
				e = write(server, frame)
				clear(frame)
				if e != nil {
					return
				}
				select {
				case <-p.committed:
				case <-p.quit:
				}
				return
			}
			e = write(server, frame)
			clear(frame)
			if e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := read(server)
			if e != nil {
				return
			}
			if frame[0] == 'K' && len(frame) == 13 {
				backend.Store(int32(binary.BigEndian.Uint32(frame[5:9])))
			}
			if held.Load() {
				if frame[0] != 'C' || string(frame[5:]) != "COMMIT\x00" {
					clear(frame)
					return
				}
				clear(frame)
				ready, e := read(server)
				valid := e == nil && len(ready) == 6 && ready[0] == 'Z' && ready[5] == 'I'
				clear(ready)
				if valid {
					close(p.committed)
				}
				return
			}
			e = write(client, frame)
			clear(frame)
			if e != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
	if held.Load() {
		p.retire(client)
		p.retire(server)
		close(p.heldJoined)
	}
}
func read(r io.Reader) ([]byte, error) {
	var header [5]byte
	if _, e := io.ReadFull(r, header[:]); e != nil {
		return nil, e
	}
	n := binary.BigEndian.Uint32(header[1:])
	if n < 4 || n > 1<<20 {
		return nil, errors.New("invalid PostgreSQL frame")
	}
	b := make([]byte, int(n)+1)
	copy(b, header[:])
	_, e := io.ReadFull(r, b[5:])
	if e != nil {
		clear(b)
		return nil, e
	}
	return b, nil
}
func write(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
