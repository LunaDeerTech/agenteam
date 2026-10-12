package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestRunnerOperationsClosedConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		present, ok bool
	}{
		{"omitted", "", false, true},
		{"empty", "", true, true},
		{"read-file", "read-file", true, true},
		{"unknown", "PRIVATE_OPERATION_CANARY", true, false},
		{"duplicate", "read-file,read-file", true, false},
		{"other-operation", "write-file", true, false},
		{"combination", "read-file,run-command", true, false},
		{"whitespace", " read-file", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{Prefix + "IDENTITY_FILE": "/owned/private/identity.json"}
			if tc.present {
				values[Prefix+"OPERATIONS"] = tc.value
			}
			var env []string
			for key, value := range values {
				env = append(env, key+"="+value)
			}
			cfg, err := Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, env)
			if !tc.ok {
				issue, ok := err.(*Error)
				if !ok || issue.Field() != Prefix+"OPERATIONS" || strings.Contains(err.Error(), "CANARY") {
					t.Fatal("invalid operation did not produce a safe configuration rejection", err)
				}
				return
			}
			if err != nil || cfg.Validate() != nil || cfg.Operations() != tc.value {
				t.Fatal("closed operation selection was not preserved", err)
			}
			values[Prefix+"OPERATIONS"] = "changed-after-load"
			if cfg.Operations() != tc.value || cfg.Validate() != nil {
				t.Fatal("configuration retained the environment lookup instead of its snapshot")
			}
			if _, explicit := cfg.Device(); explicit {
				t.Fatal("operation selection invented an explicit device configuration")
			}
		})
	}
}

func TestRunnerOperationsStoredIdentityCheckStaysOffline(t *testing.T) {
	directory, err := os.MkdirTemp(".", ".operations-identity-")
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
	missingRoot := filepath.Join(directory, "runtime-root-must-not-be-created")
	id, err := p.NewID()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := identity.NewPending(identity.Configuration{CentralURL: "https://runner.example", RunnerID: id, RootPath: missingRoot})
	if err != nil {
		t.Fatal(err)
	}
	file, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Save(stored); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadValues(map[string]string{Prefix + "IDENTITY_FILE": path, Prefix + "OPERATIONS": p.ReadFileOperation})
	if err != nil || cfg.OfflineCheck(false) != nil || cfg.Operations() != p.ReadFileOperation {
		t.Fatal("stored-only identity was rejected by offline operation validation", err)
	}
	if _, configured := cfg.Device(); configured {
		t.Fatal("offline check substituted stored identity into explicit configuration")
	}
	if _, err := os.Lstat(missingRoot); !os.IsNotExist(err) {
		t.Fatal("offline operation check created or opened a deployment root", err)
	}
	file, err = identity.Open(path)
	if err != nil {
		t.Fatal("offline check retained the identity lock", err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}
