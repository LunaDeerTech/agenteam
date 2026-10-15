// Package fixture supplies the same nonce-owned official SDK peer used by the
// protocol suite to the original App integration harness.
package fixture

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type Provider struct {
	container, network, nonce, ip, directory string
	roots                                    *x509.CertPool
	token                                    []byte
}

func certificate(t *testing.T, dir string) *x509.CertPool {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned MCP test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err = os.WriteFile(filepath.Join(dir, "ca.pem"), caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"mcp.fixture.test"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	if os.WriteFile(filepath.Join(dir, "server.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0600) != nil || os.WriteFile(filepath.Join(dir, "server.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600) != nil {
		t.Fatal("write owned TLS")
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	return roots
}
func New(t *testing.T) *Provider {
	t.Helper()
	binary := os.Getenv("AGENTEAM_MCP_PROVIDER_BINARY")
	if binary == "" || !filepath.IsAbs(binary) {
		t.Fatal("owned MCP provider binary required")
	}
	nonce, err := pgfixture.RandomHex(16)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{nonce: nonce, directory: t.TempDir()}
	p.roots = certificate(t, p.directory)
	token, err := pgfixture.RandomHex(24)
	if err != nil {
		t.Fatal(err)
	}
	p.token = []byte(token)
	if err = os.WriteFile(filepath.Join(p.directory, "token"), p.token, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := pgfixture.Docker(ctx, "network", "create", "--internal", "--label", "agenteam.m3.mcp="+nonce, "agenteam-m3-mcp-"+nonce)
	if err != nil {
		t.Fatal(err)
	}
	p.network = strings.TrimSpace(string(raw))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// A failed docker run may already have created a container. Recover its
		// exact name only after verifying our nonce, then remove that owned row.
		if p.container == "" {
			name := "agenteam-m3-mcp-" + p.nonce
			raw, err := pgfixture.Docker(ctx, "inspect", "--format", "{{ index .Config.Labels \"agenteam.m3.mcp\" }}", name)
			if err == nil && strings.TrimSpace(string(raw)) == p.nonce {
				p.container = name
			}
		}
		if p.container != "" {
			raw, err := pgfixture.Docker(ctx, "inspect", "--format", "{{ index .Config.Labels \"agenteam.m3.mcp\" }}", p.container)
			if err != nil || strings.TrimSpace(string(raw)) != p.nonce {
				t.Error("MCP provider ownership changed; cleanup refused")
				return
			}
			raw, err = pgfixture.Docker(ctx, "inspect", "--format", "{{.State.Status}}", p.container)
			if err != nil {
				t.Error("MCP provider state unavailable")
				return
			}
			if strings.TrimSpace(string(raw)) != "created" {
				if _, err = pgfixture.Docker(ctx, "stop", "--time", "5", p.container); err != nil {
					t.Error(err)
				}
				raw, err = pgfixture.Docker(ctx, "wait", p.container)
				if err != nil || strings.TrimSpace(string(raw)) != "0" {
					t.Errorf("MCP provider actual wait: %s %v", strings.TrimSpace(string(raw)), err)
				}
			}
			if _, err = pgfixture.Docker(ctx, "rm", p.container); err != nil {
				t.Error(err)
			}
		}
		if _, err := pgfixture.Docker(ctx, "network", "rm", p.network); err != nil {
			t.Error(err)
		}
		clear(p.token)
	})
	raw, err = pgfixture.Docker(ctx, "run", "--detach", "--name", "agenteam-m3-mcp-"+nonce, "--label", "agenteam.m3.mcp="+nonce, "--network", p.network, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--stop-signal", "SIGTERM", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--mount", "type=bind,src="+binary+",dst=/provider,readonly", "--mount", "type=bind,src="+p.directory+",dst=/fixture,readonly", "--entrypoint", "/provider", netfixture.RuntimeImage, "-cert", "/fixture/server.pem", "-key", "/fixture/server.key", "-token-file", "/fixture/token", "-nonce", nonce)
	if err != nil {
		t.Fatal(err)
	}
	p.container = strings.TrimSpace(string(raw))
	raw, err = pgfixture.Docker(ctx, "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", p.container)
	if err != nil {
		t.Fatal(err)
	}
	p.ip = strings.TrimSpace(string(raw))
	address, err := netip.ParseAddr(p.ip)
	if err != nil || outbound.Classify(address) != outbound.Private {
		t.Fatal("provider did not receive owned private address")
	}
	t.Logf("owned MCP provider container=%s network=%s nonce=%s", p.container, p.network, p.nonce)
	ready := time.NewTicker(40 * time.Millisecond)
	defer ready.Stop()
	for {
		if _, err = p.count(); err == nil {
			return p
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned MCP provider not ready")
		case <-ready.C:
		}
	}
}
func (p *Provider) count() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: p.roots, ServerName: "mcp.fixture.test", MinVersion: tls.VersionTLS12}, Proxy: nil}
	defer transport.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+p.ip+":8443/control", nil)
	req.Header.Set("X-Fixture-Nonce", p.nonce)
	response, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return 0, errors.New("fixture control unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return 0, errors.New("fixture control denied")
	}
	var state struct {
		Calls int64 `json:"calls"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&state)
	return state.Calls, err
}

func (p *Provider) Endpoint() string                       { return "https://mcp.fixture.test:8443/modern" }
func (p *Provider) CAFile() string                         { return filepath.Join(p.directory, "ca.pem") }
func (p *Provider) Address() string                        { return p.ip }
func (p *Provider) CallCount() (int64, error)              { return p.count() }
func (p *Provider) Credential() (sc.SecretMaterial, error) { return sc.NewSecretMaterial(p.token) }
