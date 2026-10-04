package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAccountMetadataClosedPhaseAndSafeProjection(t *testing.T) {
	user := "01900000-0000-7000-8000-000000000001"
	attempt := "01900000-0000-7000-8000-000000000002"
	session := "01900000-0000-7000-8000-000000000003"
	registration, _ := identity.RegisterService(identity.AccountAuth)
	actor, _ := registration.Actor(attempt, identity.SystemScope())
	resource, _ := NewResource(AccountAttemptResource, attempt)
	f := AccountMetadataFields{UserID: user, SessionID: session, AttemptID: attempt, Version: 1, Phase: AccountAuthenticated}
	m, e := AccountMetadata(AccountLogin, f)
	if e != nil {
		t.Fatal(e)
	}
	entry, e := NewEntry(EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: AccountLogin, Outcome: Success, Resource: resource, Metadata: m})
	if e != nil || entry.Validate() != nil {
		t.Fatal(e)
	}
	if _, e := NewEntry(EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: AccountLogin, Outcome: Denied, Resource: resource, Metadata: m}); e == nil {
		t.Fatal("contradictory outcome")
	}
	if _, e := DecodeMetadata(AccountLogin, m.JSON()); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(string(m.JSON()), `"version":"1"`, `"version":null`, 1), strings.Replace(string(m.JSON()), `"phase":"authenticated"`, `"phase":"sent"`, 1), strings.Replace(string(m.JSON()), `"attempt_id":`, `"ATTEMPT_ID":`, 1), strings.Replace(string(m.JSON()), `"version":"1"`, `"version":"1","version":"2"`, 1), strings.TrimSuffix(string(m.JSON()), "}") + `,"email":"private@example.com"}`, string(m.JSON()) + `{}`} {
		if _, e := DecodeMetadata(AccountLogin, []byte(raw)); e == nil {
			t.Fatal("untyped or invalid metadata accepted")
		}
	}
	changed := []AccountChangedField{DisplayNameChanged, ThemeChanged}
	m, e = AccountMetadata(AccountProfileUpdate, AccountMetadataFields{UserID: user, Version: 2, Phase: AccountUpdated, ChangedFields: changed})
	if e != nil {
		t.Fatal(e)
	}
	changed[0] = "private text"
	copy, _ := m.AccountFields()
	copy.ChangedFields[0] = "leak"
	copy, _ = m.AccountFields()
	if copy.ChangedFields[0] == "leak" || strings.Contains(string(m.JSON()), "private text") {
		t.Fatal("mutable metadata")
	}
	if _, e := AccountMetadata(AccountProfileUpdate, AccountMetadataFields{UserID: user, Version: 2, Phase: AccountUpdated, ChangedFields: []AccountChangedField{"email"}}); e == nil {
		t.Fatal("unregistered field")
	}
	if json.Unmarshal([]byte(`{}`), &m) == nil {
		t.Fatal("manufactured metadata")
	}
	for _, extra := range []func(*AccountMetadataFields){
		func(f *AccountMetadataFields) { f.ObjectID = user }, func(f *AccountMetadataFields) { f.JobID = user },
		func(f *AccountMetadataFields) { f.ResetID = user }, func(f *AccountMetadataFields) { f.InvitationID = user },
		func(f *AccountMetadataFields) { f.InitiatorID = user },
	} {
		copy := f
		extra(&copy)
		if _, e := AccountMetadata(AccountLogin, copy); e == nil {
			t.Fatal("unrelated metadata fact accepted")
		}
	}
	_ = foundation.NotStarted
}
func TestAccountActionsResourcesAndProducers(t *testing.T) {
	for _, a := range []Action{AccountBootstrap, AccountLogin, AccountLogout, AccountInviteCreate, AccountInviteRevoke, AccountInviteRedeem, AccountPasswordChange, AccountPasswordResetRequest, AccountPasswordResetComplete, AccountProfileUpdate, AccountAvatarUpdate, AccountSettingsUpdate, SMTPSettingsUpdate, SMTPTestRequest, SMTPDelivery} {
		if !a.Valid() || !ProducerFor(a).Valid() {
			t.Fatal(a)
		}
	}
	for _, a := range []Action{"account.any", "smtp.password", "account.login.extra"} {
		if a.Valid() {
			t.Fatal(a)
		}
	}
	for _, r := range []ResourceKind{UserResource, SessionResource, AccountAttemptResource, InvitationResource, PasswordResetResource, AccountSettingsResource, SMTPSettingsResource, MailJobResource} {
		if _, e := NewResource(r, ""); e == nil {
			t.Fatal("singleton manufactured without stable ID")
		}
	}
}
