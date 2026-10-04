package contract

import (
	"bytes"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestSessionsRevokedPayloadClosedScopeAndReason(t *testing.T) {
	uid, _ := foundation.ParseID[identity.User]("01900000-0000-7000-8000-000000000001")
	p := SessionsRevoked{UserID: uid, Scope: OneSession, SessionID: "01900000-0000-7000-8000-000000000002", Reason: LoggedOut}
	codec := event.JSONCodec[SessionsRevoked]{}
	b, e := codec.Encode(p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := codec.Decode(b)
	if e != nil || got.Validate() != nil || got != p {
		t.Fatal("payload roundtrip", e)
	}
	for _, v := range []SessionsRevoked{{UserID: uid, Scope: AllSessions, Reason: PasswordChanged}, {UserID: uid, Scope: AllSessions, Reason: PasswordWasReset}} {
		if v.Validate() != nil {
			t.Fatal("valid all-session reason")
		}
	}
	for _, change := range []func(*SessionsRevoked){func(v *SessionsRevoked) { v.Scope = AllSessions }, func(v *SessionsRevoked) { v.SessionID = "" }, func(v *SessionsRevoked) { v.Reason = PasswordChanged }, func(v *SessionsRevoked) { v.Scope = "future" }} {
		v := p
		change(&v)
		if v.Validate() == nil {
			t.Fatal("scope/reason mismatch")
		}
	}
	if bytes.Contains(b, []byte("password_version")) {
		t.Fatal("unapproved event data")
	}
}
