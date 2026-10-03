package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCursorKeyringMandatoryAndInputNeverProjected(t *testing.T) {
	for _, raw := range []string{"", `{"format":1,"current_kid":"SENTINEL","keys":[]}`, `{"format":1,"format":1,"current_kid":"SENTINEL","keys":[]}`, strings.Repeat("SENTINEL", 3000)} {
		cfg, err := loadValues(map[string]string{Prefix + "CURSOR_KEYRING": raw})
		if err == nil {
			t.Fatal("missing/invalid cursor keyring accepted")
		}
		issue, ok := err.(*Error)
		if !ok || issue.Field() != Prefix+"CURSOR_KEYRING" {
			t.Fatal("cursor configuration field lost")
		}
		assertConfigProjectionSafe(t, cfg)
		assertConfigProjectionSafe(t, err)
	}
	cfg, err := loadValues(nil)
	if err != nil || cfg.CursorKeyring().Validate() != nil {
		t.Fatal("keyring did not reach immutable config")
	}
	rawKey := "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	for _, v := range []any{cfg, struct{ private Config }{cfg}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, v), rawKey) {
				t.Fatal("config exposes key")
			}
		}
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), rawKey) {
			t.Fatal("JSON exposes key")
		}
	}
}

func TestSecretKeyringMandatoryIndependentAndSafe(t *testing.T) {
	for _, raw := range []string{"", `{"format":1,"current_version":"SENTINEL","keys":[]}`, `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, strings.Repeat("SENTINEL", 3000)} {
		cfg, err := loadValues(map[string]string{Prefix + "SECRET_KEYRING": raw})
		issue, ok := err.(*Error)
		if !ok || issue.Field() != Prefix+"SECRET_KEYRING" {
			t.Fatal("missing/unsafe master keyring classification")
		}
		assertConfigProjectionSafe(t, cfg)
		assertConfigProjectionSafe(t, err)
	}
	cfg, err := loadValues(nil)
	if err != nil || cfg.SecretKeyring().Validate() != nil || cfg.SecretKeyring().CurrentVersion() != 1 {
		t.Fatal("Secret keyring not bound to immutable config")
	}
	key := "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="
	for _, value := range []any{cfg, struct{ private Config }{cfg}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(verb, value), key) {
				t.Fatal("raw master key in configuration projection")
			}
		}
		b, _ := json.Marshal(value)
		if strings.Contains(string(b), key) {
			t.Fatal("raw master key in configuration JSON")
		}
	}
}
