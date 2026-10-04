package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func main() { os.Exit(run()) }
func run() (code int) {
	options := flag.NewFlagSet("postgres-fixture", flag.ContinueOnError)
	objects := options.Bool("objects", false, "include owned object storage suite")
	filter := options.String("run", "", "Go test name filter for an affected integration subset")
	if options.Parse(os.Args[1:]) != nil || options.NArg() != 0 {
		return fail("invalid fixture arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	nonce, err := pgfixture.RandomHex(16)
	if err != nil {
		return fail("fixture entropy failed")
	}
	password, err := pgfixture.RandomHex(24)
	if err != nil {
		return fail("fixture entropy failed")
	}
	directory, err := os.MkdirTemp("", "agenteam-d03-"+nonce+"-")
	if err != nil {
		return fail("fixture directory failed")
	}
	defer os.RemoveAll(directory)
	if err := certificates(directory); err != nil {
		return fail("fixture certificate generation failed")
	}
	envFile := filepath.Join(directory, "postgres.env")
	if err := os.WriteFile(envFile, []byte("POSTGRES_USER=fixture_owner\nPOSTGRES_DB=fixture_control\nPOSTGRES_PASSWORD="+password+"\nPGDATA=/var/lib/postgresql/data\n"), 0600); err != nil {
		return fail("fixture environment failed")
	}
	name := "agenteam-d03-" + nonce
	var networkID, containerID, unsupportedID string
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Inspect only exact task names when creation was interrupted before ID.
		for _, owned := range []struct {
			name string
			id   *string
		}{{name, &containerID}, {name + "-pg16", &unsupportedID}} {
			if *owned.id == "" {
				output, _ := pgfixture.Docker(cleanup, "inspect", "--format", "{{json .}}", owned.name)
				var found pgfixture.ContainerInspection
				if json.Unmarshal(output, &found) == nil && found.Config.Labels[pgfixture.Label] == nonce {
					*owned.id = found.ID
				}
			}
			if *owned.id == "" {
				continue
			}
			output, err := pgfixture.Docker(cleanup, "inspect", "--format", "{{json .Config.Labels}}", *owned.id)
			var labels map[string]string
			if err != nil || json.Unmarshal(output, &labels) != nil || labels[pgfixture.Label] != nonce {
				code = fail("container ownership changed during cleanup")
				continue
			}
			if _, err := pgfixture.Docker(cleanup, "rm", "--force", *owned.id); err != nil {
				code = fail("owned container cleanup failed")
			}
			if _, err := pgfixture.Docker(cleanup, "inspect", *owned.id); err == nil {
				code = fail("owned container remains")
			}
		}
		if networkID == "" {
			output, _ := pgfixture.Docker(cleanup, "network", "inspect", "--format", "{{json .}}", name)
			var found struct {
				ID     string `json:"Id"`
				Labels map[string]string
			}
			if json.Unmarshal(output, &found) == nil && found.Labels[pgfixture.Label] == nonce {
				networkID = found.ID
			}
		}
		if networkID != "" {
			output, err := pgfixture.Docker(cleanup, "network", "inspect", "--format", "{{json .Labels}}", networkID)
			var labels map[string]string
			if err != nil || json.Unmarshal(output, &labels) != nil || labels[pgfixture.Label] != nonce {
				code = fail("network ownership changed during cleanup")
			} else if _, err := pgfixture.Docker(cleanup, "network", "rm", networkID); err != nil {
				code = fail("owned network cleanup failed")
			}
		}
		if networkID != "" {
			if _, err := pgfixture.Docker(cleanup, "network", "inspect", networkID); err == nil {
				code = fail("owned network remains")
			}
		}
		fmt.Println("D03 fixture cleanup checked for nonce " + nonce)
	}()
	output, err := pgfixture.Docker(ctx, "network", "create", "--driver", "bridge", "--label", pgfixture.Label+"="+nonce, name)
	if err != nil {
		return fail("fixture network creation failed")
	}
	networkID = strings.TrimSpace(string(output))
	command := "cp /fixture/server.key /tmp/agenteam-server.key && cp /fixture/server.crt /tmp/agenteam-server.crt && chown postgres:postgres /tmp/agenteam-server.key /tmp/agenteam-server.crt && chmod 600 /tmp/agenteam-server.key /tmp/agenteam-server.crt && exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tmp/agenteam-server.crt -c ssl_key_file=/tmp/agenteam-server.key"
	output, err = pgfixture.Docker(ctx, "run", "--detach", "--name", name, "--label", pgfixture.Label+"="+nonce, "--network", name, "--publish", "127.0.0.1::5432", "--tmpfs", "/var/lib/postgresql/data:rw,nosuid,size=512m", "--mount", "type=bind,src="+directory+",dst=/fixture,readonly", "--env-file", envFile, pgfixture.Image, "bash", "-c", command)
	if err != nil {
		return fail("fixture container creation failed")
	}
	containerID = strings.TrimSpace(string(output))
	ready, readyCancel := context.WithTimeout(ctx, 40*time.Second)
	defer readyCancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := pgfixture.Docker(ready, "exec", containerID, "pg_isready", "-h", "127.0.0.1", "-U", "fixture_owner", "-d", "fixture_control"); err == nil {
			break
		}
		select {
		case <-ready.Done():
			logs, _ := exec.Command("docker", "logs", "--tail", "80", containerID).CombinedOutput()
			// Only fixture initialization has run. Keep this task-owned setup
			// diagnostic out of product logging and redact the one-time password.
			debugPath := filepath.Join(os.TempDir(), "agenteam-d03-"+nonce+"-startup.log")
			_ = os.WriteFile(debugPath, []byte(strings.ReplaceAll(string(logs), password, "[fixture-password]")), 0600)
			fmt.Fprintln(os.Stderr, "fixture startup diagnostic: "+debugPath)
			return fail("fixture startup failed")
		case <-ticker.C:
		}
	}
	output, err = pgfixture.Docker(ctx, "inspect", "--format", "{{json .}}", containerID)
	if err != nil {
		return fail("fixture inspect failed")
	}
	var inspected pgfixture.ContainerInspection
	if json.Unmarshal(output, &inspected) != nil {
		return fail("fixture inspect invalid")
	}
	ports := inspected.NetworkSettings.Ports["5432/tcp"]
	if len(ports) != 1 {
		return fail("fixture port missing")
	}
	fixture := pgfixture.Descriptor{ContainerID: containerID, NetworkID: networkID, Nonce: nonce, Port: ports[0].HostPort, Image: pgfixture.Image, User: "fixture_owner", Password: password, CAFile: filepath.Join(directory, "ca.crt"), WrongCAFile: filepath.Join(directory, "wrong-ca.crt")}
	if err := fixture.Verify(); err != nil {
		return fail("fixture verification failed")
	}
	encoded, err := json.Marshal(fixture)
	if err != nil {
		return fail("fixture serialization failed")
	}
	path := filepath.Join(directory, "fixture.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		return fail("fixture file failed")
	}
	output, err = pgfixture.Docker(ctx, "run", "--detach", "--name", name+"-pg16", "--label", pgfixture.Label+"="+nonce, "--network", name, "--publish", "127.0.0.1::5432", "--tmpfs", "/var/lib/postgresql/data:rw,nosuid,size=512m", "--mount", "type=bind,src="+directory+",dst=/fixture,readonly", "--env-file", envFile, pgfixture.UnsupportedImage, "bash", "-c", command)
	if err != nil {
		return fail("unsupported-version fixture creation failed")
	}
	unsupportedID = strings.TrimSpace(string(output))
	for {
		if _, err := pgfixture.Docker(ready, "exec", unsupportedID, "pg_isready", "-h", "127.0.0.1", "-U", "fixture_owner", "-d", "fixture_control"); err == nil {
			break
		}
		select {
		case <-ready.Done():
			return fail("unsupported-version fixture startup failed")
		case <-ticker.C:
		}
	}
	output, err = pgfixture.Docker(ctx, "inspect", "--format", "{{json .}}", unsupportedID)
	if err != nil || json.Unmarshal(output, &inspected) != nil {
		return fail("unsupported-version fixture inspect failed")
	}
	ports = inspected.NetworkSettings.Ports["5432/tcp"]
	if len(ports) != 1 {
		return fail("unsupported-version fixture port missing")
	}
	unsupported := fixture
	unsupported.ContainerID = unsupportedID
	unsupported.Port = ports[0].HostPort
	unsupported.Image = pgfixture.UnsupportedImage
	if err := unsupported.Verify(); err != nil {
		return fail("unsupported-version fixture verification failed")
	}
	encoded, err = json.Marshal(unsupported)
	if err != nil {
		return fail("unsupported-version fixture serialization failed")
	}
	unsupportedPath := filepath.Join(directory, "unsupported-fixture.json")
	if err := os.WriteFile(unsupportedPath, encoded, 0600); err != nil {
		return fail("unsupported-version fixture file failed")
	}
	for _, fixture := range []*pgfixture.Descriptor{&fixture, &unsupported} {
		conn, err := fixture.Connect(ready, "fixture_control")
		if err != nil {
			return fail("fixture version connection failed")
		}
		var version int
		var extension string
		err = conn.QueryRow(ready, "SELECT current_setting('server_version_num')::int,default_version FROM pg_available_extensions WHERE name='vector'").Scan(&version, &extension)
		_ = conn.Close(ready)
		if err != nil || extension != "0.8.1" {
			return fail("fixture version query failed")
		}
		fmt.Printf("D03 fixture actual PostgreSQL=%d available_vector=%s image=%s\n", version, extension, fixture.Image)
	}
	goBinary := os.Getenv("AGENTEAM_GO")
	if goBinary == "" {
		return fail("exact Go binary required")
	}
	arguments := []string{"test", "-tags=integration", "-race", "-count=1", "-timeout=6m", "-run=" + *filter, "./internal/central/postgres/...", "./tests/database/...", "./internal/central/app/...", "./tests/process/...", "./tests/security/..."}
	if *objects {
		arguments = append(arguments, "./tests/objects/...", "./internal/central/object/...")
	}
	cmd := exec.CommandContext(ctx, goBinary, arguments...)
	// The Go command can create test executables and migration children. They
	// all belong to this dedicated process group; cancel only this owned group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 3 * time.Second
	// Forced interruption can prevent the Go tool from cleaning go-build paths.
	// Keep all test/build temporary files inside our own final RemoveAll scope.
	cmd.Env = append(os.Environ(), pgfixture.Env+"="+path, pgfixture.UnsupportedEnv+"="+unsupportedPath, "GOTOOLCHAIN=local", "TMPDIR="+directory)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return fail("database test process failed")
	}
	return 0
}
func fail(message string) int { fmt.Fprintln(os.Stderr, message); return 1 }
func certificates(directory string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "D03 isolated fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		return err
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	server := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, key)
	if err != nil {
		return err
	}
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	ca.SerialNumber = big.NewInt(3)
	wrongDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &wrongKey.PublicKey, wrongKey)
	if err != nil {
		return err
	}
	files := map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), "wrong-ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: wrongDER}), "server.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), "server.key": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})}
	if err := os.Chmod(directory, 0755); err != nil {
		return err
	}
	for name, data := range files {
		mode := os.FileMode(0644)
		if name == "server.key" {
			mode = 0600
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, mode); err != nil {
			return err
		}
	}
	return nil
}
