package contract_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	k "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func keyJSON(current string, old, new bool) string {
	var keys []map[string]string
	if old {
		keys = append(keys, map[string]string{"kid": "old", "key_b64": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))})
	}
	if new {
		keys = append(keys, map[string]string{"kid": "new", "key_b64": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))})
	}
	return string(must(json.Marshal(struct {
		Format  int                 `json:"format"`
		Current string              `json:"current_kid"`
		Keys    []map[string]string `json:"keys"`
	}{1, current, keys})))
}
func claims() k.DeleteConfirmationClaims {
	return k.DeleteConfirmationClaims{UserID: keyID[id.User](1), ProjectID: keyID[id.Project](4), RootID: keyID[k.Document](10), ScopeDigest: sha(), ExpiresAt: instant("2026-01-01T00:10:00Z")}
}
func token() k.ConfirmationToken {
	return must(must(k.LoadConfirmationKeys(keyJSON("old", true, false))).Sign(claims()))
}
func TestConfirmationIndependentMACVectorAndExpiry(t *testing.T) {
	keys := must(k.LoadConfirmationKeys(keyJSON("old", true, false)))
	c := claims()
	signed := must(keys.Sign(c))
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"kid":"old","signature_version":1}`))
	p := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"expires_at":"2026-01-01T00:10:00.000000Z","project_id":"%s","root_id":"%s","scope_digest":"%s","user_id":"%s"}`, c.ProjectID, c.RootID, c.ScopeDigest, c.UserID)))
	unsigned := h + "." + p
	mac := hmac.New(sha256.New, bytes.Repeat([]byte{1}, 32))
	mac.Write([]byte("agenteam.knowledge.delete-confirmation.v1\x00" + unsigned))
	expected := unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if signed.ForHumanResponse() != expected {
		t.Fatal("signed protocol vector differs")
	}
	got := must(keys.Verify(signed, instant("2026-01-01T00:09:59Z")))
	if got != c {
		t.Fatal("claims changed")
	}
	_, e := keys.Verify(signed, c.ExpiresAt)
	requireError(t, e)
	var fault *f.Fault
	if !errors.As(e, &fault) || fault.Code != f.ConfirmationStale {
		t.Fatal("wrong expiry fault")
	}
	// The business digest is pure. Completed receipt replay need not retain a
	// verification key or revalidate an expired token.
	h1 := must(k.DeleteDigest(actor(), meta(), c.ProjectID, c.RootID, signed))
	parsed := must(k.ParseConfirmationToken(expected))
	if must(k.DeleteDigest(actor(), meta(), c.ProjectID, c.RootID, parsed)) != h1 {
		t.Fatal("transport roundtrip changed command")
	}
	if k.DefaultConfirmationLifetime.Minutes() != 10 {
		t.Fatal("confirmation default changed")
	}
}
func TestConfirmationRotationAndTamper(t *testing.T) {
	old := token()
	rotated := must(k.LoadConfirmationKeys(keyJSON("new", true, true)))
	requireOK(t, func() error { _, e := rotated.Verify(old, instant("2026-01-01T00:00:00Z")); return e }())
	newer := must(rotated.Sign(claims()))
	part := strings.Split(newer.ForHumanResponse(), ".")[0]
	header := string(must(base64.RawURLEncoding.DecodeString(part)))
	if !strings.Contains(header, `"kid":"new"`) {
		t.Fatal("signed with historical key")
	}
	removed := must(k.LoadConfirmationKeys(keyJSON("new", false, true)))
	_, e := removed.Verify(old, instant("2026-01-01T00:00:00Z"))
	requireError(t, e)
	parts := strings.Split(old.ForHumanResponse(), ".")
	payload := string(must(base64.RawURLEncoding.DecodeString(parts[1])))
	payload = strings.Replace(payload, keyID[id.User](1).String(), keyID[id.User](2).String(), 1)
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(payload))
	tampered := must(k.ParseConfirmationToken(strings.Join(parts, ".")))
	_, e = rotated.Verify(tampered, instant("2026-01-01T00:00:00Z"))
	requireError(t, e)
	changed := claims()
	changed.ScopeDigest = f.Digest("sha256:" + strings.Repeat("b", 64))
	other := must(rotated.Sign(changed))
	if must(k.DeleteDigest(actor(), meta(), changed.ProjectID, changed.RootID, other)) == must(k.DeleteDigest(actor(), meta(), changed.ProjectID, changed.RootID, old)) {
		t.Fatal("raw token excluded from command meaning")
	}
}
func TestConfirmationStrictTransportAndKeys(t *testing.T) {
	original := token().ForHumanResponse()
	parts := strings.Split(original, ".")
	for _, header := range []string{`{"kid":"old","signature_version":2}`, `{"kid":"old","kid":"old","signature_version":1}`, `{"kid":"old","signature_version":1,"alg":"none"}`, `{"kid":null,"signature_version":1}`, `{"signature_version":1,"kid":"old"}`} {
		bad := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + parts[1] + "." + parts[2]
		_, e := k.ParseConfirmationToken(bad)
		requireError(t, e)
	}
	for _, bad := range []string{"", original + "=", original + ".extra", strings.Repeat("a", k.MaxConfirmationBytes+1), parts[0] + ".." + parts[2], parts[0] + "." + parts[1] + ".AA"} {
		_, e := k.ParseConfirmationToken(bad)
		requireError(t, e)
	}
	valid := keyJSON("old", true, false)
	for _, bad := range []string{
		"{}", strings.Replace(valid, `"format":1`, `"format":null`, 1),
		strings.TrimSuffix(valid, "}") + `,"format":1}`,
		strings.Replace(valid, `"current_kid":"old"`, `"current_kid":"missing"`, 1),
		strings.Replace(valid, `"key_b64":`, `"secret":`, 1),
		strings.Replace(valid, `"kid":"old"`, `"kid":""`, 1),
	} {
		_, e := k.LoadConfirmationKeys(bad)
		requireError(t, e)
	}
	material := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	duplicate := fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":"%s"},{"kid":"b","key_b64":"%s"}]}`, material, material)
	_, e := k.LoadConfirmationKeys(duplicate)
	requireError(t, e)
	zero := k.ConfirmationKeys{}
	_, e = zero.Sign(claims())
	requireError(t, e)
	_, e = zero.Verify(token(), claims().ExpiresAt)
	requireError(t, e)
}
func TestConfirmationNeverImplicitlyLogsSignedMaterial(t *testing.T) {
	signed := token()
	keys := must(k.LoadConfirmationKeys(keyJSON("old", true, false)))
	for _, value := range []any{signed, keys} {
		rendered := fmt.Sprintf("%v %+v %#v", value, value, value)
		encoded := string(must(json.Marshal(value)))
		var b bytes.Buffer
		slog.New(slog.NewJSONHandler(&b, nil)).Info("check", "value", value)
		for _, text := range []string{rendered, encoded, b.String()} {
			if strings.Contains(text, signed.ForHumanResponse()) || strings.Contains(text, "key_b64") || strings.Contains(text, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))) {
				t.Fatal("signing material leaked")
			}
		}
	}
	var decoded k.ConfirmationToken
	requireError(t, json.Unmarshal([]byte(`"knowledge_confirmation"`), &decoded))
}
