package object

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type downloadKeys struct {
	current string
	keys    map[string][32]byte
}

// DownloadKeyring is independent of pagination and encryption. Even recursive
// formatting of an enclosing private field cannot traverse its key material.
type DownloadKeyring struct{ data func() downloadKeys }

func LoadDownloadKeyring(raw string, pagination cursor.Keyring, encryption secret.Keyring) (DownloadKeyring, error) {
	if len(raw) == 0 || len(raw) > 16<<10 || pagination.Validate() != nil || encryption.Validate() != nil {
		return DownloadKeyring{}, invalid()
	}
	canonical, err := cursor.CanonicalJSON([]byte(raw))
	if err != nil {
		return DownloadKeyring{}, invalid()
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(canonical, &object) != nil || len(object) != 3 || object["format"] == nil || object["current_kid"] == nil || object["keys"] == nil {
		return DownloadKeyring{}, invalid()
	}
	var format int
	var current string
	var items []map[string]json.RawMessage
	if json.Unmarshal(object["format"], &format) != nil || format != 1 || json.Unmarshal(object["current_kid"], &current) != nil || !downloadKid(current) || json.Unmarshal(object["keys"], &items) != nil || len(items) < 1 || len(items) > 32 {
		return DownloadKeyring{}, invalid()
	}
	keys := make(map[string][32]byte, len(items))
	for _, item := range items {
		if len(item) != 2 || item["kid"] == nil || item["key_b64"] == nil {
			return DownloadKeyring{}, invalid()
		}
		var kid, encoded string
		if json.Unmarshal(item["kid"], &kid) != nil || !downloadKid(kid) || json.Unmarshal(item["key_b64"], &encoded) != nil {
			return DownloadKeyring{}, invalid()
		}
		material, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(material) != 32 || base64.StdEncoding.EncodeToString(material) != encoded || pagination.ContainsMaterial(material) || encryption.ContainsMaterial(material) {
			clear(material)
			return DownloadKeyring{}, invalid()
		}
		if _, exists := keys[kid]; exists {
			clear(material)
			return DownloadKeyring{}, invalid()
		}
		for _, previous := range keys {
			if hmac.Equal(previous[:], material) {
				clear(material)
				return DownloadKeyring{}, invalid()
			}
		}
		var key [32]byte
		copy(key[:], material)
		clear(material)
		keys[kid] = key
	}
	if _, exists := keys[current]; !exists {
		return DownloadKeyring{}, invalid()
	}
	d := downloadKeys{current: current, keys: keys}
	return DownloadKeyring{data: func() downloadKeys { return d }}, nil
}

func downloadKid(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for i := range len(value) {
		c := value[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (k DownloadKeyring) Validate() error {
	if k.data == nil {
		return invalid()
	}
	return nil
}
func (k DownloadKeyring) key(kid string) ([32]byte, bool) {
	if k.data == nil {
		return [32]byte{}, false
	}
	key, ok := k.data().keys[kid]
	return key, ok
}
func (k DownloadKeyring) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "download_keyring") }
func (k DownloadKeyring) MarshalJSON() ([]byte, error) {
	return []byte(`"download_keyring"`), nil
}
func (*DownloadKeyring) UnmarshalJSON([]byte) error { return invalid() }
func (k DownloadKeyring) LogValue() slog.Value      { return slog.StringValue("download_keyring") }
