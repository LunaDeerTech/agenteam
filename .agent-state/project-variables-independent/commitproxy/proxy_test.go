package commitproxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestFramesRequireCompleteBoundedMessages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"complete-commit", append([]byte{'Q', 0, 0, 0, 11}, []byte("COMMIT\x00")...), true},
		{"short-header", []byte{'Q', 0, 0}, false},
		{"short-body", []byte{'Q', 0, 0, 0, 11, 'C'}, false},
		{"invalid-length", []byte{'Q', 0, 0, 0, 3}, false},
		{"over-bound", []byte{'Q', 0, 16, 0, 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := read(bytes.NewReader(tc.data))
			if tc.valid {
				if e != nil || !bytes.Equal(got, tc.data) {
					t.Fatal("frame changed")
				}
			} else if e == nil || got != nil {
				t.Fatal("incomplete frame admitted")
			}
		})
	}
	var exact [5]byte
	exact[0] = 'Z'
	binary.BigEndian.PutUint32(exact[1:], 5)
	got, e := read(bytes.NewReader(append(exact[:], 'I')))
	if e != nil || len(got) != 6 {
		t.Fatal("actual ReadyForQuery shape")
	}
}

type shortWriter struct {
	bytes.Buffer
	zero bool
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if w.zero {
		return 0, nil
	}
	if len(p) > 2 {
		p = p[:2]
	}
	return w.Buffer.Write(p)
}
func TestForwardingPreservesBytesAndRejectsNoProgress(t *testing.T) {
	w := &shortWriter{}
	input := []byte("frame without credential material")
	if e := write(w, input); e != nil || !bytes.Equal(w.Bytes(), input) {
		t.Fatal("short-write forwarding incomplete")
	}
	if e := write(&shortWriter{zero: true}, input); !errors.Is(e, io.ErrShortWrite) {
		t.Fatal("zero-write spun or passed")
	}
}

type closedListener struct{}

func (closedListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (closedListener) Close() error              { return nil }
func (closedListener) Addr() net.Addr            { return nil }
func TestCloseReportsActualWorkerJoinNotCancellation(t *testing.T) {
	// net.Pipe is memory-only. No socket, listener or database is started.
	a, b := net.Pipe()
	defer b.Close()
	p := &Proxy{listener: closedListener{}, connections: map[net.Conn]struct{}{a: {}}, quit: make(chan struct{}), joined: make(chan struct{})}
	returned := make(chan struct{})
	release := make(chan struct{})
	p.workers.Add(1)
	go func() { defer p.workers.Done(); defer close(returned); _, _ = a.Read(make([]byte, 1)); <-release }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := p.Close(ctx); !errors.Is(e, context.Canceled) {
		close(release)
		t.Fatal("pending worker falsely joined")
	}
	select {
	case <-returned:
		close(release)
		t.Fatal("unreleased worker returned")
	default:
	}
	close(release)
	joined, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if e := p.Close(joined); e != nil {
		t.Fatal("actual worker failed join")
	}
	select {
	case <-returned:
	default:
		t.Fatal("join marker preceded actual worker")
	}
}
