package outbound

import (
	"crypto/tls"
	"net"
)

// SMTPClientTLSConfig is for the trusted SMTP adapter only. The adapter must
// not replace the roots, name or verification settings with request options.
// Both the config and root pool are copies, so mutations cannot alter the
// trust store used by another connection.
func (t TrustStore) SMTPClientTLSConfig(host string) (*tls.Config, error) {
	if t.Validate() != nil {
		return nil, invalid()
	}
	name, _, err := normalizeAuthority(net.JoinHostPort(host, "25"), "smtp")
	if err != nil || name != host {
		return nil, invalid()
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: name, RootCAs: t.roots().Clone()}, nil
}
