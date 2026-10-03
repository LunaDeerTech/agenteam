package secret

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func testCursorKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	k, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func testKeys(t *testing.T) Keyring {
	t.Helper()
	a, b := make([]byte, 32), make([]byte, 32)
	for i := range a {
		a[i] = byte(i + 32)
		b[i] = byte(i + 96)
	}
	k, err := LoadKeyring(`{"format":1,"current_version":"2","keys":[{"version":"1","key_b64":"`+base64.StdEncoding.EncodeToString(a)+`"},{"version":"2","key_b64":"`+base64.StdEncoding.EncodeToString(b)+`"}]}`, testCursorKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func vectorEnvelope(t *testing.T) envelope {
	t.Helper()
	id, _ := foundation.ParseID[payloadMarker]("01900000-0000-7000-8000-000000000002")
	nonce, _ := masterNonce(1)
	return envelope{id: id, scope: identity.SystemScope(), ownerKind: valueOwner, ownerID: "01900000-0000-7000-8000-000000000001", format: 1, algorithm: envelopeAlgorithm, dataNonce: unhex(t, "000102030405060708090a0b"), ciphertext: unhex(t, "3afa4c0c12ff30aab06ff0297f1d8c78614aaf735c93e046"), wrapNonce: nonce, wrappedDEK: unhex(t, "df1fbaf0956e266110c9c7b347c91c0904507a2fe6b1a3872e7f63410cb1f87db9c0acf87e57dda074c7d88584a11318"), masterVersion: 1, wrapRevision: 1}
}
func TestEnvelopeIndependentAESGCMVectorAndAAD(t *testing.T) {
	// Python cryptography AESGCM independently produced this full fixture from
	// key bytes 32..63, DEK 64..95 and the documented fixed binary AAD layout.
	p, k := vectorEnvelope(t), testKeys(t)
	aad, err := dataAAD(p)
	if err != nil || hex.EncodeToString(aad) != "6167656e7465616d2e7365637265742e646174612e763100000000010100000000000000000000000000000000010190000000007000800000000000000101900000000070008000000000000002" {
		t.Fatal("data AAD vector differs")
	}
	aad, err = wrapAAD(p)
	if err != nil || hex.EncodeToString(aad) != "6167656e7465616d2e7365637265742e777261702e7631000000000101000000000000000000000000000000000101900000000070008000000000000001019000000000700080000000000000020000000000000001000102030405060708090a0bcd5ba159cb4632127cba5744e7008e4e9654cec40a80f483b1716fa7dc414f50" {
		t.Fatal("wrap AAD vector differs")
	}
	plain, err := openEnvelope(k, p)
	if err != nil || !bytes.Equal(plain, []byte{0, 255, ' ', 't', 'e', 's', 't', '\n'}) {
		t.Fatal("independent ciphertext did not open")
	}
	clear(plain)
	key, _ := k.key(1)
	fp := keyFingerprint(key)
	if hex.EncodeToString(fp[:]) != "27b88984024b9263e5928bf13dd2479a39de1e40beffb66996340ad95d7882e3" {
		t.Fatal("master identity domain differs")
	}
	nonce, _ := masterNonce(2)
	rewrapped, err := rewrap(k, p, 2, nonce)
	if err != nil || !bytes.Equal(rewrapped.ciphertext, p.ciphertext) || !bytes.Equal(rewrapped.dataNonce, p.dataNonce) || rewrapped.wrapRevision != 2 || rewrapped.masterVersion != 2 {
		t.Fatal("rewrap changed business ciphertext")
	}
	plain, err = openEnvelope(k, rewrapped)
	if err != nil || !bytes.Equal(plain, []byte{0, 255, ' ', 't', 'e', 's', 't', '\n'}) {
		t.Fatal("rewrapped payload unusable")
	}
	clear(plain)
}
func TestEnvelopeRejectsEveryBoundIdentityAndCipherField(t *testing.T) {
	for name, change := range map[string]func(*envelope){
		"scope": func(p *envelope) {
			id, _ := foundation.ParseID[identity.Project]("01900000-0000-7000-8000-000000000003")
			p.scope, _ = identity.InProject(id)
		},
		"owner_kind": func(p *envelope) { p.ownerKind = receiptOwner }, "owner_id": func(p *envelope) { p.ownerID = "01900000-0000-7000-8000-000000000003" }, "payload_id": func(p *envelope) { p.id, _ = foundation.ParseID[payloadMarker]("01900000-0000-7000-8000-000000000003") },
		"format": func(p *envelope) { p.format = 2 }, "algorithm": func(p *envelope) { p.algorithm = "none" }, "master_version": func(p *envelope) { p.masterVersion = 2 }, "wrap_revision": func(p *envelope) { p.wrapRevision = 0 },
		"data_nonce": func(p *envelope) { p.dataNonce[0] ^= 1 }, "ciphertext": func(p *envelope) { p.ciphertext[0] ^= 1 }, "data_tag": func(p *envelope) { p.ciphertext[len(p.ciphertext)-1] ^= 1 }, "wrap_nonce": func(p *envelope) { p.wrapNonce[11] ^= 2 }, "wrapped_dek": func(p *envelope) { p.wrappedDEK[0] ^= 1 }, "wrap_tag": func(p *envelope) { p.wrappedDEK[len(p.wrappedDEK)-1] ^= 1 }, "short_wrap": func(p *envelope) { p.wrappedDEK = p.wrappedDEK[:32] }, "short_nonce": func(p *envelope) { p.dataNonce = p.dataNonce[:11] },
	} {
		t.Run(name, func(t *testing.T) {
			p := vectorEnvelope(t)
			change(&p)
			if value, err := openEnvelope(testKeys(t), p); err == nil || len(value) != 0 {
				t.Fatal("tampered envelope opened")
			}
		})
	}
}
func TestFreshEnvelopeBinaryBoundsCanaryAndNonceDomain(t *testing.T) {
	k := testKeys(t)
	nonce, _ := masterNonce(3)
	id, _ := foundation.ParseID[payloadMarker]("01900000-0000-7000-8000-000000000002")
	for _, size := range []int{1, 65536} {
		value := bytes.Repeat([]byte{255}, size)
		p, err := seal(k, 2, nonce, identity.SystemScope(), valueOwner, "01900000-0000-7000-8000-000000000001", id, value)
		if err != nil {
			t.Fatal(err)
		}
		got, err := openEnvelope(k, p)
		if err != nil || !bytes.Equal(got, value) {
			t.Fatal("binary value changed")
		}
		clear(got)
		nonce, _ = masterNonce(uint64(size + 4))
	}
	for _, size := range []int{0, 65537} {
		if _, err := seal(k, 2, nonce, identity.SystemScope(), valueOwner, "01900000-0000-7000-8000-000000000001", id, make([]byte, size)); err == nil {
			t.Fatal("invalid payload size accepted")
		}
	}
	if _, err := masterNonce(0); err == nil {
		t.Fatal("zero master counter accepted")
	}
	if _, err := masterNonce(maxMasterCounter + 1); err == nil {
		t.Fatal("overflow accepted")
	}
	nonce, _ = masterNonce(maxMasterCounter)
	if len(nonce) != 12 || string(nonce[:4]) != "ATK1" {
		t.Fatal("nonce domain incorrect")
	}
	encrypted, err := sealCanary(k, 2, nonce)
	if err != nil || verifyCanary(k, 2, nonce, encrypted) != nil {
		t.Fatal("canary invalid")
	}
	encrypted[0] ^= 1
	if verifyCanary(k, 2, nonce, encrypted) == nil {
		t.Fatal("damaged canary accepted")
	}
	if verifyCanary(k, 1, nonce, encrypted) == nil {
		t.Fatal("canary version switched")
	}
}
func TestMasterKeyringStrictIndependentAndSafe(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{71}, 32))
	valid := `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"` + key + `"}]}`
	for _, raw := range []string{"", strings.Repeat(" ", 16385), valid + "{}", strings.Replace(valid, `"format":1`, `"format":1,"format":1`, 1), strings.Replace(valid, `"format":1`, `"Format":1`, 1), strings.Replace(valid, `"current_version":"1"`, `"current_version":1`, 1), strings.Replace(valid, `"version":"1"`, `"version":"01"`, 1), strings.Replace(valid, `"current_version":"1"`, `"current_version":"2"`, 1), strings.Replace(valid, key, strings.TrimRight(key, "="), 1), strings.Replace(valid, key, "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=", 1), strings.Replace(valid, `}]}`, `},{"version":"2","key_b64":"`+key+`"}]}`, 1)} {
		ring, err := LoadKeyring(raw, testCursorKeys(t))
		if err == nil {
			t.Fatal("invalid keyring accepted")
		}
		safeSecret(t, ring, key)
		safeSecret(t, err, key)
	}
	ring, err := LoadKeyring(valid, testCursorKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	safeSecret(t, ring, key)
	versions := ring.Versions()
	versions[0] = 99
	if ring.Versions()[0] != 1 {
		t.Fatal("keyring version storage mutable")
	}
}
func safeSecret(t *testing.T, v any, secret string) {
	t.Helper()
	for _, outer := range []any{v, struct{ private any }{v}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, outer), secret) {
				t.Fatal("recursive format leaked")
			}
		}
		raw, _ := json.Marshal(outer)
		if strings.Contains(string(raw), secret) {
			t.Fatal("JSON leaked")
		}
		for _, text := range []bool{true, false} {
			var b bytes.Buffer
			var h slog.Handler = slog.NewJSONHandler(&b, nil)
			if text {
				h = slog.NewTextHandler(&b, nil)
			}
			slog.New(h).LogAttrs(context.Background(), slog.LevelInfo, "test", slog.Any("value", outer))
			if strings.Contains(b.String(), secret) {
				t.Fatal("slog leaked")
			}
		}
	}
}
