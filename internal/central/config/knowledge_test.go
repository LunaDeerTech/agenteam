package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func confirmationRing(material string) string {
	return `{"format":1,"current_kid":"confirmation","keys":[{"kid":"confirmation","key_b64":"` + material + `"}]}`
}

func TestKnowledgeConfirmationConfiguration(t *testing.T) {
	const field = Prefix + "KNOWLEDGE_CONFIRMATION_KEYRING"
	for _, raw := range []string{"", "confirmation-material-SENTINEL", confirmationRing("bad-SENTINEL"), `{"format":1,"current_kid":"missing","keys":[]}`} {
		values := configValues(map[string]string{field: raw})
		if raw == "" {
			delete(values, field)
		}
		cfg, err := loadAccountValues(values)
		var issue *Error
		if !errors.As(err, &issue) || issue.Field() != field || issue.Reason() != "invalid" || cfg.Validate() == nil {
			t.Fatal("invalid or missing confirmation keys accepted")
		}
		assertConfigProjectionSafe(t, err)
	}
	parent := filepath.Join(t.TempDir(), "uncreated")
	values := configValues(map[string]string{Prefix + "ACCOUNT_RECOVERY_LOG": filepath.Join(parent, "recovery.log")})
	lookups := map[string]int{}
	cfg, err := Load(func(key string) (string, bool) {
		lookups[key]++
		if lookups[key] > 1 && key == Prefix+"ACCOUNT_KEYRING" {
			return "changed-SENTINEL", true
		}
		value, ok := values[key]
		return value, ok
	}, nil)
	if err != nil || cfg.Validate() != nil || cfg.KnowledgeConfirmationKeys().Validate() != nil || lookups[Prefix+"ACCOUNT_KEYRING"] != 1 || lookups[field] != 1 {
		t.Fatal("configuration did not retain the exact validated key inputs")
	}
	values[field] = "changed-SENTINEL"
	if cfg.KnowledgeConfirmationKeys().Validate() != nil {
		t.Fatal("configuration retained mutable input")
	}
	assertConfigProjectionSafe(t, cfg)
	assertConfigProjectionSafe(t, cfg.KnowledgeConfirmationKeys())
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Fatal("configuration opened runtime storage")
	}
}

func TestKnowledgeConfirmationRejectsCrossPurposeMaterial(t *testing.T) {
	for _, field := range []string{"CURSOR_KEYRING", "SECRET_KEYRING", "OBJECT_DOWNLOAD_KEYRING", "ACCOUNT_KEYRING"} {
		for _, historical := range []bool{false, true} {
			t.Run(field+map[bool]string{false: "/current", true: "/historical"}[historical], func(t *testing.T) {
				values := configValues(nil)
				var ring map[string]any
				if json.Unmarshal([]byte(values[Prefix+field]), &ring) != nil {
					t.Fatal("bad fixture ring")
				}
				keys := ring["keys"].([]any)
				material := keys[0].(map[string]any)["key_b64"].(string)
				if historical {
					var raw [32]byte
					for i := range raw {
						raw[i] = byte(i + 192)
					}
					material = base64.StdEncoding.EncodeToString(raw[:])
					name, value := "kid", "old"
					if field == "SECRET_KEYRING" {
						name, value = "version", "2"
					}
					ring["keys"] = append(keys, map[string]any{name: value, "key_b64": material})
					encoded, err := json.Marshal(ring)
					if err != nil {
						t.Fatal(err)
					}
					values[Prefix+field] = string(encoded)
				}
				if cfg, err := loadAccountValues(values); err != nil || cfg.Validate() != nil {
					t.Fatal("independent control rejected", err)
				}
				values[Prefix+"KNOWLEDGE_CONFIRMATION_KEYRING"] = confirmationRing(material)
				_, err := loadAccountValues(values)
				var issue *Error
				if !errors.As(err, &issue) || issue.Field() != Prefix+"KNOWLEDGE_CONFIRMATION_KEYRING" {
					t.Fatal("cross-purpose material accepted")
				}
				assertConfigProjectionSafe(t, err)
			})
		}
	}
}
