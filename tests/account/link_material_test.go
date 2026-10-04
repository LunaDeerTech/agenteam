//go:build integration

package account_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"strings"
	"testing"
)

// This fixture-only decoder obtains the randomly generated link from this
// test's isolated encrypted database and its explicit public test key. B02 has
// no SMTP/log sender yet; no production getter, URL, answer hook, or permission
// bypass is added to expose the material. Actual delivery leases are B03's port.
func fixtureLinkToken(t *testing.T, f *b02Fixture, kind c.TokenKind, id string) c.LinkToken {
	t.Helper()
	table := "invitations"
	if kind == c.PasswordResetToken {
		table = "password_resets"
	}
	var owner, payload string
	var dn, ct, wn, wrapped []byte
	var version uint64
	e := f.store.QueryRow(ctxFor(t), `SELECT p.owner_id::text,p.payload_id::text,p.data_nonce,p.ciphertext,p.wrap_nonce,p.wrapped_dek,p.master_version FROM agenteam_account.`+table+` l JOIN agenteam_secret.secrets s ON s.id=l.material_ref JOIN agenteam_secret.secret_payloads p ON p.payload_id=s.current_payload_id WHERE l.id=$1`, id).Scan(&owner, &payload, &dn, &ct, &wn, &wrapped, &version)
	if e != nil {
		t.Fatal(e)
	}
	uuid := func(raw string) []byte {
		v, e := hex.DecodeString(strings.ReplaceAll(raw, "-", ""))
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	aad := func(domain string) []byte {
		b := append([]byte(domain), 0)
		b = binary.BigEndian.AppendUint32(b, 1)
		b = append(b, 1)
		b = append(b, make([]byte, 16)...)
		b = append(b, 1)
		b = append(b, uuid(owner)...)
		return append(b, uuid(payload)...)
	}
	gcm := func(key []byte) cipher.AEAD {
		b, e := aes.NewCipher(key)
		if e != nil {
			t.Fatal(e)
		}
		a, e := cipher.NewGCM(b)
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	wa := aad("agenteam.secret.wrap.v1")
	wa = binary.BigEndian.AppendUint64(wa, version)
	wa = append(wa, dn...)
	sum := sha256.Sum256(ct)
	wa = append(wa, sum[:]...)
	dek, e := gcm(bytes.Repeat([]byte{2}, 32)).Open(nil, wn, wrapped, wa)
	if e != nil {
		t.Fatal("test envelope master", e)
	}
	defer clear(dek)
	raw, e := gcm(dek).Open(nil, dn, ct, aad("agenteam.secret.data.v1"))
	if e != nil {
		t.Fatal("test envelope", e)
	}
	defer clear(raw)
	m, e := sc.NewSecretMaterial(raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Destroy)
	if kind == c.InvitationToken {
		x, e := foundation.ParseID[c.Invitation](id)
		if e != nil {
			t.Fatal(e)
		}
		v, e := c.NewInvitationToken(x, m)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	x, e := foundation.ParseID[c.PasswordReset](id)
	if e != nil {
		t.Fatal(e)
	}
	v, e := c.NewResetToken(x, m)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
