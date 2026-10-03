package cursor

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const vectorKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
const vectorDigest = "sha256:a8050a9f4d064ea838ffe9295e3889982c9133e257d8c326f00e6c2fb0771d6b"

// Generated independently with Python json(sort_keys=True,separators=(',',':')),
// hashlib.sha256 and hmac.new(bytes(range(32)), b'agenteam.cursor.v1\x00'+H.P).
const vectorToken = "eyJraWQiOiJjMSIsInNpZ25hdHVyZV92ZXJzaW9uIjoxfQ.eyJmb3JtYXQiOjEsIm9yZGVyIjoiY3JlYXRlZF9hdDpkZXNjLGlkOmRlc2MiLCJwb3NpdGlvbiI6W3sidHlwZSI6Imluc3RhbnQiLCJ2YWx1ZSI6IjIwMjYtMTAtMDNUMTI6MzQ6NTYuMTIzNDU2WiJ9LHsidHlwZSI6InV1aWQiLCJ2YWx1ZSI6IjAxOTAwMDAwLTAwMDAtNzAwMC04MDAwLTAwMDAwMDAwMDAwMiJ9XSwicXVlcnlfZGlnZXN0Ijoic2hhMjU2OmE4MDUwYTlmNGQwNjRlYTgzOGZmZTkyOTVlMzg4OTk4MmM5MTMzZTI1N2Q4YzMyNmYwMGU2YzJmYjA3NzFkNmIiLCJzY29wZSI6eyJraW5kIjoicHJvamVjdCIsInByb2plY3RfaWQiOiIwMTkwMDAwMC0wMDAwLTcwMDAtODAwMC0wMDAwMDAwMDAwMDEifX0.PFw7MfaDXgqqWEmHGL8YajiJvb9NJJfuvn5BM1GhYdY"

func ring(t *testing.T) Keyring {
	t.Helper()
	k, e := LoadKeyring(`{"format":1,"current_kid":"c1","keys":[{"kid":"c1","key_b64":"` + vectorKey + `"}]}`)
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func binding(t *testing.T) Binding {
	t.Helper()
	p, _ := foundation.ParseID[identity.Project]("01900000-0000-7000-8000-000000000001")
	s, _ := identity.InProject(p)
	return Binding{s, foundation.Digest(vectorDigest), AuditOrder}
}
func TestIndependentCanonicalHMACVectorAndConcurrentUse(t *testing.T) {
	k, b := ring(t), binding(t)
	stamp, _ := foundation.ParseInstant("2026-10-03T12:34:56.123456Z")
	instant, _ := Instant(stamp)
	id, _ := UUID("01900000-0000-7000-8000-000000000002")
	d, e := Digest([]byte(`{"from":null,"action":"secret.create"}`))
	if e != nil || string(d) != vectorDigest {
		t.Fatal("independent canonical digest mismatch")
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			token, e := k.Sign(b, Position{Scalars: []Scalar{instant, id}})
			if e != nil || token != vectorToken {
				t.Error("independent HMAC bytes differ")
			}
			position, e := k.Verify(vectorToken, b)
			if e != nil || len(position.Scalars) != 2 || position.Scalars[0] != instant || position.Scalars[1] != id {
				t.Error("known vector rejected")
			}
		})
	}
	wg.Wait()
}

