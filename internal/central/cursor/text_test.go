package cursor

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestTextPreservesUTF8InMixedPositions(t *testing.T) {
	k, b := ring(t), binding(t)
	b.Order = "title:asc,id:asc"
	id, err := UUID("01900000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	tokens := make(map[string]bool)
	for _, value := range []string{"", " title ", "中文🙂", "é", "e\u0301", "\x00\n", "<>&\u2028\u2029", "�"} {
		scalar, err := Text(value)
		if err != nil || scalar.Kind() != "text" || scalar.Value() != value {
			t.Fatalf("UTF-8 value was rejected or changed: %q, %v", value, err)
		}
		token, err := k.Sign(b, Position{Scalars: []Scalar{scalar, id}})
		if err != nil {
			t.Fatal(err)
		}
		if tokens[token] {
			t.Fatal("distinct UTF-8 byte sequences shared a token")
		}
		tokens[token] = true
		got, err := k.Verify(token, b)
		if err != nil || len(got.Scalars) != 2 || got.Scalars[0] != scalar || got.Scalars[1] != id {
			t.Fatalf("mixed position did not preserve exact bytes: %q, %v", value, err)
		}
	}
}

func TestTextRejectsInvalidUTF8BeforeSigning(t *testing.T) {
	k, b := ring(t), binding(t)
	for _, raw := range [][]byte{{0xff}, {0x80}, {0xc0, 0xaf}, {0xed, 0xa0, 0x80}, {0xe4, 0xb8}} {
		value := string(raw)
		if scalar, err := Text(value); err == nil || scalar != (Scalar{}) {
			t.Fatalf("invalid UTF-8 returned a scalar: %x", raw)
		}
		// Reject before encoding/json can replace invalid bytes with U+FFFD.
		if token, err := k.Sign(b, Position{Scalars: []Scalar{{kind: "text", value: value}}}); err == nil || token != "" {
			t.Fatalf("invalid UTF-8 was silently repaired while signing: %x", raw)
		}
	}
	valid, _ := Text("valid")
	token, err := k.Sign(b, Position{Scalars: []Scalar{valid}})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	body, _ := base64.RawURLEncoding.DecodeString(parts[1])
	badBody := strings.Replace(string(body), `"value":"valid"`, "\"value\":\"\xff\"", 1)
	if _, err := k.Verify(signedRaw(header, []byte(badBody)), b); err == nil {
		t.Fatal("signed invalid UTF-8 wire value was accepted")
	}
}

func TestTextTokenKeepsEightKiBEnvelope(t *testing.T) {
	k, b := ring(t), binding(t)
	// Independently calculated canonical-v1/base64 envelope lengths for this
	// binding: 5821 ASCII value bytes produce 8191 token bytes; 5822 need 8193.
	within, err := Text(strings.Repeat("x", 5821))
	if err != nil {
		t.Fatal(err)
	}
	token, err := k.Sign(b, Position{Scalars: []Scalar{within}})
	if err != nil || len(token) != 8191 {
		t.Fatalf("valid boundary token failed: bytes=%d, %v", len(token), err)
	}
	if got, err := k.Verify(token, b); err != nil || len(got.Scalars) != 1 || got.Scalars[0] != within {
		t.Fatalf("valid boundary token did not round-trip: %v", err)
	}
	over, err := Text(strings.Repeat("x", 5822))
	if err != nil {
		t.Fatal("Text imposed a separate value limit")
	}
	if token, err := k.Sign(b, Position{Scalars: []Scalar{over}}); err == nil || token != "" {
		t.Fatal("text bypassed the 8 KiB token limit")
	}
}

func TestTextPositionRetainsFilterAndOrderBinding(t *testing.T) {
	k, b := ring(t), binding(t)
	b.Order = "title:asc,id:asc"
	var err error
	b.QueryDigest, err = Digest([]byte(`{"kind":"children","parent":null,"filter":"é"}`))
	if err != nil {
		t.Fatal(err)
	}
	title, _ := Text("é")
	id, _ := UUID("01900000-0000-7000-8000-000000000002")
	token, err := k.Sign(b, Position{Scalars: []Scalar{title, id}})
	if err != nil {
		t.Fatal(err)
	}
	changed := b
	changed.QueryDigest, err = Digest([]byte(`{"kind":"children","parent":null,"filter":"e\u0301"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Verify(token, changed); err == nil {
		t.Fatal("different filter bytes were accepted")
	}
	changed = b
	changed.Order = "id:asc,title:asc"
	if _, err := k.Verify(token, changed); err == nil {
		t.Fatal("different ordering was accepted")
	}
}

func TestTextAdditionKeepsLegacyThreeVariantToken(t *testing.T) {
	// Independent Python canonical JSON + HMAC vector, with the old instant,
	// UUID and >2^53 integer variants and the unchanged signature version.
	const token = "eyJraWQiOiJjMSIsInNpZ25hdHVyZV92ZXJzaW9uIjoxfQ.eyJmb3JtYXQiOjEsIm9yZGVyIjoiY3JlYXRlZF9hdDpkZXNjLGlkOmRlc2MiLCJwb3NpdGlvbiI6W3sidHlwZSI6Imluc3RhbnQiLCJ2YWx1ZSI6IjIwMjYtMTAtMDNUMTI6MzQ6NTYuMTIzNDU2WiJ9LHsidHlwZSI6InV1aWQiLCJ2YWx1ZSI6IjAxOTAwMDAwLTAwMDAtNzAwMC04MDAwLTAwMDAwMDAwMDAwMiJ9LHsidHlwZSI6ImludGVnZXIiLCJ2YWx1ZSI6IjkwMDcxOTkyNTQ3NDA5OTMifV0sInF1ZXJ5X2RpZ2VzdCI6InNoYTI1NjphODA1MGE5ZjRkMDY0ZWE4MzhmZmU5Mjk1ZTM4ODk5ODJjOTEzM2UyNTdkOGMzMjZmMDBlNmMyZmIwNzcxZDZiIiwic2NvcGUiOnsia2luZCI6InByb2plY3QiLCJwcm9qZWN0X2lkIjoiMDE5MDAwMDAtMDAwMC03MDAwLTgwMDAtMDAwMDAwMDAwMDAxIn19.9T6GztCnfPOb8Swn4pnB9Kq7-YVdLXd-S3mS88Yyd-Y"
	k, b := ring(t), binding(t)
	position, err := k.Verify(token, b)
	if err != nil || len(position.Scalars) != 3 {
		t.Fatalf("legacy token failed: %v", err)
	}
	if position.Scalars[0].Kind() != "instant" || position.Scalars[0].Value() != "2026-10-03T12:34:56.123456Z" ||
		position.Scalars[1].Kind() != "uuid" || position.Scalars[1].Value() != "01900000-0000-7000-8000-000000000002" ||
		position.Scalars[2] != Integer(9007199254740993) {
		t.Fatal("legacy scalar bytes or integer precision changed")
	}
	if got, err := k.Sign(b, position); err != nil || got != token {
		t.Fatalf("legacy token encoding changed: %v", err)
	}
}
