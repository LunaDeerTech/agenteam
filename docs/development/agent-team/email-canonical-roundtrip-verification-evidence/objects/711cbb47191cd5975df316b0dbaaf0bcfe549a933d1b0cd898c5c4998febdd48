package accountmail

import (
	"bytes"
	"errors"
	"net/mail"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestSMTPMessageCanonicalReentry(t *testing.T) {
	process, err := foundation.NewID[c.Process]()
	if err != nil {
		t.Fatal("process identity")
	}
	registry, err := NewWorkRegistry(registryProcess{process})
	if err != nil {
		t.Fatal("registry")
	}
	worker := &Worker{func() *Dependencies { return &Dependencies{PublicOrigin: "https://accounts.example.test"} }}
	fields := c.DeliveryMaterialFields{Attempt: registryAttempt(t, registry), SenderEmail: "sender@[ipv6:2001:db8::7]", Recipient: "recipient@[ipv6:2001:db8::8]"}
	if _, err := worker.message(fields); err != nil {
		t.Fatal("canonical SMTP addresses cannot prepare a message")
	}
}

func TestSMTPMessageUsesOneCanonicalWirePair(t *testing.T) {
	cases := []struct{ name, sender, recipient, senderWire, recipientWire string }{
		{"ordinary", "sender@example.test", "recipient@domain_test", "sender@example.test", "recipient@domain_test"},
		{"ipv4", "sender@[192.0.2.7]", "recipient@[192.0.2.8]", "sender@[192.0.2.7]", "recipient@[192.0.2.8]"},
		{"ipv6", "sender@[ipv6:2001:db8::7]", "recipient@[ipv6:2001:db8::8]", "sender@[IPv6:2001:db8::7]", "recipient@[IPv6:2001:db8::8]"},
		{"prefixed-ipv4", "sender@[ipv6:192.0.2.7]", "recipient@[ipv6:192.0.2.8]", "sender@[IPv6:192.0.2.7]", "recipient@[IPv6:192.0.2.8]"},
		{"mapped", "sender@[::ffff:192.0.2.7]", "recipient@[0:0:0:0:0:ffff:c000:208]", "sender@[::ffff:192.0.2.7]", "recipient@[0:0:0:0:0:ffff:c000:208]"},
		{"expanded", "sender@[ipv6:2001:0db8:0:0:0:0:0:7]", "recipient@[ipv6:::ffff:192.0.2.8]", "sender@[IPv6:2001:0db8:0:0:0:0:0:7]", "recipient@[IPv6:::ffff:192.0.2.8]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			process, _ := foundation.NewID[c.Process]()
			registry, err := NewWorkRegistry(registryProcess{process})
			if err != nil {
				t.Fatal("registry")
			}
			worker := &Worker{func() *Dependencies { return &Dependencies{PublicOrigin: "https://accounts.example.test"} }}
			fields := c.DeliveryMaterialFields{Attempt: registryAttempt(t, registry), SenderEmail: tc.sender, Recipient: tc.recipient}
			got, err := worker.message(fields)
			if err != nil {
				t.Fatal("message preparation")
			}
			defer clear(got.body)
			if got.sender != tc.senderWire || got.recipient != tc.recipientWire || fields.SenderEmail != tc.sender || fields.Recipient != tc.recipient {
				t.Fatal("wire or canonical material changed")
			}
			want := "From: <" + tc.senderWire + ">\r\nTo: <" + tc.recipientWire + ">\r\nMessage-ID: <" + fields.Attempt.Details().JobID.String() + "@accounts.example.test>\r\nSubject: Agenteam account\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nAgenteam SMTP test.\r\n"
			if string(got.body) != want {
				t.Fatal("full prepared message differs from explicit wire expectation")
			}
			fields.SenderName = "显示, Name"
			encoded, err := worker.message(fields)
			if err != nil {
				t.Fatal("display name preparation")
			}
			defer clear(encoded.body)
			parsed, err := mail.ReadMessage(bytes.NewReader(encoded.body))
			if err != nil {
				t.Fatal("header encoding")
			}
			from, err := mail.ParseAddress(parsed.Header.Get("From"))
			if err != nil || from.Name != fields.SenderName || from.Address != tc.senderWire {
				t.Fatal("display name no longer safely encoded")
			}
		})
	}
}

func TestSMTPMessageRejectsNoncanonicalAndUnsafeMaterial(t *testing.T) {
	worker := &Worker{func() *Dependencies { return &Dependencies{PublicOrigin: "https://accounts.example.test"} }}
	for _, value := range []string{"Sender@example.test", "sender@[IPv6:2001:db8::7]", "sender@[iPv6:2001:db8::7]", "sender@[ipv6:2001:DB8::7]", "sender@[ipv6:fe80::1%lo]", "x <sender@example.test>", "sender@example.test\r\nBcc: x@evil.test", "sender@example.test\x00", "sender@" + strings.Repeat("a", 248)} {
		for _, recipient := range []bool{false, true} {
			f := c.DeliveryMaterialFields{SenderEmail: "sender@example.test", Recipient: "recipient@example.test"}
			if recipient {
				f.Recipient = value
			} else {
				f.SenderEmail = value
			}
			got, err := worker.message(f)
			var fault *foundation.Fault
			if !errors.As(err, &fault) || fault.Code != foundation.InvalidArgument || got.body != nil || got.sender != "" || got.recipient != "" {
				t.Fatal("unsafe/noncanonical material produced a message")
			}
		}
	}
	for _, name := range []string{"sender\r", "sender\n", "sender\x00"} {
		if _, err := worker.message(c.DeliveryMaterialFields{SenderEmail: "sender@example.test", Recipient: "recipient@example.test", SenderName: name}); err == nil {
			t.Fatal("unsafe sender name")
		}
	}
}
