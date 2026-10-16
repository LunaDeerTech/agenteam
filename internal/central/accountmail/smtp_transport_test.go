package accountmail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type ownedSMTPResolver struct{}

func (ownedSMTPResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if host != "owned.smtp.example.test" {
		return nil, errors.New("unexpected owned SMTP hostname")
	}
	return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, ctx.Err()
}

func TestSMTPDirectIgnoresProxyEnvironment(t *testing.T) {
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var proxyConnections atomic.Int32
	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		for {
			conn, err := proxy.Accept()
			if err != nil {
				return
			}
			proxyConnections.Add(1)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = proxy.Close(); <-proxyDone })
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(key, "http://"+proxy.Addr().String())
	}
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(key, "")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A hostname, rather than a loopback literal, prevents Go's automatic
	// localhost proxy exemption from making an accidental env proxy pass.
	client, err := dialSMTP(ctx, ownedSMTPResolver{}, "owned.smtp.example.test", listener.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := server.Write([]byte("220 owned SMTP direct transport\r\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(client).ReadString('\n')
	if err != nil || line != "220 owned SMTP direct transport\r\n" {
		t.Fatal("direct SMTP peer was not reached", err)
	}
	_ = client.Close()
	_ = proxy.Close()
	<-proxyDone
	if proxyConnections.Load() != 0 {
		t.Fatal("SMTP consulted a proxy environment variable")
	}
}
