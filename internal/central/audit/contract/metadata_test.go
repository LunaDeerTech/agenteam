package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"log/slog"
	"strings"
	"testing"
)

func TestMetadataClosedSchemasAndCopy(t *testing.T) {
	changed := []ChangedField{ValueChanged, PurposeChanged, ValueChanged}
	m, err := SecretMutationMetadata(SecretUpdate, foundation.Version(9007199254740993), changed)
	if err != nil {
		t.Fatal(err)
	}
	changed[0] = "private-canary"
	raw := m.JSON()
	raw[0] = 'x'
	if string(m.JSON()) != `{"version":"9007199254740993","changed_fields":["purpose","value"]}` {
		t.Fatal("metadata lost canonical fields or ownership")
	}
	if _, err := DecodeMetadata(SecretUpdate, m.JSON()); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"version":"1","changed_fields":["value"],"prompt":"private-canary"}`, `{"version":"1","version":"2","changed_fields":["value"]}`, `{"Version":"1","changed_fields":["value"]}`, `{"version":1,"changed_fields":["value"]}`, `{"version":"01","changed_fields":["value"]}`, `{"version":"1","changed_fields":["private-canary"]}`, `{"version":"1","changed_fields":null}`, `{"version":"1","changed_fields":["value"]} {}`, strings.Repeat(" ", 4097)} {
		m, err := DecodeMetadata(SecretUpdate, []byte(raw))
		if err == nil {
			t.Fatal("unregistered metadata accepted")
		}
		safe(t, m, "private-canary")
		safe(t, err, "private-canary")
	}
	if _, err := DenialMetadata(Model, Reason("private-canary"), 1); err == nil {
		t.Fatal("arbitrary reason accepted")
	}
	if _, err := NewAppendKey(SecretProducer, "private-canary", 0); err == nil {
		t.Fatal("raw business key accepted")
	}
}
func TestMetadataActionSchemasRoundTrip(t *testing.T) {
	id := "01900000-0000-7000-8000-000000000001"
	constructors := []struct {
		action Action
		build  func() (Metadata, error)
	}{
		{SecretCreate, func() (Metadata, error) { return SecretMutationMetadata(SecretCreate, 1, []ChangedField{ValueChanged}) }},
		{SecretDelete, func() (Metadata, error) { return SecretMutationMetadata(SecretDelete, 2, nil) }},
		{SecretResolve, func() (Metadata, error) { return SecretResolveMetadata(id, Model, LeaseInvalid) }},
		{MasterRegister, func() (Metadata, error) { return MasterMetadata(MasterRegister, 1, "", 0, "") }},
		{RotationStart, func() (Metadata, error) { return MasterMetadata(RotationStart, 2, id, 0, "") }},
		{RotationComplete, func() (Metadata, error) { return MasterMetadata(RotationComplete, 2, id, 25, "") }},
		{RotationFailed, func() (Metadata, error) { return MasterMetadata(RotationFailed, 2, id, 25, RotationError) }},
		{PolicyUpdate, func() (Metadata, error) { return PolicyMetadata(3, 256) }},
		{AccessDeny, func() (Metadata, error) { return DenialMetadata(MCP, AddressForbidden, 3) }},
	}
	for _, tc := range constructors {
		m, err := tc.build()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeMetadata(tc.action, m.JSON())
		if err != nil || !bytes.Equal(m.JSON(), decoded.JSON()) {
			t.Fatalf("metadata roundtrip %s: %v", tc.action, err)
		}
		if m.Validate(Action("private-canary")) == nil {
			t.Fatal("metadata crossed action")
		}
	}
}
func safe(t *testing.T, v any, canary string) {
	t.Helper()
	for _, outer := range []any{v, struct{ private any }{v}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, outer), canary) {
				t.Fatal("format leaks")
			}
		}
		raw, _ := json.Marshal(outer)
		if strings.Contains(string(raw), canary) {
			t.Fatal("JSON leaks")
		}
		for _, text := range []bool{true, false} {
			var b bytes.Buffer
			var h slog.Handler = slog.NewJSONHandler(&b, nil)
			if text {
				h = slog.NewTextHandler(&b, nil)
			}
			slog.New(h).LogAttrs(context.Background(), slog.LevelInfo, "projection", slog.Any("value", outer))
			if strings.Contains(b.String(), canary) {
				t.Fatal("slog leaks")
			}
		}
	}
}
