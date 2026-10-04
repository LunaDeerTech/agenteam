package secret

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
)

func TestKeyringContainsCurrentAndRetainedMaterial(t *testing.T) {
	encode := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	c, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, encode(3)))
	if err != nil {
		t.Fatal(err)
	}
	k, err := LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"2","keys":[{"version":"1","key_b64":%q},{"version":"2","key_b64":%q}]}`, encode(1), encode(2)), c)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []byte{1, 2} {
		material := bytes.Repeat([]byte{b}, 32)
		if !k.ContainsMaterial(material) {
			t.Fatal("missing retained/current material")
		}
		material[0] ^= 0xff
		if k.ContainsMaterial(material) {
			t.Fatal("partial match accepted")
		}
	}
	for _, material := range [][]byte{nil, {}, bytes.Repeat([]byte{1}, 31), bytes.Repeat([]byte{1}, 33), bytes.Repeat([]byte{3}, 32)} {
		if k.ContainsMaterial(material) || (Keyring{}).ContainsMaterial(material) {
			t.Fatal("invalid material accepted")
		}
	}
}
