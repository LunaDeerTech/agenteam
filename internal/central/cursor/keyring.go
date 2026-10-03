// Package cursor signs canonical, scoped pagination positions. Signatures are
// never authorization; list services must authorize every request separately.
package cursor

import (
	"crypto/hmac"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type Keyring struct{ data func() keys }
type keys struct {
	current string
	values  map[string][32]byte
}

func invalidKeyring() error {
	return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
}
func LoadKeyring(raw string) (Keyring, error) {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return Keyring{}, invalidKeyring()
	}
	var wire struct {
		Format  int    `json:"format"`
		Current string `json:"current_kid"`
		Keys    []struct {
			ID  string `json:"kid"`
			Key string `json:"key_b64"`
		} `json:"keys"`
	}
	if _, err := strict([]byte(raw), &wire); err != nil || wire.Format != 1 || !validKid(wire.Current) || len(wire.Keys) < 1 || len(wire.Keys) > 32 {
		return Keyring{}, invalidKeyring()
	}
	d := keys{current: wire.Current, values: map[string][32]byte{}}
	for _, key := range wire.Keys {
		b, err := base64.StdEncoding.Strict().DecodeString(key.Key)
		if !validKid(key.ID) || err != nil || len(b) != 32 || base64.StdEncoding.EncodeToString(b) != key.Key {
			return Keyring{}, invalidKeyring()
		}
		if _, exists := d.values[key.ID]; exists {
			return Keyring{}, invalidKeyring()
		}
		for _, old := range d.values {
			if hmac.Equal(old[:], b) {
				return Keyring{}, invalidKeyring()
			}
		}
		var material [32]byte
		copy(material[:], b)
		d.values[key.ID] = material
	}
	if _, ok := d.values[d.current]; !ok {
		return Keyring{}, invalidKeyring()
	}
	return Keyring{data: func() keys { return d }}, nil
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
		return invalidKeyring()
	}
	return nil
}

// ContainsMaterial is solely for rejecting cross-purpose deployment key reuse.
func (k Keyring) ContainsMaterial(material []byte) bool {
	if k.data == nil {
		return false
	}
	found := false
	for _, key := range k.data().values {
		if hmac.Equal(key[:], material) {
			found = true
		}
	}
	return found
}
func (k Keyring) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "cursor_keyring") }
func (k Keyring) MarshalJSON() ([]byte, error) { return []byte(`"cursor_keyring"`), nil }
func (k *Keyring) UnmarshalJSON([]byte) error  { return invalidKeyring() }
func (k Keyring) LogValue() slog.Value         { return slog.StringValue("cursor_keyring") }
