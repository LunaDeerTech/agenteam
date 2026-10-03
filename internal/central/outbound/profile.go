package outbound

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type TrustStore struct{ roots func() *x509.CertPool }

// LoadTrustStore extends system roots with an explicit deployment CA bundle.
// Consumers cannot supply per-request roots or a verification bypass.
func LoadTrustStore(file string) (TrustStore, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return TrustStore{}, unavailable(err)
	}
	if file != "" {
		f, e := os.Open(file)
		if e != nil {
			return TrustStore{}, unavailable(e)
		}
		raw, e := io.ReadAll(io.LimitReader(f, 1<<20+1))
		_ = f.Close()
		if e != nil || len(raw) > 1<<20 {
			return TrustStore{}, invalid()
		}
		count := 0
		for len(bytes.TrimSpace(raw)) > 0 {
			if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("-----BEGIN CERTIFICATE-----")) {
				return TrustStore{}, invalid()
			}
			block, rest := pem.Decode(raw)
			if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
				return TrustStore{}, invalid()
			}
			cert, e := x509.ParseCertificate(block.Bytes)
			if e != nil || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
				return TrustStore{}, invalid()
			}
			pool.AddCert(cert)
			count++
			raw = rest
		}
		if count == 0 {
			return TrustStore{}, invalid()
		}
	}
	return TrustStore{roots: func() *x509.CertPool { return pool }}, nil
}
func (t TrustStore) Validate() error {
	if t.roots == nil {
		return invalid()
	}
	return nil
}
func (t TrustStore) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_trust") }
func (t TrustStore) MarshalJSON() ([]byte, error) { return []byte(`"outbound_trust"`), nil }
func (t TrustStore) LogValue() slog.Value         { return slog.StringValue("outbound_trust") }

type Limits struct {
	DNS, Connect, TLS, ResponseHeaders, Overall, ReadIdle time.Duration
	RequestHeaderBytes, ResponseHeaderBytes               int
	RequestBodyBytes, ResponseBodyBytes                   int64
	Redirects                                             int
}

func normalizeLimits(l Limits, stream bool) (Limits, error) {
	max := Limits{DNS: 5 * time.Second, Connect: 5 * time.Second, TLS: 10 * time.Second, ResponseHeaders: 15 * time.Second, Overall: 120 * time.Second, ReadIdle: 120 * time.Second, RequestHeaderBytes: 64 << 10, ResponseHeaderBytes: 64 << 10, RequestBodyBytes: 16 << 20, ResponseBodyBytes: 16 << 20, Redirects: 5}
	if stream {
		max.Overall = 30 * time.Minute
		max.ReadIdle = 60 * time.Second
		max.ResponseBodyBytes = 256 << 20
	}
	durations := []*time.Duration{&l.DNS, &l.Connect, &l.TLS, &l.ResponseHeaders, &l.Overall, &l.ReadIdle}
	caps := []time.Duration{max.DNS, max.Connect, max.TLS, max.ResponseHeaders, max.Overall, max.ReadIdle}
	for i, p := range durations {
		if *p < 0 || *p > caps[i] {
			return Limits{}, invalid()
		}
		if *p == 0 {
			*p = caps[i]
		}
	}
	if l.ReadIdle > l.Overall {
		l.ReadIdle = l.Overall
	}
	ints := []*int{&l.RequestHeaderBytes, &l.ResponseHeaderBytes, &l.Redirects}
	icaps := []int{max.RequestHeaderBytes, max.ResponseHeaderBytes, max.Redirects}
	for i, p := range ints {
		if *p < 0 || *p > icaps[i] {
			return Limits{}, invalid()
		}
		if *p == 0 {
			*p = icaps[i]
		}
	}
	for i, p := range []*int64{&l.RequestBodyBytes, &l.ResponseBodyBytes} {
		cap := []int64{max.RequestBodyBytes, max.ResponseBodyBytes}[i]
		if *p < 0 || *p > cap {
			return Limits{}, invalid()
		}
		if *p == 0 {
			*p = cap
		}
	}
	return l, nil
}

type callData struct {
	actor        identity.Actor
	scope        identity.Scope
	key          ac.AppendKey
	associations ac.Associations
}
type CallContext struct{ data func() callData }

