package secret

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type keyringData struct {
	current foundation.Version
	keys    map[foundation.Version][32]byte
}
type Keyring struct{ data func() keyringData }

func LoadKeyring(raw string, cursorKeys cursor.Keyring) (Keyring, error) {
	bad := func() (Keyring, error) {
		return Keyring{}, failure(InvalidConfiguration, foundation.InvalidArgument, nil)
	}
	if len(raw) == 0 || len(raw) > 16<<10 || cursorKeys.Validate() != nil {
		return bad()
	}
	canonical, err := cursor.CanonicalJSON([]byte(raw))
	if err != nil {
		return bad()
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(canonical, &object) != nil || len(object) != 3 || object["format"] == nil || object["current_version"] == nil || object["keys"] == nil {
		return bad()
	}
	var format int
	var current foundation.Version
	var items []map[string]json.RawMessage
	if json.Unmarshal(object["format"], &format) != nil || format != 1 || json.Unmarshal(object["current_version"], &current) != nil || current.Validate() != nil || json.Unmarshal(object["keys"], &items) != nil || len(items) < 1 || len(items) > 32 {
		return bad()
	}
	keys := map[foundation.Version][32]byte{}
	for _, item := range items {
		if len(item) != 2 || item["version"] == nil || item["key_b64"] == nil {
			return bad()
		}
		var version foundation.Version
		var encoded string
		if json.Unmarshal(item["version"], &version) != nil || version.Validate() != nil || json.Unmarshal(item["key_b64"], &encoded) != nil {
			return bad()
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != encoded || cursorKeys.ContainsMaterial(decoded) {
			clear(decoded)
			return bad()
		}
		var key [32]byte
		copy(key[:], decoded)
		clear(decoded)
		if _, exists := keys[version]; exists {
			return bad()
		}
		for _, previous := range keys {
			if subtle.ConstantTimeCompare(key[:], previous[:]) == 1 {
				return bad()
			}
		}
		keys[version] = key
	}
	if _, ok := keys[current]; !ok {
		return bad()
	}
	d := keyringData{current, keys}
	return Keyring{data: func() keyringData { return d }}, nil
}
func (k Keyring) Validate() error {
	if k.data == nil {
		return failure(InvalidConfiguration, foundation.InvalidArgument, nil)
	}
	return nil
}
func (k Keyring) CurrentVersion() foundation.Version {
	if k.data == nil {
		return 0
	}
	return k.data().current
}
func (k Keyring) Versions() []foundation.Version {
	if k.data == nil {
		return nil
	}
	out := make([]foundation.Version, 0, len(k.data().keys))
	for version := range k.data().keys {
		out = append(out, version)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func (k Keyring) key(version foundation.Version) ([32]byte, bool) {
	if k.data == nil {
		return [32]byte{}, false
	}
	key, ok := k.data().keys[version]
	return key, ok
}
func (k Keyring) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "secret_keyring") }
func (k Keyring) MarshalJSON() ([]byte, error) { return []byte(`"secret_keyring"`), nil }
func (k *Keyring) UnmarshalJSON([]byte) error  { return invalid() }
func (k Keyring) LogValue() slog.Value         { return slog.StringValue("secret_keyring") }
func keyFingerprint(key [32]byte) [32]byte {
	return sha256.Sum256(bytes.Join([][]byte{[]byte("agenteam.master.identity.v1"), {0}, key[:]}, nil))
}
