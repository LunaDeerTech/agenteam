package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOutboundCAIsPureStrictAndImmutable(t *testing.T) {
	if _, err := loadValues(map[string]string{Prefix + "OUTBOUND_CA_FILE": ""}); err == nil {
		t.Fatal("explicit empty CA accepted")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned fixture CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	valid := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	path := filepath.Join(t.TempDir(), "outbound-CA-private-SENTINEL.pem")
	for _, raw := range [][]byte{nil, []byte("SENTINEL"), append([]byte("unexpected\n"), valid...), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), append(valid, []byte("trailing")...)} {
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadValues(map[string]string{Prefix + "OUTBOUND_CA_FILE": path})
		issue, ok := err.(*Error)
		if !ok || issue.Field() != Prefix+"OUTBOUND_CA_FILE" {
			t.Fatal("invalid CA accepted")
		}
		assertConfigProjectionSafe(t, cfg)
		assertConfigProjectionSafe(t, err)
	}
	if err := os.WriteFile(path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadValues(map[string]string{Prefix + "OUTBOUND_CA_FILE": path})
	if err != nil || cfg.OutboundTrust().Validate() != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal("config retained mutable CA file", err)
	}
	_, err = loadValues(map[string]string{Prefix + "OUTBOUND_CA_FILE": path})
	if err == nil {
		t.Fatal("missing explicit CA ignored")
	}
	assertConfigProjectionSafe(t, err)
}
