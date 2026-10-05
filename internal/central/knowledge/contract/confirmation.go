package contract

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const DefaultConfirmationLifetime = 10 * time.Minute
const MaxConfirmationBytes = 8 << 10
const confirmationDomain = "agenteam.knowledge.delete-confirmation.v1"

type DeleteConfirmationClaims struct {
	UserID      id.UserID    `json:"user_id"`
	ProjectID   id.ProjectID `json:"project_id"`
	RootID      DocumentID   `json:"root_id"`
	ScopeDigest f.Digest     `json:"scope_digest"`
	ExpiresAt   f.Instant    `json:"expires_at"`
}

func (c DeleteConfirmationClaims) Validate() error {
	if c.UserID.Validate() != nil || c.ProjectID.Validate() != nil || c.RootID.Validate() != nil || c.ScopeDigest.Validate() != nil || c.ExpiresAt.Validate() != nil {
		return invalid()
	}
	return nil
}

type confirmationHeader struct {
	Version int    `json:"signature_version"`
	Kid     string `json:"kid"`
}
type confirmationData struct {
	raw      string
	header   confirmationHeader
	claims   DeleteConfirmationClaims
	mac      [32]byte
	unsigned string
}
type ConfirmationToken struct{ data func() confirmationData }

func validKid(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func parsePart(s string) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(s)
	if e != nil || len(b) == 0 || base64.RawURLEncoding.EncodeToString(b) != s {
		return nil, invalid()
	}
	return b, nil
}
func ParseConfirmationToken(raw string) (ConfirmationToken, error) {
	if len(raw) == 0 || len(raw) > MaxConfirmationBytes {
		return ConfirmationToken{}, invalid()
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return ConfirmationToken{}, invalid()
	}
	hb, e := parsePart(parts[0])
	if e != nil {
		return ConfirmationToken{}, e
	}
	pb, e := parsePart(parts[1])
	if e != nil {
		return ConfirmationToken{}, e
	}
	mb, e := parsePart(parts[2])
	if e != nil || len(mb) != sha256.Size {
		return ConfirmationToken{}, invalid()
	}
	h, e := decode[confirmationHeader](hb, []string{"signature_version", "kid"}, nil, nil)
	if e != nil || h.Version != 1 || !validKid(h.Kid) {
		return ConfirmationToken{}, invalid()
	}
	var claims DeleteConfirmationClaims
	if json.Unmarshal(pb, &claims) != nil {
		return ConfirmationToken{}, invalid()
	}
	// Only the canonical signed representation is admitted. Verification still
	// authenticates the original H.P bytes, never a reconstructed message.
	ch, e := cursor.CanonicalJSON(hb)
	if e != nil || !bytes.Equal(ch, hb) {
		return ConfirmationToken{}, invalid()
	}
	cp, e := cursor.CanonicalJSON(pb)
	if e != nil || !bytes.Equal(cp, pb) {
		return ConfirmationToken{}, invalid()
	}
	var mac [32]byte
	copy(mac[:], mb)
	d := confirmationData{raw, h, claims, mac, parts[0] + "." + parts[1]}
	return ConfirmationToken{func() confirmationData { return d }}, nil
}
func (t ConfirmationToken) Validate() error {
	if t.data == nil {
		return invalid()
	}
	return nil
}
func (t ConfirmationToken) ForHumanResponse() string {
	if t.data == nil {
		return ""
	}
	return t.data().raw
}

type keyEntry struct {
	Kid string `json:"kid"`
	Key string `json:"key_b64"`
}

func (k *keyEntry) UnmarshalJSON(b []byte) error {
	type w keyEntry
	v, e := decode[w](b, []string{"kid", "key_b64"}, nil, nil)
	if e == nil {
		*k = keyEntry(v)
	}
	return e
}

type keyData struct {
	current string
	values  map[string][32]byte
}
type ConfirmationKeys struct{ data func() keyData }

