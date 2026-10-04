package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObjectDeploymentConfigurationDoesNotCreateRuntimeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created")
	cfg, err := loadValues(map[string]string{Prefix + "OBJECT_SPOOL_DIR": path, Prefix + "OBJECT_SECRET_KEY": "object-secret-SENTINEL"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Objects().SpoolDirectory() != path {
		t.Fatal("spool configuration changed")
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("pure configuration created spool")
	}
	text := fmt.Sprintf("%+v %#v %q", cfg, struct{ cfg Config }{cfg}, cfg.Objects())
	raw, err := json.Marshal(cfg.Objects())
	if err != nil {
		t.Fatal(err)
	}
	text += string(raw)
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("configuration", "objects", cfg.Objects())
	text += log.String()
	for _, private := range []string{path, "object-secret-SENTINEL", "configuration-only", "127.0.0.1:1"} {
		if strings.Contains(text, private) {
			t.Fatalf("configuration exposed private value class")
		}
	}
}
func TestObjectRequiredFieldsAndUnknownKeysFailClosed(t *testing.T) {
	for _, field := range []string{"OBJECT_ENDPOINT", "OBJECT_BUCKET", "OBJECT_ACCESS_KEY", "OBJECT_SECRET_KEY", "OBJECT_TLS_MODE", "OBJECT_TRANSFER_ENDPOINT", "OBJECT_SPOOL_DIR", "OBJECT_DOWNLOAD_KEYRING"} {
		t.Run(field, func(t *testing.T) {
			cfg, err := loadValues(map[string]string{Prefix + field: ""})
			if err == nil || cfg.Validate() == nil {
				t.Fatal("empty explicit setting accepted")
			}
		})
	}
	for _, field := range []string{"OBJECT_PROXY", "OBJECT_SKIP_RECOVERY", "OBJECT_ALLOW_PUBLIC", "OBJECT_CREDENTIALS_FILE", "OBJECT_UNKNOWN", "DOWNLOAD_KEYRING"} {
		if _, err := loadValues(map[string]string{Prefix + field: "SENTINEL"}); err == nil {
			t.Fatal("unregistered object setting accepted")
		}
	}
	for _, path := range []string{"relative", "/", "/tmp/../spool", "/tmp/new\nline"} {
		if _, err := loadValues(map[string]string{Prefix + "OBJECT_SPOOL_DIR": path}); err == nil {
			t.Fatal("invalid spool location")
		}
	}
	cursor := `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`
	if _, err := loadValues(map[string]string{Prefix + "OBJECT_DOWNLOAD_KEYRING": cursor}); err == nil {
		t.Fatal("cursor material reused for download")
	}
}
