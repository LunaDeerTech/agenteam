package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configValues(values map[string]string) (Config, error) {
	return LoadConfig(func(name string) (string, bool) {
		value, ok := values[strings.TrimPrefix(name, ConfigPrefix)]
		return value, ok
	}, nil)
}
func TestConfigRejectsImplicitSourcesAndUnsafeURLs(t *testing.T) {
	base := "postgresql://fixture:password-sentinel@127.0.0.1:5432/fixture"
	cfg, err := configValues(map[string]string{"URL": base})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSMode() != "verify-full" || cfg.MaxConns() != 10 {
		t.Fatal("defaults")
	}
	for _, raw := range []string{"", "postgresql://fixture@127.0.0.1:5432/fixture", base + "?sslmode=disable", base + "?", base + "#", "postgres://u:p@a,b:5432/db", "postgres://u:p@localhost/db", "postgres://u:p@/db", "postgres://u:p@localhost:5432/", "postgres://u:%00@localhost:5432/db"} {
		if _, err := configValues(map[string]string{"URL": raw}); err == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	for _, pair := range [][2]string{{"TLS_MODE", "prefer"}, {"TLS_MODE", ""}, {"MAX_CONNS", "1"}, {"MAX_CONNS", "101"}, {"CONNECT_TIMEOUT", "99ms"}, {"LOCK_TIMEOUT", "2m"}, {"STARTUP_TIMEOUT", "999ms"}, {"CA_FILE", ""}} {
		if _, err := configValues(map[string]string{"URL": base, pair[0]: pair[1]}); err == nil {
			t.Fatalf("invalid field %s accepted", pair[0])
		}
	}
	for _, name := range []string{"PGPASSWORD", "PGSERVICE", "PGPASSFILE", "PGSSLKEY", "PGSSLUNKNOWN", "PGCHANNELBINDING", "PGREQUIREAUTH", "PGTZ"} {
		if err := RejectPGEnvironment([]string{name + "=sentinel"}); CodeOf(err) != EnvironmentRejected {
			t.Fatalf("source %s accepted", name)
		}
	}
	if RejectPGEnvironment([]string{"PGPASSWORD=", "OTHER=value"}) != nil {
		t.Fatal("unrelated environment rejected")
	}
	for _, v := range []any{cfg, &cfg, struct{ secret Config }{cfg}, invalidConfig("DATABASE_URL")} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(f, v), "password-sentinel") {
				t.Fatal("format exposed password")
			}
		}
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "password-sentinel") {
			t.Fatal("JSON exposed password")
		}
		var out bytes.Buffer
		slog.New(slog.NewJSONHandler(&out, nil)).LogAttrs(context.Background(), slog.LevelInfo, "test", slog.Any("value", v))
		if strings.Contains(out.String(), "password-sentinel") {
			t.Fatal("log exposed password")
		}
	}
}
func TestDriverConfigUsesOnlyExplicitSource(t *testing.T) {
	// Process environment mutation is deliberately confined to this sequential
	// test; production never edits environment. No real credential path is read.
	t.Setenv("PGSERVICE", "private-service-sentinel")
	cfg, err := configValues(map[string]string{"URL": "postgres://u:p@localhost:5432/db"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.driverConfig(); CodeOf(err) != EnvironmentRejected {
		t.Fatal("actual PG environment not checked")
	}
	t.Setenv("PGSERVICE", "")
	temp := t.TempDir()
	badCA := filepath.Join(temp, "bad.pem")
	if err := os.WriteFile(badCA, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := configValues(map[string]string{"URL": "postgres://u:p@localhost:5432/db", "CA_FILE": badCA}); err == nil {
		t.Fatal("invalid CA accepted")
	}
	parsed, err := cfg.driverConfig()
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TLSConfig == nil || parsed.TLSConfig.InsecureSkipVerify || parsed.TLSConfig.ServerName != "localhost" || len(parsed.Fallbacks) != 0 || parsed.Password != "p" || parsed.RuntimeParams["timezone"] != "UTC" {
		t.Fatal("TLS or explicit config altered")
	}
	noTLS, err := configValues(map[string]string{"URL": "postgres://u:p@127.0.0.1:5432/db", "TLS_MODE": "disable"})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := noTLS.driverConfig()
	if err != nil || plain.TLSConfig != nil || len(plain.Fallbacks) != 0 {
		t.Fatal("explicit disable changed")
	}
}
