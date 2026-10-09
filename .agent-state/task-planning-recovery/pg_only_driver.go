// Task-owned PG-only runner. Build this file separately; do not invoke the
// multi-resource integration scripts for the D11 Task Planning selectors.
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
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const variableStorageSelector = `^TestProjectVariable(Migration|PaginationAndLimits|Atomicity)$`
const variableStorageRepairSelector = `^TestProjectVariable(Migration|Atomicity)$`
const variableAuthoritySelector = `^TestProjectVariable(Authority|FinalAuthorityCompetition)$`
const variableHTTPSelector = `^TestProjectVariableHTTP(AuthorityAndPersistence|IntentRecovery)$`
const variableJoinSelector = `^TestProjectVariable(UnknownStopJoin|ReadCancellationJoin)$`

func main()             { os.Exit(run()) }
func fail(s string) int { fmt.Fprintln(os.Stderr, s); return 1 }
func run() (code int) {
	opts := flag.NewFlagSet("task-pg-only", flag.ContinueOnError)
	binary := opts.String("test-binary", "", "precompiled race integration executable")
	selector := opts.String("run", "", "one exact anchored top-level selector")
	directory := opts.String("directory", "", "new private task-owned run directory")
	if opts.Parse(os.Args[1:]) != nil || opts.NArg() != 0 || *binary == "" || *directory == "" || (!regexp.MustCompile(`^\^Test[A-Za-z0-9]+\$$`).MatchString(*selector) && *selector != variableStorageSelector && *selector != variableStorageRepairSelector && *selector != variableAuthoritySelector && *selector != variableHTTPSelector && *selector != variableJoinSelector) {
		return fail("exact binary, directory and one anchored top are required")
	}
	start := time.Now()
	var disk syscall.Statfs_t
	if syscall.Statfs(filepath.Dir(*directory), &disk) != nil || disk.Bavail*uint64(disk.Bsize) < 5<<30 {
		return fail("fresh disk below 5 GiB")
	}
	if err := os.Mkdir(*directory, 0700); err != nil {
		return fail("run directory must be fresh")
	}
	signalCtx, signalCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()
	ctx, cancel := context.WithTimeout(signalCtx, 105*time.Second)
	defer cancel()
	nonce, err := pgfixture.RandomHex(16)
	if err != nil {
		return fail("nonce failed")
	}
	password, err := pgfixture.RandomHex(24)
	if err != nil {
		return fail("password entropy failed")
	}
	name := "agenteam-d03-" + nonce
	var networkID, containerID string
	childStarted, childWaited := false, false
	// This file contains only owned resource identities, never credentials.
	save := func() {
		b, _ := json.Marshal(map[string]string{"nonce": nonce, "network_id": networkID, "container_id": containerID})
		_ = os.WriteFile(filepath.Join(*directory, "owned.json"), b, 0600)
	}
	save()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		clean := true
		for _, item := range []struct {
			kind string
			id   *string
		}{{"container", &containerID}, {"network", &networkID}} {
			args := []string{"inspect", "--format", "{{json .}}", name}
			if item.kind == "network" {
				args = append([]string{"network"}, args...)
			}
			if *item.id == "" {
				out, e := pgfixture.Docker(cleanup, args...)
				if e == nil {
					var found struct {
						ID     string `json:"Id"`
						Labels map[string]string
						Config struct{ Labels map[string]string }
					}
					if json.Unmarshal(out, &found) != nil {
						clean = false
						continue
					}
					labels := found.Labels
					if item.kind == "container" {
						labels = found.Config.Labels
					}
					if labels[pgfixture.Label] != nonce {
						clean = false
						continue
					}
					*item.id = found.ID
					save()
				}
			}
			if *item.id == "" {
				continue
			}
			inspect := []string{"inspect", "--format", "{{json .Config.Labels}}", *item.id}
			remove := []string{"rm", "--force", *item.id}
			if item.kind == "network" {
				inspect = []string{"network", "inspect", "--format", "{{json .Labels}}", *item.id}
				remove = []string{"network", "rm", *item.id}
			}
			out, e := pgfixture.Docker(cleanup, inspect...)
			var labels map[string]string
			if e != nil || json.Unmarshal(out, &labels) != nil || labels[pgfixture.Label] != nonce {
				clean = false
				fmt.Println("STOP cleanup identity validation failed", item.kind)
				continue
			}
			if _, e = pgfixture.Docker(cleanup, remove...); e != nil {
				clean = false
				fmt.Println("STOP owned removal failed", item.kind)
			}
		}
		for round := 1; round <= 2; round++ {
			for _, item := range []struct{ kind, id string }{{"container", containerID}, {"network", networkID}} {
				args := []string{"ps", "--all", "--quiet", "--no-trunc"}
				if item.kind == "network" {
					args = []string{"network", "ls", "--quiet", "--no-trunc"}
				}
				out, e := pgfixture.Docker(cleanup, args...)
				if e != nil {
					clean = false
					continue
				}
				for _, id := range strings.Fields(string(out)) {
					if id == item.id && item.id != "" {
						clean = false
					}
				}
			}
			fmt.Printf("RETIRE observation=%d exact_container=%s exact_network=%s clean=%t\n", round, containerID, networkID, clean)
		}
		// Credentials and CA private key inputs retire only after the resources.
		for _, file := range []string{"fixture.json", "postgres.env", "server.key", "server.crt", "ca.crt", "wrong-ca.crt"} {
			if e := os.Remove(filepath.Join(*directory, file)); e != nil && !os.IsNotExist(e) {
				clean = false
			}
		}
		if !clean {
			code = 1
			fmt.Println("STOP cleanup failed; no subsequent selector is authorized by this runner")
		}
		if time.Since(start) > 120*time.Second {
			code = 1
			fmt.Println("STOP top exceeded setup/body/cleanup 120s")
		}
		fmt.Printf("DRIVER terminal exit=%d elapsed=%s child_started=%t actual_child_wait=%t cleanup=%t\n", code, time.Since(start), childStarted, childWaited, clean)
	}()
	if err = certificates(*directory); err != nil {
		return fail("certificates failed")
	}
	// The fixture helper requires readable CA/container inputs; credentials stay 0600.
	envFile := filepath.Join(*directory, "postgres.env")
	if err = os.WriteFile(envFile, []byte("POSTGRES_USER=fixture_owner\nPOSTGRES_DB=fixture_control\nPOSTGRES_PASSWORD="+password+"\nPGDATA=/var/lib/postgresql/data\n"), 0600); err != nil {
		return fail("env write failed")
	}
	out, err := pgfixture.Docker(ctx, "network", "create", "--driver", "bridge", "--label", pgfixture.Label+"="+nonce, name)
	if err != nil {
		return fail("network create failed")
	}
	networkID = strings.TrimSpace(string(out))
	save()
	command := "cp /fixture/server.key /tmp/agenteam-server.key && cp /fixture/server.crt /tmp/agenteam-server.crt && chown postgres:postgres /tmp/agenteam-server.key /tmp/agenteam-server.crt && chmod 600 /tmp/agenteam-server.key /tmp/agenteam-server.crt && exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tmp/agenteam-server.crt -c ssl_key_file=/tmp/agenteam-server.key"
	out, err = pgfixture.Docker(ctx, "run", "--detach", "--name", name, "--label", pgfixture.Label+"="+nonce, "--network", networkID, "--publish", "127.0.0.1::5432", "--tmpfs", "/var/lib/postgresql/data:rw,nosuid,size=512m", "--mount", "type=bind,src="+*directory+",dst=/fixture,readonly", "--env-file", envFile, pgfixture.Image, "bash", "-c", command)
	if err != nil {
		return fail("container create failed")
	}
	containerID = strings.TrimSpace(string(out))
	save()
	ready, stop := context.WithTimeout(ctx, 40*time.Second)
	defer stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err = pgfixture.Docker(ready, "exec", containerID, "pg_isready", "-h", "127.0.0.1", "-U", "fixture_owner", "-d", "fixture_control"); err == nil {
			break
		}
		select {
		case <-ready.Done():
			return fail("PG readiness failed")
		case <-ticker.C:
		}
	}
	out, err = pgfixture.Docker(ctx, "inspect", "--format", "{{json .}}", containerID)
	if err != nil {
		return fail("inspect failed")
	}
	var inspected pgfixture.ContainerInspection
	if json.Unmarshal(out, &inspected) != nil {
		return fail("inspect parse failed")
	}
	ports := inspected.NetworkSettings.Ports["5432/tcp"]
	if len(ports) != 1 {
		return fail("port missing")
	}
	fixture := pgfixture.Descriptor{ContainerID: containerID, NetworkID: networkID, Nonce: nonce, Port: ports[0].HostPort, Image: pgfixture.Image, User: "fixture_owner", Password: password, CAFile: filepath.Join(*directory, "ca.crt"), WrongCAFile: filepath.Join(*directory, "wrong-ca.crt")}
	if err = fixture.Verify(); err != nil {
		return fail("fixture identity verification failed")
	}
	encoded, err := json.Marshal(fixture)
	if err != nil {
		return fail("descriptor encode failed")
	}
	path := filepath.Join(*directory, "fixture.json")
	if err = os.WriteFile(path, encoded, 0600); err != nil {
		return fail("descriptor write failed")
	}
	conn, err := fixture.Connect(ctx, "fixture_control")
	if err != nil {
		return fail("verified TLS connection failed")
	}
	var version int
	var vector string
	err = conn.QueryRow(ctx, "SELECT current_setting('server_version_num')::int,default_version FROM pg_available_extensions WHERE name='vector'").Scan(&version, &vector)
	closeErr := conn.Close(ctx)
	if err != nil || closeErr != nil || version < 170000 || version >= 180000 || vector != "0.8.1" {
		return fail("PG/vector version or TLS close failed")
	}
	fmt.Printf("OWNED nonce=%s container=%s network=%s port=%s PostgreSQL=%d vector=%s\n", nonce, containerID, networkID, fixture.Port, version, vector)
	cmd := exec.CommandContext(ctx, *binary, "-test.v", "-test.count=1", "-test.timeout=6m", "-test.run="+*selector)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Env = append(os.Environ(), pgfixture.Env+"="+path, "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "TMPDIR="+*directory)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		return fail("test child start failed")
	}
	childStarted = true
	fmt.Printf("CHILD pid=%d selector=%s\n", cmd.Process.Pid, *selector)
	err = cmd.Wait()
	childWaited = true
	fmt.Printf("CHILD actual_wait pid=%d state=%s\n", cmd.Process.Pid, cmd.ProcessState.String())
	// A process group must be empty after the direct executable has exited.
	if e := syscall.Kill(-cmd.Process.Pid, 0); e == nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return fail("STOP child process group survived actual Wait")
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return fail("test child wait failed")
	}
	return 0
}
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
