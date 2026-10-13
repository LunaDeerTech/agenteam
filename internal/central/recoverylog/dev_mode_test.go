package recoverylog

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestDevModeRecoveryLinkValidation(t *testing.T) {
	for _, dev := range []bool{false, true} {
		for _, origin := range []string{"http://public.example.test:8080", "http://localhost:8080", "https://public.example.test", "ftp://public.example.test", "http://user@public.example.test"} {
			writer := &observedWriter{}
			sink := newSinkWithOptions(writer, Options{DevMode: dev})
			link := origin + "/invite#" + testID + "." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
			material, _ := sc.NewSecretMaterial([]byte(link))
			record, err := NewInvitationRecord(testID, testAttempt, "test@example.test", material)
			if err != nil {
				t.Fatal(err)
			}
			result, err := sink.WriteInvitation(context.Background(), record)
			material.Destroy()
			closeSink(t, sink)
			allowed := origin == "http://localhost:8080" || origin == "https://public.example.test" || dev && origin == "http://public.example.test:8080"
			if allowed && (err != nil || result.State != Written) || !allowed && (err == nil || result.State != NotWritten) {
				t.Fatalf("dev=%v origin=%s result=%s error=%v", dev, origin, result.State, err)
			}
		}
	}
	for _, suffix := range []string{"/wrong#" + testID + ".bad", "/invite?token=secret", "/invite#" + testID + ".bad", "/invite#bad." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))} {
		sink := newSinkWithOptions(&observedWriter{}, Options{DevMode: true})
		material, _ := sc.NewSecretMaterial([]byte("http://public.example.test" + suffix))
		record, _ := NewInvitationRecord(testID, testAttempt, "test@example.test", material)
		result, err := sink.WriteInvitation(context.Background(), record)
		material.Destroy()
		closeSink(t, sink)
		if err == nil || result.State != NotWritten {
			t.Fatal("development accepted an invalid recovery capability")
		}
	}
}
