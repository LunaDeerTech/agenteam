package accountmail

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
)

// The tests use actual task-owned TCP sockets. The observer sits immediately
// above the socket so a permit released after an upstream buffer write is
// distinguishable from a permit held through the real encrypted socket write.
type independentSMTPWriteObserver struct {
	net.Conn
	write func([]byte) (int, error)
}

func (c independentSMTPWriteObserver) Write(b []byte) (int, error) { return c.write(b) }

func independentSMTPPair(t *testing.T) (*directSMTPConn, net.Conn, context.CancelFunc) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := dialSMTP(ctx, net.DefaultResolver, "127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	peer, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn, peer, cancel
}

func independentSMTPCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned.smtp.example.test"},
		DNSNames:  []string{"owned.smtp.example.test"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

func TestSMTPDirectIndependentPermitCoversActualTLSWrite(t *testing.T) {
	raw, peer, _ := independentSMTPPair(t)
	wire := &smtpWire{directSMTPConn: raw, ctx: raw.ctx}
	certificate, roots := independentSMTPCertificate(t)
	server := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
	client := tls.Client(wire, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "owned.smtp.example.test", RootCAs: roots})
	handshake := make(chan error, 1)
	go func() { handshake <- server.HandshakeContext(raw.ctx) }()
	if err := client.HandshakeContext(raw.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-handshake; err != nil {
		t.Fatal(err)
	}
	for _, phase := range []c.DeliveryPhase{c.SMTPAuth, c.SMTPMail} {
		var released, writes atomic.Int32
		permit, err := c.NewSendPermit(raw.ctx, phase, time.Now().Add(time.Second), func() { released.Add(1) })
		if err != nil {
			t.Fatal(err)
		}
		defer permit.Close()
		command := []byte("AUTH PLAIN owned-test-marker\r\n")
		if phase == c.SMTPMail {
			command = []byte("MAIL FROM:<owned@example.test>\r\n")
		}
		socket := raw.Conn
		raw.Conn = independentSMTPWriteObserver{Conn: socket, write: func(b []byte) (int, error) {
			writes.Add(1)
			if released.Load() != 0 || bytes.Contains(b, command) {
				t.Error("permit was released before actual TLS socket write, or socket received plaintext")
			}
			n, err := socket.Write(b)
			if released.Load() != 0 {
				t.Error("permit was released before actual socket Write returned")
			}
			return n, err
		}}
		wire.permit, wire.armed = permit, true
		if n, err := client.Write(command); err != nil || n != len(command) {
			t.Fatal("protected TLS write failed", n, err)
		}
		if writes.Load() != 1 || released.Load() != 1 || wire.armed {
			t.Fatal("actual write did not consume and release exactly one permit", writes.Load(), released.Load())
		}
		got := make([]byte, len(command))
		if _, err := io.ReadFull(server, got); err != nil || !bytes.Equal(got, command) {
			t.Fatal("owned TLS peer did not receive the protected command", err)
		}
		raw.Conn = socket
	}
}

func TestSMTPDirectIndependentCancelledPermitWaitsForActualWriteReturn(t *testing.T) {
	raw, peer, _ := independentSMTPPair(t)
	permitCtx, cancelPermit := context.WithCancel(raw.ctx)
	defer cancelPermit()
	var released atomic.Int32
	permit, err := c.NewSendPermit(permitCtx, c.SMTPMail, time.Now().Add(time.Second), func() { released.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer permit.Close()
	written, returnWrite := make(chan struct{}), make(chan struct{})
	returned := make(chan error, 1)
	socket := raw.Conn
	raw.Conn = independentSMTPWriteObserver{Conn: socket, write: func(b []byte) (int, error) {
		n, err := socket.Write(b)
		close(written)
		<-returnWrite
		return n, err
	}}
	wire := &smtpWire{directSMTPConn: raw, ctx: raw.ctx, permit: permit, armed: true}
	go func() { _, err := wire.Write([]byte("MAIL FROM:<owned@example.test>\r\n")); returned <- err }()
	select {
	case <-written:
	case <-raw.ctx.Done():
		close(returnWrite)
		t.Fatal("owned socket write did not occur")
	}
	cancelPermit()
	// EOF proves the permit's cancellation callback actually closed the socket.
	if got, err := io.ReadAll(peer); err != nil || string(got) != "MAIL FROM:<owned@example.test>\r\n" {
		close(returnWrite)
		t.Fatal("permit cancellation did not close the owned socket", err)
	}
	if released.Load() != 0 {
		t.Error("cancelled permit released while its real socket Write was still active")
	}
	select {
	case <-returned:
		t.Error("write returned before its actual I/O callback joined")
	default:
	}
	close(returnWrite)
	select {
	case <-returned:
	case <-raw.ctx.Done():
		t.Fatal("actual socket Write did not return")
	}
	if released.Load() != 1 {
		t.Fatal("permit did not release once after actual Write returned", released.Load())
	}
}

func TestSMTPDirectIndependentCancellationClosesAndJoinsRealSocket(t *testing.T) {
	conn, other, cancel := independentSMTPPair(t)
	// Use the production dial path with a cancellable context, then observe both
	// the caller's blocked read and the remote socket rather than only ctx.Err.
	if err := other.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { var b [1]byte; _, err := conn.Read(b[:]); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation reason was lost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled SMTP read remained active")
	}
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	select {
	case <-conn.cancelled:
	default:
		t.Fatal("Close returned before its active cancellation callback joined")
	}
	var b [1]byte
	if n, err := other.Read(b[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatal("owned peer did not observe actual socket closure", n, err)
	}
}
