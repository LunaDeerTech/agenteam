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
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func main()                   { os.Exit(run()) }
func fail(message string) int { fmt.Fprintln(os.Stderr, message); return 1 }
func run() (code int) {
	flags := flag.NewFlagSet("object-fixture", flag.ContinueOnError)
	filter := flags.String("run", "", "affected tests")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 {
		return fail("invalid fixture arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	nonce, e := pgfixture.RandomHex(16)
	if e != nil {
		return fail("fixture entropy failed")
	}
	directory := filepath.Join(os.TempDir(), "agenteam-d05-object-"+nonce)
	if os.Mkdir(directory, 0700) != nil {
		return fail("fixture directory failed")
	}
	defer func() {
		if os.RemoveAll(directory) != nil {
			code = fail("object fixture directory cleanup failed")
		}
		if _, e := os.Lstat(directory); !errors.Is(e, os.ErrNotExist) {
			code = fail("object fixture directory remains")
		}
	}()
	binary := os.Getenv("AGENTEAM_MINIO_BINARY")
	if binary == "" {
		binary = objectfixture.CachedBinary
	}
	if objectfixture.VerifyBinary(binary) != nil {
		return fail("verified MinIO binary required; build exact release per D05 research")
	}
	src, e := os.Open(binary)
	if e != nil {
		return fail("fixture binary open failed")
	}
	dst, e := os.OpenFile(filepath.Join(directory, "minio"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0500)
	if e != nil {
		src.Close()
		return fail("fixture binary destination failed")
	}
	_, e = io.Copy(dst, src)
	src.Close()
	closeErr := dst.Close()
	if e != nil || closeErr != nil || objectfixture.VerifyBinary(filepath.Join(directory, "minio")) != nil {
		return fail("fixture binary copy invalid")
	}
	version, e := exec.CommandContext(ctx, filepath.Join(directory, "minio"), "--version").Output()
	if e != nil || !strings.Contains(string(version), objectfixture.Release) {
		return fail("fixture server version mismatch")
	}
	fmt.Printf("D05 actual source-built MinIO %s binary_sha256=%s\n", strings.SplitN(string(version), "\n", 2)[0], objectfixture.BinarySHA)
	access, e := pgfixture.RandomHex(16)
	if e != nil {
		return fail("fixture entropy failed")
	}
	secret, e := pgfixture.RandomHex(24)
	if e != nil {
		return fail("fixture entropy failed")
	}
	env := []byte("MINIO_ROOT_USER=" + access + "\nMINIO_ROOT_PASSWORD=" + secret + "\nMINIO_BROWSER=off\nMINIO_UPDATE=off\nMINIO_CONFIG_ENV_FILE=/dev/null\nHOME=/fixture/data\n")
	if os.WriteFile(filepath.Join(directory, "credentials.env"), env, 0600) != nil || os.Mkdir(filepath.Join(directory, "data"), 0700) != nil || os.Mkdir(filepath.Join(directory, "certs"), 0700) != nil {
		return fail("fixture private files failed")
	}
	name := "agenteam-d05-object-" + nonce
	var container, network string
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		remove := func(kind, id string) {
			if id == "" {
				return
			}
			args := []string{"inspect", "--format", "{{json .Config.Labels}}", id}
			if kind == "network" {
				args = []string{"network", "inspect", "--format", "{{json .Labels}}", id}
			}
			raw, e := pgfixture.Docker(cleanup, args...)
			var labels map[string]string
			if e != nil || json.Unmarshal(raw, &labels) != nil || labels[objectfixture.Label] != nonce {
				code = fail("object fixture cleanup ownership mismatch")
				return
			}
			args = []string{"rm", "--force", id}
			if kind == "network" {
				args = []string{"network", "rm", id}
			}
			if _, e = pgfixture.Docker(cleanup, args...); e != nil {
				code = fail("object fixture resource cleanup failed")
			}
			args = []string{"inspect", id}
			if kind == "network" {
				args = []string{"network", "inspect", id}
			}
			if _, e = pgfixture.Docker(cleanup, args...); e == nil {
				code = fail("object fixture resource remains")
			}
		}
		if container == "" {
			raw, _ := pgfixture.Docker(cleanup, "inspect", "--format", "{{json .}}", name)
			var owned struct {
				ID     string
				Config struct{ Labels map[string]string }
			}
			if json.Unmarshal(raw, &owned) == nil && owned.Config.Labels[objectfixture.Label] == nonce {
				container = owned.ID
			}
		}
		if network == "" {
			raw, _ := pgfixture.Docker(cleanup, "network", "inspect", "--format", "{{json .}}", name)
			var owned struct {
				ID     string
				Labels map[string]string
			}
			if json.Unmarshal(raw, &owned) == nil && owned.Labels[objectfixture.Label] == nonce {
				network = owned.ID
			}
		}
		remove("container", container)
		remove("network", network)
		fmt.Printf("D05 object fixture exact resource cleanup checked nonce=%s\n", nonce)
	}()
	var address string
	for range 8 {
		var random [2]byte
		if _, e = rand.Read(random[:]); e != nil {
			return fail("fixture entropy failed")
		}
		subnet := fmt.Sprintf("10.%d.%d.0/24", random[0], random[1])
		raw, err := pgfixture.Docker(ctx, "network", "create", "--internal", "--subnet", subnet, "--label", objectfixture.Label+"="+nonce, name)
		if err == nil {
			network = strings.TrimSpace(string(raw))
			address = fmt.Sprintf("10.%d.%d.2", random[0], random[1])
			break
		}
	}
	if network == "" {
		return fail("object internal network creation failed")
	}
	if certificates(filepath.Join(directory, "certs"), address) != nil {
		return fail("object TLS fixture failed")
	}
	raw, e := pgfixture.Docker(ctx, "run", "--detach", "--name", name, "--label", objectfixture.Label+"="+nonce, "--network", network, "--ip", address, "--user", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()), "--env-file", filepath.Join(directory, "credentials.env"), "--mount", "type=bind,src="+directory+",dst=/fixture,readonly", "--mount", "type=bind,src="+filepath.Join(directory, "data")+",dst=/fixture/data", "--entrypoint", "/fixture/minio", pgfixture.Image, "server", "/fixture/data", "--address", ":9000", "--console-address", ":9001", "--certs-dir", "/fixture/certs", "--quiet")
	if e != nil {
		return fail("object fixture container failed")
	}
	container = strings.TrimSpace(string(raw))
	d := objectfixture.Descriptor{ContainerID: container, NetworkID: network, Nonce: nonce, PrivateIP: address, Directory: directory, CAFile: filepath.Join(directory, "certs", "ca.crt"), AccessKey: access, SecretKey: secret}
	if d.Verify() != nil {
		return fail("object fixture verification failed")
	}
	client, transport, e := d.Client()
	if e != nil {
		return fail("object fixture client failed")
	}
	defer transport.CloseIdleConnections()
	ready, stopReady := context.WithTimeout(ctx, 20*time.Second)
	defer stopReady()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, e = client.ListBuckets(ready)
		if e == nil {
			break
		}
		select {
		case <-ready.Done():
			return fail("object fixture not ready")
		case <-ticker.C:
		}
	}
	raw, _ = json.Marshal(d)
	path := filepath.Join(directory, "descriptor.json")
	if os.WriteFile(path, raw, 0600) != nil {
		return fail("object descriptor failed")
	}
	fmt.Printf("D05 object fixture isolated TLS=%s image=%s nonce=%s\n", address, pgfixture.Image, nonce)
	cmd := exec.CommandContext(ctx, "sh", "scripts/test-security.sh", "-objects", "-run", *filter)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 35 * time.Second
	cmd.Env = append(os.Environ(), objectfixture.Env+"="+path, "GOTOOLCHAIN=local", "TMPDIR="+directory)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Run(); e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			return exit.ExitCode()
		}
		return fail("object suite process failed")
	}
	return 0
}
func certificates(directory, ip string) error {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned D05 object fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		return e
	}
	serverKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	server := &x509.Certificate{SerialNumber: big.NewInt(2), IPAddresses: []net.IP{net.ParseIP(ip)}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	cert, e := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, key)
	if e != nil {
		return e
	}
	private, e := x509.MarshalECPrivateKey(serverKey)
	if e != nil {
		return e
	}
	for name, body := range map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "public.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), "private.key": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private})} {
		if e = os.WriteFile(filepath.Join(directory, name), body, 0600); e != nil {
			return e
		}
	}
	return nil
}
