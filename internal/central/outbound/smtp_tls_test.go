package outbound

import (
	"crypto/tls"
	"testing"
)

func TestSMTPTrustedTLSConfigCopiesAndValidates(t *testing.T) {
	s, e := LoadTrustStore("")
	if e != nil {
		t.Fatal(e)
	}
	for _, host := range []string{"smtp.example.com", "192.0.2.1", "2001:db8::1"} {
		c, e := s.SMTPClientTLSConfig(host)
		if e != nil {
			t.Fatal(e)
		}
		if c.ServerName != host || c.MinVersion != tls.VersionTLS12 || c.InsecureSkipVerify || c.RootCAs == nil {
			t.Fatal("TLS profile")
		}
		c.InsecureSkipVerify = true
		c.ServerName = "changed"
		c.RootCAs = nil
		next, e := s.SMTPClientTLSConfig(host)
		if e != nil || next.InsecureSkipVerify || next.ServerName != host || next.RootCAs == nil {
			t.Fatal("mutable trust store")
		}
	}
	for _, host := range []string{"", "SMTP.example.com", "smtp.example.com:25", "smtp.example.com.", "smtp.example.com/path", "user@smtp.example.com"} {
		if _, e := s.SMTPClientTLSConfig(host); e == nil {
			t.Fatalf("host accepted %q", host)
		}
	}
}
