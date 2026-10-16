package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

const capturedTestMessage = "From: sender@example.test\r\nTo: reader@example.test\r\nMessage-ID: <owned-fixture@example.test>\r\n\r\nprivate-link-sentinel\r\n"

// Drive actual DATA bytes and the final reply through a socket. Waiting for
// serve to return distinguishes an acknowledged message from merely reading
// DATA, including the peer disappearing before the final reply is written.
func submitMessage(t *testing.T, c *scenario, message string, readReply bool) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	if err := clientConn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal("fixture socket deadline failed")
	}
	done := make(chan struct{})
	go func() { c.serve(serverConn); close(done) }()
	reader := bufio.NewReader(clientConn)
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "220 ") {
		t.Fatal("fixture greeting failed")
	}
	if _, err := io.WriteString(clientConn, "DATA\r\n"); err != nil {
		t.Fatal("fixture DATA command failed")
	}
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "354 ") {
		t.Fatal("fixture DATA admission failed")
	}
	if _, err := io.WriteString(clientConn, message+".\r\n"); err != nil {
		t.Fatal("fixture DATA body failed")
	}
	if readReply {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal("fixture final reply failed")
		}
	}
	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture SMTP handler did not return")
	}
}

func TestMessageCaptureRequiresOptInAndSuccessfulFinalReply(t *testing.T) {
	for _, tc := range []struct {
		name      string
		capture   bool
		code      int
		readReply bool
		want      bool
	}{
		{"default discards body", false, 250, true, false},
		{"successful accepted body", true, 250, true, true},
		{"rejected DATA", true, 550, true, false},
		{"lost final reply", true, 250, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &scenario{config: f.Scenario{Mode: "none", DataCode: tc.code, CaptureMessage: tc.capture}, release: make(chan struct{})}
			submitMessage(t, c, capturedTestMessage, tc.readReply)
			if tc.want {
				if !bytes.Equal(c.message, []byte(capturedTestMessage)) {
					t.Fatal("accepted message was not captured exactly")
				}
				previous := c.message
				submitMessage(t, c, "Subject: replacement\r\n\r\nsecond message\r\n", true)
				if !bytes.Equal(previous, make([]byte, len(previous))) || bytes.Contains(c.message, []byte("private-link-sentinel")) {
					t.Fatal("previous capture was not erased when replaced")
				}
			} else if len(c.message) != 0 {
				t.Fatal("an unaccepted or non-opted-in message was retained")
			}
		})
	}
}

func TestPrivateMessageControlRequiresNonceAndOptIn(t *testing.T) {
	const nonce = "11111111111111111111111111111111"
	const id = "22222222222222222222222222222222"
	c := &scenario{config: f.Scenario{CaptureMessage: true}, message: []byte(capturedTestMessage)}
	s := &server{nonce: nonce, cases: map[string]*scenario{id: c}}
	for _, tc := range []struct {
		name   string
		nonces []string
		want   int
	}{
		{"missing", nil, http.StatusForbidden},
		{"wrong", []string{"wrong"}, http.StatusForbidden},
		{"duplicate", []string{nonce, nonce}, http.StatusForbidden},
		{"owned", []string{nonce}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/message/"+id, nil)
			for _, value := range tc.nonces {
				r.Header.Add("X-Fixture-Nonce", value)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("control status=%d want=%d", w.Code, tc.want)
			}
			if tc.want == http.StatusOK {
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "message/rfc822" || !bytes.Equal(w.Body.Bytes(), []byte(capturedTestMessage)) {
					t.Fatal("owned message response did not preserve its private contract")
				}
			} else if w.Body.Len() != 0 {
				t.Fatal("unauthorized control returned body content")
			}
		})
	}
	for _, path := range []string{"/state/" + id, "/message/" + id} {
		c.config.CaptureMessage = false
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-Fixture-Nonce", nonce)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if bytes.Contains(w.Body.Bytes(), []byte("private-link-sentinel")) {
			t.Fatal("ordinary state or non-opted-in endpoint exposed message bytes")
		}
		if strings.HasPrefix(path, "/message/") && w.Code != http.StatusNotFound {
			t.Fatal("non-opted-in message endpoint was enabled")
		}
	}
}
