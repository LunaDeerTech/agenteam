// Package config validates Central process and explicit database settings.
// It does not load files automatically or configure future product modules.
package config

import (
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const Prefix = "AGENTEAM_CENTRAL_"

type LookupEnv func(string) (string, bool)

// Config is immutable after loading. Its zero value is invalid.
type Config struct {
	logLevel        slog.Level
	shutdownTimeout time.Duration
	httpAddr        string
	publicOrigin    string
	database        postgres.Config
}

func (c Config) LogLevel() slog.Level           { return c.logLevel }
func (c Config) ShutdownTimeout() time.Duration { return c.shutdownTimeout }
func (c Config) HTTPAddr() string               { return c.httpAddr }
func (c Config) PublicOrigin() string           { return c.publicOrigin }
func (c Config) Database() postgres.Config      { return c.database }

// Error contains a declared field name and stable reason, never an input value.
type Error struct {
	field, reason string
	cause         func() error
}

func (e *Error) Error() string  { return e.field + ": " + e.reason }
func (e *Error) Field() string  { return e.field }
func (e *Error) Reason() string { return e.reason }
func (e *Error) Unwrap() error {
	if e.cause == nil {
		return nil
	}
	return e.cause()
}

func invalid(field string) error { return &Error{field: Prefix + field, reason: "invalid"} }

// Load uses lookup only for known keys. env contains names or NAME=value entries
// solely to detect unsupported keys in this process's prefix.
func Load(lookup LookupEnv, env []string) (Config, error) {
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, Prefix) {
			continue
		}
		switch strings.TrimPrefix(key, Prefix) {
		case "LOG_LEVEL", "SHUTDOWN_TIMEOUT", "HTTP_ADDR", "PUBLIC_ORIGIN":
		case "DATABASE_URL", "DATABASE_TLS_MODE", "DATABASE_CA_FILE", "DATABASE_MAX_CONNS", "DATABASE_CONNECT_TIMEOUT", "DATABASE_STARTUP_TIMEOUT", "DATABASE_LOCK_TIMEOUT":
		default:
			return Config{}, &Error{field: Prefix + "*", reason: "unsupported"}
		}
	}
	if lookup == nil {
		return Config{}, &Error{field: Prefix + "*", reason: "unreadable"}
	}
	value := func(key, fallback string) string {
		if v, ok := lookup(Prefix + key); ok {
			return v
		}
		return fallback
	}
	c := Config{httpAddr: value("HTTP_ADDR", "127.0.0.1:8080")}
	switch value("LOG_LEVEL", "info") {
	case "debug":
		c.logLevel = slog.LevelDebug
	case "info":
		c.logLevel = slog.LevelInfo
	case "warn":
		c.logLevel = slog.LevelWarn
	case "error":
		c.logLevel = slog.LevelError
	default:
		return Config{}, invalid("LOG_LEVEL")
	}
	var err error
	c.shutdownTimeout, err = time.ParseDuration(value("SHUTDOWN_TIMEOUT", "10s"))
	if err != nil || c.shutdownTimeout < 100*time.Millisecond || c.shutdownTimeout > 5*time.Minute {
		return Config{}, invalid("SHUTDOWN_TIMEOUT")
	}
	if !validAddress(c.httpAddr) {
		return Config{}, invalid("HTTP_ADDR")
	}
	c.publicOrigin, err = normalizeOrigin(value("PUBLIC_ORIGIN", "http://localhost:8080"))
	if err != nil {
		return Config{}, invalid("PUBLIC_ORIGIN")
	}
	c.database, err = postgres.LoadConfig(postgres.LookupEnv(lookup), env)
	if err != nil {
		field := Prefix + "DATABASE_*"
		var issue *postgres.Error
		if errors.As(err, &issue) {
			if issue.Field() == "PG*" {
				field = "PG*"
			} else if issue.Field() != "" {
				field = Prefix + issue.Field()
			}
		}
		return Config{}, &Error{field: field, reason: "invalid", cause: func() error { return err }}
	}
	return c, nil
}

func (c Config) Validate() error {
	if c.logLevel != slog.LevelDebug && c.logLevel != slog.LevelInfo && c.logLevel != slog.LevelWarn && c.logLevel != slog.LevelError {
		return invalid("LOG_LEVEL")
	}
	if c.shutdownTimeout < 100*time.Millisecond || c.shutdownTimeout > 5*time.Minute {
		return invalid("SHUTDOWN_TIMEOUT")
	}
	if !validAddress(c.httpAddr) {
		return invalid("HTTP_ADDR")
	}
	if normalized, err := normalizeOrigin(c.publicOrigin); err != nil || normalized != c.publicOrigin {
		return invalid("PUBLIC_ORIGIN")
	}
	if c.database.Validate() != nil {
		return invalid("DATABASE_*")
	}
	return nil
}

func portNumber(port string) (int, bool) {
	if port == "" {
		return 0, false
	}
	for i := range len(port) {
		if port[i] < '0' || port[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(port)
	return n, err == nil && n >= 0 && n <= 65535
}

func validAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	n, ok := portNumber(port)
	if !ok || host != "" && !validHost(host) {
		return false
	}
	if n == 0 {
		ip, err := netip.ParseAddr(host)
		return err == nil && ip.IsLoopback() && ip.Zone() == ""
	}
	return true
}

func validHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	host = strings.TrimSuffix(host, ".")
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func normalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || strings.Contains(raw, "#") || u.Host == "" || !validHost(u.Hostname()) {
		return "", invalid("PUBLIC_ORIGIN")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" || u.EscapedPath() != "" && u.EscapedPath() != "/" {
		return "", invalid("PUBLIC_ORIGIN")
	}
	host := strings.ToLower(u.Hostname())
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Is4() {
			host = ip.String()
		} else {
			host = originIPv6(ip)
		}
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return "", invalid("PUBLIC_ORIGIN")
	}
	if port != "" {
		n, ok := portNumber(port)
		if !ok || n == 0 {
			return "", invalid("PUBLIC_ORIGIN")
		}
		port = strconv.Itoa(n)
	}
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, nil
}

// Browser origins serialize IPv6 as hexadecimal pieces, compressing the first
// longest zero run. netip.String uses a dotted tail for IPv4-mapped addresses;
// retaining all 128 bits here matches URL serialization without changing their
// IPv6 origin identity into an IPv4 identity.
func originIPv6(ip netip.Addr) string {
	bytes := ip.As16()
	pieces := make([]string, 8)
	bestStart, bestLength := -1, 1
	for i := 0; i < 8; {
		value := uint16(bytes[2*i])<<8 | uint16(bytes[2*i+1])
		pieces[i] = strconv.FormatUint(uint64(value), 16)
		if value != 0 {
			i++
			continue
		}
		start := i
		for i < 8 && bytes[2*i] == 0 && bytes[2*i+1] == 0 {
			pieces[i] = "0"
			i++
		}
		if i-start > bestLength {
			bestStart, bestLength = start, i-start
		}
	}
	if bestStart < 0 {
		return strings.Join(pieces, ":")
	}
	return strings.Join(pieces[:bestStart], ":") + "::" + strings.Join(pieces[bestStart+bestLength:], ":")
}
