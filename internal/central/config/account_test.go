package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func loadAccountValues(values map[string]string) (Config, error) {
	var env []string
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, env)
}

func TestB04ConfigAccountRequiredAndSafe(t *testing.T) {
	for _, field := range []string{"ACCOUNT_KEYRING", "ACCOUNT_RECOVERY_LOG"} {
		for _, missing := range []bool{false, true} {
			values := configValues(nil)
			if missing {
				delete(values, Prefix+field)
			} else {
				values[Prefix+field] = ""
			}
			_, err := loadAccountValues(values)
			var issue *Error
			if !errors.As(err, &issue) || issue.Field() != Prefix+field || issue.Reason() != "invalid" {
				t.Fatalf("required account field %s: %v", field, err)
			}
			assertConfigProjectionSafe(t, err)
		}
	}
	for _, path := range []string{"relative-SENTINEL.log", "/", "/tmp/../SENTINEL.log", "/tmp/SENTINEL/", "/tmp/invalid\x00SENTINEL"} {
		_, err := loadValues(map[string]string{Prefix + "ACCOUNT_RECOVERY_LOG": path})
		var issue *Error
		if !errors.As(err, &issue) || issue.Field() != Prefix+"ACCOUNT_RECOVERY_LOG" {
			t.Fatal("invalid account recovery path accepted")
		}
		assertConfigProjectionSafe(t, err)
	}
	_, err := loadValues(map[string]string{Prefix + "ACCOUNT_KEYRING": "account-key-SENTINEL"})
	if err == nil {
		t.Fatal("invalid account keyring accepted")
	}
	assertConfigProjectionSafe(t, err)
	path := filepath.Join(t.TempDir(), "recovery-SENTINEL.log")
	cfg, err := loadValues(map[string]string{Prefix + "ACCOUNT_RECOVERY_LOG": path})
	if err != nil || cfg.Validate() != nil || cfg.AccountKeyring().Validate() != nil || cfg.AccountRecoveryLog() != path {
		t.Fatalf("account configuration unavailable: %v", err)
	}
	assertConfigProjectionSafe(t, cfg)
}

func TestB04ConfigAccountCheckDoesNotTouchRecoveryPath(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-created")
	path := filepath.Join(parent, "recovery.log")
	values := configValues(map[string]string{Prefix + "ACCOUNT_RECOVERY_LOG": path})
	cfg, err := loadAccountValues(values)
	if err != nil || cfg.Validate() != nil {
		t.Fatalf("pure account config check: %v", err)
	}
	values[Prefix+"ACCOUNT_RECOVERY_LOG"] = "/changed/path"
	values[Prefix+"ACCOUNT_KEYRING"] = "changed"
	if cfg.AccountRecoveryLog() != path || cfg.AccountKeyring().Validate() != nil {
		t.Fatal("account config retained mutable inputs")
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Fatal("config check touched the recovery directory")
	}
	// Actual mode, ownership and no-symlink checks belong to recoverylog.Open.
	file := filepath.Join(t.TempDir(), "existing.log")
	if err := os.WriteFile(file, []byte("unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "recovery-link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if cfg, err := loadValues(map[string]string{Prefix + "ACCOUNT_RECOVERY_LOG": link}); err != nil || cfg.Validate() != nil {
		t.Fatal("configuration check performed runtime file validation")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "unchanged" {
		t.Fatal("config check changed existing recovery data")
	}
}

func TestB04ConfigAccountRejectsAllOtherRingMaterials(t *testing.T) {
	for _, field := range []string{"CURSOR_KEYRING", "SECRET_KEYRING", "OBJECT_DOWNLOAD_KEYRING"} {
		for _, historical := range []bool{false, true} {
			t.Run(field+map[bool]string{false: "/current", true: "/history"}[historical], func(t *testing.T) {
				values := configValues(nil)
				var ring map[string]any
				if err := json.Unmarshal([]byte(values[Prefix+field]), &ring); err != nil {
					t.Fatal("invalid test ring")
				}
				keys := ring["keys"].([]any)
				material := keys[0].(map[string]any)["key_b64"].(string)
				if historical {
					var raw [32]byte
					for i := range raw {
						raw[i] = byte(i + 192)
					}
					material = base64.StdEncoding.EncodeToString(raw[:])
					idField, id := "kid", "historical"
					if field == "SECRET_KEYRING" {
						idField, id = "version", "2"
					}
					ring["keys"] = append(keys, map[string]any{idField: id, "key_b64": material})
					encoded, err := json.Marshal(ring)
					if err != nil {
						t.Fatal("invalid test ring encoding")
					}
					values[Prefix+field] = string(encoded)
				}
				// A unique account ring still succeeds with that other ring's
				// history, so failure below is the cross-purpose check itself.
				if _, err := loadAccountValues(values); err != nil {
					t.Fatalf("independent account material rejected: %v", err)
				}
				values[Prefix+"ACCOUNT_KEYRING"] = `{"format":1,"current_kid":"account","keys":[{"kid":"account","key_b64":"` + material + `"}]}`
				_, err := loadAccountValues(values)
				var issue *Error
				if !errors.As(err, &issue) || issue.Field() != Prefix+"ACCOUNT_KEYRING" {
					t.Fatal("reused current or historical key material accepted")
				}
			})
		}
	}
}

func TestB04ConfigAuthenticatedOrigin(t *testing.T) {
	for _, raw := range []string{"http://example.test", "HTTP://EXAMPLE.COM:80/", "http://localhost.", "http://sub.localhost", "http://192.0.2.1", "http://0.0.0.0", "http://[::]", "http://[::ffff:192.0.2.1]", "http://[0:0:0:0:0:ffff:c000:0201]", "http://[2001:0:0:1:0:0:1:1]", "http://[1:2:3:4:5:6:0:0]", "http://[0:0:0:0:0:0:0:0]", "http://127.0.0.1.example.test"} {
		if _, err := loadValues(map[string]string{Prefix + "PUBLIC_ORIGIN": raw}); err == nil {
			t.Fatalf("remote plaintext origin accepted: %s", raw)
		}
	}
	for raw, want := range map[string]string{
		"HTTP://LOCALHOST:80/":      "http://localhost",
		"http://127.12.34.56":       "http://127.12.34.56",
		"http://[::1]":              "http://[::1]",
		"http://[::ffff:127.0.0.1]": "http://[::ffff:7f00:1]",
		"https://EXAMPLE.TEST:443/": "https://example.test",
		"https://example.test.":     "https://example.test.",
	} {
		cfg, err := loadValues(map[string]string{Prefix + "PUBLIC_ORIGIN": raw})
		if err != nil || cfg.Validate() != nil || cfg.PublicOrigin() != want {
			t.Errorf("authenticated origin %s: %s %v", raw, cfg.PublicOrigin(), err)
		}
	}
}