func TestGenerationIndependentStringVectorsAndBounds(t *testing.T) {
	k, b := ring(t), binding(t)
	parts := strings.Split(vectorToken, ".")
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	body, _ := base64.RawURLEncoding.DecodeString(parts[1])
	position, err := k.Verify(vectorToken, b)
	if err != nil {
		t.Fatal(err)
	}
	// These MACs were generated with Python json.dumps(sort_keys=True,
	// separators=(',', ':')) and hmac, independently of the Go codec.
	for _, tc := range []struct {
		generation int64
		mac        string
	}{
		{9007199254740993, "es1UArxLpU_NF-ISiV7_PPZYwzuiMpfsGOmQjavCNso"},
		{9223372036854775807, "jVWZliX0Iu4Y7y5QOtMCrPSuara5oH1JC6M1HIkoWO8"},
	} {
		g := tc.generation
		position.OrderGeneration = &g
		raw := strings.Replace(string(body), `"position":`, `"order_generation":"`+strconv.FormatInt(g, 10)+`","position":`, 1)
		expected := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(raw)) + "." + tc.mac
		got, err := k.Sign(b, position)
		if err != nil || got != expected {
			t.Fatalf("generation %d independent signature mismatch: %v", g, err)
		}
		decoded, err := k.Verify(expected, b)
		if err != nil || decoded.OrderGeneration == nil || *decoded.OrderGeneration != g {
			t.Fatalf("generation %d lost precision: %v", g, err)
		}
	}
	for _, rawGeneration := range []string{`9007199254740993`, `"01"`, `"0"`, `"-1"`, `"9223372036854775808"`, `null`, `"+1"`, `"1.0"`} {
		raw := strings.Replace(string(body), `"position":`, `"order_generation":`+rawGeneration+`,"position":`, 1)
		if _, err := k.Verify(signedRaw(header, []byte(raw)), b); err == nil {
			t.Errorf("invalid generation %s accepted", rawGeneration)
		}
	}
	for _, g := range []int64{0, -1} {
		position.OrderGeneration = &g
		if _, err := k.Sign(b, position); err == nil {
			t.Errorf("signed invalid generation %d", g)
		}
	}
}
func TestCursorRotationAndAllBindingFields(t *testing.T) {
	old, b := ring(t), binding(t)
	newKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{91}, 32))
	rotated, e := LoadKeyring(`{"format":1,"current_kid":"c2","keys":[{"kid":"c1","key_b64":"` + vectorKey + `"},{"kid":"c2","key_b64":"` + newKey + `"}]}`)
	if e != nil {
		t.Fatal(e)
	}
	position, e := rotated.Verify(vectorToken, b)
	if e != nil {
		t.Fatal("retained kid stopped verifying")
	}
	token, e := rotated.Sign(b, position)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = old.Verify(token, b); e == nil {
		t.Fatal("new key used old kid")
	}
	removed, e := LoadKeyring(`{"format":1,"current_kid":"c2","keys":[{"kid":"c2","key_b64":"` + newKey + `"}]}`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = removed.Verify(vectorToken, b); e == nil {
		t.Fatal("retired kid still accepted")
	}
	for _, change := range []func(*Binding){func(x *Binding) { x.Scope = identity.SystemScope() }, func(x *Binding) { x.QueryDigest = foundation.Digest("sha256:" + strings.Repeat("0", 64)) }, func(x *Binding) { x.Order = "id:asc" }} {
		changed := b
		change(&changed)
		if _, e = old.Verify(vectorToken, changed); e == nil {
			t.Fatal("changed scope/filter/order accepted")
		}
	}
}
func signedRaw(header, body []byte) string {
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(body)
	key, _ := base64.StdEncoding.DecodeString(vectorKey)
	m := hmac.New(sha256.New, key)
	m.Write([]byte("agenteam.cursor.v1\x00" + h + "." + p))
	return h + "." + p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func TestCursorRejectsMalformedCanonicalAndTypedEncoding(t *testing.T) {
	k, b := ring(t), binding(t)
	parts := strings.Split(vectorToken, ".")
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	body, _ := base64.RawURLEncoding.DecodeString(parts[1])
	cases := []string{"", strings.Repeat("x", MaxTokenBytes+1), vectorToken + ".extra", parts[0] + "=." + parts[1] + "." + parts[2], parts[0] + "." + parts[1] + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	for _, raw := range []string{`{"kid":"c1","kid":"c1","signature_version":1}`, `{"kid":"c1","signature_version":2}`, `{"Kid":"c1","signature_version":1}`, `{"kid":"c1","signature_version":1,"alg":"none"}`, `{"kid":"missing","signature_version":1}`, `{"signature_version":1,"kid":"c1"}`} {
		cases = append(cases, signedRaw([]byte(raw), body))
	}
	for _, raw := range []string{strings.Replace(string(body), `"format":1`, `"format":1,"format":1`, 1), strings.Replace(string(body), `"format":1`, `"format":1,"order_generation":0`, 1), strings.Replace(string(body), `"format":1`, `"format":1,"order_generation":null`, 1), strings.Replace(string(body), `"type":"instant"`, `"type":"secret"`, 1), strings.Replace(string(body), "123456Z", "1234567Z", 1), string(body) + "{}", strings.Replace(string(body), `"format":1`, `"format":1.0`, 1)} {
		cases = append(cases, signedRaw(header, []byte(raw)))
	}
	for i, token := range cases {
		if _, e := k.Verify(token, b); e == nil {
			t.Errorf("malformed cursor %d accepted", i)
		}
	}
	for _, raw := range []string{`{"x":1,"x":2}`, `{"x":1.0}`, `{"x":900000000000000000000}`, `{"x":-0}`, `{"x":NaN}`, `[] []`, string([]byte{'"', 0xff, '"'})} {
		if _, e := CanonicalJSON([]byte(raw)); e == nil {
			t.Fatal("ambiguous canonical encoding accepted")
		}
	}
}
func TestKeyringStrictnessAndSafeProjections(t *testing.T) {
	valid := `{"format":1,"current_kid":"c1","keys":[{"kid":"c1","key_b64":"` + vectorKey + `"}]}`
	cases := []string{"", strings.Repeat(" ", 16<<10) + valid, valid + "{}", strings.Replace(valid, `"format":1`, `"format":1,"format":1`, 1), strings.Replace(valid, `"format":1`, `"Format":1`, 1), strings.Replace(valid, `"format":1`, `"format":2`, 1), strings.Replace(valid, `"format":1`, `"format":1,"secret":"canary"`, 1), strings.Replace(valid, `"current_kid":"c1"`, `"current_kid":"unknown"`, 1), strings.Replace(valid, vectorKey, strings.TrimSuffix(vectorKey, "="), 1), strings.Replace(valid, vectorKey, vectorKey+"\n", 1), strings.Replace(valid, `}]}`, `},{"kid":"c1","key_b64":"`+vectorKey+`"}]}`, 1), strings.Replace(valid, `}]}`, `},{"kid":"c2","key_b64":"`+vectorKey+`"}]}`, 1), strings.Replace(valid, `"keys":[`, `"keys":null,"x":[`, 1)}
	for i, raw := range cases {
		k, e := LoadKeyring(raw)
		if e == nil {
			t.Errorf("invalid keyring %d accepted", i)
		}
		assertHidden(t, k, vectorKey)
		assertHidden(t, e, vectorKey)
	}
	k := ring(t)
	assertHidden(t, k, vectorKey)
	material, _ := base64.StdEncoding.DecodeString(vectorKey)
	if !k.ContainsMaterial(material) || k.ContainsMaterial([]byte("wrong")) {
		t.Fatal("cross-purpose key check incorrect")
	}
}
func assertHidden(t *testing.T, v any, secret string) {
	t.Helper()
	for _, x := range []any{v, struct{ private any }{v}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, x), secret) {
				t.Fatal("recursive formatting leaked key")
			}
		}
		b, _ := json.Marshal(x)
		if strings.Contains(string(b), secret) {
			t.Fatal("JSON leaked key")
		}
		for _, text := range []bool{false, true} {
			var buf bytes.Buffer
			var handler slog.Handler = slog.NewJSONHandler(&buf, nil)
			if text {
				handler = slog.NewTextHandler(&buf, nil)
			}
			slog.New(handler).LogAttrs(context.Background(), slog.LevelInfo, "projection", slog.Any("value", x))
			if strings.Contains(buf.String(), secret) {
				t.Fatal("logging leaked key")
			}
		}
	}
}
