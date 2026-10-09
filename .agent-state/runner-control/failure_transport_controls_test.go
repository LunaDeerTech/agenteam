package process_test

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The first case is the independent Work fixed-Go counterexample, extended
// here to require the actual C observer to keep the held owner outstanding.
type failureControlConn struct {
	closed, readReturned chan struct{}
	readRelease          <-chan struct{}
	once                 sync.Once
}

func (c *failureControlConn) Read([]byte) (int, error) {
	<-c.closed
	if c.readRelease != nil {
		<-c.readRelease
	}
	close(c.readReturned)
	return 0, net.ErrClosed
}
func (*failureControlConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *failureControlConn) Close() error                   { c.once.Do(func() { close(c.closed) }); return nil }
func (*failureControlConn) LocalAddr() net.Addr              { return nil }
func (*failureControlConn) RemoteAddr() net.Addr             { return nil }
func (*failureControlConn) SetDeadline(time.Time) error      { return nil }
func (*failureControlConn) SetReadDeadline(time.Time) error  { return nil }
func (*failureControlConn) SetWriteDeadline(time.Time) error { return nil }

type failureControlBackend struct {
	entered, release, returned chan struct{}
}

func (*failureControlBackend) Read([]byte) (int, error) {
	return 0, errors.New("controlled read failure")
}
func (*failureControlBackend) Write(p []byte) (int, error) { return len(p), nil }
func (b *failureControlBackend) Close() error {
	close(b.entered)
	<-b.release
	close(b.returned)
	return nil
}

type failureControlWriter struct {
	conn   net.Conn
	header http.Header
}

func (w *failureControlWriter) Header() http.Header       { return w.header }
func (*failureControlWriter) WriteHeader(int)             {}
func (*failureControlWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *failureControlWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

type failureRoundTrip func(*http.Request) (*http.Response, error)

func (f failureRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func failureAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("controlled original call did not return")
	}
}

func failureEventually(t *testing.T, check func() bool) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !check() {
		select {
		case <-timer.C:
			t.Fatal("controlled original owner did not retire")
		case <-tick.C:
		}
	}
}

func TestFailureTransportActualProxyOwners(t *testing.T) {
	for _, held := range []string{"backend_close", "downstream_copier"} {
		t.Run(held, func(t *testing.T) {
			backend := &failureControlBackend{make(chan struct{}), make(chan struct{}), make(chan struct{})}
			readRelease := make(chan struct{})
			client := &failureControlConn{closed: make(chan struct{}), readReturned: make(chan struct{})}
			if held == "downstream_copier" {
				client.readRelease = readRelease
			}
			var release, readOnce sync.Once
			defer release.Do(func() { close(backend.release) })
			defer readOnce.Do(func() { close(readRelease) })
			defer client.Close()
			v := &runnerFailureTransport{}
			writer := &runnerFailureWriter{ResponseWriter: &failureControlWriter{client, make(http.Header)}}
			target, _ := url.Parse("http://owned.invalid")
			proxy := &httputil.ReverseProxy{
				Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(target) },
				Transport: failureRoundTrip(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 101, Header: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}}, Body: backend, Request: r}, nil
				}),
				ModifyResponse: func(r *http.Response) error { return v.observeUpgrade(writer, r) },
				ErrorLog:       log.New(io.Discard, "", 0),
			}
			request, _ := http.NewRequest("GET", "http://owned.invalid/api/v1/runner/control", nil)
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			done := make(chan struct{})
			go func() {
				defer close(done)
				v.requests.Add(1)
				defer v.requests.Add(-1)
				v.wss.Add(1)
				defer v.wss.Add(-1)
				proxy.ServeHTTP(writer, request)
			}()
			failureAwait(t, done)
			failureAwait(t, backend.entered)
			if held == "downstream_copier" {
				release.Do(func() { close(backend.release) })
				failureAwait(t, backend.returned)
				failureEventually(t, func() bool { _, _, closes, _ := v.upgradeRemainder(); return closes == 0 })
			} else {
				failureAwait(t, client.readReturned)
			}
			owners, copies, closes, _ := v.upgradeRemainder()
			if v.requests.Load() != 0 || v.wss.Load() != 0 || owners != 1 || held == "backend_close" && closes == 0 || held == "downstream_copier" && copies == 0 {
				t.Fatal("returned original handler hid the held Close or copier")
			}
			release.Do(func() { close(backend.release) })
			readOnce.Do(func() { close(readRelease) })
			failureAwait(t, backend.returned)
			failureAwait(t, client.readReturned)
			failureEventually(t, func() bool { owners, _, _, _ := v.upgradeRemainder(); return owners == 0 })
		})
	}
}

type failureConcurrentClose struct {
	calls            atomic.Int32
	entered, release chan struct{}
}

func (*failureConcurrentClose) Read([]byte) (int, error)    { return 0, io.EOF }
func (*failureConcurrentClose) Write(p []byte) (int, error) { return len(p), nil }
func (c *failureConcurrentClose) Close() error {
	if c.calls.Add(1) == 1 {
		close(c.entered)
		<-c.release
		return nil
	}
	return net.ErrClosed
}

