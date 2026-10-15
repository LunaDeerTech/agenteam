package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMCPOAuthCallbackOpaqueAndCanonical(t *testing.T) {
	state := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
	callback, err := NewOAuthCallback(state, "private-authorization-code", "https://issuer.example")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(callback)
	if err != nil {
		t.Fatal(err)
	}
	for _, public := range []string{string(raw), fmt.Sprintf("%v", callback), fmt.Sprintf("%+v", callback), fmt.Sprintf("%#v", callback), callback.LogValue().String()} {
		if strings.Contains(public, state) || strings.Contains(public, callback.code) || strings.Contains(public, callback.issuer) {
			t.Fatal("callback secret escaped safe projection")
		}
	}
	for _, input := range []struct{ state, code, issuer string }{
		{"", callback.code, callback.issuer},
		{state + "=", callback.code, callback.issuer},
		{base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("s", 31))), callback.code, callback.issuer},
		{state, "", callback.issuer},
		{state, "code\n", callback.issuer},
		{state, "code secret", callback.issuer},
		{state, strings.Repeat("x", 8193), callback.issuer},
		{state, callback.code, "https://issuer.example\x00"},
	} {
		if _, err := NewOAuthCallback(input.state, input.code, input.issuer); err == nil {
			t.Fatal("invalid callback accepted")
		}
	}
	if _, err := NewOAuthCallback(state, callback.code, ""); err != nil {
		t.Fatal("optional authorization-response issuer rejected", err)
	}
}
