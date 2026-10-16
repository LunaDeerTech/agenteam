package accountmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestEmailCodeMessageDoesNotDiscloseEnvelopeSalt(t *testing.T) {
	salt := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x7d}, 32))
	for _, purpose := range []string{"passkey_recovery", "email_change", "initialization", "operator_recovery"} {
		t.Run(purpose, func(t *testing.T) {
			raw, _ := json.Marshal(struct{ Code, Salt, Purpose string }{"004271", salt, purpose})
			defer clear(raw)
			material, err := sc.NewSecretMaterial(raw)
			if err != nil {
				t.Fatal(err)
			}
			defer material.Destroy()
			message, err := emailCodeMessage(material)
			if err != nil {
				t.Fatal(err)
			}
			defer message.Destroy()
			if err = message.Use(func(b []byte) error {
				if bytes.Count(b, []byte("004271")) != 1 || bytes.Contains(b, []byte(salt)) || bytes.Contains(b, []byte("Salt")) {
					t.Fatal("message leaked its Secret envelope or lost its code")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestEmailCodeMessageRejectsMalformedMaterial(t *testing.T) {
	salt := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, v := range []struct{ Code, Salt, Purpose string }{
		{"12345", salt, "passkey_recovery"},
		{"1234567", salt, "passkey_recovery"},
		{"1234\r\n", salt, "passkey_recovery"},
		{"１２３４５６", salt, "passkey_recovery"},
		{"123456", "short", "passkey_recovery"},
		{"123456", salt, "injected\r\nmessage"},
	} {
		raw, _ := json.Marshal(v)
		material, err := sc.NewSecretMaterial(raw)
		clear(raw)
		if err != nil {
			t.Fatal(err)
		}
		message, err := emailCodeMessage(material)
		material.Destroy()
		message.Destroy()
		if err == nil {
			t.Fatal("malformed email proof material accepted")
		}
	}
}
func TestEmailNoticeMessageNeedsNoCodeMaterial(t *testing.T) {
	job, _ := foundation.NewID[c.MailJob]()
	intent, _ := foundation.NewID[c.DeliveryIntent]()
	attempt, _ := foundation.NewID[c.Attempt]()
	process, _ := foundation.NewID[c.Process]()
	issued, err := c.NewDeliveryAttempt(c.NewDeliveryIssuer(), c.DeliveryAttemptDetails{JobID: job, IntentID: intent, AttemptID: attempt, Kind: c.EmailNoticeDelivery, ProcessID: process, Fence: 1, ConfigVersion: 1, Channel: c.SMTPChannel}, foundation.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000"))
	if err != nil {
		t.Fatal(err)
	}
	worker := &Worker{}
	for _, kind := range []c.DeliveryKind{c.EmailNoticeDelivery, c.EmailCodeDelivery} {
		details := issued.Details()
		details.Kind = kind
		value, e := c.NewDeliveryAttempt(c.NewDeliveryIssuer(), details, issued.Binding())
		if e != nil {
			t.Fatal(e)
		}
		outcome, e := worker.sendLog(context.Background(), c.DeliveryMaterialFields{Attempt: value})
		if e == nil || outcome.Result == c.DeliverySent {
			t.Fatal("email proof/security notice entered recovery log delivery")
		}
	}
	message, err := worker.link(c.DeliveryMaterialFields{Attempt: issued})
	if err != nil {
		t.Fatal("security notice unexpectedly needed a code Secret")
	}
	defer message.Destroy()
	if err = message.Use(func(b []byte) error {
		if !bytes.Contains(b, []byte("security notice")) || bytes.Contains(b, []byte("#")) {
			t.Fatal("security notice contains an unexpected link")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
