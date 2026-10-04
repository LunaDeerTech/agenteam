package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestB02SensitiveRequestsKeepExplicitProjectionOnly(t *testing.T) {
	const sensitive = "fixture-private-token-email-password"
	bid, _ := foundation.NewID[Browser]()
	expires, _ := foundation.NewInstant(time.Now().Add(time.Minute))
	b, _ := NewBrowserIdentity(NewBrowserIssuer(), bid, "fixture", expires)
	uid, _ := foundation.NewID[identity.User]()
	sid, _ := foundation.NewID[identity.Session]()
	a, _ := identity.NewHuman(uid, sid)
	password, e := sc.NewSecretMaterial([]byte(sensitive))
	if e != nil {
		t.Fatal(e)
	}
	defer password.Destroy()
	iid, _ := foundation.NewID[Invitation]()
	rid, _ := foundation.NewID[PasswordReset]()
	invite, _ := NewInvitationToken(iid, password)
	reset, _ := NewResetToken(rid, password)
	createFields := InvitationCreateFields{Actor: a, Key: "original-command", Email: sensitive}
	create, e := NewInvitationCreate(createFields)
	if e != nil {
		t.Fatal(e)
	}
	createFields.Email = "changed"
	if create.Fields().Email != sensitive {
		t.Fatal("request aliases mutable input")
	}
	challenge, _ := NewChallengeRequest(ChallengeFields{Browser: b, Email: sensitive, LoginKey: "challenge-command"})
	redeem, _ := NewInvitationRedeem(RedeemFields{Browser: b, Key: "redeem-command", Token: invite, Username: sensitive, DisplayName: sensitive, Password: password, Confirmation: password})
	request, _ := NewResetRequest(ResetRequestFields{Browser: b, Key: "reset-request", Email: sensitive, ClientIP: netip.MustParseAddr("198.51.100.4")})
	complete, _ := NewResetComplete(ResetCompleteFields{Browser: b, Key: "complete-request", Token: reset, Password: password, Confirmation: password})
	change, _ := NewPasswordChange(PasswordChangeFields{Actor: a, Key: "change-request", ExpectedVersion: 1, OldPassword: password, Password: password, Confirmation: password})
	for _, value := range []any{create, challenge, invite, reset, redeem, request, complete, change} {
		for _, wrapped := range []any{value, struct{ value any }{value}, []any{value}, map[string]any{"value": value}} {
			for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
				if strings.Contains(fmt.Sprintf(format, wrapped), sensitive) {
					t.Fatal("format disclosed private fields")
				}
			}
			var out bytes.Buffer
			slog.New(slog.NewTextHandler(&out, nil)).Info("fixture", "value", wrapped)
			if strings.Contains(out.String(), sensitive) {
				t.Fatal("slog disclosed private fields")
			}
			encoded, e := json.Marshal(wrapped)
			if e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(encoded, []byte(sensitive)) {
				t.Fatal("JSON disclosed private fields")
			}
		}
	}
	for _, v := range []any{&create, &challenge, &invite, &reset, &redeem, &request, &complete, &change} {
		if json.Unmarshal([]byte(`{}`), v) == nil {
			t.Fatal("untrusted JSON constructed a capability")
		}
	}
	if create.Fields().Email != sensitive {
		t.Fatal("failed decode modified request")
	}
	if _, e = NewInvitationRedeem(RedeemFields{Browser: b, Key: "wrong-kind", Token: reset}); e == nil {
		t.Fatal("reset used as invitation")
	}
	if _, e = NewResetComplete(ResetCompleteFields{Browser: b, Key: "wrong-kind", Token: invite}); e == nil {
		t.Fatal("invitation used as reset")
	}
}

func TestDeliveryEventClosedPayloadAndCanonicalBytes(t *testing.T) {
	id, _ := foundation.ParseID[DeliveryIntent]("01900000-0000-7000-8000-000000000003")
	codec := event.JSONCodec[DeliveryRequested]{}
	for _, kind := range []DeliveryKind{InvitationDelivery, ResetDelivery, TestDelivery} {
		v := DeliveryRequested{IntentID: id, Kind: kind}
		encoded, e := codec.Encode(v)
		if e != nil || v.Validate() != nil {
			t.Fatal(e)
		}
		want := `{"intent_id":"01900000-0000-7000-8000-000000000003","kind":"` + string(kind) + `"}`
		if string(encoded) != want {
			t.Fatal("unexpected event wire", string(encoded))
		}
		got, e := codec.Decode(encoded)
		if e != nil || got != v {
			t.Fatal("round trip", e)
		}
	}
	for _, raw := range []string{`{"intent_id":"01900000-0000-7000-8000-000000000003","kind":"invitation","email":"private@example.com"}`, `{"intent_id":"01900000-0000-7000-8000-000000000003","kind":"future"}`, `{"kind":"invitation"}`} {
		v, e := codec.Decode([]byte(raw))
		if e == nil && v.Validate() == nil {
			t.Fatal("invalid delivery payload accepted")
		}
	}
}
