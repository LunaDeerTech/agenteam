//go:build integration

package mcp_test

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

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type ownedProvider struct {
	container, network, nonce, ip, directory string
	roots                                    *x509.CertPool
	token                                    []byte
}
type fixedResolver struct{ address netip.Addr }

func (r fixedResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if host != "mcp.fixture.test" {
		return nil, errors.New("unexpected fixture host")
	}
	return []netip.Addr{r.address}, ctx.Err()
}

type rejectingAudit struct{}

func (rejectingAudit) AppendInTx(context.Context, f.Tx, ac.Entry, ac.AppendKey) (ac.AppendReceipt, error) {
	return ac.AppendReceipt{}, f.NewFault(f.Forbidden, f.NotStarted)
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
func provider(t *testing.T) *ownedProvider {
	t.Helper()
	binary := os.Getenv("AGENTEAM_MCP_PROVIDER_BINARY")
	if binary == "" || !filepath.IsAbs(binary) {
		t.Fatal("owned MCP provider binary required")
	}
	nonce, err := pgfixture.RandomHex(16)
	if err != nil {
		t.Fatal(err)
	}
	p := &ownedProvider{nonce: nonce, directory: t.TempDir()}
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
func (p *ownedProvider) count() (int64, error) {
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
func access(t *testing.T, endpoint string, material sc.SecretMaterial) protocol.Access {
	t.Helper()
	request, err := f.NewID[struct{}]()
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, request.String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := id.RegisterService(id.OutboundService)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(request.String(), id.SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	call, err := outbound.NewCallContext(actor, id.SystemScope(), key, ac.Associations{})
	if err != nil {
		t.Fatal(err)
	}
	scheme := "none"
	if material.Use(func([]byte) error { return nil }) == nil {
		scheme = "bearer"
	}
	return protocol.Access{Endpoint: endpoint, Authentication: protocol.Authentication{Scheme: scheme}, Material: material, Context: call}
}

func TestMCPProtocolRealControlled(t *testing.T) {
	// This proves production SDK/outbound against a real server and real stored
	// policy. It does not assert Owner, Secret resolution or Agent integration.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := provider(t)
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrator, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state := migrator.Migrate(ctx); !state.Migrated {
		t.Fatal(state.Fault)
	}
	store, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		if err := store.ForceClose(closeCtx); err != nil {
			t.Error(err)
		}
	})
	ports, _ := outbound.SelectedPorts(8443)
	rule, err := outbound.NewRule(p.ip+"/32", ports)
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := outbound.NewRules(rule)
	// The isolated test owns policy setup. Authorization/Audit are not replaced
	// by success stubs: denial audit fails closed and is checked as a refusal.
	if _, err = store.Exec(ctx, `UPDATE agenteam_outbound.outbound_policy SET version=version+1,rules=$1 WHERE singleton=true`, rules.JSON()); err != nil {
		t.Fatal(err)
	}
	policy, err := outbound.NewPolicyService(store, rejectingAudit{}, outbound.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	trust, err := outbound.LoadTrustStore(filepath.Join(p.directory, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := outbound.NewClient(policy, trust, fixedResolver{netip.MustParseAddr(p.ip)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		if err := client.ForceClose(cctx); err != nil {
			t.Error(err)
		}
	})
	runtime, err := protocol.New(client)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []struct{ path, revision string }{{"modern", protocol.ModernRevision}, {"legacy", protocol.LegacyRevision}} {
		t.Run(version.path, func(t *testing.T) {
			session, err := runtime.Open(ctx, access(t, "https://mcp.fixture.test:8443/"+version.path, sc.SecretMaterial{}))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := session.Close(); err != nil {
					t.Error(err)
				}
			}()
			if session.Revision() != version.revision {
				t.Fatalf("revision %s", session.Revision())
			}
			tools, err := session.ListTools(ctx)
			if err != nil || len(tools) != 5 {
				t.Fatalf("complete paged tools: count=%d error=%v", len(tools), err)
			}
			result, err := session.CallTool(ctx, "echo", json.RawMessage(`{"text":"independent-input"}`))
			if err != nil || result.IsError || len(result.Content) != 1 || result.Content[0].Text != "provider:independent-input" {
				t.Fatal("real tools/call mismatch", err)
			}
			resources, err := session.ListResources(ctx)
			if err != nil || len(resources) != 1 {
				t.Fatal("resources list", err)
			}
			templates, err := session.ListResourceTemplates(ctx)
			if err != nil || len(templates) != 1 {
				t.Fatal("resource templates", err)
			}
			contents, err := session.ReadResource(ctx, resources[0].URI)
			if err != nil || len(contents) != 1 || contents[0].Text == nil || *contents[0].Text != "real MCP resource bytes" {
				t.Fatal("resources read", err)
			}
			failed, err := session.CallTool(ctx, "failure", json.RawMessage(`{}`))
			if err != nil || !failed.IsError {
				t.Fatal("tool failure was not a known result", err)
			}
		})
	}
	t.Run("credential_reflection_and_unknown", func(t *testing.T) {
		material, _ := sc.NewSecretMaterial(p.token)
		session, err := runtime.Open(ctx, access(t, "https://mcp.fixture.test:8443/modern/protected", material))
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if _, err = session.CallTool(ctx, "reflect_credential", json.RawMessage(`{}`)); err == nil || err.Error() != string(protocol.SensitiveResponse) {
			t.Fatal("reflected credential escaped", err)
		}
		before, _ := p.count()
		_, err = session.CallTool(ctx, "unknown", json.RawMessage(`{}`))
		var result *protocol.Error
		if !errors.As(err, &result) || !result.MayHaveExecuted {
			t.Fatal("lost response reported known", err)
		}
		after, _ := p.count()
		if after != before+1 {
			t.Fatalf("unknown call sent %d times", after-before)
		}
	})
	t.Run("current_policy_rechecked", func(t *testing.T) {
		session, err := runtime.Open(ctx, access(t, "https://mcp.fixture.test:8443/modern", sc.SecretMaterial{}))
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		before, _ := p.count()
		empty, _ := outbound.NewRules()
		if _, err = store.Exec(ctx, `UPDATE agenteam_outbound.outbound_policy SET version=version+1,rules=$1 WHERE singleton=true`, empty.JSON()); err != nil {
			t.Fatal(err)
		}
		if err = policy.Reload(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = session.CallTool(ctx, "echo", json.RawMessage(`{"text":"blocked"}`))
		var result *protocol.Error
		if !errors.As(err, &result) || result.MayHaveExecuted {
			t.Fatal("policy denial allowed or marked sent", err)
		}
		after, _ := p.count()
		if after != before {
			t.Fatal("denied request reached server")
		}
	})
	fmt.Fprintln(io.Discard, "owned MCP run complete")
}
