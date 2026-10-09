package pending_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type pendingBody struct {
	io.Reader
	closes     int
	closeError error
}

func (b *pendingBody) Close() error { b.closes++; return b.closeError }

type failedPendingRead struct{}

func (failedPendingRead) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestPendingBodyBounds(t *testing.T) {
	for _, c := range []struct {
		name     string
		length   int
		declared int64
		want     bool
	}{
		{"original", 7, 7, true},
		{"maximum", p.MaxDeviceBodyBytes, p.MaxDeviceBodyBytes, true},
		{"oversize", p.MaxDeviceBodyBytes + 1, p.MaxDeviceBodyBytes + 1, false},
		{"shorter_than_declared", 7, 8, false},
		{"longer_than_declared", 7, 6, false},
		{"unknown_length", 7, -1, false},
		{"empty", 0, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			original := bytes.Repeat([]byte{'q'}, c.length)
			body := &pendingBody{Reader: bytes.NewReader(original)}
			request := &http.Request{Body: body, ContentLength: c.declared}
			if runnerCrashPendingBody(request) != c.want || body.closes != 1 {
				t.Fatal("original request EOF/length/Close not enforced")
			}
			if !bytes.Equal(original, bytes.Repeat([]byte{'q'}, c.length)) {
				t.Fatal("observer changed caller's original bytes")
			}
		})
	}
	t.Run("read_failure", func(t *testing.T) {
		body := &pendingBody{Reader: failedPendingRead{}}
		if runnerCrashPendingBody(&http.Request{Body: body, ContentLength: 7}) || body.closes != 1 {
			t.Fatal("failed body read admitted checkpoint")
		}
	})
	t.Run("close_failure", func(t *testing.T) {
		body := &pendingBody{Reader: bytes.NewReader([]byte("payload")), closeError: errors.New("controlled close failure")}
		if runnerCrashPendingBody(&http.Request{Body: body, ContentLength: 7}) || body.closes != 1 {
			t.Fatal("failed original Close admitted checkpoint")
		}
	})
}
