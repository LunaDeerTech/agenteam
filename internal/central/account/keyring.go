package account

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type keyData struct {
	current string
	keys    map[string][32]byte
}
type Keyring struct{ data func() keyData }

func LoadKeyring(raw string, pagination cursor.Keyring, encryption secret.Keyring, download object.DownloadKeyring) (Keyring, error) {
	if len(raw) == 0 || len(raw) > 16<<10 || pagination.Validate() != nil || encryption.Validate() != nil || download.Validate() != nil {
		return Keyring{}, invalid()
	}
	canonical, err := cursor.CanonicalJSON([]byte(raw))
	if err != nil {
		return Keyring{}, invalid()
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(canonical, &wire) != nil || len(wire) != 3 || wire["format"] == nil || wire["current_kid"] == nil || wire["keys"] == nil {
		return Keyring{}, invalid()
	}
	var format int
	var current string
	var items []map[string]json.RawMessage
	if json.Unmarshal(wire["format"], &format) != nil || format != 1 || json.Unmarshal(wire["current_kid"], &current) != nil || !validKid(current) || json.Unmarshal(wire["keys"], &items) != nil || len(items) < 1 || len(items) > 32 {
		return Keyring{}, invalid()
	}
	d := keyData{current: current, keys: map[string][32]byte{}}
	for _, item := range items {
		var kid, encoded string
		if len(item) != 2 || item["kid"] == nil || item["key_b64"] == nil || json.Unmarshal(item["kid"], &kid) != nil || !validKid(kid) || json.Unmarshal(item["key_b64"], &encoded) != nil {
			return Keyring{}, invalid()
		}
		b, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(b) != 32 || base64.StdEncoding.EncodeToString(b) != encoded || pagination.ContainsMaterial(b) || encryption.ContainsMaterial(b) || download.ContainsMaterial(b) {
			clear(b)
			return Keyring{}, invalid()
		}
		_, exists := d.keys[kid]
		duplicate := 0
		for _, v := range d.keys {
			duplicate |= subtle.ConstantTimeCompare(v[:], b)
		}
		if exists || duplicate == 1 {
			clear(b)
			return Keyring{}, invalid()
		}
		var v [32]byte
		copy(v[:], b)
		clear(b)
		d.keys[kid] = v
	}
	if _, ok := d.keys[current]; !ok {
		return Keyring{}, invalid()
	}
	return Keyring{func() keyData { return d }}, nil
}
func validKid(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (k Keyring) Validate() error {
	if k.data == nil {
		return invalid()
	}
	return nil
}
func (k Keyring) current() string {
	if k.data == nil {
		return ""
	}
	return k.data().current
}
func (k Keyring) mac(kid, purpose string, parts ...[]byte) ([]byte, error) {
	if k.data == nil {
		return nil, unavailable(nil)
	}
	key, ok := k.data().keys[kid]
	if !ok {
		return nil, unavailable(nil)
	}
	switch purpose {
	case "command-v1", "anonymous-cookie-v1", "csrf-v1", "privacy-counter-v1":
	default:
		return nil, invalid()
	}
	h := hmac.New(sha256.New, key[:])
	_, _ = h.Write([]byte("agenteam.account.v1\x00"))
	var n [8]byte
	for _, b := range append([][]byte{[]byte(purpose)}, parts...) {
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		_, _ = h.Write(n[:])
		_, _ = h.Write(b)
	}
	return h.Sum(nil), nil
}
func (k Keyring) check(kid, purpose string, signature []byte, parts ...[]byte) error {
	want, e := k.mac(kid, purpose, parts...)
	if e != nil {
		return e
	}
	defer clear(want)
	if len(signature) != 32 || !hmac.Equal(want, signature) {
		return fault(foundation.Unauthenticated, nil)
	}
	return nil
}
func (k Keyring) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_keyring") }
func (k Keyring) MarshalJSON() ([]byte, error) { return []byte(`"account_keyring"`), nil }
func (*Keyring) UnmarshalJSON([]byte) error    { return invalid() }
func (k Keyring) LogValue() slog.Value         { return slog.StringValue("account_keyring") }
