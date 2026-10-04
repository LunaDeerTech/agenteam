//go:build integration

package objects_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// The wrapper only observes a real TCP Write. All bytes/deadlines/close still
// use the accepted socket; no artificial blocked ResponseWriter is involved.
type measuredDownloadConn struct {
	net.Conn
	active  atomic.Int64
	once    sync.Once
	entered chan struct{}
}

func (c *measuredDownloadConn) Write(b []byte) (int, error) {
	c.active.Add(1)
	defer c.active.Add(-1)
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(b)
}

type measuredDownloadListener struct {
	net.Listener
	accepted chan *measuredDownloadConn
}

func (l *measuredDownloadListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if err = c.(*net.TCPConn).SetWriteBuffer(1024); err != nil {
		c.Close()
		return nil, err
	}
	m := &measuredDownloadConn{Conn: c, entered: make(chan struct{})}
	l.accepted <- m
	return m, nil
}

func TestArtifactDownloadForceInterruptsActualBlockedHTTPWrite(t *testing.T) {
	f := newArtifactFixture(t)
	m := f.create(t, "blocked-http", "text/plain", strings.Repeat("x", 1<<20))
	d := f.downloads(t, f.sources)
	token := f.downloadToken(t, d, m, oc.DownloadAttachment)
	done := make(chan downloaded, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := d.OpenDownload(r.Context(), f.actor, token, "")
		if err != nil {
			done <- downloaded{err: err}
			return
		}
		defer stream.Close()
		writer := &downloadWriter{ResponseWriter: w}
		result, err := stream.StreamHTTP(writer)
		done <- downloaded{result, err, writer.accepted}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	}))
	listener := &measuredDownloadListener{Listener: server.Listener, accepted: make(chan *measuredDownloadConn, 1)}
	server.Listener = listener
	server.Start()
	defer server.Close()
	client, err := net.DialTCP("tcp", nil, server.Listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err = client.SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	if err = client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// Send a real GET, then never read the body so the small TCP window fills.
	if _, err = io.WriteString(client, "GET /download HTTP/1.1\r\nHost: fixture\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal("owned HTTP request failed")
	}
	var socket *measuredDownloadConn
	select {
	case socket = <-listener.accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not accept actual socket")
	}
	select {
	case <-socket.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not write actual socket")
	}
	f.objects.StopAdmission()
	short, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	err = f.objects.Drain(short)
	cancel()
	if err == nil || socket.active.Load() != 1 {
		t.Fatal("fixture did not retain a blocked actual TCP write", err, socket.active.Load())
	}
	select {
	case <-done:
		t.Fatal("HTTP completed before force despite stopped client")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if err = f.objects.Force(ctx); err != nil || time.Since(started) > 1100*time.Millisecond {
		t.Fatal("HTTP force exceeded shared deadline", err)
	}
	select {
	case out := <-done:
		if out.err == nil || out.result.Phase != oc.DownloadFailed || int64(out.result.SentBytes) != out.accepted || out.accepted >= 1<<20 {
			t.Fatal("blocked HTTP byte accounting", out.result, out.accepted)
		}
	case <-ctx.Done():
		t.Fatal("actual HTTP writer did not join")
	}
	if socket.active.Load() != 0 {
		t.Fatal("actual TCP Write remains active")
	}
	var active int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active'`, m.Details().Object.ID.String()).Scan(&active); err != nil || active != 0 {
		t.Fatal("forced writer released before I/O join", active, err)
	}
}
