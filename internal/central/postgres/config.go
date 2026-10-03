package postgres

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const ConfigPrefix = "AGENTEAM_CENTRAL_DATABASE_"

type LookupEnv func(string) (string, bool)

// Config is immutable. The closure is intentional: even fmt traversing private
// fields of an enclosing object cannot reveal credentials or certificate paths.
type Config struct{ settings func() connectionSettings }
type connectionSettings struct {
	host, port, database, user, password, tlsMode string
	roots                                         *x509.CertPool
	maxConns                                      int32
	connectTimeout, startupTimeout, lockTimeout   time.Duration
}

func LoadConfig(lookup LookupEnv, environment []string) (Config, error) {
	if lookup == nil {
		return Config{}, invalidConfig("DATABASE")
	}
	if err := RejectPGEnvironment(environment); err != nil {
		return Config{}, err
	}
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, ConfigPrefix) {
			continue
		}
		switch strings.TrimPrefix(name, ConfigPrefix) {
		case "URL", "TLS_MODE", "CA_FILE", "MAX_CONNS", "CONNECT_TIMEOUT", "STARTUP_TIMEOUT", "LOCK_TIMEOUT":
		default:
			return Config{}, invalidConfig("DATABASE_*")
		}
	}
	value := func(name, fallback string) (string, error) {
		v, ok := lookup(ConfigPrefix + name)
		if !ok {
			return fallback, nil
		}
		if v == "" {
			return "", invalidConfig("DATABASE_" + name)
		}
		return v, nil
	}
	raw, err := value("URL", "")
	if err != nil || raw == "" || strings.ContainsRune(raw, 0) {
		return Config{}, invalidConfig("DATABASE_URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" || u.Opaque != "" || u.User == nil || u.User.Username() == "" || u.Host == "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return Config{}, invalidConfig("DATABASE_URL")
	}
	password, ok := u.User.Password()
	host, port, splitErr := net.SplitHostPort(u.Host)
	n, portErr := strconv.Atoi(port)
	if !ok || password == "" || splitErr != nil || portErr != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port || !databaseHost(host) || !strings.HasPrefix(u.Path, "/") || len(u.Path) < 2 || strings.Contains(u.Path[1:], "/") {
		return Config{}, invalidConfig("DATABASE_URL")
	}
	for _, v := range []string{host, u.Path, u.User.Username(), password} {
		if strings.ContainsRune(v, 0) {
			return Config{}, invalidConfig("DATABASE_URL")
		}
	}
	s := connectionSettings{host: host, port: port, database: u.Path[1:], user: u.User.Username(), password: password}
	s.tlsMode, err = value("TLS_MODE", "verify-full")
	if err != nil || s.tlsMode != "verify-full" && s.tlsMode != "disable" {
		return Config{}, invalidConfig("DATABASE_TLS_MODE")
	}
	ca, err := value("CA_FILE", "")
	if err != nil || ca != "" && s.tlsMode == "disable" {
		return Config{}, invalidConfig("DATABASE_CA_FILE")
	}
	if ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			return Config{}, invalidConfig("DATABASE_CA_FILE")
		}
		s.roots = x509.NewCertPool()
		if !s.roots.AppendCertsFromPEM(pem) {
			return Config{}, invalidConfig("DATABASE_CA_FILE")
		}
	}
	max, err := value("MAX_CONNS", "10")
	count, parseErr := strconv.Atoi(max)
	if err != nil || parseErr != nil || count < 2 || count > 100 || strconv.Itoa(count) != max {
		return Config{}, invalidConfig("DATABASE_MAX_CONNS")
	}
	s.maxConns = int32(count)
	for _, entry := range []struct {
		name, fallback   string
		minimum, maximum time.Duration
		dst              *time.Duration
	}{
		{"CONNECT_TIMEOUT", "5s", 100 * time.Millisecond, time.Minute, &s.connectTimeout},
		{"STARTUP_TIMEOUT", "2m", time.Second, 30 * time.Minute, &s.startupTimeout},
		{"LOCK_TIMEOUT", "5s", 100 * time.Millisecond, time.Minute, &s.lockTimeout},
	} {
		text, err := value(entry.name, entry.fallback)
		duration, parseErr := time.ParseDuration(text)
		if err != nil || parseErr != nil || duration < entry.minimum || duration > entry.maximum {
			return Config{}, invalidConfig("DATABASE_" + entry.name)
		}
		*entry.dst = duration
	}
	return Config{settings: func() connectionSettings { return s }}, nil
}
func databaseHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func RejectPGEnvironment(environment []string) error {
	for _, entry := range environment {
		name, value, present := strings.Cut(entry, "=")
		if !present || value == "" {
			continue
		}
		if strings.HasPrefix(name, "PGSSL") {
			return &Error{code: EnvironmentRejected, field: "PG*"}
		}
		switch name {
		case "PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS", "PGAPPNAME", "PGCONNECT_TIMEOUT", "PGTARGETSESSIONATTRS", "PGTZ", "PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION", "PGCHANNELBINDING", "PGREQUIREAUTH":
			return &Error{code: EnvironmentRejected, field: "PG*"}
		}
	}
	return nil
}
func (c Config) Validate() error {
	if c.settings == nil {
		return invalidConfig("DATABASE")
	}
	return nil
}
func (c Config) StartupTimeout() time.Duration {
	if c.settings == nil {
		return 0
	}
	return c.settings().startupTimeout
}
func (c Config) ConnectTimeout() time.Duration {
	if c.settings == nil {
		return 0
	}
	return c.settings().connectTimeout
}
func (c Config) LockTimeout() time.Duration {
	if c.settings == nil {
		return 0
	}
	return c.settings().lockTimeout
}
func (c Config) MaxConns() int32 {
	if c.settings == nil {
		return 0
	}
	return c.settings().maxConns
}
func (c Config) TLSMode() string {
	if c.settings == nil {
		return ""
	}
	return c.settings().tlsMode
}
func (c Config) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "database_config") }
func (c Config) MarshalJSON() ([]byte, error) { return []byte(`"database_config"`), nil }
func (c Config) LogValue() slog.Value         { return slog.StringValue("database_config") }

func (c Config) driverConfig() (*pgx.ConnConfig, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	// pgx consults the real process environment. Check again immediately before
	// parsing instead of changing global environment or trusting a test lookup.
	if err := RejectPGEnvironment(os.Environ()); err != nil {
		return nil, err
	}
	s := c.settings()
	quote := func(v string) string { return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(v) + "'" }
	dsn := "host=" + quote(s.host) + " port=" + s.port + " dbname=" + quote(s.database) + " user=" + quote(s.user) + " password=" + quote(s.password) + " sslmode=disable sslcert='' sslkey='' sslrootcert='' passfile='' servicefile='' target_session_attrs=any"
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, failure(InvalidConfiguration, err)
	}
	parsed.Fallbacks = nil
	parsed.TLSConfig = nil
	if s.tlsMode == "verify-full" {
		parsed.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.host, RootCAs: s.roots}
	}
	parsed.ConnectTimeout = s.connectTimeout
	parsed.RuntimeParams = map[string]string{"application_name": "agenteam", "timezone": "UTC", "search_path": "public"}
	return parsed, nil
}
