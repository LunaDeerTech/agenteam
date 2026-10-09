package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func loadValues(values map[string]string) (Config, error) {
	copyValues := map[string]string{Prefix + "IDENTITY_FILE": "/owned/private/identity.json"}
	for k, v := range values {
		copyValues[k] = v
	}
	values = copyValues
	var env []string
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, env)
}
func TestRunnerConfiguration(t *testing.T) {
	c, err := loadValues(map[string]string{"AGENTEAM_CENTRAL_SECRET": "credential-SENTINEL", "UNRELATED": "credential-SENTINEL"})
	if err != nil || c.Validate() != nil || c.LogLevel() != slog.LevelInfo || c.ShutdownTimeout() != 10*time.Second {
		t.Fatalf("defaults: %v", err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if c.Validate() != nil {
				t.Error("concurrent config changed")
			}
		})
	}
	wg.Wait()
	for field, values := range map[string][]string{"LOG_LEVEL": {"", "INFO", "trace", "credential-SENTINEL"}, "SHUTDOWN_TIMEOUT": {"", "0", "99ms", "5m1ns", "-1s", "credential-SENTINEL"}} {
		for _, value := range values {
			_, err := loadValues(map[string]string{Prefix + field: value})
			if err == nil || strings.Contains(fmt.Sprintf("%+v", err), "SENTINEL") {
				t.Errorf("bad value accepted/leaked for %s", field)
			}
		}
	}
	for _, key := range []string{Prefix + "CENTRAL_URL", Prefix + "credential-SENTINEL"} {
		_, err := loadValues(map[string]string{key: "credential-SENTINEL"})
		if err == nil || strings.Contains(err.Error(), "SENTINEL") {
			t.Fatal("unknown field accepted/leaked")
		}
	}
	for field, values := range map[string][]string{"LOG_LEVEL": {"debug", "info", "warn", "error"}, "SHUTDOWN_TIMEOUT": {"100ms", "5m"}} {
		for _, value := range values {
			if _, err := loadValues(map[string]string{Prefix + field: value}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := Load(nil, nil); err == nil {
		t.Fatal("nil lookup accepted")
	}
	if (Config{}).Validate() == nil {
		t.Fatal("zero config accepted")
	}
}

func TestRunnerIdentityConfigurationAndOfflineCheck(t *testing.T) {
	directory, err := os.MkdirTemp(".", ".config-identity-")
	if err != nil {
		t.Fatal(err)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(directory, "identity.json")
	runner, _ := p.NewID()
	values := map[string]string{Prefix + "IDENTITY_FILE": path, Prefix + "CENTRAL_URL": "https://central.example", Prefix + "ID": string(runner), Prefix + "ROOT_PATH": "/runner/work"}
	c, err := loadValues(values)
	if err != nil {
		t.Fatal(err)
	}
	if c.OfflineCheck(false) == nil || c.OfflineCheck(true) != nil {
		t.Fatal("absent identity was not restricted to explicit enrollment")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatal("offline check created an identity owner")
	}
	config, ok := c.Device()
	if !ok || config.RunnerID != runner {
		t.Fatal("device configuration missing")
	}
	pending, err := identity.NewPending(config)
	if err != nil {
		t.Fatal(err)
	}
	f, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Save(pending); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if c.OfflineCheck(false) != nil {
		t.Fatal("actual private identity snapshot rejected")
	}
	for _, variant := range []string{"mismatch", "partial", "empty-host", "empty-path", "relative", "query", "token-env"} {
		t.Run(variant, func(t *testing.T) {
			v := make(map[string]string)
			for k, x := range values {
				v[k] = x
			}
			switch variant {
			case "mismatch":
				v[Prefix+"ROOT_PATH"] = "/other"
			case "partial":
				delete(v, Prefix+"ID")
			case "empty-host":
				v[Prefix+"CENTRAL_URL"] = "https://:443"
			case "empty-path":
				v[Prefix+"IDENTITY_FILE"] = ""
			case "relative":
				v[Prefix+"IDENTITY_FILE"] = "relative"
			case "query":
				v[Prefix+"CENTRAL_URL"] = "https://central.example?secret=CANARY"
			case "token-env":
				v[Prefix+"ENROLLMENT_TOKEN"] = "CANARY"
			}
			bad, e := loadValues(v)
			if variant == "mismatch" {
				if e != nil || bad.OfflineCheck(false) == nil {
					t.Fatal("persisted immutable binding changed", e)
				}
			} else if e == nil {
				t.Fatal("invalid device configuration accepted")
			}
			if strings.Contains(fmt.Sprintf("%+v", e), "CANARY") {
				t.Fatal("configuration error leaked")
			}
		})
	}
	if _, err := Load(func(string) (string, bool) { return "", false }, nil); err == nil {
		t.Fatal("D15 retained credential-free D02 startup")
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("config", "value", c)
	raw, _ := json.Marshal(c)
	if strings.Contains(fmt.Sprintf("%+v %#v", c, c)+string(raw)+logs.String(), directory) {
		t.Fatal("config default formatting leaked identity path")
	}
}
func TestRunnerExplicitCARoots(t *testing.T) {
	directory := t.TempDir()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "root.pem")
	if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := loadValues(map[string]string{Prefix + "CA_FILE": path})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := c.Roots()
	if err != nil || pool == nil {
		t.Fatal("explicit deployment roots unavailable", err)
	}
	for _, bad := range [][]byte{[]byte("private-ca-CANARY"), bytes.Repeat([]byte{'x'}, (1<<20)+1)} {
		if err = os.WriteFile(path, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = c.Roots(); err == nil || strings.Contains(err.Error(), "CANARY") {
			t.Fatal("invalid or oversized trust file accepted/leaked")
		}
	}
	if _, err := loadValues(map[string]string{Prefix + "CA_FILE": ""}); err == nil {
		t.Fatal("explicit empty CA silently became default")
	}
}
