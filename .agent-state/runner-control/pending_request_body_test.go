// Fixed Go HTTP/1.1 cancellation control over TLS and in-memory net.Pipe only.
// No listener socket, database, Runner process, or product handler is started.
package pending_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryListener struct {
	conn     net.Conn
	mu       sync.Mutex
	accepted bool
	closed   chan struct{}
	once     sync.Once
}

func (l *memoryListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.accepted {
		l.accepted = true
		l.mu.Unlock()
		return l.conn, nil
	}
	l.mu.Unlock()
	<-l.closed
	return nil, net.ErrClosed
}
func (l *memoryListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *memoryListener) Addr() net.Addr { return l.conn.LocalAddr() }

func pendingTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("owned memory TLS key")
	}
	t.Cleanup(func() { clear(private) })
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned.invalid"}, DNSNames: []string{"owned.invalid"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal("owned memory TLS certificate")
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal("owned memory TLS roots")
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}}, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "owned.invalid"}
}

func pendingAwait(t *testing.T, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal(label)
	}
}

func pendingCancellation(t *testing.T, consume bool) {
	t.Helper()
	serverEnd, clientEnd := net.Pipe()
	listener := &memoryListener{conn: serverEnd, closed: make(chan struct{})}
	serverTLS, clientTLS := pendingTLS(t)
	checkpoint := make(chan context.Context, 1)
	abort := make(chan struct{})
	handlerDone := make(chan struct{})
	bodyDone := make(chan bool, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		if consume {
			n, err := io.Copy(io.Discard, io.LimitReader(r.Body, 1025))
			closeErr := r.Body.Close()
			bodyDone <- err == nil && closeErr == nil && n == r.ContentLength && n == 7
		}
		checkpoint <- r.Context()
		select {
		case <-r.Context().Done():
		case <-abort:
		}
	})}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(tls.NewListener(listener, serverTLS)) }()
	defer func() {
		close(abort)
		_ = clientEnd.Close()
		_ = serverEnd.Close()
		_ = server.Close()
		pendingAwait(t, handlerDone, "controlled original handler failed to return during cleanup")
		select {
		case err := <-serveDone:
			if !errors.Is(err, http.ErrServerClosed) {
				t.Error("original Serve returned unexpected status")
			}
		case <-time.After(time.Second):
			t.Error("original Serve did not actually return")
		}
	}()
	client := tls.Client(clientEnd, clientTLS)
	if client.SetDeadline(time.Now().Add(3*time.Second)) != nil {
		t.Fatal("owned memory client deadline")
	}
	request := "POST /api/v1/runner/enroll HTTP/1.1\r\nHost: owned.invalid\r\nContent-Length: 7\r\n\r\npayload"
	if n, err := io.Copy(client, strings.NewReader(request)); err != nil || n != int64(len(request)) {
		t.Fatal("actual memory TLS request write")
	}
	var requestContext context.Context
	select {
	case requestContext = <-checkpoint:
	case <-time.After(time.Second):
		t.Fatal("original held handler did not reach checkpoint")
	}
	if consume && !<-bodyDone {
		t.Fatal("original request body did not reach complete EOF/Close")
	}
	if requestContext.Err() != nil {
		t.Fatal("request cancelled before original transport loss")
	}
	// Closing the original underlying peer omits TLS close-notify, as an
	// unexpected process death does. The server/body owns cancellation detection.
	if clientEnd.Close() != nil {
		t.Fatal("original physical peer close")
	}
	pendingAwait(t, requestContext.Done(), "original pending request cancellation was not observed without request-body EOF")
	pendingAwait(t, handlerDone, "cancelled original handler did not actually return")
}

func TestPendingCancellationWithoutBodyEOF(t *testing.T) { pendingCancellation(t, false) }
func TestPendingCancellationAfterBodyEOF(t *testing.T)   { pendingCancellation(t, true) }
