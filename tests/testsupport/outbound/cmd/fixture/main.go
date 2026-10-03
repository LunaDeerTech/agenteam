package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func main()             { os.Exit(run()) }
func fail(s string) int { fmt.Fprintln(os.Stderr, s); return 1 }
func run() (code int) {
	options := flag.NewFlagSet("outbound-fixture", flag.ContinueOnError)
	filter := options.String("run", "", "affected test filter")
	if options.Parse(os.Args[1:]) != nil || options.NArg() != 0 {
		return fail("invalid fixture arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	nonce, err := pgfixture.RandomHex(16)
	if err != nil {
		return fail("fixture entropy failed")
	}
	directory, err := os.MkdirTemp("", "agenteam-d04-net-"+nonce+"-")
	if err != nil {
		return fail("fixture directory failed")
	}
	defer os.RemoveAll(directory)
	if certificates(directory) != nil {
		return fail("fixture certificate failed")
	}
	goBinary := os.Getenv("AGENTEAM_GO")
	if goBinary == "" {
		return fail("exact Go binary required")
	}
	build := exec.CommandContext(ctx, goBinary, "build", "-o", filepath.Join(directory, "server"), "./tests/testsupport/outbound/cmd/server")
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "CGO_ENABLED=0", "TMPDIR="+directory)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if build.Run() != nil {
		return fail("fixture server build failed")
	}
	name := "agenteam-d04-net-" + nonce
	var containerID, networkID string
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if containerID == "" {
			raw, _ := pgfixture.Docker(cleanup, "inspect", "--format", "{{json .}}", name)
			var found struct {
				ID     string
				Config struct{ Labels map[string]string }
			}
			if json.Unmarshal(raw, &found) == nil && found.Config.Labels[netfixture.Label] == nonce {
				containerID = found.ID
			}
		}
		if containerID != "" {
			raw, e := pgfixture.Docker(cleanup, "inspect", "--format", "{{json .Config.Labels}}", containerID)
			var labels map[string]string
			if e != nil || json.Unmarshal(raw, &labels) != nil || labels[netfixture.Label] != nonce {
				code = fail("outbound cleanup container ownership mismatch")
			} else if _, e = pgfixture.Docker(cleanup, "rm", "--force", containerID); e != nil {
				code = fail("outbound container cleanup failed")
			}
			if _, e := pgfixture.Docker(cleanup, "inspect", containerID); e == nil {
				code = fail("outbound container remains")
			}
		}
		if networkID == "" {
			raw, _ := pgfixture.Docker(cleanup, "network", "inspect", "--format", "{{json .}}", name)
			var found struct {
				ID     string `json:"Id"`
				Labels map[string]string
			}
			if json.Unmarshal(raw, &found) == nil && found.Labels[netfixture.Label] == nonce {
				networkID = found.ID
			}
		}
		if networkID != "" {
			raw, e := pgfixture.Docker(cleanup, "network", "inspect", "--format", "{{json .Labels}}", networkID)
			var labels map[string]string
			if e != nil || json.Unmarshal(raw, &labels) != nil || labels[netfixture.Label] != nonce {
				code = fail("outbound cleanup network ownership mismatch")
			} else if _, e = pgfixture.Docker(cleanup, "network", "rm", networkID); e != nil {
				code = fail("outbound network cleanup failed")
			}
			if _, e := pgfixture.Docker(cleanup, "network", "inspect", networkID); e == nil {
				code = fail("outbound network remains")
			}
		}
		fmt.Printf("D04 outbound fixture cleanup checked for nonce %s\n", nonce)
	}()
	raw, err := pgfixture.Docker(ctx, "network", "create", "--internal", "--label", netfixture.Label+"="+nonce, name)
	if err != nil {
		return fail("outbound network creation failed")
	}
	networkID = strings.TrimSpace(string(raw))
	raw, err = pgfixture.Docker(ctx, "run", "--detach", "--name", name, "--label", netfixture.Label+"="+nonce, "--network", networkID, "--mount", "type=bind,src="+directory+",dst=/fixture,readonly", "--entrypoint", "/fixture/server", pgfixture.Image, "-cert", "/fixture/server.crt", "-key", "/fixture/server.key", "-nonce", nonce)
	if err != nil {
		return fail("outbound container creation failed")
	}
	containerID = strings.TrimSpace(string(raw))
	raw, err = pgfixture.Docker(ctx, "inspect", "--format", "{{json .NetworkSettings}}", containerID)
	if err != nil {
		return fail("outbound container inspection failed")
	}
	var settings struct {
		Ports    map[string][]struct{ HostIP, HostPort string }
		Networks map[string]struct{ NetworkID, IPAddress string }
	}
	if json.Unmarshal(raw, &settings) != nil {
		return fail("outbound network inspection invalid")
	}
	d := netfixture.Descriptor{ContainerID: containerID, NetworkID: networkID, Nonce: nonce, CAFile: filepath.Join(directory, "ca.crt")}
	for _, n := range settings.Networks {
		if n.NetworkID == networkID {
			d.PrivateIP = n.IPAddress
		}
	}
	// An internal bridge does not publish host ports. The verified owned
	// private IP is also the fixture's trusted control channel, with its nonce.
	d.ControlPort = "9000"
	if d.Verify() != nil {
		return fail("outbound fixture ownership invalid")
	}
	ready, stopReady := context.WithTimeout(ctx, 15*time.Second)
	defer stopReady()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for d.Ready(ready) != nil {
		select {
		case <-ready.Done():
			return fail("outbound fixture did not become ready")
		case <-ticker.C:
		}
	}
	raw, _ = json.Marshal(d)
	path := filepath.Join(directory, "fixture.json")
	if os.WriteFile(path, raw, 0600) != nil {
		return fail("outbound descriptor write failed")
	}
	fmt.Printf("D04 outbound fixture actual private_socket=%s image=%s nonce=%s\n", d.PrivateIP, pgfixture.Image, nonce)
	cmd := exec.CommandContext(ctx, "sh", "scripts/test-postgres.sh", "-run", *filter)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 30 * time.Second
	cmd.Env = append(os.Environ(), netfixture.Env+"="+path, "GOTOOLCHAIN=local", "TMPDIR="+directory)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e := cmd.Run(); e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			return exit.ExitCode()
		}
		return fail("security suite process failed")
	}
	return 0
}
func certificates(directory string) error {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "D04 isolated outbound CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		return e
	}
	serverKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	server := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "fixture.test"}, DNSNames: []string{"fixture.test", "other.fixture.test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	serverDER, e := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, key)
	if e != nil {
		return e
	}
	keyDER, e := x509.MarshalECPrivateKey(serverKey)
	if e != nil {
		return e
	}
	files := map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "server.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), "server.key": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})}
	for name, content := range files {
		if e = os.WriteFile(filepath.Join(directory, name), content, 0600); e != nil {
			return e
		}
	}
	return nil
}
