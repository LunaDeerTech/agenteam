package object

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const EnvironmentPrefix = "AGENTEAM_CENTRAL_OBJECT_"

type storageConfig struct {
	endpoint, bucket, accessKey, secretKey string
	secure                                 bool
	roots                                  *x509.CertPool
}
type StorageConfig struct{ data func() storageConfig }

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// LoadStorageConfig reads only the named deployment channel. No default AWS
// credentials, environment proxy, discovery or business Secret is consulted.
// Central makes these settings mandatory in D05 B03; B01 is a library boundary.
func LoadStorageConfig(lookup func(string) (string, bool)) (StorageConfig, error) {
	if lookup == nil {
		return StorageConfig{}, invalid()
	}
	get := func(name string) string { v, _ := lookup(EnvironmentPrefix + name); return v }
	u, e := url.Parse(get("ENDPOINT"))
	if e != nil || u.User != nil || u.Opaque != "" || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Path != "" && u.Path != "/" || u.Scheme != "http" && u.Scheme != "https" {
		return StorageConfig{}, invalid()
	}
	if strings.ContainsAny(u.Hostname(), "%/\\ \t\r\n") || u.Hostname() == "" {
		return StorageConfig{}, invalid()
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return StorageConfig{}, invalid()
		}
	}
	bucket := get("BUCKET")
	if !bucketPattern.MatchString(bucket) || strings.Contains(bucket, "..") || strings.Contains(bucket, ".-") || strings.Contains(bucket, "-.") || net.ParseIP(bucket) != nil {
		return StorageConfig{}, invalid()
	}
	access, secret := get("ACCESS_KEY"), get("SECRET_KEY")
	for i, value := range []string{access, secret} {
		maximum := 512
		if i == 0 {
			maximum = 128
		}
		if value == "" || len(value) > maximum {
			return StorageConfig{}, invalid()
		}
		for _, r := range value {
			if r < 0x21 || r == 0x7f {
				return StorageConfig{}, invalid()
			}
		}
	}
	mode := get("TLS_MODE")
	if mode == "" {
		mode = "verify-full"
	}
	if mode != "verify-full" && mode != "disable" || mode == "verify-full" && u.Scheme != "https" || mode == "disable" && u.Scheme != "http" {
		return StorageConfig{}, invalid()
	}
	var roots *x509.CertPool
	ca := get("CA_FILE")
	if ca != "" {
		if mode != "verify-full" {
			return StorageConfig{}, invalid()
		}
		file, e := os.Open(ca)
		if e != nil {
			return StorageConfig{}, unavailable(e)
		}
		info, e := file.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			_ = file.Close()
			return StorageConfig{}, unavailable(e)
		}
		body, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		_ = file.Close()
		if e != nil || len(body) > 1<<20 {
			return StorageConfig{}, unavailable(e)
		}
		roots, e = x509.SystemCertPool()
		if e != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(body) {
			return StorageConfig{}, invalid()
		}
	}
	d := storageConfig{endpoint: u.Host, bucket: bucket, accessKey: access, secretKey: secret, secure: mode == "verify-full", roots: roots}
	return StorageConfig{func() storageConfig { return d }}, nil
}
func (c StorageConfig) Validate() error {
	if c.data == nil {
		return invalid()
	}
	return nil
}
func (c StorageConfig) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_storage_configuration")
}
func (c StorageConfig) MarshalJSON() ([]byte, error) {
	return []byte(`"object_storage_configuration"`), nil
}
func (*StorageConfig) UnmarshalJSON([]byte) error { return invalid() }
func (c StorageConfig) LogValue() slog.Value      { return slog.StringValue("object_storage_configuration") }
func (c StorageConfig) tlsConfig() *tls.Config {
	d := c.data()
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: d.roots}
}