func TestFailureTransportConcurrentCloseCannotRetireFirst(t *testing.T) {
	u := &runnerFailureUpgrade{copiesStarted: 2, copiesDone: 2}
	c := &failureConcurrentClose{entered: make(chan struct{}), release: make(chan struct{})}
	s := &runnerFailureStream{original: c, owner: u}
	u.streams = [2]*runnerFailureStream{s, {owner: u, closed: true}}
	v := &runnerFailureTransport{upgrades: []*runnerFailureUpgrade{u}}
	done := make(chan struct{})
	var release sync.Once
	defer func() {
		release.Do(func() { close(c.release) })
		failureAwait(t, done)
	}()
	go func() { defer close(done); _ = s.Close() }()
	failureAwait(t, c.entered)
	if !errors.Is(s.Close(), net.ErrClosed) {
		t.Fatal("original second Close error changed")
	}
	if owners, _, closes, _ := v.upgradeRemainder(); owners != 1 || closes != 1 {
		t.Fatal("second Close retired the original held Close")
	}
	release.Do(func() { close(c.release) })
	failureAwait(t, done)
	if owners, _, _, _ := v.upgradeRemainder(); owners != 0 {
		t.Fatal("actual first Close return not observed")
	}
}

type failurePlain struct{ io.Reader }

func (*failurePlain) Write(p []byte) (int, error) { return len(p), nil }
func (*failurePlain) Close() error                { return nil }

type failureWriterTo struct {
	*failurePlain
	want  io.Writer
	calls int
}

func (c *failureWriterTo) WriteTo(w io.Writer) (int64, error) {
	c.calls++
	if w != c.want {
		return 0, errors.New("changed original destination")
	}
	return 7, io.ErrUnexpectedEOF
}

type failureReaderFrom struct {
	*failurePlain
	want  io.Reader
	calls int
}

func (c *failureReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	c.calls++
	if r != c.want {
		return 0, errors.New("changed original source")
	}
	return 9, io.ErrUnexpectedEOF
}

type failureEOF struct{}

func (failureEOF) Read([]byte) (int, error) { return 0, io.EOF }

func TestFailureTransportCopyDispatchAndHalfClose(t *testing.T) {
	t.Run("original_writer_to", func(t *testing.T) {
		u := &runnerFailureUpgrade{}
		dest := &failurePlain{failureEOF{}}
		source := &failureWriterTo{failurePlain: &failurePlain{failureEOF{}}, want: dest}
		s := &runnerFailureStream{original: source, owner: u}
		d := &runnerFailureStream{original: dest, owner: u}
		if n, err := io.Copy(d, s); n != 7 || !errors.Is(err, io.ErrUnexpectedEOF) || source.calls != 1 {
			t.Fatal("original WriterTo dispatch/return changed")
		}
		if _, ok := interface{}(d).(interface{ CloseWrite() error }); ok {
			t.Fatal("invented missing CloseWrite capability")
		}
	})
	t.Run("original_reader_from", func(t *testing.T) {
		u := &runnerFailureUpgrade{}
		source := &failurePlain{failureEOF{}}
		dest := &failureReaderFrom{failurePlain: &failurePlain{failureEOF{}}, want: source}
		s := &runnerFailureStream{original: source, owner: u}
		d := &runnerFailureStream{original: dest, owner: u}
		if n, err := io.Copy(d, s); n != 9 || !errors.Is(err, io.ErrUnexpectedEOF) || dest.calls != 1 {
			t.Fatal("original ReaderFrom dispatch/return changed")
		}
	})
	t.Run("scheduled_half_close", func(t *testing.T) {
		u := &runnerFailureUpgrade{copiesStarted: 1, copiesDone: 1}
		s := &runnerFailureStream{original: &failurePlain{failureEOF{}}, owner: u, closed: true}
		d := &runnerFailureStream{original: &failurePlain{failureEOF{}}, owner: u, closed: true}
		u.streams = [2]*runnerFailureStream{s, d}
		var halfCalls int
		half := &runnerFailureHalfStream{runnerFailureStream: d, half: func() error { halfCalls++; return io.ErrClosedPipe }}
		if n, err := io.Copy(half, s); n != 0 || err != nil {
			t.Fatal("original EOF changed")
		}
		v := &runnerFailureTransport{upgrades: []*runnerFailureUpgrade{u}}
		if owners, _, _, halves := v.upgradeRemainder(); owners != 1 || halves != 1 {
			t.Fatal("copy return skipped its queued original CloseWrite")
		}
		if !errors.Is(half.CloseWrite(), io.ErrClosedPipe) || halfCalls != 1 {
			t.Fatal("original CloseWrite error/call changed")
		}
		if owners, _, _, _ := v.upgradeRemainder(); owners != 0 {
			t.Fatal("actual half Close return not observed")
		}
	})
}
