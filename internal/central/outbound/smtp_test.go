package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

func TestSMTPProxyOwnedTLSAndBeginSendRouteRefresh(t *testing.T) {
	certificate, trust := proxyTestCertificate(t, "smtp.example")
	var auths, connects atomic.Int32
	target := proxyTestStartGateway(t, func(raw net.Conn) {
		conn := tls.Server(raw, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		defer conn.Close()
		_, _ = io.WriteString(conn, "220 owned SMTP\r\n")
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil || line != "EHLO owned.example\r\n" {
			t.Error("owned SMTP setup missing")
			return
		}
		_, _ = io.WriteString(conn, "250 owned.example\r\n")
		line, err = reader.ReadString('\n')
		if err != nil || line != "AUTH owned-marker\r\n" {
			t.Error("owned SMTP AUTH missing")
			return
		}
		auths.Add(1)
		_, _ = io.WriteString(conn, "235 authenticated\r\n")
		if line, err = reader.ReadString('\n'); err == nil {
			t.Error("SMTP old route received another business command")
		}
	})
	gateway := proxyTestForwardHTTP(t, target, "", &connects)
	client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr(proxyTestPrivateIP(t))}}, gateway)
	proxyTestRuleFor(t, state, target)
	profile, err := NewSMTPProfile(CallContext{data: func() callData { return callData{} }}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	conn, err := client.DialTarget(ctx, "smtp.example", uint16(target.listener.Addr().(*net.TCPAddr).Port), profile)
	if err != nil {
		t.Fatal("SMTP tunnel:", err)
	}
	defer conn.Close()
	config, err := trust.SMTPClientTLSConfig("smtp.example")
	if err != nil {
		t.Fatal(err)
	}
	secured := tls.Client(conn, config)
	defer secured.Close()
	reader := bufio.NewReader(secured)
	if _, err = reader.ReadString('\n'); err != nil {
		t.Fatal("SMTP greeting:", err)
	}
	if _, err = io.WriteString(secured, "EHLO owned.example\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if err = conn.BeginSend(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(secured, "AUTH owned-marker\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if !conn.Decision().Sent {
		t.Fatal("SMTP auth send fact lost")
	}
	if err = conn.EndSend(); err != nil {
		t.Fatal(err)
	}
	proxyTestPublish(t, state, func(p *proxySnapshot) { p.mode = "direct" })
	err = conn.BeginSend(ctx)
	if err == nil || SafeNetworkError(err).Decision().Reason != ac.PolicyUnavailable || SafeNetworkError(err).Decision().Sent {
		t.Fatal("SMTP BeginSend reused stale route", err)
	}
	if auths.Load() != 1 || connects.Load() != 1 {
		t.Fatal("SMTP tunnel transfer count mismatch")
	}
}

func TestSMTPProxyCoalescedGreeting(t *testing.T) {
	_, trust := proxyTestCertificate(t, "smtp.example")
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		defer request.Body.Close()
		if request.RequestURI != "8.8.8.8:2525" {
			t.Error("SMTP numeric CONNECT changed")
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n\r\n220 owned greeting\r\n")
		_, _ = io.Copy(io.Discard, conn)
	})
	client, _ := proxyTestClient(t, trust, &proxyTestResolver{}, gateway)
	profile, _ := NewSMTPProfile(CallContext{data: func() callData { return callData{} }}, Limits{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := client.DialTarget(ctx, "smtp.example", 2525, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "220 owned greeting\r\n" {
		t.Fatal("coalesced SMTP greeting lost")
	}
}
