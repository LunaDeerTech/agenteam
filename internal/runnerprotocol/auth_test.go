package runnerprotocol_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func authentication(t *testing.T) (p.Authentication, [ed25519.PublicKeySize]byte) {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	nonce, err := p.ParseNonce(base64.RawURLEncoding.EncodeToString(seed[:]))
	if err != nil {
		t.Fatal(err)
	}
	message, err := p.SigningBytes(p.ID("01900000-0000-7000-8000-000000000001"), nonce, "1760000000")
	if err != nil {
		t.Fatal(err)
	}
	const golden = "agenteam-runner-control-v1\n01900000-0000-7000-8000-000000000001\nAAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8\n1760000000\n"
	if string(message) != golden {
		t.Fatal("signing bytes differ from golden")
	}
	private := ed25519.NewKeyFromSeed(seed[:])
	signature := ed25519.Sign(private, message)
	// Produced independently with Python cryptography's Ed25519 implementation.
	const goldenSignature = "ea50dcd4b68193ae4d1904fee7cb08d1acd30775e5032992ef924ca92a37025c2d1f37d3b34c2ea478a37f92b1f2195de51ea6e4b63a6b790533d85019e35b02"
	if hex.EncodeToString(signature) != goldenSignature {
		t.Fatal("signature differs from fixed vector")
	}
	a, err := p.NewAuthentication(p.ID("01900000-0000-7000-8000-000000000001"), nonce, "1760000000", signature)
	if err != nil {
		t.Fatal(err)
	}
	var public [ed25519.PublicKeySize]byte
	copy(public[:], private[ed25519.SeedSize:])
	clear(signature)
	if !a.Verify(public) {
		t.Fatal("authentication aliased caller signature")
	}
	return a, public
}

func TestAuthenticationGoldenAndStrictHeader(t *testing.T) {
	a, public := authentication(t)
	header, err := p.EncodeAuthorization(a)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := p.DecodeAuthorization(header)
	if err != nil || decoded != a || !decoded.Verify(public) {
		t.Fatal("authentication roundtrip", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(header, "Runner "))
	if err != nil {
		t.Fatal(err)
	}
	valid := string(raw)
	for name, bad := range map[string]string{
		"extra":                   strings.Replace(valid, `"runner_id":`, `"extra":1,"runner_id":`, 1),
		"escaped_duplicate":       strings.Replace(valid, `"timestamp":"1760000000"`, `"timestamp":"1760000000","time\u0073tamp":"1760000000"`, 1),
		"null":                    strings.Replace(valid, `"nonce":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"`, `"nonce":null`, 1),
		"timestamp_number":        strings.Replace(valid, `"1760000000"`, `1760000000`, 1),
		"timestamp_plus":          strings.Replace(valid, `"1760000000"`, `"+1760000000"`, 1),
		"timestamp_padding":       strings.Replace(valid, `"1760000000"`, `"01760000000"`, 1),
		"timestamp_negative_zero": strings.Replace(valid, `"1760000000"`, `"-0"`, 1),
		"bad_id":                  strings.Replace(valid, `-7000-`, `-4000-`, 1),
		"nonce_padding":           strings.Replace(valid, `GxwdHh8"`, `GxwdHh8="`, 1),
		"signature_padding":       strings.Replace(valid, `"signature":"`, `"signature":"=`, 1),
		"tail":                    valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			value, e := p.DecodeAuthorization("Runner " + base64.RawURLEncoding.EncodeToString([]byte(bad)))
			if e == nil || value.Valid() {
				t.Fatal("invalid authentication admitted")
			}
		})
	}
	for _, bad := range []string{strings.ToLower(header[:6]) + header[6:], " " + header, header + "=", header + "\n", "Runner  " + header[7:], strings.Repeat("x", p.MaxAuthorizationBytes+1)} {
		if value, e := p.DecodeAuthorization(bad); e == nil || value.Valid() {
			t.Fatal("invalid header admitted")
		}
	}
	for _, change := range [][2]string{{"1760000000", "1760000001"}, {"000000000001", "000000000002"}, {"AAECAwQFBgcICQoL", "BAECAwQFBgcICQoL"}} {
		value, e := p.DecodeAuthorization("Runner " + base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(valid, change[0], change[1], 1))))
		if e != nil || value.Verify(public) {
			t.Fatal("signed field mutation did not invalidate signature", e)
		}
	}
}

func TestAuthenticationSafeOutputAndNonce(t *testing.T) {
	a, _ := authentication(t)
	nonce, _ := a.Nonce().Wire()
	header, _ := p.EncodeAuthorization(a)
	var buffer bytes.Buffer
	slog.New(slog.NewJSONHandler(&buffer, nil)).Info("authentication", "auth", a, "nonce", a.Nonce())
	raw, _ := json.Marshal(a)
	nonceJSON, _ := json.Marshal(a.Nonce())
	safe := fmt.Sprintf("%#v %+v %s %s", a, a.Nonce(), raw, nonceJSON) + buffer.String()
	for _, secret := range []string{nonce, header, string(a.RunnerID()), string(a.Timestamp())} {
		if strings.Contains(safe, secret) {
			t.Fatal("default diagnostic exposed authentication")
		}
	}
	if _, err := p.SigningBytes(a.RunnerID(), p.Nonce{}, a.Timestamp()); err == nil {
		t.Fatal("zero nonce admitted")
	}
	if _, err := p.EncodeAuthorization(p.Authentication{}); err == nil {
		t.Fatal("zero authentication encoded")
	}
	for _, invalid := range []string{"", nonce + "=", nonce[:42] + "9", strings.Repeat("A", 44)} {
		if _, err := p.ParseNonce(invalid); err == nil {
			t.Fatal("noncanonical nonce admitted")
		}
	}
	if first, err := p.NewNonce(); err != nil || !first.Valid() {
		t.Fatal("nonce entropy", err)
	}
	for _, seconds := range []int64{-1, 0, 1, -9223372036854775808, 9223372036854775807} {
		if got, err := p.NewUnixSeconds(seconds).Seconds(); err != nil || got != seconds {
			t.Fatal("canonical seconds", err)
		}
	}
}
