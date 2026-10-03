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