// NewCallContext carries the owner-provided audit cause, never a caller boolean
// authorizing the network operation. Session/Project checks remain real Audit
// ports; failure to append a denial cannot turn that denial into an allow.
func NewCallContext(actor identity.Actor, scope identity.Scope, key ac.AppendKey, associations ac.Associations) (CallContext, error) {
	if actor.Validate() != nil || scope.Validate() != nil || key.Validate() != nil || key.Details().Producer != ac.AccessProducer || associations.Validate() != nil {
		return CallContext{}, invalid()
	}
	resource, _ := ac.NewResource(ac.PolicyResource, "")
	metadata, _ := ac.DenialMetadata(ac.SystemConsumer, ac.AddressForbidden, 1)
	if _, e := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: ac.AccessDeny, Outcome: ac.Denied, Resource: resource, Metadata: metadata, Associations: associations}); e != nil {
		return CallContext{}, invalid()
	}
	if actor.Details().Kind == identity.Service && (actor.Details().ServiceName != identity.OutboundService || actor.Details().CauseRef != key.Details().CauseRef) {
		return CallContext{}, invalid()
	}
	d := callData{actor, scope, key, associations}
	return CallContext{data: func() callData { return d }}, nil
}
func (c CallContext) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_context") }
func (c CallContext) MarshalJSON() ([]byte, error) { return []byte(`"outbound_context"`), nil }
func (c CallContext) LogValue() slog.Value         { return slog.StringValue("outbound_context") }

type credentialField struct {
	name, prefix string
	material     sc.SecretMaterial
	query        bool
}
type CredentialField struct{ data func() credentialField }

func HeaderCredential(name, prefix string, material sc.SecretMaterial) (CredentialField, error) {
	if !headerToken(name) || connectionHeader(name) || len(prefix) > 64 || !headerValue(prefix) {
		return CredentialField{}, invalid()
	}
	d := credentialField{http.CanonicalHeaderKey(name), prefix, material, false}
	return CredentialField{data: func() credentialField { return d }}, nil
}
func QueryCredential(name string, material sc.SecretMaterial) (CredentialField, error) {
	if !headerToken(name) || len(name) > 128 {
		return CredentialField{}, invalid()
	}
	d := credentialField{name, "", material, true}
	return CredentialField{data: func() credentialField { return d }}, nil
}
func (c CredentialField) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "credential_field") }
func (c CredentialField) MarshalJSON() ([]byte, error) { return []byte(`"credential_field"`), nil }
func (c CredentialField) LogValue() slog.Value         { return slog.StringValue("credential_field") }

type bindingData struct {
	origin Origin
	fields []CredentialField
}
type CredentialBinding struct{ data func() bindingData }

func NewCredentialBinding(origin Origin, fields ...CredentialField) (CredentialBinding, error) {
	if !origin.Valid() || len(fields) == 0 || len(fields) > 32 {
		return CredentialBinding{}, invalid()
	}
	seen := map[string]bool{}
	owned := append([]CredentialField(nil), fields...)
	for _, f := range owned {
		if f.data == nil {
			return CredentialBinding{}, invalid()
		}
		v := f.data()
		key := fmt.Sprint(v.query) + ":" + v.name
		if seen[key] {
			return CredentialBinding{}, invalid()
		}
		seen[key] = true
	}
	d := bindingData{origin, owned}
	return CredentialBinding{data: func() bindingData { return d }}, nil
}
func (b CredentialBinding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "credential_binding")
}
func (b CredentialBinding) MarshalJSON() ([]byte, error) { return []byte(`"credential_binding"`), nil }
func (b CredentialBinding) LogValue() slog.Value         { return slog.StringValue("credential_binding") }

type ProfileOptions struct {
	Consumer             ac.Consumer
	AllowHTTP, Streaming bool
	Limits               Limits
	Context              CallContext
	Credentials          CredentialBinding
}
type Profile struct{ data func() ProfileOptions }

func NewProfile(options ProfileOptions) (Profile, error) {
	if !options.Consumer.Valid() || options.Consumer == ac.SMTP || options.Context.data == nil {
		return Profile{}, invalid()
	}
	l, err := normalizeLimits(options.Limits, options.Streaming)
	if err != nil {
		return Profile{}, err
	}
	options.Limits = l
	return Profile{data: func() ProfileOptions { return options }}, nil
}
func (p Profile) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_profile") }
func (p Profile) MarshalJSON() ([]byte, error) { return []byte(`"outbound_profile"`), nil }
func (p Profile) LogValue() slog.Value         { return slog.StringValue("outbound_profile") }

func headerToken(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, c := range s {
		if c <= 32 || c >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}\t", c) {
			return false
		}
	}
	return true
}
func headerValue(s string) bool {
	for i := range len(s) {
		if s[i] == '\r' || s[i] == '\n' || s[i] == 0 || s[i] < 32 && s[i] != '\t' || s[i] == 127 {
			return false
		}
	}
	return true
}
func connectionHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "connection", "proxy-connection", "proxy-authorization", "keep-alive", "upgrade", "transfer-encoding", "content-length", "trailer", "te", "expect":
		return true
	}
	return false
}