func LoadConfirmationKeys(raw string) (ConfirmationKeys, error) {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return ConfirmationKeys{}, invalid()
	}
	v, e := decode[struct {
		Format  int        `json:"format"`
		Current string     `json:"current_kid"`
		Keys    []keyEntry `json:"keys"`
	}]([]byte(raw), []string{"format", "current_kid", "keys"}, nil, nil)
	if e != nil || v.Format != 1 || !validKid(v.Current) || len(v.Keys) < 1 || len(v.Keys) > 32 {
		return ConfirmationKeys{}, invalid()
	}
	d := keyData{v.Current, make(map[string][32]byte)}
	for _, entry := range v.Keys {
		b, e := base64.StdEncoding.Strict().DecodeString(entry.Key)
		if e != nil || len(b) != 32 || base64.StdEncoding.EncodeToString(b) != entry.Key || !validKid(entry.Kid) {
			return ConfirmationKeys{}, invalid()
		}
		if _, found := d.values[entry.Kid]; found {
			return ConfirmationKeys{}, invalid()
		}
		for _, old := range d.values {
			if hmac.Equal(old[:], b) {
				return ConfirmationKeys{}, invalid()
			}
		}
		var key [32]byte
		copy(key[:], b)
		d.values[entry.Kid] = key
	}
	if _, found := d.values[d.current]; !found {
		return ConfirmationKeys{}, invalid()
	}
	return ConfirmationKeys{func() keyData { return d }}, nil
}
func (k ConfirmationKeys) Validate() error {
	if k.data == nil {
		return invalid()
	}
	return nil
}
func canonicalPart(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", invalid()
	}
	b, e = cursor.CanonicalJSON(b)
	if e != nil {
		return "", invalid()
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func confirmationMAC(key [32]byte, unsigned string) []byte {
	h := hmac.New(sha256.New, key[:])
	h.Write([]byte(confirmationDomain))
	h.Write([]byte{0})
	h.Write([]byte(unsigned))
	return h.Sum(nil)
}
func (k ConfirmationKeys) Sign(c DeleteConfirmationClaims) (ConfirmationToken, error) {
	if k.Validate() != nil || c.Validate() != nil {
		return ConfirmationToken{}, invalid()
	}
	d := k.data()
	header, e := canonicalPart(confirmationHeader{1, d.current})
	if e != nil {
		return ConfirmationToken{}, e
	}
	payload, e := canonicalPart(c)
	if e != nil {
		return ConfirmationToken{}, e
	}
	unsigned := header + "." + payload
	return ParseConfirmationToken(unsigned + "." + base64.RawURLEncoding.EncodeToString(confirmationMAC(d.values[d.current], unsigned)))
}
func (k ConfirmationKeys) Verify(t ConfirmationToken, now f.Instant) (DeleteConfirmationClaims, error) {
	if k.Validate() != nil || t.Validate() != nil || now.Validate() != nil {
		return DeleteConfirmationClaims{}, invalid()
	}
	d := t.data()
	key, ok := k.data().values[d.header.Kid]
	if !ok || !hmac.Equal(d.mac[:], confirmationMAC(key, d.unsigned)) {
		return DeleteConfirmationClaims{}, invalid()
	}
	if !now.Time().Before(d.claims.ExpiresAt.Time()) {
		return DeleteConfirmationClaims{}, fault(f.ConfirmationStale)
	}
	return d.claims, nil
}

func (v DeleteConfirmationClaims) MarshalJSON() ([]byte, error) {
	type wire DeleteConfirmationClaims
	return checked(wire(v), v.Validate())
}
func (v *DeleteConfirmationClaims) UnmarshalJSON(b []byte) error {
	type wire DeleteConfirmationClaims
	w, e := decode[wire](b, []string{"user_id", "project_id", "root_id", "scope_digest", "expires_at"}, nil, nil)
	if e != nil {
		return e
	}
	n := DeleteConfirmationClaims(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (ConfirmationToken) Format(w fmt.State, _ rune) { io.WriteString(w, "knowledge_confirmation") }
func (ConfirmationToken) MarshalJSON() ([]byte, error) {
	return []byte(`"knowledge_confirmation"`), nil
}
func (*ConfirmationToken) UnmarshalJSON([]byte) error { return invalid() }
func (ConfirmationToken) LogValue() slog.Value        { return slog.StringValue("knowledge_confirmation") }

func (ConfirmationKeys) Format(w fmt.State, _ rune) { io.WriteString(w, "knowledge_confirmation_keys") }
func (ConfirmationKeys) MarshalJSON() ([]byte, error) {
	return []byte(`"knowledge_confirmation_keys"`), nil
}
func (*ConfirmationKeys) UnmarshalJSON([]byte) error { return invalid() }
func (ConfirmationKeys) LogValue() slog.Value        { return slog.StringValue("knowledge_confirmation_keys") }
