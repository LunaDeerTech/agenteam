// Package smtpfixture owns isolated SMTP servers. Its trusted control channel
// is separate from the production outbound client and never changes policy.
package smtpfixture

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	pg "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const Label = "agenteam.d07.smtpfixture"
const Host = "mail.fixture.test"

type Fixture struct {
	Nonce, ContainerID, NetworkID, PrivateIP, CAFile string
	dir, name                                        string
	mu                                               sync.Mutex
	closed                                           bool
}
type Scenario struct {
	Mode                                   string
	AuthCode, MailCode, RCPTCode, DataCode int
	NoSTARTTLS, NoAuth                     bool
	CloseStage, PauseStage                 string
	GreetingBytes, GreetingLines           int
}
type State struct {
	Connections, Closed, TLS, Auth, Mail, RCPT, Data, Messages int
	Phase                                                      string
	MessageBytes                                               int
	MessageSHA, MessageID                                      string
}
type Endpoint struct {
	ID   string
	Port int
}

func Start(ctx context.Context) (_ *Fixture, err error) {
	nonce, err := pg.RandomHex(16)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "agenteam-d07-smtp-"+nonce+"-")
	if err != nil {
		return nil, err
	}
	f := &Fixture{Nonce: nonce, dir: dir, name: "agenteam-d07-smtp-" + nonce, CAFile: filepath.Join(dir, "ca.crt")}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = errors.Join(err, f.Close(cleanup))
		}
	}()
	if err = certificates(dir); err != nil {
		return nil, errors.New("SMTP fixture certificate setup failed")
	}
	goBinary := os.Getenv("AGENTEAM_GO")
	if goBinary == "" {
		return nil, errors.New("SMTP fixture requires fixed AGENTEAM_GO")
	}
	version, err := exec.CommandContext(ctx, goBinary, "env", "GOVERSION").Output()
	if err != nil || strings.TrimSpace(string(version)) != "go1.27.1" {
		return nil, errors.New("SMTP fixture Go version mismatch")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	cmd := exec.CommandContext(ctx, goBinary, "build", "-o", filepath.Join(dir, "server"), "./tests/testsupport/smtp/cmd/server")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "CGO_ENABLED=0")
	if output, e := cmd.CombinedOutput(); e != nil {
		return nil, fmt.Errorf("SMTP fixture server build failed: %s", output)
	}
	raw, err := pg.Docker(ctx, "network", "create", "--internal", "--label", Label+"="+nonce, f.name)
	if err != nil {
		return nil, err
	}
	f.NetworkID = strings.TrimSpace(string(raw))
	raw, err = pg.Docker(ctx, "run", "--detach", "--name", f.name, "--label", Label+"="+nonce, "--network", f.NetworkID, "--read-only", "--mount", "type=bind,src="+dir+",dst=/fixture,readonly", "--entrypoint", "/fixture/server", pg.Image, "-nonce", nonce, "-cert", "/fixture/server.crt", "-key", "/fixture/server.key")
	if err != nil {
		return nil, err
	}
	f.ContainerID = strings.TrimSpace(string(raw))
	raw, err = pg.Docker(ctx, "inspect", "--format", "{{json .NetworkSettings.Networks}}", f.ContainerID)
	if err != nil {
		return nil, err
	}
	var networks map[string]struct{ NetworkID, IPAddress string }
	if json.Unmarshal(raw, &networks) != nil {
		return nil, errors.New("SMTP fixture network inspection failed")
	}
	for _, n := range networks {
		if n.NetworkID == f.NetworkID {
			f.PrivateIP = n.IPAddress
		}
	}
	if err = f.Verify(ctx); err != nil {
		return nil, err
	}
	ready, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for f.control(ready, "GET", "/ready", nil, nil) != nil {
		select {
		case <-ready.Done():
			return nil, errors.New("SMTP fixture readiness failed")
		case <-tick.C:
		}
	}
	fmt.Printf("D07 SMTP fixture private_socket=%s image=%s nonce=%s\n", f.PrivateIP, pg.Image, f.Nonce)
	return f, nil
}
func (f *Fixture) Verify(ctx context.Context) error {
	ip, e := netip.ParseAddr(f.PrivateIP)
	if e != nil || outbound.Classify(ip) != outbound.Private || len(f.ContainerID) != 64 || len(f.NetworkID) != 64 {
		return errors.New("SMTP fixture identity invalid")
	}
	raw, e := pg.Docker(ctx, "inspect", "--format", "{{json .}}", f.ContainerID)
	if e != nil {
		return e
	}
	var v struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
		State           struct{ Running bool }
		NetworkSettings struct {
			Networks map[string]struct{ NetworkID, IPAddress string }
			Ports    map[string]any
		}
	}
	if json.Unmarshal(raw, &v) != nil || v.ID != f.ContainerID || v.Name != "/"+f.name || v.Config.Image != pg.Image || v.Config.Labels[Label] != f.Nonce || !v.State.Running {
		return errors.New("SMTP fixture container ownership mismatch")
	}
	for _, ports := range v.NetworkSettings.Ports {
		if ports != nil {
			return errors.New("SMTP fixture unexpectedly published a port")
		}
	}
	match := false
	for _, n := range v.NetworkSettings.Networks {
		if n.NetworkID == f.NetworkID && n.IPAddress == f.PrivateIP {
			match = true
		}
	}
	if !match {
		return errors.New("SMTP fixture socket mismatch")
	}
	raw, e = pg.Docker(ctx, "network", "inspect", "--format", "{{json .Labels}}", f.NetworkID)
	var labels map[string]string
	if e != nil || json.Unmarshal(raw, &labels) != nil || labels[Label] != f.Nonce {
		return errors.New("SMTP fixture network ownership mismatch")
	}
	return nil
}
func (f *Fixture) Close(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	// Recover only an exact named, labelled creation if Docker's response was lost.
	if f.ContainerID == "" {
		raw, _ := pg.Docker(ctx, "inspect", "--format", "{{json .}}", f.name)
		var v struct {
			ID     string
			Config struct{ Labels map[string]string }
		}
		if json.Unmarshal(raw, &v) == nil && v.Config.Labels[Label] == f.Nonce {
			f.ContainerID = v.ID
		}
	}
	if f.NetworkID == "" {
		raw, _ := pg.Docker(ctx, "network", "inspect", "--format", "{{json .}}", f.name)
		var v struct {
			ID     string `json:"Id"`
			Labels map[string]string
		}
		if json.Unmarshal(raw, &v) == nil && v.Labels[Label] == f.Nonce {
			f.NetworkID = v.ID
		}
	}
	for _, resource := range []struct {
		id      string
		network bool
	}{{f.ContainerID, false}, {f.NetworkID, true}} {
		if resource.id == "" {
			continue
		}
		args := []string{"inspect", "--format", "{{json .Config.Labels}}", resource.id}
		if resource.network {
			args = []string{"network", "inspect", "--format", "{{json .Labels}}", resource.id}
		}
		raw, e := pg.Docker(ctx, args...)
		var labels map[string]string
		if e != nil || json.Unmarshal(raw, &labels) != nil || labels[Label] != f.Nonce {
			return errors.New("SMTP cleanup ownership mismatch")
		}
		remove := []string{"rm", "--force", resource.id}
		inspect := []string{"inspect", resource.id}
		if resource.network {
			remove = []string{"network", "rm", resource.id}
			inspect = []string{"network", "inspect", resource.id}
		}
		if _, e = pg.Docker(ctx, remove...); e != nil {
			return e
		}
		if _, e = pg.Docker(ctx, inspect...); e == nil {
			return errors.New("SMTP fixture resource remains")
		}
	}
	if e := os.RemoveAll(f.dir); e != nil {
		return e
	}
	f.closed = true
	fmt.Printf("D07 SMTP fixture exact cleanup checked nonce=%s\n", f.Nonce)
	return nil
}
func (f *Fixture) control(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, e := json.Marshal(in)
		if e != nil {
			return e
		}
		body = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://"+net.JoinHostPort(f.PrivateIP, "9000")+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("X-Fixture-Nonce", f.Nonce)
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("SMTP fixture control unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("SMTP fixture control rejected")
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
	}
	return nil
}
func (f *Fixture) Create(ctx context.Context, s Scenario) (Endpoint, error) {
	if e := f.Verify(ctx); e != nil {
		return Endpoint{}, e
	}
	var out Endpoint
	e := f.control(ctx, "POST", "/create", s, &out)
	return out, e
}
func (f *Fixture) Release(ctx context.Context, id string) error {
	return f.control(ctx, "POST", "/release/"+id, nil, nil)
}
func (f *Fixture) State(ctx context.Context, id string) (State, error) {
	var out State
	e := f.control(ctx, "GET", "/state/"+id, nil, &out)
	return out, e
}

func certificates(dir string) error {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned D07 SMTP fixture"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		return e
	}
	parsed, e := x509.ParseCertificate(der)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		return e
	}
	leaf, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{Host}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, e = x509.CreateCertificate(rand.Reader, cert, parsed, &leaf.PublicKey, key)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "server.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		return e
	}
	priv, e := x509.MarshalPKCS8PrivateKey(leaf)
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "server.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}), 0600)
}
